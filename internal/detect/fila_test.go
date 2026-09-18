package detect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// detectorDeTeste responde o que mandarem, e só quando o teste soltar.
type detectorDeTeste struct {
	mu     sync.Mutex
	vistos []string // câmera de cada pedaço olhado, na ordem
	solta  chan error
}

func (d *detectorDeTeste) Nome() string { return "teste" }
func (d *detectorDeTeste) Olha(ctx context.Context, p Pedaco) ([]Achado, error) {
	d.mu.Lock()
	d.vistos = append(d.vistos, p.Camera)
	d.mu.Unlock()
	select {
	case err := <-d.solta:
		return []Achado{}, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func mudo() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func pedacoDe(cam string) Pedaco { return Pedaco{Camera: cam, Fmp4: []byte("x")} }

func TestFilaGuardaUmPorCameraENaoBloqueia(t *testing.T) {
	d := &detectorDeTeste{solta: make(chan error)}
	f := novaFila(d, func(Olhada) {}, mudo(), 1, time.Minute, time.Millisecond)

	// Sem trabalhador rodando: nada sai da fila.
	if !f.Oferece(pedacoDe("a")) {
		t.Fatal("fila vazia recusou")
	}
	if f.Oferece(pedacoDe("a")) {
		t.Error("a mesma câmera entrou duas vezes: ela ocuparia a fila das outras")
	}
	if !f.Oferece(pedacoDe("b")) {
		t.Error("outra câmera tinha que entrar")
	}
	if f.Tamanho() != 2 {
		t.Errorf("tamanho %d, esperado 2", f.Tamanho())
	}
}

// TestFilaNaoTemTotal: a câmera agitada no limite dela não recusa a marca de
// nenhuma outra, por mais câmeras que haja - o teto da fila cresce com elas.
func TestFilaNaoTemTotal(t *testing.T) {
	f := novaFila(&detectorDeTeste{}, func(Olhada) {}, mudo(), 2, time.Minute, time.Millisecond)
	for i := range 40 {
		cam := fmt.Sprintf("cam%d", i)
		if !f.Oferece(pedacoDe(cam)) || !f.Oferece(pedacoDe(cam)) {
			t.Fatalf("%s recusada com %d esperando: não há total", cam, f.Tamanho())
		}
		if f.Oferece(pedacoDe(cam)) {
			t.Fatalf("%s entrou pela terceira vez: o limite é 2 por câmera", cam)
		}
	}
}

// TestFilaAtendeEmOrdemEDevolveAVaga: o trabalhador tira da fila ao começar a
// olhar, e é aí que a câmera pode oferecer de novo - o que está sendo olhado
// não está mais na fila.
func TestFilaAtendeEmOrdemEDevolveAVaga(t *testing.T) {
	d := &detectorDeTeste{solta: make(chan error)}
	entregues := make(chan Olhada, 10)
	f := novaFila(d, func(o Olhada) { entregues <- o }, mudo(), 1, time.Minute, time.Millisecond)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go f.Roda(ctx)

	f.Oferece(pedacoDe("a"))
	f.Oferece(pedacoDe("b"))
	espera(t, func() bool { return f.Tamanho() == 1 }) // "a" está sendo olhado
	if l := f.Estado().Fila.Cameras; !l["a"].Olhando || l["a"].Esperando != 0 || l["b"].Esperando != 1 {
		t.Fatalf("câmeras %+v: esperava \"a\" sendo olhado e \"b\" esperando", l)
	}
	if !f.Oferece(pedacoDe("a")) {
		t.Fatal("a vaga de \"a\" não voltou quando o pedaço dele saiu da fila")
	}
	for range 3 {
		d.solta <- nil
		o := <-entregues
		if o.Pedaco.Fmp4 != nil {
			t.Error("a olhada devolvida ainda segura os bytes do vídeo")
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if got := d.vistos; len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "a" {
		t.Errorf("ordem %v, esperado [a b a]", got)
	}
}

// TestFilaPausaDepoisDeFalha: sidecar fora do ar não vira laço apertado, e o
// erro chega a quem recebe - é ele que conta a falha no funil.
func TestFilaPausaDepoisDeFalha(t *testing.T) {
	d := &detectorDeTeste{solta: make(chan error, 10)}
	entregues := make(chan Olhada, 10)
	pausa := 200 * time.Millisecond
	f := novaFila(d, func(o Olhada) { entregues <- o }, mudo(), 1, time.Minute, pausa)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go f.Roda(ctx)

	fora := errors.New("connection refused")
	d.solta <- fora
	d.solta <- nil
	f.Oferece(pedacoDe("a"))
	f.Oferece(pedacoDe("b"))

	o := <-entregues
	if !errors.Is(o.Erro, fora) {
		t.Fatalf("erro %v, esperado o do detector", o.Erro)
	}
	inicio := time.Now()
	<-entregues
	if d := time.Since(inicio); d < pausa/2 {
		t.Errorf("a segunda olhada saiu %v depois da falha, antes da pausa de %v", d, pausa)
	}
}

// TestFilaNaoPausaPorPedacoRecusado: vídeo corrompido que a câmera mandou é
// rotina em algumas câmeras, e não pode parar a fila de todas por 30 s.
func TestFilaNaoPausaPorPedacoRecusado(t *testing.T) {
	d := &detectorDeTeste{solta: make(chan error, 10)}
	entregues := make(chan Olhada, 10)
	f := novaFila(d, func(o Olhada) { entregues <- o }, mudo(), 1, time.Minute, time.Hour)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go f.Roda(ctx)

	d.solta <- fmt.Errorf("%w: HTTP 422", ErrPedacoRecusado)
	d.solta <- nil
	f.Oferece(pedacoDe("a"))
	f.Oferece(pedacoDe("b"))
	<-entregues
	select {
	case <-entregues:
	case <-time.After(5 * time.Second):
		t.Fatal("a fila pausou depois de um pedaço recusado")
	}
	if f.foraDoAr {
		t.Error("pedaço recusado marcou o detector como fora do ar")
	}
}

func TestFilaDesisteDoDetectorTravado(t *testing.T) {
	d := &detectorDeTeste{solta: make(chan error)} // nunca solta
	entregues := make(chan Olhada, 1)
	f := novaFila(d, func(o Olhada) { entregues <- o }, mudo(), 1, 50*time.Millisecond, time.Millisecond)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go f.Roda(ctx)
	f.Oferece(pedacoDe("a"))
	select {
	case o := <-entregues:
		if !errors.Is(o.Erro, context.DeadlineExceeded) {
			t.Errorf("erro %v, esperado o prazo", o.Erro)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a fila esperou o detector travado para sempre")
	}
}

func espera(t *testing.T, cond func() bool) {
	t.Helper()
	for range 500 {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("a condição não aconteceu")
}

// TestFilaGuardaPicoETempos: o que a tela de Diagnóstico mostra da fila. O pico
// fica depois que ela esvazia, e a análise não conta a olhada que falhou - um
// prazo esgotado diria que o detector demora 60 s, quando ele caiu.
func TestFilaGuardaPicoETempos(t *testing.T) {
	d := &detectorDeTeste{solta: make(chan error)}
	entregues := make(chan Olhada, 10)
	f := novaFila(d, func(o Olhada) { entregues <- o }, mudo(), 1, time.Minute, time.Millisecond)

	if e := f.Estado(); e.Fila.Agora != 0 || e.Tempos.AnaliseMs != 0 {
		t.Fatalf("fila nova: %+v", e)
	}
	f.Oferece(pedacoDe("a"))
	f.Oferece(pedacoDe("b"))
	f.Oferece(pedacoDe("c"))
	time.Sleep(30 * time.Millisecond) // a espera de cada um na fila

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go f.Roda(ctx)

	espera(t, func() bool { return f.Tamanho() == 2 }) // "a" está sendo olhado
	time.Sleep(20 * time.Millisecond)                  // o tempo da análise
	d.solta <- nil
	<-entregues
	d.solta <- errors.New("fora do ar")
	<-entregues

	e := f.Estado()
	if e.Fila.Pico != 3 {
		t.Errorf("pico %d, esperado 3", e.Fila.Pico)
	}
	if e.Tempos.AnaliseMs < 20 || e.Tempos.AnaliseMs > 1000 {
		t.Errorf("análise %d ms: esperava a da olhada que respondeu, ~20 ms", e.Tempos.AnaliseMs)
	}
	if e.Tempos.EsperaMs < 30 {
		t.Errorf("espera %d ms, esperado ao menos 30", e.Tempos.EsperaMs)
	}
}
