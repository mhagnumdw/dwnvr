//go:build linux

package api

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// machineUptime returns how long the machine has been on.
//
// Inside the container this is still the HOST uptime, and not the container's:
// /proc/uptime comes from the kernel, which is shared. That is exactly what the
// screen means - "the machine rebooted" is information of a different order
// than "dwnvr restarted", and it is the comparison between the two that
// explains the rest of the diagnosis.
func machineUptime() (time.Duration, bool) {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, false
	}
	// The file is "<seconds up> <seconds idle>"; only the first one matters.
	field, _, _ := strings.Cut(strings.TrimSpace(string(b)), " ")
	s, err := strconv.ParseFloat(field, 64)
	if err != nil {
		return 0, false
	}
	return time.Duration(s * float64(time.Second)), true
}
