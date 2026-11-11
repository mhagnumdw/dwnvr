package config

import (
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/mhagnumdw/dwnvr/internal/detect"
)

// CameraAlert says when a detection from this camera becomes an alert. The
// alerta package evaluates and sends it; here is what the camera config
// stores.
type CameraAlert struct {
	// Enabled off keeps the rest, to turn it back on without redoing it.
	Enabled bool `json:"enabled"`

	// Families are the families that alert, like "pessoa". With the alert on,
	// at least one.
	Families []detect.Family `json:"families,omitempty"`

	// Areas are the IDs of this camera's areas the object has to fall in.
	// Empty means the whole image.
	Areas []string `json:"areas,omitempty"`

	// Schedules are the times when the alert goes out, added together. Empty
	// means any time. They apply in the server's time zone, the same as the
	// recordings' day.
	Schedules []AlertSchedule `json:"schedules,omitempty"`
}

// AlertSchedule is the days of the week and a From and a To, to the minute
// ("HH:MM"). Both ends are included, as in the Detections range: the whole
// day is "00:00" to "23:59". No schedule crosses midnight: To is greater than
// From, and the night is two schedules, one until 23:59 and another from
// 00:00.
type AlertSchedule struct {
	// Days goes from 0 (Sunday) to 6 (Saturday), as time.Weekday counts.
	Days []time.Weekday `json:"days"`
	From string         `json:"from"`
	To   string         `json:"to"`
}

// MinuteOfDay reads "HH:MM" as the minutes since midnight, from 0 (00:00) to
// 1439 (23:59). It only accepts the five characters, with leading zeros.
func MinuteOfDay(s string) (int, bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	for i, c := range []byte(s) {
		if i != 2 && (c < '0' || c > '9') {
			return 0, false
		}
	}
	h := int(s[0]-'0')*10 + int(s[1]-'0')
	m := int(s[3]-'0')*10 + int(s[4]-'0')
	if h > 23 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// checkAlert returns the alert with only the families, areas and schedules
// that are valid, and one Warning per item removed. The areas are those that
// already went through checkAreas: the alert can't point to an area that was
// dropped.
//
// On, but without any family, without any of the areas it had, or without any
// of the schedules it had, the alert is turned off. Without the areas it would
// apply to the whole image, and someone who marked only the gate would get the
// whole backyard without knowing why; without the schedules it would apply
// all day, and someone who marked only the night would get the day.
func checkAlert(cam Camera, av *[]Warning) *CameraAlert {
	if cam.Alert == nil {
		return nil
	}
	al := *cam.Alert
	reject := func(field string, value any, accepted, inEffect string) {
		*av = append(*av, Warning{Camera: cam.ID, Field: field, Value: value, Accepted: accepted, InEffect: inEffect})
	}

	families := make([]detect.Family, 0, len(al.Families))
	for i, f := range al.Families {
		switch {
		case !slices.Contains(detect.Families, f):
			reject(fmt.Sprintf("alert.families[%d]", i), fmt.Sprintf("%q", f),
				"uma família: "+acceptedFamilies(), "a família fica de fora do alerta")
		case slices.Contains(families, f):
			reject(fmt.Sprintf("alert.families[%d]", i), fmt.Sprintf("%q", f),
				"uma família que não se repita no alerta", "a família conta uma vez")
		default:
			families = append(families, f)
		}
	}

	areas := make([]string, 0, len(al.Areas))
	for i, id := range al.Areas {
		switch {
		case !slices.ContainsFunc(cam.Areas, func(a Area) bool { return a.ID == id }):
			reject(fmt.Sprintf("alert.areas[%d]", i), fmt.Sprintf("%q", id),
				"o id de uma área desta câmera", "a área fica de fora do alerta")
		case slices.Contains(areas, id):
			reject(fmt.Sprintf("alert.areas[%d]", i), fmt.Sprintf("%q", id),
				"uma área que não se repita no alerta", "a área conta uma vez")
		default:
			areas = append(areas, id)
		}
	}

	schedules, schedulesChanged := checkSchedules(al.Schedules, reject)

	if al.Enabled && len(families) == 0 {
		reject("alert.families", "vazio", "pelo menos uma família com o alerta ligado",
			"o alerta fica desligado")
		al.Enabled = false
	}
	if al.Enabled && len(al.Areas) > 0 && len(areas) == 0 {
		reject("alert.areas", "sem nenhuma área que exista",
			"pelo menos uma área da câmera, ou nenhuma para a imagem inteira",
			"o alerta fica desligado")
		al.Enabled = false
	}
	if al.Enabled && len(al.Schedules) > 0 && len(schedules) == 0 {
		reject("alert.schedules", "sem nenhum horário que valha",
			"pelo menos um horário válido, ou nenhum para valer a qualquer hora",
			"o alerta fica desligado")
		al.Enabled = false
	}

	// With nothing removed, the camera keeps its own slices: the stored JSON
	// doesn't swap an absent field for an empty one.
	if len(families) < len(al.Families) {
		al.Families = families
	}
	if len(areas) < len(al.Areas) {
		al.Areas = areas
	}
	if schedulesChanged {
		al.Schedules = schedules
	}
	return &al
}

// checkSchedules returns only the valid schedules, up to MaxAlertSchedules,
// and whether it removed or changed any. A schedule with a day outside 0 to 6,
// with no day, with a time that isn't HH:MM, or with To not after From is
// dropped whole. A repeated day counts once, and overlapping schedules apply
// as their union. The input slices don't change: the repeated day comes out
// in a copy.
func checkSchedules(in []AlertSchedule, reject func(field string, value any, accepted, inEffect string)) (
	[]AlertSchedule, bool) {
	const droppedNote = "o horário fica de fora do alerta"
	schedules := make([]AlertSchedule, 0, len(in))
	changed := false
	for i, h := range in {
		field := fmt.Sprintf("alert.schedules[%d]", i)
		fromMin, fromOk := MinuteOfDay(h.From)
		toMin, toOk := MinuteOfDay(h.To)
		outOfWeek := slices.IndexFunc(h.Days, func(d time.Weekday) bool { return d < time.Sunday || d > time.Saturday })
		switch {
		case len(schedules) >= MaxAlertSchedules:
			reject(field, fmt.Sprintf("%s até %s", h.From, h.To),
				fmt.Sprintf("até %d horários por câmera", MaxAlertSchedules), droppedNote)
		case len(h.Days) == 0:
			reject(field+".days", "vazio", "pelo menos um dia, de 0 (domingo) a 6 (sábado)", droppedNote)
		case outOfWeek >= 0:
			reject(fmt.Sprintf("%s.days[%d]", field, outOfWeek), int(h.Days[outOfWeek]),
				"um dia de 0 (domingo) a 6 (sábado)", droppedNote)
		case !fromOk:
			reject(field+".from", fmt.Sprintf("%q", h.From), "uma hora HH:MM, de 00:00 a 23:59", droppedNote)
		case !toOk:
			reject(field+".to", fmt.Sprintf("%q", h.To), "uma hora HH:MM, de 00:00 a 23:59", droppedNote)
		case toMin <= fromMin:
			reject(field+".to", fmt.Sprintf("%q", h.To),
				fmt.Sprintf("uma hora depois do from %s; a noite são dois horários, até 23:59 e a partir de 00:00",
					h.From), droppedNote)
		default:
			days := make([]time.Weekday, 0, len(h.Days))
			for j, d := range h.Days {
				if slices.Contains(days, d) {
					reject(fmt.Sprintf("%s.days[%d]", field, j), int(d),
						"um dia que não se repita no horário", "o dia conta uma vez")
					continue
				}
				days = append(days, d)
			}
			if len(days) < len(h.Days) {
				h.Days = days
				changed = true
			}
			schedules = append(schedules, h)
			continue
		}
		// The other cases removed the schedule.
		changed = true
	}
	return schedules, changed
}

func acceptedFamilies() string {
	names := make([]string, len(detect.Families))
	for i, f := range detect.Families {
		names[i] = string(f)
	}
	return strings.Join(names, ", ")
}

// Alerts is the alerts section of dwnvr.yaml: where the alert of every camera
// that turns it on goes. Optional: without it, the alert stays only in the
// record, which is the log line and the mark on the detection.
type Alerts struct {
	Email AlertsEmail `yaml:"email"`
}

// UnmarshalYAML reads the section without ever failing: it's optional, and a
// typo in it must not keep dwnvr from starting and recording. A single
// address, without brackets, counts as a list of one. What can't be read is
// left out with a warning, and without a list no alert e-mail goes out.
func (a *Alerts) UnmarshalYAML(n *yaml.Node) error {
	var raw struct {
		Email struct {
			To yaml.Node `yaml:"to"`
		} `yaml:"email"`
	}
	if err := n.Decode(&raw); err != nil {
		a.Email.Warnings = append(a.Email.Warnings, fmt.Sprintf(
			"a seção alerts (linha %d) não está no formato esperado, e nenhum e-mail de alerta sai", n.Line))
		return nil
	}
	to := &raw.Email.To
	switch {
	case to.Kind == 0 || to.ShortTag() == "!!null":
		// No list: the alert stays only in the record.
	case to.Kind == yaml.ScalarNode:
		a.Email.To = []string{to.Value}
	case to.Kind == yaml.SequenceNode:
		for _, item := range to.Content {
			if item.Kind != yaml.ScalarNode {
				a.Email.Warnings = append(a.Email.Warnings, fmt.Sprintf(
					"alerts.email.to tem na linha %d um item que não é endereço de e-mail, e ele ficou de fora da lista", item.Line))
				continue
			}
			a.Email.To = append(a.Email.To, item.Value)
		}
	default:
		a.Email.Warnings = append(a.Email.Warnings, fmt.Sprintf(
			"alerts.email.to (linha %d) precisa ser um endereço ou uma lista de endereços, e nenhum e-mail de alerta sai", to.Line))
	}
	return nil
}

// AlertsEmail is the e-mail alert. It goes out through the smtp section, the
// same one as the set-password link: here is only who receives it.
type AlertsEmail struct {
	// To receives the alert of every camera with the alert on, in a single
	// message to everyone. Addresses only, no name: "fulano@exemplo.com".
	To []string `yaml:"to"`

	// Warnings are the section's problems, which don't block boot: a section
	// or list that can't be read, an address left out of the list, and a list
	// without the smtp section. They go to the log and to the Warnings card in
	// Diagnostics, in pt-BR until i18n.
	Warnings []string `yaml:"-"`
}

// check removes from To what isn't an address and the duplicates, and says
// what it removed. The rule is the one for a user's e-mail
// (usuarios.ValidarEmail): the bare address, no name in front. One wrong
// address doesn't silence the others.
func (e *AlertsEmail) check(smtp SMTP) []string {
	var warnings []string
	var to []string
	for _, p := range e.To {
		p = strings.TrimSpace(p)
		if a, err := mail.ParseAddress(p); err != nil || a.Address != p {
			warnings = append(warnings, fmt.Sprintf(
				"alerts.email.to %q não é um endereço de e-mail e ficou de fora da lista", p))
			continue
		}
		// The same address twice would get two copies of the message.
		if !slices.ContainsFunc(to, func(x string) bool { return strings.EqualFold(x, p) }) {
			to = append(to, p)
		}
	}
	e.To = to
	// A rejected smtp section already has its own warning, with the reason.
	if len(to) > 0 && smtp.Host == "" {
		warnings = append(warnings, "falta a seção smtp, e sem ela a lista alerts.email.to não recebe nada")
	}
	return warnings
}
