// Package go2rtc fala com a instância de go2rtc que o usuário administra.
//
// O dwnvr nunca configura o go2rtc: ele descobre os streams existentes e
// consome o fMP4 que o go2rtc já sabe produzir. Toda a conversa com as câmeras
// — RTSP, transporte, credenciais — continua sendo responsabilidade do go2rtc.
package go2rtc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
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
		// Sem timeout global: a resposta de stream.mp4 é infinita por natureza.
		// O controle de vida da conexão fica com o context de quem chama.
		HTTP: &http.Client{},
	}
}

// StreamURL monta a URL de captura.
//
// O modo de áudio vira um filtro de codec, e é por isso que ele pode ser
// escolhido por câmera sem nenhuma outra mudança no sistema:
//
//	none  descarta o áudio na origem — não trafega e não custa nada
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
func (c *Client) OpenStream(ctx context.Context, cam, audio string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.StreamURL(cam, audio), nil)
	if err != nil {
		return nil, err
	}
	c.auth(req)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("go2rtc devolveu HTTP %d para %q", resp.StatusCode, cam)
	}
	return resp.Body, nil
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
	URL    string  `json:"url"`
	Medias []Media `json:"medias"`
}

type Media struct {
	Kind      string  `json:"kind"`
	Direction string  `json:"direction"`
	Codecs    []Codec `json:"codecs"`
}

type Codec struct {
	Name     string `json:"name"`
	Channels int    `json:"channels"`
}

// Transcoding indica que a fonte é um ffmpeg, ou seja, que há transcodificação
// acontecendo — informação que vale a pena mostrar no diagnóstico, já que o
// objetivo declarado é não transcodificar vídeo.
func (p Producer) Transcoding() bool { return strings.HasPrefix(p.URL, "ffmpeg:") }

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
