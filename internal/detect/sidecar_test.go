package detect

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func fakeSidecar(t *testing.T, f http.HandlerFunc) Detector {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return Detectors["sidecar"](srv.URL + "/")
}

func TestSidecarSendsClipAndMinScoreAndReadsDetections(t *testing.T) {
	clip := []byte("init+fragments")
	d := fakeSidecar(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.Method != http.MethodPost || r.URL.Path != "/detect":
			t.Errorf("%s %s, expected POST /detect", r.Method, r.URL.Path)
		case r.URL.Query().Get("piso") != "0.2":
			t.Errorf("piso %q, expected MinScore", r.URL.Query().Get("piso"))
		case !bytes.Equal(body, clip):
			t.Errorf("body %q, expected the clip", body)
		}
		_, _ = io.WriteString(w, `{"achados":[{"classe":"person","score":0.87,"caixa":[0.1,0.2,0.3,0.4]}]}`)
	})
	out, err := d.Infer(t.Context(), Clip{Fmp4: clip})
	if err != nil {
		t.Fatal(err)
	}
	want := Detection{Class: "person", Score: 0.87, Box: [4]float64{0.1, 0.2, 0.3, 0.4}}
	if len(out.Detections) != 1 || out.Detections[0] != want {
		t.Errorf("detections %+v, expected [%+v]", out.Detections, want)
	}
}

// An inference with nothing is a result: it's what counts as a miss in the
// tracker. It must not come back nil, which reads as "didn't infer".
// That also holds for a response without the field, or with it null.
func TestSidecarEmptyInferenceIsAResult(t *testing.T) {
	for _, body := range []string{`{"achados":[]}`, `{"achados":null}`, `{}`} {
		d := fakeSidecar(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, body)
		})
		out, err := d.Infer(t.Context(), Clip{Fmp4: []byte("x")})
		if err != nil || out.Detections == nil || len(out.Detections) != 0 {
			t.Errorf("%s: detections %v, error %v: expected an empty slice and no error", body, out.Detections, err)
		}
	}
}

func TestSidecarErrorComesBackWithReason(t *testing.T) {
	d := fakeSidecar(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"erro":"pedaço não decodifica: nenhum quadro"}`)
	})
	_, err := d.Infer(t.Context(), Clip{Fmp4: []byte("x")})
	if err == nil || !strings.Contains(err.Error(), "nenhum quadro") || !strings.Contains(err.Error(), "422") {
		t.Errorf("error %v: expected the HTTP status and the sidecar's reason", err)
	}
	// 422 is a bad clip with the object detector up - it must not become
	// "down".
	if !errors.Is(err, ErrClipRejected) {
		t.Errorf("error %v: a 4xx should be ErrClipRejected", err)
	}
}

func TestSidecarServerErrorIsNotRejection(t *testing.T) {
	d := fakeSidecar(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"erro":"quebrou"}`)
	})
	_, err := d.Infer(t.Context(), Clip{Fmp4: []byte("x")})
	if err == nil || errors.Is(err, ErrClipRejected) {
		t.Errorf("error %v: 5xx is an object detector with a problem, not a rejected clip", err)
	}
}

func TestSidecarHonorsTheDeadline(t *testing.T) {
	// The fake server stays silent until the test ends. Its Cleanup runs after
	// this one (the order is reversed), so it's released before closing.
	release := make(chan struct{})
	d := fakeSidecar(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
	})
	t.Cleanup(func() { close(release) })
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := d.Infer(ctx, Clip{Fmp4: []byte("x")}); err == nil {
		t.Fatal("a silent sidecar should give an error")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("waited %v for a silent sidecar", d)
	}
}

func TestSidecarHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/health" {
			t.Errorf("%s %s, expected GET /health", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"modelo": "modelo.onnx", "entrada": "512x288", "threads": 2}`)
	}))
	defer srv.Close()
	s, err := SidecarHealth(t.Context(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if s != (Health{Model: "modelo.onnx", InputSize: "512x288", Threads: 2}) {
		t.Errorf("health %+v", s)
	}
}

// When down, the error comes without the `Get "<url>":` in front: the screen
// shows the address on a separate line.
func TestSidecarHealthDown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := SidecarHealth(t.Context(), url)
	if err == nil || strings.Contains(err.Error(), "/health") {
		t.Errorf("error %v, expected only the network failure", err)
	}
}
