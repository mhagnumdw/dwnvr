package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
)

// O proxy do live só leva ao go2rtc o websocket do player. O resto da API dele
// - config, restart, streams - não pode ser alcançado pela sessão do dwnvr.
func TestLiveProxySoRepassaOWebsocket(t *testing.T) {
	var recebidas []string
	var usuario, senha string
	go2rtcFalso := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recebidas = append(recebidas, r.Method+" "+r.URL.RequestURI())
		usuario, senha, _ = r.BasicAuth()
	}))
	t.Cleanup(go2rtcFalso.Close)

	s, _ := testServer(t)
	s.client = go2rtc.New(config.Go2RTC{URL: go2rtcFalso.URL, Username: "admin", Password: "senha"})
	h := s.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/live/ws?src=cam_frente", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/live/ws: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if len(recebidas) != 1 || recebidas[0] != "GET /api/ws?src=cam_frente" {
		t.Fatalf("o go2rtc recebeu %v, esperava só GET /api/ws?src=cam_frente", recebidas)
	}
	if usuario != "admin" || senha != "senha" {
		t.Errorf("o go2rtc recebeu a credencial %q/%q", usuario, senha)
	}

	recebidas = nil
	for _, pedido := range []struct{ metodo, caminho string }{
		{http.MethodGet, "/api/live/config"},
		{http.MethodPatch, "/api/live/config"},
		{http.MethodPost, "/api/live/restart"},
		{http.MethodGet, "/api/live/streams"},
		{http.MethodPost, "/api/live/ws?src=cam_frente"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(pedido.metodo, pedido.caminho, nil))
		if rec.Code < 400 {
			t.Errorf("%s %s: HTTP %d, esperava recusa", pedido.metodo, pedido.caminho, rec.Code)
		}
	}
	if len(recebidas) != 0 {
		t.Errorf("chegou ao go2rtc o que não é o websocket: %v", recebidas)
	}
}
