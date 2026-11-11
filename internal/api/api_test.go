package api

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/recorder"
	"github.com/mhagnumdw/dwnvr/internal/store"
	"github.com/mhagnumdw/dwnvr/internal/users"
)

func testServer(t *testing.T) (*Server, *store.Camera) {
	t.Helper()
	st := store.New(t.TempDir())

	// Config loaded from a nonexistent dwnvr.yaml in a temporary directory. The
	// path matters: CamerasPath comes from it, and a zeroed Config would make
	// SaveCameras dump a cameras.json into the package directory.
	cfg, _, err := config.Load(filepath.Join(t.TempDir(), "dwnvr.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Storage.Root = st.Root()
	cfg.Defaults = config.Defaults{SegmentSeconds: 30, QuotaMB: 100, Audio: config.AudioNone}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Manager without Start: it registers the camera without starting a real
	// recording, which is what these tests need.
	mgr := recorder.NewManager(cfg, nil, st, log)
	mgr.Set(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true})

	cad, _, err := users.Open(cfg.UsersPath(), cfg.Server.Username)
	if err != nil {
		t.Fatal(err)
	}
	accesses, err := users.OpenAccessLog(cfg.AccessPath(), log)
	if err != nil {
		t.Fatal(err)
	}

	s := &Server{
		cfg:           cfg,
		store:         st,
		mgr:           mgr,
		users:         cad,
		accesses:      accesses,
		sessionSecret: []byte("segredo-de-teste-com-32-bytes!!!"),
		log:           log,
	}
	return s, st.Camera("cam_teste")
}

func seed(t *testing.T, cam *store.Camera, base time.Time, offsets ...[2]int64) {
	t.Helper()
	for _, o := range offsets {
		e := store.Entry{
			StartMs: base.UnixMilli() + o[0], DurMs: o[1],
			Size: 1000, Gen: "aabbcc", InitSize: 737, FirstFrag: 500,
		}
		if err := cam.Append(e); err != nil {
			t.Fatal(err)
		}
	}
}

func getTimeline(t *testing.T, s *Server, query string) timelineResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleTimeline(rec, httptest.NewRequest(http.MethodGet, "/api/rec/timeline?"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var out timelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unreadable response: %v", err)
	}
	return out
}

func getEvents(t *testing.T, s *Server, query string) eventsResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleEvents(rec, httptest.NewRequest(http.MethodGet, "/api/rec/events?"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var out eventsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unreadable response: %v", err)
	}
	return out
}

func TestEventsReturnsTheDayMarks(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	for _, off := range []int64{0, 60_000, 120_000} {
		if err := cam.AppendEvent(store.Event{AtMs: base.UnixMilli() + off}); err != nil {
			t.Fatal(err)
		}
	}

	got := getEvents(t, s, "cam=cam_teste&day="+base.Format(store.DayLayout))
	if len(got.Onsets) != 3 {
		t.Fatalf("expected 3 marks, got %d: %v", len(got.Onsets), got.Onsets)
	}
	if got.Onsets[0] != base.UnixMilli() {
		t.Errorf("first mark = %d, expected %d", got.Onsets[0], base.UnixMilli())
	}
}

// The object mark lives in the same file as the onset, and must not show up in
// the heat strip as a second onset.
func TestEventsSeparatesObjectFromMotion(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local).UnixMilli()
	for _, ev := range []store.Event{
		{AtMs: base},
		{AtMs: base, Family: "pessoa", Class: "person", Score: 0.87},
		{AtMs: base + 60_000},
	} {
		if err := cam.AppendEvent(ev); err != nil {
			t.Fatal(err)
		}
	}
	got := getEvents(t, s, "cam=cam_teste&day=2026-08-08")
	if len(got.Onsets) != 2 {
		t.Errorf("onsets %v: the object mark got into the heat strip", got.Onsets)
	}
	want := markedObject{AtMs: base, Family: "pessoa", Class: "person", Score: 0.87}
	if len(got.Objects) != 1 || got.Objects[0] != want {
		t.Errorf("objects %+v, expected [%+v]", got.Objects, want)
	}
}

// A mark with a box carries the box and the instant of the inspected frame to
// the screen; a mark without them goes out without the two keys, and not with
// zeros - a [0,0,0,0] box would be drawn as a dot in the corner of the video.
func TestEventsCarriesTheBoxOnlyForMarksThatHaveOne(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local).UnixMilli()
	box := [4]float64{0.5156, 0.2611, 0.6734, 0.9778}
	for _, ev := range []store.Event{
		{AtMs: base, Family: "pessoa", Class: "person", Score: 0.87},
		{AtMs: base + 60_000, Family: "veiculo", Class: "car", Score: 0.91,
			FrameMs: base + 62_000, Box: &box},
	} {
		if err := cam.AppendEvent(ev); err != nil {
			t.Fatal(err)
		}
	}

	rec := httptest.NewRecorder()
	s.handleEvents(rec, httptest.NewRequest(http.MethodGet, "/api/rec/events?cam=cam_teste&day=2026-08-08", nil))
	var raw struct {
		Objects []map[string]json.RawMessage `json:"objetos"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil || len(raw.Objects) != 2 {
		t.Fatalf("response %s (%v)", rec.Body.String(), err)
	}
	for _, key := range []string{"quadroMs", "caixa"} {
		if _, has := raw.Objects[0][key]; has {
			t.Errorf("the mark without a box came out with %q: %s", key, rec.Body.String())
		}
	}

	got := getEvents(t, s, "cam=cam_teste&day=2026-08-08").Objects[1]
	if got.FrameMs != base+62_000 {
		t.Errorf("quadroMs = %d, expected %d", got.FrameMs, base+62_000)
	}
	if got.Box == nil || *got.Box != box {
		t.Errorf("box = %v, expected %v", got.Box, box)
	}
}

// The tail path: the current day's screen asks again every ten seconds and only
// wants what it does not have yet.
func TestEventsTailBringsOnlyWhatIsNew(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	for _, off := range []int64{0, 60_000, 120_000} {
		if err := cam.AppendEvent(store.Event{AtMs: base.UnixMilli() + off}); err != nil {
			t.Fatal(err)
		}
	}

	from := base.UnixMilli() + 60_001
	got := getEvents(t, s, fmt.Sprintf("cam=cam_teste&from=%d&to=%d",
		from, base.Add(24*time.Hour).UnixMilli()))
	if len(got.Onsets) != 1 || got.Onsets[0] != base.UnixMilli()+120_000 {
		t.Errorf("tail = %v, expected only the mark at 120s", got.Onsets)
	}
}

// A camera without detection enabled is the common case, and the screen must
// tell "no marks" from "an error happened". Empty list, not null.
func TestEventsWithoutMarksReturnsEmptyList(t *testing.T) {
	s, _ := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)

	rec := httptest.NewRecorder()
	s.handleEvents(rec, httptest.NewRequest(http.MethodGet,
		"/api/rec/events?cam=cam_teste&day="+base.Format(store.DayLayout), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"onsets":[]`) {
		t.Errorf("expected empty list, got %s", rec.Body.String())
	}
}

// Adjacent segments must become ONE range: the 24h bar would be covered with
// fake 1-pixel gaps if each segment became a range.
func TestTimelineMergesContiguousSegments(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	seed(t, cam, base, [2]int64{0, 30000}, [2]int64{30000, 30000}, [2]int64{60000, 30000})

	got := getTimeline(t, s, "cam=cam_teste&day="+base.Format(store.DayLayout))
	if len(got.Ranges) != 1 {
		t.Fatalf("expected 1 range, got %d: %v", len(got.Ranges), got.Ranges)
	}
	if got.Ranges[0][0] != base.UnixMilli() || got.Ranges[0][1] != base.UnixMilli()+90000 {
		t.Errorf("range = %v, expected it to cover the whole 90s", got.Ranges[0])
	}
	if len(got.Segments) != 3 {
		t.Errorf("expected 3 segments, got %d", len(got.Segments))
	}
	if len(got.Gens) != 1 || got.Gens[0] != "aabbcc" {
		t.Errorf("generations table = %v", got.Gens)
	}
}

// A real gap must show up: that is exactly what the screen exists to show, and
// it is what was missing in the NVRs that motivated the project.
func TestTimelineSplitsTracksAtTheGap(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	// 30s, 40s gap, then 30s.
	seed(t, cam, base, [2]int64{0, 30000}, [2]int64{70000, 30000})

	got := getTimeline(t, s, "cam=cam_teste&day="+base.Format(store.DayLayout))
	if len(got.Ranges) != 2 {
		t.Fatalf("expected 2 ranges, got %d: %v", len(got.Ranges), got.Ranges)
	}
}

// A slack smaller than the tolerance is jitter, not a gap.
func TestTimelineIgnoresSmallSlack(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	seed(t, cam, base, [2]int64{0, 30000}, [2]int64{30500, 30000})

	got := getTimeline(t, s, "cam=cam_teste&day="+base.Format(store.DayLayout))
	if len(got.Ranges) != 1 {
		t.Errorf("500ms slack became a gap: %v", got.Ranges)
	}
}

// The recorder prevents overlap, but the index may have old data. The merge
// must not shorten a range because of that.
func TestTimelineDoesNotShrinkWithOverlap(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	seed(t, cam, base, [2]int64{0, 30000}, [2]int64{29000, 30000})

	got := getTimeline(t, s, "cam=cam_teste&day="+base.Format(store.DayLayout))
	if len(got.Ranges) != 1 {
		t.Fatalf("expected 1 range, got %v", got.Ranges)
	}
	if want := base.UnixMilli() + 59000; got.Ranges[0][1] != want {
		t.Errorf("range end = %d, expected %d", got.Ranges[0][1], want)
	}
}

// The Recordings screen rereads today in a loop asking only for the tail,
// starting 1ms after the start of the last segment it already has. It counts on
// that segment coming back in the response: the overlap is what tells whether
// the first new range continues the last old range, and without it the screen
// would have to repeat on its side the joining tolerance that lives here.
func TestPartialTimelineReturnsTheAnchorSegment(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	seed(t, cam, base, [2]int64{0, 30000}, [2]int64{30000, 30000}, [2]int64{60000, 30000})

	// From the last segment the screen knew (the middle one) onward.
	anchor := base.UnixMilli() + 30000
	end := base.AddDate(0, 0, 1).UnixMilli()
	got := getTimeline(t, s, fmt.Sprintf("cam=cam_teste&from=%d&to=%d", anchor+1, end))

	if len(got.Segments) != 2 {
		t.Fatalf("expected the anchor plus the new segment, got %v", got.Segments)
	}
	if got.Segments[0][0] != anchor {
		t.Errorf("first segment = %d, expected the anchor at %d", got.Segments[0][0], anchor)
	}
	// And the tail range must start at the anchor, not at the new segment: that
	// is what makes the screen recognize that the two ranges are the same.
	if len(got.Ranges) != 1 || got.Ranges[0][0] != anchor {
		t.Errorf("ranges = %v, expected a single one starting at %d", got.Ranges, anchor)
	}
}

func TestTimelineWithoutRecordingReturnsEmptyLists(t *testing.T) {
	s, _ := testServer(t)
	got := getTimeline(t, s, "cam=cam_teste&day=2026-08-08")
	// Empty lists, not null: the frontend should not have to handle both.
	if got.Ranges == nil || got.Segments == nil || got.Gens == nil {
		t.Errorf("expected empty lists, got ranges=%v segments=%v gens=%v",
			got.Ranges, got.Segments, got.Gens)
	}
}

// --- delete recordings ------------------------------------------------------

func deleteReq(t *testing.T, h http.HandlerFunc, url string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodDelete, url, nil))
	return rec
}

func freedBytes(t *testing.T, rec *httptest.ResponseRecorder) int64 {
	t.Helper()
	var out struct {
		FreedBytes int64 `json:"freedBytes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unreadable response: %v", err)
	}
	return out.FreedBytes
}

// The default is to keep. Deleting hours of video as a side effect of a click on
// "remove" would be too destructive to be implicit, and this test is what keeps
// the new option from becoming the default behavior by accident. The thumbnail
// and the segment find the entry through the index: the thumbnail cuts the init
// and the first fragment, and the segment, everything after the init. An instant
// that is not the start of a segment is 404.
func TestThumbAndSegmentCutTheIndexFile(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	var e store.Entry
	for i := range 3 {
		e = store.Entry{
			StartMs: base.UnixMilli() + int64(i)*30_000, DurMs: 30_000,
			Size: 2000, Gen: "aabbcc", InitSize: 737, FirstFrag: 500,
		}
		if err := cam.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	// Numbered bytes, so that a wrong cut does not go unnoticed.
	content := make([]byte, e.Size)
	for i := range content {
		content[i] = byte(i)
	}
	if err := cam.EnsureDirs(e.Day()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cam.SegmentPath(e.StartMs), content, 0o644); err != nil {
		t.Fatal(err)
	}

	request := func(h http.HandlerFunc, rota string, start int64) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodGet,
			fmt.Sprintf("/api/rec/%s?cam=cam_teste&t=%d", rota, start), nil))
		return rec
	}
	if rec := request(s.handleThumb, "thumb", e.StartMs); rec.Code != http.StatusOK ||
		!bytes.Equal(rec.Body.Bytes(), content[:e.InitSize+e.FirstFrag]) {
		t.Errorf("thumbnail: HTTP %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if rec := request(s.handleSegment, "seg", e.StartMs); rec.Code != http.StatusOK ||
		!bytes.Equal(rec.Body.Bytes(), content[e.InitSize:]) {
		t.Errorf("segment: HTTP %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if rec := request(s.handleThumb, "thumb", e.StartMs+1); rec.Code != http.StatusNotFound {
		t.Errorf("instant outside the index: HTTP %d, expected 404", rec.Code)
	}
}

func TestDeleteCameraKeepsRecordingsByDefault(t *testing.T) {
	s, cam := testServer(t)
	seed(t, cam, time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local), [2]int64{0, 30000})

	rec := deleteReq(t, s.handleDeleteCamera, "/api/cameras?id=cam_teste")
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(cam.Dir()); err != nil {
		t.Errorf("the recordings disappeared without anyone asking: %v", err)
	}
}

func TestDeleteCameraDeletesRecordingsWhenAsked(t *testing.T) {
	s, cam := testServer(t)
	seed(t, cam, time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local), [2]int64{0, 30000})

	rec := deleteReq(t, s.handleDeleteCamera, "/api/cameras?id=cam_teste&recordings=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(cam.Dir()); !os.IsNotExist(err) {
		t.Errorf("the camera directory is still there: err=%v", err)
	}
	if got := freedBytes(t, rec); got != 1000 {
		t.Errorf("freedBytes=%d, expected 1000", got)
	}
}

// Deleting the recordings must not unregister the camera: it has to keep
// recording, from scratch.
func TestDeleteRecordingsKeepsTheRegistration(t *testing.T) {
	s, cam := testServer(t)
	seed(t, cam, time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local), [2]int64{0, 30000})

	rec := deleteReq(t, s.handleDeleteRecordings, "/api/rec?cam=cam_teste")
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(cam.Dir()); !os.IsNotExist(err) {
		t.Errorf("the camera directory is still there: err=%v", err)
	}
	if !s.knownCamera("cam_teste") {
		t.Error("the camera was unregistered, and only the recordings should have gone")
	}
	if got := freedBytes(t, rec); got != 1000 {
		t.Errorf("freedBytes=%d, expected 1000", got)
	}
}

// The material of an already removed camera is reachable only from here - and
// the ID, which no longer goes through knownCamera, is checked against the
// directories that actually exist.
func TestDeleteRecordingsOfRemovedCamera(t *testing.T) {
	s, _ := testServer(t)
	orphan := s.store.Camera("cam_antiga")
	seed(t, orphan, time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local), [2]int64{0, 30000})

	rec := deleteReq(t, s.handleDeleteRecordings, "/api/rec?cam=cam_antiga")
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(orphan.Dir()); !os.IsNotExist(err) {
		t.Errorf("the orphan's directory is still there: err=%v", err)
	}
	// The in-memory index of an orphan is empty, so the total can only come
	// from the scan. Zero here would mean we are back to reading it from the
	// wrong place.
	if got := freedBytes(t, rec); got != 1000 {
		t.Errorf("freedBytes=%d, expected 1000", got)
	}
}

func TestDeleteRecordingsRequiresExistingDirectory(t *testing.T) {
	s, _ := testServer(t)
	for _, cam := range []string{"", "outra", "../../etc", "cam_teste/../x"} {
		rec := deleteReq(t, s.handleDeleteRecordings, "/api/rec?cam="+cam)
		if rec.Code != http.StatusNotFound {
			t.Errorf("cam=%q returned HTTP %d, expected 404", cam, rec.Code)
		}
	}
}

// An unregistered camera is rejected before it becomes a path on disk.
func TestUnknownCameraIsRejected(t *testing.T) {
	s, _ := testServer(t)
	for _, cam := range []string{"", "outra", "../../etc", "cam_teste/../x"} {
		rec := httptest.NewRecorder()
		s.handleDays(rec, httptest.NewRequest(http.MethodGet, "/api/rec/days?cam="+cam, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("cam=%q returned HTTP %d, expected 400", cam, rec.Code)
		}
	}
}

func TestValidGen(t *testing.T) {
	valid := []string{"aabbcc", "0123456789abcdef", "0123456789abcdef0123456789abcdef"}
	invalid := []string{"", "../../etc/passwd", "AABBCC", "aa-bb", "g123",
		"aabbccddeeff00112233445566778899aa", "0123456789abcdef0123456789abcdef0",
		"aabbcc\n"}

	for _, g := range valid {
		if err := validGen(g); err != nil {
			t.Errorf("validGen(%q) rejected: %v", g, err)
		}
	}
	for _, g := range invalid {
		if err := validGen(g); err == nil {
			t.Errorf("validGen(%q) accepted, should reject", g)
		}
	}
}

// defaultConfig is the config of someone who wrote no dwnvr.yaml at all.
func defaultConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, _, err := config.Load(filepath.Join(t.TempDir(), "dwnvr.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestValidateCameraQuota(t *testing.T) {
	// Zero is the case that separates "I did not set it" from "I set a bad
	// value": it must keep passing so that Resolve applies the default.
	floor := config.QuotaMBRange.Min
	valid := []int64{0, floor, floor + 1, 20480}
	invalid := []int64{-1, 1, floor - 1}

	cfg := defaultConfig(t)
	for _, q := range valid {
		if err := validateCamera(cfg, config.Camera{ID: "cam_teste", QuotaMB: q}); err != nil {
			t.Errorf("quotaMB=%d rejected: %v", q, err)
		}
	}
	for _, q := range invalid {
		if err := validateCamera(cfg, config.Camera{ID: "cam_teste", QuotaMB: q}); err == nil {
			t.Errorf("quotaMB=%d accepted, should reject", q)
		}
	}
}

// TestValidateCameraSegment: the API enforces the same range as the screen,
// from 10 to 300 s. It used to accept up to 3600.
func TestValidateCameraSegment(t *testing.T) {
	cfg := defaultConfig(t)
	for _, seg := range []int{0, 10, 30, 300} {
		if err := validateCamera(cfg, config.Camera{ID: "cam_teste", SegmentSeconds: seg}); err != nil {
			t.Errorf("segmentSeconds=%d rejected: %v", seg, err)
		}
	}
	for _, seg := range []int{-1, 5, 301, 600, 3600} {
		if err := validateCamera(cfg, config.Camera{ID: "cam_teste", SegmentSeconds: seg}); err == nil {
			t.Errorf("segmentSeconds=%d accepted, should reject", seg)
		}
	}
}

func TestValidateCameraDetect(t *testing.T) {
	// Empty and zero mean "use the default"; filling in only one of the two is
	// valid.
	valid := []config.Camera{
		{ID: "cam_teste"},
		{ID: "cam_teste", DetectMethod: "periodico"},
		{ID: "cam_teste", DetectSensitivity: 1},
		{ID: "cam_teste", DetectMethod: "kleinberg-p", DetectSensitivity: 5},
	}
	invalid := []config.Camera{
		{ID: "cam_teste", DetectMethod: "magico"},
		{ID: "cam_teste", DetectSensitivity: 6},
		{ID: "cam_teste", DetectMethod: "periodico", DetectSensitivity: 9},
	}
	cfg := defaultConfig(t)
	for _, c := range valid {
		if err := validateCamera(cfg, c); err != nil {
			t.Errorf("%q level %d rejected: %v", c.DetectMethod, c.DetectSensitivity, err)
		}
	}
	for _, c := range invalid {
		if err := validateCamera(cfg, c); err == nil {
			t.Errorf("%q level %d accepted, should reject", c.DetectMethod, c.DetectSensitivity)
		}
	}
}

// The new camera form starts from "padrao", and not from numbers repeated by
// hand in the interface: that is how it follows the defaults in dwnvr.yaml. The
// "faixas" do the same for the min/max of the inputs.
func TestCamerasBringsTheDefault(t *testing.T) {
	s, _ := testServer(t)
	s.cfg.Defaults.DetectMethod, s.cfg.Defaults.DetectSensitivity = "periodico", 2
	s.client = go2rtc.New(config.Go2RTC{URL: "http://127.0.0.1:1"}) // down

	rec := httptest.NewRecorder()
	s.requireAuth(s.handleCameras)(rec, httptest.NewRequest(http.MethodGet, "/api/cameras", nil))
	var got struct {
		Default config.Camera                     `json:"padrao"`
		Tracks  map[string]map[string]json.Number `json:"faixas"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response %s (%v)", rec.Body.String(), err)
	}
	p := got.Default
	if p.Detect == nil || *p.Detect || p.DetectMethod != "periodico" || p.DetectSensitivity != 2 {
		t.Errorf("padrao %+v: expected detect false, periodico, level 2", p)
	}

	// The min/max of the inputs comes from here. Without a ceiling, the max
	// goes away, and the screen input has no max instead of being stuck at
	// zero.
	seg, quota := got.Tracks["segmentSeconds"], got.Tracks["quotaMB"]
	if seg["min"] != "10" || seg["max"] != "300" {
		t.Errorf("faixas.segmentSeconds %v: expected min 10, max 300", seg)
	}
	if _, has := quota["max"]; quota["min"] != "100" || has {
		t.Errorf("faixas.quotaMB %v: expected min 100 and no max", quota)
	}
}

// The object detector queue only shows up in /api/health when the object
// detector is configured.
func TestHealthShowsTheDetectorOnlyWhenConfigured(t *testing.T) {
	hasDetector := func(s *Server) bool {
		rec := httptest.NewRecorder()
		s.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
		var got map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("response %s (%v)", rec.Body.String(), err)
		}
		_, has := got["detector"]
		return has
	}

	s, _ := testServer(t)
	if hasDetector(s) {
		t.Error("detector in /api/health without detector.url")
	}
	s.cfg.Detector.URL = "http://127.0.0.1:1"
	s.mgr = recorder.NewManager(s.cfg, nil, s.store, s.log)
	if !hasDetector(s) {
		t.Error("detector.url configured and /api/health does not show the queue")
	}
}

// The reset reconnects button on the Diagnostics screen reaches this route.
func TestResetReconnectsHasRoute(t *testing.T) {
	s, _ := testServer(t)
	s.client = go2rtc.New(config.Go2RTC{URL: "http://127.0.0.1:1"}) // the Handler builds the live proxy with it
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/reconnects/reset", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
}

// /api/health tells the size of the reconnects window, and only talks about
// go2rtc when it is not responding.
func TestHealthReconnectWindowAndGo2rtc(t *testing.T) {
	health := func(s *Server) map[string]json.RawMessage {
		rec := httptest.NewRecorder()
		s.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
		var got map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("response %s (%v)", rec.Body.String(), err)
		}
		return got
	}

	s, _ := testServer(t)
	got := health(s)
	if string(got["reconnectsWindowHours"]) != "8" {
		t.Errorf("reconnectsWindowHours = %s, expected 8", got["reconnectsWindowHours"])
	}
	if _, has := got["go2rtc"]; has {
		t.Error("go2rtc in /api/health with no failure at all")
	}

	// Closed port: the first exchange with go2rtc fails without a response.
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	s.client = go2rtc.New(config.Go2RTC{URL: closed.URL})
	_, _ = s.client.Version(t.Context())

	var g struct {
		Error string    `json:"error"`
		Since time.Time `json:"since"`
	}
	if err := json.Unmarshal(health(s)["go2rtc"], &g); err != nil || g.Error == "" || g.Since.IsZero() {
		t.Errorf("go2rtc unreachable and /api/health says %+v (%v)", g, err)
	}
}

// Recordings of a removed camera belong to dwnvr, but go in a separate field:
// added to dwnvrBytes they would vanish from the warning, and left out of
// everything the screen would credit them to other programs.
func TestHealthShowsOrphansSeparately(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 9, 3, 12, 0, 0, 0, time.Local)
	seed(t, cam, base, [2]int64{0, 30_000})
	seed(t, s.store.Camera("cam_velha"), base, [2]int64{0, 30_000}, [2]int64{30_000, 30_000})

	rec := httptest.NewRecorder()
	s.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	var got struct {
		Disk struct {
			DwnvrBytes  int64  `json:"dwnvrBytes"`
			OrphanBytes *int64 `json:"orphanBytes"`
		} `json:"disk"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response %s (%v)", rec.Body.String(), err)
	}
	if got.Disk.OrphanBytes == nil || *got.Disk.OrphanBytes != 2000 {
		t.Errorf("orphanBytes = %v, expected 2000", got.Disk.OrphanBytes)
	}
	if got.Disk.DwnvrBytes != 1000 {
		t.Errorf("dwnvrBytes = %d, expected only the 1000 of the registered camera", got.Disk.DwnvrBytes)
	}
}

// The Detections tab depends on this field to show up.
func TestCamerasTellsWhetherThereIsADetector(t *testing.T) {
	hasDetector := func(s *Server) bool {
		rec := httptest.NewRecorder()
		s.requireAuth(s.handleCameras)(rec, httptest.NewRequest(http.MethodGet, "/api/cameras", nil))
		var got struct {
			Detector *bool `json:"detector"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("response %s (%v)", rec.Body.String(), err)
		}
		if got.Detector == nil {
			t.Fatal("/api/cameras did not bring the detector field")
		}
		return *got.Detector
	}

	s, _ := testServer(t)
	s.client = go2rtc.New(config.Go2RTC{URL: "http://127.0.0.1:1"}) // down
	if hasDetector(s) {
		t.Error("detector: true without detector.url")
	}
	s.cfg.Detector.URL = "http://127.0.0.1:1"
	s.mgr = recorder.NewManager(s.cfg, nil, s.store, s.log)
	if !hasDetector(s) {
		t.Error("detector.url configured and /api/cameras says detector: false")
	}
}

func TestRangeParams(t *testing.T) {
	s, _ := testServer(t)

	// The day= shortcut must cover the whole day in local time.
	r := httptest.NewRequest(http.MethodGet, "/x?day=2026-08-08", nil)
	from, to, err := s.rangeParams(r)
	if err != nil {
		t.Fatalf("valid day rejected: %v", err)
	}
	if to-from != 24*3600*1000 {
		t.Errorf("day covered %dms, expected 24h", to-from)
	}
	if got := time.UnixMilli(from).Format("2006-01-02 15:04:05"); got != "2026-08-08 00:00:00" {
		t.Errorf("day started at %s", got)
	}

	ruins := []string{"day=08/08/2026", "from=abc&to=1", "from=1", "from=100&to=100", "from=200&to=100"}
	for _, q := range ruins {
		if _, _, err := s.rangeParams(httptest.NewRequest(http.MethodGet, "/x?"+q, nil)); err == nil {
			t.Errorf("%q was accepted, should be rejected", q)
		}
	}
}

// The export accepts up to the cap and rejects one millisecond more. With no
// recording in the interval, what fits in the cap falls into the "no recording"
// 404.
func TestExportRespectsTheCap(t *testing.T) {
	s, _ := testServer(t)
	const from = int64(1786176000000)
	cases := []struct {
		span time.Duration
		code int
	}{
		{maxExportSpan, http.StatusNotFound},
		{maxExportSpan + time.Millisecond, http.StatusBadRequest},
	}
	for _, c := range cases {
		url := fmt.Sprintf("/api/rec/export?cam=cam_teste&from=%d&to=%d", from, from+c.span.Milliseconds())
		rec := httptest.NewRecorder()
		s.handleExport(rec, httptest.NewRequest(http.MethodGet, url, nil))
		if rec.Code != c.code {
			t.Errorf("interval of %s returned HTTP %d, expected %d", c.span, rec.Code, c.code)
		}
	}
}

// The favicon is an SVG with a comment on top, and an XML comment does not
// allow a double hyphen. Mentioning the name of a CSS variable in there makes
// the file malformed - and the symptom is cruel: the server returns 200, with
// the right content-type and size, and the browser simply draws nothing. That
// is how a broken version ended up running in production without any deploy
// check complaining. This test is the missing gate.
func TestEmbeddedFaviconIsWellFormedXML(t *testing.T) {
	b, err := dist.ReadFile("dist/favicon.svg")
	if err != nil {
		t.Fatalf("favicon is not embedded: %v", err)
	}

	dec := xml.NewDecoder(bytes.NewReader(b))
	for {
		_, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("favicon.svg is malformed, the browser will not draw it: %v", err)
		}
	}
}

// It is no use for the file to be intact if the page stops pointing to it: the
// visible result is the same, a tab without an icon.
func TestIndexPointsToTheFavicon(t *testing.T) {
	b, err := dist.ReadFile("dist/index.html")
	if err != nil {
		t.Fatalf("index.html is not embedded: %v", err)
	}
	if !strings.Contains(string(b), `href="/favicon.svg"`) {
		t.Error("index.html does not reference /favicon.svg")
	}
}

// The manifest is what makes the browser offer "Install app", and it fails
// silently: with an icon missing or of the wrong size, the screen stays the
// same and the option disappears from the menu. This test checks what Chrome
// requires: valid JSON, served as JSON, with the declared icons embedded and of
// the stated size. The name is manifest.json, and not .webmanifest, because
// Go's type table does not know that extension and the image has no
// /etc/mime.types: it would come out as text/plain.
func TestInstallableAppManifest(t *testing.T) {
	s, _ := testServer(t)
	rec := httptest.NewRecorder()
	s.webHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/manifest.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("manifest.json: status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("manifest.json servido como %q", ct)
	}

	var m struct {
		Name     string
		StartURL string `json:"start_url"`
		Display  string
		Icons    []struct{ Src, Sizes, Purpose string }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("manifest.json is not valid JSON: %v", err)
	}
	if m.Name == "" || m.StartURL == "" || m.Display != "standalone" {
		t.Errorf("missing name, start_url or display standalone: %+v", m)
	}

	sizes := map[string]bool{}
	for _, ic := range m.Icons {
		b, err := dist.ReadFile("dist/" + ic.Src)
		if err != nil {
			t.Errorf("icon %s is not embedded: %v", ic.Src, err)
			continue
		}
		cfg, err := png.DecodeConfig(bytes.NewReader(b))
		if err != nil {
			t.Errorf("icon %s is not PNG: %v", ic.Src, err)
			continue
		}
		if got := fmt.Sprintf("%dx%d", cfg.Width, cfg.Height); got != ic.Sizes {
			t.Errorf("icon %s declares %s and is %s", ic.Src, ic.Sizes, got)
		}
		if ic.Purpose == "" {
			sizes[ic.Sizes] = true
		}
	}
	for _, wants := range []string{"192x192", "512x512"} {
		if !sizes[wants] {
			t.Errorf("missing a general-purpose %s icon, which Chrome requires to install", wants)
		}
	}
}
