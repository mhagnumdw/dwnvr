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

// The "This server" card in Diagnostics: the machine that records, seen from
// inside the process. It complements /api/health, which answers "are the cameras
// recording?", with "does the machine have the headroom to record?".
//
// It is a separate endpoint, and not more fields in /api/health, because it
// costs more: a write test with fsync on the storage and a trip to go2rtc and to
// the object detector. /api/health is polled in a loop by anyone who only came
// to see the cameras; this one only runs with the card open.
//
// Everything comes from /proc and /sys, without exec: the image is FROM scratch
// and there is no binary to call. Each field is optional and disappears when its
// source does not exist - kernel without PSI, system without cgroup v2, outside
// Linux.

// fontes are the roots of /proc and /sys, swappable in tests.
type sources struct{ proc, sys string }

var systemSources = sources{proc: "/proc", sys: "/sys"}

type serverInfo struct {
	CollectedAt collection          `json:"coletadoEm"`
	Machine     machineInfo         `json:"maquina"`
	Process     processInfo         `json:"processo"`
	Memory      *memoryInfo         `json:"memoria,omitempty"`
	Pressure    map[string]pressure `json:"pressao,omitempty"`
	Storage     storageInfo         `json:"storage"`
	Go2RTC      go2rtcInfo          `json:"go2rtc"`
	// Detector disappears without `detector.url`: whoever does not use object
	// detection does not even pay for the trip to it.
	Detector *detectorInfo `json:"detector,omitempty"`
	// SMTP disappears without the smtp section. It goes without a probe:
	// checking the email server means sending an email, and that is the test
	// button.
	SMTP *smtpInfo `json:"smtp,omitempty"`
	// Alerts are the numbers of each alert notifier since dwnvr started. It
	// disappears without a notifier, or before some camera turns on the alert.
	Alerts []alertNotifierInfo `json:"alertas,omitempty"`
	Log    *logInfo            `json:"log,omitempty"`
}

// alertNotifierInfo is the Dispatcher's NotifierState. Ultimo disappears before
// the first send, and Error when the last send succeeded.
type alertNotifierInfo struct {
	Name     string     `json:"nome"`
	Sent     int64      `json:"enviados"`
	Failures int64      `json:"falhas"`
	Last     *time.Time `json:"ultimo,omitempty"`
	Error    string     `json:"erro,omitempty"`
}

// collection is the moment of the reading on the server's clock, in its time
// zone. It goes with the time zone, and not only the instant, for the same
// reason as the `clock` in /api/health: the question is the time THERE, and
// converted to the browser's time zone it would hide a server with a wrong time
// zone or NTP.
type collection struct {
	At            string `json:"em"`
	Abbreviation  string `json:"sigla"`
	OffsetSeconds int    `json:"offsetSeconds"`
	// Zone is the IANA name ("America/Sao_Paulo"), when the server knows it.
	Zone string `json:"zona,omitempty"`
}

type machineInfo struct {
	// Board comes from the device tree, which only exists on ARM ("OrangePi
	// Zero3"); Processor, from /proc/cpuinfo, which only has a name on x86. One
	// or the other is usually missing, and whatever is missing disappears.
	Board     string `json:"placa,omitempty"`
	Processor string `json:"processador,omitempty"`
	// Kernel is the host's kernel even inside the container, which has no
	// kernel of its own.
	Kernel string `json:"kernel,omitempty"`
	Cores  int    `json:"nucleos"`
	// AllowedCores is the container's CPU ceiling (cgroup), in cores: 1.5 is
	// one and a half cores. It disappears without a ceiling.
	AllowedCores float64 `json:"nucleosLiberados,omitempty"`
	// Load is the load average over 1, 5 and 15 minutes.
	Load     []float64 `json:"carga,omitempty"`
	Sensores []sensor  `json:"sensores,omitempty"`
	// MHz is the highest current frequency among the cores, and MHzMax the
	// hardware ceiling. Below the ceiling with the machine hot is the kernel
	// itself holding back the CPU to cool it down.
	MHz      int    `json:"mhz,omitempty"`
	MHzMax   int    `json:"mhzMax,omitempty"`
	Governor string `json:"governor,omitempty"`
}

type sensor struct {
	Name    string  `json:"nome"`
	Celsius float64 `json:"celsius"`
}

type memoryInfo struct {
	TotalBytes     int64 `json:"totalBytes"`
	AvailableBytes int64 `json:"disponivelBytes"`
	SwapTotalBytes int64 `json:"swapTotalBytes"`
	SwapUsedBytes  int64 `json:"swapUsadaBytes"`
	// LimitBytes is the container's memory ceiling (cgroup). It disappears
	// without a limit.
	LimitBytes int64 `json:"limiteBytes,omitempty"`
	// OOMKills counts the processes the kernel killed for lack of memory since
	// the MACHINE booted, and not only in the container: when the victim is
	// dwnvr itself, Docker starts a new container and the old cgroup's counter
	// goes away with it. The machine's counter survives, and it is what
	// explains a "dwnvr up for 5 min" on a machine that has been on for days.
	OOMKills *int64 `json:"oomKills,omitempty"`
}

// pressure is one line of PSI (/proc/pressure/*): the fraction of time, in %,
// during which some task (some) or all of them (full) were stalled waiting for
// the resource.
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
	// Type, Origin and Options come from the mount that contains the path:
	// "ext4", "/dev/sda1", "rw,noatime". They disappear if mountinfo is not
	// readable.
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
	// URL is the address dwnvr actually uses, without user and password.
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
	// Total is how many warnings and errors there were since dwnvr started;
	// Lines, only the most recent ones.
	Total int64         `json:"total"`
	Lines []logbuf.Line `json:"linhas"`
}

// recentes is what the log handler in main.go offers. It goes through an
// interface, and not through one more parameter of New, because the logger
// already arrives here: it is enough to ask it.
type recent interface {
	Recent() ([]logbuf.Line, int64)
}

func (s *Server) handleServerDiagnostics(w http.ResponseWriter, r *http.Request) {
	f := systemSources
	now := time.Now()
	sigla, offset := now.Zone()
	resp := serverInfo{
		CollectedAt: collection{At: now.Format(time.RFC3339), Abbreviation: sigla, OffsetSeconds: offset, Zone: localZone()},
		Machine:     collectMachine(f),
		Process:     collectProcess(f, s.startedAt),
		Memory:      collectMemory(f),
		Pressure:    collectPressure(f),
		Storage:     collectMount(f, s.cfg.Storage.Root),
		SMTP:        smtpInDiagnostics(s.cfg.SMTP),
		Alerts:      notifiersInDiagnostics(s.mgr.Alerts()),
	}

	// The probes wait on the outside - disk and network - and together they can
	// add up to a few seconds on a bad server. In parallel, the screen waits
	// for the worst of them, not the sum.
	var wg sync.WaitGroup
	wg.Go(func() { resp.Storage.Write = s.testWrite() })
	wg.Go(func() { resp.Go2RTC = s.probeGo2RTC(r.Context()) })
	if s.cfg.Detector.URL != "" {
		wg.Go(func() { resp.Detector = s.probeDetector(r.Context()) })
	}
	wg.Wait()

	if h, ok := s.log.Handler().(recent); ok {
		lines, total := h.Recent()
		resp.Log = &logInfo{Total: total, Lines: lines}
	}

	writeJSON(w, resp)
}

// notifiersInDiagnostics is nil without a Dispatcher, which is only born with
// the first camera that turns on the alert, and without a notifier.
func notifiersInDiagnostics(c *alert.Dispatcher) []alertNotifierInfo {
	if c == nil {
		return nil
	}
	var out []alertNotifierInfo
	for _, m := range c.NotifierStates() {
		info := alertNotifierInfo{Name: m.Name, Sent: m.Sent, Failures: m.Failures, Error: m.Err}
		if !m.Last.IsZero() {
			info.Last = &m.Last
		}
		out = append(out, info)
	}
	return out
}

// ---- machine ---------------------------------------------------------------

func collectMachine(f sources) machineInfo {
	m := machineInfo{
		// The device tree ends the text with a NUL, which TrimSpace does not
		// remove.
		Board:        strings.TrimRight(readText(filepath.Join(f.proc, "device-tree/model")), "\x00"),
		Processor:    readProcessor(f.proc),
		Kernel:       readText(filepath.Join(f.proc, "sys/kernel/osrelease")),
		Cores:        runtime.NumCPU(),
		AllowedCores: cgroupCores(f),
	}
	if b, err := os.ReadFile(filepath.Join(f.proc, "loadavg")); err == nil {
		m.Load = readLoad(string(b))
	}
	m.Sensores = readSensors(f.sys)

	cpus, _ := filepath.Glob(filepath.Join(f.sys, "devices/system/cpu/cpu[0-9]*/cpufreq"))
	for _, d := range cpus {
		if khz := readInt(filepath.Join(d, "scaling_cur_freq")); khz/1000 > int64(m.MHz) {
			m.MHz = int(khz / 1000)
		}
		if khz := readInt(filepath.Join(d, "cpuinfo_max_freq")); khz/1000 > int64(m.MHzMax) {
			m.MHzMax = int(khz / 1000)
		}
		if m.Governor == "" {
			m.Governor = readText(filepath.Join(d, "scaling_governor"))
		}
	}
	return m
}

// readProcessor returns the "model name" of the first core. On 64-bit ARM the
// field does not exist - only codes like "CPU part : 0xd03" -, and then what
// identifies the machine is the board.
func readProcessor(proc string) string {
	b, err := os.ReadFile(filepath.Join(proc, "cpuinfo"))
	if err != nil {
		return ""
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), ":")
		if ok && strings.TrimSpace(key) == "model name" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// readLoad reads "0.52 0.40 0.31 1/123 4567".
func readLoad(s string) []float64 {
	fields := strings.Fields(s)
	if len(fields) < 3 {
		return nil
	}
	out := make([]float64, 3)
	for i := range out {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return nil
		}
		out[i] = v
	}
	return out
}

// lerSensores reads the kernel's thermal zones. On an ARM board they have names
// like "cpu_thermal" and "gpu_thermal"; on a PC, "x86_pkg_temp" and "acpitz". A
// zone that does not respond (some return EINVAL with the sensor off) is left
// out.
//
// The name is not unique: a PC can have two "acpitz" zones. A repeated name
// gets the zone number ("acpitz 0", "acpitz 1"), otherwise the two lines on the
// screen and in "copy" are indistinguishable.
func readSensors(sys string) []sensor {
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
		name := readText(filepath.Join(z, "type"))
		if name == "" {
			name = filepath.Base(z)
		}
		out = append(out, sensor{Name: name, Celsius: float64(milli/100) / 10})
		numbers = append(numbers, strings.TrimPrefix(filepath.Base(z), "thermal_zone"))
		times[name]++
	}
	for i := range out {
		if times[out[i].Name] > 1 {
			out[i].Name += " " + numbers[i]
		}
	}
	// Hottest first: it is the one the screen highlights.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Celsius > out[j].Celsius })
	return out
}

// ---- memory ----------------------------------------------------------------

func collectMemory(f sources) *memoryInfo {
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

// readMeminfo reads "MemTotal:  1012345 kB" into key -> kB.
func readMeminfo(b []byte) map[string]int64 {
	out := map[string]int64{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		key, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		if v, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
			out[key] = v
		}
	}
	return out
}

// readField finds "<name> <number>" in a file with one key per line, like
// /proc/vmstat.
func readField(b []byte, name string) (int64, bool) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && fields[0] == name {
			v, err := strconv.ParseInt(fields[1], 10, 64)
			return v, err == nil
		}
	}
	return 0, false
}

// fromCgroup reads a cgroup v2 file of this process. The path comes from
// /proc/self/cgroup ("0::/path"); inside a container with its own cgroup
// namespace it is "/", and the file is at the root of /sys/fs/cgroup. Empty
// without cgroup v2 or without the file.
func fromCgroup(f sources, name string) string {
	b, err := os.ReadFile(filepath.Join(f.proc, "self/cgroup"))
	if err != nil {
		return ""
	}
	for l := range strings.SplitSeq(string(b), "\n") {
		if filePath, ok := strings.CutPrefix(l, "0::"); ok {
			return readText(filepath.Join(f.sys, "fs/cgroup", filePath, name))
		}
	}
	return ""
}

// cgroupLimit returns the memory ceiling of this process's cgroup, or 0 without
// a ceiling. v1 stays as a fallback for old kernels.
func cgroupLimit(f sources) int64 {
	if v := fromCgroup(f, "memory.max"); v != "" && v != "max" {
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	// In v1, "no limit" is a huge number (PAGE_COUNTER_MAX in bytes), and not a
	// word. Above 1 PiB, it is treated as no limit.
	if n := readInt(filepath.Join(f.sys, "fs/cgroup/memory/memory.limit_in_bytes")); n > 0 && n < 1<<50 {
		return n
	}
	return 0
}

// cgroupCores returns the container's CPU ceiling in cores, or 0 without a
// ceiling. The v2 cpu.max is "<quota> <period>" in microseconds, or "max
// <period>" without a ceiling: Docker's `--cpus 1.5` becomes "150000 100000".
// In v1, the quota is -1 without a ceiling.
func cgroupCores(f sources) float64 {
	var quota, period int64
	if fields := strings.Fields(fromCgroup(f, "cpu.max")); len(fields) == 2 {
		quota, _ = strconv.ParseInt(fields[0], 10, 64)
		period, _ = strconv.ParseInt(fields[1], 10, 64)
	} else {
		quota = readInt(filepath.Join(f.sys, "fs/cgroup/cpu/cpu.cfs_quota_us"))
		period = readInt(filepath.Join(f.sys, "fs/cgroup/cpu/cpu.cfs_period_us"))
	}
	if quota <= 0 || period <= 0 {
		return 0
	}
	return float64(quota) / float64(period)
}

// ---- pressure --------------------------------------------------------------

// collectPressure reads the PSI for CPU, memory and disk. A kernel without PSI
// (before 4.20, or built without it) has no /proc/pressure, and the whole field
// disappears.
func collectPressure(f sources) map[string]pressure {
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

// readPressure reads the two PSI lines:
//
//	some avg10=1.53 avg60=0.87 avg300=0.26 total=123456
//	full avg10=0.00 avg60=0.00 avg300=0.00 total=0
func readPressure(s string) pressure {
	var p pressure
	for l := range strings.SplitSeq(s, "\n") {
		fields := strings.Fields(l)
		if len(fields) < 4 {
			continue
		}
		m := &medias{}
		for _, c := range fields[1:] {
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
		switch fields[0] {
		case "some":
			p.Some = m
		case "full":
			p.Full = m
		}
	}
	return p
}

// ---- storage ---------------------------------------------------------------

// collectMount finds out which mount the storage is on. Filesystem and device
// explain a lot: exfat and ntfs run via FUSE, slow and CPU-hungry; /dev/mmcblk*
// is the SD card, which is what usually fails. And a faulty card is remounted
// read-only by the kernel without telling anyone - dwnvr simply stops being
// able to record.
func collectMount(f sources, root string) storageInfo {
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

// mountOf finds in mountinfo the most specific mount that contains the path.
// The format of each line is
//
//	36 35 98:0 /mnt1 /mnt/parent rw,noatime master:1 - ext3 /dev/root rw,errors=continue
//
// with a variable number of optional fields before the "-".
func mountOf(b []byte, filePath string) (mount, bool) {
	var best mount
	found := false
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		sep := -1
		for i, c := range fields {
			if c == "-" {
				sep = i
				break
			}
		}
		if sep < 6 || len(fields) < sep+3 {
			continue
		}
		m := mount{
			point:   unescape(fields[4]),
			options: fields[5],
			kind:    fields[sep+1],
			origin:  unescape(fields[sep+2]),
		}
		if len(fields) > sep+3 {
			m.superOptions = fields[sep+3]
		}
		if !contains(m.point, filePath) {
			continue
		}
		// >= and not >: mounts stacked on the same point appear in the order
		// they were made, and the last one is the one that counts.
		if !found || len(m.point) >= len(best.point) {
			best, found = m, true
		}
	}
	return best, found
}

func contains(point, filePath string) bool {
	return point == "/" || filePath == point || strings.HasPrefix(filePath, point+"/")
}

func hasOption(options, name string) bool {
	return slices.Contains(strings.Split(options, ","), name)
}

// unescape undoes the \040 (space) and friends with which the kernel writes
// paths in mountinfo.
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

// How long the write test waits before giving up and saying the disk did not
// respond. An fsync of 4 KB takes milliseconds even on a modest SD card; going
// past that is a sick or swamped disk.
const writeDeadline = 5 * time.Second

// testWrite proves the storage accepts writes NOW: it creates a file, writes 4
// KB, forces it to disk and deletes it. The fsync time is the number that
// matters - an SD card at the end of its life still accepts writes, only in
// seconds.
//
// A stuck disk holds the fsync for an indefinite time, and nothing interrupts
// an fsync. That is why the test runs on the side, the response gives up at the
// deadline, and a test still stuck makes the following requests answer exactly
// that instead of piling more files and more goroutines onto the disk that no
// longer responds.
//
// The failure goes to the log only when it changes. With the card open the test
// runs every 15 s, and a warning on every round would fill in minutes the 50
// lines the screen shows, pushing out precisely the error that explained the
// disk.
func (s *Server) testWrite() write {
	if !s.writing.CompareAndSwap(false, true) {
		return write{Error: "o teste anterior ainda não terminou: o disco não está respondendo"}
	}
	ready := make(chan write, 1)
	go func() {
		defer s.writing.Store(false)
		start := time.Now()
		err := writeTest(s.cfg.Storage.Root)
		ms := time.Since(start).Milliseconds()
		if err != nil {
			if err.Error() != s.writeErr {
				s.log.Warn("storage write test failed", "erro", err)
			}
			s.writeErr = err.Error()
			ready <- write{Ms: ms, Error: writeReason(err)}
			return
		}
		s.writeErr = ""
		ready <- write{OK: true, Ms: ms}
	}()
	select {
	case e := <-ready:
		return e
	case <-time.After(writeDeadline):
		return write{Ms: writeDeadline.Milliseconds(), Error: "o disco não respondeu em " + writeDeadline.String()}
	}
}

// testFile is always the same, and not a random name per round: if dwnvr dies
// between creating and deleting it, at most this file is left over, and the
// next test reuses and deletes it. The leading dot hides it from a plain ls and
// it never looks like a camera; store.Orphans only looks at directories.
const testFile = ".dwnvr-teste"

func writeTest(root string) (err error) {
	// One test at a time, guaranteed by the writing flag: the fixed name cannot
	// be contended.
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

// writeReason translates the errors that have a known cause. The rest goes
// generic to the screen, with the detail in the log - as fail() does with a
// filesystem error.
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
		// Same exposure as go2rtcError in /api/cameras: the network error is
		// the diagnosis, and the URL in it carries no password (the client
		// sends Basic Auth in a header).
		info.Error = err.Error()
		return info
	}
	info.OK, info.Version = true, v
	return info
}

// ---- object detector -------------------------------------------------------

// probeDetector asks the object detector which model it loaded. The queue in
// /api/health already shows the object detector down, but only when some camera
// sends an inference: with no motion on any camera, the outage goes unnoticed.
// This question answers at any time.
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

// ---- utilities -------------------------------------------------------------

// withoutCredential strips the "user:password@" from a URL before it goes to
// the screen. dwnvr sends the go2rtc credential in a header, but nothing stops
// someone from writing it directly in the URL in dwnvr.yaml.
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

func readInt(filePath string) int64 {
	n, _ := strconv.ParseInt(readText(filePath), 10, 64)
	return n
}
