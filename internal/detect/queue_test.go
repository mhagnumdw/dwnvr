package detect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"sync"
	"testing"
	"time"
)

// fakeDetector answers whatever it's told to, and only when the test releases
// it.
type fakeDetector struct {
	mu      sync.Mutex
	seen    []string // camera of each inferred clip, in order
	release chan error
}

func (d *fakeDetector) Name() string { return "teste" }
func (d *fakeDetector) Infer(ctx context.Context, p Clip) (Output, error) {
	d.mu.Lock()
	d.seen = append(d.seen, p.Camera)
	d.mu.Unlock()
	select {
	case err := <-d.release:
		return Output{Detections: []Detection{}}, err
	case <-ctx.Done():
		return Output{}, ctx.Err()
	}
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func clipOf(cam string) Clip { return Clip{Camera: cam, Fmp4: []byte("x")} }

func TestQueueLimitsPerCameraAndNeverBlocks(t *testing.T) {
	d := &fakeDetector{release: make(chan error)}
	f := newQueue(d, func(Inference) {}, discardLogger(), 1, time.Minute, time.Millisecond)

	// No worker running: nothing leaves the queue.
	if !f.Offer(clipOf("a")) {
		t.Fatal("an empty queue refused")
	}
	if f.Offer(clipOf("a")) {
		t.Error("the same camera got in twice: it would take up the others' queue")
	}
	if !f.Offer(clipOf("b")) {
		t.Error("another camera should get in")
	}
	if f.Len() != 2 {
		t.Errorf("length %d, expected 2", f.Len())
	}
}

// TestQueueHasNoTotalCap: a busy camera at its limit doesn't refuse any other
// camera's mark, however many cameras there are - the queue's ceiling grows
// with them.
func TestQueueHasNoTotalCap(t *testing.T) {
	f := newQueue(&fakeDetector{}, func(Inference) {}, discardLogger(), 2, time.Minute, time.Millisecond)
	for i := range 40 {
		cam := fmt.Sprintf("cam%d", i)
		first, second := f.Offer(clipOf(cam)), f.Offer(clipOf(cam))
		if !first || !second {
			t.Fatalf("%s refused with %d waiting: there's no total", cam, f.Len())
		}
		if f.Offer(clipOf(cam)) {
			t.Fatalf("%s got in a third time: the limit is 2 per camera", cam)
		}
	}
}

// TestQueueServesInOrderAndFreesTheSlot: the worker takes from the queue when
// inference starts, and that's when the camera can offer again - what is
// being inferred is no longer in the queue.
func TestQueueServesInOrderAndFreesTheSlot(t *testing.T) {
	d := &fakeDetector{release: make(chan error)}
	delivered := make(chan Inference, 10)
	f := newQueue(d, func(o Inference) { delivered <- o }, discardLogger(), 1, time.Minute, time.Millisecond)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go f.Run(ctx)

	f.Offer(clipOf("a"))
	f.Offer(clipOf("b"))
	waitFor(t, func() bool { return f.Len() == 1 }) // "a" is being inferred
	if l := f.State().Queue.Cameras; !l["a"].Busy || l["a"].Waiting != 0 || l["b"].Waiting != 1 {
		t.Fatalf("cameras %+v: expected \"a\" being inferred and \"b\" waiting", l)
	}
	if !f.Offer(clipOf("a")) {
		t.Fatal("the slot of \"a\" didn't come back when its clip left the queue")
	}
	for range 3 {
		d.release <- nil
		o := <-delivered
		if o.Clip.Fmp4 != nil {
			t.Error("the returned inference still holds the video bytes")
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if got := d.seen; len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "a" {
		t.Errorf("order %v, expected [a b a]", got)
	}
}

// TestQueuePausesAfterFailure: a sidecar that's down doesn't become a tight
// loop, and the error reaches the receiver - it's what counts the failure in
// the funnel.
func TestQueuePausesAfterFailure(t *testing.T) {
	d := &fakeDetector{release: make(chan error, 10)}
	delivered := make(chan Inference, 10)
	pause := 200 * time.Millisecond
	f := newQueue(d, func(o Inference) { delivered <- o }, discardLogger(), 1, time.Minute, pause)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go f.Run(ctx)

	down := errors.New("connection refused")
	d.release <- down
	d.release <- nil
	f.Offer(clipOf("a"))
	f.Offer(clipOf("b"))

	o := <-delivered
	if !errors.Is(o.Err, down) {
		t.Fatalf("error %v, expected the object detector's", o.Err)
	}
	start := time.Now()
	<-delivered
	if d := time.Since(start); d < pause/2 {
		t.Errorf("the second inference came out %v after the failure, before the %v pause", d, pause)
	}
}

// TestQueueDoesNotPauseOnRejectedClip: corrupted video from the camera is
// routine on some cameras, and must not stop the queue for everyone for 30 s.
func TestQueueDoesNotPauseOnRejectedClip(t *testing.T) {
	d := &fakeDetector{release: make(chan error, 10)}
	delivered := make(chan Inference, 10)
	f := newQueue(d, func(o Inference) { delivered <- o }, discardLogger(), 1, time.Minute, time.Hour)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go f.Run(ctx)

	d.release <- fmt.Errorf("%w: HTTP 422", ErrClipRejected)
	d.release <- nil
	f.Offer(clipOf("a"))
	f.Offer(clipOf("b"))
	<-delivered
	select {
	case <-delivered:
	case <-time.After(5 * time.Second):
		t.Fatal("the queue paused after a rejected clip")
	}
	if f.State().Outage != nil {
		t.Error("a rejected clip marked the object detector as down")
	}
}

func TestQueueGivesUpOnStuckDetector(t *testing.T) {
	d := &fakeDetector{release: make(chan error)} // never releases
	delivered := make(chan Inference, 1)
	f := newQueue(d, func(o Inference) { delivered <- o }, discardLogger(), 1, 50*time.Millisecond, time.Millisecond)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go f.Run(ctx)
	f.Offer(clipOf("a"))
	select {
	case o := <-delivered:
		if !errors.Is(o.Err, context.DeadlineExceeded) {
			t.Errorf("error %v, expected the timeout", o.Err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the queue waited for the stuck object detector forever")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for range 500 {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("the condition never happened")
}

// TestQueueKeepsPeakAndTimes: what the Diagnostics screen shows of the queue.
// The peak stays after it empties, and the inference time doesn't count the
// failed inference - a timeout would say the object detector takes 60 s, when
// it went down.
func TestQueueKeepsPeakAndTimes(t *testing.T) {
	d := &fakeDetector{release: make(chan error)}
	delivered := make(chan Inference, 10)
	f := newQueue(d, func(o Inference) { delivered <- o }, discardLogger(), 1, time.Minute, time.Millisecond)

	if e := f.State(); e.Queue.Length != 0 || e.Times.InferenceMs != 0 {
		t.Fatalf("new queue: %+v", e)
	}
	f.Offer(clipOf("a"))
	f.Offer(clipOf("b"))
	f.Offer(clipOf("c"))
	time.Sleep(30 * time.Millisecond) // each one's wait in the queue

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go f.Run(ctx)

	waitFor(t, func() bool { return f.Len() == 2 }) // "a" is being inferred
	time.Sleep(20 * time.Millisecond)               // the inference time
	d.release <- nil
	<-delivered
	d.release <- errors.New("down")
	<-delivered

	e := f.State()
	if e.Queue.Peak != 3 {
		t.Errorf("peak %d, expected 3", e.Queue.Peak)
	}
	if e.Times.InferenceMs < 20 || e.Times.InferenceMs > 1000 {
		t.Errorf("inference %d ms: expected the one that answered, ~20 ms", e.Times.InferenceMs)
	}
	if e.Times.WaitMs < 30 {
		t.Errorf("wait %d ms, expected at least 30", e.Times.WaitMs)
	}
}

// TestQueueReportsOutageSince: the Diagnostics screen warning. "since" stays
// at the first consecutive failure, the error is the latest one's, and it all
// goes away on the first inference the object detector answers.
func TestQueueReportsOutageSince(t *testing.T) {
	d := &fakeDetector{release: make(chan error, 10)}
	delivered := make(chan Inference, 10)
	f := newQueue(d, func(o Inference) { delivered <- o }, discardLogger(), 1, time.Minute, time.Millisecond)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go f.Run(ctx)

	// The inference is delivered before the state is recorded: wait for its
	// error to show up, or for the warning to go away.
	infer := func(err error) {
		t.Helper()
		d.release <- err
		f.Offer(clipOf("a"))
		<-delivered
		waitFor(t, func() bool {
			e := f.State().Outage
			if err == nil {
				return e == nil
			}
			return e != nil && e.Err == err.Error()
		})
	}

	if f.State().Outage != nil {
		t.Fatal("a new queue already says the object detector is down")
	}
	infer(errors.New("connection refused"))
	first := f.State().Outage
	infer(errors.New("no such host"))
	if e := f.State().Outage; !e.Since.Equal(first.Since) {
		t.Errorf("since %v, expected the first failure's (%v)", e.Since, first.Since)
	}
	infer(nil)
}

// The warning shows the reason without the URL that http.Client puts in
// front, and a timeout stated with the timeout.
func TestQueueFailureReason(t *testing.T) {
	f := newQueue(&fakeDetector{}, func(Inference) {}, discardLogger(), 1, time.Minute, time.Millisecond)
	refused := errors.New("dial tcp 172.18.0.3:8480: connect: connection refused")
	tests := []struct {
		err  error
		want string
	}{
		{&url.Error{Op: "Post", URL: "http://dwnvr-detect:8480/detect?piso=0.2", Err: refused}, refused.Error()},
		{&url.Error{Op: "Post", URL: "http://dwnvr-detect:8480/detect", Err: context.DeadlineExceeded}, "não respondeu em 60 s"},
		{errors.New("sidecar answered HTTP 500: failed"), "sidecar answered HTTP 500: failed"},
	}
	for _, tt := range tests {
		if got := f.reason(tt.err); got != tt.want {
			t.Errorf("reason(%v) = %q, want %q", tt.err, got, tt.want)
		}
	}
}
