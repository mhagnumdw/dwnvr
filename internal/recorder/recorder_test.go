package recorder

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

// mudo evita que o teste precise de um logger de verdade só para exercitar o
// alarme, que loga por definição.
func mudo() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// Uma câmera parada tem que reportar taxa ZERO, não uma taxa quase nula.
//
// A média exponencial sozinha só se aproxima de zero: em 09/08/2026, depois de
// 3h38 sem gravar, o /api/health devolvia bitrateKbps=3,7e-126. Como isso ainda
// é "maior que zero", a estimativa de retenção dividia a cota por ele e
// publicava 5,4e+128 dias - o diagnóstico mostrava uma câmera morta como se
// estivesse saudável.
func TestBitrateZeraQuandoNaoChegaByte(t *testing.T) {
	r := &Recorder{}
	t0 := time.Now()

	// Duas amostras com tráfego: a taxa sobe.
	r.sampleBitrate(t0)
	r.bytes.Store(1 << 20) // 1 MiB
	r.sampleBitrate(t0.Add(15 * time.Second))

	if r.bitrateKbps <= 0 {
		t.Fatalf("taxa deveria ser positiva com tráfego, veio %v", r.bitrateKbps)
	}

	// A partir daqui nenhum byte novo entra.
	for i := 1; i <= 3; i++ {
		r.sampleBitrate(t0.Add(time.Duration(15+15*i) * time.Second))
		if r.bitrateKbps != 0 {
			t.Fatalf("amostra %d sem tráfego: taxa %v, esperava 0", i, r.bitrateKbps)
		}
	}
}

// A estimativa de retenção não pode explodir quando a taxa é desprezível: é
// exatamente daí que saía "5,4e+128 dias" na tela.
//
// Sem histórico em disco não há densidade, então quem responde é a taxa - e é
// justamente aí que ela precisa do piso.
func TestRetencaoNaoEstimaComTaxaDesprezivel(t *testing.T) {
	tests := []struct {
		nome   string
		taxa   float64
		estima bool
	}{
		{"parada", 0, false},
		{"resíduo de média exponencial", 3.7e-126, false},
		{"abaixo do piso", 0.5, false},
		{"câmera de verdade", 900, true},
	}
	for _, tt := range tests {
		t.Run(tt.nome, func(t *testing.T) {
			dias := retainDays(20480, 0, 0, tt.taxa)

			if tt.estima && dias <= 0 {
				t.Errorf("taxa %v deveria produzir estimativa, veio %v", tt.taxa, dias)
			}
			if !tt.estima && dias != 0 {
				t.Errorf("taxa %v não deveria produzir estimativa, veio %v", tt.taxa, dias)
			}
		})
	}
}

// A estimativa sai da densidade do que já está gravado, e não da taxa do
// instante. O caso ancorado é a cam_frente em 18/08/2026, que motivou a
// mudança: 20478 MB de gravação cobrindo 8,23 dias, com cota de 20480 MB.
//
// Com a cota cheia, "cabem" tem que dar o mesmo que o "retido" mostra ao lado -
// o número velho, tirado da taxa daquele instante, dizia 9,6 dias de madrugada e
// ~5 de tarde, e era essa oscilação que fazia a tela parecer errada.
func TestRetencaoUsaDensidadeDoQueEstaGravado(t *testing.T) {
	const (
		dia   = int64(86400000)
		bytes = int64(20478) << 20
		span  = int64(8.23 * float64(dia))
	)

	// Uma taxa absurda junto, de propósito: se a densidade não tivesse
	// precedência, ela apareceria no resultado.
	dias := retainDays(20480, bytes, span, 5000)
	if dias < 8.1 || dias > 8.4 {
		t.Errorf("dias = %v, esperava ~8,23 (a densidade real, não a taxa)", dias)
	}

	// A conta é linear na cota: dobrar a cota dobra o que cabe. É disso que a
	// tela de edição depende para responder "e se eu puser 40 GB?".
	if dobro := retainDays(40960, bytes, span, 5000); dobro < 2*dias-0.01 || dobro > 2*dias+0.01 {
		t.Errorf("cota dobrada = %v, esperava o dobro de %v", dobro, dias)
	}
}

// Câmera recém-cadastrada é quando a estimativa mais serve - é com ela que se
// escolhe a cota - e é quando não há histórico de onde tirar densidade. Abaixo
// do span mínimo, quem responde é a taxa.
func TestRetencaoCaiNaTaxaComHistoricoCurto(t *testing.T) {
	const bytes = int64(200) << 20

	curto := retainDays(20480, bytes, minSpanForEstimate-1, 900)
	porTaxa := retainDays(20480, 0, 0, 900)
	if curto != porTaxa {
		t.Errorf("span curto deu %v, esperava a estimativa por taxa (%v)", curto, porTaxa)
	}

	// Um minuto a mais de histórico e a densidade assume.
	longo := retainDays(20480, bytes, minSpanForEstimate+60000, 900)
	if longo == porTaxa {
		t.Errorf("com span suficiente a densidade deveria assumir, veio a taxa (%v)", longo)
	}

	// Sem nenhuma das duas fontes o campo fica zerado e a tela mostra "-".
	if vazio := retainDays(20480, 0, 0, 0); vazio != 0 {
		t.Errorf("sem taxa e sem histórico = %v, esperava 0", vazio)
	}
}

// Um recorder que acabou de subir não pode ser dado como parado: o primeiro
// segmento só fecha depois de segmentSeconds, e alarmar nesse intervalo faria a
// tela gritar a cada reinício.
func TestSilencioToleraSubidaEAlarmaDepois(t *testing.T) {
	r := &Recorder{startedAt: time.Now(), log: mudo()}
	r.cam.SegmentSeconds = 30

	r.mu.RLock()
	limite := r.silenceLimitLocked()
	r.mu.RUnlock()
	if limite != 90*time.Second {
		t.Fatalf("limite = %v, esperava 90s (3 segmentos)", limite)
	}

	// Recém-subido: nada de alarme, e nada logado.
	r.checkSilence(r.startedAt.Add(60 * time.Second))
	if r.silentLogged {
		t.Error("alarmou antes de o primeiro segmento poder existir")
	}

	// Passou do limite sem fechar segmento nenhum: alarma.
	r.checkSilence(r.startedAt.Add(2 * time.Minute))
	if !r.silentLogged {
		t.Fatal("deveria ter alarmado depois de 2min sem gravar")
	}

	// Fechou um segmento: volta ao normal.
	r.setLastEndMs(r.startedAt.Add(2 * time.Minute).UnixMilli())
	r.checkSilence(r.startedAt.Add(2*time.Minute + time.Second))
	if r.silentLogged {
		t.Error("deveria ter voltado ao normal depois de gravar")
	}
}

// O piso de um minuto protege quem usa segmentos curtos: 3×5s alarmaria a cada
// hesitação de rede.
func TestLimiteDeSilencioTemPiso(t *testing.T) {
	r := &Recorder{}
	r.cam.SegmentSeconds = 5
	r.mu.RLock()
	defer r.mu.RUnlock()
	if got := r.silenceLimitLocked(); got != time.Minute {
		t.Errorf("limite = %v, esperava o piso de 1min", got)
	}
}
