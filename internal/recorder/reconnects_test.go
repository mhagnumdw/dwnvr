package recorder

import (
	"testing"
	"time"
)

// A janela conta só as reconexões recentes, e a lista não guarda as velhas: com
// um enlace ruim por dias ela cresceria sem fim.
func TestReconnectsInWindow(t *testing.T) {
	t0 := time.Now()
	r := &Recorder{countingSince: t0}

	r.countReconnect(t0)
	r.countReconnect(t0.Add(time.Hour))
	now := t0.Add(ReconnectWindow + 30*time.Minute)
	r.countReconnect(now)

	if r.reconnects != 3 {
		t.Errorf("total: %d, esperava 3", r.reconnects)
	}
	if n := r.reconnectsInWindowLocked(now); n != 2 {
		t.Errorf("na janela: %d, esperava 2 (a primeira já saiu)", n)
	}
	if len(r.recentReconnects) != 2 {
		t.Errorf("a lista guardou %d instantes, esperava só os 2 da janela", len(r.recentReconnects))
	}
	if !r.lastReconnect.Equal(now) {
		t.Errorf("última: %v, esperava %v", r.lastReconnect, now)
	}
}

// Zerar recomeça tudo a partir de agora: total, janela, última e o "desde".
func TestResetReconnects(t *testing.T) {
	t0 := time.Now()
	r := &Recorder{countingSince: t0}
	r.countReconnect(t0.Add(time.Minute))
	r.countReconnect(t0.Add(2 * time.Minute))

	wasReset := t0.Add(3 * time.Minute)
	r.resetReconnects(wasReset)

	if r.reconnects != 0 || r.reconnectsInWindowLocked(wasReset) != 0 || !r.lastReconnect.IsZero() {
		t.Errorf("sobrou contagem: total %d, janela %d, última %v",
			r.reconnects, r.reconnectsInWindowLocked(wasReset), r.lastReconnect)
	}
	if !r.countingSince.Equal(wasReset) {
		t.Errorf("contando desde %v, esperava %v", r.countingSince, wasReset)
	}
}

// O "desconectada desde" é o instante da queda. As tentativas que falham depois
// dela não podem empurrá-lo para frente, senão a tela diria sempre "há 5s".
func TestDisconnectedSinceMarksOnlyTheTransition(t *testing.T) {
	r := &Recorder{}

	r.setConnected(false, "recusou")
	first := r.disconnectedAt
	if first.IsZero() {
		t.Fatal("a primeira falha, antes de conectar, deveria marcar")
	}
	time.Sleep(time.Millisecond)
	r.setConnected(false, "recusou de novo")
	if !r.disconnectedAt.Equal(first) {
		t.Errorf("a segunda falha moveu o desde: %v -> %v", first, r.disconnectedAt)
	}

	r.setConnected(true, "")
	if !r.disconnectedAt.IsZero() {
		t.Errorf("conectada, mas disconnectedAt ficou %v", r.disconnectedAt)
	}

	time.Sleep(time.Millisecond)
	r.setConnected(false, "caiu")
	if !r.disconnectedAt.After(first) {
		t.Errorf("a queda nova deveria marcar um desde novo, ficou %v", r.disconnectedAt)
	}
}
