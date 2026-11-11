package detect

import (
	"math"
	"strconv"
	"testing"
)

// --- the score ----------------------------------------------------------------

// stillScene returns the size of the i-th P frame of a scene without motion:
// ~1000 bytes with a deterministic ±10% noise, which is what compression does
// with the grain of a still camera.
func stillScene(i int) int { return 900 + (i*7919)%200 }

// TestIFrameDoesNotMoveTheSalience is the property that makes `kleinberg-p`
// work: the I frame comes back every GOP, with or without motion, and is
// dozens of times larger than a P. If it reached the function, every GOP would
// look like an event.
func TestIFrameDoesNotMoveTheSalience(t *testing.T) {
	raw := ScoreFuncs["kleinberg"]()
	pOnly := ScoreFuncs["kleinberg-p"]()

	var before float64
	for i := range 400 {
		raw.Push(stillScene(i), false, 67)
		before = pOnly.Push(stillScene(i), false, 67)
	}

	const iFrame = 50_000
	if d := raw.Push(iFrame, true, 67); d < KleinbergPThreshold[DefaultLevel] {
		t.Fatalf("without hiding the I frame it should stand out: %g", d)
	}
	if d := pOnly.Push(iFrame, true, 67); d != before {
		t.Errorf("the I frame moved the salience: %g, before %g", d, before)
	}
}

// TestLongEventDoesNotBecomeTheNewNormal checks FreezeAbove: a motion longer
// than the mean's half life must not be learned as the camera's normal.
// Without freezing, the baseline chases the event and the salience drops in
// the middle of it.
func TestLongEventDoesNotBecomeTheNewNormal(t *testing.T) {
	s := ScoreFuncs["kleinberg"]()
	i := 0
	for ; i < 400; i++ {
		s.Push(stillScene(i), false, 67)
	}
	// Three half lives of continuous motion, at 3x the normal size.
	var d float64
	for range 3 * int(KleinbergHalfLife) {
		d = s.Push(3*stillScene(i), false, 67)
		i++
	}
	if d < KleinbergPThreshold[DefaultLevel] {
		t.Errorf("at the end of the event the salience dropped to %g: the baseline absorbed the event", d)
	}
}

// TestBurstBecomesASingleOnset checks the score and the trigger together, on
// the default method: a still scene doesn't fire, and a motion of a second and
// a half - P frames grow when the image changes - fires once, not once per
// frame.
func TestBurstBecomesASingleOnset(t *testing.T) {
	m := NewMethod(DefaultMethod, DefaultLevel)
	now := int64(1_000_000)
	onsets := 0
	push := func(bytes int) {
		now += 67
		if m.Frame(now, bytes, false).Onset {
			onsets++
		}
	}

	i := 0
	for ; i < 600; i++ {
		push(stillScene(i))
	}
	if onsets != 0 {
		t.Fatalf("a still scene fired %d times", onsets)
	}

	for range 22 {
		push(5 * stillScene(i))
		i++
	}
	for range 100 {
		push(stillScene(i))
		i++
	}
	if onsets != 1 {
		t.Errorf("the burst gave %d onsets, want 1", onsets)
	}
}

// TestPeakPicksTheLargestPFrame checks the frame to infer: the one with the
// highest salience in the PeakWindowMs after the onset, only among P frames,
// and only one that stands out STRICTLY more takes the place.
func TestPeakPicksTheLargestPFrame(t *testing.T) {
	type q struct {
		salience float64
		keyframe bool
	}
	tape := []q{
		{0, false}, {0, false},
		{20, false}, // onset
		{25, false},
		{40, true},  // the largest, but it's an I frame: stays out
		{30, false}, // the peak
		{30, false}, // ties: doesn't take the place
		{12, false},
	}
	values := make([]float64, 0, len(tape))
	for _, f := range tape {
		values = append(values, f.salience)
	}
	m := newStatistical(&fakeScore{values: values}, 10)

	now := int64(1_000_000)
	var candidate int64
	peak := now + 6*100
	for i := 0; ; i++ {
		now += 100
		k := false
		if i < len(tape) {
			k = tape[i].keyframe
		}
		v := m.Frame(now, 1000, k)
		if v.Candidate {
			candidate = now
		}
		if v.Cut {
			break
		}
		if now > 1_000_000+2*PeakWindowMs {
			t.Fatal("the peak window didn't close")
		}
	}
	if candidate != peak {
		t.Errorf("inferred the frame at %d ms, the peak is at %d ms",
			candidate-1_000_000, peak-1_000_000)
	}
}

// --- the hysteresis trigger -------------------------------------------------

// fakeScore returns the saliences from a list, in order. It's how the trigger
// is tested without depending on any statistics.
type fakeScore struct {
	values []float64
	i      int
	warmup int
}

func (s *fakeScore) Name() string { return "falso" }
func (s *fakeScore) Warmup() int  { return s.warmup }
func (s *fakeScore) Reset()       { s.i = 0 }
func (s *fakeScore) Push(int, bool, float64) float64 {
	v := s.values[min(s.i, len(s.values)-1)]
	s.i++
	return v
}

// pilot pushes frames into a method with a clock that MOVES between calls.
// Having a single clock is the point: rewinding it between two stretches of
// the same test would be a jump backwards, which the method treats - rightly -
// as a recording gap.
type pilot struct {
	m      Method
	now    int64
	stepMs int64
}

func newPilot(m Method, stepMs int64) *pilot {
	return &pilot{m: m, now: 1_000_000, stepMs: stepMs}
}

// run pushes `count` consecutive frames and returns the instants when the
// trigger opened.
func (p *pilot) run(count int) []int64 {
	var out []int64
	for range count {
		p.now += p.stepMs
		if p.m.Frame(p.now, 1000, false).Onset {
			out = append(out, p.now)
		}
	}
	return out
}

// skip opens a recording gap before the next frame.
func (p *pilot) skip(ms int64) { p.now += ms }

func TestHysteresisDoesNotSplitOneEvent(t *testing.T) {
	// An event that hovers around the high threshold: goes up, drops to the
	// middle of the band, goes up again. Without hysteresis this would become
	// three markers.
	high := 10.0
	values := []float64{0, 0, 0, 20, 5, 5, 20, 5, 20, 0, 0, 0}
	onsets := newPilot(newStatistical(&fakeScore{values: values}, high), 100).
		run(len(values))
	if len(onsets) != 1 {
		t.Fatalf("want 1 onset, got %d: %v", len(onsets), onsets)
	}
}

func TestHysteresisClosesOnlyAfterSecondsToClose(t *testing.T) {
	high := 10.0
	// Goes up, then stays below the low threshold. With 100 ms frames, the
	// 3 s of SecondsToClose are 30 frames - only from the 31st below does the
	// interval close and a new peak can open another.
	values := make([]float64, 0, 80)
	values = append(values, 20)
	for range 25 {
		values = append(values, 0)
	}
	values = append(values, 20) // too early: the interval is still open
	for range 40 {
		values = append(values, 0)
	}
	values = append(values, 20) // now yes

	onsets := newPilot(newStatistical(&fakeScore{values: values}, high), 100).
		run(len(values))
	if len(onsets) != 2 {
		t.Fatalf("want 2 onsets (the middle one suppressed by the hysteresis), got %d: %v",
			len(onsets), onsets)
	}
}

func TestWarmupDoesNotTrigger(t *testing.T) {
	// Very high salience from the first frame: nothing may come out while the
	// function has no baseline.
	values := []float64{999}
	p := newPilot(newStatistical(&fakeScore{values: values, warmup: 300}, 10), 100)

	if onsets := p.run(300); len(onsets) != 0 {
		t.Fatalf("fired during warmup: %v", onsets)
	}
	if onsets := p.run(1); len(onsets) != 1 {
		t.Fatal("didn't fire on the first frame after warmup")
	}
}

func TestRecordingGapResetsEverything(t *testing.T) {
	m := newStatistical(&fakeScore{values: []float64{999}, warmup: 300}, 10)
	p := newPilot(m, 100)

	p.run(301) // warms up and opens the interval
	if !m.open {
		t.Fatal("the interval should be open")
	}

	// A jump larger than GapMs is a recording gap: warmup starts over and the
	// interval is forced closed.
	p.skip(GapMs + 5_000)
	if onsets := p.run(1); len(onsets) != 0 {
		t.Error("fired on the first frame after the gap, still without a baseline")
	}
	if m.open {
		t.Error("the gap should have closed the interval")
	}
	if m.frames != 1 {
		t.Errorf("warmup didn't start over: %d frames", m.frames)
	}
}

// --- the periodic method ----------------------------------------------------

// TestPeriodicCostsExactlyWhatItPromises is the reason this method exists:
// being the exact reference to beat. If its cost slips, it stops being a
// reference.
func TestPeriodicCostsExactlyWhatItPromises(t *testing.T) {
	for level := MinLevel; level <= MaxLevel; level++ {
		// One hour of camera at 15 frames per second, with 67 ms frames - not
		// a divisor of the period, on purpose: it's the accumulated remainder
		// that "subtract instead of zeroing" exists not to lose.
		const stepMs, count = 67, 3_600_000 / 67
		onsets := newPilot(NewMethod("periodico", level), stepMs).run(count)

		expected := OnsetsPerHour[level]
		if diff := math.Abs(float64(len(onsets)) - expected); diff > 1 {
			t.Errorf("level %d: %d onsets in the hour, expected %.0f",
				level, len(onsets), expected)
		}
	}
}

func TestPeriodicWaitsAFullPeriodAfterAGap(t *testing.T) {
	p := newPilot(NewMethod("periodico", 5), 67) // 100/hour = one every 36 s
	p.run(500)                                   // 33.5 s: almost there

	// The gap must throw away what it already waited.
	p.skip(GapMs + 1)
	if onsets := p.run(1); len(onsets) != 0 {
		t.Error("fired right at the restart")
	}
	if onsets := p.run(400); len(onsets) != 0 {
		t.Errorf("fired before a full period: %v", onsets)
	}
}

func TestPeriodicInfersAtTheOnsetItself(t *testing.T) {
	m := NewMethod("periodico", 5) // one every 36 s
	now := int64(1_000_000)
	var onsetMs int64
	for range 2 * 36_000 / 67 {
		now += 67
		v := m.Frame(now, 1000, false)
		if v.Onset {
			if !v.Candidate {
				t.Fatal("the periodic onset must be the frame to infer")
			}
			onsetMs = now
			continue
		}
		if v.Candidate {
			t.Fatal("the periodic method has no peak: no frame after the onset is a candidate")
		}
		if onsetMs != 0 {
			if !v.Cut {
				t.Fatal("the periodic window must close on the frame after the onset")
			}
			return
		}
	}
	t.Fatal("the periodic method didn't fire")
}

func TestGapCutsTheOpenWindow(t *testing.T) {
	values := []float64{999}
	m := newStatistical(&fakeScore{values: values, warmup: 10}, 10)
	p := newPilot(m, 100)
	p.run(11) // warms up and opens
	if !m.peak.open {
		t.Fatal("the peak window should be open")
	}
	p.skip(GapMs + 1)
	p.now += p.stepMs
	if v := m.Frame(p.now, 1000, false); !v.Cut {
		t.Error("the gap must cut the clip that was waiting for the window")
	}
}

// --- the clipper ------------------------------------------------------------

// tape builds synthetic frames for the clipper: the moof says the instant, the
// mdat says whether it's I or P. That way the cut clip reads back as text.
type tape struct {
	r       *Clipper
	now     int64
	emitted []Clip
}

func newTape() *tape {
	f := &tape{r: NewClipper("cam"), now: 1000}
	f.r.Init([]byte("[init]"))
	return f
}

func (f *tape) frame(v Verdict, keyframe bool) {
	f.now += 100
	kind := "P"
	if keyframe {
		kind = "I"
	}
	moof := []byte("<" + strconv.FormatInt(f.now, 10))
	mdat := []byte(kind + ">")
	if p, ok := f.r.Frame(v, f.now, keyframe, moof, mdat); ok {
		f.emitted = append(f.emitted, p)
	}
}

var (
	nothing   = Verdict{}
	onset     = Verdict{Onset: true, Candidate: true}
	candidate = Verdict{Candidate: true}
	cut       = Verdict{Cut: true}
)

func TestClipperCutsFromIFrameToCandidate(t *testing.T) {
	f := newTape()
	f.frame(nothing, true)    // 1100 I
	f.frame(nothing, false)   // 1200
	f.frame(onset, false)     // 1300
	f.frame(nothing, false)   // 1400
	f.frame(candidate, false) // 1500: the peak
	f.frame(nothing, false)   // 1600
	f.frame(cut, false)       // 1700: outside the window

	if len(f.emitted) != 1 {
		t.Fatalf("%d clips, expected 1", len(f.emitted))
	}
	p := f.emitted[0]
	want := "[init]<1100I><1200P><1300P><1400P><1500P>"
	if got := string(p.Fmp4); got != want {
		t.Errorf("clip\n got %s\nwant %s", got, want)
	}
	if p.OnsetMs != 1300 || p.FrameMs != 1500 || p.Camera != "cam" {
		t.Errorf("onset %d, frame %d, camera %q", p.OnsetMs, p.FrameMs, p.Camera)
	}
}

// TestClipperKeepsCandidateGOPWhenIFrameArrives is the reason there are two
// buffers: the peak stayed in the old GOP, and the I frame that came after
// must not wipe it.
func TestClipperKeepsCandidateGOPWhenIFrameArrives(t *testing.T) {
	f := newTape()
	f.frame(nothing, true)    // 1100 I
	f.frame(onset, false)     // 1200
	f.frame(candidate, false) // 1300: the peak
	f.frame(nothing, true)    // 1400 I: new GOP, inside the window
	f.frame(nothing, false)   // 1500
	f.frame(nothing, true)    // 1600 I: and another
	f.frame(cut, false)       // 1700

	want := "[init]<1100I><1200P><1300P>"
	if len(f.emitted) != 1 || string(f.emitted[0].Fmp4) != want {
		t.Fatalf("clips %q, expected [%s]", clips(f.emitted), want)
	}
}

func TestClipperFollowsCandidateIntoNewGOP(t *testing.T) {
	f := newTape()
	f.frame(nothing, true)    // 1100 I
	f.frame(onset, false)     // 1200
	f.frame(nothing, true)    // 1300 I
	f.frame(nothing, false)   // 1400
	f.frame(candidate, false) // 1500: the peak, already in the new GOP
	f.frame(cut, true)        // 1600 I, and outside the window

	want := "[init]<1300I><1400P><1500P>"
	if len(f.emitted) != 1 || string(f.emitted[0].Fmp4) != want {
		t.Fatalf("clips %q, expected [%s]", clips(f.emitted), want)
	}
	// And the next GOP starts clean.
	f.frame(onset, false) // 1700
	f.frame(cut, false)
	if want := "[init]<1600I><1700P>"; len(f.emitted) != 2 || string(f.emitted[1].Fmp4) != want {
		t.Fatalf("clips %q, the second should be %s", clips(f.emitted), want)
	}
}

func TestClipperDoesNotCutWithoutIFrame(t *testing.T) {
	f := newTape()
	f.frame(nothing, false) // the connection started in the middle of a GOP
	f.frame(onset, false)   // without its I frame, nothing decodes
	f.frame(cut, false)
	if len(f.emitted) != 0 {
		t.Fatalf("cut a clip without an I frame: %q", clips(f.emitted))
	}
}

// TestClipperCountsOnsetWithoutVideo: an onset that ends without a clip is
// counted where it ends, and one still waiting for its peak is NOT - that's
// what the Diagnostics screen used to show as lost during the seconds of the
// window.
func TestClipperCountsOnsetWithoutVideo(t *testing.T) {
	f := newTape()
	f.frame(nothing, false) // the connection started in the middle of a GOP
	f.frame(onset, false)   // window open: not lost yet
	if n := f.r.NoVideo(); n != 0 {
		t.Fatalf("counted %d with the window open", n)
	}
	f.frame(cut, false) // closed without an I frame
	if n := f.r.NoVideo(); n != 1 {
		t.Fatalf("without an I frame: %d, expected 1", n)
	}

	f.frame(nothing, true)
	f.frame(onset, false)
	f.frame(cut, false) // this one comes out
	if n := f.r.NoVideo(); n != 1 || len(f.emitted) != 1 {
		t.Fatalf("cut %d and counted %d without video, expected 1 and 1", len(f.emitted), n)
	}

	f.frame(onset, false)
	f.r.Forget() // detection switched in the middle of the window
	if n := f.r.NoVideo(); n != 2 {
		t.Fatalf("forgotten: %d, expected 2", n)
	}

	f.frame(nothing, true)
	f.frame(onset, false)
	f.frame(onset, false) // the method was switched and the pending one didn't close
	if n := f.r.NoVideo(); n != 3 {
		t.Fatalf("replaced: %d, expected 3", n)
	}
}

func TestClipperCloseReturnsThePending(t *testing.T) {
	f := newTape()
	f.frame(nothing, true)
	f.frame(onset, false)
	p, ok := f.r.Close()
	if !ok || string(p.Fmp4) != "[init]<1100I><1200P>" {
		t.Fatalf("Close: ok=%v %q", ok, p.Fmp4)
	}
	if _, ok := f.r.Close(); ok {
		t.Error("the same clip came out twice")
	}
	// Until the next Init, nothing is stored.
	f.frame(nothing, true)
	f.frame(onset, false)
	if _, ok := f.r.Close(); ok {
		t.Error("stored a frame from a connection that had already ended")
	}
}

func TestClipperRespectsTheCap(t *testing.T) {
	f := newTape()
	f.frame(nothing, true)
	f.frame(onset, false)
	// A frame that busts the cap isn't stored, and doesn't become a candidate.
	large := make([]byte, MaxGOPBytes)
	if _, ok := f.r.Frame(candidate, 5000, false, nil, large); ok {
		t.Fatal("cut too early")
	}
	f.frame(cut, false)
	if len(f.emitted) != 1 || string(f.emitted[0].Fmp4) != "[init]<1100I><1200P>" {
		t.Fatalf("clips %q: the candidate that fit should still stand", clips(f.emitted))
	}
	if n := cap(f.r.current) + cap(f.r.spare); n > 2*MaxGOPBytes {
		t.Errorf("the buffers grew to %d bytes", n)
	}
}

func clips(ps []Clip) []string {
	var out []string
	for _, p := range ps {
		out = append(out, string(p.Fmp4))
	}
	return out
}

func TestClipperDoesNotAllocateWhenStoring(t *testing.T) {
	r := NewClipper("cam")
	r.Init(make([]byte, 700))
	moof, mdat := make([]byte, 100), make([]byte, 900)
	now := int64(0)
	frame := func(i int) {
		now += 67
		r.Frame(nothing, now, i%60 == 0, moof, mdat)
	}
	// Warms up outside the measurement: the buffers grow to the GOP.
	for i := range 240 {
		frame(i)
	}
	i := 240
	n := testing.AllocsPerRun(2000, func() {
		frame(i)
		i++
	})
	if n != 0 {
		t.Errorf("%v allocations per stored frame, expected 0", n)
	}
}

// --- the contracts ----------------------------------------------------------

func TestNewMethodFallsBackToDefault(t *testing.T) {
	cases := []struct {
		method string
		level  int
	}{
		{"nao-existe", 4},
		{"", 4},
		{"kleinberg-p", 0},
		{"kleinberg-p", 9},
	}
	for _, c := range cases {
		m := NewMethod(c.method, c.level)
		if m == nil {
			t.Fatalf("%q level %d: no method", c.method, c.level)
		}
	}
	// The default must be the actual default, not just any method.
	if got := NewMethod("nao-existe", 4).Name(); got != DefaultMethod {
		t.Errorf("default name: %q, expected %q", got, DefaultMethod)
	}
	// A level out of range falls back to DefaultLevel, and the threshold must
	// be that level's.
	e := NewMethod("kleinberg-p", 99).(*statistical)
	if e.high != KleinbergPThreshold[DefaultLevel] {
		t.Errorf("threshold %g, expected the default level's %g", e.high, KleinbergPThreshold[DefaultLevel])
	}
}

func TestLowThresholdGoesDownEvenWhenNegative(t *testing.T) {
	if got := LowThreshold(10); math.Abs(got-4) > 1e-9 {
		t.Errorf("LowThreshold(10) = %g, expected 4", got)
	}
	if got := LowThreshold(-10); got >= -10 {
		t.Errorf("LowThreshold(-10) = %g: with a negative threshold closing means going DOWN", got)
	}
}

// TestNoAllocationOnHotPath guards a requirement that is not a detail: this
// runs for every camera, at 15 frames per second, on an Orange Pi Zero 3. With
// ten cameras, one allocation per frame would be 150 per second of garbage for
// the GC to collect.
func TestNoAllocationOnHotPath(t *testing.T) {
	for name := range Methods {
		m := NewMethod(name, DefaultLevel)
		var t0 int64 = 1_000_000
		// Warms up outside the measurement.
		for range 400 {
			t0 += 67
			m.Frame(t0, 900, false)
		}
		n := testing.AllocsPerRun(2000, func() {
			t0 += 67
			m.Frame(t0, 900, false)
		})
		if n != 0 {
			t.Errorf("%s: %v allocations per frame, expected 0", name, n)
		}
	}
}

// TestScoreDoesNotAllocateOnHotPath is the same requirement as the test above,
// but on the FUNCTIONS, not on the methods: a new function goes into
// ScoreFuncs before going into Methods, and without this it would get in
// without anyone measuring its price per frame.
func TestScoreDoesNotAllocateOnHotPath(t *testing.T) {
	for name, ctor := range ScoreFuncs {
		s := ctor()
		i := 0
		for ; i < 400; i++ {
			s.Push(stillScene(i), false, 67)
		}
		n := testing.AllocsPerRun(2000, func() {
			i++
			s.Push(stillScene(i), false, 67)
		})
		if n != 0 {
			t.Errorf("%s: %v allocations per frame, expected 0", name, n)
		}
	}
}

func TestNoneDetectorReturnsAnEmptyResult(t *testing.T) {
	d := Detectors["nenhum"]("")
	out, err := d.Infer(t.Context(), Clip{Fmp4: []byte("gop")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Detections) != 0 {
		t.Errorf("found %d objects without inferring", len(out.Detections))
	}
}
