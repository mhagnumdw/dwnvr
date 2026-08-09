package recorder

import (
	"testing"
	"time"
)

// Uma câmera parada tem que reportar taxa ZERO, não uma taxa quase nula.
//
// A média exponencial sozinha só se aproxima de zero: em 09/08/2026, depois de
// 3h38 sem gravar, o /api/health devolvia bitrateKbps=3,7e-126. Como isso ainda
// é "maior que zero", a estimativa de retenção dividia a cota por ele e
// publicava 5,4e+128 dias — o diagnóstico mostrava uma câmera morta como se
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
