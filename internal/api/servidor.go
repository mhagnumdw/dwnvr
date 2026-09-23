package api

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/logbuf"
)

// O card "Este servidor" do Diagnóstico: a máquina que grava, vista de dentro
// do processo. Complementa o /api/health, que responde "as câmeras estão
// gravando?", com "a máquina tem fôlego para gravar?".
//
// É um endpoint à parte, e não mais campos do /api/health, porque custa mais: um
// teste de escrita com fsync no storage e uma ida ao go2rtc. O /api/health é
// relido em ciclo por quem só veio ver as câmeras; este só roda com o card
// aberto.
//
// Tudo sai de /proc e /sys, sem exec: a imagem é FROM scratch e não há binário
// nenhum para chamar. Cada campo é opcional e some quando a fonte não existe -
// kernel sem PSI, sistema sem cgroup v2, fora do Linux.

// fontes são as raízes de /proc e /sys, trocáveis nos testes.
type fontes struct{ proc, sys string }

var fontesDoSistema = fontes{proc: "/proc", sys: "/sys"}

type servidorInfo struct {
	Maquina maquinaInfo        `json:"maquina"`
	Memoria *memoriaInfo       `json:"memoria,omitempty"`
	Pressao map[string]pressao `json:"pressao,omitempty"`
	Storage storageInfo        `json:"storage"`
	Go2RTC  go2rtcInfo         `json:"go2rtc"`
	Log     *logInfo           `json:"log,omitempty"`
}

type maquinaInfo struct {
	Nucleos int `json:"nucleos"`
	// Carga é o load average de 1, 5 e 15 minutos.
	Carga    []float64 `json:"carga,omitempty"`
	Sensores []sensor  `json:"sensores,omitempty"`
	// MHz é a maior frequência atual entre os núcleos, e MHzMax o teto do
	// hardware. Abaixo do teto com a máquina quente é o próprio kernel
	// segurando a CPU para esfriar.
	MHz      int    `json:"mhz,omitempty"`
	MHzMax   int    `json:"mhzMax,omitempty"`
	Governor string `json:"governor,omitempty"`
}

type sensor struct {
	Nome    string  `json:"nome"`
	Celsius float64 `json:"celsius"`
}

type memoriaInfo struct {
	TotalBytes      int64 `json:"totalBytes"`
	DisponivelBytes int64 `json:"disponivelBytes"`
	SwapTotalBytes  int64 `json:"swapTotalBytes"`
	SwapUsadaBytes  int64 `json:"swapUsadaBytes"`
	// LimiteBytes é o teto de memória do container (cgroup). Some sem limite.
	LimiteBytes int64 `json:"limiteBytes,omitempty"`
	// OOMKills conta os processos que o kernel matou por falta de memória desde
	// que a MÁQUINA ligou, e não só no container: quando a vítima é o próprio
	// dwnvr, o Docker sobe um container novo e o contador do cgroup antigo vai
	// embora com ele. O da máquina sobrevive, e é o que explica um "dwnvr no ar
	// há 5 min" numa máquina ligada há dias.
	OOMKills *int64 `json:"oomKills,omitempty"`
}

// pressao é uma linha do PSI (/proc/pressure/*): a fração do tempo, em %, em
// que alguma tarefa (some) ou todas (full) ficaram paradas esperando o recurso.
type pressao struct {
	Some *medias `json:"some,omitempty"`
	Full *medias `json:"full,omitempty"`
}

type medias struct {
	Avg10  float64 `json:"avg10"`
	Avg60  float64 `json:"avg60"`
	Avg300 float64 `json:"avg300"`
}

type storageInfo struct {
	Caminho string `json:"caminho"`
	// Tipo, Origem e Opcoes vêm da montagem que contém o caminho: "ext4",
	// "/dev/sda1", "rw,noatime". Somem se o mountinfo não for legível.
	Tipo           string  `json:"tipo,omitempty"`
	Origem         string  `json:"origem,omitempty"`
	Opcoes         string  `json:"opcoes,omitempty"`
	SomenteLeitura bool    `json:"somenteLeitura"`
	Escrita        escrita `json:"escrita"`
}

type escrita struct {
	OK   bool   `json:"ok"`
	Ms   int64  `json:"ms"`
	Erro string `json:"erro,omitempty"`
}

type go2rtcInfo struct {
	OK     bool   `json:"ok"`
	Ms     int64  `json:"ms"`
	Versao string `json:"versao,omitempty"`
	Erro   string `json:"erro,omitempty"`
}

type logInfo struct {
	// Total é quantos avisos e erros houve desde que o dwnvr subiu; Linhas, só
	// os mais recentes.
	Total  int64          `json:"total"`
	Linhas []logbuf.Linha `json:"linhas"`
}

// recentes é o que o handler de log do main.go oferece. Vai por interface, e
// não por mais um parâmetro de New, porque o logger já chega aqui: é só
// perguntar a ele.
type recentes interface {
	Recentes() ([]logbuf.Linha, int64)
}

func (s *Server) handleServidor(w http.ResponseWriter, r *http.Request) {
	f := fontesDoSistema
	resp := servidorInfo{
		Maquina: coletarMaquina(f),
		Memoria: coletarMemoria(f),
		Pressao: coletarPressao(f),
		Storage: coletarMontagem(f, s.cfg.Storage.Root),
	}

	// As duas sondas esperam por fora - disco e rede - e juntas podem somar
	// alguns segundos num servidor ruim. Em paralelo, a tela espera a pior das
	// duas, não a soma.
	var wg sync.WaitGroup
	wg.Go(func() { resp.Storage.Escrita = s.testarEscrita() })
	wg.Go(func() { resp.Go2RTC = s.sondarGo2RTC(r.Context()) })
	wg.Wait()

	if h, ok := s.log.Handler().(recentes); ok {
		linhas, total := h.Recentes()
		resp.Log = &logInfo{Total: total, Linhas: linhas}
	}

	writeJSON(w, resp)
}

// ---- máquina ---------------------------------------------------------------

func coletarMaquina(f fontes) maquinaInfo {
	m := maquinaInfo{Nucleos: runtime.NumCPU()}
	if b, err := os.ReadFile(filepath.Join(f.proc, "loadavg")); err == nil {
		m.Carga = lerCarga(string(b))
	}
	m.Sensores = lerSensores(f.sys)

	cpus, _ := filepath.Glob(filepath.Join(f.sys, "devices/system/cpu/cpu[0-9]*/cpufreq"))
	for _, d := range cpus {
		if khz := lerInt(filepath.Join(d, "scaling_cur_freq")); khz/1000 > int64(m.MHz) {
			m.MHz = int(khz / 1000)
		}
		if khz := lerInt(filepath.Join(d, "cpuinfo_max_freq")); khz/1000 > int64(m.MHzMax) {
			m.MHzMax = int(khz / 1000)
		}
		if m.Governor == "" {
			m.Governor = lerTexto(filepath.Join(d, "scaling_governor"))
		}
	}
	return m
}

// lerCarga lê "0.52 0.40 0.31 1/123 4567".
func lerCarga(s string) []float64 {
	campos := strings.Fields(s)
	if len(campos) < 3 {
		return nil
	}
	out := make([]float64, 3)
	for i := range out {
		v, err := strconv.ParseFloat(campos[i], 64)
		if err != nil {
			return nil
		}
		out[i] = v
	}
	return out
}

// lerSensores lê as zonas térmicas do kernel. Numa placa ARM elas têm nomes
// como "cpu_thermal" e "gpu_thermal"; num PC, "x86_pkg_temp" e "acpitz". Zona
// que não responde (algumas devolvem EINVAL com o sensor desligado) fica de
// fora.
//
// O nome não é único: um PC pode ter duas zonas "acpitz". Nome repetido leva o
// número da zona ("acpitz 0", "acpitz 1"), senão as duas linhas da tela e do
// "copiar" ficam indistinguíveis.
func lerSensores(sys string) []sensor {
	zonas, _ := filepath.Glob(filepath.Join(sys, "class/thermal/thermal_zone*"))
	var out []sensor
	var numeros []string
	vezes := map[string]int{}
	for _, z := range zonas {
		b, err := os.ReadFile(filepath.Join(z, "temp"))
		if err != nil {
			continue
		}
		mili, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if err != nil || mili <= 0 {
			continue
		}
		nome := lerTexto(filepath.Join(z, "type"))
		if nome == "" {
			nome = filepath.Base(z)
		}
		out = append(out, sensor{Nome: nome, Celsius: float64(mili/100) / 10})
		numeros = append(numeros, strings.TrimPrefix(filepath.Base(z), "thermal_zone"))
		vezes[nome]++
	}
	for i := range out {
		if vezes[out[i].Nome] > 1 {
			out[i].Nome += " " + numeros[i]
		}
	}
	// Mais quente primeiro: é ele que a tela destaca.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Celsius > out[j].Celsius })
	return out
}

// ---- memória ---------------------------------------------------------------

func coletarMemoria(f fontes) *memoriaInfo {
	b, err := os.ReadFile(filepath.Join(f.proc, "meminfo"))
	if err != nil {
		return nil
	}
	kb := lerMeminfo(b)
	m := &memoriaInfo{
		TotalBytes:      kb["MemTotal"] << 10,
		DisponivelBytes: kb["MemAvailable"] << 10,
		SwapTotalBytes:  kb["SwapTotal"] << 10,
		SwapUsadaBytes:  (kb["SwapTotal"] - kb["SwapFree"]) << 10,
		LimiteBytes:     limiteDoCgroup(f),
	}
	if b, err := os.ReadFile(filepath.Join(f.proc, "vmstat")); err == nil {
		if n, ok := lerCampo(b, "oom_kill"); ok {
			m.OOMKills = &n
		}
	}
	return m
}

// lerMeminfo lê "MemTotal:  1012345 kB" em chave -> kB.
func lerMeminfo(b []byte) map[string]int64 {
	out := map[string]int64{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		chave, resto, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		campos := strings.Fields(resto)
		if len(campos) == 0 {
			continue
		}
		if v, err := strconv.ParseInt(campos[0], 10, 64); err == nil {
			out[chave] = v
		}
	}
	return out
}

// lerCampo acha "<nome> <número>" num arquivo de uma chave por linha, como o
// /proc/vmstat.
func lerCampo(b []byte, nome string) (int64, bool) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		campos := strings.Fields(sc.Text())
		if len(campos) == 2 && campos[0] == nome {
			v, err := strconv.ParseInt(campos[1], 10, 64)
			return v, err == nil
		}
	}
	return 0, false
}

// limiteDoCgroup devolve o teto de memória do cgroup deste processo, ou 0 sem
// teto. No cgroup v2 o caminho vem do /proc/self/cgroup ("0::/caminho"); dentro
// de um container com namespace de cgroup próprio ele é "/", e o arquivo está
// na raiz de /sys/fs/cgroup. O v1 fica como plano B para kernel antigo.
func limiteDoCgroup(f fontes) int64 {
	if b, err := os.ReadFile(filepath.Join(f.proc, "self/cgroup")); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			caminho, ok := strings.CutPrefix(l, "0::")
			if !ok {
				continue
			}
			if v := lerTexto(filepath.Join(f.sys, "fs/cgroup", caminho, "memory.max")); v != "" && v != "max" {
				n, _ := strconv.ParseInt(v, 10, 64)
				return n
			}
		}
	}
	// No v1, "sem limite" é um número enorme (PAGE_COUNTER_MAX em bytes), e
	// não uma palavra. Acima de 1 PiB, trata-se como sem limite.
	if n := lerInt(filepath.Join(f.sys, "fs/cgroup/memory/memory.limit_in_bytes")); n > 0 && n < 1<<50 {
		return n
	}
	return 0
}

// ---- pressão ---------------------------------------------------------------

// coletarPressao lê o PSI de CPU, memória e disco. Kernel sem PSI (anterior ao
// 4.20, ou compilado sem) não tem /proc/pressure, e o campo inteiro some.
func coletarPressao(f fontes) map[string]pressao {
	out := map[string]pressao{}
	for arquivo, chave := range map[string]string{"cpu": "cpu", "memory": "memoria", "io": "io"} {
		if b, err := os.ReadFile(filepath.Join(f.proc, "pressure", arquivo)); err == nil {
			out[chave] = lerPressao(string(b))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// lerPressao lê as duas linhas do PSI:
//
//	some avg10=1.53 avg60=0.87 avg300=0.26 total=123456
//	full avg10=0.00 avg60=0.00 avg300=0.00 total=0
func lerPressao(s string) pressao {
	var p pressao
	for _, l := range strings.Split(s, "\n") {
		campos := strings.Fields(l)
		if len(campos) < 4 {
			continue
		}
		m := &medias{}
		for _, c := range campos[1:] {
			k, v, _ := strings.Cut(c, "=")
			n, _ := strconv.ParseFloat(v, 64)
			switch k {
			case "avg10":
				m.Avg10 = n
			case "avg60":
				m.Avg60 = n
			case "avg300":
				m.Avg300 = n
			}
		}
		switch campos[0] {
		case "some":
			p.Some = m
		case "full":
			p.Full = m
		}
	}
	return p
}

// ---- storage ---------------------------------------------------------------

// coletarMontagem descobre em que montagem o storage está. Sistema de
// arquivos e dispositivo explicam muito: exfat e ntfs rodam via FUSE, lentos e
// caros em CPU; /dev/mmcblk* é o cartão SD, que é o que costuma falhar. E um
// cartão com defeito é remontado somente leitura pelo kernel sem avisar
// ninguém - o dwnvr simplesmente para de conseguir gravar.
func coletarMontagem(f fontes, root string) storageInfo {
	info := storageInfo{Caminho: root}
	caminho, err := filepath.Abs(root)
	if err != nil {
		return info
	}
	if real, err := filepath.EvalSymlinks(caminho); err == nil {
		caminho = real
	}
	b, err := os.ReadFile(filepath.Join(f.proc, "self/mountinfo"))
	if err != nil {
		return info
	}
	if m, ok := montagemDe(b, caminho); ok {
		info.Tipo, info.Origem, info.Opcoes = m.tipo, m.origem, m.opcoes
		info.SomenteLeitura = temOpcao(m.opcoes, "ro") || temOpcao(m.superOpcoes, "ro")
	}
	return info
}

type montagem struct {
	ponto, opcoes, tipo, origem, superOpcoes string
}

// montagemDe acha no mountinfo a montagem mais específica que contém o
// caminho. O formato de cada linha é
//
//	36 35 98:0 /mnt1 /mnt/parent rw,noatime master:1 - ext3 /dev/root rw,errors=continue
//
// com um número variável de campos opcionais antes do "-".
func montagemDe(b []byte, caminho string) (montagem, bool) {
	var melhor montagem
	achou := false
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		campos := strings.Fields(sc.Text())
		sep := -1
		for i, c := range campos {
			if c == "-" {
				sep = i
				break
			}
		}
		if sep < 6 || len(campos) < sep+3 {
			continue
		}
		m := montagem{
			ponto:  desescapar(campos[4]),
			opcoes: campos[5],
			tipo:   campos[sep+1],
			origem: desescapar(campos[sep+2]),
		}
		if len(campos) > sep+3 {
			m.superOpcoes = campos[sep+3]
		}
		if !contem(m.ponto, caminho) {
			continue
		}
		// >= e não >: montagens empilhadas no mesmo ponto aparecem na ordem
		// em que foram feitas, e a última é a que vale.
		if !achou || len(m.ponto) >= len(melhor.ponto) {
			melhor, achou = m, true
		}
	}
	return melhor, achou
}

func contem(ponto, caminho string) bool {
	return ponto == "/" || caminho == ponto || strings.HasPrefix(caminho, ponto+"/")
}

func temOpcao(opcoes, nome string) bool {
	for _, o := range strings.Split(opcoes, ",") {
		if o == nome {
			return true
		}
	}
	return false
}

// desescapar desfaz o \040 (espaço) e companhia com que o kernel escreve
// caminhos no mountinfo.
func desescapar(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Quanto o teste de escrita espera antes de desistir e dizer que o disco não
// respondeu. Um fsync de 4 KB leva milissegundos até num cartão SD modesto;
// passar disso é disco doente ou atolado.
const prazoEscrita = 5 * time.Second

// testarEscrita prova que o storage aceita escrita AGORA: cria um arquivo,
// grava 4 KB, força para o disco e apaga. O tempo do fsync é o número que
// importa - um cartão SD no fim da vida ainda aceita escrita, só que em
// segundos.
//
// Um disco travado prende o fsync por tempo indeterminado, e nada interrompe
// um fsync. Por isso o teste roda à parte, a resposta desiste no prazo, e um
// teste ainda preso faz os pedidos seguintes responderem isso mesmo em vez de
// empilhar mais arquivos e mais goroutines no disco que já não responde.
func (s *Server) testarEscrita() escrita {
	if !s.escrevendo.CompareAndSwap(false, true) {
		return escrita{Erro: "o teste anterior ainda não terminou: o disco não está respondendo"}
	}
	pronto := make(chan escrita, 1)
	go func() {
		defer s.escrevendo.Store(false)
		inicio := time.Now()
		err := escreverTeste(s.cfg.Storage.Root)
		ms := time.Since(inicio).Milliseconds()
		if err != nil {
			s.log.Warn("teste de escrita no storage falhou", "erro", err)
			pronto <- escrita{Ms: ms, Erro: motivoDaEscrita(err)}
			return
		}
		pronto <- escrita{OK: true, Ms: ms}
	}()
	select {
	case e := <-pronto:
		return e
	case <-time.After(prazoEscrita):
		return escrita{Ms: prazoEscrita.Milliseconds(), Erro: "o disco não respondeu em " + prazoEscrita.String()}
	}
}

func escreverTeste(root string) (err error) {
	// Nome com ponto na frente: some do ls comum e nunca parece câmera, e o
	// store.Orphans só olha diretórios.
	f, err := os.CreateTemp(root, ".dwnvr-teste-*")
	if err != nil {
		return err
	}
	defer func() {
		if rerr := os.Remove(f.Name()); err == nil {
			err = rerr
		}
	}()
	if _, err = f.Write(make([]byte, 4096)); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// motivoDaEscrita traduz os erros que têm causa conhecida. O resto vai
// genérico para a tela, com o detalhe no log - como o fail() faz com erro de
// filesystem.
func motivoDaEscrita(err error) string {
	switch {
	case errors.Is(err, syscall.EROFS):
		return "o sistema de arquivos está somente leitura"
	case errors.Is(err, os.ErrPermission):
		return "sem permissão de escrita (confira DWNVR_UID e DWNVR_GID)"
	case errors.Is(err, syscall.ENOSPC):
		return "disco cheio"
	case errors.Is(err, os.ErrNotExist):
		return "o diretório do storage não existe"
	}
	return "falhou (detalhe no log do dwnvr)"
}

// ---- go2rtc ----------------------------------------------------------------

func (s *Server) sondarGo2RTC(ctx context.Context) go2rtcInfo {
	if s.client == nil {
		return go2rtcInfo{Erro: "não configurado"}
	}
	inicio := time.Now()
	v, err := s.client.Versao(ctx)
	info := go2rtcInfo{Ms: time.Since(inicio).Milliseconds()}
	if err != nil {
		// Mesma exposição do go2rtcError do /api/cameras: o erro de rede é
		// o diagnóstico, e a URL nele não traz senha (o cliente usa Basic
		// Auth por cabeçalho).
		info.Erro = err.Error()
		return info
	}
	info.OK, info.Versao = true, v
	return info
}

// ---- utilidades ------------------------------------------------------------

func lerTexto(caminho string) string {
	b, err := os.ReadFile(caminho)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func lerInt(caminho string) int64 {
	n, _ := strconv.ParseInt(lerTexto(caminho), 10, 64)
	return n
}
