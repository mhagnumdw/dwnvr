package retention

import (
	"testing"
	"time"
)

// O "desde" do disco abaixo do mínimo fica preso na primeira passada que viu o
// problema, e some na primeira que o vê resolvido.
func TestAbaixoDoMinimoDesde(t *testing.T) {
	m := &Manager{}
	t0 := time.Now()

	m.anotaDisco(false, t0)
	if !m.AbaixoDoMinimoDesde().IsZero() {
		t.Fatal("disco acima do mínimo não deveria ter desde")
	}

	m.anotaDisco(true, t0.Add(time.Minute))
	m.anotaDisco(true, t0.Add(2*time.Minute))
	if got := m.AbaixoDoMinimoDesde(); !got.Equal(t0.Add(time.Minute)) {
		t.Errorf("desde %v, esperava a primeira passada abaixo (%v)", got, t0.Add(time.Minute))
	}

	m.anotaDisco(false, t0.Add(3*time.Minute))
	if !m.AbaixoDoMinimoDesde().IsZero() {
		t.Error("o disco voltou e o desde ficou")
	}
}
