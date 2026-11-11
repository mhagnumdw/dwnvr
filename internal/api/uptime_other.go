//go:build !linux

package api

import "time"

// dwnvr runs on Linux; outside it there is no /proc/uptime. The stub exists so
// that `go build ./...` and the editor work during development - the screen
// simply omits the machine uptime when it is not known.
func machineUptime() (time.Duration, bool) { return 0, false }
