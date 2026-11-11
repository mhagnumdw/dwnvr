// Package config loads dwnvr's configuration.
//
// The configuration is deliberately split in two files:
//
//   - dwnvr.yaml   infrastructure, edited by hand, NEVER rewritten by the app
//   - cameras.json the camera list, managed by the camera API
//
// The split exists because rewriting a YAML file wipes out the comments of
// whoever wrote it. Since the cameras screen needs to save cameras, mixing the
// two would destroy the user's notes on every click.
package config

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/mhagnumdw/dwnvr/internal/area"
	"github.com/mhagnumdw/dwnvr/internal/detect"
)

// Audio modes supported per camera.
const (
	AudioNone = "none"
	AudioFLAC = "flac"
	AudioAAC  = "aac"
)

// DefaultStallSeconds is how long without receiving a single byte is enough
// to consider the stream dead.
//
// Fifteen seconds is generous: even a 1 fps camera sends bytes every second.
// The number doesn't need to be tight - it needs to be MUCH smaller than the
// 3h38 a camera spent silently stopped before this existed.
const DefaultStallSeconds = 15

type Config struct {
	Server   Server   `yaml:"server"`
	Go2RTC   Go2RTC   `yaml:"go2rtc"`
	Storage  Storage  `yaml:"storage"`
	Defaults Defaults `yaml:"defaults"`
	Detector Detector `yaml:"detector"`
	SMTP     SMTP     `yaml:"smtp"`
	Alerts   Alerts   `yaml:"alerts"`

	// dir is the directory of dwnvr.yaml; cameras.json sits next to it.
	dir string `yaml:"-"`
}

type Server struct {
	Listen string `yaml:"listen"`

	// Leaving username and password empty turns authentication off. That's
	// acceptable on a trusted network, and it's the default so as not to block
	// the first use - but dwnvr warns in the log, because whoever opens the
	// UI sees the recordings of every camera.
	Username string `yaml:"username"`
	Password string `yaml:"password"`

	// LoginAttemptsPerMinute is the rate of login attempts from each address,
	// after a burst of ratelimit.LoginBurst in a row. Zero turns the limit
	// off.
	LoginAttemptsPerMinute int `yaml:"loginAttemptsPerMinute"`

	// TrustedProxies are the networks of the proxies whose X-Forwarded-For
	// says who really arrived (see api.clienteDe). Outside them, the
	// connection's address counts, because the client is the one writing the
	// header. An empty list trusts nobody.
	TrustedProxies []string `yaml:"trustedProxies"`

	// PublicURL is the address dwnvr is opened at, like
	// "https://dwnvr.exemplo.ts.net": the server has no way to know it,
	// because each person comes in a different way. The alert e-mail uses it
	// in the button that opens the recording at the instant. Empty, the
	// e-mail goes out without the button. It's stored without the trailing
	// "/" (see checkPublicURL).
	PublicURL string `yaml:"publicUrl"`
}

// AuthEnabled says whether any credential was configured.
func (s Server) AuthEnabled() bool { return s.Username != "" || s.Password != "" }

// SecretPath is where the secret that signs the session cookies lives. It's
// generated on first use; keeping it in a file avoids dropping every session
// on each process restart.
func (c *Config) SecretPath() string { return filepath.Join(c.dir, ".session-secret") }

type Go2RTC struct {
	URL      string `yaml:"url"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// Detector is the object detector: the `dwnvr-detect` container, which says
// whether the motion was a person, a vehicle or an animal. It's optional and
// kept apart because decoding video and running a model require native code,
// and dwnvr is pure Go.
type Detector struct {
	// URL of the sidecar, e.g. http://dwnvr-detect:8480. Empty means "no
	// object detector": cameras with `detect` on keep marking only motion, and
	// dwnvr doesn't keep a single extra byte of video because of it.
	URL string `yaml:"url"`
}

type Storage struct {
	// Root is where the recordings are written.
	Root string `yaml:"root"`

	// MinFreeMB is the global free-disk lock. When the disk drops below it,
	// dwnvr evicts the oldest recordings of every camera, regardless of the
	// individual quotas. It exists because the sum of the quotas is easy to
	// get wrong, and filling the disk is worse than losing old recordings.
	MinFreeMB int64 `yaml:"minFreeMB"`
}

// Defaults apply to any camera that doesn't override the field.
type Defaults struct {
	SegmentSeconds int    `yaml:"segmentSeconds"`
	QuotaMB        int64  `yaml:"quotaMB"`
	MaxDays        int    `yaml:"maxDays"`
	Audio          string `yaml:"audio"`
	StallSeconds   int    `yaml:"stallSeconds"`

	// Detect turns motion marking on. It comes off: whoever didn't ask for the
	// heat strip on the timeline - nor the object detector it wakes up -
	// shouldn't start paying for it.
	Detect bool `yaml:"detect"`

	DetectMethod      string `yaml:"detectMecanismo"`
	DetectSensitivity int    `yaml:"detectSensibilidade"`
}

// Camera is a registered camera. The ID is the stream name in go2rtc: dwnvr
// stores no camera URL or credential, because configuring go2rtc is not its
// job.
type Camera struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`

	// Zero in any field below means "use the default".
	SegmentSeconds int    `json:"segmentSeconds,omitempty"`
	QuotaMB        int64  `json:"quotaMB,omitempty"`
	MaxDays        int    `json:"maxDays,omitempty"`
	Audio          string `json:"audio,omitempty"`

	// StallSeconds is per camera because the tolerance depends on the link: a
	// camera on poor Wi-Fi may need more slack than one on a cable, and
	// lowering the threshold too much trades silent loss for excessive
	// reconnections.
	StallSeconds int `json:"stallSeconds,omitempty"`

	// Detect turns motion marking on for this camera.
	//
	// It's a pointer, not a bool, because here zero MUST be distinguishable
	// from absent: with a bool, "off" and "not configured" would be the same
	// thing, and a camera turned off by hand would turn itself back on as soon
	// as the default changed.
	Detect *bool `json:"detect,omitempty"`

	// DetectMethod picks what decides to fire: `kleinberg-p`, the best of the
	// 24 candidates measured, or `periodico`, which fires every X seconds and
	// is the honest reference to beat.
	//
	// It's per camera because cameras aren't alike: on one of them, measured,
	// the winner caught 10.1% of arrivals and the runner-up 23.4%. Switching
	// the method of a difficult camera is cheaper than raising the level of
	// all of them.
	DetectMethod string `json:"detectMecanismo,omitempty"`

	// DetectSensitivity is the level from 1 to 5. It's the COST: each level
	// doubles the number of detections per hour, and each detection is the
	// feature's entire CPU cost.
	DetectSensitivity int `json:"detectSensibilidade,omitempty"`

	// Areas are the areas drawn over this camera's image, which the
	// Detections filter and the alert use. Changing an area doesn't restart
	// recording: recordingParamsChanged doesn't look at them.
	Areas []Area `json:"areas,omitempty"`

	// Alert says when a detection from this camera becomes an alert. Absent
	// means no alert, same as off. It doesn't restart recording either.
	Alert *CameraAlert `json:"alert,omitempty"`
}

func defaults() Config {
	return Config{
		Server: Server{
			Listen:                 ":8080",
			LoginAttemptsPerMinute: DefaultLoginAttemptsPerMinute,
			TrustedProxies:         slices.Clone(DefaultTrustedProxies),
		},
		Go2RTC: Go2RTC{URL: "http://localhost:1984"},
		Storage: Storage{
			Root:      "/mnt/storage/dwnvr/recordings",
			MinFreeMB: 2048,
		},
		Defaults: Defaults{
			SegmentSeconds:    30,
			QuotaMB:           10240,
			Audio:             AudioNone,
			StallSeconds:      DefaultStallSeconds,
			Detect:            false,
			DetectMethod:      detect.DefaultMethod,
			DetectSensitivity: detect.DefaultLevel,
		},
	}
}

// Load reads dwnvr.yaml. A missing file is not an error: dwnvr starts with
// the defaults, which makes first use trivial.
//
// Only what keeps dwnvr from working is an error: an unreadable file and an
// empty storage.root. A value out of range goes back to the code's default
// and becomes a Warning, because recording with the default is better than
// recording nothing. A broken smtp section doesn't block boot either, but
// doesn't become a Warning: there's no default to stand in for a wrong server,
// and e-mail stays off (see SMTP.Problem). The alerts section never blocks
// boot: the alerts.email.to list keeps only the addresses that work (see
// Alerts.UnmarshalYAML and AlertsEmail.Warnings).
func Load(path string) (*Config, []Warning, error) {
	cfg := defaults()
	cfg.dir = filepath.Dir(path)

	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return &cfg, nil, nil
	case err != nil:
		return nil, nil, err
	}

	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	if cfg.Storage.Root == "" {
		return nil, nil, errors.New("storage.root can't be empty")
	}
	cfg.SMTP.Problem = cfg.SMTP.check()
	cfg.Alerts.Email.Warnings = append(cfg.Alerts.Email.Warnings, cfg.Alerts.Email.check(cfg.SMTP)...)
	return &cfg, cfg.check(), nil
}

// Warning is an out-of-range value found in a configuration file. It doesn't
// keep dwnvr from starting: the field takes the default, and the warning says
// what was written, what's accepted and what took effect instead.
//
// Its texts are in pt-BR because they reach the screen; they move to the
// catalogs with i18n.
type Warning struct {
	Camera   string // empty when the value came from dwnvr.yaml
	Field    string // as written in the file: "quotaMB", "defaults.quotaMB"
	Value    any
	Accepted string // the recommendation: "de 10 a 300 s"
	InEffect any    // what took effect instead
}

// Text is the sentence for whoever can fix it right away; the API rejects
// with it.
func (a Warning) Text() string {
	return fmt.Sprintf("%s %v fora do aceito: use %s", a.Field, a.Value, a.Accepted)
}

// check swaps every out-of-range value in defaults and storage for the code's
// default. Here zero isn't "use the default" as on the camera: the default is
// the value itself, so a written zero is as invalid as a negative.
func (c *Config) check() []Warning {
	p := defaults()
	var av []Warning
	checkRange(&av, Warning{Field: "server.loginAttemptsPerMinute"}, &c.Server.LoginAttemptsPerMinute,
		LoginAttemptsPerMinuteRange, p.Server.LoginAttemptsPerMinute, p.Server.LoginAttemptsPerMinute)
	if _, err := Prefixes(c.Server.TrustedProxies); err != nil {
		av = append(av, Warning{Field: "server.trustedProxies", Value: c.Server.TrustedProxies,
			Accepted: "endereços IP ou redes CIDR, como 172.16.0.0/12", InEffect: p.Server.TrustedProxies})
		c.Server.TrustedProxies = p.Server.TrustedProxies
	}
	if e, ok := checkPublicURL(c.Server.PublicURL); ok {
		c.Server.PublicURL = e
	} else {
		av = append(av, Warning{Field: "server.publicUrl", Value: c.Server.PublicURL,
			Accepted: "o endereço do dwnvr, como https://dwnvr.exemplo.ts.net",
			InEffect: "nenhum: o e-mail de alerta sai sem o botão"})
		c.Server.PublicURL = p.Server.PublicURL
	}
	checkRange(&av, Warning{Field: "storage.minFreeMB"}, &c.Storage.MinFreeMB,
		MinFreeMBRange, p.Storage.MinFreeMB, p.Storage.MinFreeMB)

	d, pd := &c.Defaults, p.Defaults
	checkRange(&av, Warning{Field: "defaults.segmentSeconds"}, &d.SegmentSeconds,
		SegmentSecondsRange, pd.SegmentSeconds, pd.SegmentSeconds)
	checkRange(&av, Warning{Field: "defaults.quotaMB"}, &d.QuotaMB, QuotaMBRange, pd.QuotaMB, pd.QuotaMB)
	checkRange(&av, Warning{Field: "defaults.maxDays"}, &d.MaxDays, MaxDaysRange, pd.MaxDays, pd.MaxDays)
	checkRange(&av, Warning{Field: "defaults.stallSeconds"}, &d.StallSeconds,
		StallSecondsRange, pd.StallSeconds, pd.StallSeconds)
	if !validAudio(d.Audio) {
		av = append(av, Warning{Field: "defaults.audio", Value: d.Audio,
			Accepted: acceptedAudio, InEffect: pd.Audio})
		d.Audio = pd.Audio
	}
	if _, ok := detect.Methods[d.DetectMethod]; !ok {
		av = append(av, Warning{Field: "defaults.detectMecanismo", Value: d.DetectMethod,
			Accepted: acceptedMethods(), InEffect: pd.DetectMethod})
		d.DetectMethod = pd.DetectMethod
	}
	checkRange(&av, Warning{Field: "defaults.detectSensibilidade"}, &d.DetectSensitivity,
		sensitivityRange, pd.DetectSensitivity, pd.DetectSensitivity)
	return av
}

// checkPublicURL accepts an empty server.publicUrl, or http(s)://host with an
// optional path, for dwnvr behind a proxy on a subpath. It rejects an address
// with only the port (https://:8080), a query, a "#" and user:password, which
// would break the link built on top of it. It returns the address without the
// trailing "/".
func checkPublicURL(e string) (string, bool) {
	e = strings.TrimSpace(e)
	if e == "" {
		return "", true
	}
	u, err := url.Parse(e)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(e, "#") {
		return "", false
	}
	return strings.TrimRight(e, "/"), true
}

// CheckCamera returns the camera with each out-of-range field zeroed, which is
// Resolve's "use the default", and one Warning per replaced field. It's the
// rule for both doors: the API rejects if there's a warning, boot starts with
// the camera fixed.
//
// The ID doesn't go through here: it becomes a directory name, and there's no
// default to stand in for it. See ValidateCameraID.
func (c *Config) CheckCamera(cam Camera) (Camera, []Warning) {
	var av []Warning
	base := func(field string) Warning { return Warning{Camera: cam.ID, Field: field} }

	// Zero in any field is "use the default", and so it's never invalid.
	if cam.QuotaMB != 0 {
		checkRange(&av, base("quotaMB"), &cam.QuotaMB, QuotaMBRange, 0, c.Defaults.QuotaMB)
	}
	if cam.SegmentSeconds != 0 {
		checkRange(&av, base("segmentSeconds"), &cam.SegmentSeconds,
			SegmentSecondsRange, 0, c.Defaults.SegmentSeconds)
	}
	if cam.MaxDays != 0 {
		checkRange(&av, base("maxDays"), &cam.MaxDays, MaxDaysRange, 0, c.Defaults.MaxDays)
	}
	if cam.StallSeconds != 0 {
		checkRange(&av, base("stallSeconds"), &cam.StallSeconds,
			StallSecondsRange, 0, c.Defaults.StallSeconds)
	}
	if cam.Audio != "" && !validAudio(cam.Audio) {
		a := base("audio")
		a.Value, a.Accepted, a.InEffect = cam.Audio, acceptedAudio, c.Defaults.Audio
		av = append(av, a)
		cam.Audio = ""
	}
	if _, ok := detect.Methods[cam.DetectMethod]; cam.DetectMethod != "" && !ok {
		a := base("detectMecanismo")
		a.Value, a.Accepted, a.InEffect = cam.DetectMethod, acceptedMethods(), c.Defaults.DetectMethod
		av = append(av, a)
		cam.DetectMethod = ""
	}
	if cam.DetectSensitivity != 0 {
		checkRange(&av, base("detectSensibilidade"), &cam.DetectSensitivity,
			sensitivityRange, 0, c.Defaults.DetectSensitivity)
	}
	cam.Areas = checkAreas(cam, &av)
	cam.Alert = checkAlert(cam, &av)
	return cam, av
}

// checkRange checks a numeric field. Out of range, the value is swapped for
// `replacement` and becomes a Warning that says what took effect,
// `inEffect`. On the camera the two differ (the replacement is zero, and the
// default is what applies); in dwnvr.yaml they're the same number.
func checkRange[T int | int64](av *[]Warning, a Warning, v *T, f Range, replacement, inEffect T) {
	if f.Contains(int64(*v)) {
		return
	}
	a.Value, a.Accepted, a.InEffect = *v, f.String(), inEffect
	*av = append(*av, a)
	*v = replacement
}

// Prefixes reads the server.trustedProxies list. Each item is a CIDR network
// or a single address, which becomes a network of its own (/32 or /128).
func Prefixes(items []string) ([]netip.Prefix, error) {
	ps := make([]netip.Prefix, 0, len(items))
	for _, it := range items {
		if p, err := netip.ParsePrefix(it); err == nil {
			ps = append(ps, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(it)
		if err != nil {
			return nil, fmt.Errorf("%q is neither an address nor a network", it)
		}
		ps = append(ps, netip.PrefixFrom(a, a.BitLen()))
	}
	return ps, nil
}

const acceptedAudio = "none, flac ou aac"

func validAudio(mode string) bool {
	switch mode {
	case AudioNone, AudioFLAC, AudioAAC:
		return true
	}
	return false
}

func acceptedMethods() string {
	names := make([]string, 0, len(detect.Methods))
	for n := range detect.Methods {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, " ou ")
}

// CamerasPath is the path of cameras.json, next to dwnvr.yaml.
func (c *Config) CamerasPath() string { return filepath.Join(c.dir, "cameras.json") }

// UsersPath is where the people added on screen live, besides the owner from
// dwnvr.yaml. It sits next to cameras.json, not in storage, which is only for
// recordings.
func (c *Config) UsersPath() string { return filepath.Join(c.dir, "usuarios.json") }

// AccessPath is where each person's last access lives, next to usuarios.json,
// but in a separate file (see usuarios.Acessos).
func (c *Config) AccessPath() string { return filepath.Join(c.dir, "acessos.json") }

// Resolve returns the camera with the defaults already applied, so the rest of
// the code never has to ask "is this zero intentional?".
func (c *Config) Resolve(cam Camera) Camera {
	if cam.SegmentSeconds <= 0 {
		cam.SegmentSeconds = c.Defaults.SegmentSeconds
	}
	if cam.QuotaMB <= 0 {
		cam.QuotaMB = c.Defaults.QuotaMB
	}
	if cam.MaxDays <= 0 {
		cam.MaxDays = c.Defaults.MaxDays
	}
	if cam.Audio == "" {
		cam.Audio = c.Defaults.Audio
	}
	if cam.StallSeconds <= 0 {
		cam.StallSeconds = c.Defaults.StallSeconds
	}
	if cam.Detect == nil {
		v := c.Defaults.Detect
		cam.Detect = &v
	}
	if cam.DetectMethod == "" {
		cam.DetectMethod = c.Defaults.DetectMethod
	}
	if cam.DetectSensitivity <= 0 {
		cam.DetectSensitivity = c.Defaults.DetectSensitivity
	}
	if cam.Name == "" {
		cam.Name = cam.ID
	}
	// The copy: the slice is the same as the raw camera the Manager keeps, and
	// the rule written here would change its.
	if len(cam.Areas) > 0 {
		cam.Areas = slices.Clone(cam.Areas)
		for i := range cam.Areas {
			if cam.Areas[i].Rule == "" {
				cam.Areas[i].Rule = area.DefaultRule
			}
		}
	}
	return cam
}

// DropNewDefaults is Resolve in reverse at save time: it zeroes the field that
// arrives equal to the default and that the camera didn't set before.
// `before` is the camera as it was stored, or nil when it's new.
//
// The screen edits the camera as GET /api/cameras shows it, already resolved,
// and sends it back whole. Saved like that, it ended up with every default's
// value in cameras.json, and changing the defaults in dwnvr.yaml no longer
// reached it.
//
// A field the camera already set stays, even if equal to the default: someone
// chose it. The price is that the screen can't pin on a camera a value equal
// to today's default; it follows the default.
func (c *Config) DropNewDefaults(cam Camera, before *Camera) Camera {
	var a Camera
	if before != nil {
		a = *before
	}
	d := c.Defaults
	if a.SegmentSeconds <= 0 && cam.SegmentSeconds == d.SegmentSeconds {
		cam.SegmentSeconds = 0
	}
	if a.QuotaMB <= 0 && cam.QuotaMB == d.QuotaMB {
		cam.QuotaMB = 0
	}
	if a.MaxDays <= 0 && cam.MaxDays == d.MaxDays {
		cam.MaxDays = 0
	}
	if a.Audio == "" && cam.Audio == d.Audio {
		cam.Audio = ""
	}
	if a.StallSeconds <= 0 && cam.StallSeconds == d.StallSeconds {
		cam.StallSeconds = 0
	}
	if a.Detect == nil && cam.Detect != nil && *cam.Detect == d.Detect {
		cam.Detect = nil
	}
	if a.DetectMethod == "" && cam.DetectMethod == d.DetectMethod {
		cam.DetectMethod = ""
	}
	if a.DetectSensitivity <= 0 && cam.DetectSensitivity == d.DetectSensitivity {
		cam.DetectSensitivity = 0
	}
	return cam
}

// LoadCameras reads cameras.json. Absent means "no camera registered".
//
// Only an invalid ID blocks boot, because it becomes a directory name on disk.
// Any other out-of-range field takes the default and becomes a Warning: one
// bad number on one camera must not leave all of them without recording. The
// file isn't rewritten here; the next save from the screen writes the camera
// already fixed, which is the one running.
func (c *Config) LoadCameras() ([]Camera, []Warning, error) {
	b, err := os.ReadFile(c.CamerasPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	var cams []Camera
	if err := json.Unmarshal(b, &cams); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", c.CamerasPath(), err)
	}
	var warnings []Warning
	for i := range cams {
		if err := ValidateCameraID(cams[i].ID); err != nil {
			return nil, nil, fmt.Errorf("camera #%d: %w", i, err)
		}
		var av []Warning
		cams[i], av = c.CheckCamera(cams[i].WithAreaIDs())
		warnings = append(warnings, av...)
	}
	return cams, warnings, nil
}

// SaveCameras writes cameras.json atomically (see WriteAtomic).
func (c *Config) SaveCameras(cams []Camera) error {
	b, err := json.MarshalIndent(cams, "", "  ")
	if err != nil {
		return err
	}
	// cameras.json must be readable by whoever administers the installation.
	return WriteAtomic(c.CamerasPath(), append(b, '\n'), 0o644)
}

// WriteAtomic writes the file atomically (a temp file in the same directory +
// rename), so a crash in the middle of the write never leaves a truncated
// file.
func WriteAtomic(path string, b []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	// CreateTemp creates it with 0600.
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// SessionSecret returns the cookie signing secret, generating it on the first
// call.
func (c *Config) SessionSecret() ([]byte, error) {
	path := c.SecretPath()
	b, err := os.ReadFile(path)
	if err == nil && len(b) >= 32 {
		return b, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	// 0600: whoever reads this file can forge sessions.
	if err := os.WriteFile(path, secret, 0o600); err != nil {
		return nil, err
	}
	return secret, nil
}

// ValidateCameraID rejects names that would become a dangerous path: the
// camera ID is used directly as a directory name inside storage. It also
// rejects the comma, which separates IDs in the API lists (`cams=a,b`).
//
// Its errors are in pt-BR because the API shows them on screen.
func ValidateCameraID(id string) error {
	if id == "" {
		return errors.New("id vazio")
	}
	if id != filepath.Base(id) || id == "." || id == ".." {
		return fmt.Errorf("id %q inválido: não pode conter caminho", id)
	}
	if strings.ContainsAny(id, `/\:`) {
		return fmt.Errorf("id %q inválido: contém separador de caminho", id)
	}
	if strings.Contains(id, ",") {
		return fmt.Errorf("id %q inválido: não pode conter vírgula", id)
	}
	return nil
}
