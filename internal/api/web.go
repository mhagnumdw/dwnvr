package api

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web
var webFS embed.FS

// webHandler serve a interface embutida no binário.
//
// Embutir com embed.FS mantém a promessa de um binário único: nada de copiar
// uma pasta de assets junto, nada de caminho de instalação para configurar.
func (s *Server) webHandler() http.Handler {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusInternalServerError, "interface indisponível")
		})
	}

	files := http.FileServerFS(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A interface muda a cada versão do binário e é minúscula; revalidar
		// sempre evita a classe de bug em que o navegador serve uma tela velha
		// contra uma API nova.
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}
