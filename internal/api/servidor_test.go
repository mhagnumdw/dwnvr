package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/logbuf"
)

// fixture cria um arquivo, com os diretórios no caminho.
func fixture(t *testing.T, raiz, caminho, conteudo string) {
	t.Helper()
	p := filepath.Join(raiz, caminho)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(conteudo), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Uma Orange Pi Zero 3 quente, recortada de arquivos reais do /proc e /sys.
func sistemaFalso(t *testing.T) fontes {
	f := fontes{proc: t.TempDir(), sys: t.TempDir()}
	fixture(t, f.proc, "loadavg", "3.10 2.05 1.50 2/180 4321\n")
	fixture(t, f.proc, "meminfo", "MemTotal:        1012345 kB\nMemFree:           51200 kB\nMemAvailable:     204800 kB\nSwapTotal:        524288 kB\nSwapFree:         262144 kB\n")
	fixture(t, f.proc, "vmstat", "nr_free_pages 12800\noom_kill 2\n")
	fixture(t, f.proc, "pressure/cpu", "some avg10=12.50 avg60=8.00 avg300=3.25 total=123\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n")
	fixture(t, f.proc, "pressure/io", "some avg10=40.00 avg60=35.10 avg300=20.00 total=999\nfull avg10=30.00 avg60=25.00 avg300=10.00 total=888\n")
	fixture(t, f.proc, "self/cgroup", "0::/\n")
	fixture(t, f.sys, "fs/cgroup/memory.max", "536870912\n")
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

func TestColetaMaquinaQuente(t *testing.T) {
	m := coletarMaquina(sistemaFalso(t))
	if len(m.Carga) != 3 || m.Carga[0] != 3.10 {
		t.Errorf("carga %v", m.Carga)
	}
	if len(m.Sensores) != 2 || m.Sensores[0] != (sensor{"cpu_thermal", 84.3}) {
		t.Errorf("sensores %v: o mais quente tem que vir primeiro, em °C", m.Sensores)
	}
	if m.MHz != 1008 || m.MHzMax != 1512 || m.Governor != "ondemand" {
		t.Errorf("frequência %d de %d (%s)", m.MHz, m.MHzMax, m.Governor)
	}
}

// Um PC com duas zonas "acpitz": o nome repetido ganha o número da zona, senão
// a tela não tem como separar as duas linhas.
func TestSensoresComNomeRepetido(t *testing.T) {
	sys := t.TempDir()
	for zona, temp := range map[string]string{"0": "90000", "1": "88000", "2": "50000"} {
		tipo := "acpitz"
		if zona == "2" {
			tipo = "x86_pkg_temp"
		}
		fixture(t, sys, "class/thermal/thermal_zone"+zona+"/type", tipo+"\n")
		fixture(t, sys, "class/thermal/thermal_zone"+zona+"/temp", temp+"\n")
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

func TestColetaMemoria(t *testing.T) {
	m := coletarMemoria(sistemaFalso(t))
	if m == nil {
		t.Fatal("sem memória")
	}
	if m.DisponivelBytes != 204800<<10 || m.SwapUsadaBytes != 262144<<10 {
		t.Errorf("disponível %d, swap usada %d", m.DisponivelBytes, m.SwapUsadaBytes)
	}
	if m.LimiteBytes != 512<<20 {
		t.Errorf("limite do cgroup %d", m.LimiteBytes)
	}
	if m.OOMKills == nil || *m.OOMKills != 2 {
		t.Errorf("oom_kill %v", m.OOMKills)
	}
}

// Sem arquivo de pressão de memória, as outras duas chegam e a de memória
// some - em vez de vir zerada, que leria como "sem pressão nenhuma".
func TestColetaPressao(t *testing.T) {
	p := coletarPressao(sistemaFalso(t))
	if _, tem := p["memoria"]; tem {
		t.Error("pressão de memória inventada")
	}
	io := p["io"]
	if io.Some == nil || io.Some.Avg60 != 35.10 || io.Full == nil || io.Full.Avg10 != 30 {
		t.Errorf("io %+v", io)
	}
	if p["cpu"].Some.Avg300 != 3.25 {
		t.Errorf("cpu %+v", p["cpu"].Some)
	}
	if coletarPressao(fontes{proc: t.TempDir()}) != nil {
		t.Error("kernel sem PSI tem que omitir o campo inteiro")
	}
}

func TestMontagemMaisEspecifica(t *testing.T) {
	f := sistemaFalso(t)
	casos := []struct {
		caminho, tipo, origem string
		ro                    bool
	}{
		{"/storage", "ext4", "/dev/mmcblk0p2", false},
		// O ro das super-opções é o do kernel remontando após erro, mesmo
		// com a montagem dizendo rw. E o \040 é um espaço no caminho.
		{"/mnt/hd externo/cams", "ext4", "/dev/sda1", true},
		// /mnt/hdx não contém /mnt/hd: prefixo de texto não é prefixo de caminho.
		{"/mnt/hdx/cams", "fuseblk", "/dev/sdb1", false},
	}
	for _, c := range casos {
		s := coletarMontagem(f, c.caminho)
		if s.Tipo != c.tipo || s.Origem != c.origem || s.SomenteLeitura != c.ro {
			t.Errorf("%s: %s %s ro=%v", c.caminho, s.Tipo, s.Origem, s.SomenteLeitura)
		}
	}
}

func TestTesteDeEscrita(t *testing.T) {
	s, _ := testServer(t)
	e := s.testarEscrita()
	if !e.OK || e.Erro != "" {
		t.Fatalf("escrita num diretório gravável: %+v", e)
	}
	sobras, _ := os.ReadDir(s.cfg.Storage.Root)
	for _, x := range sobras {
		if !x.IsDir() {
			t.Errorf("o teste deixou %s para trás", x.Name())
		}
	}

	s.cfg.Storage.Root = filepath.Join(t.TempDir(), "nao-existe")
	if e := s.testarEscrita(); e.OK || e.Erro != "o diretório do storage não existe" {
		t.Errorf("storage inexistente: %+v", e)
	}
}

// A resposta inteira, pelo handler: go2rtc fora do ar não derruba o resto, e
// os avisos do log chegam.
func TestHandleServidor(t *testing.T) {
	s, _ := testServer(t)
	h := logbuf.New(slog.DiscardHandler, 10)
	s.log = slog.New(h)
	s.log.Warn("conexão caiu", "cam", "garagem")

	rec := httptest.NewRecorder()
	s.handleServidor(rec, httptest.NewRequest(http.MethodGet, "/api/health/servidor", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var got servidorInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Go2RTC.OK || got.Go2RTC.Erro == "" {
		t.Errorf("go2rtc sem cliente: %+v", got.Go2RTC)
	}
	if !got.Storage.Escrita.OK {
		t.Errorf("escrita: %+v", got.Storage.Escrita)
	}
	if got.Log == nil || got.Log.Total != 1 || got.Log.Linhas[0].Texto != "conexão caiu cam=garagem" {
		t.Errorf("log: %+v", got.Log)
	}
}
