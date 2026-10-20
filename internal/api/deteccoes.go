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
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// limitePadraoDeDeteccoes é a página quando a tela não diz o tamanho. Dá umas
// seis telas de celular na grade de duas colunas.
const limitePadraoDeDeteccoes = 60

// limiteMaximoDeDeteccoes é o teto de uma página. A resposta de 200 detecções
// fica na casa dos 50 KB; mais que isso é a tela pedindo o que não vai mostrar.
const limiteMaximoDeDeteccoes = 200

// deteccao é o cartão da tela de Detecções: uma olhada do detector de objetos
// numa câmera, com tudo o que ele achou naquele quadro. Duas famílias no mesmo
// instante são DUAS marcas no disco, mas uma detecção só: o quadro é um.
type deteccao struct {
	Cam        string `json:"cam"`
	InstanteMs int64  `json:"instanteMs"`
	// QuadroMs é o instante do quadro olhado, que é onde as caixas estão no
	// lugar certo. Ver store.Evento.
	QuadroMs int64 `json:"quadroMs,omitempty"`
	// TemQuadro diz se há imagem em /api/deteccoes/quadro para esta detecção.
	TemQuadro bool             `json:"temQuadro"`
	Objetos   []objetoNoQuadro `json:"objetos"`
}

type objetoNoQuadro struct {
	Familia string      `json:"familia"`
	Classe  string      `json:"classe"`
	Score   float64     `json:"score"`
	Caixa   *[4]float64 `json:"caixa,omitempty"`
}

type paginaDeDeteccoes struct {
	// Deteccoes vêm sempre da mais nova para a mais velha, nas duas direções.
	Deteccoes []deteccao `json:"deteccoes"`
	// Fim diz que não há mais nada na direção pedida. Pode vir falso numa
	// página que por acaso pegou a última detecção; a próxima vem vazia e com
	// Fim verdadeiro.
	Fim bool `json:"fim"`
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
//	area=...     só os objetos dentro de uma área; ver filtroDaQuery.
//	rule=...     a regra da área: touch ou foot (o padrão).
//
// A página nunca parte um instante ao meio: se a última detecção empata com
// outras no mesmo milissegundo, em outra câmera, elas vêm juntas e a página
// passa um pouco do limite. Sem isso o cursor `antes` pularia as que ficaram
// de fora.
func (s *Server) handleDeteccoes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Has("antes") && q.Has("depois") {
		writeError(w, http.StatusBadRequest, "use antes ou depois, não os dois")
		return
	}
	descendo := !q.Has("depois")
	cursor := int64(math.MaxInt64)
	nome := "antes"
	if !descendo {
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

	limite := limitePadraoDeDeteccoes
	if q.Has("limite") {
		v, err := strconv.Atoi(q.Get("limite"))
		if err != nil || v < 1 || v > limiteMaximoDeDeteccoes {
			writeError(w, http.StatusBadRequest,
				fmt.Sprintf("parâmetro limite inválido (de 1 a %d)", limiteMaximoDeDeteccoes))
			return
		}
		limite = v
	}

	cams, filtro, err := s.filtroDaQuery(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	pagina, err := paginaDeDeteccoesDe(cams, filtro, cursor, descendo, limite)
	if err != nil {
		s.fail(w, "lendo as detecções", err)
		return
	}
	writeJSON(w, pagina)
}

// handleDiasDeDeteccao devolve os dias com pelo menos uma detecção, em ordem
// crescente, e quantas detecções cada um tem. É o que o calendário da tela de
// Detecções deixa escolher, e o número que ele mostra em cada dia.
//
//	cams, familias, area e rule: os mesmos de handleDeteccoes.
func (s *Server) handleDiasDeDeteccao(w http.ResponseWriter, r *http.Request) {
	cams, filtro, err := s.filtroDaQuery(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	dias, porDia, err := diasComDeteccao(cams, filtro)
	if err != nil {
		s.fail(w, "lendo os dias com detecção", err)
		return
	}
	writeJSON(w, map[string]any{"dias": dias, "deteccoesPorDia": porDia})
}

// diasComDeteccao percorre os dias com gravação e conta, em cada um, as
// detecções do filtro; o dia sem nenhuma fica de fora. Contar obriga a ler
// todas as câmeras de cada dia: no hardware de referência, com o cache de
// disco frio, a primeira chamada leva ~1,3 s, e as seguintes saem da memória
// do store em ~5 ms. Dia sem gravação fica de fora mesmo com marca - a
// rolagem também não o alcança, porque anda pelos mesmos diasDasCameras.
func diasComDeteccao(cams []*store.Camera, filtro filtroDeObjetos) ([]string, map[string]int, error) {
	dias := []string{}
	porDia := map[string]int{}
	for _, dia := range diasDasCameras(cams) {
		n := 0
		for _, cam := range cams {
			marcas, err := cam.ObjetosDoDia(dia)
			if err != nil {
				return nil, nil, fmt.Errorf("%s, %s: %w", cam.ID, dia, err)
			}
			n += contaDeteccoes(marcas, filtro)
		}
		if n > 0 {
			dias = append(dias, dia)
			porDia[dia] = n
		}
	}
	return dias, porDia, nil
}

// contaDeteccoes conta as detecções de uma câmera num dia como o agrupa as
// monta: uma por instante, por mais famílias que o quadro tenha.
func contaDeteccoes(marcas []store.Evento, filtro filtroDeObjetos) int {
	n := 0
	ultimo := int64(-1)
	for _, m := range marcas {
		if m.InstanteMs == ultimo || !filtro.passa(m) {
			continue
		}
		n++
		ultimo = m.InstanteMs
	}
	return n
}

// paginaDeDeteccoesDe junta as câmeras dia a dia, a partir do dia do cursor e
// na direção pedida, até encher a página. Cada dia de cada câmera vem da
// memória do store; só o primeiro pedido de um dia lê o disco.
func paginaDeDeteccoesDe(cams []*store.Camera, filtro filtroDeObjetos,
	cursor int64, descendo bool, limite int) (paginaDeDeteccoes, error) {

	dias := diasDasCameras(cams)
	if descendo {
		slices.Reverse(dias)
	}
	var diaDoCursor string
	if cursor != math.MaxInt64 {
		diaDoCursor = time.UnixMilli(cursor).Format(store.DayLayout)
	}

	// Na direção do rolar: do mais novo para o mais velho descendo, e o
	// contrário subindo. A resposta subindo é revertida no fim, então o
	// desempate também inverte: um mesmo instante sai sempre na mesma ordem
	// de câmera, venha de que direção vier.
	vemAntes := func(a, b deteccao) bool {
		if a.InstanteMs != b.InstanteMs {
			return (a.InstanteMs > b.InstanteMs) == descendo
		}
		return (a.Cam < b.Cam) == descendo
	}
	passaDoCursor := func(t int64) bool {
		if descendo {
			return t < cursor
		}
		return t > cursor
	}

	out := paginaDeDeteccoes{Deteccoes: []deteccao{}, Fim: true}
	for i, dia := range dias {
		if diaDoCursor != "" && (descendo && dia > diaDoCursor || !descendo && dia < diaDoCursor) {
			continue
		}
		var doDia []deteccao
		for _, cam := range cams {
			marcas, err := cam.ObjetosDoDia(dia)
			if err != nil {
				return out, fmt.Errorf("%s, %s: %w", cam.ID, dia, err)
			}
			doDia = agrupa(doDia, cam.ID, marcas, filtro, passaDoCursor)
		}
		sort.Slice(doDia, func(a, b int) bool { return vemAntes(doDia[a], doDia[b]) })
		out.Deteccoes = append(out.Deteccoes, doDia...)
		if len(out.Deteccoes) < limite {
			continue
		}

		// Corta no limite, mas leva junto o que empata com a última.
		corte := limite
		ultimo := out.Deteccoes[corte-1].InstanteMs
		for corte < len(out.Deteccoes) && out.Deteccoes[corte].InstanteMs == ultimo {
			corte++
		}
		sobrou := corte < len(out.Deteccoes) || i < len(dias)-1
		out.Deteccoes = out.Deteccoes[:corte]
		out.Fim = !sobrou
		break
	}
	if !descendo {
		slices.Reverse(out.Deteccoes)
	}
	return out, nil
}

// agrupa junta as marcas de uma câmera num dia em detecções, uma por instante,
// e as acrescenta a `em`. As marcas vêm em ordem de instante, então as do
// mesmo instante estão lado a lado.
func agrupa(em []deteccao, cam string, marcas []store.Evento,
	filtro filtroDeObjetos, passaDoCursor func(int64) bool) []deteccao {

	aberta := -1 // índice, em `em`, da detecção do instante corrente
	for _, m := range marcas {
		if !passaDoCursor(m.InstanteMs) || !filtro.passa(m) {
			continue
		}
		obj := objetoNoQuadro{Familia: m.Familia, Classe: m.Classe, Score: m.Score, Caixa: m.Caixa}
		if aberta >= 0 && em[aberta].InstanteMs == m.InstanteMs {
			d := &em[aberta]
			d.Objetos = append(d.Objetos, obj)
			d.TemQuadro = d.TemQuadro || m.TemQuadro
			continue
		}
		em = append(em, deteccao{Cam: cam, InstanteMs: m.InstanteMs, QuadroMs: m.QuadroMs,
			TemQuadro: m.TemQuadro, Objetos: []objetoNoQuadro{obj}})
		aberta = len(em) - 1
	}
	return em
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
type filtroDeObjetos struct {
	// familias nil é "todas".
	familias map[string]bool
	// regra nil é "sem área"; com ela, a área é a da câmera do filtro.
	regra area.RegraDaArea
	area  area.Area
}

// passa diz se a marca fica. Com área, a marca sem caixa, de antes de a caixa
// ser gravada, fica de fora: não há como saber onde ela estava.
func (f filtroDeObjetos) passa(m store.Evento) bool {
	if f.familias != nil && !f.familias[m.Familia] {
		return false
	}
	if f.regra != nil {
		return m.Caixa != nil && f.regra.Dentro(f.area, *m.Caixa)
	}
	return true
}

// filtroDaQuery resolve os parâmetros que a lista e o calendário dividem:
// `cams`, `familias`, `area` e `rule`.
//
// `area=<cam>:<x>,<y>,<x>,<y>,...` é uma área desenhada sobre a imagem da
// câmera, com X e Y em milésimos do quadro (inteiros de 0 a 1000), para a URL
// ficar curta. A área é de uma câmera só, então a lista passa a ser só dela:
// com `cams` junto, a câmera da área tem de estar nele.
func (s *Server) filtroDaQuery(q url.Values) ([]*store.Camera, filtroDeObjetos, error) {
	var f filtroDeObjetos
	cams, err := s.camerasDoFiltro(q.Get("cams"))
	if err != nil {
		return nil, f, err
	}
	if f.familias, err = familiasDoFiltro(q.Get("familias")); err != nil {
		return nil, f, err
	}
	if !q.Has("area") {
		if q.Has("rule") {
			return nil, f, errors.New("parâmetro rule sem area")
		}
		return cams, f, nil
	}

	cam, a, err := areaDoFiltro(q.Get("area"))
	if err != nil {
		return nil, f, err
	}
	if !s.knownCamera(cam) {
		return nil, f, fmt.Errorf("câmera %q da área não cadastrada", cam)
	}
	if q.Has("cams") && !slices.ContainsFunc(cams, func(c *store.Camera) bool { return c.ID == cam }) {
		return nil, f, fmt.Errorf("a câmera %q da área não está em cams", cam)
	}
	if f.regra, err = area.NovaRegra(q.Get("rule")); err != nil {
		return nil, f, err
	}
	f.area = a
	return []*store.Camera{s.store.Camera(cam)}, f, nil
}

// areaDoFiltro lê `<cam>:<x>,<y>,...`, em milésimos, e devolve a câmera e a
// área em fração do quadro.
func areaDoFiltro(v string) (string, area.Area, error) {
	i := strings.LastIndexByte(v, ':')
	if i <= 0 {
		return "", area.Area{}, errors.New("parâmetro area inválido: use <câmera>:<x>,<y>,<x>,<y> e assim por diante")
	}
	cam, numeros := v[:i], strings.Split(v[i+1:], ",")
	if len(numeros)%2 != 0 {
		return "", area.Area{}, errors.New("parâmetro area inválido: X e Y vão aos pares")
	}
	pontos := make([]area.Ponto, 0, len(numeros)/2)
	for j := 0; j < len(numeros); j += 2 {
		x, errX := strconv.Atoi(numeros[j])
		y, errY := strconv.Atoi(numeros[j+1])
		if errX != nil || errY != nil || x < 0 || x > 1000 || y < 0 || y > 1000 {
			return "", area.Area{}, fmt.Errorf("parâmetro area inválido: %q,%q não são milésimos de 0 a 1000",
				numeros[j], numeros[j+1])
		}
		pontos = append(pontos, area.Ponto{float64(x) / 1000, float64(y) / 1000})
	}
	a, err := area.Nova(pontos)
	if err != nil {
		return "", area.Area{}, fmt.Errorf("parâmetro area inválido: %w", err)
	}
	return cam, a, nil
}

// camerasDoFiltro resolve `cams=a,b`. Vazio são todas as cadastradas. Um ID
// que não é de câmera cadastrada é recusado, pelo mesmo motivo do camParam:
// ele vira caminho no disco.
func (s *Server) camerasDoFiltro(lista string) ([]*store.Camera, error) {
	var out []*store.Camera
	if lista == "" {
		for _, c := range s.mgr.Cameras() {
			out = append(out, s.store.Camera(c.ID))
		}
		return out, nil
	}
	for id := range strings.SplitSeq(lista, ",") {
		if !s.knownCamera(id) {
			return nil, fmt.Errorf("câmera %q não cadastrada", id)
		}
		out = append(out, s.store.Camera(id))
	}
	return out, nil
}

// familiasDoFiltro resolve `familias=pessoa,veiculo`. Vazio devolve nil, que é
// "todas" - inclusive uma família que um detector futuro venha a marcar.
func familiasDoFiltro(lista string) (map[string]bool, error) {
	if lista == "" {
		return nil, nil
	}
	out := map[string]bool{}
	for f := range strings.SplitSeq(lista, ",") {
		if !slices.Contains(detect.Familias, detect.Familia(f)) {
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
func (s *Server) handleQuadroDaDeteccao(w http.ResponseWriter, r *http.Request) {
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

	f, err := os.Open(cam.QuadroPath(time.UnixMilli(t).Format(store.DayLayout), t))
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
