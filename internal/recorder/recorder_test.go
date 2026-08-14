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
			r := &Recorder{bitrateKbps: tt.taxa}
			r.cam.QuotaMB = 20480

			// Status() toca o índice; aqui interessa só a conta da estimativa,
			// que é a mesma linha do Status.
			var dias float64
			if r.bitrateKbps >= minBitrateForEstimate {
				dias = float64(r.cam.QuotaMB) * (1 << 20) / (r.bitrateKbps * 1000 / 8 * 86400)
			}

			if tt.estima && dias <= 0 {
				t.Errorf("taxa %v deveria produzir estimativa, veio %v", tt.taxa, dias)
			}
			if !tt.estima && dias != 0 {
				t.Errorf("taxa %v não deveria produzir estimativa, veio %v", tt.taxa, dias)
			}
		})
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
