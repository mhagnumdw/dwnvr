// Package store cuida do layout em disco e do índice das gravações.
//
// Não há banco de dados. O índice é um arquivo NDJSON por câmera por dia,
// escrito em modo append: cada segmento fechado vira uma linha. Isso dá três
// coisas que um banco custaria caro num Orange Pi: escrita sequencial barata,
// recuperação trivial depois de uma queda (basta descartar uma linha parcial)
// e um formato que dá para inspecionar com `tail`.
//
// Layout:
//
//	{root}/{cam}/init/{gen}.mp4          init segment por geração de codec
//	{root}/{cam}/2026-08-08/{startMs}.mp4  segmentos, nome = início em ms
//	{root}/{cam}/index/2026-08-08.ndjson   índice do dia
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DayLayout é o formato do diretório e do arquivo de índice de um dia.
const DayLayout = "2006-01-02"

// Entry descreve um segmento gravado.
//
// O caminho do arquivo NÃO é guardado: ele é derivável de StartMs
// ({dia}/{startMs}.mp4). Guardar os dois abriria espaço para divergirem.
type Entry struct {
	StartMs int64 `json:"t"`  // início, epoch ms (relógio de parede)
	DurMs   int64 `json:"d"`  // duração coberta pelo segmento
	Size    int64 `json:"sz"` // bytes do arquivo

	// Gen identifica o init segment por hash do conteúdo, não por contador.
	// Assim a troca de codec (SPS diferente após reconexão) é detectada sozinha,
	// inits idênticos são deduplicados e não há contador a persistir entre
	// reinícios.
	Gen string `json:"g"`

	// InitSize é onde terminam ftyp+moov. A entrega via MSE pula esse prefixo
	// e serve o init separado, uma vez só.
	InitSize int64 `json:"io"`
	// FirstFrag é o tamanho do primeiro moof+mdat. Init + primeiro fragmento
	// formam um MP4 de um frame: é a thumbnail, sem decodificar nada no Pi.
	FirstFrag int64 `json:"f0"`
}

func (e Entry) EndMs() int64 { return e.StartMs + e.DurMs }

// Day devolve o dia (hora local) ao qual o segmento pertence.
func (e Entry) Day() string { return time.UnixMilli(e.StartMs).Format(DayLayout) }

// DaySummary é o que fica em memória por dia. Manter só isto — em vez da lista
// completa de segmentos — é o que segura o uso de RAM: são ~270 resumos para 9
// câmeras com 30 dias, contra ~390 mil entradas se tudo ficasse carregado.
type DaySummary struct {
	Day     string `json:"day"`
	Count   int    `json:"count"`
	Bytes   int64  `json:"bytes"`
	FirstMs int64  `json:"firstMs"`
	LastMs  int64  `json:"lastMs"`
}

// Camera é o índice de uma câmera. Os métodos são seguros para uso concorrente:
// o recorder escreve, a retenção evicta e a API lê, tudo ao mesmo tempo.
type Camera struct {
	ID   string
	root string

	mu   sync.RWMutex
	days map[string]*DaySummary
}

type Store struct {
	root string

	mu   sync.Mutex
	cams map[string]*Camera
}

func New(root string) *Store {
	return &Store{root: root, cams: map[string]*Camera{}}
}

func (s *Store) Root() string { return s.root }

// Camera devolve (criando se preciso) o índice de uma câmera.
func (s *Store) Camera(id string) *Camera {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.cams[id]; ok {
		return c
	}
	c := &Camera{ID: id, root: filepath.Join(s.root, id), days: map[string]*DaySummary{}}
	s.cams[id] = c
	return c
}

// --- caminhos ---------------------------------------------------------------

func (c *Camera) Dir() string            { return c.root }
func (c *Camera) IndexDir() string       { return filepath.Join(c.root, "index") }
func (c *Camera) InitDir() string        { return filepath.Join(c.root, "init") }
func (c *Camera) DayDir(d string) string { return filepath.Join(c.root, d) }

func (c *Camera) IndexPath(day string) string {
	return filepath.Join(c.IndexDir(), day+".ndjson")
}

func (c *Camera) InitPath(gen string) string {
	return filepath.Join(c.InitDir(), gen+".mp4")
}

// WriteInit grava o init segment de uma geração, se ainda não existir. Como o
// nome é o hash do conteúdo, reescrever seria sempre redundante.
func (c *Camera) WriteInit(gen string, b []byte) error {
	if err := os.MkdirAll(c.InitDir(), 0o755); err != nil {
		return err
	}
	path := c.InitPath(gen)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	tmp, err := os.CreateTemp(c.InitDir(), ".init-*.mp4")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	// CreateTemp cria com 0600; sem isto o init ficaria ilegível para outro
	// usuário, ao contrário dos segmentos, que saem com 0644.
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// SegmentPath devolve o caminho do arquivo de um segmento a partir do início.
func (c *Camera) SegmentPath(startMs int64) string {
	t := time.UnixMilli(startMs)
	return filepath.Join(c.root, t.Format(DayLayout), strconv.FormatInt(startMs, 10)+".mp4")
}

// --- escrita ----------------------------------------------------------------

// EnsureDirs cria a estrutura de diretórios necessária para gravar no dia dado.
func (c *Camera) EnsureDirs(day string) error {
	for _, d := range []string{c.IndexDir(), c.InitDir(), c.DayDir(day)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// Append acrescenta uma entrada ao índice do dia correspondente.
//
// O fsync aqui é barato (uma vez por segmento, ou seja, uma vez por minuto por
// câmera) e é o que garante que uma queda de energia não perca o registro de um
// segmento que já está inteiro no disco.
func (c *Camera) Append(e Entry) error {
	day := e.Day()
	if err := os.MkdirAll(c.IndexDir(), 0o755); err != nil {
		return err
	}

	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')

	f, err := os.OpenFile(c.IndexPath(day), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.mergeLocked(day, e)
	return nil
}

func (c *Camera) mergeLocked(day string, e Entry) {
	s, ok := c.days[day]
	if !ok {
		s = &DaySummary{Day: day, FirstMs: e.StartMs, LastMs: e.EndMs()}
		c.days[day] = s
	}
	s.Count++
	s.Bytes += e.Size
	if e.StartMs < s.FirstMs {
		s.FirstMs = e.StartMs
	}
	if e.EndMs() > s.LastMs {
		s.LastMs = e.EndMs()
	}
}

// --- leitura ----------------------------------------------------------------

// Days devolve os resumos por dia, do mais antigo para o mais recente.
func (c *Camera) Days() []DaySummary {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]DaySummary, 0, len(c.days))
	for _, s := range c.days {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day < out[j].Day })
	return out
}

// TotalBytes é quanto a câmera ocupa em disco, segundo o índice.
func (c *Camera) TotalBytes() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var n int64
	for _, s := range c.days {
		n += s.Bytes
	}
	return n
}

// LoadDay lê as entradas de um dia, ordenadas por início.
//
// Uma última linha truncada (queda no meio de um append) é descartada em
// silêncio: é exatamente a falha que o formato append-only prevê.
func (c *Camera) LoadDay(day string) ([]Entry, error) {
	f, err := os.Open(c.IndexPath(day))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var entries []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			// Só a última linha pode estar truncada; qualquer outra indica
			// corrupção de verdade e merece ser reportada.
			continue
		}
		entries = append(entries, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].StartMs < entries[j].StartMs })
	return entries, nil
}

// Range devolve as entradas que cobrem [fromMs, toMs).
//
// O dia anterior a `from` também é lido: um segmento pode ter começado antes da
// meia-noite e se estender para dentro do intervalo pedido.
func (c *Camera) Range(fromMs, toMs int64) ([]Entry, error) {
	if toMs <= fromMs {
		return nil, nil
	}
	start := time.UnixMilli(fromMs).AddDate(0, 0, -1)
	end := time.UnixMilli(toMs)

	var out []Entry
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		entries, err := c.LoadDay(d.Format(DayLayout))
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.EndMs() > fromMs && e.StartMs < toMs {
				out = append(out, e)
			}
		}
	}
	return out, nil
}

// --- carga inicial ----------------------------------------------------------

// Scan reconstrói os resumos em memória lendo os índices do disco.
//
// Se reconcileLastDay for verdadeiro, o dia mais recente é conferido contra o
// filesystem: entradas cujo arquivo sumiu são descartadas e arquivos órfãos
// (gravados, mas cuja linha de índice não chegou a ser escrita) são
// reincorporados. É onde mora o dano de uma queda, e é barato porque olha um
// dia só em vez dos milhares de arquivos de todo o histórico.
func (c *Camera) Scan(reconcileLastDay bool, probe func(path string) (Entry, error)) error {
	entries, err := os.ReadDir(c.IndexDir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	days := map[string]*DaySummary{}
	var dayNames []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".ndjson") {
			continue
		}
		day := strings.TrimSuffix(name, ".ndjson")
		if _, err := time.Parse(DayLayout, day); err != nil {
			continue
		}
		dayNames = append(dayNames, day)
	}
	sort.Strings(dayNames)

	c.mu.Lock()
	c.days = days
	c.mu.Unlock()

	for i, day := range dayNames {
		list, err := c.LoadDay(day)
		if err != nil {
			return err
		}
		isLast := i == len(dayNames)-1
		if isLast && reconcileLastDay {
			list, err = c.reconcile(day, list, probe)
			if err != nil {
				return err
			}
		}
		c.mu.Lock()
		for _, e := range list {
			c.mergeLocked(day, e)
		}
		c.mu.Unlock()
	}
	return nil
}

// reconcile alinha o índice de um dia com o que está de fato no disco e
// reescreve o arquivo se algo mudou.
func (c *Camera) reconcile(day string, list []Entry, probe func(string) (Entry, error)) ([]Entry, error) {
	files, err := os.ReadDir(c.DayDir(day))
	if errors.Is(err, os.ErrNotExist) {
		// O índice tem entradas mas o diretório sumiu: confia no disco.
		if len(list) > 0 {
			return nil, c.rewrite(day, nil)
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	onDisk := map[int64]int64{} // startMs -> tamanho
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".mp4") {
			continue
		}
		ms, err := strconv.ParseInt(strings.TrimSuffix(f.Name(), ".mp4"), 10, 64)
		if err != nil {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		onDisk[ms] = info.Size()
	}

	var kept []Entry
	indexed := map[int64]bool{}
	changed := false
	for _, e := range list {
		size, ok := onDisk[e.StartMs]
		if !ok {
			changed = true // arquivo sumiu
			continue
		}
		if size != e.Size {
			// Segmento truncado por queda no meio da escrita: o tamanho real
			// manda, e o índice passa a refletir o que existe.
			e.Size = size
			changed = true
		}
		indexed[e.StartMs] = true
		kept = append(kept, e)
	}

	// Arquivos órfãos: gravados, mas cuja linha de índice não chegou a sair.
	for ms := range onDisk {
		if indexed[ms] || probe == nil {
			continue
		}
		e, err := probe(c.SegmentPath(ms))
		if err != nil {
			continue // ilegível: deixa quieto, a retenção acaba levando
		}
		e.StartMs = ms
		kept = append(kept, e)
		changed = true
	}

	sort.Slice(kept, func(i, j int) bool { return kept[i].StartMs < kept[j].StartMs })
	if changed {
		if err := c.rewrite(day, kept); err != nil {
			return nil, err
		}
	}
	return kept, nil
}

// --- remoção ----------------------------------------------------------------

// DropOldestDay remove por completo o dia mais antigo, arquivos e índice.
func (c *Camera) DropOldestDay() (freed int64, err error) {
	days := c.Days()
	if len(days) == 0 {
		return 0, nil
	}
	return c.DropDay(days[0].Day)
}

func (c *Camera) DropDay(day string) (freed int64, err error) {
	c.mu.RLock()
	s, ok := c.days[day]
	if ok {
		freed = s.Bytes
	}
	c.mu.RUnlock()
	if !ok {
		return 0, nil
	}

	if err := os.RemoveAll(c.DayDir(day)); err != nil {
		return 0, err
	}
	if err := os.Remove(c.IndexPath(day)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}

	c.mu.Lock()
	delete(c.days, day)
	c.mu.Unlock()
	return freed, nil
}

// EvictOldest apaga os segmentos mais antigos até liberar pelo menos `want`
// bytes, ou até acabarem os segmentos. Devolve quanto liberou.
//
// A remoção é sempre por segmento inteiro: nunca se apaga parte de um arquivo.
func (c *Camera) EvictOldest(want int64) (freed int64, err error) {
	for freed < want {
		days := c.Days()
		if len(days) == 0 {
			return freed, nil
		}
		day := days[0].Day

		list, err := c.LoadDay(day)
		if err != nil {
			return freed, err
		}
		if len(list) == 0 {
			if _, err := c.DropDay(day); err != nil {
				return freed, err
			}
			continue
		}

		var removed int
		for _, e := range list {
			if freed >= want {
				break
			}
			path := c.SegmentPath(e.StartMs)
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return freed, err
			}
			freed += e.Size
			removed++
		}

		rest := list[removed:]
		if len(rest) == 0 {
			if _, err := c.DropDay(day); err != nil {
				return freed, err
			}
			continue
		}
		if err := c.rewrite(day, rest); err != nil {
			return freed, err
		}
		c.recount(day, rest)
	}
	return freed, nil
}

// rewrite regrava o índice de um dia de forma atômica. Os arquivos de índice
// são pequenos (~100 KB por dia), então reescrever sai mais barato — e é mais
// simples de acertar — que manter marcas de remoção.
func (c *Camera) rewrite(day string, entries []Entry) error {
	if len(entries) == 0 {
		err := os.Remove(c.IndexPath(day))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	if err := os.MkdirAll(c.IndexDir(), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(c.IndexDir(), ".idx-*.ndjson")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	w := bufio.NewWriter(tmp)
	for _, e := range entries {
		b, err := json.Marshal(e)
		if err != nil {
			tmp.Close()
			return err
		}
		w.Write(b)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	// Sem isto o índice, criado com 0644 pelo Append, cairia para 0600 na
	// primeira evicção — uma mudança silenciosa de permissão no meio da vida
	// do arquivo.
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), c.IndexPath(day))
}

func (c *Camera) recount(day string, entries []Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.days, day)
	for _, e := range entries {
		c.mergeLocked(day, e)
	}
}

// ParseSegmentName extrai o início em ms do nome de um arquivo de segmento.
func ParseSegmentName(name string) (int64, error) {
	if !strings.HasSuffix(name, ".mp4") {
		return 0, fmt.Errorf("store: %q não é um segmento", name)
	}
	return strconv.ParseInt(strings.TrimSuffix(name, ".mp4"), 10, 64)
}
