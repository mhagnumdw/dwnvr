package alerta

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Central recebe os alertas de todas as câmeras, conta, põe no log e os
// entrega aos meios.
//
// O registro do alerta é a linha no log e a marca `alert` na linha do objeto,
// que o Recorder grava antes de a Central receber. Ele existe com meio
// configurado ou sem.
//
// Recebe é chamado da goroutine da fila do detector de objetos e nunca espera:
// o que pode esperar é o envio, na goroutine do Roda. Sem meio, não há espera
// nem goroutine de envio.
type Central struct {
	log   *slog.Logger
	meios []Notificador

	// espera leva o alerta à goroutine de envio. Nil sem meio.
	espera chan Alerta

	total, descartados atomic.Int64

	mu      sync.Mutex
	estados []EstadoDoMeio
}

// EstadoDoMeio é o que se sabe dos envios de um meio desde que o dwnvr subiu.
// É dele que sai o Aviso de meio falhando.
type EstadoDoMeio struct {
	Nome     string
	Enviados int64
	Falhas   int64
	// FalhandoDesde é a primeira falha desde o último envio que deu certo, e
	// Erro, o da falha mais recente. Zerados quando um envio dá certo.
	FalhandoDesde time.Time
	Erro          string
}

// NovaCentral nasce com a primeira câmera que liga o alerta: quem nunca liga
// não paga nada.
func NovaCentral(meios []Notificador, log *slog.Logger) *Central {
	c := &Central{log: log, meios: meios}
	if len(meios) > 0 {
		c.espera = make(chan Alerta, AlertasNaEspera)
		c.estados = make([]EstadoDoMeio, len(meios))
		for i, m := range meios {
			c.estados[i].Nome = m.Nome()
		}
	}
	return c
}

// Recebe conta o alerta, põe no log e o põe na espera dos meios. Com a espera
// cheia, o alerta é descartado e contado: ele já está gravado na detecção, e
// esperar aqui pararia as olhadas de todas as câmeras.
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
	select {
	case c.espera <- a:
	default:
		c.descartados.Add(1)
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
			c.envia(ctx, a)
		}
	}
}

// envia passa o alerta a cada meio, cada um com o próprio PrazoDoEnvio: o meio
// travado não come o prazo do seguinte.
func (c *Central) envia(ctx context.Context, a Alerta) {
	for i, m := range c.meios {
		ctxDoEnvio, cancel := context.WithTimeout(ctx, PrazoDoEnvio)
		err := m.Envia(ctxDoEnvio, a)
		cancel()

		c.mu.Lock()
		e := &c.estados[i]
		if err == nil {
			e.Enviados++
			e.FalhandoDesde, e.Erro = time.Time{}, ""
		} else {
			e.Falhas++
			if e.FalhandoDesde.IsZero() {
				e.FalhandoDesde = time.Now()
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

// Descartados são os alertas que não couberam na espera dos meios.
func (c *Central) Descartados() int64 { return c.descartados.Load() }

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
