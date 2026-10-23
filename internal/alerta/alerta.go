// Package alerta decide quando uma detecção vira alerta, guarda o registro do
// alerta e o entrega aos meios de envio: o e-mail, e depois outros.
//
// O alerta avisa do que a detecção marcou, e ela não marca toda chegada: não é
// alarme (ver docs/deteccao.md). Ele também nunca segura a fila do detector de
// objetos: a regra e o registro são contas em memória, e o envio, que pode
// esperar um servidor de e-mail, roda numa goroutine própria.
package alerta

import (
	"context"
	"log/slog"
	"slices"

	"github.com/mhagnumdw/dwnvr/internal/config"
)

// Alerta é uma olhada que passou na regra de alerta da câmera. O JSON tem o
// formato de uma detecção do /api/deteccoes, com as áreas em que cada objeto
// caiu: o alerta é uma detecção, e a tela a desenha com o mesmo código.
type Alerta struct {
	Camera string `json:"cam"`
	// InstanteMs é o onset, a hora da cena, e não a da resposta do detector
	// de objetos, que chega de segundos a meio minuto depois.
	InstanteMs int64    `json:"instanteMs"`
	QuadroMs   int64    `json:"quadroMs,omitempty"`
	TemQuadro  bool     `json:"temQuadro"`
	Objetos    []Objeto `json:"objetos"`

	// NomeDaCamera e Quadro são para os meios: o e-mail leva o nome no
	// assunto e o quadro no anexo. Não vão ao JSON: a tela tem o nome pelo
	// cadastro, e o quadro pelo /api/deteccoes/quadro.
	NomeDaCamera string `json:"-"`
	Quadro       []byte `json:"-"`
}

// Objeto é uma marca que passou na regra. Areas são os nomes das áreas do
// alerta em que ela caiu, como estavam na hora; vazio quando o alerta vale na
// imagem inteira.
type Objeto struct {
	Familia string     `json:"familia"`
	Classe  string     `json:"classe"`
	Score   float64    `json:"score"`
	Caixa   [4]float64 `json:"caixa"`
	Areas   []string   `json:"areas,omitempty"`
}

// Notificador é um meio de avisar alguém: o e-mail, e depois Telegram, ntfy e
// afins. Não guarda estado entre envios: quem decide o que vira alerta, e
// quando, é este pacote.
type Notificador interface {
	// Envia entrega um alerta, e só volta quando o meio aceitou ou recusou,
	// dentro do prazo do ctx.
	Envia(ctx context.Context, a Alerta) error

	// Nome é o do meio, como "email": aparece no log e na tela.
	Nome() string
}

// Notificadores é o mapa de construtores: um meio novo entra numa linha. O
// construtor devolve nil quando o meio não está configurado no dwnvr.yaml. O
// e-mail entra na Etapa 5 do TODO das áreas e alertas.
var Notificadores = map[string]func(cfg *config.Config) (Notificador, error){}

// Meios constrói os meios configurados, em ordem de nome. O que falha ao
// nascer fica de fora e vai ao log: um e-mail mal configurado não pode calar
// o registro, nem os outros meios.
func Meios(cfg *config.Config, log *slog.Logger) []Notificador {
	nomes := make([]string, 0, len(Notificadores))
	for nome := range Notificadores {
		nomes = append(nomes, nome)
	}
	slices.Sort(nomes)
	var meios []Notificador
	for _, nome := range nomes {
		n, err := Notificadores[nome](cfg)
		if err != nil {
			log.Error("meio de alerta ficou de fora", "meio", nome, "erro", err)
			continue
		}
		if n != nil {
			meios = append(meios, n)
		}
	}
	return meios
}
