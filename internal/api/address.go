package api

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

// clientOf is the address of whoever made the request, for the login limit.
//
// Behind a proxy, the connection comes from the proxy, the same for everyone,
// and who really arrived is in X-Forwarded-For. But the header is written by
// the client, and it only counts when the connection comes from a trusted proxy
// (server.trustedProxies). Then it is read from right to left, skipping the
// trusted proxies, and the first address outside them is the client: what is to
// its left was written by someone untrusted and may be made up.
//
// `tailscale serve` and the Caddy of the docs replace the header they receive,
// but a proxy that appends to what came (Caddy with trusted_proxies, for
// example) does not fool this reading.
func clientOf(r *http.Request, trusted []netip.Prefix) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	client, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	client = client.Unmap()

	isTrusted := func(a netip.Addr) bool {
		return slices.ContainsFunc(trusted, func(p netip.Prefix) bool { return p.Contains(a) })
	}
	if !isTrusted(client) {
		return client
	}
	var jumps []string
	for _, v := range r.Header.Values("X-Forwarded-For") {
		jumps = append(jumps, strings.Split(v, ",")...)
	}
	for _, s := range slices.Backward(jumps) {
		a, err := netip.ParseAddr(strings.TrimSpace(s))
		if err != nil {
			break // the rest of the chain is not trusted: keep the last good one
		}
		client = a.Unmap()
		if !isTrusted(client) {
			break
		}
	}
	return client
}
