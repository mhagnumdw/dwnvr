package go2rtc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// go2rtc only reads go2rtc.yaml when it starts. Someone editing the file with
// everything running sees no effect at all - neither a new camera on the
// cameras screen, nor the changed URL of a camera already recording - until
// restarting it. This file is what lets dwnvr notice that and offer the
// restart.

// Deadline for the two exchanges below: trivial answers, asked for by an open
// screen, like Version's.
const configFileTimeout = 3 * time.Second

// Drift is what go2rtc.yaml says and the running go2rtc doesn't do yet.
//
// Only the streams section is compared: a change in another section (port,
// WebRTC) doesn't show up here.
type Drift struct {
	// Added are in the file and weren't loaded.
	Added []string `json:"novas,omitempty"`
	// Changed exist on both sides, with a different source.
	Changed []string `json:"alteradas,omitempty"`
	// NotInFile are running and are no longer in the file.
	NotInFile []string `json:"foraDoArquivo,omitempty"`
}

func (d Drift) Empty() bool {
	return len(d.Added) == 0 && len(d.Changed) == 0 && len(d.NotInFile) == 0
}

// ConfigFile returns go2rtc.yaml as go2rtc reads it from disk now.
//
// The content has the RTSP URLs with user and password: it stays on the
// server, only for FindDrift, and never goes to the browser.
func (c *Client) ConfigFile(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, configFileTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/config", nil)
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
		return nil, fmt.Errorf("go2rtc returned HTTP %d on /api/config", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// Restart asks go2rtc to restart its own process, and with that read
// go2rtc.yaml again. The container stays the same; every stream drops for a
// few seconds, and the recorders reconnect on their own.
func (c *Client) Restart(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, configFileTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/restart", nil)
	if err != nil {
		return err
	}
	c.auth(req)

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("go2rtc returned HTTP %d on /api/restart", resp.StatusCode)
	}
	return nil
}

// FindDrift compares the streams section of go2rtc.yaml with what go2rtc
// serves.
//
// Added and not in the file are always detectable: the name is enough.
// Changed, only when go2rtc shows the source, and it doesn't always: while an
// ffmpeg: or exec: source is being consumed, the producer carries the
// already-expanded command instead of the url. Without the url there's
// nothing to compare with, and the camera is left out - better silent than
// warning about a change that didn't happen. Active RTSP keeps the url, and
// that's the real-camera case.
func FindDrift(file []byte, running map[string]Stream) (Drift, error) {
	var doc struct {
		Streams map[string]yaml.Node `yaml:"streams"`
	}
	if err := yaml.Unmarshal(file, &doc); err != nil {
		return Drift{}, fmt.Errorf("unreadable go2rtc.yaml: %w", err)
	}

	var d Drift
	for name, node := range doc.Streams {
		st, ok := running[name]
		if !ok {
			d.Added = append(d.Added, name)
			continue
		}
		if sourcesChanged(sources(node), st.Producers) {
			d.Changed = append(d.Changed, name)
		}
	}
	for name := range running {
		if _, ok := doc.Streams[name]; !ok {
			d.NotInFile = append(d.NotInFile, name)
		}
	}

	// Both sides are maps: without sorting, the list would change order on
	// every screen reload.
	slices.Sort(d.Added)
	slices.Sort(d.Changed)
	slices.Sort(d.NotInFile)
	return d, nil
}

// sources reads a stream's value, which go2rtc accepts as a string, a list of
// strings or empty.
func sources(node yaml.Node) []string {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value == "" {
			return nil
		}
		return []string{node.Value}
	case yaml.SequenceNode:
		var out []string
		for _, item := range node.Content {
			if item.Kind == yaml.ScalarNode && item.Value != "" {
				out = append(out, item.Value)
			}
		}
		return out
	}
	return nil
}

func sourcesChanged(inFile []string, producers []Producer) bool {
	// go2rtc creates one producer per source, in file order.
	if len(inFile) != len(producers) {
		return true
	}
	for i, p := range producers {
		if p.URL == "" {
			continue // source in use without url: no way to know
		}
		if !sameSource(inFile[i], p.URL) {
			return true
		}
	}
	return false
}

// sameSource compares two sources, tolerating go2rtc having stored the URL
// without the parameters after the #. Changing only one of those parameters
// goes unnoticed; the alternative would be a permanent warning about a change
// that didn't happen.
func sameSource(a, b string) bool {
	if a == b {
		return true
	}
	withoutParams := func(s string) string {
		s, _, _ = strings.Cut(s, "#")
		return s
	}
	return withoutParams(a) == withoutParams(b)
}
