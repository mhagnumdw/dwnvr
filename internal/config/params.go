package config

import (
	"fmt"

	"github.com/mhagnumdw/dwnvr/internal/detect"
)

// The accepted ranges for each numeric field. They are the single rule for the
// two doors a value comes in through: the API rejects what falls outside
// them, and reading the files at boot swaps it for the default and warns (see
// Warning). The Cameras screen gets them via GET /api/cameras (CameraRanges),
// and the inputs' min/max come from here, with no number repeated in
// Cameras.svelte.
var (
	// QuotaMBRange: a quota of a few MB can't hold even one segment, and the
	// camera would spend its life deleting what it just recorded.
	QuotaMBRange = Range{Min: 100, Unit: "MB"}

	// SegmentSecondsRange: the open segment is what's lost in a power cut, and
	// it's the file the player downloads whole on every seek. Five minutes are
	// already about 75 MB at 2 Mbps; the floor avoids a flood of small files
	// on disk.
	SegmentSecondsRange = Range{Min: 10, Max: 300, Unit: "s"}

	// StallSecondsRange: zero or negative would turn off the watchdog that
	// keeps a camera from silently stopping recording; above one hour, too.
	StallSecondsRange = Range{Min: 1, Max: 3600, Unit: "s"}

	// MaxDaysRange: zero means "no age limit, the quota rules".
	MaxDaysRange = Range{Min: 0, Unit: "dias"}

	// MinFreeMBRange: zero turns off the global disk lock; negative means
	// nothing.
	MinFreeMBRange = Range{Min: 0, Unit: "MB"}

	// sensitivityRange is that of the levels detect knows; the number lives
	// there because each level has a measured threshold.
	sensitivityRange = Range{Min: detect.MinLevel, Max: detect.MaxLevel}

	// LoginAttemptsPerMinuteRange: zero turns off the limit. Each attempt costs
	// a whole password hash (0.77 s of CPU on an Orange Pi Zero 3). 12 per
	// minute, one every 5 s, is already a lot for someone typing a password.
	LoginAttemptsPerMinuteRange = Range{Min: 0, Max: 12, Unit: "por minuto"}

	// SMTPPortRange is the range of TCP ports.
	SMTPPortRange = Range{Min: 1, Max: 65535}
)

// DefaultSMTPPort is the port for each smtp.security, the one providers use:
// 587 is submission with STARTTLS (RFC 6409), 465 implicit TLS (RFC 8314), and
// 25 SMTP without TLS, for a relay on the local network.
var DefaultSMTPPort = map[string]int{"starttls": 587, "tls": 465, "none": 25}

// DefaultLoginAttemptsPerMinute: after the burst, one attempt every 30 s.
// Someone who mistyped waits little, and someone guessing passwords gets about
// 2,900 a day per address, against one every 0.77 s without a limit.
const DefaultLoginAttemptsPerMinute = 2

// MaxAlertSchedules is what the screen's grid generates in the worst case:
// every other hour checked gives 12 schedules a day, and with each day
// different that's 84 a week. The 84, even with all seven days in each, take
// about 4.5 KB of the 16 KB that POST /api/cameras reads, next to the ~3 KB of
// the eight areas.
const MaxAlertSchedules = 84

// DefaultTrustedProxies are loopback and the range Docker uses for compose
// networks. That's where the two HTTPS paths in the docs come from: `tailscale
// serve`, through the network gateway, and the compose Caddy, through the
// container IP. Both replace whatever X-Forwarded-For the client sends
// (checked on 10/13/2026). Loopback covers dwnvr outside Docker, with the
// proxy on the same machine.
var DefaultTrustedProxies = []string{"127.0.0.0/8", "::1/128", "172.16.0.0/12"}

// CameraRanges are the ranges of the camera's numeric fields, by their JSON
// name. It's what the API sends to the screen; a new numeric field with an
// input on screen goes in here.
var CameraRanges = map[string]Range{
	"quotaMB":             QuotaMBRange,
	"segmentSeconds":      SegmentSecondsRange,
	"maxDays":             MaxDaysRange,
	"stallSeconds":        StallSecondsRange,
	"detectSensibilidade": sensitivityRange,
}

// Range is a closed interval of accepted values. Max zero means "no ceiling",
// and is left out of the JSON so the screen's input has no max.
type Range struct {
	Min  int64  `json:"min"`
	Max  int64  `json:"max,omitempty"`
	Unit string `json:"-"`
}

// Contains says whether v is within the range.
func (f Range) Contains(v int64) bool {
	return v >= f.Min && (f.Max == 0 || v <= f.Max)
}

// String describes the range as a recommendation, in the screen's language:
// "de 10 a 300 s", "100 MB ou mais".
func (f Range) String() string {
	unit := ""
	if f.Unit != "" {
		unit = " " + f.Unit
	}
	if f.Max == 0 {
		return fmt.Sprintf("%d%s ou mais", f.Min, unit)
	}
	return fmt.Sprintf("de %d a %d%s", f.Min, f.Max, unit)
}
