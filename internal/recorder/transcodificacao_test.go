package recorder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// TestConexaoReleATranscodificacao: cada conexão pergunta de novo ao go2rtc se
// a fonte passa por um ffmpeg, e o valor da conexão anterior não sobrevive.
// É o que faz o aviso do Diagnóstico acompanhar um go2rtc.yaml editado: ao
// reiniciar, o go2rtc derruba todo mundo, e cada recorder reconecta.
func TestConexaoReleATranscodificacao(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/streams":
			_, _ = w.Write([]byte(`{
				"cam_ffmpeg": {"producers": [{"source": "exec:ffmpeg -i rtsp://x -c:v libx264"}]},
				"cam_rtsp": {"producers": [{"url": "rtsp://x"}]}}`))
		case "/api/stream.mp4":
			_, _ = w.Write(append(caixa("ftyp", []byte("isom")), moovComAudio()...))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := go2rtc.New(config.Go2RTC{URL: srv.URL})

	for id, esperado := range map[string]bool{"cam_ffmpeg": true, "cam_rtsp": false} {
		t.Run(id, func(t *testing.T) {
			cam := config.Camera{ID: id, Enabled: true, SegmentSeconds: 30, StallSeconds: 5,
				Audio: config.AudioAAC}
			r := newRecorder(cam, client, store.New(t.TempDir()).Camera(id), mudo())
			// O contrário do esperado, como se viesse da conexão anterior.
			r.transcoding = !esperado

			ctx, cancel := context.WithCancel(context.Background())
			fim := make(chan struct{})
			go func() { _ = r.session(ctx); close(fim) }()
			defer func() { cancel(); <-fim }()

			prazo := time.Now().Add(3 * time.Second)
			for r.Status().Transcoding != esperado {
				if time.Now().After(prazo) {
					t.Fatalf("transcoding continua %v, esperava %v", !esperado, esperado)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if got := r.Status().Audio; got != config.AudioAAC {
				t.Errorf("audio = %q, esperava o configurado, %q", got, config.AudioAAC)
			}
		})
	}
}
