package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
)

// probeTTL is how long a probe is valid. Short because the answer can change
// without notice - it is enough for the owner to edit go2rtc.yaml -, and long
// enough to open and close the form a few times without reconnecting to the
// camera on every click.
const probeTTL = 5 * time.Minute

// probeEntry is a stored probe. The producer URL is part of the comparison
// because it is what identifies the SOURCE: changing a stream's RTSP while
// keeping the name would invalidate the result without changing the cache key.
type probeEntry struct {
	probe     go2rtc.AudioProbe
	sourceURL string
	at        time.Time
}

// probeCache stores the probe results and serializes concurrent ones.
//
// There is a single mutex, and not one per stream, on purpose: probing means
// opening a connection to a camera, and two screens open at the same time must
// not turn into several simultaneous connections on a device with 1.5 GB of RAM.
// Each probe lasts a few seconds.
type probeCache struct {
	mu      sync.Mutex
	entries map[string]probeEntry
}

func (c *probeCache) get(name, sourceURL string) (go2rtc.AudioProbe, bool) {
	e, ok := c.entries[name]
	if !ok || e.sourceURL != sourceURL || time.Since(e.at) > probeTTL {
		return go2rtc.AudioProbe{}, false
	}
	return e.probe, true
}

func (c *probeCache) put(name, sourceURL string, p go2rtc.AudioProbe) {
	if c.entries == nil {
		c.entries = map[string]probeEntry{}
	}
	c.entries[name] = probeEntry{probe: p, sourceURL: sourceURL, at: time.Now()}
}

// handleProbeStream answers whether a stream delivers audio, probing it if
// needed.
//
// The registration screen needs this answer BEFORE the camera exists, and that
// is exactly when go2rtc cannot give it: it only fills `medias` while someone
// consumes the stream. See go2rtc.Client.ProbeAudio for the mechanism.
//
// A stream that already has a live producer - every registered camera that is
// recording - is not probed: its `medias` is already filled, and opening a
// second connection just to ask what is already known would cost one more
// session with the camera.
func (s *Server) handleProbeStream(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("src")
	if name == "" {
		writeError(w, http.StatusBadRequest, "faltou o parâmetro src")
		return
	}

	streams, err := s.client.Streams(r.Context())
	if err != nil {
		s.log.Error("listing go2rtc streams", "err", err)
		writeError(w, http.StatusBadGateway, "o go2rtc não respondeu")
		return
	}
	st, ok := streams[name]
	if !ok {
		writeError(w, http.StatusNotFound, "o go2rtc não tem nenhum stream com esse nome")
		return
	}

	// probed says whether the answer cost a new connection to the camera. It is
	// what lets the screen tell "I just checked" from "this was already known".
	resp := map[string]any{"name": name, "hasAudio": false, "probed": false}
	setAudio := func(has bool, codecs []string) {
		resp["hasAudio"] = has
		if len(codecs) > 0 {
			resp["audioCodecs"] = codecs
		}
	}

	// What distinguishes a live stream from an idle one is `medias`, not the
	// presence of the producer: go2rtc lists the CONFIGURED producer all along,
	// with only the url, and only fills the tracks when it actually connects to
	// the camera.
	var sourceURL string
	known := false
	for _, p := range st.Producers {
		sourceURL = p.URL
		if len(p.Medias) == 0 {
			continue
		}
		known = true
		if p.HasAudio() {
			setAudio(true, p.AudioCodecs())
		}
	}
	if known {
		writeJSON(w, resp)
		return
	}

	s.probes.mu.Lock()
	defer s.probes.mu.Unlock()

	if p, ok := s.probes.get(name, sourceURL); ok {
		setAudio(p.HasAudio, p.Codecs)
		writeJSON(w, resp)
		return
	}

	probe, err := s.client.ProbeAudio(r.Context(), name)
	if err != nil {
		// It is not a server error: the camera may simply be down, and the
		// screen knows what to do with that - leave the audio options clickable,
		// instead of lying that the camera has no microphone. Hence the 200 with
		// the reason attached, and nothing stored in the cache.
		s.log.Warn("probing stream audio", "stream", name, "err", err)
		resp["erro"] = err.Error()
		writeJSON(w, resp)
		return
	}

	s.probes.put(name, sourceURL, probe)
	resp["probed"] = true
	setAudio(probe.HasAudio, probe.Codecs)
	writeJSON(w, resp)
}
