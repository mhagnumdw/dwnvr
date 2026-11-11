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
	box := func(typ string, partes ...[]byte) []byte {
		body := []byte{}
		for _, p := range partes {
			body = append(body, p...)
		}
		b := make([]byte, 8, 8+len(body))
		binary.BigEndian.PutUint32(b[0:4], uint32(8+len(body)))
		copy(b[4:8], typ)
		return append(b, body...)
	}
	u32 := func(v uint32) []byte {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, v)
		return b
	}
	track := func(id uint32, handler string) []byte {
		return box("trak",
			box("tkhd", u32(0), u32(0), u32(0), u32(id)),
			box("mdia",
				box("mdhd", u32(0), u32(0), u32(0), u32(90000), u32(0)),
				box("hdlr", u32(0), u32(0), []byte(handler))))
	}
	return append(box("ftyp", []byte("iso5")),
		box("moov", box("mvhd", u32(0)), track(1, "vide"), track(2, "soun"))...)
}

// serverComGo2RTC monta um Server apontando para um go2rtc falso, e devolve o
// contador de aberturas de stream.mp4 - que é a métrica que interessa: cada
// abertura é uma conexão com a câmera.
func serverWithGo2RTC(t *testing.T, streams string) (*Server, *int) {
	t.Helper()
	opens := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/streams":
			_, _ = w.Write([]byte(streams))
		case "/api/stream.mp4":
			opens++
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
	}, &opens
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
func TestProbeDoesNotProbeStreamWithLiveProducer(t *testing.T) {
	s, opens := serverWithGo2RTC(t, `{"cam_x": {"producers": [
		{"url": "rtsp://x", "medias": ["video, recvonly, H265", "audio, recvonly, PCMA/16000"]}]}}`)

	r := probe(t, s, "cam_x")
	if r["hasAudio"] != true {
		t.Errorf("hasAudio = %v, esperava true", r["hasAudio"])
	}
	if r["probed"] != false {
		t.Errorf("probed = %v, esperava false", r["probed"])
	}
	if *opens != 0 {
		t.Errorf("abriu o stream %d vezes, esperava nenhuma", *opens)
	}
}

// ocioso é o recorte fiel de um stream que ninguém está consumindo. O go2rtc
// LISTA o produtor configurado desde sempre, com a url - só não preenche as
// trilhas enquanto não conecta na câmera. Ter modelado isso como
// `"producers": null` fazia a sonda nunca disparar em stream nenhum, e o teste
// existe para que essa suposição errada não volte.
const idle = `{"cam_x": {"producers": [
	{"url": "exec:ffmpeg -i avsynctest -f rtsp {output}"}], "consumers": []}}`

func TestProbeProbesIdleStreamOnlyOnce(t *testing.T) {
	s, opens := serverWithGo2RTC(t, idle)

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
	if *opens != 1 {
		t.Errorf("abriu o stream %d vezes, esperava 1", *opens)
	}
}

func TestProbeRejectsNonexistentStream(t *testing.T) {
	s, _ := serverWithGo2RTC(t, idle)

	rec := httptest.NewRecorder()
	s.handleProbeStream(rec, httptest.NewRequest(http.MethodGet, "/api/streams/probe?src=cam_y", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("HTTP %d, esperava 404", rec.Code)
	}
}
