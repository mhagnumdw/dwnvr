// Package config carrega a configuração do dwnvr.
//
// A configuração é deliberadamente dividida em dois arquivos:
//
//   - dwnvr.yaml   infraestrutura, editado à mão, NUNCA reescrito pela aplicação
//   - cameras.json lista de câmeras, gerenciada pela API de cadastro
//
// A separação existe porque reescrever um YAML apaga os comentários de quem o
// escreveu. Como a tela de cadastro precisa gravar câmeras, misturar as duas
// coisas destruiria as anotações do usuário a cada clique.
package config

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Modos de áudio suportados por câmera.
const (
	AudioNone = "none"
	AudioFLAC = "flac"
	AudioAAC  = "aac"
)

// DefaultStallSeconds é quanto tempo sem receber um único byte basta para
// considerar o stream morto.
//
// Quinze segundos é folgado: mesmo uma câmera de 1 fps manda bytes todo
// segundo. O número não precisa ser justo — precisa ser MUITO menor que as 3h38
// que uma câmera passou parada em silêncio antes disto existir.
const DefaultStallSeconds = 15

type Config struct {
	Server   Server   `yaml:"server"`
	Go2RTC   Go2RTC   `yaml:"go2rtc"`
	Storage  Storage  `yaml:"storage"`
	Defaults Defaults `yaml:"defaults"`

	// dir é o diretório do dwnvr.yaml; cameras.json fica ao lado dele.
	dir string `yaml:"-"`
}

type Server struct {
	Listen string `yaml:"listen"`

	// Deixar usuário e senha vazios desliga a autenticação. Isso é aceitável
	// numa rede confiável, e é o padrão para não travar o primeiro uso — mas o
	// dwnvr avisa no log, porque quem acessa a interface enxerga as gravações
	// de todas as câmeras.
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// AuthEnabled diz se alguma credencial foi configurada.
func (s Server) AuthEnabled() bool { return s.Username != "" || s.Password != "" }

// SecretPath é onde fica o segredo que assina os cookies de sessão. Ele é
// gerado no primeiro uso; guardá-lo em arquivo evita derrubar todas as sessões
// a cada reinício do processo.
func (c *Config) SecretPath() string { return filepath.Join(c.dir, ".session-secret") }

type Go2RTC struct {
	URL      string `yaml:"url"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type Storage struct {
	// Root é onde as gravações são escritas.
	Root string `yaml:"root"`

	// MinFreeMB é a trava global de disco livre. Quando o disco cai abaixo
	// disso, o dwnvr evicta as gravações mais antigas de todas as câmeras,
	// independentemente das cotas individuais. Existe porque a soma das cotas
	// erra fácil, e encher o disco é pior que perder gravação antiga.
	MinFreeMB int64 `yaml:"minFreeMB"`

	// RequireSeparateDisk recusa gravar se Root estiver no mesmo sistema de
	// arquivos da raiz. Protege o caso real de o disco externo desmontar e a
	// gravação passar a encher o cartão SD do sistema sem ninguém perceber.
	//
	// Deixe desligado em instalações de disco único, onde gravar no rootfs é
	// justamente a intenção.
	RequireSeparateDisk bool `yaml:"requireSeparateDisk"`
}

// Defaults são aplicados a qualquer câmera que não sobrescreva o campo.
type Defaults struct {
	SegmentSeconds int    `yaml:"segmentSeconds"`
	QuotaMB        int64  `yaml:"quotaMB"`
	MaxDays        int    `yaml:"maxDays"`
	Audio          string `yaml:"audio"`
	StallSeconds   int    `yaml:"stallSeconds"`
}

// Camera é uma câmera registrada. O ID é o nome do stream no go2rtc: o dwnvr
// não guarda URL nem credencial de câmera, porque configurar o go2rtc não é
// responsabilidade dele.
type Camera struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`

	// Zero em qualquer campo abaixo significa "usar o default".
	SegmentSeconds int    `json:"segmentSeconds,omitempty"`
	QuotaMB        int64  `json:"quotaMB,omitempty"`
	MaxDays        int    `json:"maxDays,omitempty"`
	Audio          string `json:"audio,omitempty"`

	// StallSeconds é por câmera porque a tolerância depende do enlace: uma
	// câmera num Wi-Fi ruim pode precisar de mais folga que uma no cabo, e
	// baixar o limiar demais troca perda silenciosa por reconexão em excesso.
	StallSeconds int `json:"stallSeconds,omitempty"`
}

func defaults() Config {
	return Config{
		Server: Server{Listen: ":8080"},
		Go2RTC: Go2RTC{URL: "http://localhost:1984"},
		Storage: Storage{
			Root:      "/mnt/storage/dwnvr",
			MinFreeMB: 2048,
		},
		Defaults: Defaults{
			SegmentSeconds: 60,
			QuotaMB:        10240,
			Audio:          AudioNone,
			StallSeconds:   DefaultStallSeconds,
		},
	}
}

// Load lê o dwnvr.yaml. Um arquivo ausente não é erro: o dwnvr sobe com os
// padrões, o que torna o primeiro uso trivial.
func Load(path string) (*Config, error) {
	cfg := defaults()
	cfg.dir = filepath.Dir(path)

	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return &cfg, cfg.validate()
	case err != nil:
		return nil, err
	}

	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &cfg, cfg.validate()
}

func (c *Config) validate() error {
	if c.Storage.Root == "" {
		return errors.New("storage.root não pode ser vazio")
	}
	if c.Defaults.SegmentSeconds <= 0 {
		return errors.New("defaults.segmentSeconds precisa ser positivo")
	}
	// Zero não desliga a vigilância: desligá-la é reintroduzir a perda
	// silenciosa de gravação. Quem precisa de mais folga aumenta o número.
	if c.Defaults.StallSeconds <= 0 {
		return errors.New("defaults.stallSeconds precisa ser positivo")
	}
	if err := ValidAudio(c.Defaults.Audio); err != nil {
		return fmt.Errorf("defaults.audio: %w", err)
	}
	return nil
}

func ValidAudio(mode string) error {
	switch mode {
	case AudioNone, AudioFLAC, AudioAAC:
		return nil
	default:
		return fmt.Errorf("modo %q inválido (use none, flac ou aac)", mode)
	}
}

// CamerasPath é o caminho do cameras.json, ao lado do dwnvr.yaml.
func (c *Config) CamerasPath() string { return filepath.Join(c.dir, "cameras.json") }

// Resolve devolve a câmera com os defaults já aplicados, para que o resto do
// código nunca precise perguntar "esse zero é intencional?".
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
	if cam.Name == "" {
		cam.Name = cam.ID
	}
	return cam
}

// LoadCameras lê o cameras.json. Ausente significa "nenhuma câmera cadastrada".
func (c *Config) LoadCameras() ([]Camera, error) {
	b, err := os.ReadFile(c.CamerasPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var cams []Camera
	if err := json.Unmarshal(b, &cams); err != nil {
		return nil, fmt.Errorf("%s: %w", c.CamerasPath(), err)
	}
	for i, cam := range cams {
		if err := ValidateCameraID(cam.ID); err != nil {
			return nil, fmt.Errorf("câmera #%d: %w", i, err)
		}
		if cam.Audio != "" {
			if err := ValidAudio(cam.Audio); err != nil {
				return nil, fmt.Errorf("câmera %q: %w", cam.ID, err)
			}
		}
	}
	return cams, nil
}

// SaveCameras grava o cameras.json de forma atômica (arquivo temporário no
// mesmo diretório + rename), para que uma queda no meio da escrita nunca deixe
// um cadastro truncado.
func (c *Config) SaveCameras(cams []Camera) error {
	b, err := json.MarshalIndent(cams, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	path := c.CamerasPath()
	tmp, err := os.CreateTemp(filepath.Dir(path), ".cameras-*.json")
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
	// CreateTemp cria com 0600; o cameras.json precisa ser legível por quem
	// administra a instalação.
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// SessionSecret devolve o segredo de assinatura dos cookies, gerando-o na
// primeira chamada.
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
	// 0600: quem lê este arquivo consegue forjar sessões.
	if err := os.WriteFile(path, secret, 0o600); err != nil {
		return nil, err
	}
	return secret, nil
}

// ValidateCameraID recusa nomes que virariam caminho perigoso: o ID da câmera
// é usado diretamente como nome de diretório dentro do storage.
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
	return nil
}
