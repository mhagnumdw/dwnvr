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

// alertEmailDe é o alertEmail do /api/cameras do admin, com o go2rtc fora do
// ar: ele vem mesmo assim.
func alertEmailDe(t *testing.T, s *Server) string {
	t.Helper()
	s.client = go2rtc.New(config.Go2RTC{URL: "http://127.0.0.1:1"})
	return string(request(t, s.requireAuth(s.handleCameras), "/api/cameras")["alertEmail"])
}

// pede chama o handler e devolve o corpo como mapa de JSON cru.
func request(t *testing.T, h http.HandlerFunc, rota string) map[string]json.RawMessage {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, rota, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: HTTP %d: %s", rota, rec.Code, rec.Body.String())
	}
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

// O /api/health só fala de alerta depois que alguma câmera o liga, e então
// leva só o total.
func TestAlertsInHealth(t *testing.T) {
	s, _ := testServer(t)
	if _, ok := request(t, s.handleHealth, "/api/health")["alerts"]; ok {
		t.Fatal("o /api/health falou de alerta sem câmera com alerta")
	}

	// Um Manager com detector de objetos, que só não sobe: sem ele, o alerta
	// não nasce.
	s.cfg.Detector.URL = "http://127.0.0.1:1"
	s.mgr = recorder.NewManager(s.cfg, nil, s.store, s.log)
	sim := true
	s.mgr.Set(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, Detect: &sim,
		Alert: &config.CameraAlert{Enabled: true, Families: []detect.Family{detect.Person}}})
	s.mgr.Alerts().Receive(alert.Alert{Camera: "cam_teste", AtMs: 1000,
		Objects: []alert.Object{{Family: "pessoa", Class: "person", Score: 0.87}}})

	if got := string(request(t, s.handleHealth, "/api/health")["alerts"]); got != `{"total":1}` {
		t.Errorf("alerts no /api/health: %s", got)
	}
}

// Com um episódio de descarte valendo, o /api/health leva os alertas que não
// saíram pelos meios, por motivo, com o teto e o desde quando.
func TestDropInHealth(t *testing.T) {
	s, _ := testServer(t)
	s.cfg.Detector.URL = "http://127.0.0.1:1"
	// Um meio de e-mail para um servidor que não existe: o teto vale na
	// entrada, antes de qualquer envio.
	s.cfg.SMTP = config.SMTP{Host: "127.0.0.1", Port: 1, Security: "none", From: "dwnvr@exemplo.com"}
	s.cfg.Alerts.Email.To = []string{"fulano@exemplo.com"}
	s.mgr = recorder.NewManager(s.cfg, nil, s.store, s.log)
	sim := true
	s.mgr.Set(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, Detect: &sim,
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

// Com o meio falhando, o /api/health leva o desde quando e o erro, para o
// Aviso vermelho, e o /api/health/servidor, os números do meio.
func TestFailingNotifierInHealth(t *testing.T) {
	s, _ := testServer(t)
	s.cfg.Detector.URL = "http://127.0.0.1:1"
	// Um servidor de e-mail que não existe: o envio falha na conexão.
	s.cfg.SMTP = config.SMTP{Host: "127.0.0.1", Port: 1, Security: "none", From: "dwnvr@exemplo.com"}
	s.cfg.Alerts.Email.To = []string{"fulano@exemplo.com"}
	s.mgr = recorder.NewManager(s.cfg, nil, s.store, s.log)
	sim := true
	s.mgr.Set(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, Detect: &sim,
		Alert: &config.CameraAlert{Enabled: true, Families: []detect.Family{detect.Person}}})

	// O meio ainda não tentou: nada falhando, e os números zerados.
	if got := string(request(t, s.handleHealth, "/api/health")["alerts"]); got != `{"total":0}` {
		t.Errorf("alerts antes do envio: %s", got)
	}
	if got := string(request(t, s.handleServerDiagnostics, "/api/health/servidor")["alertas"]); got != `[{"nome":"email","enviados":0,"falhas":0}]` {
		t.Errorf("alertas no diagnóstico antes do envio: %s", got)
	}

	// Sem o Start do Manager, a goroutine de envio é a do teste.
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
	if len(notifiers) != 1 || notifiers[0].Failures != 1 || notifiers[0].Sent != 0 || notifiers[0].Ultimo == nil ||
		notifiers[0].Error != alerts.Failing[0].Error {
		t.Errorf("alertas no diagnóstico: %+v", notifiers)
	}

	var byEmail struct{ FailingSince, Error string }
	if err := json.Unmarshal([]byte(alertEmailDe(t, s)), &byEmail); err != nil {
		t.Fatal(err)
	}
	if byEmail.FailingSince == "" || byEmail.Error != alerts.Failing[0].Error {
		t.Errorf("alertEmail no /api/cameras com o meio falhando: %+v", byEmail)
	}
}

// A aba Alerta diz para onde o alerta vai: sem o meio, as seções que faltam
// no dwnvr.yaml, com o motivo da smtp recusada; com ele, a lista, mesmo antes
// de alguma câmera ligar o alerta.
func TestEmailAlertInCameras(t *testing.T) {
	s, _ := testServer(t)
	if got := alertEmailDe(t, s); got != `{"missing":["alerts.email.to","smtp"]}` {
		t.Errorf("alertEmail sem o meio: %s", got)
	}

	s.cfg.SMTP = config.SMTP{Host: "smtp.exemplo.com", Problem: "smtp.from vazio"}
	s.cfg.Alerts.Email.To = []string{"fulano@exemplo.com"}
	if got := alertEmailDe(t, s); got != `{"missing":["smtp"],"smtpError":"smtp.from vazio"}` {
		t.Errorf("alertEmail com a smtp recusada: %s", got)
	}

	s.cfg.SMTP = config.SMTP{Host: "127.0.0.1", Port: 1, Security: "none", From: "dwnvr@exemplo.com"}
	s.cfg.Alerts.Email.To = []string{"fulano@exemplo.com", "ciclana@exemplo.com"}
	if got := alertEmailDe(t, s); got != `{"to":["fulano@exemplo.com","ciclana@exemplo.com"]}` {
		t.Errorf("alertEmail com o meio: %s", got)
	}
}
