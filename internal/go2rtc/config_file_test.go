package go2rtc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

const testFile = `
streams:
  cam_rtsp: rtsp://admin:senha@192.168.0.61:554/onvif1
  cam_changed: rtsp://admin:senha@192.168.0.99:554/onvif1
  cam_ffmpeg_active: ffmpeg:virtual?video=testsrc&size=640x360#video=h264
  cam_two_sources:
    - rtsp://192.168.0.62/main
    - ffmpeg:cam_two_sources#audio=aac
  cam_with_param: rtsp://192.168.0.63/main#backchannel=0
  cam_new: rtsp://192.168.0.64/main
  cam_empty:
rtsp:
  listen: ":8554"
`

func TestFindDrift(t *testing.T) {
	running := map[string]Stream{
		"cam_rtsp":    {Producers: []Producer{{URL: "rtsp://admin:senha@192.168.0.61:554/onvif1"}}},
		"cam_changed": {Producers: []Producer{{URL: "rtsp://admin:senha@192.168.0.60:554/onvif1"}}},
		// In use, the ffmpeg producer carries the expanded command and an empty
		// url: there's no way to compare, and the camera must not show up as changed.
		"cam_ffmpeg_active": {Producers: []Producer{{}}},
		"cam_two_sources": {Producers: []Producer{
			{URL: "rtsp://192.168.0.62/main"},
			{URL: "ffmpeg:cam_two_sources#audio=aac"},
		}},
		"cam_with_param": {Producers: []Producer{{URL: "rtsp://192.168.0.63/main"}}},
		"cam_empty":      {},
		"cam_removed":    {Producers: []Producer{{URL: "rtsp://192.168.0.65/main"}}},
	}

	d, err := FindDrift([]byte(testFile), running)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"cam_new"}; !slices.Equal(d.Added, want) {
		t.Errorf("Added = %v, want %v", d.Added, want)
	}
	if want := []string{"cam_changed"}; !slices.Equal(d.Changed, want) {
		t.Errorf("Changed = %v, want %v", d.Changed, want)
	}
	if want := []string{"cam_removed"}; !slices.Equal(d.NotInFile, want) {
		t.Errorf("NotInFile = %v, want %v", d.NotInFile, want)
	}
}

func TestFindDriftExtraSource(t *testing.T) {
	file := "streams:\n  cam:\n    - rtsp://a/main\n    - rtsp://a/sub\n"
	running := map[string]Stream{"cam": {Producers: []Producer{{URL: "rtsp://a/main"}}}}

	d, err := FindDrift([]byte(file), running)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(d.Changed, []string{"cam"}) {
		t.Errorf("Changed = %v, want [cam]", d.Changed)
	}
}

func TestFindDriftSame(t *testing.T) {
	file := "streams:\n  cam: rtsp://a/main\n"
	running := map[string]Stream{"cam": {Producers: []Producer{{URL: "rtsp://a/main"}}}}

	d, err := FindDrift([]byte(file), running)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Empty() {
		t.Errorf("drift where there is none: %+v", d)
	}
}

func TestFindDriftUnreadableFile(t *testing.T) {
	if _, err := FindDrift([]byte("streams: [nao: fecha"), nil); err == nil {
		t.Error("broken yaml should give an error")
	}
}

func TestRestartUsesPOST(t *testing.T) {
	var method, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if err := c.Restart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/api/restart" {
		t.Errorf("request = %s %s, want POST /api/restart", method, path)
	}
}
