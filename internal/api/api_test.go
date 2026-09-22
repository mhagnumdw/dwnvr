package api

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/recorder"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

func testServer(t *testing.T) (*Server, *store.Camera) {
	t.Helper()
	st := store.New(t.TempDir())

	// Config carregada a partir de um dwnvr.yaml inexistente num diretório
	// temporário. O caminho importa: é dele que sai o CamerasPath, e um Config
	// zerado faria o SaveCameras despejar um cameras.json no diretório do pacote.
	cfg, err := config.Load(filepath.Join(t.TempDir(), "dwnvr.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Storage.Root = st.Root()
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

func getEvents(t *testing.T, s *Server, query string) eventsResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleEvents(rec, httptest.NewRequest(http.MethodGet, "/api/rec/events?"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	var out eventsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("resposta ilegível: %v", err)
	}
	return out
}

func TestEventsDevolveAsMarcasDoDia(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	for _, off := range []int64{0, 60_000, 120_000} {
		if err := cam.AppendEvento(store.Evento{InstanteMs: base.UnixMilli() + off}); err != nil {
			t.Fatal(err)
		}
	}

	got := getEvents(t, s, "cam=cam_teste&day="+base.Format(store.DayLayout))
	if len(got.Onsets) != 3 {
		t.Fatalf("esperava 3 marcas, veio %d: %v", len(got.Onsets), got.Onsets)
	}
	if got.Onsets[0] != base.UnixMilli() {
		t.Errorf("primeira marca = %d, esperado %d", got.Onsets[0], base.UnixMilli())
	}
}

// A marca de objeto mora no mesmo arquivo do onset, e não pode aparecer na
// faixa de calor como um segundo onset.
func TestEventsSeparaObjetoDeMovimento(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local).UnixMilli()
	for _, ev := range []store.Evento{
		{InstanteMs: base},
		{InstanteMs: base, Familia: "pessoa", Classe: "person", Score: 0.87},
		{InstanteMs: base + 60_000},
	} {
		if err := cam.AppendEvento(ev); err != nil {
			t.Fatal(err)
		}
	}
	got := getEvents(t, s, "cam=cam_teste&day=2026-08-08")
	if len(got.Onsets) != 2 {
		t.Errorf("onsets %v: a marca de objeto entrou na faixa de calor", got.Onsets)
	}
	want := objetoMarcado{InstanteMs: base, Familia: "pessoa", Classe: "person", Score: 0.87}
	if len(got.Objetos) != 1 || got.Objetos[0] != want {
		t.Errorf("objetos %+v, esperado [%+v]", got.Objetos, want)
	}
}

// A marca com caixa leva a caixa e o instante do quadro olhado até a tela; a
// marca sem eles sai sem as duas chaves, e não com zeros - uma caixa
// [0,0,0,0] seria desenhada como um ponto no canto do vídeo.
func TestEventsLevaACaixaSoDaMarcaQueTem(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local).UnixMilli()
	caixa := [4]float64{0.5156, 0.2611, 0.6734, 0.9778}
	for _, ev := range []store.Evento{
		{InstanteMs: base, Familia: "pessoa", Classe: "person", Score: 0.87},
		{InstanteMs: base + 60_000, Familia: "veiculo", Classe: "car", Score: 0.91,
			QuadroMs: base + 62_000, Caixa: &caixa},
	} {
		if err := cam.AppendEvento(ev); err != nil {
			t.Fatal(err)
		}
	}

	rec := httptest.NewRecorder()
	s.handleEvents(rec, httptest.NewRequest(http.MethodGet, "/api/rec/events?cam=cam_teste&day=2026-08-08", nil))
	var cru struct {
		Objetos []map[string]json.RawMessage `json:"objetos"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &cru); err != nil || len(cru.Objetos) != 2 {
		t.Fatalf("resposta %s (%v)", rec.Body.String(), err)
	}
	for _, chave := range []string{"quadroMs", "caixa"} {
		if _, tem := cru.Objetos[0][chave]; tem {
			t.Errorf("a marca sem caixa saiu com %q: %s", chave, rec.Body.String())
		}
	}

	got := getEvents(t, s, "cam=cam_teste&day=2026-08-08").Objetos[1]
	if got.QuadroMs != base+62_000 {
		t.Errorf("quadroMs = %d, esperado %d", got.QuadroMs, base+62_000)
	}
	if got.Caixa == nil || *got.Caixa != caixa {
		t.Errorf("caixa = %v, esperado %v", got.Caixa, caixa)
	}
}

// O caminho de cauda: a tela do dia corrente pergunta de novo a cada dez
// segundos e só quer o que ainda não tem.
func TestEventsCaudaSoTrazOQueEhNovo(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	for _, off := range []int64{0, 60_000, 120_000} {
		if err := cam.AppendEvento(store.Evento{InstanteMs: base.UnixMilli() + off}); err != nil {
			t.Fatal(err)
		}
	}

	from := base.UnixMilli() + 60_001
	got := getEvents(t, s, fmt.Sprintf("cam=cam_teste&from=%d&to=%d",
		from, base.Add(24*time.Hour).UnixMilli()))
	if len(got.Onsets) != 1 || got.Onsets[0] != base.UnixMilli()+120_000 {
		t.Errorf("cauda = %v, esperava só a marca dos 120s", got.Onsets)
	}
}

// Câmera sem detecção ligada é o caso comum, e a tela precisa distinguir "sem
// marcas" de "deu erro". Lista vazia, e não null.
func TestEventsSemMarcasDevolveListaVazia(t *testing.T) {
	s, _ := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)

	rec := httptest.NewRecorder()
	s.handleEvents(rec, httptest.NewRequest(http.MethodGet,
		"/api/rec/events?cam=cam_teste&day="+base.Format(store.DayLayout), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"onsets":[]`) {
		t.Errorf("esperava lista vazia, veio %s", rec.Body.String())
	}
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

// A tela de Gravações relê o dia de hoje em ciclo pedindo só a cauda, a partir
// de 1ms depois do início do último segmento que ela já tem. Ela conta com esse
// segmento voltar na resposta: é a sobreposição que diz se a primeira faixa nova
// continua a última faixa antiga, e sem ela a tela precisaria repetir do lado
// dela a tolerância de emenda que mora aqui.
func TestTimelineParcialDevolveOSegmentoAncora(t *testing.T) {
	s, cam := testServer(t)
	base := time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
	seed(t, cam, base, [2]int64{0, 30000}, [2]int64{30000, 30000}, [2]int64{60000, 30000})

	// Do último segmento que a tela conhecia (o do meio) em diante.
	ancora := base.UnixMilli() + 30000
	fim := base.AddDate(0, 0, 1).UnixMilli()
	got := getTimeline(t, s, fmt.Sprintf("cam=cam_teste&from=%d&to=%d", ancora+1, fim))

	if len(got.Segments) != 2 {
		t.Fatalf("esperava a âncora mais o segmento novo, veio %v", got.Segments)
	}
	if got.Segments[0][0] != ancora {
		t.Errorf("primeiro segmento = %d, esperava a âncora em %d", got.Segments[0][0], ancora)
	}
	// E a faixa da cauda tem que começar na âncora, não no segmento novo: é isso
	// que faz a tela reconhecer que as duas faixas são a mesma.
	if len(got.Ranges) != 1 || got.Ranges[0][0] != ancora {
		t.Errorf("faixas = %v, esperava uma só começando em %d", got.Ranges, ancora)
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

// --- apagar gravações -------------------------------------------------------

func deleteReq(t *testing.T, h http.HandlerFunc, url string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodDelete, url, nil))
	return rec
}

func freedBytes(t *testing.T, rec *httptest.ResponseRecorder) int64 {
	t.Helper()
	var out struct {
		FreedBytes int64 `json:"freedBytes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("resposta ilegível: %v", err)
	}
	return out.FreedBytes
}

// O padrão é preservar. Apagar horas de vídeo como efeito colateral de um clique
// em "remover" seria destrutivo demais para ser implícito, e este teste é o que
// impede que a opção nova vire o comportamento padrão por descuido.
func TestDeleteCameraMantemGravacoesPorPadrao(t *testing.T) {
	s, cam := testServer(t)
	seed(t, cam, time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local), [2]int64{0, 30000})

	rec := deleteReq(t, s.handleDeleteCamera, "/api/cameras?id=cam_teste")
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(cam.Dir()); err != nil {
		t.Errorf("as gravações sumiram sem ninguém pedir: %v", err)
	}
}

func TestDeleteCameraApagaGravacoesQuandoPedido(t *testing.T) {
	s, cam := testServer(t)
	seed(t, cam, time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local), [2]int64{0, 30000})

	rec := deleteReq(t, s.handleDeleteCamera, "/api/cameras?id=cam_teste&recordings=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(cam.Dir()); !os.IsNotExist(err) {
		t.Errorf("o diretório da câmera continua lá: err=%v", err)
	}
	if got := freedBytes(t, rec); got != 1000 {
		t.Errorf("freedBytes=%d, esperava 1000", got)
	}
}

// Apagar as gravações não pode descadastrar a câmera: ela precisa continuar
// gravando, do zero.
func TestDeleteRecordingsMantemOCadastro(t *testing.T) {
	s, cam := testServer(t)
	seed(t, cam, time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local), [2]int64{0, 30000})

	rec := deleteReq(t, s.handleDeleteRecordings, "/api/rec?cam=cam_teste")
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(cam.Dir()); !os.IsNotExist(err) {
		t.Errorf("o diretório da câmera continua lá: err=%v", err)
	}
	if !s.knownCamera("cam_teste") {
		t.Error("a câmera foi descadastrada, e só as gravações deviam sumir")
	}
	if got := freedBytes(t, rec); got != 1000 {
		t.Errorf("freedBytes=%d, esperava 1000", got)
	}
}

// O material de uma câmera já removida só é alcançável por aqui - e o ID, que
// não passa mais pelo knownCamera, é conferido contra os diretórios que existem
// de fato.
func TestDeleteRecordingsDeCameraRemovida(t *testing.T) {
	s, _ := testServer(t)
	orfa := s.store.Camera("cam_antiga")
	seed(t, orfa, time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local), [2]int64{0, 30000})

	rec := deleteReq(t, s.handleDeleteRecordings, "/api/rec?cam=cam_antiga")
	if rec.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(orfa.Dir()); !os.IsNotExist(err) {
		t.Errorf("o diretório da órfã continua lá: err=%v", err)
	}
	// O índice em memória de uma órfã está vazio, então o total só pode vir da
	// varredura. Zero aqui significaria que voltamos a lê-lo do lugar errado.
	if got := freedBytes(t, rec); got != 1000 {
		t.Errorf("freedBytes=%d, esperava 1000", got)
	}
}

func TestDeleteRecordingsExigeDiretorioExistente(t *testing.T) {
	s, _ := testServer(t)
	for _, cam := range []string{"", "outra", "../../etc", "cam_teste/../x"} {
		rec := deleteReq(t, s.handleDeleteRecordings, "/api/rec?cam="+cam)
		if rec.Code != http.StatusNotFound {
			t.Errorf("cam=%q devolveu HTTP %d, esperava 404", cam, rec.Code)
		}
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

func TestValidateCameraCota(t *testing.T) {
	// O zero é o caso que separa "não informei" de "informei um valor ruim":
	// ele tem que continuar passando para o Resolve aplicar o default.
	validas := []int64{0, minQuotaMB, minQuotaMB + 1, 20480}
	invalidas := []int64{-1, 1, minQuotaMB - 1}

	for _, q := range validas {
		if err := validateCamera(config.Camera{ID: "cam_teste", QuotaMB: q}); err != nil {
			t.Errorf("quotaMB=%d recusada: %v", q, err)
		}
	}
	for _, q := range invalidas {
		if err := validateCamera(config.Camera{ID: "cam_teste", QuotaMB: q}); err == nil {
			t.Errorf("quotaMB=%d aceita, deveria recusar", q)
		}
	}
}

func TestValidateCameraDetect(t *testing.T) {
	// Vazio e zero são "usar o default"; preencher só um dos dois vale.
	validas := []config.Camera{
		{ID: "cam_teste"},
		{ID: "cam_teste", DetectMecanismo: "periodico"},
		{ID: "cam_teste", DetectSensibilidade: 1},
		{ID: "cam_teste", DetectMecanismo: "kleinberg-p", DetectSensibilidade: 5},
	}
	invalidas := []config.Camera{
		{ID: "cam_teste", DetectMecanismo: "magico"},
		{ID: "cam_teste", DetectSensibilidade: 6},
		{ID: "cam_teste", DetectMecanismo: "periodico", DetectSensibilidade: 9},
	}
	for _, c := range validas {
		if err := validateCamera(c); err != nil {
			t.Errorf("%q nível %d recusada: %v", c.DetectMecanismo, c.DetectSensibilidade, err)
		}
	}
	for _, c := range invalidas {
		if err := validateCamera(c); err == nil {
			t.Errorf("%q nível %d aceita, deveria recusar", c.DetectMecanismo, c.DetectSensibilidade)
		}
	}
}

// O formulário de câmera nova parte do "padrao", e não de números repetidos à
// mão na interface: é assim que ele segue o defaults do dwnvr.yaml.
func TestCamerasTrazOPadrao(t *testing.T) {
	s, _ := testServer(t)
	s.cfg.Defaults.DetectMecanismo, s.cfg.Defaults.DetectSensibilidade = "periodico", 2
	s.client = go2rtc.New(config.Go2RTC{URL: "http://127.0.0.1:1"}) // fora do ar

	rec := httptest.NewRecorder()
	s.handleCameras(rec, httptest.NewRequest(http.MethodGet, "/api/cameras", nil))
	var got struct {
		Padrao config.Camera `json:"padrao"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("resposta %s (%v)", rec.Body.String(), err)
	}
	p := got.Padrao
	if p.Detect == nil || *p.Detect || p.DetectMecanismo != "periodico" || p.DetectSensibilidade != 2 {
		t.Errorf("padrao %+v: esperava detect false, periodico, nível 2", p)
	}
}

// A fila do detector só aparece no /api/health com o detector configurado.
func TestHealthMostraODetectorSoQuandoConfigurado(t *testing.T) {
	temDetector := func(s *Server) bool {
		rec := httptest.NewRecorder()
		s.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
		var got map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("resposta %s (%v)", rec.Body.String(), err)
		}
		_, tem := got["detector"]
		return tem
	}

	s, _ := testServer(t)
	if temDetector(s) {
		t.Error("detector no /api/health sem detector.url")
	}
	s.cfg.Detector.URL = "http://127.0.0.1:1"
	s.mgr = recorder.NewManager(s.cfg, nil, s.store, s.log)
	if !temDetector(s) {
		t.Error("detector.url configurado e o /api/health não mostra a fila")
	}
}

// A aba Detecções depende deste campo para aparecer.
func TestCamerasDizSeHaDetector(t *testing.T) {
	temDetector := func(s *Server) bool {
		rec := httptest.NewRecorder()
		s.handleCameras(rec, httptest.NewRequest(http.MethodGet, "/api/cameras", nil))
		var got struct {
			Detector *bool `json:"detector"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("resposta %s (%v)", rec.Body.String(), err)
		}
		if got.Detector == nil {
			t.Fatal("o /api/cameras não trouxe o campo detector")
		}
		return *got.Detector
	}

	s, _ := testServer(t)
	s.client = go2rtc.New(config.Go2RTC{URL: "http://127.0.0.1:1"}) // fora do ar
	if temDetector(s) {
		t.Error("detector: true sem detector.url")
	}
	s.cfg.Detector.URL = "http://127.0.0.1:1"
	s.mgr = recorder.NewManager(s.cfg, nil, s.store, s.log)
	if !temDetector(s) {
		t.Error("detector.url configurado e o /api/cameras diz detector: false")
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

// O favicon é um SVG com um comentário em cima, e comentário XML não admite
// hífen duplo. Citar ali dentro o nome de uma variável CSS torna o arquivo
// malformado - e o sintoma é cruel: o servidor devolve 200, com content-type e
// tamanho certos, e o navegador simplesmente não desenha nada. Foi assim que
// uma versão quebrada chegou a rodar em produção sem que nenhuma verificação
// de deploy reclamasse. Este teste é a porta que faltava.
func TestFaviconEmbutidoEhXMLBemFormado(t *testing.T) {
	b, err := dist.ReadFile("dist/favicon.svg")
	if err != nil {
		t.Fatalf("favicon não está embutido: %v", err)
	}

	dec := xml.NewDecoder(bytes.NewReader(b))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("favicon.svg malformado, o navegador não vai desenhá-lo: %v", err)
		}
	}
}

// De nada adianta o arquivo estar íntegro se a página parar de apontar para
// ele: o resultado visível é o mesmo, aba sem ícone.
func TestIndexApontaParaOFavicon(t *testing.T) {
	b, err := dist.ReadFile("dist/index.html")
	if err != nil {
		t.Fatalf("index.html não está embutido: %v", err)
	}
	if !strings.Contains(string(b), `href="/favicon.svg"`) {
		t.Error("index.html não referencia /favicon.svg")
	}
}
