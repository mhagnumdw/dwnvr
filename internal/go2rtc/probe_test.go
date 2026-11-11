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

// The boxes are built by hand because the fmp4 package keeps its helpers in
// its own tests, and what this proves is the probe's behavior, not MP4
// reading - which already has tests in internal/fmp4.
func mkBox(typ string, parts ...[]byte) []byte {
	body := []byte{}
	for _, p := range parts {
		body = append(body, p...)
	}
	b := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(b[0:4], uint32(8+len(body)))
	copy(b[4:8], typ)
	return append(b, body...)
}

func u32b(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

// track builds the minimum ParseMoov requires: id, timescale and handler.
func track(id uint32, handler string) []byte {
	tkhd := mkBox("tkhd", u32b(0), u32b(0), u32b(0), u32b(id))
	mdhd := mkBox("mdhd", u32b(0), u32b(0), u32b(0), u32b(90000), u32b(0))
	hdlr := mkBox("hdlr", u32b(0), u32b(0), []byte(handler))
	return mkBox("trak", tkhd, mkBox("mdia", mdhd, hdlr))
}

func initSegment(withAudio bool) []byte {
	tracks := [][]byte{mkBox("mvhd", u32b(0)), track(1, "vide")}
	if withAudio {
		tracks = append(tracks, track(2, "soun"))
	}
	return append(mkBox("ftyp", []byte("iso5")), mkBox("moov", tracks...)...)
}

// fakeGo2RTC answers the two endpoints the probe uses: stream.mp4, which
// delivers the init segment and then goes quiet, and /api/streams, which only
// starts listing `medias` AFTER someone opens the stream - which is exactly
// how the real go2rtc behaves and the reason the probe exists.
func fakeGo2RTC(t *testing.T, withAudio bool, medias string) (*httptest.Server, *int) {
	t.Helper()
	opens := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/stream.mp4":
			opens++
			_, _ = w.Write(initSegment(withAudio))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case "/api/streams":
			if opens == 0 || medias == "" {
				_, _ = w.Write([]byte(`{"cam_x": {"producers": null}}`))
				return
			}
			_, _ = w.Write([]byte(`{"cam_x": {"producers": [{"url": "rtsp://x", "medias": [` + medias + `]}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &opens
}

func TestProbeAudioFindsAudioOnIdleStream(t *testing.T) {
	srv, opens := fakeGo2RTC(t, true,
		`"video, recvonly, H265", "audio, recvonly, PCMA/16000"`)

	c := New(config.Go2RTC{URL: srv.URL})
	p, err := c.ProbeAudio(context.Background(), "cam_x")
	if err != nil {
		t.Fatalf("ProbeAudio: %v", err)
	}
	if !p.HasAudio {
		t.Error("the camera delivers audio and the probe said it doesn't")
	}
	// The codec is what the CAMERA advertises, not the converted fLaC: it's what
	// tells the user what they'll record.
	if len(p.Codecs) != 1 || p.Codecs[0] != "PCMA/16000" {
		t.Errorf("codecs = %v, want [PCMA/16000]", p.Codecs)
	}
	// A probe is a connection to the camera. Two would be wasteful on a device
	// recording nine cameras with 1.5 GB of RAM.
	if *opens != 1 {
		t.Errorf("opened the stream %d times, want 1", *opens)
	}
}

func TestProbeAudioOnCameraWithoutAudio(t *testing.T) {
	srv, _ := fakeGo2RTC(t, false, `"video, recvonly, H265"`)

	c := New(config.Go2RTC{URL: srv.URL})
	p, err := c.ProbeAudio(context.Background(), "cam_x")
	if err != nil {
		t.Fatalf("ProbeAudio: %v", err)
	}
	if p.HasAudio {
		t.Error("the camera has no audio and the probe said it does")
	}
	if len(p.Codecs) != 0 {
		t.Errorf("codecs = %v, want empty", p.Codecs)
	}
}

// Without /api/streams the probe still answers: the moov is proof enough
// that there's an audio track, only the codec name is missing.
func TestProbeAudioWithoutMediasFallsBackToMoov(t *testing.T) {
	srv, _ := fakeGo2RTC(t, true, "")

	c := New(config.Go2RTC{URL: srv.URL})
	p, err := c.ProbeAudio(context.Background(), "cam_x")
	if err != nil {
		t.Fatalf("ProbeAudio: %v", err)
	}
	if !p.HasAudio {
		t.Error("the moov had an audio track and the probe said it didn't")
	}
	if len(p.Codecs) != 0 {
		t.Errorf("codecs = %v, want empty without medias", p.Codecs)
	}
}

func TestProbeAudioAsksForFLACStream(t *testing.T) {
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
		t.Fatal("want an error with the server returning 404")
	}
	// mp4=flac is the only filter that brings the audio track along. Asking for
	// video=h264,h265, as the none mode does, would return a moov without audio
	// and the probe would lie on every camera.
	if !strings.Contains(query, "mp4=flac") {
		t.Errorf("the probe asked for %q, want mp4=flac", query)
	}
}
