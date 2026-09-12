package recorder

// relogio estima em que instante do relógio de parede cada quadro de uma
// conexão foi entregue, casando o relógio de mídia da câmera (tfdt) com a hora
// em que os fragmentos chegam.
//
// Nenhum dos dois serve sozinho. A chegada oscila com a rede, que entrega em
// rajada - medido em ±1,1s num segmento de 30s. O relógio de mídia é estável,
// mas anda no ritmo do cristal da câmera, não no do relógio real: a
// cam_lateral2 corre 0,04% à frente, e em 11/09/2026 a timeline dela chegou a
// marcar 21,8s a mais que a hora impressa na imagem.
//
// A diferença chegada - mídia é o atraso da entrega somado a uma constante que
// deriva devagar. O atraso nunca é negativo, então o MENOR valor visto numa
// janela é o quadro que chegou com o atraso mínimo: é a estimativa que a rajada
// não contamina, porque a rajada só atrasa. A janela desliza em baldes para
// seguir a deriva - um mínimo de toda a conexão ficaria preso ao passado quando
// a câmera anda mais devagar que o relógio real.
type relogio struct {
	balde int64 // quantos ms de mídia cada balde cobre

	inicio   int64 // mídia (ms) em que o balde atual começou
	atual    int64 // menor chegada - mídia do balde atual
	anterior int64 // o mesmo, do balde que acabou de fechar

	temAtual, temAnterior bool
}

// observar registra um fragmento: seu instante de mídia e a hora em que chegou,
// os dois em ms.
func (c *relogio) observar(midiaMs, chegadaMs int64) {
	d := chegadaMs - midiaMs
	switch {
	case !c.temAtual:
		c.inicio, c.atual, c.temAtual = midiaMs, d, true
	case midiaMs-c.inicio >= c.balde:
		c.anterior, c.temAnterior = c.atual, true
		c.inicio, c.atual = midiaMs, d
	default:
		c.atual = min(c.atual, d)
	}
}

// parede traduz um instante de mídia para o relógio de parede. O falso só sai
// antes da primeira observação.
//
// Olha os dois baldes: o atual pode ter acabado de abrir com um único quadro,
// e um quadro só é justamente a medida que a rajada contamina.
func (c *relogio) parede(midiaMs int64) (int64, bool) {
	if !c.temAtual {
		return 0, false
	}
	d := c.atual
	if c.temAnterior {
		d = min(d, c.anterior)
	}
	return midiaMs + d, true
}

// inicioEmendado decide onde começa, na timeline, um segmento que continua o
// anterior na mesma conexão.
//
// Dentro de uma conexão a mídia é contínua, então o lugar natural é a emenda -
// o fim do segmento anterior. Mas ancorar SEMPRE ali acumula a diferença de
// ritmo entre a câmera e o relógio real, segmento após segmento, até a próxima
// reconexão. Por isso a emenda se aproxima do relógio estimado (alvo) quando a
// diferença passa da tolerância.
//
// A aproximação é de meio quadro por segmento, e é esse tamanho que a torna
// gratuita. Para trás, o segmento novo sobrepõe menos que um quadro do
// anterior, e o MSE só descarta um quadro sobreposto quando o novo começa no
// mesmo instante que ele - aqui nenhum quadro se perde. Para frente, abre um
// vão menor que um quadro, que o navegador emenda sem parar a reprodução. Meio
// quadro a cada 30s corrige até 0,1% a 15fps, folga de sobra sobre os 0,04%
// medidos.
//
// A exceção é a timeline atrasada além de `buraco`: aí a mídia deixou de contar
// um tempo que passou de fato - a câmera congelou e retomou sem saltar o
// relógio dela - e o honesto é mostrar um buraco, como qualquer outra falta de
// gravação. O atraso de uma câmera lenta nunca chega lá, porque os passos de
// meio quadro o corrigem antes. Adiantada, não há o que pular: saltar para trás
// sobreporia gravação, então segue corrigindo de meio em meio quadro.
func inicioEmendado(emenda, alvo, meioQuadro, tolerancia, buraco int64) int64 {
	atraso := alvo - emenda // positivo: a timeline está atrás do relógio
	switch {
	case atraso > buraco:
		return alvo
	case atraso > tolerancia:
		return emenda + meioQuadro
	case atraso < -tolerancia:
		return emenda - meioQuadro
	default:
		return emenda
	}
}
