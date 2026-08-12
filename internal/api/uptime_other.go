//go:build !linux

package api

import "time"

// O dwnvr roda em Linux; fora dele não há /proc/uptime. O stub existe para que
// `go build ./...` e o editor funcionem no desenvolvimento — a tela simplesmente
// omite o uptime da máquina quando ele não é conhecido.
func machineUptime() (time.Duration, bool) { return 0, false }
