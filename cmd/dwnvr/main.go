// Command dwnvr is a continuous-recording NVR for modest hardware.
//
// It consumes the fMP4 go2rtc already produces, cuts it into keyframe-aligned
// segments and indexes them in NDJSON files. It doesn't decode, doesn't
// transcode and doesn't use a database.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	// The time zone database is embedded in the binary. dwnvr uses local time
	// to decide which day a segment belongs to, and a FROM scratch image has no
	// /usr/share/zoneinfo - without this, TZ would be silently ignored and
	// every day would become UTC.
	_ "time/tzdata"

	"github.com/mhagnumdw/dwnvr/internal/api"
	"github.com/mhagnumdw/dwnvr/internal/buildinfo"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/email"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/logbuf"
	"github.com/mhagnumdw/dwnvr/internal/recorder"
	"github.com/mhagnumdw/dwnvr/internal/retention"
	"github.com/mhagnumdw/dwnvr/internal/store"
	"github.com/mhagnumdw/dwnvr/internal/users"
)

func main() {
	var (
		cfgPath = flag.String("config", "/etc/dwnvr/dwnvr.yaml", "path of dwnvr.yaml")
		debug   = flag.Bool("debug", false, "debug-level log")
		health  = flag.Bool("healthcheck", false, "queries the local dwnvr and exits with 0 if it is healthy")
	)
	flag.Parse()

	// The healthcheck is the binary itself because the image is FROM scratch:
	// there's no shell or curl inside it for the Docker HEALTHCHECK to call.
	if *health {
		os.Exit(healthcheck(*cfgPath))
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	// The latest warnings and errors also stay in memory, so the Diagnostics
	// screen shows them without anyone needing docker logs.
	log := slog.New(logbuf.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}), 50))

	if err := run(log, *cfgPath); err != nil {
		log.Error("dwnvr exited with an error", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger, cfgPath string) error {
	// First log line on purpose: `docker logs dwnvr | head -1` is the cheapest
	// way to check that the deploy really changed the version.
	b := buildinfo.Get()
	log.Info("dwnvr starting", "version", b.Version, "commit", b.Commit, "build", b.Date)

	cfg, warnings, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	warnConfig(log, cfgPath, warnings)
	if cfg.SMTP.Problem != "" {
		log.Warn("e-mail off: the smtp section of dwnvr.yaml is unusable until it is fixed",
			"file", cfgPath, "problem", cfg.SMTP.Problem)
	}
	for _, a := range cfg.Alerts.Email.Warnings {
		log.Warn("e-mail alerts: "+a, "file", cfgPath)
	}
	cams, warnings, err := cfg.LoadCameras()
	if err != nil {
		return err
	}
	warnConfig(log, cfg.CamerasPath(), warnings)
	if len(cams) == 0 {
		log.Warn("no camera registered", "file", cfg.CamerasPath())
	}

	if err := prepareStorage(log, cfg); err != nil {
		return err
	}

	st := store.New(cfg.Storage.Root)
	client := go2rtc.New(cfg.Go2RTC)

	// Loads the index from disk. The most recent day is checked against the
	// files: that's where a crash leaves damage, and checking only it avoids
	// scanning hundreds of thousands of files on every boot.
	start := time.Now()
	for _, cam := range cams {
		idx, err := st.Load(cam.ID)
		if err != nil {
			return fmt.Errorf("reading index of %s: %w", cam.ID, err)
		}
		days := idx.Days()
		log.Info("index loaded", "cam", cam.ID,
			"days", len(days), "mb", idx.TotalBytes()>>20)
	}
	log.Info("index ready", "took", time.Since(start).Round(time.Millisecond))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mgr := recorder.NewManager(cfg, client, st, log)
	mgr.Start(ctx, cams)

	// Retention reads the manager's live list: a quota changed on the
	// registration screen must apply on the next pass, not only after a
	// restart.
	ret := retention.New(cfg, st, mgr.Cameras, log)
	go ret.Run(ctx)

	secret, err := cfg.SessionSecret()
	if err != nil {
		return fmt.Errorf("preparing the session secret: %w", err)
	}
	if !cfg.Server.AuthEnabled() {
		log.Warn("authentication off: anyone on the network sees the recordings of every camera",
			"how_to_enable", "set server.username and server.password in "+cfgPath)
	}

	// An unreadable usuarios.json doesn't stop recording: only the owner logs
	// in until it is fixed, and the screen doesn't write over it.
	registry, registryWarnings, err := users.Open(cfg.UsersPath(), cfg.Server.Username)
	if err != nil {
		log.Error("unreadable user registry; only the owner from dwnvr.yaml logs in until it is fixed", "err", err)
	}
	for _, a := range registryWarnings {
		log.Warn("user registry: "+a, "file", cfg.UsersPath())
	}
	if !cfg.Server.AuthEnabled() && registry.Count() > 0 {
		log.Warn("authentication off: the registry users were ignored",
			"file", cfg.UsersPath(), "users", registry.Count())
	}

	accesses, err := users.OpenAccessLog(cfg.AccessPath(), log)
	if err != nil {
		log.Warn("unreadable last accesses; they start over empty", "err", err)
	}

	// Without the smtp section on, no e-mail is set up. It goes as a nil
	// interface, not as a nil *email.SMTP, which the API would read as on.
	var sender email.Sender
	if cfg.SMTP.Enabled() {
		if smtp, err := email.NewSMTP(cfg.SMTP); err != nil {
			log.Warn("e-mail off", "file", cfgPath, "problem", err)
		} else {
			sender = smtp
			log.Info("e-mail on", "server", cfg.SMTP.Host, "from", cfg.SMTP.From)
		}
	}

	srv := &http.Server{
		Addr:    cfg.Server.Listen,
		Handler: api.New(cfg, st, client, mgr, ret, registry, accesses, sender, secret, log).Handler(),
		// No WriteTimeout: export and the live proxy are long responses by
		// nature, and a cap here would cut them in the middle.
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("dwnvr no ar", "http", cfg.Server.Listen,
			"storage", cfg.Storage.Root, "go2rtc", cfg.Go2RTC.URL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("HTTP server went down", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("shutting down; closing open segments")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)

	// With no new request, what the last minutes noted goes to disk now, and
	// isn't lost on restart.
	if err := accesses.FlushPending(); err != nil {
		log.Error("saving the last accesses", "file", cfg.AccessPath(), "err", err)
	}

	// Stop waits for each open segment to be closed and indexed. Without it,
	// every restart would lose the last minute of each camera.
	mgr.Stop()
	log.Info("stopped")
	return nil
}

// prepareStorage validates the recordings destination before any write.
func prepareStorage(log *slog.Logger, cfg *config.Config) error {
	if err := os.MkdirAll(cfg.Storage.Root, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", cfg.Storage.Root, err)
	}

	free, err := retention.FreeBytes(cfg.Storage.Root)
	if err != nil {
		return err
	}
	total, err := retention.TotalBytes(cfg.Storage.Root)
	if err != nil {
		return err
	}
	log.Info("storage", "path", cfg.Storage.Root,
		"free_gb", free>>30, "total_gb", total>>30, "min_free_mb", cfg.Storage.MinFreeMB)

	if free < cfg.Storage.MinFreeMB<<20 {
		log.Warn("disk already below the minimum free; retention will evict old recordings",
			"free_mb", free>>20, "min_mb", cfg.Storage.MinFreeMB)
	}
	return nil
}

// warnConfig logs each out-of-range value the boot replaced with the default.
// As WARN on purpose: besides docker logs, the line shows on the Diagnostics
// screen, which is where whoever installed it will look.
func warnConfig(log *slog.Logger, file string, warnings []config.Warning) {
	for _, a := range warnings {
		attrs := []any{"file", file}
		if a.Camera != "" {
			attrs = append(attrs, "cam", a.Camera)
		}
		attrs = append(attrs, "field", a.Field, "value", a.Value,
			"accepted", a.Accepted, "using", a.InEffect)
		log.Warn("value out of range in the configuration; the default applies until it is fixed", attrs...)
	}
}

// healthcheck queries the local instance and returns the exit code for the
// Docker HEALTHCHECK.
func healthcheck(cfgPath string) int {
	// The warnings belong to the main process, which already logged them on
	// startup.
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck: unreadable configuration:", err)
		return 1
	}

	// listen is usually ":8080"; the healthcheck talks to the loopback.
	addr := cfg.Server.Listen
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/session", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: HTTP %d\n", resp.StatusCode)
		return 1
	}
	return 0
}
