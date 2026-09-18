package detect

import (
	"math"
	"testing"
)

// Caixas em fração do quadro, como o detector vai devolver.
var (
	vagaDaRua  = [4]float64{0.10, 0.50, 0.30, 0.70}
	outraVaga  = [4]float64{0.60, 0.50, 0.80, 0.70}
	espantalho = [4]float64{0.30, 0.20, 0.34, 0.34}
	quaseAVaga = [4]float64{0.11, 0.51, 0.31, 0.71} // o mesmo carro, a caixa tremeu
)

func achado(classe string, score float64, caixa [4]float64) Achado {
	return Achado{Classe: classe, Score: score, Caixa: caixa}
}

// marcou devolve as classes que viraram marca, na ordem.
func marcou(m Marcador, achados ...Achado) []string {
	var out []string
	for _, a := range m.Olha(achados) {
		out = append(out, a.Classe)
	}
	return out
}

func TestRastreioNaoRemarcaOCarroEstacionado(t *testing.T) {
	m := NovoMarcador("rastreio")
	if got := marcou(m, achado("car", 0.9, vagaDaRua)); len(got) != 1 {
		t.Fatalf("a chegada tinha que marcar: %v", got)
	}
	for i := range 50 {
		if got := marcou(m, achado("car", 0.9, quaseAVaga)); len(got) != 0 {
			t.Fatalf("olhada %d: o carro parado marcou de novo", i)
		}
	}
}

// TestRastreioDevolveAVagaQuandoOCarroSai é o que a janela de tempo não sabe
// fazer: o carro seguinte, na mesma vaga, é uma chegada.
func TestRastreioDevolveAVagaQuandoOCarroSai(t *testing.T) {
	m := NovoMarcador("rastreio")
	marcou(m, achado("car", 0.9, vagaDaRua))

	// Quatro olhadas sem ele: o lugar continua ocupado.
	for range FaltasParaLiberar - 1 {
		marcou(m)
	}
	if got := marcou(m, achado("car", 0.9, vagaDaRua)); len(got) != 0 {
		t.Fatal("liberou o lugar antes de FaltasParaLiberar olhadas")
	}

	// Cinco seguidas sem ele: livre.
	for range FaltasParaLiberar {
		marcou(m)
	}
	if got := marcou(m, achado("car", 0.9, vagaDaRua)); len(got) != 1 {
		t.Fatal("o carro que voltou para a vaga livre tinha que marcar")
	}
}

// TestRastreioAprendeAbaixoDoCorte: o espantalho sai ora a 0,30, ora a 0,45. Se o
// rastreio só aprendesse com o que passa do corte, a primeira aparição acima
// dele viraria pessoa falsa.
func TestRastreioAprendeAbaixoDoCorte(t *testing.T) {
	m := NovoMarcador("rastreio")
	if got := marcou(m, achado("person", 0.30, espantalho)); len(got) != 0 {
		t.Fatal("caixa abaixo do corte não pode marcar")
	}
	if got := marcou(m, achado("person", 0.45, espantalho)); len(got) != 0 {
		t.Fatal("o espantalho já era conhecido e marcou mesmo assim")
	}
	// Abaixo do piso ele nem é visto: não renova o lugar.
	for range FaltasParaLiberar {
		marcou(m, achado("person", PisoDoDetector-0.01, espantalho))
	}
	if got := marcou(m, achado("person", 0.45, espantalho)); len(got) != 1 {
		t.Fatal("caixa abaixo do piso renovou o lugar")
	}
}

func TestRastreioSeparaLugaresEFamilias(t *testing.T) {
	m := NovoMarcador("rastreio")
	got := marcou(m,
		achado("car", 0.9, vagaDaRua),
		achado("car", 0.8, outraVaga),
		achado("chair", 0.9, espantalho), // fora das famílias: nem existe para o rastreio
	)
	if len(got) != 2 {
		t.Fatalf("dois carros em dois lugares são duas marcas: %v", got)
	}
	// Uma pessoa no lugar do carro é outra família, e chega.
	if got := marcou(m, achado("person", 0.9, vagaDaRua)); len(got) != 1 {
		t.Fatalf("pessoa no lugar do carro tinha que marcar: %v", got)
	}
}

func TestSemMemoriaMarcaTudoQuePassaDoCorte(t *testing.T) {
	m := NovoMarcador("nenhum")
	for range 3 {
		got := marcou(m,
			achado("car", 0.9, vagaDaRua),
			achado("person", 0.39, espantalho),
			achado("chair", 0.9, outraVaga),
		)
		if len(got) != 1 || got[0] != "car" {
			t.Fatalf("esperava só o carro, a cada olhada: %v", got)
		}
	}
}

func TestNovoMarcadorCaiNoPadrao(t *testing.T) {
	if got := NovoMarcador("nao-existe").Nome(); got != MarcadorPadrao {
		t.Errorf("nome desconhecido deu %q, esperado %q", got, MarcadorPadrao)
	}
}

func TestIoUNaoDependeDaUnidade(t *testing.T) {
	emPixels := func(c [4]float64) [4]float64 { return [4]float64{c[0] * 640, c[1] * 360, c[2] * 640, c[3] * 360} }
	f := iou(vagaDaRua, quaseAVaga)
	p := iou(emPixels(vagaDaRua), emPixels(quaseAVaga))
	if math.Abs(f-p) > 1e-12 || f < IoUDoMesmoLugar {
		t.Errorf("IoU em fração %v, em pixel %v", f, p)
	}
	if got := iou(vagaDaRua, outraVaga); got != 0 {
		t.Errorf("caixas separadas deram IoU %v", got)
	}
	if got := iou(vagaDaRua, vagaDaRua); got != 1 {
		t.Errorf("a mesma caixa deu IoU %v", got)
	}
}
