package alert

import (
	"slices"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/area"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// Rule is a camera's alert built for evaluating: the areas are already
// polygon and rule, the schedules are already minutes, and the inference only
// does the math.
type Rule struct {
	camera, name string
	families     []detect.Family
	areas        []ruleArea
	// outlines are the same areas as the Alert carries them, built once here:
	// the inference just passes the slice along.
	outlines []AlertArea

	// schedules are, per weekday, the schedules in minutes of the day. Nil
	// means any time; a day not in the map doesn't alert.
	schedules map[time.Weekday][]daySchedule
}

// daySchedule is one schedule of a day, in minutes since midnight, with both
// ends included: from 0 to 1439 is the whole day.
type daySchedule struct{ fromMin, toMin int }

type ruleArea struct {
	id, name string
	area     area.Area
	rule     area.Rule
}

// NewRule builds the camera's rule already checked by CheckCamera, or returns
// nil when it doesn't alert: camera disabled, without motion marking or
// without the alert on. The nil is the "no alert" tested on every inference,
// and costs no more than that test.
func NewRule(cam config.Camera) *Rule {
	al := cam.Alert
	if !cam.Enabled || cam.Detect == nil || !*cam.Detect || al == nil || !al.Enabled || len(al.Families) == 0 {
		return nil
	}
	r := &Rule{camera: cam.ID, name: cam.Name, families: slices.Clone(al.Families)}
	for _, id := range al.Areas {
		i := slices.IndexFunc(cam.Areas, func(a config.Area) bool { return a.ID == id })
		if i < 0 {
			continue
		}
		a := cam.Areas[i]
		polygon, errPoints := area.New(a.Points)
		rule, errRule := area.NewRule(a.Rule)
		if errPoints != nil || errRule != nil {
			continue
		}
		r.areas = append(r.areas, ruleArea{id: a.ID, name: a.Name, area: polygon, rule: rule})
		r.outlines = append(r.outlines, AlertArea{Name: a.Name, Points: polygon.Points})
	}
	for _, h := range al.Schedules {
		fromMin, fromOk := config.MinuteOfDay(h.From)
		toMin, toOk := config.MinuteOfDay(h.To)
		if !fromOk || !toOk || toMin <= fromMin {
			continue
		}
		if r.schedules == nil {
			r.schedules = map[time.Weekday][]daySchedule{}
		}
		for _, d := range h.Days {
			r.schedules[d] = append(r.schedules[d], daySchedule{fromMin: fromMin, toMin: toMin})
		}
	}
	// CheckCamera already turned off an alert that lost its areas or
	// schedules; this only guarantees it never falls back to the whole image, or
	// to any time, some other way.
	if (len(al.Areas) > 0 && len(r.areas) == 0) || (len(al.Schedules) > 0 && r.schedules == nil) {
		return nil
	}
	return r
}

// Evaluate says whether an inference's marks become an alert, and marks on
// them the objects that pass: `Alert` and, with areas, `AlertAreas`. With
// schedules, the onset has to fall in one of them, and outside them no object
// passes. Inside, each mark of an alert family that falls on the whole image,
// or in some of the areas, each by its own rule, passes. All that pass go in
// the same alert: two people in the same inference are a single alert.
//
// The marks are the lines the Recorder is about to write, all from the same
// onset, and the evaluation comes before the write so the mark goes into it:
// the alert has the same numbers Detections shows.
func (r *Rule) Evaluate(marks []store.Event, frame []byte) (Alert, bool) {
	if len(marks) == 0 || !r.withinSchedule(marks[0].AtMs) {
		return Alert{}, false
	}
	var objects []Object
	for i, m := range marks {
		if m.Box == nil || !slices.Contains(r.families, detect.Family(m.Family)) {
			continue
		}
		var ids, names []string
		for _, a := range r.areas {
			if a.rule.Inside(a.area, *m.Box) {
				ids, names = append(ids, a.id), append(names, a.name)
			}
		}
		if len(r.areas) > 0 && len(ids) == 0 {
			continue
		}
		marks[i].Alert, marks[i].AlertAreas = true, ids
		objects = append(objects, Object{Family: m.Family, Class: m.Class, Score: m.Score,
			Box: *m.Box, Areas: names})
	}
	if len(objects) == 0 {
		return Alert{}, false
	}
	m := marks[0]
	return Alert{Camera: r.camera, CameraName: r.name, AtMs: m.AtMs, FrameMs: m.FrameMs,
		HasFrame: m.HasFrame, Objects: objects, Areas: r.outlines, Frame: frame}, true
}

// withinSchedule says whether the instant falls in one of the alert's
// schedules, in the server's time zone, the same that makes the recordings'
// day. The minute counts: "until 13:00" goes until 13:00:59. Without
// schedules, every instant falls in.
func (r *Rule) withinSchedule(atMs int64) bool {
	if r.schedules == nil {
		return true
	}
	t := time.UnixMilli(atMs)
	minute := t.Hour()*60 + t.Minute()
	for _, h := range r.schedules[t.Weekday()] {
		if minute >= h.fromMin && minute <= h.toMin {
			return true
		}
	}
	return false
}
