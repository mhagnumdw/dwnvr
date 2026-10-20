// Package area responde "o objeto está dentro desta área?": a área é um
// polígono desenhado sobre a imagem de uma câmera, e o objeto é a caixa que o
// detector de objetos devolveu.
//
// A área não muda o que a detecção grava. Ela serve de filtro em Detecções e,
// depois, decide se o alerta sai (ver docs/TODO/TODO_areas-e-alertas-de-deteccao.md).
//
// Tudo é fração do quadro, de 0 a 1, como a caixa: a área vale sobre o quadro
// da detecção e sobre o keyframe do segmento, em qualquer resolução, e se
// compara com a caixa sem conversão.
package area

import (
	"errors"
	"fmt"
)

// Ponto é um vértice da área: X e Y em fração do quadro, com Y crescendo para
// baixo, como na imagem.
type Ponto [2]float64

// Area é um polígono simples: os vértices em ordem, sem repetir o primeiro no
// fim. Pode ser côncavo (um L).
type Area struct {
	Pontos []Ponto
}

// Nova confere os pontos e devolve a área. O erro diz o motivo, para a API
// devolver a quem mandou.
func Nova(pontos []Ponto) (Area, error) {
	if len(pontos) < MinPontosDaArea {
		return Area{}, fmt.Errorf("a área precisa de pelo menos %d pontos, veio com %d", MinPontosDaArea, len(pontos))
	}
	if len(pontos) > MaxPontosDaArea {
		return Area{}, fmt.Errorf("a área aceita até %d pontos, veio com %d", MaxPontosDaArea, len(pontos))
	}
	for i, p := range pontos {
		if p[0] < 0 || p[0] > 1 || p[1] < 0 || p[1] > 1 {
			return Area{}, fmt.Errorf("o ponto %d (%g, %g) está fora do quadro: as coordenadas vão de 0 a 1", i+1, p[0], p[1])
		}
	}
	return Area{Pontos: pontos}, nil
}

// RegraDaArea é o contrato que decide quando um objeto conta como dentro da
// área. São duas, porque a câmera vê em perspectiva: quem está perto dela
// cobre, na imagem, o chão que fica atrás (ver §As duas regras do TODO).
//
// Não guarda estado e não aloca: o calendário de Detecções passa por todas as
// marcas de todos os dias.
type RegraDaArea interface {
	// Dentro diz se a caixa [x1, y1, x2, y2], em fração do quadro, conta como
	// dentro da área. A borda da área é dentro.
	Dentro(a Area, caixa [4]float64) bool

	// Nome é o valor do JSON e da query (`rule=`).
	Nome() string
}

// Regras é o mapa de construtores: uma regra nova entra numa linha. A chave é
// o valor do JSON, em en-US.
var Regras = map[string]func() RegraDaArea{
	"touch": func() RegraDaArea { return encosta{} },
	"foot":  func() RegraDaArea { return pisa{} },
}

// ErrRegraDesconhecida é o nome que não está em Regras.
var ErrRegraDesconhecida = errors.New("regra da área desconhecida")

// NovaRegra devolve a regra pelo nome, e a RegraDaAreaPadrao quando o nome
// vem vazio. Ao contrário do NovoMarcador, nome desconhecido é erro: ele vem
// de quem usa (a query, o cadastro), e cair calado no padrão mostraria outra
// lista sem dizer.
func NovaRegra(nome string) (RegraDaArea, error) {
	if nome == "" {
		nome = RegraDaAreaPadrao
	}
	nova, ok := Regras[nome]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrRegraDesconhecida, nome)
	}
	return nova(), nil
}

// --- encosta ----------------------------------------------------------------

// encosta conta a caixa como dentro quando ela e a área têm algum ponto em
// comum. É o que se espera sem pensar, e pega o objeto pela metade na borda;
// em perspectiva, também pega quem só passa na frente da área.
type encosta struct{}

func (encosta) Nome() string { return "touch" }

// Dentro testa os três jeitos de um retângulo e um polígono se tocarem: um
// vértice da área dentro da caixa (a área menor que a caixa), um canto da
// caixa dentro da área (a caixa menor que a área) ou uma borda cruzando a
// outra. Fora esses, os dois estão separados.
func (encosta) Dentro(a Area, caixa [4]float64) bool {
	x1, y1, x2, y2 := caixa[0], caixa[1], caixa[2], caixa[3]
	for _, p := range a.Pontos {
		if p[0] >= x1-ToleranciaDaBorda && p[0] <= x2+ToleranciaDaBorda &&
			p[1] >= y1-ToleranciaDaBorda && p[1] <= y2+ToleranciaDaBorda {
			return true
		}
	}
	cantos := [4]Ponto{{x1, y1}, {x2, y1}, {x2, y2}, {x1, y2}}
	for _, c := range cantos {
		if contem(a, c) {
			return true
		}
	}
	n := len(a.Pontos)
	for i := range n {
		p, q := a.Pontos[i], a.Pontos[(i+1)%n]
		for j := range 4 {
			if cruzam(p, q, cantos[j], cantos[(j+1)%4]) {
				return true
			}
		}
	}
	return false
}

// --- pisa -------------------------------------------------------------------

// pisa conta a caixa como dentro quando o meio da borda de baixo dela, onde
// ficam os pés e as rodas, está dentro da área. A área se desenha no chão, e
// quem está na frente dela, mais perto da câmera, não conta.
type pisa struct{}

func (pisa) Nome() string { return "foot" }

func (pisa) Dentro(a Area, caixa [4]float64) bool {
	return contem(a, Ponto{(caixa[0] + caixa[2]) / 2, caixa[3]})
}

// --- geometria --------------------------------------------------------------

// contem diz se o ponto está dentro da área ou na borda dela. O teste do raio
// (quantas bordas uma semirreta para a direita cruza) sozinho não decide a
// borda: na área do tamanho da imagem, o pé de quem está cortado embaixo cai
// em Y = 1, e sairia como fora.
func contem(a Area, pt Ponto) bool {
	n := len(a.Pontos)
	dentro := false
	for i := range n {
		p, q := a.Pontos[i], a.Pontos[(i+1)%n]
		if naBorda(p, q, pt) {
			return true
		}
		if (p[1] > pt[1]) != (q[1] > pt[1]) &&
			pt[0] < p[0]+(pt[1]-p[1])*(q[0]-p[0])/(q[1]-p[1]) {
			dentro = !dentro
		}
	}
	return dentro
}

// lado é o produto vetorial (q-p) x (r-p): positivo de um lado da reta pq,
// negativo do outro, perto de zero sobre ela.
func lado(p, q, r Ponto) float64 {
	return (q[0]-p[0])*(r[1]-p[1]) - (q[1]-p[1])*(r[0]-p[0])
}

// naCaixaDe diz se r está no retângulo que tem pq de diagonal: junto com
// lado ≈ 0, é r sobre o segmento pq.
func naCaixaDe(p, q, r Ponto) bool {
	return r[0] >= min(p[0], q[0])-ToleranciaDaBorda && r[0] <= max(p[0], q[0])+ToleranciaDaBorda &&
		r[1] >= min(p[1], q[1])-ToleranciaDaBorda && r[1] <= max(p[1], q[1])+ToleranciaDaBorda
}

// naBorda diz se r está sobre o segmento pq.
func naBorda(p, q, r Ponto) bool {
	return abs(lado(p, q, r)) <= ToleranciaDaBorda && naCaixaDe(p, q, r)
}

// cruzam diz se os segmentos ab e cd têm algum ponto em comum, inclusive
// quando só se encostam ou correm um sobre o outro.
func cruzam(a, b, c, d Ponto) bool {
	d1, d2 := lado(c, d, a), lado(c, d, b)
	d3, d4 := lado(a, b, c), lado(a, b, d)
	if ((d1 > ToleranciaDaBorda && d2 < -ToleranciaDaBorda) || (d1 < -ToleranciaDaBorda && d2 > ToleranciaDaBorda)) &&
		((d3 > ToleranciaDaBorda && d4 < -ToleranciaDaBorda) || (d3 < -ToleranciaDaBorda && d4 > ToleranciaDaBorda)) {
		return true
	}
	return naBorda(c, d, a) || naBorda(c, d, b) || naBorda(a, b, c) || naBorda(a, b, d)
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
