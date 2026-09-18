package detect

import "context"

// Achado é um objeto que o detector viu num quadro.
//
// A caixa vai em FRAÇÃO do quadro, e não em pixels, porque é a única forma que
// continua valendo quando a resolução da câmera muda - e ela muda: há câmera
// cujo init anuncia 2560x1440 enquanto o stream entrega 1920x1080.
type Achado struct {
	Classe string  `json:"classe"`
	Score  float64 `json:"score"`
	// X1, Y1, X2, Y2 em fração do quadro, de 0 a 1.
	Caixa [4]float64 `json:"caixa"`
}

// Detector é o terceiro contrato: quem olha o pedaço de vídeo e diz o que
// havia no último quadro dele.
//
// Ele é o único dos contratos que custa caro - ~6,5 s de parede e 244 MB de
// pico num Orange Pi Zero 3 -, e é por isso que mora atrás de um gatilho e
// num container à parte: decodificar H264/HEVC e rodar um modelo exigem código
// nativo, e o dwnvr continua Go puro, CGO_ENABLED=0, numa imagem de 8 MB.
//
// NÃO TEM ESTADO, e não decide o que vira marca. Ele devolve toda caixa a
// partir de PisoDoDetector, de qualquer classe; o corte por família e a
// memória de objeto parado são do Marcador, no dwnvr. Um detector que
// reinicia não pode virar marca falsa na timeline.
type Detector interface {
	// Olha manda o pedaço e devolve o que havia no ÚLTIMO quadro dele, que é
	// o quadro que o Mecanismo mandou olhar. Fatia vazia é resultado, não
	// falta de resultado: é ela que diz "este onset já foi olhado, e não era
	// nada" - e é ela que conta como falta no rastreio.
	Olha(ctx context.Context, p Pedaco) ([]Achado, error)

	Nome() string
}

// Detectores é o mapa de construtores, gêmeo dos outros. O argumento é o
// endereço do detector, para quem tem um; o `nenhum` o ignora.
//
// O `nenhum` é o que faz a detecção de movimento funcionar sem sidecar nenhum:
// o gatilho marca movimento na timeline, e ninguém olha.
var Detectores = map[string]func(url string) Detector{
	"nenhum": func(string) Detector { return nenhum{} },
}

// nenhum é o detector que não olha. Não é um esboço: é o comportamento correto
// de quem não instalou o sidecar, e o que garante que a marcação de movimento
// seja usável sozinha.
type nenhum struct{}

func (nenhum) Nome() string { return "nenhum" }

func (nenhum) Olha(context.Context, Pedaco) ([]Achado, error) { return nil, nil }
