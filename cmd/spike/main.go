// Command spike valida as premissas centrais do dwnvr: dá para gravar o fMP4
// que o go2rtc já produz, cortando em keyframes, sem ffmpeg e sem tocar nos
// bytes de mídia — e isso cabe no orçamento de CPU/RAM de um Orange Pi Zero 3.
//
// Uso:
//
//	spike -go2rtc http://servidor.local:1984 -cams cam_jardim,cam_porta -out ./rec -seg 60 -dur 300
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/fmp4"
)

// writeBufSize é o tamanho do buffer de escrita por câmera. Escrever em blocos
// grandes importa mais que o normal aqui: o destino é um disco USB num Pi, e
// gravar 15 fragmentos por segundo direto no disco geraria I/O miúdo demais.
const writeBufSize = 256 << 10

func main() {
	var (
		base    = flag.String("go2rtc", "http://servidor.local:1984", "URL base do go2rtc")
		cams    = flag.String("cams", "cam_jardim", "streams do go2rtc, separados por vírgula")
		out     = flag.String("out", "./rec", "diretório de saída")
		segSecs = flag.Int("seg", 60, "duração alvo do segmento em segundos")
		durSecs = flag.Int("dur", 300, "tempo total de gravação em segundos (0 = infinito)")
		audio   = flag.String("audio", "none", "none | flac | aac")
	)
	flag.Parse()

	list := strings.Split(*cams, ",")
	for i := range list {
		list[i] = strings.TrimSpace(list[i])
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *durSecs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(*durSecs)*time.Second)
		defer cancel()
	}

	st := &stats{}
	start := time.Now()

	var wg sync.WaitGroup
	for _, cam := range list {
		if cam == "" {
			continue
		}
		wg.Add(1)
		go func(cam string) {
			defer wg.Done()
			dir := filepath.Join(*out, cam)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				log.Printf("[%s] mkdir: %v", cam, err)
				return
			}
			recordLoop(ctx, *base, cam, dir, time.Duration(*segSecs)*time.Second, *audio, st)
		}(cam)
	}

	go reportLoop(ctx, st, start, len(list))
	wg.Wait()

	elapsed := time.Since(start)
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	log.Printf("=== fim ===")
	log.Printf("%d câmeras, %d segmentos, %.1f MB em %s",
		len(list), st.segments.Load(), float64(st.bytes.Load())/(1<<20), elapsed.Round(time.Second))
	log.Printf("taxa somada: %.0f kbps  →  %.1f GB/dia",
		float64(st.bytes.Load())*8/elapsed.Seconds()/1000,
		float64(st.bytes.Load())/elapsed.Seconds()*86400/(1<<30))
	log.Printf("Go heap em uso: %.1f MB | total alocado: %.1f MB | GCs: %d",
		float64(ms.HeapAlloc)/(1<<20), float64(ms.TotalAlloc)/(1<<20), ms.NumGC)
}

type stats struct {
	bytes     atomic.Int64
	segments  atomic.Int64
	frags     atomic.Int64
	reconnect atomic.Int64
}

func reportLoop(ctx context.Context, st *stats, start time.Time, ncams int) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			el := time.Since(start).Seconds()
			log.Printf("[stats] %s | %.1f MB gravados | %.0f kbps | heap %.1f MB | goroutines %d | reconexões %d",
				time.Since(start).Round(time.Second),
				float64(st.bytes.Load())/(1<<20),
				float64(st.bytes.Load())*8/el/1000,
				float64(ms.HeapAlloc)/(1<<20),
				runtime.NumGoroutine(),
				st.reconnect.Load())
		}
	}
}

func streamURL(base, cam, audio string) string {
	switch audio {
	case "flac":
		return fmt.Sprintf("%s/api/stream.mp4?src=%s&mp4=flac", base, cam)
	case "aac":
		return fmt.Sprintf("%s/api/stream.mp4?src=%s&video=h264,h265&audio=aac", base, cam)
	default:
		return fmt.Sprintf("%s/api/stream.mp4?src=%s&video=h264,h265", base, cam)
	}
}

// recordLoop mantém a câmera gravando, reconectando com backoff. Uma câmera que
// cai não pode derrubar as outras nem entrar em laço apertado de reconexão.
func recordLoop(ctx context.Context, base, cam, dir string, segDur time.Duration, audio string, st *stats) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := recordOnce(ctx, base, cam, dir, segDur, audio, st)
		if ctx.Err() != nil {
			return
		}
		if err != nil && !errors.Is(err, io.EOF) {
			log.Printf("[%s] conexão caiu: %v", cam, err)
		} else {
			log.Printf("[%s] stream encerrado pelo go2rtc", cam)
		}
		st.reconnect.Add(1)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func recordOnce(ctx context.Context, base, cam, dir string, segDur time.Duration, audio string, st *stats) error {
	url := streamURL(base, cam, audio)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("go2rtc devolveu HTTP %d", resp.StatusCode)
	}

	s := &segmenter{dir: dir, cam: cam, segDur: segDur, st: st}
	defer s.close()

	r := fmp4.NewReader(resp.Body)
	var pendingMoof []byte

	for {
		typ, box, err := r.NextBox()
		if err != nil {
			return err
		}

		switch typ {
		case "ftyp", "moov":
			s.init = append(s.init, box...)
			if typ == "moov" {
				mv, err := fmp4.ParseMoov(box)
				if err != nil {
					return fmt.Errorf("moov ilegível: %w", err)
				}
				vt, ok := mv.VideoTrack()
				if !ok {
					return errors.New("stream sem trilha de vídeo")
				}
				s.movie, s.videoTrack = mv, vt
				var desc []string
				for _, t := range mv.Tracks {
					desc = append(desc, fmt.Sprintf("%s/%s@%d", t.Handler, t.Codec, t.Timescale))
				}
				log.Printf("[%s] conectado: %s (init %d bytes)", cam, strings.Join(desc, " "), len(s.init))
			}

		case "moof":
			if s.movie == nil {
				return errors.New("moof antes do moov")
			}
			frag, err := fmp4.ParseMoof(box, s.videoTrack.ID)
			if err != nil {
				return fmt.Errorf("moof ilegível: %w", err)
			}
			if frag.TrackID == s.videoTrack.ID {
				if err := s.maybeRotate(frag); err != nil {
					return err
				}
				s.lastVideoTime = frag.BaseDecodeTime
				s.frags++
				st.frags.Add(1)
			}
			pendingMoof = append(pendingMoof[:0], box...)
			if s.f != nil {
				if err := fmp4.RebaseMoof(pendingMoof, s.bases); err != nil {
					return fmt.Errorf("rebase do moof: %w", err)
				}
			}

		case "mdat":
			if len(pendingMoof) == 0 {
				continue // fragmento parcial após reconexão
			}
			if err := s.write(pendingMoof); err != nil {
				return err
			}
			if err := s.write(box); err != nil {
				return err
			}
			pendingMoof = pendingMoof[:0]
		}
	}
}

type segmenter struct {
	dir    string
	cam    string
	segDur time.Duration
	st     *stats

	init       []byte
	movie      *fmp4.Movie
	videoTrack fmp4.Track

	f     *os.File
	w     *bufWriter
	path  string
	bases map[uint32]uint64

	segBase       uint64
	segStartWC    time.Time
	segBytes      int64
	lastVideoTime uint64
	frags         int
}

func (s *segmenter) mediaElapsed(now uint64) time.Duration {
	if now < s.segBase || s.videoTrack.Timescale == 0 {
		return 0
	}
	return time.Duration(float64(now-s.segBase) / float64(s.videoTrack.Timescale) * float64(time.Second))
}

func (s *segmenter) maybeRotate(frag fmp4.Fragment) error {
	if s.f == nil {
		// Nunca abrir um segmento fora de keyframe: ele não tocaria sozinho.
		if !frag.Keyframe {
			return nil
		}
		return s.open(frag)
	}
	if frag.Keyframe && s.mediaElapsed(frag.BaseDecodeTime) >= s.segDur {
		if err := s.rotate(); err != nil {
			return err
		}
		return s.open(frag)
	}
	return nil
}

func (s *segmenter) open(frag fmp4.Fragment) error {
	now := time.Now()
	s.path = filepath.Join(s.dir, fmt.Sprintf("%d.mp4", now.UnixMilli()))

	f, err := os.Create(s.path)
	if err != nil {
		return err
	}
	s.f, s.w = f, newBufWriter(f, writeBufSize)
	s.segBase, s.segStartWC, s.segBytes = frag.BaseDecodeTime, now, 0

	s.bases = make(map[uint32]uint64, len(s.movie.Tracks))
	for _, t := range s.movie.Tracks {
		s.bases[t.ID] = fmp4.ScaleTime(frag.BaseDecodeTime, s.videoTrack.Timescale, t.Timescale)
	}
	return s.write(s.init)
}

func (s *segmenter) write(b []byte) error {
	if s.w == nil {
		return nil
	}
	n, err := s.w.Write(b)
	s.segBytes += int64(n)
	s.st.bytes.Add(int64(n))
	return err
}

func (s *segmenter) rotate() error {
	if s.f == nil {
		return nil
	}
	dur := s.mediaElapsed(s.lastVideoTime)
	if err := s.w.Flush(); err != nil {
		return err
	}
	if err := s.f.Close(); err != nil {
		return err
	}
	s.st.segments.Add(1)
	log.Printf("[%s] %s %6.2fs %7.2f MB", s.cam, filepath.Base(s.path), dur.Seconds(), float64(s.segBytes)/(1<<20))
	s.f, s.w = nil, nil
	return nil
}

func (s *segmenter) close() { _ = s.rotate() }

// bufWriter é um bufio.Writer que reporta bytes escritos de forma confiável
// mesmo quando o buffer absorve a escrita.
type bufWriter struct {
	f   *os.File
	buf []byte
}

func newBufWriter(f *os.File, size int) *bufWriter {
	return &bufWriter{f: f, buf: make([]byte, 0, size)}
}

func (w *bufWriter) Write(b []byte) (int, error) {
	if len(w.buf)+len(b) > cap(w.buf) {
		if err := w.Flush(); err != nil {
			return 0, err
		}
	}
	if len(b) > cap(w.buf) {
		return w.f.Write(b)
	}
	w.buf = append(w.buf, b...)
	return len(b), nil
}

func (w *bufWriter) Flush() error {
	if len(w.buf) == 0 {
		return nil
	}
	_, err := w.f.Write(w.buf)
	w.buf = w.buf[:0]
	return err
}
