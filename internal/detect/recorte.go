package detect

// Pedaco é o que vai ao detector: um trecho de vídeo que se decodifica
// sozinho, do frame I até o quadro a olhar, que é o ÚLTIMO dele.
type Pedaco struct {
	Camera string

	// OnsetMs é onde a marca fica na timeline.
	OnsetMs int64

	// QuadroMs é o instante do quadro a olhar, o último do pedaço. Pode ser o
	// próprio onset, ou o pico que veio até JanelaDoPicoMs depois dele.
	QuadroMs int64

	// Fmp4 é o init da conexão seguido dos fragmentos (moof+mdat) do frame I
	// até o quadro a olhar, inclusive. Qualquer decodificador o abre como um
	// arquivo .mp4.
	Fmp4 []byte
}

// Recortador guarda o GOP corrente de uma câmera, para que o pedaço mandado ao
// detector possa ser cortado no quadro que o Mecanismo escolheu - que pode vir
// até JanelaDoPicoMs depois do onset, e não só no onset.
//
// São DOIS buffers, e não um, porque o candidato pode ficar para trás: se um
// frame I chega com a janela aberta, o GOP que tem o candidato vai para a
// reserva, e o novo GOP começa no outro buffer. O candidato está sempre em um
// dos dois, com qualquer número de frames I dentro da janela.
//
// Os buffers são reaproveitados: depois que crescem até o maior GOP da câmera,
// guardar um quadro é só copiar a caixa, sem alocação. Só o corte aloca - uma
// vez por onset -, e é a cópia que segue para a fila.
//
// Não é seguro para uso concorrente: pertence à goroutine de gravação da
// câmera, como o Mecanismo.
type Recortador struct {
	camera string
	init   []byte

	corrente, reserva []byte

	// guardando diz se o quadro que chega entra no GOP corrente: só depois do
	// primeiro frame I da conexão, e só até TetoDoGOPBytes. Quadro sem o frame
	// I dele não se decodifica.
	guardando bool

	pendente      bool
	onsetMs       int64
	candidatoMs   int64
	candNaReserva bool
	// candidatoFim é até onde o pedaço vai, no buffer do candidato. Zero é
	// "nenhum candidato guardado", e o corte não sai.
	candidatoFim int

	// semVideo conta os onsets que terminaram sem pedaço. Ver SemVideo.
	semVideo int64
}

// SemVideo diz quantos onsets, desde que o recortador nasceu, terminaram sem
// pedaço para mandar ao detector: a janela fechou sem quadro guardado - logo
// depois de conectar, antes do primeiro frame I, ou com o GOP acima do teto -,
// ou o onset foi esquecido no meio da janela.
//
// É contado aqui, onde acontece, e não deduzido de "onsets menos pedaços": a
// diferença também inclui o onset que ainda espera o pico, e ele ia aparecer
// como perdido durante os segundos da janela.
func (r *Recortador) SemVideo() int64 { return r.semVideo }

// larga encerra o onset pendente sem pedaço.
func (r *Recortador) larga() {
	if r.pendente {
		r.semVideo++
	}
	r.pendente, r.candidatoFim = false, 0
}

// NovoRecortador devolve o recortador de uma câmera. Ele só passa a guardar
// alguma coisa depois do primeiro Init.
func NovoRecortador(camera string) *Recortador {
	return &Recortador{camera: camera}
}

// Init começa uma conexão nova. O init é o ftyp+moov dela, que abre todo
// pedaço; o GOP que havia não continua nele e é esquecido. Quem chama já deve
// ter recolhido o pedaço pendente com Encerra.
func (r *Recortador) Init(init []byte) {
	r.init = init
	r.Esquece()
}

// Esquece joga fora o GOP e o pedaço pendente, e volta a guardar no próximo
// frame I, na mesma conexão. É para quando o recortador deixou de ver quadros
// que existiram - a detecção ficou desligada um tempo - e o GOP que ele tem
// ficou com buraco: pedaço com quadro faltando decodifica como imagem borrada.
func (r *Recortador) Esquece() {
	r.corrente = r.corrente[:0]
	r.guardando = false
	r.larga()
}

// Encerra fecha a conexão e devolve o pedaço que estava esperando a janela, se
// já havia o que olhar. A janela não vai fechar sozinha - o Mecanismo leva
// Zera na reconexão -, e o vídeo que ela tinha continua bom.
func (r *Recortador) Encerra() (Pedaco, bool) {
	p, ok := r.corta()
	// Sem init, nenhum frame I volta a abrir o GOP: o que chegar até o próximo
	// Init não pertence a conexão nenhuma.
	r.init, r.guardando = nil, false
	return p, ok
}

// Quadro guarda um quadro de vídeo e atende o Veredito que o Mecanismo deu a
// ele, na ordem que o Veredito manda. Devolve o pedaço quando a janela de um
// onset fecha.
//
// `moof` e `mdat` são as duas caixas do fragmento, como vão para o disco.
func (r *Recortador) Quadro(v Veredito, instanteMs int64, keyframe bool, moof, mdat []byte) (p Pedaco, ok bool) {
	if v.Corta {
		p, ok = r.corta()
	}

	if keyframe {
		if r.pendente && r.candidatoFim > 0 && !r.candNaReserva {
			r.corrente, r.reserva = r.reserva, r.corrente
			r.candNaReserva = true
		}
		r.corrente = r.corrente[:0]
		r.guardando = r.init != nil
	}

	guardou := false
	if r.guardando {
		if len(r.corrente)+len(moof)+len(mdat) > TetoDoGOPBytes {
			// O resto deste GOP não cabe: não é guardado, e nenhum quadro dele
			// vira candidato. O que já estava marcado continua valendo.
			r.guardando = false
		} else {
			r.corrente = append(r.corrente, moof...)
			r.corrente = append(r.corrente, mdat...)
			guardou = true
		}
	}

	if v.Onset {
		// Um pendente que nunca recebeu Corta - o mecanismo foi trocado no meio
		// da janela - é substituído, e não cortado.
		r.larga()
		r.pendente, r.onsetMs = true, instanteMs
	}
	if v.Candidato && r.pendente && guardou {
		r.candidatoMs, r.candNaReserva, r.candidatoFim = instanteMs, false, len(r.corrente)
	}
	return p, ok
}

// corta monta o pedaço pendente: o init, e o buffer do candidato até ele.
func (r *Recortador) corta() (Pedaco, bool) {
	if !r.pendente {
		return Pedaco{}, false
	}
	if r.candidatoFim == 0 {
		r.larga()
		return Pedaco{}, false
	}
	r.pendente = false
	buf := r.corrente
	if r.candNaReserva {
		buf = r.reserva
	}
	out := make([]byte, 0, len(r.init)+r.candidatoFim)
	out = append(out, r.init...)
	out = append(out, buf[:r.candidatoFim]...)
	return Pedaco{Camera: r.camera, OnsetMs: r.onsetMs, QuadroMs: r.candidatoMs, Fmp4: out}, true
}
