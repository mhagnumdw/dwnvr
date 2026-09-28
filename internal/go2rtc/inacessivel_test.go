package go2rtc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
)

// Sem resposta nenhuma, o go2rtc fica inacessível desde a primeira falha; as
// seguintes só trocam a mensagem. Qualquer resposta, até um 500, apaga o estado:
// quem respondeu está no ar.
func TestInacessivelDesdeAPrimeiraFalha(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // porta fechada: o Do falha sem resposta

	c := New(config.Go2RTC{URL: url})
	ctx := context.Background()

	_, _ = c.Versao(ctx)
	desde, erro := c.Inacessivel()
	if desde.IsZero() || erro == "" {
		t.Fatalf("falha de transporte não marcou: desde %v, erro %q", desde, erro)
	}
	time.Sleep(time.Millisecond)
	_, _ = c.Versao(ctx)
	if d, _ := c.Inacessivel(); !d.Equal(desde) {
		t.Errorf("a segunda falha moveu o desde: %v -> %v", desde, d)
	}

	// O go2rtc volta, respondendo erro HTTP: continua sendo resposta.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv2.Close()
	c.BaseURL = srv2.URL
	_, _ = c.Versao(ctx)
	if d, e := c.Inacessivel(); !d.IsZero() || e != "" {
		t.Errorf("respondeu 500 e ficou inacessível: desde %v, erro %q", d, e)
	}
}

// O dwnvr desistindo da requisição (recorder parando, tela fechada) não diz
// nada sobre o go2rtc.
func TestInacessivelIgnoraCancelamento(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := New(config.Go2RTC{URL: srv.URL})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	if _, err := c.Versao(ctx); err == nil {
		t.Fatal("a requisição cancelada deveria falhar")
	}
	if d, e := c.Inacessivel(); !d.IsZero() {
		t.Errorf("cancelamento marcou inacessível: desde %v, erro %q", d, e)
	}
}
