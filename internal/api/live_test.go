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
func TestLiveProxyOnlyForwardsTheWebsocket(t *testing.T) {
	var received []string
	var username, password string
	fakeGo2rtc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = append(received, r.Method+" "+r.URL.RequestURI())
		username, password, _ = r.BasicAuth()
	}))
	t.Cleanup(fakeGo2rtc.Close)

	s, _ := testServer(t)
	s.client = go2rtc.New(config.Go2RTC{URL: fakeGo2rtc.URL, Username: "admin", Password: "senha"})
	h := s.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/live/ws?src=cam_frente", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/live/ws: HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if len(received) != 1 || received[0] != "GET /api/ws?src=cam_frente" {
		t.Fatalf("o go2rtc recebeu %v, esperava só GET /api/ws?src=cam_frente", received)
	}
	if username != "admin" || password != "senha" {
		t.Errorf("o go2rtc recebeu a credencial %q/%q", username, password)
	}

	received = nil
	for _, request := range []struct{ method, filePath string }{
		{http.MethodGet, "/api/live/config"},
		{http.MethodPatch, "/api/live/config"},
		{http.MethodPost, "/api/live/restart"},
		{http.MethodGet, "/api/live/streams"},
		{http.MethodPost, "/api/live/ws?src=cam_frente"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(request.method, request.filePath, nil))
		if rec.Code < 400 {
			t.Errorf("%s %s: HTTP %d, esperava recusa", request.method, request.filePath, rec.Code)
		}
	}
	if len(received) != 0 {
		t.Errorf("chegou ao go2rtc o que não é o websocket: %v", received)
	}
}
