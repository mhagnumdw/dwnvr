// Package recorder grava, por câmera, o fMP4 contínuo do go2rtc em segmentos
// alinhados a keyframe - sem decodificar nem reescrever mídia.
package recorder

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/fmp4"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

const (
	// writeBufSize agrupa as escritas. O destino típico é um disco USB num
	// servidor modesto, e mandar 15 fragmentos por segundo direto ao disco
	// geraria I/O miúdo demais para nada.
	writeBufSize = 256 << 10

	minBackoff = time.Second
	maxBackoff = 30 * time.Second

	// clockJumpThreshold é o salto de relógio a partir do qual avisamos.
	// Placas SBC baratas costumam não ter RTC: sem rede no boot elas começam
	// com uma data errada e o NTP corrige depois, o que embaralharia o índice
	// em silêncio.
	clockJumpThreshold = 5 * time.Second

	// Ver relogio e inicioEmendado em relogio.go.
	//
	// relogioBalde é quanto de mídia cada balde do estimador cobre. Precisa
	// conter com folga um quadro entregue sem rajada; 30s são 450 quadros a
	// 15fps. Os dois baldes somados ficam em 60s, e nesse tempo a deriva
	// medida (0,04%) não chega a 25ms.
	relogioBalde = 30 * time.Second
	// relogioTolerancia é quanto a timeline pode se afastar do relógio sem ser
	// corrigida. A hora impressa na imagem só mostra segundos, e abaixo disso
	// ninguém nota; acima, cada correção ainda é de meio quadro.
	relogioTolerancia = 250 * time.Millisecond
	// relogioBuraco é o atraso da timeline a partir do qual a mídia deixou de
	// contar um tempo que passou de fato, e o segmento pula para o relógio
	// deixando um buraco. O jitter da rede não chega aqui: o estimador já o
	// filtrou.
	relogioBuraco = time.Second

	// minBitrateForEstimate é a taxa abaixo da qual a estimativa de retenção
	// não é publicada. Nenhum stream de câmera real fica abaixo disso.
	minBitrateForEstimate = 1.0 // kbps

	// minSpanForEstimate é o histórico mínimo para estimar a retenção pela
	// densidade do que está gravado. Abaixo disso a medida é curta demais e a
	// estimativa cai na taxa instantânea.
	minSpanForEstimate = int64(time.Hour / time.Millisecond)
)

// retainDays estima quantos dias de gravação cabem na cota.
//
// É o número que torna a cota compreensível: "20 GB" não diz nada, "≈ 8,2 dias"
// diz tudo. E é lido ao lado do "retido", que é o passado que existe de fato -
// então os dois têm que fechar quando a cota enche, senão a tela se contradiz.
//
// Daí a preferência pela densidade média do que já está em disco (bytes por dia
// de histórico) em vez da taxa do instante: a taxa de uma câmera de rua cai à
// metade de madrugada e dobra de tarde, e dividir a cota por ela fazia a
// estimativa balançar entre 5 e 9,6 dias no mesmo dia - sempre brigando com um
// "retido" que não se move na mesma proporção.
//
// O viés que sobra é honesto e é o mesmo do "retido": câmera que ficou dias
// fora do ar tem esse tempo contado no span, o que dilui a densidade e infla a
// estimativa. Os dois números erram juntos, na mesma direção, o que é
// preferível a divergirem.
//
// Sem histórico que dê medida - câmera nova, que é justamente quando a
// estimativa mais serve para escolher a cota - cai na taxa instantânea. Sem
// nenhuma das duas, devolve zero e a tela mostra "-" em vez de mentir.
func retainDays(quotaMB, bytes, spanMs int64, bitrateKbps float64) float64 {
	quota := float64(quotaMB) * (1 << 20)

	if bytes > 0 && spanMs >= minSpanForEstimate {
		bytesPerDay := float64(bytes) * 86400000 / float64(spanMs)
		return quota / bytesPerDay
	}
	if bitrateKbps >= minBitrateForEstimate {
		bytesPerDay := bitrateKbps * 1000 / 8 * 86400
		return quota / bytesPerDay
	}
	return 0
}

// Status é a visão de saúde de uma câmera, consumida pela tela de diagnóstico.
type Status struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Enabled     bool      `json:"enabled"`
	Connected   bool      `json:"connected"`
	ConnectedAt time.Time `json:"connectedAt,omitzero"`
	BitrateKbps float64   `json:"bitrateKbps"`
	Bytes       int64     `json:"bytes"`
	Segments    int64     `json:"segments"`
	Reconnects  int64     `json:"reconnects"`
	LastError   string    `json:"lastError,omitempty"`
	VideoCodec  string    `json:"videoCodec,omitempty"`
	HasAudio    bool      `json:"hasAudio"`
	Gen         string    `json:"gen,omitempty"`

	// Width e Height são a resolução que está sendo gravada, lida do init da
	// própria conexão - não do que a câmera diz que faz. Ficam zeradas enquanto
	// a câmera nunca conectou.
	Width  uint16 `json:"width,omitempty"`
	Height uint16 `json:"height,omitempty"`

	// LastSegmentAt e Silent respondem à pergunta que mais importa num NVR:
	// "esta câmera está gravando AGORA?". Connected não responde - uma conexão
	// pode estar de pé sem produzir um segmento sequer.
	LastSegmentAt time.Time `json:"lastSegmentAt,omitzero"`
	Silent        bool      `json:"silent"`

	// OldestSegmentAt é o começo da gravação mais antiga que a câmera ainda
	// tem em disco - a retenção REAL, que é outra coisa do que RetainDays logo
	// abaixo: uma diz o passado que existe, a outra estima o que caberia.
	//
	// Vai como instante, e não como dias já calculados, para que a tela possa
	// escrever tanto "12 dias 4h" quanto "desde 31/07" a partir do mesmo campo.
	// A divergência de relógio entre o servidor e o navegador, que fez o uptime
	// ser enviado em segundos, não incomoda aqui: são segundos de erro contra
	// dias de medida.
	OldestSegmentAt time.Time `json:"oldestSegmentAt,omitzero"`

	// Detect e Onsets respondem "o gatilho de movimento está vivo nesta
	// câmera?". Sem eles a única forma de saber é abrir o arquivo de eventos
	// do dia no disco.
	Detect bool  `json:"detect"`
	Onsets int64 `json:"onsets"`

	// Funil é o destino dos onsets desta câmera desde que o dwnvr subiu. Só
	// existe com o detector de objetos configurado.
	Funil *Funil `json:"funil,omitempty"`

	// QuotaMB e Bytes em disco alimentam a estimativa de retenção mostrada na
	// tela de cadastro ("com esta cota, cabem ~N dias").
	QuotaMB    int64   `json:"quotaMB"`
	DiskBytes  int64   `json:"diskBytes"`
	RetainDays float64 `json:"retainDays"`
}

// Funil diz o que aconteceu com cada onset da câmera. As fatias fecham a conta
// com os Onsets do Status: onsets = semVideo + descartados + falhas + recusados +
// semObjeto + comObjeto + naFila.
type Funil struct {
	// Pedacos são os onsets cujo vídeo foi cortado e oferecido ao detector.
	// Não é fatia: é o total do que chegou à fila.
	Pedacos int64 `json:"pedacos"`
	// SemVideo são onsets que terminaram sem pedaço para mandar - logo depois
	// de conectar, antes do primeiro frame I. Ver Recortador.SemVideo.
	SemVideo int64 `json:"semVideo"`
	// Descartados a fila recusou: a câmera já tinha PedacosPorCamera esperando
	// a vez. Não são olhados nem depois: ficam só como movimento na timeline.
	Descartados int64 `json:"descartados"`
	// Falhas são olhadas que o detector não respondeu: fora do ar, ou travado.
	Falhas int64 `json:"falhas"`
	// Recusados o detector respondeu que não conseguiu olhar - vídeo que não
	// decodifica, que a própria câmera mandou corrompido.
	Recusados int64 `json:"recusados"`
	// SemObjeto: olhado, e nada que tenha CHEGADO agora - inclusive o carro
	// que já estava estacionado.
	SemObjeto int64 `json:"semObjeto"`
	// ComObjeto: olhado, e virou marca de objeto na timeline.
	ComObjeto int64 `json:"comObjeto"`
	// NaFila é o resto, o que ainda não tem desfecho: esperando o pico da
	// janela para ser cortado, esperando a vez na fila, ou sendo olhado agora.
	NaFila int64 `json:"naFila"`
}

// Recorder grava uma câmera.
type Recorder struct {
	cam    config.Camera
	client *go2rtc.Client
	idx    *store.Camera
	log    *slog.Logger

	bytes      atomic.Int64
	segments   atomic.Int64
	reconnects atomic.Int64
	onsets     atomic.Int64

	// mec é o gatilho de movimento desta câmera. Ele pertence à goroutine da
	// sessão e só ela o toca - por isso não tem lock nenhum, e por isso a troca
	// de configuração vem pelo detectPedido logo abaixo em vez de ser escrita
	// aqui direto.
	mec detect.Mecanismo

	// pedacos é para onde vai o pedaço de vídeo que o detector deve olhar. É a
	// fila do detector de objetos que o liga; nil é "ninguém olha", e aí o GOP
	// nem é guardado - quem não instalou o detector não paga a cópia.
	pedacos func(detect.Pedaco)

	// recorte guarda o GOP corrente e corta o pedaço no quadro que o mec mandou
	// olhar. Como o mec, pertence à goroutine da sessão.
	//
	// Ele só existe com a detecção ligada E um destino para o pedaço: nasce no
	// primeiro quadro em que as duas coisas valem, e morre quando a detecção é
	// desligada. Morrer é o que devolve os buffers do GOP, que podem ter
	// crescido até TetoDoGOPBytes cada.
	recorte *detect.Recortador

	// initAtual é o init da conexão corrente, para o recortador que nascer no
	// meio dela. Da goroutine da sessão.
	initAtual []byte

	// semVideoAntes soma o SemVideo dos recortadores que já morreram, para o
	// Funil não zerar a cada vez que a detecção é desligada e ligada de novo.
	// Da goroutine da sessão.
	semVideoAntes int64

	// marcador decide o que o detector achou que vira marca. Ele é da
	// goroutine da FILA, e não da sessão: é ela que entrega as olhadas, uma de
	// cada vez. Sobrevive a reconexão de propósito - o carro estacionado
	// continua lá quando o stream volta.
	marcador detect.Marcador

	// As fatias do Funil. semVideo é a cópia do contador do recorte, somado ao
	// semVideoAntes, que são da goroutine da sessão: é publicada aqui para o
	// Status ler sem corrida.
	cortados, descartados, falhas, recusados, semObjeto, comObjeto, semVideo atomic.Int64

	// detectPedido é a configuração de detecção que a API acabou de salvar. O
	// laço de gravação a aplica no próximo quadro, e é isso que permite mudar a
	// sensibilidade sem abrir um buraco na gravação: nem a API espera pelo
	// recorder, nem o recorder reconecta para obedecer.
	detectPedido atomic.Pointer[ajusteDetect]

	mu          sync.RWMutex
	connected   bool
	connectedAt time.Time
	lastErr     string
	videoCodec  string
	width       uint16
	height      uint16
	hasAudio    bool
	gen         string
	bitrateKbps float64

	// lastEnd é o fim (em relógio de parede) do último segmento fechado. É a
	// emenda de onde o próximo parte - ver segmenter.inicioDe.
	lastEnd int64

	startedAt    time.Time
	silentLogged bool

	sampleAt    time.Time
	sampleBytes int64
}

// ajusteDetect é a configuração de detecção de uma câmera, na forma em que ela
// atravessa da API para o laço de gravação.
type ajusteDetect struct {
	ligada        bool
	mecanismo     string
	sensibilidade int
}

func detectDe(cam config.Camera) ajusteDetect {
	return ajusteDetect{
		ligada:        cam.Detect != nil && *cam.Detect,
		mecanismo:     cam.DetectMecanismo,
		sensibilidade: cam.DetectSensibilidade,
	}
}

func newRecorder(cam config.Camera, client *go2rtc.Client, idx *store.Camera, log *slog.Logger) *Recorder {
	r := &Recorder{
		cam: cam, client: client, idx: idx, log: log.With("cam", cam.ID),
		startedAt: time.Now(),
	}
	r.pedeDetect(cam)
	return r
}

// pedeDetect avisa o laço de gravação que a detecção desta câmera mudou.
func (r *Recorder) pedeDetect(cam config.Camera) {
	a := detectDe(cam)
	r.detectPedido.Store(&a)
}

// aplicaDetectPendente troca o gatilho quando a configuração mudou. Roda no
// caminho quente, então é um Swap atômico e nada mais: sem pedido, sai em uma
// instrução.
func (r *Recorder) aplicaDetectPendente() {
	pedido := r.detectPedido.Swap(nil)
	if pedido == nil {
		return
	}
	// O recortador só é alimentado enquanto há gatilho. Na troca, o GOP que ele
	// tinha pode ter buraco - os quadros de quando a detecção estava desligada -,
	// e o pedaço que o mecanismo velho esperava não tem mais quem o feche.
	if r.recorte != nil {
		r.recorte.Esquece()
		r.publicaSemVideo()
	}
	if !pedido.ligada {
		r.mec = nil
		if r.recorte != nil {
			r.semVideoAntes += r.recorte.SemVideo()
			r.recorte = nil
		}
		return
	}
	r.mec = detect.Novo(pedido.mecanismo, pedido.sensibilidade)
	r.log.Info("gatilho de movimento ligado",
		"mecanismo", r.mec.Nome(), "sensibilidade", pedido.sensibilidade,
		"onsets_por_hora", detect.OnsetsPorHora[min(max(pedido.sensibilidade, detect.NivelMin), detect.NivelMax)])
}

// movimento é o gancho do gatilho no laço de gravação.
//
// Ele roda UMA VEZ POR QUADRO DE VÍDEO, para cada câmera: várias câmeras a 15
// quadros por segundo num Orange Pi Zero 3. Por isso é O(1) e não aloca - o
// pacote detect tem um teste que guarda essas duas propriedades - e por isso o
// sinal é `len(box)`, que o laço já tem na mão, e não um pixel decodificado.
//
// Só quadro de VÍDEO chega aqui. Fragmento de áudio chega igual ao de vídeo no
// stream, e os samples dele também vêm marcados como sync: sem o filtro de
// trilha, todo pacote de áudio viraria um keyframe gigante para a estatística.
//
// `moof` e `mdat` são as caixas do fragmento como vão para o disco. O mdat é o
// sinal; as duas juntas são o que o recortador guarda.
func (r *Recorder) movimento(instanteMs int64, keyframe bool, moof, mdat []byte) {
	r.aplicaDetectPendente()
	if r.mec == nil {
		return
	}
	v := r.mec.Quadro(instanteMs, len(mdat), keyframe)
	if r.recorte == nil && r.pedacos != nil {
		r.recorte = detect.NovoRecortador(r.cam.ID)
		r.recorte.Init(r.initAtual)
	}
	if r.recorte != nil {
		r.entrega(r.recorte.Quadro(v, instanteMs, keyframe, moof, mdat))
	}
	if !v.Onset {
		return
	}
	r.onsets.Add(1)
	if err := r.idx.AppendEvento(store.Evento{InstanteMs: instanteMs}); err != nil {
		r.log.Warn("falha ao gravar marca de movimento", "erro", err)
	}
}

// entrega manda para o detector o pedaço que o recortador acabou de cortar, e
// publica quantos onsets ficaram sem pedaço até aqui.
func (r *Recorder) entrega(p detect.Pedaco, cortou bool) {
	if cortou {
		r.pedacos(p)
	}
	r.publicaSemVideo()
}

func (r *Recorder) publicaSemVideo() {
	r.semVideo.Store(r.semVideoAntes + r.recorte.SemVideo())
}

// ofereceA liga o recorder à fila do detector. É o destino dos pedaços, e é
// chamado no laço de gravação: a fila não bloqueia nunca.
func (r *Recorder) ofereceA(f *detect.Fila) func(detect.Pedaco) {
	return func(p detect.Pedaco) {
		r.cortados.Add(1)
		if !f.Oferece(p) {
			r.descartados.Add(1)
		}
	}
}

// olhou recebe a resposta do detector a um pedaço desta câmera, na goroutine
// da fila, e grava o que virou marca.
func (r *Recorder) olhou(o detect.Olhada) {
	if errors.Is(o.Erro, detect.ErrPedacoRecusado) {
		r.recusados.Add(1)
		return
	}
	if o.Erro != nil {
		r.falhas.Add(1)
		return
	}
	if r.marcador == nil {
		r.marcador = detect.NovoMarcador(detect.MarcadorPadrao)
	}
	marcas := r.marcador.Olha(o.Achados)
	if len(marcas) == 0 {
		r.semObjeto.Add(1)
		return
	}
	r.comObjeto.Add(1)

	// Uma linha por família, com a classe de maior confiança: duas pessoas
	// chegando juntas são uma chegada de `pessoa` na timeline.
	melhor := map[detect.Familia]detect.Achado{}
	for _, a := range marcas {
		f := detect.FamiliasPadrao[a.Classe]
		if b, ok := melhor[f]; !ok || a.Score > b.Score {
			melhor[f] = a
		}
	}
	for _, f := range detect.Familias {
		a, ok := melhor[f]
		if !ok {
			continue
		}
		// A caixa com 4 casas: 0,0001 do quadro é menos de meio pixel numa
		// câmera de 2560 px, e a linha fica legível num `tail`.
		var caixa [4]float64
		for i, v := range a.Caixa {
			caixa[i] = math.Round(v*10000) / 10000
		}
		ev := store.Evento{InstanteMs: o.Pedaco.OnsetMs, Familia: string(f), Classe: a.Classe,
			Score: math.Round(a.Score*1000) / 1000, QuadroMs: o.Pedaco.QuadroMs, Caixa: &caixa}
		if err := r.idx.AppendEvento(ev); err != nil {
			r.log.Warn("falha ao gravar marca de objeto", "erro", err)
		}
	}
}

// funil monta as fatias. NaFila sai por diferença, porque é a única que não é
// um evento: é o que ainda não aconteceu.
func (r *Recorder) funil() *Funil {
	if r.pedacos == nil {
		return nil
	}
	f := &Funil{
		Pedacos: r.cortados.Load(), SemVideo: r.semVideo.Load(), Descartados: r.descartados.Load(),
		Falhas: r.falhas.Load(), Recusados: r.recusados.Load(), SemObjeto: r.semObjeto.Load(),
		ComObjeto: r.comObjeto.Load(),
	}
	f.NaFila = max(0, r.onsets.Load()-f.SemVideo-f.Descartados-f.Falhas-f.Recusados-f.SemObjeto-f.ComObjeto)
	return f
}

// silenceLimitLocked é quanto tempo sem fechar um segmento basta para dizer que
// a câmera parou. Três segmentos de folga absorvem o corte por keyframe, que
// nunca cai exatamente na duração alvo; o piso de um minuto evita alarme falso
// em quem configurou segmentos muito curtos.
func (r *Recorder) silenceLimitLocked() time.Duration {
	d := 3 * time.Duration(r.cam.SegmentSeconds) * time.Second
	return max(d, time.Minute)
}

// lastActivityLocked é o instante mais recente entre subir, conectar e fechar um
// segmento - a referência para saber há quanto tempo nada acontece.
func (r *Recorder) lastActivityLocked() time.Time {
	ref := r.startedAt
	if r.connectedAt.After(ref) {
		ref = r.connectedAt
	}
	if r.lastEnd > 0 {
		if t := time.UnixMilli(r.lastEnd); t.After(ref) {
			ref = t
		}
	}
	return ref
}

// checkSilence avisa UMA vez quando a câmera para de gravar, e outra quando
// volta.
//
// É a lacuna que o watchdog sozinho deixa: ele reconecta em silêncio, e em
// 09/08/2026 nove câmeras pararam às 08:18 sem que nada avisasse - o problema
// só foi descoberto porque alguém foi olhar. Num NVR, perceber que parou de
// gravar é a segunda função mais importante depois de gravar.
func (r *Recorder) checkSilence(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	silent := now.Sub(r.lastActivityLocked()) > r.silenceLimitLocked()
	if silent == r.silentLogged {
		return
	}
	r.silentLogged = silent

	if silent {
		r.log.Error("câmera parou de gravar",
			"parada_desde", r.lastActivityLocked().Format(time.TimeOnly),
			"conectada", r.connected, "ultimo_erro", r.lastErr)
		return
	}
	r.log.Info("câmera voltou a gravar")
}

func (r *Recorder) Status() Status {
	r.mu.RLock()
	defer r.mu.RUnlock()

	disk, oldest, newest := r.idx.Resumo()
	st := Status{
		ID: r.cam.ID, Name: r.cam.Name, Enabled: r.cam.Enabled,
		Connected: r.connected, ConnectedAt: r.connectedAt,
		BitrateKbps: r.bitrateKbps,
		Bytes:       r.bytes.Load(), Segments: r.segments.Load(),
		Reconnects: r.reconnects.Load(), LastError: r.lastErr,
		Detect: r.cam.Detect != nil && *r.cam.Detect, Onsets: r.onsets.Load(),
		Funil:      r.funil(),
		VideoCodec: r.videoCodec, HasAudio: r.hasAudio, Gen: r.gen,
		Width: r.width, Height: r.height,
		QuotaMB: r.cam.QuotaMB, DiskBytes: disk,
		Silent: time.Since(r.lastActivityLocked()) > r.silenceLimitLocked(),
	}
	if r.lastEnd > 0 {
		st.LastSegmentAt = time.UnixMilli(r.lastEnd)
	}
	var span int64
	if oldest > 0 {
		st.OldestSegmentAt = time.UnixMilli(oldest)
		// Só com as duas pontas o span é span. Sem o mais antigo, `newest - 0`
		// seria a idade do epoch, e a densidade sairia perto de zero.
		span = newest - oldest
	}
	st.RetainDays = retainDays(r.cam.QuotaMB, disk, span, r.bitrateKbps)
	return st
}

// sampleBitrate calcula a taxa observada desde a última amostra.
func (r *Recorder) sampleBitrate(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	total := r.bytes.Load()
	if !r.sampleAt.IsZero() {
		dt := now.Sub(r.sampleAt).Seconds()
		switch {
		case total == r.sampleBytes:
			// Nenhum byte na janela inteira: a câmera parou, e a taxa tem que
			// dizer isso de uma vez. A média exponencial sozinha só se
			// APROXIMA de zero - depois de 3h38 parada ela marcava 3,7e-126,
			// um número que passa por "maior que zero" e fazia a estimativa de
			// retenção virar 5,4e+128 dias na tela de diagnóstico.
			r.bitrateKbps = 0
		case dt > 0:
			inst := float64(total-r.sampleBytes) * 8 / dt / 1000
			// Média exponencial: suaviza o vaivém do VBR sem esconder uma
			// câmera que parou de mandar dados.
			if r.bitrateKbps == 0 {
				r.bitrateKbps = inst
			} else {
				r.bitrateKbps = 0.7*r.bitrateKbps + 0.3*inst
			}
		}
	}
	r.sampleAt, r.sampleBytes = now, total
}

func (r *Recorder) setConnected(v bool, err string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.connected = v
	if v {
		r.connectedAt = time.Now()
	}
	if err != "" {
		r.lastErr = err
	}
}

// run mantém a câmera gravando, reconectando com backoff exponencial. Uma
// câmera com problema não pode derrubar as outras nem entrar em laço apertado.
func (r *Recorder) run(ctx context.Context) {
	backoff := minBackoff
	for ctx.Err() == nil {
		err := r.session(ctx)
		if ctx.Err() != nil {
			return
		}

		r.setConnected(false, errText(err))
		r.reconnects.Add(1)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			r.log.Warn("conexão caiu", "erro", err, "reconectando_em", backoff)
		} else {
			r.log.Info("stream encerrado pelo go2rtc", "reconectando_em", backoff)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// session é uma conexão inteira ao go2rtc, do moov ao fim do stream.
func (r *Recorder) session(ctx context.Context) error {
	body, err := r.client.OpenStream(ctx, r.cam.ID, r.cam.Audio,
		time.Duration(r.cam.StallSeconds)*time.Second)
	if err != nil {
		return err
	}
	defer body.Close()

	seg := &segmenter{
		rec: r, segDur: time.Duration(r.cam.SegmentSeconds) * time.Second,
		relogio: relogio{balde: relogioBalde.Milliseconds()},
	}
	defer seg.close()

	rd := fmp4.NewReader(body)
	var pending []byte

	// O SPS in-band é procurado UMA vez por conexão, no fragmento do primeiro
	// keyframe - que é onde os parameter sets aparecem, sempre antes do IDR.
	// Varrer todo mdat custaria 15 varreduras por segundo por câmera para
	// reencontrar eternamente o mesmo dado.
	var wantInbandSPS bool

	// O fragmento corrente: o moof dele já foi lido, o mdat ainda não.
	// Fragmento de áudio chega exatamente igual ao de vídeo, e o que decide se
	// o mdat é imagem é o fragEhVideo, reescrito a cada moof, de vídeo ou de
	// áudio. Um "é keyframe" solto, sem ele, sobreviveria de um fragmento de
	// vídeo até o mdat de áudio seguinte, e o gatilho e a busca do SPS leriam
	// áudio achando que era imagem.
	var fragEhVideo, fragKeyframe bool
	var fragInstanteMs int64

	// A estatística do gatilho não atravessa reconexão: o que veio antes de um
	// buraco não descreve mais a câmera.
	if r.mec != nil {
		r.mec.Zera()
	}
	// O pedaço que esperava a janela quando a conexão caiu continua bom: o
	// vídeo dele já chegou inteiro.
	defer func() {
		if r.recorte != nil {
			r.entrega(r.recorte.Encerra())
		}
	}()

	for {
		typ, box, err := rd.NextBox()
		if err != nil {
			return err
		}

		switch typ {
		case "ftyp", "moov":
			seg.init = append(seg.init, box...)
			if typ != "moov" {
				continue
			}
			mv, err := fmp4.ParseMoov(box)
			if err != nil {
				return fmt.Errorf("moov ilegível: %w", err)
			}
			vt, ok := mv.VideoTrack()
			if !ok {
				return errors.New("stream sem trilha de vídeo")
			}
			seg.movie, seg.videoTrack = mv, vt

			// A geração é o hash do init. Se o SPS mudar depois de uma
			// reconexão, o hash muda e os segmentos novos passam a apontar
			// para outro init - sem quebrar a reprodução dos antigos.
			gen := fmp4.InitGen(seg.init)
			if err := r.idx.WriteInit(gen, seg.init); err != nil {
				return fmt.Errorf("gravando init: %w", err)
			}
			seg.gen = gen
			r.initAtual = seg.init
			if r.recorte != nil {
				r.recorte.Init(seg.init)
			}

			// A resolução do init é só a primeira aproximação: se a câmera
			// mandar parameter sets in-band, é o SPS deles que vale. Ver
			// wantInbandSPS logo abaixo.
			r.setResolution(vt.Width, vt.Height)
			wantInbandSPS = true

			r.mu.Lock()
			r.videoCodec, r.hasAudio, r.gen = vt.Codec, mv.HasAudio(), gen
			r.mu.Unlock()
			r.setConnected(true, "")
			r.log.Info("conectado", "codec", vt.Codec, "audio", mv.HasAudio(),
				"resolucao_no_init", fmt.Sprintf("%dx%d", vt.Width, vt.Height),
				"gen", gen, "init_bytes", len(seg.init))

		case "moof":
			if seg.movie == nil {
				return errors.New("moof antes do moov")
			}
			frag, err := fmp4.ParseMoof(box, seg.videoTrack.ID)
			if err != nil {
				return fmt.Errorf("moof ilegível: %w", err)
			}
			if fragEhVideo = frag.TrackID == seg.videoTrack.ID; fragEhVideo {
				// Antes de rotacionar: o keyframe que abre o segmento também
				// conta para a estimativa de onde ele começa.
				seg.relogio.observar(seg.midiaMs(frag.BaseDecodeTime), time.Now().UnixMilli())
				if err := seg.maybeRotate(frag); err != nil {
					return err
				}
				seg.lastEnd = frag.EndTime()
				fragKeyframe = frag.Keyframe
				// Depois do maybeRotate: se ele abriu um segmento novo, é o
				// início DELE que ancora este quadro.
				fragInstanteMs = seg.instanteDe(frag.BaseDecodeTime)
			}
			pending = append(pending[:0], box...)
			if seg.open() {
				if err := fmp4.RebaseMoof(pending, seg.bases); err != nil {
					return fmt.Errorf("rebase do moof: %w", err)
				}
			}

		case "mdat":
			if fragEhVideo {
				if wantInbandSPS && fragKeyframe {
					wantInbandSPS = false
					r.readInbandSPS(seg.videoTrack, box)
				}
				r.movimento(fragInstanteMs, fragKeyframe, pending, box)
			}
			fragEhVideo, fragKeyframe = false, false
			if len(pending) == 0 {
				continue // sobra de fragmento parcial após reconexão
			}
			if err := seg.write(pending); err != nil {
				return err
			}
			if err := seg.write(box); err != nil {
				return err
			}
			pending = pending[:0]
			seg.frags++
			if seg.frags == 1 {
				seg.firstFrag = seg.bytes - int64(len(seg.init))
			}
		}
	}
}

func (r *Recorder) setResolution(w, h uint16) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.width, r.height = w, h
}

// readInbandSPS corrige a resolução com o SPS que veio junto do keyframe.
//
// Vale mais que o SPS do init porque é o que o decodificador obedece. A câmera
// da cam_teta do primeiro deployment anunciava 2560x1440 no init e transmitia
// 1920x1080 - sem isto, a tela mostraria com confiança um número que nenhum
// frame gravado tem.
func (r *Recorder) readInbandSPS(vt fmp4.Track, mdat []byte) {
	sps, ok := fmp4.FindSPS(vt.Codec, fmp4.BoxPayload(mdat), vt.NALLengthSize)
	if !ok {
		return
	}
	w, h, ok := fmp4.SPSSize(vt.Codec, sps)
	if !ok || (w == vt.Width && h == vt.Height) {
		return
	}
	r.setResolution(w, h)
	r.log.Warn("o init anuncia uma resolução que o stream não usa",
		"init", fmt.Sprintf("%dx%d", vt.Width, vt.Height),
		"gravando", fmt.Sprintf("%dx%d", w, h))
}

// --- segmentação ------------------------------------------------------------

type segmenter struct {
	rec    *Recorder
	segDur time.Duration

	init       []byte
	gen        string
	movie      *fmp4.Movie
	videoTrack fmp4.Track

	f     *os.File
	w     *bufio.Writer
	entry store.Entry
	bases map[uint32]uint64

	baseDTS uint64
	// lastEnd é o FIM do último fragmento, não o início dele: é essa
	// diferença de um frame que faz a emenda de segmentos na exportação
	// resultar em DTS estritamente crescente.
	lastEnd   uint64
	bytes     int64
	frags     int
	firstFrag int64
	day       string

	relogio relogio
	// emendado diz que esta conexão já fechou um segmento: o próximo continua
	// a mesma mídia, e começa na emenda em vez de no relógio de parede.
	emendado bool
}

func (s *segmenter) open() bool { return s.f != nil }

// elapsed usa o relógio de mídia (tfdt), não o de parede: assim o corte fica
// estável mesmo quando a rede entrega frames em rajada.
func (s *segmenter) elapsed(dts uint64) time.Duration {
	if dts < s.baseDTS || s.videoTrack.Timescale == 0 {
		return 0
	}
	return time.Duration(float64(dts-s.baseDTS) / float64(s.videoTrack.Timescale) * float64(time.Second))
}

// midiaMs é o instante de mídia desde o começo da conexão, que é de onde o
// go2rtc conta o tfdt.
func (s *segmenter) midiaMs(dts uint64) int64 {
	if s.videoTrack.Timescale == 0 {
		return 0
	}
	return int64(float64(dts) / float64(s.videoTrack.Timescale) * 1000)
}

// instanteDe traduz o relógio de mídia de um fragmento para o relógio de
// parede, que é o eixo da timeline.
//
// É a MESMA conta que foi usada para produzir as séries em que o gatilho foi
// medido: o início do segmento, que é relógio de parede, mais
// o tempo de mídia já decorrido dentro dele. Usar o relógio de parede da
// chegada do pacote poria a marca alguns décimos fora do vídeo que ela aponta,
// porque a rede entrega em rajada - foi medido em ±1,1 s num segmento de 30 s.
//
// Sem segmento aberto sobra o relógio de parede. É a janela entre conectar e o
// primeiro keyframe, e nenhuma marca sai dela: o gatilho ainda está aquecendo.
func (s *segmenter) instanteDe(dts uint64) int64 {
	if !s.open() {
		return time.Now().UnixMilli()
	}
	return s.entry.StartMs + s.elapsed(dts).Milliseconds()
}

func (s *segmenter) maybeRotate(frag fmp4.Fragment) error {
	if !frag.Keyframe {
		// Um segmento que não começa em keyframe não abre sozinho, então
		// keyframe é condição necessária para qualquer corte.
		return nil
	}
	if !s.open() {
		return s.start(frag)
	}

	// Além da duração alvo, corta na virada do dia: assim nenhum segmento
	// atravessa a meia-noite e cada um pertence a um único índice diário.
	rotate := s.elapsed(frag.BaseDecodeTime) >= s.segDur ||
		time.Now().Format(store.DayLayout) != s.day
	if !rotate {
		return nil
	}
	if err := s.finish(); err != nil {
		return err
	}
	return s.start(frag)
}

func (s *segmenter) start(frag fmp4.Fragment) error {
	startMs := s.inicioDe(frag)

	s.day = time.UnixMilli(startMs).Format(store.DayLayout)
	if err := s.rec.idx.EnsureDirs(s.day); err != nil {
		return err
	}

	path := s.rec.idx.SegmentPath(startMs)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	s.f, s.w = f, bufio.NewWriterSize(f, writeBufSize)
	s.baseDTS, s.lastEnd = frag.BaseDecodeTime, frag.EndTime()
	s.bytes, s.frags, s.firstFrag = 0, 0, 0
	s.entry = store.Entry{StartMs: startMs, Gen: s.gen, InitSize: int64(len(s.init))}

	// A base de cada trilha é o mesmo instante do keyframe, convertido para a
	// timescale dela: zerar as trilhas isoladamente perderia o desalinhamento
	// real entre áudio e vídeo.
	s.bases = make(map[uint32]uint64, len(s.movie.Tracks))
	for _, t := range s.movie.Tracks {
		s.bases[t.ID] = fmp4.ScaleTime(frag.BaseDecodeTime, s.videoTrack.Timescale, t.Timescale)
	}

	// Cada segmento carrega o próprio init: abre no VLC, no ffprobe e num
	// <video> sem nenhum pré-processamento.
	return s.write(s.init)
}

// inicioDe decide o instante da timeline em que começa o segmento aberto por
// frag.
//
// O início vem do relógio de parede, mas a duração vem do relógio de mídia (os
// timestamps da câmera). Se cada segmento começasse na hora em que chegou, o
// jitter da rede faria o novo começar ANTES de o anterior terminar, e no MSE o
// trecho sobreposto é sobrescrito - perde-se gravação. Por isso, dentro de uma
// conexão, o segmento parte da emenda com o anterior, corrigida aos poucos em
// direção ao relógio (inicioEmendado).
//
// O primeiro segmento da conexão não tem emenda: a mídia recomeçou do zero, e o
// que houve entre as conexões foi tempo sem gravação. Ele segue o relógio,
// só nunca começando antes do fim do último segmento gravado - o que também
// cobre o salto de relógio para trás de servidores sem RTC.
func (s *segmenter) inicioDe(frag fmp4.Fragment) int64 {
	lastEnd := s.rec.lastEndMs()
	// Sem observação não há estimativa; não acontece, porque o fragmento que
	// abre o segmento é observado antes, mas a hora de chegada é o palpite
	// que o código antigo usava.
	alvo, ok := s.relogio.parede(s.midiaMs(frag.BaseDecodeTime))
	if !ok {
		alvo = time.Now().UnixMilli()
	}

	if lastEnd-alvo > clockJumpThreshold.Milliseconds() {
		s.rec.log.Warn("início do segmento muito antes do fim do anterior",
			"estimado", time.UnixMilli(alvo), "fim_anterior_ms", lastEnd,
			"diferenca_ms", lastEnd-alvo)
	}

	if !s.emendado {
		return max(alvo, lastEnd)
	}
	return inicioEmendado(lastEnd, alvo, s.meioQuadroMs(frag),
		relogioTolerancia.Milliseconds(), relogioBuraco.Milliseconds())
}

// meioQuadroMs é o tamanho de cada correção do início - ver inicioEmendado.
// Sai do próprio keyframe porque a taxa de quadros é da câmera, não uma
// constante: meio quadro a 15fps é 33ms, a 30fps é 16ms. O piso de 1ms é para
// uma câmera acima de 500fps, que não existe, não desligar a correção.
func (s *segmenter) meioQuadroMs(frag fmp4.Fragment) int64 {
	if frag.SampleCount == 0 || s.videoTrack.Timescale == 0 {
		return 1
	}
	quadro := float64(frag.Duration) / float64(frag.SampleCount) / float64(s.videoTrack.Timescale) * 1000
	return max(int64(quadro/2), 1)
}

func (s *segmenter) write(b []byte) error {
	if s.w == nil {
		return nil
	}
	n, err := s.w.Write(b)
	s.bytes += int64(n)
	s.rec.bytes.Add(int64(n))
	return err
}

// finish fecha o segmento e só então registra no índice. Essa ordem importa:
// uma queda entre as duas coisas deixa um arquivo órfão, que a reconciliação do
// boot reincorpora - enquanto a ordem inversa deixaria o índice apontando para
// um arquivo que nunca existiu.
func (s *segmenter) finish() error {
	if s.f == nil {
		return nil
	}
	err := s.w.Flush()
	if cerr := s.f.Close(); err == nil {
		err = cerr
	}
	s.f, s.w = nil, nil
	if err != nil {
		return err
	}

	s.entry.DurMs = s.elapsed(s.lastEnd).Milliseconds()
	s.entry.Size = s.bytes
	s.entry.FirstFrag = s.firstFrag

	if err := s.rec.idx.Append(s.entry); err != nil {
		return err
	}
	s.rec.setLastEndMs(s.entry.StartMs + s.entry.DurMs)
	s.emendado = true
	s.rec.segments.Add(1)
	s.rec.log.Debug("segmento fechado", "inicio", s.entry.StartMs,
		"dur_s", float64(s.entry.DurMs)/1000, "mb", float64(s.entry.Size)/(1<<20))
	return nil
}

func (s *segmenter) close() {
	if err := s.finish(); err != nil {
		s.rec.log.Error("falha ao fechar segmento", "erro", err)
	}
}

func (r *Recorder) lastEndMs() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastEnd
}

func (r *Recorder) setLastEndMs(ms int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastEnd = ms
}
