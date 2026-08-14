package go2rtc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	c := New(config.Go2RTC{URL: "http://go2rtc:1984/"})

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

// streamMudo devolve um servidor que entrega `prefixo` e depois emudece sem
// fechar a conexão — exatamente o que o go2rtc faz quando o produtor RTSP morre
// e que custou 3h38 de gravação em 09/08/2026.
func streamMudo(t *testing.T, prefixo string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(prefixo))
		w.(http.Flusher).Flush()
		// Sem fechar, sem escrever: só o cancelamento do guarda tira daqui.
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOpenStreamDerrubaStreamQueEmudece(t *testing.T) {
	srv := streamMudo(t, "primeiros bytes")

	c := New(config.Go2RTC{URL: srv.URL})
	body, err := c.OpenStream(context.Background(), "cam_x", config.AudioNone, 150*time.Millisecond)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer body.Close()

	inicio := time.Now()
	n, err := io.Copy(io.Discard, body)
	se := time.Since(inicio)

	if err == nil {
		t.Fatal("a leitura deveria falhar quando o go2rtc emudece")
	}
	// A mensagem precisa dizer o que houve: sem isto o log mostra
	// "context canceled" e ninguém entende por que a câmera reconectou.
	if !strings.Contains(err.Error(), "não enviou nada") {
		t.Errorf("erro %q não explica a estagnação", err)
	}
	if n != int64(len("primeiros bytes")) {
		t.Errorf("leu %d bytes antes de desistir, esperava %d", n, len("primeiros bytes"))
	}
	if se > 5*time.Second {
		t.Errorf("demorou %s para detectar a estagnação", se)
	}
}

// O guarda não pode derrubar quem está entregando: uma reconexão a cada 15s
// abriria buracos na gravação em vez de fechá-los.
func TestOpenStreamNaoDerrubaStreamAtivo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 20; i++ {
			if _, err := w.Write([]byte("x")); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			time.Sleep(15 * time.Millisecond)
		}
	}))
	defer srv.Close()

	c := New(config.Go2RTC{URL: srv.URL})
	body, err := c.OpenStream(context.Background(), "cam_x", config.AudioNone, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer body.Close()

	// 20 escritas espaçadas de 15ms passam de 300ms no total, bem além do
	// limiar de 100ms: só um limiar por INATIVIDADE deixa isso passar.
	n, err := io.Copy(io.Discard, body)
	if err != nil {
		t.Fatalf("stream ativo foi derrubado: %v", err)
	}
	if n != 20 {
		t.Errorf("leu %d bytes, esperava 20", n)
	}
}

// Fechar cedo não pode deixar o timer nem o context vazando.
func TestOpenStreamCloseAntesDaEstagnacao(t *testing.T) {
	srv := streamMudo(t, "abc")

	c := New(config.Go2RTC{URL: srv.URL})
	body, err := c.OpenStream(context.Background(), "cam_x", config.AudioNone, time.Hour)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	if err := body.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// Um go2rtc travado ainda aceita a conexão TCP e nunca responde. Sem
// ResponseHeaderTimeout isso trava o Do() para sempre, antes de o stallGuard
// sequer existir — a mesma perda silenciosa, num ponto que o guarda não cobre.
func TestOpenStreamDesisteDeQuemNaoResponde(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // aceita e emudece, sem nunca mandar cabeçalho
	}))
	defer srv.Close()

	c := New(config.Go2RTC{URL: srv.URL})
	c.HTTP.Transport.(*http.Transport).ResponseHeaderTimeout = 150 * time.Millisecond

	inicio := time.Now()
	body, err := c.OpenStream(context.Background(), "cam_x", config.AudioNone, time.Hour)
	if err == nil {
		body.Close()
		t.Fatal("OpenStream deveria desistir de um go2rtc que não responde")
	}
	if se := time.Since(inicio); se > 5*time.Second {
		t.Errorf("demorou %s para desistir", se)
	}
}
