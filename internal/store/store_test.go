package store

import (
	"fmt"
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

func TestOldestMs(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()

	if got := c.OldestMs(); got != 0 {
		t.Errorf("câmera sem gravação devia dar 0, veio %d", got)
	}

	// Dias fora de ordem: o mapa de resumos não tem ordem nenhuma, e o mais
	// antigo tem que sair do menor FirstMs, não do primeiro que a iteração vir.
	for _, d := range []int{2, 0, 1} {
		e := entryAt(base.AddDate(0, 0, d), 60_000, 1_000_000)
		if err := c.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := c.OldestMs(), base.UnixMilli(); got != want {
		t.Errorf("OldestMs=%d, esperava %d", got, want)
	}

	// Um segmento anterior dentro do dia que já é o mais antigo: o mínimo é
	// por segmento, não por dia.
	antes := base.Add(-2 * time.Hour)
	if err := c.Append(entryAt(antes, 60_000, 1_000_000)); err != nil {
		t.Fatal(err)
	}
	if got, want := c.OldestMs(), antes.UnixMilli(); got != want {
		t.Errorf("OldestMs=%d após segmento mais cedo, esperava %d", got, want)
	}
}

// Resumo existe para poupar uma varredura do mapa por leitura do /api/health,
// então o que ele devolve tem que ser idêntico ao dos métodos que substitui -
// mais o extremo novo, o mais recente.
func TestResumoBateComOsMetodosSeparados(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()

	if bytes, oldest, newest := c.Resumo(); bytes != 0 || oldest != 0 || newest != 0 {
		t.Errorf("câmera sem gravação devia dar tudo 0, veio %d/%d/%d", bytes, oldest, newest)
	}

	// Dias fora de ordem, como o mapa de resumos entrega: os extremos têm que
	// sair da comparação, não da ordem da iteração.
	for _, d := range []int{2, 0, 1} {
		if err := c.Append(entryAt(base.AddDate(0, 0, d), 60_000, 1_000_000)); err != nil {
			t.Fatal(err)
		}
	}

	bytes, oldest, newest := c.Resumo()
	if want := c.TotalBytes(); bytes != want {
		t.Errorf("bytes=%d, TotalBytes diz %d", bytes, want)
	}
	if want := c.OldestMs(); oldest != want {
		t.Errorf("oldest=%d, OldestMs diz %d", oldest, want)
	}
	// O mais recente é o FIM do último segmento, não o começo: é o que faz o
	// span cobrir a gravação inteira.
	if want := base.AddDate(0, 0, 2).UnixMilli() + 60_000; newest != want {
		t.Errorf("newest=%d, esperava %d", newest, want)
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

// Purge é a única remoção que leva o init junto. A retenção nunca o toca, então
// sem isto sobraria um diretório init/ de uma câmera que não existe mais.
func TestPurgeApagaSegmentosIndicesEInits(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()

	hoje := entryAt(base, 60_000, 1_000_000)
	ontem := entryAt(base.AddDate(0, 0, -1), 60_000, 500_000)
	for _, e := range []Entry{hoje, ontem} {
		writeSegmentFile(t, c, e)
		if err := c.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.WriteInit("abc123", []byte("ftyp+moov")); err != nil {
		t.Fatal(err)
	}

	freed, err := c.Purge()
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if freed != 1_500_000 {
		t.Errorf("freed=%d, esperava 1500000", freed)
	}
	if _, err := os.Stat(c.Dir()); !os.IsNotExist(err) {
		t.Errorf("o diretório da câmera continua lá: err=%v", err)
	}
	if c.TotalBytes() != 0 || len(c.Days()) != 0 {
		t.Errorf("o resumo em memória sobreviveu: %d bytes, %d dias",
			c.TotalBytes(), len(c.Days()))
	}
}

// Purgar uma câmera que nunca gravou não é erro: é o que acontece ao remover uma
// câmera cadastrada minutos antes, com o disco ainda vazio.
func TestPurgeSemNadaGravadoNaoFalha(t *testing.T) {
	c := newTestCamera(t)
	freed, err := c.Purge()
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if freed != 0 {
		t.Errorf("freed=%d, esperava 0", freed)
	}
}

func TestOrphansIgnoraCamerasCadastradas(t *testing.T) {
	s := New(t.TempDir())
	base := baseTime()

	for _, id := range []string{"cam_a", "cam_b", "cam_viva"} {
		c := s.Camera(id)
		e := entryAt(base, 60_000, 1_000_000)
		writeSegmentFile(t, c, e)
		if err := c.Append(e); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.Orphans(map[string]bool{"cam_viva": true})
	if err != nil {
		t.Fatalf("Orphans: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("esperava 2 órfãs, veio %d: %+v", len(got), got)
	}
	// Vem ordenado, para que a lista não dance a cada recarga da tela.
	if got[0].ID != "cam_a" || got[1].ID != "cam_b" {
		t.Errorf("IDs = %s, %s", got[0].ID, got[1].ID)
	}
	if got[0].Bytes != 1_000_000 || got[0].Days != 1 {
		t.Errorf("cam_a = %d bytes, %d dias", got[0].Bytes, got[0].Days)
	}
	if got[0].FirstMs != base.UnixMilli() || got[0].LastMs != base.UnixMilli()+60_000 {
		t.Errorf("intervalo de cam_a = %d a %d", got[0].FirstMs, got[0].LastMs)
	}
}

// Diretório sem índice é sobra de evicção ou de cópia à mão. Não dá para dizer o
// tamanho, mas escondê-lo seria pior: é justamente o lixo que só sai do disco se
// alguém o enxergar.
func TestOrphansAceitaDiretorioSemIndice(t *testing.T) {
	root := t.TempDir()
	s := New(root)

	if err := os.MkdirAll(filepath.Join(root, "cam_lixo"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Arquivo solto na raiz não é câmera nenhuma e não pode virar uma linha na
	// tela.
	if err := os.WriteFile(filepath.Join(root, "anotacao.txt"), []byte("oi"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := s.Orphans(nil)
	if err != nil {
		t.Fatalf("Orphans: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("esperava só cam_lixo, veio %+v", got)
	}
	if got[0].ID != "cam_lixo" || got[0].Bytes != 0 || got[0].Days != 0 {
		t.Errorf("cam_lixo = %+v", got[0])
	}
}

// --- eventos de movimento ---------------------------------------------------

func appendEventos(t *testing.T, c *Camera, base time.Time, offsets ...time.Duration) {
	t.Helper()
	for _, o := range offsets {
		if err := c.AppendEvento(Evento{InstanteMs: base.Add(o).UnixMilli()}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEventosAppendELeitura(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	appendEventos(t, c, base, 0, time.Minute, 2*time.Minute)

	evs, err := c.LoadEventos(base.Format(DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Fatalf("esperava 3 marcas, veio %d", len(evs))
	}
	if evs[0].InstanteMs != base.UnixMilli() {
		t.Errorf("primeira marca em %d, esperado %d", evs[0].InstanteMs, base.UnixMilli())
	}

	// Dia sem arquivo é o caso comum - câmera sem detecção ligada - e não pode
	// ser erro.
	vazio, err := c.LoadEventos("2020-01-01")
	if err != nil || len(vazio) != 0 {
		t.Errorf("dia sem eventos: %d marcas, erro %v", len(vazio), err)
	}
}

// TestEnsureDirsNaoCriaEventos: quem só grava não ganha o diretório da
// detecção.
func TestEnsureDirsNaoCriaEventos(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	if err := c.EnsureDirs(base.Format(DayLayout)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.EventosDir()); !os.IsNotExist(err) {
		t.Fatalf("eventos/ criado sem nenhum evento: %v", err)
	}
	appendEventos(t, c, base, 0)
	if _, err := os.Stat(c.EventosPath(base.Format(DayLayout))); err != nil {
		t.Errorf("o primeiro evento não criou o arquivo: %v", err)
	}
}

func TestEventoRangeCortaNasPontas(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	appendEventos(t, c, base, 0, time.Minute, 2*time.Minute, 3*time.Minute)

	// [base+1min, base+3min): pega a de 1 e a de 2, e não a de 3.
	evs, err := c.EventoRange(base.Add(time.Minute).UnixMilli(), base.Add(3*time.Minute).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("esperava 2 marcas, veio %d: %v", len(evs), evs)
	}
	if evs[0].InstanteMs != base.Add(time.Minute).UnixMilli() {
		t.Errorf("corte errado na ponta de baixo: %v", evs)
	}
}

func TestEventoRangeAtravessaDias(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	// Uma marca hoje ao meio-dia e outra amanhã ao meio-dia.
	appendEventos(t, c, base, 0, 24*time.Hour)

	evs, err := c.EventoRange(base.UnixMilli(), base.Add(48*time.Hour).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("esperava 2 marcas em dois dias, veio %d", len(evs))
	}
}

func TestDropDayLevaOsEventosJunto(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	e := entryAt(base, 30_000, 1000)
	writeSegmentFile(t, c, e)
	if err := c.Append(e); err != nil {
		t.Fatal(err)
	}
	appendEventos(t, c, base, 0, time.Minute)

	if _, err := c.DropDay(base.Format(DayLayout)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.EventosPath(base.Format(DayLayout))); !os.IsNotExist(err) {
		t.Errorf("o arquivo de eventos sobreviveu ao DropDay: %v", err)
	}
}

// TestEvictOldestAparaAsMarcasSemGravacao guarda a regra que faz a faixa de
// calor ser clicável: marca sem vídeo por baixo é marca que não leva a lugar
// nenhum.
func TestEvictOldestAparaAsMarcasSemGravacao(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()

	// Três segmentos de 1 minuto e uma marca dentro de cada um.
	for i := range 3 {
		e := entryAt(base.Add(time.Duration(i)*time.Minute), 60_000, 1000)
		writeSegmentFile(t, c, e)
		if err := c.Append(e); err != nil {
			t.Fatal(err)
		}
		appendEventos(t, c, base, time.Duration(i)*time.Minute+time.Second)
	}

	// Libera o primeiro segmento. A marca dele tem que ir junto; as outras
	// duas ficam.
	if _, err := c.EvictOldest(1000); err != nil {
		t.Fatal(err)
	}
	evs, err := c.LoadEventos(base.Format(DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("esperava 2 marcas depois da evicção, veio %d: %v", len(evs), evs)
	}
	if evs[0].InstanteMs < base.Add(time.Minute).UnixMilli() {
		t.Errorf("sobrou marca do segmento apagado: %v", evs)
	}
}

func TestEventosIgnoraLinhaTruncada(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	appendEventos(t, c, base, 0, time.Minute)

	// Uma queda no meio de um append deixa exatamente isto.
	path := c.EventosPath(base.Format(DayLayout))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"instanteMs":178849`)
	f.Close()

	evs, err := c.LoadEventos(base.Format(DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Errorf("a linha truncada não foi descartada: %d marcas", len(evs))
	}
}

// Os quadros das detecções: um .jpg por detecção, nomeado pelo instante dela.
func TestQuadrosDoDia(t *testing.T) {
	c := newTestCamera(t)
	dia := baseTime().Format(DayLayout)
	jpeg := []byte("\xff\xd8um quadro\xff\xd9")
	instante := baseTime().UnixMilli()

	if err := c.WriteQuadro(dia, instante, jpeg); err != nil {
		t.Fatal(err)
	}

	caminho := c.QuadroPath(dia, instante)
	if filepath.Base(caminho) != fmt.Sprintf("%d.jpg", instante) {
		t.Errorf("caminho %q: o nome tem que ser o instante da detecção", caminho)
	}
	got, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(jpeg) {
		t.Errorf("quadro gravado %q, esperado %q", got, jpeg)
	}

	// As duas marcas do mesmo instante - uma por família - gravam o mesmo
	// arquivo, e não dois.
	if err := c.WriteQuadro(dia, instante, jpeg); err != nil {
		t.Fatal(err)
	}
	entradas, err := os.ReadDir(c.QuadrosDir(dia))
	if err != nil {
		t.Fatal(err)
	}
	if len(entradas) != 1 {
		t.Errorf("%d arquivos na pasta do dia, esperado 1", len(entradas))
	}
}

// Os quadros entram na cota da câmera e saem com o dia, como o vídeo.
func TestQuadrosContamNaCotaESaemComODia(t *testing.T) {
	c := newTestCamera(t)
	e := entryAt(baseTime(), 60_000, 1_000_000)
	if err := c.Append(e); err != nil {
		t.Fatal(err)
	}
	writeSegmentFile(t, c, e)

	jpeg := make([]byte, 14_000)
	for i := range 3 {
		if err := c.WriteQuadro(e.Day(), e.StartMs+int64(i)*1000, jpeg); err != nil {
			t.Fatal(err)
		}
	}

	if got, want := c.TotalBytes(), int64(1_000_000+3*14_000); got != want {
		t.Errorf("TotalBytes=%d, esperado %d: os quadros contam na cota", got, want)
	}

	// A conta de um dia é memorizada; um quadro novo depois disso tem que
	// entrar no total, senão a cota congela no primeiro valor lido.
	if err := c.WriteQuadro(e.Day(), e.StartMs+9000, jpeg); err != nil {
		t.Fatal(err)
	}
	if got, want := c.TotalBytes(), int64(1_000_000+4*14_000); got != want {
		t.Errorf("TotalBytes=%d, esperado %d: quadro gravado depois da conta", got, want)
	}

	freed, err := c.DropDay(e.Day())
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(1_000_000 + 4*14_000); freed != want {
		t.Errorf("liberou %d, esperado %d: os quadros saem com o dia", freed, want)
	}
	if _, err := os.Stat(c.QuadrosDir(e.Day())); !os.IsNotExist(err) {
		t.Errorf("a pasta de quadros do dia sobrou: %v", err)
	}
	if got := c.TotalBytes(); got != 0 {
		t.Errorf("TotalBytes=%d depois do DropDay, esperado 0", got)
	}
}
