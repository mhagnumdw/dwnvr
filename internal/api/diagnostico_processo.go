package api

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// O grupo "Processo do dwnvr" do card "Este servidor": o dwnvr visto por ele
// mesmo, ao lado da máquina inteira. É o que separa "a máquina está sofrendo"
// de "é o dwnvr que faz a máquina sofrer". Como o resto do card, sai de /proc,
// e o campo some quando a fonte não existe.
//
// Fica de fora o ReadMemStats (heap, GCs): ele para o programa inteiro por um
// instante, e a memória que pesa na máquina já está em MemoriaBytes.

type processoInfo struct {
	// CPUMs é o tempo de CPU que o dwnvr gastou desde que subiu, somando
	// usuário e kernel; VivoMs, há quanto tempo ele está no ar. A tela tira a
	// porcentagem da diferença entre duas leituras - CPU de agora, e não a
	// média de dias -, e cai na média desde a subida na primeira leitura. Vai
	// assim, e não já em porcentagem, para o servidor não guardar a leitura
	// anterior de cada tela aberta. Ponteiro porque zero é medida: o dwnvr
	// recém-subido ainda não gastou um tique de CPU.
	CPUMs  *int64 `json:"cpuMs,omitempty"`
	VivoMs int64  `json:"vivoMs"`
	// MemoriaBytes é a memória física ocupada pelo dwnvr (VmRSS). É o número
	// que bate contra o limite do container, e não o memory.current do
	// cgroup: esse soma o cache dos vídeos gravados, que o kernel devolve
	// quando precisa, e numa máquina que grava o dia todo vive perto do teto.
	MemoriaBytes int64 `json:"memoriaBytes,omitempty"`
	// Goroutines não tem teto certo - depende do número de câmeras e de quem
	// assiste. O sinal de vazamento é crescer de uma leitura para outra.
	Goroutines int `json:"goroutines"`
	// ArquivosAbertos conta os descritores abertos (arquivos e conexões), e
	// LimiteDeArquivos é o teto do processo. Bater nele faz falhar ao mesmo
	// tempo a gravação, que abre segmento, e a rede, que abre socket.
	ArquivosAbertos  int   `json:"arquivosAbertos,omitempty"`
	LimiteDeArquivos int64 `json:"limiteDeArquivos,omitempty"`
	// UID e GID com que o dwnvr roda. Errados, o storage e o cameras.json
	// não aceitam escrita; ver DWNVR_UID e DWNVR_GID no docker-compose.yml.
	UID *int `json:"uid,omitempty"`
	GID *int `json:"gid,omitempty"`
	// Go e Arquitetura dizem qual binário roda de fato: "go1.27.1" e
	// "linux/arm64".
	Go          string `json:"go"`
	Arquitetura string `json:"arquitetura"`
}

// ticksPorSegundo é o USER_HZ, a unidade dos tempos do /proc/self/stat. Ele é
// fixo em 100 na ABI do Linux em toda arquitetura em que o dwnvr roda, e o
// sysconf que o leria precisa de C.
const ticksPorSegundo = 100

func coletarProcesso(f fontes, desde time.Time) processoInfo {
	p := processoInfo{
		VivoMs:      time.Since(desde).Milliseconds(),
		Goroutines:  runtime.NumGoroutine(),
		Go:          runtime.Version(),
		Arquitetura: arquitetura(),
	}
	if b, err := os.ReadFile(filepath.Join(f.proc, "self/stat")); err == nil {
		if ms, ok := lerTempoDeCPU(string(b)); ok {
			p.CPUMs = &ms
		}
	}
	if b, err := os.ReadFile(filepath.Join(f.proc, "self/status")); err == nil {
		p.MemoriaBytes, p.UID, p.GID = lerStatus(b)
	}
	if fds, err := os.ReadDir(filepath.Join(f.proc, "self/fd")); err == nil {
		p.ArquivosAbertos = len(fds)
	}
	if b, err := os.ReadFile(filepath.Join(f.proc, "self/limits")); err == nil {
		p.LimiteDeArquivos = lerLimiteDeArquivos(b)
	}
	return p
}

// lerTempoDeCPU soma utime e stime do /proc/self/stat, em ms. O segundo campo
// é o nome do programa entre parênteses, e pode ter espaço e parêntese dentro;
// a contagem dos campos começa depois do ÚLTIMO ")". Ali, utime e stime são o
// 12º e o 13º.
//
//	1234 (dwnvr) S 1 1234 1234 0 -1 4194560 2200 0 0 0 44346 36809 0 0 20 0 15 0 ...
func lerTempoDeCPU(s string) (int64, bool) {
	_, resto, ok := strings.Cut(s[strings.LastIndexByte(s, ')')+1:], " ")
	if !ok {
		return 0, false
	}
	campos := strings.Fields(resto)
	if len(campos) < 13 {
		return 0, false
	}
	utime, err1 := strconv.ParseInt(campos[11], 10, 64)
	stime, err2 := strconv.ParseInt(campos[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return (utime + stime) * 1000 / ticksPorSegundo, true
}

// lerStatus tira do /proc/self/status a memória física (VmRSS, em kB) e o UID
// e o GID reais, que são o primeiro dos quatro números de cada linha.
func lerStatus(b []byte) (rss int64, uid, gid *int) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		chave, resto, ok := strings.Cut(sc.Text(), ":")
		campos := strings.Fields(resto)
		if !ok || len(campos) == 0 {
			continue
		}
		n, err := strconv.ParseInt(campos[0], 10, 64)
		if err != nil {
			continue
		}
		switch chave {
		case "VmRSS":
			rss = n << 10
		case "Uid":
			v := int(n)
			uid = &v
		case "Gid":
			v := int(n)
			gid = &v
		}
	}
	return rss, uid, gid
}

// lerLimiteDeArquivos acha o limite flexível (soft), que é o que vale, na
// linha do /proc/self/limits:
//
//	Max open files            524287               524288               files
//
// "unlimited" e linha ausente dão 0, e o limite some da resposta.
func lerLimiteDeArquivos(b []byte) int64 {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		if resto, ok := strings.CutPrefix(sc.Text(), "Max open files"); ok {
			campos := strings.Fields(resto)
			if len(campos) > 0 {
				n, _ := strconv.ParseInt(campos[0], 10, 64)
				return n
			}
		}
	}
	return 0
}

// arquitetura é o GOOS/GOARCH do binário, com a versão do ARM de 32 bits
// quando houver: "linux/arm/v7" e "linux/arm/v6" rodam em placas diferentes.
func arquitetura() string {
	a := runtime.GOOS + "/" + runtime.GOARCH
	if info, ok := debug.ReadBuildInfo(); ok {
		var aSb161 strings.Builder
		for _, s := range info.Settings {
			if s.Key == "GOARM" && s.Value != "" {
				aSb161.WriteString("/v" + strings.TrimSuffix(strings.TrimSuffix(s.Value, ",softfloat"), ",hardfloat"))
			}
		}
		a += aSb161.String()
	}
	return a
}
