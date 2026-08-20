// Package go2rtc fala com a instância de go2rtc que o usuário administra.
//
// O dwnvr nunca configura o go2rtc: ele descobre os streams existentes e
// consome o fMP4 que o go2rtc já sabe produzir. Toda a conversa com as câmeras
// - RTSP, transporte, credenciais - continua sendo responsabilidade do go2rtc.
package go2rtc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/fmp4"
)

type Client struct {
	BaseURL  string
	Username string
	Password string
	HTTP     *http.Client
}

func New(cfg config.Go2RTC) *Client {
	return &Client{
		BaseURL:  strings.TrimRight(cfg.URL, "/"),
		Username: cfg.Username,
		Password: cfg.Password,
		// Sem timeout global: a resposta de stream.mp4 é infinita por natureza,
		// e um Timeout de http.Client vale para o corpo inteiro. Quem cuida do
		// corpo é o stallGuard; aqui só se protege o que vem ANTES dele.
		//
		// ResponseHeaderTimeout cobre um buraco real: um go2rtc travado ainda
		// aceita a conexão TCP (o kernel aceita por ele) e nunca responde. Sem
		// isto, Do() bloqueia para sempre - a mesma falha silenciosa que o
		// guarda evita depois, só que num ponto onde ele ainda não existe.
		HTTP: &http.Client{
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				ResponseHeaderTimeout: 2 * config.DefaultStallSeconds * time.Second,
			},
		},
	}
}

// StreamURL monta a URL de captura.
//
// O modo de áudio vira um filtro de codec, e é por isso que ele pode ser
// escolhido por câmera sem nenhuma outra mudança no sistema:
//
//	none  descarta o áudio na origem - não trafega e não custa nada
//	flac  o go2rtc converte pcm_alaw→FLAC em Go puro, sem disparar ffmpeg
//	aac   exige que o go2rtc.yaml tenha uma fonte ffmpeg:cam#audio=aac
func (c *Client) StreamURL(cam, audio string) string {
	q := url.Values{}
	q.Set("src", cam)
	switch audio {
	case config.AudioFLAC:
		q.Set("mp4", "flac")
	case config.AudioAAC:
		q.Set("video", "h264,h265")
		q.Set("audio", "aac")
	default:
		q.Set("video", "h264,h265")
	}
	return c.BaseURL + "/api/stream.mp4?" + q.Encode()
}

// OpenStream abre o fMP4 contínuo de uma câmera. Quem chama fecha o corpo.
//
// O corpo devolvido se fecha sozinho se o go2rtc passar `idle` sem entregar um
// único byte - ver stallGuard para o porquê.
func (c *Client) OpenStream(ctx context.Context, cam, audio string, idle time.Duration) (io.ReadCloser, error) {
	if idle <= 0 {
		idle = config.DefaultStallSeconds * time.Second
	}

	// O context próprio é o que permite ao guarda derrubar ESTA requisição sem
	// afetar quem chamou.
	ctx, cancel := context.WithCancel(ctx)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.StreamURL(cam, audio), nil)
	if err != nil {
		cancel()
		return nil, err
	}
	c.auth(req)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("go2rtc devolveu HTTP %d para %q", resp.StatusCode, cam)
	}

	g := &stallGuard{body: resp.Body, idle: idle, cancel: cancel}
	// A primeira leitura ganha o dobro do prazo: abrir o stream faz o go2rtc
	// estabelecer a sessão RTSP com a câmera, o que é legitimamente mais lento
	// que entregar o próximo fragmento de um stream que já está correndo.
	g.timer = time.AfterFunc(2*idle, g.trip)
	return g, nil
}

// stallGuard derruba a conexão quando o go2rtc para de mandar bytes sem fechá-la.
//
// Existe por causa de uma falha real: os produtores RTSP do go2rtc rodam sobre
// UDP, e quando o fluxo da câmera para não há erro de socket nenhum - o go2rtc
// simplesmente deixa de escrever, com a resposta HTTP aberta. Do lado de cá o
// Read bloqueia para sempre, sem erro, sem EOF e sem log: em 09/08/2026 cinco
// câmeras ficaram 3h38 sem gravar reportando `connected: true`.
//
// Fechar a conexão é também o que RECUPERA a câmera. Como o dwnvr é o único
// consumidor do stream, sair faz o go2rtc derrubar o produtor morto, e a
// reconexão de run() abre uma sessão RTSP nova.
type stallGuard struct {
	body   io.ReadCloser
	idle   time.Duration
	timer  *time.Timer
	cancel context.CancelFunc

	stalled atomic.Bool
}

func (g *stallGuard) trip() {
	// A ordem importa: quem observar o erro da leitura precisa já enxergar o
	// motivo, senão o cancelamento vira um "context canceled" sem explicação.
	g.stalled.Store(true)
	g.cancel()
}

func (g *stallGuard) Read(p []byte) (int, error) {
	n, err := g.body.Read(p)
	if n > 0 {
		g.timer.Reset(g.idle)
	}
	if err != nil && g.stalled.Load() {
		return n, fmt.Errorf("go2rtc não enviou nada por %s", g.idle)
	}
	return n, err
}

func (g *stallGuard) Close() error {
	g.timer.Stop()
	err := g.body.Close()
	g.cancel()
	return err
}

func (c *Client) auth(req *http.Request) {
	if c.Username != "" || c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
}

// Stream é o que o go2rtc reporta sobre um stream em /api/streams.
type Stream struct {
	Producers []Producer `json:"producers"`
	Consumers []any      `json:"consumers"`
}

type Producer struct {
	URL string `json:"url"`

	// Medias vem como texto livre, uma linha por trilha, no formato
	// "<tipo>, <direção>, <codec>" - por exemplo "audio, recvonly, PCMA/16000".
	// Não é JSON estruturado: é a representação SDP que o go2rtc expõe.
	Medias []string `json:"medias"`
}

// Transcoding indica que a fonte é um ffmpeg, ou seja, que há transcodificação
// acontecendo - informação que vale a pena mostrar no diagnóstico, já que o
// objetivo declarado é não transcodificar vídeo.
func (p Producer) Transcoding() bool { return strings.HasPrefix(p.URL, "ffmpeg:") }

// HasAudio diz se o produtor entrega alguma trilha de áudio. É o que permite à
// tela de cadastro só oferecer áudio nas câmeras que de fato o têm.
func (p Producer) HasAudio() bool {
	for _, m := range p.Medias {
		if strings.HasPrefix(m, "audio") {
			return true
		}
	}
	return false
}

// AudioCodecs devolve os codecs de áudio anunciados, por exemplo "PCMA/16000".
func (p Producer) AudioCodecs() []string {
	var out []string
	for _, m := range p.Medias {
		if !strings.HasPrefix(m, "audio") {
			continue
		}
		// "audio, recvonly, PCMA/16000" -> "PCMA/16000"
		if i := strings.LastIndex(m, ", "); i >= 0 {
			out = append(out, strings.TrimSpace(m[i+2:]))
		}
	}
	return out
}

// Streams lista os streams configurados no go2rtc. É a fonte da tela de
// cadastro: o usuário escolhe entre o que já existe, em vez de digitar nomes.
func (c *Client) Streams(ctx context.Context) (map[string]Stream, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/streams", nil)
	if err != nil {
		return nil, err
	}
	c.auth(req)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("go2rtc devolveu HTTP %d em /api/streams", resp.StatusCode)
	}

	var out map[string]Stream
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// Prazos da sonda. São curtos de propósito: quem espera é uma tela aberta, e o
// stallGuard dá o dobro de probeIdle para o primeiro byte, que é onde mora a
// lentidão legítima (o go2rtc ainda vai estabelecer a sessão RTSP com a câmera).
const (
	probeIdle    = 4 * time.Second
	probeTimeout = 12 * time.Second
)

// AudioProbe é o que uma sonda descobriu sobre o áudio de um stream.
type AudioProbe struct {
	HasAudio bool
	Codecs   []string
}

// ProbeAudio descobre se um stream entrega áudio, abrindo-o por alguns segundos.
//
// Existe porque o go2rtc só preenche `medias` enquanto alguém consome o stream:
// num stream ocioso ele nem abre a conexão com a câmera, e a tela de cadastro
// ficava sem ter como saber se pode oferecer FLAC ou AAC. A sonda é, por alguns
// segundos, o consumidor que faltava.
//
// Ela pede o stream em modo FLAC de propósito: `mp4=flac` é o único filtro que
// traz a trilha de áudio, e ler o moov é a prova de que a conexão de fato
// entregou mídia - o mesmo init segment que o gravador leria. Com a conexão
// ainda aberta, o `medias` do produtor responde qual codec a câmera anuncia.
func (c *Client) ProbeAudio(ctx context.Context, cam string) (AudioProbe, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	body, err := c.OpenStream(ctx, cam, config.AudioFLAC, probeIdle)
	if err != nil {
		return AudioProbe{}, err
	}
	defer body.Close()

	var probe AudioProbe
	rd := fmp4.NewReader(body)
	for {
		typ, box, err := rd.NextBox()
		if err != nil {
			return AudioProbe{}, err
		}
		if typ != "moov" {
			continue
		}
		mv, err := fmp4.ParseMoov(box)
		if err != nil {
			return AudioProbe{}, fmt.Errorf("moov ilegível: %w", err)
		}
		probe.HasAudio = mv.HasAudio()
		break
	}

	// O moov só mostraria "fLaC", que é o codec DEPOIS da conversão. O nome que
	// interessa ao usuário - PCMA/16000 e afins - é o que a câmera anuncia, e
	// esse só existe no produtor, que agora está vivo por causa desta conexão.
	if streams, err := c.Streams(ctx); err == nil {
		for _, p := range streams[cam].Producers {
			if p.HasAudio() {
				probe.HasAudio = true
				probe.Codecs = append(probe.Codecs, p.AudioCodecs()...)
			}
		}
	}
	return probe, nil
}
