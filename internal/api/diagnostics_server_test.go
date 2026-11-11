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

// fixture creates a file, with the directories in the path.
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

// A hot Orange Pi Zero 3, cut from real files in /proc and /sys.
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
	// The name in parentheses with a space and ")" inside is the case that
	// breaks whoever counts the fields from the start of the line.
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
		t.Errorf("load %v", m.Load)
	}
	if len(m.Sensores) != 2 || m.Sensores[0] != (sensor{"cpu_thermal", 84.3}) {
		t.Errorf("sensors %v: the hottest must come first, in °C", m.Sensores)
	}
	if m.MHz != 1008 || m.MHzMax != 1512 || m.Governor != "ondemand" {
		t.Errorf("frequency %d of %d (%s)", m.MHz, m.MHzMax, m.Governor)
	}
	// On ARM cpuinfo has no processor name: what identifies the machine is the
	// board.
	if m.Board != "OrangePi Zero3" || m.Processor != "" || m.Kernel != "6.12.58-current-sunxi64" {
		t.Errorf("board %q, processor %q, kernel %q", m.Board, m.Processor, m.Kernel)
	}
	if m.AllowedCores != 1.5 {
		t.Errorf("container CPU ceiling %v, expected 1.5", m.AllowedCores)
	}
}

// On a PC: without a device tree, the name comes from cpuinfo. And without a
// CPU ceiling, the field disappears.
func TestCollectMachinePC(t *testing.T) {
	f := fontes{proc: t.TempDir(), sys: t.TempDir()}
	fixture(t, f.proc, "cpuinfo", "processor\t: 0\nvendor_id\t: GenuineIntel\nmodel name\t: Intel(R) Celeron(R) N4020 CPU @ 1.10GHz\n")
	fixture(t, f.proc, "self/cgroup", "0::/\n")
	fixture(t, f.sys, "fs/cgroup/cpu.max", "max 100000\n")
	m := collectMachine(f)
	if m.Board != "" || m.Processor != "Intel(R) Celeron(R) N4020 CPU @ 1.10GHz" {
		t.Errorf("board %q, processor %q", m.Board, m.Processor)
	}
	if m.AllowedCores != 0 {
		t.Errorf("without a CPU ceiling, got %v", m.AllowedCores)
	}
}

func TestCollectProcess(t *testing.T) {
	p := collectProcess(fakeSystem(t), time.Now().Add(-time.Hour))
	// (44346 + 36809) ticks of 10 ms.
	if p.CPUMs == nil || *p.CPUMs != 811550 {
		t.Errorf("CPU %v ms, expected 811550", p.CPUMs)
	}
	if p.AliveMs < 3600000 {
		t.Errorf("up for %d ms, expected 1 h", p.AliveMs)
	}
	if p.MemoryBytes != 33184<<10 {
		t.Errorf("memory %d", p.MemoryBytes)
	}
	if p.UID == nil || *p.UID != 1000 || p.GID == nil || *p.GID != 1001 {
		t.Errorf("uid %v gid %v", p.UID, p.GID)
	}
	if p.OpenFiles != 3 || p.FileLimit != 1024 {
		t.Errorf("files %d of %d", p.OpenFiles, p.FileLimit)
	}
	if p.Goroutines < 1 || p.Go == "" || p.Architecture == "" {
		t.Errorf("runtime %+v", p)
	}

	// Outside Linux none of this exists, and the fields disappear instead of
	// coming as a zero disguised as a measurement.
	p = collectProcess(fontes{proc: t.TempDir()}, time.Now())
	if p.CPUMs != nil || p.MemoryBytes != 0 || p.UID != nil || p.FileLimit != 0 {
		t.Errorf("without /proc: %+v", p)
	}
}

func TestWithoutCredential(t *testing.T) {
	// The URL with a credential is assembled, and not written out in full, so
	// the pre-commit secrets detector does not take it for a real password.
	comSenha := (&url.URL{Scheme: "http", Host: "go2rtc:1984", User: url.UserPassword("admin", "teste")}).String()
	cases := map[string]string{
		comSenha:                    "http://go2rtc:1984",
		"http://dwnvr-detect:8480/": "http://dwnvr-detect:8480/",
	}
	for entry, want := range cases {
		if got := withoutCredential(entry); got != want {
			t.Errorf("%s: %s, expected %s", entry, got, want)
		}
	}
}

// A PC with two "acpitz" zones: the repeated name gets the zone number,
// otherwise the screen has no way to tell the two lines apart.
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
			t.Errorf("sensor %d: %v, expected %v", i, got[i], want[i])
		}
	}
}

func TestCollectMemory(t *testing.T) {
	m := collectMemory(fakeSystem(t))
	if m == nil {
		t.Fatal("no memory")
	}
	if m.AvailableBytes != 204800<<10 || m.SwapUsedBytes != 262144<<10 {
		t.Errorf("available %d, swap used %d", m.AvailableBytes, m.SwapUsedBytes)
	}
	if m.LimitBytes != 512<<20 {
		t.Errorf("cgroup limit %d", m.LimitBytes)
	}
	if m.OOMKills == nil || *m.OOMKills != 2 {
		t.Errorf("oom_kill %v", m.OOMKills)
	}
}

// Without the memory pressure file, the other two arrive and the memory one
// disappears - instead of coming zeroed, which would read as "no pressure at
// all".
func TestCollectPressure(t *testing.T) {
	p := collectPressure(fakeSystem(t))
	if _, has := p["memoria"]; has {
		t.Error("made-up memory pressure")
	}
	io := p["io"]
	if io.Some == nil || io.Some.Avg60 != 35.10 || io.Full == nil || io.Full.Avg10 != 30 {
		t.Errorf("io %+v", io)
	}
	if p["cpu"].Some.Avg300 != 3.25 {
		t.Errorf("cpu %+v", p["cpu"].Some)
	}
	if collectPressure(fontes{proc: t.TempDir()}) != nil {
		t.Error("kernel without PSI must omit the whole field")
	}
}

func TestMostSpecificMount(t *testing.T) {
	f := fakeSystem(t)
	cases := []struct {
		filePath, kind, origin string
		ro                     bool
	}{
		{"/storage", "ext4", "/dev/mmcblk0p2", false},
		// The ro in the super options is the kernel remounting after an error,
		// even with the mount saying rw. And \040 is a space in the path.
		{"/mnt/hd externo/cams", "ext4", "/dev/sda1", true},
		// /mnt/hdx does not contain /mnt/hd: a text prefix is not a path prefix.
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
	// The leftover of a dwnvr that died in the middle of the test: the next one
	// reuses and deletes it.
	fixture(t, s.cfg.Storage.Root, testFile, "sobra")
	e := s.testWrite()
	if !e.OK || e.Error != "" {
		t.Fatalf("write to a writable directory: %+v", e)
	}
	leftovers, _ := os.ReadDir(s.cfg.Storage.Root)
	for _, x := range leftovers {
		if !x.IsDir() {
			t.Errorf("the test left %s behind", x.Name())
		}
	}

	s.cfg.Storage.Root = filepath.Join(t.TempDir(), "nao-existe")
	if e := s.testWrite(); e.OK || e.Error != "o diretório do storage não existe" {
		t.Errorf("nonexistent storage: %+v", e)
	}
}

// The same repeated failure goes to the log only once: with the card open, the
// test runs every 15 s and must not take over the lines the screen shows.
func TestWriteFailureDoesNotFloodTheLog(t *testing.T) {
	s, _ := testServer(t)
	h := logbuf.New(slog.DiscardHandler, 10)
	s.log = slog.New(h)
	s.cfg.Storage.Root = filepath.Join(t.TempDir(), "nao-existe")
	for range 3 {
		s.testWrite()
	}
	if _, total := h.Recent(); total != 1 {
		t.Errorf("3 identical failures produced %d warnings, expected 1", total)
	}

	// Writing worked again and then failed again: it is another episode, and it
	// warns again.
	root := s.cfg.Storage.Root
	s.cfg.Storage.Root = t.TempDir()
	s.testWrite()
	s.cfg.Storage.Root = root
	s.testWrite()
	if _, total := h.Recent(); total != 2 {
		t.Errorf("failure after a success: %d warnings, expected 2", total)
	}
}

// The whole response, through the handler: go2rtc down does not take down the
// rest, and the log warnings arrive.
func TestHandleServerDiagnostics(t *testing.T) {
	s, _ := testServer(t)
	h := logbuf.New(slog.DiscardHandler, 10)
	s.log = slog.New(h)
	s.log.Warn("connection dropped", "cam", "garagem")

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
		t.Errorf("go2rtc without a client: %+v", got.Go2RTC)
	}
	if !got.Storage.Write.OK {
		t.Errorf("write: %+v", got.Storage.Write)
	}
	if got.Log == nil || got.Log.Total != 1 || got.Log.Lines[0].Text != "connection dropped cam=garagem" {
		t.Errorf("log: %+v", got.Log)
	}
	if em, err := time.Parse(time.RFC3339, got.CollectedAt.Em); err != nil || time.Since(em) > time.Minute || got.CollectedAt.Abbreviation == "" {
		t.Errorf("coletadoEm: %+v", got.CollectedAt)
	}
	// Without an object detector configured, neither the question nor the
	// field.
	if got.Detector != nil {
		t.Errorf("object detector without detector.url: %+v", got.Detector)
	}
}

// With an object detector, the response carries what its /health said, and the
// address without the credential.
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
