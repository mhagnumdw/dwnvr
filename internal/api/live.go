package api

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// liveProxy forwards the live view signaling to go2rtc.
//
// dwnvr does not touch the live media: the WebRTC signaling and the MSE fMP4 go
// straight through to go2rtc, which already handles that very well. The proxy
// exists for three practical reasons:
//
//   - a single origin for the browser, which avoids CORS and a second port
//   - the go2rtc credential stays on the server, never in the browser
//   - live now respects the same session as the rest of the interface
//
// In WebRTC only the signaling crosses here; the media goes straight from the
// browser to go2rtc over UDP 8555, so the proxy is not in the path of the video
// packets.
//
// Only the player's websocket goes through, always to go2rtc's /api/ws: its API
// has config and restart, and the dwnvr session must not reach anything beyond
// what the screen uses.
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
			// go2rtc validates the origin on some routes; presenting itself as
			// go2rtc avoids a cross-origin rejection.
			pr.Out.Host = target.Host
			pr.Out.Header.Del("Origin")
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			s.log.Warn("go2rtc proxy failed", "path", r.URL.Path, "err", err)
			writeError(w, http.StatusBadGateway, "go2rtc indisponível")
		},
	}
	return proxy
}
