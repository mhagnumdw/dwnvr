package config

import (
	"fmt"
	"slices"
	"strings"
	"time"

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

	// Schedules são os horários em que o alerta sai, somados. Vazio é a
	// qualquer hora. Valem no fuso do servidor, o mesmo do dia das gravações.
	Schedules []HorarioDoAlerta `json:"schedules,omitempty"`
}

// HorarioDoAlerta são os dias da semana e um De e um Até, ao minuto ("HH:MM").
// As duas pontas entram, como no intervalo de Detecções: o dia inteiro é
// "00:00" até "23:59". Nenhum horário atravessa a meia-noite: o To é maior que
// o From, e a noite são dois horários, um até 23:59 e outro a partir de 00:00.
type HorarioDoAlerta struct {
	// Days vai de 0 (domingo) a 6 (sábado), como o time.Weekday conta.
	Days []time.Weekday `json:"days"`
	From string         `json:"from"`
	To   string         `json:"to"`
}

// MinutoDoDia lê "HH:MM" como os minutos desde a meia-noite, de 0 (00:00) a
// 1439 (23:59). Só aceita os cinco caracteres, com os zeros à esquerda.
func MinutoDoDia(s string) (int, bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	for i, c := range []byte(s) {
		if i != 2 && (c < '0' || c > '9') {
			return 0, false
		}
	}
	h := int(s[0]-'0')*10 + int(s[1]-'0')
	m := int(s[3]-'0')*10 + int(s[4]-'0')
	if h > 23 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// confereAlerta devolve o alerta só com as famílias, as áreas e os horários
// que valem, e um Aviso por item tirado. As áreas são as que já passaram pelo
// confereAreas: o alerta não pode apontar para uma área que saiu.
//
// Ligado e sem nenhuma família, sem nenhuma das áreas que tinha ou sem nenhum
// dos horários que tinha, o alerta é desligado. Sem as áreas ele valeria na
// imagem inteira, e quem marcou só o portão receberia o quintal todo sem saber
// por quê; sem os horários ele valeria o dia todo, e quem marcou só a noite
// receberia o dia.
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

	horarios, mexeuNosHorarios := confereHorarios(al.Schedules, recusa)

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
	if al.Enabled && len(al.Schedules) > 0 && len(horarios) == 0 {
		recusa("alert.schedules", "sem nenhum horário que valha",
			"pelo menos um horário válido, ou nenhum para valer a qualquer hora",
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
	if mexeuNosHorarios {
		al.Schedules = horarios
	}
	return &al
}

// confereHorarios devolve só os horários que valem, até MaxHorariosDoAlerta,
// e se tirou ou mudou algum. O horário com dia fora de 0 a 6, sem dia, com
// hora que não é HH:MM ou com o To que não fica depois do From sai inteiro. O
// dia repetido conta uma vez, e os horários que se sobrepõem valem pela soma.
// Os slices de entrada não mudam: o dia repetido sai numa cópia.
func confereHorarios(entrada []HorarioDoAlerta, recusa func(campo string, valor any, aceito, usando string)) (
	[]HorarioDoAlerta, bool) {
	const fica = "o horário fica de fora do alerta"
	horarios := make([]HorarioDoAlerta, 0, len(entrada))
	mexeu := false
	for i, h := range entrada {
		campo := fmt.Sprintf("alert.schedules[%d]", i)
		de, deOk := MinutoDoDia(h.From)
		ate, ateOk := MinutoDoDia(h.To)
		foraDaSemana := slices.IndexFunc(h.Days, func(d time.Weekday) bool { return d < time.Sunday || d > time.Saturday })
		switch {
		case len(horarios) >= MaxHorariosDoAlerta:
			recusa(campo, fmt.Sprintf("%s até %s", h.From, h.To),
				fmt.Sprintf("até %d horários por câmera", MaxHorariosDoAlerta), fica)
		case len(h.Days) == 0:
			recusa(campo+".days", "vazio", "pelo menos um dia, de 0 (domingo) a 6 (sábado)", fica)
		case foraDaSemana >= 0:
			recusa(fmt.Sprintf("%s.days[%d]", campo, foraDaSemana), int(h.Days[foraDaSemana]),
				"um dia de 0 (domingo) a 6 (sábado)", fica)
		case !deOk:
			recusa(campo+".from", fmt.Sprintf("%q", h.From), "uma hora HH:MM, de 00:00 a 23:59", fica)
		case !ateOk:
			recusa(campo+".to", fmt.Sprintf("%q", h.To), "uma hora HH:MM, de 00:00 a 23:59", fica)
		case ate <= de:
			recusa(campo+".to", fmt.Sprintf("%q", h.To),
				fmt.Sprintf("uma hora depois do from %s; a noite são dois horários, até 23:59 e a partir de 00:00",
					h.From), fica)
		default:
			dias := make([]time.Weekday, 0, len(h.Days))
			for j, d := range h.Days {
				if slices.Contains(dias, d) {
					recusa(fmt.Sprintf("%s.days[%d]", campo, j), int(d),
						"um dia que não se repita no horário", "o dia conta uma vez")
					continue
				}
				dias = append(dias, d)
			}
			if len(dias) < len(h.Days) {
				h.Days = dias
				mexeu = true
			}
			horarios = append(horarios, h)
			continue
		}
		// Os outros casos tiraram o horário.
		mexeu = true
	}
	return horarios, mexeu
}

func familiasAceitas() string {
	nomes := make([]string, len(detect.Familias))
	for i, f := range detect.Familias {
		nomes[i] = string(f)
	}
	return strings.Join(nomes, ", ")
}
