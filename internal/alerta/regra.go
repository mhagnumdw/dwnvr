package alerta

import (
	"slices"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/area"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// Regra é o alerta de uma câmera montado para avaliar: as áreas já viram
// polígono e regra, os horários já viram minutos, e a olhada só faz as contas.
type Regra struct {
	camera, nome string
	familias     []detect.Familia
	areas        []areaDaRegra
	// contornos são as mesmas áreas como o Alerta as leva, montadas uma vez
	// aqui: a olhada só passa o slice adiante.
	contornos []AreaDoAlerta

	// horarios são, por dia da semana, os horários em minutos do dia. Nil é a
	// qualquer hora; o dia que não está no mapa não alerta.
	horarios map[time.Weekday][]horarioDoDia
}

// horarioDoDia é um horário de um dia, em minutos desde a meia-noite, com as
// duas pontas incluídas: de 0 a 1439 é o dia inteiro.
type horarioDoDia struct{ de, ate int }

type areaDaRegra struct {
	id, nome string
	area     area.Area
	regra    area.RegraDaArea
}

// NovaRegra monta a regra da câmera já conferida pelo ConfereCamera, ou
// devolve nil quando ela não alerta: câmera desabilitada, sem marcação de
// movimento ou sem alerta ligado. O nil é o "sem alerta" que se testa a cada
// olhada, e não custa mais do que esse teste.
func NovaRegra(cam config.Camera) *Regra {
	al := cam.Alert
	if !cam.Enabled || cam.Detect == nil || !*cam.Detect || al == nil || !al.Enabled || len(al.Families) == 0 {
		return nil
	}
	r := &Regra{camera: cam.ID, nome: cam.Name, familias: slices.Clone(al.Families)}
	for _, id := range al.Areas {
		i := slices.IndexFunc(cam.Areas, func(a config.Area) bool { return a.ID == id })
		if i < 0 {
			continue
		}
		a := cam.Areas[i]
		poligono, errPontos := area.Nova(a.Points)
		regra, errRegra := area.NovaRegra(a.Rule)
		if errPontos != nil || errRegra != nil {
			continue
		}
		r.areas = append(r.areas, areaDaRegra{id: a.ID, nome: a.Name, area: poligono, regra: regra})
		r.contornos = append(r.contornos, AreaDoAlerta{Nome: a.Name, Pontos: poligono.Pontos})
	}
	for _, h := range al.Schedules {
		de, deOk := config.MinutoDoDia(h.From)
		ate, ateOk := config.MinutoDoDia(h.To)
		if !deOk || !ateOk || ate <= de {
			continue
		}
		if r.horarios == nil {
			r.horarios = map[time.Weekday][]horarioDoDia{}
		}
		for _, d := range h.Days {
			r.horarios[d] = append(r.horarios[d], horarioDoDia{de: de, ate: ate})
		}
	}
	// O ConfereCamera já desligou o alerta que perdeu as áreas ou os horários;
	// isto só garante que ele nunca passe para a imagem inteira, ou para a
	// qualquer hora, por outro caminho.
	if (len(al.Areas) > 0 && len(r.areas) == 0) || (len(al.Schedules) > 0 && r.horarios == nil) {
		return nil
	}
	return r
}

// Avalia diz se as marcas de uma olhada viram alerta, e marca nelas os
// objetos que passam: `Alert` e, com áreas, `AlertAreas`. Com horários, o
// onset precisa cair num deles, e fora nenhum objeto passa. Dentro, passa
// cada marca de uma família do alerta que cai na imagem inteira, ou em alguma
// das áreas, cada uma pela própria regra. Todas as que passam vão no mesmo
// alerta: duas pessoas na mesma olhada são um alerta só.
//
// As marcas são as linhas que o Recorder vai gravar, todas do mesmo onset, e
// a avaliação vem antes da escrita para a marca entrar nela: o alerta tem os
// mesmos números que Detecções mostra.
func (r *Regra) Avalia(marcas []store.Evento, quadro []byte) (Alerta, bool) {
	if len(marcas) == 0 || !r.noHorario(marcas[0].InstanteMs) {
		return Alerta{}, false
	}
	var objetos []Objeto
	for i, m := range marcas {
		if m.Caixa == nil || !slices.Contains(r.familias, detect.Familia(m.Familia)) {
			continue
		}
		var ids, nomes []string
		for _, a := range r.areas {
			if a.regra.Dentro(a.area, *m.Caixa) {
				ids, nomes = append(ids, a.id), append(nomes, a.nome)
			}
		}
		if len(r.areas) > 0 && len(ids) == 0 {
			continue
		}
		marcas[i].Alert, marcas[i].AlertAreas = true, ids
		objetos = append(objetos, Objeto{Familia: m.Familia, Classe: m.Classe, Score: m.Score,
			Caixa: *m.Caixa, Areas: nomes})
	}
	if len(objetos) == 0 {
		return Alerta{}, false
	}
	m := marcas[0]
	return Alerta{Camera: r.camera, NomeDaCamera: r.nome, InstanteMs: m.InstanteMs, QuadroMs: m.QuadroMs,
		TemQuadro: m.TemQuadro, Objetos: objetos, Areas: r.contornos, Quadro: quadro}, true
}

// noHorario diz se o instante cai num dos horários do alerta, no fuso do
// servidor, o mesmo que vira o dia das gravações. Vale o minuto: "até 13:00"
// vai até 13:00:59. Sem horários, todo instante cai.
func (r *Regra) noHorario(instanteMs int64) bool {
	if r.horarios == nil {
		return true
	}
	t := time.UnixMilli(instanteMs)
	minuto := t.Hour()*60 + t.Minute()
	for _, h := range r.horarios[t.Weekday()] {
		if minuto >= h.de && minuto <= h.ate {
			return true
		}
	}
	return false
}
