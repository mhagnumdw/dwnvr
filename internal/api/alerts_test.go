package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/alert"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/recorder"
)

// alertEmailDe is the alertEmail of the admin's /api/cameras, with go2rtc down:
// it comes anyway.
func alertEmailOf(t *testing.T, s *Server) string {
	t.Helper()
	s.client = go2rtc.New(config.Go2RTC{URL: "http://127.0.0.1:1"})
	return string(request(t, s.requireAuth(s.handleCameras), "/api/cameras")["alertEmail"])
}

// request calls the handler and returns the body as a map of raw JSON.
func request(t *testing.T, h http.HandlerFunc, route string) map[string]json.RawMessage {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, route, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: HTTP %d: %s", route, rec.Code, rec.Body.String())
	}
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

// /api/health only talks about alerts after some camera turns one on, and then
// carries only the total.
func TestAlertsInHealth(t *testing.T) {
	s, _ := testServer(t)
	if _, ok := request(t, s.handleHealth, "/api/health")["alerts"]; ok {
		t.Fatal("/api/health talked about alerts without a camera with alert")
	}

	// A Manager with an object detector, which just does not start: without it,
	// the alert is not born.
	s.cfg.Detector.URL = "http://127.0.0.1:1"
	s.mgr = recorder.NewManager(s.cfg, nil, s.store, s.log)
	yes := true
	s.mgr.Set(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, Detect: &yes,
		Alert: &config.CameraAlert{Enabled: true, Families: []detect.Family{detect.Person}}})
	s.mgr.Alerts().Receive(alert.Alert{Camera: "cam_teste", AtMs: 1000,
		Objects: []alert.Object{{Family: "pessoa", Class: "person", Score: 0.87}}})

	if got := string(request(t, s.handleHealth, "/api/health")["alerts"]); got != `{"total":1}` {
		t.Errorf("alerts no /api/health: %s", got)
	}
}

// With a drop episode in effect, /api/health carries the alerts that did not go
// out through the notifiers, by reason, with the ceiling and since when.
func TestDropInHealth(t *testing.T) {
	s, _ := testServer(t)
	s.cfg.Detector.URL = "http://127.0.0.1:1"
	// An email notifier for a server that does not exist: the ceiling applies
	// at the entry, before any send.
	s.cfg.SMTP = config.SMTP{Host: "127.0.0.1", Port: 1, Security: "none", From: "dwnvr@exemplo.com"}
	s.cfg.Alerts.Email.To = []string{"fulano@exemplo.com"}
	s.mgr = recorder.NewManager(s.cfg, nil, s.store, s.log)
	yes := true
	s.mgr.Set(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, Detect: &yes,
		Alert: &config.CameraAlert{Enabled: true, Families: []detect.Family{detect.Person}}})
	for range alert.AlertBurst + 2 {
		s.mgr.Alerts().Receive(alert.Alert{Camera: "cam_teste", AtMs: 1000,
			Objects: []alert.Object{{Family: "pessoa", Class: "person", Score: 0.87}}})
	}

	var alerts struct {
		Dropped *struct {
			QueueFull, OverLimit, LimitPerHour int
			Since, LastAt                      string
		}
	}
	if err := json.Unmarshal(request(t, s.handleHealth, "/api/health")["alerts"], &alerts); err != nil {
		t.Fatal(err)
	}
	d := alerts.Dropped
	if d == nil || d.OverLimit != 2 || d.QueueFull != 0 || d.LimitPerHour != alert.AlertsPerHour ||
		d.Since == "" || d.LastAt == "" {
		t.Errorf("dropped no /api/health: %+v", d)
	}
}

// With the notifier failing, /api/health carries the since-when and the error,
// for the red Warning, and /api/health/servidor, the notifier's numbers.
func TestFailingNotifierInHealth(t *testing.T) {
	s, _ := testServer(t)
	s.cfg.Detector.URL = "http://127.0.0.1:1"
	// An email server that does not exist: the send fails on connect.
	s.cfg.SMTP = config.SMTP{Host: "127.0.0.1", Port: 1, Security: "none", From: "dwnvr@exemplo.com"}
	s.cfg.Alerts.Email.To = []string{"fulano@exemplo.com"}
	s.mgr = recorder.NewManager(s.cfg, nil, s.store, s.log)
	yes := true
	s.mgr.Set(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, Detect: &yes,
		Alert: &config.CameraAlert{Enabled: true, Families: []detect.Family{detect.Person}}})

	// The notifier has not tried yet: nothing failing, and the numbers zeroed.
	if got := string(request(t, s.handleHealth, "/api/health")["alerts"]); got != `{"total":0}` {
		t.Errorf("alerts before the send: %s", got)
	}
	if got := string(request(t, s.handleServerDiagnostics, "/api/health/servidor")["alertas"]); got != `[{"nome":"email","enviados":0,"falhas":0}]` {
		t.Errorf("alerts in diagnostics before the send: %s", got)
	}

	// Without the Manager's Start, the send goroutine is the test's.
	go s.mgr.Alerts().Run(t.Context())
	s.mgr.Alerts().Receive(alert.Alert{Camera: "cam_teste", AtMs: 1000,
		Objects: []alert.Object{{Family: "pessoa", Class: "person", Score: 0.87}}})

	var alerts struct {
		Failing []struct{ Name, Since, Error string }
	}
	for range 500 {
		if err := json.Unmarshal(request(t, s.handleHealth, "/api/health")["alerts"], &alerts); err != nil {
			t.Fatal(err)
		}
		if len(alerts.Failing) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(alerts.Failing) != 1 {
		t.Fatalf("failing no /api/health: %+v", alerts.Failing)
	}
	if f := alerts.Failing[0]; f.Name != "email" || f.Since == "" || f.Error == "" {
		t.Errorf("failing no /api/health: %+v", f)
	}

	var notifiers []alertNotifierInfo
	if err := json.Unmarshal(request(t, s.handleServerDiagnostics, "/api/health/servidor")["alertas"], &notifiers); err != nil {
		t.Fatal(err)
	}
	if len(notifiers) != 1 || notifiers[0].Failures != 1 || notifiers[0].Sent != 0 || notifiers[0].Last == nil ||
		notifiers[0].Error != alerts.Failing[0].Error {
		t.Errorf("alerts in diagnostics: %+v", notifiers)
	}

	var byEmail struct{ FailingSince, Error string }
	if err := json.Unmarshal([]byte(alertEmailOf(t, s)), &byEmail); err != nil {
		t.Fatal(err)
	}
	if byEmail.FailingSince == "" || byEmail.Error != alerts.Failing[0].Error {
		t.Errorf("alertEmail in /api/cameras with the notifier failing: %+v", byEmail)
	}
}

// The Alert tab says where the alert goes: without the notifier, the sections
// missing from dwnvr.yaml, with the reason the smtp was rejected; with it, the
// list, even before some camera turns on the alert.
func TestEmailAlertInCameras(t *testing.T) {
	s, _ := testServer(t)
	if got := alertEmailOf(t, s); got != `{"missing":["alerts.email.to","smtp"]}` {
		t.Errorf("alertEmail without the notifier: %s", got)
	}

	s.cfg.SMTP = config.SMTP{Host: "smtp.exemplo.com", Problem: "smtp.from vazio"}
	s.cfg.Alerts.Email.To = []string{"fulano@exemplo.com"}
	if got := alertEmailOf(t, s); got != `{"missing":["smtp"],"smtpError":"smtp.from vazio"}` {
		t.Errorf("alertEmail with the smtp rejected: %s", got)
	}

	s.cfg.SMTP = config.SMTP{Host: "127.0.0.1", Port: 1, Security: "none", From: "dwnvr@exemplo.com"}
	s.cfg.Alerts.Email.To = []string{"fulano@exemplo.com", "ciclana@exemplo.com"}
	if got := alertEmailOf(t, s); got != `{"to":["fulano@exemplo.com","ciclana@exemplo.com"]}` {
		t.Errorf("alertEmail with the notifier: %s", got)
	}
}
