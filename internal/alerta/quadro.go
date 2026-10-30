package alerta

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"slices"

	"github.com/mhagnumdw/dwnvr/internal/area"
	"github.com/mhagnumdw/dwnvr/internal/detect"
)

// O quadro do alerta é o quadro olhado com o que fez o alerta sair por cima:
// a caixa de cada objeto que passou na regra, na cor da família, e o contorno
// tracejado das áreas do alerta. É o mesmo desenho que Detecções mostra com o
// filtro "só alertas", sem as caixas que não entraram no alerta e sem rótulo:
// a biblioteca padrão do Go não traz fonte, e o texto da mensagem já diz cada
// objeto e as áreas em que caiu.
//
// Desenha a Central, uma vez por alerta, na goroutine de envio, e todos os
// meios recebem o mesmo quadro: nem a fila do detector de objetos nem o meio
// fazem esse trabalho.

// coresDasFamilias são as da tela (web/src/lib/icones.ts): a caixa do e-mail
// tem a cor da caixa de Detecções.
var coresDasFamilias = map[detect.Familia]color.RGBA{
	detect.Pessoa:  {0x3f, 0xb9, 0x50, 0xff},
	detect.Veiculo: {0xa3, 0x71, 0xf7, 0xff},
	detect.Animal:  {0xdb, 0x61, 0xa2, 0xff},
}

// corDesconhecida é a da família que a tela não conhece, a mesma de lá.
var corDesconhecida = color.RGBA{0x8b, 0x94, 0x9e, 0xff}

// corDaArea é o `--accent` do web/src/app.css, a cor do contorno da área na
// tela.
var corDaArea = color.RGBA{0x2f, 0x81, 0xf7, 0xff}

// quadroDesenhado devolve o JPEG do quadro do alerta com as caixas e as áreas
// desenhadas. O erro é o do quadro que não decodifica ou não codifica: quem
// chama decide mandar o quadro como veio.
func quadroDesenhado(a Alerta) ([]byte, error) {
	src, err := jpeg.Decode(bytes.NewReader(a.Quadro))
	if err != nil {
		return nil, fmt.Errorf("o quadro não decodifica: %w", err)
	}
	// O JPEG decodifica em YCbCr com a cor em meia resolução; desenhar nele
	// borraria o traço. O draw.Draw de YCbCr para RGBA tem caminho rápido na
	// biblioteca padrão.
	b := src.Bounds()
	img := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(img, img.Bounds(), src, b.Min, draw.Src)

	for _, ar := range a.Areas {
		contornoTracejado(img, ar.Pontos, corDaArea)
	}
	// Ao contrário da ordem dos objetos, que vem com a família de maior
	// prioridade primeiro: a caixa dela fica por cima das outras.
	for _, o := range slices.Backward(a.Objetos) {

		cor, ok := coresDasFamilias[detect.Familia(o.Familia)]
		if !ok {
			cor = corDesconhecida
		}
		contornoDaCaixa(img, o.Caixa, cor)
	}

	// O desenhado sai com 1,4 a 1,5 vez o tamanho do olhado (medido em 4
	// quadros reais): o dobro reservado evita crescer o buffer no meio.
	var saida bytes.Buffer
	saida.Grow(len(a.Quadro) * 2)
	if err := jpeg.Encode(&saida, img, &jpeg.Options{Quality: QualidadeDoQuadro}); err != nil {
		return nil, fmt.Errorf("o quadro não codifica: %w", err)
	}
	return saida.Bytes(), nil
}

// contornoDaCaixa desenha o retângulo da caixa [x1, y1, x2, y2], em fração do
// quadro, afastado do objeto por CaixaFolgaPx e com o traço por dentro, como a
// tela (caixas.ts). Perto da borda a caixa para no quadro: a folga some só
// daquele lado.
func contornoDaCaixa(img *image.RGBA, caixa [4]float64, cor color.RGBA) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	r := image.Rect(
		int(math.Floor(caixa[0]*float64(w)))-CaixaFolgaPx,
		int(math.Floor(caixa[1]*float64(h)))-CaixaFolgaPx,
		int(math.Ceil(caixa[2]*float64(w)))+CaixaFolgaPx,
		int(math.Ceil(caixa[3]*float64(h)))+CaixaFolgaPx,
	).Intersect(img.Rect)
	if r.Empty() {
		return
	}
	t := CaixaTracoPx
	tinta := image.NewUniform(cor)
	// Quatro faixas cheias: o draw.Draw de uma cor só sobre RGBA é uma cópia
	// de linha, sem conta por pixel.
	for _, faixa := range [...]image.Rectangle{
		image.Rect(r.Min.X, r.Min.Y, r.Max.X, r.Min.Y+t), // em cima
		image.Rect(r.Min.X, r.Max.Y-t, r.Max.X, r.Max.Y), // embaixo
		image.Rect(r.Min.X, r.Min.Y, r.Min.X+t, r.Max.Y), // à esquerda
		image.Rect(r.Max.X-t, r.Min.Y, r.Max.X, r.Max.Y), // à direita
	} {
		draw.Draw(img, faixa.Intersect(r), tinta, image.Point{}, draw.Src)
	}
}

// contornoTracejado desenha o polígono da área, fechado, em traços de
// AreaTracoLigadoPx com vãos de AreaTracoDesligadoPx, como o setLineDash da
// tela (area.ts). O tracejado segue de um lado ao outro sem recomeçar no
// vértice, como no canvas.
func contornoTracejado(img *image.RGBA, pontos []area.Ponto, cor color.RGBA) {
	if len(pontos) < 2 {
		return
	}
	w, h := float64(img.Rect.Dx()), float64(img.Rect.Dy())
	periodo := float64(AreaTracoLigadoPx + AreaTracoDesligadoPx)
	andado := 0.0
	for i, p := range pontos {
		q := pontos[(i+1)%len(pontos)]
		x0, y0 := p[0]*w, p[1]*h
		dx, dy := q[0]*w-x0, q[1]*h-y0
		lado := math.Hypot(dx, dy)
		// Um pixel por passo: nenhum buraco no traço, e um perímetro de
		// quadro inteiro a 480 px são uns 1500 passos.
		for s := 0.0; s < lado; s++ {
			if math.Mod(andado+s, periodo) < AreaTracoLigadoPx {
				ponto(img, x0+dx*s/lado, y0+dy*s/lado, cor)
			}
		}
		andado += lado
	}
}

// ponto pinta um quadrado de AreaTracoPx de lado centrado em (x, y), o pincel
// do contorno da área. O que cai fora do quadro fica de fora.
func ponto(img *image.RGBA, x, y float64, cor color.RGBA) {
	x0 := int(math.Round(x - AreaTracoPx/2.0))
	y0 := int(math.Round(y - AreaTracoPx/2.0))
	r := image.Rect(x0, y0, x0+AreaTracoPx, y0+AreaTracoPx).Intersect(img.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, cor)
		}
	}
}
