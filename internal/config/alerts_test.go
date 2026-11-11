package config

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/detect"
)

// gate is a valid area, for the alert to point to.
var gate = Area{ID: "k3f9a2", Name: "Portão", Points: square}

// TestAlertHoldsAtBoot: an alert written by hand, with an area that exists,
// starts as it is.
func TestAlertHoldsAtBoot(t *testing.T) {
	_, cams, warnings, err := loadWithWarnings(t, "", `[{"id":"cam_a",
		"areas":[{"id":"k3f9a2","name":"Portão","points":[[0.1,0.1],[0.5,0.1],[0.5,0.5]]}],
		"alert":{"enabled":true,"families":["pessoa","veiculo"],"areas":["k3f9a2"]}}]`)
	if err != nil || len(warnings) > 0 {
		t.Fatalf("error %v, warnings %+v", err, warnings)
	}
	al := cams[0].Alert
	if al == nil || !al.Enabled || !slices.Equal(al.Families, []detect.Family{detect.Person, detect.Vehicle}) ||
		!slices.Equal(al.Areas, []string{"k3f9a2"}) {
		t.Errorf("alert %+v", al)
	}
}

// TestBadAlertComesOutWithWarning: a family or area that isn't valid comes
// out with a warning saying what's accepted, and the rest of the alert stays
// on. At boot that's it; in the API, the same warning becomes the rejection.
func TestBadAlertComesOutWithWarning(t *testing.T) {
	cases := map[string]struct {
		alert    CameraAlert
		field    string
		accepted string
		families []detect.Family
		areas    []string
	}{
		"família desconhecida": {
			CameraAlert{Enabled: true, Families: []detect.Family{detect.Person, "gente"}},
			"alert.families[1]", "pessoa, veiculo, animal", []detect.Family{detect.Person}, nil,
		},
		"família repetida": {
			CameraAlert{Enabled: true, Families: []detect.Family{detect.Person, detect.Person}},
			"alert.families[1]", "não se repita", []detect.Family{detect.Person}, nil,
		},
		"área que não existe": {
			CameraAlert{Enabled: true, Families: []detect.Family{detect.Person}, Areas: []string{"k3f9a2", "sumiu"}},
			"alert.areas[1]", "área desta câmera", []detect.Family{detect.Person}, []string{"k3f9a2"},
		},
		"área repetida": {
			CameraAlert{Enabled: true, Families: []detect.Family{detect.Person}, Areas: []string{"k3f9a2", "k3f9a2"}},
			"alert.areas[1]", "não se repita", []detect.Family{detect.Person}, []string{"k3f9a2"},
		},
	}
	cfg := &Config{}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			before := CameraAlert{Families: slices.Clone(c.alert.Families), Areas: slices.Clone(c.alert.Areas)}
			cam, warnings := cfg.CheckCamera(Camera{ID: "cam_a", Areas: []Area{gate}, Alert: &c.alert})
			if !slices.Equal(c.alert.Families, before.Families) || !slices.Equal(c.alert.Areas, before.Areas) {
				t.Error("CheckCamera changed the input alert")
			}
			al := cam.Alert
			if !al.Enabled || !slices.Equal(al.Families, c.families) || !slices.Equal(al.Areas, c.areas) {
				t.Errorf("got %+v, want on with %v and %v", al, c.families, c.areas)
			}
			if len(warnings) != 1 {
				t.Fatalf("%d warnings: %+v", len(warnings), warnings)
			}
			a := warnings[0]
			if a.Camera != "cam_a" || a.Field != c.field || !strings.Contains(a.Accepted, c.accepted) {
				t.Errorf("warning %+v, want %s with %q", a, c.field, c.accepted)
			}
		})
	}
}

// TestAlertWithoutFamilyIsDisabled: on and without a valid family, the
// alert has nothing to alert about, and is turned off.
func TestAlertWithoutFamilyIsDisabled(t *testing.T) {
	for name, families := range map[string][]detect.Family{
		"nenhuma":      nil,
		"só inválidas": {"gente"},
	} {
		t.Run(name, func(t *testing.T) {
			cam, warnings := (&Config{}).CheckCamera(Camera{ID: "cam_a",
				Alert: &CameraAlert{Enabled: true, Families: families}})
			if cam.Alert.Enabled {
				t.Error("the alert stayed on without a family")
			}
			if len(warnings) == 0 || warnings[len(warnings)-1].Field != "alert.families" {
				t.Errorf("warnings %+v, want them to end with alert.families", warnings)
			}
		})
	}
}

// TestAlertWithoutItsAreasIsDisabled: an alert that loses all its areas is
// turned off, and doesn't start applying to the whole image. It also holds for
// an area that was in the file but was dropped for being invalid.
func TestAlertWithoutItsAreasIsDisabled(t *testing.T) {
	invalid := Area{ID: "k3f9a2", Name: "Portão", Points: square[:2]}
	for name, areas := range map[string][]Area{
		"área apagada":  nil,
		"área inválida": {invalid},
	} {
		t.Run(name, func(t *testing.T) {
			cam, warnings := (&Config{}).CheckCamera(Camera{ID: "cam_a", Areas: areas,
				Alert: &CameraAlert{Enabled: true, Families: []detect.Family{detect.Person}, Areas: []string{"k3f9a2"}}})
			if cam.Alert.Enabled {
				t.Errorf("the alert stayed on: %+v", cam.Alert)
			}
			if len(warnings) == 0 || warnings[len(warnings)-1].Field != "alert.areas" {
				t.Errorf("warnings %+v, want them to end with alert.areas", warnings)
			}
		})
	}
}

// TestDisabledAlertKeepsTheRest: turning off doesn't erase the families or
// the areas, and off without a family is valid too.
func TestDisabledAlertKeepsTheRest(t *testing.T) {
	cfg := &Config{}
	stored := CameraAlert{Families: []detect.Family{detect.Animal}, Areas: []string{"k3f9a2"}}
	cam, warnings := cfg.CheckCamera(Camera{ID: "cam_a", Areas: []Area{gate}, Alert: &stored})
	if len(warnings) > 0 || cam.Alert.Enabled || !slices.Equal(cam.Alert.Families, stored.Families) ||
		!slices.Equal(cam.Alert.Areas, stored.Areas) {
		t.Errorf("alert %+v, warnings %+v", cam.Alert, warnings)
	}

	cam, warnings = cfg.CheckCamera(Camera{ID: "cam_a", Alert: &CameraAlert{}})
	if len(warnings) > 0 || cam.Alert == nil {
		t.Errorf("alert %+v, warnings %+v", cam.Alert, warnings)
	}
}

// TestNoAlertNoWarning: a camera without an alert, which is every camera
// today, gains nothing in the JSON.
func TestNoAlertNoWarning(t *testing.T) {
	cam, warnings := (&Config{}).CheckCamera(Camera{ID: "cam_a", Areas: []Area{gate}})
	if cam.Alert != nil || len(warnings) > 0 {
		t.Errorf("alert %+v, warnings %+v", cam.Alert, warnings)
	}
}

// TestSchedulesHoldAtBoot: schedules written by hand start as they are, and
// the overlapping ones (12:00 to 13:00 and 12:30 to 14:00 on Wednesday) apply
// as their union, with no warning. The first three are the example in
// docs/configuracao.md.
func TestSchedulesHoldAtBoot(t *testing.T) {
	_, cams, warnings, err := loadWithWarnings(t, "", `[{"id":"cam_a",
		"alert":{"enabled":true,"families":["pessoa"],"schedules":[
			{"days":[3],"from":"00:00","to":"05:15"},
			{"days":[3],"from":"12:00","to":"13:00"},
			{"days":[3],"from":"18:30","to":"23:59"},
			{"days":[1,3],"from":"12:30","to":"14:00"}]}}]`)
	if err != nil || len(warnings) > 0 {
		t.Fatalf("error %v, warnings %+v", err, warnings)
	}
	al := cams[0].Alert
	want := []AlertSchedule{
		{Days: []time.Weekday{time.Wednesday}, From: "00:00", To: "05:15"},
		{Days: []time.Weekday{time.Wednesday}, From: "12:00", To: "13:00"},
		{Days: []time.Weekday{time.Wednesday}, From: "18:30", To: "23:59"},
		{Days: []time.Weekday{time.Monday, time.Wednesday}, From: "12:30", To: "14:00"},
	}
	if al == nil || !al.Enabled || !reflect.DeepEqual(al.Schedules, want) {
		t.Errorf("alert %+v", al)
	}
}

// TestBadScheduleComesOutWithWarning: a schedule that isn't valid comes out
// whole, with a warning saying what's accepted, and the alert stays on with the
// others. A repeated day doesn't remove the schedule: it counts once.
func TestBadScheduleComesOutWithWarning(t *testing.T) {
	night := AlertSchedule{Days: []time.Weekday{time.Sunday}, From: "22:00", To: "23:59"}
	cases := map[string]struct {
		bad      AlertSchedule
		field    string
		accepted string
		kept     []AlertSchedule
	}{
		"sem dia": {AlertSchedule{From: "01:00", To: "02:00"},
			"alert.schedules[1].days", "pelo menos um dia", []AlertSchedule{night}},
		"dia 7": {AlertSchedule{Days: []time.Weekday{1, 7}, From: "01:00", To: "02:00"},
			"alert.schedules[1].days[1]", "de 0 (domingo) a 6 (sábado)", []AlertSchedule{night}},
		"dia negativo": {AlertSchedule{Days: []time.Weekday{-1}, From: "01:00", To: "02:00"},
			"alert.schedules[1].days[0]", "de 0 (domingo) a 6 (sábado)", []AlertSchedule{night}},
		"hora sem o zero": {AlertSchedule{Days: []time.Weekday{1}, From: "9:00", To: "10:00"},
			"alert.schedules[1].from", "HH:MM", []AlertSchedule{night}},
		"24:00": {AlertSchedule{Days: []time.Weekday{1}, From: "23:00", To: "24:00"},
			"alert.schedules[1].to", "HH:MM", []AlertSchedule{night}},
		"até igual ao de": {AlertSchedule{Days: []time.Weekday{1}, From: "10:00", To: "10:00"},
			"alert.schedules[1].to", "depois do from 10:00", []AlertSchedule{night}},
		"atravessa a meia-noite": {AlertSchedule{Days: []time.Weekday{1}, From: "22:00", To: "06:00"},
			"alert.schedules[1].to", "a noite são dois horários", []AlertSchedule{night}},
		"dia repetido": {AlertSchedule{Days: []time.Weekday{1, 2, 1}, From: "01:00", To: "02:00"},
			"alert.schedules[1].days[2]", "não se repita", []AlertSchedule{night,
				{Days: []time.Weekday{1, 2}, From: "01:00", To: "02:00"}}},
	}
	cfg := &Config{}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			in := []AlertSchedule{night, c.bad}
			days := slices.Clone(c.bad.Days)
			cam, warnings := cfg.CheckCamera(Camera{ID: "cam_a", Alert: &CameraAlert{Enabled: true,
				Families: []detect.Family{detect.Person}, Schedules: in}})
			if !slices.Equal(in[1].Days, days) {
				t.Error("CheckCamera changed the input days")
			}
			al := cam.Alert
			if !al.Enabled || !reflect.DeepEqual(al.Schedules, c.kept) {
				t.Errorf("got %+v, want on with %+v", al, c.kept)
			}
			if len(warnings) != 1 {
				t.Fatalf("%d warnings: %+v", len(warnings), warnings)
			}
			a := warnings[0]
			if a.Camera != "cam_a" || a.Field != c.field || !strings.Contains(a.Accepted, c.accepted) {
				t.Errorf("warning %+v, want %s with %q", a, c.field, c.accepted)
			}
		})
	}
}

// TestSchedulesBeyondTheCap: the schedule after MaxAlertSchedules is
// dropped, and the first ones stay.
func TestSchedulesBeyondTheCap(t *testing.T) {
	schedules := make([]AlertSchedule, MaxAlertSchedules+1)
	for i := range schedules {
		minute := fmt.Sprintf("%02d:%02d", i/60, i%60)
		schedules[i] = AlertSchedule{Days: []time.Weekday{time.Monday}, From: minute, To: "23:59"}
	}
	cam, warnings := (&Config{}).CheckCamera(Camera{ID: "cam_a", Alert: &CameraAlert{Enabled: true,
		Families: []detect.Family{detect.Person}, Schedules: schedules}})
	if !cam.Alert.Enabled || !reflect.DeepEqual(cam.Alert.Schedules, schedules[:MaxAlertSchedules]) {
		t.Errorf("%d schedules kept, on %v", len(cam.Alert.Schedules), cam.Alert.Enabled)
	}
	field := fmt.Sprintf("alert.schedules[%d]", MaxAlertSchedules)
	if len(warnings) != 1 || warnings[0].Field != field {
		t.Errorf("warnings %+v, want one on %s", warnings, field)
	}
}

// TestAlertWithoutValidScheduleIsDisabled: an alert that loses all its
// schedules is turned off. Without them it would apply at any time, and
// someone who marked only the night would get the day.
func TestAlertWithoutValidScheduleIsDisabled(t *testing.T) {
	cam, warnings := (&Config{}).CheckCamera(Camera{ID: "cam_a", Alert: &CameraAlert{Enabled: true,
		Families:  []detect.Family{detect.Person},
		Schedules: []AlertSchedule{{Days: []time.Weekday{1}, From: "22:00", To: "06:00"}}}})
	if cam.Alert.Enabled || len(cam.Alert.Schedules) != 0 {
		t.Errorf("alert %+v", cam.Alert)
	}
	if len(warnings) != 2 || warnings[1].Field != "alert.schedules" {
		t.Errorf("warnings %+v, want them to end with alert.schedules", warnings)
	}
}

func TestMinuteOfDay(t *testing.T) {
	for s, want := range map[string]int{"00:00": 0, "00:59": 59, "12:30": 750, "23:59": 1439} {
		if m, ok := MinuteOfDay(s); !ok || m != want {
			t.Errorf("MinuteOfDay(%q) = %d, %v; want %d", s, m, ok, want)
		}
	}
	for _, s := range []string{"", "9:00", "09:0", "24:00", "12:60", "+1:00", "12-00", "12:000", " 1:00", "1a:00"} {
		if m, ok := MinuteOfDay(s); ok {
			t.Errorf("MinuteOfDay(%q) = %d, want it rejected", s, m)
		}
	}
}

// TestAlertsEmailWithoutSection: whoever doesn't configure it pays nothing,
// and gets no warning for it.
func TestAlertsEmailWithoutSection(t *testing.T) {
	cfg, err := loadYAML(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if e := cfg.Alerts.Email; len(e.To) != 0 || len(e.Warnings) != 0 {
		t.Errorf("alerts.email %+v, expected empty and without a warning", e)
	}
}

// TestAlertsEmailDropsWhatDoesNotWork: an invalid address is dropped with a
// warning, a repeated one is dropped silently, and the others go on, in the
// written order.
func TestAlertsEmailDropsWhatDoesNotWork(t *testing.T) {
	cfg, err := loadYAML(t, `
smtp:
  host: smtp.gmail.com
  username: dwnvr@exemplo.com
  password: troque-me
alerts:
  email:
    to:
      - fulano@exemplo.com
      - "Ciclana <ciclana@exemplo.com>"
      - " beltrano@exemplo.com "
      - sem-arroba
      - Fulano@Exemplo.com
`)
	if err != nil {
		t.Fatal(err)
	}
	e := cfg.Alerts.Email
	if want := []string{"fulano@exemplo.com", "beltrano@exemplo.com"}; !slices.Equal(e.To, want) {
		t.Errorf("to %q, expected %q", e.To, want)
	}
	if len(e.Warnings) != 2 || !strings.Contains(e.Warnings[0], `"Ciclana <ciclana@exemplo.com>"`) ||
		!strings.Contains(e.Warnings[1], `"sem-arroba"`) {
		t.Errorf("warnings %q, expected one for the name in front and one for the missing @", e.Warnings)
	}
}

// TestAlertsEmailWithoutSMTP: the list without the smtp section doesn't
// block boot, and becomes a warning. With the section rejected, the warning is
// the section's, which says the reason.
func TestAlertsEmailWithoutSMTP(t *testing.T) {
	cfg, err := loadYAML(t, "alerts:\n  email:\n    to: [fulano@exemplo.com]\n")
	if err != nil {
		t.Fatal(err)
	}
	if av := cfg.Alerts.Email.Warnings; len(av) != 1 || !strings.Contains(av[0], "falta a seção smtp") {
		t.Errorf("warnings %q, expected the one for the missing smtp section", av)
	}

	cfg, err = loadYAML(t, "smtp:\n  host: smtp.gmail.com\n  security: ssl\n"+
		"alerts:\n  email:\n    to: [fulano@exemplo.com]\n")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SMTP.Problem == "" || len(cfg.Alerts.Email.Warnings) != 0 {
		t.Errorf("problem %q and warnings %q; expected only the problem", cfg.SMTP.Problem, cfg.Alerts.Email.Warnings)
	}
}

// TestAlertsEmailNeverBlocksBoot: a typo in the alerts section, which is
// optional, must not stop recording. A single address counts as a list of one;
// the rest that can't be read is left out with a warning, and dwnvr starts.
func TestAlertsEmailNeverBlocksBoot(t *testing.T) {
	const smtp = "smtp:\n  host: smtp.gmail.com\n  username: dwnvr@exemplo.com\n  password: troque-me\n"
	cases := []struct {
		name     string
		yaml     string
		to       []string
		warnings []string // a snippet of each warning, in order
	}{
		{"endereço sozinho", "alerts:\n  email:\n    to: fulano@exemplo.com\n",
			[]string{"fulano@exemplo.com"}, nil},
		{"endereço sozinho entre aspas", "alerts:\n  email:\n    to: \"fulano@exemplo.com\"\n",
			[]string{"fulano@exemplo.com"}, nil},
		{"to vazio", "alerts:\n  email:\n    to:\n", nil, nil},
		{"item que não é endereço", "alerts:\n  email:\n    to:\n      - fulano@exemplo.com\n      - {nome: ciclana}\n",
			[]string{"fulano@exemplo.com"}, []string{"linha 11 um item que não é endereço"}},
		{"to que não é endereço nem lista", "alerts:\n  email:\n    to: {nome: fulano}\n",
			nil, []string{"alerts.email.to (linha 9) precisa ser um endereço ou uma lista"}},
		{"email que não é seção", "alerts:\n  email: fulano@exemplo.com\n",
			nil, []string{"a seção alerts (linha 8) não está no formato esperado"}},
		{"alerts que não é seção", "alerts: [fulano@exemplo.com]\n",
			nil, []string{"a seção alerts (linha 7) não está no formato esperado"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := loadYAML(t, smtp+c.yaml)
			if err != nil {
				t.Fatalf("dwnvr didn't start: %v", err)
			}
			e := cfg.Alerts.Email
			if !slices.Equal(e.To, c.to) {
				t.Errorf("to %q, expected %q", e.To, c.to)
			}
			if len(e.Warnings) != len(c.warnings) {
				t.Fatalf("warnings %q, expected %d", e.Warnings, len(c.warnings))
			}
			for i, snippet := range c.warnings {
				if !strings.Contains(e.Warnings[i], snippet) {
					t.Errorf("warning %q, expected with %q", e.Warnings[i], snippet)
				}
			}
		})
	}
}
