package detect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
)

// sidecar is the Detector that sends the clip to the `dwnvr-detect` container
// over HTTP and returns what it found. The container is
// `dwnvr-detect/servidor.py`.
//
// It has no timeout of its own: the caller passes the deadline in ctx,
// because it's the queue that knows how long an inference is still worth
// waiting for.
type sidecar struct {
	url    string
	client *http.Client
}

// NewSidecar returns the Detector for the container serving at `url` (e.g.
// http://dwnvr-detect:8480).
func NewSidecar(url string) Detector {
	return &sidecar{url: strings.TrimRight(url, "/"), client: &http.Client{}}
}

func (s *sidecar) Name() string { return "sidecar" }

// ErrClipRejected is the answer of an object detector that is up and rejected
// the clip - video that doesn't decode, for example. It's not an outage: the
// queue doesn't pause, and the funnel counts it separately.
//
// It really happens: some cameras send H.264 errors in part of the segments,
// and a clip that catches one of those stretches doesn't decode.
var ErrClipRejected = errors.New("object detector rejected the clip")

// sidecarResponse is the JSON of POST /detect: the detections, or the reason
// it didn't infer.
type sidecarResponse struct {
	Detections []Detection `json:"achados"`
	Err        string      `json:"erro"`

	// Frame is the JPEG of the inferred frame, in base64, and only comes when
	// there was a detection. Base64 costs a third more bytes on a connection
	// that is local - the sidecar runs on the same machine -, and in exchange
	// the answer stays a single JSON, readable with `curl` when debugging.
	Frame []byte `json:"quadro,omitempty"`
}

// maxResponse is a wide margin: an inference with 300 boxes above the floor,
// which is the most RF-DETR returns, is about 30 KB - and the base64 frame, a
// few dozen KB.
const maxResponse = 4 << 20

func (s *sidecar) Infer(ctx context.Context, p Clip) (Output, error) {
	url := s.url + "/detect?piso=" + strconv.FormatFloat(MinScore, 'f', -1, 64) +
		"&larguraDoQuadro=" + strconv.Itoa(FrameWidth) + "&qualidade=" + strconv.Itoa(FrameQuality)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(p.Fmp4))
	if err != nil {
		return Output{}, err
	}
	req.Header.Set("Content-Type", "video/mp4")
	resp, err := s.client.Do(req)
	if err != nil {
		return Output{}, err
	}
	defer resp.Body.Close()

	var r sidecarResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponse)).Decode(&r); err != nil {
		return Output{}, fmt.Errorf("unreadable sidecar response (HTTP %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return Output{}, fmt.Errorf("%w: HTTP %d: %s", ErrClipRejected, resp.StatusCode, r.Err)
	}
	if resp.StatusCode != http.StatusOK {
		return Output{}, fmt.Errorf("sidecar answered HTTP %d: %s", resp.StatusCode, r.Err)
	}
	if r.Detections == nil {
		r.Detections = []Detection{} // an inference with nothing is a result, not a missing one
	}
	return Output{Detections: r.Detections, Frame: r.Frame}, nil
}

// Health is what the sidecar's GET /health returns: the loaded model, the
// size it infers the frame at, and how many threads it uses.
type Health struct {
	Model     string `json:"modelo"`
	InputSize string `json:"entrada"`
	Threads   int    `json:"threads"`
}

// healthTimeout is short because whoever waits is an open screen. The sidecar
// answers /health right away even in the middle of an inference; taking longer
// than that is already the diagnosis.
const healthTimeout = 3 * time.Second

// SidecarHealth asks the sidecar at `url` whether it's up and with which
// model. It serves the Diagnostics screen; the queue doesn't use it, because
// it finds out about an outage in the inference itself. The error comes
// without the `Get "<url>":` that http.Client puts in front, because the
// screen already shows the address next to it.
func SidecarHealth(ctx context.Context, url string) (Health, error) {
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(url, "/")+"/health", nil)
	if err != nil {
		return Health{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Health{}, fmt.Errorf("não respondeu em %d s", int(healthTimeout.Seconds()))
		}
		if ue, ok := errors.AsType[*neturl.Error](err); ok {
			return Health{}, ue.Err
		}
		return Health{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Health{}, fmt.Errorf("sidecar answered HTTP %d on /health", resp.StatusCode)
	}
	var s Health
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&s); err != nil {
		return Health{}, fmt.Errorf("unreadable /health response: %w", err)
	}
	return s, nil
}
