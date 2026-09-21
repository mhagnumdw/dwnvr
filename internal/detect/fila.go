package detect

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Olhada é o que a fila devolve de cada pedaço atendido: o que o detector
// achou, ou por que não achou nada.
type Olhada struct {
	// Pedaco vai sem os bytes do vídeo: a resposta não precisa deles, e
	// segurá-los até quem recebe terminar seria RAM à toa.
	Pedaco  Pedaco
	Achados []Achado

	// Quadro é o JPEG do quadro olhado, ou nil. Ver Visao.
	Quadro []byte

	Erro error
}

// Fila liga os recorders ao Detector. As câmeras oferecem pedaços; UM
// trabalhador os manda ao detector, um de cada vez, na ordem em que chegaram.
//
// Ela existe porque o detector é lento e as câmeras disparam em rajada: um
// carro passa por três câmeras, a noite cai em todas de uma vez. Sem limite,
// numa reencenação de três dias de gravação a fila acumulou 2,5 horas de
// atraso. Com
// ele, o que não cabe é DESCARTADO - vira só movimento na timeline -, e ninguém
// espera: Oferece nunca bloqueia, então o laço de gravação nunca sente a fila.
//
// Sidecar fora do ar também não chega à gravação: a olhada falha, a fila pausa
// e continua limitada, e os pedaços que não cabem são descartados.
type Fila struct {
	det     Detector
	entrega func(Olhada)
	log     *slog.Logger

	porCamera    int
	prazo, pausa time.Duration

	mu     sync.Mutex
	espera []naEspera
	// naFila é quantos pedaços de cada câmera esperam a vez. O que está sendo
	// olhado já saiu da fila: olhando é a câmera dele, "" com o detector
	// parado.
	naFila   map[string]int
	olhando  string
	foraDoAr bool

	// O que a tela de Diagnóstico mostra da fila: o maior tamanho que ela já
	// teve, e os tempos das últimas olhadas. Protegidos pelo mu.
	pico            int
	analise, demora media

	aviso chan struct{}
}

// naEspera é um pedaço na fila, com o instante em que entrou nela.
type naEspera struct {
	p     Pedaco
	desde time.Time
}

// EstadoDaFila é o retrato da fila para a tela de Diagnóstico.
type EstadoDaFila struct {
	Fila struct {
		// Agora são os pedaços esperando a vez, sem contar o que está sendo
		// olhado; Pico, o maior Agora desde que o dwnvr subiu. Cap é o teto,
		// PedacosPorCamera vezes as câmeras com detecção - quem sabe quantas
		// são é o Manager, e é ele que preenche.
		Agora int `json:"agora"`
		Cap   int `json:"cap"`
		Pico  int `json:"pico"`
		// PorCamera é o PedacosPorCamera: quantos pedaços uma câmera pode ter
		// esperando a vez. Com todos os lugares ocupados, a próxima marca dela
		// é descartada.
		PorCamera int `json:"porCamera"`
		// Cameras diz, das câmeras com algo na fila ou sendo olhado agora,
		// quantos pedaços esperam a vez e se um está sendo olhado. Câmera fora
		// daqui não tem nada nem numa coisa nem na outra.
		Cameras map[string]CameraNaFila `json:"cameras"`
	} `json:"fila"`
	Tempos struct {
		// AnaliseMs é quanto uma olhada leva no detector; EsperaMs, quanto o
		// pedaço esperou a vez antes dela. Médias das últimas OlhadasNaMedia
		// olhadas, zero antes da primeira. A análise conta só as olhadas que
		// o detector respondeu: uma que esgotou o prazo diz que ele caiu, não
		// quanto ele demora.
		AnaliseMs int64 `json:"analiseMs"`
		EsperaMs  int64 `json:"esperaMs"`
	} `json:"tempos"`
}

// CameraNaFila é o que uma câmera tem na fila agora. Esperando nunca passa de
// PedacosPorCamera; Olhando é à parte, porque o pedaço que o detector está
// processando já saiu da fila.
type CameraNaFila struct {
	Esperando int  `json:"esperando"`
	Olhando   bool `json:"olhando"`
}

// media é a média das últimas OlhadasNaMedia durações.
type media struct {
	ultimas [OlhadasNaMedia]time.Duration
	n, prox int
}

func (m *media) anota(d time.Duration) {
	m.ultimas[m.prox] = d
	m.prox = (m.prox + 1) % len(m.ultimas)
	m.n = min(m.n+1, len(m.ultimas))
}

func (m *media) ms() int64 {
	if m.n == 0 {
		return 0
	}
	var soma time.Duration
	for _, d := range m.ultimas[:m.n] {
		soma += d
	}
	return (soma / time.Duration(m.n)).Milliseconds()
}

// NovaFila devolve a fila do detector. `entrega` recebe cada olhada, na
// goroutine do trabalhador e uma de cada vez - é isso que permite a quem
// recebe usar o Marcador de cada câmera sem lock.
func NovaFila(det Detector, entrega func(Olhada), log *slog.Logger) *Fila {
	return novaFila(det, entrega, log, PedacosPorCamera,
		PrazoDaOlhadaMs*time.Millisecond, PausaDepoisDeFalhaMs*time.Millisecond)
}

func novaFila(det Detector, entrega func(Olhada), log *slog.Logger,
	porCamera int, prazo, pausa time.Duration) *Fila {
	return &Fila{
		det: det, entrega: entrega, log: log,
		porCamera: porCamera, prazo: prazo, pausa: pausa,
		naFila: map[string]int{}, aviso: make(chan struct{}, 1),
	}
}

// Oferece põe o pedaço na fila e diz se ele entrou: entra se a câmera dele
// tiver menos de PedacosPorCamera esperando, seja qual for o tamanho da fila.
// Não bloqueia nunca: é chamado de dentro do laço de gravação.
func (f *Fila) Oferece(p Pedaco) bool {
	f.mu.Lock()
	cabe := f.naFila[p.Camera] < f.porCamera
	if cabe {
		f.espera = append(f.espera, naEspera{p, time.Now()})
		f.naFila[p.Camera]++
		f.pico = max(f.pico, len(f.espera))
	}
	f.mu.Unlock()
	if cabe {
		select {
		case f.aviso <- struct{}{}:
		default:
		}
	}
	return cabe
}

// Tamanho é quantos pedaços esperam agora, sem contar o que está sendo olhado.
func (f *Fila) Tamanho() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.espera)
}

// Estado devolve o retrato da fila agora.
func (f *Fila) Estado() EstadoDaFila {
	f.mu.Lock()
	defer f.mu.Unlock()
	var e EstadoDaFila
	e.Fila.Agora, e.Fila.Pico, e.Fila.PorCamera = len(f.espera), f.pico, f.porCamera
	e.Fila.Cameras = map[string]CameraNaFila{}
	for _, w := range f.espera {
		l := e.Fila.Cameras[w.p.Camera]
		l.Esperando++
		e.Fila.Cameras[w.p.Camera] = l
	}
	if f.olhando != "" {
		l := e.Fila.Cameras[f.olhando]
		l.Olhando = true
		e.Fila.Cameras[f.olhando] = l
	}
	e.Tempos.AnaliseMs, e.Tempos.EsperaMs = f.analise.ms(), f.demora.ms()
	return e
}

// Roda atende a fila até o ctx acabar.
func (f *Fila) Roda(ctx context.Context) {
	for {
		p, ok := f.proximo(ctx)
		if !ok {
			return
		}
		octx, cancel := context.WithTimeout(ctx, f.prazo)
		inicio := time.Now()
		visao, err := f.det.Olha(octx, p)
		levou := time.Since(inicio)
		cancel()
		if ctx.Err() != nil {
			return // desligando: a falha é nossa, não do detector
		}
		if err == nil {
			f.mu.Lock()
			f.analise.anota(levou)
			f.mu.Unlock()
		}
		p.Fmp4 = nil
		f.entrega(Olhada{Pedaco: p, Achados: visao.Achados, Quadro: visao.Quadro, Erro: err})
		f.mu.Lock()
		f.olhando = ""
		f.mu.Unlock()
		// Pedaço recusado é detector no ar: nem pausa, nem alarme.
		caiu := err != nil && !errors.Is(err, ErrPedacoRecusado)
		f.anotaEstado(caiu, err)
		if caiu {
			select {
			case <-ctx.Done():
				return
			case <-time.After(f.pausa):
			}
		}
	}
}

// proximo tira o pedaço mais antigo da fila, esperando se ela estiver vazia.
func (f *Fila) proximo(ctx context.Context) (Pedaco, bool) {
	for {
		f.mu.Lock()
		if len(f.espera) > 0 {
			e := f.espera[0]
			f.espera[0] = naEspera{} // solta os bytes para o GC
			f.espera = f.espera[1:]
			// Sai da fila ao começar a ser olhado: a vaga da câmera volta
			// agora, e não quando a resposta sair.
			f.naFila[e.p.Camera]--
			f.olhando = e.p.Camera
			f.demora.anota(time.Since(e.desde))
			f.mu.Unlock()
			return e.p, true
		}
		f.mu.Unlock()
		select {
		case <-ctx.Done():
			return Pedaco{}, false
		case <-f.aviso:
		}
	}
}

// anotaEstado avisa no log UMA vez quando o detector sai do ar, e outra quando
// volta - como o aviso de câmera parada. Uma linha por olhada falha seriam
// centenas por hora dizendo a mesma coisa.
func (f *Fila) anotaEstado(fora bool, err error) {
	if fora == f.foraDoAr {
		return
	}
	f.foraDoAr = fora
	if fora {
		f.log.Error("detector de objetos fora do ar: as câmeras seguem gravando e marcando movimento",
			"detector", f.det.Nome(), "erro", err)
		return
	}
	f.log.Info("detector de objetos voltou", "detector", f.det.Nome())
}
