// Package limite conta as requisições de cada cliente e diz quando ele passou
// do ritmo permitido.
//
// O login usa (ver internal/api/auth.go), e o teto do alerta também, com uma
// chave só para a instalação inteira (ver internal/alerta). Um limite em
// outras rotas usaria o mesmo Limitador, com outro ritmo e outra rajada.
package limite

import (
	"sync"
	"time"
)

// Limitador decide se a próxima requisição de um cliente passa.
type Limitador interface {
	// Permite registra a requisição do cliente, identificado pela chave, e diz
	// se ela passa. A recusada não conta: quem insiste durante a espera não a
	// estica.
	Permite(chave string) Resultado
}

// Resultado é a decisão sobre uma requisição.
type Resultado struct {
	Permitido bool

	// Espera é quanto falta para a próxima requisição passar. Só vale com
	// Permitido falso.
	Espera time.Duration

	// PrimeiraRecusa marca a primeira recusa desde a última requisição que
	// passou: é o começo de um episódio, e só ele vira log. Sem isso, quem
	// insiste viraria uma linha de log por tentativa.
	PrimeiraRecusa bool
}

// GCRA é o Limitador pelo Generic Cell Rate Algorithm
// (https://en.wikipedia.org/wiki/Generic_cell_rate_algorithm).
//
// Cada cliente tem um só instante guardado, o TAT (theoretical arrival time):
// quando a próxima requisição chegaria se o cliente andasse exatamente no
// ritmo. Cada requisição que passa empurra o TAT um intervalo para a frente. A
// requisição passa enquanto o TAT não estiver adiantado mais que a folga, que
// é o que deixa passar a rajada.
//
// O mesmo resultado de um token bucket, guardando um instante por cliente em
// vez de um contador e um instante. E um TAT que já passou é igual a cliente
// novo, o que torna a limpeza trivial.
type GCRA struct {
	intervalo time.Duration // entre duas requisições no ritmo
	folga     time.Duration // quanto o TAT pode se adiantar: (rajada - 1) intervalos
	agora     func() time.Time

	mu       sync.Mutex
	clientes map[string]*cliente
	limpouEm time.Time
}

type cliente struct {
	tat       time.Time
	recusando bool
}

// NovoGCRA deixa passar porMinuto requisições por minuto de cada cliente,
// com até rajada delas seguidas. porMinuto e rajada têm de ser positivos.
func NovoGCRA(porMinuto, rajada int) *GCRA {
	return NovoGCRAACada(time.Minute/time.Duration(porMinuto), rajada)
}

// NovoGCRAACada deixa passar uma requisição de cada cliente a cada intervalo,
// com até rajada delas seguidas: o ritmo que "N por minuto" não escreve, como
// 15 por hora. intervalo e rajada têm de ser positivos.
func NovoGCRAACada(intervalo time.Duration, rajada int) *GCRA {
	return novoGCRA(intervalo, rajada, time.Now)
}

// novoGCRA é o NovoGCRAACada com o relógio trocável, para os testes.
func novoGCRA(intervalo time.Duration, rajada int, agora func() time.Time) *GCRA {
	return &GCRA{
		intervalo: intervalo,
		folga:     time.Duration(rajada-1) * intervalo,
		agora:     agora,
		clientes:  map[string]*cliente{},
		limpouEm:  agora(),
	}
}

// Permite é o Limitador.Permite pelo GCRA: a requisição passa se o TAT da
// chave não estiver adiantado mais que a folga, e aí o empurra um intervalo.
func (g *GCRA) Permite(chave string) Resultado {
	g.mu.Lock()
	defer g.mu.Unlock()

	agora := g.agora()
	g.limpa(agora)

	c, ok := g.clientes[chave]
	if !ok {
		c = &cliente{tat: agora}
		g.clientes[chave] = c
	}
	tat := c.tat
	if tat.Before(agora) {
		tat = agora
	}

	if adiantado := tat.Sub(agora); adiantado > g.folga {
		primeira := !c.recusando
		c.recusando = true
		return Resultado{Espera: adiantado - g.folga, PrimeiraRecusa: primeira}
	}
	c.tat = tat.Add(g.intervalo)
	c.recusando = false
	return Resultado{Permitido: true}
}

// limpa apaga, no máximo uma vez por LimpezaACada, os clientes cujo TAT já
// passou: para eles a rajada está inteira de novo, como para quem nunca
// tentou. Roda dentro do Permite, sem goroutine própria.
func (g *GCRA) limpa(agora time.Time) {
	if agora.Sub(g.limpouEm) < LimpezaACada {
		return
	}
	g.limpouEm = agora
	for k, c := range g.clientes {
		if !c.tat.After(agora) {
			delete(g.clientes, k)
		}
	}
}
