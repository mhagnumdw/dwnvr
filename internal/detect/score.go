package detect

import "math"

// Score responde "o quanto ESTE quadro destoa do que esta câmera costuma
// fazer". Zero é o normal dela; 3 é três desvios acima do normal dela.
//
// Ele não sabe o que é um onset e não decide nada: quem decide é o Mecanismo.
// Essa separação é o que permite regular todas as funções no mesmo custo e
// comparar o que elas pegam - foi assim que a função padrão foi escolhida entre
// duas dezenas de candidatas.
//
// Não é seguro para uso concorrente: cada câmera tem o seu, e só a goroutine
// dela o toca.
type Score interface {
	// Empurra recebe UM quadro e devolve o destaque dele. `bytes` é a caixa
	// mdat inteira, cabeçalho incluído, que é o número que o recorder tem na
	// mão sem conta nenhuma. `dtMs` é quanto tempo passou desde o quadro
	// anterior.
	Empurra(bytes int, keyframe bool, dtMs float64) float64

	// Zera esquece tudo. Buraco de gravação ou reconexão: o que veio antes não
	// vale mais.
	Zera()

	// Aquecimento é quantos quadros a função precisa ver depois de cada Zera
	// antes de o número dela valer.
	Aquecimento() int

	Nome() string
}

// FuncoesDeScore é o mapa que faz trocar de função ser trocar uma string, para
// que a função padrão possa ser substituída sem tocar em quem a usa.
//
// Só as que venceram a medição estão aqui. Função nova entra depois de medida
// contra estas, no mesmo custo.
var FuncoesDeScore = map[string]func() Score{
	"kleinberg":   func() Score { return novoKleinberg() },
	"kleinberg-p": func() Score { return soP(novoKleinberg()) },
}

// --- média e desvio exponenciais --------------------------------------------

// ewma é a média e o desvio exponenciais sobre os quais o `kleinberg` mede o z.
//
// A meia vida é em QUADROS, não em segundos: as câmeras vão de 10 a 15 fps e a
// régua tem que ser a mesma em número de amostras.
type ewma struct {
	alfa        float64
	media, vari float64
	n           int
	temMedia    bool
}

func novoEwma(meiaVida float64) *ewma {
	return &ewma{alfa: 1 - math.Exp(math.Log(0.5)/meiaVida)}
}

func (e *ewma) zera() { e.media, e.vari, e.n, e.temMedia = 0, 0, 0, false }

func (e *ewma) empurra(x float64) {
	e.n++
	if !e.temMedia {
		e.media, e.vari, e.temMedia = x, 0, true
		return
	}
	d := x - e.media
	e.media += e.alfa * d
	e.vari = (1 - e.alfa) * (e.vari + e.alfa*d*d)
}

func (e *ewma) desvio() float64 { return math.Sqrt(e.vari) }

// --- o `kleinberg` ----------------------------------------------------------

// kleinberg é um detector de rajada em dois estados, "calmo" e "agitado".
//
// Ele foi feito exatamente para a pergunta "quando começou a rajada num
// fluxo", e aqui está na forma online e barata: a razão de verossimilhança
// entre os dois estados, acumulada com um custo para trocar de estado.
//
// Ganhou das outras candidatas com folga de sete erros padrão sobre a segunda
// colocada, e não se degrada à noite - metade das horas de um NVR é noite.
type kleinberg struct {
	e              *ewma
	calmo, agitado float64
}

func novoKleinberg() *kleinberg {
	k := &kleinberg{e: novoEwma(MeiaVidaKleinberg)}
	k.Zera()
	return k
}

func (k *kleinberg) Nome() string     { return "kleinberg" }
func (k *kleinberg) Aquecimento() int { return AquecimentoKleinberg }

func (k *kleinberg) Zera() {
	k.e.zera()
	k.calmo, k.agitado = 0, -CustoTrocaKleinberg
}

func (k *kleinberg) Empurra(bytes int, _ bool, _ float64) float64 {
	// O log é o que põe câmeras de 28 e de 305 kbps na mesma escala: o que
	// interessa é a RAZÃO entre o quadro e o normal dela, não a diferença.
	x := math.Log(float64(max(bytes, 1)))

	if !k.e.temMedia || k.e.n < QuadrosAntesDoPrimeiroDestaque {
		k.e.empurra(x)
		return 0
	}

	z := (x - k.e.media) / math.Max(k.e.desvio(), PisoDesvioLog)
	// Aprende só fora do evento. Ver CongelaAcima.
	if z < CongelaAcima {
		k.e.empurra(x)
	}

	// Log-verossimilhança de cada estado. O agitado espera a média
	// FatorKleinberg desvios acima.
	lc := -0.5 * z * z
	la := -0.5 * (z - FatorKleinberg) * (z - FatorKleinberg)
	novoCalmo := math.Max(k.calmo, k.agitado-CustoTrocaKleinberg) + lc
	novoAgitado := math.Max(k.agitado, k.calmo-CustoTrocaKleinberg) + la

	// Normaliza, senão os dois afundam juntos e o float estoura.
	piso := math.Max(novoCalmo, novoAgitado)
	k.calmo, k.agitado = novoCalmo-piso, novoAgitado-piso
	return k.agitado - k.calmo
}

// --- a variante que esconde o frame I ---------------------------------------

// semFrameI embrulha uma função e esconde dela os frames I.
//
// O frame I não mede movimento, mede a CENA inteira de novo: ele volta a cada
// GOP tenha acontecido alguma coisa ou não, e é 27 a 258 vezes maior que um P
// de cena parada. Como ele é raro - 1,6% a 5% dos quadros -, uma função
// regulada em 50 onsets/hora gastaria o orçamento INTEIRO decidindo qual frame
// I é grande demais, e o movimento, que mora nos P, nunca chegaria a ser
// olhado.
//
// O efeito disso, medido, não é sutil: as candidatas se partem em duas, com as
// sete funções que não veem o frame I acima da régua e as sete que veem abaixo
// dela - pior que disparar a esmo.
//
// Custa zero: o recorder já tem esse bit na mão.
type semFrameI struct {
	dentro Score
	ultimo float64
}

func soP(dentro Score) Score { return &semFrameI{dentro: dentro} }

func (s *semFrameI) Nome() string     { return s.dentro.Nome() + "-p" }
func (s *semFrameI) Aquecimento() int { return s.dentro.Aquecimento() }

func (s *semFrameI) Zera() {
	s.dentro.Zera()
	s.ultimo = 0
}

func (s *semFrameI) Empurra(bytes int, keyframe bool, dtMs float64) float64 {
	if keyframe {
		return s.ultimo
	}
	s.ultimo = s.dentro.Empurra(bytes, keyframe, dtMs)
	return s.ultimo
}
