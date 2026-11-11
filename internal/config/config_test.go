package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// load writes dwnvr.yaml and cameras.json to a temp directory and reads
// both as dwnvr reads them at startup. Warnings are left out; whoever wants to
// look at them uses loadWithWarnings.
func load(t *testing.T, yaml, cameras string) (*Config, []Camera, error) {
	t.Helper()
	cfg, cams, _, err := loadWithWarnings(t, yaml, cameras)
	return cfg, cams, err
}

func loadWithWarnings(t *testing.T, yaml, cameras string) (*Config, []Camera, []Warning, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "dwnvr.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if cameras != "" {
		if err := os.WriteFile(filepath.Join(dir, "cameras.json"), []byte(cameras), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, warnings, err := Load(path)
	if err != nil {
		return nil, nil, nil, err
	}
	cams, camWarnings, err := cfg.LoadCameras()
	return cfg, cams, append(warnings, camWarnings...), err
}

// TestDetectionComesOff: someone who only records doesn't turn anything on without asking.
func TestDetectionComesOff(t *testing.T) {
	cfg, cams, err := load(t, "", `[{"id":"cam_a","stream":"cam_a"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Detector.URL != "" {
		t.Errorf("detector.url %q without anyone configuring it", cfg.Detector.URL)
	}
	r := cfg.Resolve(cams[0])
	if r.Detect == nil || *r.Detect {
		t.Errorf("detect %v, expected off", r.Detect)
	}
}

// TestDetectOffOnCameraBeatsTheDefault: a camera turned off by hand doesn't
// turn back on when the default changes. That's why the field is a pointer.
func TestDetectOffOnCameraBeatsTheDefault(t *testing.T) {
	cfg, cams, err := load(t, "defaults:\n  detect: true\n",
		`[{"id":"cam_a","stream":"cam_a","detect":false},{"id":"cam_b","stream":"cam_b"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if a := cfg.Resolve(cams[0]); *a.Detect {
		t.Error("cam_a turned off by hand was turned on by the default")
	}
	if b := cfg.Resolve(cams[1]); !*b.Detect {
		t.Error("cam_b without the field didn't inherit the default on")
	}
}

// TestDropNewDefaultsUndoesResolve: a new camera, resolved, goes back to
// having only the id, the name and enabled. A field that goes into Resolve and
// is left out of DropNewDefaults fails here. maxDays goes in the yaml because
// its default is zero, and with zero the test wouldn't see it.
func TestDropNewDefaultsUndoesResolve(t *testing.T) {
	cfg, _, err := load(t, "defaults:\n  maxDays: 7\n", "")
	if err != nil {
		t.Fatal(err)
	}
	raw := Camera{ID: "cam_a", Name: "A", Enabled: true}
	resolved := cfg.Resolve(raw)
	if got := cfg.DropNewDefaults(resolved, nil); !reflect.DeepEqual(got, raw) {
		t.Errorf("new camera saved as %+v, want %+v", got, raw)
	}

	// A camera that already set each field, with the same value as the default,
	// stays as it was: the value was chosen by someone.
	if got := cfg.DropNewDefaults(resolved, &resolved); !reflect.DeepEqual(got, resolved) {
		t.Errorf("configured camera saved as %+v, want %+v", got, resolved)
	}
}

// byField indexes the warnings by field, for the test to check one by one.
func byField(warnings []Warning) map[string]Warning {
	m := make(map[string]Warning, len(warnings))
	for _, a := range warnings {
		m[a.Field] = a
	}
	return m
}

// TestBadCameraValueStartsWithDefault: an out-of-range number in
// cameras.json doesn't bring boot down. The field goes back to the default, and
// the warning says what was written, what's accepted and what took effect. The
// good camera isn't touched.
func TestBadCameraValueStartsWithDefault(t *testing.T) {
	cfg, cams, warnings, err := loadWithWarnings(t, "", `[
		{"id":"cam_ruim","quotaMB":5,"segmentSeconds":600,"maxDays":-1,"stallSeconds":99999,
		 "audio":"mp3","detectMecanismo":"magico","detectSensibilidade":9},
		{"id":"cam_boa","quotaMB":100,"segmentSeconds":300,"maxDays":7,"stallSeconds":30,
		 "audio":"flac","detectMecanismo":"periodico","detectSensibilidade":1}]`)
	if err != nil {
		t.Fatalf("boot failed: %v", err)
	}

	p := defaults().Defaults
	wanted := map[string]any{
		"quotaMB": p.QuotaMB, "segmentSeconds": p.SegmentSeconds, "maxDays": p.MaxDays,
		"stallSeconds": p.StallSeconds, "audio": p.Audio,
		"detectMecanismo": p.DetectMethod, "detectSensibilidade": p.DetectSensitivity,
	}
	got := byField(warnings)
	if len(warnings) != len(wanted) {
		t.Errorf("%d warnings, want %d: %+v", len(warnings), len(wanted), warnings)
	}
	for field, inEffect := range wanted {
		a, ok := got[field]
		switch {
		case !ok:
			t.Errorf("%s without a warning", field)
		case a.Camera != "cam_ruim" || a.Accepted == "" || a.InEffect != inEffect:
			t.Errorf("%s: warning %+v, want cam_ruim using %v", field, a, inEffect)
		}
	}

	r := cfg.Resolve(cams[0])
	if r.QuotaMB != p.QuotaMB || r.SegmentSeconds != p.SegmentSeconds || r.MaxDays != p.MaxDays ||
		r.StallSeconds != p.StallSeconds || r.Audio != p.Audio ||
		r.DetectMethod != p.DetectMethod || r.DetectSensitivity != p.DetectSensitivity {
		t.Errorf("cam_ruim resolved %+v, want the defaults", r)
	}
	if b := cams[1]; b.QuotaMB != 100 || b.SegmentSeconds != 300 || b.MaxDays != 7 ||
		b.StallSeconds != 30 || b.Audio != AudioFLAC ||
		b.DetectMethod != "periodico" || b.DetectSensitivity != 1 {
		t.Errorf("cam_boa was changed: %+v", b)
	}
}

// TestZeroOnCameraDoesNotWarn: zero and empty mean "use the default", not an error.
func TestZeroOnCameraDoesNotWarn(t *testing.T) {
	_, _, warnings, err := loadWithWarnings(t, "", `[{"id":"cam_a"}]`)
	if err != nil || len(warnings) > 0 {
		t.Errorf("error %v, warnings %+v", err, warnings)
	}
}

// TestInvalidIDStillFails: the ID becomes a directory name on disk, and
// there's no default to stand in for it. It's the only camera field that brings
// boot down.
func TestInvalidIDStillFails(t *testing.T) {
	if _, _, err := load(t, "", `[{"id":"../fora"}]`); err == nil {
		t.Error("ID with a path accepted at boot")
	}
}

// TestIDWithCommaFails: the comma separates the IDs in the API's `cams=`,
// so a camera with a comma in its ID could never be filtered on its own.
func TestIDWithCommaFails(t *testing.T) {
	if _, _, err := load(t, "", `[{"id":"cam_a,cam_b"}]`); err == nil {
		t.Error("ID with a comma accepted at boot")
	}
	if err := ValidateCameraID("cam,a"); err == nil {
		t.Error("ValidateCameraID accepted a comma")
	}
}

// TestBadYamlValueStartsWithDefault: the same holds for dwnvr.yaml, but
// there zero is invalid too, because the default is the written value itself.
func TestBadYamlValueStartsWithDefault(t *testing.T) {
	cfg, _, warnings, err := loadWithWarnings(t, `
storage:
  minFreeMB: -1
defaults:
  segmentSeconds: 0
  quotaMB: 5
  maxDays: -3
  stallSeconds: 0
  audio: mp3
  detectMecanismo: magico
  detectSensibilidade: 9
`, "")
	if err != nil {
		t.Fatalf("boot failed: %v", err)
	}
	p := defaults()
	if cfg.Storage.MinFreeMB != p.Storage.MinFreeMB || cfg.Defaults != p.Defaults {
		t.Errorf("config %+v %+v, want the code defaults", cfg.Storage, cfg.Defaults)
	}
	got := byField(warnings)
	for _, field := range []string{"storage.minFreeMB", "defaults.segmentSeconds",
		"defaults.quotaMB", "defaults.maxDays", "defaults.stallSeconds", "defaults.audio",
		"defaults.detectMecanismo", "defaults.detectSensibilidade"} {
		if a, ok := got[field]; !ok || a.Camera != "" {
			t.Errorf("%s: warning %+v", field, a)
		}
	}
	if len(warnings) != 8 {
		t.Errorf("%d warnings, want 8: %+v", len(warnings), warnings)
	}
}

// TestNoFileStartsWithDefaults: first use doesn't require writing anything.
// Without dwnvr.yaml and without cameras.json dwnvr starts with the defaults,
// with no warning.
func TestNoFileStartsWithDefaults(t *testing.T) {
	cfg, warnings, err := Load(filepath.Join(t.TempDir(), "dwnvr.yaml"))
	if err != nil || len(warnings) > 0 {
		t.Fatalf("error %v, warnings %+v", err, warnings)
	}
	if cfg.Defaults != defaults().Defaults {
		t.Errorf("defaults %+v, want the code's", cfg.Defaults)
	}
	cams, warnings, err := cfg.LoadCameras()
	if err != nil || len(cams) > 0 || len(warnings) > 0 {
		t.Errorf("missing cameras.json: cameras %v, warnings %+v, error %v", cams, warnings, err)
	}
}

// TestUnreadableCamerasFails: a cameras.json that isn't JSON has no field to
// go back to the default. Starting with no camera at all would hide the
// problem.
func TestUnreadableCamerasFails(t *testing.T) {
	if _, _, err := load(t, "", `[{"id":"cam_a",`); err == nil {
		t.Error("broken cameras.json accepted at boot")
	}
}

// TestWarningTextSaysWhatToUse: it's the sentence the API returns to the screen when it rejects.
func TestWarningTextSaysWhatToUse(t *testing.T) {
	cfg := defaults()
	_, warnings := cfg.CheckCamera(Camera{ID: "cam_a", QuotaMB: 5})
	if len(warnings) != 1 {
		t.Fatalf("warnings %+v, want 1", warnings)
	}
	if got, wanted := warnings[0].Text(), "quotaMB 5 fora do aceito: use 100 MB ou mais"; got != wanted {
		t.Errorf("%q, want %q", got, wanted)
	}
}

// TestEmptyStorageRootFails: without knowing where to record, there's no safe default.
func TestEmptyStorageRootFails(t *testing.T) {
	if _, _, err := load(t, "storage:\n  root: \"\"\n", ""); err == nil {
		t.Error("empty storage.root accepted")
	}
}

func TestRangeDescribesTheRecommendation(t *testing.T) {
	cases := map[Range]string{
		SegmentSecondsRange: "de 10 a 300 s",
		QuotaMBRange:        "100 MB ou mais",
		sensitivityRange:    "de 1 a 5",
	}
	for f, wanted := range cases {
		if got := f.String(); got != wanted {
			t.Errorf("%+v: %q, want %q", f, got, wanted)
		}
	}
}

// TestExampleLoads: the dwnvr.example.yaml at the root is what the user
// copies, and it must start without editing.
func TestExampleLoads(t *testing.T) {
	b, err := os.ReadFile("../../dwnvr.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := load(t, string(b), "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.Detect {
		t.Error("the example turns detection on")
	}
}

// TestLoginLimitComesOn: without the keys in the yaml, login already comes
// out limited and the compose proxy is trusted. Zero turns it off, and an empty
// list trusts nobody: both apply as written, with no warning.
func TestLoginLimitComesOn(t *testing.T) {
	cfg, _, err := load(t, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.LoginAttemptsPerMinute != DefaultLoginAttemptsPerMinute ||
		!slices.Equal(cfg.Server.TrustedProxies, DefaultTrustedProxies) {
		t.Errorf("without the keys: %+v", cfg.Server)
	}

	cfg, warnings := loadWarningsOf(t, `
server:
  loginAttemptsPerMinute: 0
  trustedProxies: []
`)
	if len(warnings) > 0 {
		t.Fatalf("warnings %+v", warnings)
	}
	if cfg.Server.LoginAttemptsPerMinute != 0 || len(cfg.Server.TrustedProxies) != 0 {
		t.Errorf("off: %+v", cfg.Server)
	}
}

// TestBadLoginLimitStartsWithDefault: out of range, or a proxy that isn't
// an address, goes back to the default with a warning. One bad proxy drops the
// whole list: trusting only the part written right would change the address of
// whoever the owner meant to cover.
func TestBadLoginLimitStartsWithDefault(t *testing.T) {
	cfg, warnings := loadWarningsOf(t, `
server:
  loginAttemptsPerMinute: -1
  trustedProxies: [10.0.0.0/8, servidor-do-proxy]
`)
	if cfg.Server.LoginAttemptsPerMinute != DefaultLoginAttemptsPerMinute ||
		!slices.Equal(cfg.Server.TrustedProxies, DefaultTrustedProxies) {
		t.Errorf("config %+v, want the defaults", cfg.Server)
	}
	got := byField(warnings)
	for _, field := range []string{"server.loginAttemptsPerMinute", "server.trustedProxies"} {
		if _, ok := got[field]; !ok {
			t.Errorf("no warning for %s: %+v", field, warnings)
		}
	}

	if _, warnings := loadWarningsOf(t, "server:\n  loginAttemptsPerMinute: 13\n"); len(warnings) != 1 {
		t.Errorf("13 per minute: warnings %+v, want 1", warnings)
	}
}

// TestPrefixesAcceptsBareAddress: an IP without a mask is a network of its own.
func TestPrefixesAcceptsBareAddress(t *testing.T) {
	ps, err := Prefixes([]string{"192.168.0.10", "fd00::1", "10.1.2.3/8"})
	if err != nil {
		t.Fatal(err)
	}
	wanted := []string{"192.168.0.10/32", "fd00::1/128", "10.0.0.0/8"}
	for i, p := range ps {
		if p.String() != wanted[i] {
			t.Errorf("%d: %s, want %s", i, p, wanted[i])
		}
	}
}

// loadWarningsOf reads only dwnvr.yaml and returns the config and the
// warnings; a read error fails the test.
func loadWarningsOf(t *testing.T, yaml string) (*Config, []Warning) {
	t.Helper()
	cfg, _, warnings, err := loadWithWarnings(t, yaml, "")
	if err != nil {
		t.Fatal(err)
	}
	return cfg, warnings
}

// TestPublicURL: the address comes out without the trailing "/", and with a
// path it works for dwnvr behind a proxy. What would break the link built on
// top of it becomes a warning and counts as not written.
func TestPublicURL(t *testing.T) {
	for written, want := range map[string]string{
		"":                              "",
		"https://dwnvr.exemplo.ts.net":  "https://dwnvr.exemplo.ts.net",
		"https://dwnvr.exemplo.ts.net/": "https://dwnvr.exemplo.ts.net",
		"http://192.168.0.10:8080":      "http://192.168.0.10:8080",
		" https://exemplo.com/dwnvr/ ":  "https://exemplo.com/dwnvr",
	} {
		cfg, warnings := loadWarningsOf(t, fmt.Sprintf("server:\n  publicUrl: %q\n", written))
		if len(warnings) > 0 || cfg.Server.PublicURL != want {
			t.Errorf("%q: %q with warnings %+v, expected %q", written, cfg.Server.PublicURL, warnings, want)
		}
	}
	password := "senha"
	withPassword := (&url.URL{Scheme: "https", User: url.UserPassword("fulano", password), Host: "exemplo.com"}).String()
	for _, bad := range []string{
		"dwnvr.exemplo.ts.net", "ftp://exemplo.com", "https://", "https://:8080", "https://exemplo.com/?a=1",
		"https://exemplo.com/#rec", withPassword, "https://exemplo.com/?",
	} {
		cfg, warnings := loadWarningsOf(t, fmt.Sprintf("server:\n  publicUrl: %q\n", bad))
		if _, ok := byField(warnings)["server.publicUrl"]; !ok || cfg.Server.PublicURL != "" {
			t.Errorf("%q: %q with warnings %+v, expected empty with a warning", bad, cfg.Server.PublicURL, warnings)
		}
	}
}
