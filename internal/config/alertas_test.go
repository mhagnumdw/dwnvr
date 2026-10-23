package config

import (
	"slices"
	"strings"
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/detect"
)

// portao é uma área válida, para o alerta apontar.
var portao = Area{ID: "k3f9a2", Name: "Portão", Points: quadrado}

// TestAlertaValeNoBoot: o alerta escrito à mão, com uma área que existe, sobe
// como está.
func TestAlertaValeNoBoot(t *testing.T) {
	_, cams, avisos, err := carregaComAvisos(t, "", `[{"id":"cam_a",
		"areas":[{"id":"k3f9a2","name":"Portão","points":[[0.1,0.1],[0.5,0.1],[0.5,0.5]]}],
		"alert":{"enabled":true,"families":["pessoa","veiculo"],"areas":["k3f9a2"]}}]`)
	if err != nil || len(avisos) > 0 {
		t.Fatalf("erro %v, avisos %+v", err, avisos)
	}
	al := cams[0].Alert
	if al == nil || !al.Enabled || !slices.Equal(al.Families, []detect.Familia{detect.Pessoa, detect.Veiculo}) ||
		!slices.Equal(al.Areas, []string{"k3f9a2"}) {
		t.Errorf("alerta %+v", al)
	}
}

// TestAlertaRuimSaiComAviso: a família ou a área que não vale sai com um aviso
// que diz o que é aceito, e o resto do alerta fica ligado. No boot é isso; na
// API, o mesmo aviso vira a recusa.
func TestAlertaRuimSaiComAviso(t *testing.T) {
	casos := map[string]struct {
		alerta   AlertaDaCamera
		campo    string
		aceito   string
		familias []detect.Familia
		areas    []string
	}{
		"família desconhecida": {
			AlertaDaCamera{Enabled: true, Families: []detect.Familia{detect.Pessoa, "gente"}},
			"alert.families[1]", "pessoa, veiculo, animal", []detect.Familia{detect.Pessoa}, nil,
		},
		"família repetida": {
			AlertaDaCamera{Enabled: true, Families: []detect.Familia{detect.Pessoa, detect.Pessoa}},
			"alert.families[1]", "não se repita", []detect.Familia{detect.Pessoa}, nil,
		},
		"área que não existe": {
			AlertaDaCamera{Enabled: true, Families: []detect.Familia{detect.Pessoa}, Areas: []string{"k3f9a2", "sumiu"}},
			"alert.areas[1]", "área desta câmera", []detect.Familia{detect.Pessoa}, []string{"k3f9a2"},
		},
		"área repetida": {
			AlertaDaCamera{Enabled: true, Families: []detect.Familia{detect.Pessoa}, Areas: []string{"k3f9a2", "k3f9a2"}},
			"alert.areas[1]", "não se repita", []detect.Familia{detect.Pessoa}, []string{"k3f9a2"},
		},
	}
	cfg := &Config{}
	for nome, c := range casos {
		t.Run(nome, func(t *testing.T) {
			antes := AlertaDaCamera{Families: slices.Clone(c.alerta.Families), Areas: slices.Clone(c.alerta.Areas)}
			cam, avisos := cfg.ConfereCamera(Camera{ID: "cam_a", Areas: []Area{portao}, Alert: &c.alerta})
			if !slices.Equal(c.alerta.Families, antes.Families) || !slices.Equal(c.alerta.Areas, antes.Areas) {
				t.Error("o ConfereCamera mexeu no alerta de entrada")
			}
			al := cam.Alert
			if !al.Enabled || !slices.Equal(al.Families, c.familias) || !slices.Equal(al.Areas, c.areas) {
				t.Errorf("ficou %+v, esperava ligado com %v e %v", al, c.familias, c.areas)
			}
			if len(avisos) != 1 {
				t.Fatalf("%d avisos: %+v", len(avisos), avisos)
			}
			a := avisos[0]
			if a.Camera != "cam_a" || a.Campo != c.campo || !strings.Contains(a.Aceito, c.aceito) {
				t.Errorf("aviso %+v, esperava %s com %q", a, c.campo, c.aceito)
			}
		})
	}
}

// TestAlertaSemFamiliaDesliga: ligado e sem família que valha, o alerta não
// tem o que avisar, e fica desligado.
func TestAlertaSemFamiliaDesliga(t *testing.T) {
	for nome, familias := range map[string][]detect.Familia{
		"nenhuma":      nil,
		"só inválidas": {"gente"},
	} {
		t.Run(nome, func(t *testing.T) {
			cam, avisos := (&Config{}).ConfereCamera(Camera{ID: "cam_a",
				Alert: &AlertaDaCamera{Enabled: true, Families: familias}})
			if cam.Alert.Enabled {
				t.Error("o alerta ficou ligado sem família")
			}
			if len(avisos) == 0 || avisos[len(avisos)-1].Campo != "alert.families" {
				t.Errorf("avisos %+v, esperava terminar em alert.families", avisos)
			}
		})
	}
}

// TestAlertaSemAsAreasDesliga: o alerta que perde todas as áreas fica
// desligado, e não passa a valer na imagem inteira. Vale também para a área
// que existia no arquivo mas saiu por ser inválida.
func TestAlertaSemAsAreasDesliga(t *testing.T) {
	invalida := Area{ID: "k3f9a2", Name: "Portão", Points: quadrado[:2]}
	for nome, areas := range map[string][]Area{
		"área apagada":  nil,
		"área inválida": {invalida},
	} {
		t.Run(nome, func(t *testing.T) {
			cam, avisos := (&Config{}).ConfereCamera(Camera{ID: "cam_a", Areas: areas,
				Alert: &AlertaDaCamera{Enabled: true, Families: []detect.Familia{detect.Pessoa}, Areas: []string{"k3f9a2"}}})
			if cam.Alert.Enabled {
				t.Errorf("o alerta ficou ligado: %+v", cam.Alert)
			}
			if len(avisos) == 0 || avisos[len(avisos)-1].Campo != "alert.areas" {
				t.Errorf("avisos %+v, esperava terminar em alert.areas", avisos)
			}
		})
	}
}

// TestAlertaDesligadoGuardaOResto: desligar não apaga as famílias nem as
// áreas, e desligado sem família também vale.
func TestAlertaDesligadoGuardaOResto(t *testing.T) {
	cfg := &Config{}
	guardado := AlertaDaCamera{Families: []detect.Familia{detect.Animal}, Areas: []string{"k3f9a2"}}
	cam, avisos := cfg.ConfereCamera(Camera{ID: "cam_a", Areas: []Area{portao}, Alert: &guardado})
	if len(avisos) > 0 || cam.Alert.Enabled || !slices.Equal(cam.Alert.Families, guardado.Families) ||
		!slices.Equal(cam.Alert.Areas, guardado.Areas) {
		t.Errorf("alerta %+v, avisos %+v", cam.Alert, avisos)
	}

	cam, avisos = cfg.ConfereCamera(Camera{ID: "cam_a", Alert: &AlertaDaCamera{}})
	if len(avisos) > 0 || cam.Alert == nil {
		t.Errorf("alerta %+v, avisos %+v", cam.Alert, avisos)
	}
}

// TestSemAlertaNaoAvisa: a câmera sem alerta, que é toda câmera de hoje, não
// ganha nada no JSON.
func TestSemAlertaNaoAvisa(t *testing.T) {
	cam, avisos := (&Config{}).ConfereCamera(Camera{ID: "cam_a", Areas: []Area{portao}})
	if cam.Alert != nil || len(avisos) > 0 {
		t.Errorf("alerta %+v, avisos %+v", cam.Alert, avisos)
	}
}
