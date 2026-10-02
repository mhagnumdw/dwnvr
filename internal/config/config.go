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
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/mhagnumdw/dwnvr/internal/detect"
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
// segundo. O número não precisa ser justo - precisa ser MUITO menor que as 3h38
// que uma câmera passou parada em silêncio antes disto existir.
const DefaultStallSeconds = 15

type Config struct {
	Server   Server   `yaml:"server"`
	Go2RTC   Go2RTC   `yaml:"go2rtc"`
	Storage  Storage  `yaml:"storage"`
	Defaults Defaults `yaml:"defaults"`
	Detector Detector `yaml:"detector"`

	// dir é o diretório do dwnvr.yaml; cameras.json fica ao lado dele.
	dir string `yaml:"-"`
}

type Server struct {
	Listen string `yaml:"listen"`

	// Deixar usuário e senha vazios desliga a autenticação. Isso é aceitável
	// numa rede confiável, e é o padrão para não travar o primeiro uso - mas o
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

// Detector é o detector de objetos: o container `dwnvr-detect`, que diz se o
// movimento era pessoa, veículo ou animal. É opcional e fica à parte porque
// decodificar vídeo e rodar um modelo exigem código nativo, e o dwnvr é Go
// puro.
type Detector struct {
	// URL do sidecar, ex.: http://dwnvr-detect:8480. Vazio é "sem detector":
	// as câmeras com `detect` ligado continuam marcando só movimento, e o
	// dwnvr não guarda um byte de vídeo a mais por causa disso.
	URL string `yaml:"url"`
}

type Storage struct {
	// Root é onde as gravações são escritas.
	Root string `yaml:"root"`

	// MinFreeMB é a trava global de disco livre. Quando o disco cai abaixo
	// disso, o dwnvr evicta as gravações mais antigas de todas as câmeras,
	// independentemente das cotas individuais. Existe porque a soma das cotas
	// erra fácil, e encher o disco é pior que perder gravação antiga.
	MinFreeMB int64 `yaml:"minFreeMB"`
}

// Defaults são aplicados a qualquer câmera que não sobrescreva o campo.
type Defaults struct {
	SegmentSeconds int    `yaml:"segmentSeconds"`
	QuotaMB        int64  `yaml:"quotaMB"`
	MaxDays        int    `yaml:"maxDays"`
	Audio          string `yaml:"audio"`
	StallSeconds   int    `yaml:"stallSeconds"`

	// Detect liga a marcação de movimento. Vem desligada: quem não pediu a
	// faixa de calor na timeline - nem o detector de objetos que acorda por
	// ela - não deve começar a pagar por isso.
	Detect bool `yaml:"detect"`

	DetectMecanismo     string `yaml:"detectMecanismo"`
	DetectSensibilidade int    `yaml:"detectSensibilidade"`
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

	// Detect liga a marcação de movimento nesta câmera.
	//
	// Ele é ponteiro, e não bool, porque aqui o zero PRECISA se distinguir do
	// ausente: com um bool, "desligada" e "não configurada" seriam a mesma
	// coisa, e uma câmera desligada à mão voltaria a ligar sozinha assim que o
	// default mudasse.
	Detect *bool `json:"detect,omitempty"`

	// DetectMecanismo escolhe quem decide disparar: `kleinberg-p`, o melhor
	// dos 24 candidatos medidos, ou `periodico`, que dispara de X em X
	// segundos e é a régua honesta a bater.
	//
	// É por câmera porque as câmeras não são iguais: numa delas, medida, o
	// campeão pegou 10,1% das chegadas e o segundo colocado 23,4%. Trocar o
	// mecanismo de uma câmera difícil é mais barato que subir o nível de
	// todas.
	DetectMecanismo string `json:"detectMecanismo,omitempty"`

	// DetectSensibilidade é o nível de 1 a 5. Ele é o CUSTO: cada nível dobra
	// o número de detecções por hora, e cada detecção é o gasto de CPU inteiro
	// da feature.
	DetectSensibilidade int `json:"detectSensibilidade,omitempty"`
}

func defaults() Config {
	return Config{
		Server: Server{Listen: ":8080"},
		Go2RTC: Go2RTC{URL: "http://localhost:1984"},
		Storage: Storage{
			Root:      "/mnt/storage/dwnvr/recordings",
			MinFreeMB: 2048,
		},
		Defaults: Defaults{
			SegmentSeconds:      30,
			QuotaMB:             10240,
			Audio:               AudioNone,
			StallSeconds:        DefaultStallSeconds,
			Detect:              false,
			DetectMecanismo:     detect.MecanismoPadrao,
			DetectSensibilidade: detect.NivelPadrao,
		},
	}
}

// Load lê o dwnvr.yaml. Um arquivo ausente não é erro: o dwnvr sobe com os
// padrões, o que torna o primeiro uso trivial.
//
// Só é erro o que impede o dwnvr de funcionar: o arquivo ilegível e o
// storage.root vazio. Um valor fora da faixa volta ao padrão do código e vira
// Aviso, porque gravar com o padrão é melhor que não gravar nada.
func Load(path string) (*Config, []Aviso, error) {
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
		return nil, nil, errors.New("storage.root não pode ser vazio")
	}
	return &cfg, cfg.confere(), nil
}

// Aviso é um valor fora da faixa encontrado num arquivo de configuração. Ele
// não impede o dwnvr de subir: o campo passa a valer o padrão, e o aviso diz o
// que estava escrito, o que é aceito e o que ficou valendo no lugar.
type Aviso struct {
	Camera string // vazio quando o valor veio do dwnvr.yaml
	Campo  string // como está escrito no arquivo: "quotaMB", "defaults.quotaMB"
	Valor  any
	Aceito string // a recomendação: "de 10 a 300 s"
	Usando any    // o que passou a valer no lugar
}

// Texto é a frase para quem pode corrigir na hora; a API recusa com ela.
func (a Aviso) Texto() string {
	return fmt.Sprintf("%s %v fora do aceito: use %s", a.Campo, a.Valor, a.Aceito)
}

// confere troca pelo padrão do código todo valor de defaults e de storage fora
// da faixa. Aqui o zero não é "usar o padrão" como na câmera: o padrão é o
// próprio valor, então um zero escrito é tão inválido quanto um negativo.
func (c *Config) confere() []Aviso {
	p := defaults()
	var av []Aviso
	confereFaixa(&av, Aviso{Campo: "storage.minFreeMB"}, &c.Storage.MinFreeMB,
		FaixaMinFreeMB, p.Storage.MinFreeMB, p.Storage.MinFreeMB)

	d, pd := &c.Defaults, p.Defaults
	confereFaixa(&av, Aviso{Campo: "defaults.segmentSeconds"}, &d.SegmentSeconds,
		FaixaSegmentSeconds, pd.SegmentSeconds, pd.SegmentSeconds)
	confereFaixa(&av, Aviso{Campo: "defaults.quotaMB"}, &d.QuotaMB, FaixaQuotaMB, pd.QuotaMB, pd.QuotaMB)
	confereFaixa(&av, Aviso{Campo: "defaults.maxDays"}, &d.MaxDays, FaixaMaxDays, pd.MaxDays, pd.MaxDays)
	confereFaixa(&av, Aviso{Campo: "defaults.stallSeconds"}, &d.StallSeconds,
		FaixaStallSeconds, pd.StallSeconds, pd.StallSeconds)
	if !audioValido(d.Audio) {
		av = append(av, Aviso{Campo: "defaults.audio", Valor: d.Audio,
			Aceito: audiosAceitos, Usando: pd.Audio})
		d.Audio = pd.Audio
	}
	if _, ok := detect.Mecanismos[d.DetectMecanismo]; !ok {
		av = append(av, Aviso{Campo: "defaults.detectMecanismo", Valor: d.DetectMecanismo,
			Aceito: mecanismosAceitos(), Usando: pd.DetectMecanismo})
		d.DetectMecanismo = pd.DetectMecanismo
	}
	confereFaixa(&av, Aviso{Campo: "defaults.detectSensibilidade"}, &d.DetectSensibilidade,
		faixaSensibilidade, pd.DetectSensibilidade, pd.DetectSensibilidade)
	return av
}

// ConfereCamera devolve a câmera com cada campo fora da faixa zerado, que é o
// "usar o padrão" do Resolve, e um Aviso por campo trocado. É a régua das duas
// portas: a API recusa se houver aviso, o boot sobe com a câmera corrigida.
//
// O ID não passa por aqui: ele vira nome de diretório, e não há padrão que o
// substitua. Ver ValidateCameraID.
func (c *Config) ConfereCamera(cam Camera) (Camera, []Aviso) {
	var av []Aviso
	base := func(campo string) Aviso { return Aviso{Camera: cam.ID, Campo: campo} }

	// Zero em qualquer campo é "usar o padrão", e por isso nunca é inválido.
	if cam.QuotaMB != 0 {
		confereFaixa(&av, base("quotaMB"), &cam.QuotaMB, FaixaQuotaMB, 0, c.Defaults.QuotaMB)
	}
	if cam.SegmentSeconds != 0 {
		confereFaixa(&av, base("segmentSeconds"), &cam.SegmentSeconds,
			FaixaSegmentSeconds, 0, c.Defaults.SegmentSeconds)
	}
	if cam.MaxDays != 0 {
		confereFaixa(&av, base("maxDays"), &cam.MaxDays, FaixaMaxDays, 0, c.Defaults.MaxDays)
	}
	if cam.StallSeconds != 0 {
		confereFaixa(&av, base("stallSeconds"), &cam.StallSeconds,
			FaixaStallSeconds, 0, c.Defaults.StallSeconds)
	}
	if cam.Audio != "" && !audioValido(cam.Audio) {
		a := base("audio")
		a.Valor, a.Aceito, a.Usando = cam.Audio, audiosAceitos, c.Defaults.Audio
		av = append(av, a)
		cam.Audio = ""
	}
	if _, ok := detect.Mecanismos[cam.DetectMecanismo]; cam.DetectMecanismo != "" && !ok {
		a := base("detectMecanismo")
		a.Valor, a.Aceito, a.Usando = cam.DetectMecanismo, mecanismosAceitos(), c.Defaults.DetectMecanismo
		av = append(av, a)
		cam.DetectMecanismo = ""
	}
	if cam.DetectSensibilidade != 0 {
		confereFaixa(&av, base("detectSensibilidade"), &cam.DetectSensibilidade,
			faixaSensibilidade, 0, c.Defaults.DetectSensibilidade)
	}
	return cam, av
}

// confereFaixa confere um campo numérico. Fora da faixa, o valor é trocado por
// `troca` e vira um Aviso que conta o que passou a valer, `usando`. Na câmera
// os dois diferem (a troca é o zero, e quem vale é o default); no dwnvr.yaml
// são o mesmo número.
func confereFaixa[T int | int64](av *[]Aviso, a Aviso, v *T, f Faixa, troca, usando T) {
	if f.Contem(int64(*v)) {
		return
	}
	a.Valor, a.Aceito, a.Usando = *v, f.String(), usando
	*av = append(*av, a)
	*v = troca
}

const audiosAceitos = "none, flac ou aac"

func audioValido(mode string) bool {
	switch mode {
	case AudioNone, AudioFLAC, AudioAAC:
		return true
	}
	return false
}

func mecanismosAceitos() string {
	nomes := make([]string, 0, len(detect.Mecanismos))
	for n := range detect.Mecanismos {
		nomes = append(nomes, n)
	}
	sort.Strings(nomes)
	return strings.Join(nomes, " ou ")
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
	if cam.Detect == nil {
		v := c.Defaults.Detect
		cam.Detect = &v
	}
	if cam.DetectMecanismo == "" {
		cam.DetectMecanismo = c.Defaults.DetectMecanismo
	}
	if cam.DetectSensibilidade <= 0 {
		cam.DetectSensibilidade = c.Defaults.DetectSensibilidade
	}
	if cam.Name == "" {
		cam.Name = cam.ID
	}
	return cam
}

// LoadCameras lê o cameras.json. Ausente significa "nenhuma câmera cadastrada".
//
// Só o ID inválido impede o boot, porque ele vira nome de diretório no disco.
// Qualquer outro campo fora da faixa passa a usar o padrão e vira Aviso: um
// número ruim numa câmera não pode deixar todas sem gravar. O arquivo não é
// reescrito aqui; o próximo save pela tela grava a câmera já corrigida, que é
// a que está rodando.
func (c *Config) LoadCameras() ([]Camera, []Aviso, error) {
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
	var avisos []Aviso
	for i := range cams {
		if err := ValidateCameraID(cams[i].ID); err != nil {
			return nil, nil, fmt.Errorf("câmera #%d: %w", i, err)
		}
		var av []Aviso
		cams[i], av = c.ConfereCamera(cams[i])
		avisos = append(avisos, av...)
	}
	return cams, avisos, nil
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
