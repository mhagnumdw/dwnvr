package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// carrega grava o dwnvr.yaml e o cameras.json num diretório temporário e lê
// os dois como o dwnvr lê ao subir.
func carrega(t *testing.T, yaml, cameras string) (*Config, []Camera, error) {
	t.Helper()
	dir := t.TempDir()
	caminho := filepath.Join(dir, "dwnvr.yaml")
	if err := os.WriteFile(caminho, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	if cameras != "" {
		if err := os.WriteFile(filepath.Join(dir, "cameras.json"), []byte(cameras), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := Load(caminho)
	if err != nil {
		return nil, nil, err
	}
	cams, err := cfg.LoadCameras()
	return cfg, cams, err
}

// TestDeteccaoVemDesligada: quem só grava não liga nada sem pedir.
func TestDeteccaoVemDesligada(t *testing.T) {
	cfg, cams, err := carrega(t, "", `[{"id":"cam_a","stream":"cam_a"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Detector.URL != "" {
		t.Errorf("detector.url %q sem ninguém configurar", cfg.Detector.URL)
	}
	r := cfg.Resolve(cams[0])
	if r.Detect == nil || *r.Detect {
		t.Errorf("detect %v, esperado desligado", r.Detect)
	}
}

// TestDetectDesligadoNaCameraVenceODefault: a câmera desligada à mão não
// volta a ligar quando o default muda. É por isso que o campo é ponteiro.
func TestDetectDesligadoNaCameraVenceODefault(t *testing.T) {
	cfg, cams, err := carrega(t, "defaults:\n  detect: true\n",
		`[{"id":"cam_a","stream":"cam_a","detect":false},{"id":"cam_b","stream":"cam_b"}]`)
	if err != nil {
		t.Fatal(err)
	}
	if a := cfg.Resolve(cams[0]); *a.Detect {
		t.Error("cam_a desligada à mão ligou pelo default")
	}
	if b := cfg.Resolve(cams[1]); !*b.Detect {
		t.Error("cam_b sem o campo não herdou o default ligado")
	}
}

func TestDetectRecusaValorInvalido(t *testing.T) {
	casos := []struct{ nome, yaml, cameras, quer string }{
		{"mecanismo no default", "defaults:\n  detectMecanismo: magico\n", "", "detectMecanismo"},
		{"nível no default", "defaults:\n  detectSensibilidade: 9\n", "", "detectSensibilidade"},
		{"mecanismo na câmera", "", `[{"id":"cam_a","stream":"cam_a","detectMecanismo":"magico"}]`, "detectMecanismo"},
		{"nível na câmera", "", `[{"id":"cam_a","stream":"cam_a","detectSensibilidade":6}]`, "detectSensibilidade"},
	}
	for _, c := range casos {
		_, _, err := carrega(t, c.yaml, c.cameras)
		if err == nil || !strings.Contains(err.Error(), c.quer) {
			t.Errorf("%s: erro %v, esperado um que cite %s", c.nome, err, c.quer)
		}
	}
}

// TestExemploCarrega: o dwnvr.example.yaml da raiz é o que o usuário copia, e
// tem que subir sem edição.
func TestExemploCarrega(t *testing.T) {
	b, err := os.ReadFile("../../dwnvr.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := carrega(t, string(b), "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.Detect {
		t.Error("o exemplo liga a detecção")
	}
}
