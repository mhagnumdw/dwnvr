package retention

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

const mb = 1 << 20

// agoraFixo é o relógio dos testes: 12h locais, longe da meia-noite, para que
// "N dias atrás" não dependa da hora nem do fuso de quem roda.
var agoraFixo = time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)

// diaAtras é o meio-dia de N dias antes do relógio dos testes.
func diaAtras(n int) time.Time { return agoraFixo.AddDate(0, 0, -n) }

func dia(n int) string { return diaAtras(n).Format(store.DayLayout) }

// novoManager monta a retenção sobre um store vazio num diretório temporário,
// com o relógio fixo. O disco livre, cada teste que o usa define.
func novoManager(t *testing.T, minFreeMB int64, cams *[]config.Camera) (*Manager, *store.Store) {
	t.Helper()
	st := store.New(t.TempDir())
	cfg := &config.Config{Storage: config.Storage{MinFreeMB: minFreeMB}}
	m := New(cfg, st, func() []config.Camera { return *cams }, slog.New(slog.DiscardHandler))
	m.agora = func() time.Time { return agoraFixo }
	m.livre = func(string) (int64, error) {
		t.Fatal("este teste não deveria consultar o disco")
		return 0, nil
	}
	return m, st
}

// grava registra `n` segmentos de `tamanho` bytes, um por minuto a partir de
// `inicio`. O arquivo é esparso: tem o tamanho certo sem ocupar disco.
func grava(t *testing.T, st *store.Store, cam string, inicio time.Time, n int, tamanho int64) {
	t.Helper()
	c := st.Camera(cam)
	for i := range n {
		e := store.Entry{StartMs: inicio.Add(time.Duration(i) * time.Minute).UnixMilli(), DurMs: 60_000, Size: tamanho}
		if err := c.EnsureDirs(e.Day()); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(c.SegmentPath(e.StartMs))
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(tamanho); err != nil {
			t.Fatal(err)
		}
		f.Close()
		if err := c.Append(e); err != nil {
			t.Fatal(err)
		}
	}
}

// discoDe simula um disco de `capacidade` bytes onde só o dwnvr escreve: o
// livre é o que as câmeras ainda não ocupam, e cai a cada gravação apagada.
func discoDe(st *store.Store, capacidade int64, cams ...string) func(string) (int64, error) {
	return func(string) (int64, error) {
		usado := int64(0)
		for _, c := range cams {
			usado += st.Camera(c).TotalBytes()
		}
		return capacidade - usado, nil
	}
}

func dias(st *store.Store, cam string) []string {
	var out []string
	for _, d := range st.Camera(cam).Days() {
		out = append(out, d.Day)
	}
	return out
}

func confereDias(t *testing.T, st *store.Store, cam string, want ...string) {
	t.Helper()
	got := dias(st, cam)
	if len(got) != len(want) {
		t.Fatalf("%s ficou com os dias %v, esperava %v", cam, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s ficou com os dias %v, esperava %v", cam, got, want)
		}
	}
}

// A câmera acima da cota perde o mais antigo até caber, e só ela: a cota de
// uma não pode apagar gravação da outra.
func TestCotaApagaSoDaCameraAcima(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 3}, {ID: "cam_b", QuotaMB: 10}}
	m, st := novoManager(t, 0, &cams)
	grava(t, st, "cam_a", diaAtras(0), 5, mb)
	grava(t, st, "cam_b", diaAtras(0), 5, mb)

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}

	a := st.Camera("cam_a")
	if got := a.TotalBytes(); got != 3*mb {
		t.Errorf("cam_a ocupa %d MB, esperava a cota, 3", got/mb)
	}
	// Saíram os dois primeiros segmentos, não dois quaisquer.
	if got, want := a.OldestMs(), diaAtras(0).Add(2*time.Minute).UnixMilli(); got != want {
		t.Errorf("o mais antigo da cam_a começa em %d, esperava %d (o terceiro)", got, want)
	}
	if got := st.Camera("cam_b").TotalBytes(); got != 5*mb {
		t.Errorf("cam_b ocupa %d MB, esperava os 5 intactos", got/mb)
	}
}

// O dia além do maxDays sai inteiro; o próprio dia do limite fica.
func TestIdadeMaximaApagaOsDiasAlem(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 100, MaxDays: 3}}
	m, st := novoManager(t, 0, &cams)
	for d := 5; d >= 0; d-- {
		grava(t, st, "cam_a", diaAtras(d), 1, mb)
	}

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	confereDias(t, st, "cam_a", dia(3), dia(2), dia(1), dia(0))
}

// maxDays zero é "sem limite de idade": quem pensa em GB não perde gravação
// por ela ser velha.
func TestIdadeMaximaZeroNaoApagaNada(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 100}}
	m, st := novoManager(t, 0, &cams)
	for d := 400; d >= 0; d -= 100 {
		grava(t, st, "cam_a", diaAtras(d), 1, mb)
	}

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	confereDias(t, st, "cam_a", dia(400), dia(300), dia(200), dia(100), dia(0))
}

// Com o disco abaixo do mínimo, sai o dia mais antigo de qualquer câmera, um
// por vez, até o disco voltar; as cotas não seguram nada.
func TestDiscoApertadoApagaODiaMaisAntigoDeQualquerCamera(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 100}, {ID: "cam_b", QuotaMB: 100}}
	m, st := novoManager(t, 5, &cams)
	for _, d := range []int{2, 1, 0} {
		grava(t, st, "cam_a", diaAtras(d), 2, mb)
	}
	for _, d := range []int{3, 0} {
		grava(t, st, "cam_b", diaAtras(d), 2, mb)
	}
	// 10 MB ocupados num disco de 12: sobram 2, e o mínimo é 5. Apagar o
	// dia 3 da cam_b dá 4, ainda pouco; o dia 2 da cam_a dá 6.
	m.livre = discoDe(st, 12*mb, "cam_a", "cam_b")

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	confereDias(t, st, "cam_a", dia(1), dia(0))
	confereDias(t, st, "cam_b", dia(0))
	if !m.AbaixoDoMinimoDesde().IsZero() {
		t.Error("o disco voltou ao mínimo e o aviso ficou")
	}
}

// A cota roda antes do disco. Ela apara segmento a segmento, e o disco apaga
// um dia inteiro: na ordem invertida, um aperto que a cota resolveria com 1 MB
// custaria o dia todo.
func TestCotaRodaAntesDoDisco(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 5}}
	m, st := novoManager(t, 5, &cams)
	for _, d := range []int{2, 1, 0} {
		grava(t, st, "cam_a", diaAtras(d), 2, mb)
	}
	// 6 MB ocupados, 4,5 livres: abaixo do mínimo de 5 até a cota tirar 1 MB.
	m.livre = discoDe(st, 10*mb+mb/2, "cam_a")

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	confereDias(t, st, "cam_a", dia(2), dia(1), dia(0))
	if got := st.Camera("cam_a").TotalBytes(); got != 5*mb {
		t.Errorf("cam_a ocupa %d MB, esperava 5: só a cota deveria ter apagado", got/mb)
	}
}

// Câmera cadastrada ou alterada com o dwnvr no ar vale na passada seguinte,
// sem reiniciar.
func TestCameraNovaOuAlteradaValeNaPassadaSeguinte(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 10}}
	m, st := novoManager(t, 0, &cams)
	grava(t, st, "cam_a", diaAtras(0), 5, mb)
	grava(t, st, "cam_b", diaAtras(0), 5, mb)

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	if got := st.Camera("cam_a").TotalBytes(); got != 5*mb {
		t.Fatalf("cam_a ocupa %d MB antes de mudar a cota, esperava 5", got/mb)
	}

	cams = []config.Camera{{ID: "cam_a", QuotaMB: 2}, {ID: "cam_b", QuotaMB: 3}}
	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	if got := st.Camera("cam_a").TotalBytes(); got != 2*mb {
		t.Errorf("cam_a ocupa %d MB, esperava a cota nova, 2", got/mb)
	}
	if got := st.Camera("cam_b").TotalBytes(); got != 3*mb {
		t.Errorf("cam_b ocupa %d MB, esperava a cota da câmera nova, 3", got/mb)
	}
}

// minFreeMB zero desliga a rede de segurança: o disco nem é consultado.
func TestDiscoSemMinimoNaoConsultaODisco(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 100}}
	m, st := novoManager(t, 0, &cams)
	grava(t, st, "cam_a", diaAtras(0), 1, mb)

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	if !m.AbaixoDoMinimoDesde().IsZero() {
		t.Error("sem mínimo configurado não há aviso de disco")
	}
}

// Disco tomado por algo fora do dwnvr: apagar não devolve o espaço. A passada
// apaga tudo o que é do dwnvr e para, em vez de girar para sempre, e o aviso
// fica ligado.
func TestDiscoQueNaoVoltaApagaTudoEPara(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 100}}
	m, st := novoManager(t, 5, &cams)
	for _, d := range []int{1, 0} {
		grava(t, st, "cam_a", diaAtras(d), 1, mb)
	}
	m.livre = func(string) (int64, error) { return mb, nil }

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	confereDias(t, st, "cam_a")
	if got := m.AbaixoDoMinimoDesde(); !got.Equal(agoraFixo) {
		t.Errorf("aviso de disco desde %v, esperava %v", got, agoraFixo)
	}
}

// Gravação de câmera removida não sai nem com o disco apertado: quem paga é a
// câmera no ar, e os avisos dizem quanto há em órfãs. No fim, com as cadastradas
// drenadas, o aviso não pode dizer que não sobrou nada.
func TestDiscoApertadoNaoApagaOrfaMasAvisa(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 100}}
	m, st := novoManager(t, 5, &cams)
	var log bytes.Buffer
	m.log = slog.New(slog.NewTextHandler(&log, nil))
	grava(t, st, "cam_velha", diaAtras(5), 3, mb)
	for _, d := range []int{1, 0} {
		grava(t, st, "cam_a", diaAtras(d), 1, mb)
	}
	m.livre = func(string) (int64, error) { return mb, nil }

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	confereDias(t, st, "cam_a")
	confereDias(t, st, "cam_velha", dia(5))

	saida := log.String()
	if got := strings.Count(saida, "orfas_mb=3"); got != 3 {
		t.Errorf("orfas_mb=3 em %d avisos, esperava nos 3 (2 dias evictados e o fim):\n%s", got, saida)
	}
	if strings.Contains(saida, "não há mais nada a evictar") {
		t.Errorf("o aviso disse que não sobrou nada, com 3 MB de órfã:\n%s", saida)
	}
	if !strings.Contains(saida, "só restam gravações de câmeras removidas") {
		t.Errorf("faltou o aviso de que só restam órfãs:\n%s", saida)
	}
}

// Sem saber o espaço livre, a passada não apaga nada às cegas: devolve o erro.
func TestDiscoIlegivelDevolveOErro(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 100}}
	m, st := novoManager(t, 5, &cams)
	grava(t, st, "cam_a", diaAtras(0), 1, mb)
	falha := errors.New("statfs falhou")
	m.livre = func(string) (int64, error) { return 0, falha }

	if err := m.Enforce(); !errors.Is(err, falha) {
		t.Fatalf("Enforce devolveu %v, esperava o erro do disco", err)
	}
	confereDias(t, st, "cam_a", dia(0))
}

// Run faz uma passada logo na subida, antes do primeiro tique, e sai quando o
// contexto acaba.
func TestRunPassaNaSubida(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 1}}
	m, st := novoManager(t, 0, &cams)
	grava(t, st, "cam_a", diaAtras(0), 3, mb)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	m.Run(ctx)

	if got := st.Camera("cam_a").TotalBytes(); got != mb {
		t.Errorf("cam_a ocupa %d MB depois da subida, esperava a cota, 1", got/mb)
	}
}

// O espaço livre que a retenção lê é o do usuário comum, e nunca passa da
// capacidade.
func TestFreeBytesETotalBytes(t *testing.T) {
	dir := t.TempDir()
	free, err := FreeBytes(dir)
	if err != nil {
		t.Fatal(err)
	}
	total, err := TotalBytes(dir)
	if err != nil {
		t.Fatal(err)
	}
	if free <= 0 || total <= 0 || free > total {
		t.Errorf("livre %d, total %d", free, total)
	}
}

// O "desde" do disco abaixo do mínimo fica preso na primeira passada que viu o
// problema, e some na primeira que o vê resolvido.
func TestAbaixoDoMinimoDesde(t *testing.T) {
	m := &Manager{}
	t0 := time.Now()

	m.anotaDisco(false, t0)
	if !m.AbaixoDoMinimoDesde().IsZero() {
		t.Fatal("disco acima do mínimo não deveria ter desde")
	}

	m.anotaDisco(true, t0.Add(time.Minute))
	m.anotaDisco(true, t0.Add(2*time.Minute))
	if got := m.AbaixoDoMinimoDesde(); !got.Equal(t0.Add(time.Minute)) {
		t.Errorf("desde %v, esperava a primeira passada abaixo (%v)", got, t0.Add(time.Minute))
	}

	m.anotaDisco(false, t0.Add(3*time.Minute))
	if !m.AbaixoDoMinimoDesde().IsZero() {
		t.Error("o disco voltou e o desde ficou")
	}
}
