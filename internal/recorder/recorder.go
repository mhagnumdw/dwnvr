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
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
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

	// QuotaMB e Bytes em disco alimentam a estimativa de retenção mostrada na
	// tela de cadastro ("com esta cota, cabem ~N dias").
	QuotaMB    int64   `json:"quotaMB"`
	DiskBytes  int64   `json:"diskBytes"`
	RetainDays float64 `json:"retainDays"`
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

	// lastEnd é o fim (em relógio de parede) do último segmento fechado. É o
	// piso do início do próximo, o que impede sobreposição entre segmentos.
	lastEnd int64

	startedAt    time.Time
	silentLogged bool

	sampleAt    time.Time
	sampleBytes int64
}

func newRecorder(cam config.Camera, client *go2rtc.Client, idx *store.Camera, log *slog.Logger) *Recorder {
	return &Recorder{
		cam: cam, client: client, idx: idx, log: log.With("cam", cam.ID),
		startedAt: time.Now(),
	}
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

	seg := &segmenter{rec: r, segDur: time.Duration(r.cam.SegmentSeconds) * time.Second}
	defer seg.close()

	rd := fmp4.NewReader(body)
	var pending []byte

	// O SPS in-band é procurado UMA vez por conexão, no fragmento do primeiro
	// keyframe - que é onde os parameter sets aparecem, sempre antes do IDR.
	// Varrer todo mdat custaria 15 varreduras por segundo por câmera para
	// reencontrar eternamente o mesmo dado.
	var wantInbandSPS, keyframePending bool

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
			if frag.TrackID == seg.videoTrack.ID {
				if err := seg.maybeRotate(frag); err != nil {
					return err
				}
				seg.lastEnd = frag.EndTime()
				keyframePending = frag.Keyframe
			}
			pending = append(pending[:0], box...)
			if seg.open() {
				if err := fmp4.RebaseMoof(pending, seg.bases); err != nil {
					return fmt.Errorf("rebase do moof: %w", err)
				}
			}

		case "mdat":
			if wantInbandSPS && keyframePending {
				wantInbandSPS = false
				r.readInbandSPS(seg.videoTrack, box)
			}
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
// da cozinha do primeiro deployment anunciava 2560x1440 no init e transmitia
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
	now := time.Now()
	startMs := now.UnixMilli()

	// O início vem do relógio de parede, mas a duração vem do relógio de mídia
	// (os timestamps da câmera). Os dois derivam entre si com o jitter da rede
	// - medido em ±1,1s num segmento de 30s -, e quando a mídia adianta o
	// segmento novo começaria ANTES de o anterior terminar. No MSE isso faz o
	// trecho sobreposto ser sobrescrito, ou seja, perde-se gravação.
	//
	// Ancorar no fim do segmento anterior elimina a sobreposição sem abandonar
	// o relógio de parede como referência: a correção só age quando há
	// sobreposição de fato, e o segmento seguinte volta a seguir o relógio
	// assim que ele alcança. Isso também cobre o salto de relógio para trás de
	// servidores sem RTC.
	if lastEnd := s.rec.lastEndMs(); startMs < lastEnd {
		if lastEnd-startMs > int64(clockJumpThreshold/time.Millisecond) {
			s.rec.log.Warn("início do segmento muito antes do fim do anterior",
				"agora", now, "fim_anterior_ms", lastEnd, "diferenca_ms", lastEnd-startMs)
		}
		startMs = lastEnd
	}

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
