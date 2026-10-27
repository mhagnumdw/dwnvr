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

	"github.com/mhagnumdw/dwnvr/internal/limite"
)

func mudo() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// alertaDe é um alerta da câmera com uma pessoa no Portão, no instante dado.
func alertaDe(instanteMs int64) Alerta {
	return Alerta{Camera: "cam_a", NomeDaCamera: "Frente", InstanteMs: instanteMs, Quadro: []byte("jpeg"),
		Objetos: []Objeto{{Familia: "pessoa", Classe: "person", Score: 0.87, Areas: []string{"Portão"}}}}
}

// TestRegistroSemMeio: sem meio, o alerta é contado e vai ao log, e nada mais
// nasce.
func TestRegistroSemMeio(t *testing.T) {
	var log strings.Builder
	c := NovaCentral(nil, slog.New(slog.NewTextHandler(&log, nil)))
	if c.espera != nil {
		t.Fatal("a espera nasceu sem meio")
	}

	c.Recebe(alertaDe(1))
	c.Recebe(alertaDe(2))
	if !strings.Contains(log.String(), "msg=alerta cam=cam_a familias=pessoa objetos=1 areas=Portão") {
		t.Errorf("log: %s", log.String())
	}
	if c.Total() != 2 {
		t.Errorf("total %d, esperava 2", c.Total())
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
	c.teto = tetoDeTeste{passa: true} // só a espera pode descartar
	// Sem o Roda, ninguém tira da espera: é o meio mais lento possível.
	inicio := time.Now()
	for i := range AlertasNaEspera + 5 {
		c.Recebe(alertaDe(int64(i)))
	}
	if d := time.Since(inicio); d > time.Second {
		t.Errorf("o Recebe levou %v", d)
	}
	d, ok := c.Descartes()
	if !ok || d.FilaCheia != 5 || d.AcimaDoTeto != 0 || c.Total() != AlertasNaEspera+5 {
		t.Errorf("descarte %+v e %d no total, esperava 5 na fila cheia e %d", d, c.Total(), AlertasNaEspera+5)
	}
}

// tetoDeTeste é um limite.Limitador que deixa passar tudo, ou nada.
type tetoDeTeste struct{ passa bool }

func (t tetoDeTeste) Permite(string) limite.Resultado { return limite.Resultado{Permitido: t.passa} }

// TestTeto: o teto de verdade deixa passar a rajada inteira, e o seguinte é
// descartado e contado, sem ocupar lugar na espera.
func TestTeto(t *testing.T) {
	c := NovaCentral([]Notificador{&meioDeTeste{}}, mudo())
	for i := range RajadaDeAlertas + 1 {
		c.Recebe(alertaDe(int64(i)))
	}
	d, ok := c.Descartes()
	if !ok || d.AcimaDoTeto != 1 || d.FilaCheia != 0 || len(c.espera) != RajadaDeAlertas {
		t.Errorf("descarte %+v e %d na espera, esperava 1 acima do teto e %d", d, len(c.espera), RajadaDeAlertas)
	}
	if c.Total() != RajadaDeAlertas+1 {
		t.Errorf("total %d: o descartado também conta no registro", c.Total())
	}
}

// TestEpisodioDoDescarte: os descartes somam no mesmo episódio enquanto o
// último tiver menos de AvisoDoDescarteDura; depois disso o episódio some, e
// o descarte seguinte começa outro, do zero. Só o primeiro de cada um vai ao
// log.
func TestEpisodioDoDescarte(t *testing.T) {
	var log strings.Builder
	c := NovaCentral([]Notificador{&meioDeTeste{}}, slog.New(slog.NewTextHandler(&log, nil)))
	c.teto = tetoDeTeste{passa: false}
	t0 := time.Date(2026, 10, 27, 14, 2, 0, 0, time.Local)
	agora := t0
	c.agora = func() time.Time { return agora }

	c.Recebe(alertaDe(1))
	agora = t0.Add(30 * time.Minute)
	c.Recebe(alertaDe(2))
	agora = agora.Add(AvisoDoDescarteDura)
	d, ok := c.Descartes()
	if !ok || d.AcimaDoTeto != 2 || !d.Desde.Equal(t0) || !d.Ultimo.Equal(t0.Add(30*time.Minute)) {
		t.Fatalf("descarte %+v (%v), esperava 2 desde %v", d, ok, t0)
	}
	if n := strings.Count(log.String(), "alertas deixaram de sair"); n != 1 {
		t.Errorf("%d linhas de log, esperava 1: %s", n, log.String())
	}

	agora = agora.Add(time.Second)
	if d, ok := c.Descartes(); ok {
		t.Fatalf("o episódio seguiu depois do AvisoDoDescarteDura: %+v", d)
	}
	c.Recebe(alertaDe(3))
	if d, _ := c.Descartes(); d.AcimaDoTeto != 1 || !d.Desde.Equal(agora) {
		t.Errorf("o episódio novo não começou do zero: %+v", d)
	}
	if n := strings.Count(log.String(), "alertas deixaram de sair"); n != 2 {
		t.Errorf("%d linhas de log, esperava 2", n)
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
