package alerta

import (
	"slices"

	"github.com/mhagnumdw/dwnvr/internal/area"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// Regra é o alerta de uma câmera montado para avaliar: as áreas já viram
// polígono e regra, e a olhada só faz as contas.
type Regra struct {
	camera, nome string
	familias     []detect.Familia
	areas        []areaDaRegra
}

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
	}
	// O ConfereCamera já desligou o alerta que perdeu as áreas; isto só
	// garante que ele nunca passe para a imagem inteira por outro caminho.
	if len(al.Areas) > 0 && len(r.areas) == 0 {
		return nil
	}
	return r
}

// Avalia diz se as marcas de uma olhada viram alerta, e marca nelas os
// objetos que passam: `Alert` e, com áreas, `AlertAreas`. Passa cada marca de
// uma família do alerta que cai na imagem inteira, ou em alguma das áreas,
// cada uma pela própria regra. Todas as que passam vão no mesmo alerta: duas
// pessoas na mesma olhada são um alerta só.
//
// As marcas são as linhas que o Recorder vai gravar, todas do mesmo onset, e
// a avaliação vem antes da escrita para a marca entrar nela: o alerta tem os
// mesmos números que Detecções mostra.
func (r *Regra) Avalia(marcas []store.Evento, quadro []byte) (Alerta, bool) {
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
		TemQuadro: m.TemQuadro, Objetos: objetos, Quadro: quadro}, true
}
