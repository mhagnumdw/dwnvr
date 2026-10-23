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
// leva só o total. A lista vem do /api/alertas, com o formato de uma
// detecção.
func TestAlertasNoHealthENaLista(t *testing.T) {
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
	s.mgr.Alertas().Recebe(alerta.Alerta{Camera: "cam_teste", InstanteMs: 1000, QuadroMs: 2400, TemQuadro: true,
		Objetos: []alerta.Objeto{{Familia: "pessoa", Classe: "person", Score: 0.87,
			Caixa: [4]float64{0.1, 0.2, 0.3, 0.6}, Areas: []string{"Portão"}}}})

	if got := string(pede(t, s.handleHealth, "/api/health")["alerts"]); got != `{"total":1}` {
		t.Errorf("alerts no /api/health: %s", got)
	}
	lista := pede(t, s.handleAlertas, "/api/alertas")
	if string(lista["total"]) != "1" {
		t.Errorf("total %s", lista["total"])
	}
	want := `[{"cam":"cam_teste","instanteMs":1000,"quadroMs":2400,"temQuadro":true,"objetos":[` +
		`{"familia":"pessoa","classe":"person","score":0.87,"caixa":[0.1,0.2,0.3,0.6],"areas":["Portão"]}]}]`
	if got := string(lista["recent"]); got != want {
		t.Errorf("recent\n %s\nesperava\n %s", got, want)
	}
}

// Sem alerta, a lista vem vazia, e não nula: a tela não precisa testar.
func TestAlertasSemCentral(t *testing.T) {
	s, _ := testServer(t)
	lista := pede(t, s.handleAlertas, "/api/alertas")
	if string(lista["total"]) != "0" || string(lista["recent"]) != "[]" {
		t.Errorf("lista %s %s", lista["total"], lista["recent"])
	}
}
