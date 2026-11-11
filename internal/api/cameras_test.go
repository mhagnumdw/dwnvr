package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/area"
	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/recorder"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// comGo2RTC troca o Manager do testServer por um que grava de verdade contra
// um go2rtc falso, e devolve quantas conexões de stream estão abertas agora -
// cada uma é um recorder no ar.
func withGo2RTC(t *testing.T, s *Server) *atomic.Int32 {
	t.Helper()
	var open atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/streams":
			_, _ = w.Write([]byte(`{"cam_teste": {}, "cam_a": {}, "cam_b": {}}`))
		case "/api/stream.mp4":
			open.Add(1)
			defer open.Add(-1)
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	s.client = go2rtc.New(config.Go2RTC{URL: srv.URL})
	s.mgr = recorder.NewManager(s.cfg, s.client, s.store, s.log)
	ctx, cancel := context.WithCancel(context.Background())
	s.mgr.Start(ctx, []config.Camera{{ID: "cam_teste", Name: "Teste", Enabled: true}})
	// Roda antes do srv.Close: o Stop espera cada recorder largar a conexão.
	t.Cleanup(func() { cancel(); s.mgr.Stop() })
	return &open
}

// saveCamera faz o POST /api/cameras. `cam` é uma config.Camera, ou o JSON da
// câmera como a tela o manda.
func saveCamera(t *testing.T, s *Server, cam any) {
	t.Helper()
	b, _ := json.Marshal(cam)
	rec := httptest.NewRecorder()
	s.handleSaveCamera(rec, httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewReader(b)))
	if rec.Code != http.StatusOK {
		t.Errorf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
}

// esperaAbertas espera o go2rtc falso ficar com n conexões por um instante
// seguido. Parar um recorder solta a conexão de forma assíncrona, e subir outro
// a abre também assim: uma leitura só pegaria o meio da troca.
func waitOpen(t *testing.T, open *atomic.Int32, n int32) {
	t.Helper()
	end := time.Now().Add(3 * time.Second)
	since := time.Time{}
	for time.Now().Before(end) {
		if open.Load() != n {
			since = time.Time{}
		} else if since.IsZero() {
			since = time.Now()
		} else if time.Since(since) > 300*time.Millisecond {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%d conexões abertas no go2rtc, esperava %d", open.Load(), n)
}

// Dois saves da mesma câmera ao mesmo tempo, os dois trocando o áudio - o que
// exige reconectar. Sem o cadastro, cada um parava o recorder antigo e subia
// um novo, e o primeiro novo ficava gravando fora do mapa até o restart: duas
// conexões com a câmera e só uma na tela.
func TestConcurrentSavesLeaveNoOrphanRecorder(t *testing.T) {
	s, _ := testServer(t)
	open := withGo2RTC(t, s)
	waitOpen(t, open, 1)

	audios := []string{config.AudioNone, config.AudioAAC, config.AudioFLAC}
	for i := range 10 {
		// Os dois diferem do áudio atual e entre si: os dois reconectam, seja
		// qual for a ordem em que entram.
		var wg sync.WaitGroup
		for _, a := range []string{audios[(i+1)%3], audios[(i+2)%3]} {
			wg.Go(func() {
				saveCamera(t, s, config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, Audio: a})
			})
		}
		wg.Wait()
		waitOpen(t, open, 1)
	}
}

// Dois saves de câmeras diferentes ao mesmo tempo liam a mesma lista, e quem
// gravava o cameras.json por último apagava a alteração do outro.
func TestConcurrentSavesLoseNoChangeInTheFile(t *testing.T) {
	s, _ := testServer(t)
	withGo2RTC(t, s)

	for i := range 50 {
		var wg sync.WaitGroup
		for _, id := range []string{"cam_a", "cam_b"} {
			wg.Go(func() {
				saveCamera(t, s, config.Camera{ID: id, Name: fmt.Sprint(id, " ", i)})
			})
		}
		wg.Wait()

		cams, _, err := s.cfg.LoadCameras()
		if err != nil {
			t.Fatal(err)
		}
		nomes := map[string]string{}
		for _, c := range cams {
			nomes[c.ID] = c.Name
		}
		for _, id := range []string{"cam_a", "cam_b"} {
			if want := fmt.Sprint(id, " ", i); nomes[id] != want {
				t.Fatalf("rodada %d: %s no arquivo com nome %q, esperava %q", i, id, nomes[id], want)
			}
		}
	}
}

// Câmera removida sem apagar as gravações, dwnvr reiniciado e a câmera
// cadastrada de novo com o mesmo ID. O índice só era lido no boot, e só das
// cadastradas: o recadastro começava vazio, e as gravações antigas sumiam de
// Gravações, das órfãs e da cota até o reinício seguinte.
func TestReRegistrationReadsRecordingsAlreadyOnDisk(t *testing.T) {
	s, _ := testServer(t)
	withGo2RTC(t, s)

	// O que ficou da vida anterior: outro Store sobre o mesmo storage, como o
	// processo que rodava antes do reinício.
	antes := store.New(s.store.Root()).Camera("cam_a")
	start := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	seed(t, antes, start, [2]int64{0, 30000})
	if err := antes.EnsureDirs(start.Format(store.DayLayout)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(antes.SegmentPath(start.UnixMilli()), make([]byte, 1000), 0o644); err != nil {
		t.Fatal(err)
	}

	saveCamera(t, s, config.Camera{ID: "cam_a", Name: "A", Enabled: true})

	if got := s.store.Camera("cam_a").TotalBytes(); got != 1000 {
		t.Errorf("índice de cam_a com %d bytes depois do recadastro, esperava 1000", got)
	}
}

// comDefaultsDeVerdade é o testServer com os defaults de quem não escreveu
// dwnvr.yaml, todos preenchidos, e o go2rtc fora do ar: o GET lista as
// câmeras sem os streams, e o POST salva sem conferir o stream.
func withRealDefaults(t *testing.T) *Server {
	t.Helper()
	s, _ := testServer(t)
	s.cfg.Defaults = defaultConfig(t).Defaults
	s.client = go2rtc.New(config.Go2RTC{URL: "http://127.0.0.1:1"})
	return s
}

// respostaDoGet é o GET /api/cameras do admin, com cada câmera e o padrao no
// JSON genérico: a tela edita esse objeto e o devolve inteiro, e o teste faz
// o mesmo, sem passar pela config.Camera.
func getResponse(t *testing.T, s *Server) (cams map[string]map[string]any, defaults map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.requireAuth(s.handleCameras)(rec, httptest.NewRequest(http.MethodGet, "/api/cameras", nil))
	var got struct {
		Cameras []map[string]any `json:"cameras"`
		Default map[string]any   `json:"padrao"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("resposta %s (%v)", rec.Body.String(), err)
	}
	cams = map[string]map[string]any{}
	for _, c := range got.Cameras {
		cams[fmt.Sprint(c["id"])] = c
	}
	return cams, got.Default
}

// noArquivo é cada câmera do cameras.json, lido do disco: é o arquivo que
// decide se o default do dwnvr.yaml ainda chega à câmera.
func inFile(t *testing.T, s *Server) map[string]map[string]any {
	t.Helper()
	b, err := os.ReadFile(s.cfg.CamerasPath())
	if err != nil {
		t.Fatal(err)
	}
	var list []map[string]any
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatalf("cameras.json %s (%v)", b, err)
	}
	cams := map[string]map[string]any{}
	for _, c := range list {
		cams[fmt.Sprint(c["id"])] = c
	}
	return cams
}

func keys(m map[string]any) []string {
	return slices.Sorted(maps.Keys(m))
}

// A tela edita a câmera como o GET a mostra, já com os defaults aplicados, e
// a devolve inteira. Salvar sem mudar nada gravava cada default no
// cameras.json, e mudar o defaults do dwnvr.yaml não chegava mais à câmera. A
// câmera nova, que a tela monta a partir do padrao, era gravada do mesmo jeito.
func TestSaveWithoutChangesDoesNotFreezeTheDefaults(t *testing.T) {
	s := withRealDefaults(t)
	cams, defaults := getResponse(t, s)

	saveCamera(t, s, cams["cam_teste"])
	defaults["id"], defaults["name"], defaults["enabled"] = "cam_a", "A", true
	saveCamera(t, s, defaults)

	file := inFile(t, s)
	for _, id := range []string{"cam_teste", "cam_a"} {
		if got, want := keys(file[id]), []string{"enabled", "id", "name"}; !slices.Equal(got, want) {
			t.Errorf("%s no cameras.json com %v, esperava só %v", id, got, want)
		}
	}
}

// O que a câmera já definia fica no arquivo, mesmo igual ao default; o que a
// pessoa mudou entra; o resto segue o default.
func TestSaveKeepsWhatTheCameraSets(t *testing.T) {
	s := withRealDefaults(t)
	d := s.cfg.Defaults
	s.mgr.Set(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true,
		QuotaMB: d.QuotaMB, Audio: config.AudioAAC})

	cams, _ := getResponse(t, s)
	cam := cams["cam_teste"]
	cam["segmentSeconds"], cam["detect"] = 60, !d.Detect
	saveCamera(t, s, cam)

	got := inFile(t, s)["cam_teste"]
	want := map[string]any{
		"id": "cam_teste", "name": "Teste", "enabled": true,
		"quotaMB": float64(d.QuotaMB), "audio": config.AudioAAC,
		"segmentSeconds": float64(60), "detect": !d.Detect,
	}
	if !maps.Equal(got, want) {
		t.Errorf("cam_teste no cameras.json\n %v\nesperava\n %v", got, want)
	}
}

// A área chega da tela sem id: o servidor dá um, e é ele que vai para o
// cameras.json. A área inválida recusa o save inteiro, com o motivo.
func TestSaveDaCameraComAreas(t *testing.T) {
	s, _ := testServer(t)
	withGo2RTC(t, s)

	points := []area.Point{{0.1, 0.1}, {0.5, 0.1}, {0.5, 0.5}}
	saveCamera(t, s, config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true,
		Areas: []config.Area{{Name: "Portão", Points: points}}})
	cams, _, err := s.cfg.LoadCameras()
	if err != nil {
		t.Fatal(err)
	}
	if len(cams) != 1 || len(cams[0].Areas) != 1 || cams[0].Areas[0].ID == "" {
		t.Fatalf("no arquivo: %+v", cams)
	}

	b, _ := json.Marshal(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true,
		Areas: []config.Area{{Name: "Portão", Points: points[:2]}}})
	rec := httptest.NewRecorder()
	s.handleSaveCamera(rec, httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewReader(b)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "pontos") {
		t.Errorf("HTTP %d: %s, esperava 400 falando dos pontos", rec.Code, rec.Body.String())
	}
}

// O alerta vai ao cameras.json como chegou, com os horários. O que aponta
// para uma área que a câmera não tem recusa o save inteiro, com o motivo: a
// tela tira a área do alerta antes de apagá-la. O horário que atravessa a
// meia-noite também.
func TestSaveCameraWithAlert(t *testing.T) {
	s, _ := testServer(t)
	withGo2RTC(t, s)

	gate := config.Area{ID: "k3f9a2", Name: "Portão", Points: []area.Point{{0.1, 0.1}, {0.5, 0.1}, {0.5, 0.5}}}
	alert := &config.CameraAlert{Enabled: true, Families: []detect.Family{detect.Person}, Areas: []string{"k3f9a2"},
		Schedules: []config.AlertSchedule{{Days: []time.Weekday{time.Saturday, time.Sunday}, From: "00:00", To: "23:59"}}}
	saveCamera(t, s, config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true,
		Areas: []config.Area{gate}, Alert: alert})
	cams, _, err := s.cfg.LoadCameras()
	if err != nil {
		t.Fatal(err)
	}
	if len(cams) != 1 || !reflect.DeepEqual(cams[0].Alert, alert) {
		t.Fatalf("no arquivo: %+v", cams)
	}

	b, _ := json.Marshal(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, Alert: alert})
	rec := httptest.NewRecorder()
	s.handleSaveCamera(rec, httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewReader(b)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "alert.areas[0]") {
		t.Errorf("HTTP %d: %s, esperava 400 falando de alert.areas[0]", rec.Code, rec.Body.String())
	}

	night := &config.CameraAlert{Enabled: true, Families: []detect.Family{detect.Person},
		Schedules: []config.AlertSchedule{{Days: []time.Weekday{time.Monday}, From: "22:00", To: "06:00"}}}
	b, _ = json.Marshal(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, Alert: night})
	rec = httptest.NewRecorder()
	s.handleSaveCamera(rec, httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewReader(b)))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "alert.schedules[0].to") {
		t.Errorf("HTTP %d: %s, esperava 400 falando de alert.schedules[0].to", rec.Code, rec.Body.String())
	}
}
