package store

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/fmp4"
)

// baseTime is a fixed instant at 12:00 local time, far from midnight, so the
// tests don't depend on the time zone of whoever runs them.
func baseTime() time.Time {
	return time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)
}

func entryAt(t time.Time, durMs, size int64) Entry {
	return Entry{
		StartMs: t.UnixMilli(), DurMs: durMs, Size: size,
		Gen: "abc123", InitSize: 737, FirstFrag: 50000,
	}
}

// writeSegmentFile creates the file matching an entry, for the tests that
// need the disk to match the index.
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

func TestAppendAndRead(t *testing.T) {
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
		t.Fatalf("want 1 day, got %d", len(days))
	}
	if days[0].Count != 3 {
		t.Errorf("Count=%d, want 3", days[0].Count)
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
		t.Fatalf("LoadDay returned %d entries", len(entries))
	}
	if entries[0].InitSize != 737 || entries[0].Gen != "abc123" {
		t.Errorf("fields didn't survive the round trip: %+v", entries[0])
	}
}

// The summaries must survive a restart without any cache file: they're
// rebuilt by reading the indexes from disk.
func TestScanRebuildsSummaries(t *testing.T) {
	root := t.TempDir()
	c := New(root).Camera("cam_teste")
	base := baseTime()

	for i := range 5 {
		if err := c.Append(entryAt(base.Add(time.Duration(i)*time.Minute), 60_000, 2_000_000)); err != nil {
			t.Fatal(err)
		}
	}

	// Simulates a restart: a new instance, the same directory.
	c2 := New(root).Camera("cam_teste")
	if err := c2.Scan(false, nil); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got := c2.TotalBytes(); got != 10_000_000 {
		t.Errorf("after Scan TotalBytes=%d, want 10000000", got)
	}
	if days := c2.Days(); len(days) != 1 || days[0].Count != 5 {
		t.Errorf("wrong summary after Scan: %+v", days)
	}
}

func TestOldestMs(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()

	if got := c.OldestMs(); got != 0 {
		t.Errorf("a camera without recordings should give 0, got %d", got)
	}

	// Days out of order: the summaries map has no order at all, and the oldest
	// must come from the smallest FirstMs, not from the first one the iteration
	// sees.
	for _, d := range []int{2, 0, 1} {
		e := entryAt(base.AddDate(0, 0, d), 60_000, 1_000_000)
		if err := c.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := c.OldestMs(), base.UnixMilli(); got != want {
		t.Errorf("OldestMs=%d, want %d", got, want)
	}

	// An earlier segment within the day that's already the oldest: the minimum
	// is per segment, not per day.
	before := base.Add(-2 * time.Hour)
	if err := c.Append(entryAt(before, 60_000, 1_000_000)); err != nil {
		t.Fatal(err)
	}
	if got, want := c.OldestMs(), before.UnixMilli(); got != want {
		t.Errorf("OldestMs=%d after an earlier segment, want %d", got, want)
	}
}

// Summary exists to save one map scan per /api/health read, so what it
// returns must be identical to what the methods it replaces return - plus the
// new end, the most recent.
func TestSummaryMatchesTheSeparateMethods(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()

	if bytes, oldest, newest, written := c.Summary(); bytes != 0 || oldest != 0 || newest != 0 || written != 0 {
		t.Errorf("a camera without recordings should give all 0, got %d/%d/%d/%d", bytes, oldest, newest, written)
	}

	// Days out of order, as the summaries map delivers them: the ends have to
	// come from the comparison, not from the iteration order.
	for _, d := range []int{2, 0, 1} {
		if err := c.Append(entryAt(base.AddDate(0, 0, d), 60_000, 1_000_000)); err != nil {
			t.Fatal(err)
		}
	}

	bytes, oldest, newest, written := c.Summary()
	if want := c.TotalBytes(); bytes != want {
		t.Errorf("bytes=%d, TotalBytes says %d", bytes, want)
	}
	if want := c.OldestMs(); oldest != want {
		t.Errorf("oldest=%d, OldestMs says %d", oldest, want)
	}
	// The most recent is the END of the last segment, not its start: that's
	// what makes the span cover the whole recording.
	if want := base.AddDate(0, 0, 2).UnixMilli() + 60_000; newest != want {
		t.Errorf("newest=%d, want %d", newest, want)
	}
	// Three one-minute segments on three days: the span is two days, but the
	// video that exists is three minutes. The difference is the gap.
	if written != 3*60_000 {
		t.Errorf("recorded=%d, want %d", written, 3*60_000)
	}
}

// sumByLoadDay is the recorded time through the expensive path, reading
// each day's index from disk: the reference the in-memory accumulation is
// checked against.
func sumByLoadDay(t *testing.T, c *Camera) int64 {
	t.Helper()
	var n int64
	for _, d := range c.Days() {
		list, err := c.LoadDay(d.Day)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range list {
			n += e.DurMs
		}
	}
	return n
}

// The summary's DurMs is kept in memory, segment by segment, so /api/health
// doesn't need to read the indexes. If some path that changes the index stops
// going through mergeLocked, the two silently diverge - and the screen starts
// showing a gap where there's none, or hiding one that exists. This test
// checks the accumulation against the disk after each of those paths.
func TestDurMsMatchesTheIndex(t *testing.T) {
	root := t.TempDir()
	c := New(root).Camera("cam_teste")
	base := baseTime()

	check := func(c *Camera, when string) {
		t.Helper()
		_, _, _, written := c.Summary()
		if want := sumByLoadDay(t, c); written != want {
			t.Errorf("%s: recorded=%d, the index sums %d", when, written, want)
		}
	}

	// Uneven durations, as in real life: the cut is by keyframe and a
	// reconnection shortens the segment. Two days, with a gap between them.
	durations := []int64{60_000, 58_400, 61_200, 12_300, 60_000}
	for d := range 2 {
		for i, dur := range durations {
			e := entryAt(base.AddDate(0, 0, d).Add(time.Duration(i)*time.Minute), dur, 1_000_000)
			writeSegmentFile(t, c, e)
			if err := c.Append(e); err != nil {
				t.Fatal(err)
			}
		}
	}
	check(c, "após Append")

	c2 := New(root).Camera("cam_teste")
	if err := c2.Scan(false, nil); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	check(c2, "após Scan")

	// Partial eviction: the oldest day loses two segments and is recounted.
	if _, err := c2.EvictOldest(1_500_000); err != nil {
		t.Fatal(err)
	}
	check(c2, "após EvictOldest parcial")

	// Eviction of the whole remaining day: the summary goes away through DropDay.
	if _, err := c2.EvictOldest(3_000_000); err != nil {
		t.Fatal(err)
	}
	check(c2, "após EvictOldest do dia inteiro")
	if _, _, _, written := c2.Summary(); written == 0 {
		t.Error("the second day shouldn't have been touched")
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

	// Asks for 2.5 MB: evicts 3 whole segments, because removal is always by
	// whole segment.
	freed, err := c.EvictOldest(2_500_000)
	if err != nil {
		t.Fatalf("EvictOldest: %v", err)
	}
	if freed != 3_000_000 {
		t.Errorf("freed %d, want 3000000", freed)
	}
	if got := c.TotalBytes(); got != 2_000_000 {
		t.Errorf("TotalBytes=%d after eviction, want 2000000", got)
	}

	// The oldest files must really be gone, and the new ones must have stayed.
	for i, e := range entries {
		_, err := os.Stat(c.SegmentPath(e.StartMs))
		if i < 3 && err == nil {
			t.Errorf("segment %d should have been deleted", i)
		}
		if i >= 3 && err != nil {
			t.Errorf("segment %d shouldn't have been deleted: %v", i, err)
		}
	}

	// The index on disk must reflect the removal, not just memory.
	rest, err := c.LoadDay(base.Format(DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 2 {
		t.Errorf("index on disk has %d entries, want 2", len(rest))
	}
}

func TestEvictOldestCrossesDays(t *testing.T) {
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
		t.Fatalf("want 3 days, got %d", len(c.Days()))
	}

	// Frees more than a whole day: the emptied day has to disappear.
	if _, err := c.EvictOldest(3_000_000); err != nil {
		t.Fatal(err)
	}
	days := c.Days()
	if len(days) != 2 {
		t.Fatalf("want 2 days after eviction, got %d: %+v", len(days), days)
	}
	if days[0].Day != base.AddDate(0, 0, 1).Format(DayLayout) {
		t.Errorf("oldest day is now %s", days[0].Day)
	}
	// The removed day's directory must not be left behind.
	if _, err := os.Stat(c.DayDir(base.Format(DayLayout))); !os.IsNotExist(err) {
		t.Error("the evicted day's directory still exists")
	}
}

// A segment may start before midnight and extend into the requested
// interval. If Range only looked at the interval's days, that segment would
// disappear from the timeline right after the day turned.
func TestRangeIncludesSegmentCrossingMidnight(t *testing.T) {
	c := newTestCamera(t)
	day := time.Date(2026, 8, 8, 0, 0, 0, 0, time.Local)
	before := day.Add(-30 * time.Second) // 23:59:30 on the previous day

	// A segment that starts at 23:59:30 and lasts 60s, going into the next day.
	if err := c.Append(entryAt(before, 60_000, 500_000)); err != nil {
		t.Fatal(err)
	}
	// A normal segment, already within the day.
	if err := c.Append(entryAt(day.Add(time.Minute), 60_000, 500_000)); err != nil {
		t.Fatal(err)
	}

	got, err := c.Range(day.UnixMilli(), day.Add(2*time.Hour).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Range returned %d entries, want 2 (was the one crossing midnight lost?)", len(got))
	}
	if got[0].StartMs != before.UnixMilli() {
		t.Error("the first entry should be the one that started on the previous day")
	}
}

// An interval from 23:59:50 to 00:00:35 has a `from` time later than the
// `to` time. If the day loop compared instants instead of days, it would stop
// before the next day, and what comes after midnight would disappear from the
// export.
func TestRangeCrossesMidnightWithLaterFromTime(t *testing.T) {
	c := newTestCamera(t)
	day := time.Date(2026, 8, 8, 0, 0, 0, 0, time.Local)

	// One segment before midnight and another after it.
	if err := c.Append(entryAt(day.Add(-30*time.Second), 30_000, 100)); err != nil {
		t.Fatal(err)
	}
	if err := c.Append(entryAt(day, 30_000, 100)); err != nil {
		t.Fatal(err)
	}

	got, err := c.Range(day.Add(-10*time.Second).UnixMilli(), day.Add(35*time.Second).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Range returned %d entries, want 2 (was the one after midnight lost?)", len(got))
	}
}

func TestRangeExcludesOutsideTheInterval(t *testing.T) {
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
		t.Errorf("Range returned %d, want 3: %+v", len(got), got)
	}
}

// Reconciliation: it's what saves the index after a kill -9.
func TestScanReconcilesOrphanFile(t *testing.T) {
	root := t.TempDir()
	c := New(root).Camera("cam_teste")
	base := baseTime()

	e1 := entryAt(base, 60_000, 1_000)
	writeSegmentFile(t, c, e1)
	if err := c.Append(e1); err != nil {
		t.Fatal(err)
	}

	// Orphan: the file was written, but the process died before indexing it.
	orphan := entryAt(base.Add(time.Minute), 60_000, 2_000)
	writeSegmentFile(t, c, orphan)

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
		t.Errorf("TotalBytes=%d, want 3000 (was the orphan brought back in?)", got)
	}
	entries, err := c2.LoadDay(base.Format(DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("index has %d entries, want 2", len(entries))
	}
	if entries[1].StartMs != orphan.StartMs {
		t.Errorf("the orphan came in with StartMs=%d, want %d", entries[1].StartMs, orphan.StartMs)
	}
}

func TestScanReconcilesEntryWithoutFile(t *testing.T) {
	root := t.TempDir()
	c := New(root).Camera("cam_teste")
	base := baseTime()

	e1 := entryAt(base, 60_000, 1_000)
	writeSegmentFile(t, c, e1)
	if err := c.Append(e1); err != nil {
		t.Fatal(err)
	}
	// Indexed, but the file doesn't exist (deleted from outside).
	if err := c.Append(entryAt(base.Add(time.Minute), 60_000, 9_000)); err != nil {
		t.Fatal(err)
	}

	c2 := New(root).Camera("cam_teste")
	if err := c2.Scan(true, nil); err != nil {
		t.Fatal(err)
	}
	if got := c2.TotalBytes(); got != 1_000 {
		t.Errorf("TotalBytes=%d, want 1000 (was the ghost entry dropped?)", got)
	}
}

// A power cut leaves, per camera, the same pair: an indexed segment with
// the full duration and a zero-byte file (the index has Sync, the video
// doesn't), and the next file, also zero bytes, without an index line. Seen on
// the real installation on 09/08, 09/12, 09/14, 09/18 and 09/20/2026. Neither
// may be left over: the timeline would draw a stretch that doesn't play.
func TestScanDropsZeroByteSegment(t *testing.T) {
	root := t.TempDir()
	c := New(root).Camera("cam_teste")
	base := baseTime()

	good := entryAt(base, 36_000, 1_000)
	writeSegmentFile(t, c, good)
	empty := entryAt(base.Add(36*time.Second), 36_000, 5_000)
	if err := c.Append(good); err != nil {
		t.Fatal(err)
	}
	if err := c.Append(empty); err != nil {
		t.Fatal(err)
	}
	empty.Size = 0
	writeSegmentFile(t, c, empty)
	orphan := entryAt(base.Add(72*time.Second), 0, 0)
	writeSegmentFile(t, c, orphan)

	// The real probe: an empty file doesn't make it fail, and an all-zero
	// Entry comes out.
	probe := func(path string) (Entry, error) {
		info, err := fmp4.ProbeSegment(path)
		if err != nil {
			return Entry{}, err
		}
		return Entry{DurMs: info.DurationMs, Gen: info.Gen}, nil
	}
	c2 := New(root).Camera("cam_teste")
	if err := c2.Scan(true, probe); err != nil {
		t.Fatal(err)
	}

	entries, err := c2.LoadDay(base.Format(DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].StartMs != good.StartMs {
		t.Fatalf("index kept %v, want only the good segment", entries)
	}
	for _, e := range []Entry{empty, orphan} {
		if _, err := os.Stat(c2.SegmentPath(e.StartMs)); !os.IsNotExist(err) {
			t.Errorf("the zero-byte file at %d stayed on disk (err=%v)", e.StartMs, err)
		}
	}
}

// A crash in the middle of a write leaves the segment smaller than the index
// says. The real size has to win, otherwise quota accounting stays wrong
// forever.
func TestScanFixesMismatchedSize(t *testing.T) {
	root := t.TempDir()
	c := New(root).Camera("cam_teste")
	base := baseTime()

	e := entryAt(base, 60_000, 5_000)
	writeSegmentFile(t, c, e)
	if err := c.Append(e); err != nil {
		t.Fatal(err)
	}
	// Truncates the file, as a power cut would.
	if err := os.Truncate(c.SegmentPath(e.StartMs), 1_234); err != nil {
		t.Fatal(err)
	}

	c2 := New(root).Camera("cam_teste")
	if err := c2.Scan(true, nil); err != nil {
		t.Fatal(err)
	}
	if got := c2.TotalBytes(); got != 1_234 {
		t.Errorf("TotalBytes=%d, want 1234", got)
	}
}

// A truncated line at the end of the index is the expected failure of the
// append-only format and must not bring down reading the whole day.
func TestLoadDayIgnoresTruncatedLine(t *testing.T) {
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
	if _, err := f.WriteString(`{"t":123,"d":60000,"sz":`); err != nil { // half a JSON
		t.Fatal(err)
	}
	f.Close()

	entries, err := c.LoadDay(base.Format(DayLayout))
	if err != nil {
		t.Fatalf("LoadDay failed because of a truncated line: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("want 1 valid entry, got %d", len(entries))
	}
}

// scanDay's shortcut reads the start of the line without decoding it. If
// this test breaks, Entry's field order changed: the index stays right, but
// every thumbnail goes back to decoding the whole day.
func TestLineHeadReadsWhatAppendWrites(t *testing.T) {
	c := newTestCamera(t)
	e := entryAt(baseTime(), 30_017, 1_000)
	if err := c.Append(e); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(c.IndexPath(e.Day()))
	if err != nil {
		t.Fatal(err)
	}
	start, dur, ok := lineHead(bytes.TrimSpace(b))
	if !ok || start != e.StartMs || dur != e.DurMs {
		t.Errorf("lineHead(%q) = %d, %d, %v", b, start, dur, ok)
	}
}

func TestLineHeadRejectsOtherFormat(t *testing.T) {
	for _, line := range []string{
		`{"d":60000,"t":1791923604690,"sz":1}`,
		`{"t":1791923604690,"sz":1,"d":60000}`,
		`{"t":-5,"d":60000,"sz":1}`,
		`{"t":1791923604690,"d":60000}`,
		`{"t":12345678901234567890,"d":1,"sz":1}`,
		`{"t":1791923604690,"d":`,
		``,
	} {
		if _, _, ok := lineHead([]byte(line)); ok {
			t.Errorf("lineHead(%q) accepted a line outside the format", line)
		}
	}
}

func TestEntryReturnsOnlyTheRequestedSegment(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	var recorded []Entry
	for i := range 5 {
		e := entryAt(base.Add(time.Duration(i)*time.Minute), 60_000, int64(1000+i))
		if err := c.Append(e); err != nil {
			t.Fatal(err)
		}
		recorded = append(recorded, e)
	}

	for _, want := range recorded {
		got, ok, err := c.Entry(want.StartMs)
		if err != nil || !ok || got != want {
			t.Errorf("Entry(%d) = %+v, %v, %v; want %+v", want.StartMs, got, ok, err, want)
		}
	}
	// The lookup is by exact start: 1ms after it is no segment at all.
	if _, ok, err := c.Entry(recorded[2].StartMs + 1); ok || err != nil {
		t.Errorf("Entry off a start: ok=%v, err=%v", ok, err)
	}
	// A day without an index: not finding isn't an error.
	if _, ok, err := c.Entry(base.AddDate(0, 0, -3).UnixMilli()); ok || err != nil {
		t.Errorf("Entry on a day without an index: ok=%v, err=%v", ok, err)
	}
}

// A line outside Append's format - written by hand, for example - doesn't
// disappear: it falls through to the full decode.
func TestEntryAndRangeFindLineInOtherFormat(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	if err := c.Append(entryAt(base, 60_000, 1_000)); err != nil {
		t.Fatal(err)
	}
	other := base.Add(time.Minute).UnixMilli()
	f, err := os.OpenFile(c.IndexPath(base.Format(DayLayout)), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(f, `{"sz":7,"d":60000,"t":%d,"g":"abc123","io":737,"f0":50000}`+"\n", other)
	f.Close()

	if e, ok, err := c.Entry(other); err != nil || !ok || e.Size != 7 {
		t.Errorf("Entry = %+v, %v, %v", e, ok, err)
	}
	got, err := c.Range(other, other+1)
	if err != nil || len(got) != 1 || got[0].StartMs != other {
		t.Errorf("Range = %+v, %v", got, err)
	}
}

// The truncated line may start with exactly the start being looked for: the
// start matches, the decode fails, and it's left out as in LoadDay.
func TestEntryIgnoresTruncatedLine(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	if err := c.Append(entryAt(base, 60_000, 1_000)); err != nil {
		t.Fatal(err)
	}
	truncated := base.Add(time.Minute).UnixMilli()
	f, err := os.OpenFile(c.IndexPath(base.Format(DayLayout)), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(f, `{"t":%d,"d":60000,"sz":`, truncated)
	f.Close()

	if _, ok, err := c.Entry(truncated); ok || err != nil {
		t.Errorf("Entry of the truncated line: ok=%v, err=%v", ok, err)
	}
	if _, ok, err := c.Entry(base.UnixMilli()); !ok || err != nil {
		t.Errorf("Entry of the good line: ok=%v, err=%v", ok, err)
	}
}

// Range picks lines by their start; the reference is what it did before,
// filtering the whole decoded day.
func TestRangeMatchesFilterOverTheWholeDay(t *testing.T) {
	c := newTestCamera(t)
	day := time.Date(2026, 8, 8, 0, 0, 0, 0, time.Local)

	// Varied durations, starting on the previous day so a segment crosses
	// midnight.
	start := day.Add(-90 * time.Second)
	for i := range 40 {
		dur := int64(20_000 + (i%5)*7_000)
		if err := c.Append(entryAt(start, dur, 100)); err != nil {
			t.Fatal(err)
		}
		start = start.Add(time.Duration(dur) * time.Millisecond)
	}
	var all []Entry
	for _, d := range []time.Time{day.AddDate(0, 0, -1), day} {
		l, err := c.LoadDay(d.Format(DayLayout))
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, l...)
	}

	// Starts before midnight: some intervals cross the turn of the day, with
	// the `from` time later than the `to` time.
	for from := day.Add(-2 * time.Minute).UnixMilli(); from < start.UnixMilli(); from += 11_000 {
		to := from + 45_000
		var want []Entry
		for _, e := range all {
			if e.EndMs() > from && e.StartMs < to {
				want = append(want, e)
			}
		}
		got, err := c.Range(from, to)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("Range(%d, %d) returned %d entries, want %d", from, to, len(got), len(want))
		}
	}
}

func TestWriteInitIsIdempotent(t *testing.T) {
	c := newTestCamera(t)
	if err := c.WriteInit("deadbeef", []byte("init-v1")); err != nil {
		t.Fatal(err)
	}
	// An init with the same hash must never be rewritten.
	if err := c.WriteInit("deadbeef", []byte("outro-conteudo")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(c.InitPath("deadbeef"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "init-v1" {
		t.Errorf("init was overwritten: %q", got)
	}
}

// The index is born 0644 in Append and is rewritten through a temp file on
// eviction. Without an explicit Chmod it would drop to 0600 in the middle of
// its life, silently changing the permission.
func TestPermissionsConsistentAfterEviction(t *testing.T) {
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
	before, err := os.Stat(idxPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.EvictOldest(1_500); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(idxPath)
	if err != nil {
		t.Fatal(err)
	}
	if before.Mode() != after.Mode() {
		t.Errorf("index permission changed from %v to %v after eviction", before.Mode(), after.Mode())
	}

	init, err := os.Stat(c.InitPath("cafe01"))
	if err != nil {
		t.Fatal(err)
	}
	if init.Mode().Perm() != 0o644 {
		t.Errorf("init written with permission %v, want 0644", init.Mode().Perm())
	}
}

func TestSegmentPath(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	got := c.SegmentPath(base.UnixMilli())
	want := filepath.Join(c.Dir(), base.Format(DayLayout), "1786363200000.mp4")
	// The exact name depends on the time zone; check only the structure.
	if filepath.Dir(got) != filepath.Dir(want) {
		t.Errorf("segment directory = %s, want %s", filepath.Dir(got), filepath.Dir(want))
	}
	ms, err := ParseSegmentName(filepath.Base(got))
	if err != nil {
		t.Fatal(err)
	}
	if ms != base.UnixMilli() {
		t.Errorf("name round trip failed: %d != %d", ms, base.UnixMilli())
	}
}

// Purge is the only removal that takes the init along. Retention never
// touches it, so without this an init/ directory would be left over from a
// camera that no longer exists.
func TestPurgeDeletesSegmentsIndexesAndInits(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()

	today := entryAt(base, 60_000, 1_000_000)
	yesterday := entryAt(base.AddDate(0, 0, -1), 60_000, 500_000)
	for _, e := range []Entry{today, yesterday} {
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
		t.Errorf("freed=%d, want 1500000", freed)
	}
	if _, err := os.Stat(c.Dir()); !os.IsNotExist(err) {
		t.Errorf("the camera directory is still there: err=%v", err)
	}
	if c.TotalBytes() != 0 || len(c.Days()) != 0 {
		t.Errorf("the in-memory summary survived: %d bytes, %d days",
			c.TotalBytes(), len(c.Days()))
	}
}

// Purging a camera that never recorded isn't an error: it's what happens
// when removing a camera registered minutes before, with the disk still
// empty.
func TestPurgeWithNothingRecordedDoesNotFail(t *testing.T) {
	c := newTestCamera(t)
	freed, err := c.Purge()
	if err != nil {
		t.Fatalf("Purge: %v", err)
	}
	if freed != 0 {
		t.Errorf("freed=%d, want 0", freed)
	}
}

// The ID becomes a directory right under storage. One that gave the storage
// itself, or what's above it, would make Purge delete everything there.
func TestCameraRejectsIDThatIsNotADirectoryName(t *testing.T) {
	s := New(t.TempDir())
	for _, id := range []string{"", ".", "..", "../fora", "a/b", "a/..", "/etc"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Camera(%q) accepted an ID that isn't a directory name", id)
				}
			}()
			s.Camera(id)
		}()
	}
	for _, id := range []string{"sala", "cam..1", "..."} {
		if got := s.Camera(id).Dir(); got != filepath.Join(s.Root(), id) {
			t.Errorf("Camera(%q).Dir() = %q", id, got)
		}
	}
}

func TestOrphansIgnoresRegisteredCameras(t *testing.T) {
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
		t.Fatalf("want 2 orphans, got %d: %+v", len(got), got)
	}
	// It comes sorted, so the list doesn't jump around on every screen reload.
	if got[0].ID != "cam_a" || got[1].ID != "cam_b" {
		t.Errorf("IDs = %s, %s", got[0].ID, got[1].ID)
	}
	if got[0].Bytes != 1_000_000 || got[0].Days != 1 {
		t.Errorf("cam_a = %d bytes, %d days", got[0].Bytes, got[0].Days)
	}
	if got[0].FirstMs != base.UnixMilli() || got[0].LastMs != base.UnixMilli()+60_000 {
		t.Errorf("cam_a range = %d to %d", got[0].FirstMs, got[0].LastMs)
	}
}

// A directory without an index is leftover from eviction or a manual copy.
// Its size can't be told, but hiding it would be worse: it's exactly the
// garbage that only leaves the disk if someone sees it.
func TestOrphansAcceptsDirectoryWithoutIndex(t *testing.T) {
	root := t.TempDir()
	s := New(root)

	if err := os.MkdirAll(filepath.Join(root, "cam_lixo"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A loose file at the root is no camera and must not become a line on the
	// screen.
	if err := os.WriteFile(filepath.Join(root, "anotacao.txt"), []byte("oi"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := s.Orphans(nil)
	if err != nil {
		t.Fatalf("Orphans: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want only cam_lixo, got %+v", got)
	}
	if got[0].ID != "cam_lixo" || got[0].Bytes != 0 || got[0].Days != 0 {
		t.Errorf("cam_lixo = %+v", got[0])
	}
}

// --- motion events -----------------------------------------------------------

func appendEvents(t *testing.T, c *Camera, base time.Time, offsets ...time.Duration) {
	t.Helper()
	for _, o := range offsets {
		if err := c.AppendEvent(Event{AtMs: base.Add(o).UnixMilli()}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEventsAppendAndRead(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	appendEvents(t, c, base, 0, time.Minute, 2*time.Minute)

	evs, err := c.LoadEvents(base.Format(DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Fatalf("want 3 marks, got %d", len(evs))
	}
	if evs[0].AtMs != base.UnixMilli() {
		t.Errorf("first mark at %d, expected %d", evs[0].AtMs, base.UnixMilli())
	}

	// A day without a file is the common case - a camera without detection on -
	// and must not be an error.
	empty, err := c.LoadEvents("2020-01-01")
	if err != nil || len(empty) != 0 {
		t.Errorf("day without events: %d marks, error %v", len(empty), err)
	}
}

// TestEnsureDirsDoesNotCreateEvents: someone who only records doesn't get
// the detection directory.
func TestEnsureDirsDoesNotCreateEvents(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	if err := c.EnsureDirs(base.Format(DayLayout)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.EventsDir()); !os.IsNotExist(err) {
		t.Fatalf("eventos/ created without any event: %v", err)
	}
	appendEvents(t, c, base, 0)
	if _, err := os.Stat(c.EventsPath(base.Format(DayLayout))); err != nil {
		t.Errorf("the first event didn't create the file: %v", err)
	}
}

func TestEventRangeCutsAtTheEnds(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	appendEvents(t, c, base, 0, time.Minute, 2*time.Minute, 3*time.Minute)

	// [base+1min, base+3min): takes the 1 and the 2, not the 3.
	evs, err := c.EventRange(base.Add(time.Minute).UnixMilli(), base.Add(3*time.Minute).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("want 2 marks, got %d: %v", len(evs), evs)
	}
	if evs[0].AtMs != base.Add(time.Minute).UnixMilli() {
		t.Errorf("wrong cut at the lower end: %v", evs)
	}
}

func TestEventRangeCrossesDays(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	// One mark today at noon and another tomorrow at noon.
	appendEvents(t, c, base, 0, 24*time.Hour)

	evs, err := c.EventRange(base.UnixMilli(), base.Add(48*time.Hour).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("want 2 marks on two days, got %d", len(evs))
	}
}

// The same case as TestRangeCrossesMidnightWithLaterFromTime: the
// detection stretch crossing midnight must not lose the next day's marks.
func TestEventRangeCrossesMidnightWithLaterFromTime(t *testing.T) {
	c := newTestCamera(t)
	day := time.Date(2026, 8, 8, 0, 0, 0, 0, time.Local)
	appendEvents(t, c, day, -5*time.Second, 10*time.Second)

	evs, err := c.EventRange(day.Add(-10*time.Second).UnixMilli(), day.Add(35*time.Second).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("want 2 marks, one on each side of midnight, got %d", len(evs))
	}
}

func TestDropDayTakesTheEventsAlong(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	e := entryAt(base, 30_000, 1000)
	writeSegmentFile(t, c, e)
	if err := c.Append(e); err != nil {
		t.Fatal(err)
	}
	appendEvents(t, c, base, 0, time.Minute)

	if _, err := c.DropDay(base.Format(DayLayout)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(c.EventsPath(base.Format(DayLayout))); !os.IsNotExist(err) {
		t.Errorf("the events file survived DropDay: %v", err)
	}
}

// TestEvictOldestTrimsMarksWithoutRecording guards the rule that makes the
// heat strip clickable: a mark without video underneath is a mark that leads
// nowhere.
func TestEvictOldestTrimsMarksWithoutRecording(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()

	// Three 1-minute segments and one mark inside each.
	for i := range 3 {
		e := entryAt(base.Add(time.Duration(i)*time.Minute), 60_000, 1000)
		writeSegmentFile(t, c, e)
		if err := c.Append(e); err != nil {
			t.Fatal(err)
		}
		appendEvents(t, c, base, time.Duration(i)*time.Minute+time.Second)
	}

	// Frees the first segment. Its mark has to go along; the other two
	// stay.
	if _, err := c.EvictOldest(1000); err != nil {
		t.Fatal(err)
	}
	evs, err := c.LoadEvents(base.Format(DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("want 2 marks after eviction, got %d: %v", len(evs), evs)
	}
	if evs[0].AtMs < base.Add(time.Minute).UnixMilli() {
		t.Errorf("a mark of the deleted segment was left: %v", evs)
	}
}

func TestEventsIgnoreTruncatedLine(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	appendEvents(t, c, base, 0, time.Minute)

	// A crash in the middle of an append leaves exactly this.
	path := c.EventsPath(base.Format(DayLayout))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"instanteMs":178849`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	evs, err := c.LoadEvents(base.Format(DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Errorf("the truncated line wasn't dropped: %d marks", len(evs))
	}
}

// The detection frames: one .jpg per detection, named by its instant.
func TestDayFrames(t *testing.T) {
	c := newTestCamera(t)
	day := baseTime().Format(DayLayout)
	jpeg := []byte("\xff\xd8um quadro\xff\xd9")
	at := baseTime().UnixMilli()

	if err := c.WriteFrame(day, at, jpeg); err != nil {
		t.Fatal(err)
	}

	path := c.FramePath(day, at)
	if filepath.Base(path) != fmt.Sprintf("%d.jpg", at) {
		t.Errorf("path %q: the name must be the detection's instant", path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(jpeg) {
		t.Errorf("frame written %q, expected %q", got, jpeg)
	}

	// The two marks of the same instant - one per family - write the same
	// file, not two.
	if err := c.WriteFrame(day, at, jpeg); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(c.FramesDir(day))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("%d files in the day's folder, expected 1", len(entries))
	}
}

// Frames count toward the camera's quota and leave with the day, like the video.
func TestFramesCountInQuotaAndGoWithTheDay(t *testing.T) {
	c := newTestCamera(t)
	e := entryAt(baseTime(), 60_000, 1_000_000)
	if err := c.Append(e); err != nil {
		t.Fatal(err)
	}
	writeSegmentFile(t, c, e)

	jpeg := make([]byte, 14_000)
	for i := range 3 {
		if err := c.WriteFrame(e.Day(), e.StartMs+int64(i)*1000, jpeg); err != nil {
			t.Fatal(err)
		}
	}

	if got, want := c.TotalBytes(), int64(1_000_000+3*14_000); got != want {
		t.Errorf("TotalBytes=%d, expected %d: frames count in the quota", got, want)
	}

	// A day's sum is memoized; a new frame after that must go into the total,
	// otherwise the quota freezes at the first value read.
	if err := c.WriteFrame(e.Day(), e.StartMs+9000, jpeg); err != nil {
		t.Fatal(err)
	}
	if got, want := c.TotalBytes(), int64(1_000_000+4*14_000); got != want {
		t.Errorf("TotalBytes=%d, expected %d: frame written after the sum", got, want)
	}

	freed, err := c.DropDay(e.Day())
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(1_000_000 + 4*14_000); freed != want {
		t.Errorf("freed %d, expected %d: frames leave with the day", freed, want)
	}
	if _, err := os.Stat(c.FramesDir(e.Day())); !os.IsNotExist(err) {
		t.Errorf("the day's frames folder was left: %v", err)
	}
	if got := c.TotalBytes(); got != 0 {
		t.Errorf("TotalBytes=%d after DropDay, expected 0", got)
	}
}
