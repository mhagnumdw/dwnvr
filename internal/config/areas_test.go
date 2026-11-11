package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/area"
)

// square is any valid area.
var square = []area.Point{{0.1, 0.1}, {0.5, 0.1}, {0.5, 0.5}, {0.1, 0.5}}

// TestAreaWithoutIDGetsOneAtBoot: an area written by hand without an id gets
// one, and an existing id doesn't change.
func TestAreaWithoutIDGetsOneAtBoot(t *testing.T) {
	cfg, cams, warnings, err := loadWithWarnings(t, "", `[{"id":"cam_a","areas":[
		{"name":"Portão","points":[[0.1,0.1],[0.5,0.1],[0.5,0.5]]},
		{"id":"k3f9a2","name":"Garagem","rule":"touch","points":[[0.1,0.1],[0.5,0.1],[0.5,0.5]]}]}]`)
	if err != nil || len(warnings) > 0 {
		t.Fatalf("error %v, warnings %+v", err, warnings)
	}
	as := cams[0].Areas
	if len(as) != 2 {
		t.Fatalf("%d areas, want 2: %+v", len(as), as)
	}
	if len(as[0].ID) != areaIDLen || !validAreaID(as[0].ID) || as[0].ID == "k3f9a2" {
		t.Errorf("generated id %q", as[0].ID)
	}
	if as[1].ID != "k3f9a2" {
		t.Errorf("the existing id became %q", as[1].ID)
	}

	// A missing rule is only filled in by Resolve, and in its copy.
	r := cfg.Resolve(cams[0])
	if r.Areas[0].Rule != area.DefaultRule || r.Areas[1].Rule != "touch" {
		t.Errorf("resolved rules %q and %q", r.Areas[0].Rule, r.Areas[1].Rule)
	}
	if cams[0].Areas[0].Rule != "" {
		t.Errorf("Resolve wrote %q on the raw camera", cams[0].Areas[0].Rule)
	}
}

// TestGeneratedIDsDoNotRepeat: eight areas without an id on one camera come
// out with eight different ids, and the input camera doesn't change.
func TestGeneratedIDsDoNotRepeat(t *testing.T) {
	cam := Camera{ID: "cam_a"}
	for i := range area.MaxAreasPerCamera {
		cam.Areas = append(cam.Areas, Area{Name: fmt.Sprint("área ", i), Points: square})
	}
	fresh := cam.WithAreaIDs()
	seen := map[string]bool{}
	for _, a := range fresh.Areas {
		if seen[a.ID] {
			t.Errorf("id %q repeated", a.ID)
		}
		seen[a.ID] = true
	}
	if cam.Areas[0].ID != "" {
		t.Error("WithAreaIDs changed the input camera")
	}
}

// TestBadAreaComesOutWithWarning: each invalid area comes out with a warning
// saying what's accepted, and the valid one stays. At boot that's it; in the
// API, the same warning becomes the rejection.
func TestBadAreaComesOutWithWarning(t *testing.T) {
	good := Area{ID: "boa", Name: "Portão", Points: square}
	cases := map[string]struct {
		bad      Area
		accepted string
	}{
		"nome vazio":     {Area{ID: "x1", Name: "  ", Points: square}, "nome"},
		"nome repetido":  {Area{ID: "x1", Name: " portão ", Points: square}, "não se repita"},
		"id repetido":    {Area{ID: "boa", Name: "Outra", Points: square}, "id que não se repita"},
		"id com barra":   {Area{ID: "a/b", Name: "Outra", Points: square}, "letras minúsculas"},
		"id vazio":       {Area{Name: "Outra", Points: square}, "letras minúsculas"},
		"regra estranha": {Area{ID: "x1", Name: "Outra", Rule: "inside", Points: square}, "foot ou touch"},
		"dois pontos":    {Area{ID: "x1", Name: "Outra", Points: square[:2]}, "pontos"},
		"fora do quadro": {Area{ID: "x1", Name: "Outra", Points: []area.Point{{0, 0}, {1.2, 0}, {0, 1}}}, "de 0 a 1"},
	}
	cfg := &Config{}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cam, warnings := cfg.CheckCamera(Camera{ID: "cam_a", Areas: []Area{good, c.bad}})
			if len(cam.Areas) != 1 || cam.Areas[0].ID != "boa" {
				t.Errorf("kept %+v, want only the good one", cam.Areas)
			}
			if len(warnings) != 1 {
				t.Fatalf("%d warnings: %+v", len(warnings), warnings)
			}
			a := warnings[0]
			if a.Camera != "cam_a" || a.Field != "areas[1]" || !strings.Contains(a.Accepted, c.accepted) {
				t.Errorf("warning %+v, want areas[1] with %q", a, c.accepted)
			}
		})
	}
}

// TestTooManyAreasAreDropped: past the cap, the extra ones are dropped.
func TestTooManyAreasAreDropped(t *testing.T) {
	cam := Camera{ID: "cam_a"}
	for i := range area.MaxAreasPerCamera + 2 {
		cam.Areas = append(cam.Areas, Area{ID: fmt.Sprint("a", i), Name: fmt.Sprint("área ", i), Points: square})
	}
	cam, warnings := (&Config{}).CheckCamera(cam)
	if len(cam.Areas) != area.MaxAreasPerCamera || len(warnings) != 2 {
		t.Errorf("%d areas and %d warnings, want %d and 2", len(cam.Areas), len(warnings), area.MaxAreasPerCamera)
	}
}

// TestNoAreasNoWarning: a camera without areas, which is every camera
// today, gains nothing in the JSON.
func TestNoAreasNoWarning(t *testing.T) {
	cam, warnings := (&Config{}).CheckCamera(Camera{ID: "cam_a"})
	if cam.Areas != nil || len(warnings) > 0 {
		t.Errorf("areas %+v, warnings %+v", cam.Areas, warnings)
	}
}
