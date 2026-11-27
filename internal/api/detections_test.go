package api

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/area"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// detectionsScenario sets up two cameras with detections on two days, and returns noon
// of the second day.
//
//	day 1 (yesterday), 12:00 + N min: cam_test 0, 2 · cam_b 1
//	day 2 (today), 12:00 + N min: cam_test 0 (person+vehicle), 3 · cam_b 1 (animal),
//	3
//
// Minute 3 of today ties on both cameras, in the same millisecond.
func detectionsScenario(t *testing.T) (*Server, time.Time) {
	t.Helper()
	s, camTest := testServer(t)
	s.mgr.Set(config.Camera{ID: "cam_b", Name: "B", Enabled: true})
	camB := s.store.Camera("cam_b")

	today := time.Date(2026, 8, 9, 12, 0, 0, 0, time.Local)
	yesterday := today.AddDate(0, 0, -1)
	for _, cam := range []*store.Camera{camTest, camB} {
		seed(t, cam, yesterday, [2]int64{0, 30_000})
		seed(t, cam, today, [2]int64{0, 30_000})
	}

	mark := func(cam *store.Camera, base time.Time, min int64, family string) {
		t.Helper()
		ev := store.Event{AtMs: base.UnixMilli() + min*60_000, Family: family, Class: "x", Score: 0.9}
		if err := cam.AppendEvent(store.Event{AtMs: ev.AtMs}); err != nil { // the onset
			t.Fatal(err)
		}
		if err := cam.AppendEvent(ev); err != nil {
			t.Fatal(err)
		}
	}
	mark(camTest, yesterday, 0, "person")
	mark(camTest, yesterday, 2, "person")
	mark(camB, yesterday, 1, "vehicle")
	mark(camTest, today, 0, "person")
	mark(camTest, today, 0, "vehicle")
	mark(camTest, today, 3, "person")
	mark(camB, today, 1, "animal")
	mark(camB, today, 3, "person")
	return s, today
}

func getPage(t *testing.T, s *Server, query string) detectionsPage {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleDetections(rec, httptest.NewRequest(http.MethodGet, "/api/detections?"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var out detectionsPage
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unreadable response: %v", err)
	}
	return out
}

// labels sums up the page as "cam@minute", relative to today's noon.
func labels(p detectionsPage, today time.Time) []string {
	out := []string{}
	for _, d := range p.Detections {
		out = append(out, fmt.Sprintf("%s@%d", d.Cam, (d.AtMs-today.UnixMilli())/60_000))
	}
	return out
}

func equal(a, b []string) bool { return fmt.Sprint(a) == fmt.Sprint(b) }

// Without a cursor, the newest of all cameras together; and two families at the
// same instant are a single detection.
func TestDetectionsPageMergesTheCameras(t *testing.T) {
	s, today := detectionsScenario(t)
	p := getPage(t, s, "")
	want := []string{"cam_b@3", "cam_test@3", "cam_b@1", "cam_test@0",
		"cam_test@-1438", "cam_b@-1439", "cam_test@-1440"}
	if got := labels(p, today); !equal(got, want) || !p.End {
		t.Fatalf("page %v end=%v, want %v end=true", got, p.End, want)
	}
	if n := len(p.Detections[3].Objects); n != 2 {
		t.Errorf("the two families of the same frame gave %d objects, want 2", n)
	}
}

// Scrolling down, page by page, goes through everything exactly once - and the
// tie at minute 3 comes whole, even going past the limit.
func TestDetectionsPageScrollsWithoutSkippingOrRepeating(t *testing.T) {
	s, today := detectionsScenario(t)

	p := getPage(t, s, "limit=1")
	if got := labels(p, today); !equal(got, []string{"cam_b@3", "cam_test@3"}) || p.End {
		t.Fatalf("first page %v end=%v: the tie needed to come whole", got, p.End)
	}

	var all []string
	query := "limit=2"
	for range 10 {
		p := getPage(t, s, query)
		all = append(all, labels(p, today)...)
		if p.End {
			break
		}
		query = fmt.Sprintf("limit=2&before=%d", p.Detections[len(p.Detections)-1].AtMs)
	}
	want := []string{"cam_b@3", "cam_test@3", "cam_b@1", "cam_test@0",
		"cam_test@-1438", "cam_b@-1439", "cam_test@-1440"}
	if !equal(all, want) {
		t.Errorf("scrolling: %v, want %v", all, want)
	}
}

// After a "go to", scrolling up brings the ones closest to the cursor, and
// still in screen order: from newest to oldest.
func TestDetectionsPageGoesUpFromTheCursor(t *testing.T) {
	s, today := detectionsScenario(t)
	cursor := today.AddDate(0, 0, -1).UnixMilli() + 60_000 // yesterday, cam_b@-1439

	p := getPage(t, s, fmt.Sprintf("limit=2&after=%d", cursor))
	if got := labels(p, today); !equal(got, []string{"cam_test@0", "cam_test@-1438"}) || p.End {
		t.Errorf("going up: %v end=%v", got, p.End)
	}
	p = getPage(t, s, fmt.Sprintf("after=%d", today.UnixMilli()+60_000))
	if got := labels(p, today); !equal(got, []string{"cam_b@3", "cam_test@3"}) || !p.End {
		t.Errorf("reaching the top: %v end=%v", got, p.End)
	}
}

// The family filter removes the object, and the detection left with none
// disappears.
func TestDetectionsPageFilters(t *testing.T) {
	s, today := detectionsScenario(t)

	p := getPage(t, s, "families=vehicle,animal")
	if got := labels(p, today); !equal(got, []string{"cam_b@1", "cam_test@0", "cam_b@-1439"}) {
		t.Errorf("families: %v", got)
	}
	if o := p.Detections[1].Objects; len(o) != 1 || o[0].Family != "vehicle" {
		t.Errorf("the hidden person stayed in the detection: %+v", o)
	}

	p = getPage(t, s, "cams=cam_b")
	if got := labels(p, today); !equal(got, []string{"cam_b@3", "cam_b@1", "cam_b@-1439"}) {
		t.Errorf("cameras: %v", got)
	}
}

// gateInThousandths is the area of the filter test: X from 0.4 to 0.6 and Y
// from 0.5 to 0.7, as the query sends it.
const gateInThousandths = "cam_test:400,500,600,500,600,700,400,700"

// areaScenario adds to detectionsScenario marks with a box, from today:
//
//	cam_test 5: person with a foot on the gate, and a vehicle far from it
//	cam_test 6: person in front, covering the gate, with the foot below
//	cam_b     5: person in the same place, but on another camera
//
// The marks of detectionsScenario have no box.
func areaScenario(t *testing.T) (*Server, time.Time) {
	t.Helper()
	s, today := detectionsScenario(t)
	mark := func(cam string, min int64, family string, box [4]float64) {
		t.Helper()
		ev := store.Event{AtMs: today.UnixMilli() + min*60_000, Family: family, Class: "x",
			Score: 0.9, Box: &box}
		if err := s.store.Camera(cam).AppendEvent(ev); err != nil {
			t.Fatal(err)
		}
	}
	mark("cam_test", 5, "person", [4]float64{0.45, 0.3, 0.55, 0.65})
	mark("cam_test", 5, "vehicle", [4]float64{0.05, 0.05, 0.15, 0.2})
	mark("cam_test", 6, "person", [4]float64{0.35, 0.2, 0.65, 0.95})
	mark("cam_b", 5, "person", [4]float64{0.45, 0.3, 0.55, 0.65})
	return s, today
}

// The area filter removes the object that is outside, like the family one, but
// only on the area's camera: the other cameras stay whole. The mark without a
// box on the area's camera goes: there is no way to know where it was.
func TestDetectionsPageFiltersByArea(t *testing.T) {
	s, today := areaScenario(t)
	camB := []string{"cam_b@3", "cam_b@1", "cam_b@-1439"}

	p := getPage(t, s, "area="+gateInThousandths)
	if got := labels(p, today); !equal(got, append([]string{"cam_b@5", "cam_test@5"}, camB...)) {
		t.Errorf("foot, the default: %v", got)
	}
	if o := p.Detections[1].Objects; len(o) != 1 || o[0].Family != "person" {
		t.Errorf("the vehicle outside the area stayed in the detection: %+v", o)
	}

	p = getPage(t, s, "area="+gateInThousandths+"&rule=touch")
	if got := labels(p, today); !equal(got, append([]string{"cam_test@6", "cam_b@5", "cam_test@5"}, camB...)) {
		t.Errorf("touch: %v", got)
	}

	// The whole image gives the same as no area, except for the marks without a
	// box.
	for _, rule := range []string{"touch", "foot"} {
		p = getPage(t, s, "area=cam_test:0,0,1000,0,1000,1000,0,1000&rule="+rule)
		if got := labels(p, today); !equal(got, append([]string{"cam_test@6", "cam_b@5", "cam_test@5"}, camB...)) {
			t.Errorf("whole image, %s: %v", rule, got)
		}
		if o := p.Detections[2].Objects; len(o) != 2 {
			t.Errorf("whole image, %s: the minute 5 detection lost an object: %+v", rule, o)
		}
	}

	p = getPage(t, s, "area="+gateInThousandths+"&rule=touch&families=vehicle")
	if got := labels(p, today); !equal(got, []string{"cam_b@-1439"}) {
		t.Errorf("family and area together: %v", got)
	}

	// The area's camera, when unchecked, disappears from the list, with or
	// without an area.
	p = getPage(t, s, "cams=cam_b&area="+gateInThousandths)
	if got := labels(p, today); !equal(got, append([]string{"cam_b@5"}, camB...)) {
		t.Errorf("the area's camera outside cams: %v", got)
	}

	p = getPage(t, s, "cams=cam_test&area="+gateInThousandths)
	if got := labels(p, today); !equal(got, []string{"cam_test@5"}) {
		t.Errorf("only the area's camera: %v", got)
	}
}

// The calendar follows the area, as it follows cameras and families, and counts
// the other cameras whole.
func TestDetectionDaysByArea(t *testing.T) {
	s, today := areaScenario(t)
	day := today.Format(store.DayLayout)
	yesterday := today.AddDate(0, 0, -1).Format(store.DayLayout)
	for q, want := range map[string]map[string]int{
		"area=" + gateInThousandths:                      {yesterday: 1, day: 4},
		"area=" + gateInThousandths + "&rule=touch":      {yesterday: 1, day: 5},
		"area=cam_test:0,0,100,0,100,100,0,100":          {yesterday: 1, day: 3},
		"area=" + gateInThousandths + "&families=animal": {day: 1},
		"area=" + gateInThousandths + "&cams=cam_test":   {day: 1},
	} {
		rec := httptest.NewRecorder()
		s.handleDetectionDays(rec, httptest.NewRequest(http.MethodGet, "/api/detections/days?"+q, nil))
		var out struct {
			DetectionsPerDay map[string]int `json:"detectionsPerDay"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: unreadable response: %s", q, rec.Body.String())
		}
		if !maps.Equal(out.DetectionsPerDay, want) {
			t.Errorf("%s: %v, want %v", q, out.DetectionsPerDay, want)
		}
	}
}

// registerAreas gives the cameras of areaScenario the registry areas:
//
//	cam_test/portao: the gate of gateInThousandths, with foot
//	cam_test/canto:  the top left corner, with touch, where the vehicle is
//	cam_b/portao:     the same gate, on the other camera
func registerAreas(s *Server) {
	gate := []area.Point{{0.4, 0.5}, {0.6, 0.5}, {0.6, 0.7}, {0.4, 0.7}}
	corner := []area.Point{{0, 0}, {0.2, 0}, {0.2, 0.25}, {0, 0.25}}
	s.mgr.Set(config.Camera{ID: "cam_test", Name: "Teste", Enabled: true, Areas: []config.Area{
		{ID: "portao", Name: "Portão", Rule: "foot", Points: gate},
		{ID: "canto", Name: "Canto", Rule: "touch", Points: corner},
	}})
	s.mgr.Set(config.Camera{ID: "cam_b", Name: "B", Enabled: true, Areas: []config.Area{
		{ID: "portao", Name: "Portão", Points: gate},
	}})
}

// The registered areas filter like the drawn one, each with its own rule, and
// those of one camera add up: any of them will do. The drawn one joins the sum.
func TestDetectionsPageFiltersByRegisteredArea(t *testing.T) {
	s, today := areaScenario(t)
	registerAreas(s)
	camB := []string{"cam_b@3", "cam_b@1", "cam_b@-1439"}

	p := getPage(t, s, "areas=cam_test/portao")
	if got := labels(p, today); !equal(got, append([]string{"cam_b@5", "cam_test@5"}, camB...)) {
		t.Errorf("the gate: %v", got)
	}
	if o := p.Detections[1].Objects; len(o) != 1 || o[0].Family != "person" {
		t.Errorf("the vehicle outside the gate stayed in the detection: %+v", o)
	}

	p = getPage(t, s, "areas=cam_test/portao,cam_test/canto")
	if got := labels(p, today); !equal(got, append([]string{"cam_b@5", "cam_test@5"}, camB...)) {
		t.Errorf("gate and corner: %v", got)
	}
	if o := p.Detections[1].Objects; len(o) != 2 {
		t.Errorf("gate and corner: the sum lost the corner vehicle: %+v", o)
	}

	p = getPage(t, s, "areas=cam_test/canto&area="+gateInThousandths)
	if o := p.Detections[1].Objects; len(o) != 2 {
		t.Errorf("the drawn one and the registered one: the sum lost an object: %+v", o)
	}

	// Each camera with its own area: the person at cam_b@5 is at the gate, and
	// the marks without a box on both go.
	p = getPage(t, s, "areas=cam_test/portao,cam_b/portao")
	if got := labels(p, today); !equal(got, []string{"cam_b@5", "cam_test@5"}) {
		t.Errorf("one area on each camera: %v", got)
	}
}

func TestDetectionDaysByRegisteredArea(t *testing.T) {
	s, today := areaScenario(t)
	registerAreas(s)
	rec := httptest.NewRecorder()
	s.handleDetectionDays(rec, httptest.NewRequest(http.MethodGet,
		"/api/detections/days?areas=cam_test/portao", nil))
	var out struct {
		DetectionsPerDay map[string]int `json:"detectionsPerDay"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unreadable response: %s", rec.Body.String())
	}
	want := map[string]int{today.AddDate(0, 0, -1).Format(store.DayLayout): 1, today.Format(store.DayLayout): 4}
	if !maps.Equal(out.DetectionsPerDay, want) {
		t.Errorf("%v, want %v", out.DetectionsPerDay, want)
	}
}

func TestAreaFilterRejectsBadParameter(t *testing.T) {
	s, _ := areaScenario(t)
	registerAreas(s)
	for _, q := range []string{
		"rule=foot", // rule without area
		"area=" + gateInThousandths + "&rule=pisa", // the screen's name isn't the JSON one
		"area=semCamera",
		"area=:0,0,1000,0,0,1000",
		"area=cam_test:0,0,1000,0,1000",   // X without Y
		"area=cam_test:0,0,1000,0",        // two points
		"area=cam_test:0,0,1001,0,0,1000", // outside the frame
		"area=cam_test:0,0,x,0,0,1000",
		"area=fantasma:0,0,1000,0,0,1000",
		"areas=cam_test/fantasma", // the deleted area
		"areas=fantasma/portao",
		"areas=cam_test",
		"areas=cam_test/",
		"areas=/portao",
		"areas=cam_test/portao,",               // empty item
		"areas=cam_test/portao,cam_b/fantasma", // one good and one deleted
		"areas=",
	} {
		for name, h := range map[string]http.HandlerFunc{
			"detections": s.handleDetections, "days": s.handleDetectionDays,
		} {
			rec := httptest.NewRecorder()
			h(rec, httptest.NewRequest(http.MethodGet, "/api/detections?"+q, nil))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s, %s: HTTP %d, want 400", name, q, rec.Code)
			}
		}
	}
}

// alertScenario adds to detectionsScenario marks with the alert recorded:
//
//	today, cam_test 7: person who alerted at the gate, and a vehicle that
//	didn't
//	today,     cam_b     8: person who didn't alert
//	yesterday, cam_b     4: person who alerted on the whole image
func alertScenario(t *testing.T) (*Server, time.Time) {
	t.Helper()
	s, today := detectionsScenario(t)
	mark := func(cam string, base time.Time, min int64, family string, alerted bool, areas ...string) {
		t.Helper()
		ev := store.Event{AtMs: base.UnixMilli() + min*60_000, Family: family, Class: "x",
			Score: 0.9, Alert: alerted, AlertAreas: areas}
		if err := s.store.Camera(cam).AppendEvent(ev); err != nil {
			t.Fatal(err)
		}
	}
	yesterday := today.AddDate(0, 0, -1)
	mark("cam_test", today, 7, "person", true, "portao")
	mark("cam_test", today, 7, "vehicle", false)
	mark("cam_b", today, 8, "person", false)
	mark("cam_b", yesterday, 4, "person", true)
	return s, today
}

// `alert=1` keeps only the detection with an object that alerted, and whole:
// the vehicle that wasn't part of the alert stays in it, without the mark. The
// mark comes as recorded, with the area id. With the family of the object that
// alerted left out, the detection goes.
func TestDetectionsPageAlertsOnly(t *testing.T) {
	s, today := alertScenario(t)

	p := getPage(t, s, "alert=1")
	if got := labels(p, today); !equal(got, []string{"cam_test@7", "cam_b@-1436"}) {
		t.Fatalf("alerts only: %v", got)
	}
	o := p.Detections[0].Objects
	if len(o) != 2 || !o[0].Alert || !slices.Equal(o[0].AlertAreas, []string{"portao"}) ||
		o[1].Alert || o[1].AlertAreas != nil {
		t.Errorf("objects of minute 7: %+v", o)
	}
	if o := p.Detections[1].Objects; len(o) != 1 || !o[0].Alert || o[0].AlertAreas != nil {
		t.Errorf("alert on the whole image: %+v", o)
	}

	if got := labels(getPage(t, s, "alert=1&families=vehicle"), today); len(got) != 0 {
		t.Errorf("vehicle that didn't alert: %v", got)
	}
	if got := labels(getPage(t, s, "alert=1&cams=cam_b"), today); !equal(got, []string{"cam_b@-1436"}) {
		t.Errorf("alerts only from cam_b: %v", got)
	}

	// Without the filter, the mark comes all the same.
	p = getPage(t, s, "cams=cam_test&families=person")
	if o := p.Detections[0].Objects; labels(p, today)[0] != "cam_test@7" || !o[0].Alert {
		t.Errorf("without the filter: %v %+v", labels(p, today), o)
	}

	rec := httptest.NewRecorder()
	s.handleDetectionDays(rec, httptest.NewRequest(http.MethodGet, "/api/detections/days?alert=1", nil))
	var days struct {
		DetectionsPerDay map[string]int `json:"detectionsPerDay"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &days); err != nil {
		t.Fatal(err)
	}
	if want := map[string]int{today.Format(store.DayLayout): 1,
		today.AddDate(0, 0, -1).Format(store.DayLayout): 1}; !maps.Equal(days.DetectionsPerDay, want) {
		t.Errorf("calendar: %v, want %v", days.DetectionsPerDay, want)
	}
}

func TestDetectionsPageRejectsBadParameter(t *testing.T) {
	s, _ := detectionsScenario(t)
	for _, q := range []string{"cams=../etc", "families=fantasma", "limit=0",
		"limit=999", "before=x", "before=1&after=2", "alert=0", "alert=true"} {
		rec := httptest.NewRecorder()
		s.handleDetections(rec, httptest.NewRequest(http.MethodGet, "/api/detections?"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: HTTP %d, want 400", q, rec.Code)
		}
	}
}

// The calendar only offers a day with a detection of the filter: the day that
// only recorded is left out, and the camera and family filter removes the day
// that became empty. The count is of detections, not of marks: person and
// vehicle in the same frame today count only once.
func TestDetectionDays(t *testing.T) {
	s, today := detectionsScenario(t)
	dayBeforeYesterday := today.AddDate(0, 0, -2)
	seed(t, s.store.Camera("cam_test"), dayBeforeYesterday, [2]int64{0, 30_000}) // recorded, detected nothing

	yesterday := today.AddDate(0, 0, -1).Format(store.DayLayout)
	day := today.Format(store.DayLayout)
	for q, want := range map[string]map[string]int{
		"":                                   {yesterday: 3, day: 4},
		"families=animal":                    {day: 1},
		"families=person,vehicle":            {yesterday: 3, day: 3},
		"cams=cam_b&families=vehicle":        {yesterday: 1},
		"cams=cam_b&families=animal,vehicle": {yesterday: 1, day: 1},
	} {
		rec := httptest.NewRecorder()
		s.handleDetectionDays(rec, httptest.NewRequest(http.MethodGet, "/api/detections/days?"+q, nil))
		var out struct {
			Days             []string       `json:"days"`
			DetectionsPerDay map[string]int `json:"detectionsPerDay"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Days == nil || out.DetectionsPerDay == nil {
			t.Fatalf("%s: unreadable response or no list: %s", q, rec.Body.String())
		}
		wantDays := slices.Sorted(maps.Keys(want))
		if !equal(out.Days, wantDays) {
			t.Errorf("%s: %v, want %v", q, out.Days, wantDays)
		}
		if !maps.Equal(out.DetectionsPerDay, want) {
			t.Errorf("%s: %v, want %v", q, out.DetectionsPerDay, want)
		}
	}

	for _, q := range []string{"cams=../etc", "families=fantasma"} {
		rec := httptest.NewRecorder()
		s.handleDetectionDays(rec, httptest.NewRequest(http.MethodGet, "/api/detections/days?"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: HTTP %d, want 400", q, rec.Code)
		}
	}
}

// The frame goes as it is on disk, cacheable forever; a detection without a
// frame is 404, and the screen shows the empty frame.
func TestDetectionFrame(t *testing.T) {
	s, cam := testServer(t)
	t0 := time.Date(2026, 8, 9, 12, 0, 0, 0, time.Local).UnixMilli()
	jpeg := []byte("\xff\xd8 pretends to be a jpeg \xff\xd9")
	if err := cam.WriteFrame(time.UnixMilli(t0).Format(store.DayLayout), t0, jpeg); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handleDetectionFrame(rec, httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/detections/frame?cam=cam_test&t=%d", t0), nil))
	if rec.Code != http.StatusOK || rec.Body.String() != string(jpeg) {
		t.Fatalf("HTTP %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if h := rec.Header(); h.Get("Content-Type") != "image/jpeg" || h.Get("Cache-Control") == "" {
		t.Errorf("headers: %v", h)
	}

	rec = httptest.NewRecorder()
	s.handleDetectionFrame(rec, httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/detections/frame?cam=cam_test&t=%d", t0+1), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("no frame: HTTP %d, want 404", rec.Code)
	}
}
