package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/fmp4"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// gapTolerance é a folga para considerar dois segmentos contíguos na linha do
// tempo. Segmentos consecutivos encostam com alguns milissegundos de diferença
// por causa do arredondamento da duração; sem essa folga a barra de 24h ficaria
// coberta de buracos falsos de 1 pixel.
const gapTolerance = 2000 * time.Millisecond

// maxExportSpan limita a janela de exportação. Sem teto, um pedido de 30 dias
// tentaria emendar dezenas de milhares de segmentos numa única resposta.
const maxExportSpan = 6 * time.Hour

// handleDays lista os dias com gravação — as bolinhas do calendário.
func (s *Server) handleDays(w http.ResponseWriter, r *http.Request) {
	cam, err := s.camParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]any{"cam": cam.ID, "days": cam.Days()})
}

// timelineResponse é deliberadamente compacto: um dia com segmentos de 1 minuto
// tem 1440 entradas, e a forma verbosa (um objeto com chaves por segmento)
// triplicaria o tamanho da resposta que a timeline busca a cada troca de dia.
type timelineResponse struct {
	Cam  string `json:"cam"`
	From int64  `json:"from"`
	To   int64  `json:"to"`
	// Ranges são faixas contíguas [início, fim] para desenhar a barra.
	Ranges [][2]int64 `json:"ranges"`
	// Gens é a tabela de gerações de init; Segments referencia por índice.
	Gens []string `json:"gens"`
	// Segments é [inícioMs, duraçãoMs, índiceNaTabelaDeGens].
	Segments [][3]int64 `json:"segments"`
}

// handleTimeline devolve, de uma vez, o que a barra precisa para desenhar
// (ranges) e o que o player precisa para tocar (segments).
func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	cam, err := s.camParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	from, to, err := s.rangeParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	entries, err := cam.Range(from, to)
	if err != nil {
		s.fail(w, "lendo o índice", err)
		return
	}

	resp := timelineResponse{
		Cam: cam.ID, From: from, To: to,
		Ranges: [][2]int64{}, Gens: []string{}, Segments: [][3]int64{},
	}
	genIdx := map[string]int{}

	for _, e := range entries {
		i, ok := genIdx[e.Gen]
		if !ok {
			i = len(resp.Gens)
			genIdx[e.Gen] = i
			resp.Gens = append(resp.Gens, e.Gen)
		}
		resp.Segments = append(resp.Segments, [3]int64{e.StartMs, e.DurMs, int64(i)})

		// Estende a faixa atual se o segmento encosta nela; senão abre outra.
		n := len(resp.Ranges)
		if n > 0 && e.StartMs-resp.Ranges[n-1][1] <= gapTolerance.Milliseconds() {
			if end := e.EndMs(); end > resp.Ranges[n-1][1] {
				resp.Ranges[n-1][1] = end
			}
			continue
		}
		resp.Ranges = append(resp.Ranges, [2]int64{e.StartMs, e.EndMs()})
	}
	writeJSON(w, resp)
}

// handleInit serve o init segment de uma geração.
func (s *Server) handleInit(w http.ResponseWriter, r *http.Request) {
	cam, err := s.camParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	gen := r.URL.Query().Get("g")
	if err := validGen(gen); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// O nome é o hash do conteúdo, então o arquivo nunca muda: pode ser
	// cacheado para sempre.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeFile(w, r, cam.InitPath(gen))
}

// handleSegment serve os fragmentos de um segmento, PULANDO o init.
//
// O init é servido à parte e uma vez só (via EXT-X-MAP ou pelo player), então
// reenviá-lo a cada segmento seria desperdício — e, no MSE, reanexar o init a
// cada append é desnecessário.
func (s *Server) handleSegment(w http.ResponseWriter, r *http.Request) {
	cam, entry, err := s.segmentParam(r)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	f, err := os.Open(cam.SegmentPath(entry.StartMs))
	if err != nil {
		writeError(w, http.StatusNotFound, "segmento não encontrado")
		return
	}
	defer f.Close()

	body := io.NewSectionReader(f, entry.InitSize, entry.Size-entry.InitSize)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "video/mp4")
	// ServeContent cuida de Range e de If-None-Match sozinho.
	http.ServeContent(w, r, "", time.UnixMilli(entry.StartMs), body)
}

// handleThumb devolve init + primeiro fragmento: um MP4 de um frame só.
//
// É a miniatura da timeline, e o ponto todo é que o Pi não decodifica nada —
// ele só recorta bytes que já estão no disco. Quem decodifica é o navegador.
func (s *Server) handleThumb(w http.ResponseWriter, r *http.Request) {
	cam, entry, err := s.segmentParam(r)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if entry.FirstFrag <= 0 {
		writeError(w, http.StatusNotFound, "segmento sem fragmento inicial indexado")
		return
	}

	f, err := os.Open(cam.SegmentPath(entry.StartMs))
	if err != nil {
		writeError(w, http.StatusNotFound, "segmento não encontrado")
		return
	}
	defer f.Close()

	body := io.NewSectionReader(f, 0, entry.InitSize+entry.FirstFrag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, "", time.UnixMilli(entry.StartMs), body)
}

// handlePlaylist gera uma playlist HLS VOD do intervalo pedido.
//
// O player da interface usa MSE direto, mas a playlist dá compatibilidade
// imediata com VLC, ffplay e Safari — o que também a torna a ferramenta mais
// prática para depurar uma gravação sem abrir o navegador.
func (s *Server) handlePlaylist(w http.ResponseWriter, r *http.Request) {
	cam, err := s.camParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	from, to, err := s.rangeParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entries, err := cam.Range(from, to)
	if err != nil {
		s.fail(w, "lendo o índice", err)
		return
	}
	if len(entries) == 0 {
		writeError(w, http.StatusNotFound, "nenhuma gravação no intervalo")
		return
	}

	var maxDur int64
	for _, e := range entries {
		if e.DurMs > maxDur {
			maxDur = e.DurMs
		}
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	fmt.Fprint(w, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-PLAYLIST-TYPE:VOD\n")
	fmt.Fprintf(w, "#EXT-X-TARGETDURATION:%d\n", (maxDur+999)/1000)

	var (
		prevEnd int64
		prevGen string
	)
	for i, e := range entries {
		// Descontinuidade em buraco de gravação e em troca de init: nos dois
		// casos o decodificador precisa ser reiniciado.
		gap := i > 0 && e.StartMs-prevEnd > gapTolerance.Milliseconds()
		if i > 0 && (gap || e.Gen != prevGen) {
			fmt.Fprint(w, "#EXT-X-DISCONTINUITY\n")
		}
		if i == 0 || e.Gen != prevGen {
			fmt.Fprintf(w, "#EXT-X-MAP:URI=\"init?cam=%s&g=%s\"\n", cam.ID, e.Gen)
		}
		fmt.Fprintf(w, "#EXT-X-PROGRAM-DATE-TIME:%s\n",
			time.UnixMilli(e.StartMs).Format(time.RFC3339Nano))
		fmt.Fprintf(w, "#EXTINF:%.3f,\n", float64(e.DurMs)/1000)
		fmt.Fprintf(w, "seg?cam=%s&t=%d\n", cam.ID, e.StartMs)

		prevEnd, prevGen = e.EndMs(), e.Gen
	}
	fmt.Fprint(w, "#EXT-X-ENDLIST\n")
}

// handleExport emenda os segmentos de um intervalo num único MP4.
//
// Custa quase nada: os bytes de mídia são copiados como estão e só o tfdt de
// cada fragmento é deslocado para formar uma linha do tempo contínua. Nada é
// decodificado nem reencodado, e a resposta é escrita em fluxo, sem carregar o
// arquivo inteiro na memória.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	cam, err := s.camParam(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	from, to, err := s.rangeParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if time.Duration(to-from)*time.Millisecond > maxExportSpan {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("intervalo maior que o máximo de %s", maxExportSpan))
		return
	}

	entries, err := cam.Range(from, to)
	if err != nil {
		s.fail(w, "lendo o índice", err)
		return
	}
	if len(entries) == 0 {
		writeError(w, http.StatusNotFound, "nenhuma gravação no intervalo")
		return
	}

	// Um MP4 só pode ter um init. Atravessar uma troca de geração exigiria
	// reencodar ou entregar dois arquivos; recusar é mais honesto que entregar
	// algo que só toca até a metade.
	gen := entries[0].Gen
	for _, e := range entries {
		if e.Gen != gen {
			writeError(w, http.StatusConflict,
				"o intervalo atravessa uma troca de codec; exporte os trechos separadamente")
			return
		}
	}

	initBytes, err := os.ReadFile(cam.InitPath(gen))
	if err != nil {
		s.fail(w, "lendo o init segment", err)
		return
	}
	movie, err := fmp4.ParseMoov(initBytes[findMoov(initBytes):])
	if err != nil {
		s.fail(w, "init segment ilegível", err)
		return
	}
	video, ok := movie.VideoTrack()
	if !ok {
		s.fail(w, "init sem trilha de vídeo", errors.New("sem trilha de vídeo"))
		return
	}

	name := fmt.Sprintf("%s_%s.mp4", cam.ID, time.UnixMilli(from).Format("2006-01-02_15-04-05"))
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	if _, err := w.Write(initBytes); err != nil {
		return
	}

	// offsetMs acumula a duração já escrita: cada segmento começa em zero, e é
	// esse acumulado que os emenda numa linha do tempo contínua.
	var offsetMs int64
	for _, e := range entries {
		deltas := make(map[uint32]int64, len(movie.Tracks))
		for _, t := range movie.Tracks {
			deltas[t.ID] = offsetMs * int64(t.Timescale) / 1000
		}
		if err := s.appendSegment(w, cam, e, video.ID, deltas); err != nil {
			// A resposta já começou; não dá para trocar o status agora.
			s.log.Warn("exportação interrompida", "cam", cam.ID, "t", e.StartMs, "erro", err)
			return
		}
		offsetMs += e.DurMs
	}
}

// appendSegment copia os fragmentos de um segmento para w, deslocando o tfdt.
func (s *Server) appendSegment(w io.Writer, cam *store.Camera, e store.Entry,
	videoTrackID uint32, deltas map[uint32]int64) error {

	f, err := os.Open(cam.SegmentPath(e.StartMs))
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Seek(e.InitSize, io.SeekStart); err != nil {
		return err
	}

	rd := fmp4.NewReader(f)
	for {
		typ, box, err := rd.NextBox()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return err
		}
		if typ == "moof" {
			if err := fmp4.ShiftMoof(box, deltas); err != nil {
				return err
			}
		}
		if _, err := w.Write(box); err != nil {
			return err
		}
	}
}

// findMoov localiza a caixa moov dentro do init (que começa com ftyp).
func findMoov(init []byte) int {
	for off := 0; off+8 <= len(init); {
		size := int(be32u(init[off:]))
		if size < 8 || off+size > len(init) {
			break
		}
		if string(init[off+4:off+8]) == "moov" {
			return off
		}
		off += size
	}
	return 0
}

func be32u(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// --- parâmetros ------------------------------------------------------------

func (s *Server) camParam(r *http.Request) (*store.Camera, error) {
	id := r.URL.Query().Get("cam")
	if !s.knownCamera(id) {
		return nil, fmt.Errorf("câmera %q não cadastrada", id)
	}
	return s.store.Camera(id), nil
}

// segmentParam resolve cam+t para a entrada de índice correspondente.
//
// A entrada é consultada no índice em vez de o caminho ser montado direto a
// partir de t: é o índice que sabe InitSize e FirstFrag, e passar por ele
// garante que só se sirva segmento que o dwnvr realmente gravou.
func (s *Server) segmentParam(r *http.Request) (*store.Camera, store.Entry, error) {
	cam, err := s.camParam(r)
	if err != nil {
		return nil, store.Entry{}, err
	}
	t, err := strconv.ParseInt(r.URL.Query().Get("t"), 10, 64)
	if err != nil {
		return nil, store.Entry{}, errors.New("parâmetro t inválido")
	}

	entries, err := cam.LoadDay(time.UnixMilli(t).Format(store.DayLayout))
	if err != nil {
		return nil, store.Entry{}, err
	}
	for _, e := range entries {
		if e.StartMs == t {
			return cam, e, nil
		}
	}
	return nil, store.Entry{}, errors.New("segmento não encontrado no índice")
}

func (s *Server) rangeParams(r *http.Request) (from, to int64, err error) {
	q := r.URL.Query()

	// Atalho: day=2026-08-08 equivale ao dia inteiro em hora local.
	if day := q.Get("day"); day != "" {
		d, perr := time.ParseInLocation(store.DayLayout, day, time.Local)
		if perr != nil {
			return 0, 0, errors.New("parâmetro day inválido (use AAAA-MM-DD)")
		}
		return d.UnixMilli(), d.AddDate(0, 0, 1).UnixMilli(), nil
	}

	if from, err = strconv.ParseInt(q.Get("from"), 10, 64); err != nil {
		return 0, 0, errors.New("parâmetro from inválido")
	}
	if to, err = strconv.ParseInt(q.Get("to"), 10, 64); err != nil {
		return 0, 0, errors.New("parâmetro to inválido")
	}
	if to <= from {
		return 0, 0, errors.New("to precisa ser maior que from")
	}
	return from, to, nil
}

// validGen recusa qualquer coisa que não seja o hash hexadecimal esperado: a
// geração vira nome de arquivo, e aceitar caminho aqui abriria travessia de
// diretório.
func validGen(gen string) error {
	if gen == "" {
		return errors.New("parâmetro g obrigatório")
	}
	if len(gen) > 32 {
		return errors.New("parâmetro g inválido")
	}
	for _, c := range gen {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return errors.New("parâmetro g inválido")
		}
	}
	return nil
}
