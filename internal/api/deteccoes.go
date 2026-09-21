package api

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

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

	cams, err := s.camerasDoFiltro(q.Get("cams"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	familias, err := familiasDoFiltro(q.Get("familias"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	pagina, err := paginaDeDeteccoesDe(cams, familias, cursor, descendo, limite)
	if err != nil {
		s.fail(w, "lendo as detecções", err)
		return
	}
	writeJSON(w, pagina)
}

// paginaDeDeteccoesDe junta as câmeras dia a dia, a partir do dia do cursor e
// na direção pedida, até encher a página. Cada dia de cada câmera vem da
// memória do store; só o primeiro pedido de um dia lê o disco.
func paginaDeDeteccoesDe(cams []*store.Camera, familias map[string]bool,
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
			doDia = agrupa(doDia, cam.ID, marcas, familias, passaDoCursor)
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
	familias map[string]bool, passaDoCursor func(int64) bool) []deteccao {

	aberta := -1 // índice, em `em`, da detecção do instante corrente
	for _, m := range marcas {
		if !passaDoCursor(m.InstanteMs) || familias != nil && !familias[m.Familia] {
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
	for _, id := range strings.Split(lista, ",") {
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
	for _, f := range strings.Split(lista, ",") {
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
