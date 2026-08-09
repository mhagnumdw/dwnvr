//go:build !unix

package retention

import "errors"

// O dwnvr é feito para rodar em Linux (Docker ou systemd). Estes stubs existem
// só para que `go build ./...` e o tooling do editor funcionem em outros
// sistemas durante o desenvolvimento.

var errUnsupported = errors.New("retention: consulta de disco só é suportada em sistemas unix")

func FreeBytes(string) (int64, error)  { return 0, errUnsupported }
func TotalBytes(string) (int64, error) { return 0, errUnsupported }
