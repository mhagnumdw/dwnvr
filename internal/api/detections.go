package api

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/area"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// defaultDetectionsLimit is the page when the screen doesn't give the size. It
// is about six phone screens in the two-column grid.
const defaultDetectionsLimit = 60

// maxDetectionsLimit is the cap for one page. A response with 200 detections is
// around 50 KB; more than that is the screen asking for what it won't show.
const maxDetectionsLimit = 200

// detection is the card of the Detections screen: one inference of the object
// detector on a camera, with everything it found in that frame. Two families at
// the same instant are TWO marks on disk, but only one detection: the frame is
// one.
type detection struct {
	Cam  string `json:"cam"`
	AtMs int64  `json:"instanteMs"`
	// FrameMs is the instant of the inferred frame, which is where the boxes
	// are in the right place. See store.Event.
	FrameMs int64 `json:"quadroMs,omitempty"`
	// HasFrame says whether there is an image at /api/deteccoes/quadro for this
	// detection.
	HasFrame bool            `json:"temQuadro"`
	Objects  []objectInFrame `json:"objetos"`
}

type objectInFrame struct {
	Family string      `json:"familia"`
	Class  string      `json:"classe"`
	Score  float64     `json:"score"`
	Box    *[4]float64 `json:"caixa,omitempty"`
	// Alert and AlertAreas come as recorded: the object passed the camera's
	// alert rule, in the areas with these ids. See store.Event.
	Alert      bool     `json:"alert,omitempty"`
	AlertAreas []string `json:"alertAreas,omitempty"`
}

type detectionsPage struct {
	// Detections always come from newest to oldest, in both directions.
	Detections []detection `json:"deteccoes"`
	// End says there is nothing more in the requested direction. It may be
	// false on a page that happened to get the last detection; the next one
	// comes empty and with End true.
	End bool `json:"fim"`
}

// handleDetections returns a page of object detections from all cameras together,
// from newest to oldest. It is the scrolling of the Detections screen.
//
//	antes=<ms>   the detections before this instant (scrolling down).
//	             Without it, the newest.
//	depois=<ms>  the ones after (scrolling up, after a "go to").
//	limite=<N>   how many; default defaultDetectionsLimit.
//	cams=a,b     only these cameras; without it, all.
//	familias=... only these families; without it, all.
//	area=... on the area's camera, only the objects inside it; see
//	filterFromQuery.
//	rule=...     the area rule: touch or foot (the default).
//	areas=...    the same, with registered areas: <camera>/<id>,<camera>/<id>.
//	alert=1 only the detections with an object that alerted; see
//	filterFromQuery.
//
// The page never splits an instant in half: if the last detection ties with others
// in the same millisecond, on another camera, they come together and the page goes
// a little past the limit. Without this the `antes` cursor would skip the ones
// left out.
func (s *Server) handleDetections(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Has("antes") && q.Has("depois") {
		writeError(w, http.StatusBadRequest, "use antes ou depois, não os dois")
		return
	}
	descending := !q.Has("depois")
	cursor := int64(math.MaxInt64)
	name := "antes"
	if !descending {
		name = "depois"
	}
	if q.Has(name) {
		v, err := strconv.ParseInt(q.Get(name), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "parâmetro "+name+" inválido")
			return
		}
		cursor = v
	}

	limit := defaultDetectionsLimit
	if q.Has("limite") {
		v, err := strconv.Atoi(q.Get("limite"))
		if err != nil || v < 1 || v > maxDetectionsLimit {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("parâmetro limite inválido (de 1 a %d)", maxDetectionsLimit))
			return
		}
		limit = v
	}

	cams, filter, err := s.filterFromQuery(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	page, err := detectionsPageOf(cams, filter, cursor, descending, limit)
	if err != nil {
		s.fail(w, "lendo as detecções", err)
		return
	}
	writeJSON(w, page)
}

// handleDetectionDays returns the days with at least one detection, in
// ascending order, and how many detections each one has. It is what the
// calendar of the Detections screen lets you pick, and the number it shows on
// each day.
//
//	cams, familias, area, rule, areas and alert: the same as
//	handleDetections.
func (s *Server) handleDetectionDays(w http.ResponseWriter, r *http.Request) {
	cams, filter, err := s.filterFromQuery(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	days, perDay, err := daysWithDetection(cams, filter)
	if err != nil {
		s.fail(w, "lendo os dias com detecção", err)
		return
	}
	writeJSON(w, map[string]any{"dias": days, "deteccoesPorDia": perDay})
}

// daysWithDetection walks the days with recordings and counts, in each one, the
// detections of the filter; a day with none is left out. Counting requires
// reading all cameras of each day: on the reference hardware, with a cold disk
// cache, the first call takes ~1.3 s, and the following ones come from the
// store's memory in ~5 ms. A day without recordings is left out even with a
// mark - scrolling doesn't reach it either, because it walks the same
// diasDasCameras.
func daysWithDetection(cams []*store.Camera, filter objectFilter) ([]string, map[string]int, error) {
	days := []string{}
	perDay := map[string]int{}
	for _, day := range camerasDays(cams) {
		n := 0
		for _, cam := range cams {
			marks, err := cam.DayObjects(day)
			if err != nil {
				return nil, nil, fmt.Errorf("%s, %s: %w", cam.ID, day, err)
			}
			n += countDetections(marks, filter.cameraOf(cam.ID))
		}
		if n > 0 {
			days = append(days, day)
			perDay[day] = n
		}
	}
	return days, perDay, nil
}

// countDetections counts the detections of a camera on a day the way group
// builds them: one per instant, however many families the frame has, and with
// `alert=1` only the instant where some remaining object alerted.
func countDetections(marks []store.Event, filter objectFilter) int {
	n := 0
	last := int64(-1)
	for _, m := range marks {
		if m.AtMs == last || !filter.passes(m) || filter.alertsOnly && !m.Alert {
			continue
		}
		n++
		last = m.AtMs
	}
	return n
}

// detectionsPageOf merges the cameras day by day, starting from the cursor's
// day and in the requested direction, until the page is full. Each day of each
// camera comes from the store's memory; only the first request for a day reads
// the disk.
func detectionsPageOf(cams []*store.Camera, filter objectFilter,
	cursor int64, descending bool, limit int) (detectionsPage, error) {

	days := camerasDays(cams)
	if descending {
		slices.Reverse(days)
	}
	var cursorDay string
	if cursor != math.MaxInt64 {
		cursorDay = time.UnixMilli(cursor).Format(store.DayLayout)
	}

	// In the scrolling direction: from newest to oldest going down, and the
	// opposite going up. The response going up is reversed at the end, so the
	// tie-break is also inverted: the same instant always comes out in the same
	// camera order, whichever direction it comes from.
	comesBefore := func(a, b detection) bool {
		if a.AtMs != b.AtMs {
			return (a.AtMs > b.AtMs) == descending
		}
		return (a.Cam < b.Cam) == descending
	}
	pastCursor := func(t int64) bool {
		if descending {
			return t < cursor
		}
		return t > cursor
	}

	out := detectionsPage{Detections: []detection{}, End: true}
	for i, day := range days {
		if cursorDay != "" && (descending && day > cursorDay || !descending && day < cursorDay) {
			continue
		}
		var ofDay []detection
		for _, cam := range cams {
			marks, err := cam.DayObjects(day)
			if err != nil {
				return out, fmt.Errorf("%s, %s: %w", cam.ID, day, err)
			}
			ofDay = group(ofDay, cam.ID, marks, filter.cameraOf(cam.ID), pastCursor)
		}
		sort.Slice(ofDay, func(a, b int) bool { return comesBefore(ofDay[a], ofDay[b]) })
		out.Detections = append(out.Detections, ofDay...)
		if len(out.Detections) < limit {
			continue
		}

		// Cuts at the limit, but takes along what ties with the last one.
		cut := limit
		last := out.Detections[cut-1].AtMs
		for cut < len(out.Detections) && out.Detections[cut].AtMs == last {
			cut++
		}
		leftover := cut < len(out.Detections) || i < len(days)-1
		out.Detections = out.Detections[:cut]
		out.End = !leftover
		break
	}
	if !descending {
		slices.Reverse(out.Detections)
	}
	return out, nil
}

// group merges the marks of a camera on a day into detections, one per instant,
// and appends them to `em`. The marks come in instant order, so the ones at the
// same instant are side by side.
func group(into []detection, cam string, marks []store.Event,
	filter objectFilter, pastCursor func(int64) bool) []detection {

	start := len(into)
	isOpen := -1 // index, in `em`, of the detection of the current instant
	for _, m := range marks {
		if !pastCursor(m.AtMs) || !filter.passes(m) {
			continue
		}
		obj := objectInFrame{Family: m.Family, Class: m.Class, Score: m.Score, Box: m.Box,
			Alert: m.Alert, AlertAreas: m.AlertAreas}
		if isOpen >= 0 && into[isOpen].AtMs == m.AtMs {
			d := &into[isOpen]
			d.Objects = append(d.Objects, obj)
			d.HasFrame = d.HasFrame || m.HasFrame
			continue
		}
		into = append(into, detection{Cam: cam, AtMs: m.AtMs, FrameMs: m.FrameMs,
			HasFrame: m.HasFrame, Objects: []objectInFrame{obj}})
		isOpen = len(into) - 1
	}
	if !filter.alertsOnly {
		return into
	}
	// With `alert=1` the detection stays whole, with the object that didn't
	// alert: the screen shows it dimmed, "not part of the alert".
	fresh := slices.DeleteFunc(into[start:], func(d detection) bool {
		return !slices.ContainsFunc(d.Objects, func(o objectInFrame) bool { return o.Alert })
	})
	return into[:start+len(fresh)]
}

// diasDasCameras is the union of the cameras' days with recordings, in
// ascending order. It comes from the in-memory summary, without touching the
// disk.
func camerasDays(cams []*store.Camera) []string {
	var days []string
	for _, c := range cams {
		for _, d := range c.Days() {
			days = append(days, d.Day)
		}
	}
	slices.Sort(days)
	return slices.Compact(days)
}

// objectFilter decides, mark by mark, which object stays in the detection. What
// goes takes along the detection left with no object at all.
type objectFilter struct {
	// families nil means "all".
	families map[string]bool
	// perCamera holds the areas of each camera: the object stays if it is in
	// any of them. A camera not in the map goes on without an area filter.
	perCamera map[string][]areaWithRule
	// areas are those of the marks' camera: see daCamera.
	areas []areaWithRule
	// alertsOnly keeps only the detection in which some remaining object
	// alerted. It is per detection, not per object: the one that didn't alert
	// stays in it.
	alertsOnly bool
}

// areaWithRule is an area with its rule: the one drawn on the spot, with the
// query's `rule`, or the registered one, with the registry's rule.
type areaWithRule struct {
	rule area.Rule
	area area.Area
}

// daCamera is the filter for one camera's marks: the areas only apply on their
// own camera, and the others only go through the family filter.
func (f objectFilter) cameraOf(cam string) objectFilter {
	f.areas = f.perCamera[cam]
	return f
}

// passes says whether the mark stays. With an area, a mark without a box, from
// before the box was recorded, is left out: there is no way to know where it
// was.
func (f objectFilter) passes(m store.Event) bool {
	if f.families != nil && !f.families[m.Family] {
		return false
	}
	if len(f.areas) == 0 {
		return true
	}
	if m.Box == nil {
		return false
	}
	for _, a := range f.areas {
		if a.rule.Inside(a.area, *m.Box) {
			return true
		}
	}
	return false
}

// add appends an area to its camera.
func (f *objectFilter) add(cam string, a areaWithRule) {
	if f.perCamera == nil {
		f.perCamera = map[string][]areaWithRule{}
	}
	f.perCamera[cam] = append(f.perCamera[cam], a)
}

// filterFromQuery resolves the parameters the list and the calendar share:
// `cams`, `familias`, `area`, `rule`, `areas` and `alert`.
//
// `area=<cam>:<x>,<y>,<x>,<y>,...` is an area drawn over the camera image, with
// X and Y in thousandths of the frame (integers from 0 to 1000), to keep the
// URL short. `areas=<cam>/<id>,<cam>/<id>` are registered areas, resolved by
// the current registry: editing the area changes the filter, including over the
// past. Neither the camera id nor the area id has a comma or a slash
// (ValidateCameraID and the generated area id).
//
// A camera's areas add up, the drawn one included: any of them will do. They
// restrict only their own camera: the others in `cams` stay whole, and the
// area's camera outside `cams` doesn't show up, with or without an area.
//
// `alert=1` keeps only the detections in which some object that passed the
// other filters alerted, by the mark recorded at the time: changing the
// camera's alert doesn't change the past. With `familias=veiculo`, the
// detection that alerted only because of the person goes, because the person
// goes.
func (s *Server) filterFromQuery(q url.Values) ([]*store.Camera, objectFilter, error) {
	var f objectFilter
	cams, err := s.filterCameras(q.Get("cams"))
	if err != nil {
		return nil, f, err
	}
	if q.Has("alert") {
		if q.Get("alert") != "1" {
			return nil, f, errors.New("parâmetro alert inválido (só alert=1)")
		}
		f.alertsOnly = true
	}
	if f.families, err = filterFamilies(q.Get("familias")); err != nil {
		return nil, f, err
	}
	if q.Has("areas") {
		if err := s.filterRegisteredAreas(&f, q.Get("areas")); err != nil {
			return nil, f, err
		}
	}
	if !q.Has("area") {
		if q.Has("rule") {
			return nil, f, errors.New("parâmetro rule sem area")
		}
		return cams, f, nil
	}

	cam, a, err := filterArea(q.Get("area"))
	if err != nil {
		return nil, f, err
	}
	if !s.knownCamera(cam) {
		return nil, f, fmt.Errorf("câmera %q da área não cadastrada", cam)
	}
	rule, err := area.NewRule(q.Get("rule"))
	if err != nil {
		return nil, f, err
	}
	f.add(cam, areaWithRule{rule: rule, area: a})
	return cams, f, nil
}

// filterRegisteredAreas resolves each `<cam>/<id>` of `areas=` through the
// registry. An area that no longer exists is rejected, like a camera that
// doesn't exist: filtering in silence would show a different list without
// saying so.
func (s *Server) filterRegisteredAreas(f *objectFilter, list string) error {
	registry := map[string]config.Camera{}
	for _, c := range s.mgr.Cameras() {
		registry[c.ID] = c
	}
	for item := range strings.SplitSeq(list, ",") {
		camID, id, ok := strings.Cut(item, "/")
		if !ok || camID == "" || id == "" {
			return fmt.Errorf("parâmetro areas inválido: %q não é <câmera>/<id>", item)
		}
		cam, ok := registry[camID]
		if !ok {
			return fmt.Errorf("câmera %q da área não cadastrada", camID)
		}
		i := slices.IndexFunc(cam.Areas, func(a config.Area) bool { return a.ID == id })
		if i < 0 {
			return fmt.Errorf("a câmera %q não tem a área %q", camID, id)
		}
		// The registry has already gone through CheckCamera: the rule and the
		// points are valid.
		cad := cam.Areas[i]
		rule, err := area.NewRule(cad.Rule)
		if err != nil {
			return fmt.Errorf("área %q da câmera %q: %w", id, camID, err)
		}
		a, err := area.New(cad.Points)
		if err != nil {
			return fmt.Errorf("área %q da câmera %q: %w", id, camID, err)
		}
		f.add(camID, areaWithRule{rule: rule, area: a})
	}
	return nil
}

// filterArea reads `<cam>:<x>,<y>,...`, in thousandths, and returns the camera
// and the area as a fraction of the frame.
func filterArea(v string) (string, area.Area, error) {
	i := strings.LastIndexByte(v, ':')
	if i <= 0 {
		return "", area.Area{}, errors.New("parâmetro area inválido: use <câmera>:<x>,<y>,<x>,<y> e assim por diante")
	}
	cam, numbers := v[:i], strings.Split(v[i+1:], ",")
	if len(numbers)%2 != 0 {
		return "", area.Area{}, errors.New("parâmetro area inválido: X e Y vão aos pares")
	}
	points := make([]area.Point, 0, len(numbers)/2)
	for j := 0; j < len(numbers); j += 2 {
		x, errX := strconv.Atoi(numbers[j])
		y, errY := strconv.Atoi(numbers[j+1])
		if errX != nil || errY != nil || x < 0 || x > 1000 || y < 0 || y > 1000 {
			return "", area.Area{}, fmt.Errorf("parâmetro area inválido: %q,%q não são milésimos de 0 a 1000",
				numbers[j], numbers[j+1])
		}
		points = append(points, area.Point{float64(x) / 1000, float64(y) / 1000})
	}
	a, err := area.New(points)
	if err != nil {
		return "", area.Area{}, fmt.Errorf("parâmetro area inválido: %w", err)
	}
	return cam, a, nil
}

// filterCameras resolves `cams=a,b`. Empty means all registered ones. An ID
// that isn't a registered camera is rejected, for the same reason as in
// camParam: it becomes a path on disk.
func (s *Server) filterCameras(list string) ([]*store.Camera, error) {
	var out []*store.Camera
	if list == "" {
		for _, c := range s.mgr.Cameras() {
			out = append(out, s.store.Camera(c.ID))
		}
		return out, nil
	}
	for id := range strings.SplitSeq(list, ",") {
		if !s.knownCamera(id) {
			return nil, fmt.Errorf("câmera %q não cadastrada", id)
		}
		out = append(out, s.store.Camera(id))
	}
	return out, nil
}

// filterFamilies resolves `familias=pessoa,veiculo`. Empty returns nil, which
// means "all" - including a family that a future object detector may mark.
func filterFamilies(list string) (map[string]bool, error) {
	if list == "" {
		return nil, nil
	}
	out := map[string]bool{}
	for f := range strings.SplitSeq(list, ",") {
		if !slices.Contains(detect.Families, detect.Family(f)) {
			return nil, fmt.Errorf("família %q desconhecida", f)
		}
		out[f] = true
	}
	return out, nil
}

// handleDetectionFrame serves the JPEG of the frame the object detector
// inferred on.
//
// The file goes as it is on disk: nothing is resized or re-encoded per request.
// It never changes after being written - the name is the instant -, so the
// browser can keep it forever, and scrolling back asks nothing of the server.
func (s *Server) handleDetectionFrame(w http.ResponseWriter, r *http.Request) {
	cam, err := s.camParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	t, err := strconv.ParseInt(r.URL.Query().Get("t"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "parâmetro t inválido")
		return
	}

	f, err := os.Open(cam.FramePath(time.UnixMilli(t).Format(store.DayLayout), t))
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "detecção sem quadro")
		return
	}
	if err != nil {
		s.fail(w, "abrindo o quadro", err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.fail(w, "abrindo o quadro", err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeContent(w, r, "", info.ModTime(), f)
}
