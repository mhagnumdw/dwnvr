package limite

import (
	"testing"
	"time"
)

// relogio é um relógio parado, que o teste avança à mão.
type relogio struct{ t time.Time }

// agora é o instante em que o relógio está parado.
func (r *relogio) agora() time.Time { return r.t }

// anda avança o relógio em d.
func (r *relogio) anda(d time.Duration) { r.t = r.t.Add(d) }

// novoRelogio é um relógio parado num instante fixo.
func novoRelogio() *relogio {
	return &relogio{t: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)}
}

// passa registra uma requisição da chave k e falha o teste se ela for
// recusada.
func passa(t *testing.T, g *GCRA, k string) {
	t.Helper()
	if r := g.Permite(k); !r.Permitido {
		t.Fatalf("%s: recusada, esperava passar (espera %s)", k, r.Espera)
	}
}

// A rajada passa inteira; a seguinte é recusada com a espera de um
// intervalo, e depois dele passa uma só.
func TestRajadaEDepoisORitmo(t *testing.T) {
	rel := novoRelogio()
	g := novoGCRA(2, 5, rel.agora) // um a cada 30 s

	for range 5 {
		passa(t, g, "a")
	}
	r := g.Permite("a")
	if r.Permitido || r.Espera != 30*time.Second || !r.PrimeiraRecusa {
		t.Fatalf("sexta: %+v, esperava recusa de 30s, a primeira", r)
	}

	rel.anda(10 * time.Second)
	r = g.Permite("a")
	if r.Permitido || r.Espera != 20*time.Second || r.PrimeiraRecusa {
		t.Fatalf("durante a espera: %+v, esperava recusa de 20s, não a primeira", r)
	}

	rel.anda(20 * time.Second)
	passa(t, g, "a")
	if r := g.Permite("a"); r.Permitido || r.Espera != 30*time.Second || !r.PrimeiraRecusa {
		t.Fatalf("logo depois da que passou: %+v, esperava nova recusa de 30s", r)
	}
}

// Insistir durante a espera não a estica: a recusada não conta.
func TestRecusaNaoEstica(t *testing.T) {
	rel := novoRelogio()
	g := novoGCRA(2, 1, rel.agora)
	passa(t, g, "a")
	for range 100 {
		g.Permite("a")
	}
	rel.anda(30 * time.Second)
	passa(t, g, "a")
}

// Parado, o cliente recupera uma vaga por intervalo, até a rajada inteira.
func TestAsVagasVoltam(t *testing.T) {
	rel := novoRelogio()
	g := novoGCRA(2, 5, rel.agora)
	for range 5 {
		passa(t, g, "a")
	}
	rel.anda(time.Minute) // duas vagas
	passa(t, g, "a")
	passa(t, g, "a")
	if r := g.Permite("a"); r.Permitido {
		t.Fatal("terceira depois de 1 min passou; esperava só duas")
	}

	rel.anda(time.Hour)
	for range 5 {
		passa(t, g, "a")
	}
	if r := g.Permite("a"); r.Permitido {
		t.Fatal("parado muito tempo, a rajada passou de 5")
	}
}

// Cada chave tem o seu ritmo: quem passou do limite não trava os outros.
func TestChavesSeparadas(t *testing.T) {
	rel := novoRelogio()
	g := novoGCRA(2, 2, rel.agora)
	passa(t, g, "a")
	passa(t, g, "a")
	if g.Permite("a").Permitido {
		t.Fatal("a passou da rajada")
	}
	passa(t, g, "b")
	passa(t, g, "b")
}

// A limpeza apaga quem recuperou a rajada inteira e mantém quem ainda deve.
func TestLimpeza(t *testing.T) {
	rel := novoRelogio()
	g := novoGCRA(60, 5, rel.agora) // um por segundo
	passa(t, g, "parado")
	for range 5 {
		passa(t, g, "devendo")
	}

	rel.anda(LimpezaACada - time.Second)
	for range 70 {
		g.Permite("devendo") // adianta o TAT dele além do minuto
	}
	if n := len(g.clientes); n != 2 {
		t.Fatalf("antes da hora da limpeza: %d clientes, esperava 2", n)
	}

	rel.anda(time.Second)
	g.Permite("devendo")
	if _, ok := g.clientes["parado"]; ok {
		t.Error("a limpeza manteve quem já tinha a rajada inteira")
	}
	if _, ok := g.clientes["devendo"]; !ok {
		t.Error("a limpeza apagou quem ainda estava devendo")
	}
}
