package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/area"
)

// quadrado é uma área válida qualquer.
var quadrado = []area.Ponto{{0.1, 0.1}, {0.5, 0.1}, {0.5, 0.5}, {0.1, 0.5}}

// TestAreaSemIDGanhaUmNoBoot: a área escrita à mão sem id ganha um, e o id que
// já existe não muda.
func TestAreaSemIDGanhaUmNoBoot(t *testing.T) {
	cfg, cams, avisos, err := carregaComAvisos(t, "", `[{"id":"cam_a","areas":[
		{"name":"Portão","points":[[0.1,0.1],[0.5,0.1],[0.5,0.5]]},
		{"id":"k3f9a2","name":"Garagem","rule":"touch","points":[[0.1,0.1],[0.5,0.1],[0.5,0.5]]}]}]`)
	if err != nil || len(avisos) > 0 {
		t.Fatalf("erro %v, avisos %+v", err, avisos)
	}
	as := cams[0].Areas
	if len(as) != 2 {
		t.Fatalf("%d áreas, esperava 2: %+v", len(as), as)
	}
	if len(as[0].ID) != tamanhoDoIDDaArea || !idDaAreaValido(as[0].ID) || as[0].ID == "k3f9a2" {
		t.Errorf("id gerado %q", as[0].ID)
	}
	if as[1].ID != "k3f9a2" {
		t.Errorf("o id que existia virou %q", as[1].ID)
	}

	// A regra ausente só se preenche no Resolve, e na cópia dele.
	r := cfg.Resolve(cams[0])
	if r.Areas[0].Rule != area.RegraDaAreaPadrao || r.Areas[1].Rule != "touch" {
		t.Errorf("regras resolvidas %q e %q", r.Areas[0].Rule, r.Areas[1].Rule)
	}
	if cams[0].Areas[0].Rule != "" {
		t.Errorf("o Resolve escreveu %q na câmera crua", cams[0].Areas[0].Rule)
	}
}

// TestIDsGeradosNaoRepetem: oito áreas sem id numa câmera saem com oito ids
// diferentes, e a câmera de entrada não muda.
func TestIDsGeradosNaoRepetem(t *testing.T) {
	cam := Camera{ID: "cam_a"}
	for i := range area.MaxAreasPorCamera {
		cam.Areas = append(cam.Areas, Area{Name: fmt.Sprint("área ", i), Points: quadrado})
	}
	nova := cam.ComIDsNasAreas()
	vistos := map[string]bool{}
	for _, a := range nova.Areas {
		if vistos[a.ID] {
			t.Errorf("id %q repetido", a.ID)
		}
		vistos[a.ID] = true
	}
	if cam.Areas[0].ID != "" {
		t.Error("ComIDsNasAreas mexeu na câmera de entrada")
	}
}

// TestAreaRuimSaiComAviso: cada área inválida sai com um aviso que diz o que é
// aceito, e a válida fica. No boot é isso; na API, o mesmo aviso vira a
// recusa.
func TestAreaRuimSaiComAviso(t *testing.T) {
	boa := Area{ID: "boa", Name: "Portão", Points: quadrado}
	casos := map[string]struct {
		ruim   Area
		aceito string
	}{
		"nome vazio":     {Area{ID: "x1", Name: "  ", Points: quadrado}, "nome"},
		"nome repetido":  {Area{ID: "x1", Name: " portão ", Points: quadrado}, "não se repita"},
		"id repetido":    {Area{ID: "boa", Name: "Outra", Points: quadrado}, "id que não se repita"},
		"id com barra":   {Area{ID: "a/b", Name: "Outra", Points: quadrado}, "letras minúsculas"},
		"id vazio":       {Area{Name: "Outra", Points: quadrado}, "letras minúsculas"},
		"regra estranha": {Area{ID: "x1", Name: "Outra", Rule: "inside", Points: quadrado}, "foot ou touch"},
		"dois pontos":    {Area{ID: "x1", Name: "Outra", Points: quadrado[:2]}, "pontos"},
		"fora do quadro": {Area{ID: "x1", Name: "Outra", Points: []area.Ponto{{0, 0}, {1.2, 0}, {0, 1}}}, "de 0 a 1"},
	}
	cfg := &Config{}
	for nome, c := range casos {
		t.Run(nome, func(t *testing.T) {
			cam, avisos := cfg.ConfereCamera(Camera{ID: "cam_a", Areas: []Area{boa, c.ruim}})
			if len(cam.Areas) != 1 || cam.Areas[0].ID != "boa" {
				t.Errorf("ficaram %+v, esperava só a boa", cam.Areas)
			}
			if len(avisos) != 1 {
				t.Fatalf("%d avisos: %+v", len(avisos), avisos)
			}
			a := avisos[0]
			if a.Camera != "cam_a" || a.Campo != "areas[1]" || !strings.Contains(a.Aceito, c.aceito) {
				t.Errorf("aviso %+v, esperava areas[1] com %q", a, c.aceito)
			}
		})
	}
}

// TestAreasDemaisSaem: passando do teto, as que sobram saem.
func TestAreasDemaisSaem(t *testing.T) {
	cam := Camera{ID: "cam_a"}
	for i := range area.MaxAreasPorCamera + 2 {
		cam.Areas = append(cam.Areas, Area{ID: fmt.Sprint("a", i), Name: fmt.Sprint("área ", i), Points: quadrado})
	}
	cam, avisos := (&Config{}).ConfereCamera(cam)
	if len(cam.Areas) != area.MaxAreasPorCamera || len(avisos) != 2 {
		t.Errorf("%d áreas e %d avisos, esperava %d e 2", len(cam.Areas), len(avisos), area.MaxAreasPorCamera)
	}
}

// TestSemAreasNaoAvisa: a câmera sem áreas, que é toda câmera de hoje, não
// ganha nada no JSON.
func TestSemAreasNaoAvisa(t *testing.T) {
	cam, avisos := (&Config{}).ConfereCamera(Camera{ID: "cam_a"})
	if cam.Areas != nil || len(avisos) > 0 {
		t.Errorf("áreas %+v, avisos %+v", cam.Areas, avisos)
	}
}
