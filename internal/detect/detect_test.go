package detect

import (
	"math"
	"strconv"
	"testing"
)

// --- o score -----------------------------------------------------------------

// cenaParada devolve o tamanho do i-ésimo quadro P de uma cena sem movimento:
// ~1000 bytes com um ruído determinístico de ±10%, que é o que a compressão
// faz com o granulado de uma câmera parada.
func cenaParada(i int) int { return 900 + (i*7919)%200 }

// TestSemFrameINaoMexeNoDestaque é a propriedade que faz o `kleinberg-p`
// funcionar: o frame I volta a cada GOP, com ou sem movimento, e é dezenas de
// vezes maior que um P. Se ele chegasse à função, cada GOP pareceria um evento.
func TestSemFrameINaoMexeNoDestaque(t *testing.T) {
	cru := FuncoesDeScore["kleinberg"]()
	semI := FuncoesDeScore["kleinberg-p"]()

	var antes float64
	for i := range 400 {
		cru.Empurra(cenaParada(i), false, 67)
		antes = semI.Empurra(cenaParada(i), false, 67)
	}

	const frameI = 50_000
	if d := cru.Empurra(frameI, true, 67); d < LimiarKleinbergP[NivelPadrao] {
		t.Fatalf("sem esconder o frame I ele teria que destacar: %g", d)
	}
	if d := semI.Empurra(frameI, true, 67); d != antes {
		t.Errorf("o frame I mexeu no destaque: %g, antes %g", d, antes)
	}
}

// TestEventoLongoNaoViraONovoNormal confere o CongelaAcima: um movimento mais
// longo que a meia vida da média não pode ser aprendido como o normal da
// câmera. Sem congelar, a base persegue o evento e o destaque cai no meio dele.
func TestEventoLongoNaoViraONovoNormal(t *testing.T) {
	s := FuncoesDeScore["kleinberg"]()
	i := 0
	for ; i < 400; i++ {
		s.Empurra(cenaParada(i), false, 67)
	}
	// Três vezes a meia vida de movimento contínuo, a 3x o tamanho normal.
	var d float64
	for range 3 * int(MeiaVidaKleinberg) {
		d = s.Empurra(3*cenaParada(i), false, 67)
		i++
	}
	if d < LimiarKleinbergP[NivelPadrao] {
		t.Errorf("no fim do evento o destaque caiu para %g: a base absorveu o evento", d)
	}
}

// TestRajadaViraUmOnsetSo confere o score e o gatilho juntos, no mecanismo
// padrão: cena parada não dispara, e um movimento de um segundo e meio - os
// quadros P crescem quando a imagem muda - dispara uma vez, e não uma por
// quadro.
func TestRajadaViraUmOnsetSo(t *testing.T) {
	m := Novo(MecanismoPadrao, NivelPadrao)
	agora := int64(1_000_000)
	onsets := 0
	empurra := func(bytes int) {
		agora += 67
		if m.Quadro(agora, bytes, false).Onset {
			onsets++
		}
	}

	i := 0
	for ; i < 600; i++ {
		empurra(cenaParada(i))
	}
	if onsets != 0 {
		t.Fatalf("cena parada disparou %d vezes", onsets)
	}

	for range 22 {
		empurra(5 * cenaParada(i))
		i++
	}
	for range 100 {
		empurra(cenaParada(i))
		i++
	}
	if onsets != 1 {
		t.Errorf("a rajada deu %d onsets, esperava 1", onsets)
	}
}

// TestPicoEscolheOMaiorQuadroP confere o quadro a olhar: o de maior destaque
// nos JanelaDoPicoMs depois do onset, só entre quadros P, e só quem destaca
// ESTRITAMENTE mais toma o lugar.
func TestPicoEscolheOMaiorQuadroP(t *testing.T) {
	type q struct {
		destaque float64
		keyframe bool
	}
	fita := []q{
		{0, false}, {0, false},
		{20, false}, // onset
		{25, false},
		{40, true},  // o maior, mas é frame I: fica fora
		{30, false}, // o pico
		{30, false}, // empata: não toma o lugar
		{12, false},
	}
	valores := make([]float64, 0, len(fita))
	for _, f := range fita {
		valores = append(valores, f.destaque)
	}
	m := novoEstatistico(&scoreFalso{valores: valores}, 10)

	agora := int64(1_000_000)
	var candidato int64
	pico := agora + 6*100
	for i := 0; ; i++ {
		agora += 100
		k := false
		if i < len(fita) {
			k = fita[i].keyframe
		}
		v := m.Quadro(agora, 1000, k)
		if v.Candidato {
			candidato = agora
		}
		if v.Corta {
			break
		}
		if agora > 1_000_000+2*JanelaDoPicoMs {
			t.Fatal("a janela do pico não fechou")
		}
	}
	if candidato != pico {
		t.Errorf("olhou o quadro de %d ms, o pico está em %d ms",
			candidato-1_000_000, pico-1_000_000)
	}
}

// --- o gatilho de histerese -------------------------------------------------

// scoreFalso devolve os destaques de uma lista, na ordem. É como se testa o
// gatilho sem depender de estatística nenhuma.
type scoreFalso struct {
	valores     []float64
	i           int
	aquecimento int
}

func (s *scoreFalso) Nome() string     { return "falso" }
func (s *scoreFalso) Aquecimento() int { return s.aquecimento }
func (s *scoreFalso) Zera()            { s.i = 0 }
func (s *scoreFalso) Empurra(int, bool, float64) float64 {
	v := s.valores[min(s.i, len(s.valores)-1)]
	s.i++
	return v
}

// piloto empurra quadros num mecanismo com um relógio que ANDA entre as
// chamadas. Ter um relógio só é o ponto: rebobiná-lo entre dois trechos do
// mesmo teste seria um salto para trás, que o mecanismo trata - com razão -
// como buraco de gravação.
type piloto struct {
	m       Mecanismo
	agora   int64
	passoMs int64
}

func novoPiloto(m Mecanismo, passoMs int64) *piloto {
	return &piloto{m: m, agora: 1_000_000, passoMs: passoMs}
}

// roda empurra `quantos` quadros seguidos e devolve os instantes em que o
// gatilho abriu.
func (p *piloto) roda(quantos int) []int64 {
	var out []int64
	for range quantos {
		p.agora += p.passoMs
		if p.m.Quadro(p.agora, 1000, false).Onset {
			out = append(out, p.agora)
		}
	}
	return out
}

// pula abre um buraco de gravação antes do próximo quadro.
func (p *piloto) pula(ms int64) { p.agora += ms }

func TestHistereseNaoPicotaUmEventoSo(t *testing.T) {
	// Um evento que balança em volta do limiar alto: sobe, cai para o meio da
	// faixa, sobe de novo. Sem histerese isso viraria três marcadores.
	alto := 10.0
	valores := []float64{0, 0, 0, 20, 5, 5, 20, 5, 20, 0, 0, 0}
	onsets := novoPiloto(novoEstatistico(&scoreFalso{valores: valores}, alto), 100).
		roda(len(valores))
	if len(onsets) != 1 {
		t.Fatalf("esperava 1 onset, veio %d: %v", len(onsets), onsets)
	}
}

func TestHistereseSoFechaDepoisDeSegundosParaFechar(t *testing.T) {
	alto := 10.0
	// Sobe, e depois fica abaixo do limiar baixo. Com quadros de 100 ms, os 3 s
	// de SegundosParaFechar são 30 quadros - só a partir do 31º abaixo o
	// intervalo fecha e um novo pico pode abrir outro.
	valores := make([]float64, 0, 80)
	valores = append(valores, 20)
	for range 25 {
		valores = append(valores, 0)
	}
	valores = append(valores, 20) // cedo demais: o intervalo ainda está aberto
	for range 40 {
		valores = append(valores, 0)
	}
	valores = append(valores, 20) // agora sim

	onsets := novoPiloto(novoEstatistico(&scoreFalso{valores: valores}, alto), 100).
		roda(len(valores))
	if len(onsets) != 2 {
		t.Fatalf("esperava 2 onsets (o do meio suprimido pela histerese), veio %d: %v",
			len(onsets), onsets)
	}
}

func TestAquecimentoNaoDispara(t *testing.T) {
	// Destaque altíssimo desde o primeiro quadro: nada pode sair enquanto a
	// função não tiver régua.
	valores := []float64{999}
	p := novoPiloto(novoEstatistico(&scoreFalso{valores: valores, aquecimento: 300}, 10), 100)

	if onsets := p.roda(300); len(onsets) != 0 {
		t.Fatalf("disparou durante o aquecimento: %v", onsets)
	}
	if onsets := p.roda(1); len(onsets) != 1 {
		t.Fatal("não disparou no primeiro quadro depois do aquecimento")
	}
}

func TestBuracoDeGravacaoZeraTudo(t *testing.T) {
	m := novoEstatistico(&scoreFalso{valores: []float64{999}, aquecimento: 300}, 10)
	p := novoPiloto(m, 100)

	p.roda(301) // aquece e abre o intervalo
	if !m.aberto {
		t.Fatal("o intervalo devia estar aberto")
	}

	// Um salto maior que LacunaMs é buraco de gravação: o aquecimento recomeça
	// e o intervalo fecha à força.
	p.pula(LacunaMs + 5_000)
	if onsets := p.roda(1); len(onsets) != 0 {
		t.Error("disparou no primeiro quadro depois do buraco, ainda sem régua")
	}
	if m.aberto {
		t.Error("o buraco tinha que fechar o intervalo")
	}
	if m.quadros != 1 {
		t.Errorf("o aquecimento não recomeçou: %d quadros", m.quadros)
	}
}

// --- o mecanismo periódico --------------------------------------------------

// TestPeriodicoCustaExatamenteOQuePromete é a razão de este mecanismo existir:
// ser a régua exata a bater. Se o custo dele escorregar, ele deixa de ser
// régua.
func TestPeriodicoCustaExatamenteOQuePromete(t *testing.T) {
	for nivel := NivelMin; nivel <= NivelMax; nivel++ {
		// Uma hora de câmera a 15 quadros por segundo, com quadros de 67 ms -
		// não divisor do período, de propósito: é o resto acumulado que o
		// "desconta em vez de zerar" existe para não perder.
		const passoMs, quantos = 67, 3_600_000 / 67
		onsets := novoPiloto(Novo("periodico", nivel), passoMs).roda(quantos)

		esperado := OnsetsPorHora[nivel]
		if diff := math.Abs(float64(len(onsets)) - esperado); diff > 1 {
			t.Errorf("nível %d: %d onsets na hora, esperado %.0f",
				nivel, len(onsets), esperado)
		}
	}
}

func TestPeriodicoEsperaUmPeriodoInteiroDepoisDoBuraco(t *testing.T) {
	p := novoPiloto(Novo("periodico", 5), 67) // 100/hora = um a cada 36 s
	p.roda(500)                               // 33,5 s: quase lá

	// O buraco tem que jogar fora o que já esperou.
	p.pula(LacunaMs + 1)
	if onsets := p.roda(1); len(onsets) != 0 {
		t.Error("disparou na cara do reinício")
	}
	if onsets := p.roda(400); len(onsets) != 0 {
		t.Errorf("disparou antes de um período inteiro: %v", onsets)
	}
}

func TestPeriodicoOlhaNoProprioOnset(t *testing.T) {
	m := Novo("periodico", 5) // um a cada 36 s
	agora := int64(1_000_000)
	var onsetMs int64
	for range 2 * 36_000 / 67 {
		agora += 67
		v := m.Quadro(agora, 1000, false)
		if v.Onset {
			if !v.Candidato {
				t.Fatal("o onset do periódico tem que ser o quadro a olhar")
			}
			onsetMs = agora
			continue
		}
		if v.Candidato {
			t.Fatal("o periódico não tem pico: nenhum quadro depois do onset é candidato")
		}
		if onsetMs != 0 {
			if !v.Corta {
				t.Fatal("a janela do periódico tem que fechar no quadro seguinte ao onset")
			}
			return
		}
	}
	t.Fatal("o periódico não disparou")
}

func TestBuracoCortaAJanelaAberta(t *testing.T) {
	valores := []float64{999}
	m := novoEstatistico(&scoreFalso{valores: valores, aquecimento: 10}, 10)
	p := novoPiloto(m, 100)
	p.roda(11) // aquece e abre
	if !m.pico.aberta {
		t.Fatal("a janela do pico devia estar aberta")
	}
	p.pula(LacunaMs + 1)
	p.agora += p.passoMs
	if v := m.Quadro(p.agora, 1000, false); !v.Corta {
		t.Error("o buraco tem que cortar o pedaço que esperava a janela")
	}
}

// --- o recortador -----------------------------------------------------------

// fita monta quadros sintéticos para o recortador: o moof diz o instante, o
// mdat diz se é I ou P. Assim o pedaço cortado se lê de volta como texto.
type fita struct {
	r     *Recortador
	agora int64
	saiu  []Pedaco
}

func novaFita() *fita {
	f := &fita{r: NovoRecortador("cam"), agora: 1000}
	f.r.Init([]byte("[init]"))
	return f
}

func (f *fita) quadro(v Veredito, keyframe bool) {
	f.agora += 100
	tipo := "P"
	if keyframe {
		tipo = "I"
	}
	moof := []byte("<" + strconv.FormatInt(f.agora, 10))
	mdat := []byte(tipo + ">")
	if p, ok := f.r.Quadro(v, f.agora, keyframe, moof, mdat); ok {
		f.saiu = append(f.saiu, p)
	}
}

var (
	nada      = Veredito{}
	onset     = Veredito{Onset: true, Candidato: true}
	candidato = Veredito{Candidato: true}
	corta     = Veredito{Corta: true}
)

func TestRecortadorCortaDoFrameIAteOCandidato(t *testing.T) {
	f := novaFita()
	f.quadro(nada, true)       // 1100 I
	f.quadro(nada, false)      // 1200
	f.quadro(onset, false)     // 1300
	f.quadro(nada, false)      // 1400
	f.quadro(candidato, false) // 1500: o pico
	f.quadro(nada, false)      // 1600
	f.quadro(corta, false)     // 1700: fora da janela

	if len(f.saiu) != 1 {
		t.Fatalf("%d pedaços, esperado 1", len(f.saiu))
	}
	p := f.saiu[0]
	want := "[init]<1100I><1200P><1300P><1400P><1500P>"
	if got := string(p.Fmp4); got != want {
		t.Errorf("pedaço\n got %s\nwant %s", got, want)
	}
	if p.OnsetMs != 1300 || p.QuadroMs != 1500 || p.Camera != "cam" {
		t.Errorf("onset %d, quadro %d, câmera %q", p.OnsetMs, p.QuadroMs, p.Camera)
	}
}

// TestRecortadorGuardaOGOPDoCandidatoQuandoChegaFrameI é o motivo de haver dois
// buffers: o pico ficou no GOP velho, e o frame I que chegou depois não pode
// apagá-lo.
func TestRecortadorGuardaOGOPDoCandidatoQuandoChegaFrameI(t *testing.T) {
	f := novaFita()
	f.quadro(nada, true)       // 1100 I
	f.quadro(onset, false)     // 1200
	f.quadro(candidato, false) // 1300: o pico
	f.quadro(nada, true)       // 1400 I: GOP novo, dentro da janela
	f.quadro(nada, false)      // 1500
	f.quadro(nada, true)       // 1600 I: e outro
	f.quadro(corta, false)     // 1700

	want := "[init]<1100I><1200P><1300P>"
	if len(f.saiu) != 1 || string(f.saiu[0].Fmp4) != want {
		t.Fatalf("pedaços %q, esperado [%s]", pedacos(f.saiu), want)
	}
}

func TestRecortadorSegueOCandidatoParaOGOPNovo(t *testing.T) {
	f := novaFita()
	f.quadro(nada, true)       // 1100 I
	f.quadro(onset, false)     // 1200
	f.quadro(nada, true)       // 1300 I
	f.quadro(nada, false)      // 1400
	f.quadro(candidato, false) // 1500: o pico, já no GOP novo
	f.quadro(corta, true)      // 1600 I, e fora da janela

	want := "[init]<1300I><1400P><1500P>"
	if len(f.saiu) != 1 || string(f.saiu[0].Fmp4) != want {
		t.Fatalf("pedaços %q, esperado [%s]", pedacos(f.saiu), want)
	}
	// E o GOP seguinte começa limpo.
	f.quadro(onset, false) // 1700
	f.quadro(corta, false)
	if want := "[init]<1600I><1700P>"; len(f.saiu) != 2 || string(f.saiu[1].Fmp4) != want {
		t.Fatalf("pedaços %q, o segundo devia ser %s", pedacos(f.saiu), want)
	}
}

func TestRecortadorNaoCortaSemFrameI(t *testing.T) {
	f := novaFita()
	f.quadro(nada, false)  // a conexão começou no meio de um GOP
	f.quadro(onset, false) // sem o frame I dele, nada se decodifica
	f.quadro(corta, false)
	if len(f.saiu) != 0 {
		t.Fatalf("cortou pedaço sem frame I: %q", pedacos(f.saiu))
	}
}

// TestRecortadorContaOOnsetSemVideo: o onset que termina sem pedaço é contado
// onde termina, e o que ainda espera o pico NÃO é - era isso que a tela de
// Diagnóstico mostrava como perdido durante os segundos da janela.
func TestRecortadorContaOOnsetSemVideo(t *testing.T) {
	f := novaFita()
	f.quadro(nada, false)  // a conexão começou no meio de um GOP
	f.quadro(onset, false) // janela aberta: ainda não é perdido
	if n := f.r.SemVideo(); n != 0 {
		t.Fatalf("contou %d com a janela aberta", n)
	}
	f.quadro(corta, false) // fechou sem frame I
	if n := f.r.SemVideo(); n != 1 {
		t.Fatalf("sem frame I: %d, esperado 1", n)
	}

	f.quadro(nada, true)
	f.quadro(onset, false)
	f.quadro(corta, false) // este sai
	if n := f.r.SemVideo(); n != 1 || len(f.saiu) != 1 {
		t.Fatalf("cortou %d e contou %d sem vídeo, esperado 1 e 1", len(f.saiu), n)
	}

	f.quadro(onset, false)
	f.r.Esquece() // detecção trocada no meio da janela
	if n := f.r.SemVideo(); n != 2 {
		t.Fatalf("esquecido: %d, esperado 2", n)
	}

	f.quadro(nada, true)
	f.quadro(onset, false)
	f.quadro(onset, false) // o mecanismo foi trocado e o pendente não fechou
	if n := f.r.SemVideo(); n != 3 {
		t.Fatalf("substituído: %d, esperado 3", n)
	}
}

func TestRecortadorEncerraDevolveOPendente(t *testing.T) {
	f := novaFita()
	f.quadro(nada, true)
	f.quadro(onset, false)
	p, ok := f.r.Encerra()
	if !ok || string(p.Fmp4) != "[init]<1100I><1200P>" {
		t.Fatalf("Encerra: ok=%v %q", ok, p.Fmp4)
	}
	if _, ok := f.r.Encerra(); ok {
		t.Error("o mesmo pedaço saiu duas vezes")
	}
	// Até o próximo Init, nada é guardado.
	f.quadro(nada, true)
	f.quadro(onset, false)
	if _, ok := f.r.Encerra(); ok {
		t.Error("guardou quadro de uma conexão que já tinha acabado")
	}
}

func TestRecortadorRespeitaOTeto(t *testing.T) {
	f := novaFita()
	f.quadro(nada, true)
	f.quadro(onset, false)
	// Um quadro que estoura o teto não é guardado, e não vira candidato.
	grande := make([]byte, TetoDoGOPBytes)
	if _, ok := f.r.Quadro(candidato, 5000, false, nil, grande); ok {
		t.Fatal("cortou antes da hora")
	}
	f.quadro(corta, false)
	if len(f.saiu) != 1 || string(f.saiu[0].Fmp4) != "[init]<1100I><1200P>" {
		t.Fatalf("pedaços %q: o candidato que cabia tinha que continuar valendo", pedacos(f.saiu))
	}
	if n := cap(f.r.corrente) + cap(f.r.reserva); n > 2*TetoDoGOPBytes {
		t.Errorf("os buffers cresceram para %d bytes", n)
	}
}

func pedacos(ps []Pedaco) []string {
	var out []string
	for _, p := range ps {
		out = append(out, string(p.Fmp4))
	}
	return out
}

func TestRecortadorNaoAlocaAoGuardar(t *testing.T) {
	r := NovoRecortador("cam")
	r.Init(make([]byte, 700))
	moof, mdat := make([]byte, 100), make([]byte, 900)
	agora := int64(0)
	quadro := func(i int) {
		agora += 67
		r.Quadro(nada, agora, i%60 == 0, moof, mdat)
	}
	// Aquece fora da medição: os buffers crescem até o GOP.
	for i := range 240 {
		quadro(i)
	}
	i := 240
	n := testing.AllocsPerRun(2000, func() {
		quadro(i)
		i++
	})
	if n != 0 {
		t.Errorf("%v alocações por quadro guardado, esperado 0", n)
	}
}

// --- os contratos -----------------------------------------------------------

func TestNovoCaiNoPadraoEmVezDeFicarSemGatilho(t *testing.T) {
	casos := []struct {
		mecanismo string
		nivel     int
	}{
		{"nao-existe", 4},
		{"", 4},
		{"kleinberg-p", 0},
		{"kleinberg-p", 9},
	}
	for _, c := range casos {
		m := Novo(c.mecanismo, c.nivel)
		if m == nil {
			t.Fatalf("%q nível %d: sem mecanismo", c.mecanismo, c.nivel)
		}
	}
	// O padrão tem que ser o padrão de verdade, e não um mecanismo qualquer.
	if got := Novo("nao-existe", 4).Nome(); got != MecanismoPadrao {
		t.Errorf("nome do padrão: %q, esperado %q", got, MecanismoPadrao)
	}
	// Nível fora da faixa cai no NivelPadrao, e o limiar tem que ser o dele.
	e := Novo("kleinberg-p", 99).(*estatistico)
	if e.alto != LimiarKleinbergP[NivelPadrao] {
		t.Errorf("limiar %g, esperado o do nível padrão %g", e.alto, LimiarKleinbergP[NivelPadrao])
	}
}

func TestLimiarBaixoDesceMesmoComLimiarNegativo(t *testing.T) {
	if got := LimiarBaixo(10); math.Abs(got-4) > 1e-9 {
		t.Errorf("LimiarBaixo(10) = %g, esperado 4", got)
	}
	if got := LimiarBaixo(-10); got >= -10 {
		t.Errorf("LimiarBaixo(-10) = %g: com limiar negativo fechar é DESCER", got)
	}
}

// TestNaoAlocaNoCaminhoQuente guarda um requisito que não é detalhe: isto roda
// para cada câmera, a 15 quadros por segundo, num Orange Pi Zero 3. Com dez
// câmeras, uma alocação por quadro seriam 150 por segundo só de lixo para o GC
// recolher.
func TestNaoAlocaNoCaminhoQuente(t *testing.T) {
	for nome := range Mecanismos {
		m := Novo(nome, NivelPadrao)
		var t0 int64 = 1_000_000
		// Aquece fora da medição.
		for range 400 {
			t0 += 67
			m.Quadro(t0, 900, false)
		}
		n := testing.AllocsPerRun(2000, func() {
			t0 += 67
			m.Quadro(t0, 900, false)
		})
		if n != 0 {
			t.Errorf("%s: %v alocações por quadro, esperado 0", nome, n)
		}
	}
}

// TestScoreNaoAlocaNoCaminhoQuente é o mesmo requisito do teste acima, mas
// sobre as FUNÇÕES, e não sobre os mecanismos: função nova entra em
// FuncoesDeScore antes de entrar em Mecanismos, e sem isto ela passaria sem
// que ninguém medisse o preço dela por quadro.
func TestScoreNaoAlocaNoCaminhoQuente(t *testing.T) {
	for nome, novo := range FuncoesDeScore {
		s := novo()
		i := 0
		for ; i < 400; i++ {
			s.Empurra(cenaParada(i), false, 67)
		}
		n := testing.AllocsPerRun(2000, func() {
			i++
			s.Empurra(cenaParada(i), false, 67)
		})
		if n != 0 {
			t.Errorf("%s: %v alocações por quadro, esperado 0", nome, n)
		}
	}
}

func TestDetectorNenhumNaoOlhaEIssoEUmResultado(t *testing.T) {
	d := Detectores["nenhum"]("")
	achados, err := d.Olha(t.Context(), Pedaco{Fmp4: []byte("gop")})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(achados) != 0 {
		t.Errorf("achou %d objetos sem olhar", len(achados))
	}
}
