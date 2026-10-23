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

// TestAreaNaoReiniciaAGravacao: a área e o alerta são do filtro e do aviso, e
// desenhar uma área ou ligar o alerta não pode abrir buraco na gravação.
func TestAreaNaoReiniciaAGravacao(t *testing.T) {
	cam := config.Camera{ID: "cam_teste", Audio: config.AudioNone, SegmentSeconds: 30, StallSeconds: 5}
	comArea := cam
	comArea.Areas = []config.Area{{ID: "k3f9a2", Name: "Portão",
		Points: []area.Ponto{{0.1, 0.1}, {0.5, 0.1}, {0.5, 0.5}}}}
	comArea.Alert = &config.AlertaDaCamera{Enabled: true, Families: []detect.Familia{detect.Pessoa}}
	if recordingParamsChanged(cam, comArea) {
		t.Error("mudar as áreas ou o alerta reiniciaria a gravação")
	}
}

// comAlerta devolve um Manager com detector de objetos e uma câmera com o
// alerta de pessoa no Portão, a metade esquerda do quadro, e o recorder dela
// no lugar, sem gravar nada.
func comAlerta(t *testing.T) *Manager {
	t.Helper()
	st := store.New(t.TempDir())
	m := NewManager(&config.Config{}, nil, st, mudo())
	m.fila = detect.NovaFila(detect.Detectores["nenhum"](""), m.olhou, mudo())
	sim := true
	cam := config.Camera{ID: "cam_teste", Name: "Frente", Enabled: true, Detect: &sim,
		Areas: []config.Area{{ID: "esq", Name: "Portão",
			Points: []area.Ponto{{0, 0}, {0.5, 0}, {0.5, 1}, {0, 1}}}},
		Alert: &config.AlertaDaCamera{Enabled: true, Families: []detect.Familia{detect.Pessoa},
			Areas: []string{"esq"}}}
	// Sem Start, o Set não sobe recorder: ele entra à mão, como o Start o deixaria.
	m.Set(cam)
	m.recs[cam.ID] = &running{rec: newRecorder(m.cfg.Resolve(cam), nil, st.Camera(cam.ID), mudo())}
	return m
}

// TestOlhadaViraAlerta: a olhada passa pelo Recorder, que grava as marcas, e o
// alerta sai com os mesmos números delas. A pessoa fora da área e a moto, que
// não é da família, não entram; a olhada só com elas não alerta.
func TestOlhadaViraAlerta(t *testing.T) {
	m := comAlerta(t)
	agora := time.Now().UnixMilli()
	m.olhou(detect.Olhada{
		Pedaco: detect.Pedaco{Camera: "cam_teste", OnsetMs: agora, QuadroMs: agora + 1400},
		Achados: []detect.Achado{
			{Classe: "person", Score: 0.87654, Caixa: [4]float64{0.25, 0.2, 0.35, 0.6}},
			{Classe: "person", Score: 0.55, Caixa: [4]float64{0.75, 0.2, 0.85, 0.6}},
			{Classe: "motorcycle", Score: 0.71, Caixa: [4]float64{0.1, 0.4, 0.3, 0.7}},
		},
		Quadro: []byte("\xff\xd8jpeg-de-teste\xff\xd9"),
	})
	c := m.Alertas()
	if c == nil || c.Total() != 1 {
		t.Fatalf("central %v, esperava um alerta", c)
	}
	a := c.Recentes()[0]
	if a.Camera != "cam_teste" || a.InstanteMs != agora || a.QuadroMs != agora+1400 || !a.TemQuadro {
		t.Errorf("alerta %+v", a)
	}
	if len(a.Objetos) != 1 || a.Objetos[0].Score != 0.877 || len(a.Objetos[0].Areas) != 1 ||
		a.Objetos[0].Areas[0] != "Portão" {
		t.Errorf("objetos %+v, esperava só a pessoa do Portão, com o score gravado", a.Objetos)
	}

	// Lugar novo para o rastreio, mas fora da área: marca, e não alerta.
	m.olhou(detect.Olhada{
		Pedaco:  detect.Pedaco{Camera: "cam_teste", OnsetMs: agora + 60000, QuadroMs: agora + 61000},
		Achados: []detect.Achado{{Classe: "person", Score: 0.9, Caixa: [4]float64{0.6, 0.6, 0.7, 0.95}}},
	})
	if c.Total() != 1 {
		t.Errorf("%d alertas, esperava 1: a segunda pessoa estava fora da área", c.Total())
	}
}

// TestSemAlertaNadaNasce: sem câmera que alerte, a Central não existe. Não
// alerta a câmera sem alerta ligado, a sem marcação de movimento, nem a de
// uma instalação sem detector de objetos.
func TestSemAlertaNadaNasce(t *testing.T) {
	sim, nao := true, false
	alerta := &config.AlertaDaCamera{Enabled: true, Families: []detect.Familia{detect.Pessoa}}
	casos := map[string]struct {
		cam      config.Camera
		detector bool
	}{
		"sem alerta":       {config.Camera{ID: "a", Enabled: true, Detect: &sim}, true},
		"alerta desligado": {config.Camera{ID: "a", Enabled: true, Detect: &sim, Alert: &config.AlertaDaCamera{}}, true},
		"sem marcação":     {config.Camera{ID: "a", Enabled: true, Detect: &nao, Alert: alerta}, true},
		"sem detector":     {config.Camera{ID: "a", Enabled: true, Detect: &sim, Alert: alerta}, false},
	}
	for nome, c := range casos {
		t.Run(nome, func(t *testing.T) {
			m := NewManager(&config.Config{}, nil, store.New(t.TempDir()), mudo())
			if c.detector {
				m.fila = detect.NovaFila(detect.Detectores["nenhum"](""), m.olhou, mudo())
			}
			m.Set(c.cam)
			if m.Alertas() != nil || len(m.regras) > 0 {
				t.Errorf("nasceu: central %v, regras %v", m.Alertas(), m.regras)
			}
		})
	}
}

// TestDesligarOAlertaGuardaOHistorico: a câmera que desliga o alerta perde a
// regra, e a Central fica, com o que já registrou.
func TestDesligarOAlertaGuardaOHistorico(t *testing.T) {
	m := comAlerta(t)
	cam := m.Cameras()[0]
	cam.Alert = &config.AlertaDaCamera{Families: cam.Alert.Families, Areas: cam.Alert.Areas}
	m.Set(cam)
	if m.regras["cam_teste"] != nil || m.Alertas() == nil {
		t.Errorf("regra %v, central %v: esperava sem regra e com a central", m.regras["cam_teste"], m.Alertas())
	}
}
