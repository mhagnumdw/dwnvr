package ratelimit

import "time"

const (
	// LoginBurst is how many login attempts in a row go through before the
	// rate kicks in. Someone who mistypes a password usually gets it wrong two
	// or three times; five covers that comfortably, so nobody who just made a
	// typo ever sees the wait. After the burst, attempts follow the
	// server.loginAttemptsPerMinute rate, and one slot comes back for every
	// interval without an attempt.
	LoginBurst = 5

	// SweepEvery is the interval between the sweeps that drop clients that
	// have recovered their whole burst. Without it, every address that ever
	// tried would stay in the map forever.
	SweepEvery = time.Minute
)
