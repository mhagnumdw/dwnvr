package config

import (
	"os"
	"path/filepath"
	"testing"
)

// carrega grava o dwnvr.yaml e o cameras.json num diretório temporário e lê
// os dois como o dwnvr lê ao subir. Os avisos ficam de fora; quem quer
// olhá-los usa carregaComAvisos.
func carrega(t *testing.T, yaml, cameras string) (*Config, []Camera, error) {
	t.Helper()
	cfg, cams, _, err := carregaComAvisos(t, yaml, cameras)
	return cfg, cams, err
}

func carregaComAvisos(t *testing.T, yaml, cameras string) (*Config, []Camera, []Aviso, error) {
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
	cfg, avisos, err := Load(caminho)
	if err != nil {
		return nil, nil, nil, err
	}
	cams, avisosCam, err := cfg.LoadCameras()
	return cfg, cams, append(avisos, avisosCam...), err
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

// porCampo indexa os avisos pelo campo, para o teste conferir um a um.
func porCampo(avisos []Aviso) map[string]Aviso {
	m := make(map[string]Aviso, len(avisos))
	for _, a := range avisos {
		m[a.Campo] = a
	}
	return m
}

// TestValorRuimNaCameraSobeComOPadrao: um número fora da faixa no cameras.json
// não derruba o boot. O campo volta ao padrão, e o aviso diz o que estava
// escrito, o que é aceito e o que ficou valendo. A câmera boa não é tocada.
func TestValorRuimNaCameraSobeComOPadrao(t *testing.T) {
	cfg, cams, avisos, err := carregaComAvisos(t, "", `[
		{"id":"cam_ruim","quotaMB":5,"segmentSeconds":600,"maxDays":-1,"stallSeconds":99999,
		 "audio":"mp3","detectMecanismo":"magico","detectSensibilidade":9},
		{"id":"cam_boa","quotaMB":100,"segmentSeconds":300,"maxDays":7,"stallSeconds":30,
		 "audio":"flac","detectMecanismo":"periodico","detectSensibilidade":1}]`)
	if err != nil {
		t.Fatalf("o boot recusou: %v", err)
	}

	p := defaults().Defaults
	quer := map[string]any{
		"quotaMB": p.QuotaMB, "segmentSeconds": p.SegmentSeconds, "maxDays": p.MaxDays,
		"stallSeconds": p.StallSeconds, "audio": p.Audio,
		"detectMecanismo": p.DetectMecanismo, "detectSensibilidade": p.DetectSensibilidade,
	}
	got := porCampo(avisos)
	if len(avisos) != len(quer) {
		t.Errorf("%d avisos, esperava %d: %+v", len(avisos), len(quer), avisos)
	}
	for campo, usando := range quer {
		a, ok := got[campo]
		switch {
		case !ok:
			t.Errorf("%s sem aviso", campo)
		case a.Camera != "cam_ruim" || a.Aceito == "" || a.Usando != usando:
			t.Errorf("%s: aviso %+v, esperava cam_ruim usando %v", campo, a, usando)
		}
	}

	r := cfg.Resolve(cams[0])
	if r.QuotaMB != p.QuotaMB || r.SegmentSeconds != p.SegmentSeconds || r.MaxDays != p.MaxDays ||
		r.StallSeconds != p.StallSeconds || r.Audio != p.Audio ||
		r.DetectMecanismo != p.DetectMecanismo || r.DetectSensibilidade != p.DetectSensibilidade {
		t.Errorf("cam_ruim resolvida %+v, esperava os padrões", r)
	}
	if b := cams[1]; b.QuotaMB != 100 || b.SegmentSeconds != 300 || b.MaxDays != 7 ||
		b.StallSeconds != 30 || b.Audio != AudioFLAC ||
		b.DetectMecanismo != "periodico" || b.DetectSensibilidade != 1 {
		t.Errorf("cam_boa foi alterada: %+v", b)
	}
}

// TestZeroNaCameraNaoAvisa: zero e vazio são "usar o padrão", não erro.
func TestZeroNaCameraNaoAvisa(t *testing.T) {
	_, _, avisos, err := carregaComAvisos(t, "", `[{"id":"cam_a"}]`)
	if err != nil || len(avisos) > 0 {
		t.Errorf("erro %v, avisos %+v", err, avisos)
	}
}

// TestIDInvalidoAindaRecusa: o ID vira nome de diretório no disco, e não
// existe padrão que o substitua. É o único campo da câmera que derruba o boot.
func TestIDInvalidoAindaRecusa(t *testing.T) {
	if _, _, err := carrega(t, "", `[{"id":"../fora"}]`); err == nil {
		t.Error("ID com caminho aceito no boot")
	}
}

// TestValorRuimNoYamlSobeComOPadrao: o mesmo vale para o dwnvr.yaml, mas lá o
// zero também é inválido, porque o padrão é o próprio valor escrito.
func TestValorRuimNoYamlSobeComOPadrao(t *testing.T) {
	cfg, _, avisos, err := carregaComAvisos(t, `
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
		t.Fatalf("o boot recusou: %v", err)
	}
	p := defaults()
	if cfg.Storage.MinFreeMB != p.Storage.MinFreeMB || cfg.Defaults != p.Defaults {
		t.Errorf("config %+v %+v, esperava os padrões do código", cfg.Storage, cfg.Defaults)
	}
	got := porCampo(avisos)
	for _, campo := range []string{"storage.minFreeMB", "defaults.segmentSeconds",
		"defaults.quotaMB", "defaults.maxDays", "defaults.stallSeconds", "defaults.audio",
		"defaults.detectMecanismo", "defaults.detectSensibilidade"} {
		if a, ok := got[campo]; !ok || a.Camera != "" {
			t.Errorf("%s: aviso %+v", campo, a)
		}
	}
	if len(avisos) != 8 {
		t.Errorf("%d avisos, esperava 8: %+v", len(avisos), avisos)
	}
}

// TestSemArquivoSobeComOsPadroes: o primeiro uso não exige escrever nada. Sem
// dwnvr.yaml e sem cameras.json o dwnvr sobe com os padrões, sem aviso.
func TestSemArquivoSobeComOsPadroes(t *testing.T) {
	cfg, avisos, err := Load(filepath.Join(t.TempDir(), "dwnvr.yaml"))
	if err != nil || len(avisos) > 0 {
		t.Fatalf("erro %v, avisos %+v", err, avisos)
	}
	if cfg.Defaults != defaults().Defaults {
		t.Errorf("defaults %+v, esperava os do código", cfg.Defaults)
	}
	cams, avisos, err := cfg.LoadCameras()
	if err != nil || len(cams) > 0 || len(avisos) > 0 {
		t.Errorf("cameras.json ausente: câmeras %v, avisos %+v, erro %v", cams, avisos, err)
	}
}

// TestCamerasIlegivelRecusa: um cameras.json que não é JSON não tem campo para
// voltar ao padrão. Subir sem câmera nenhuma esconderia o problema.
func TestCamerasIlegivelRecusa(t *testing.T) {
	if _, _, err := carrega(t, "", `[{"id":"cam_a",`); err == nil {
		t.Error("cameras.json quebrado aceito no boot")
	}
}

// TestAvisoTextoDizOQueUsar: é a frase que a API devolve à tela quando recusa.
func TestAvisoTextoDizOQueUsar(t *testing.T) {
	cfg := defaults()
	_, avisos := cfg.ConfereCamera(Camera{ID: "cam_a", QuotaMB: 5})
	if len(avisos) != 1 {
		t.Fatalf("avisos %+v, esperava 1", avisos)
	}
	if got, quer := avisos[0].Texto(), "quotaMB 5 fora do aceito: use 100 MB ou mais"; got != quer {
		t.Errorf("%q, esperava %q", got, quer)
	}
}

// TestStorageRootVazioRecusa: sem saber onde gravar, não há padrão seguro.
func TestStorageRootVazioRecusa(t *testing.T) {
	if _, _, err := carrega(t, "storage:\n  root: \"\"\n", ""); err == nil {
		t.Error("storage.root vazio aceito")
	}
}

func TestFaixaDescreveARecomendacao(t *testing.T) {
	casos := map[Faixa]string{
		FaixaSegmentSeconds: "de 10 a 300 s",
		FaixaQuotaMB:        "100 MB ou mais",
		faixaSensibilidade:  "de 1 a 5",
	}
	for f, quer := range casos {
		if got := f.String(); got != quer {
			t.Errorf("%+v: %q, esperava %q", f, got, quer)
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
