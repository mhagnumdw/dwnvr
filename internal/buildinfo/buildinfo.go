// Package buildinfo says which code this binary is running.
//
// It is a package of its own, and not variables in main, because the API
// needs to read it to answer /api/version.
package buildinfo

import (
	"runtime"
	"runtime/debug"
)

// Set at link time, via -ldflags -X, by the Makefile and the Dockerfile.
// They are empty in any other build path - hence the fallback in Get.
var (
	Version = ""
	Commit  = ""
	Date    = ""
)

type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Go      string `json:"go"`
}

// info is computed once: ReadBuildInfo walks the symbol table and has no
// reason to run on every request.
var info = resolve()

// Get returns this binary's identification.
func Get() Info { return info }

// resolve prefers the values injected at link time and, when there are none,
// falls back to the stamp Go itself records when building inside a git
// repository.
//
// That fallback is what makes a plain `go build ./cmd/dwnvr` or `go run` still
// report the right commit, instead of needing the Makefile to tell the truth.
// It doesn't cover the Docker image: there .dockerignore excludes .git/, and
// that is exactly why the Dockerfile passes the values as ARGs.
func resolve() Info {
	out := Info{Version: Version, Commit: Commit, Date: Date, Go: runtime.Version()}

	bi, ok := debug.ReadBuildInfo()
	if ok {
		var revision, time string
		var modified bool
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				revision = s.Value
			case "vcs.time":
				time = s.Value
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
		if out.Commit == "" {
			out.Commit = revision
		}
		if out.Date == "" {
			out.Date = time
		}
		// The same format `git describe --always --dirty` would produce, so the
		// version doesn't change its look depending on who built it.
		if out.Version == "" && revision != "" {
			out.Version = shortHash(revision)
			if modified {
				out.Version += "-dirty"
			}
		}
	}

	if out.Version == "" {
		out.Version = "dev"
	}
	return out
}

func shortHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}
