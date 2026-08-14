package api

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

// dist é o build da SPA (web/, Svelte + Vite), embutido no binário.
//
// Embutir mantém a promessa de um binário único: nada de copiar uma pasta de
// assets na instalação, nada de caminho para configurar. Como o embed exige que
// os arquivos existam em tempo de compilação, o build da interface é
// versionado - veja web/README.md.
//
//go:embed all:dist
var dist embed.FS

func (s *Server) webHandler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusInternalServerError, "interface indisponível")
		})
	}

	files := http.FileServerFS(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		// Os arquivos gerados pelo Vite têm hash no nome, então nunca mudam de
		// conteúdo e podem ser cacheados para sempre. O index.html é o oposto:
		// ele é quem aponta para os hashes novos depois de uma atualização.
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}

		files.ServeHTTP(w, r)
	})
}
