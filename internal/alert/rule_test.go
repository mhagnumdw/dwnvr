package alert

import (
	"slices"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/area"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// leftHalf and backgroundFloor are two areas that cross: the first is the
// left half of the frame; the second, the whole bottom band of it.
var (
	leftHalf = config.Area{ID: "esq", Name: "Portão",
		Points: []area.Point{{0, 0}, {0.5, 0}, {0.5, 1}, {0, 1}}}
	backgroundFloor = config.Area{ID: "chao", Name: "Calçada", Rule: "touch",
		Points: []area.Point{{0, 0.8}, {1, 0.8}, {1, 1}, {0, 1}}}
)

// camera returns a camera that alerts, as Resolve hands it over.
func camera(families []detect.Family, areas ...string) config.Camera {
	yes := true
	return config.Camera{ID: "cam_a", Name: "Frente", Enabled: true, Detect: &yes,
		Areas: []config.Area{leftHalf, backgroundFloor},
		Alert: &config.CameraAlert{Enabled: true, Families: families, Areas: areas}}
}

// mark is a line written by the Recorder, at onset 1000 and frame 2400.
func mark(family, class string, score float64, box [4]float64) store.Event {
	return store.Event{AtMs: 1000, FrameMs: 2400, HasFrame: true,
		Family: family, Class: class, Score: score, Box: &box}
}

var (
	// personOnLeft has the foot at (0.3, 0.6): on the left half.
	personOnLeft = mark("pessoa", "person", 0.87, [4]float64{0.25, 0.2, 0.35, 0.6})
	// personOnRight has the foot at (0.8, 0.9): on the bottom band, on the right.
	personOnRight = mark("pessoa", "person", 0.55, [4]float64{0.75, 0.5, 0.85, 0.9})
	// motorcycleOnLeft has the foot at (0.2, 0.7).
	motorcycleOnLeft = mark("veiculo", "motorcycle", 0.71, [4]float64{0.1, 0.4, 0.3, 0.7})
)

// TestRuleOnlyExistsWithAlertOn: nil is "no alert", and covers everything
// that keeps the camera from having inferences or from alerting.
func TestRuleOnlyExistsWithAlertOn(t *testing.T) {
	no := false
	cases := map[string]func(c *config.Camera){
		"sem alerta":          func(c *config.Camera) { c.Alert = nil },
		"alerta desligado":    func(c *config.Camera) { c.Alert.Enabled = false },
		"sem família":         func(c *config.Camera) { c.Alert.Families = nil },
		"câmera desabilitada": func(c *config.Camera) { c.Enabled = false },
		"sem marcação":        func(c *config.Camera) { c.Detect = &no },
		"marcação ausente":    func(c *config.Camera) { c.Detect = nil },
		"áreas que sumiram":   func(c *config.Camera) { c.Alert.Areas = []string{"sumiu"} },
		"horários que não valem": func(c *config.Camera) {
			c.Alert.Schedules = []config.AlertSchedule{{Days: []time.Weekday{time.Monday}, From: "22:00", To: "06:00"}}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			cam := camera([]detect.Family{detect.Person}, "esq")
			change(&cam)
			if r := NewRule(cam); r != nil {
				t.Errorf("rule %+v, want nil", r)
			}
		})
	}
	if NewRule(camera([]detect.Family{detect.Person})) == nil {
		t.Error("the camera with the alert on got no rule")
	}
}

// TestOnTheWholeImage: without areas, every mark of an alert family passes,
// and the object carries no area. The motorcycle isn't in the family and is
// left out.
func TestOnTheWholeImage(t *testing.T) {
	r := NewRule(camera([]detect.Family{detect.Person}))
	jpeg := []byte("jpeg")
	a, ok := r.Evaluate([]store.Event{personOnLeft, personOnRight, motorcycleOnLeft}, jpeg)
	if !ok {
		t.Fatal("didn't alert")
	}
	if a.Camera != "cam_a" || a.CameraName != "Frente" || a.AtMs != 1000 || a.FrameMs != 2400 ||
		!a.HasFrame || string(a.Frame) != "jpeg" {
		t.Errorf("alert %+v", a)
	}
	// Two people in the same inference are a single alert, with both.
	if len(a.Objects) != 2 || a.Objects[0].Score != 0.87 || a.Objects[1].Score != 0.55 {
		t.Fatalf("objects %+v, want both people", a.Objects)
	}
	o := a.Objects[0]
	if o.Family != "pessoa" || o.Class != "person" || o.Box != *personOnLeft.Box || o.Areas != nil {
		t.Errorf("object %+v", o)
	}
}

// TestInTheAreas: only what falls in one of the alert areas passes, each
// area by its own rule, and the object carries the name of all it fell in.
func TestInTheAreas(t *testing.T) {
	cases := map[string]struct {
		areas []string
		marks []store.Event
		// expected: the score of each object that passes, and its areas.
		scores  []float64
		areasOf [][]string
	}{
		"pé na área": {
			[]string{"esq"}, []store.Event{personOnLeft, personOnRight},
			[]float64{0.87}, [][]string{{"Portão"}},
		},
		"ninguém na área": {
			[]string{"esq"}, []store.Event{personOnRight}, nil, nil,
		},
		"encosta, sem pisar": {
			// The left person's box goes up to 0.6 and doesn't touch the 0.8
			// band; the right one's does.
			[]string{"chao"}, []store.Event{personOnLeft, personOnRight},
			[]float64{0.55}, [][]string{{"Calçada"}},
		},
		"nas duas": {
			[]string{"esq", "chao"},
			[]store.Event{mark("pessoa", "person", 0.9, [4]float64{0.2, 0.5, 0.3, 0.95})},
			[]float64{0.9}, [][]string{{"Portão", "Calçada"}},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := NewRule(camera([]detect.Family{detect.Person}, c.areas...))
			a, ok := r.Evaluate(c.marks, nil)
			if ok != (len(c.scores) > 0) {
				t.Fatalf("alerted: %v, objects %+v", ok, a.Objects)
			}
			if len(a.Objects) != len(c.scores) {
				t.Fatalf("objects %+v, want %v", a.Objects, c.scores)
			}
			for i, o := range a.Objects {
				if o.Score != c.scores[i] || !slices.Equal(o.Areas, c.areasOf[i]) {
					t.Errorf("object %d: %+v, want score %v in areas %v", i, o, c.scores[i], c.areasOf[i])
				}
			}
		})
	}
}

// TestSeveralFamilies: with person and vehicle, both pass in the same alert.
func TestSeveralFamilies(t *testing.T) {
	r := NewRule(camera([]detect.Family{detect.Person, detect.Vehicle}, "esq"))
	a, ok := r.Evaluate([]store.Event{personOnLeft, motorcycleOnLeft}, nil)
	if !ok || len(a.Objects) != 2 || a.Objects[1].Family != "veiculo" {
		t.Errorf("alerted: %v, objects %+v", ok, a.Objects)
	}
}

// TestCarriesTheAlertAreas: the alert carries all its areas, with the
// points, and not only those some object fell in: the frame draws the outline
// of all of them. On the whole image, it carries none.
func TestCarriesTheAlertAreas(t *testing.T) {
	r := NewRule(camera([]detect.Family{detect.Person}, "esq", "chao"))
	a, ok := r.Evaluate([]store.Event{personOnLeft}, nil)
	if !ok || len(a.Areas) != 2 {
		t.Fatalf("alerted: %v, areas %+v", ok, a.Areas)
	}
	for i, expected := range []config.Area{leftHalf, backgroundFloor} {
		if a.Areas[i].Name != expected.Name || !slices.Equal(a.Areas[i].Points, expected.Points) {
			t.Errorf("area %d: %+v, want %s %v", i, a.Areas[i], expected.Name, expected.Points)
		}
	}

	r = NewRule(camera([]detect.Family{detect.Person}))
	if a, _ := r.Evaluate([]store.Event{personOnLeft}, nil); a.Areas != nil {
		t.Errorf("on the whole image it carried the areas %+v", a.Areas)
	}
}

// TestMarksTheLines: Evaluate marks, on the lines the Recorder is about to
// write, the object that passed, with the areas' id, not the name. What didn't
// pass stays as it was.
func TestMarksTheLines(t *testing.T) {
	r := NewRule(camera([]detect.Family{detect.Person}, "esq", "chao"))
	lines := []store.Event{personOnLeft, personOnRight, motorcycleOnLeft}
	if _, ok := r.Evaluate(lines, nil); !ok {
		t.Fatal("didn't alert")
	}
	expected := []struct {
		alert bool
		areas []string
	}{{true, []string{"esq"}}, {true, []string{"chao"}}, {false, nil}}
	for i, e := range expected {
		if lines[i].Alert != e.alert || !slices.Equal(lines[i].AlertAreas, e.areas) {
			t.Errorf("line %d: alert %v in %v, want %v in %v", i,
				lines[i].Alert, lines[i].AlertAreas, e.alert, e.areas)
		}
	}

	// Without areas in the alert, the mark comes without `alertAreas`.
	lines = []store.Event{personOnRight}
	NewRule(camera([]detect.Family{detect.Person})).Evaluate(lines, nil)
	if !lines[0].Alert || lines[0].AlertAreas != nil {
		t.Errorf("whole image: alert %v in %v", lines[0].Alert, lines[0].AlertAreas)
	}
}

// wednesday are the three Wednesday schedules from the docs example, plus
// all of Saturday.
var wednesday = []config.AlertSchedule{
	{Days: []time.Weekday{time.Wednesday}, From: "00:00", To: "05:15"},
	{Days: []time.Weekday{time.Wednesday}, From: "12:00", To: "13:00"},
	{Days: []time.Weekday{time.Wednesday}, From: "18:30", To: "23:59"},
	{Days: []time.Weekday{time.Saturday}, From: "00:00", To: "23:59"},
}

// atTime is the instant, in the server's time zone, on a weekday of October
// 2026 (the 18th is a Sunday), at the given hour, minute and second.
func atTime(day time.Weekday, hour, minute, second int) int64 {
	return time.Date(2026, 10, 18+int(day), hour, minute, second, 0, time.Local).UnixMilli()
}

// TestWithinSchedule: the onset has to fall in one of the schedules, with
// both ends included to the minute, and outside them no line gets the mark.
func TestWithinSchedule(t *testing.T) {
	cases := map[string]struct {
		atMs   int64
		alerts bool
	}{
		"de madrugada":             {atTime(time.Wednesday, 3, 0, 0), true},
		"no primeiro minuto":       {atTime(time.Wednesday, 0, 0, 0), true},
		"no último minuto":         {atTime(time.Wednesday, 5, 15, 59), true},
		"no minuto seguinte":       {atTime(time.Wednesday, 5, 16, 0), false},
		"entre dois horários":      {atTime(time.Wednesday, 11, 59, 59), false},
		"no De do segundo":         {atTime(time.Wednesday, 12, 0, 0), true},
		"no Até do segundo":        {atTime(time.Wednesday, 13, 0, 59), true},
		"depois do segundo":        {atTime(time.Wednesday, 13, 1, 0), false},
		"no fim do dia":            {atTime(time.Wednesday, 23, 59, 59), true},
		"na meia-noite seguinte":   {atTime(time.Thursday, 0, 0, 0), false},
		"na hora certa, outro dia": {atTime(time.Tuesday, 3, 0, 0), false},
		"no sábado inteiro":        {atTime(time.Saturday, 14, 30, 0), true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cam := camera([]detect.Family{detect.Person})
			cam.Alert.Schedules = wednesday
			line := personOnLeft
			line.AtMs = c.atMs
			lines := []store.Event{line}
			_, ok := NewRule(cam).Evaluate(lines, nil)
			if ok != c.alerts || lines[0].Alert != c.alerts {
				t.Errorf("alerted %v, line with alert %v; want %v", ok, lines[0].Alert, c.alerts)
			}
		})
	}
}

// TestScheduleWithAreas: within the schedule, the area still decides;
// outside, not even the person in the area alerts.
func TestScheduleWithAreas(t *testing.T) {
	cam := camera([]detect.Family{detect.Person}, "esq")
	cam.Alert.Schedules = wednesday
	r := NewRule(cam)
	for _, c := range []struct {
		atMs    int64
		objects int
	}{{atTime(time.Wednesday, 3, 0, 0), 1}, {atTime(time.Wednesday, 8, 0, 0), 0}} {
		lines := []store.Event{personOnLeft, personOnRight}
		for i := range lines {
			lines[i].AtMs = c.atMs
		}
		a, _ := r.Evaluate(lines, nil)
		if len(a.Objects) != c.objects || lines[0].Alert != (c.objects > 0) || lines[1].Alert {
			t.Errorf("at %s: objects %+v, lines %+v", time.UnixMilli(c.atMs).Format("15:04"), a.Objects, lines)
		}
	}
}
