package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/recorder"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// comGo2RTC troca o Manager do testServer por um que grava de verdade contra
// um go2rtc falso, e devolve quantas conexões de stream estão abertas agora -
// cada uma é um recorder no ar.
func comGo2RTC(t *testing.T, s *Server) *atomic.Int32 {
	t.Helper()
	var abertas atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/streams":
			_, _ = w.Write([]byte(`{"cam_teste": {}, "cam_a": {}, "cam_b": {}}`))
		case "/api/stream.mp4":
			abertas.Add(1)
			defer abertas.Add(-1)
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
	return &abertas
}

func saveCamera(t *testing.T, s *Server, cam config.Camera) {
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
func esperaAbertas(t *testing.T, abertas *atomic.Int32, n int32) {
	t.Helper()
	fim := time.Now().Add(3 * time.Second)
	desde := time.Time{}
	for time.Now().Before(fim) {
		if abertas.Load() != n {
			desde = time.Time{}
		} else if desde.IsZero() {
			desde = time.Now()
		} else if time.Since(desde) > 300*time.Millisecond {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%d conexões abertas no go2rtc, esperava %d", abertas.Load(), n)
}

// Dois saves da mesma câmera ao mesmo tempo, os dois trocando o áudio - o que
// exige reconectar. Sem o cadastro, cada um parava o recorder antigo e subia
// um novo, e o primeiro novo ficava gravando fora do mapa até o restart: duas
// conexões com a câmera e só uma na tela.
func TestSavesConcorrentesNaoDeixamRecorderOrfao(t *testing.T) {
	s, _ := testServer(t)
	abertas := comGo2RTC(t, s)
	esperaAbertas(t, abertas, 1)

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
		esperaAbertas(t, abertas, 1)
	}
}

// Dois saves de câmeras diferentes ao mesmo tempo liam a mesma lista, e quem
// gravava o cameras.json por último apagava a alteração do outro.
func TestSavesConcorrentesNaoPerdemAlteracaoNoArquivo(t *testing.T) {
	s, _ := testServer(t)
	comGo2RTC(t, s)

	for i := range 50 {
		var wg sync.WaitGroup
		for _, id := range []string{"cam_a", "cam_b"} {
			wg.Go(func() {
				saveCamera(t, s, config.Camera{ID: id, Name: fmt.Sprint(id, " ", i)})
			})
		}
		wg.Wait()

		cams, err := s.cfg.LoadCameras()
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
func TestRecadastroLeAsGravacoesQueJaEstaoNoDisco(t *testing.T) {
	s, _ := testServer(t)
	comGo2RTC(t, s)

	// O que ficou da vida anterior: outro Store sobre o mesmo storage, como o
	// processo que rodava antes do reinício.
	antes := store.New(s.store.Root()).Camera("cam_a")
	inicio := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	seed(t, antes, inicio, [2]int64{0, 30000})
	if err := antes.EnsureDirs(inicio.Format(store.DayLayout)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(antes.SegmentPath(inicio.UnixMilli()), make([]byte, 1000), 0o644); err != nil {
		t.Fatal(err)
	}

	saveCamera(t, s, config.Camera{ID: "cam_a", Name: "A", Enabled: true})

	if got := s.store.Camera("cam_a").TotalBytes(); got != 1000 {
		t.Errorf("índice de cam_a com %d bytes depois do recadastro, esperava 1000", got)
	}
}
