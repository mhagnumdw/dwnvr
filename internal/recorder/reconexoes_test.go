package recorder

import (
	"testing"
	"time"
)

// A janela conta só as reconexões recentes, e a lista não guarda as velhas: com
// um enlace ruim por dias ela cresceria sem fim.
func TestReconexoesNaJanela(t *testing.T) {
	t0 := time.Now()
	r := &Recorder{contandoDesde: t0}

	r.contaReconexao(t0)
	r.contaReconexao(t0.Add(time.Hour))
	agora := t0.Add(JanelaDeReconexoes + 30*time.Minute)
	r.contaReconexao(agora)

	if r.reconexoes != 3 {
		t.Errorf("total: %d, esperava 3", r.reconexoes)
	}
	if n := r.reconexoesNaJanelaLocked(agora); n != 2 {
		t.Errorf("na janela: %d, esperava 2 (a primeira já saiu)", n)
	}
	if len(r.reconexoesRecentes) != 2 {
		t.Errorf("a lista guardou %d instantes, esperava só os 2 da janela", len(r.reconexoesRecentes))
	}
	if !r.ultimaReconexao.Equal(agora) {
		t.Errorf("última: %v, esperava %v", r.ultimaReconexao, agora)
	}
}

// Zerar recomeça tudo a partir de agora: total, janela, última e o "desde".
func TestZerarReconexoes(t *testing.T) {
	t0 := time.Now()
	r := &Recorder{contandoDesde: t0}
	r.contaReconexao(t0.Add(time.Minute))
	r.contaReconexao(t0.Add(2 * time.Minute))

	zerou := t0.Add(3 * time.Minute)
	r.zerarReconexoes(zerou)

	if r.reconexoes != 0 || r.reconexoesNaJanelaLocked(zerou) != 0 || !r.ultimaReconexao.IsZero() {
		t.Errorf("sobrou contagem: total %d, janela %d, última %v",
			r.reconexoes, r.reconexoesNaJanelaLocked(zerou), r.ultimaReconexao)
	}
	if !r.contandoDesde.Equal(zerou) {
		t.Errorf("contando desde %v, esperava %v", r.contandoDesde, zerou)
	}
}

// O "desconectada desde" é o instante da queda. As tentativas que falham depois
// dela não podem empurrá-lo para frente, senão a tela diria sempre "há 5s".
func TestDesconectadaDesdeMarcaSoATransicao(t *testing.T) {
	r := &Recorder{}

	r.setConnected(false, "recusou")
	primeira := r.disconnectedAt
	if primeira.IsZero() {
		t.Fatal("a primeira falha, antes de conectar, deveria marcar")
	}
	time.Sleep(time.Millisecond)
	r.setConnected(false, "recusou de novo")
	if !r.disconnectedAt.Equal(primeira) {
		t.Errorf("a segunda falha moveu o desde: %v -> %v", primeira, r.disconnectedAt)
	}

	r.setConnected(true, "")
	if !r.disconnectedAt.IsZero() {
		t.Errorf("conectada, mas disconnectedAt ficou %v", r.disconnectedAt)
	}

	time.Sleep(time.Millisecond)
	r.setConnected(false, "caiu")
	if !r.disconnectedAt.After(primeira) {
		t.Errorf("a queda nova deveria marcar um desde novo, ficou %v", r.disconnectedAt)
	}
}
