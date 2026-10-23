package config

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mhagnumdw/dwnvr/internal/detect"
)

// AlertaDaCamera diz quando uma detecção desta câmera vira alerta. Quem avalia
// e envia é o pacote alerta; aqui fica o que o cadastro guarda.
type AlertaDaCamera struct {
	// Enabled desligado guarda o resto, para religar sem refazer.
	Enabled bool `json:"enabled"`

	// Families são as famílias que alertam, como "pessoa". Com o alerta
	// ligado, pelo menos uma.
	Families []detect.Familia `json:"families,omitempty"`

	// Areas são os IDs das áreas desta câmera em que o objeto precisa cair.
	// Vazio é a imagem inteira.
	Areas []string `json:"areas,omitempty"`
}

// confereAlerta devolve o alerta só com as famílias e as áreas que existem, e
// um Aviso por item tirado. As áreas são as que já passaram pelo
// confereAreas: o alerta não pode apontar para uma área que saiu.
//
// Ligado e sem nenhuma família, ou sem nenhuma das áreas que tinha, o alerta
// é desligado. Sem as áreas ele valeria na imagem inteira, e quem marcou só o
// portão receberia o quintal todo sem saber por quê.
func confereAlerta(cam Camera, av *[]Aviso) *AlertaDaCamera {
	if cam.Alert == nil {
		return nil
	}
	al := *cam.Alert
	recusa := func(campo string, valor any, aceito, usando string) {
		*av = append(*av, Aviso{Camera: cam.ID, Campo: campo, Valor: valor, Aceito: aceito, Usando: usando})
	}

	familias := make([]detect.Familia, 0, len(al.Families))
	for i, f := range al.Families {
		switch {
		case !slices.Contains(detect.Familias, f):
			recusa(fmt.Sprintf("alert.families[%d]", i), fmt.Sprintf("%q", f),
				"uma família: "+familiasAceitas(), "a família fica de fora do alerta")
		case slices.Contains(familias, f):
			recusa(fmt.Sprintf("alert.families[%d]", i), fmt.Sprintf("%q", f),
				"uma família que não se repita no alerta", "a família conta uma vez")
		default:
			familias = append(familias, f)
		}
	}

	areas := make([]string, 0, len(al.Areas))
	for i, id := range al.Areas {
		switch {
		case !slices.ContainsFunc(cam.Areas, func(a Area) bool { return a.ID == id }):
			recusa(fmt.Sprintf("alert.areas[%d]", i), fmt.Sprintf("%q", id),
				"o id de uma área desta câmera", "a área fica de fora do alerta")
		case slices.Contains(areas, id):
			recusa(fmt.Sprintf("alert.areas[%d]", i), fmt.Sprintf("%q", id),
				"uma área que não se repita no alerta", "a área conta uma vez")
		default:
			areas = append(areas, id)
		}
	}

	if al.Enabled && len(familias) == 0 {
		recusa("alert.families", "vazio", "pelo menos uma família com o alerta ligado",
			"o alerta fica desligado")
		al.Enabled = false
	}
	if al.Enabled && len(al.Areas) > 0 && len(areas) == 0 {
		recusa("alert.areas", "sem nenhuma área que exista",
			"pelo menos uma área da câmera, ou nenhuma para a imagem inteira",
			"o alerta fica desligado")
		al.Enabled = false
	}

	// Sem nada tirado, a câmera segue com os slices dela: o JSON gravado não
	// troca um ausente por um vazio.
	if len(familias) < len(al.Families) {
		al.Families = familias
	}
	if len(areas) < len(al.Areas) {
		al.Areas = areas
	}
	return &al
}

func familiasAceitas() string {
	nomes := make([]string, len(detect.Familias))
	for i, f := range detect.Familias {
		nomes[i] = string(f)
	}
	return strings.Join(nomes, ", ")
}
