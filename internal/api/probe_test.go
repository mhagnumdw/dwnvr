package api

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
)

// moovComAudio é o mínimo que o ParseMoov aceita: um ftyp, uma trilha de vídeo e
// uma de áudio. É o que a sonda lê para saber que a conexão entregou mídia.
func moovComAudio() []byte {
	caixa := func(typ string, partes ...[]byte) []byte {
		corpo := []byte{}
		for _, p := range partes {
			corpo = append(corpo, p...)
		}
		b := make([]byte, 8, 8+len(corpo))
		binary.BigEndian.PutUint32(b[0:4], uint32(8+len(corpo)))
		copy(b[4:8], typ)
		return append(b, corpo...)
	}
	u32 := func(v uint32) []byte {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, v)
		return b
	}
	trilha := func(id uint32, handler string) []byte {
		return caixa("trak",
			caixa("tkhd", u32(0), u32(0), u32(0), u32(id)),
			caixa("mdia",
				caixa("mdhd", u32(0), u32(0), u32(0), u32(90000), u32(0)),
				caixa("hdlr", u32(0), u32(0), []byte(handler))))
	}
	return append(caixa("ftyp", []byte("iso5")),
		caixa("moov", caixa("mvhd", u32(0)), trilha(1, "vide"), trilha(2, "soun"))...)
}

// serverComGo2RTC monta um Server apontando para um go2rtc falso, e devolve o
// contador de aberturas de stream.mp4 - que é a métrica que interessa: cada
// abertura é uma conexão com a câmera.
func serverComGo2RTC(t *testing.T, streams string) (*Server, *int) {
	t.Helper()
	aberturas := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/streams":
			_, _ = w.Write([]byte(streams))
		case "/api/stream.mp4":
			aberturas++
			_, _ = w.Write(moovComAudio())
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	return &Server{
		client: go2rtc.New(config.Go2RTC{URL: srv.URL}),
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, &aberturas
}

func probe(t *testing.T, s *Server, src string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleProbeStream(rec, httptest.NewRequest(http.MethodGet, "/api/streams/probe?src="+src, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("resposta ilegível: %v", err)
	}
	return out
}

// Câmera que já grava tem consumidor, e com consumidor o go2rtc já preenche o
// `medias`. Sondar aí seria abrir uma segunda sessão com a câmera para descobrir
// o que a resposta já dizia.
func TestProbeNaoSondaStreamComProdutorVivo(t *testing.T) {
	s, aberturas := serverComGo2RTC(t, `{"cam_x": {"producers": [
		{"url": "rtsp://x", "medias": ["video, recvonly, H265", "audio, recvonly, PCMA/16000"]}]}}`)

	r := probe(t, s, "cam_x")
	if r["hasAudio"] != true {
		t.Errorf("hasAudio = %v, esperava true", r["hasAudio"])
	}
	if r["probed"] != false {
		t.Errorf("probed = %v, esperava false", r["probed"])
	}
	if *aberturas != 0 {
		t.Errorf("abriu o stream %d vezes, esperava nenhuma", *aberturas)
	}
}

// ocioso é o recorte fiel de um stream que ninguém está consumindo. O go2rtc
// LISTA o produtor configurado desde sempre, com a url - só não preenche as
// trilhas enquanto não conecta na câmera. Ter modelado isso como
// `"producers": null` fazia a sonda nunca disparar em stream nenhum, e o teste
// existe para que essa suposição errada não volte.
const ocioso = `{"cam_x": {"producers": [
	{"url": "exec:ffmpeg -i avsynctest -f rtsp {output}"}], "consumers": []}}`

func TestProbeSondaStreamOciosoUmaVezSo(t *testing.T) {
	s, aberturas := serverComGo2RTC(t, ocioso)

	r := probe(t, s, "cam_x")
	if r["hasAudio"] != true {
		t.Errorf("hasAudio = %v, esperava true", r["hasAudio"])
	}
	if r["probed"] != true {
		t.Errorf("probed = %v, esperava true", r["probed"])
	}

	// Abrir e fechar o formulário de novo não pode custar outra conexão.
	r = probe(t, s, "cam_x")
	if r["hasAudio"] != true {
		t.Errorf("cache devolveu hasAudio = %v", r["hasAudio"])
	}
	if r["probed"] != false {
		t.Errorf("probed = %v na segunda chamada, esperava false", r["probed"])
	}
	if *aberturas != 1 {
		t.Errorf("abriu o stream %d vezes, esperava 1", *aberturas)
	}
}

func TestProbeRecusaStreamInexistente(t *testing.T) {
	s, _ := serverComGo2RTC(t, ocioso)

	rec := httptest.NewRecorder()
	s.handleProbeStream(rec, httptest.NewRequest(http.MethodGet, "/api/streams/probe?src=cam_y", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("HTTP %d, esperava 404", rec.Code)
	}
}
