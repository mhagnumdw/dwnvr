package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/config"
)

// The address of whoever made the request, direct and behind a proxy, with the
// default trustedProxies.
func TestClientOf(t *testing.T) {
	confiaveis, err := config.Prefixes(config.DefaultTrustedProxies)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		nome, remote string
		xff          []string
		wants        string
	}{
		// Directly on 8080, from the home network: the header belongs to
		// whoever sent it.
		{"direct, no header", "192.168.1.5:5000", nil, "192.168.1.5"},
		{"direct, forged header", "192.168.1.5:5000", []string{"203.0.113.9"}, "192.168.1.5"},
		// Through `tailscale serve` or Caddy: the connection comes from the
		// compose network.
		{"through the proxy", "172.18.0.1:5000", []string{"100.64.0.7"}, "100.64.0.7"},
		{"through the proxy, no header", "172.18.0.1:5000", nil, "172.18.0.1"},
		// A proxy that appends to the header that came: the forged one stays on
		// the left.
		{"proxy that appends", "172.18.0.3:5000", []string{"203.0.113.9, 192.168.1.5"}, "192.168.1.5"},
		{"two headers", "172.18.0.3:5000", []string{"203.0.113.9", "192.168.1.5"}, "192.168.1.5"},
		// The whole chain trusted: keep the farthest one.
		{"trusted chain", "127.0.0.1:5000", []string{"172.18.0.1"}, "172.18.0.1"},
		// Garbage in the header: keep the last good address before it.
		{"lixo no header", "172.18.0.1:5000", []string{"garbage, 172.20.0.2"}, "172.20.0.2"},
		{"IPv4 inside IPv6", "[::ffff:172.18.0.1]:5000", []string{"100.64.0.7"}, "100.64.0.7"},
	} {
		t.Run(tc.nome, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/login", nil)
			r.RemoteAddr = tc.remote
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if got := clientOf(r, confiaveis).String(); got != tc.wants {
				t.Errorf("%s with %q: %s, expected %s", tc.remote, tc.xff, got, tc.wants)
			}
		})
	}
}

// With the list empty, no one is trusted: the connection always counts.
func TestClientOfWithoutProxies(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	r.RemoteAddr = "172.18.0.1:5000"
	r.Header.Set("X-Forwarded-For", "100.64.0.7")
	if got := clientOf(r, nil).String(); got != "172.18.0.1" {
		t.Errorf("%s, expected the connection", got)
	}
}
