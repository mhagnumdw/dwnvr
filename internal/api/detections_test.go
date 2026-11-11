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
func detectionsScenario(t *testing.T) (*Server, time.Time) {
	t.Helper()
	s, camTest := testServer(t)
	s.mgr.Set(config.Camera{ID: "cam_b", Name: "B", Enabled: true})
	camB := s.store.Camera("cam_b")

	today := time.Date(2026, 8, 9, 12, 0, 0, 0, time.Local)
	yesterday := today.AddDate(0, 0, -1)
	for _, cam := range []*store.Camera{camTest, camB} {
		seed(t, cam, yesterday, [2]int64{0, 30_000})
		seed(t, cam, today, [2]int64{0, 30_000})
	}

	mark := func(cam *store.Camera, base time.Time, min int64, family string) {
		t.Helper()
		ev := store.Event{AtMs: base.UnixMilli() + min*60_000, Family: family, Class: "x", Score: 0.9}
		if err := cam.AppendEvent(store.Event{AtMs: ev.AtMs}); err != nil { // o onset
			t.Fatal(err)
		}
		if err := cam.AppendEvent(ev); err != nil {
			t.Fatal(err)
		}
	}
	mark(camTest, yesterday, 0, "pessoa")
	mark(camTest, yesterday, 2, "pessoa")
	mark(camB, yesterday, 1, "veiculo")
	mark(camTest, today, 0, "pessoa")
	mark(camTest, today, 0, "veiculo")
	mark(camTest, today, 3, "pessoa")
	mark(camB, today, 1, "animal")
	mark(camB, today, 3, "pessoa")
	return s, today
}

func getPage(t *testing.T, s *Server, query string) detectionsPage {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleDetections(rec, httptest.NewRequest(http.MethodGet, "/api/deteccoes?"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var out detectionsPage
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("resposta ilegível: %v", err)
	}
	return out
}

// rotulos resume a página como "cam@minuto", relativo ao meio-dia de hoje.
func labels(p detectionsPage, today time.Time) []string {
	out := []string{}
	for _, d := range p.Detections {
		out = append(out, fmt.Sprintf("%s@%d", d.Cam, (d.AtMs-today.UnixMilli())/60_000))
	}
	return out
}

func equal(a, b []string) bool { return fmt.Sprint(a) == fmt.Sprint(b) }

// Sem cursor, as mais novas de todas as câmeras juntas; e duas famílias no
// mesmo instante são uma detecção só.
func TestDetectionsPageMergesTheCameras(t *testing.T) {
	s, today := detectionsScenario(t)
	p := getPage(t, s, "")
	want := []string{"cam_b@3", "cam_teste@3", "cam_b@1", "cam_teste@0",
		"cam_teste@-1438", "cam_b@-1439", "cam_teste@-1440"}
	if got := labels(p, today); !equal(got, want) || !p.End {
		t.Fatalf("página %v fim=%v, esperado %v fim=true", got, p.End, want)
	}
	if n := len(p.Detections[3].Objects); n != 2 {
		t.Errorf("as duas famílias do mesmo quadro deram %d objetos, esperado 2", n)
	}
}

// Rolar para baixo, página a página, passa por tudo exatamente uma vez - e o
// empate do minuto 3 vem inteiro, mesmo passando do limite.
func TestDetectionsPageScrollsWithoutSkippingOrRepeating(t *testing.T) {
	s, today := detectionsScenario(t)

	p := getPage(t, s, "limite=1")
	if got := labels(p, today); !equal(got, []string{"cam_b@3", "cam_teste@3"}) || p.End {
		t.Fatalf("primeira página %v fim=%v: o empate precisava vir inteiro", got, p.End)
	}

	var all []string
	query := "limite=2"
	for range 10 {
		p := getPage(t, s, query)
		all = append(all, labels(p, today)...)
		if p.End {
			break
		}
		query = fmt.Sprintf("limite=2&antes=%d", p.Detections[len(p.Detections)-1].AtMs)
	}
	want := []string{"cam_b@3", "cam_teste@3", "cam_b@1", "cam_teste@0",
		"cam_teste@-1438", "cam_b@-1439", "cam_teste@-1440"}
	if !equal(all, want) {
		t.Errorf("rolando: %v, esperado %v", all, want)
	}
}

// Depois de um "ir para", rolar para cima traz as mais próximas do cursor, e
// ainda na ordem da tela: da mais nova para a mais velha.
func TestDetectionsPageGoesUpFromTheCursor(t *testing.T) {
	s, today := detectionsScenario(t)
	cursor := today.AddDate(0, 0, -1).UnixMilli() + 60_000 // ontem, cam_b@-1439

	p := getPage(t, s, fmt.Sprintf("limite=2&depois=%d", cursor))
	if got := labels(p, today); !equal(got, []string{"cam_teste@0", "cam_teste@-1438"}) || p.End {
		t.Errorf("subindo: %v fim=%v", got, p.End)
	}
	p = getPage(t, s, fmt.Sprintf("depois=%d", today.UnixMilli()+60_000))
	if got := labels(p, today); !equal(got, []string{"cam_b@3", "cam_teste@3"}) || !p.End {
		t.Errorf("chegando ao topo: %v fim=%v", got, p.End)
	}
}

// O filtro de família tira o objeto, e a detecção que fica sem nenhum some.
func TestDetectionsPageFilters(t *testing.T) {
	s, today := detectionsScenario(t)

	p := getPage(t, s, "familias=veiculo,animal")
	if got := labels(p, today); !equal(got, []string{"cam_b@1", "cam_teste@0", "cam_b@-1439"}) {
		t.Errorf("famílias: %v", got)
	}
	if o := p.Detections[1].Objects; len(o) != 1 || o[0].Family != "veiculo" {
		t.Errorf("a pessoa escondida continuou na detecção: %+v", o)
	}

	p = getPage(t, s, "cams=cam_b")
	if got := labels(p, today); !equal(got, []string{"cam_b@3", "cam_b@1", "cam_b@-1439"}) {
		t.Errorf("câmeras: %v", got)
	}
}

// portaoEmMilesimos é a área do teste do filtro: X de 0,4 a 0,6 e Y de 0,5 a
// 0,7, como a query a manda.
const gateInThousandths = "cam_teste:400,500,600,500,600,700,400,700"

// cenarioDeArea acrescenta ao cenarioDeDeteccoes marcas com caixa, de hoje:
//
//	cam_teste 5: pessoa com o pé no portão, e um veículo longe dele
//	cam_teste 6: pessoa na frente, cobrindo o portão, com o pé abaixo
//	cam_b     5: pessoa no mesmo lugar, mas em outra câmera
//
// As marcas do cenarioDeDeteccoes não têm caixa.
func areaScenario(t *testing.T) (*Server, time.Time) {
	t.Helper()
	s, today := detectionsScenario(t)
	mark := func(cam string, min int64, family string, box [4]float64) {
		t.Helper()
		ev := store.Event{AtMs: today.UnixMilli() + min*60_000, Family: family, Class: "x",
			Score: 0.9, Box: &box}
		if err := s.store.Camera(cam).AppendEvent(ev); err != nil {
			t.Fatal(err)
		}
	}
	mark("cam_teste", 5, "pessoa", [4]float64{0.45, 0.3, 0.55, 0.65})
	mark("cam_teste", 5, "veiculo", [4]float64{0.05, 0.05, 0.15, 0.2})
	mark("cam_teste", 6, "pessoa", [4]float64{0.35, 0.2, 0.65, 0.95})
	mark("cam_b", 5, "pessoa", [4]float64{0.45, 0.3, 0.55, 0.65})
	return s, today
}

// O filtro de área tira o objeto que está fora, como o de família, mas só na
// câmera da área: as outras câmeras seguem inteiras. A marca sem caixa da
// câmera da área sai: não há como saber onde estava.
func TestDetectionsPageFiltersByArea(t *testing.T) {
	s, today := areaScenario(t)
	camB := []string{"cam_b@3", "cam_b@1", "cam_b@-1439"}

	p := getPage(t, s, "area="+gateInThousandths)
	if got := labels(p, today); !equal(got, append([]string{"cam_b@5", "cam_teste@5"}, camB...)) {
		t.Errorf("foot, o padrão: %v", got)
	}
	if o := p.Detections[1].Objects; len(o) != 1 || o[0].Family != "pessoa" {
		t.Errorf("o veículo fora da área continuou na detecção: %+v", o)
	}

	p = getPage(t, s, "area="+gateInThousandths+"&rule=touch")
	if got := labels(p, today); !equal(got, append([]string{"cam_teste@6", "cam_b@5", "cam_teste@5"}, camB...)) {
		t.Errorf("touch: %v", got)
	}

	// A imagem inteira dá o mesmo que sem área, salvo as marcas sem caixa.
	for _, rule := range []string{"touch", "foot"} {
		p = getPage(t, s, "area=cam_teste:0,0,1000,0,1000,1000,0,1000&rule="+rule)
		if got := labels(p, today); !equal(got, append([]string{"cam_teste@6", "cam_b@5", "cam_teste@5"}, camB...)) {
			t.Errorf("imagem inteira, %s: %v", rule, got)
		}
		if o := p.Detections[2].Objects; len(o) != 2 {
			t.Errorf("imagem inteira, %s: a detecção do minuto 5 perdeu objeto: %+v", rule, o)
		}
	}

	p = getPage(t, s, "area="+gateInThousandths+"&rule=touch&familias=veiculo")
	if got := labels(p, today); !equal(got, []string{"cam_b@-1439"}) {
		t.Errorf("família e área juntas: %v", got)
	}

	// A câmera da área desmarcada some da lista, com área ou sem.
	p = getPage(t, s, "cams=cam_b&area="+gateInThousandths)
	if got := labels(p, today); !equal(got, append([]string{"cam_b@5"}, camB...)) {
		t.Errorf("a câmera da área fora de cams: %v", got)
	}

	p = getPage(t, s, "cams=cam_teste&area="+gateInThousandths)
	if got := labels(p, today); !equal(got, []string{"cam_teste@5"}) {
		t.Errorf("só a câmera da área: %v", got)
	}
}

// O calendário segue a área, como segue câmeras e famílias, e conta as outras
// câmeras inteiras.
func TestDetectionDaysByArea(t *testing.T) {
	s, today := areaScenario(t)
	day := today.Format(store.DayLayout)
	yesterday := today.AddDate(0, 0, -1).Format(store.DayLayout)
	for q, want := range map[string]map[string]int{
		"area=" + gateInThousandths:                      {yesterday: 1, day: 4},
		"area=" + gateInThousandths + "&rule=touch":      {yesterday: 1, day: 5},
		"area=cam_teste:0,0,100,0,100,100,0,100":         {yesterday: 1, day: 3},
		"area=" + gateInThousandths + "&familias=animal": {day: 1},
		"area=" + gateInThousandths + "&cams=cam_teste":  {day: 1},
	} {
		rec := httptest.NewRecorder()
		s.handleDetectionDays(rec, httptest.NewRequest(http.MethodGet, "/api/deteccoes/dias?"+q, nil))
		var out struct {
			DetectionsPerDay map[string]int `json:"deteccoesPorDia"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%s: resposta ilegível: %s", q, rec.Body.String())
		}
		if !maps.Equal(out.DetectionsPerDay, want) {
			t.Errorf("%s: %v, esperado %v", q, out.DetectionsPerDay, want)
		}
	}
}

// cadastraAreas dá às câmeras do cenarioDeArea as áreas do cadastro:
//
//	cam_teste/portao: o portão de portaoEmMilesimos, com a foot
//	cam_teste/canto:  o canto de cima à esquerda, com a touch, onde está o veículo
//	cam_b/portao:     o mesmo portão, na outra câmera
func registerAreas(s *Server) {
	gate := []area.Point{{0.4, 0.5}, {0.6, 0.5}, {0.6, 0.7}, {0.4, 0.7}}
	canto := []area.Point{{0, 0}, {0.2, 0}, {0.2, 0.25}, {0, 0.25}}
	s.mgr.Set(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, Areas: []config.Area{
		{ID: "portao", Name: "Portão", Rule: "foot", Points: gate},
		{ID: "canto", Name: "Canto", Rule: "touch", Points: canto},
	}})
	s.mgr.Set(config.Camera{ID: "cam_b", Name: "B", Enabled: true, Areas: []config.Area{
		{ID: "portao", Name: "Portão", Points: gate},
	}})
}

// As áreas cadastradas filtram como a desenhada, cada uma com a regra dela, e
// as de uma câmera somam: qualquer uma serve. A desenhada entra na soma.
func TestDetectionsPageFiltersByRegisteredArea(t *testing.T) {
	s, today := areaScenario(t)
	registerAreas(s)
	camB := []string{"cam_b@3", "cam_b@1", "cam_b@-1439"}

	p := getPage(t, s, "areas=cam_teste/portao")
	if got := labels(p, today); !equal(got, append([]string{"cam_b@5", "cam_teste@5"}, camB...)) {
		t.Errorf("o portão: %v", got)
	}
	if o := p.Detections[1].Objects; len(o) != 1 || o[0].Family != "pessoa" {
		t.Errorf("o veículo fora do portão continuou na detecção: %+v", o)
	}

	p = getPage(t, s, "areas=cam_teste/portao,cam_teste/canto")
	if got := labels(p, today); !equal(got, append([]string{"cam_b@5", "cam_teste@5"}, camB...)) {
		t.Errorf("portão e canto: %v", got)
	}
	if o := p.Detections[1].Objects; len(o) != 2 {
		t.Errorf("portão e canto: a soma perdeu o veículo do canto: %+v", o)
	}

	p = getPage(t, s, "areas=cam_teste/canto&area="+gateInThousandths)
	if o := p.Detections[1].Objects; len(o) != 2 {
		t.Errorf("a desenhada e a cadastrada: a soma perdeu objeto: %+v", o)
	}

	// Cada câmera com a área dela: a pessoa de cam_b@5 está no portão, e as
	// marcas sem caixa das duas saem.
	p = getPage(t, s, "areas=cam_teste/portao,cam_b/portao")
	if got := labels(p, today); !equal(got, []string{"cam_b@5", "cam_teste@5"}) {
		t.Errorf("uma área em cada câmera: %v", got)
	}
}

func TestDetectionDaysByRegisteredArea(t *testing.T) {
	s, today := areaScenario(t)
	registerAreas(s)
	rec := httptest.NewRecorder()
	s.handleDetectionDays(rec, httptest.NewRequest(http.MethodGet,
		"/api/deteccoes/dias?areas=cam_teste/portao", nil))
	var out struct {
		DetectionsPerDay map[string]int `json:"deteccoesPorDia"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("resposta ilegível: %s", rec.Body.String())
	}
	want := map[string]int{today.AddDate(0, 0, -1).Format(store.DayLayout): 1, today.Format(store.DayLayout): 4}
	if !maps.Equal(out.DetectionsPerDay, want) {
		t.Errorf("%v, esperado %v", out.DetectionsPerDay, want)
	}
}

func TestAreaFilterRejectsBadParameter(t *testing.T) {
	s, _ := areaScenario(t)
	registerAreas(s)
	for _, q := range []string{
		"rule=foot", // regra sem área
		"area=" + gateInThousandths + "&rule=pisa", // o nome da tela não é o do JSON
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
			"deteccoes": s.handleDetections, "dias": s.handleDetectionDays,
		} {
			rec := httptest.NewRecorder()
			h(rec, httptest.NewRequest(http.MethodGet, "/api/deteccoes?"+q, nil))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s, %s: HTTP %d, esperado 400", nome, q, rec.Code)
			}
		}
	}
}

// cenarioDeAlerta acrescenta ao cenarioDeDeteccoes marcas com o alerta
// gravado:
//
//	hoje,  cam_teste 7: pessoa que alertou no portão, e um veículo que não
//	hoje,  cam_b     8: pessoa que não alertou
//	ontem, cam_b     4: pessoa que alertou na imagem inteira
func alertScenario(t *testing.T) (*Server, time.Time) {
	t.Helper()
	s, today := detectionsScenario(t)
	mark := func(cam string, base time.Time, min int64, family string, alerted bool, areas ...string) {
		t.Helper()
		ev := store.Event{AtMs: base.UnixMilli() + min*60_000, Family: family, Class: "x",
			Score: 0.9, Alert: alerted, AlertAreas: areas}
		if err := s.store.Camera(cam).AppendEvent(ev); err != nil {
			t.Fatal(err)
		}
	}
	yesterday := today.AddDate(0, 0, -1)
	mark("cam_teste", today, 7, "pessoa", true, "portao")
	mark("cam_teste", today, 7, "veiculo", false)
	mark("cam_b", today, 8, "pessoa", false)
	mark("cam_b", yesterday, 4, "pessoa", true)
	return s, today
}

// `alert=1` deixa só a detecção com objeto que alertou, e inteira: o veículo
// que não entrou no alerta segue nela, sem a marca. A marca vem como gravada,
// com o id da área. Com a família do objeto que alertou fora, a detecção sai.
func TestDetectionsPageAlertsOnly(t *testing.T) {
	s, today := alertScenario(t)

	p := getPage(t, s, "alert=1")
	if got := labels(p, today); !equal(got, []string{"cam_teste@7", "cam_b@-1436"}) {
		t.Fatalf("só alertas: %v", got)
	}
	o := p.Detections[0].Objects
	if len(o) != 2 || !o[0].Alert || !slices.Equal(o[0].AlertAreas, []string{"portao"}) ||
		o[1].Alert || o[1].AlertAreas != nil {
		t.Errorf("objetos do minuto 7: %+v", o)
	}
	if o := p.Detections[1].Objects; len(o) != 1 || !o[0].Alert || o[0].AlertAreas != nil {
		t.Errorf("alerta na imagem inteira: %+v", o)
	}

	if got := labels(getPage(t, s, "alert=1&familias=veiculo"), today); len(got) != 0 {
		t.Errorf("veículo que não alertou: %v", got)
	}
	if got := labels(getPage(t, s, "alert=1&cams=cam_b"), today); !equal(got, []string{"cam_b@-1436"}) {
		t.Errorf("só alertas da cam_b: %v", got)
	}

	// Sem o filtro, a marca vem do mesmo jeito.
	p = getPage(t, s, "cams=cam_teste&familias=pessoa")
	if o := p.Detections[0].Objects; labels(p, today)[0] != "cam_teste@7" || !o[0].Alert {
		t.Errorf("sem o filtro: %v %+v", labels(p, today), o)
	}

	rec := httptest.NewRecorder()
	s.handleDetectionDays(rec, httptest.NewRequest(http.MethodGet, "/api/deteccoes/dias?alert=1", nil))
	var dias struct {
		DetectionsPerDay map[string]int `json:"deteccoesPorDia"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dias); err != nil {
		t.Fatal(err)
	}
	if want := map[string]int{today.Format(store.DayLayout): 1,
		today.AddDate(0, 0, -1).Format(store.DayLayout): 1}; !maps.Equal(dias.DetectionsPerDay, want) {
		t.Errorf("calendário: %v, esperado %v", dias.DetectionsPerDay, want)
	}
}

func TestDetectionsPageRejectsBadParameter(t *testing.T) {
	s, _ := detectionsScenario(t)
	for _, q := range []string{"cams=../etc", "familias=fantasma", "limite=0",
		"limite=999", "antes=x", "antes=1&depois=2", "alert=0", "alert=true"} {
		rec := httptest.NewRecorder()
		s.handleDetections(rec, httptest.NewRequest(http.MethodGet, "/api/deteccoes?"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: HTTP %d, esperado 400", q, rec.Code)
		}
	}
}

// O calendário só oferece dia com detecção do filtro: o dia que só gravou
// fica de fora, e o filtro de câmera e de família tira o dia que esvaziou. A
// contagem é de detecções, não de marcas: pessoa e veículo no mesmo quadro de
// hoje contam uma vez só.
func TestDetectionDays(t *testing.T) {
	s, today := detectionsScenario(t)
	dayBeforeYesterday := today.AddDate(0, 0, -2)
	seed(t, s.store.Camera("cam_teste"), dayBeforeYesterday, [2]int64{0, 30_000}) // gravou, nada detectou

	yesterday := today.AddDate(0, 0, -1).Format(store.DayLayout)
	day := today.Format(store.DayLayout)
	for q, want := range map[string]map[string]int{
		"":                                   {yesterday: 3, day: 4},
		"familias=animal":                    {day: 1},
		"familias=pessoa,veiculo":            {yesterday: 3, day: 3},
		"cams=cam_b&familias=veiculo":        {yesterday: 1},
		"cams=cam_b&familias=animal,veiculo": {yesterday: 1, day: 1},
	} {
		rec := httptest.NewRecorder()
		s.handleDetectionDays(rec, httptest.NewRequest(http.MethodGet, "/api/deteccoes/dias?"+q, nil))
		var out struct {
			Dias             []string       `json:"dias"`
			DetectionsPerDay map[string]int `json:"deteccoesPorDia"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Dias == nil || out.DetectionsPerDay == nil {
			t.Fatalf("%s: resposta ilegível ou sem lista: %s", q, rec.Body.String())
		}
		wantDias := slices.Sorted(maps.Keys(want))
		if !equal(out.Dias, wantDias) {
			t.Errorf("%s: %v, esperado %v", q, out.Dias, wantDias)
		}
		if !maps.Equal(out.DetectionsPerDay, want) {
			t.Errorf("%s: %v, esperado %v", q, out.DetectionsPerDay, want)
		}
	}

	for _, q := range []string{"cams=../etc", "familias=fantasma"} {
		rec := httptest.NewRecorder()
		s.handleDetectionDays(rec, httptest.NewRequest(http.MethodGet, "/api/deteccoes/dias?"+q, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: HTTP %d, esperado 400", q, rec.Code)
		}
	}
}

// O quadro vai como está no disco, cacheável para sempre; a detecção sem
// quadro é 404, e a tela mostra o quadro vazio.
func TestDetectionFrame(t *testing.T) {
	s, cam := testServer(t)
	t0 := time.Date(2026, 8, 9, 12, 0, 0, 0, time.Local).UnixMilli()
	jpeg := []byte("\xff\xd8 finge que é jpeg \xff\xd9")
	if err := cam.WriteFrame(time.UnixMilli(t0).Format(store.DayLayout), t0, jpeg); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handleDetectionFrame(rec, httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/deteccoes/quadro?cam=cam_teste&t=%d", t0), nil))
	if rec.Code != http.StatusOK || rec.Body.String() != string(jpeg) {
		t.Fatalf("HTTP %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if h := rec.Header(); h.Get("Content-Type") != "image/jpeg" || h.Get("Cache-Control") == "" {
		t.Errorf("cabeçalhos: %v", h)
	}

	rec = httptest.NewRecorder()
	s.handleDetectionFrame(rec, httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/deteccoes/quadro?cam=cam_teste&t=%d", t0+1), nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("sem quadro: HTTP %d, esperado 404", rec.Code)
	}
}
