package recorder

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// --- synthetic boxes --------------------------------------------------------
//
// The minimum fMP4 for the recording loop to run: a moov with a video track and
// an audio track, and moof+mdat pairs of both. That's enough to reproduce the
// trap this test exists to guard - the audio fragment arrives exactly like the
// video one.

const (
	flagsIFrame = 0x02000000
	flagsPFrame = 0x01010000

	videoID, audioID = uint32(1), uint32(2)
	videoTimescale   = uint32(90000)
)

func u32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func u64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func box(typ string, parts ...[]byte) []byte {
	var body []byte
	for _, p := range parts {
		body = append(body, p...)
	}
	out := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(out[0:4], uint32(8+len(body)))
	copy(out[4:8], typ)
	return append(out, body...)
}

func track(id, timescale uint32, kind string, sample []byte) []byte {
	return box("trak",
		box("tkhd", u32(0), u32(0), u32(0), u32(id)),
		box("mdia",
			box("mdhd", u32(0), u32(0), u32(0), u32(timescale), u32(0)),
			box("hdlr", u32(0), u32(0), []byte(kind)),
			box("minf", box("stbl", box("stsd", u32(0), u32(1), sample)))))
}

func moovWithAudio() []byte {
	// 78 bytes of fixed fields is the header of the video sample entry.
	video := track(videoID, videoTimescale, "vide", box("avc1", make([]byte, 78)))
	audio := track(audioID, 16000, "soun", box("fLaC"))
	trex := append(
		box("trex", u32(0), u32(videoID), u32(1), u32(0), u32(0), u32(flagsPFrame)),
		box("trex", u32(0), u32(audioID), u32(1), u32(0), u32(0), u32(0))...)
	return box("moov", box("mvhd", u32(0)), video, audio, box("mvex", trex))
}

func moof(id uint32, dts uint64, flags uint32, dur uint32) []byte {
	return box("moof",
		box("mfhd", u32(0), u32(1)),
		box("traf",
			box("tfhd", u32(0), u32(id)),
			box("tfdt", []byte{1, 0, 0, 0}, u64(dts)),
			// 0x000105 = data-offset | first-sample-flags | sample-duration
			box("trun", u32(0x000105), u32(1), u32(0), u32(flags), u32(dur))))
}

// mdat with `n` bytes of payload, so that len(box) - which is the trigger's
// signal - is n+8.
func mdat(n int) []byte { return box("mdat", make([]byte, n)) }

// --- the spy ----------------------------------------------------------------

// spy is a Method that decides nothing: it just notes everything the hook hands
// it. It's how one sees, from outside, what the recording loop is treating as
// "a video frame".
type spy struct {
	instants []int64
	sizes    []int
	keys     []bool
	zeros    int
}

func (e *spy) Name() string { return "spy" }
func (e *spy) Reset()       { e.zeros++ }

func (e *spy) Frame(atMs int64, bytes int, keyframe bool) detect.Verdict {
	e.instants = append(e.instants, atMs)
	e.sizes = append(e.sizes, bytes)
	e.keys = append(e.keys, keyframe)
	return detect.Verdict{}
}

// recordStream starts a fake go2rtc that serves `body` and closes, runs a whole
// recorder session against it and returns what the hook saw.
func recordStream(t *testing.T, body []byte) (*spy, *store.Camera) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	idx := store.New(t.TempDir()).Camera("cam_teste")
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30}
	r := newRecorder(cam, go2rtc.New(config.Go2RTC{URL: srv.URL}), idx, discardLogger())

	// The spy takes the place of the real trigger. The pending request
	// newRecorder left is discarded, or else the first frame would replace it.
	esp := &spy{}
	r.detectRequest.Store(nil)
	r.trigger = esp

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = r.session(ctx) // ends in EOF when the body runs out
	return esp, idx
}

// TestHookIgnoresAudioFrames guards the `keyframePending` trap: an audio
// fragment arrives exactly like a video one, and audio samples also come flagged
// as sync. Without the track filter, each audio packet would enter the
// statistics as a giant I-frame - and the camera's baseline would become the
// audio's.
func TestHookIgnoresAudioFrames(t *testing.T) {
	var body []byte
	body = append(body, box("ftyp", []byte("isom"))...)
	body = append(body, moovWithAudio()...)

	// Video: one 5,000-byte I-frame and two 100-byte P-frames. Between them,
	// two audio packets of 900 bytes each.
	body = append(body, moof(videoID, 0, flagsIFrame, 6000)...)
	body = append(body, mdat(5000)...)
	body = append(body, moof(audioID, 0, 0, 1024)...)
	body = append(body, mdat(900)...)
	body = append(body, moof(videoID, 6000, flagsPFrame, 6000)...)
	body = append(body, mdat(100)...)
	body = append(body, moof(audioID, 1024, 0, 1024)...)
	body = append(body, mdat(900)...)
	body = append(body, moof(videoID, 12000, flagsPFrame, 6000)...)
	body = append(body, mdat(100)...)

	esp, _ := recordStream(t, body)

	if len(esp.sizes) != 3 {
		t.Fatalf("the hook saw %d frames, want the 3 video ones: %v",
			len(esp.sizes), esp.sizes)
	}
	expectedOnes := []int{5008, 108, 108}
	for i, want := range expectedOnes {
		if esp.sizes[i] != want {
			t.Errorf("frame %d: %d bytes, want %d - audio leaked into the trigger",
				i, esp.sizes[i], want)
		}
	}
	// Only the first is an I-frame. If the video keyframe survived the audio
	// mdat, the next frame would also show up as a key.
	if !esp.keys[0] || esp.keys[1] || esp.keys[2] {
		t.Errorf("wrong I-frame flags: %v", esp.keys)
	}
}

// TestHookAnchorsOnTheTimelineClock: each frame's instant is the segment start
// (wall clock) plus the media time elapsed within it. It's the same math used
// to measure the trigger, and it's what makes the mark land on the video it
// points to.
func TestHookAnchorsOnTheTimelineClock(t *testing.T) {
	var body []byte
	body = append(body, box("ftyp", []byte("isom"))...)
	body = append(body, moovWithAudio()...)
	// Frames every 6000/90000 s = 66.7 ms.
	for i := range 5 {
		flags := uint32(flagsPFrame)
		if i == 0 {
			flags = flagsIFrame
		}
		body = append(body, moof(videoID, uint64(i)*6000, flags, 6000)...)
		body = append(body, mdat(100)...)
	}

	before := time.Now().UnixMilli()
	esp, _ := recordStream(t, body)
	after := time.Now().UnixMilli()

	if len(esp.instants) != 5 {
		t.Fatalf("want 5 frames, got %d", len(esp.instants))
	}
	// The first frame opens the segment, so it lands on the wall clock.
	if esp.instants[0] < before || esp.instants[0] > after {
		t.Errorf("first frame at %d, outside the window [%d, %d]",
			esp.instants[0], before, after)
	}
	// The following ones move with the MEDIA clock, not the arrival one: 66 ms
	// each, even if the network delivered all five at once.
	for i := 1; i < len(esp.instants); i++ {
		got := esp.instants[i] - esp.instants[0]
		want := int64(i) * 6000 * 1000 / int64(videoTimescale)
		if got != want {
			t.Errorf("frame %d at %d ms from the start, want %d ms", i, got, want)
		}
	}
}

// TestHookWritesTheOnsetToTheDayFile closes the loop: what the method approves
// becomes a line on disk, in the day of its own instant.
func TestHookWritesTheOnsetToTheDayFile(t *testing.T) {
	var body []byte
	body = append(body, box("ftyp", []byte("isom"))...)
	body = append(body, moovWithAudio()...)
	body = append(body, moof(videoID, 0, flagsIFrame, 6000)...)
	body = append(body, mdat(100)...)
	body = append(body, moof(videoID, 6000, flagsPFrame, 6000)...)
	body = append(body, mdat(100)...)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	idx := store.New(t.TempDir()).Camera("cam_teste")
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30}
	r := newRecorder(cam, go2rtc.New(config.Go2RTC{URL: srv.URL}), idx, discardLogger())
	r.detectRequest.Store(nil)
	r.trigger = alwaysFires{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = r.session(ctx)

	if got := r.onsets.Load(); got != 2 {
		t.Fatalf("onset counter = %d, want 2", got)
	}
	evs, err := idx.LoadEvents(time.Now().Format(store.DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("want 2 marks in the day file, got %d", len(evs))
	}
}

type alwaysFires struct{}

func (alwaysFires) Name() string { return "always" }
func (alwaysFires) Reset()       {}
func (alwaysFires) Frame(int64, int, bool) detect.Verdict {
	return detect.Verdict{Onset: true, Candidate: true}
}

// TestDetectChangeMidFlightDoesNotRace guards the promise of requestDetect: the
// API changes a camera's sensitivity while the recording loop is in the middle
// of a session, with no shared lock and no reconnection. Run with -race, it's
// what proves that passing through the atomic pointer is enough.
func TestDetectChangeMidFlightDoesNotRace(t *testing.T) {
	var head []byte
	head = append(head, box("ftyp", []byte("isom"))...)
	head = append(head, moovWithAudio()...)

	// A go2rtc that delivers frames slowly, so the session is still alive while
	// the changes happen.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(head)
		fl, _ := w.(http.Flusher)
		for i := range 300 {
			flags := uint32(flagsPFrame)
			if i%30 == 0 {
				flags = flagsIFrame
			}
			_, _ = w.Write(moof(videoID, uint64(i)*6000, flags, 6000))
			_, _ = w.Write(mdat(100 + i%7))
			if fl != nil {
				fl.Flush()
			}
			if r.Context().Err() != nil {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}))
	defer srv.Close()

	idx := store.New(t.TempDir()).Camera("cam_teste")
	on := true
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30,
		Detect: &on, DetectMethod: "kleinberg-p", DetectSensitivity: 4}
	r := newRecorder(cam, go2rtc.New(config.Go2RTC{URL: srv.URL}), idx, discardLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	end := make(chan struct{})
	go func() {
		defer close(end)
		_ = r.session(ctx)
	}()

	// On the "API" side: on, off, switch method and level, nonstop.
	off := false
	for i := range 200 {
		c := cam
		switch i % 3 {
		case 0:
			c.Detect = &off
		case 1:
			c.DetectMethod, c.DetectSensitivity = "periodico", 1+i%5
		default:
			c.DetectSensitivity = 1 + i%5
		}
		r.requestDetect(c)
		time.Sleep(200 * time.Microsecond)
	}
	<-end
}

// script is a Method that returns hand-written verdicts, one per video frame.
// It's how one picks, from outside, where the clip must be cut.
type script struct {
	verdicts []detect.Verdict
	i        int
}

func (r *script) Name() string { return "script" }
func (r *script) Reset()       {}
func (r *script) Frame(int64, int, bool) detect.Verdict {
	var v detect.Verdict
	if r.i < len(r.verdicts) {
		v = r.verdicts[r.i]
	}
	r.i++
	return v
}

// TestHookCutsTheClipAsItWentToDisk closes the clipper's loop inside the
// recording loop: the clip is the connection's init followed by the VIDEO
// fragments from the I-frame up to the frame to look at, with the same bytes
// the segment recorded - audio stays out, and the candidate left in the old GOP
// survives the next I-frame.
func TestHookCutsTheClipAsItWentToDisk(t *testing.T) {
	head := append(box("ftyp", []byte("isom")), moovWithAudio()...)
	body := append([]byte(nil), head...)
	// Video frames: I P P P I P. Audio between them.
	keys := []bool{true, false, false, false, true, false}
	var fragsVideo [][]byte
	for i, key := range keys {
		flags := uint32(flagsPFrame)
		if key {
			flags = flagsIFrame
		}
		m, d := moof(videoID, uint64(i)*6000, flags, 6000), mdat(100+i)
		body = append(body, m...)
		body = append(body, d...)
		fragsVideo = append(fragsVideo, append(append([]byte(nil), m...), d...))
		body = append(body, moof(audioID, uint64(i)*1024, 0, 1024)...)
		body = append(body, mdat(50)...)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	idx := store.New(dir).Camera("cam_teste")
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30}
	r := newRecorder(cam, go2rtc.New(config.Go2RTC{URL: srv.URL}), idx, discardLogger())
	r.detectRequest.Store(nil)
	// Onset at frame 1, peak at 2, I-frame at 4 within the window, cut at 5.
	r.trigger = &script{verdicts: []detect.Verdict{
		{}, {Onset: true, Candidate: true}, {Candidate: true}, {}, {}, {Cut: true},
	}}
	var out []detect.Clip
	r.clips = func(p detect.Clip) { out = append(out, p) }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = r.session(ctx)

	if len(out) != 1 {
		t.Fatalf("%d clips, want 1", len(out))
	}
	p := out[0]
	if !bytes.HasPrefix(p.Fmp4, head) {
		t.Fatal("the clip doesn't start with the connection's init")
	}
	fragments := p.Fmp4[len(head):]

	// The fragments are the ones that went to disk, byte for byte - the moof
	// already rebased. That's what makes the clip decode the same as the
	// recorded segment.
	segs, _ := filepath.Glob(filepath.Join(dir, "cam_teste", "2*", "*.mp4"))
	if len(segs) != 1 {
		t.Fatalf("want 1 segment on disk, found %v", segs)
	}
	recorded, err := os.ReadFile(segs[0])
	if err != nil {
		t.Fatal(err)
	}
	// On disk the audio sits between one frame and the next; in the clip, it
	// doesn't. So the check is box by box: each moof+mdat of the clip is in the
	// segment.
	var boxes [][]byte
	for rest := fragments; len(rest) >= 8; {
		n := int(binary.BigEndian.Uint32(rest))
		if n < 8 || n > len(rest) {
			t.Fatalf("box of %d bytes in a remainder of %d", n, len(rest))
		}
		boxes, rest = append(boxes, rest[:n]), rest[n:]
	}
	// Exactly the video ones from the I-frame to the peak: frames 0, 1 and 2,
	// no audio.
	if len(boxes) != 6 {
		t.Fatalf("%d boxes in the clip, want 6 (moof+mdat of three frames)", len(boxes))
	}
	for i := 0; i < len(boxes); i += 2 {
		pair := append(append([]byte(nil), boxes[i]...), boxes[i+1]...)
		if !bytes.Contains(recorded, pair) {
			t.Errorf("clip frame %d doesn't have the bytes the segment recorded", i/2)
		}
		if len(pair) != len(fragsVideo[i/2]) {
			t.Errorf("clip frame %d with %d bytes, the video one has %d", i/2, len(pair), len(fragsVideo[i/2]))
		}
	}
	// The onset is frame 1 and the peak is 2, on the media clock truncated to
	// ms.
	ms := func(q int64) int64 { return q * 6000 * 1000 / int64(videoTimescale) }
	if p.FrameMs-p.OnsetMs != ms(2)-ms(1) {
		t.Errorf("onset %d and frame %d: the frame to look at should be the one after the onset",
			p.OnsetMs, p.FrameMs)
	}
}

// TestHookWithoutDestinationKeepsNoGOP: whoever didn't install the object
// detector doesn't pay for the GOP copy.
func TestHookWithoutDestinationKeepsNoGOP(t *testing.T) {
	var body []byte
	body = append(body, box("ftyp", []byte("isom"))...)
	body = append(body, moovWithAudio()...)
	body = append(body, moof(videoID, 0, flagsIFrame, 6000)...)
	body = append(body, mdat(100)...)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	idx := store.New(t.TempDir()).Camera("cam_teste")
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30}
	r := newRecorder(cam, go2rtc.New(config.Go2RTC{URL: srv.URL}), idx, discardLogger())
	r.detectRequest.Store(nil)
	r.trigger = alwaysFires{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = r.session(ctx)
	if r.clipper != nil {
		t.Error("no destination for the clip, the clipper shouldn't exist")
	}
}

// personDetector always answers the same person, in the same place.
type personDetector struct{}

func (personDetector) Name() string { return "person" }
func (personDetector) Infer(context.Context, detect.Clip) (detect.Output, error) {
	return detect.Output{
		Detections: []detect.Detection{{Class: "person", Score: 0.87654, Box: [4]float64{0.1, 0.1, 0.2, 0.4}}},
		Frame:      []byte("\xff\xd8test-jpeg\xff\xd9"),
	}, nil
}

// TestFromOnsetToObjectMark closes the whole path on dwnvr's side: the onset
// becomes a clip, the clip goes through the queue and the object detector,
// tracking lets the first person become a mark - and not the second, in the
// same place -, and the mark goes to the day file at the ONSET instant. The
// funnel adds up.
func TestFromOnsetToObjectMark(t *testing.T) {
	// The stream arrives in two halves, with a pause between them: the second
	// onset must find the first one already out of the queue, being inferred -
	// the test doesn't depend on how many places the camera has.
	var halves [2][]byte
	halves[0] = append(box("ftyp", []byte("isom")), moovWithAudio()...)
	for i := range 6 {
		flags := uint32(flagsPFrame)
		if i%3 == 0 {
			flags = flagsIFrame
		}
		halves[i/3] = append(halves[i/3], moof(videoID, uint64(i)*6000, flags, 6000)...)
		halves[i/3] = append(halves[i/3], mdat(100)...)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(halves[0])
		w.(http.Flusher).Flush()
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write(halves[1])
	}))
	defer srv.Close()

	idx := store.New(t.TempDir()).Camera("cam_teste")
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30}
	r := newRecorder(cam, go2rtc.New(config.Go2RTC{URL: srv.URL}), idx, discardLogger())
	r.detectRequest.Store(nil)
	// Two onsets, each with the window closing on the next frame.
	r.trigger = &script{verdicts: []detect.Verdict{
		{}, {Onset: true, Candidate: true}, {Cut: true},
		{}, {Onset: true, Candidate: true}, {Cut: true},
	}}
	inferences := make(chan detect.Inference, 4)
	queue := detect.NewQueue(personDetector{}, func(o detect.Inference) {
		r.onInference(o, nil)
		inferences <- o
	}, discardLogger())
	r.clips = r.offerTo(queue)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go queue.Run(ctx)

	_ = r.session(ctx)
	var onsets, frames []int64
	for range 2 {
		select {
		case o := <-inferences:
			onsets = append(onsets, o.Clip.OnsetMs)
			frames = append(frames, o.Clip.FrameMs)
		case <-ctx.Done():
			t.Fatal("the queue didn't return both inferences")
		}
	}

	evs, err := idx.LoadEvents(time.Now().Format(store.DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	var objects []store.Event
	for _, ev := range evs {
		if ev.IsObject() {
			objects = append(objects, ev)
		}
	}
	if len(objects) != 1 {
		t.Fatalf("%d object marks, want 1: the person standing in the same place can't mark again", len(objects))
	}
	// The box is checked apart: it's a pointer, and != would compare the
	// address.
	got := objects[0]
	box := got.Box
	got.Box = nil
	jpeg := []byte("\xff\xd8test-jpeg\xff\xd9")
	want := store.Event{AtMs: onsets[0], Family: "pessoa", Class: "person", Score: 0.877,
		FrameMs: frames[0], HasFrame: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mark %+v, want %+v", got, want)
	}
	if box == nil || *box != [4]float64{0.1, 0.1, 0.2, 0.4} {
		t.Errorf("box %v, want the object detector's, [0.1 0.1 0.2 0.4]", box)
	}

	// The frame the Detections screen shows: the file is named after the mark's
	// instant and holds the bytes the object detector sent.
	parsed, err := os.ReadFile(idx.FramePath(time.Now().Format(store.DayLayout), got.AtMs))
	if err != nil {
		t.Fatalf("reading the mark's frame: %v", err)
	}
	if !bytes.Equal(parsed, jpeg) {
		t.Errorf("recorded frame %q, want %q", parsed, jpeg)
	}

	f := r.funnel()
	if f.Clips != 2 || f.WithObject != 1 || f.NoObject != 1 || f.Queued != 0 || f.NoVideo != 0 {
		t.Errorf("funnel %+v: want 2 clips, 1 with object, 1 without, nothing queued nor without video", *f)
	}
}

// TestOneLinePerObject: two people and a motorcycle arriving together are three
// lines at the same instant, not one per family. The lower-confidence person is
// the one the area filter may need. The box below the cut doesn't become a
// mark, and the frame is still just one.
func TestOneLinePerObject(t *testing.T) {
	idx := store.New(t.TempDir()).Camera("cam_teste")
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30}
	r := newRecorder(cam, go2rtc.New(config.Go2RTC{}), idx, discardLogger())

	now := time.Now().UnixMilli()
	r.onInference(detect.Inference{
		Clip: detect.Clip{OnsetMs: now, FrameMs: now + 1400},
		// Out of order on purpose: recording sorts by family and confidence.
		Detections: []detect.Detection{
			{Class: "motorcycle", Score: 0.71, Box: [4]float64{0.60, 0.40, 0.72, 0.66}},
			{Class: "person", Score: 0.55, Box: [4]float64{0.18, 0.30, 0.24, 0.64}},
			{Class: "cat", Score: 0.30, Box: [4]float64{0.80, 0.70, 0.85, 0.80}},
			{Class: "person", Score: 0.87, Box: [4]float64{0.41, 0.32, 0.47, 0.62}},
		},
		Frame: []byte("\xff\xd8test-jpeg\xff\xd9"),
	}, nil)

	day := time.Now().Format(store.DayLayout)
	evs, err := idx.LoadEvents(day)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		family, class string
		score         float64
		box           [4]float64
	}
	want := []row{
		{"pessoa", "person", 0.87, [4]float64{0.41, 0.32, 0.47, 0.62}},
		{"pessoa", "person", 0.55, [4]float64{0.18, 0.30, 0.24, 0.64}},
		{"veiculo", "motorcycle", 0.71, [4]float64{0.60, 0.40, 0.72, 0.66}},
	}
	if len(evs) != len(want) {
		t.Fatalf("%d lines, want %d: %+v", len(evs), len(want), evs)
	}
	for i, ev := range evs {
		got := row{ev.Family, ev.Class, ev.Score, *ev.Box}
		if got != want[i] {
			t.Errorf("line %d: %+v, want %+v", i, got, want[i])
		}
		if ev.AtMs != now || ev.FrameMs != now+1400 || !ev.HasFrame {
			t.Errorf("line %d: instant %d, frame %d, hasFrame %v; want %d, %d and true",
				i, ev.AtMs, ev.FrameMs, ev.HasFrame, now, now+1400)
		}
	}

	frames, err := os.ReadDir(filepath.Dir(idx.FramePath(day, now)))
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Errorf("%d frames recorded, want 1 for the three lines", len(frames))
	}
}

// TestClipperLivesAndDiesWithDetection: with the object detector configured,
// the camera with detection off keeps no GOP, and turning detection off
// releases the buffers. What was already counted as no video isn't reset by the
// change.
func TestClipperLivesAndDiesWithDetection(t *testing.T) {
	idx := store.New(t.TempDir()).Camera("cam_teste")
	off, on := false, true
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30,
		Detect: &off, DetectMethod: "kleinberg-p", DetectSensitivity: 4}
	r := newRecorder(cam, nil, idx, discardLogger())
	r.clips = func(detect.Clip) {}
	r.curInit = []byte("init")
	frame := func(ms int64, v detect.Verdict) {
		if v != (detect.Verdict{}) {
			r.trigger = &script{verdicts: []detect.Verdict{v}}
		}
		r.motion(ms, false, moof(videoID, 0, flagsPFrame, 6000), mdat(100))
	}

	frame(0, detect.Verdict{})
	if r.clipper != nil {
		t.Fatal("detection off and the clipper was created")
	}

	cam.Detect = &on
	r.requestDetect(cam)
	frame(40, detect.Verdict{})
	if r.clipper == nil {
		t.Fatal("detection on and the clipper wasn't created")
	}
	// An onset with no stored I-frame: when detection turns off, it ends up
	// with no video.
	frame(80, detect.Verdict{Onset: true, Candidate: true})

	cam.Detect = &off
	r.requestDetect(cam)
	frame(120, detect.Verdict{})
	if r.clipper != nil {
		t.Fatal("detection off and the clipper is still alive")
	}
	if n := r.noVideo.Load(); n != 1 {
		t.Fatalf("noVideo %d, want 1", n)
	}

	cam.Detect = &on
	r.requestDetect(cam)
	frame(160, detect.Verdict{})
	if n := r.noVideo.Load(); n != 1 {
		t.Errorf("noVideo %d after turning back on, want the 1 from before", n)
	}
}

// TestFunnelAddsUp: Queued is what remains of the onsets after the slices with
// an outcome, and it's never negative.
func TestFunnelAddsUp(t *testing.T) {
	r := &Recorder{clips: func(detect.Clip) {}}
	r.onsets.Add(7)
	r.noVideo.Add(1)
	r.dropped.Add(1)
	r.withObject.Add(2)
	if f := r.funnel(); f.Queued != 3 {
		t.Errorf("queued %d, want 3: 7 onsets, 4 with an outcome", f.Queued)
	}
	r.noObject.Add(5)
	if f := r.funnel(); f.Queued != 0 {
		t.Errorf("queued %d, want 0 with more outcomes than onsets", f.Queued)
	}
	if (&Recorder{}).funnel() != nil {
		t.Error("no object detector, the funnel had to be nil")
	}
}
