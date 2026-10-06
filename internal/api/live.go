package api

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// liveProxy repassa ao go2rtc a sinalização da visualização ao vivo.
//
// O dwnvr não toca na mídia do live: a sinalização WebRTC e o fMP4 do MSE
// passam direto para o go2rtc, que já resolve isso muito bem. O proxy existe
// por três motivos práticos:
//
//   - uma origem só para o navegador, o que evita CORS e uma segunda porta
//   - a credencial do go2rtc fica no servidor, nunca no navegador
//   - o live passa a respeitar a mesma sessão do resto da interface
//
// No WebRTC apenas a sinalização atravessa aqui; a mídia vai direto do
// navegador ao go2rtc pela UDP 8555, então o proxy não entra no caminho dos
// pacotes de vídeo.
//
// Só o websocket do player passa, sempre para o /api/ws do go2rtc: a API dele
// tem config e restart, e a sessão do dwnvr não deve alcançar nada além do que
// a tela usa.
func (s *Server) liveProxy() http.Handler {
	target, err := url.Parse(s.client.BaseURL)
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusInternalServerError, "URL do go2rtc inválida")
		})
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			// /api/live/ws?src=X  ->  /api/ws?src=X
			pr.Out.URL.Path = "/api/ws"
			pr.Out.URL.RawQuery = pr.In.URL.RawQuery

			if s.client.Username != "" || s.client.Password != "" {
				pr.Out.SetBasicAuth(s.client.Username, s.client.Password)
			}
			// O go2rtc valida a origem em algumas rotas; apresentar-se como
			// ele mesmo evita recusa por origem cruzada.
			pr.Out.Host = target.Host
			pr.Out.Header.Del("Origin")
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.log.Warn("proxy do go2rtc falhou", "caminho", r.URL.Path, "erro", err)
			writeError(w, http.StatusBadGateway, "go2rtc indisponível")
		},
	}
	return proxy
}
