package alerta

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func mudo() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// alertaDe é um alerta da câmera com uma pessoa no Portão, no instante dado.
func alertaDe(instanteMs int64) Alerta {
	return Alerta{Camera: "cam_a", NomeDaCamera: "Frente", InstanteMs: instanteMs, Quadro: []byte("jpeg"),
		Objetos: []Objeto{{Familia: "pessoa", Classe: "person", Score: 0.87, Areas: []string{"Portão"}}}}
}

// TestRegistroSemMeio: sem meio, o alerta vai ao log e à lista, e nada mais
// nasce. A lista vem do mais novo para o mais velho, sem o quadro, e guarda
// só os últimos AlertasNaTela.
func TestRegistroSemMeio(t *testing.T) {
	var log strings.Builder
	c := NovaCentral(nil, slog.New(slog.NewTextHandler(&log, nil)))
	if c.espera != nil {
		t.Fatal("a espera nasceu sem meio")
	}

	c.Recebe(alertaDe(1))
	if !strings.Contains(log.String(), "msg=alerta cam=cam_a familias=pessoa objetos=1 areas=Portão") {
		t.Errorf("log: %s", log.String())
	}
	if r := c.Recentes(); len(r) != 1 || r[0].InstanteMs != 1 || r[0].Quadro != nil {
		t.Errorf("recentes %+v", r)
	}

	for i := range AlertasNaTela + 4 {
		c.Recebe(alertaDe(int64(i + 2)))
	}
	r := c.Recentes()
	if len(r) != AlertasNaTela || c.Total() != AlertasNaTela+5 {
		t.Fatalf("%d na lista e %d no total, esperava %d e %d", len(r), c.Total(), AlertasNaTela, AlertasNaTela+5)
	}
	if r[0].InstanteMs != AlertasNaTela+5 || r[AlertasNaTela-1].InstanteMs != 6 {
		t.Errorf("lista de %d a %d, esperava de %d a 6", r[0].InstanteMs, r[AlertasNaTela-1].InstanteMs, AlertasNaTela+5)
	}
	// Roda sem meio volta na hora, e nenhuma goroutine fica para trás.
	c.Roda(context.Background())
}

// meioDeTeste é um Notificador que obedece a quem testa: devolve o erro de
// agora, anota o prazo de cada envio e avisa que o alerta chegou.
type meioDeTeste struct {
	mu     sync.Mutex
	erro   error
	prazos []time.Duration
	chegou chan Alerta
}

func (m *meioDeTeste) Nome() string { return "teste" }

func (m *meioDeTeste) Envia(ctx context.Context, a Alerta) error {
	if fim, ok := ctx.Deadline(); ok {
		m.mu.Lock()
		m.prazos = append(m.prazos, time.Until(fim))
		m.mu.Unlock()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.chegou != nil {
		m.chegou <- a
	}
	return m.erro
}

// TestMeioLentoNaoSeguraQuemRecebe: com o meio parado, a espera enche e o que
// não cabe é descartado e contado, sem o Recebe esperar. O registro fica com
// todos.
func TestMeioLentoNaoSeguraQuemRecebe(t *testing.T) {
	c := NovaCentral([]Notificador{&meioDeTeste{}}, mudo())
	// Sem o Roda, ninguém tira da espera: é o meio mais lento possível.
	inicio := time.Now()
	for i := range AlertasNaEspera + 5 {
		c.Recebe(alertaDe(int64(i)))
	}
	if d := time.Since(inicio); d > time.Second {
		t.Errorf("o Recebe levou %v", d)
	}
	if c.Descartados() != 5 || c.Total() != AlertasNaEspera+5 {
		t.Errorf("%d descartados e %d no total, esperava 5 e %d", c.Descartados(), c.Total(), AlertasNaEspera+5)
	}
}

// TestMeioQueFalha: a falha é contada, com o "desde quando" e o erro, e o
// primeiro envio que dá certo os zera. Cada envio vai com o PrazoDoEnvio.
func TestMeioQueFalha(t *testing.T) {
	meio := &meioDeTeste{erro: errors.New("senha recusada"), chegou: make(chan Alerta, 4)}
	c := NovaCentral([]Notificador{meio}, mudo())
	ctx := t.Context()
	go c.Roda(ctx)

	espera := func() {
		t.Helper()
		select {
		case <-meio.chegou:
		case <-time.After(5 * time.Second):
			t.Fatal("o meio não recebeu o alerta")
		}
	}
	// O estado é escrito logo depois do Envia voltar: espera um pouco por ele.
	estado := func(cond func(EstadoDoMeio) bool) EstadoDoMeio {
		t.Helper()
		for range 500 {
			if e := c.Meios()[0]; cond(e) {
				return e
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("estado %+v", c.Meios()[0])
		return EstadoDoMeio{}
	}

	c.Recebe(alertaDe(1))
	espera()
	e := estado(func(e EstadoDoMeio) bool { return e.Falhas == 1 })
	desde := e.FalhandoDesde
	if desde.IsZero() || e.Erro != "senha recusada" || e.Nome != "teste" {
		t.Errorf("estado %+v", e)
	}

	c.Recebe(alertaDe(2))
	espera()
	e = estado(func(e EstadoDoMeio) bool { return e.Falhas == 2 })
	if !e.FalhandoDesde.Equal(desde) {
		t.Errorf("falhando desde %v, esperava o da primeira falha, %v", e.FalhandoDesde, desde)
	}

	meio.mu.Lock()
	meio.erro = nil
	meio.mu.Unlock()
	c.Recebe(alertaDe(3))
	espera()
	e = estado(func(e EstadoDoMeio) bool { return e.Enviados == 1 })
	if !e.FalhandoDesde.IsZero() || e.Erro != "" || e.Falhas != 2 {
		t.Errorf("depois do envio que deu certo: %+v", e)
	}

	meio.mu.Lock()
	defer meio.mu.Unlock()
	for _, p := range meio.prazos {
		if p <= 0 || p > PrazoDoEnvio {
			t.Errorf("prazo do envio %v, esperava até %v", p, PrazoDoEnvio)
		}
	}
	if len(meio.prazos) != 3 {
		t.Errorf("%d envios com prazo, esperava 3", len(meio.prazos))
	}
}
