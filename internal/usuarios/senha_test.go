package usuarios

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSenhaGeradaConfere(t *testing.T) {
	ctx := context.Background()
	guardado, err := GerarSenha(ctx, "uma senha boa")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(guardado, "$pbkdf2-sha256$i=600000$") {
		t.Errorf("formato inesperado: %s", guardado)
	}
	if strings.Contains(guardado, "uma senha boa") {
		t.Error("a senha foi guardada como veio")
	}

	for senha, quer := range map[string]bool{"uma senha boa": true, "uma senha boA": false, "": false} {
		ok, err := ConferirSenha(ctx, senha, guardado)
		if err != nil || ok != quer {
			t.Errorf("senha %q: ok=%v err=%v, esperava %v", senha, ok, err, quer)
		}
	}
}

// Sem senha guardada, a conta é feita do mesmo jeito e a resposta é não. É o
// que impede o tempo de resposta do login de dizer se o usuário existe.
func TestSemSenhaGuardadaPagaAContaENaoEntra(t *testing.T) {
	ctx := context.Background()
	guardado, err := GerarSenha(ctx, "x")
	if err != nil {
		t.Fatal(err)
	}
	medir := func(guardado string) time.Duration {
		inicio := time.Now()
		if ok, _ := ConferirSenha(ctx, "x", guardado); ok && guardado == "" {
			t.Error("sem senha guardada, entrou")
		}
		return time.Since(inicio)
	}
	comSenha, semSenha := medir(guardado), medir("")
	if semSenha < comSenha/2 {
		t.Errorf("sem senha guardada levou %v, contra %v com senha: o tempo diz quem existe", semSenha, comSenha)
	}
}

func TestSenhaGuardadaMalformada(t *testing.T) {
	ruins := []string{
		"senha-em-texto",
		"$argon2id$v=19$m=7168,t=5,p=1$c2FsdA$aGFzaA",
		"$pbkdf2-sha256$600000$c2FsdA$aGFzaA",
		"$pbkdf2-sha256$i=1000$" + b64.EncodeToString([]byte("salt-de-16-bytes")) + "$" + b64.EncodeToString(make([]byte, 32)),
		"$pbkdf2-sha256$i=600000$!!!$" + b64.EncodeToString(make([]byte, 32)),
		"$pbkdf2-sha256$i=600000$c2FsdA$curto",
	}
	for _, g := range ruins {
		var err error
		if alg, errNome := algoritmoDe(g); errNome != nil {
			err = errNome
		} else {
			_, err = alg.Conferir("x", g)
		}
		if err == nil {
			t.Errorf("%q foi aceito", g)
		}
	}
}

// Uma conta por vez: a segunda espera a primeira, e quem desiste na fila sai
// sem fazer a conta.
func TestFilaUmaContaPorVez(t *testing.T) {
	vez <- struct{}{} // alguém fazendo a conta
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := ConferirSenha(ctx, "x", ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("com a vez ocupada, esperava desistir pelo prazo, veio %v", err)
	}
	<-vez

	var mu sync.Mutex
	dentro, maximo := 0, 0
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			_ = naVez(context.Background(), func() {
				mu.Lock()
				dentro++
				maximo = max(maximo, dentro)
				mu.Unlock()
				time.Sleep(10 * time.Millisecond)
				mu.Lock()
				dentro--
				mu.Unlock()
			})
		})
	}
	wg.Wait()
	if maximo != 1 {
		t.Errorf("%d contas ao mesmo tempo, esperava 1", maximo)
	}
}
