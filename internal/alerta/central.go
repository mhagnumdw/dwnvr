package alerta

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/limite"
)

// Central recebe os alertas de todas as câmeras, conta, põe no log e os
// entrega aos meios.
//
// O registro do alerta é a linha no log e a marca `alert` na linha do objeto,
// que o Recorder grava antes de a Central receber. Ele existe com meio
// configurado ou sem.
//
// Recebe é chamado da goroutine da fila do detector de objetos e nunca espera:
// o que pode esperar é o envio, na goroutine do Roda. Sem meio, não há espera,
// teto nem goroutine de envio.
type Central struct {
	log   *slog.Logger
	meios []Notificador
	agora func() time.Time

	// espera leva o alerta à goroutine de envio, e teto decide se ele entra
	// nela. Nil sem meio.
	espera chan Alerta
	teto   limite.Limitador

	total atomic.Int64

	mu       sync.Mutex
	estados  []EstadoDoMeio
	descarte Descarte
}

// chaveDoTeto é a única chave do teto: ele vale para a instalação inteira, e
// não por câmera, porque protege a conta de e-mail.
const chaveDoTeto = "alertas"

// Descarte é um episódio de alertas que não saíram pelos meios: começa no
// primeiro descarte e acaba AvisoDoDescarteDura depois do último. É dele que
// sai o Aviso do alerta que não saiu. Os alertas seguem gravados na detecção.
type Descarte struct {
	// FilaCheia são os que não couberam na espera, com o envio lento ou
	// travado; AcimaDoTeto, os que passaram do AlertasPorHora.
	FilaCheia, AcimaDoTeto int64
	Desde, Ultimo          time.Time
}

// motivoDoDescarte diz por que o alerta não saiu, no log.
type motivoDoDescarte string

const (
	filaCheia   motivoDoDescarte = "a fila de envio encheu"
	acimaDoTeto motivoDoDescarte = "passou do teto de alertas por hora"
)

// EstadoDoMeio é o que se sabe dos envios de um meio desde que o dwnvr subiu.
// É dele que sai o Aviso de meio falhando.
type EstadoDoMeio struct {
	Nome     string
	Enviados int64
	Falhas   int64
	// Ultimo é o instante da tentativa mais recente, desse certo ou não: com o
	// Erro vazio, ela deu certo. Zero antes do primeiro envio.
	Ultimo time.Time
	// FalhandoDesde é a primeira falha desde o último envio que deu certo, e
	// Erro, o da falha mais recente. Zerados quando um envio dá certo.
	FalhandoDesde time.Time
	Erro          string
}

// NovaCentral nasce com a primeira câmera que liga o alerta: quem nunca liga
// não paga nada.
func NovaCentral(meios []Notificador, log *slog.Logger) *Central {
	c := &Central{log: log, meios: meios, agora: time.Now}
	if len(meios) > 0 {
		c.espera = make(chan Alerta, AlertasNaEspera)
		c.teto = limite.NovoGCRAACada(time.Hour/AlertasPorHora, RajadaDeAlertas)
		c.estados = make([]EstadoDoMeio, len(meios))
		for i, m := range meios {
			c.estados[i].Nome = m.Nome()
		}
	}
	return c
}

// Recebe conta o alerta, põe no log e o põe na espera dos meios. Acima do teto,
// ou com a espera cheia, o alerta é descartado e contado: ele já está gravado
// na detecção, e esperar aqui pararia as olhadas de todas as câmeras.
func (c *Central) Recebe(a Alerta) {
	c.total.Add(1)
	attrs := []any{"cam", a.Camera, "familias", familiasDe(a), "objetos", len(a.Objetos)}
	if areas := areasDe(a); areas != "" {
		attrs = append(attrs, "areas", areas)
	}
	c.log.Info("alerta", attrs...)

	if c.espera == nil {
		return
	}
	if !c.teto.Permite(chaveDoTeto).Permitido {
		c.descarta(acimaDoTeto)
		return
	}
	select {
	case c.espera <- a:
	default:
		c.descarta(filaCheia)
	}
}

// descarta conta o alerta que não saiu no episódio de agora, ou começa outro
// quando o último descarte já passou do AvisoDoDescarteDura. Só o primeiro de
// cada episódio vai ao log: numa rajada, seria uma linha por alerta.
func (c *Central) descarta(motivo motivoDoDescarte) {
	agora := c.agora()
	c.mu.Lock()
	d := &c.descarte
	novo := d.Ultimo.IsZero() || agora.Sub(d.Ultimo) > AvisoDoDescarteDura
	if novo {
		*d = Descarte{Desde: agora}
	}
	if motivo == filaCheia {
		d.FilaCheia++
	} else {
		d.AcimaDoTeto++
	}
	d.Ultimo = agora
	c.mu.Unlock()
	if novo {
		c.log.Warn("alertas deixaram de sair pelos meios", "motivo", string(motivo))
	}
}

// Roda entrega os alertas da espera aos meios, um de cada vez, até o ctx
// acabar. Sem meio, volta na hora.
func (c *Central) Roda(ctx context.Context) {
	if c.espera == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case a := <-c.espera:
			c.envia(ctx, c.comQuadroDesenhado(a))
		}
	}
}

// comQuadroDesenhado troca o quadro olhado pelo quadro com as caixas e as
// áreas desenhadas, uma vez por alerta, para todos os meios. Se o desenho
// falhar, o quadro vai como veio: o alerta não deixa de sair por causa dele.
func (c *Central) comQuadroDesenhado(a Alerta) Alerta {
	if len(a.Quadro) == 0 {
		return a
	}
	quadro, err := quadroDesenhado(a)
	if err != nil {
		c.log.Warn("o quadro do alerta vai sem desenho", "cam", a.Camera, "erro", err)
		return a
	}
	a.Quadro = quadro
	return a
}

// envia passa o alerta a cada meio, cada um com o próprio PrazoDoEnvio: o meio
// travado não come o prazo do seguinte.
func (c *Central) envia(ctx context.Context, a Alerta) {
	for i, m := range c.meios {
		ctxDoEnvio, cancel := context.WithTimeout(ctx, PrazoDoEnvio)
		err := m.Envia(ctxDoEnvio, a)
		cancel()

		agora := c.agora()
		c.mu.Lock()
		e := &c.estados[i]
		e.Ultimo = agora
		if err == nil {
			e.Enviados++
			e.FalhandoDesde, e.Erro = time.Time{}, ""
		} else {
			e.Falhas++
			if e.FalhandoDesde.IsZero() {
				e.FalhandoDesde = agora
			}
			e.Erro = err.Error()
		}
		c.mu.Unlock()
		if err != nil {
			c.log.Warn("falha ao enviar o alerta", "meio", m.Nome(), "cam", a.Camera, "erro", err)
		}
	}
}

// Total é quantos alertas a Central recebeu desde que nasceu. É o que o
// /api/health leva.
func (c *Central) Total() int64 { return c.total.Load() }

// Descartes devolve o episódio de descarte que ainda vale, e falso quando não
// há nenhum: nunca houve, ou o último descarte passou do AvisoDoDescarteDura.
func (c *Central) Descartes() (Descarte, bool) {
	agora := c.agora()
	c.mu.Lock()
	defer c.mu.Unlock()
	d := c.descarte
	if d.Ultimo.IsZero() || agora.Sub(d.Ultimo) > AvisoDoDescarteDura {
		return Descarte{}, false
	}
	return d, true
}

// Meios devolve o estado de cada meio, na ordem em que foram construídos.
func (c *Central) Meios() []EstadoDoMeio {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]EstadoDoMeio(nil), c.estados...)
}

// familiasDe junta as famílias dos objetos, sem repetir, na ordem em que vêm:
// a do Recorder, que põe a de maior prioridade primeiro.
func familiasDe(a Alerta) string {
	var fs []string
	for _, o := range a.Objetos {
		if !slices.Contains(fs, o.Familia) {
			fs = append(fs, o.Familia)
		}
	}
	return strings.Join(fs, ",")
}

// areasDe junta as áreas em que os objetos caíram, sem repetir.
func areasDe(a Alerta) string {
	var as []string
	for _, o := range a.Objetos {
		for _, nome := range o.Areas {
			if !slices.Contains(as, nome) {
				as = append(as, nome)
			}
		}
	}
	return strings.Join(as, ",")
}
