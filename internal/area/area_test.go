package area

import (
	"errors"
	"testing"
)

// As áreas, em fração do quadro.
var (
	// imagemInteira é a área do tamanho da imagem: tem de dar o mesmo que sem
	// filtro.
	imagemInteira = []Ponto{{0, 0}, {1, 0}, {1, 1}, {0, 1}}

	// portao é um retângulo no meio do chão, de 0,4 a 0,6 em X e de 0,5 a 0,7
	// em Y.
	portao = []Ponto{{0.4, 0.5}, {0.6, 0.5}, {0.6, 0.7}, {0.4, 0.7}}

	// emL é côncava: a perna de baixo vai de X 0,1 a 0,9 e de Y 0,7 a 0,9, e a
	// de cima sobe pela esquerda até Y 0,3. O canto de dentro (X 0,3 a 0,9, Y
	// 0,3 a 0,7) é fora.
	emL = []Ponto{{0.1, 0.3}, {0.3, 0.3}, {0.3, 0.7}, {0.9, 0.7}, {0.9, 0.9}, {0.1, 0.9}}
)

func area(t *testing.T, pontos []Ponto) Area {
	t.Helper()
	a, err := Nova(pontos)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func regra(t *testing.T, nome string) RegraDaArea {
	t.Helper()
	r, err := NovaRegra(nome)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// confere roda os casos nas duas regras: cada caso diz o que cada uma
// responde.
func confere(t *testing.T, a Area, casos []struct {
	nome        string
	caixa       [4]float64
	touch, foot bool
},
) {
	t.Helper()
	for _, c := range casos {
		if got := regra(t, "touch").Dentro(a, c.caixa); got != c.touch {
			t.Errorf("%s: touch deu %v, devia dar %v", c.nome, got, c.touch)
		}
		if got := regra(t, "foot").Dentro(a, c.caixa); got != c.foot {
			t.Errorf("%s: foot deu %v, devia dar %v", c.nome, got, c.foot)
		}
	}
}

// TestImagemInteiraEOMesmoQueSemFiltro inclui as caixas cortadas nas bordas
// da imagem, as mais comuns: quem está perto da câmera sai cortado embaixo, e
// o pé cai em Y = 1, sobre a borda da área.
func TestImagemInteiraEOMesmoQueSemFiltro(t *testing.T) {
	confere(t, area(t, imagemInteira), []struct {
		nome        string
		caixa       [4]float64
		touch, foot bool
	}{
		{"no meio", [4]float64{0.4, 0.3, 0.5, 0.6}, true, true},
		{"cortada embaixo", [4]float64{0.4, 0.5, 0.6, 1}, true, true},
		{"cortada à esquerda", [4]float64{0, 0.2, 0.1, 0.5}, true, true},
		{"cortada à direita", [4]float64{0.9, 0.2, 1, 0.5}, true, true},
		{"a imagem toda", [4]float64{0, 0, 1, 1}, true, true},
	})
}

// TestQuemPassaNaFrenteNaoPisa é o motivo das duas regras: a pessoa perto da
// câmera cobre, na imagem, a área que fica atrás dela.
func TestQuemPassaNaFrenteNaoPisa(t *testing.T) {
	confere(t, area(t, portao), []struct {
		nome        string
		caixa       [4]float64
		touch, foot bool
	}{
		{"no portão", [4]float64{0.45, 0.3, 0.55, 0.65}, true, true},
		{"na frente, cobrindo o portão", [4]float64{0.35, 0.2, 0.65, 0.95}, true, false},
		{"pé na borda de baixo do portão", [4]float64{0.45, 0.3, 0.55, 0.7}, true, true},
		{"longe", [4]float64{0.05, 0.05, 0.15, 0.2}, false, false},
		{"a caixa dentro do portão, pequena", [4]float64{0.45, 0.55, 0.5, 0.6}, true, true},
		{"o portão dentro da caixa, que pisa abaixo dele", [4]float64{0.3, 0.4, 0.7, 0.8}, true, false},
	})
}

// TestCaixaQueSoEncostaNaBorda: um ponto em comum já é encostar.
func TestCaixaQueSoEncostaNaBorda(t *testing.T) {
	confere(t, area(t, portao), []struct {
		nome        string
		caixa       [4]float64
		touch, foot bool
	}{
		{"borda direita da caixa na esquerda do portão", [4]float64{0.3, 0.55, 0.4, 0.65}, true, false},
		{"um canto da caixa no canto do portão", [4]float64{0.6, 0.7, 0.7, 0.8}, true, false},
		{"a um milésimo da esquerda do portão", [4]float64{0.3, 0.55, 0.399, 0.65}, false, false},
		{"base da caixa no topo do portão", [4]float64{0.45, 0.3, 0.55, 0.5}, true, true},
		{"cruzando o portão de lado a lado, sem canto dentro", [4]float64{0.3, 0.55, 0.7, 0.6}, true, true},
		{"cruzando o portão de lado a lado, com o pé abaixo", [4]float64{0.3, 0.55, 0.7, 0.75}, true, false},
	})
}

// TestAreaConcava: o canto de dentro do L é fora, nas duas regras.
func TestAreaConcava(t *testing.T) {
	confere(t, area(t, emL), []struct {
		nome        string
		caixa       [4]float64
		touch, foot bool
	}{
		{"na perna de baixo", [4]float64{0.5, 0.6, 0.6, 0.8}, true, true},
		{"na perna de cima", [4]float64{0.15, 0.2, 0.25, 0.5}, true, true},
		{"no canto de dentro", [4]float64{0.5, 0.35, 0.7, 0.6}, false, false},
		{"no canto de dentro, com o pé na perna de baixo", [4]float64{0.5, 0.35, 0.7, 0.75}, true, true},
		{"sobre a quina de dentro, com o pé fora", [4]float64{0.25, 0.4, 0.4, 0.6}, true, false},
	})
}

func TestNovaRecusaComOMotivo(t *testing.T) {
	demais := make([]Ponto, MaxPontosDaArea+1)
	for _, c := range []struct {
		nome   string
		pontos []Ponto
	}{
		{"dois pontos", []Ponto{{0, 0}, {1, 1}}},
		{"pontos demais", demais},
		{"X acima de 1", []Ponto{{0, 0}, {1.2, 0}, {1, 1}}},
		{"Y negativo", []Ponto{{0, 0}, {1, -0.1}, {1, 1}}},
	} {
		if _, err := Nova(c.pontos); err == nil {
			t.Errorf("%s: devia recusar", c.nome)
		}
	}
}

func TestNovaRegra(t *testing.T) {
	if r := regra(t, ""); r.Nome() != RegraDaAreaPadrao {
		t.Errorf("sem nome devia ser a padrão, %q; veio %q", RegraDaAreaPadrao, r.Nome())
	}
	for nome := range Regras {
		if r := regra(t, nome); r.Nome() != nome {
			t.Errorf("Regras[%q] devolve uma regra que se chama %q", nome, r.Nome())
		}
	}
	if _, err := NovaRegra("pisa"); !errors.Is(err, ErrRegraDesconhecida) {
		t.Errorf("nome fora do mapa devia ser ErrRegraDesconhecida, veio %v", err)
	}
}

// TestRegrasNaoAlocam: o calendário de Detecções passa por todas as marcas de
// todos os dias.
func TestRegrasNaoAlocam(t *testing.T) {
	a := area(t, emL)
	caixa := [4]float64{0.25, 0.4, 0.4, 0.6}
	for nome := range Regras {
		r := regra(t, nome)
		if n := testing.AllocsPerRun(100, func() { r.Dentro(a, caixa) }); n != 0 {
			t.Errorf("%s aloca %v vezes por caixa", nome, n)
		}
	}
}
