package api

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Dropping someone has two halves. The session falls on its own: the cookie key
// comes from the stored password (see sessionKey), and removing the person or
// generating a new link changes that password, so every request of theirs
// starts being rejected. What does not fall on its own are the connections
// already open, which do not present the cookie again: the live websocket and
// the session warning. Those are what this file takes care of.
//
// WebRTC does not go through here, and dwnvr has no way to cut it: the media
// goes straight from go2rtc to the browser over UDP, and the player itself
// closes the negotiation websocket as soon as the video plays. go2rtc 1.9 has
// no route to drop a consumer; only restarting it, which would stop all cameras
// and the recording. Whoever closes WebRTC is the screen: the session warning
// tells it right away, and the periodic check of Live is the safety net.

// connections are the open connections of each person, by login.
type connections struct {
	mu        sync.Mutex
	perPerson map[string]map[net.Conn]struct{}
}

// registrar records the person's connection. release takes it off the list,
// without closing it.
func (c *connections) register(username string, conn net.Conn) (release func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.perPerson == nil {
		c.perPerson = map[string]map[net.Conn]struct{}{}
	}
	if c.perPerson[username] == nil {
		c.perPerson[username] = map[net.Conn]struct{}{}
	}
	c.perPerson[username][conn] = struct{}{}
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.perPerson[username], conn)
		if len(c.perPerson[username]) == 0 {
			delete(c.perPerson, username)
		}
	}
}

// drop closes all open connections of the person and says how many there were.
func (c *connections) drop(username string) int {
	c.mu.Lock()
	open := c.perPerson[username]
	delete(c.perPerson, username)
	c.mu.Unlock()
	for conn := range open {
		_ = conn.Close()
	}
	return len(open)
}

// dropPerson closes whatever the person has open, after they were removed or
// got a new link.
func (s *Server) dropPerson(username, reason string) {
	if n := s.open.drop(username); n > 0 {
		s.log.Info("person's connections dropped", "user", username, "reason", reason, "connections", n)
	}
}

// hijackRecorder is the ResponseWriter of the live proxy: when
// httputil.ReverseProxy takes over the connection for the websocket, it is
// recorded as the person's.
type hijackRecorder struct {
	http.ResponseWriter
	onTake func(net.Conn)
}

func (w hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err == nil {
		w.onTake(conn)
	}
	return conn, rw, err
}

func (w hijackRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// handleLive is the live proxy with the connection recorded: dropping the
// person closes the websocket, and with it the video that comes inside it (MSE)
// and the negotiation in progress.
func (s *Server) handleLive(proxy http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var release func()
		username := personOf(r).Username
		proxy.ServeHTTP(hijackRecorder{w, func(c net.Conn) { release = s.open.register(username, c) }}, r)
		// ReverseProxy only returns when the websocket closes.
		if release != nil {
			release()
		}
	}
}

// handleSessionWarning is a websocket that carries no message at all: the
// server closes it when the person is removed or gets a new link, and the
// screen, on seeing it close, checks the session. It is how Live learns right
// away that it must close WebRTC, which dwnvr cannot reach.
//
// Written by hand, without a library: with no messages, the protocol comes down
// to the RFC 6455 handshake and waiting for the other side to close.
func (s *Server) handleSessionWarning(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") || key == "" ||
		r.Header.Get("Sec-WebSocket-Version") != "13" {
		writeError(w, http.StatusBadRequest, "esperava um websocket")
		return
	}
	conn, rw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		s.log.Warn("session warning without websocket", "err", err)
		return
	}
	defer conn.Close()

	h := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\n" +
		"Connection: Upgrade\r\nSec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(h[:]) + "\r\n\r\n")
	if rw.Flush() != nil {
		return
	}
	// The server's ReadHeaderTimeout left a deadline on the connection.
	_ = conn.SetDeadline(time.Time{})

	release := s.open.register(personOf(r).Username, conn)
	defer release()
	// The browser sends nothing besides the close: reading to the end is
	// waiting for the tab to close, or for the server to drop it.
	_, _ = io.Copy(io.Discard, rw)
}
