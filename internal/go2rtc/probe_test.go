package go2rtc

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/config"
)

// As caixas são montadas à mão porque o pacote fmp4 guarda os helpers dele nos
// próprios testes, e o que se quer provar aqui é o comportamento da sonda, não a
// leitura de MP4 - que já tem teste em internal/fmp4.
func caixa(typ string, partes ...[]byte) []byte {
	corpo := []byte{}
	for _, p := range partes {
		corpo = append(corpo, p...)
	}
	b := make([]byte, 8, 8+len(corpo))
	binary.BigEndian.PutUint32(b[0:4], uint32(8+len(corpo)))
	copy(b[4:8], typ)
	return append(b, corpo...)
}

func u32b(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

// trilha monta o mínimo que o ParseMoov exige: id, timescale e handler.
func trilha(id uint32, handler string) []byte {
	tkhd := caixa("tkhd", u32b(0), u32b(0), u32b(0), u32b(id))
	mdhd := caixa("mdhd", u32b(0), u32b(0), u32b(0), u32b(90000), u32b(0))
	hdlr := caixa("hdlr", u32b(0), u32b(0), []byte(handler))
	return caixa("trak", tkhd, caixa("mdia", mdhd, hdlr))
}

func initSegment(comAudio bool) []byte {
	trilhas := [][]byte{caixa("mvhd", u32b(0)), trilha(1, "vide")}
	if comAudio {
		trilhas = append(trilhas, trilha(2, "soun"))
	}
	return append(caixa("ftyp", []byte("iso5")), caixa("moov", trilhas...)...)
}

// go2rtcFalso responde os dois endpoints que a sonda usa: o stream.mp4, que
// entrega o init segment e depois fica calado, e o /api/streams, que só passa a
// listar `medias` DEPOIS de alguém abrir o stream - que é exatamente o
// comportamento do go2rtc de verdade e a razão de a sonda existir.
func go2rtcFalso(t *testing.T, comAudio bool, medias string) (*httptest.Server, *int) {
	t.Helper()
	aberturas := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/stream.mp4":
			aberturas++
			_, _ = w.Write(initSegment(comAudio))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case "/api/streams":
			if aberturas == 0 || medias == "" {
				_, _ = w.Write([]byte(`{"cam_x": {"producers": null}}`))
				return
			}
			_, _ = w.Write([]byte(`{"cam_x": {"producers": [{"url": "rtsp://x", "medias": [` + medias + `]}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &aberturas
}

func TestProbeAudioAchaAudioDeStreamOcioso(t *testing.T) {
	srv, aberturas := go2rtcFalso(t, true,
		`"video, recvonly, H265", "audio, recvonly, PCMA/16000"`)

	c := New(config.Go2RTC{URL: srv.URL})
	p, err := c.ProbeAudio(context.Background(), "cam_x")
	if err != nil {
		t.Fatalf("ProbeAudio: %v", err)
	}
	if !p.HasAudio {
		t.Error("a câmera entrega áudio e a sonda disse que não")
	}
	// O codec é o que a CÂMERA anuncia, não o fLaC da conversão: é ele que diz
	// ao usuário o que ele vai gravar.
	if len(p.Codecs) != 1 || p.Codecs[0] != "PCMA/16000" {
		t.Errorf("codecs = %v, esperava [PCMA/16000]", p.Codecs)
	}
	// Uma sonda é uma conexão com a câmera. Duas seriam desperdício num
	// dispositivo que grava nove câmeras com 1,5 GB de RAM.
	if *aberturas != 1 {
		t.Errorf("abriu o stream %d vezes, esperava 1", *aberturas)
	}
}

func TestProbeAudioEmCameraMuda(t *testing.T) {
	srv, _ := go2rtcFalso(t, false, `"video, recvonly, H265"`)

	c := New(config.Go2RTC{URL: srv.URL})
	p, err := c.ProbeAudio(context.Background(), "cam_x")
	if err != nil {
		t.Fatalf("ProbeAudio: %v", err)
	}
	if p.HasAudio {
		t.Error("a câmera não tem áudio e a sonda disse que tem")
	}
	if len(p.Codecs) != 0 {
		t.Errorf("codecs = %v, esperava vazio", p.Codecs)
	}
}

// Sem o /api/streams a sonda ainda responde: o moov já é prova suficiente de
// que existe trilha de áudio, só falta o nome do codec.
func TestProbeAudioSemMediasCaiNoMoov(t *testing.T) {
	srv, _ := go2rtcFalso(t, true, "")

	c := New(config.Go2RTC{URL: srv.URL})
	p, err := c.ProbeAudio(context.Background(), "cam_x")
	if err != nil {
		t.Fatalf("ProbeAudio: %v", err)
	}
	if !p.HasAudio {
		t.Error("o moov trazia trilha de áudio e a sonda disse que não")
	}
	if len(p.Codecs) != 0 {
		t.Errorf("codecs = %v, esperava vazio sem o medias", p.Codecs)
	}
}

func TestProbeAudioPedeOStreamEmFLAC(t *testing.T) {
	var query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/stream.mp4" {
			query = r.URL.RawQuery
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := New(config.Go2RTC{URL: srv.URL})
	if _, err := c.ProbeAudio(context.Background(), "cam_x"); err == nil {
		t.Fatal("esperava erro com o servidor devolvendo 404")
	}
	// mp4=flac é o único filtro que traz a trilha de áudio junto. Pedir
	// video=h264,h265, como faz o modo none, devolveria um moov sem áudio e a
	// sonda mentiria em toda câmera.
	if !strings.Contains(query, "mp4=flac") {
		t.Errorf("a sonda pediu %q, esperava mp4=flac", query)
	}
}
