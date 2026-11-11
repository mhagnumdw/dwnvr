package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/area"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/recorder"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// withGo2RTC swaps the testServer's Manager for one that really records against
// a fake go2rtc, and returns how many stream connections are open right now -
// each one is a running recorder.
func withGo2RTC(t *testing.T, s *Server) *atomic.Int32 {
	t.Helper()
	var open atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/streams":
			_, _ = w.Write([]byte(`{"cam_teste": {}, "cam_a": {}, "cam_b": {}}`))
		case "/api/stream.mp4":
			open.Add(1)
			defer open.Add(-1)
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	s.client = go2rtc.New(config.Go2RTC{URL: srv.URL})
	s.mgr = recorder.NewManager(s.cfg, s.client, s.store, s.log)
	ctx, cancel := context.WithCancel(context.Background())
	s.mgr.Start(ctx, []config.Camera{{ID: "cam_teste", Name: "Teste", Enabled: true}})
	// Runs before srv.Close: Stop waits for each recorder to let go of the
	// connection.
	t.Cleanup(func() { cancel(); s.mgr.Stop() })
	return &open
}

// saveCamera does the POST /api/cameras. `cam` is a config.Camera, or the
// camera JSON as the screen sends it.
func saveCamera(t *testing.T, s *Server, cam any) {
	t.Helper()
	b, _ := json.Marshal(cam)
	rec := httptest.NewRecorder()
	s.handleSaveCamera(rec, httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewReader(b)))
	if rec.Code != http.StatusOK {
		t.Errorf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
}

// waitOpen waits for the fake go2rtc to hold n connections for a moment in a
// row. Stopping a recorder releases the connection asynchronously, and starting
// another opens it the same way: a single read would catch the middle of the
// swap.
func waitOpen(t *testing.T, open *atomic.Int32, n int32) {
	t.Helper()
	end := time.Now().Add(3 * time.Second)
	since := time.Time{}
	for time.Now().Before(end) {
		if open.Load() != n {
			since = time.Time{}
		} else if since.IsZero() {
			since = time.Now()
		} else if time.Since(since) > 300*time.Millisecond {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%d connections open on go2rtc, want %d", open.Load(), n)
}

// Two saves of the same camera at the same time, both changing the audio -
// which requires reconnecting. Without the registry, each one stopped the old
// recorder and started a new one, and the first new one kept recording outside
// the map until the restart: two connections to the camera and only one on the
// screen.
func TestConcurrentSavesLeaveNoOrphanRecorder(t *testing.T) {
	s, _ := testServer(t)
	open := withGo2RTC(t, s)
	waitOpen(t, open, 1)

	audios := []string{config.AudioNone, config.AudioAAC, config.AudioFLAC}
	for i := range 10 {
		// Both differ from the current audio and from each other: both
		// reconnect, whatever the order in which they come in.
		var wg sync.WaitGroup
		for _, a := range []string{audios[(i+1)%3], audios[(i+2)%3]} {
			wg.Go(func() {
				saveCamera(t, s, config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, Audio: a})
			})
		}
		wg.Wait()
		waitOpen(t, open, 1)
	}
}

// Two saves of different cameras at the same time read the same list, and
// whoever wrote cameras.json last erased the other's change.
func TestConcurrentSavesLoseNoChangeInTheFile(t *testing.T) {
	s, _ := testServer(t)
	withGo2RTC(t, s)

	for i := range 50 {
		var wg sync.WaitGroup
		for _, id := range []string{"cam_a", "cam_b"} {
			wg.Go(func() {
				saveCamera(t, s, config.Camera{ID: id, Name: fmt.Sprint(id, " ", i)})
			})
		}
		wg.Wait()

		cams, _, err := s.cfg.LoadCameras()
		if err != nil {
			t.Fatal(err)
		}
		nomes := map[string]string{}
		for _, c := range cams {
			nomes[c.ID] = c.Name
		}
		for _, id := range []string{"cam_a", "cam_b"} {
			if want := fmt.Sprint(id, " ", i); nomes[id] != want {
				t.Fatalf("round %d: %s in the file with name %q, want %q", i, id, nomes[id], want)
			}
		}
	}
}

// Camera removed without deleting the recordings, dwnvr restarted and the
// camera registered again with the same ID. The index was only read at boot,
// and only for the registered ones: the re-registration started empty, and the
// old recordings disappeared from Recordings, from the orphans and from the
// quota until the next restart.
func TestReRegistrationReadsRecordingsAlreadyOnDisk(t *testing.T) {
	s, _ := testServer(t)
	withGo2RTC(t, s)

	// What was left from the previous life: another Store over the same
	// storage, like the process that ran before the restart.
	antes := store.New(s.store.Root()).Camera("cam_a")
	start := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	seed(t, antes, start, [2]int64{0, 30000})
	if err := antes.EnsureDirs(start.Format(store.DayLayout)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(antes.SegmentPath(start.UnixMilli()), make([]byte, 1000), 0o644); err != nil {
		t.Fatal(err)
	}

	saveCamera(t, s, config.Camera{ID: "cam_a", Name: "A", Enabled: true})

	if got := s.store.Camera("cam_a").TotalBytes(); got != 1000 {
		t.Errorf("cam_a index with %d bytes after the re-registration, want 1000", got)
	}
}

// withRealDefaults is the testServer with the defaults of someone who didn't
// write dwnvr.yaml, all filled in, and go2rtc down: the GET lists the cameras
// without the streams, and the POST saves without checking the stream.
func withRealDefaults(t *testing.T) *Server {
	t.Helper()
	s, _ := testServer(t)
	s.cfg.Defaults = defaultConfig(t).Defaults
	s.client = go2rtc.New(config.Go2RTC{URL: "http://127.0.0.1:1"})
	return s
}

// getResponse is the admin's GET /api/cameras, with each camera and the
// `padrao` in generic JSON: the screen edits this object and sends it back
// whole, and the test does the same, without going through config.Camera.
func getResponse(t *testing.T, s *Server) (cams map[string]map[string]any, defaults map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.requireAuth(s.handleCameras)(rec, httptest.NewRequest(http.MethodGet, "/api/cameras", nil))
	var got struct {
		Cameras []map[string]any `json:"cameras"`
		Default map[string]any   `json:"padrao"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response %s (%v)", rec.Body.String(), err)
	}
	cams = map[string]map[string]any{}
	for _, c := range got.Cameras {
		cams[fmt.Sprint(c["id"])] = c
	}
	return cams, got.Default
}

// inFile is each camera of cameras.json, read from disk: it is the file that
// decides whether the dwnvr.yaml default still reaches the camera.
func inFile(t *testing.T, s *Server) map[string]map[string]any {
	t.Helper()
	b, err := os.ReadFile(s.cfg.CamerasPath())
	if err != nil {
		t.Fatal(err)
	}
	var list []map[string]any
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatalf("cameras.json %s (%v)", b, err)
	}
	cams := map[string]map[string]any{}
	for _, c := range list {
		cams[fmt.Sprint(c["id"])] = c
	}
	return cams
}

func keys(m map[string]any) []string {
	return slices.Sorted(maps.Keys(m))
}

// The screen edits the camera as the GET shows it, with the defaults already
// applied, and sends it back whole. Saving without changing anything wrote each
// default to cameras.json, and changing the defaults in dwnvr.yaml no longer
// reached the camera. A new camera, which the screen builds from `padrao`, was
// written the same way.
func TestSaveWithoutChangesDoesNotFreezeTheDefaults(t *testing.T) {
	s := withRealDefaults(t)
	cams, defaults := getResponse(t, s)

	saveCamera(t, s, cams["cam_teste"])
	defaults["id"], defaults["name"], defaults["enabled"] = "cam_a", "A", true
	saveCamera(t, s, defaults)

	file := inFile(t, s)
	for _, id := range []string{"cam_teste", "cam_a"} {
		if got, want := keys(file[id]), []string{"enabled", "id", "name"}; !slices.Equal(got, want) {
			t.Errorf("%s in cameras.json with %v, want only %v", id, got, want)
		}
	}
}

// What the camera already defined stays in the file, even if equal to the
// default; what the person changed goes in; the rest follows the default.
func TestSaveKeepsWhatTheCameraSets(t *testing.T) {
	s := withRealDefaults(t)
	d := s.cfg.Defaults
	s.mgr.Set(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true,
		QuotaMB: d.QuotaMB, Audio: config.AudioAAC})

	cams, _ := getResponse(t, s)
	cam := cams["cam_teste"]
	cam["segmentSeconds"], cam["detect"] = 60, !d.Detect
	saveCamera(t, s, cam)

	got := inFile(t, s)["cam_teste"]
	want := map[string]any{
		"id": "cam_teste", "name": "Teste", "enabled": true,
		"quotaMB": float64(d.QuotaMB), "audio": config.AudioAAC,
		"segmentSeconds": float64(60), "detect": !d.Detect,
	}
	if !maps.Equal(got, want) {
		t.Errorf("cam_teste no cameras.json\n %v\nesperava\n %v", got, want)
	}
}

// The area arrives from the screen without an id: the server gives it one, and
// that is what goes to cameras.json. An invalid area rejects the whole save,
// with the reason.
func TestSaveDaCameraComAreas(t *testing.T) {
	s, _ := testServer(t)
	withGo2RTC(t, s)

	points := []area.Point{{0.1, 0.1}, {0.5, 0.1}, {0.5, 0.5}}
	saveCamera(t, s, config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true,
		Areas: []config.Area{{Name: "Portão", Points: points}}})
	cams, _, err := s.cfg.LoadCameras()
	if err != nil {
		t.Fatal(err)
	}
	if len(cams) != 1 || len(cams[0].Areas) != 1 || cams[0].Areas[0].ID == "" {
		t.Fatalf("in the file: %+v", cams)
	}

	b, _ := json.Marshal(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true,
		Areas: []config.Area{{Name: "Portão", Points: points[:2]}}})
	rec := httptest.NewRecorder()
	s.handleSaveCamera(rec, httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewReader(b)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "pontos") {
		t.Errorf("HTTP %d: %s, want 400 mentioning the points (pontos)", rec.Code, rec.Body.String())
	}
}

// The alert goes to cameras.json as it arrived, with the schedules. One that
// points to an area the camera doesn't have rejects the whole save, with the
// reason: the screen removes the area from the alert before deleting it. So
// does a schedule that crosses midnight.
func TestSaveCameraWithAlert(t *testing.T) {
	s, _ := testServer(t)
	withGo2RTC(t, s)

	gate := config.Area{ID: "k3f9a2", Name: "Portão", Points: []area.Point{{0.1, 0.1}, {0.5, 0.1}, {0.5, 0.5}}}
	alert := &config.CameraAlert{Enabled: true, Families: []detect.Family{detect.Person}, Areas: []string{"k3f9a2"},
		Schedules: []config.AlertSchedule{{Days: []time.Weekday{time.Saturday, time.Sunday}, From: "00:00", To: "23:59"}}}
	saveCamera(t, s, config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true,
		Areas: []config.Area{gate}, Alert: alert})
	cams, _, err := s.cfg.LoadCameras()
	if err != nil {
		t.Fatal(err)
	}
	if len(cams) != 1 || !reflect.DeepEqual(cams[0].Alert, alert) {
		t.Fatalf("in the file: %+v", cams)
	}

	b, _ := json.Marshal(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, Alert: alert})
	rec := httptest.NewRecorder()
	s.handleSaveCamera(rec, httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewReader(b)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "alert.areas[0]") {
		t.Errorf("HTTP %d: %s, want 400 mentioning alert.areas[0]", rec.Code, rec.Body.String())
	}

	night := &config.CameraAlert{Enabled: true, Families: []detect.Family{detect.Person},
		Schedules: []config.AlertSchedule{{Days: []time.Weekday{time.Monday}, From: "22:00", To: "06:00"}}}
	b, _ = json.Marshal(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, Alert: night})
	rec = httptest.NewRecorder()
	s.handleSaveCamera(rec, httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewReader(b)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "alert.schedules[0].to") {
		t.Errorf("HTTP %d: %s, want 400 mentioning alert.schedules[0].to", rec.Code, rec.Body.String())
	}
}
