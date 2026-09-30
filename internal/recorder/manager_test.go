package recorder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// TestTetoDaFilaCresceComAsCameras: o teto que a tela de Diagnóstico mostra é
// o limite por câmera vezes as câmeras com detecção ligada - e a desligada não
// conta, porque não oferece nada.
func TestTetoDaFilaCresceComAsCameras(t *testing.T) {
	sim, nao := true, false
	m := &Manager{
		fila: detect.NovaFila(detect.Detectores["nenhum"](""), func(detect.Olhada) {}, mudo()),
		recs: map[string]*running{},
	}
	for id, d := range map[string]*bool{"a": &sim, "b": &sim, "c": &nao, "d": nil} {
		m.recs[id] = &running{rec: &Recorder{cam: config.Camera{ID: id, Detect: d}}}
	}
	if got, want := m.Detector().Fila.Cap, 2*detect.PedacosPorCamera; got != want {
		t.Errorf("teto %d, esperado %d: duas câmeras com detecção", got, want)
	}
}

// TestSetEmPlenoVooNaoCorre: trocar o que não exige reconectar (nome, cota,
// detecção) escreve a câmera nova no recorder que está no ar, e quem lê a
// câmera dele - a sessão ao conectar, o recortador ao nascer, o Status, o
// alarme de silêncio e o teto da fila - lê ao mesmo tempo, cada um na sua
// goroutine. Só pega a corrida com `go test -race`.
func TestSetEmPlenoVooNaoCorre(t *testing.T) {
	var corpo []byte
	corpo = append(corpo, caixa("ftyp", []byte("isom"))...)
	corpo = append(corpo, moovComAudio()...)
	for i := range 30 {
		flags := uint32(flagsQuadroP)
		if i%10 == 0 {
			flags = flagsQuadroI
		}
		corpo = append(corpo, moof(idVideo, uint64(i)*6000, flags, 6000)...)
		corpo = append(corpo, mdat(100+i%7)...)
	}
	// Um go2rtc que entrega poucos quadros e encerra: cada sessão termina logo,
	// e a próxima lê a câmera de novo ao conectar.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(corpo)
	}))
	defer srv.Close()

	st := store.New(t.TempDir())
	m := NewManager(&config.Config{}, go2rtc.New(config.Go2RTC{URL: srv.URL}), st, mudo())
	m.fila = detect.NovaFila(detect.Detectores["nenhum"](""), m.olhou, mudo())

	ligada, desligada := true, false
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30,
		StallSeconds: 5, QuotaMB: 1000, Detect: &ligada, DetectMecanismo: "periodico",
		DetectSensibilidade: 5}

	// O recorder entra à mão, sem o run: o teste é quem chama a sessão, uma
	// atrás da outra, sem o backoff de reconexão no meio.
	rec := newRecorder(cam, m.client, st.Camera(cam.ID), mudo())
	rec.pedacos = rec.ofereceA(m.fila)
	m.recs[cam.ID] = &running{rec: rec}
	m.cams[cam.ID] = cam

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pronto := make(chan struct{})

	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-pronto:
				return
			default:
			}
			_ = rec.session(ctx)
		}
	})
	wg.Go(func() {
		for {
			select {
			case <-pronto:
				return
			default:
			}
			m.Status()
			m.Detector()
			rec.checkSilence(time.Now())
		}
	})

	// Do lado da "API": nome, cota e detecção mudam; nada que reconecte. A
	// detecção alternando é o que faz o recortador nascer de novo.
	for i := range 200 {
		c := cam
		c.Name = "Teste " + string(rune('A'+i%26))
		c.QuotaMB = int64(1000 + i)
		if i%2 == 1 {
			c.Detect = &desligada
		}
		m.Set(c)
		time.Sleep(200 * time.Microsecond)
	}
	close(pronto)
	wg.Wait()

	if got := m.Status()[0].Name; got != "Teste "+string(rune('A'+199%26)) {
		t.Errorf("nome no Status: %q, esperava o do último Set", got)
	}
}
