package alerta

import (
	"slices"
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/area"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// metadeEsquerda e chaoDoFundo são duas áreas que se cruzam: a primeira é a
// metade esquerda do quadro; a segunda, a faixa de baixo dele inteira.
var (
	metadeEsquerda = config.Area{ID: "esq", Name: "Portão",
		Points: []area.Ponto{{0, 0}, {0.5, 0}, {0.5, 1}, {0, 1}}}
	chaoDoFundo = config.Area{ID: "chao", Name: "Calçada", Rule: "touch",
		Points: []area.Ponto{{0, 0.8}, {1, 0.8}, {1, 1}, {0, 1}}}
)

// camera devolve uma câmera que alerta, como o Resolve a entrega.
func camera(familias []detect.Familia, areas ...string) config.Camera {
	sim := true
	return config.Camera{ID: "cam_a", Name: "Frente", Enabled: true, Detect: &sim,
		Areas: []config.Area{metadeEsquerda, chaoDoFundo},
		Alert: &config.AlertaDaCamera{Enabled: true, Families: familias, Areas: areas}}
}

// marca é uma linha gravada pelo Recorder, no onset 1000 e no quadro 2400.
func marca(familia, classe string, score float64, caixa [4]float64) store.Evento {
	return store.Evento{InstanteMs: 1000, QuadroMs: 2400, TemQuadro: true,
		Familia: familia, Classe: classe, Score: score, Caixa: &caixa}
}

var (
	// pessoaNaEsquerda tem o pé em (0,3; 0,6): na metade esquerda.
	pessoaNaEsquerda = marca("pessoa", "person", 0.87, [4]float64{0.25, 0.2, 0.35, 0.6})
	// pessoaNaDireita tem o pé em (0,8; 0,9): na faixa de baixo, à direita.
	pessoaNaDireita = marca("pessoa", "person", 0.55, [4]float64{0.75, 0.5, 0.85, 0.9})
	// motoNaEsquerda tem o pé em (0,2; 0,7).
	motoNaEsquerda = marca("veiculo", "motorcycle", 0.71, [4]float64{0.1, 0.4, 0.3, 0.7})
)

// TestRegraSoNasceComAlertaLigado: o nil é o "sem alerta", e cobre tudo o que
// impede a câmera de ter olhada ou de alertar.
func TestRegraSoNasceComAlertaLigado(t *testing.T) {
	nao := false
	casos := map[string]func(c *config.Camera){
		"sem alerta":          func(c *config.Camera) { c.Alert = nil },
		"alerta desligado":    func(c *config.Camera) { c.Alert.Enabled = false },
		"sem família":         func(c *config.Camera) { c.Alert.Families = nil },
		"câmera desabilitada": func(c *config.Camera) { c.Enabled = false },
		"sem marcação":        func(c *config.Camera) { c.Detect = &nao },
		"marcação ausente":    func(c *config.Camera) { c.Detect = nil },
		"áreas que sumiram":   func(c *config.Camera) { c.Alert.Areas = []string{"sumiu"} },
	}
	for nome, muda := range casos {
		t.Run(nome, func(t *testing.T) {
			cam := camera([]detect.Familia{detect.Pessoa}, "esq")
			muda(&cam)
			if r := NovaRegra(cam); r != nil {
				t.Errorf("regra %+v, esperava nil", r)
			}
		})
	}
	if NovaRegra(camera([]detect.Familia{detect.Pessoa})) == nil {
		t.Error("a câmera com alerta ligado ficou sem regra")
	}
}

// TestNaImagemInteira: sem áreas, passa toda marca de uma família do alerta,
// e o objeto não leva área. A moto não é da família e fica de fora.
func TestNaImagemInteira(t *testing.T) {
	r := NovaRegra(camera([]detect.Familia{detect.Pessoa}))
	jpeg := []byte("jpeg")
	a, ok := r.Avalia([]store.Evento{pessoaNaEsquerda, pessoaNaDireita, motoNaEsquerda}, jpeg)
	if !ok {
		t.Fatal("não alertou")
	}
	if a.Camera != "cam_a" || a.NomeDaCamera != "Frente" || a.InstanteMs != 1000 || a.QuadroMs != 2400 ||
		!a.TemQuadro || string(a.Quadro) != "jpeg" {
		t.Errorf("alerta %+v", a)
	}
	// Duas pessoas na mesma olhada são um alerta só, com as duas.
	if len(a.Objetos) != 2 || a.Objetos[0].Score != 0.87 || a.Objetos[1].Score != 0.55 {
		t.Fatalf("objetos %+v, esperava as duas pessoas", a.Objetos)
	}
	o := a.Objetos[0]
	if o.Familia != "pessoa" || o.Classe != "person" || o.Caixa != *pessoaNaEsquerda.Caixa || o.Areas != nil {
		t.Errorf("objeto %+v", o)
	}
}

// TestNasAreas: só passa quem cai numa das áreas do alerta, cada área pela
// própria regra, e o objeto leva o nome de todas em que caiu.
func TestNasAreas(t *testing.T) {
	casos := map[string]struct {
		areas  []string
		marcas []store.Evento
		// esperados: o score de cada objeto que passa, e as áreas dele.
		scores  []float64
		areasDe [][]string
	}{
		"pé na área": {
			[]string{"esq"}, []store.Evento{pessoaNaEsquerda, pessoaNaDireita},
			[]float64{0.87}, [][]string{{"Portão"}},
		},
		"ninguém na área": {
			[]string{"esq"}, []store.Evento{pessoaNaDireita}, nil, nil,
		},
		"encosta, sem pisar": {
			// A caixa da pessoa da esquerda vai até 0,6 e não encosta na
			// faixa de 0,8; a da direita encosta.
			[]string{"chao"}, []store.Evento{pessoaNaEsquerda, pessoaNaDireita},
			[]float64{0.55}, [][]string{{"Calçada"}},
		},
		"nas duas": {
			[]string{"esq", "chao"},
			[]store.Evento{marca("pessoa", "person", 0.9, [4]float64{0.2, 0.5, 0.3, 0.95})},
			[]float64{0.9}, [][]string{{"Portão", "Calçada"}},
		},
	}
	for nome, c := range casos {
		t.Run(nome, func(t *testing.T) {
			r := NovaRegra(camera([]detect.Familia{detect.Pessoa}, c.areas...))
			a, ok := r.Avalia(c.marcas, nil)
			if ok != (len(c.scores) > 0) {
				t.Fatalf("alertou: %v, objetos %+v", ok, a.Objetos)
			}
			if len(a.Objetos) != len(c.scores) {
				t.Fatalf("objetos %+v, esperava %v", a.Objetos, c.scores)
			}
			for i, o := range a.Objetos {
				if o.Score != c.scores[i] || !slices.Equal(o.Areas, c.areasDe[i]) {
					t.Errorf("objeto %d: %+v, esperava score %v nas áreas %v", i, o, c.scores[i], c.areasDe[i])
				}
			}
		})
	}
}

// TestVariasFamilias: com pessoa e veículo, os dois passam no mesmo alerta.
func TestVariasFamilias(t *testing.T) {
	r := NovaRegra(camera([]detect.Familia{detect.Pessoa, detect.Veiculo}, "esq"))
	a, ok := r.Avalia([]store.Evento{pessoaNaEsquerda, motoNaEsquerda}, nil)
	if !ok || len(a.Objetos) != 2 || a.Objetos[1].Familia != "veiculo" {
		t.Errorf("alertou: %v, objetos %+v", ok, a.Objetos)
	}
}
