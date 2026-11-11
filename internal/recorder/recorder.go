// Package recorder writes, per camera, go2rtc's continuous fMP4 into
// keyframe-aligned segments - without decoding or rewriting media.
package recorder

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/alert"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/fmp4"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

const (
	// writeBufSize batches the writes. The typical destination is a USB disk
	// on a modest server, and sending 15 fragments a second straight to disk
	// would make I/O too fine-grained for nothing.
	writeBufSize = 256 << 10

	minBackoff = time.Second
	maxBackoff = 30 * time.Second

	// clockJumpThreshold is the clock jump from which we warn. Cheap SBC
	// boards often have no RTC: without network at boot they start with a wrong
	// date and NTP fixes it later, which would silently scramble the index.
	clockJumpThreshold = 5 * time.Second

	// See mediaClock and splicedStart in clock.go.
	//
	// clockBucket is how much media each estimator bucket covers. It has to
	// comfortably contain a frame delivered without a burst; 30s are 450 frames at
	// 15fps. The two buckets add up to 60s, and in that time the measured drift
	// (0.04%) doesn't reach 25ms.
	clockBucket = 30 * time.Second
	// clockTolerance is how far the timeline may drift from the clock without
	// being corrected. The time printed on the image only shows seconds, and below
	// that nobody notices; above it, each correction is still half a frame.
	clockTolerance = 250 * time.Millisecond
	// clockGap is the timeline lag from which the media stopped counting time
	// that actually passed, and the segment jumps to the clock leaving a gap.
	// Network jitter doesn't get here: the estimator already filtered it.
	clockGap = time.Second

	// minBitrateForEstimate is the rate below which the retention estimate
	// isn't published. No real camera stream stays below it.
	minBitrateForEstimate = 1.0 // kbps

	// minSpanForEstimate is the minimum history to estimate retention from
	// the density of what's recorded. Below it the measure is too short and the
	// estimate falls back to the instant rate.
	minSpanForEstimate = int64(time.Hour / time.Millisecond)
)

// retainDays estimates how many days of recording fit in the quota.
//
// It's the number that makes the quota understandable: "20 GB" says nothing,
// "≈ 8.2 days" says it all. And it's read next to "retained", which is the
// past that actually exists - so the two have to agree when the quota fills,
// otherwise the screen contradicts itself.
//
// Hence the preference for the average density of what's already on disk
// (bytes per day of history) over the instant rate: a street camera's rate
// halves at dawn and doubles in the afternoon, and dividing the quota by it
// made the estimate swing between 5 and 9.6 days on the same day - always at
// odds with a "retained" that doesn't move in the same proportion.
//
// The bias that's left is honest and is the same as "retained"'s: a camera
// that was offline for days has that time counted in the span, which dilutes
// the density and inflates the estimate. Both numbers err together, in the
// same direction, which is better than diverging. RecordedMs would give the
// density without that bias, and was left out on purpose: with it, the
// estimate would end up matching the recorded time, not the retained, when the
// quota fills.
//
// Without history that gives a measure - a new camera, which is exactly when
// the estimate helps most to choose the quota - it falls back to the instant
// rate. Without either, it returns zero and the screen shows "-" instead of
// lying.
func retainDays(quotaMB, bytes, spanMs int64, bitrateKbps float64) float64 {
	quota := float64(quotaMB) * (1 << 20)

	if bytes > 0 && spanMs >= minSpanForEstimate {
		bytesPerDay := float64(bytes) * 86400000 / float64(spanMs)
		return quota / bytesPerDay
	}
	if bitrateKbps >= minBitrateForEstimate {
		bytesPerDay := bitrateKbps * 1000 / 8 * 86400
		return quota / bytesPerDay
	}
	return 0
}

// Status is a camera's health view, consumed by the diagnostics screen.
type Status struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Enabled     bool      `json:"enabled"`
	Connected   bool      `json:"connected"`
	ConnectedAt time.Time `json:"connectedAt,omitzero"`
	BitrateKbps float64   `json:"bitrateKbps"`
	Bytes       int64     `json:"bytes"`
	Segments    int64     `json:"segments"`
	Reconnects  int64     `json:"reconnects"`
	LastError   string    `json:"lastError,omitempty"`
	VideoCodec  string    `json:"videoCodec,omitempty"`
	HasAudio    bool      `json:"hasAudio"`
	Gen         string    `json:"gen,omitempty"`

	// Audio is the configured audio, so the screen can match it against
	// HasAudio without depending on the camera list, which it doesn't reread.
	// Empty on a camera without a recorder: without a connection, there's no
	// track to expect.
	Audio string `json:"audio,omitempty"`
	// Transcoding says whether go2rtc transcodes this camera's source. It's
	// checked once per connection: the source only changes with go2rtc
	// restarting, and then every recorder reconnects.
	Transcoding bool `json:"transcoding"`

	// DisconnectedAt is when the connection dropped; only set with Connected false.
	DisconnectedAt time.Time `json:"disconnectedAt,omitzero"`

	// ReconnectsSince is the start of the Reconnects count: when the recorder
	// started, or when the count was reset from the screen. ReconnectsInWindow are
	// the ones that dropped within ReconnectWindow, and LastReconnectAt the most
	// recent of all.
	ReconnectsSince    time.Time `json:"reconnectsSince"`
	ReconnectsInWindow int64     `json:"reconnectsInWindow"`
	LastReconnectAt    time.Time `json:"lastReconnectAt,omitzero"`

	// Width and Height are the resolution being recorded, read from the
	// connection's own init - not from what the camera says it does. They're zero
	// while the camera has never connected.
	Width  uint16 `json:"width,omitempty"`
	Height uint16 `json:"height,omitempty"`

	// LastSegmentAt and Silent answer the question that matters most in an
	// NVR: "is this camera recording NOW?". Connected doesn't answer it - a
	// connection can be up without producing a single segment.
	LastSegmentAt time.Time `json:"lastSegmentAt,omitzero"`
	Silent        bool      `json:"silent"`

	// OldestSegmentAt is the start of the oldest recording the camera still
	// has on disk - the REAL retention, which is something else than RetainDays
	// just below: one says the past that exists, the other estimates what would
	// fit.
	//
	// It goes as an instant, not as already computed days, so the screen can
	// write both "12 dias 4h" and "desde 31/07" from the same field. The clock
	// difference between the server and the browser, which made uptime be sent in
	// seconds, doesn't matter here: seconds of error against days of measure.
	OldestSegmentAt time.Time `json:"oldestSegmentAt,omitzero"`

	// RecordedMs is the sum of the durations of the segments on disk: how much
	// video actually exists since OldestSegmentAt. The distance between the two is
	// the recording gaps (camera offline, power cut), which OldestSegmentAt alone
	// doesn't show.
	RecordedMs int64 `json:"recordedMs,omitempty"`

	// Detect and Onsets answer "is the motion trigger alive on this camera?".
	// Without them the only way to know is opening the day's event file on disk.
	Detect bool  `json:"detect"`
	Onsets int64 `json:"onsets"`

	// Funnel is the destination of this camera's onsets since dwnvr started.
	// It only exists with the object detector configured.
	Funnel *Funnel `json:"funil,omitempty"`

	// QuotaMB and disk Bytes feed the retention estimate shown on the cameras
	// screen ("with this quota, ~N days fit").
	QuotaMB    int64   `json:"quotaMB"`
	DiskBytes  int64   `json:"diskBytes"`
	RetainDays float64 `json:"retainDays"`
}

// Funnel says what happened to each of the camera's onsets. The slices add
// up to the Status Onsets: onsets = noVideo + dropped + failed + rejected +
// noObject + withObject + queued.
type Funnel struct {
	// Clips are the onsets whose video was cut and offered to the object
	// detector. Not a slice: it's the total that reached the queue.
	Clips int64 `json:"pedacos"`
	// NoVideo are onsets that ended without a clip to send - right after
	// connecting, before the first I frame. See Clipper.NoVideo.
	NoVideo int64 `json:"semVideo"`
	// Dropped the queue refused: the camera already had ClipsPerCamera
	// waiting. They're not inferred later either: they stay only as motion on
	// the timeline.
	Dropped int64 `json:"descartados"`
	// Failed are inferences the object detector didn't answer: down, or
	// stuck.
	Failed int64 `json:"falhas"`
	// Rejected the object detector answered it couldn't infer - video that
	// doesn't decode, which the camera itself sent corrupted.
	Rejected int64 `json:"recusados"`
	// NoObject: inferred, and nothing that ARRIVED just now - including the
	// car that was already parked.
	NoObject int64 `json:"semObjeto"`
	// WithObject: inferred, and became an object mark on the timeline.
	WithObject int64 `json:"comObjeto"`
	// Queued is the rest, what has no outcome yet: waiting for the window's
	// peak to be cut, waiting its turn in the queue, or being inferred now.
	Queued int64 `json:"naFila"`
}

// Recorder records one camera.
type Recorder struct {
	// cam is swapped with the camera running, when the Manager gets a change
	// that doesn't require reconnecting (name, quota, detection). So it's only
	// read under mu, or through the copy camera() returns.
	cam    config.Camera
	client *go2rtc.Client
	idx    *store.Camera
	log    *slog.Logger

	bytes    atomic.Int64
	segments atomic.Int64
	onsets   atomic.Int64

	// trigger is this camera's motion trigger. It belongs to the session's
	// goroutine and only it touches it - that's why it has no lock at all, and why
	// configuration changes come through detectRequest just below instead of
	// being written here directly.
	trigger detect.Method

	// clips is where the video clip the object detector should infer goes. The
	// object detector queue is what wires it; nil means "nobody infers", and then
	// the GOP isn't even kept - whoever didn't install the object detector
	// doesn't pay for the copy.
	clips func(detect.Clip)

	// clipper keeps the current GOP and cuts the clip at the frame the trigger
	// picked. Like the trigger, it belongs to the session's goroutine.
	//
	// It only exists with detection on AND a destination for the clip: it's born
	// on the first frame where both hold, and dies when detection is turned off.
	// Dying is what gives back the GOP buffers, which may have grown up to
	// MaxGOPBytes each.
	clipper *detect.Clipper

	// curInit is the current connection's init, for a clipper born in the
	// middle of it. From the session's goroutine.
	curInit []byte

	// noVideoBefore sums the NoVideo of the clippers that already died, so the
	// Funnel doesn't reset every time detection is turned off and on again. From
	// the session's goroutine.
	noVideoBefore int64

	// marker decides which of the object detector's findings become a mark.
	// It belongs to the QUEUE's goroutine, not the session's: that's what delivers
	// the inferences, one at a time. It survives reconnection on purpose - the
	// parked car is still there when the stream comes back.
	marker detect.Marker

	// The Funnel slices. noVideo is the copy of the clipper's counter, added
	// to noVideoBefore, which belong to the session's goroutine: it's published
	// here so Status reads it without a race.
	clipsCut, dropped, failed, rejected, noObject, withObject, noVideo atomic.Int64

	// detectRequest is the detection configuration the API just saved. The
	// recording loop applies it on the next frame, and that's what allows
	// changing the sensitivity without opening a gap in the recording: the API
	// doesn't wait for the recorder, and the recorder doesn't reconnect to
	// comply.
	detectRequest atomic.Pointer[detectSettings]

	mu          sync.RWMutex
	connected   bool
	connectedAt time.Time
	lastErr     string
	videoCodec  string
	width       uint16
	height      uint16
	hasAudio    bool
	transcoding bool
	gen         string
	bitrateKbps float64

	// disconnectedAt is when the connection dropped, and only holds with
	// connected false: it's the screen's "disconnected since".
	disconnectedAt time.Time

	// lastEnd is the end (wall clock) of the last closed segment. It's the
	// splice the next one starts from - see segmenter.startOf.
	lastEnd int64

	startedAt    time.Time
	silentLogged bool

	// Reconnections are counted since countingSince, which is born with the
	// recorder and moves when someone resets the count from the screen.
	// recentReconnects keeps the instant (ms) of those that dropped within
	// ReconnectWindow, and nothing beyond it: it's pruned on every insert.
	reconnects       int64
	countingSince    time.Time
	lastReconnect    time.Time
	recentReconnects []int64

	sampleAt    time.Time
	sampleBytes int64
}

// detectSettings is a camera's detection configuration, in the form it
// crosses from the API to the recording loop.
type detectSettings struct {
	on          bool
	method      string
	sensitivity int
}

func detectOf(cam config.Camera) detectSettings {
	return detectSettings{
		on:          cam.Detect != nil && *cam.Detect,
		method:      cam.DetectMethod,
		sensitivity: cam.DetectSensitivity,
	}
}

func newRecorder(cam config.Camera, client *go2rtc.Client, idx *store.Camera, log *slog.Logger) *Recorder {
	r := &Recorder{
		cam: cam, client: client, idx: idx, log: log.With("cam", cam.ID),
		startedAt: time.Now(),
	}
	r.countingSince = r.startedAt
	r.requestDetect(cam)
	return r
}

// camera returns a copy of the current camera. Whoever needs it outside mu
// - the session, which will still wait for go2rtc to answer - works with the
// copy.
func (r *Recorder) camera() config.Camera {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cam
}

// swapCamera puts the new camera in place of the old one, with the recorder running.
func (r *Recorder) swapCamera(cam config.Camera) {
	r.mu.Lock()
	r.cam = cam
	r.mu.Unlock()
}

// requestDetect tells the recording loop that this camera's detection changed.
func (r *Recorder) requestDetect(cam config.Camera) {
	a := detectOf(cam)
	r.detectRequest.Store(&a)
}

// applyPendingDetect swaps the trigger when the configuration changed. It
// runs on the hot path, so it's an atomic Swap and nothing else: without a
// request, it's out in one instruction.
func (r *Recorder) applyPendingDetect() {
	req := r.detectRequest.Swap(nil)
	if req == nil {
		return
	}
	// The clipper is only fed while there's a trigger. On the swap, its GOP
	// may have a hole - the frames from when detection was off -, and the clip the
	// old method was waiting for has no one to close it anymore.
	if r.clipper != nil {
		r.clipper.Forget()
		r.publishNoVideo()
	}
	if !req.on {
		r.trigger = nil
		if r.clipper != nil {
			r.noVideoBefore += r.clipper.NoVideo()
			r.clipper = nil
		}
		return
	}
	r.trigger = detect.NewMethod(req.method, req.sensitivity)
	r.log.Info("motion trigger on",
		"method", r.trigger.Name(), "sensitivity", req.sensitivity,
		"onsets_per_hour", detect.OnsetsPerHour[min(max(req.sensitivity, detect.MinLevel), detect.MaxLevel)])
}

// motion is the trigger's hook in the recording loop.
//
// It runs ONCE PER VIDEO FRAME, for each camera: several cameras at 15 frames
// a second on an Orange Pi Zero 3. That's why it's O(1) and doesn't allocate -
// the detect package has a test that guards those two properties - and why the
// signal is `len(box)`, which the loop already has at hand, and not a decoded
// pixel.
//
// Only VIDEO frames get here. An audio fragment arrives just like a video one
// in the stream, and its samples are also flagged as sync: without the track
// filter, every audio packet would become a giant keyframe for the
// statistics.
//
// `moof` and `mdat` are the fragment's boxes as they go to disk. The mdat is
// the signal; together they're what the clipper keeps.
func (r *Recorder) motion(atMs int64, keyframe bool, moof, mdat []byte) {
	r.applyPendingDetect()
	if r.trigger == nil {
		return
	}
	v := r.trigger.Frame(atMs, len(mdat), keyframe)
	if r.clipper == nil && r.clips != nil {
		r.clipper = detect.NewClipper(r.camera().ID)
		r.clipper.Init(r.curInit)
	}
	if r.clipper != nil {
		r.deliver(r.clipper.Frame(v, atMs, keyframe, moof, mdat))
	}
	if !v.Onset {
		return
	}
	r.onsets.Add(1)
	if err := r.idx.AppendEvent(store.Event{AtMs: atMs}); err != nil {
		r.log.Warn("writing the motion mark failed", "err", err)
	}
}

// deliver sends to the object detector the clip the clipper just cut, and
// publishes how many onsets were left without a clip so far.
func (r *Recorder) deliver(p detect.Clip, cut bool) {
	if cut {
		r.clips(p)
	}
	r.publishNoVideo()
}

func (r *Recorder) publishNoVideo() {
	r.noVideo.Store(r.noVideoBefore + r.clipper.NoVideo())
}

// offerTo wires the recorder to the object detector queue. It's the
// clips' destination, and it's called in the recording loop: the queue never
// blocks.
func (r *Recorder) offerTo(f *detect.Queue) func(detect.Clip) {
	return func(p detect.Clip) {
		r.clipsCut.Add(1)
		if !f.Offer(p) {
			r.dropped.Add(1)
		}
	}
}

// onInference gets the object detector's answer to a clip from this
// camera, on the queue's goroutine, and writes what became a mark. With the
// camera's alert rule, nil when it doesn't alert, it returns the inference's
// alert, with the same numbers as the written lines, which is what Detections
// shows.
func (r *Recorder) onInference(o detect.Inference, rule *alert.Rule) (alert.Alert, bool) {
	if errors.Is(o.Err, detect.ErrClipRejected) {
		r.rejected.Add(1)
		return alert.Alert{}, false
	}
	if o.Err != nil {
		r.failed.Add(1)
		return alert.Alert{}, false
	}
	if r.marker == nil {
		r.marker = detect.NewMarker(detect.DefaultMarker)
	}
	marks := r.marker.Mark(o.Detections)
	if len(marks) == 0 {
		r.noObject.Add(1)
		return alert.Alert{}, false
	}
	r.withObject.Add(1)

	// The frame is written ONCE, before the lines, and all lines of the same
	// instant point to the same file. Failing here doesn't cancel the mark: better
	// a detection without a thumbnail than a lost detection.
	var hasFrame bool
	if len(o.Frame) > 0 {
		day := store.Event{AtMs: o.Clip.OnsetMs}.Day()
		if err := r.idx.WriteFrame(day, o.Clip.OnsetMs, o.Frame); err != nil {
			r.log.Warn("writing the detection frame failed", "err", err)
		} else {
			hasFrame = true
		}
	}

	// One line per object that became a mark, all at the onset instant: two
	// people arriving together are two `pessoa` lines, and the area filter finds
	// the detection through any of them. Whoever wants one arrival per family,
	// like the timeline, merges on read. The order is by family and, within it,
	// by confidence: whoever reads the raw line sees what matters most first.
	slices.SortStableFunc(marks, func(a, b detect.Detection) int {
		fa := slices.Index(detect.Families, detect.ClassFamilies[a.Class])
		fb := slices.Index(detect.Families, detect.ClassFamilies[b.Class])
		return cmp.Or(cmp.Compare(fa, fb), cmp.Compare(b.Score, a.Score))
	})
	lines := make([]store.Event, 0, len(marks))
	for _, a := range marks {
		f, ok := detect.ClassFamilies[a.Class]
		if !ok {
			continue
		}
		// The box with 4 decimals: 0.0001 of the frame is less than half a
		// pixel on a 2560 px camera, and the line stays readable in a `tail`.
		var box [4]float64
		for i, v := range a.Box {
			box[i] = math.Round(v*10000) / 10000
		}
		lines = append(lines, store.Event{AtMs: o.Clip.OnsetMs, Family: string(f),
			Class: a.Class, Score: math.Round(a.Score*1000) / 1000, FrameMs: o.Clip.FrameMs,
			Box: &box, HasFrame: hasFrame})
	}

	// The alert rule runs before the write, so the `alert` mark goes into
	// the line of the object that passed.
	var al alert.Alert
	var alerted bool
	if rule != nil {
		al, alerted = rule.Evaluate(lines, o.Frame)
	}
	for _, ev := range lines {
		if err := r.idx.AppendEvent(ev); err != nil {
			r.log.Warn("writing the object mark failed", "err", err)
		}
	}
	return al, alerted
}

// funnel builds the slices. Queued comes out by difference, because it's
// the only one that isn't an event: it's what hasn't happened yet.
func (r *Recorder) funnel() *Funnel {
	if r.clips == nil {
		return nil
	}
	f := &Funnel{
		Clips: r.clipsCut.Load(), NoVideo: r.noVideo.Load(), Dropped: r.dropped.Load(),
		Failed: r.failed.Load(), Rejected: r.rejected.Load(), NoObject: r.noObject.Load(),
		WithObject: r.withObject.Load(),
	}
	f.Queued = max(0, r.onsets.Load()-f.NoVideo-f.Dropped-f.Failed-f.Rejected-f.NoObject-f.WithObject)
	return f
}

// silenceLimitLocked is how long without closing a segment is enough to
// say the camera stopped. Three segments of slack absorb the keyframe cut,
// which never lands exactly on the target duration; the one-minute floor
// avoids false alarms for whoever configured very short segments.
func (r *Recorder) silenceLimitLocked() time.Duration {
	d := 3 * time.Duration(r.cam.SegmentSeconds) * time.Second
	return max(d, time.Minute)
}

// lastActivityLocked is the most recent instant among starting, connecting
// and closing a segment - the reference for how long nothing has happened.
func (r *Recorder) lastActivityLocked() time.Time {
	ref := r.startedAt
	if r.connectedAt.After(ref) {
		ref = r.connectedAt
	}
	if r.lastEnd > 0 {
		if t := time.UnixMilli(r.lastEnd); t.After(ref) {
			ref = t
		}
	}
	return ref
}

// checkSilence warns ONCE when the camera stops recording, and again when
// it comes back.
//
// It's the gap the watchdog alone leaves: it reconnects silently, and on
// 08/09/2026 nine cameras stopped at 08:18 without anything warning - the
// problem was only found because someone went to look. In an NVR, noticing it
// stopped recording is the second most important function after recording.
func (r *Recorder) checkSilence(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	silent := now.Sub(r.lastActivityLocked()) > r.silenceLimitLocked()
	if silent == r.silentLogged {
		return
	}
	r.silentLogged = silent

	if silent {
		r.log.Error("camera stopped recording",
			"stopped_since", r.lastActivityLocked().Format(time.TimeOnly),
			"connected", r.connected, "last_error", r.lastErr)
		return
	}
	r.log.Info("camera is recording again")
}

func (r *Recorder) Status() Status {
	r.mu.RLock()
	defer r.mu.RUnlock()

	disk, oldest, newest, recorded := r.idx.Summary()
	st := Status{
		ID: r.cam.ID, Name: r.cam.Name, Enabled: r.cam.Enabled,
		Connected: r.connected, ConnectedAt: r.connectedAt,
		BitrateKbps: r.bitrateKbps,
		Bytes:       r.bytes.Load(), Segments: r.segments.Load(),
		Reconnects: r.reconnects, LastError: r.lastErr,
		ReconnectsSince: r.countingSince, LastReconnectAt: r.lastReconnect,
		ReconnectsInWindow: r.reconnectsInWindowLocked(time.Now()), DisconnectedAt: r.disconnectedAt,
		Detect: r.cam.Detect != nil && *r.cam.Detect, Onsets: r.onsets.Load(),
		Funnel:     r.funnel(),
		VideoCodec: r.videoCodec, HasAudio: r.hasAudio, Gen: r.gen,
		Audio: r.cam.Audio, Transcoding: r.transcoding,
		Width: r.width, Height: r.height,
		QuotaMB: r.cam.QuotaMB, DiskBytes: disk, RecordedMs: recorded,
		Silent: time.Since(r.lastActivityLocked()) > r.silenceLimitLocked(),
	}
	if r.lastEnd > 0 {
		st.LastSegmentAt = time.UnixMilli(r.lastEnd)
	}
	var span int64
	if oldest > 0 {
		st.OldestSegmentAt = time.UnixMilli(oldest)
		// Only with both ends is the span a span. Without the oldest,
		// `newest - 0` would be the epoch's age, and the density would come out
		// near zero.
		span = newest - oldest
	}
	st.RetainDays = retainDays(r.cam.QuotaMB, disk, span, r.bitrateKbps)
	return st
}

// sampleBitrate computes the observed rate since the last sample.
func (r *Recorder) sampleBitrate(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	total := r.bytes.Load()
	if !r.sampleAt.IsZero() {
		dt := now.Sub(r.sampleAt).Seconds()
		switch {
		case total == r.sampleBytes:
			// No byte in the whole window: the camera stopped, and the rate has
			// to say so at once. The exponential average alone only APPROACHES
			// zero - after 3h38 stopped it read 3.7e-126, a number that passes
			// as "greater than zero" and made the retention estimate become
			// 5.4e+128 days on the diagnostics screen.
			r.bitrateKbps = 0
		case dt > 0:
			inst := float64(total-r.sampleBytes) * 8 / dt / 1000
			// Exponential average: smooths the VBR swings without hiding a
			// camera that stopped sending data.
			if r.bitrateKbps == 0 {
				r.bitrateKbps = inst
			} else {
				r.bitrateKbps = 0.7*r.bitrateKbps + 0.3*inst
			}
		}
	}
	r.sampleAt, r.sampleBytes = now, total
}

// checkTranscoding asks go2rtc whether the camera's source goes through an
// ffmpeg. It's called with the camera freshly connected, which is when its
// producer is up and Transcoding() sees the command. Failing keeps the
// previous connection's value: the go2rtc-down warning is a different one.
func (r *Recorder) checkTranscoding(ctx context.Context, id string) {
	streams, err := r.client.Streams(ctx)
	if err != nil {
		return
	}
	t := false
	for _, p := range streams[id].Producers {
		t = t || p.Transcoding()
	}
	r.mu.Lock()
	r.transcoding = t
	r.mu.Unlock()
}

func (r *Recorder) setConnected(v bool, err string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	switch {
	case v:
		r.connectedAt = now
		r.disconnectedAt = time.Time{}
	case r.connected || r.disconnectedAt.IsZero():
		// Only the transition marks it: the following attempts, which also
		// fail, must not push "since" forward.
		r.disconnectedAt = now
	}
	r.connected = v
	if err != "" {
		r.lastErr = err
	}
}

// countReconnect records a drop: it adds to the total, keeps the instant
// as the last one and appends it to the window, which is pruned right here so
// it doesn't grow.
func (r *Recorder) countReconnect(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reconnects++
	r.lastReconnect = now
	cutoff := now.Add(-ReconnectWindow).UnixMilli()
	i := 0
	for i < len(r.recentReconnects) && r.recentReconnects[i] < cutoff {
		i++
	}
	r.recentReconnects = append(r.recentReconnects[i:], now.UnixMilli())
}

// reconnectsInWindowLocked counts the reconnections within the window
// without pruning the list: it runs under Status's RLock, and only whoever
// writes may touch it.
func (r *Recorder) reconnectsInWindowLocked(now time.Time) int64 {
	cutoff := now.Add(-ReconnectWindow).UnixMilli()
	var n int64
	for _, t := range r.recentReconnects {
		if t >= cutoff {
			n++
		}
	}
	return n
}

// resetReconnects restarts the count now. It doesn't touch the
// connection: only the numbers the screen shows.
func (r *Recorder) resetReconnects(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reconnects = 0
	r.countingSince = now
	r.lastReconnect = time.Time{}
	r.recentReconnects = nil
}

// run keeps the camera recording, reconnecting with exponential backoff. A
// camera with a problem must not bring the others down nor go into a tight
// loop.
func (r *Recorder) run(ctx context.Context) {
	backoff := minBackoff
	for ctx.Err() == nil {
		err := r.session(ctx)
		if ctx.Err() != nil {
			return
		}

		r.setConnected(false, errText(err))
		r.countReconnect(time.Now())
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			r.log.Warn("connection dropped", "err", err, "reconnecting_in", backoff)
		} else {
			r.log.Info("stream ended by go2rtc", "reconnecting_in", backoff)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// session is a whole connection to go2rtc, from the moov to the end of the stream.
func (r *Recorder) session(ctx context.Context) error {
	// The copy is taken once, on connecting: what it uses (audio, stall,
	// segment duration) only changes by reconnecting, and mu can't stay held
	// while go2rtc takes its time to answer.
	cam := r.camera()
	body, err := r.client.OpenStream(ctx, cam.ID, cam.Audio,
		time.Duration(cam.StallSeconds)*time.Second)
	if err != nil {
		return err
	}
	defer body.Close()

	seg := &segmenter{
		rec: r, segDur: time.Duration(cam.SegmentSeconds) * time.Second,
		clock: mediaClock{bucket: clockBucket.Milliseconds()},
	}
	defer seg.close()

	rd := fmp4.NewReader(body)
	var pending []byte

	// The moof of the connection's first keyframe waits for its mdat. The
	// mdat is where the in-band parameter sets come, which may fix the init (see
	// fixInit), and the init has to be fixed before the first segment opens,
	// because each segment starts with it. It's ONCE per connection: scanning
	// every mdat would cost 15 scans a second per camera to forever find the same
	// data again.
	var deferredMoof []byte
	var deferredFrag fmp4.Fragment

	// The current fragment: its moof was already read, its mdat not yet. An
	// audio fragment arrives exactly like a video one, and what decides whether
	// the mdat is a picture is fragIsVideo, rewritten on every moof, video or
	// audio. A loose "is keyframe", without it, would survive from a video
	// fragment to the next audio mdat, and the trigger would read audio thinking
	// it was a picture.
	var fragIsVideo, fragKeyframe bool
	var fragAtMs int64

	// The trigger's statistics don't cross a reconnection: what came before a
	// gap no longer describes the camera.
	if r.trigger != nil {
		r.trigger.Reset()
	}
	// The clip that was waiting for the window when the connection dropped is
	// still good: its video already arrived whole.
	defer func() {
		if r.clipper != nil {
			r.deliver(r.clipper.Close())
		}
	}()

	// applyMoof handles a fragment's moof: segment rotation, the clock, and
	// the moof kept in pending until its mdat arrives.
	applyMoof := func(frag fmp4.Fragment, box []byte) error {
		if fragIsVideo = frag.TrackID == seg.videoTrack.ID; fragIsVideo {
			// Before rotating: the keyframe that opens the segment also
			// counts toward the estimate of where it starts.
			seg.clock.observe(seg.mediaMs(frag.BaseDecodeTime), time.Now().UnixMilli())
			if err := seg.maybeRotate(frag); err != nil {
				return err
			}
			seg.lastEnd = frag.EndTime()
			fragKeyframe = frag.Keyframe
			// After maybeRotate: if it opened a new segment, ITS start is
			// what anchors this frame.
			fragAtMs = seg.wallTimeOf(frag.BaseDecodeTime)
		}
		pending = append(pending[:0], box...)
		if seg.open() {
			if err := fmp4.RebaseMoof(pending, seg.bases); err != nil {
				return fmt.Errorf("moof rebase: %w", err)
			}
		}
		return nil
	}

	for {
		typ, box, err := rd.NextBox()
		if err != nil {
			return err
		}

		switch typ {
		case "ftyp", "moov":
			seg.init = append(seg.init, box...)
			if typ != "moov" {
				continue
			}
			mv, err := fmp4.ParseMoov(box)
			if err != nil {
				return fmt.Errorf("unreadable moov: %w", err)
			}
			vt, ok := mv.VideoTrack()
			if !ok {
				return errors.New("stream without a video track")
			}
			seg.movie, seg.videoTrack = mv, vt
			seg.gen = "" // the new init only counts after it's fixed

			// The init's resolution is only the first approximation: if the
			// camera sends in-band parameter sets, their SPS wins. The init is
			// only written on the first keyframe, already fixed (fixInit).
			r.setResolution(vt.Width, vt.Height)

			r.mu.Lock()
			r.videoCodec, r.hasAudio = vt.Codec, mv.HasAudio()
			r.mu.Unlock()
			r.setConnected(true, "")
			// Separately: the go2rtc query must not delay the first
			// segment.
			go r.checkTranscoding(ctx, cam.ID)
			r.log.Info("connected", "codec", vt.Codec, "audio", mv.HasAudio(),
				"init_resolution", fmt.Sprintf("%dx%d", vt.Width, vt.Height))

		case "moof":
			if seg.movie == nil {
				return errors.New("moof before moov")
			}
			frag, err := fmp4.ParseMoof(box, seg.videoTrack.ID)
			if err != nil {
				return fmt.Errorf("unreadable moof: %w", err)
			}
			if seg.gen == "" && frag.TrackID == seg.videoTrack.ID && frag.Keyframe {
				deferredMoof, deferredFrag = append(deferredMoof[:0], box...), frag
				continue
			}
			if err := applyMoof(frag, box); err != nil {
				return err
			}

		case "mdat":
			if len(deferredMoof) > 0 {
				if err := r.fixInit(seg, box); err != nil {
					return err
				}
				if err := applyMoof(deferredFrag, deferredMoof); err != nil {
					return err
				}
				deferredMoof = deferredMoof[:0]
			}
			if fragIsVideo {
				r.motion(fragAtMs, fragKeyframe, pending, box)
			}
			fragIsVideo, fragKeyframe = false, false
			if len(pending) == 0 {
				continue // leftover of a partial fragment after reconnecting
			}
			if err := seg.write(pending); err != nil {
				return err
			}
			if err := seg.write(box); err != nil {
				return err
			}
			pending = pending[:0]
			seg.frags++
			if seg.frags == 1 {
				seg.firstFrag = seg.bytes - int64(len(seg.init))
			}
		}
	}
}

func (r *Recorder) setResolution(w, h uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.width, r.height = w, h
}

// fixInit writes the connection's init, on its first keyframe, and hands
// it to recording and detection. keyframe is that keyframe's mdat.
//
// Before writing, it checks the init against the in-band parameter sets: the
// SPS that comes with the keyframe wins, because that's what the decoder
// obeys. go2rtc's init may describe another stream - with H.265 from cameras
// that don't send the parameter sets in the SDP, a fixed 2560x1440 SPS for a
// 640x360 stream. Desktop Chrome shows it right, but the hardware decoder of
// some phones goes by the init and draws the image at 1/4 of the size, in a
// corner. The written init comes out already fixed (fmp4.FixInit); if the
// fixed one doesn't parse, it writes the original, and the worst case is the
// same as before.
func (r *Recorder) fixInit(seg *segmenter, keyframe []byte) error {
	vt := seg.videoTrack
	payload := fmp4.BoxPayload(keyframe)
	if init, ok := fmp4.FixInit(seg.init, vt, payload); ok {
		mv, nvt, err := readInit(init)
		if err == nil {
			r.log.Warn("the init advertises a resolution the stream doesn't use; writing the fixed init",
				"init", fmt.Sprintf("%dx%d", vt.Width, vt.Height),
				"recording", fmt.Sprintf("%dx%d", nvt.Width, nvt.Height))
			seg.init, seg.movie, seg.videoTrack = init, mv, nvt
			r.setResolution(nvt.Width, nvt.Height)
		} else {
			// Shouldn't happen: the fix only rebuilds boxes the reader
			// knows. If it does, the camera's init still gets written.
			r.log.Warn("fixed init unreadable; writing the camera's", "err", err)
		}
	}

	// The generation is the init's hash. If the SPS changes after a
	// reconnection, the hash changes and the new segments point to another init -
	// without breaking playback of the old ones.
	gen := fmp4.InitGen(seg.init)
	if err := r.idx.WriteInit(gen, seg.init); err != nil {
		return fmt.Errorf("writing init: %w", err)
	}
	seg.gen = gen
	r.curInit = seg.init
	if r.clipper != nil {
		r.clipper.Init(seg.init)
	}
	r.mu.Lock()
	r.gen = gen
	r.mu.Unlock()
	r.log.Info("init written", "gen", gen, "init_bytes", len(seg.init))
	return nil
}

// readInit reads the video track of an init (ftyp+moov).
func readInit(init []byte) (*fmp4.Movie, fmp4.Track, error) {
	rd := fmp4.NewReader(bytes.NewReader(init))
	for {
		typ, box, err := rd.NextBox()
		if err != nil {
			return nil, fmp4.Track{}, err
		}
		if typ != "moov" {
			continue
		}
		mv, err := fmp4.ParseMoov(box)
		if err != nil {
			return nil, fmp4.Track{}, err
		}
		vt, ok := mv.VideoTrack()
		if !ok {
			return nil, fmp4.Track{}, errors.New("init without a video track")
		}
		return mv, vt, nil
	}
}

// --- segmenting -------------------------------------------------------------

type segmenter struct {
	rec    *Recorder
	segDur time.Duration

	init       []byte
	gen        string
	movie      *fmp4.Movie
	videoTrack fmp4.Track

	f     *os.File
	w     *bufio.Writer
	entry store.Entry
	bases map[uint32]uint64

	baseDTS uint64
	// lastEnd is the END of the last fragment, not its start: that
	// one-frame difference is what makes splicing segments in the export result
	// in strictly increasing DTS.
	lastEnd   uint64
	bytes     int64
	frags     int
	firstFrag int64
	day       string

	clock mediaClock
	// spliced says this connection already closed a segment: the next one
	// continues the same media, and starts at the splice instead of the wall
	// clock.
	spliced bool
}

func (s *segmenter) open() bool { return s.f != nil }

// elapsed uses the media clock (tfdt), not the wall clock: that way the
// cut stays stable even when the network delivers frames in bursts.
func (s *segmenter) elapsed(dts uint64) time.Duration {
	if dts < s.baseDTS || s.videoTrack.Timescale == 0 {
		return 0
	}
	return time.Duration(float64(dts-s.baseDTS) / float64(s.videoTrack.Timescale) * float64(time.Second))
}

// mediaMs is the media instant since the start of the connection, which
// is where go2rtc counts tfdt from.
func (s *segmenter) mediaMs(dts uint64) int64 {
	if s.videoTrack.Timescale == 0 {
		return 0
	}
	return int64(float64(dts) / float64(s.videoTrack.Timescale) * 1000)
}

// wallTimeOf translates a fragment's media clock to the wall clock, which
// is the timeline's axis.
//
// It's the SAME math used to produce the series the trigger was measured on:
// the segment's start, which is wall clock, plus the media time already
// elapsed within it. Using the packet's wall-clock arrival would put the mark
// a few tenths off the video it points to, because the network delivers in
// bursts - measured at ±1.1 s in a 30 s segment.
//
// Without an open segment the wall clock is what's left. That's the window
// between connecting and the first keyframe, and no mark comes out of it: the
// trigger is still warming up.
func (s *segmenter) wallTimeOf(dts uint64) int64 {
	if !s.open() {
		return time.Now().UnixMilli()
	}
	return s.entry.StartMs + s.elapsed(dts).Milliseconds()
}

func (s *segmenter) maybeRotate(frag fmp4.Fragment) error {
	if !frag.Keyframe {
		// A segment that doesn't start at a keyframe doesn't open on its
		// own, so a keyframe is a necessary condition for any cut.
		return nil
	}
	if !s.open() {
		return s.start(frag)
	}

	// Besides the target duration, it cuts at the turn of the day: that way
	// no segment crosses midnight and each one belongs to a single daily index.
	rotate := s.elapsed(frag.BaseDecodeTime) >= s.segDur ||
		time.Now().Format(store.DayLayout) != s.day
	if !rotate {
		return nil
	}
	if err := s.finish(); err != nil {
		return err
	}
	return s.start(frag)
}

func (s *segmenter) start(frag fmp4.Fragment) error {
	startMs := s.startOf(frag)

	s.day = time.UnixMilli(startMs).Format(store.DayLayout)
	if err := s.rec.idx.EnsureDirs(s.day); err != nil {
		return err
	}

	path := s.rec.idx.SegmentPath(startMs)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	s.f, s.w = f, bufio.NewWriterSize(f, writeBufSize)
	s.baseDTS, s.lastEnd = frag.BaseDecodeTime, frag.EndTime()
	s.bytes, s.frags, s.firstFrag = 0, 0, 0
	s.entry = store.Entry{StartMs: startMs, Gen: s.gen, InitSize: int64(len(s.init))}

	// Each track's base is the same keyframe instant, converted to its
	// timescale: zeroing the tracks independently would lose the real offset
	// between audio and video.
	s.bases = make(map[uint32]uint64, len(s.movie.Tracks))
	for _, t := range s.movie.Tracks {
		s.bases[t.ID] = fmp4.ScaleTime(frag.BaseDecodeTime, s.videoTrack.Timescale, t.Timescale)
	}

	// Each segment carries its own init: it opens in VLC, ffprobe and a
	// <video> without any preprocessing.
	return s.write(s.init)
}

// startOf decides the timeline instant where the segment opened by frag
// starts.
//
// The start comes from the wall clock, but the duration comes from the media
// clock (the camera's timestamps). If each segment started at the time it
// arrived, network jitter would make the new one start BEFORE the previous one
// ended, and in MSE the overlapped stretch is overwritten - recordings are
// lost. So, within a connection, the segment starts at the splice with the
// previous one, corrected little by little toward the clock (splicedStart).
//
// The connection's first segment has no splice: the media started over from
// zero, and what happened between connections was time without recording. It
// follows the clock, only never starting before the end of the last recorded
// segment - which also covers the clock jumping backwards on servers without
// an RTC.
func (s *segmenter) startOf(frag fmp4.Fragment) int64 {
	lastEnd := s.rec.lastEndMs()
	// Without an observation there's no estimate; it doesn't happen, because
	// the fragment that opens the segment is observed first, but the arrival
	// time is the guess the old code used.
	target, ok := s.clock.wall(s.mediaMs(frag.BaseDecodeTime))
	if !ok {
		target = time.Now().UnixMilli()
	}

	if lastEnd-target > clockJumpThreshold.Milliseconds() {
		s.rec.log.Warn("segment start long before the end of the previous one",
			"estimated", time.UnixMilli(target), "prev_end_ms", lastEnd,
			"diff_ms", lastEnd-target)
	}

	if !s.spliced {
		return max(target, lastEnd)
	}
	return splicedStart(lastEnd, target, s.halfFrameMs(frag),
		clockTolerance.Milliseconds(), clockGap.Milliseconds())
}

// halfFrameMs is the size of each start correction - see splicedStart. It
// comes from the keyframe itself because the frame rate is the camera's, not
// a constant: half a frame at 15fps is 33ms, at 30fps it's 16ms. The 1ms
// floor is so a camera above 500fps, which doesn't exist, doesn't turn the
// correction off.
func (s *segmenter) halfFrameMs(frag fmp4.Fragment) int64 {
	if frag.SampleCount == 0 || s.videoTrack.Timescale == 0 {
		return 1
	}
	frame := float64(frag.Duration) / float64(frag.SampleCount) / float64(s.videoTrack.Timescale) * 1000
	return max(int64(frame/2), 1)
}

func (s *segmenter) write(b []byte) error {
	if s.w == nil {
		return nil
	}
	n, err := s.w.Write(b)
	s.bytes += int64(n)
	s.rec.bytes.Add(int64(n))
	return err
}

// finish closes the segment and only then records it in the index. That
// order matters: a crash between the two leaves an orphan file, which the
// boot reconciliation brings back in - while the reverse order would leave the
// index pointing to a file that never existed.
func (s *segmenter) finish() error {
	if s.f == nil {
		return nil
	}
	err := s.w.Flush()
	if cerr := s.f.Close(); err == nil {
		err = cerr
	}
	s.f, s.w = nil, nil
	if err != nil {
		return err
	}

	s.entry.DurMs = s.elapsed(s.lastEnd).Milliseconds()
	s.entry.Size = s.bytes
	s.entry.FirstFrag = s.firstFrag

	if err := s.rec.idx.Append(s.entry); err != nil {
		return err
	}
	s.rec.setLastEndMs(s.entry.StartMs + s.entry.DurMs)
	s.spliced = true
	s.rec.segments.Add(1)
	s.rec.log.Debug("segment closed", "start", s.entry.StartMs,
		"dur_s", float64(s.entry.DurMs)/1000, "mb", float64(s.entry.Size)/(1<<20))
	return nil
}

func (s *segmenter) close() {
	if err := s.finish(); err != nil {
		s.rec.log.Error("closing the segment failed", "err", err)
	}
}

func (r *Recorder) lastEndMs() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastEnd
}

func (r *Recorder) setLastEndMs(ms int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastEnd = ms
}
