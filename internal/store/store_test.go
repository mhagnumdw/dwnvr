package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// baseTime é um instante fixo às 12h locais, longe da meia-noite, para que os
// testes não dependam do fuso de quem os roda.
func baseTime() time.Time {
	return time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
}

func entryAt(t time.Time, durMs, size int64) Entry {
	return Entry{
		StartMs: t.UnixMilli(), DurMs: durMs, Size: size,
		Gen: "abc123", InitSize: 737, FirstFrag: 50000,
	}
}

// writeSegmentFile cria o arquivo correspondente a uma entrada, para os testes
// que precisam do disco batendo com o índice.
func writeSegmentFile(t *testing.T, c *Camera, e Entry) {
	t.Helper()
	if err := c.EnsureDirs(e.Day()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.SegmentPath(e.StartMs), make([]byte, e.Size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newTestCamera(t *testing.T) *Camera {
	t.Helper()
	return New(t.TempDir()).Camera("cam_teste")
}

func TestAppendELeitura(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()

	for i := range 3 {
		e := entryAt(base.Add(time.Duration(i)*time.Minute), 60_000, 1_000_000)
		if err := c.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	days := c.Days()
	if len(days) != 1 {
		t.Fatalf("esperava 1 dia, veio %d", len(days))
	}
	if days[0].Count != 3 {
		t.Errorf("Count=%d, esperava 3", days[0].Count)
	}
	if days[0].Bytes != 3_000_000 {
		t.Errorf("Bytes=%d", days[0].Bytes)
	}
	if got := c.TotalBytes(); got != 3_000_000 {
		t.Errorf("TotalBytes=%d", got)
	}

	entries, err := c.LoadDay(base.Format(DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("LoadDay devolveu %d entradas", len(entries))
	}
	if entries[0].InitSize != 737 || entries[0].Gen != "abc123" {
		t.Errorf("campos não sobreviveram ao round-trip: %+v", entries[0])
	}
}

// Os resumos precisam sobreviver ao reinício sem nenhum arquivo de cache: eles
// são reconstruídos lendo os índices do disco.
func TestScanReconstroiResumos(t *testing.T) {
	root := t.TempDir()
	c := New(root).Camera("cam_teste")
	base := baseTime()

	for i := range 5 {
		if err := c.Append(entryAt(base.Add(time.Duration(i)*time.Minute), 60_000, 2_000_000)); err != nil {
			t.Fatal(err)
		}
	}

	// Simula um reinício: instância nova, mesmo diretório.
	c2 := New(root).Camera("cam_teste")
	if err := c2.Scan(false, nil); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got := c2.TotalBytes(); got != 10_000_000 {
		t.Errorf("após Scan TotalBytes=%d, esperava 10000000", got)
	}
	if days := c2.Days(); len(days) != 1 || days[0].Count != 5 {
		t.Errorf("resumo errado após Scan: %+v", days)
	}
}

func TestEvictOldest(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()

	var entries []Entry
	for i := range 5 {
		e := entryAt(base.Add(time.Duration(i)*time.Minute), 60_000, 1_000_000)
		entries = append(entries, e)
		writeSegmentFile(t, c, e)
		if err := c.Append(e); err != nil {
			t.Fatal(err)
		}
	}

	// Pede 2,5 MB: evicta 3 segmentos inteiros, porque remoção é sempre por
	// segmento completo.
	freed, err := c.EvictOldest(2_500_000)
	if err != nil {
		t.Fatalf("EvictOldest: %v", err)
	}
	if freed != 3_000_000 {
		t.Errorf("liberou %d, esperava 3000000", freed)
	}
	if got := c.TotalBytes(); got != 2_000_000 {
		t.Errorf("TotalBytes=%d após evicção, esperava 2000000", got)
	}

	// Os arquivos mais antigos têm que ter sumido de verdade, e os novos ficado.
	for i, e := range entries {
		_, err := os.Stat(c.SegmentPath(e.StartMs))
		if i < 3 && err == nil {
			t.Errorf("segmento %d deveria ter sido apagado", i)
		}
		if i >= 3 && err != nil {
			t.Errorf("segmento %d não deveria ter sido apagado: %v", i, err)
		}
	}

	// O índice em disco tem que refletir a remoção, não só a memória.
	rest, err := c.LoadDay(base.Format(DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 2 {
		t.Errorf("índice em disco tem %d entradas, esperava 2", len(rest))
	}
}

func TestEvictOldestAtravessaDias(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()

	for d := range 3 {
		for i := range 2 {
			e := entryAt(base.AddDate(0, 0, d).Add(time.Duration(i)*time.Minute), 60_000, 1_000_000)
			writeSegmentFile(t, c, e)
			if err := c.Append(e); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(c.Days()) != 3 {
		t.Fatalf("esperava 3 dias, veio %d", len(c.Days()))
	}

	// Libera mais que um dia inteiro: o dia esvaziado tem que desaparecer.
	if _, err := c.EvictOldest(3_000_000); err != nil {
		t.Fatal(err)
	}
	days := c.Days()
	if len(days) != 2 {
		t.Fatalf("esperava 2 dias após evicção, veio %d: %+v", len(days), days)
	}
	if days[0].Day != base.AddDate(0, 0, 1).Format(DayLayout) {
		t.Errorf("dia mais antigo agora é %s", days[0].Day)
	}
	// O diretório do dia removido não pode ficar para trás.
	if _, err := os.Stat(c.DayDir(base.Format(DayLayout))); !os.IsNotExist(err) {
		t.Error("o diretório do dia evictado ainda existe")
	}
}

// Um segmento pode começar antes da meia-noite e se estender para dentro do
// intervalo pedido. Se Range só olhasse os dias do intervalo, esse segmento
// sumiria da linha do tempo logo depois da virada do dia.
func TestRangeIncluiSegmentoQueAtravessaMeiaNoite(t *testing.T) {
	c := newTestCamera(t)
	dia := time.Date(2026, 8, 8, 0, 0, 0, 0, time.Local)
	antes := dia.Add(-30 * time.Second) // 23:59:30 do dia anterior

	// Segmento que começa às 23:59:30 e dura 60s, entrando no dia seguinte.
	if err := c.Append(entryAt(antes, 60_000, 500_000)); err != nil {
		t.Fatal(err)
	}
	// Segmento normal, já dentro do dia.
	if err := c.Append(entryAt(dia.Add(time.Minute), 60_000, 500_000)); err != nil {
		t.Fatal(err)
	}

	got, err := c.Range(dia.UnixMilli(), dia.Add(2*time.Hour).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Range devolveu %d entradas, esperava 2 (a que atravessa a meia-noite foi perdida?)", len(got))
	}
	if got[0].StartMs != antes.UnixMilli() {
		t.Error("a primeira entrada deveria ser a que começou no dia anterior")
	}
}

func TestRangeExcluiForaDoIntervalo(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	for i := range 10 {
		if err := c.Append(entryAt(base.Add(time.Duration(i)*time.Minute), 60_000, 100)); err != nil {
			t.Fatal(err)
		}
	}
	got, err := c.Range(base.Add(3*time.Minute).UnixMilli(), base.Add(6*time.Minute).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("Range devolveu %d, esperava 3: %+v", len(got), got)
	}
}

// Reconciliação: é o que salva o índice depois de um kill -9.
func TestScanReconciliaArquivoOrfao(t *testing.T) {
	root := t.TempDir()
	c := New(root).Camera("cam_teste")
	base := baseTime()

	e1 := entryAt(base, 60_000, 1_000)
	writeSegmentFile(t, c, e1)
	if err := c.Append(e1); err != nil {
		t.Fatal(err)
	}

	// Órfão: o arquivo foi gravado, mas o processo morreu antes de indexar.
	orfao := entryAt(base.Add(time.Minute), 60_000, 2_000)
	writeSegmentFile(t, c, orfao)

	c2 := New(root).Camera("cam_teste")
	probe := func(path string) (Entry, error) {
		fi, err := os.Stat(path)
		if err != nil {
			return Entry{}, err
		}
		return Entry{DurMs: 60_000, Size: fi.Size(), Gen: "recuperado"}, nil
	}
	if err := c2.Scan(true, probe); err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if got := c2.TotalBytes(); got != 3_000 {
		t.Errorf("TotalBytes=%d, esperava 3000 (o órfão foi reincorporado?)", got)
	}
	entries, err := c2.LoadDay(base.Format(DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("índice tem %d entradas, esperava 2", len(entries))
	}
	if entries[1].StartMs != orfao.StartMs {
		t.Errorf("o órfão entrou com StartMs=%d, esperava %d", entries[1].StartMs, orfao.StartMs)
	}
}

func TestScanReconciliaEntradaSemArquivo(t *testing.T) {
	root := t.TempDir()
	c := New(root).Camera("cam_teste")
	base := baseTime()

	e1 := entryAt(base, 60_000, 1_000)
	writeSegmentFile(t, c, e1)
	if err := c.Append(e1); err != nil {
		t.Fatal(err)
	}
	// Indexado, mas o arquivo não existe (apagado por fora).
	if err := c.Append(entryAt(base.Add(time.Minute), 60_000, 9_000)); err != nil {
		t.Fatal(err)
	}

	c2 := New(root).Camera("cam_teste")
	if err := c2.Scan(true, nil); err != nil {
		t.Fatal(err)
	}
	if got := c2.TotalBytes(); got != 1_000 {
		t.Errorf("TotalBytes=%d, esperava 1000 (a entrada fantasma foi descartada?)", got)
	}
}

// Uma queda no meio da escrita deixa o segmento menor do que o índice diz. O
// tamanho real tem que prevalecer, senão a contabilidade da cota fica errada
// para sempre.
func TestScanCorrigeTamanhoDivergente(t *testing.T) {
	root := t.TempDir()
	c := New(root).Camera("cam_teste")
	base := baseTime()

	e := entryAt(base, 60_000, 5_000)
	writeSegmentFile(t, c, e)
	if err := c.Append(e); err != nil {
		t.Fatal(err)
	}
	// Trunca o arquivo, como faria uma queda de energia.
	if err := os.Truncate(c.SegmentPath(e.StartMs), 1_234); err != nil {
		t.Fatal(err)
	}

	c2 := New(root).Camera("cam_teste")
	if err := c2.Scan(true, nil); err != nil {
		t.Fatal(err)
	}
	if got := c2.TotalBytes(); got != 1_234 {
		t.Errorf("TotalBytes=%d, esperava 1234", got)
	}
}

// Uma linha truncada no fim do índice é a falha esperada do formato
// append-only e não pode derrubar a leitura do dia inteiro.
func TestLoadDayIgnoraLinhaTruncada(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	if err := c.Append(entryAt(base, 60_000, 1_000)); err != nil {
		t.Fatal(err)
	}

	path := c.IndexPath(base.Format(DayLayout))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"t":123,"d":60000,"sz":`) // JSON pela metade
	f.Close()

	entries, err := c.LoadDay(base.Format(DayLayout))
	if err != nil {
		t.Fatalf("LoadDay falhou por causa de uma linha truncada: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("esperava 1 entrada válida, veio %d", len(entries))
	}
}

func TestWriteInitEhIdempotente(t *testing.T) {
	c := newTestCamera(t)
	if err := c.WriteInit("deadbeef", []byte("init-v1")); err != nil {
		t.Fatal(err)
	}
	// Um init com o mesmo hash nunca deve ser reescrito.
	if err := c.WriteInit("deadbeef", []byte("outro-conteudo")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(c.InitPath("deadbeef"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "init-v1" {
		t.Errorf("init foi sobrescrito: %q", got)
	}
}

// O índice nasce 0644 no Append e é reescrito via arquivo temporário na
// evicção. Sem um Chmod explícito ele cairia para 0600 no meio da vida,
// mudando a permissão em silêncio.
func TestPermissoesConsistentesAposEviccao(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()

	for i := range 3 {
		e := entryAt(base.Add(time.Duration(i)*time.Minute), 60_000, 1_000)
		writeSegmentFile(t, c, e)
		if err := c.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.WriteInit("cafe01", []byte("init")); err != nil {
		t.Fatal(err)
	}

	idxPath := c.IndexPath(base.Format(DayLayout))
	antes, err := os.Stat(idxPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.EvictOldest(1_500); err != nil {
		t.Fatal(err)
	}
	depois, err := os.Stat(idxPath)
	if err != nil {
		t.Fatal(err)
	}
	if antes.Mode() != depois.Mode() {
		t.Errorf("permissão do índice mudou de %v para %v após a evicção", antes.Mode(), depois.Mode())
	}

	init, err := os.Stat(c.InitPath("cafe01"))
	if err != nil {
		t.Fatal(err)
	}
	if init.Mode().Perm() != 0o644 {
		t.Errorf("init gravado com permissão %v, esperava 0644", init.Mode().Perm())
	}
}

func TestSegmentPath(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	got := c.SegmentPath(base.UnixMilli())
	want := filepath.Join(c.Dir(), base.Format(DayLayout), "1786363200000.mp4")
	// O nome exato depende do fuso; confere só a estrutura.
	if filepath.Dir(got) != filepath.Dir(want) {
		t.Errorf("diretório do segmento = %s, esperava %s", filepath.Dir(got), filepath.Dir(want))
	}
	ms, err := ParseSegmentName(filepath.Base(got))
	if err != nil {
		t.Fatal(err)
	}
	if ms != base.UnixMilli() {
		t.Errorf("ida e volta do nome falhou: %d != %d", ms, base.UnixMilli())
	}
}
