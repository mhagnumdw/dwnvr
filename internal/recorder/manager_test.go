package recorder

import (
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
)

// TestTetoDaFilaCresceComAsCameras: o teto que a tela de Diagnóstico mostra é
// o limite por câmera vezes as câmeras com detecção ligada - e a desligada não
// conta, porque não oferece nada.
func TestTetoDaFilaCresceComAsCameras(t *testing.T) {
	sim, nao := true, false
	m := &Manager{
		fila: detect.NovaFila(detect.Detectores["nenhum"](""), func(detect.Olhada) {}, mudo()),
		recs: map[string]*running{},
	}
	for id, d := range map[string]*bool{"a": &sim, "b": &sim, "c": &nao, "d": nil} {
		m.recs[id] = &running{rec: &Recorder{cam: config.Camera{ID: id, Detect: d}}}
	}
	if got, want := m.Detector().Fila.Cap, 2*detect.PedacosPorCamera; got != want {
		t.Errorf("teto %d, esperado %d: duas câmeras com detecção", got, want)
	}
}
