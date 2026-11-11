// Package api exposes the dwnvr HTTP interface: recordings, live and
// diagnostics.
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/alert"
	"github.com/mhagnumdw/dwnvr/internal/buildinfo"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/email"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/ratelimit"
	"github.com/mhagnumdw/dwnvr/internal/recorder"
	"github.com/mhagnumdw/dwnvr/internal/retention"
	"github.com/mhagnumdw/dwnvr/internal/store"
	"github.com/mhagnumdw/dwnvr/internal/users"
)

type Server struct {
	cfg    *config.Config
	store  *store.Store
	client *go2rtc.Client
	mgr    *recorder.Manager
	log    *slog.Logger
	// sessionSecret is the content of .session-secret, from which comes the key
	// that signs each person's cookies (see sessionKey).
	sessionSecret []byte
	// users are the people in usuarios.json. The owner comes from cfg.
	users *users.Registry
	// accesses is each person's last access, which session records.
	accesses *users.AccessLog
	// sending sends the emails. It is nil without the smtp section enabled:
	// then the email routes answer 409, and the screen hides what is about
	// email.
	sending email.Sender
	// open are the connections that do not present the cookie again (live and
	// the session warning), to drop whoever is removed. See drop.go.
	open connections
	// ret says since when the disk has been below the minimum. It can be nil in
	// tests, and then the warning comes out without the "since".
	ret *retention.Manager
	// When this process started. It is the reference for the application
	// uptime, which the diagnostics screen compares with the machine's to tell
	// "only dwnvr restarted" from "the machine restarted".
	startedAt time.Time
	// Result of the audio probes, so as not to reopen a connection to the
	// camera every time the registry form is opened. See probe.go.
	probes probeCache
	// A storage write test in progress, and the error of the last one that
	// failed. See diagnostics_server.go. Only the goroutine that won writing
	// touches writeErr.
	writing  atomic.Bool
	writeErr string
	// registry queues whoever touches the camera lifecycle: save, remove and
	// delete recordings. Each one reads the list, writes cameras.json and stops
	// or starts a recorder, and two at once - two tabs, two devices - lost each
	// other's change in the file and left an orphan recorder recording outside
	// the Manager's map. The race is between goroutines of this process, the
	// only one that writes there: hence a mutex, and not a lock file, which
	// would only be useful between processes.
	registry sync.Mutex
	// loginLimit counts the login attempts of each address. It is nil with
	// server.loginAttemptsPerMinute at zero: then login pays nothing for it.
	loginLimit ratelimit.Limiter
	// proxies are the networks of server.trustedProxies, already parsed (see
	// clientOf).
	proxies []netip.Prefix
}

// New builds the HTTP server with the pieces main already opened, and the login
// limit from the config.
func New(cfg *config.Config, st *store.Store, client *go2rtc.Client,
	mgr *recorder.Manager, ret *retention.Manager, cad *users.Registry,
	accesses *users.AccessLog, sending email.Sender, secret []byte, log *slog.Logger) *Server {

	s := &Server{cfg: cfg, store: st, client: client, mgr: mgr, ret: ret,
		users: cad, accesses: accesses, sending: sending, sessionSecret: secret, log: log,
		startedAt: time.Now()}
	s.loginLimit, s.proxies = loginLimiter(cfg.Server)
	return s
}

// loginLimiter builds the login limit from the config, already checked at boot
// (see config.Load): an invalid proxy list there has already fallen back to the
// default.
func loginLimiter(c config.Server) (ratelimit.Limiter, []netip.Prefix) {
	proxies, _ := config.Prefixes(c.TrustedProxies)
	if c.LoginAttemptsPerMinute <= 0 {
		return nil, proxies
	}
	return ratelimit.NewGCRA(c.LoginAttemptsPerMinute, ratelimit.LoginBurst), proxies
}

// knownCamera keeps an arbitrary ID coming from the URL from becoming a path on
// disk. Only registered cameras are accepted, which closes directory traversal
// at the source instead of trying to sanitize the path later.
func (s *Server) knownCamera(id string) bool {
	for _, c := range s.mgr.Cameras() {
		if c.ID == id {
			return true
		}
	}
	return false
}

// registeredIDs is the set of registered cameras, in the form the orphan scan
// needs: everything that is in storage and not here is material from a camera
// that was already removed.
func (s *Server) registeredIDs() map[string]bool {
	ids := map[string]bool{}
	for _, c := range s.mgr.Cameras() {
		ids[c.ID] = true
	}
	return ids
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Session
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/session", s.handleSession)
	mux.HandleFunc("GET /api/session/ws", s.requireAuth(s.handleSessionWarning))

	// Invite link: public, because whoever opens it has no password yet. The
	// token comes in the body, and it alone gives access.
	mux.HandleFunc("POST /api/convite/conferir", s.handleCheckInvite)
	mux.HandleFunc("POST /api/convite", s.handleInvite)

	// Users: the screen belongs to the admin.
	mux.HandleFunc("GET /api/usuarios", s.requireAdmin(s.handleUsers))
	mux.HandleFunc("POST /api/usuarios", s.requireAdmin(s.handleCreateUser))
	mux.HandleFunc("POST /api/usuarios/conferir", s.requireAdmin(s.handleCheckNew))
	mux.HandleFunc("POST /api/usuarios/link", s.requireAdmin(s.handleNewLink))
	mux.HandleFunc("POST /api/usuarios/link/email", s.requireAdmin(s.handleSendLink))
	mux.HandleFunc("POST /api/usuarios/email", s.requireAdmin(s.handleChangeEmail))
	mux.HandleFunc("POST /api/usuarios/email/conferir", s.requireAdmin(s.handleCheckEmail))
	mux.HandleFunc("DELETE /api/usuarios", s.requireAdmin(s.handleRemoveUser))
	mux.HandleFunc("DELETE /api/usuarios/avatar", s.requireAdmin(s.handleRemoveAvatarOf))

	// My account: each person changes what is theirs, in any role.
	mux.HandleFunc("GET /api/conta", s.requireAuth(s.handleAccount))
	mux.HandleFunc("POST /api/conta/senha", s.requireAuth(s.handleChangePassword))
	mux.HandleFunc("POST /api/conta/nome", s.requireAuth(s.handleChangeName))
	mux.HandleFunc("PUT /api/conta/avatar", s.requireAuth(s.handleChangeAvatar))
	mux.HandleFunc("DELETE /api/conta/avatar", s.requireAuth(s.handleRemoveAvatar))
	mux.HandleFunc("GET /api/avatar", s.requireAuth(s.handleAvatar))

	// Version stays outside authentication for the same reason as the login
	// screen: it must be visible before signing in. It is also the deploy probe
	// - a curl answers whether dwnvr came up with the new code, without a
	// cookie.
	mux.HandleFunc("GET /api/version", s.handleVersion)

	// Cameras and diagnostics. The camera list is for everyone, and comes
	// trimmed for the viewer; the registry and the server diagnostics, only for
	// the admin.
	mux.HandleFunc("GET /api/cameras", s.requireAuth(s.handleCameras))
	mux.HandleFunc("POST /api/cameras", s.requireAdmin(s.handleSaveCamera))
	mux.HandleFunc("DELETE /api/cameras", s.requireAdmin(s.handleDeleteCamera))
	mux.HandleFunc("GET /api/streams/probe", s.requireAdmin(s.handleProbeStream))
	mux.HandleFunc("POST /api/go2rtc/restart", s.requireAdmin(s.handleRestartGo2rtc))
	mux.HandleFunc("GET /api/health", s.requireAdmin(s.handleHealth))
	mux.HandleFunc("POST /api/reconnects/reset", s.requireAdmin(s.handleResetReconnects))
	mux.HandleFunc("GET /api/health/servidor", s.requireAdmin(s.handleServerDiagnostics))
	mux.HandleFunc("POST /api/email/teste", s.requireAdmin(s.handleEmailTest))
	mux.HandleFunc("DELETE /api/rec", s.requireAdmin(s.handleDeleteRecordings))

	// Recordings
	mux.HandleFunc("GET /api/rec/days", s.requireAuth(s.handleDays))
	mux.HandleFunc("GET /api/rec/timeline", s.requireAuth(s.handleTimeline))
	mux.HandleFunc("GET /api/rec/events", s.requireAuth(s.handleEvents))
	mux.HandleFunc("GET /api/rec/init", s.requireAuth(s.handleInit))
	mux.HandleFunc("GET /api/rec/seg", s.requireAuth(s.handleSegment))
	mux.HandleFunc("GET /api/rec/thumb", s.requireAuth(s.handleThumb))
	mux.HandleFunc("GET /api/rec/playlist.m3u8", s.requireAuth(s.handlePlaylist))
	mux.HandleFunc("GET /api/rec/export", s.requireAuth(s.handleExport))

	// Object detections, from all cameras together: the Detections screen.
	mux.HandleFunc("GET /api/deteccoes", s.requireAuth(s.handleDetections))
	mux.HandleFunc("GET /api/deteccoes/quadro", s.requireAuth(s.handleDetectionFrame))
	mux.HandleFunc("GET /api/deteccoes/dias", s.requireAuth(s.handleDetectionDays))

	// Live: signaling and media stay with go2rtc; dwnvr only proxies the
	// player's websocket, and the rest of the go2rtc API stays out.
	mux.HandleFunc("GET /api/live/ws", s.requireAuth(s.handleLive(s.liveProxy())))

	// The interface is served WITHOUT authentication, on purpose: it is only
	// HTML, CSS and JS, with no camera data at all. Protecting it would keep
	// the browser from loading the login screen itself. Everything that is data
	// is behind the API.
	mux.Handle("/", s.webHandler())

	return logRequests(s.log, mux)
}

// cameraInfo is the camera as the screen sees it: the registry entry with the
// defaults already applied, plus the directory where its recordings live.
//
// The path does not go into config.Camera because it is not registry - it is a
// consequence of the server's storage.root, and a field there would end up
// written to cameras.json as if it were configurable.
type cameraInfo struct {
	config.Camera
	Dir string `json:"dir"`
}

// handleCameras lists the registered cameras, what go2rtc offers and what is
// left on disk from cameras already removed. The viewer gets only the cameras
// and the object detector (see viewerCameras).
//
// Putting the three things in a single response is what lets the registry screen
// show only streams that really exist, instead of asking the user to type a name
// and find the error later - and it is where orphan recordings become visible
// again, since no other endpoint sees a camera without a registry entry.
func (s *Server) handleCameras(w http.ResponseWriter, r *http.Request) {
	if !personOf(r).admin() {
		s.viewerCameras(w)
		return
	}
	raw := s.mgr.Cameras()
	cams := make([]cameraInfo, len(raw))
	registered := map[string]bool{}
	for i, c := range raw {
		cams[i] = cameraInfo{Camera: s.cfg.Resolve(c), Dir: s.store.Camera(c.ID).Dir()}
		registered[c.ID] = true
	}

	type streamInfo struct {
		Name        string   `json:"name"`
		Registered  bool     `json:"registered"`
		HasAudio    bool     `json:"hasAudio"`
		AudioCodecs []string `json:"audioCodecs,omitempty"`
		Transcoding bool     `json:"transcoding"`
	}

	// "padrao" is an empty camera with the defaults applied: the new camera
	// form starts from it, to follow the defaults of dwnvr.yaml. "faixas" gives
	// the min/max of the inputs, so the screen enforces the same rule as the
	// API.
	resp := map[string]any{"cameras": cams, "padrao": s.cfg.Resolve(config.Camera{}),
		"faixas": config.CameraRanges}

	// The interface only shows the Detections tab with the object detector
	// configured: without it there is no detection at all to list. It goes
	// here, and not in /api/health, because this is the response the interface
	// fetches on entering.
	resp["detector"] = s.mgr.Detector() != nil

	// The Alert tab says where the alert goes, and only the admin sees it.
	resp["alertEmail"] = s.emailAlert()

	if orphans, err := s.store.Orphans(registered); err != nil {
		// It does not block the listing: in the common case there is no orphan
		// at all, and a failure scanning the storage must not bring down the
		// whole cameras screen.
		s.log.Error("listing orphan recordings", "err", err)
	} else {
		resp["orphans"] = orphans
	}

	streams, err := s.client.Streams(r.Context())
	if err != nil {
		// go2rtc being down cannot block listing the cameras already registered
		// - only discovering new ones.
		resp["go2rtcError"] = err.Error()
		writeJSON(w, resp)
		return
	}

	available := make([]streamInfo, 0, len(streams))
	for name, st := range streams {
		info := streamInfo{Name: name, Registered: registered[name]}
		for _, p := range st.Producers {
			if p.Transcoding() {
				info.Transcoding = true
			}
			if p.HasAudio() {
				info.HasAudio = true
				info.AudioCodecs = append(info.AudioCodecs, p.AudioCodecs()...)
			}
		}
		available = append(available, info)
	}
	// go2rtc returns the streams in a map, and Go does not guarantee order when
	// iterating it: without this the list changed order on every screen reload.
	slices.SortFunc(available, func(a, b streamInfo) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	resp["streams"] = available

	// go2rtc only reads go2rtc.yaml on startup: a camera added later does not
	// show up in streams, and nothing on the screen explained why. The
	// difference between the file and what it serves becomes the restart
	// warning.
	//
	// Failing here is nobody's error - an external go2rtc may not expose
	// /api/config -, and the screen goes on without the warning. Debug, and not
	// Warn, because this runs every time the screen opens and would flood the
	// Diagnostics warnings.
	if file, err := s.client.ConfigFile(r.Context()); err != nil {
		s.log.Debug("reading go2rtc.yaml through the go2rtc API", "err", err)
	} else if d, err := go2rtc.FindDrift(file, streams); err != nil {
		s.log.Debug("comparing go2rtc.yaml with the streams", "err", err)
	} else if !d.Empty() {
		resp["go2rtcConfigFile"] = d
	}
	writeJSON(w, resp)
}

// viewerCameras is the viewer's /api/cameras: the cameras, without the path on disk
// and with only the "alert enabled", and whether there is an object detector. The rest
// only serves the Cameras screen, which belongs to the admin, and go2rtc is not even
// queried: each opening of the screen would cost two calls to it.
func (s *Server) viewerCameras(w http.ResponseWriter) {
	raw := s.mgr.Cameras()
	cams := make([]config.Camera, len(raw))
	for i, c := range raw {
		cams[i] = s.cfg.Resolve(c)
		// Of the alert only the enabled flag goes, which makes Detections show
		// the "alerts only". Its families and areas stay with the admin: what
		// alerted comes marked in each detection.
		if a := cams[i].Alert; a != nil && a.Enabled {
			cams[i].Alert = &config.CameraAlert{Enabled: true}
		} else {
			cams[i].Alert = nil
		}
	}
	writeJSON(w, map[string]any{"cameras": cams, "detector": s.mgr.Detector() != nil})
}

// handleRestartGo2rtc makes go2rtc reread go2rtc.yaml. All cameras stop for a
// few seconds; the screen is what warns about it beforehand.
func (s *Server) handleRestartGo2rtc(w http.ResponseWriter, r *http.Request) {
	if err := s.client.Restart(r.Context()); err != nil {
		s.log.Error("restarting go2rtc", "err", err)
		writeError(w, http.StatusBadGateway, "o go2rtc não aceitou reiniciar: "+err.Error())
		return
	}
	s.log.Info("go2rtc restarted from the interface")
	writeJSON(w, map[string]bool{"reiniciado": true})
}

// handleResetReconnects restarts the reconnect count of all cameras. It drops
// no connection at all: only the numbers on the screen go back to zero.
func (s *Server) handleResetReconnects(w http.ResponseWriter, r *http.Request) {
	s.mgr.ResetReconnects()
	s.log.Info("reconnects reset from the interface")
	writeJSON(w, map[string]bool{"zeradas": true})
}

// handleVersion says which code is running.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, buildinfo.Get())
}

// handleHealth feeds the diagnostics screen.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{"cameras": s.mgr.Status()}

	if free, err := retention.FreeBytes(s.cfg.Storage.Root); err == nil {
		total, _ := retention.TotalBytes(s.cfg.Storage.Root)
		var used int64
		for _, c := range s.mgr.Status() {
			used += c.DiskBytes
		}
		disk := map[string]any{
			"freeBytes":  free,
			"totalBytes": total,
			"dwnvrBytes": used,
			"minFreeMB":  s.cfg.Storage.MinFreeMB,
			"belowMin":   free < s.cfg.Storage.MinFreeMB<<20,
		}
		// Orphans go separately, and not added to dwnvrBytes: they belong to
		// dwnvr, but no retention limit reaches them, and that is what the
		// screen needs to say. Left out of the count, they would show up as
		// space of other programs. With no orphan, the scan is a ReadDir of the
		// storage.
		if orphans, err := s.store.OrphanBytes(s.registeredIDs()); err != nil {
			s.log.Error("listing orphan recordings", "err", err)
		} else {
			disk["orphanBytes"] = orphans
		}
		// The "since" comes from retention, which measures the disk on every
		// pass. Right after the disk crosses the minimum it has not passed yet,
		// and the warning comes out without it for up to a minute.
		if s.ret != nil {
			if since := s.ret.BelowMinimumSince(); !since.IsZero() {
				disk["belowMinSince"] = since
			}
		}
		resp["disk"] = disk
	}

	// Seconds, and not an ISO instant: the browser clock and the server clock
	// are not the same, and a time zone or NTP difference would become a false
	// "up for 3 hours". A duration already computed here cannot be misread
	// there.
	up := map[string]any{"appSeconds": int64(time.Since(s.startedAt).Seconds())}
	if d, ok := machineUptime(); ok {
		up["machineSeconds"] = int64(d.Seconds())
	}
	resp["uptime"] = up

	// The object detector queue is a single one for all cameras, so it goes
	// outside their list. It disappears without an object detector configured.
	if d := s.mgr.Detector(); d != nil {
		resp["detector"] = d
	}

	// Of the alert goes the total since dwnvr started, which disappears while
	// no camera has enabled the alert: the alert itself is stored in the
	// detection, and is seen in Detections. With a discard episode in effect,
	// the alerts that did not go out through the notifiers, for the yellow
	// Warning; with a notifier failing, the since when and the error, for the
	// red one. And the warnings of the alerts section of dwnvr.yaml, for the
	// Warnings card, which hold even without a camera with an alert: they are
	// about the configuration.
	alerts := map[string]any{}
	if c := s.mgr.Alerts(); c != nil {
		alerts["total"] = c.Total()
		if d, ok := c.Drops(); ok {
			alerts["dropped"] = map[string]any{
				"queueFull": d.QueueFull, "overLimit": d.OverCap,
				"limitPerHour": alert.AlertsPerHour,
				"since":        d.Since, "lastAt": d.Last,
			}
		}
		var failing []map[string]any
		for _, m := range c.NotifierStates() {
			if !m.FailingSince.IsZero() {
				failing = append(failing, map[string]any{
					"name": m.Name, "since": m.FailingSince, "error": m.Err,
				})
			}
		}
		if len(failing) > 0 {
			alerts["failing"] = failing
		}
	}
	if av := s.cfg.Alerts.Email.Warnings; len(av) > 0 {
		alerts["warnings"] = av
	}
	if len(alerts) > 0 {
		resp["alerts"] = alerts
	}

	// The clock, unlike the uptimes above, does go as an instant - and it is
	// the only field of this response that does. The screen wants precisely the
	// time OVER THERE: whoever administers dwnvr looks at this to know whether
	// the server has the wrong time zone or NTP, and a duration would not
	// answer that. The offset is already embedded in the text and the interface
	// shows it without reconverting; converting to the browser's time zone
	// would give the time the user already has on their own clock, which is not
	// the question.
	now := time.Now()
	sigla, offset := now.Zone()
	clock := map[string]any{
		"now":           now.Format(time.RFC3339),
		"abbr":          sigla,
		"offsetSeconds": offset,
	}
	if name := localZone(); name != "" {
		clock["zone"] = name
	}
	resp["clock"] = clock

	// The window size goes along so the screen writes "in the last 8h" without
	// repeating the number.
	resp["reconnectsWindowHours"] = recorder.ReconnectWindow.Hours()

	// It comes here, and not only in /api/cameras, because this is the endpoint
	// the diagnostics screen rereads every few seconds: go2rtc going down with
	// it open has to show up, and go away when it comes back.
	if s.client != nil {
		if since, errText := s.client.Unreachable(); !since.IsZero() {
			resp["go2rtc"] = map[string]any{"error": errText, "since": since}
		}
	}

	// The smtp section with a problem becomes a Warning on the screen, and not
	// only a log line: email stays disabled until someone fixes dwnvr.yaml and
	// restarts.
	if s.cfg.SMTP.Problem != "" {
		resp["smtp"] = map[string]any{"error": s.cfg.SMTP.Problem}
	}

	// usuarios.json edited by hand with a problem too: the ignored entry does
	// not get in, and the repeated email does not work for signing in. Without
	// authentication the whole registry is ignored, and the warning would say
	// nothing.
	if s.cfg.Server.AuthEnabled() {
		u := map[string]any{}
		if err := s.users.Unreadable(); err != nil {
			u["error"] = err.Error()
		}
		if warnings := s.users.Warnings(); len(warnings) > 0 {
			u["warnings"] = warnings
		}
		if len(u) > 0 {
			resp["users"] = u
		}
	}

	writeJSON(w, resp)
}

// localZone finds the IANA name of the server time zone ("America/Fortaleza").
//
// The time package does not expose it: time.Local is called "Local" and what is
// left is the abbreviation ("-03"), which does not identify the region - "-03"
// is São Paulo, Buenos Aires and a handful of other places. The sources checked
// here are the same ones time itself uses to build time.Local, in the same
// order.
//
// It returns "" when none of them answers, and then the screen shows only the
// time and the abbreviation: the name is a detail of the title, not the main
// information.
func localZone() string {
	// TZ can come as ":America/Fortaleza" or point to a file; only the name
	// matters.
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); tz != "" && !strings.HasPrefix(tz, "/") {
		return tz
	}
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s
		}
	}
	// /etc/localtime is usually a link to .../zoneinfo/America/Fortaleza.
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, after, ok := strings.Cut(target, "zoneinfo/"); ok {
			return after
		}
	}
	return ""
}

// --- utilities -------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The response was already partially written; all that is left is to
		// log it.
		return
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// fail logs the real error and returns a generic message: filesystem detail
// must not leak to the browser.
func (s *Server) fail(w http.ResponseWriter, what string, err error) {
	s.log.Error(what, "err", err)
	writeError(w, http.StatusInternalServerError, what)
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (w *statusWriter) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

// Hijack must be passed through for the WebSocket proxy to work.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(sw, r)
		// Only what went wrong becomes a log: with a timeline asking for
		// hundreds of segments, logging everything would drown the server
		// journal. 429 stays out: whoever goes over the limit insists, and the
		// limiter already logs the start of each episode (see limitLogin).
		if sw.code >= 400 && sw.code != http.StatusTooManyRequests {
			log.Warn("request rejected", "status", sw.code,
				"method", r.Method, "path", r.URL.Path, "from", r.RemoteAddr, forwarded(r))
		}
	})
}
