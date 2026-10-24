package alerta

import (
	"slices"
	"testing"
	"time"

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
		"horários que não valem": func(c *config.Camera) {
			c.Alert.Schedules = []config.HorarioDoAlerta{{Days: []time.Weekday{time.Monday}, From: "22:00", To: "06:00"}}
		},
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

// TestMarcaAsLinhas: a Avalia marca, nas linhas que o Recorder vai gravar, o
// objeto que passou, com o id das áreas, e não o nome. O que não passou fica
// como estava.
func TestMarcaAsLinhas(t *testing.T) {
	r := NovaRegra(camera([]detect.Familia{detect.Pessoa}, "esq", "chao"))
	linhas := []store.Evento{pessoaNaEsquerda, pessoaNaDireita, motoNaEsquerda}
	if _, ok := r.Avalia(linhas, nil); !ok {
		t.Fatal("não alertou")
	}
	esperado := []struct {
		alert bool
		areas []string
	}{{true, []string{"esq"}}, {true, []string{"chao"}}, {false, nil}}
	for i, e := range esperado {
		if linhas[i].Alert != e.alert || !slices.Equal(linhas[i].AlertAreas, e.areas) {
			t.Errorf("linha %d: alert %v em %v, esperava %v em %v", i,
				linhas[i].Alert, linhas[i].AlertAreas, e.alert, e.areas)
		}
	}

	// Sem áreas no alerta, a marca vem sem `alertAreas`.
	linhas = []store.Evento{pessoaNaDireita}
	NovaRegra(camera([]detect.Familia{detect.Pessoa})).Avalia(linhas, nil)
	if !linhas[0].Alert || linhas[0].AlertAreas != nil {
		t.Errorf("imagem inteira: alert %v em %v", linhas[0].Alert, linhas[0].AlertAreas)
	}
}

// quarta são os três horários da quarta do exemplo da doc, mais o sábado
// inteiro.
var quarta = []config.HorarioDoAlerta{
	{Days: []time.Weekday{time.Wednesday}, From: "00:00", To: "05:15"},
	{Days: []time.Weekday{time.Wednesday}, From: "12:00", To: "13:00"},
	{Days: []time.Weekday{time.Wednesday}, From: "18:30", To: "23:59"},
	{Days: []time.Weekday{time.Saturday}, From: "00:00", To: "23:59"},
}

// em é o instante, no fuso do servidor, de um dia da semana de outubro de
// 2026 (o dia 18 é um domingo), na hora, no minuto e no segundo dados.
func em(dia time.Weekday, hora, minuto, segundo int) int64 {
	return time.Date(2026, 10, 18+int(dia), hora, minuto, segundo, 0, time.Local).UnixMilli()
}

// TestNoHorario: o onset precisa cair num dos horários, com as duas pontas
// incluídas ao minuto, e fora deles nenhuma linha ganha a marca.
func TestNoHorario(t *testing.T) {
	casos := map[string]struct {
		instanteMs int64
		alerta     bool
	}{
		"de madrugada":             {em(time.Wednesday, 3, 0, 0), true},
		"no primeiro minuto":       {em(time.Wednesday, 0, 0, 0), true},
		"no último minuto":         {em(time.Wednesday, 5, 15, 59), true},
		"no minuto seguinte":       {em(time.Wednesday, 5, 16, 0), false},
		"entre dois horários":      {em(time.Wednesday, 11, 59, 59), false},
		"no De do segundo":         {em(time.Wednesday, 12, 0, 0), true},
		"no Até do segundo":        {em(time.Wednesday, 13, 0, 59), true},
		"depois do segundo":        {em(time.Wednesday, 13, 1, 0), false},
		"no fim do dia":            {em(time.Wednesday, 23, 59, 59), true},
		"na meia-noite seguinte":   {em(time.Thursday, 0, 0, 0), false},
		"na hora certa, outro dia": {em(time.Tuesday, 3, 0, 0), false},
		"no sábado inteiro":        {em(time.Saturday, 14, 30, 0), true},
	}
	for nome, c := range casos {
		t.Run(nome, func(t *testing.T) {
			cam := camera([]detect.Familia{detect.Pessoa})
			cam.Alert.Schedules = quarta
			linha := pessoaNaEsquerda
			linha.InstanteMs = c.instanteMs
			linhas := []store.Evento{linha}
			_, ok := NovaRegra(cam).Avalia(linhas, nil)
			if ok != c.alerta || linhas[0].Alert != c.alerta {
				t.Errorf("alertou %v, linha com alert %v; esperava %v", ok, linhas[0].Alert, c.alerta)
			}
		})
	}
}

// TestHorarioComAreas: dentro do horário, a área ainda decide; fora, nem a
// pessoa na área alerta.
func TestHorarioComAreas(t *testing.T) {
	cam := camera([]detect.Familia{detect.Pessoa}, "esq")
	cam.Alert.Schedules = quarta
	r := NovaRegra(cam)
	for _, c := range []struct {
		instanteMs int64
		objetos    int
	}{{em(time.Wednesday, 3, 0, 0), 1}, {em(time.Wednesday, 8, 0, 0), 0}} {
		linhas := []store.Evento{pessoaNaEsquerda, pessoaNaDireita}
		for i := range linhas {
			linhas[i].InstanteMs = c.instanteMs
		}
		a, _ := r.Avalia(linhas, nil)
		if len(a.Objetos) != c.objetos || linhas[0].Alert != (c.objetos > 0) || linhas[1].Alert {
			t.Errorf("às %s: objetos %+v, linhas %+v", time.UnixMilli(c.instanteMs).Format("15:04"), a.Objetos, linhas)
		}
	}
}
