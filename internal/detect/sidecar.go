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

// respostaDoSidecar é o JSON de POST /detecta: os achados, ou o motivo de não
// ter olhado.
type respostaDoSidecar struct {
	Achados []Achado `json:"achados"`
	Erro    string   `json:"erro"`
}

// maiorResposta é folga larga: uma olhada com 300 caixas acima do piso, que é
// o máximo que o RF-DETR devolve, dá uns 30 KB.
const maiorResposta = 1 << 20

func (s *sidecar) Olha(ctx context.Context, p Pedaco) ([]Achado, error) {
	url := s.url + "/detecta?piso=" + strconv.FormatFloat(PisoDoDetector, 'f', -1, 64)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(p.Fmp4))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "video/mp4")
	resp, err := s.cliente.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var r respostaDoSidecar
	if err := json.NewDecoder(io.LimitReader(resp.Body, maiorResposta)).Decode(&r); err != nil {
		return nil, fmt.Errorf("resposta do sidecar ilegível (HTTP %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return nil, fmt.Errorf("%w: HTTP %d: %s", ErrPedacoRecusado, resp.StatusCode, r.Erro)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sidecar respondeu HTTP %d: %s", resp.StatusCode, r.Erro)
	}
	if r.Achados == nil {
		r.Achados = []Achado{} // olhada sem nada é resultado, não falta dele
	}
	return r.Achados, nil
}
