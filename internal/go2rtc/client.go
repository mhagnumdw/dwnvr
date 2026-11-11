// Package go2rtc talks to the go2rtc instance the user runs.
//
// dwnvr never configures go2rtc: it discovers the existing streams and
// consumes the fMP4 go2rtc already knows how to produce. All the talking with
// the cameras - RTSP, transport, credentials - remains go2rtc's job.
package go2rtc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/fmp4"
)

type Client struct {
	BaseURL  string
	Username string
	Password string
	HTTP     *http.Client

	// unreachableSince is the first of the consecutive transport failures:
	// go2rtc answered nothing, not even an HTTP error. Any answer clears it.
	// The recorders reopen the stream every few seconds when they drop, so the
	// instant comes out accurate without a probe of its own. unreachableErr is
	// the most recent failure.
	mu               sync.Mutex
	unreachableSince time.Time
	unreachableErr   string
}

// Unreachable says since when go2rtc hasn't answered, and why. Zero when the
// last exchange with it got an answer.
func (c *Client) Unreachable() (since time.Time, errText string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.unreachableSince, c.unreachableErr
}

// do makes the request and records whether go2rtc answered. Cancellation by
// the caller doesn't count: that's dwnvr giving up, not go2rtc going away.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.HTTP.Do(req)
	if err != nil && errors.Is(req.Context().Err(), context.Canceled) {
		return resp, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case err == nil:
		c.unreachableSince, c.unreachableErr = time.Time{}, ""
	case c.unreachableSince.IsZero():
		c.unreachableSince, c.unreachableErr = time.Now(), err.Error()
	default:
		c.unreachableErr = err.Error()
	}
	return resp, err
}

func New(cfg config.Go2RTC) *Client {
	return &Client{
		BaseURL:  strings.TrimRight(cfg.URL, "/"),
		Username: cfg.Username,
		Password: cfg.Password,
		// No global timeout: the stream.mp4 response is endless by nature, and
		// an http.Client Timeout applies to the whole body. The stallGuard
		// looks after the body; here only what comes BEFORE it is protected.
		//
		// ResponseHeaderTimeout covers a real hole: a stuck go2rtc still
		// accepts the TCP connection (the kernel accepts it on its behalf) and
		// never answers. Without this, Do() blocks forever - the same silent
		// failure the guard prevents later, only at a point where it doesn't
		// exist yet.
		HTTP: &http.Client{
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				ResponseHeaderTimeout: 2 * config.DefaultStallSeconds * time.Second,
			},
		},
	}
}

// StreamURL builds the capture URL.
//
// The audio mode becomes a codec filter, which is why it can be picked per
// camera without any other change in the system:
//
//	none  drops the audio at the source - it doesn't travel and costs nothing
//	flac  go2rtc converts pcm_alaw→FLAC in pure Go, without spawning ffmpeg
//	aac   requires go2rtc.yaml to have an ffmpeg:cam#audio=aac source
func (c *Client) StreamURL(cam, audio string) string {
	q := url.Values{}
	q.Set("src", cam)
	switch audio {
	case config.AudioFLAC:
		q.Set("mp4", "flac")
	case config.AudioAAC:
		q.Set("video", "h264,h265")
		q.Set("audio", "aac")
	default:
		q.Set("video", "h264,h265")
	}
	return c.BaseURL + "/api/stream.mp4?" + q.Encode()
}

// OpenStream opens a camera's continuous fMP4. The caller closes the body.
//
// The returned body closes itself if go2rtc goes `idle` without delivering a
// single byte - see stallGuard for why.
func (c *Client) OpenStream(ctx context.Context, cam, audio string, idle time.Duration) (io.ReadCloser, error) {
	if idle <= 0 {
		idle = config.DefaultStallSeconds * time.Second
	}

	// Its own context is what lets the guard kill THIS request without
	// affecting the caller.
	ctx, cancel := context.WithCancel(ctx)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.StreamURL(cam, audio), nil)
	if err != nil {
		cancel()
		return nil, err
	}
	c.auth(req)

	resp, err := c.do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("go2rtc returned HTTP %d for %q", resp.StatusCode, cam)
	}

	g := &stallGuard{body: resp.Body, idle: idle, cancel: cancel}
	// The first read gets twice the deadline: opening the stream makes go2rtc
	// set up the RTSP session with the camera, which is legitimately slower
	// than delivering the next fragment of a stream already running.
	g.timer = time.AfterFunc(2*idle, g.trip)
	return g, nil
}

// stallGuard drops the connection when go2rtc stops sending bytes without
// closing it.
//
// It exists because of a real failure: go2rtc's RTSP producers run over UDP,
// and when the camera's flow stops there's no socket error at all - go2rtc
// just stops writing, with the HTTP response open. On this side Read blocks
// forever, with no error, no EOF and no log: on 08/09/2026 five cameras went
// 3h38 without recording while reporting `connected: true`.
//
// Closing the connection is also what RECOVERS the camera. Since dwnvr is the
// stream's only consumer, leaving makes go2rtc drop the dead producer, and
// run()'s reconnection opens a new RTSP session.
type stallGuard struct {
	body   io.ReadCloser
	idle   time.Duration
	timer  *time.Timer
	cancel context.CancelFunc

	stalled atomic.Bool
}

func (g *stallGuard) trip() {
	// Order matters: whoever sees the read error must already see the reason,
	// otherwise the cancellation becomes an unexplained "context canceled".
	g.stalled.Store(true)
	g.cancel()
}

func (g *stallGuard) Read(p []byte) (int, error) {
	n, err := g.body.Read(p)
	if n > 0 {
		g.timer.Reset(g.idle)
	}
	if err != nil && g.stalled.Load() {
		return n, fmt.Errorf("go2rtc sent nothing for %s", g.idle)
	}
	return n, err
}

func (g *stallGuard) Close() error {
	g.timer.Stop()
	err := g.body.Close()
	g.cancel()
	return err
}

func (c *Client) auth(req *http.Request) {
	if c.Username != "" || c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
}

// Stream is what go2rtc reports about a stream in /api/streams.
type Stream struct {
	Producers []Producer `json:"producers"`
	Consumers []any      `json:"consumers"`
}

type Producer struct {
	// URL is the source as written in go2rtc.yaml, but only while the stream
	// is idle. With someone consuming it, an ffmpeg: or exec: source comes
	// with an empty URL and the running command in Source. RTSP in use keeps
	// the URL.
	URL    string `json:"url"`
	Source string `json:"source"`

	// Medias comes as free text, one line per track, in the format
	// "<kind>, <direction>, <codec>" - for example "audio, recvonly,
	// PCMA/16000". It's not structured JSON: it's the SDP representation
	// go2rtc exposes.
	Medias []string `json:"medias"`
}

// Transcoding says the source is an ffmpeg, that is, that transcoding is
// happening - worth showing in diagnostics, since the stated goal is not to
// transcode video.
//
// It counts both ffmpeg: and an exec: that runs ffmpeg, because go2rtc turns
// the former into the latter: that's what shows up in Source with the stream
// in use, and the CPU cost is the same when the user writes it by hand.
func (p Producer) Transcoding() bool {
	for _, s := range []string{p.URL, p.Source} {
		if strings.HasPrefix(s, "ffmpeg:") {
			return true
		}
		if cmd, ok := strings.CutPrefix(s, "exec:"); ok {
			prog, _, _ := strings.Cut(cmd, " ")
			if path.Base(prog) == "ffmpeg" {
				return true
			}
		}
	}
	return false
}

// HasAudio says whether the producer delivers any audio track. It's what lets
// the cameras screen offer audio only on the cameras that actually have it.
func (p Producer) HasAudio() bool {
	for _, m := range p.Medias {
		if strings.HasPrefix(m, "audio") {
			return true
		}
	}
	return false
}

// AudioCodecs returns the advertised audio codecs, for example "PCMA/16000".
func (p Producer) AudioCodecs() []string {
	var out []string
	for _, m := range p.Medias {
		if !strings.HasPrefix(m, "audio") {
			continue
		}
		// "audio, recvonly, PCMA/16000" -> "PCMA/16000"
		if _, after, ok := strings.CutLast(m, ", "); ok {
			out = append(out, strings.TrimSpace(after))
		}
	}
	return out
}

// Streams lists the streams configured in go2rtc. It's the source of the
// cameras screen: the user picks from what already exists, instead of typing
// names.
func (c *Client) Streams(ctx context.Context) (map[string]Stream, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/streams", nil)
	if err != nil {
		return nil, err
	}
	c.auth(req)

	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("go2rtc returned HTTP %d on /api/streams", resp.StatusCode)
	}

	var out map[string]Stream
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// Version asks go2rtc for its version. It serves the Diagnostics screen as
// proof of life: an answer here means the URL, the credential and the network
// to it are right. The deadline is short because whoever waits is an open
// screen, and a go2rtc that takes longer than that for a trivial answer is
// already the diagnosis.
func (c *Client) Version(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api", nil)
	if err != nil {
		return "", err
	}
	c.auth(req)

	resp, err := c.do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("go2rtc returned HTTP %d on /api", resp.StatusCode)
	}

	// /api also carries go2rtc's paths and addresses; only the version
	// matters, and only it is read - the rest must not reach the browser.
	var out struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Version, nil
}

// Probe deadlines. They're short on purpose: whoever waits is an open screen,
// and the stallGuard gives twice probeIdle for the first byte, which is where
// the legitimate slowness lives (go2rtc still has to set up the RTSP session
// with the camera).
const (
	probeIdle    = 4 * time.Second
	probeTimeout = 12 * time.Second
)

// AudioProbe is what a probe found out about a stream's audio.
type AudioProbe struct {
	HasAudio bool
	Codecs   []string
}

// ProbeAudio finds out whether a stream delivers audio, opening it for a few
// seconds.
//
// It exists because go2rtc only fills in `medias` while someone consumes the
// stream: on an idle stream it doesn't even open the connection to the
// camera, and the cameras screen had no way to know whether it could offer
// FLAC or AAC. For a few seconds, the probe is the missing consumer.
//
// It asks for the stream in FLAC mode on purpose: `mp4=flac` is the only
// filter that brings the audio track, and reading the moov is proof that the
// connection actually delivered media - the same init segment the recorder
// would read. With the connection still open, the producer's `medias` answers
// which codec the camera advertises.
func (c *Client) ProbeAudio(ctx context.Context, cam string) (AudioProbe, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	body, err := c.OpenStream(ctx, cam, config.AudioFLAC, probeIdle)
	if err != nil {
		return AudioProbe{}, err
	}
	defer body.Close()

	var probe AudioProbe
	rd := fmp4.NewReader(body)
	for {
		typ, box, err := rd.NextBox()
		if err != nil {
			return AudioProbe{}, err
		}
		if typ != "moov" {
			continue
		}
		mv, err := fmp4.ParseMoov(box)
		if err != nil {
			return AudioProbe{}, fmt.Errorf("unreadable moov: %w", err)
		}
		probe.HasAudio = mv.HasAudio()
		break
	}

	// The moov would only show "fLaC", which is the codec AFTER conversion.
	// The name that matters to the user - PCMA/16000 and the like - is what the
	// camera advertises, and that only exists in the producer, which is alive
	// now because of this connection.
	if streams, err := c.Streams(ctx); err == nil {
		for _, p := range streams[cam].Producers {
			if p.HasAudio() {
				probe.HasAudio = true
				probe.Codecs = append(probe.Codecs, p.AudioCodecs()...)
			}
		}
	}
	return probe, nil
}
