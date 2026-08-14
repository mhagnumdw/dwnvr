// Package buildinfo diz qual código este binário está executando.
//
// Existe como pacote próprio, e não como variáveis do main, porque a API
// precisa lê-lo para responder /api/version.
package buildinfo

import (
	"runtime"
	"runtime/debug"
)

// Preenchidos na ligação, via -ldflags -X, pelo Makefile e pelo Dockerfile.
// Ficam vazios em qualquer outro caminho de compilação - daí o fallback de Get.
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

// info é calculado uma vez: ReadBuildInfo percorre a tabela de símbolos e não
// tem por que rodar a cada requisição.
var info = resolve()

// Get devolve a identificação deste binário.
func Get() Info { return info }

// resolve prefere os valores injetados na ligação e, quando não há nenhum, cai
// no carimbo que o próprio Go grava ao compilar dentro de um repositório git.
//
// Esse fallback é o que faz um `go build ./cmd/dwnvr` ou um `go run` avulso
// ainda reportarem o commit certo, em vez de exigirem o Makefile para dizer a
// verdade. Ele não cobre a imagem Docker: lá o .dockerignore exclui o .git/, e
// é justamente por isso que o Dockerfile passa os valores por ARG.
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
		// O mesmo formato que `git describe --always --dirty` produziria, para
		// que a versão não mude de cara conforme quem compilou.
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
