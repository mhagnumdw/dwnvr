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

// moovWithAudio is the minimum ParseMoov accepts: an ftyp, a video track and an
// audio track. It is what the probe reads to know the connection delivered
// media.
func moovWithAudio() []byte {
	box := func(typ string, parts ...[]byte) []byte {
		body := []byte{}
		for _, p := range parts {
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

// serverWithGo2RTC builds a Server pointing to a fake go2rtc, and returns the
// counter of stream.mp4 opens - which is the metric that matters: each open is
// a connection to the camera.
func serverWithGo2RTC(t *testing.T, streams string) (*Server, *int) {
	t.Helper()
	opens := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/streams":
			_, _ = w.Write([]byte(streams))
		case "/api/stream.mp4":
			opens++
			_, _ = w.Write(moovWithAudio())
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
		t.Fatalf("unreadable response: %v", err)
	}
	return out
}

// A camera that is already recording has a consumer, and with a consumer go2rtc
// already fills `medias`. Probing there would open a second session with the
// camera to find out what the answer already said.
func TestProbeDoesNotProbeStreamWithLiveProducer(t *testing.T) {
	s, opens := serverWithGo2RTC(t, `{"cam_x": {"producers": [
		{"url": "rtsp://x", "medias": ["video, recvonly, H265", "audio, recvonly, PCMA/16000"]}]}}`)

	r := probe(t, s, "cam_x")
	if r["hasAudio"] != true {
		t.Errorf("hasAudio = %v, expected true", r["hasAudio"])
	}
	if r["probed"] != false {
		t.Errorf("probed = %v, expected false", r["probed"])
	}
	if *opens != 0 {
		t.Errorf("opened the stream %d times, expected none", *opens)
	}
}

// idle is the faithful cut of a stream that nobody is consuming. go2rtc LISTS
// the configured producer all along, with the url - it just does not fill the
// tracks until it connects to the camera. Having modeled this as `"producers":
// null` made the probe never fire on any stream, and the test exists so that
// this wrong assumption does not come back.
const idle = `{"cam_x": {"producers": [
	{"url": "exec:ffmpeg -i avsynctest -f rtsp {output}"}], "consumers": []}}`

func TestProbeProbesIdleStreamOnlyOnce(t *testing.T) {
	s, opens := serverWithGo2RTC(t, idle)

	r := probe(t, s, "cam_x")
	if r["hasAudio"] != true {
		t.Errorf("hasAudio = %v, expected true", r["hasAudio"])
	}
	if r["probed"] != true {
		t.Errorf("probed = %v, expected true", r["probed"])
	}

	// Opening and closing the form again must not cost another connection.
	r = probe(t, s, "cam_x")
	if r["hasAudio"] != true {
		t.Errorf("cache returned hasAudio = %v", r["hasAudio"])
	}
	if r["probed"] != false {
		t.Errorf("probed = %v on the second call, expected false", r["probed"])
	}
	if *opens != 1 {
		t.Errorf("opened the stream %d times, expected 1", *opens)
	}
}

func TestProbeRejectsNonexistentStream(t *testing.T) {
	s, _ := serverWithGo2RTC(t, idle)

	rec := httptest.NewRecorder()
	s.handleProbeStream(rec, httptest.NewRequest(http.MethodGet, "/api/streams/probe?src=cam_y", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("HTTP %d, expected 404", rec.Code)
	}
}
