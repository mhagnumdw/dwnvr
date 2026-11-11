package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
)

// The live proxy only takes the player's websocket to go2rtc. The rest of its
// API - config, restart, streams - cannot be reached by the dwnvr session.
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
		t.Fatalf("go2rtc received %v, expected only GET /api/ws?src=cam_frente", received)
	}
	if username != "admin" || password != "senha" {
		t.Errorf("go2rtc received the credential %q/%q", username, password)
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
			t.Errorf("%s %s: HTTP %d, expected rejection", request.method, request.filePath, rec.Code)
		}
	}
	if len(received) != 0 {
		t.Errorf("what reached go2rtc is not the websocket: %v", received)
	}
}
