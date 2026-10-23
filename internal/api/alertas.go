package api

import (
	"net/http"

	"github.com/mhagnumdw/dwnvr/internal/alerta"
)

// listaDeAlertas é o corpo do GET /api/alertas: o total desde que o dwnvr
// subiu e os últimos alertas, do mais novo para o mais velho.
type listaDeAlertas struct {
	Total  int64           `json:"total"`
	Recent []alerta.Alerta `json:"recent"`
}

// handleAlertas alimenta o card Alertas do Diagnóstico. É um endpoint à
// parte, e não mais um campo do /api/health, porque a lista pesa: o
// /api/health é relido em ciclo, pelo Diagnóstico e pelo sino de toda tela,
// e leva só o total. A tela pede a lista quando ele muda.
func (s *Server) handleAlertas(w http.ResponseWriter, r *http.Request) {
	resp := listaDeAlertas{Recent: []alerta.Alerta{}}
	if c := s.mgr.Alertas(); c != nil {
		resp.Total, resp.Recent = c.Total(), c.Recentes()
	}
	writeJSON(w, resp)
}
