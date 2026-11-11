package api

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/alert"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/logbuf"
)

// O card "Este servidor" do Diagnóstico: a máquina que grava, vista de dentro
// do processo. Complementa o /api/health, que responde "as câmeras estão
// gravando?", com "a máquina tem fôlego para gravar?".
//
// É um endpoint à parte, e não mais campos do /api/health, porque custa mais: um
// teste de escrita com fsync no storage e uma ida ao go2rtc e ao detector de
// objetos. O /api/health é relido em ciclo por quem só veio ver as câmeras;
// este só roda com o card aberto.
//
// Tudo sai de /proc e /sys, sem exec: a imagem é FROM scratch e não há binário
// nenhum para chamar. Cada campo é opcional e some quando a fonte não existe -
// kernel sem PSI, sistema sem cgroup v2, fora do Linux.

// fontes são as raízes de /proc e /sys, trocáveis nos testes.
type fontes struct{ proc, sys string }

var systemSources = fontes{proc: "/proc", sys: "/sys"}

type serverInfo struct {
	CollectedAt collection          `json:"coletadoEm"`
	Machine     machineInfo         `json:"maquina"`
	Process     processInfo         `json:"processo"`
	Memory      *memoryInfo         `json:"memoria,omitempty"`
	Pressure    map[string]pressure `json:"pressao,omitempty"`
	Storage     storageInfo         `json:"storage"`
	Go2RTC      go2rtcInfo          `json:"go2rtc"`
	// Detector some sem `detector.url`: quem não usa a detecção de objetos
	// não paga nem a ida até ele.
	Detector *detectorInfo `json:"detector,omitempty"`
	// SMTP some sem a seção smtp. Vai sem sonda: conferir o servidor de
	// e-mail é mandar um, e isso é o botão de teste.
	SMTP *smtpInfo `json:"smtp,omitempty"`
	// Alertas são os números de cada meio do alerta desde que o dwnvr subiu.
	// Some sem meio, ou antes de alguma câmera ligar o alerta.
	Alerts []alertNotifierInfo `json:"alertas,omitempty"`
	Log    *logInfo            `json:"log,omitempty"`
}

// meioDoAlertaInfo é o NotifierState da Central. Ultimo some antes do primeiro
// envio, e Erro, com o último envio tendo dado certo.
type alertNotifierInfo struct {
	Nome     string     `json:"nome"`
	Sent     int64      `json:"enviados"`
	Failures int64      `json:"falhas"`
	Ultimo   *time.Time `json:"ultimo,omitempty"`
	Error    string     `json:"erro,omitempty"`
}

// coleta é o instante da leitura no relógio do servidor, com o fuso dele. Vai
// com o fuso, e não só o instante, pelo mesmo motivo do `clock` do
// /api/health: a pergunta é a hora de LÁ, e convertida para o fuso do
// navegador ela esconderia um servidor com fuso ou NTP errado.
type collection struct {
	Em            string `json:"em"`
	Abbreviation  string `json:"sigla"`
	OffsetSeconds int    `json:"offsetSeconds"`
	// Zona é o nome IANA ("America/Sao_Paulo"), quando o servidor sabe.
	Zone string `json:"zona,omitempty"`
}

type machineInfo struct {
	// Placa vem do device tree, que só existe em ARM ("OrangePi Zero3");
	// Processador, do /proc/cpuinfo, que só traz nome no x86. Um ou outro
	// costuma faltar, e o que falta some.
	Board     string `json:"placa,omitempty"`
	Processor string `json:"processador,omitempty"`
	// Kernel é o do host mesmo dentro do container, que não tem kernel
	// próprio.
	Kernel string `json:"kernel,omitempty"`
	Cores  int    `json:"nucleos"`
	// NucleosLiberados é o teto de CPU do container (cgroup), em núcleos: 1.5
	// é um núcleo e meio. Some sem teto.
	AllowedCores float64 `json:"nucleosLiberados,omitempty"`
	// Carga é o load average de 1, 5 e 15 minutos.
	Load     []float64 `json:"carga,omitempty"`
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

type memoryInfo struct {
	TotalBytes     int64 `json:"totalBytes"`
	AvailableBytes int64 `json:"disponivelBytes"`
	SwapTotalBytes int64 `json:"swapTotalBytes"`
	SwapUsedBytes  int64 `json:"swapUsadaBytes"`
	// LimiteBytes é o teto de memória do container (cgroup). Some sem limite.
	LimitBytes int64 `json:"limiteBytes,omitempty"`
	// OOMKills conta os processos que o kernel matou por falta de memória desde
	// que a MÁQUINA ligou, e não só no container: quando a vítima é o próprio
	// dwnvr, o Docker sobe um container novo e o contador do cgroup antigo vai
	// embora com ele. O da máquina sobrevive, e é o que explica um "dwnvr no ar
	// há 5 min" numa máquina ligada há dias.
	OOMKills *int64 `json:"oomKills,omitempty"`
}

// pressao é uma linha do PSI (/proc/pressure/*): a fração do tempo, em %, em
// que alguma tarefa (some) ou todas (full) ficaram paradas esperando o recurso.
type pressure struct {
	Some *medias `json:"some,omitempty"`
	Full *medias `json:"full,omitempty"`
}

type medias struct {
	Avg10  float64 `json:"avg10"`
	Avg60  float64 `json:"avg60"`
	Avg300 float64 `json:"avg300"`
}

type storageInfo struct {
	Path string `json:"caminho"`
	// Tipo, Origem e Opcoes vêm da montagem que contém o caminho: "ext4",
	// "/dev/sda1", "rw,noatime". Somem se o mountinfo não for legível.
	Type     string `json:"tipo,omitempty"`
	Origin   string `json:"origem,omitempty"`
	Options  string `json:"opcoes,omitempty"`
	ReadOnly bool   `json:"somenteLeitura"`
	Write    write  `json:"escrita"`
}

type write struct {
	OK    bool   `json:"ok"`
	Ms    int64  `json:"ms"`
	Error string `json:"erro,omitempty"`
}

type go2rtcInfo struct {
	OK      bool   `json:"ok"`
	Ms      int64  `json:"ms"`
	Version string `json:"versao,omitempty"`
	Error   string `json:"erro,omitempty"`
	// URL é o endereço que o dwnvr usa de fato, sem usuário e senha.
	URL string `json:"url,omitempty"`
}

type detectorInfo struct {
	OK      bool   `json:"ok"`
	Ms      int64  `json:"ms"`
	Model   string `json:"modelo,omitempty"`
	Entry   string `json:"entrada,omitempty"`
	Threads int    `json:"threads,omitempty"`
	Error   string `json:"erro,omitempty"`
	URL     string `json:"url"`
}

type logInfo struct {
	// Total é quantos avisos e erros houve desde que o dwnvr subiu; Linhas, só
	// os mais recentes.
	Total int64         `json:"total"`
	Lines []logbuf.Line `json:"linhas"`
}

// recentes é o que o handler de log do main.go oferece. Vai por interface, e
// não por mais um parâmetro de New, porque o logger já chega aqui: é só
// perguntar a ele.
type recentes interface {
	Recent() ([]logbuf.Line, int64)
}

func (s *Server) handleServerDiagnostics(w http.ResponseWriter, r *http.Request) {
	f := systemSources
	now := time.Now()
	sigla, offset := now.Zone()
	resp := serverInfo{
		CollectedAt: collection{Em: now.Format(time.RFC3339), Abbreviation: sigla, OffsetSeconds: offset, Zone: localZone()},
		Machine:     collectMachine(f),
		Process:     collectProcess(f, s.startedAt),
		Memory:      collectMemory(f),
		Pressure:    collectPressure(f),
		Storage:     collectMount(f, s.cfg.Storage.Root),
		SMTP:        smtpInDiagnostics(s.cfg.SMTP),
		Alerts:      notifiersInDiagnostics(s.mgr.Alerts()),
	}

	// As sondas esperam por fora - disco e rede - e juntas podem somar alguns
	// segundos num servidor ruim. Em paralelo, a tela espera a pior delas, não
	// a soma.
	var wg sync.WaitGroup
	wg.Go(func() { resp.Storage.Write = s.testWrite() })
	wg.Go(func() { resp.Go2RTC = s.probeGo2RTC(r.Context()) })
	if s.cfg.Detector.URL != "" {
		wg.Go(func() { resp.Detector = s.probeDetector(r.Context()) })
	}
	wg.Wait()

	if h, ok := s.log.Handler().(recentes); ok {
		lines, total := h.Recent()
		resp.Log = &logInfo{Total: total, Lines: lines}
	}

	writeJSON(w, resp)
}

// meiosNoDiagnostico é nil sem Central, que só nasce com a primeira câmera que
// liga o alerta, e sem meio.
func notifiersInDiagnostics(c *alert.Dispatcher) []alertNotifierInfo {
	if c == nil {
		return nil
	}
	var out []alertNotifierInfo
	for _, m := range c.NotifierStates() {
		info := alertNotifierInfo{Nome: m.Name, Sent: m.Sent, Failures: m.Failures, Error: m.Err}
		if !m.Last.IsZero() {
			info.Ultimo = &m.Last
		}
		out = append(out, info)
	}
	return out
}

// ---- máquina ---------------------------------------------------------------

func collectMachine(f fontes) machineInfo {
	m := machineInfo{
		// O device tree termina o texto com um NUL, que o TrimSpace não tira.
		Board:        strings.TrimRight(readText(filepath.Join(f.proc, "device-tree/model")), "\x00"),
		Processor:    readProcessor(f.proc),
		Kernel:       readText(filepath.Join(f.proc, "sys/kernel/osrelease")),
		Cores:        runtime.NumCPU(),
		AllowedCores: cgroupCores(f),
	}
	if b, err := os.ReadFile(filepath.Join(f.proc, "loadavg")); err == nil {
		m.Load = readLoad(string(b))
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
			m.Governor = readText(filepath.Join(d, "scaling_governor"))
		}
	}
	return m
}

// lerProcessador devolve o "model name" do primeiro núcleo. No ARM de 64 bits
// o campo não existe - só códigos como "CPU part : 0xd03" -, e aí quem
// identifica a máquina é a placa.
func readProcessor(proc string) string {
	b, err := os.ReadFile(filepath.Join(proc, "cpuinfo"))
	if err != nil {
		return ""
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		key, valor, ok := strings.Cut(sc.Text(), ":")
		if ok && strings.TrimSpace(key) == "model name" {
			return strings.TrimSpace(valor)
		}
	}
	return ""
}

// lerCarga lê "0.52 0.40 0.31 1/123 4567".
func readLoad(s string) []float64 {
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
	zones, _ := filepath.Glob(filepath.Join(sys, "class/thermal/thermal_zone*"))
	var out []sensor
	var numbers []string
	times := map[string]int{}
	for _, z := range zones {
		b, err := os.ReadFile(filepath.Join(z, "temp"))
		if err != nil {
			continue
		}
		milli, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if err != nil || milli <= 0 {
			continue
		}
		nome := readText(filepath.Join(z, "type"))
		if nome == "" {
			nome = filepath.Base(z)
		}
		out = append(out, sensor{Nome: nome, Celsius: float64(milli/100) / 10})
		numbers = append(numbers, strings.TrimPrefix(filepath.Base(z), "thermal_zone"))
		times[nome]++
	}
	for i := range out {
		if times[out[i].Nome] > 1 {
			out[i].Nome += " " + numbers[i]
		}
	}
	// Mais quente primeiro: é ele que a tela destaca.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Celsius > out[j].Celsius })
	return out
}

// ---- memória ---------------------------------------------------------------

func collectMemory(f fontes) *memoryInfo {
	b, err := os.ReadFile(filepath.Join(f.proc, "meminfo"))
	if err != nil {
		return nil
	}
	kb := readMeminfo(b)
	m := &memoryInfo{
		TotalBytes:     kb["MemTotal"] << 10,
		AvailableBytes: kb["MemAvailable"] << 10,
		SwapTotalBytes: kb["SwapTotal"] << 10,
		SwapUsedBytes:  (kb["SwapTotal"] - kb["SwapFree"]) << 10,
		LimitBytes:     cgroupLimit(f),
	}
	if b, err := os.ReadFile(filepath.Join(f.proc, "vmstat")); err == nil {
		if n, ok := readField(b, "oom_kill"); ok {
			m.OOMKills = &n
		}
	}
	return m
}

// lerMeminfo lê "MemTotal:  1012345 kB" em chave -> kB.
func readMeminfo(b []byte) map[string]int64 {
	out := map[string]int64{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		key, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		campos := strings.Fields(rest)
		if len(campos) == 0 {
			continue
		}
		if v, err := strconv.ParseInt(campos[0], 10, 64); err == nil {
			out[key] = v
		}
	}
	return out
}

// lerCampo acha "<nome> <número>" num arquivo de uma chave por linha, como o
// /proc/vmstat.
func readField(b []byte, nome string) (int64, bool) {
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

// doCgroup lê um arquivo do cgroup v2 deste processo. O caminho vem do
// /proc/self/cgroup ("0::/caminho"); dentro de um container com namespace de
// cgroup próprio ele é "/", e o arquivo está na raiz de /sys/fs/cgroup. Vazio
// sem cgroup v2 ou sem o arquivo.
func fromCgroup(f fontes, nome string) string {
	b, err := os.ReadFile(filepath.Join(f.proc, "self/cgroup"))
	if err != nil {
		return ""
	}
	for l := range strings.SplitSeq(string(b), "\n") {
		if filePath, ok := strings.CutPrefix(l, "0::"); ok {
			return readText(filepath.Join(f.sys, "fs/cgroup", filePath, nome))
		}
	}
	return ""
}

// limiteDoCgroup devolve o teto de memória do cgroup deste processo, ou 0 sem
// teto. O v1 fica como plano B para kernel antigo.
func cgroupLimit(f fontes) int64 {
	if v := fromCgroup(f, "memory.max"); v != "" && v != "max" {
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	// No v1, "sem limite" é um número enorme (PAGE_COUNTER_MAX em bytes), e
	// não uma palavra. Acima de 1 PiB, trata-se como sem limite.
	if n := lerInt(filepath.Join(f.sys, "fs/cgroup/memory/memory.limit_in_bytes")); n > 0 && n < 1<<50 {
		return n
	}
	return 0
}

// nucleosDoCgroup devolve o teto de CPU do container em núcleos, ou 0 sem
// teto. O cpu.max do v2 é "<cota> <período>" em microssegundos, ou "max
// <período>" sem teto: o `--cpus 1.5` do Docker vira "150000 100000". No v1, a
// cota é -1 sem teto.
func cgroupCores(f fontes) float64 {
	var quota, period int64
	if campos := strings.Fields(fromCgroup(f, "cpu.max")); len(campos) == 2 {
		quota, _ = strconv.ParseInt(campos[0], 10, 64)
		period, _ = strconv.ParseInt(campos[1], 10, 64)
	} else {
		quota = lerInt(filepath.Join(f.sys, "fs/cgroup/cpu/cpu.cfs_quota_us"))
		period = lerInt(filepath.Join(f.sys, "fs/cgroup/cpu/cpu.cfs_period_us"))
	}
	if quota <= 0 || period <= 0 {
		return 0
	}
	return float64(quota) / float64(period)
}

// ---- pressão ---------------------------------------------------------------

// coletarPressao lê o PSI de CPU, memória e disco. Kernel sem PSI (anterior ao
// 4.20, ou compilado sem) não tem /proc/pressure, e o campo inteiro some.
func collectPressure(f fontes) map[string]pressure {
	out := map[string]pressure{}
	for file, key := range map[string]string{"cpu": "cpu", "memory": "memoria", "io": "io"} {
		if b, err := os.ReadFile(filepath.Join(f.proc, "pressure", file)); err == nil {
			out[key] = readPressure(string(b))
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
func readPressure(s string) pressure {
	var p pressure
	for l := range strings.SplitSeq(s, "\n") {
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
func collectMount(f fontes, root string) storageInfo {
	info := storageInfo{Path: root}
	filePath, err := filepath.Abs(root)
	if err != nil {
		return info
	}
	if real, err := filepath.EvalSymlinks(filePath); err == nil {
		filePath = real
	}
	b, err := os.ReadFile(filepath.Join(f.proc, "self/mountinfo"))
	if err != nil {
		return info
	}
	if m, ok := mountOf(b, filePath); ok {
		info.Type, info.Origin, info.Options = m.kind, m.origin, m.options
		info.ReadOnly = hasOption(m.options, "ro") || hasOption(m.superOptions, "ro")
	}
	return info
}

type mount struct {
	point, options, kind, origin, superOptions string
}

// montagemDe acha no mountinfo a montagem mais específica que contém o
// caminho. O formato de cada linha é
//
//	36 35 98:0 /mnt1 /mnt/parent rw,noatime master:1 - ext3 /dev/root rw,errors=continue
//
// com um número variável de campos opcionais antes do "-".
func mountOf(b []byte, filePath string) (mount, bool) {
	var best mount
	found := false
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
		m := mount{
			point:   unescape(campos[4]),
			options: campos[5],
			kind:    campos[sep+1],
			origin:  unescape(campos[sep+2]),
		}
		if len(campos) > sep+3 {
			m.superOptions = campos[sep+3]
		}
		if !contains(m.point, filePath) {
			continue
		}
		// >= e não >: montagens empilhadas no mesmo ponto aparecem na ordem
		// em que foram feitas, e a última é a que vale.
		if !found || len(m.point) >= len(best.point) {
			best, found = m, true
		}
	}
	return best, found
}

func contains(point, filePath string) bool {
	return point == "/" || filePath == point || strings.HasPrefix(filePath, point+"/")
}

func hasOption(options, nome string) bool {
	return slices.Contains(strings.Split(options, ","), nome)
}

// desescapar desfaz o \040 (espaço) e companhia com que o kernel escreve
// caminhos no mountinfo.
func unescape(s string) string {
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
const writeDeadline = 5 * time.Second

// testarEscrita prova que o storage aceita escrita AGORA: cria um arquivo,
// grava 4 KB, força para o disco e apaga. O tempo do fsync é o número que
// importa - um cartão SD no fim da vida ainda aceita escrita, só que em
// segundos.
//
// Um disco travado prende o fsync por tempo indeterminado, e nada interrompe
// um fsync. Por isso o teste roda à parte, a resposta desiste no prazo, e um
// teste ainda preso faz os pedidos seguintes responderem isso mesmo em vez de
// empilhar mais arquivos e mais goroutines no disco que já não responde.
//
// A falha vai para o log só quando muda. Com o card aberto o teste roda de 15
// em 15 s, e um aviso a cada rodada encheria em minutos as 50 linhas que a
// tela mostra, empurrando para fora justamente o erro que explicava o disco.
func (s *Server) testWrite() write {
	if !s.writing.CompareAndSwap(false, true) {
		return write{Error: "o teste anterior ainda não terminou: o disco não está respondendo"}
	}
	pronto := make(chan write, 1)
	go func() {
		defer s.writing.Store(false)
		start := time.Now()
		err := writeTest(s.cfg.Storage.Root)
		ms := time.Since(start).Milliseconds()
		if err != nil {
			if err.Error() != s.writeErr {
				s.log.Warn("teste de escrita no storage falhou", "erro", err)
			}
			s.writeErr = err.Error()
			pronto <- write{Ms: ms, Error: writeReason(err)}
			return
		}
		s.writeErr = ""
		pronto <- write{OK: true, Ms: ms}
	}()
	select {
	case e := <-pronto:
		return e
	case <-time.After(writeDeadline):
		return write{Ms: writeDeadline.Milliseconds(), Error: "o disco não respondeu em " + writeDeadline.String()}
	}
}

// arquivoDeTeste é sempre o mesmo, e não um nome aleatório por rodada: se o
// dwnvr morrer entre criar e apagar, sobra no máximo este arquivo, e o próximo
// teste o reaproveita e apaga. O ponto na frente o esconde do ls comum e ele
// nunca parece câmera; o store.Orphans só olha diretórios.
const testFile = ".dwnvr-teste"

func writeTest(root string) (err error) {
	// Um teste de cada vez, garantido pelo escrevendo: o nome fixo não tem
	// como ser disputado.
	f, err := os.OpenFile(filepath.Join(root, testFile), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
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
func writeReason(err error) string {
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

func (s *Server) probeGo2RTC(ctx context.Context) go2rtcInfo {
	if s.client == nil {
		return go2rtcInfo{Error: "não configurado"}
	}
	start := time.Now()
	v, err := s.client.Version(ctx)
	info := go2rtcInfo{Ms: time.Since(start).Milliseconds(), URL: withoutCredential(s.client.BaseURL)}
	if err != nil {
		// Mesma exposição do go2rtcError do /api/cameras: o erro de rede é
		// o diagnóstico, e a URL nele não traz senha (o cliente usa Basic
		// Auth por cabeçalho).
		info.Error = err.Error()
		return info
	}
	info.OK, info.Version = true, v
	return info
}

// ---- detector de objetos ---------------------------------------------------

// sondarDetector pergunta ao detector de objetos qual modelo ele carregou. A
// fila do /api/health já mostra o detector fora do ar, mas só quando alguma
// câmera manda olhar: sem movimento em câmera nenhuma, a queda passa
// despercebida. Esta pergunta responde a qualquer hora.
func (s *Server) probeDetector(ctx context.Context) *detectorInfo {
	start := time.Now()
	health, err := detect.SidecarHealth(ctx, s.cfg.Detector.URL)
	info := &detectorInfo{Ms: time.Since(start).Milliseconds(), URL: withoutCredential(s.cfg.Detector.URL)}
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.OK, info.Model, info.Entry, info.Threads = true, health.Model, health.InputSize, health.Threads
	return info
}

// ---- utilidades ------------------------------------------------------------

// semCredencial tira o "usuario:senha@" de uma URL antes de ela ir para a
// tela. O dwnvr manda a credencial do go2rtc por cabeçalho, mas nada impede
// alguém de escrevê-la direto na URL do dwnvr.yaml.
func withoutCredential(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	p.User = nil
	return p.String()
}

func readText(filePath string) string {
	b, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func lerInt(filePath string) int64 {
	n, _ := strconv.ParseInt(readText(filePath), 10, 64)
	return n
}
