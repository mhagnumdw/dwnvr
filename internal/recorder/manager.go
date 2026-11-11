package recorder

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/alert"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// bitrateSampleInterval is how often the observed rate is resampled. It
// has to be much smaller than a segment's duration so diagnostics don't stall
// between one segment and the next.
const bitrateSampleInterval = 15 * time.Second

// running is a live recorder plus what's needed to stop it on its own.
type running struct {
	rec    *Recorder
	cancel context.CancelFunc
	done   chan struct{}
}

// Manager keeps one recorder per enabled camera and lets cameras be added,
// changed and removed without restarting the process - adding a camera and
// having to restart the service for it to start recording would be a bad way
// to lose recordings.
//
// Set, Remove and Pause decide and then act, and don't queue among
// themselves: two in parallel on the same camera start two recorders, and one
// keeps recording outside the map. Whoever calls them from different
// goroutines queues them outside - the API does that with
// Server.cadastro.
type Manager struct {
	cfg    *config.Config
	client *go2rtc.Client
	store  *store.Store
	log    *slog.Logger

	mu   sync.RWMutex
	recs map[string]*running
	cams map[string]config.Camera

	// rules are the alert of each camera that alerts, built in Set. A camera
	// without an alert has no entry, and its inference does no math at all.
	rules map[string]*alert.Rule
	// dispatcher is born with the first rule and stays until dwnvr stops, so
	// the /api/health total doesn't go back to zero when the last alert is turned
	// off. Nil means "no camera turned the alert on": nothing of the alert
	// exists.
	dispatcher *alert.Dispatcher

	// queue takes the video clips of every camera to the object detector. Nil
	// without `detector.url`: then nobody infers, and no recorder keeps a GOP.
	queue *detect.Queue

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewManager(cfg *config.Config, client *go2rtc.Client, st *store.Store, log *slog.Logger) *Manager {
	m := &Manager{
		cfg: cfg, client: client, store: st, log: log,
		recs: map[string]*running{}, cams: map[string]config.Camera{},
		rules: map[string]*alert.Rule{},
	}
	if cfg.Detector.URL != "" {
		m.queue = detect.NewQueue(detect.Detectors["sidecar"](cfg.Detector.URL), m.onInference, log)
	}
	return m
}

// onInference hands the object detector's answer to the camera's recorder,
// with its alert rule, and passes the resulting alert to the Dispatcher. A
// camera that went away while its clip was waiting simply doesn't get it.
func (m *Manager) onInference(o detect.Inference) {
	m.mu.RLock()
	r := m.recs[o.Clip.Camera]
	rule, dispatcher := m.rules[o.Clip.Camera], m.dispatcher
	m.mu.RUnlock()
	if r == nil {
		return
	}
	if a, ok := r.rec.onInference(o, rule); ok {
		dispatcher.Receive(a)
	}
}

// Start brings up the recorders of the given cameras.
func (m *Manager) Start(ctx context.Context, cams []config.Camera) {
	m.ctx, m.cancel = context.WithCancel(ctx)

	for _, cam := range cams {
		m.Set(cam)
	}

	m.wg.Go(func() {
		m.sampleLoop(m.ctx)
	})

	if m.queue != nil {
		m.log.Info("object detector configured", "url", m.cfg.Detector.URL)
		m.wg.Go(func() {
			m.queue.Run(m.ctx)
		})
	}
}

// Set adds or updates a camera, starting, stopping or restarting the
// recorder as the configuration asks.
func (m *Manager) Set(raw config.Camera) {
	cam := m.cfg.Resolve(raw)
	// Without an object detector there's no inference, and the alert would have
	// nothing to tell: the rule isn't even built.
	var rule *alert.Rule
	if m.queue != nil {
		rule = alert.NewRule(cam)
	}

	m.mu.Lock()
	m.cams[cam.ID] = raw
	if rule != nil {
		m.rules[cam.ID] = rule
		m.enableAlertsLocked()
	} else {
		delete(m.rules, cam.ID)
	}
	old := m.recs[cam.ID]
	m.mu.Unlock()

	// A disabled camera must not keep a recorder running.
	if !cam.Enabled {
		if old != nil {
			m.stop(cam.ID)
			m.log.Info("camera disabled, recording stopped", "cam", cam.ID)
		}
		return
	}

	// Restart only when something the recorder actually uses changed:
	// changing the display name shouldn't open a gap in the recording.
	if old != nil {
		if !recordingParamsChanged(old.rec.camera(), cam) {
			old.rec.swapCamera(cam)
			// Detection changes mid-flight: the recording loop swaps the trigger on
			// the next frame. Reconnecting to change the sensitivity would open a gap in
			// the recording for something that doesn't depend on the connection.
			old.rec.requestDetect(cam)
			return
		}
		m.stop(cam.ID)
		m.log.Info("recording settings changed, restarting", "cam", cam.ID)
	}

	m.start(cam)
}

// enableAlertsLocked creates the Dispatcher on the first camera that turns
// the alert on, with the notifiers from dwnvr.yaml, and starts the send
// goroutine. Before Start there's no ctx for it; Start calls Set for each
// camera after creating it. With m.mu held.
func (m *Manager) enableAlertsLocked() {
	if m.dispatcher != nil {
		return
	}
	m.dispatcher = alert.NewDispatcher(alert.BuildNotifiers(m.cfg, m.log), m.log)
	if m.ctx != nil {
		c := m.dispatcher
		m.wg.Go(func() {
			c.Run(m.ctx)
		})
	}
}

// Alerts returns the Dispatcher, or nil while no camera has turned the alert on.
func (m *Manager) Alerts() *alert.Dispatcher {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dispatcher
}

// recordingParamsChanged says whether the change requires reopening the
// connection. Name, quota and detection don't: the quota is applied by
// retention, which reads the configuration on every pass, and the motion
// trigger is swapped by the recording loop itself on the next frame.
func recordingParamsChanged(a, b config.Camera) bool {
	return a.Audio != b.Audio || a.SegmentSeconds != b.SegmentSeconds ||
		a.StallSeconds != b.StallSeconds
}

func (m *Manager) start(cam config.Camera) {
	if m.ctx == nil || m.ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	rec := newRecorder(cam, m.client, m.store.Camera(cam.ID), m.log)
	if m.queue != nil {
		rec.clips = rec.offerTo(m.queue)
	}
	r := &running{rec: rec, cancel: cancel, done: make(chan struct{})}

	m.mu.Lock()
	m.recs[cam.ID] = r
	m.mu.Unlock()

	m.wg.Go(func() {
		defer close(r.done)
		rec.run(ctx)
	})
}

// stop ends a camera's recorder and waits for the open segment to be
// closed and indexed.
func (m *Manager) stop(id string) {
	m.mu.Lock()
	r := m.recs[id]
	delete(m.recs, id)
	m.mu.Unlock()

	if r == nil {
		return
	}
	r.cancel()
	<-r.done
}

// Pause stops the camera's recorder, runs fn and brings it back up.
//
// It's what allows deleting the recordings of a camera that's recording. A
// RemoveAll with the segment open would leave the writer pouring bytes into an
// already unlinked inode, and finish() would write to the index an entry
// pointing to a file that no longer exists - damage only the next boot's
// reconciliation would undo. The stop() here waits for the open segment to be
// closed and indexed before fn starts.
//
// Unlike Remove, the camera stays registered the whole time: whoever queries
// /api/health in the middle of the operation sees a stopped camera, not a
// camera that vanished. An unregistered ID - the case of a camera already
// removed - only runs fn, because there's no recorder to stop.
func (m *Manager) Pause(id string, fn func() error) error {
	m.mu.RLock()
	raw, registered := m.cams[id]
	m.mu.RUnlock()

	if !registered {
		return fn()
	}

	m.stop(id)
	// The recorder comes back even if fn fails: leaving the camera stopped
	// because of an error in something else would turn a failed operation into
	// lost recordings until someone noticed.
	cam := m.cfg.Resolve(raw)
	if cam.Enabled {
		defer m.start(cam)
	}
	return fn()
}

// Remove takes the camera out of the manager. The recordings on disk aren't
// touched here: whoever wants them deleted too asks for it explicitly in the
// API, which calls Purge after this stop. Deleting hours of video as a side
// effect of a click on "remove" would be too destructive to be implicit.
func (m *Manager) Remove(id string) {
	m.stop(id)
	m.mu.Lock()
	delete(m.cams, id)
	delete(m.rules, id)
	m.mu.Unlock()
	m.log.Info("camera removed from the manager", "cam", id)
}

// Cameras returns the current configuration, and is the source retention
// and the API use so everyone sees the same list.
func (m *Manager) Cameras() []config.Camera {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]config.Camera, 0, len(m.cams))
	for _, c := range m.cams {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *Manager) sampleLoop(ctx context.Context) {
	t := time.NewTicker(bitrateSampleInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			for _, r := range m.recorders() {
				r.sampleBitrate(now)
				r.checkSilence(now)
			}
		}
	}
}

func (m *Manager) recorders() []*Recorder {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Recorder, 0, len(m.recs))
	for _, r := range m.recs {
		out = append(out, r.rec)
	}
	return out
}

// ResetReconnects restarts the reconnection count of every live camera.
// It's the diagnostics screen button: after tinkering with a link, whoever
// administers it wants to see if the drops come back, without the old history
// mixed in.
func (m *Manager) ResetReconnects() {
	now := time.Now()
	for _, r := range m.recorders() {
		r.resetReconnects(now)
	}
}

// Status returns the health of every registered camera, including disabled
// ones - vanishing from the diagnostics screen is the worst way to signal that
// a camera stopped.
func (m *Manager) Status() []Status {
	m.mu.RLock()
	cams := make([]config.Camera, 0, len(m.cams))
	for _, c := range m.cams {
		cams = append(cams, m.cfg.Resolve(c))
	}
	recs := make(map[string]*Recorder, len(m.recs))
	for id, r := range m.recs {
		recs[id] = r.rec
	}
	m.mu.RUnlock()

	out := make([]Status, 0, len(cams))
	for _, cam := range cams {
		if r, ok := recs[cam.ID]; ok {
			out = append(out, r.Status())
			continue
		}
		// A camera without a recorder (disabled) keeps what it already
		// recorded on disk: its past must show on screen like the others',
		// otherwise disabling a camera looks like it deleted everything.
		idx := m.store.Camera(cam.ID)
		disk, oldest, newest, recorded := idx.Summary()
		st := Status{
			ID: cam.ID, Name: cam.Name, Enabled: cam.Enabled,
			QuotaMB: cam.QuotaMB, DiskBytes: disk, RecordedMs: recorded,
		}
		var span int64
		if oldest > 0 {
			st.OldestSegmentAt = time.UnixMilli(oldest)
			span = newest - oldest
		}
		// Without a recorder there's no instant rate, but the density of what's
		// on disk doesn't need it: the quota estimate shows the same as for the
		// running cameras, instead of turning into "-" just for being disabled.
		st.RetainDays = retainDays(cam.QuotaMB, disk, span, 0)
		out = append(out, st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Detector returns the snapshot of the object detector queue, or nil
// without `detector.url` - then there's no queue, and the screen has nothing
// to show.
//
// The cap is counted here because the Manager is what knows which cameras
// have detection on right now: each one may have ClipsPerCamera waiting.
func (m *Manager) Detector() *detect.QueueState {
	if m.queue == nil {
		return nil
	}
	e := m.queue.State()
	for _, r := range m.recorders() {
		if d := r.camera().Detect; d != nil && *d {
			e.Queue.Cap += detect.ClipsPerCamera
		}
	}
	return &e
}

// Stop ends everything and waits for each open segment to be closed and
// indexed. Without it, every restart would lose each camera's last minute.
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
}
