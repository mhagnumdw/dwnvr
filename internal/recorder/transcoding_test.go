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

// TestConnectionRereadsTranscoding: each connection asks go2rtc again whether
// the source goes through an ffmpeg, and the previous connection's value
// doesn't survive. It's what makes the Diagnostics notice follow an edited
// go2rtc.yaml: on restart, go2rtc drops everyone, and each recorder reconnects.
func TestConnectionRereadsTranscoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/streams":
			_, _ = w.Write([]byte(`{
				"cam_ffmpeg": {"producers": [{"source": "exec:ffmpeg -i rtsp://x -c:v libx264"}]},
				"cam_rtsp": {"producers": [{"url": "rtsp://x"}]}}`))
		case "/api/stream.mp4":
			_, _ = w.Write(append(box("ftyp", []byte("isom")), moovWithAudio()...))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := go2rtc.New(config.Go2RTC{URL: srv.URL})

	for id, expected := range map[string]bool{"cam_ffmpeg": true, "cam_rtsp": false} {
		t.Run(id, func(t *testing.T) {
			cam := config.Camera{ID: id, Enabled: true, SegmentSeconds: 30, StallSeconds: 5,
				Audio: config.AudioAAC}
			r := newRecorder(cam, client, store.New(t.TempDir()).Camera(id), discardLogger())
			// The opposite of what's expected, as if it came from the previous
			// connection.
			r.transcoding = !expected

			ctx, cancel := context.WithCancel(context.Background())
			end := make(chan struct{})
			go func() { _ = r.session(ctx); close(end) }()
			defer func() { cancel(); <-end }()

			timeout := time.Now().Add(3 * time.Second)
			for r.Status().Transcoding != expected {
				if time.Now().After(timeout) {
					t.Fatalf("transcoding still %v, want %v", !expected, expected)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if got := r.Status().Audio; got != config.AudioAAC {
				t.Errorf("audio = %q, want the configured one, %q", got, config.AudioAAC)
			}
		})
	}
}
