package detect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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
		"&quadro=" + strconv.Itoa(LarguraDoQuadro) + "&qualidade=" + strconv.Itoa(QualidadeDoQuadro)
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
