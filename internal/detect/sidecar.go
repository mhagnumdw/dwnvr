package detect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
)

// sidecar é o Detector que manda o pedaço ao container `dwnvr-detect` por
// HTTP e devolve o que ele achou. O container é `dwnvr-detect/servidor.py`.
//
// Ele não tem timeout próprio: quem chama passa o prazo no ctx, porque é a
// fila que sabe quanto uma olhada ainda vale.
type sidecar struct {
	url     string
	cliente *http.Client
}

// NovoSidecar devolve o Detector do container que atende em `url` (ex.:
// http://dwnvr-detect:8480).
func NovoSidecar(url string) Detector {
	return &sidecar{url: strings.TrimRight(url, "/"), cliente: &http.Client{}}
}

func (s *sidecar) Nome() string { return "sidecar" }

// ErrPedacoRecusado é a resposta de um detector que está no ar e recusou o
// pedaço - vídeo que não decodifica, por exemplo. Não é queda: a fila não
// pausa, e o funil conta à parte.
//
// Acontece de verdade: há câmera que manda erro de H.264 numa parte dos
// segmentos, e o pedaço que pega um desses trechos não decodifica.
var ErrPedacoRecusado = errors.New("detector recusou o pedaço")

// respostaDoSidecar é o JSON de POST /detect: os achados, ou o motivo de não
// ter olhado.
type respostaDoSidecar struct {
	Achados []Achado `json:"achados"`
	Erro    string   `json:"erro"`

	// Quadro é o JPEG do quadro olhado, em base64, e só vem quando houve
	// achado. Base64 custa um terço a mais de bytes numa conexão que é local
	// - o sidecar roda na mesma máquina -, e em troca a resposta continua um
	// JSON só, que se lê com `curl` na hora de depurar.
	Quadro []byte `json:"quadro,omitempty"`
}

// maiorResposta é folga larga: uma olhada com 300 caixas acima do piso, que é
// o máximo que o RF-DETR devolve, dá uns 30 KB - e o quadro em base64, umas
// dezenas de KB.
const maiorResposta = 4 << 20

func (s *sidecar) Olha(ctx context.Context, p Pedaco) (Visao, error) {
	url := s.url + "/detect?piso=" + strconv.FormatFloat(PisoDoDetector, 'f', -1, 64) +
		"&larguraDoQuadro=" + strconv.Itoa(LarguraDoQuadro) + "&qualidade=" + strconv.Itoa(QualidadeDoQuadro)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(p.Fmp4))
	if err != nil {
		return Visao{}, err
	}
	req.Header.Set("Content-Type", "video/mp4")
	resp, err := s.cliente.Do(req)
	if err != nil {
		return Visao{}, err
	}
	defer resp.Body.Close()

	var r respostaDoSidecar
	if err := json.NewDecoder(io.LimitReader(resp.Body, maiorResposta)).Decode(&r); err != nil {
		return Visao{}, fmt.Errorf("resposta do sidecar ilegível (HTTP %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return Visao{}, fmt.Errorf("%w: HTTP %d: %s", ErrPedacoRecusado, resp.StatusCode, r.Erro)
	}
	if resp.StatusCode != http.StatusOK {
		return Visao{}, fmt.Errorf("sidecar respondeu HTTP %d: %s", resp.StatusCode, r.Erro)
	}
	if r.Achados == nil {
		r.Achados = []Achado{} // olhada sem nada é resultado, não falta dele
	}
	return Visao{Achados: r.Achados, Quadro: r.Quadro}, nil
}

// Saude é o que o GET /health do sidecar devolve: o modelo carregado, o
// tamanho em que ele olha o quadro e quantas threads usa.
type Saude struct {
	Modelo  string `json:"modelo"`
	Entrada string `json:"entrada"`
	Threads int    `json:"threads"`
}

// prazoDaSaude é curto porque quem espera é uma tela aberta. O sidecar
// responde o /health na hora mesmo no meio de uma olhada; passar disso já é o
// diagnóstico.
const prazoDaSaude = 3 * time.Second

// SaudeDoSidecar pergunta ao sidecar em `url` se ele está no ar e com qual
// modelo. Serve à tela de Diagnóstico; a fila não usa, porque descobre a queda
// na própria olhada. O erro vem sem o `Get "<url>":` que o http.Client põe na
// frente, porque a tela já mostra o endereço ao lado.
func SaudeDoSidecar(ctx context.Context, url string) (Saude, error) {
	ctx, cancel := context.WithTimeout(ctx, prazoDaSaude)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(url, "/")+"/health", nil)
	if err != nil {
		return Saude{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Saude{}, fmt.Errorf("não respondeu em %d s", int(prazoDaSaude.Seconds()))
		}
		if ue, ok := errors.AsType[*neturl.Error](err); ok {
			return Saude{}, ue.Err
		}
		return Saude{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Saude{}, fmt.Errorf("sidecar respondeu HTTP %d em /health", resp.StatusCode)
	}
	var s Saude
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&s); err != nil {
		return Saude{}, fmt.Errorf("resposta do /health ilegível: %w", err)
	}
	return s, nil
}
