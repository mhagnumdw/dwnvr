package recorder

import (
	"math"
	"math/rand/v2"
	"testing"
)

// A estimativa não pode seguir a rajada: ela só atrasa a chegada, e o quadro
// que chega sem atraso extra é a medida certa. Aqui 1 em cada 20 quadros chega
// no atraso mínimo e o resto até 1,1s depois - a oscilação medida na rede real.
func TestRelogioIgnoraRajada(t *testing.T) {
	c := relogio{balde: relogioBalde.Milliseconds()}
	rng := rand.New(rand.NewPCG(1, 2))
	const inicioParede, atrasoMin = int64(1_789_151_000_000), int64(300)

	for i := range 15 * 120 { // 2 minutos a 15fps
		midia := int64(i) * 1000 / 15
		atraso := atrasoMin
		if i%20 != 0 {
			atraso += rng.Int64N(1100)
		}
		c.observar(midia, inicioParede+midia+atraso)
	}

	midia := int64(15*120) * 1000 / 15
	got, ok := c.parede(midia)
	if want := inicioParede + midia + atrasoMin; !ok || got != want {
		t.Fatalf("parede = %d (ok=%v), esperava %d: errou %dms", got, ok, want, got-want)
	}
}

func TestRelogioSemObservacao(t *testing.T) {
	var c relogio
	if _, ok := c.parede(0); ok {
		t.Fatal("sem observação não há estimativa")
	}
}

func TestInicioEmendado(t *testing.T) {
	const emenda, meio, tol, buraco = int64(100_000), int64(33), int64(250), int64(1000)
	tests := []struct {
		nome string
		alvo int64
		want int64
	}{
		{"dentro da tolerância fica na emenda", emenda + 200, emenda},
		{"dentro da tolerância, adiantada", emenda - 200, emenda},
		{"atrasada: meio quadro para frente", emenda + 600, emenda + meio},
		{"adiantada: meio quadro para trás", emenda - 600, emenda - meio},
		{"adiantada muito: ainda só meio quadro", emenda - 60_000, emenda - meio},
		{"atrasada além do buraco: pula para o relógio", emenda + 10_000, emenda + 10_000},
	}
	for _, tt := range tests {
		t.Run(tt.nome, func(t *testing.T) {
			if got := inicioEmendado(emenda, tt.alvo, meio, tol, buraco); got != tt.want {
				t.Errorf("inicioEmendado = %d, esperava %d", got, tt.want)
			}
		})
	}
}

// sim reproduz o segmenter sobre uma conexão sintética: quadros a 15fps, um
// keyframe a cada 2s, corte depois de 30s de mídia. O relógio da câmera anda
// `deriva` mais rápido que o real (negativa: mais devagar), a rede atrasa cada
// quadro entre 300ms e 1,4s, e `congela` quadros depois do índice indicado
// chegam `congelaMs` mais tarde sem que a mídia conte esse tempo.
type sim struct {
	deriva    float64
	horas     float64
	congela   int
	congelaMs float64
	antigo    bool // usa a regra de antes: sempre na emenda quando o relógio fica atrás
}

type segSim struct {
	inicio, dur int64
	verdade     float64 // quando o keyframe foi entregue sem atraso extra
}

func (p sim) rodar() []segSim {
	const quadroMidia = 1000.0 / 15
	const atrasoMin = 300.0
	rng := rand.New(rand.NewPCG(3, 4))
	c := relogio{balde: relogioBalde.Milliseconds()}

	var segs []segSim
	var lastEnd, midiaInicio int64
	aberto := false
	n := int(p.horas * 3600 * 15)
	for i := range n {
		midia := float64(i) * quadroMidia
		real := 1_789_151_000_000 + midia/(1+p.deriva)
		if p.congela > 0 && i >= p.congela {
			real += p.congelaMs
		}
		chegada := real + atrasoMin + rng.Float64()*1100
		c.observar(int64(midia), int64(chegada))

		keyframe := i%30 == 0
		if !keyframe || (aberto && int64(midia)-midiaInicio < 30_000) {
			continue
		}
		if aberto {
			segs[len(segs)-1].dur = int64(midia) - midiaInicio
			lastEnd = segs[len(segs)-1].inicio + segs[len(segs)-1].dur
		}
		alvo, _ := c.parede(int64(midia))
		var inicio int64
		switch {
		case !aberto:
			inicio = max(alvo, lastEnd)
		case p.antigo:
			inicio = max(int64(chegada), lastEnd)
		default:
			inicio = inicioEmendado(lastEnd, alvo, 33,
				relogioTolerancia.Milliseconds(), relogioBuraco.Milliseconds())
		}
		segs = append(segs, segSim{inicio: inicio, verdade: real + atrasoMin})
		midiaInicio, aberto = int64(midia), true
	}
	return segs[:len(segs)-1] // o último não fechou
}

// O defeito de 11/09/2026: a cam_zeta corre 0,04% à frente do relógio real,
// e a regra antiga, que só empurrava o início para a emenda, deixava a timeline
// ir se adiantando segmento após segmento. Em 8h passa de 10s.
func TestRegraAntigaAcumulaDeriva(t *testing.T) {
	segs := sim{deriva: 0.0004, horas: 8, antigo: true}.rodar()
	ultimo := segs[len(segs)-1]
	if erro := float64(ultimo.inicio) - ultimo.verdade; erro < 10_000 {
		t.Fatalf("a simulação deveria reproduzir a deriva da regra antiga; erro final %.0fms", erro)
	}
}

// A regra nova mantém a timeline perto do relógio nos dois sentidos de deriva,
// e cada emenda sobrepõe ou afasta no máximo meio quadro - que o MSE toca sem
// perder quadro e sem parar.
//
// O primeiro segmento da conexão é exceção conhecida: ele só tem a chegada do
// próprio keyframe como medida, e herda a rajada dela. A simulação usa o pior
// caso da Fase 2 (até 1,1s); medido no go2rtc real em 11/09/2026, o primeiro
// fragmento chegou no máximo 74ms acima do atraso mínimo. Daí em diante a
// correção de meio quadro por segmento tira o excesso, e o teste confere que
// ele só diminui e some em 15 minutos.
func TestTimelineSegueRelogioSemPerderQuadro(t *testing.T) {
	for _, tt := range []struct {
		nome   string
		deriva float64
	}{
		{"câmera no ritmo", 0},
		{"câmera adiantada 0,04%, a cam_zeta", 0.0004},
		{"câmera atrasada 0,04%", -0.0004},
		{"câmera adiantada 0,08%, o dobro da cam_zeta", 0.0008},
	} {
		t.Run(tt.nome, func(t *testing.T) {
			segs := sim{deriva: tt.deriva, horas: 8}.rodar()
			// O erro tolerado é a tolerância mais o que os quadros entregues
			// com atraso mínimo deixam de folga dentro de um balde.
			limite := float64(relogioTolerancia.Milliseconds()) + 60
			pior := math.Abs(float64(segs[0].inicio) - segs[0].verdade)
			for k, s := range segs {
				if k > 0 {
					fimAnterior := segs[k-1].inicio + segs[k-1].dur
					if d := s.inicio - fimAnterior; d < -33 || d > 33 {
						t.Fatalf("segmento %d: emenda de %dms, passa de meio quadro", k, d)
					}
				}
				erro := math.Abs(float64(s.inicio) - s.verdade)
				if k < 30 { // os primeiros 15 minutos
					if erro > max(pior, limite) {
						t.Fatalf("segmento %d: erro subiu para %.0fms na convergência", k, erro)
					}
					pior = max(erro, limite)
					continue
				}
				if erro > limite {
					t.Fatalf("segmento %d (%.1fh): timeline a %.0fms do relógio",
						k, float64(k)*30/3600, erro)
				}
			}
		})
	}
}

// Câmera que congela 10s e volta sem saltar o relógio dela: a mídia não contou
// esse tempo, e a timeline tem que mostrar um buraco em vez de ficar 10s
// atrasada até a próxima reconexão.
func TestCongelamentoViraBuraco(t *testing.T) {
	const congelaEm = 15 * 600 // aos 10 minutos
	segs := sim{horas: 0.5, congela: congelaEm, congelaMs: 10_000}.rodar()

	buracos := 0
	for k := 1; k < len(segs); k++ {
		if segs[k].inicio-(segs[k-1].inicio+segs[k-1].dur) > 5000 {
			buracos++
		}
	}
	if buracos != 1 {
		t.Fatalf("esperava um buraco depois do congelamento, vieram %d", buracos)
	}
	// Os dois baldes do estimador precisam esvaziar do passado antes do pulo:
	// até 3 segmentos depois do congelamento o erro ainda pode ser o dele.
	for k, s := range segs {
		if float64(k)*30 < 600+3*30 {
			continue
		}
		if erro := float64(s.inicio) - s.verdade; erro < -400 || erro > 400 {
			t.Fatalf("segmento %d: timeline a %.0fms do relógio depois do congelamento", k, erro)
		}
	}
}
