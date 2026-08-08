package go2rtc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/config"
)

// respostaReal é um recorte fiel do /api/streams de um go2rtc de verdade.
//
// O formato importa: `medias` é uma lista de STRINGS no formato SDP, não de
// objetos. Modelá-la como struct fazia o decode falhar inteiro, e o teste
// existe para que essa suposição errada não volte.
const respostaReal = `{
  "cam_jardim": {
    "producers": [{
      "id": 47,
      "format_name": "rtsp",
      "url": "rtsp://admin:senha@192.168.0.109:554/onvif1",
      "medias": ["video, recvonly, H265", "audio, recvonly, PCMA/16000"]
    }],
    "consumers": []
  },
  "cam_portao": {
    "producers": [{
      "url": "rtsp://admin:senha@192.168.0.101:554/onvif2",
      "medias": ["video, recvonly, H265"]
    }]
  },
  "cam_transcodificada": {
    "producers": [{
      "url": "ffmpeg:cam_jardim#audio=aac",
      "medias": ["video, recvonly, H265", "audio, recvonly, MPEG4-GENERIC/16000/1"]
    }]
  },
  "cam_ociosa": {"producers": null, "consumers": null}
}`

func TestStreamsDecodificaRespostaReal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/streams" {
			t.Errorf("caminho inesperado: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respostaReal))
	}))
	defer srv.Close()

	c := New(config.Go2RTC{URL: srv.URL})
	streams, err := c.Streams(context.Background())
	if err != nil {
		t.Fatalf("Streams: %v", err)
	}
	if len(streams) != 4 {
		t.Fatalf("esperava 4 streams, veio %d", len(streams))
	}

	jardim := streams["cam_jardim"].Producers[0]
	if !jardim.HasAudio() {
		t.Error("cam_jardim deveria ter áudio")
	}
	if got := jardim.AudioCodecs(); len(got) != 1 || got[0] != "PCMA/16000" {
		t.Errorf("AudioCodecs = %v, esperava [PCMA/16000]", got)
	}
	if jardim.Transcoding() {
		t.Error("fonte rtsp não é transcodificação")
	}

	portao := streams["cam_portao"].Producers[0]
	if portao.HasAudio() {
		t.Error("cam_portao não tem trilha de áudio")
	}
	if len(portao.AudioCodecs()) != 0 {
		t.Error("AudioCodecs deveria ser vazio sem trilha de áudio")
	}

	if !streams["cam_transcodificada"].Producers[0].Transcoding() {
		t.Error("fonte ffmpeg: deveria ser sinalizada como transcodificação")
	}

	// Uma câmera sem produtor conectado não pode quebrar a listagem: é o
	// estado normal de um stream que ninguém abriu ainda.
	if len(streams["cam_ociosa"].Producers) != 0 {
		t.Error("stream ocioso deveria ter zero produtores")
	}
}

// A URL de captura é o único lugar onde o modo de áudio vira comportamento, e
// é isso que torna a escolha por câmera possível sem mais nenhuma mudança.
func TestStreamURLPorModoDeAudio(t *testing.T) {
	c := New(config.Go2RTC{URL: "http://pi:1984/"})

	tests := []struct {
		modo    string
		quer    string
		naoQuer string
	}{
		{config.AudioNone, "video=h264%2Ch265", "audio"},
		{config.AudioFLAC, "mp4=flac", "video="},
		{config.AudioAAC, "audio=aac", "mp4="},
	}
	for _, tt := range tests {
		got := c.StreamURL("cam_x", tt.modo)
		if !strings.Contains(got, tt.quer) {
			t.Errorf("modo %s: URL %q não contém %q", tt.modo, got, tt.quer)
		}
		if tt.naoQuer != "" && strings.Contains(got, tt.naoQuer) {
			t.Errorf("modo %s: URL %q não deveria conter %q", tt.modo, got, tt.naoQuer)
		}
		if !strings.Contains(got, "src=cam_x") {
			t.Errorf("modo %s: URL %q sem src", tt.modo, got)
		}
	}

	// A barra final da URL base não pode virar barra dupla no caminho.
	if strings.Contains(c.StreamURL("x", config.AudioNone), "//api") {
		t.Error("barra dupla no caminho da URL de captura")
	}
}

// Garante que Stream continua decodificável mesmo se o go2rtc acrescentar
// campos: o dwnvr não pode quebrar a cada versão nova dele.
func TestStreamIgnoraCamposDesconhecidos(t *testing.T) {
	var s Stream
	err := json.Unmarshal([]byte(`{"producers":[{"url":"x","medias":["video, recvonly, H264"],
		"campo_novo":123}],"outro_campo":"z"}`), &s)
	if err != nil {
		t.Fatalf("campos desconhecidos quebraram o decode: %v", err)
	}
	if len(s.Producers) != 1 || s.Producers[0].URL != "x" {
		t.Errorf("produtor não decodificado: %+v", s.Producers)
	}
}
