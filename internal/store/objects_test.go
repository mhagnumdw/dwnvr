package store

import (
	"os"
	"testing"
	"time"
)

func appendObjeto(t *testing.T, c *Camera, atMs int64, family string) {
	t.Helper()
	if err := c.AppendEvent(Event{AtMs: atMs, Family: family, Class: "x", Score: 0.9}); err != nil {
		t.Fatal(err)
	}
}

func instants(evs []Event) []int64 {
	out := make([]int64, len(evs))
	for i, ev := range evs {
		out[i] = ev.AtMs
	}
	return out
}

// Only the object marks, in instant order - even if the object detector
// answered out of order.
func TestDayObjectsOnlyMarksInOrder(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime().UnixMilli()
	day := baseTime().Format(DayLayout)
	if err := c.AppendEvent(Event{AtMs: base}); err != nil {
		t.Fatal(err)
	}
	appendObjeto(t, c, base+2000, "pessoa")
	if err := c.AppendEvent(Event{AtMs: base + 3000}); err != nil {
		t.Fatal(err)
	}
	appendObjeto(t, c, base+1000, "veiculo")

	got, err := c.DayObjects(day)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{base + 1000, base + 2000}; len(got) != 2 || got[0].AtMs != want[0] || got[1].AtMs != want[1] {
		t.Errorf("instants %v, expected %v", instants(got), want)
	}

	empty, err := c.DayObjects("2020-01-01")
	if err != nil || len(empty) != 0 {
		t.Errorf("day without a file: %d marks, error %v", len(empty), err)
	}
}

// The current day grows: the next read picks up what came in, and the
// slice already handed out doesn't change under whoever got it.
func TestDayObjectsFollowsTheGrowingFile(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime().UnixMilli()
	day := baseTime().Format(DayLayout)
	appendObjeto(t, c, base+2000, "pessoa")

	before, _ := c.DayObjects(day)
	appendObjeto(t, c, base+1000, "animal")
	if err := c.AppendEvent(Event{AtMs: base + 5000}); err != nil {
		t.Fatal(err)
	}

	after, err := c.DayObjects(day)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 || after[0].AtMs != base+1000 {
		t.Errorf("after growing: %v", instants(after))
	}
	if len(before) != 1 || before[0].AtMs != base+2000 {
		t.Errorf("the slice handed out before changed: %v", instants(before))
	}
}

// The line the recorder is still writing doesn't go in, and isn't lost:
// it's read whole when it ends.
func TestDayObjectsWaitsForTheLineToEnd(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime().UnixMilli()
	day := baseTime().Format(DayLayout)
	appendObjeto(t, c, base, "pessoa")

	f, err := os.OpenFile(c.EventsPath(day), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(`{"instanteMs":1786201260000,"familia":"vei`); err != nil {
		t.Fatal(err)
	}

	got, _ := c.DayObjects(day)
	if len(got) != 1 {
		t.Fatalf("the half line went in: %v", instants(got))
	}

	if _, err := f.WriteString(`culo","classe":"car","score":0.8}` + "\n"); err != nil {
		t.Fatal(err)
	}
	got, _ = c.DayObjects(day)
	if len(got) != 2 || got[1].Family != "veiculo" {
		t.Errorf("the finished line didn't go in whole: %+v", got)
	}
}

// Retention rewrites the file with a rename (trimEvents), and it may even
// grow again past the old size: what was memoized no longer holds.
func TestDayObjectsNoticesTheRewrite(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime().UnixMilli()
	day := baseTime().Format(DayLayout)
	appendObjeto(t, c, base, "pessoa")
	appendObjeto(t, c, base+60_000, "pessoa")
	if _, err := c.DayObjects(day); err != nil {
		t.Fatal(err)
	}

	if err := c.trimEvents(day, base+1); err != nil {
		t.Fatal(err)
	}
	for i := int64(2); i < 6; i++ {
		appendObjeto(t, c, base+i*60_000, "animal")
	}

	got, err := c.DayObjects(day)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 || got[0].AtMs != base+60_000 {
		t.Errorf("after the rewrite: %v", instants(got))
	}

	if err := os.Remove(c.EventsPath(day)); err != nil { // retention took the day
		t.Fatal(err)
	}
	if got, _ := c.DayObjects(day); len(got) != 0 {
		t.Errorf("the deleted day stayed in memory: %v", instants(got))
	}
}

// A day deleted by retention leaves memory right away, not only on
// restart: it disappears from Days(), and DayObjects is never called for it
// again.
func TestDayObjectsReleasesTheDeletedDay(t *testing.T) {
	c := newTestCamera(t)
	base := baseTime()
	yesterday := base.AddDate(0, 0, -1)
	for _, d := range []time.Time{yesterday, base} {
		e := entryAt(d, 30_000, 1000)
		writeSegmentFile(t, c, e)
		if err := c.Append(e); err != nil {
			t.Fatal(err)
		}
		appendObjeto(t, c, d.UnixMilli(), "pessoa")
		if _, err := c.DayObjects(d.Format(DayLayout)); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := c.DropDay(yesterday.Format(DayLayout)); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.memo.days[yesterday.Format(DayLayout)]; ok {
		t.Error("the day deleted by DropDay stayed in memory")
	}
	if _, ok := c.memo.days[base.Format(DayLayout)]; !ok {
		t.Error("DropDay also released the day that stayed")
	}

	if _, err := c.Purge(); err != nil {
		t.Fatal(err)
	}
	if len(c.memo.days) != 0 {
		t.Errorf("Purge left %d day(s) in memory", len(c.memo.days))
	}
}
