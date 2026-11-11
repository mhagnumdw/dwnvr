package alert

import (
	"time"

	"github.com/mhagnumdw/dwnvr/internal/detect"
)

// This file holds every number in the package, each with its reason next to
// it.

// QueuedAlerts is how many alerts wait their turn to go out through the
// notifiers. Each one carries the JPEG of the inferred frame, a few dozen KB at
// 480 px, and 32 stay under 2 MB. With a stuck notifier, each send takes up to
// SendTimeout, and the send goroutine handles 120 an hour. On the test
// installation, `pessoa` on the 9 cameras, on the whole image, gave ~629
// alerts a day (measured on 10/22/2026), about 26 an hour: the queue only
// fills in a burst, and what doesn't fit is dropped and counted.
const QueuedAlerts = 32

// SendTimeout is how long a send may take before it counts as a failure. An
// e-mail with the frame attached takes a few seconds even on a slow home
// uplink; 30 s separates slow from stuck, without a stuck notifier holding up
// the others for too long.
const SendTimeout = 30 * time.Second

// BoxPaddingPx is how much the box drawn on the alert frame grows on each side
// beyond the object detector's box, the same CAIXA_FOLGA_PX as the screen
// (web/src/lib/caixas.ts): the stroke sits inside the box and, without
// padding, would cover the edge of the object.
const BoxPaddingPx = 5

// BoxStrokePx is the box stroke thickness, the screen's.
const BoxStrokePx = 2

// AreaStrokePx is the area outline thickness. The screen uses 1.5 px; on the
// frame, which only has whole pixels, 1 px vanishes in the JPEG compression.
const AreaStrokePx = 2

// AreaDashOnPx and AreaDashOffPx are the dash pattern of the area outline,
// the screen's setLineDash([6, 4]) (web/src/lib/area.ts): the dashes are what
// say "area", and not object.
const (
	AreaDashOnPx  = 6
	AreaDashOffPx = 4
)

// FrameQuality is the JPEG quality of the drawn frame: the same as the
// inferred frame. Above it no lost detail comes back; measured on 10/27/2026,
// on 4 real 480×270 frames, 85 gave ~25% more (from 24-43 KB to 30-54 KB) with
// no visible difference in the stroke or the image, zoomed in 4 times.
const FrameQuality = detect.FrameQuality

// AlertsPerHour and AlertBurst are the notifiers' send cap, for the whole
// installation: after AlertBurst in a row, one alert goes out every
// hour/AlertsPerHour (4 min), and what comes before that is dropped and
// counted. A rate, not "at most N per hour": after a burst, someone arriving
// half an hour later still triggers an e-mail.
//
// The cap protects the e-mail account, because the provider blocks whoever
// sends too much. A regular Gmail account sends up to 500 messages a day,
// counted per message and not per recipient
// (support.google.com/mail/answer/22839), and the same account sends the
// set-password link. The worst day with these numbers is 20 + 15 × 24 = 380,
// leaving ~120 for the rest. With 20 an hour, the worst day would be 500,
// right at the limit. The math holds for dwnvr running all day: the cap lives
// in memory, and every restart gives the whole burst back.
const (
	AlertsPerHour = 15
	AlertBurst    = 20
)

// DropWarningLasts is how long the warning about alerts that didn't go out
// stays on screen after the last drop. A drop is an event, not a state: going
// away on the next send, it would leave within seconds, and the bell, which
// rereads every 30 s, might not even light up. A drop after that starts
// another episode, with the count from zero.
const DropWarningLasts = time.Hour
