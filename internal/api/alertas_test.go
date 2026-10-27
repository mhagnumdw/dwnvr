package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/alerta"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/recorder"
)

// pede chama o handler e devolve o corpo como mapa de JSON cru.
func pede(t *testing.T, h http.HandlerFunc, rota string) map[string]json.RawMessage {
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
func TestAlertasNoHealth(t *testing.T) {
	s, _ := testServer(t)
	if _, ok := pede(t, s.handleHealth, "/api/health")["alerts"]; ok {
		t.Fatal("o /api/health falou de alerta sem câmera com alerta")
	}

	// Um Manager com detector de objetos, que só não sobe: sem ele, o alerta
	// não nasce.
	s.cfg.Detector.URL = "http://127.0.0.1:1"
	s.mgr = recorder.NewManager(s.cfg, nil, s.store, s.log)
	sim := true
	s.mgr.Set(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, Detect: &sim,
		Alert: &config.AlertaDaCamera{Enabled: true, Families: []detect.Familia{detect.Pessoa}}})
	s.mgr.Alertas().Recebe(alerta.Alerta{Camera: "cam_teste", InstanteMs: 1000,
		Objetos: []alerta.Objeto{{Familia: "pessoa", Classe: "person", Score: 0.87}}})

	if got := string(pede(t, s.handleHealth, "/api/health")["alerts"]); got != `{"total":1}` {
		t.Errorf("alerts no /api/health: %s", got)
	}
}

// Com um episódio de descarte valendo, o /api/health leva os alertas que não
// saíram pelos meios, por motivo, com o teto e o desde quando.
func TestDescarteNoHealth(t *testing.T) {
	s, _ := testServer(t)
	s.cfg.Detector.URL = "http://127.0.0.1:1"
	// Um meio de e-mail para um servidor que não existe: o teto vale na
	// entrada, antes de qualquer envio.
	s.cfg.SMTP = config.SMTP{Host: "127.0.0.1", Port: 1, Security: "none", From: "dwnvr@exemplo.com"}
	s.cfg.Alerts.Email.To = []string{"fulano@exemplo.com"}
	s.mgr = recorder.NewManager(s.cfg, nil, s.store, s.log)
	sim := true
	s.mgr.Set(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, Detect: &sim,
		Alert: &config.AlertaDaCamera{Enabled: true, Families: []detect.Familia{detect.Pessoa}}})
	for range alerta.RajadaDeAlertas + 2 {
		s.mgr.Alertas().Recebe(alerta.Alerta{Camera: "cam_teste", InstanteMs: 1000,
			Objetos: []alerta.Objeto{{Familia: "pessoa", Classe: "person", Score: 0.87}}})
	}

	var alerts struct {
		Dropped *struct {
			QueueFull, OverLimit, LimitPerHour int
			Since, LastAt                      string
		}
	}
	if err := json.Unmarshal(pede(t, s.handleHealth, "/api/health")["alerts"], &alerts); err != nil {
		t.Fatal(err)
	}
	d := alerts.Dropped
	if d == nil || d.OverLimit != 2 || d.QueueFull != 0 || d.LimitPerHour != alerta.AlertasPorHora ||
		d.Since == "" || d.LastAt == "" {
		t.Errorf("dropped no /api/health: %+v", d)
	}
}
