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

// The "dwnvr process" group of the "This server" card: dwnvr seen by itself,
// next to the whole machine. It is what separates "the machine is struggling"
// from "it is dwnvr that makes the machine struggle". Like the rest of the
// card, it comes from /proc, and the field disappears when its source does not
// exist.
//
// ReadMemStats (heap, GCs) is left out: it stops the whole program for an
// instant, and the memory that weighs on the machine is already in MemoryBytes.

type processInfo struct {
	// CPUMs is the CPU time dwnvr has spent since it started, adding user and
	// kernel; AliveMs, how long it has been up. The screen derives the
	// percentage from the difference between two readings - current CPU, and
	// not the average over days -, and falls back to the average since startup
	// on the first reading. It goes like this, and not already as a percentage,
	// so the server does not keep the previous reading of each open screen.
	// Pointer because zero is a measurement: a freshly started dwnvr has not
	// spent a CPU tick yet.
	CPUMs   *int64 `json:"cpuMs,omitempty"`
	AliveMs int64  `json:"vivoMs"`
	// MemoryBytes is the physical memory used by dwnvr (VmRSS). It is the
	// number that hits the container limit, and not the cgroup's
	// memory.current: that one includes the cache of the recorded videos, which
	// the kernel gives back when it needs to, and on a machine that records all
	// day it lives close to the ceiling.
	MemoryBytes int64 `json:"memoriaBytes,omitempty"`
	// Goroutines has no fixed ceiling - it depends on the number of cameras and
	// on who is watching. The sign of a leak is growing from one reading to the
	// next.
	Goroutines int `json:"goroutines"`
	// OpenFiles counts the open descriptors (files and connections), and
	// FileLimit is the process ceiling. Hitting it makes recording, which opens
	// segments, and the network, which opens sockets, fail at the same time.
	OpenFiles int   `json:"arquivosAbertos,omitempty"`
	FileLimit int64 `json:"limiteDeArquivos,omitempty"`
	// UID and GID dwnvr runs as. If they are wrong, the storage and
	// cameras.json do not accept writes; see DWNVR_UID and DWNVR_GID in
	// docker-compose.yml.
	UID *int `json:"uid,omitempty"`
	GID *int `json:"gid,omitempty"`
	// Go and Architecture say which binary is actually running: "go1.27.1" and
	// "linux/arm64".
	Go           string `json:"go"`
	Architecture string `json:"arquitetura"`
}

// ticksPerSecond is USER_HZ, the unit of the times in /proc/self/stat. It is
// fixed at 100 in the Linux ABI on every architecture dwnvr runs on, and the
// sysconf that would read it needs C.
const ticksPerSecond = 100

func collectProcess(f sources, since time.Time) processInfo {
	p := processInfo{
		AliveMs:      time.Since(since).Milliseconds(),
		Goroutines:   runtime.NumGoroutine(),
		Go:           runtime.Version(),
		Architecture: architecture(),
	}
	if b, err := os.ReadFile(filepath.Join(f.proc, "self/stat")); err == nil {
		if ms, ok := readCPUTime(string(b)); ok {
			p.CPUMs = &ms
		}
	}
	if b, err := os.ReadFile(filepath.Join(f.proc, "self/status")); err == nil {
		p.MemoryBytes, p.UID, p.GID = readStatus(b)
	}
	if fds, err := os.ReadDir(filepath.Join(f.proc, "self/fd")); err == nil {
		p.OpenFiles = len(fds)
	}
	if b, err := os.ReadFile(filepath.Join(f.proc, "self/limits")); err == nil {
		p.FileLimit = readFileLimit(b)
	}
	return p
}

// readCPUTime adds utime and stime from /proc/self/stat, in ms. The second
// field is the program name in parentheses, and it can have spaces and
// parentheses inside; the field count starts after the LAST ")". From there,
// utime and stime are the 12th and the 13th.
//
//	1234 (dwnvr) S 1 1234 1234 0 -1 4194560 2200 0 0 0 44346 36809 0 0 20 0 15 0 ...
func readCPUTime(s string) (int64, bool) {
	_, rest, ok := strings.Cut(s[strings.LastIndexByte(s, ')')+1:], " ")
	if !ok {
		return 0, false
	}
	fields := strings.Fields(rest)
	if len(fields) < 13 {
		return 0, false
	}
	utime, err1 := strconv.ParseInt(fields[11], 10, 64)
	stime, err2 := strconv.ParseInt(fields[12], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return (utime + stime) * 1000 / ticksPerSecond, true
}

// readStatus takes from /proc/self/status the physical memory (VmRSS, in kB) and
// the real UID and GID, which are the first of the four numbers on each line.
func readStatus(b []byte) (rss int64, uid, gid *int) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		key, rest, ok := strings.Cut(sc.Text(), ":")
		fields := strings.Fields(rest)
		if !ok || len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			continue
		}
		switch key {
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

// readFileLimit finds the soft limit, which is the one that counts, on the
// /proc/self/limits line:
//
//	Max open files            524287               524288               files
//
// "unlimited" and a missing line give 0, and the limit disappears from the
// response.
func readFileLimit(b []byte) int64 {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(sc.Text(), "Max open files"); ok {
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				n, _ := strconv.ParseInt(fields[0], 10, 64)
				return n
			}
		}
	}
	return 0
}

// architecture is the binary's GOOS/GOARCH, with the 32-bit ARM version when
// there is one: "linux/arm/v7" and "linux/arm/v6" run on different boards.
func architecture() string {
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
