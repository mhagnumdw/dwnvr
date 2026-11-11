package recorder

import (
	"math"
	"math/rand/v2"
	"testing"
)

// A estimativa não pode seguir a rajada: ela só atrasa a chegada, e o quadro
// que chega sem atraso extra é a medida certa. Aqui 1 em cada 20 quadros chega
// no atraso mínimo e o resto até 1,1s depois - a oscilação medida na rede real.
func TestClockIgnoresBursts(t *testing.T) {
	c := mediaClock{bucket: clockBucket.Milliseconds()}
	rng := rand.New(rand.NewPCG(1, 2))
	const wallStart, minDelay = int64(1_789_151_000_000), int64(300)

	for i := range 15 * 120 { // 2 minutos a 15fps
		media := int64(i) * 1000 / 15
		delay := minDelay
		if i%20 != 0 {
			delay += rng.Int64N(1100)
		}
		c.observe(media, wallStart+media+delay)
	}

	media := int64(15*120) * 1000 / 15
	got, ok := c.wall(media)
	if want := wallStart + media + minDelay; !ok || got != want {
		t.Fatalf("parede = %d (ok=%v), esperava %d: errou %dms", got, ok, want, got-want)
	}
}

func TestClockWithoutObservation(t *testing.T) {
	var c mediaClock
	if _, ok := c.wall(0); ok {
		t.Fatal("sem observação não há estimativa")
	}
}

func TestSplicedStart(t *testing.T) {
	const splice, half, tol, gap = int64(100_000), int64(33), int64(250), int64(1000)
	tests := []struct {
		name   string
		target int64
		want   int64
	}{
		{"dentro da tolerância fica na emenda", splice + 200, splice},
		{"dentro da tolerância, adiantada", splice - 200, splice},
		{"atrasada: meio quadro para frente", splice + 600, splice + half},
		{"adiantada: meio quadro para trás", splice - 600, splice - half},
		{"adiantada muito: ainda só meio quadro", splice - 60_000, splice - half},
		{"atrasada além do buraco: pula para o relógio", splice + 10_000, splice + 10_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := splicedStart(splice, tt.target, half, tol, gap); got != tt.want {
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
type simulation struct {
	drift    float64
	hours    float64
	freeze   int
	freezeMs float64
	old      bool // usa a regra de antes: sempre na emenda quando o relógio fica atrás
}

type simSeg struct {
	start, dur int64
	truth      float64 // quando o keyframe foi entregue sem atraso extra
}

func (p simulation) run() []simSeg {
	const mediaFrame = 1000.0 / 15
	const minDelay = 300.0
	rng := rand.New(rand.NewPCG(3, 4))
	c := mediaClock{bucket: clockBucket.Milliseconds()}

	var segs []simSeg
	var lastEnd, mediaStart int64
	opened := false
	n := int(p.hours * 3600 * 15)
	for i := range n {
		media := float64(i) * mediaFrame
		real := 1_789_151_000_000 + media/(1+p.drift)
		if p.freeze > 0 && i >= p.freeze {
			real += p.freezeMs
		}
		arrival := real + minDelay + rng.Float64()*1100
		c.observe(int64(media), int64(arrival))

		keyframe := i%30 == 0
		if !keyframe || (opened && int64(media)-mediaStart < 30_000) {
			continue
		}
		if opened {
			segs[len(segs)-1].dur = int64(media) - mediaStart
			lastEnd = segs[len(segs)-1].start + segs[len(segs)-1].dur
		}
		target, _ := c.wall(int64(media))
		var start int64
		switch {
		case !opened:
			start = max(target, lastEnd)
		case p.old:
			start = max(int64(arrival), lastEnd)
		default:
			start = splicedStart(lastEnd, target, 33,
				clockTolerance.Milliseconds(), clockGap.Milliseconds())
		}
		segs = append(segs, simSeg{start: start, truth: real + minDelay})
		mediaStart, opened = int64(media), true
	}
	return segs[:len(segs)-1] // o último não fechou
}

// O defeito de 11/09/2026: a cam_zeta corre 0,04% à frente do relógio real,
// e a regra antiga, que só empurrava o início para a emenda, deixava a timeline
// ir se adiantando segmento após segmento. Em 8h passa de 10s.
func TestOldRuleAccumulatesDrift(t *testing.T) {
	segs := simulation{drift: 0.0004, hours: 8, old: true}.run()
	last := segs[len(segs)-1]
	if errMsg := float64(last.start) - last.truth; errMsg < 10_000 {
		t.Fatalf("a simulação deveria reproduzir a deriva da regra antiga; erro final %.0fms", errMsg)
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
func TestTimelineFollowsTheClockWithoutLosingFrames(t *testing.T) {
	for _, tt := range []struct {
		name  string
		drift float64
	}{
		{"câmera no ritmo", 0},
		{"câmera adiantada 0,04%, a cam_zeta", 0.0004},
		{"câmera atrasada 0,04%", -0.0004},
		{"câmera adiantada 0,08%, o dobro da cam_zeta", 0.0008},
	} {
		t.Run(tt.name, func(t *testing.T) {
			segs := simulation{drift: tt.drift, hours: 8}.run()
			// O erro tolerado é a tolerância mais o que os quadros entregues
			// com atraso mínimo deixam de folga dentro de um balde.
			limit := float64(clockTolerance.Milliseconds()) + 60
			worst := math.Abs(float64(segs[0].start) - segs[0].truth)
			for k, s := range segs {
				if k > 0 {
					prevEnd := segs[k-1].start + segs[k-1].dur
					if d := s.start - prevEnd; d < -33 || d > 33 {
						t.Fatalf("segmento %d: emenda de %dms, passa de meio quadro", k, d)
					}
				}
				errMsg := math.Abs(float64(s.start) - s.truth)
				if k < 30 { // os primeiros 15 minutos
					if errMsg > max(worst, limit) {
						t.Fatalf("segmento %d: erro subiu para %.0fms na convergência", k, errMsg)
					}
					worst = max(errMsg, limit)
					continue
				}
				if errMsg > limit {
					t.Fatalf("segmento %d (%.1fh): timeline a %.0fms do relógio",
						k, float64(k)*30/3600, errMsg)
				}
			}
		})
	}
}

// Câmera que congela 10s e volta sem saltar o relógio dela: a mídia não contou
// esse tempo, e a timeline tem que mostrar um buraco em vez de ficar 10s
// atrasada até a próxima reconexão.
func TestFreezeBecomesAGap(t *testing.T) {
	const freezeAt = 15 * 600 // aos 10 minutos
	segs := simulation{hours: 0.5, freeze: freezeAt, freezeMs: 10_000}.run()

	gaps := 0
	for k := 1; k < len(segs); k++ {
		if segs[k].start-(segs[k-1].start+segs[k-1].dur) > 5000 {
			gaps++
		}
	}
	if gaps != 1 {
		t.Fatalf("esperava um buraco depois do congelamento, vieram %d", gaps)
	}
	// Os dois baldes do estimador precisam esvaziar do passado antes do pulo:
	// até 3 segmentos depois do congelamento o erro ainda pode ser o dele.
	for k, s := range segs {
		if float64(k)*30 < 600+3*30 {
			continue
		}
		if errMsg := float64(s.start) - s.truth; errMsg < -400 || errMsg > 400 {
			t.Fatalf("segmento %d: timeline a %.0fms do relógio depois do congelamento", k, errMsg)
		}
	}
}
