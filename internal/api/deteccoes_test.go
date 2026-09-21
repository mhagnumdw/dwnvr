package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// cenarioDeDeteccoes monta duas câmeras com detecções em dois dias, e devolve
// o meio-dia do segundo dia.
//
//	dia 1 (ontem), 12:00 + N min: cam_teste 0, 2 · cam_b 1
//	dia 2 (hoje),  12:00 + N min: cam_teste 0 (pessoa+veiculo), 3 · cam_b 1 (animal), 3
//
// O minuto 3 de hoje empata nas duas câmeras, no mesmo milissegundo.
func cenarioDeDeteccoes(t *testing.T) (*Server, time.Time) {
	t.Helper()
	s, camTeste := testServer(t)
	s.mgr.Set(config.Camera{ID: "cam_b", Name: "B", Enabled: true})
	camB := s.store.Camera("cam_b")

	hoje := time.Date(2026, 8, 9, 12, 0, 0, 0, time.Local)
	ontem := hoje.AddDate(0, 0, -1)
	for _, cam := range []*store.Camera{camTeste, camB} {
		seed(t, cam, ontem, [2]int64{0, 30_000})
		seed(t, cam, hoje, [2]int64{0, 30_000})
	}

	marca := func(cam *store.Camera, base time.Time, min int64, familia string) {
		t.Helper()
		ev := store.Evento{InstanteMs: base.UnixMilli() + min*60_000, Familia: familia, Classe: "x", Score: 0.9}
		if err := cam.AppendEvento(store.Evento{InstanteMs: ev.InstanteMs}); err != nil { // o onset
			t.Fatal(err)
		}
		if err := cam.AppendEvento(ev); err != nil {
			t.Fatal(err)
		}
	}
	marca(camTeste, ontem, 0, "pessoa")
	marca(camTeste, ontem, 2, "pessoa")
	marca(camB, ontem, 1, "veiculo")
	marca(camTeste, hoje, 0, "pessoa")
	marca(camTeste, hoje, 0, "veiculo")
	marca(camTeste, hoje, 3, "pessoa")
	marca(camB, hoje, 1, "animal")
	marca(camB, hoje, 3, "pessoa")
	return s, hoje
}

func getPagina(t *testing.T, s *Server, query string) paginaDeDeteccoes {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleDeteccoes(rec, httptest.NewRequest(http.MethodGet, "/api/deteccoes?"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var out paginaDeDeteccoes
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("resposta ilegível: %v", err)
	}
	return out
}

// rotulos resume a página como "cam@minuto", relativo ao meio-dia de hoje.
func rotulos(p paginaDeDeteccoes, hoje time.Time) []string {
	out := []string{}
	for _, d := range p.Deteccoes {
		out = append(out, fmt.Sprintf("%s@%d", d.Cam, (d.InstanteMs-hoje.UnixMilli())/60_000))
	}
	return out
}

func iguais(a, b []string) bool { return fmt.Sprint(a) == fmt.Sprint(b) }

// Sem cursor, as mais novas de todas as câmeras juntas; e duas famílias no
// mesmo instante são uma detecção só.
func TestPaginaDeDeteccoesJuntaAsCameras(t *testing.T) {
	s, hoje := cenarioDeDeteccoes(t)
	p := getPagina(t, s, "")
	want := []string{"cam_b@3", "cam_teste@3", "cam_b@1", "cam_teste@0",
		"cam_teste@-1438", "cam_b@-1439", "cam_teste@-1440"}
	if got := rotulos(p, hoje); !iguais(got, want) || !p.Fim {
		t.Fatalf("página %v fim=%v, esperado %v fim=true", got, p.Fim, want)
	}
	if n := len(p.Deteccoes[3].Objetos); n != 2 {
		t.Errorf("as duas famílias do mesmo quadro deram %d objetos, esperado 2", n)
	}
}

// Rolar para baixo, página a página, passa por tudo exatamente uma vez - e o
// empate do minuto 3 vem inteiro, mesmo passando do limite.
func TestPaginaDeDeteccoesRolaSemPularNemRepetir(t *testing.T) {
	s, hoje := cenarioDeDeteccoes(t)

	p := getPagina(t, s, "limite=1")
	if got := rotulos(p, hoje); !iguais(got, []string{"cam_b@3", "cam_teste@3"}) || p.Fim {
		t.Fatalf("primeira página %v fim=%v: o empate precisava vir inteiro", got, p.Fim)
	}

	var todas []string
	query := "limite=2"
	for i := 0; i < 10; i++ {
		p := getPagina(t, s, query)
		todas = append(todas, rotulos(p, hoje)...)
		if p.Fim {
			break
		}
		query = fmt.Sprintf("limite=2&antes=%d", p.Deteccoes[len(p.Deteccoes)-1].InstanteMs)
	}
	want := []string{"cam_b@3", "cam_teste@3", "cam_b@1", "cam_teste@0",
		"cam_teste@-1438", "cam_b@-1439", "cam_teste@-1440"}
	if !iguais(todas, want) {
		t.Errorf("rolando: %v, esperado %v", todas, want)
	}
}

// Depois de um "ir para", rolar para cima traz as mais próximas do cursor, e
// ainda na ordem da tela: da mais nova para a mais velha.
func TestPaginaDeDeteccoesSobeAPartirDoCursor(t *testing.T) {
	s, hoje := cenarioDeDeteccoes(t)
	cursor := hoje.AddDate(0, 0, -1).UnixMilli() + 60_000 // ontem, cam_b@-1439

	p := getPagina(t, s, fmt.Sprintf("limite=2&depois=%d", cursor))
	if got := rotulos(p, hoje); !iguais(got, []string{"cam_teste@0", "cam_teste@-1438"}) || p.Fim {
		t.Errorf("subindo: %v fim=%v", got, p.Fim)
	}
	p = getPagina(t, s, fmt.Sprintf("depois=%d", hoje.UnixMilli()+60_000))
	if got := rotulos(p, hoje); !iguais(got, []string{"cam_b@3", "cam_teste@3"}) || !p.Fim {
		t.Errorf("chegando ao topo: %v fim=%v", got, p.Fim)
	}
}

// O filtro de família tira o objeto, e a detecção que fica sem nenhum some.
func TestPaginaDeDeteccoesFiltra(t *testing.T) {
	s, hoje := cenarioDeDeteccoes(t)

	p := getPagina(t, s, "familias=veiculo,animal")
	if got := rotulos(p, hoje); !iguais(got, []string{"cam_b@1", "cam_teste@0", "cam_b@-1439"}) {
		t.Errorf("famílias: %v", got)
	}
	if o := p.Deteccoes[1].Objetos; len(o) != 1 || o[0].Familia != "veiculo" {
		t.Errorf("a pessoa escondida continuou na detecção: %+v", o)
	}

	p = getPagina(t, s, "cams=cam_b")
	if got := rotulos(p, hoje); !iguais(got, []string{"cam_b@3", "cam_b@1", "cam_b@-1439"}) {
		t.Errorf("câmeras: %v", got)
	}
}

func TestPaginaDeDeteccoesRecusaParametroRuim(t *testing.T) {
	s, _ := cenarioDeDeteccoes(t)
	for _, q := range []string{"cams=../etc", "familias=fantasma", "limite=0",
		"limite=999", "antes=x", "antes=1&depois=2"} {
		rec := httptest.NewRecorder()
		s.handleDeteccoes(rec, httptest.NewRequest(http.MethodGet, "/api/deteccoes?"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: HTTP %d, esperado 400", q, rec.Code)
		}
	}
}

// O quadro vai como está no disco, cacheável para sempre; a detecção sem
// quadro é 404, e a tela mostra o quadro vazio.
func TestQuadroDaDeteccao(t *testing.T) {
	s, cam := testServer(t)
	t0 := time.Date(2026, 8, 9, 12, 0, 0, 0, time.Local).UnixMilli()
	jpeg := []byte("\xff\xd8 finge que é jpeg \xff\xd9")
	if err := cam.WriteQuadro(time.UnixMilli(t0).Format(store.DayLayout), t0, jpeg); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handleQuadroDaDeteccao(rec, httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/deteccoes/quadro?cam=cam_teste&t=%d", t0), nil))
	if rec.Code != http.StatusOK || rec.Body.String() != string(jpeg) {
		t.Fatalf("HTTP %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if h := rec.Header(); h.Get("Content-Type") != "image/jpeg" || h.Get("Cache-Control") == "" {
		t.Errorf("cabeçalhos: %v", h)
	}

	rec = httptest.NewRecorder()
	s.handleQuadroDaDeteccao(rec, httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/deteccoes/quadro?cam=cam_teste&t=%d", t0+1), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("sem quadro: HTTP %d, esperado 404", rec.Code)
	}
}
