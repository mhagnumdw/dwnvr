package recorder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/area"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// TestQueueCapGrowsWithTheCameras: the cap the Diagnostics screen shows is the
// per-camera limit times the cameras with detection on - and the one turned off
// doesn't count, because it offers nothing.
func TestQueueCapGrowsWithTheCameras(t *testing.T) {
	yes, no := true, false
	m := &Manager{
		queue: detect.NewQueue(detect.Detectors["nenhum"](""), func(detect.Inference) {}, discardLogger()),
		recs:  map[string]*running{},
	}
	for id, d := range map[string]*bool{"a": &yes, "b": &yes, "c": &no, "d": nil} {
		m.recs[id] = &running{rec: &Recorder{cam: config.Camera{ID: id, Detect: d}}}
	}
	if got, want := m.Detector().Queue.Cap, 2*detect.ClipsPerCamera; got != want {
		t.Errorf("cap %d, want %d: two cameras with detection", got, want)
	}
}

// TestSetMidFlightDoesNotRace: changing what doesn't require reconnecting
// (name, quota, detection) writes the new camera into the live recorder, while
// whoever reads its camera - the session on connect, the clipper when created,
// Status, the silence alarm and the queue cap - reads at the same time, each in
// its own goroutine. Only `go test -race` catches the race.
func TestSetMidFlightDoesNotRace(t *testing.T) {
	var body []byte
	body = append(body, box("ftyp", []byte("isom"))...)
	body = append(body, moovWithAudio()...)
	for i := range 30 {
		flags := uint32(flagsPFrame)
		if i%10 == 0 {
			flags = flagsIFrame
		}
		body = append(body, moof(videoID, uint64(i)*6000, flags, 6000)...)
		body = append(body, mdat(100+i%7)...)
	}
	// A go2rtc that delivers a few frames and closes: each session ends
	// quickly, and the next one reads the camera again on connect.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	st := store.New(t.TempDir())
	m := NewManager(&config.Config{}, go2rtc.New(config.Go2RTC{URL: srv.URL}), st, discardLogger())
	m.queue = detect.NewQueue(detect.Detectors["nenhum"](""), m.onInference, discardLogger())

	on, off := true, false
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30,
		StallSeconds: 5, QuotaMB: 1000, Detect: &on, DetectMethod: "periodico",
		DetectSensitivity: 5}

	// The recorder is set up by hand, without run: the test is what calls the
	// session, one after another, without the reconnection backoff in between.
	rec := newRecorder(cam, m.client, st.Camera(cam.ID), discardLogger())
	rec.clips = rec.offerTo(m.queue)
	m.recs[cam.ID] = &running{rec: rec}
	m.cams[cam.ID] = cam

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ready := make(chan struct{})

	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-ready:
				return
			default:
			}
			_ = rec.session(ctx)
		}
	})
	wg.Go(func() {
		for {
			select {
			case <-ready:
				return
			default:
			}
			m.Status()
			m.Detector()
			rec.checkSilence(time.Now())
		}
	})

	// On the "API" side: name, quota and detection change; nothing that
	// reconnects. Detection toggling is what makes the clipper be created
	// again.
	for i := range 200 {
		c := cam
		c.Name = "Teste " + string(rune('A'+i%26))
		c.QuotaMB = int64(1000 + i)
		if i%2 == 1 {
			c.Detect = &off
		}
		m.Set(c)
		time.Sleep(200 * time.Microsecond)
	}
	close(ready)
	wg.Wait()

	if got := m.Status()[0].Name; got != "Teste "+string(rune('A'+199%26)) {
		t.Errorf("name in Status: %q, want the one from the last Set", got)
	}
}

// TestAreaDoesNotRestartRecording: the area and the alert belong to the filter
// and the notice, and drawing an area or turning the alert on can't open a gap
// in the recording.
func TestAreaDoesNotRestartRecording(t *testing.T) {
	cam := config.Camera{ID: "cam_teste", Audio: config.AudioNone, SegmentSeconds: 30, StallSeconds: 5}
	withArea := cam
	withArea.Areas = []config.Area{{ID: "k3f9a2", Name: "Gate",
		Points: []area.Point{{0.1, 0.1}, {0.5, 0.1}, {0.5, 0.5}}}}
	withArea.Alert = &config.CameraAlert{Enabled: true, Families: []detect.Family{detect.Person}}
	if recordingParamsChanged(cam, withArea) {
		t.Error("changing the areas or the alert would restart recording")
	}
}

// withAlert returns a Manager with an object detector and a camera with a
// person alert on the Gate, the left half of the frame, and its recorder in
// place, recording nothing.
func withAlert(t *testing.T) *Manager {
	t.Helper()
	st := store.New(t.TempDir())
	m := NewManager(&config.Config{}, nil, st, discardLogger())
	m.queue = detect.NewQueue(detect.Detectors["nenhum"](""), m.onInference, discardLogger())
	yes := true
	cam := config.Camera{ID: "cam_teste", Name: "Front", Enabled: true, Detect: &yes,
		Areas: []config.Area{{ID: "esq", Name: "Gate",
			Points: []area.Point{{0, 0}, {0.5, 0}, {0.5, 1}, {0, 1}}}},
		Alert: &config.CameraAlert{Enabled: true, Families: []detect.Family{detect.Person},
			Areas: []string{"esq"}}}
	// Without Start, Set doesn't start a recorder: it is set up by hand, as Start
	// would leave it.
	m.Set(cam)
	m.recs[cam.ID] = &running{rec: newRecorder(m.cfg.Resolve(cam), nil, st.Camera(cam.ID), discardLogger())}
	return m
}

// TestInferenceBecomesAlert: the inference goes through the Recorder, which
// marks `alert` and the area id on the line of the object that passed the rule,
// and the Dispatcher counts the alert. The person outside the area and the
// motorcycle, which isn't of the family, are recorded with no mark; an
// inference with only them doesn't alert.
func TestInferenceBecomesAlert(t *testing.T) {
	m := withAlert(t)
	now := time.Now().UnixMilli()
	m.onInference(detect.Inference{
		Clip: detect.Clip{Camera: "cam_teste", OnsetMs: now, FrameMs: now + 1400},
		Detections: []detect.Detection{
			{Class: "person", Score: 0.87654, Box: [4]float64{0.25, 0.2, 0.35, 0.6}},
			{Class: "person", Score: 0.55, Box: [4]float64{0.75, 0.2, 0.85, 0.6}},
			{Class: "motorcycle", Score: 0.71, Box: [4]float64{0.1, 0.4, 0.3, 0.7}},
		},
		Frame: []byte("\xff\xd8test-jpeg\xff\xd9"),
	})
	c := m.Alerts()
	if c == nil || c.Total() != 1 {
		t.Fatalf("dispatcher %v, want one alert", c)
	}
	lines, err := m.store.Camera("cam_teste").DayObjects(time.UnixMilli(now).Format(store.DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("%d lines, want the 3 marks", len(lines))
	}
	for i, l := range lines {
		// Only the first one, the 0.877 person on the Gate, alerted.
		if l.Alert != (i == 0) || l.Alert != (len(l.AlertAreas) == 1 && l.AlertAreas[0] == "esq") {
			t.Errorf("line %d (%s %.3f): alert %v in %v", i, l.Family, l.Score, l.Alert, l.AlertAreas)
		}
	}

	// New place for tracking, but outside the area: marks, and doesn't alert.
	m.onInference(detect.Inference{
		Clip:       detect.Clip{Camera: "cam_teste", OnsetMs: now + 60000, FrameMs: now + 61000},
		Detections: []detect.Detection{{Class: "person", Score: 0.9, Box: [4]float64{0.6, 0.6, 0.7, 0.95}}},
	})
	if c.Total() != 1 {
		t.Errorf("%d alerts, want 1: the second person was outside the area", c.Total())
	}
}

// TestNoAlertNothingIsCreated: with no camera that alerts, the Dispatcher
// doesn't exist. Neither the camera without the alert on alerts, nor the one
// without motion marking, nor the one in an install without an object detector.
func TestNoAlertNothingIsCreated(t *testing.T) {
	yes, no := true, false
	alertItem := &config.CameraAlert{Enabled: true, Families: []detect.Family{detect.Person}}
	cases := map[string]struct {
		cam      config.Camera
		detector bool
	}{
		"no alert":    {config.Camera{ID: "a", Enabled: true, Detect: &yes}, true},
		"alert off":   {config.Camera{ID: "a", Enabled: true, Detect: &yes, Alert: &config.CameraAlert{}}, true},
		"no marking":  {config.Camera{ID: "a", Enabled: true, Detect: &no, Alert: alertItem}, true},
		"no detector": {config.Camera{ID: "a", Enabled: true, Detect: &yes, Alert: alertItem}, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m := NewManager(&config.Config{}, nil, store.New(t.TempDir()), discardLogger())
			if c.detector {
				m.queue = detect.NewQueue(detect.Detectors["nenhum"](""), m.onInference, discardLogger())
			}
			m.Set(c.cam)
			if m.Alerts() != nil || len(m.rules) > 0 {
				t.Errorf("created: dispatcher %v, rules %v", m.Alerts(), m.rules)
			}
		})
	}
}

// TestTurningOffTheAlertKeepsTheHistory: the camera that turns the alert off
// loses its rule, and the Dispatcher stays, the same one, with what it already
// recorded.
func TestTurningOffTheAlertKeepsTheHistory(t *testing.T) {
	m := withAlert(t)
	now := time.Now().UnixMilli()
	m.onInference(detect.Inference{
		Clip:       detect.Clip{Camera: "cam_teste", OnsetMs: now, FrameMs: now + 1400},
		Detections: []detect.Detection{{Class: "person", Score: 0.87654, Box: [4]float64{0.25, 0.2, 0.35, 0.6}}},
		Frame:      []byte("\xff\xd8test-jpeg\xff\xd9"),
	})
	before := m.Alerts()
	if before == nil || before.Total() != 1 {
		t.Fatalf("dispatcher %v, want one alert before turning off", before)
	}

	cam := m.Cameras()[0]
	cam.Alert = &config.CameraAlert{Families: cam.Alert.Families, Areas: cam.Alert.Areas}
	m.Set(cam)
	if m.rules["cam_teste"] != nil {
		t.Errorf("rule %v: want no rule", m.rules["cam_teste"])
	}
	if after := m.Alerts(); after != before || before.Total() != 1 {
		t.Errorf("dispatcher %p with %d, want the same %p with 1", after, before.Total(), before)
	}
}
