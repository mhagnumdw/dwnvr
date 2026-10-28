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

	"github.com/mhagnumdw/dwnvr/internal/area"
	"github.com/mhagnumdw/dwnvr/internal/config"
)

// Alerta é uma olhada que passou na regra de alerta da câmera, como os meios a
// recebem. O que fica gravado é a marca `alert` em cada linha de objeto (ver
// store.Evento); isto é o que sai pelo log e pelos meios, como o e-mail.
type Alerta struct {
	Camera, NomeDaCamera string
	// InstanteMs é o onset, a hora da cena, e não a da resposta do detector
	// de objetos, que chega de segundos a meio minuto depois.
	InstanteMs int64
	QuadroMs   int64
	TemQuadro  bool
	Objetos    []Objeto
	// Areas são as áreas do alerta, todas, e não só as em que algum objeto
	// caiu: o contorno delas vai desenhado no quadro. Vazio quando o alerta
	// vale na imagem inteira.
	Areas []AreaDoAlerta
	// Quadro é o JPEG olhado, que o e-mail leva no anexo. A Central o troca
	// pelo quadro com as caixas e as áreas desenhadas antes de passá-lo aos
	// meios (ver quadro.go).
	Quadro []byte
}

// AreaDoAlerta é uma área do alerta como estava na hora: o nome e o polígono,
// em fração do quadro.
type AreaDoAlerta struct {
	Nome   string
	Pontos []area.Ponto
}

// Objeto é uma marca que passou na regra. Areas são os nomes das áreas do
// alerta em que ela caiu, como estavam na hora, que é o que o log e o e-mail
// mostram; vazio quando o alerta vale na imagem inteira.
type Objeto struct {
	Familia string
	Classe  string
	Score   float64
	Caixa   [4]float64
	Areas   []string
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
// construtor devolve nil quando o meio não está configurado no dwnvr.yaml.
var Notificadores = map[string]func(cfg *config.Config) (Notificador, error){
	MeioEmail: NovoEmail,
}

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
