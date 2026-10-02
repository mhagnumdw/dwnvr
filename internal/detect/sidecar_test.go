package detect

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func sidecarFalso(t *testing.T, f http.HandlerFunc) Detector {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return Detectores["sidecar"](srv.URL + "/")
}

func TestSidecarMandaOPedacoEOPisoELeOsAchados(t *testing.T) {
	pedaco := []byte("init+fragmentos")
	d := sidecarFalso(t, func(w http.ResponseWriter, r *http.Request) {
		corpo, _ := io.ReadAll(r.Body)
		switch {
		case r.Method != http.MethodPost || r.URL.Path != "/detect":
			t.Errorf("%s %s, esperado POST /detect", r.Method, r.URL.Path)
		case r.URL.Query().Get("piso") != "0.2":
			t.Errorf("piso %q, esperado o PisoDoDetector", r.URL.Query().Get("piso"))
		case !bytes.Equal(corpo, pedaco):
			t.Errorf("corpo %q, esperado o pedaço", corpo)
		}
		_, _ = io.WriteString(w, `{"achados":[{"classe":"person","score":0.87,"caixa":[0.1,0.2,0.3,0.4]}]}`)
	})
	visao, err := d.Olha(t.Context(), Pedaco{Fmp4: pedaco})
	if err != nil {
		t.Fatal(err)
	}
	want := Achado{Classe: "person", Score: 0.87, Caixa: [4]float64{0.1, 0.2, 0.3, 0.4}}
	if len(visao.Achados) != 1 || visao.Achados[0] != want {
		t.Errorf("achados %+v, esperado [%+v]", visao.Achados, want)
	}
}

// Olhada sem nada é resultado: é ela que conta como falta no rastreio. Não
// pode voltar nil, que é como se lê "não olhou".
// Vale também para a resposta sem o campo, ou com ele nulo.
func TestSidecarOlhadaVaziaEResultado(t *testing.T) {
	for _, corpo := range []string{`{"achados":[]}`, `{"achados":null}`, `{}`} {
		d := sidecarFalso(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, corpo)
		})
		visao, err := d.Olha(t.Context(), Pedaco{Fmp4: []byte("x")})
		if err != nil || visao.Achados == nil || len(visao.Achados) != 0 {
			t.Errorf("%s: achados %v, erro %v: esperado fatia vazia sem erro", corpo, visao.Achados, err)
		}
	}
}

func TestSidecarErroVoltaComOMotivo(t *testing.T) {
	d := sidecarFalso(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"erro":"pedaço não decodifica: nenhum quadro"}`)
	})
	_, err := d.Olha(t.Context(), Pedaco{Fmp4: []byte("x")})
	if err == nil || !strings.Contains(err.Error(), "nenhum quadro") || !strings.Contains(err.Error(), "422") {
		t.Errorf("erro %v: esperado o HTTP e o motivo do sidecar", err)
	}
	// 422 é pedaço ruim com o detector no ar - não pode virar "fora do ar".
	if !errors.Is(err, ErrPedacoRecusado) {
		t.Errorf("erro %v: um 4xx tinha que ser ErrPedacoRecusado", err)
	}
}

func TestSidecarErroDoServidorNaoERecusa(t *testing.T) {
	d := sidecarFalso(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"erro":"quebrou"}`)
	})
	_, err := d.Olha(t.Context(), Pedaco{Fmp4: []byte("x")})
	if err == nil || errors.Is(err, ErrPedacoRecusado) {
		t.Errorf("erro %v: 5xx é detector com problema, não pedaço recusado", err)
	}
}

func TestSidecarObedeceOPrazo(t *testing.T) {
	// O servidor falso fica mudo até o teste acabar. O Cleanup dele roda
	// depois deste (a ordem é inversa), então ele é solto antes de fechar.
	solta := make(chan struct{})
	d := sidecarFalso(t, func(w http.ResponseWriter, r *http.Request) {
		<-solta
	})
	t.Cleanup(func() { close(solta) })
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	inicio := time.Now()
	if _, err := d.Olha(ctx, Pedaco{Fmp4: []byte("x")}); err == nil {
		t.Fatal("sidecar mudo tinha que dar erro")
	}
	if d := time.Since(inicio); d > 2*time.Second {
		t.Errorf("esperou %v por um sidecar mudo", d)
	}
}

func TestSaudeDoSidecar(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/health" {
			t.Errorf("%s %s, esperado GET /health", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"modelo": "modelo.onnx", "entrada": "512x288", "threads": 2}`)
	}))
	defer srv.Close()
	s, err := SaudeDoSidecar(t.Context(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if s != (Saude{Modelo: "modelo.onnx", Entrada: "512x288", Threads: 2}) {
		t.Errorf("saúde %+v", s)
	}
}

// Fora do ar, o erro vem sem o `Get "<url>":` na frente: a tela mostra o
// endereço numa linha à parte.
func TestSaudeDoSidecarForaDoAr(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := SaudeDoSidecar(t.Context(), url)
	if err == nil || strings.Contains(err.Error(), "/health") {
		t.Errorf("erro %v, esperado só a falha de rede", err)
	}
}
