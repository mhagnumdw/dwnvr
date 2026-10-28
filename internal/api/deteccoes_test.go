package api

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/area"
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
	for range 10 {
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

// portaoEmMilesimos é a área do teste do filtro: X de 0,4 a 0,6 e Y de 0,5 a
// 0,7, como a query a manda.
const portaoEmMilesimos = "cam_teste:400,500,600,500,600,700,400,700"

// cenarioDeArea acrescenta ao cenarioDeDeteccoes marcas com caixa, de hoje:
//
//	cam_teste 5: pessoa com o pé no portão, e um veículo longe dele
//	cam_teste 6: pessoa na frente, cobrindo o portão, com o pé abaixo
//	cam_b     5: pessoa no mesmo lugar, mas em outra câmera
//
// As marcas do cenarioDeDeteccoes não têm caixa.
func cenarioDeArea(t *testing.T) (*Server, time.Time) {
	t.Helper()
	s, hoje := cenarioDeDeteccoes(t)
	marca := func(cam string, min int64, familia string, caixa [4]float64) {
		t.Helper()
		ev := store.Evento{InstanteMs: hoje.UnixMilli() + min*60_000, Familia: familia, Classe: "x",
			Score: 0.9, Caixa: &caixa}
		if err := s.store.Camera(cam).AppendEvento(ev); err != nil {
			t.Fatal(err)
		}
	}
	marca("cam_teste", 5, "pessoa", [4]float64{0.45, 0.3, 0.55, 0.65})
	marca("cam_teste", 5, "veiculo", [4]float64{0.05, 0.05, 0.15, 0.2})
	marca("cam_teste", 6, "pessoa", [4]float64{0.35, 0.2, 0.65, 0.95})
	marca("cam_b", 5, "pessoa", [4]float64{0.45, 0.3, 0.55, 0.65})
	return s, hoje
}

// O filtro de área tira o objeto que está fora, como o de família, mas só na
// câmera da área: as outras câmeras seguem inteiras. A marca sem caixa da
// câmera da área sai: não há como saber onde estava.
func TestPaginaDeDeteccoesFiltraPorArea(t *testing.T) {
	s, hoje := cenarioDeArea(t)
	camB := []string{"cam_b@3", "cam_b@1", "cam_b@-1439"}

	p := getPagina(t, s, "area="+portaoEmMilesimos)
	if got := rotulos(p, hoje); !iguais(got, append([]string{"cam_b@5", "cam_teste@5"}, camB...)) {
		t.Errorf("foot, o padrão: %v", got)
	}
	if o := p.Deteccoes[1].Objetos; len(o) != 1 || o[0].Familia != "pessoa" {
		t.Errorf("o veículo fora da área continuou na detecção: %+v", o)
	}

	p = getPagina(t, s, "area="+portaoEmMilesimos+"&rule=touch")
	if got := rotulos(p, hoje); !iguais(got, append([]string{"cam_teste@6", "cam_b@5", "cam_teste@5"}, camB...)) {
		t.Errorf("touch: %v", got)
	}

	// A imagem inteira dá o mesmo que sem área, salvo as marcas sem caixa.
	for _, regra := range []string{"touch", "foot"} {
		p = getPagina(t, s, "area=cam_teste:0,0,1000,0,1000,1000,0,1000&rule="+regra)
		if got := rotulos(p, hoje); !iguais(got, append([]string{"cam_teste@6", "cam_b@5", "cam_teste@5"}, camB...)) {
			t.Errorf("imagem inteira, %s: %v", regra, got)
		}
		if o := p.Deteccoes[2].Objetos; len(o) != 2 {
			t.Errorf("imagem inteira, %s: a detecção do minuto 5 perdeu objeto: %+v", regra, o)
		}
	}

	p = getPagina(t, s, "area="+portaoEmMilesimos+"&rule=touch&familias=veiculo")
	if got := rotulos(p, hoje); !iguais(got, []string{"cam_b@-1439"}) {
		t.Errorf("família e área juntas: %v", got)
	}

	// A câmera da área desmarcada some da lista, com área ou sem.
	p = getPagina(t, s, "cams=cam_b&area="+portaoEmMilesimos)
	if got := rotulos(p, hoje); !iguais(got, append([]string{"cam_b@5"}, camB...)) {
		t.Errorf("a câmera da área fora de cams: %v", got)
	}

	p = getPagina(t, s, "cams=cam_teste&area="+portaoEmMilesimos)
	if got := rotulos(p, hoje); !iguais(got, []string{"cam_teste@5"}) {
		t.Errorf("só a câmera da área: %v", got)
	}
}

// O calendário segue a área, como segue câmeras e famílias, e conta as outras
// câmeras inteiras.
func TestDiasDeDeteccaoPorArea(t *testing.T) {
	s, hoje := cenarioDeArea(t)
	dia := hoje.Format(store.DayLayout)
	ontem := hoje.AddDate(0, 0, -1).Format(store.DayLayout)
	for q, want := range map[string]map[string]int{
		"area=" + portaoEmMilesimos:                      {ontem: 1, dia: 4},
		"area=" + portaoEmMilesimos + "&rule=touch":      {ontem: 1, dia: 5},
		"area=cam_teste:0,0,100,0,100,100,0,100":         {ontem: 1, dia: 3},
		"area=" + portaoEmMilesimos + "&familias=animal": {dia: 1},
		"area=" + portaoEmMilesimos + "&cams=cam_teste":  {dia: 1},
	} {
		rec := httptest.NewRecorder()
		s.handleDiasDeDeteccao(rec, httptest.NewRequest(http.MethodGet, "/api/deteccoes/dias?"+q, nil))
		var out struct {
			DeteccoesPorDia map[string]int `json:"deteccoesPorDia"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: resposta ilegível: %s", q, rec.Body.String())
		}
		if !maps.Equal(out.DeteccoesPorDia, want) {
			t.Errorf("%s: %v, esperado %v", q, out.DeteccoesPorDia, want)
		}
	}
}

// cadastraAreas dá às câmeras do cenarioDeArea as áreas do cadastro:
//
//	cam_teste/portao: o portão de portaoEmMilesimos, com a foot
//	cam_teste/canto:  o canto de cima à esquerda, com a touch, onde está o veículo
//	cam_b/portao:     o mesmo portão, na outra câmera
func cadastraAreas(s *Server) {
	portao := []area.Ponto{{0.4, 0.5}, {0.6, 0.5}, {0.6, 0.7}, {0.4, 0.7}}
	canto := []area.Ponto{{0, 0}, {0.2, 0}, {0.2, 0.25}, {0, 0.25}}
	s.mgr.Set(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, Areas: []config.Area{
		{ID: "portao", Name: "Portão", Rule: "foot", Points: portao},
		{ID: "canto", Name: "Canto", Rule: "touch", Points: canto},
	}})
	s.mgr.Set(config.Camera{ID: "cam_b", Name: "B", Enabled: true, Areas: []config.Area{
		{ID: "portao", Name: "Portão", Points: portao},
	}})
}

// As áreas cadastradas filtram como a desenhada, cada uma com a regra dela, e
// as de uma câmera somam: qualquer uma serve. A desenhada entra na soma.
func TestPaginaDeDeteccoesFiltraPorAreaCadastrada(t *testing.T) {
	s, hoje := cenarioDeArea(t)
	cadastraAreas(s)
	camB := []string{"cam_b@3", "cam_b@1", "cam_b@-1439"}

	p := getPagina(t, s, "areas=cam_teste/portao")
	if got := rotulos(p, hoje); !iguais(got, append([]string{"cam_b@5", "cam_teste@5"}, camB...)) {
		t.Errorf("o portão: %v", got)
	}
	if o := p.Deteccoes[1].Objetos; len(o) != 1 || o[0].Familia != "pessoa" {
		t.Errorf("o veículo fora do portão continuou na detecção: %+v", o)
	}

	p = getPagina(t, s, "areas=cam_teste/portao,cam_teste/canto")
	if got := rotulos(p, hoje); !iguais(got, append([]string{"cam_b@5", "cam_teste@5"}, camB...)) {
		t.Errorf("portão e canto: %v", got)
	}
	if o := p.Deteccoes[1].Objetos; len(o) != 2 {
		t.Errorf("portão e canto: a soma perdeu o veículo do canto: %+v", o)
	}

	p = getPagina(t, s, "areas=cam_teste/canto&area="+portaoEmMilesimos)
	if o := p.Deteccoes[1].Objetos; len(o) != 2 {
		t.Errorf("a desenhada e a cadastrada: a soma perdeu objeto: %+v", o)
	}

	// Cada câmera com a área dela: a pessoa de cam_b@5 está no portão, e as
	// marcas sem caixa das duas saem.
	p = getPagina(t, s, "areas=cam_teste/portao,cam_b/portao")
	if got := rotulos(p, hoje); !iguais(got, []string{"cam_b@5", "cam_teste@5"}) {
		t.Errorf("uma área em cada câmera: %v", got)
	}
}

func TestDiasDeDeteccaoPorAreaCadastrada(t *testing.T) {
	s, hoje := cenarioDeArea(t)
	cadastraAreas(s)
	rec := httptest.NewRecorder()
	s.handleDiasDeDeteccao(rec, httptest.NewRequest(http.MethodGet,
		"/api/deteccoes/dias?areas=cam_teste/portao", nil))
	var out struct {
		DeteccoesPorDia map[string]int `json:"deteccoesPorDia"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("resposta ilegível: %s", rec.Body.String())
	}
	want := map[string]int{hoje.AddDate(0, 0, -1).Format(store.DayLayout): 1, hoje.Format(store.DayLayout): 4}
	if !maps.Equal(out.DeteccoesPorDia, want) {
		t.Errorf("%v, esperado %v", out.DeteccoesPorDia, want)
	}
}

func TestFiltroDeAreaRecusaParametroRuim(t *testing.T) {
	s, _ := cenarioDeArea(t)
	cadastraAreas(s)
	for _, q := range []string{
		"rule=foot", // regra sem área
		"area=" + portaoEmMilesimos + "&rule=pisa", // o nome da tela não é o do JSON
		"area=semCamera",
		"area=:0,0,1000,0,0,1000",
		"area=cam_teste:0,0,1000,0,1000",   // X sem Y
		"area=cam_teste:0,0,1000,0",        // dois pontos
		"area=cam_teste:0,0,1001,0,0,1000", // fora do quadro
		"area=cam_teste:0,0,x,0,0,1000",
		"area=fantasma:0,0,1000,0,0,1000",
		"areas=cam_teste/fantasma", // a área apagada
		"areas=fantasma/portao",
		"areas=cam_teste",
		"areas=cam_teste/",
		"areas=/portao",
		"areas=cam_teste/portao,",               // item vazio
		"areas=cam_teste/portao,cam_b/fantasma", // uma boa e uma apagada
		"areas=",
	} {
		for nome, h := range map[string]http.HandlerFunc{
			"deteccoes": s.handleDeteccoes, "dias": s.handleDiasDeDeteccao,
		} {
			rec := httptest.NewRecorder()
			h(rec, httptest.NewRequest(http.MethodGet, "/api/deteccoes?"+q, nil))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s, %s: HTTP %d, esperado 400", nome, q, rec.Code)
			}
		}
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

// O calendário só oferece dia com detecção do filtro: o dia que só gravou
// fica de fora, e o filtro de câmera e de família tira o dia que esvaziou. A
// contagem é de detecções, não de marcas: pessoa e veículo no mesmo quadro de
// hoje contam uma vez só.
func TestDiasDeDeteccao(t *testing.T) {
	s, hoje := cenarioDeDeteccoes(t)
	anteontem := hoje.AddDate(0, 0, -2)
	seed(t, s.store.Camera("cam_teste"), anteontem, [2]int64{0, 30_000}) // gravou, nada detectou

	ontem := hoje.AddDate(0, 0, -1).Format(store.DayLayout)
	dia := hoje.Format(store.DayLayout)
	for q, want := range map[string]map[string]int{
		"":                                   {ontem: 3, dia: 4},
		"familias=animal":                    {dia: 1},
		"familias=pessoa,veiculo":            {ontem: 3, dia: 3},
		"cams=cam_b&familias=veiculo":        {ontem: 1},
		"cams=cam_b&familias=animal,veiculo": {ontem: 1, dia: 1},
	} {
		rec := httptest.NewRecorder()
		s.handleDiasDeDeteccao(rec, httptest.NewRequest(http.MethodGet, "/api/deteccoes/dias?"+q, nil))
		var out struct {
			Dias            []string       `json:"dias"`
			DeteccoesPorDia map[string]int `json:"deteccoesPorDia"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Dias == nil || out.DeteccoesPorDia == nil {
			t.Fatalf("%s: resposta ilegível ou sem lista: %s", q, rec.Body.String())
		}
		wantDias := slices.Sorted(maps.Keys(want))
		if !iguais(out.Dias, wantDias) {
			t.Errorf("%s: %v, esperado %v", q, out.Dias, wantDias)
		}
		if !maps.Equal(out.DeteccoesPorDia, want) {
			t.Errorf("%s: %v, esperado %v", q, out.DeteccoesPorDia, want)
		}
	}

	for _, q := range []string{"cams=../etc", "familias=fantasma"} {
		rec := httptest.NewRecorder()
		s.handleDiasDeDeteccao(rec, httptest.NewRequest(http.MethodGet, "/api/deteccoes/dias?"+q, nil))
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
