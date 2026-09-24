package logbuf

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestSoAvisoEErroEntram(t *testing.T) {
	var saida bytes.Buffer
	h := New(slog.NewTextHandler(&saida, nil), 10)
	log := slog.New(h)

	log.Info("índice carregado", "cam", "a")
	log.Warn("conexão caiu", "erro", errors.New("EOF"))
	log.Error("câmera parou de gravar")

	linhas, total := h.Recentes()
	if total != 2 || len(linhas) != 2 {
		t.Fatalf("guardou %d de total %d, esperava 2 e 2", len(linhas), total)
	}
	// Mais nova primeiro.
	if linhas[0].Nivel != "ERROR" || linhas[1].Texto != "conexão caiu erro=EOF" {
		t.Errorf("linhas %+v", linhas)
	}
	// O log de sempre continua saindo inteiro.
	if !strings.Contains(saida.String(), "índice carregado") {
		t.Error("o INFO sumiu do log de saída")
	}
}

func TestAnelDescartaOMaisVelho(t *testing.T) {
	h := New(slog.DiscardHandler, 3)
	log := slog.New(h)
	for _, m := range []string{"1", "2", "3", "4", "5"} {
		log.Warn(m)
	}
	linhas, total := h.Recentes()
	if total != 5 {
		t.Errorf("total %d, esperava 5", total)
	}
	var got []string
	for _, l := range linhas {
		got = append(got, l.Texto)
	}
	if strings.Join(got, ",") != "5,4,3" {
		t.Errorf("guardou %v, esperava 5,4,3", got)
	}
}

// O log.With("cam", ...) de cada recorder precisa gravar no mesmo anel, e com
// o atributo junto - é ele que diz de qual câmera é o erro.
func TestWithCompartilhaOAnel(t *testing.T) {
	h := New(slog.DiscardHandler, 5)
	slog.New(h).With("cam", "garagem").WithGroup("g").Warn("conexão caiu", "espera", "2s depois")

	linhas, _ := h.Recentes()
	if len(linhas) != 1 {
		t.Fatalf("guardou %d linhas", len(linhas))
	}
	if want := `conexão caiu cam=garagem g.espera="2s depois"`; linhas[0].Texto != want {
		t.Errorf("texto %q, esperava %q", linhas[0].Texto, want)
	}
}
