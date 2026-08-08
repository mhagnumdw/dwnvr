package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/recorder"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

func testServer(t *testing.T) (*Server, *store.Camera) {
	t.Helper()
	st := store.New(t.TempDir())
	cfg := &config.Config{}
	cfg.Defaults = config.Defaults{SegmentSeconds: 30, QuotaMB: 100, Audio: config.AudioNone}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Manager sem Start: registra a câmera sem subir gravação de verdade, que
	// é o que estes testes precisam.
	mgr := recorder.NewManager(cfg, nil, st, log)
	mgr.Set(config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true})

	s := &Server{
		cfg:    cfg,
		store:  st,
		mgr:    mgr,
		secret: []byte("segredo-de-teste-com-32-bytes!!!"),
		log:    log,
	}
	return s, st.Camera("cam_teste")
}

func seed(t *testing.T, cam *store.Camera, base time.Time, offsets ...[2]int64) {
	t.Helper()
	for _, o := range offsets {
		e := store.Entry{
			StartMs: base.UnixMilli() + o[0], DurMs: o[1],
			Size: 1000, Gen: "aabbcc", InitSize: 737, FirstFrag: 500,
		}
		if err := cam.Append(e); err != nil {
			t.Fatal(err)
		}
	}
}

func getTimeline(t *testing.T, s *Server, query string) timelineResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleTimeline(rec, httptest.NewRequest(http.MethodGet, "/api/rec/timeline?"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var out timelineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("resposta ilegível: %v", err)
	}
	return out
}

// Segmentos encostados têm que virar UMA faixa: a barra de 24h ficaria coberta
// de buracos falsos de 1 pixel se cada segmento virasse uma faixa.
func TestTimelineFundeSegmentosContiguos(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	seed(t, cam, base, [2]int64{0, 30000}, [2]int64{30000, 30000}, [2]int64{60000, 30000})

	got := getTimeline(t, s, "cam=cam_teste&day="+base.Format(store.DayLayout))
	if len(got.Ranges) != 1 {
		t.Fatalf("esperava 1 faixa, veio %d: %v", len(got.Ranges), got.Ranges)
	}
	if got.Ranges[0][0] != base.UnixMilli() || got.Ranges[0][1] != base.UnixMilli()+90000 {
		t.Errorf("faixa = %v, esperava cobrir os 90s inteiros", got.Ranges[0])
	}
	if len(got.Segments) != 3 {
		t.Errorf("esperava 3 segmentos, veio %d", len(got.Segments))
	}
	if len(got.Gens) != 1 || got.Gens[0] != "aabbcc" {
		t.Errorf("tabela de gerações = %v", got.Gens)
	}
}

// Um buraco de verdade precisa aparecer: é justamente o que a tela existe para
// mostrar, e foi o que faltou nos NVRs que motivaram o projeto.
func TestTimelineSeparaFaixasNoBuraco(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	// 30s, buraco de 40s, mais 30s.
	seed(t, cam, base, [2]int64{0, 30000}, [2]int64{70000, 30000})

	got := getTimeline(t, s, "cam=cam_teste&day="+base.Format(store.DayLayout))
	if len(got.Ranges) != 2 {
		t.Fatalf("esperava 2 faixas, veio %d: %v", len(got.Ranges), got.Ranges)
	}
}

// Uma folga menor que a tolerância é jitter, não buraco.
func TestTimelineIgnoraFolgaPequena(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	seed(t, cam, base, [2]int64{0, 30000}, [2]int64{30500, 30000})

	got := getTimeline(t, s, "cam=cam_teste&day="+base.Format(store.DayLayout))
	if len(got.Ranges) != 1 {
		t.Errorf("folga de 500ms virou buraco: %v", got.Ranges)
	}
}

// O gravador impede sobreposição, mas o índice pode ter dados antigos. A fusão
// não pode encurtar uma faixa por causa disso.
func TestTimelineNaoEncurtaComSobreposicao(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	seed(t, cam, base, [2]int64{0, 30000}, [2]int64{29000, 30000})

	got := getTimeline(t, s, "cam=cam_teste&day="+base.Format(store.DayLayout))
	if len(got.Ranges) != 1 {
		t.Fatalf("esperava 1 faixa, veio %v", got.Ranges)
	}
	if want := base.UnixMilli() + 59000; got.Ranges[0][1] != want {
		t.Errorf("fim da faixa = %d, esperava %d", got.Ranges[0][1], want)
	}
}

func TestTimelineSemGravacaoDevolveListasVazias(t *testing.T) {
	s, _ := testServer(t)
	got := getTimeline(t, s, "cam=cam_teste&day=2026-08-08")
	// Listas vazias, e não null: o frontend não deve precisar tratar os dois.
	if got.Ranges == nil || got.Segments == nil || got.Gens == nil {
		t.Errorf("esperava listas vazias, veio ranges=%v segments=%v gens=%v",
			got.Ranges, got.Segments, got.Gens)
	}
}

// Câmera não cadastrada é recusada antes de virar caminho no disco.
func TestCameraDesconhecidaEhRecusada(t *testing.T) {
	s, _ := testServer(t)
	for _, cam := range []string{"", "outra", "../../etc", "cam_teste/../x"} {
		rec := httptest.NewRecorder()
		s.handleDays(rec, httptest.NewRequest(http.MethodGet, "/api/rec/days?cam="+cam, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("cam=%q devolveu HTTP %d, esperava 400", cam, rec.Code)
		}
	}
}

func TestValidGen(t *testing.T) {
	validos := []string{"aabbcc", "0123456789abcdef"}
	invalidos := []string{"", "../../etc/passwd", "AABBCC", "aa-bb", "g123",
		"aabbccddeeff00112233445566778899aa"}

	for _, g := range validos {
		if err := validGen(g); err != nil {
			t.Errorf("validGen(%q) recusou: %v", g, err)
		}
	}
	for _, g := range invalidos {
		if err := validGen(g); err == nil {
			t.Errorf("validGen(%q) aceitou, deveria recusar", g)
		}
	}
}

func TestRangeParams(t *testing.T) {
	s, _ := testServer(t)

	// O atalho day= tem que cobrir o dia inteiro em hora local.
	r := httptest.NewRequest(http.MethodGet, "/x?day=2026-08-08", nil)
	from, to, err := s.rangeParams(r)
	if err != nil {
		t.Fatalf("day válido recusado: %v", err)
	}
	if to-from != 24*3600*1000 {
		t.Errorf("day cobriu %dms, esperava 24h", to-from)
	}
	if got := time.UnixMilli(from).Format("2006-01-02 15:04:05"); got != "2026-08-08 00:00:00" {
		t.Errorf("day começou em %s", got)
	}

	ruins := []string{"day=08/08/2026", "from=abc&to=1", "from=1", "from=100&to=100", "from=200&to=100"}
	for _, q := range ruins {
		if _, _, err := s.rangeParams(httptest.NewRequest(http.MethodGet, "/x?"+q, nil)); err == nil {
			t.Errorf("%q foi aceito, deveria ser recusado", q)
		}
	}
}

// --- sessão -----------------------------------------------------------------

func TestTokenDeSessao(t *testing.T) {
	s, _ := testServer(t)

	valido := s.signToken(time.Now().Add(time.Hour).Unix())
	if !s.validToken(valido) {
		t.Error("token recém-assinado foi recusado")
	}

	if s.validToken(s.signToken(time.Now().Add(-time.Hour).Unix())) {
		t.Error("token expirado foi aceito")
	}

	// Esticar o prazo tem que invalidar: o HMAC cobre o payload, então a
	// assinatura deixa de bater.
	_, sig, _ := strings.Cut(valido, ".")
	if s.validToken("99999999999." + sig) {
		t.Error("prazo esticado foi aceito")
	}

	// Assinatura de outro segredo não pode valer.
	outro := &Server{secret: []byte("outro-segredo-de-32-bytes!!!!!!!")}
	if s.validToken(outro.signToken(time.Now().Add(time.Hour).Unix())) {
		t.Error("token assinado com outro segredo foi aceito")
	}

	for _, ruim := range []string{"", "semponto", "abc.def", ".", "123."} {
		if s.validToken(ruim) {
			t.Errorf("token malformado %q foi aceito", ruim)
		}
	}
}

func TestRequireAuth(t *testing.T) {
	s, _ := testServer(t)
	chamou := false
	h := s.requireAuth(func(w http.ResponseWriter, r *http.Request) { chamou = true })

	// Sem credencial configurada a autenticação fica desligada.
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if !chamou {
		t.Error("sem credenciais, o handler deveria ter sido chamado")
	}

	s.cfg.Server.Username, s.cfg.Server.Password = "admin", "senha"
	chamou = false
	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if chamou || rec.Code != http.StatusUnauthorized {
		t.Errorf("com credenciais, sem cookie: chamou=%v status=%d", chamou, rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie,
		Value: s.signToken(time.Now().Add(time.Hour).Unix())})
	rec = httptest.NewRecorder()
	h(rec, req)
	if !chamou {
		t.Error("cookie válido deveria ter passado")
	}
}
