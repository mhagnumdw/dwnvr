// Package store handles the on-disk layout and the index of the recordings.
//
// There's no database. The index is one NDJSON file per camera per day,
// written in append mode: each closed segment becomes a line. That gives three
// things a database would cost dearly on modest hardware: cheap sequential
// writes, trivial recovery after a crash (just drop a partial line) and a
// format you can inspect with `tail`.
//
// Layout:
//
//	{root}/{cam}/init/{gen}.mp4             init segment per codec generation
//	{root}/{cam}/2026-08-08/{startMs}.mp4   segments, name = start in ms
//	{root}/{cam}/index/2026-08-08.ndjson    the day's index
//	{root}/{cam}/eventos/2026-08-08.ndjson  the day's motion onsets
package store

import (
	"bufio"
	"bytes"
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

	"github.com/mhagnumdw/dwnvr/internal/fmp4"
)

// DayLayout is the format of a day's directory and index file.
const DayLayout = "2006-01-02"

// Entry describes a recorded segment.
//
// The file path is NOT stored: it can be derived from StartMs
// ({day}/{startMs}.mp4). Storing both would leave room for them to diverge.
type Entry struct {
	StartMs int64 `json:"t"`  // start, epoch ms (wall clock)
	DurMs   int64 `json:"d"`  // duration the segment covers
	Size    int64 `json:"sz"` // file bytes

	// Gen identifies the init segment by content hash, not by counter. That
	// way a codec change (a different SPS after reconnecting) is detected on its
	// own, identical inits are deduplicated and there's no counter to persist
	// across restarts.
	Gen string `json:"g"`

	// InitSize is where ftyp+moov end. MSE delivery skips that prefix and
	// serves the init separately, only once.
	InitSize int64 `json:"io"`
	// FirstFrag is the size of the first moof+mdat. Init + first fragment make
	// a one-frame MP4: that's the thumbnail, without decoding anything on the
	// server.
	FirstFrag int64 `json:"f0"`
}

func (e Entry) EndMs() int64 { return e.StartMs + e.DurMs }

// Day returns the day (local time) the segment belongs to.
func (e Entry) Day() string { return time.UnixMilli(e.StartMs).Format(DayLayout) }

// Event is an ONSET: the instant a camera's image changed enough to be
// worth a more expensive inference. It's what the timeline heat strip draws,
// and what wakes the object detector.
//
// The key is spelled out, unlike Entry's above. There the abbreviation pays
// off: hundreds of thousands of segments in a 30-day history. Here it's on the
// order of a thousand lines per camera per day at the default level, and
// saving eight bytes on a line someone may need to read with `tail` isn't
// worth the trade.
//
// The same line serves the OBJECT MARK the object detector returns later, with
// `familia` filled in: {"instanteMs":...,"familia":"pessoa",
// "classe":"person","score":0.87,"quadroMs":...,"caixa":[0.51,0.26,0.67,0.98]}.
// The instant is that of the ONSET that originated it - that's what matches
// the arrival -, even if the answer came seconds later. Living in the same
// file gets the same retention for free: the mark goes away together with the
// video it points to.
type Event struct {
	AtMs int64 `json:"instanteMs"`

	// An empty Family is a motion onset; filled in, it's an object mark.
	Family string  `json:"familia,omitempty"`
	Class  string  `json:"classe,omitempty"`
	Score  float64 `json:"score,omitempty"`

	// FrameMs is the instant of the frame the object detector inferred, which
	// may come up to PeakWindowMs after the onset. It's on that frame, not on the
	// onset, that the Box is in the right place: drawn at the mark's instant, it
	// would arrive up to 3 s before the object.
	FrameMs int64 `json:"quadroMs,omitempty"`
	// Box is X1, Y1, X2, Y2 as a fraction of the frame, from 0 to 1. A
	// pointer, not an array, because of omitempty: an array is never "empty" to
	// JSON, and every onset line of the day would come out with
	// "caixa":[0,0,0,0].
	Box *[4]float64 `json:"caixa,omitempty"`

	// HasFrame says the JPEG of the inferred frame was written, in
	// quadros/{day}/{atMs}.jpg. False is a mark without an image: older than this
	// version, or an inference where the frame didn't come back from the object
	// detector.
	//
	// The path isn't stored because it can be derived from the instant, just as
	// the segment's isn't stored in the index. Two marks of different families at
	// the same instant point to the SAME file: there's only one frame.
	HasFrame bool `json:"temQuadro,omitempty"`

	// Alert says this object passed the camera's alert rule, at the time it was
	// inferred. AlertAreas are the ids of the alert areas it fell in, not the
	// names: renaming the area still holds, and a deleted one the screen shows as
	// "área apagada". Empty when the alert applies to the whole image. A line from
	// before alerts has neither.
	Alert      bool     `json:"alert,omitempty"`
	AlertAreas []string `json:"alertAreas,omitempty"`
}

// IsObject says whether the line is an object mark, not a motion onset.
func (e Event) IsObject() bool { return e.Family != "" }

// Day returns the day (local time) the event belongs to.
func (e Event) Day() string { return time.UnixMilli(e.AtMs).Format(DayLayout) }

// DaySummary is what's kept in memory per day. Keeping only this - instead
// of the full list of segments - is what holds down RAM use: ~270 summaries
// for 9 cameras with 30 days, against ~390 thousand entries if everything
// stayed loaded.
type DaySummary struct {
	Day     string `json:"day"`
	Count   int    `json:"count"`
	Bytes   int64  `json:"bytes"`
	FirstMs int64  `json:"firstMs"`
	LastMs  int64  `json:"lastMs"`

	// DurMs is the sum of the day's segment durations: the video that actually
	// exists. LastMs - FirstMs doesn't give this, because it also covers the time
	// the camera was offline.
	DurMs int64 `json:"durMs"`
}

// Camera is a camera's index. Its methods are safe for concurrent use: the
// recorder writes, retention evicts and the API reads, all at the same time.
type Camera struct {
	ID   string
	root string

	mu   sync.RWMutex
	days map[string]*DaySummary

	// framesBytes is what each day's frames take, memoized. See
	// FramesBytes.
	framesBytes map[string]int64

	// memo are the object marks of each day already read. See DayObjects.
	memo objectsMemo
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

// newCamera is the only place where a camera ID becomes a path on disk.
//
// The caller has already checked the ID: registration and reading
// cameras.json go through config.ValidateCameraID, the API only accepts a
// registered camera and orphans come from a ReadDir. This is the last lock,
// and it lives in store because only it knows {root}/{cam} must be a directory
// right under storage: with "", "." or "..", the Join would give the storage
// itself or the directory above it, and a Purge would delete everything
// there. Getting here with one of those is a bug in the caller, not user
// input - hence the panic.
//
// IsLocal alone isn't enough, because it accepts "." and "a/..", which give
// the storage itself. But it stays: it's the check CodeQL recognizes, and
// without it go/path-injection flags every disk access in store again.
func newCamera(root, id string) *Camera {
	if filepath.IsLocal(id) && filepath.Base(id) == id && id != "." {
		return &Camera{ID: id, root: filepath.Join(root, id), days: map[string]*DaySummary{},
			framesBytes: map[string]int64{}}
	}
	panic(fmt.Sprintf("store: camera id %q is not a directory name", id))
}

// Camera returns (creating it if needed) a camera's index.
func (s *Store) Camera(id string) *Camera {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.cams[id]; ok {
		return c
	}
	c := newCamera(s.root, id)
	s.cams[id] = c
	return c
}

// Forget removes the camera from the in-memory map.
//
// Without this, every ID ever looked up would stay in the Store forever -
// including that of a camera that was just removed and had its recordings
// deleted, which would keep answering as an empty index.
func (s *Store) Forget(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cams, id)
}

// Load returns a camera's index, reading it from disk if it isn't in memory
// yet.
//
// It's the entry point of every registered camera: boot goes through here,
// and so does registering a new camera. Without the latter, re-registering
// with the same ID a camera removed before a restart started with an empty
// index, and its recordings stayed out of Recordings, orphans and the quota
// until the next restart - which brought them all back at once.
//
// A camera already in memory comes back as it is: either it was read here, or
// it was removed without deleting in the same process, and its index still
// holds.
//
// Scan checks the last day and deletes zero-byte segments, which is only safe
// with the camera's recorder stopped. The caller guarantees that: at boot it
// hasn't started yet, and on registration Load comes before Manager.Set.
func (s *Store) Load(id string) (*Camera, error) {
	s.mu.Lock()
	c, ok := s.cams[id]
	s.mu.Unlock()
	if ok {
		return c, nil
	}

	c = newCamera(s.root, id)
	if err := c.Scan(true, ProbeSegment); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Someone may have put the camera in the map during the Scan; theirs stays.
	if existing, ok := s.cams[id]; ok {
		return existing, nil
	}
	s.cams[id] = c
	return c, nil
}

// ProbeSegment rebuilds the index entry of an orphan segment - fully
// written, but whose index line didn't get written before a crash.
func ProbeSegment(path string) (Entry, error) {
	info, err := fmp4.ProbeSegment(path)
	if err != nil {
		return Entry{}, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return Entry{}, err
	}
	return Entry{
		DurMs:     info.DurationMs,
		Size:      fi.Size(),
		Gen:       info.Gen,
		InitSize:  info.InitSize,
		FirstFrag: info.FirstFragSize,
	}, nil
}

// --- paths ------------------------------------------------------------------

func (c *Camera) Dir() string            { return c.root }
func (c *Camera) IndexDir() string       { return filepath.Join(c.root, "index") }
func (c *Camera) InitDir() string        { return filepath.Join(c.root, "init") }
func (c *Camera) DayDir(d string) string { return filepath.Join(c.root, d) }

func (c *Camera) EventsDir() string { return filepath.Join(c.root, "eventos") }

func (c *Camera) EventsPath(day string) string {
	return filepath.Join(c.EventsDir(), day+".ndjson")
}

// FramesDir is the day's folder with the detection frames, one .jpg per
// detection, named by its instant - as a segment is named by its start.
func (c *Camera) FramesDir(day string) string {
	return filepath.Join(c.root, "quadros", day)
}

// FramePath is the JPEG of the frame the object detector inferred for that
// detection. It opens with any viewer, and deleting one is deleting a file.
func (c *Camera) FramePath(day string, atMs int64) string {
	return filepath.Join(c.FramesDir(day), strconv.FormatInt(atMs, 10)+".jpg")
}

func (c *Camera) IndexPath(day string) string {
	return filepath.Join(c.IndexDir(), day+".ndjson")
}

func (c *Camera) InitPath(gen string) string {
	return filepath.Join(c.InitDir(), gen+".mp4")
}

// WriteInit writes a generation's init segment, if it doesn't exist yet.
// Since the name is the content hash, rewriting would always be redundant.
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
	// CreateTemp creates it with 0600; without this the init would be
	// unreadable by another user, unlike segments, which come out with 0644.
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// SegmentPath returns a segment's file path from its start.
func (c *Camera) SegmentPath(startMs int64) string {
	t := time.UnixMilli(startMs)
	return filepath.Join(c.root, t.Format(DayLayout), strconv.FormatInt(startMs, 10)+".mp4")
}

// --- writing ----------------------------------------------------------------

// EnsureDirs creates the directory structure needed to record on the given
// day.
//
// eventos/ isn't included: it's born on the first AppendEvent, and a camera
// without detection on doesn't get an empty directory.
func (c *Camera) EnsureDirs(day string) error {
	for _, d := range []string{c.IndexDir(), c.InitDir(), c.DayDir(day)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// Append adds an entry to the matching day's index.
//
// The fsync here is cheap (once per segment, that is, once a minute per
// camera) and it's what guarantees a power cut doesn't lose the record of a
// segment already whole on disk.
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
	s.DurMs += e.DurMs
	if e.StartMs < s.FirstMs {
		s.FirstMs = e.StartMs
	}
	if e.EndMs() > s.LastMs {
		s.LastMs = e.EndMs()
	}
}

// AppendEvent adds an onset to the day's file.
//
// WITHOUT fsync, unlike Append above, and the difference is deliberate: a
// segment lost from the index is video that exists on disk and disappears from
// the UI, while a lost onset is one mark less on a heat strip that has
// thousands. Paying an fsync per mark would be expensive for the wrong
// reason.
//
// This is called from inside the recorder loop, once every few dozen seconds.
// Three syscalls at that pace don't bother the same loop that already writes
// the video.
func (c *Camera) AppendEvent(ev Event) error {
	day := ev.Day()
	if err := os.MkdirAll(c.EventsDir(), 0o755); err != nil {
		return err
	}

	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	b = append(b, '\n')

	f, err := os.OpenFile(c.EventsPath(day), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// WriteFrame writes the JPEG of that detection's frame.
//
// No fsync and no temp file, unlike the init: a lost frame is one thumbnail
// less in a grid of thousands, and the screen already knows how to show a
// detection without an image. Writing directly saves the rename on a call
// that happens inside the camera's recording loop.
func (c *Camera) WriteFrame(day string, atMs int64, jpeg []byte) error {
	if len(jpeg) == 0 {
		return nil
	}
	if err := os.MkdirAll(c.FramesDir(day), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(c.FramePath(day, atMs), jpeg, 0o644); err != nil {
		return err
	}

	c.mu.Lock()
	if n, ok := c.framesBytes[day]; ok {
		c.framesBytes[day] = n + int64(len(jpeg))
	}
	c.mu.Unlock()
	return nil
}

// FramesBytes is how much a day's frames take. It counts toward the
// camera's quota, like the video.
//
// The number is memoized per day, and WriteFrame keeps it current: without
// that, every retention pass would stat every frame of every day - tens of
// thousands in a 30-day history, which is exactly the price of one file per
// detection. A day's sum is done ONCE, the first time someone asks for it.
func (c *Camera) FramesBytes(day string) int64 {
	c.mu.RLock()
	n, ok := c.framesBytes[day]
	c.mu.RUnlock()
	if ok {
		return n
	}

	entries, err := os.ReadDir(c.FramesDir(day))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0 // unreadable folder: don't make up a number for the quota
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		n += info.Size()
	}

	c.mu.Lock()
	c.framesBytes[day] = n
	c.mu.Unlock()
	return n
}

// --- reading ----------------------------------------------------------------

// Days returns the per-day summaries, oldest first.
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

// OldestMs is the start of the oldest segment on disk, or 0 if there's
// none.
//
// It's what the UI uses to say how long the camera has had recordings - the
// real retention, as opposed to the estimate "N days fit in the quota".
//
// It's not Days()[0].FirstMs because this runs on every /api/health read,
// every 3 seconds and for every camera: Days() allocates the whole list of
// summaries and sorts it just to read the first item.
func (c *Camera) OldestMs() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var oldest int64
	for _, s := range c.days {
		// A zero FirstMs would be a summary without any segment, which shouldn't
		// exist - but if it did, it would pull the minimum to zero and wipe out the
		// information of all the others.
		if s.FirstMs > 0 && (oldest == 0 || s.FirstMs < oldest) {
			oldest = s.FirstMs
		}
	}
	return oldest
}

// Summary returns, in a single pass, what the screen needs to describe the
// quota: how much the camera takes, both ends of the history on disk and how
// much video actually exists between them. What `newestMs - oldestMs` lacks to
// reach recordedMs are the recording gaps.
//
// It exists for economy: OldestMs and TotalBytes do exactly these sums, but
// whoever builds the Status needs both at once, and calling them in sequence
// cost two RLocks and two map scans on every /api/health read - times the
// number of cameras. The two separate methods still exist for whoever wants
// only one of the numbers (retention, Purge).
func (c *Camera) Summary() (bytes, oldestMs, newestMs, recordedMs int64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, s := range c.days {
		bytes += s.Bytes
		recordedMs += s.DurMs
		// A zero FirstMs would be a summary without any segment, which shouldn't
		// exist - but if it did, it would pull the minimum to zero and wipe out the
		// information of all the others. Same reason on the LastMs side.
		if s.FirstMs > 0 && (oldestMs == 0 || s.FirstMs < oldestMs) {
			oldestMs = s.FirstMs
		}
		if s.LastMs > newestMs {
			newestMs = s.LastMs
		}
	}
	return bytes, oldestMs, newestMs, recordedMs
}

// TotalBytes is everything the camera takes: the video plus the detection
// frames. It's the number the per-camera quota limits.
//
// Frames come in through each day's memoized sum, not a stat on every file:
// they're one file per detection, tens of thousands in a 30-day history, and
// retention asks for this on every pass. See FramesBytes.
func (c *Camera) TotalBytes() int64 {
	c.mu.RLock()
	days := make([]string, 0, len(c.days))
	var n int64
	for _, s := range c.days {
		n += s.Bytes
		days = append(days, s.Day)
	}
	c.mu.RUnlock()

	for _, d := range days {
		n += c.FramesBytes(d)
	}
	return n
}

// LoadDay reads a day's entries, sorted by start.
//
// A truncated last line (a crash in the middle of an append) is silently
// dropped: it's exactly the failure the append-only format anticipates.
func (c *Camera) LoadDay(day string) ([]Entry, error) {
	return c.scanDay(day, func(int64, int64) bool { return true })
}

// Entry returns the entry of the segment starting at startMs, and false if
// the index has none. It's the question of the thumbnail and of each player
// segment, so it decodes only the right line, not the whole day.
func (c *Camera) Entry(startMs int64) (Entry, bool, error) {
	day := time.UnixMilli(startMs).Format(DayLayout)
	list, err := c.scanDay(day, func(segStartMs, _ int64) bool { return segStartMs == startMs })
	if err != nil || len(list) == 0 {
		return Entry{}, false, err
	}
	return list[0], true, nil
}

// Range returns the entries that cover [fromMs, toMs).
//
// The day before `from` is read too: a segment may have started before
// midnight and extend into the requested interval.
func (c *Camera) Range(fromMs, toMs int64) ([]Entry, error) {
	if toMs <= fromMs {
		return nil, nil
	}
	covers := func(segStartMs, durMs int64) bool { return segStartMs+durMs > fromMs && segStartMs < toMs }

	var out []Entry
	for _, d := range days(time.UnixMilli(fromMs).AddDate(0, 0, -1), toMs) {
		entries, err := c.scanDay(d, covers)
		if err != nil {
			return nil, err
		}
		out = append(out, entries...)
	}
	return out, nil
}

// days returns the days (DayLayout) from `from` up to the day of the last
// instant before toMs, inclusive.
//
// The end compares days, not instants: from 23:59:50 to 00:00:35, stepping
// one day from `from` lands at 23:59:50 on the next day, already after toMs,
// and the next day would be left out.
func days(from time.Time, toMs int64) []string {
	last := time.UnixMilli(toMs - 1).Format(DayLayout)
	var out []string
	for d := from; d.Format(DayLayout) <= last; d = d.AddDate(0, 0, 1) {
		out = append(out, d.Format(DayLayout))
	}
	return out
}

// scanDay reads the entries of a day that `keep` accepts, sorted by start.
//
// The expensive part of the index is decoding the line: a day of 30s segments
// has ~2,700 lines, and decoding all of them costs ~45ms of CPU on an Orange
// Pi Zero 3. The thumbnail wants a single line, and the tail of today, one or
// two. So the start and duration come from the start of the line, which
// json.Marshal of Entry always writes the same way (`{"t":…,"d":…,`), and only
// the accepted line is decoded. A line that doesn't start like that - edited
// by hand, or from an Entry with a different field order - is decoded whole:
// slower, never wrong.
func (c *Camera) scanDay(day string, keep func(segStartMs, durMs int64) bool) ([]Entry, error) {
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
		if segStartMs, durMs, ok := lineHead(line); ok && !keep(segStartMs, durMs) {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			// Only the last line can be truncated; any other means real corruption
			// and deserves to be reported.
			continue
		}
		if keep(e.StartMs, e.DurMs) {
			entries = append(entries, e)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].StartMs < entries[j].StartMs })
	return entries, nil
}

// lineHead reads the start and the duration from the beginning of an index
// line without decoding it: `{"t":1791923604690,"d":30017,"sz":…`. ok comes
// out false when the line doesn't start exactly like that.
func lineHead(line []byte) (segStartMs, durMs int64, ok bool) {
	rest, ok := bytes.CutPrefix(line, []byte(`{"t":`))
	if !ok {
		return 0, 0, false
	}
	if segStartMs, rest, ok = numberUntil(rest, ','); !ok {
		return 0, 0, false
	}
	if rest, ok = bytes.CutPrefix(rest, []byte(`"d":`)); !ok {
		return 0, 0, false
	}
	if durMs, _, ok = numberUntil(rest, ','); !ok {
		return 0, 0, false
	}
	return segStartMs, durMs, true
}

// numberUntil reads the digits at the start of b up to the byte `end`, and
// returns what comes after it. Unsigned and with at most 18 digits, so it
// never overflows int64: anything else is left for the full decode.
func numberUntil(b []byte, end byte) (n int64, rest []byte, ok bool) {
	i := 0
	for ; i < len(b) && b[i] >= '0' && b[i] <= '9'; i++ {
		n = n*10 + int64(b[i]-'0')
	}
	if i == 0 || i > 18 || i == len(b) || b[i] != end {
		return 0, nil, false
	}
	return n, b[i+1:], true
}

// LoadEvents reads a day's onsets, sorted. A day without a file returns an
// empty slice and no error: a camera without detection on is the common
// case, not a failure.
func (c *Camera) LoadEvents(day string) ([]Event, error) {
	f, err := os.Open(c.EventsPath(day))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev Event
		// Same tolerance as LoadDay: the last line may have been cut by a crash,
		// and the append-only format anticipates exactly that.
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		out = append(out, ev)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AtMs < out[j].AtMs })
	return out, nil
}

// EventRange returns the onsets in [fromMs, toMs).
//
// Unlike the segments' Range, the previous day does NOT need to be read: an
// onset is an instant, not an interval, and doesn't cross midnight.
func (c *Camera) EventRange(fromMs, toMs int64) ([]Event, error) {
	if toMs <= fromMs {
		return nil, nil
	}
	var out []Event
	for _, d := range days(time.UnixMilli(fromMs), toMs) {
		evs, err := c.LoadEvents(d)
		if err != nil {
			return nil, err
		}
		for _, ev := range evs {
			if ev.AtMs >= fromMs && ev.AtMs < toMs {
				out = append(out, ev)
			}
		}
	}
	return out, nil
}

// --- initial load -----------------------------------------------------------

// Scan rebuilds the in-memory summaries by reading the indexes from disk.
//
// If reconcileLastDay is true, the most recent day is checked against the
// filesystem: entries whose file is gone are dropped and orphan files
// (written, but whose index line didn't get written) are brought back in.
// That's where crash damage lives, and it's cheap because it looks at a
// single day instead of the thousands of files of the whole history.
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

// reconcile aligns a day's index with what's actually on disk and rewrites
// the file if anything changed.
func (c *Camera) reconcile(day string, list []Entry, probe func(string) (Entry, error)) ([]Entry, error) {
	files, err := os.ReadDir(c.DayDir(day))
	if errors.Is(err, os.ErrNotExist) {
		// The index has entries but the directory is gone: trust the disk.
		if len(list) > 0 {
			return nil, c.rewrite(day, nil)
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	onDisk := map[int64]int64{} // startMs -> size
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

	// A zero-byte segment is a recording that didn't happen: the crash took the
	// video before it left the page cache, but the index line, which has Sync, was
	// already on disk. It leaves here like a file that's gone, otherwise the
	// timeline draws a stretch the player doesn't play. Deleting is safe because
	// Scan runs before the camera's recorder opens any segment (see Load); if it
	// fails, without an index line retention takes it along with the day.
	for ms, size := range onDisk {
		if size == 0 {
			_ = os.Remove(c.SegmentPath(ms))
			delete(onDisk, ms)
		}
	}

	var kept []Entry
	indexed := map[int64]bool{}
	changed := false
	for _, e := range list {
		size, ok := onDisk[e.StartMs]
		if !ok {
			changed = true // file is gone
			continue
		}
		if size != e.Size {
			// Segment truncated by a crash mid-write: the real size wins, and the index
			// now reflects what exists.
			e.Size = size
			changed = true
		}
		indexed[e.StartMs] = true
		kept = append(kept, e)
	}

	// Orphan files: written, but whose index line didn't make it out.
	for ms := range onDisk {
		if indexed[ms] || probe == nil {
			continue
		}
		e, err := probe(c.SegmentPath(ms))
		if err != nil {
			continue // unreadable: leave it, retention takes it eventually
		}
		if e.Gen == "" || e.DurMs == 0 {
			// Readable, but without an init or without a fragment: there's nothing to
			// play, and an empty generation would cut the strip the player splices.
			// Rejecting zero only holds here, with the Entry fresh from the probe: on an
			// old index line, zero is a field that didn't exist yet.
			continue
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

// --- removal ----------------------------------------------------------------

// DropOldestDay removes the oldest day entirely, files and index.
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
	freed += c.FramesBytes(day) // the detections' frames go along, below

	if err := os.RemoveAll(c.DayDir(day)); err != nil {
		return 0, err
	}
	if err := os.RemoveAll(c.FramesDir(day)); err != nil {
		return 0, err
	}
	for _, p := range []string{c.IndexPath(day), c.EventsPath(day)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return 0, err
		}
	}

	c.mu.Lock()
	delete(c.days, day)
	delete(c.framesBytes, day)
	c.mu.Unlock()
	c.forgetObjects(day)
	return freed, nil
}

// Purge deletes everything the camera has on disk: segments, indexes and
// inits.
//
// It's the only removal that takes the `init/` directory along. Retention
// never touches it, because an init is tiny and serves several days of
// segments at once - but when no segment is left, it has no reason to exist
// either.
//
// The total returned comes from the index, not from a filesystem walk: it's
// the same measure the quota uses, so the number matches what the screen
// showed before deleting. A file on disk without an index line doesn't count.
func (c *Camera) Purge() (freed int64, err error) {
	freed = c.TotalBytes()

	if err := os.RemoveAll(c.root); err != nil {
		return 0, err
	}

	c.mu.Lock()
	c.days = map[string]*DaySummary{}
	c.mu.Unlock()
	c.forgetObjects("")
	return freed, nil
}

// EvictOldest deletes the oldest segments until it frees at least `want`
// bytes, or runs out of segments. It returns how much it freed.
//
// Removal is always of whole segments: part of a file is never deleted.
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
		// The marks of the stretch just deleted go along. A mark without a
		// recording underneath is a heat strip pointing to video that no longer
		// exists: clicking it leads nowhere.
		if err := c.trimEvents(day, rest[0].StartMs); err != nil {
			return freed, err
		}
		c.recount(day, rest)
	}
	return freed, nil
}

// trimEvents drops the onsets before keepFromMs, rewriting the day's file.
// A day without events is neither an error nor work.
func (c *Camera) trimEvents(day string, keepFromMs int64) error {
	evs, err := c.LoadEvents(day)
	if err != nil || len(evs) == 0 {
		return err
	}
	cut := sort.Search(len(evs), func(i int) bool { return evs[i].AtMs >= keepFromMs })
	if cut == 0 {
		return nil
	}
	if cut == len(evs) {
		err := os.Remove(c.EventsPath(day))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, ev := range evs[cut:] {
		if err := enc.Encode(ev); err != nil {
			return err
		}
	}
	return writeAtomic(c.EventsPath(day), buf.Bytes())
}

// writeAtomic rewrites a whole file without ever leaving an intermediate
// state visible: it writes to a temp file in the SAME directory and renames.
//
// The Chmod is not a detail: without it the file, created with 0644 by the
// append, would drop to 0600 on the first rewrite - a silent permission change
// in the middle of the file's life.
func writeAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*.ndjson")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// rewrite rewrites a day's index. Index files are small (~100 KB a day),
// so rewriting comes out cheaper - and simpler to get right - than keeping
// deletion marks.
func (c *Camera) rewrite(day string, entries []Entry) error {
	if len(entries) == 0 {
		err := os.Remove(c.IndexPath(day))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			return err
		}
	}
	return writeAtomic(c.IndexPath(day), buf.Bytes())
}

func (c *Camera) recount(day string, entries []Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.days, day)
	for _, e := range entries {
		c.mergeLocked(day, e)
	}
}

// --- orphans ----------------------------------------------------------------

// OrphanInfo describes a recording directory that doesn't belong to any
// registered camera.
type OrphanInfo struct {
	ID      string `json:"id"`
	Bytes   int64  `json:"bytes"`
	Days    int    `json:"days"`
	FirstMs int64  `json:"firstMs"`
	LastMs  int64  `json:"lastMs"`
}

// Orphans lists what was left on disk by cameras already removed.
//
// Removing a camera doesn't delete its recordings, and from then on that
// material disappears from the whole application: the recording endpoints
// only accept a registered camera, and retention only walks the registered
// ones. Without this scan it would take up disk without showing up anywhere.
//
// The size comes from the index files - a few dozen per camera - and not from
// a walk over the segments, which would be tens of thousands. It's also the
// same source the quota uses, so the numbers don't diverge between screens.
func (s *Store) Orphans(registered map[string]bool) ([]OrphanInfo, error) {
	entries, err := os.ReadDir(s.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	out := make([]OrphanInfo, 0)
	for _, e := range entries {
		// Names from ReadDir are always a single path component, so there's no
		// directory traversal to close here.
		id := e.Name()
		if !e.IsDir() || registered[id] {
			continue
		}

		// A throwaway camera: memoizing it in the Store would turn an unregistered
		// ID into a permanent in-memory entry on every listing.
		c := newCamera(s.root, id)
		if err := c.Scan(false, nil); err != nil {
			return nil, err
		}

		info := OrphanInfo{ID: id}
		for _, d := range c.Days() {
			info.Days++
			info.Bytes += d.Bytes
			if info.FirstMs == 0 || d.FirstMs < info.FirstMs {
				info.FirstMs = d.FirstMs
			}
			if d.LastMs > info.LastMs {
				info.LastMs = d.LastMs
			}
		}
		out = append(out, info)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// OrphanBytes sums the size of everything Orphans lists.
func (s *Store) OrphanBytes(registered map[string]bool) (int64, error) {
	orphans, err := s.Orphans(registered)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, o := range orphans {
		total += o.Bytes
	}
	return total, nil
}

// ParseSegmentName extracts the start in ms from a segment file name.
func ParseSegmentName(name string) (int64, error) {
	if !strings.HasSuffix(name, ".mp4") {
		return 0, fmt.Errorf("store: %q is not a segment", name)
	}
	return strconv.ParseInt(strings.TrimSuffix(name, ".mp4"), 10, 64)
}
