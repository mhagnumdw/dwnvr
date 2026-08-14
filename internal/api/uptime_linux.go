//go:build linux

package api

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// machineUptime devolve há quanto tempo a máquina está ligada.
//
// Dentro do container isto continua sendo o uptime do HOST, e não do
// container: /proc/uptime vem do kernel, que é compartilhado. É justamente o
// que a tela quer dizer - "a máquina reiniciou" é informação de outra ordem
// que "o dwnvr reiniciou", e é a comparação entre os dois que explica o resto
// do diagnóstico.
func machineUptime() (time.Duration, bool) {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, false
	}
	// O arquivo é "<segundos ligado> <segundos ocioso>"; só o primeiro interessa.
	campo, _, _ := strings.Cut(strings.TrimSpace(string(b)), " ")
	s, err := strconv.ParseFloat(campo, 64)
	if err != nil {
		return 0, false
	}
	return time.Duration(s * float64(time.Second)), true
}
