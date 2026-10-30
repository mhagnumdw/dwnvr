package alerta

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand/v2"
	"testing"

	"github.com/mhagnumdw/dwnvr/internal/area"
)

// fundo é a cor do quadro de teste: escura, longe das cores das caixas e da
// área, inclusive do cinza da família desconhecida.
var fundo = color.RGBA{0x20, 0x20, 0x20, 0xff}

// quadroDeTeste é um JPEG de 480×270, o tamanho do quadro olhado
// (detect.LarguraDoQuadro), na qualidade dele. Com ruído, o quadro custa
// decodificar e codificar como um quadro de câmera; sem, é um fundo liso,
// para o teste ler a cor de cada pixel.
func quadroDeTeste(t testing.TB, ruido bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 480, 270))
	aleatorio := rand.New(rand.NewPCG(1, 2))
	for y := range 270 {
		for x := range 480 {
			c := fundo
			if ruido {
				v := uint8(x*200/480 + aleatorio.IntN(40))
				c = color.RGBA{v, uint8(y * 200 / 270), v / 2, 0xff}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 75}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// decodifica lê o quadro desenhado de volta, como quem abre o anexo.
func decodifica(t *testing.T, quadro []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(quadro))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// perto diz se o pixel ficou mais perto da cor do que do fundo. O JPEG mexe
// na cor, e mais ainda num traço de 2 px, porque guarda a cor em meia
// resolução: comparar com tolerância fixa daria um teste frágil. Medido em
// 27/10/2026, na qualidade do quadro, o meio de um vão do tracejado fica a
// 2.336 do fundo, e o traço mais fraco a 26.353: o meio do caminho separa os
// dois com folga.
func perto(img image.Image, x, y int, cor color.RGBA) bool {
	c := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
	return distancia(c, cor) < distancia(c, fundo)
}

func distancia(a, b color.RGBA) int {
	dr, dg, db := int(a.R)-int(b.R), int(a.G)-int(b.G), int(a.B)-int(b.B)
	return dr*dr + dg*dg + db*db
}

// TestCaixaNaCorDaFamilia: a caixa sai afastada do objeto pela folga, com o
// traço por dentro, na cor da família; dentro e fora dela o quadro fica como
// era.
func TestCaixaNaCorDaFamilia(t *testing.T) {
	// A caixa vai de (120, 54) a (240, 216) no quadro de 480×270, e a
	// desenhada, de (115, 49) a (245, 221).
	a := Alerta{Quadro: quadroDeTeste(t, false),
		Objetos: []Objeto{{Familia: "pessoa", Caixa: [4]float64{0.25, 0.2, 0.5, 0.8}}}}
	quadro, err := quadroDesenhado(a)
	if err != nil {
		t.Fatal(err)
	}
	img := decodifica(t, quadro)
	if img.Bounds() != image.Rect(0, 0, 480, 270) {
		t.Fatalf("o quadro mudou de tamanho: %v", img.Bounds())
	}
	verde := coresDasFamilias["pessoa"]
	for _, p := range []image.Point{{180, 49}, {180, 220}, {115, 130}, {244, 130}} {
		if !perto(img, p.X, p.Y, verde) {
			t.Errorf("o traço não está em %v", p)
		}
	}
	// A folga: a borda da caixa do detector de objetos fica livre.
	for _, p := range []image.Point{{180, 54}, {120, 130}, {180, 130}, {100, 130}} {
		if perto(img, p.X, p.Y, verde) {
			t.Errorf("pintou %v, fora do traço", p)
		}
	}
}

// TestPrioridadePorCima: com duas caixas no mesmo lugar, a da família de
// maior prioridade, que vem primeiro, fica por cima.
func TestPrioridadePorCima(t *testing.T) {
	caixa := [4]float64{0.25, 0.2, 0.5, 0.8}
	a := Alerta{Quadro: quadroDeTeste(t, false), Objetos: []Objeto{
		{Familia: "pessoa", Caixa: caixa}, {Familia: "veiculo", Caixa: caixa}}}
	quadro, err := quadroDesenhado(a)
	if err != nil {
		t.Fatal(err)
	}
	c := color.RGBAModel.Convert(decodifica(t, quadro).At(180, 49)).(color.RGBA)
	if distancia(c, coresDasFamilias["pessoa"]) >= distancia(c, coresDasFamilias["veiculo"]) {
		t.Errorf("em cima ficou %v, e não a pessoa", c)
	}
}

// TestContornoTracejado: o contorno da área sai tracejado: na linha de cima
// de um retângulo, o meio de cada traço de 6 px pintado, e o meio de cada vão de 4 px, não.
func TestContornoTracejado(t *testing.T) {
	// A linha de cima vai de x = 48 a x = 432, em y = 54.
	a := Alerta{Quadro: quadroDeTeste(t, false), Objetos: []Objeto{},
		Areas: []AreaDoAlerta{{Nome: "Portão", Pontos: []area.Ponto{{0.1, 0.2}, {0.9, 0.2}, {0.9, 0.8}, {0.1, 0.8}}}}}
	quadro, err := quadroDesenhado(a)
	if err != nil {
		t.Fatal(err)
	}
	img := decodifica(t, quadro)
	ligados, desligados := 0, 0
	for x := 48; x < 48+10*20; x++ {
		if (x-48)%10 == 2 && !perto(img, x, 54, corDaArea) {
			ligados++
		}
		if (x-48)%10 == 7 && perto(img, x, 54, corDaArea) {
			desligados++
		}
	}
	if ligados > 0 || desligados > 0 {
		t.Errorf("em 20 traços, %d sem tinta no meio do traço e %d pintados no meio do vão", ligados, desligados)
	}
	if perto(img, 240, 135, corDaArea) {
		t.Error("pintou dentro da área")
	}
}

// TestCaixaNaBorda: a caixa colada na borda para no quadro, sem pânico, e a
// fora do quadro não desenha nada.
func TestCaixaNaBorda(t *testing.T) {
	a := Alerta{Quadro: quadroDeTeste(t, false), Objetos: []Objeto{
		{Familia: "animal", Caixa: [4]float64{0, 0, 0.1, 0.1}},
		{Familia: "outra", Caixa: [4]float64{0.9, 0.9, 1, 1}},
		{Familia: "pessoa", Caixa: [4]float64{1.2, 1.2, 1.5, 1.5}}}}
	quadro, err := quadroDesenhado(a)
	if err != nil {
		t.Fatal(err)
	}
	img := decodifica(t, quadro)
	if !perto(img, 0, 10, coresDasFamilias["animal"]) || !perto(img, 479, 260, corDesconhecida) {
		t.Error("a caixa da borda não ficou no quadro")
	}
}

// TestQuadroQueNaoDecodifica: o quadro estragado é erro, e a Central manda o
// quadro como veio.
func TestQuadroQueNaoDecodifica(t *testing.T) {
	a := alertaDe(1)
	if _, err := quadroDesenhado(a); err == nil {
		t.Fatal("desenhou num quadro que não é JPEG")
	}
	if got := NovaCentral(nil, mudo()).comQuadroDesenhado(a); string(got.Quadro) != "jpeg" {
		t.Errorf("o quadro virou %q", got.Quadro)
	}
}

// TestMeioRecebeOQuadroDesenhado: o meio recebe o quadro desenhado, e não o
// olhado.
func TestMeioRecebeOQuadroDesenhado(t *testing.T) {
	meio := &meioDeTeste{chegou: make(chan Alerta, 1)}
	c := NovaCentral([]Notificador{meio}, mudo())
	ctx := t.Context()
	go c.Roda(ctx)

	a := alertaDe(1)
	a.Quadro = quadroDeTeste(t, false)
	a.Objetos[0].Caixa = [4]float64{0.25, 0.2, 0.5, 0.8}
	c.Recebe(a)
	recebido := <-meio.chegou
	if bytes.Equal(recebido.Quadro, a.Quadro) {
		t.Fatal("o meio recebeu o quadro olhado")
	}
	if !perto(decodifica(t, recebido.Quadro), 180, 49, coresDasFamilias["pessoa"]) {
		t.Error("o quadro que o meio recebeu não tem a caixa")
	}
}

// BenchmarkQuadroDesenhado mede o desenho de um alerta típico, duas pessoas
// e uma área, num quadro de 480×270 com textura de câmera.
func BenchmarkQuadroDesenhado(b *testing.B) {
	a := Alerta{Quadro: quadroDeTeste(b, true),
		Objetos: []Objeto{
			{Familia: "pessoa", Caixa: [4]float64{0.25, 0.2, 0.35, 0.6}},
			{Familia: "pessoa", Caixa: [4]float64{0.6, 0.3, 0.7, 0.9}}},
		Areas: []AreaDoAlerta{{Nome: "Portão",
			Pontos: []area.Ponto{{0.1, 0.3}, {0.5, 0.25}, {0.8, 0.5}, {0.7, 0.95}, {0.15, 0.9}}}}}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := quadroDesenhado(a); err != nil {
			b.Fatal(err)
		}
	}
}
