package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/logbuf"
)

// fixture cria um arquivo, com os diretórios no caminho.
func fixture(t *testing.T, root, filePath, content string) {
	t.Helper()
	p := filepath.Join(root, filePath)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Uma Orange Pi Zero 3 quente, recortada de arquivos reais do /proc e /sys.
func fakeSystem(t *testing.T) fontes {
	f := fontes{proc: t.TempDir(), sys: t.TempDir()}
	fixture(t, f.proc, "loadavg", "3.10 2.05 1.50 2/180 4321\n")
	fixture(t, f.proc, "meminfo", "MemTotal:        1012345 kB\nMemFree:           51200 kB\nMemAvailable:     204800 kB\nSwapTotal:        524288 kB\nSwapFree:         262144 kB\n")
	fixture(t, f.proc, "vmstat", "nr_free_pages 12800\noom_kill 2\n")
	fixture(t, f.proc, "pressure/cpu", "some avg10=12.50 avg60=8.00 avg300=3.25 total=123\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n")
	fixture(t, f.proc, "pressure/io", "some avg10=40.00 avg60=35.10 avg300=20.00 total=999\nfull avg10=30.00 avg60=25.00 avg300=10.00 total=888\n")
	fixture(t, f.proc, "device-tree/model", "OrangePi Zero3\x00")
	fixture(t, f.proc, "sys/kernel/osrelease", "6.12.58-current-sunxi64\n")
	fixture(t, f.proc, "cpuinfo", "processor\t: 0\nBogoMIPS\t: 48.00\nCPU part\t: 0xd03\n")
	fixture(t, f.proc, "self/cgroup", "0::/\n")
	fixture(t, f.sys, "fs/cgroup/memory.max", "536870912\n")
	fixture(t, f.sys, "fs/cgroup/cpu.max", "150000 100000\n")
	// O nome entre parênteses com espaço e ")" dentro é o caso que derruba
	// quem conta os campos desde o começo da linha.
	fixture(t, f.proc, "self/stat", "1857 (dw nvr) x) S 1 1857 1857 0 -1 4194560 2200 0 0 0 44346 36809 0 0 20 0 15 0 28549530\n")
	fixture(t, f.proc, "self/status", "Name:\tdwnvr\nUid:\t1000\t1000\t1000\t1000\nGid:\t1001\t1001\t1001\t1001\nVmRSS:\t   33184 kB\nThreads:\t15\n")
	fixture(t, f.proc, "self/limits", "Limit                     Soft Limit           Hard Limit           Units\nMax open files            1024                 524288               files\n")
	for _, fd := range []string{"0", "1", "2"} {
		fixture(t, f.proc, "self/fd/"+fd, "")
	}
	fixture(t, f.sys, "class/thermal/thermal_zone0/type", "cpu_thermal\n")
	fixture(t, f.sys, "class/thermal/thermal_zone0/temp", "84312\n")
	fixture(t, f.sys, "class/thermal/thermal_zone1/type", "ddr_thermal\n")
	fixture(t, f.sys, "class/thermal/thermal_zone1/temp", "61000\n")
	for _, cpu := range []string{"cpu0", "cpu1"} {
		fixture(t, f.sys, "devices/system/cpu/"+cpu+"/cpufreq/scaling_cur_freq", "1008000\n")
		fixture(t, f.sys, "devices/system/cpu/"+cpu+"/cpufreq/cpuinfo_max_freq", "1512000\n")
		fixture(t, f.sys, "devices/system/cpu/"+cpu+"/cpufreq/scaling_governor", "ondemand\n")
	}
	fixture(t, f.proc, "self/mountinfo",
		"22 1 179:2 / / rw,noatime shared:1 - ext4 /dev/mmcblk0p2 rw,commit=600\n"+
			"30 22 8:1 / /mnt/hd\\040externo rw,relatime shared:5 - ext4 /dev/sda1 ro,errors=remount-ro\n"+
			"31 22 0:40 / /mnt/hdx rw,relatime - fuseblk /dev/sdb1 rw\n")
	return f
}

func TestCollectMachineHot(t *testing.T) {
	m := collectMachine(fakeSystem(t))
	if len(m.Load) != 3 || m.Load[0] != 3.10 {
		t.Errorf("carga %v", m.Load)
	}
	if len(m.Sensores) != 2 || m.Sensores[0] != (sensor{"cpu_thermal", 84.3}) {
		t.Errorf("sensores %v: o mais quente tem que vir primeiro, em °C", m.Sensores)
	}
	if m.MHz != 1008 || m.MHzMax != 1512 || m.Governor != "ondemand" {
		t.Errorf("frequência %d de %d (%s)", m.MHz, m.MHzMax, m.Governor)
	}
	// No ARM o cpuinfo não tem nome de processador: quem identifica é a placa.
	if m.Board != "OrangePi Zero3" || m.Processor != "" || m.Kernel != "6.12.58-current-sunxi64" {
		t.Errorf("placa %q, processador %q, kernel %q", m.Board, m.Processor, m.Kernel)
	}
	if m.AllowedCores != 1.5 {
		t.Errorf("teto de CPU do container %v, esperado 1.5", m.AllowedCores)
	}
}

// Num PC: sem device tree, o nome vem do cpuinfo. E sem teto de CPU, o campo
// some.
func TestCollectMachinePC(t *testing.T) {
	f := fontes{proc: t.TempDir(), sys: t.TempDir()}
	fixture(t, f.proc, "cpuinfo", "processor\t: 0\nvendor_id\t: GenuineIntel\nmodel name\t: Intel(R) Celeron(R) N4020 CPU @ 1.10GHz\n")
	fixture(t, f.proc, "self/cgroup", "0::/\n")
	fixture(t, f.sys, "fs/cgroup/cpu.max", "max 100000\n")
	m := collectMachine(f)
	if m.Board != "" || m.Processor != "Intel(R) Celeron(R) N4020 CPU @ 1.10GHz" {
		t.Errorf("placa %q, processador %q", m.Board, m.Processor)
	}
	if m.AllowedCores != 0 {
		t.Errorf("sem teto de CPU, veio %v", m.AllowedCores)
	}
}

func TestCollectProcess(t *testing.T) {
	p := collectProcess(fakeSystem(t), time.Now().Add(-time.Hour))
	// (44346 + 36809) ticks de 10 ms.
	if p.CPUMs == nil || *p.CPUMs != 811550 {
		t.Errorf("CPU %v ms, esperado 811550", p.CPUMs)
	}
	if p.AliveMs < 3600000 {
		t.Errorf("no ar há %d ms, esperado 1 h", p.AliveMs)
	}
	if p.MemoryBytes != 33184<<10 {
		t.Errorf("memória %d", p.MemoryBytes)
	}
	if p.UID == nil || *p.UID != 1000 || p.GID == nil || *p.GID != 1001 {
		t.Errorf("uid %v gid %v", p.UID, p.GID)
	}
	if p.OpenFiles != 3 || p.FileLimit != 1024 {
		t.Errorf("arquivos %d de %d", p.OpenFiles, p.FileLimit)
	}
	if p.Goroutines < 1 || p.Go == "" || p.Architecture == "" {
		t.Errorf("runtime %+v", p)
	}

	// Fora do Linux nada disso existe, e os campos somem em vez de vir zero
	// disfarçado de medida.
	p = collectProcess(fontes{proc: t.TempDir()}, time.Now())
	if p.CPUMs != nil || p.MemoryBytes != 0 || p.UID != nil || p.FileLimit != 0 {
		t.Errorf("sem /proc: %+v", p)
	}
}

func TestWithoutCredential(t *testing.T) {
	// A URL com credencial é montada, e não escrita por extenso, para o
	// detector de segredos do pre-commit não tomá-la por uma senha de verdade.
	comSenha := (&url.URL{Scheme: "http", Host: "go2rtc:1984", User: url.UserPassword("admin", "teste")}).String()
	cases := map[string]string{
		comSenha:                    "http://go2rtc:1984",
		"http://dwnvr-detect:8480/": "http://dwnvr-detect:8480/",
	}
	for entry, want := range cases {
		if got := withoutCredential(entry); got != want {
			t.Errorf("%s: %s, esperado %s", entry, got, want)
		}
	}
}

// Um PC com duas zonas "acpitz": o nome repetido ganha o número da zona, senão
// a tela não tem como separar as duas linhas.
func TestSensorsWithRepeatedName(t *testing.T) {
	sys := t.TempDir()
	for zone, temp := range map[string]string{"0": "90000", "1": "88000", "2": "50000"} {
		kind := "acpitz"
		if zone == "2" {
			kind = "x86_pkg_temp"
		}
		fixture(t, sys, "class/thermal/thermal_zone"+zone+"/type", kind+"\n")
		fixture(t, sys, "class/thermal/thermal_zone"+zone+"/temp", temp+"\n")
	}
	got := lerSensores(sys)
	want := []sensor{{"acpitz 0", 90}, {"acpitz 1", 88}, {"x86_pkg_temp", 50}}
	if len(got) != len(want) {
		t.Fatalf("sensores %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sensor %d: %v, esperado %v", i, got[i], want[i])
		}
	}
}

func TestCollectMemory(t *testing.T) {
	m := collectMemory(fakeSystem(t))
	if m == nil {
		t.Fatal("sem memória")
	}
	if m.AvailableBytes != 204800<<10 || m.SwapUsedBytes != 262144<<10 {
		t.Errorf("disponível %d, swap usada %d", m.AvailableBytes, m.SwapUsedBytes)
	}
	if m.LimitBytes != 512<<20 {
		t.Errorf("limite do cgroup %d", m.LimitBytes)
	}
	if m.OOMKills == nil || *m.OOMKills != 2 {
		t.Errorf("oom_kill %v", m.OOMKills)
	}
}

// Sem arquivo de pressão de memória, as outras duas chegam e a de memória
// some - em vez de vir zerada, que leria como "sem pressão nenhuma".
func TestCollectPressure(t *testing.T) {
	p := collectPressure(fakeSystem(t))
	if _, has := p["memoria"]; has {
		t.Error("pressão de memória inventada")
	}
	io := p["io"]
	if io.Some == nil || io.Some.Avg60 != 35.10 || io.Full == nil || io.Full.Avg10 != 30 {
		t.Errorf("io %+v", io)
	}
	if p["cpu"].Some.Avg300 != 3.25 {
		t.Errorf("cpu %+v", p["cpu"].Some)
	}
	if collectPressure(fontes{proc: t.TempDir()}) != nil {
		t.Error("kernel sem PSI tem que omitir o campo inteiro")
	}
}

func TestMostSpecificMount(t *testing.T) {
	f := fakeSystem(t)
	cases := []struct {
		filePath, kind, origin string
		ro                     bool
	}{
		{"/storage", "ext4", "/dev/mmcblk0p2", false},
		// O ro das super-opções é o do kernel remontando após erro, mesmo
		// com a montagem dizendo rw. E o \040 é um espaço no caminho.
		{"/mnt/hd externo/cams", "ext4", "/dev/sda1", true},
		// /mnt/hdx não contém /mnt/hd: prefixo de texto não é prefixo de caminho.
		{"/mnt/hdx/cams", "fuseblk", "/dev/sdb1", false},
	}
	for _, c := range cases {
		s := collectMount(f, c.filePath)
		if s.Type != c.kind || s.Origin != c.origin || s.ReadOnly != c.ro {
			t.Errorf("%s: %s %s ro=%v", c.filePath, s.Type, s.Origin, s.ReadOnly)
		}
	}
}

func TestWriteTest(t *testing.T) {
	s, _ := testServer(t)
	// A sobra de um dwnvr que morreu no meio do teste: o próximo a reaproveita
	// e apaga.
	fixture(t, s.cfg.Storage.Root, testFile, "sobra")
	e := s.testWrite()
	if !e.OK || e.Error != "" {
		t.Fatalf("escrita num diretório gravável: %+v", e)
	}
	leftovers, _ := os.ReadDir(s.cfg.Storage.Root)
	for _, x := range leftovers {
		if !x.IsDir() {
			t.Errorf("o teste deixou %s para trás", x.Name())
		}
	}

	s.cfg.Storage.Root = filepath.Join(t.TempDir(), "nao-existe")
	if e := s.testWrite(); e.OK || e.Error != "o diretório do storage não existe" {
		t.Errorf("storage inexistente: %+v", e)
	}
}

// A mesma falha repetida vai uma vez só para o log: com o card aberto, o teste
// roda a cada 15 s e não pode tomar as linhas que a tela mostra.
func TestWriteFailureDoesNotFloodTheLog(t *testing.T) {
	s, _ := testServer(t)
	h := logbuf.New(slog.DiscardHandler, 10)
	s.log = slog.New(h)
	s.cfg.Storage.Root = filepath.Join(t.TempDir(), "nao-existe")
	for range 3 {
		s.testWrite()
	}
	if _, total := h.Recent(); total != 1 {
		t.Errorf("3 falhas iguais geraram %d avisos, esperado 1", total)
	}

	// Voltou a gravar e falhou de novo: é outro episódio, e avisa outra vez.
	root := s.cfg.Storage.Root
	s.cfg.Storage.Root = t.TempDir()
	s.testWrite()
	s.cfg.Storage.Root = root
	s.testWrite()
	if _, total := h.Recent(); total != 2 {
		t.Errorf("falha depois de um sucesso: %d avisos, esperado 2", total)
	}
}

// A resposta inteira, pelo handler: go2rtc fora do ar não derruba o resto, e
// os avisos do log chegam.
func TestHandleServerDiagnostics(t *testing.T) {
	s, _ := testServer(t)
	h := logbuf.New(slog.DiscardHandler, 10)
	s.log = slog.New(h)
	s.log.Warn("conexão caiu", "cam", "garagem")

	rec := httptest.NewRecorder()
	s.handleServerDiagnostics(rec, httptest.NewRequest(http.MethodGet, "/api/health/servidor", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var got serverInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Go2RTC.OK || got.Go2RTC.Error == "" {
		t.Errorf("go2rtc sem cliente: %+v", got.Go2RTC)
	}
	if !got.Storage.Write.OK {
		t.Errorf("escrita: %+v", got.Storage.Write)
	}
	if got.Log == nil || got.Log.Total != 1 || got.Log.Lines[0].Text != "conexão caiu cam=garagem" {
		t.Errorf("log: %+v", got.Log)
	}
	if em, err := time.Parse(time.RFC3339, got.CollectedAt.Em); err != nil || time.Since(em) > time.Minute || got.CollectedAt.Abbreviation == "" {
		t.Errorf("coletadoEm: %+v", got.CollectedAt)
	}
	// Sem detector configurado, nem a pergunta nem o campo.
	if got.Detector != nil {
		t.Errorf("detector sem detector.url: %+v", got.Detector)
	}
}

// Com detector, a resposta traz o que o /health dele disse, e o endereço sem
// a credencial.
func TestDiagnosticsWithDetector(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"modelo": "modelo.onnx", "entrada": "512x288", "threads": 2}`))
	}))
	defer srv.Close()
	s, _ := testServer(t)
	s.cfg.Detector.URL = strings.Replace(srv.URL, "http://", "http://u:senha@", 1)

	rec := httptest.NewRecorder()
	s.handleServerDiagnostics(rec, httptest.NewRequest(http.MethodGet, "/api/health/servidor", nil))
	var got serverInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	d := got.Detector
	if d == nil || !d.OK || d.Model != "modelo.onnx" || d.Threads != 2 || d.URL != srv.URL {
		t.Errorf("detector: %+v", d)
	}
}
