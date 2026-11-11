package detect

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"
)

// Inference is what the queue returns for each clip it serves: what the
// object detector found, or why it found nothing.
type Inference struct {
	// Clip comes without the video bytes: the answer doesn't need them, and
	// holding them until the receiver is done would be RAM for nothing.
	Clip       Clip
	Detections []Detection

	// Frame is the JPEG of the inferred frame, or nil. See Output.
	Frame []byte

	Err error
}

// Queue connects the recorders to the Detector. Cameras offer clips; ONE
// worker sends them to the object detector, one at a time, in the order they
// arrived.
//
// It exists because the object detector is slow and the cameras fire in
// bursts: a car passes three cameras, night falls on all of them at once.
// Without a limit, in a replay of three days of recordings the queue piled up
// 2.5 hours of latency. With it, whatever doesn't fit is DROPPED - it becomes
// just motion on the timeline -, and nobody waits: Offer never blocks, so the
// recording loop never feels the queue.
//
// A sidecar that's down doesn't reach recording either: the inference fails,
// the queue pauses and stays bounded, and the clips that don't fit are
// dropped.
type Queue struct {
	det     Detector
	deliver func(Inference)
	log     *slog.Logger

	perCamera      int
	timeout, pause time.Duration

	mu      sync.Mutex
	waiting []queued
	// inQueue is how many clips of each camera are waiting their turn. The
	// one being inferred has already left the queue: busy is its camera, ""
	// with the object detector idle.
	inQueue map[string]int
	busy    string

	// downSince is the first of the consecutive inferences the object detector
	// didn't answer, zero while it's up; downErr, the error of the most recent
	// one. Guarded by mu.
	downSince time.Time
	downErr   string

	// What the Diagnostics screen shows of the queue: the largest size it has
	// ever had, and the times of the latest inferences. Guarded by mu.
	peak            int
	inference, wait rollingAvg

	wake chan struct{}
}

// queued is a clip in the queue, with the instant it entered it.
type queued struct {
	p     Clip
	since time.Time
}

// QueueState is the snapshot of the queue for the Diagnostics screen.
type QueueState struct {
	Queue struct {
		// Length is the clips waiting their turn, not counting the one being
		// inferred; Peak, the largest Length since dwnvr started. Cap is the
		// ceiling, ClipsPerCamera times the cameras with detection - the
		// Manager knows how many there are, and it fills it in.
		Length int `json:"agora"`
		Cap    int `json:"cap"`
		Peak   int `json:"pico"`
		// PerCamera is ClipsPerCamera: how many clips a camera can have
		// waiting their turn. With every slot taken, its next mark is
		// dropped.
		PerCamera int `json:"porCamera"`
		// Cameras says, for the cameras with something in the queue or being
		// inferred now, how many clips are waiting their turn and whether one
		// is being inferred. A camera not in here has nothing in either.
		Cameras map[string]QueuedCamera `json:"cameras"`
	} `json:"fila"`
	Times struct {
		// InferenceMs is how long an inference takes in the object detector;
		// WaitMs, how long the clip waited its turn before it. Averages of the
		// last InferencesInAverage inferences, zero before the first. The
		// inference time only counts the inferences the object detector
		// answered: one that timed out says it went down, not how long it
		// takes.
		InferenceMs int64 `json:"analiseMs"`
		WaitMs      int64 `json:"esperaMs"`
	} `json:"tempos"`
	// Outage is only present while the object detector doesn't answer. It's
	// the Diagnostics screen warning: with it down, the cameras keep
	// recording and marking motion, and the missing object marks wouldn't be
	// noticed any other way.
	Outage *Outage `json:"foraDoAr,omitempty"`
}

// Outage is since when the object detector hasn't answered, and why.
type Outage struct {
	Since time.Time `json:"desde"`
	Err   string    `json:"erro"`
}

// QueuedCamera is what a camera has in the queue now. Waiting never goes above
// ClipsPerCamera; Busy is separate, because the clip the object detector is
// processing has already left the queue.
type QueuedCamera struct {
	Waiting int  `json:"esperando"`
	Busy    bool `json:"olhando"`
}

// rollingAvg is the average of the last InferencesInAverage durations.
type rollingAvg struct {
	last    [InferencesInAverage]time.Duration
	n, next int
}

func (m *rollingAvg) record(d time.Duration) {
	m.last[m.next] = d
	m.next = (m.next + 1) % len(m.last)
	m.n = min(m.n+1, len(m.last))
}

func (m *rollingAvg) ms() int64 {
	if m.n == 0 {
		return 0
	}
	var sum time.Duration
	for _, d := range m.last[:m.n] {
		sum += d
	}
	return (sum / time.Duration(m.n)).Milliseconds()
}

// NewQueue returns the object detector queue. `deliver` gets each inference,
// on the worker goroutine and one at a time - that's what lets the receiver
// use each camera's Marker without a lock.
func NewQueue(det Detector, deliver func(Inference), log *slog.Logger) *Queue {
	return newQueue(det, deliver, log, ClipsPerCamera,
		InferenceTimeoutMs*time.Millisecond, PauseAfterFailureMs*time.Millisecond)
}

func newQueue(det Detector, deliver func(Inference), log *slog.Logger,
	perCamera int, timeout, pause time.Duration) *Queue {
	return &Queue{
		det: det, deliver: deliver, log: log,
		perCamera: perCamera, timeout: timeout, pause: pause,
		inQueue: map[string]int{}, wake: make(chan struct{}, 1),
	}
}

// Offer puts the clip in the queue and says whether it got in: it does if its
// camera has fewer than ClipsPerCamera waiting, whatever the size of the
// queue. It never blocks: it's called from inside the recording loop.
func (f *Queue) Offer(p Clip) bool {
	f.mu.Lock()
	fits := f.inQueue[p.Camera] < f.perCamera
	if fits {
		f.waiting = append(f.waiting, queued{p, time.Now()})
		f.inQueue[p.Camera]++
		f.peak = max(f.peak, len(f.waiting))
	}
	f.mu.Unlock()
	if fits {
		select {
		case f.wake <- struct{}{}:
		default:
		}
	}
	return fits
}

// Len is how many clips are waiting now, not counting the one being inferred.
func (f *Queue) Len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.waiting)
}

// State returns the snapshot of the queue now.
func (f *Queue) State() QueueState {
	f.mu.Lock()
	defer f.mu.Unlock()
	var e QueueState
	e.Queue.Length, e.Queue.Peak, e.Queue.PerCamera = len(f.waiting), f.peak, f.perCamera
	e.Queue.Cameras = map[string]QueuedCamera{}
	for _, w := range f.waiting {
		l := e.Queue.Cameras[w.p.Camera]
		l.Waiting++
		e.Queue.Cameras[w.p.Camera] = l
	}
	if f.busy != "" {
		l := e.Queue.Cameras[f.busy]
		l.Busy = true
		e.Queue.Cameras[f.busy] = l
	}
	e.Times.InferenceMs, e.Times.WaitMs = f.inference.ms(), f.wait.ms()
	if !f.downSince.IsZero() {
		e.Outage = &Outage{Since: f.downSince, Err: f.downErr}
	}
	return e
}

// Run serves the queue until ctx is done.
func (f *Queue) Run(ctx context.Context) {
	for {
		p, ok := f.next(ctx)
		if !ok {
			return
		}
		ictx, cancel := context.WithTimeout(ctx, f.timeout)
		start := time.Now()
		out, err := f.det.Infer(ictx, p)
		took := time.Since(start)
		cancel()
		if ctx.Err() != nil {
			return // shutting down: the failure is ours, not the object detector's
		}
		if err == nil {
			f.mu.Lock()
			f.inference.record(took)
			f.mu.Unlock()
		}
		p.Fmp4 = nil
		f.deliver(Inference{Clip: p, Detections: out.Detections, Frame: out.Frame, Err: err})
		f.mu.Lock()
		f.busy = ""
		f.mu.Unlock()
		// A rejected clip means the object detector is up: no pause, no alarm.
		down := err != nil && !errors.Is(err, ErrClipRejected)
		f.recordState(down, err)
		if down {
			select {
			case <-ctx.Done():
				return
			case <-time.After(f.pause):
			}
		}
	}
}

// reason is the failure as the Diagnostics screen shows it. Without the
// `Post "<url>":` that http.Client puts in front, the same on every failure
// and too long for the warning; and a timeout is stated with the timeout,
// not as "context deadline exceeded". The log keeps the full error.
func (f *Queue) reason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("não respondeu em %d s", int(f.timeout.Seconds()))
	}
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return ue.Err.Error()
	}
	return err.Error()
}

// next takes the oldest clip from the queue, waiting if it's empty.
func (f *Queue) next(ctx context.Context) (Clip, bool) {
	for {
		f.mu.Lock()
		if len(f.waiting) > 0 {
			e := f.waiting[0]
			f.waiting[0] = queued{} // releases the bytes for the GC
			f.waiting = f.waiting[1:]
			// Leaves the queue when inference starts: the camera's slot comes
			// back now, not when the answer comes out.
			f.inQueue[e.p.Camera]--
			f.busy = e.p.Camera
			f.wait.record(time.Since(e.since))
			f.mu.Unlock()
			return e.p, true
		}
		f.mu.Unlock()
		select {
		case <-ctx.Done():
			return Clip{}, false
		case <-f.wake:
		}
	}
}

// recordState logs ONCE when the object detector goes down, and again when it
// comes back - like the stopped-camera warning. One line per failed inference
// would be hundreds an hour saying the same thing.
func (f *Queue) recordState(down bool, err error) {
	f.mu.Lock()
	was := !f.downSince.IsZero()
	switch {
	case !down:
		f.downSince, f.downErr = time.Time{}, ""
	case !was:
		f.downSince, f.downErr = time.Now(), f.reason(err)
	default:
		f.downErr = f.reason(err)
	}
	f.mu.Unlock()
	if down == was {
		return
	}
	if down {
		f.log.Error("object detector down: cameras keep recording and marking motion",
			"detector", f.det.Name(), "err", err)
		return
	}
	f.log.Info("object detector is back", "detector", f.det.Name())
}
