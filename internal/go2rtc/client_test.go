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

// realResponse is a faithful excerpt of /api/streams from a real go2rtc.
//
// The format matters: `medias` is a list of STRINGS in SDP format, not of
// objects. Modeling it as a struct made the whole decode fail, and the test
// exists so that wrong assumption doesn't come back.
const realResponse = `{
  "cam_gama": {
    "producers": [{
      "id": 47,
      "format_name": "rtsp",
      "url": "rtsp://admin:senha@192.168.0.61:554/onvif1",
      "medias": ["video, recvonly, H265", "audio, recvonly, PCMA/16000"]
    }],
    "consumers": []
  },
  "cam_iota": {
    "producers": [{
      "url": "rtsp://admin:senha@192.168.0.60:554/onvif2",
      "medias": ["video, recvonly, H265"]
    }]
  },
  "cam_transcodificada": {
    "producers": [{
      "url": "ffmpeg:cam_gama#audio=aac",
      "medias": ["video, recvonly, H265", "audio, recvonly, MPEG4-GENERIC/16000/1"]
    }]
  },
  "cam_ociosa": {"producers": null, "consumers": null}
}`

func TestStreamsDecodesRealResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/streams" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(realResponse))
	}))
	defer srv.Close()

	c := New(config.Go2RTC{URL: srv.URL})
	streams, err := c.Streams(context.Background())
	if err != nil {
		t.Fatalf("Streams: %v", err)
	}
	if len(streams) != 4 {
		t.Fatalf("want 4 streams, got %d", len(streams))
	}

	camGamma := streams["cam_gama"].Producers[0]
	if !camGamma.HasAudio() {
		t.Error("cam_gama should have audio")
	}
	if got := camGamma.AudioCodecs(); len(got) != 1 || got[0] != "PCMA/16000" {
		t.Errorf("AudioCodecs = %v, want [PCMA/16000]", got)
	}
	if camGamma.Transcoding() {
		t.Error("an rtsp source is not transcoding")
	}

	camIota := streams["cam_iota"].Producers[0]
	if camIota.HasAudio() {
		t.Error("cam_iota has no audio track")
	}
	if len(camIota.AudioCodecs()) != 0 {
		t.Error("AudioCodecs should be empty without an audio track")
	}

	if !streams["cam_transcodificada"].Producers[0].Transcoding() {
		t.Error("an ffmpeg: source should be flagged as transcoding")
	}

	// A camera without a connected producer must not break the listing: it's
	// the normal state of a stream nobody has opened yet.
	if len(streams["cam_ociosa"].Producers) != 0 {
		t.Error("an idle stream should have zero producers")
	}
}

// With the stream in use, go2rtc replaces the ffmpeg: or exec: source with
// the running command, in `source`, and leaves `url` null. The producers below
// are excerpts from go2rtc 1.9.14's /api/streams, idle and in use.
func TestTranscodingWithStreamInUse(t *testing.T) {
	tests := []struct {
		name, json string
		want       bool
	}{
		{"ffmpeg: ocioso", `{"url": "ffmpeg:virtual?video=testsrc&size=320x180&rate=10#video=h264"}`, true},
		{"ffmpeg: in use", `{"url": null, "source": "exec:ffmpeg -hide_banner -v error -re -f lavfi -i testsrc -c:v libx264 -f rtsp rtsp://127.0.0.1:8554/a817"}`, true},
		{"exec:ffmpeg ocioso", `{"url": "exec:ffmpeg -hide_banner -re -f lavfi -i testsrc -c:v libx264 -f rtsp {output}"}`, true},
		{"exec:ffmpeg in use", `{"url": null, "source": "exec:ffmpeg -hide_banner -re -f lavfi -i testsrc -c:v libx264 -f rtsp rtsp://127.0.0.1:8554/7ef1"}`, true},
		{"exec: with the ffmpeg path", `{"url": "exec:/usr/bin/ffmpeg -i rtsp://cam/1 -c:v libx264 -f rtsp {output}"}`, true},
		{"exec: de outro programa", `{"url": "exec:gst-launch-1.0 videotestsrc ! x264enc ! rtspclientsink location={output}"}`, false},
		{"rtsp in use", `{"url": "rtsp://admin:senha@192.168.0.61:554/onvif1", "format_name": "rtsp"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p Producer
			if err := json.Unmarshal([]byte(tt.json), &p); err != nil {
				t.Fatal(err)
			}
			if got := p.Transcoding(); got != tt.want {
				t.Errorf("Transcoding() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The capture URL is the only place where the audio mode becomes behavior,
// and that's what makes the per-camera choice possible without any other
// change.
func TestStreamURLByAudioMode(t *testing.T) {
	c := New(config.Go2RTC{URL: "http://go2rtc:1984/"})

	tests := []struct {
		mode      string
		wanted    string
		notWanted string
	}{
		{config.AudioNone, "video=h264%2Ch265", "audio"},
		{config.AudioFLAC, "mp4=flac", "video="},
		{config.AudioAAC, "audio=aac", "mp4="},
	}
	for _, tt := range tests {
		got := c.StreamURL("cam_x", tt.mode)
		if !strings.Contains(got, tt.wanted) {
			t.Errorf("mode %s: URL %q doesn't contain %q", tt.mode, got, tt.wanted)
		}
		if tt.notWanted != "" && strings.Contains(got, tt.notWanted) {
			t.Errorf("mode %s: URL %q shouldn't contain %q", tt.mode, got, tt.notWanted)
		}
		if !strings.Contains(got, "src=cam_x") {
			t.Errorf("mode %s: URL %q without src", tt.mode, got)
		}
	}

	// The base URL's trailing slash must not become a double slash in the path.
	if strings.Contains(c.StreamURL("x", config.AudioNone), "//api") {
		t.Error("double slash in the capture URL path")
	}
}

// Makes sure Stream stays decodable even if go2rtc adds fields: dwnvr must
// not break with each new version of it.
func TestStreamIgnoresUnknownFields(t *testing.T) {
	var s Stream
	err := json.Unmarshal([]byte(`{"producers":[{"url":"x","medias":["video, recvonly, H264"],
		"campo_novo":123}],"outro_campo":"z"}`), &s)
	if err != nil {
		t.Fatalf("unknown fields broke the decode: %v", err)
	}
	if len(s.Producers) != 1 || s.Producers[0].URL != "x" {
		t.Errorf("producer not decoded: %+v", s.Producers)
	}
}

// silentStream returns a server that delivers `prefix` and then goes silent
// without closing the connection - exactly what go2rtc does when the RTSP
// producer dies, and what cost 3h38 of recording on 08/09/2026.
func silentStream(t *testing.T, prefix string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(prefix))
		w.(http.Flusher).Flush()
		// No closing, no writing: only the guard's cancellation gets out of here.
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOpenStreamDropsStreamThatGoesSilent(t *testing.T) {
	srv := silentStream(t, "primeiros bytes")

	c := New(config.Go2RTC{URL: srv.URL})
	body, err := c.OpenStream(context.Background(), "cam_x", config.AudioNone, 150*time.Millisecond)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer body.Close()

	start := time.Now()
	n, err := io.Copy(io.Discard, body)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("the read should fail when go2rtc goes silent")
	}
	// The message has to say what happened: without it the log shows
	// "context canceled" and nobody understands why the camera reconnected.
	if !strings.Contains(err.Error(), "sent nothing") {
		t.Errorf("error %q doesn't explain the stall", err)
	}
	if n != int64(len("primeiros bytes")) {
		t.Errorf("read %d bytes before giving up, want %d", n, len("primeiros bytes"))
	}
	if elapsed > 5*time.Second {
		t.Errorf("took %s to detect the stall", elapsed)
	}
}

// The guard must not drop a stream that is delivering: a reconnection every
// 15s would open gaps in the recording instead of closing them.
func TestOpenStreamKeepsActiveStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for range 20 {
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

	// 20 writes 15ms apart add up to over 300ms, well beyond the 100ms
	// threshold: only an INACTIVITY threshold lets that through.
	n, err := io.Copy(io.Discard, body)
	if err != nil {
		t.Fatalf("active stream was dropped: %v", err)
	}
	if n != 20 {
		t.Errorf("read %d bytes, want 20", n)
	}
}

// Closing early must not leak the timer or the context.
func TestOpenStreamCloseBeforeStall(t *testing.T) {
	srv := silentStream(t, "abc")

	c := New(config.Go2RTC{URL: srv.URL})
	body, err := c.OpenStream(context.Background(), "cam_x", config.AudioNone, time.Hour)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	if err := body.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// A stuck go2rtc still accepts the TCP connection and never answers.
// Without ResponseHeaderTimeout that blocks Do() forever, before the
// stallGuard even exists - the same silent loss, at a point the guard doesn't
// cover.
func TestOpenStreamGivesUpOnNoResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // accepts and goes silent, never sending a header
	}))
	defer srv.Close()

	c := New(config.Go2RTC{URL: srv.URL})
	c.HTTP.Transport.(*http.Transport).ResponseHeaderTimeout = 150 * time.Millisecond

	start := time.Now()
	body, err := c.OpenStream(context.Background(), "cam_x", config.AudioNone, time.Hour)
	if err == nil {
		body.Close()
		t.Fatal("OpenStream should give up on a go2rtc that doesn't answer")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %s to give up", elapsed)
	}
}

// The /api of a go2rtc 1.9 carries more than the version; only that must be read.
func TestVersionReadsOnlyTheVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"config_path":"/config/go2rtc.yaml","host":"192.168.0.10:1984","pid":1,"rtsp":{"listen":":8554"},"version":"1.9.9"}`)
	}))
	defer srv.Close()

	v, err := New(config.Go2RTC{URL: srv.URL}).Version(context.Background())
	if err != nil || v != "1.9.9" {
		t.Errorf("version %q, error %v", v, err)
	}
}
