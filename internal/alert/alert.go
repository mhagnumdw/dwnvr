// Package alert decides when a detection becomes an alert, keeps the alert
// record and hands it to the notifiers: e-mail, and later others.
//
// The alert tells about what detection marked, and detection doesn't mark
// every arrival: it's not an alarm (see docs/deteccao.md). It also never holds
// up the object detector's queue: the rule and the record are in-memory math,
// and sending, which may wait on a mail server, runs on its own goroutine.
//
// The texts of the alert e-mail are in pt-BR until i18n.
package alert

import (
	"context"
	"log/slog"
	"slices"

	"github.com/mhagnumdw/dwnvr/internal/area"
	"github.com/mhagnumdw/dwnvr/internal/config"
)

// Alert is an inference that passed the camera's alert rule, as the notifiers
// get it. What's stored is the `alert` mark on each object line (see
// store.Event); this is what goes out through the log and the notifiers, like
// e-mail.
type Alert struct {
	Camera, CameraName string
	// AtMs is the onset, the time of the scene, not that of the object
	// detector's answer, which comes from seconds to half a minute later.
	AtMs     int64
	FrameMs  int64
	HasFrame bool
	Objects  []Object
	// Areas are the alert's areas, all of them, not only those some object
	// fell in: their outline is drawn on the frame. Empty when the alert
	// applies to the whole image.
	Areas []AlertArea
	// Frame is the inferred JPEG, which the e-mail carries as an attachment.
	// The Dispatcher swaps it for the frame with the boxes and areas drawn
	// before handing it to the notifiers (see frame.go).
	Frame []byte
}

// AlertArea is an alert area as it was at the time: the name and the polygon,
// as a fraction of the frame.
type AlertArea struct {
	Name   string
	Points []area.Point
}

// Object is a mark that passed the rule. Areas are the names of the alert
// areas it fell in, as they were at the time, which is what the log and the
// e-mail show; empty when the alert applies to the whole image.
type Object struct {
	Family string
	Class  string
	Score  float64
	Box    [4]float64
	Areas  []string
}

// Notifier is a way to let someone know: e-mail, and later Telegram, ntfy and
// the like. It keeps no state between sends: this package is what decides
// what becomes an alert, and when.
type Notifier interface {
	// Send delivers an alert, and only returns when the notifier accepted or
	// rejected it, within the ctx deadline.
	Send(ctx context.Context, a Alert) error

	// Name is the notifier's, like "email": it shows up in the log and on
	// screen.
	Name() string
}

// Notifiers is the constructor map: a new notifier is one more line. The
// constructor returns nil when the notifier isn't configured in dwnvr.yaml.
var Notifiers = map[string]func(cfg *config.Config) (Notifier, error){
	NotifierEmail: NewEmail,
}

// BuildNotifiers builds the configured notifiers, in name order. One that
// fails to build is left out and goes to the log: a badly configured e-mail
// must not silence the record, nor the other notifiers.
func BuildNotifiers(cfg *config.Config, log *slog.Logger) []Notifier {
	names := make([]string, 0, len(Notifiers))
	for name := range Notifiers {
		names = append(names, name)
	}
	slices.Sort(names)
	var notifiers []Notifier
	for _, name := range names {
		n, err := Notifiers[name](cfg)
		if err != nil {
			log.Error("alert notifier left out", "notifier", name, "err", err)
			continue
		}
		if n != nil {
			notifiers = append(notifiers, n)
		}
	}
	return notifiers
}
