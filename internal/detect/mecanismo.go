package detect

import "math"

// Mecanismo decide QUANDO disparar e ONDE olhar. Ele recebe os quadros na
// ordem em que chegam e responde, quadro a quadro, se aquele instante é um
// onset - o instante que acorda o detector de objetos - e qual quadro o
// detector deve olhar por causa dele.
//
// São duas perguntas porque as respostas são instantes diferentes. A MARCA fica
// no onset, que é o que casa com a chegada. O QUADRO a olhar é o de maior
// destaque nos JanelaDoPicoMs seguintes: no onset o objeto ainda está entrando,
// pela metade, e olhar no pico leva `pessoa` de 9,0% para 12,2% das chegadas,
// sem uma detecção a mais.
//
// É a peça que o usuário troca no cadastro da câmera, e por isso ela é um
// contrato: o `kleinberg-p` é o estatístico, que gasta cada olhada onde a
// imagem mudou, e o `periodico` é a régua honesta a bater, que a UI oferece a
// quem preferir um custo previsível a um custo bem gasto. Mecanismo novo entra
// no mapa Mecanismos.
//
// Não é seguro para uso concorrente: cada câmera tem o seu.
type Mecanismo interface {
	// Quadro recebe UM quadro de VÍDEO - áudio nunca chega aqui - e diz o que
	// fazer com ele. Ver Veredito.
	//
	// `instanteMs` é o relógio de parede do quadro, `bytes` é a caixa mdat
	// inteira e `keyframe` diz se é frame I. É O(1) e não aloca: roda para
	// todas as câmeras, a 15 quadros por segundo, num Orange Pi Zero 3.
	Quadro(instanteMs int64, bytes int, keyframe bool) Veredito

	// Zera esquece tudo, inclusive a janela do pico que estiver aberta. O
	// recorder chama isto a cada reconexão.
	Zera()

	Nome() string
}

// Veredito é a resposta do Mecanismo a um quadro. São três avisos
// independentes, e quem os recebe os atende NESTA ordem - é ela que faz um
// quadro poder, em tese, fechar a janela de um onset e abrir a do seguinte:
//
//  1. Corta: a janela do onset anterior acabou ANTES deste quadro. O pedaço do
//     GOP vai até o último Candidato, e este quadro fica de fora.
//  2. Onset: este quadro abre um intervalo de movimento. É a marca da timeline.
//  3. Candidato: o quadro a olhar passa a ser ESTE. O pedaço, quando cortado,
//     vai até ele, inclusive.
//
// O quadro do onset é sempre o primeiro candidato: se nada depois dele destacar
// mais, olhar no pico é olhar no onset - e é o que acontece em 60% das vezes.
type Veredito struct {
	Corta     bool
	Onset     bool
	Candidato bool
}

// Mecanismos é o mapa de construtores: trocar de mecanismo é trocar a string
// do campo `detectMecanismo` da câmera. O nível é o de 1 a 5 do cadastro, e é
// ele que carrega o custo - cada mecanismo sabe traduzi-lo no que lhe cabe.
var Mecanismos = map[string]func(nivel int) Mecanismo{
	"kleinberg-p": func(nivel int) Mecanismo {
		return novoEstatistico(FuncoesDeScore["kleinberg-p"](), LimiarKleinbergP[nivel])
	},
	"periodico": func(nivel int) Mecanismo {
		return novoPeriodico(3_600_000 / OnsetsPorHora[nivel])
	},
}

// --- a janela do pico -------------------------------------------------------

// janelaDoPico acompanha, depois de cada onset, qual quadro destacou mais.
//
// É a mesma conta sobre a qual o ganho de olhar no pico foi medido: começa no
// próprio onset, só quadro P entra, e só quem destaca ESTRITAMENTE mais toma o
// lugar. Frame I fica fora pelo mesmo motivo
// que fica fora do score - ele é grande sempre, com ou sem movimento.
type janelaDoPico struct {
	larguraMs int64
	aberta    bool
	onsetMs   int64
	melhor    float64
}

// abre começa a janela de um onset, com ele mesmo como o melhor até aqui.
func (j *janelaDoPico) abre(instanteMs int64, destaque float64) {
	j.aberta, j.onsetMs, j.melhor = true, instanteMs, destaque
}

// fecha encerra a janela e diz se havia uma aberta. É o que o buraco de
// gravação e o Zera usam: depois deles o vídeo é outro, e o pico não pode vir
// de lá.
func (j *janelaDoPico) fecha() bool {
	estava := j.aberta
	j.aberta = false
	return estava
}

// passou fecha a janela se este quadro já está fora dela.
func (j *janelaDoPico) passou(instanteMs int64) bool {
	if !j.aberta || instanteMs <= j.onsetMs+j.larguraMs {
		return false
	}
	j.aberta = false
	return true
}

// supera diz se este quadro passa a ser o candidato.
func (j *janelaDoPico) supera(destaque float64, keyframe bool) bool {
	if !j.aberta || keyframe || destaque <= j.melhor {
		return false
	}
	j.melhor = destaque
	return true
}

// Novo devolve o mecanismo de uma câmera. Nome desconhecido ou nível fora da
// faixa caem no padrão: uma configuração digitada errada não pode deixar a
// câmera sem gatilho em silêncio.
func Novo(mecanismo string, nivel int) Mecanismo {
	if nivel < NivelMin || nivel > NivelMax {
		nivel = NivelPadrao
	}
	novo, ok := Mecanismos[mecanismo]
	if !ok {
		novo = Mecanismos[MecanismoPadrao]
	}
	return novo(nivel)
}

// --- o relógio dos quadros --------------------------------------------------

// relogio mede quanto tempo passou entre um quadro e o seguinte, e é onde o
// buraco de gravação é percebido.
type relogio struct {
	ultimoMs int64
	tem      bool
}

func (r *relogio) zera() { r.tem = false }

// passo devolve o tempo desde o quadro anterior e se houve buraco no meio. O
// primeiro quadro depois de um zera não é buraco: não há antes nenhum para
// comparar, e o estado já está limpo.
func (r *relogio) passo(instanteMs int64) (dtMs float64, lacuna bool) {
	anterior, tinha := r.ultimoMs, r.tem
	r.ultimoMs, r.tem = instanteMs, true
	if !tinha {
		return 0, false
	}
	d := instanteMs - anterior
	// Relógio para trás é tão pouco confiável quanto buraco: placas sem RTC
	// sobem com a data errada e o NTP corrige depois.
	if d < 0 || d > LacunaMs {
		return 0, true
	}
	return float64(d), false
}

// --- o mecanismo estatístico: score + histerese -----------------------------

// estatistico é o Score de uma câmera mais o gatilho de histerese que
// transforma o destaque dele em onsets.
//
// Duas escolhas mudam o número, e por isso ficam escritas:
//
// O ONSET SAI NA ABERTURA, não depois de confirmar. Ele é o instante em que o
// pedaço do GOP é despachado, e esperar confirmação custaria atraso - que é
// justamente o que faz o episódio se perder. Onset 9 segundos atrasado é o
// detector olhando um quintal vazio.
//
// NÃO HÁ REFRATÁRIO AQUI. A histerese já é o mecanismo que impede o mesmo
// evento de disparar vinte vezes; um refratário por cima misturaria duas
// coisas no mesmo número, e foi assim que os limiares foram medidos. O freio
// da fila (PedacosPorCamera) é outra coisa: ele protege o ORÇAMENTO, e mora do
// lado do detector.
type estatistico struct {
	score       Score
	alto, baixo float64
	fecharMs    int64

	rel     relogio
	quadros int

	aberto        bool
	temAbaixo     bool
	abaixoDesdeMs int64

	pico janelaDoPico
}

func novoEstatistico(score Score, alto float64) *estatistico {
	return &estatistico{
		score:    score,
		alto:     alto,
		baixo:    LimiarBaixo(alto),
		fecharMs: int64(SegundosParaFechar * 1000),
		pico:     janelaDoPico{larguraMs: JanelaDoPicoMs},
	}
}

// LimiarBaixo é o limiar de fechar, a partir do de abrir.
//
// Escrito com valor absoluto porque nem todo destaque é positivo - o
// `kleinberg` devolve uma diferença de log-verossimilhanças, que passa a maior
// parte do tempo abaixo de zero. Com limiar alto positivo isto é exatamente
// `FracaoLimiarBaixo * alto`; com limiar negativo, ele DESCE em vez de subir,
// que é o que "fechar" tem que significar nos dois casos.
func LimiarBaixo(alto float64) float64 {
	return alto - (1-FracaoLimiarBaixo)*math.Max(math.Abs(alto), 1e-9)
}

func (m *estatistico) Nome() string { return m.score.Nome() }

func (m *estatistico) Zera() {
	m.score.Zera()
	m.rel.zera()
	m.quadros = 0
	m.aberto, m.temAbaixo = false, false
	m.pico.fecha()
}

func (m *estatistico) Quadro(instanteMs int64, bytes int, keyframe bool) Veredito {
	var v Veredito
	dtMs, lacuna := m.rel.passo(instanteMs)
	if lacuna {
		// O buraco fecha o intervalo à força: o score levou Zera e o que vinha
		// antes não vale mais. A janela do pico fecha junto, e o pedaço que ela
		// esperava sai com o que já tinha.
		m.score.Zera()
		m.quadros = 0
		m.aberto, m.temAbaixo = false, false
		v.Corta = m.pico.fecha()
	}

	destaque := m.score.Empurra(bytes, keyframe, dtMs)

	if m.pico.passou(instanteMs) {
		v.Corta = true
	}
	v.Candidato = m.pico.supera(destaque, keyframe)

	// Durante o aquecimento a função ainda não tem régua, e o número dela não
	// vale. Disparar aqui seria gastar orçamento com o próprio aquecimento.
	m.quadros++
	if m.quadros <= m.score.Aquecimento() {
		return v
	}

	switch {
	case destaque >= m.alto:
		m.temAbaixo = false
		if m.aberto {
			return v
		}
		m.aberto = true
		v.Onset, v.Candidato = true, true
		m.pico.abre(instanteMs, destaque)

	case destaque < m.baixo:
		if !m.temAbaixo {
			m.temAbaixo, m.abaixoDesdeMs = true, instanteMs
		} else if instanteMs-m.abaixoDesdeMs >= m.fecharMs {
			m.aberto, m.temAbaixo = false, false
		}

	default:
		// Entre os dois limiares o intervalo continua como está, e o relógio
		// de fechar recomeça: é isto que impede o marcador de picotar quando o
		// sinal oscila em volta da linha.
		m.temAbaixo = false
	}
	return v
}

// --- o mecanismo periódico --------------------------------------------------

// periodico dispara de X em X segundos, ignorando o sinal por completo.
//
// Ele é a régua a bater: se a estatística não ganhar dele com folga, ela não
// valeu a pena. No nível 4 os dois gastam as mesmas 50 detecções por hora, e o
// `kleinberg-p` entrega 41,5% das chegadas onde este entrega 15,1%.
//
// Continua existindo por dois motivos: é o custo mais previsível que existe -
// exatamente 3600/período por hora, chova ou faça sol - e é a segunda opinião
// de quem desconfiar do gatilho estatístico numa câmera específica.
//
// Ele não tem destaque, então não tem pico: olha no próprio onset. A janela
// dele tem largura zero e fecha no quadro seguinte.
type periodico struct {
	periodoMs float64
	desdeMs   float64
	rel       relogio
	pico      janelaDoPico
}

func novoPeriodico(periodoMs float64) *periodico {
	return &periodico{periodoMs: periodoMs}
}

func (p *periodico) Nome() string { return "periodico" }

func (p *periodico) Zera() {
	p.rel.zera()
	p.desdeMs = 0
	p.pico.fecha()
}

func (p *periodico) Quadro(instanteMs int64, _ int, _ bool) Veredito {
	var v Veredito
	dtMs, lacuna := p.rel.passo(instanteMs)
	if lacuna {
		// Depois de reconectar a câmera espera um período inteiro, em vez de
		// disparar na cara do reinício.
		p.desdeMs = 0
		v.Corta = p.pico.fecha()
		return v
	}
	v.Corta = p.pico.passou(instanteMs)

	p.desdeMs += dtMs
	if p.desdeMs < p.periodoMs {
		return v
	}
	// Desconta o período em vez de zerar: zerar joga fora a fração do quadro
	// que sobrou e o disparo anda para a frente a cada volta, o que faria o
	// custo deixar de ser exatamente 3600/período - que é a única razão de
	// este mecanismo existir.
	p.desdeMs -= p.periodoMs
	p.pico.abre(instanteMs, 0)
	v.Onset, v.Candidato = true, true
	return v
}
