package recorder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/area"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// TestTetoDaFilaCresceComAsCameras: o teto que a tela de Diagnóstico mostra é
// o limite por câmera vezes as câmeras com detecção ligada - e a desligada não
// conta, porque não oferece nada.
func TestQueueCapGrowsWithTheCameras(t *testing.T) {
	yes, no := true, false
	m := &Manager{
		queue: detect.NewQueue(detect.Detectors["nenhum"](""), func(detect.Inference) {}, discardLogger()),
		recs:  map[string]*running{},
	}
	for id, d := range map[string]*bool{"a": &yes, "b": &yes, "c": &no, "d": nil} {
		m.recs[id] = &running{rec: &Recorder{cam: config.Camera{ID: id, Detect: d}}}
	}
	if got, want := m.Detector().Queue.Cap, 2*detect.ClipsPerCamera; got != want {
		t.Errorf("teto %d, esperado %d: duas câmeras com detecção", got, want)
	}
}

// TestSetEmPlenoVooNaoCorre: trocar o que não exige reconectar (nome, cota,
// detecção) escreve a câmera nova no recorder que está no ar, e quem lê a
// câmera dele - a sessão ao conectar, o recortador ao nascer, o Status, o
// alarme de silêncio e o teto da fila - lê ao mesmo tempo, cada um na sua
// goroutine. Só pega a corrida com `go test -race`.
func TestSetMidFlightDoesNotRace(t *testing.T) {
	var body []byte
	body = append(body, box("ftyp", []byte("isom"))...)
	body = append(body, moovWithAudio()...)
	for i := range 30 {
		flags := uint32(flagsPFrame)
		if i%10 == 0 {
			flags = flagsIFrame
		}
		body = append(body, moof(videoID, uint64(i)*6000, flags, 6000)...)
		body = append(body, mdat(100+i%7)...)
	}
	// Um go2rtc que entrega poucos quadros e encerra: cada sessão termina logo,
	// e a próxima lê a câmera de novo ao conectar.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	st := store.New(t.TempDir())
	m := NewManager(&config.Config{}, go2rtc.New(config.Go2RTC{URL: srv.URL}), st, discardLogger())
	m.queue = detect.NewQueue(detect.Detectors["nenhum"](""), m.onInference, discardLogger())

	on, off := true, false
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30,
		StallSeconds: 5, QuotaMB: 1000, Detect: &on, DetectMethod: "periodico",
		DetectSensitivity: 5}

	// O recorder entra à mão, sem o run: o teste é quem chama a sessão, uma
	// atrás da outra, sem o backoff de reconexão no meio.
	rec := newRecorder(cam, m.client, st.Camera(cam.ID), discardLogger())
	rec.clips = rec.offerTo(m.queue)
	m.recs[cam.ID] = &running{rec: rec}
	m.cams[cam.ID] = cam

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ready := make(chan struct{})

	var wg sync.WaitGroup
	wg.Go(func() {
		for {
			select {
			case <-ready:
				return
			default:
			}
			_ = rec.session(ctx)
		}
	})
	wg.Go(func() {
		for {
			select {
			case <-ready:
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
			c.Detect = &off
		}
		m.Set(c)
		time.Sleep(200 * time.Microsecond)
	}
	close(ready)
	wg.Wait()

	if got := m.Status()[0].Name; got != "Teste "+string(rune('A'+199%26)) {
		t.Errorf("nome no Status: %q, esperava o do último Set", got)
	}
}

// TestAreaNaoReiniciaAGravacao: a área e o alerta são do filtro e do aviso, e
// desenhar uma área ou ligar o alerta não pode abrir buraco na gravação.
func TestAreaDoesNotRestartRecording(t *testing.T) {
	cam := config.Camera{ID: "cam_teste", Audio: config.AudioNone, SegmentSeconds: 30, StallSeconds: 5}
	withArea := cam
	withArea.Areas = []config.Area{{ID: "k3f9a2", Name: "Portão",
		Points: []area.Point{{0.1, 0.1}, {0.5, 0.1}, {0.5, 0.5}}}}
	withArea.Alert = &config.CameraAlert{Enabled: true, Families: []detect.Family{detect.Person}}
	if recordingParamsChanged(cam, withArea) {
		t.Error("mudar as áreas ou o alerta reiniciaria a gravação")
	}
}

// comAlerta devolve um Manager com detector de objetos e uma câmera com o
// alerta de pessoa no Portão, a metade esquerda do quadro, e o recorder dela
// no lugar, sem gravar nada.
func withAlert(t *testing.T) *Manager {
	t.Helper()
	st := store.New(t.TempDir())
	m := NewManager(&config.Config{}, nil, st, discardLogger())
	m.queue = detect.NewQueue(detect.Detectors["nenhum"](""), m.onInference, discardLogger())
	yes := true
	cam := config.Camera{ID: "cam_teste", Name: "Frente", Enabled: true, Detect: &yes,
		Areas: []config.Area{{ID: "esq", Name: "Portão",
			Points: []area.Point{{0, 0}, {0.5, 0}, {0.5, 1}, {0, 1}}}},
		Alert: &config.CameraAlert{Enabled: true, Families: []detect.Family{detect.Person},
			Areas: []string{"esq"}}}
	// Sem Start, o Set não sobe recorder: ele entra à mão, como o Start o deixaria.
	m.Set(cam)
	m.recs[cam.ID] = &running{rec: newRecorder(m.cfg.Resolve(cam), nil, st.Camera(cam.ID), discardLogger())}
	return m
}

// TestOlhadaViraAlerta: a olhada passa pelo Recorder, que marca na linha do
// objeto que passou na regra o `alert` e o id da área, e a Central conta o
// alerta. A pessoa fora da área e a moto, que não é da família, são gravadas
// sem marca; a olhada só com elas não alerta.
func TestInferenceBecomesAlert(t *testing.T) {
	m := withAlert(t)
	now := time.Now().UnixMilli()
	m.onInference(detect.Inference{
		Clip: detect.Clip{Camera: "cam_teste", OnsetMs: now, FrameMs: now + 1400},
		Detections: []detect.Detection{
			{Class: "person", Score: 0.87654, Box: [4]float64{0.25, 0.2, 0.35, 0.6}},
			{Class: "person", Score: 0.55, Box: [4]float64{0.75, 0.2, 0.85, 0.6}},
			{Class: "motorcycle", Score: 0.71, Box: [4]float64{0.1, 0.4, 0.3, 0.7}},
		},
		Frame: []byte("\xff\xd8jpeg-de-teste\xff\xd9"),
	})
	c := m.Alerts()
	if c == nil || c.Total() != 1 {
		t.Fatalf("central %v, esperava um alerta", c)
	}
	lines, err := m.store.Camera("cam_teste").DayObjects(time.UnixMilli(now).Format(store.DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("%d linhas, esperava as 3 marcas", len(lines))
	}
	for i, l := range lines {
		// Só a primeira, a pessoa de 0,877 no Portão, alertou.
		if l.Alert != (i == 0) || l.Alert != (len(l.AlertAreas) == 1 && l.AlertAreas[0] == "esq") {
			t.Errorf("linha %d (%s %.3f): alert %v em %v", i, l.Family, l.Score, l.Alert, l.AlertAreas)
		}
	}

	// Lugar novo para o rastreio, mas fora da área: marca, e não alerta.
	m.onInference(detect.Inference{
		Clip:       detect.Clip{Camera: "cam_teste", OnsetMs: now + 60000, FrameMs: now + 61000},
		Detections: []detect.Detection{{Class: "person", Score: 0.9, Box: [4]float64{0.6, 0.6, 0.7, 0.95}}},
	})
	if c.Total() != 1 {
		t.Errorf("%d alertas, esperava 1: a segunda pessoa estava fora da área", c.Total())
	}
}

// TestSemAlertaNadaNasce: sem câmera que alerte, a Central não existe. Não
// alerta a câmera sem alerta ligado, a sem marcação de movimento, nem a de
// uma instalação sem detector de objetos.
func TestNoAlertNothingIsCreated(t *testing.T) {
	yes, no := true, false
	alertItem := &config.CameraAlert{Enabled: true, Families: []detect.Family{detect.Person}}
	cases := map[string]struct {
		cam      config.Camera
		detector bool
	}{
		"sem alerta":       {config.Camera{ID: "a", Enabled: true, Detect: &yes}, true},
		"alerta desligado": {config.Camera{ID: "a", Enabled: true, Detect: &yes, Alert: &config.CameraAlert{}}, true},
		"sem marcação":     {config.Camera{ID: "a", Enabled: true, Detect: &no, Alert: alertItem}, true},
		"sem detector":     {config.Camera{ID: "a", Enabled: true, Detect: &yes, Alert: alertItem}, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			m := NewManager(&config.Config{}, nil, store.New(t.TempDir()), discardLogger())
			if c.detector {
				m.queue = detect.NewQueue(detect.Detectors["nenhum"](""), m.onInference, discardLogger())
			}
			m.Set(c.cam)
			if m.Alerts() != nil || len(m.rules) > 0 {
				t.Errorf("nasceu: central %v, regras %v", m.Alerts(), m.rules)
			}
		})
	}
}

// TestDesligarOAlertaGuardaOHistorico: a câmera que desliga o alerta perde a
// regra, e a Central fica, a mesma, com o que já registrou.
func TestTurningOffTheAlertKeepsTheHistory(t *testing.T) {
	m := withAlert(t)
	now := time.Now().UnixMilli()
	m.onInference(detect.Inference{
		Clip:       detect.Clip{Camera: "cam_teste", OnsetMs: now, FrameMs: now + 1400},
		Detections: []detect.Detection{{Class: "person", Score: 0.87654, Box: [4]float64{0.25, 0.2, 0.35, 0.6}}},
		Frame:      []byte("\xff\xd8jpeg-de-teste\xff\xd9"),
	})
	before := m.Alerts()
	if before == nil || before.Total() != 1 {
		t.Fatalf("central %v, esperava um alerta antes de desligar", before)
	}

	cam := m.Cameras()[0]
	cam.Alert = &config.CameraAlert{Families: cam.Alert.Families, Areas: cam.Alert.Areas}
	m.Set(cam)
	if m.rules["cam_teste"] != nil {
		t.Errorf("regra %v: esperava sem regra", m.rules["cam_teste"])
	}
	if after := m.Alerts(); after != before || before.Total() != 1 {
		t.Errorf("central %p com %d, esperava a mesma %p com 1", after, before.Total(), before)
	}
}
