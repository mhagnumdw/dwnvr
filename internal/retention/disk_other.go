//go:build !unix

package retention

import "errors"

// dwnvr is made to run on Linux (Docker or systemd). These stubs only exist so
// `go build ./...` and the editor tooling work on other systems during
// development.

var errUnsupported = errors.New("retention: disk queries are only supported on unix systems")

func FreeBytes(string) (int64, error)  { return 0, errUnsupported }
func TotalBytes(string) (int64, error) { return 0, errUnsupported }
