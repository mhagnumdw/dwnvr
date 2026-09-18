package detect

import (
	"math"
	"sort"
)

// Familia é o agrupamento das classes do modelo que a timeline colore. São
// três porque 9 cores não se distinguem num ícone de 14 px; 3 sim.
type Familia string

const (
	Pessoa  Familia = "pessoa"
	Veiculo Familia = "veiculo"
	Animal  Familia = "animal"
)

// Familias são as três, na ordem de prioridade: `pessoa` é a que mais importa.
var Familias = []Familia{Pessoa, Veiculo, Animal}

// Marcador é o quarto contrato: decide, olhada a olhada, o que o detector
// achou que vira MARCA na timeline.
//
// Existe porque a timeline marca CHEGADA, e não presença. Sem ele, o carro
// estacionado acenderia `veiculo` a cada onset - centenas de vezes por dia
// num estacionamento - e um espantalho que o modelo confunde com gente seria uma
// `pessoa` falsa a cada movimento da cena. O que os distingue de uma chegada é a caixa no MESMO LUGAR, e só
// quem lembra das olhadas anteriores enxerga isso.
//
// Por isso ele tem estado, e por isso mora no dwnvr e não no sidecar: o
// detector pode reiniciar, e o dwnvr não pode herdar isso como marca falsa.
//
// Não é seguro para uso concorrente: cada câmera tem o seu, e quem chama
// entrega as olhadas dela uma de cada vez, na ordem em que aconteceram.
type Marcador interface {
	// Olha recebe tudo o que o detector achou numa olhada - toda caixa a partir
	// de PisoDoDetector, de qualquer classe - e devolve as que viram marca.
	//
	// Olhada sem nada também tem que chegar aqui: é ela que conta como falta
	// para os lugares ocupados. Detector que falhou não é olhada, e não chega.
	Olha(achados []Achado) []Achado

	Nome() string
}

// Marcadores é o mapa de construtores, gêmeo dos outros três.
var Marcadores = map[string]func() Marcador{
	"nenhum":   func() Marcador { return semMemoria{} },
	"rastreio": func() Marcador { return novoRastreio(FaltasParaLiberar, IoUDoMesmoLugar) },
}

// NovoMarcador devolve o marcador de uma câmera. Nome desconhecido cai no
// padrão, pelo mesmo motivo do Novo dos mecanismos.
func NovoMarcador(nome string) Marcador {
	novo, ok := Marcadores[nome]
	if !ok {
		novo = Marcadores[MarcadorPadrao]
	}
	return novo()
}

// marcavel diz se a caixa é de uma família e passa do corte dela.
func marcavel(a Achado) (Familia, bool) {
	f, ok := FamiliasPadrao[a.Classe]
	return f, ok && a.Score >= CorteDaFamilia[f]
}

// --- sem memória ------------------------------------------------------------

// semMemoria marca toda caixa de família que passa do corte. É o que se teria
// sem rastreio nenhum, e a régua contra a qual o rastreio foi medido.
type semMemoria struct{}

func (semMemoria) Nome() string { return "nenhum" }

func (semMemoria) Olha(achados []Achado) []Achado {
	var marcas []Achado
	for _, a := range achados {
		if _, ok := marcavel(a); ok {
			marcas = append(marcas, a)
		}
	}
	return marcas
}

// --- o rastreio de lugares ocupados -----------------------------------------

// lugar é um canto do quadro onde uma família foi vista e ainda não sumiu.
type lugar struct {
	familia Familia
	caixa   [4]float64
	faltas  int
}

// rastreio lembra dos lugares OCUPADOS, por família. Uma caixa que cai num
// lugar ocupado não marca e renova o lugar; uma caixa em lugar livre marca, se
// passar do corte, e o ocupa - mesmo abaixo do corte. O lugar é liberado depois
// de `faltas` olhadas seguidas sem ser visto.
//
// A ordem das operações abaixo é a da versão sobre a qual FaltasParaLiberar e
// IoUDoMesmoLugar foram medidos, inclusive nos detalhes que parecem não
// importar: caixas por confiança decrescente, o
// lugar criado nesta olhada já pode ser casado pela caixa seguinte, e no empate
// de IoU vence o lugar mais antigo.
type rastreio struct {
	faltas   int
	iouMin   float64
	ocupados []lugar
}

func novoRastreio(faltas int, iouMin float64) *rastreio {
	return &rastreio{faltas: faltas, iouMin: iouMin}
}

func (r *rastreio) Nome() string { return "rastreio" }

func (r *rastreio) Olha(achados []Achado) []Achado {
	type candidata struct {
		a Achado
		f Familia
	}
	var fila []candidata
	for _, a := range achados {
		if f, ok := FamiliasPadrao[a.Classe]; ok && a.Score >= PisoDoDetector {
			fila = append(fila, candidata{a, f})
		}
	}
	sort.SliceStable(fila, func(i, j int) bool { return fila[i].a.Score > fila[j].a.Score })

	var marcas []Achado
	visto := make([]bool, len(r.ocupados), len(r.ocupados)+len(fila))
	for _, c := range fila {
		casou, melhor := -1, 0.0
		for j, o := range r.ocupados {
			if o.familia != c.f {
				continue
			}
			if v := iou(c.a.Caixa, o.caixa); v > r.iouMin && (casou < 0 || v > melhor) {
				casou, melhor = j, v
			}
		}
		if casou >= 0 {
			r.ocupados[casou].caixa, r.ocupados[casou].faltas = c.a.Caixa, 0
			visto[casou] = true
			continue
		}
		if c.a.Score >= CorteDaFamilia[c.f] {
			marcas = append(marcas, c.a)
		}
		r.ocupados = append(r.ocupados, lugar{familia: c.f, caixa: c.a.Caixa})
		visto = append(visto, true)
	}

	vivos := r.ocupados[:0]
	for j, o := range r.ocupados {
		if !visto[j] {
			o.faltas++
		}
		if o.faltas < r.faltas {
			vivos = append(vivos, o)
		}
	}
	r.ocupados = vivos
	return marcas
}

// iou é a interseção sobre a união de duas caixas (x1, y1, x2, y2), de 0 a 1.
// Não depende da unidade: fração do quadro ou pixel dão o mesmo número.
//
// Cada produto passa por float64() para não virar multiplica-e-soma fundido,
// que o compilador faz no arm64: o resultado mudaria no último bit, e a
// mesma olhada daria decisões diferentes em amd64 e arm64 num empate.
func iou(a, b [4]float64) float64 {
	ix := math.Max(0, math.Min(a[2], b[2])-math.Max(a[0], b[0]))
	iy := math.Max(0, math.Min(a[3], b[3])-math.Max(a[1], b[1]))
	i := float64(ix * iy)
	u := float64((a[2]-a[0])*(a[3]-a[1])) + float64((b[2]-b[0])*(b[3]-b[1])) - i
	if u <= 0 {
		return 0
	}
	return i / u
}
