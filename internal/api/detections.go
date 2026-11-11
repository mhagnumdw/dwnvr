package api

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/area"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// limitePadraoDeDeteccoes é a página quando a tela não diz o tamanho. Dá umas
// seis telas de celular na grade de duas colunas.
const defaultDetectionsLimit = 60

// limiteMaximoDeDeteccoes é o teto de uma página. A resposta de 200 detecções
// fica na casa dos 50 KB; mais que isso é a tela pedindo o que não vai mostrar.
const maxDetectionsLimit = 200

// deteccao é o cartão da tela de Detecções: uma olhada do detector de objetos
// numa câmera, com tudo o que ele achou naquele quadro. Duas famílias no mesmo
// instante são DUAS marcas no disco, mas uma detecção só: o quadro é um.
type detection struct {
	Cam  string `json:"cam"`
	AtMs int64  `json:"instanteMs"`
	// QuadroMs é o instante do quadro olhado, que é onde as caixas estão no
	// lugar certo. Ver store.Event.
	FrameMs int64 `json:"quadroMs,omitempty"`
	// TemQuadro diz se há imagem em /api/deteccoes/quadro para esta detecção.
	HasFrame bool            `json:"temQuadro"`
	Objects  []objectInFrame `json:"objetos"`
}

type objectInFrame struct {
	Family string      `json:"familia"`
	Class  string      `json:"classe"`
	Score  float64     `json:"score"`
	Box    *[4]float64 `json:"caixa,omitempty"`
	// Alert e AlertAreas vêm como gravados: o objeto passou na regra de
	// alerta da câmera, nas áreas destes ids. Ver store.Event.
	Alert      bool     `json:"alert,omitempty"`
	AlertAreas []string `json:"alertAreas,omitempty"`
}

type detectionsPage struct {
	// Deteccoes vêm sempre da mais nova para a mais velha, nas duas direções.
	Detections []detection `json:"deteccoes"`
	// Fim diz que não há mais nada na direção pedida. Pode vir falso numa
	// página que por acaso pegou a última detecção; a próxima vem vazia e com
	// Fim verdadeiro.
	End bool `json:"fim"`
}

// handleDeteccoes devolve uma página de detecções de objeto de todas
// as câmeras juntas, da mais nova para a mais velha. É a rolagem da tela de
// Detecções.
//
//	antes=<ms>   as detecções anteriores a este instante (rolar para baixo).
//	             Sem ele, as mais novas.
//	depois=<ms>  as posteriores (rolar para cima, depois de um "ir para").
//	limite=<N>   quantas; padrão limitePadraoDeDeteccoes.
//	cams=a,b     só estas câmeras; sem ele, todas.
//	familias=... só estas famílias; sem ele, todas.
//	area=...     na câmera da área, só os objetos dentro dela; ver filtroDaQuery.
//	rule=...     a regra da área: touch ou foot (o padrão).
//	areas=...    o mesmo, com áreas cadastradas: <câmera>/<id>,<câmera>/<id>.
//	alert=1      só as detecções com objeto que alertou; ver filtroDaQuery.
//
// A página nunca parte um instante ao meio: se a última detecção empata com
// outras no mesmo milissegundo, em outra câmera, elas vêm juntas e a página
// passa um pouco do limite. Sem isso o cursor `antes` pularia as que ficaram
// de fora.
func (s *Server) handleDetections(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Has("antes") && q.Has("depois") {
		writeError(w, http.StatusBadRequest, "use antes ou depois, não os dois")
		return
	}
	descending := !q.Has("depois")
	cursor := int64(math.MaxInt64)
	nome := "antes"
	if !descending {
		nome = "depois"
	}
	if q.Has(nome) {
		v, err := strconv.ParseInt(q.Get(nome), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "parâmetro "+nome+" inválido")
			return
		}
		cursor = v
	}

	limit := defaultDetectionsLimit
	if q.Has("limite") {
		v, err := strconv.Atoi(q.Get("limite"))
		if err != nil || v < 1 || v > maxDetectionsLimit {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("parâmetro limite inválido (de 1 a %d)", maxDetectionsLimit))
			return
		}
		limit = v
	}

	cams, filter, err := s.filterFromQuery(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	page, err := detectionsPageOf(cams, filter, cursor, descending, limit)
	if err != nil {
		s.fail(w, "lendo as detecções", err)
		return
	}
	writeJSON(w, page)
}

// handleDiasDeDeteccao devolve os dias com pelo menos uma detecção, em ordem
// crescente, e quantas detecções cada um tem. É o que o calendário da tela de
// Detecções deixa escolher, e o número que ele mostra em cada dia.
//
//	cams, familias, area, rule, areas e alert: os mesmos de handleDeteccoes.
func (s *Server) handleDetectionDays(w http.ResponseWriter, r *http.Request) {
	cams, filter, err := s.filterFromQuery(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	dias, perDay, err := daysWithDetection(cams, filter)
	if err != nil {
		s.fail(w, "lendo os dias com detecção", err)
		return
	}
	writeJSON(w, map[string]any{"dias": dias, "deteccoesPorDia": perDay})
}

// diasComDeteccao percorre os dias com gravação e conta, em cada um, as
// detecções do filtro; o dia sem nenhuma fica de fora. Contar obriga a ler
// todas as câmeras de cada dia: no hardware de referência, com o cache de
// disco frio, a primeira chamada leva ~1,3 s, e as seguintes saem da memória
// do store em ~5 ms. Dia sem gravação fica de fora mesmo com marca - a
// rolagem também não o alcança, porque anda pelos mesmos diasDasCameras.
func daysWithDetection(cams []*store.Camera, filter objectFilter) ([]string, map[string]int, error) {
	dias := []string{}
	perDay := map[string]int{}
	for _, day := range diasDasCameras(cams) {
		n := 0
		for _, cam := range cams {
			marks, err := cam.DayObjects(day)
			if err != nil {
				return nil, nil, fmt.Errorf("%s, %s: %w", cam.ID, day, err)
			}
			n += countDetections(marks, filter.daCamera(cam.ID))
		}
		if n > 0 {
			dias = append(dias, day)
			perDay[day] = n
		}
	}
	return dias, perDay, nil
}

// contaDeteccoes conta as detecções de uma câmera num dia como o agrupa as
// monta: uma por instante, por mais famílias que o quadro tenha, e com
// `alert=1` só o instante em que algum objeto que fica alertou.
func countDetections(marks []store.Event, filter objectFilter) int {
	n := 0
	ultimo := int64(-1)
	for _, m := range marks {
		if m.AtMs == ultimo || !filter.passes(m) || filter.alertsOnly && !m.Alert {
			continue
		}
		n++
		ultimo = m.AtMs
	}
	return n
}

// paginaDeDeteccoesDe junta as câmeras dia a dia, a partir do dia do cursor e
// na direção pedida, até encher a página. Cada dia de cada câmera vem da
// memória do store; só o primeiro pedido de um dia lê o disco.
func detectionsPageOf(cams []*store.Camera, filter objectFilter,
	cursor int64, descending bool, limit int) (detectionsPage, error) {

	dias := diasDasCameras(cams)
	if descending {
		slices.Reverse(dias)
	}
	var cursorDay string
	if cursor != math.MaxInt64 {
		cursorDay = time.UnixMilli(cursor).Format(store.DayLayout)
	}

	// Na direção do rolar: do mais novo para o mais velho descendo, e o
	// contrário subindo. A resposta subindo é revertida no fim, então o
	// desempate também inverte: um mesmo instante sai sempre na mesma ordem
	// de câmera, venha de que direção vier.
	comesBefore := func(a, b detection) bool {
		if a.AtMs != b.AtMs {
			return (a.AtMs > b.AtMs) == descending
		}
		return (a.Cam < b.Cam) == descending
	}
	pastCursor := func(t int64) bool {
		if descending {
			return t < cursor
		}
		return t > cursor
	}

	out := detectionsPage{Detections: []detection{}, End: true}
	for i, day := range dias {
		if cursorDay != "" && (descending && day > cursorDay || !descending && day < cursorDay) {
			continue
		}
		var ofDay []detection
		for _, cam := range cams {
			marks, err := cam.DayObjects(day)
			if err != nil {
				return out, fmt.Errorf("%s, %s: %w", cam.ID, day, err)
			}
			ofDay = group(ofDay, cam.ID, marks, filter.daCamera(cam.ID), pastCursor)
		}
		sort.Slice(ofDay, func(a, b int) bool { return comesBefore(ofDay[a], ofDay[b]) })
		out.Detections = append(out.Detections, ofDay...)
		if len(out.Detections) < limit {
			continue
		}

		// Corta no limite, mas leva junto o que empata com a última.
		cut := limit
		ultimo := out.Detections[cut-1].AtMs
		for cut < len(out.Detections) && out.Detections[cut].AtMs == ultimo {
			cut++
		}
		leftover := cut < len(out.Detections) || i < len(dias)-1
		out.Detections = out.Detections[:cut]
		out.End = !leftover
		break
	}
	if !descending {
		slices.Reverse(out.Detections)
	}
	return out, nil
}

// agrupa junta as marcas de uma câmera num dia em detecções, uma por instante,
// e as acrescenta a `em`. As marcas vêm em ordem de instante, então as do
// mesmo instante estão lado a lado.
func group(em []detection, cam string, marks []store.Event,
	filter objectFilter, pastCursor func(int64) bool) []detection {

	start := len(em)
	isOpen := -1 // índice, em `em`, da detecção do instante corrente
	for _, m := range marks {
		if !pastCursor(m.AtMs) || !filter.passes(m) {
			continue
		}
		obj := objectInFrame{Family: m.Family, Class: m.Class, Score: m.Score, Box: m.Box,
			Alert: m.Alert, AlertAreas: m.AlertAreas}
		if isOpen >= 0 && em[isOpen].AtMs == m.AtMs {
			d := &em[isOpen]
			d.Objects = append(d.Objects, obj)
			d.HasFrame = d.HasFrame || m.HasFrame
			continue
		}
		em = append(em, detection{Cam: cam, AtMs: m.AtMs, FrameMs: m.FrameMs,
			HasFrame: m.HasFrame, Objects: []objectInFrame{obj}})
		isOpen = len(em) - 1
	}
	if !filter.alertsOnly {
		return em
	}
	// Com `alert=1` a detecção fica inteira, com o objeto que não alertou: a
	// tela o mostra apagado, "não entrou no alerta".
	novas := slices.DeleteFunc(em[start:], func(d detection) bool {
		return !slices.ContainsFunc(d.Objects, func(o objectInFrame) bool { return o.Alert })
	})
	return em[:start+len(novas)]
}

// diasDasCameras é a união dos dias com gravação das câmeras, em ordem
// crescente. Vem do resumo em memória, sem tocar o disco.
func diasDasCameras(cams []*store.Camera) []string {
	var dias []string
	for _, c := range cams {
		for _, d := range c.Days() {
			dias = append(dias, d.Day)
		}
	}
	slices.Sort(dias)
	return slices.Compact(dias)
}

// filtroDeObjetos decide, marca a marca, que objeto fica na detecção. O que
// sai leva junto a detecção que fica sem objeto nenhum.
type objectFilter struct {
	// familias nil é "todas".
	families map[string]bool
	// porCamera são as áreas de cada câmera: o objeto fica se está em
	// qualquer uma delas. A câmera fora do mapa segue sem filtro de área.
	perCamera map[string][]areaWithRule
	// areas são as da câmera das marcas: ver daCamera.
	areas []areaWithRule
	// soAlertas deixa só a detecção em que algum objeto que fica alertou. É
	// por detecção, e não por objeto: o que não alertou fica nela.
	alertsOnly bool
}

// areaComRegra é uma área com a regra dela: a desenhada na hora, com o `rule`
// da query, ou a cadastrada, com a regra do cadastro.
type areaWithRule struct {
	rule area.Rule
	area area.Area
}

// daCamera é o filtro das marcas de uma câmera: as áreas só valem na câmera
// delas, e as outras passam só pelo filtro de família.
func (f objectFilter) daCamera(cam string) objectFilter {
	f.areas = f.perCamera[cam]
	return f
}

// passa diz se a marca fica. Com área, a marca sem caixa, de antes de a caixa
// ser gravada, fica de fora: não há como saber onde ela estava.
func (f objectFilter) passes(m store.Event) bool {
	if f.families != nil && !f.families[m.Family] {
		return false
	}
	if len(f.areas) == 0 {
		return true
	}
	if m.Box == nil {
		return false
	}
	for _, a := range f.areas {
		if a.rule.Inside(a.area, *m.Box) {
			return true
		}
	}
	return false
}

// soma acrescenta uma área à câmera dela.
func (f *objectFilter) add(cam string, a areaWithRule) {
	if f.perCamera == nil {
		f.perCamera = map[string][]areaWithRule{}
	}
	f.perCamera[cam] = append(f.perCamera[cam], a)
}

// filtroDaQuery resolve os parâmetros que a lista e o calendário dividem:
// `cams`, `familias`, `area`, `rule`, `areas` e `alert`.
//
// `area=<cam>:<x>,<y>,<x>,<y>,...` é uma área desenhada sobre a imagem da
// câmera, com X e Y em milésimos do quadro (inteiros de 0 a 1000), para a URL
// ficar curta. `areas=<cam>/<id>,<cam>/<id>` são áreas cadastradas, resolvidas
// pelo cadastro de agora: editar a área muda o filtro, inclusive sobre o
// passado. Nem o id da câmera nem o da área têm vírgula ou barra
// (ValidateCameraID e o id gerado da área).
//
// As áreas de uma câmera somam, a desenhada junto: qualquer uma serve. Elas
// restringem só a câmera delas: as outras de `cams` seguem inteiras, e a
// câmera da área fora de `cams` não aparece, com área ou sem.
//
// `alert=1` deixa só as detecções em que algum objeto que passou nos outros
// filtros alertou, pela marca gravada na hora: mudar o alerta da câmera não
// muda o passado. Com `familias=veiculo`, a detecção que alertou só pela
// pessoa sai, porque a pessoa sai.
func (s *Server) filterFromQuery(q url.Values) ([]*store.Camera, objectFilter, error) {
	var f objectFilter
	cams, err := s.filterCameras(q.Get("cams"))
	if err != nil {
		return nil, f, err
	}
	if q.Has("alert") {
		if q.Get("alert") != "1" {
			return nil, f, errors.New("parâmetro alert inválido (só alert=1)")
		}
		f.alertsOnly = true
	}
	if f.families, err = filterFamilies(q.Get("familias")); err != nil {
		return nil, f, err
	}
	if q.Has("areas") {
		if err := s.filterRegisteredAreas(&f, q.Get("areas")); err != nil {
			return nil, f, err
		}
	}
	if !q.Has("area") {
		if q.Has("rule") {
			return nil, f, errors.New("parâmetro rule sem area")
		}
		return cams, f, nil
	}

	cam, a, err := filterArea(q.Get("area"))
	if err != nil {
		return nil, f, err
	}
	if !s.knownCamera(cam) {
		return nil, f, fmt.Errorf("câmera %q da área não cadastrada", cam)
	}
	rule, err := area.NewRule(q.Get("rule"))
	if err != nil {
		return nil, f, err
	}
	f.add(cam, areaWithRule{rule: rule, area: a})
	return cams, f, nil
}

// areasCadastradasDoFiltro resolve cada `<cam>/<id>` de `areas=` pelo
// cadastro. A área que não existe mais é recusada, como a câmera que não
// existe: o filtro em silêncio mostraria outra lista sem dizer.
func (s *Server) filterRegisteredAreas(f *objectFilter, list string) error {
	registry := map[string]config.Camera{}
	for _, c := range s.mgr.Cameras() {
		registry[c.ID] = c
	}
	for item := range strings.SplitSeq(list, ",") {
		camID, id, ok := strings.Cut(item, "/")
		if !ok || camID == "" || id == "" {
			return fmt.Errorf("parâmetro areas inválido: %q não é <câmera>/<id>", item)
		}
		cam, ok := registry[camID]
		if !ok {
			return fmt.Errorf("câmera %q da área não cadastrada", camID)
		}
		i := slices.IndexFunc(cam.Areas, func(a config.Area) bool { return a.ID == id })
		if i < 0 {
			return fmt.Errorf("a câmera %q não tem a área %q", camID, id)
		}
		// O cadastro já passou pelo CheckCamera: a regra e os pontos valem.
		cad := cam.Areas[i]
		rule, err := area.NewRule(cad.Rule)
		if err != nil {
			return fmt.Errorf("área %q da câmera %q: %w", id, camID, err)
		}
		a, err := area.New(cad.Points)
		if err != nil {
			return fmt.Errorf("área %q da câmera %q: %w", id, camID, err)
		}
		f.add(camID, areaWithRule{rule: rule, area: a})
	}
	return nil
}

// areaDoFiltro lê `<cam>:<x>,<y>,...`, em milésimos, e devolve a câmera e a
// área em fração do quadro.
func filterArea(v string) (string, area.Area, error) {
	i := strings.LastIndexByte(v, ':')
	if i <= 0 {
		return "", area.Area{}, errors.New("parâmetro area inválido: use <câmera>:<x>,<y>,<x>,<y> e assim por diante")
	}
	cam, numbers := v[:i], strings.Split(v[i+1:], ",")
	if len(numbers)%2 != 0 {
		return "", area.Area{}, errors.New("parâmetro area inválido: X e Y vão aos pares")
	}
	points := make([]area.Point, 0, len(numbers)/2)
	for j := 0; j < len(numbers); j += 2 {
		x, errX := strconv.Atoi(numbers[j])
		y, errY := strconv.Atoi(numbers[j+1])
		if errX != nil || errY != nil || x < 0 || x > 1000 || y < 0 || y > 1000 {
			return "", area.Area{}, fmt.Errorf("parâmetro area inválido: %q,%q não são milésimos de 0 a 1000",
				numbers[j], numbers[j+1])
		}
		points = append(points, area.Point{float64(x) / 1000, float64(y) / 1000})
	}
	a, err := area.New(points)
	if err != nil {
		return "", area.Area{}, fmt.Errorf("parâmetro area inválido: %w", err)
	}
	return cam, a, nil
}

// camerasDoFiltro resolve `cams=a,b`. Vazio são todas as cadastradas. Um ID
// que não é de câmera cadastrada é recusado, pelo mesmo motivo do camParam:
// ele vira caminho no disco.
func (s *Server) filterCameras(list string) ([]*store.Camera, error) {
	var out []*store.Camera
	if list == "" {
		for _, c := range s.mgr.Cameras() {
			out = append(out, s.store.Camera(c.ID))
		}
		return out, nil
	}
	for id := range strings.SplitSeq(list, ",") {
		if !s.knownCamera(id) {
			return nil, fmt.Errorf("câmera %q não cadastrada", id)
		}
		out = append(out, s.store.Camera(id))
	}
	return out, nil
}

// familiasDoFiltro resolve `familias=pessoa,veiculo`. Vazio devolve nil, que é
// "todas" - inclusive uma família que um detector futuro venha a marcar.
func filterFamilies(list string) (map[string]bool, error) {
	if list == "" {
		return nil, nil
	}
	out := map[string]bool{}
	for f := range strings.SplitSeq(list, ",") {
		if !slices.Contains(detect.Families, detect.Family(f)) {
			return nil, fmt.Errorf("família %q desconhecida", f)
		}
		out[f] = true
	}
	return out, nil
}

// handleQuadroDaDeteccao serve o JPEG do quadro que o detector olhou.
//
// O arquivo vai como está no disco: nada é redimensionado nem reencodado por
// requisição. Ele nunca muda depois de gravado - o nome é o instante -, então
// o navegador pode guardá-lo para sempre, e rolar de volta não pede nada ao
// servidor.
func (s *Server) handleDetectionFrame(w http.ResponseWriter, r *http.Request) {
	cam, err := s.camParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	t, err := strconv.ParseInt(r.URL.Query().Get("t"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "parâmetro t inválido")
		return
	}

	f, err := os.Open(cam.FramePath(time.UnixMilli(t).Format(store.DayLayout), t))
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "detecção sem quadro")
		return
	}
	if err != nil {
		s.fail(w, "abrindo o quadro", err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.fail(w, "abrindo o quadro", err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeContent(w, r, "", info.ModTime(), f)
}
