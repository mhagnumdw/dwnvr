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

// fixedNow is the tests' clock: 12:00 local time, far from midnight, so "N
// days ago" doesn't depend on the hour nor on the time zone of whoever runs it.
var fixedNow = time.Date(2026, 8, 8, 12, 0, 0, 0, time.Local)

// daysAgo is noon N days before the tests' clock.
func daysAgo(n int) time.Time { return fixedNow.AddDate(0, 0, -n) }

func dayOf(n int) string { return daysAgo(n).Format(store.DayLayout) }

// newManager builds retention over an empty store in a temp directory, with
// the fixed clock. The free disk is defined by each test that uses it.
func newManager(t *testing.T, minFreeMB int64, cams *[]config.Camera) (*Manager, *store.Store) {
	t.Helper()
	st := store.New(t.TempDir())
	cfg := &config.Config{Storage: config.Storage{MinFreeMB: minFreeMB}}
	m := New(cfg, st, func() []config.Camera { return *cams }, slog.New(slog.DiscardHandler))
	m.now = func() time.Time { return fixedNow }
	m.free = func(string) (int64, error) {
		t.Fatal("this test shouldn't query the disk")
		return 0, nil
	}
	return m, st
}

// record writes `n` segments of `size` bytes, one per minute starting at
// `start`. The file is sparse: it has the right size without taking disk.
func record(t *testing.T, st *store.Store, cam string, start time.Time, n int, size int64) {
	t.Helper()
	c := st.Camera(cam)
	for i := range n {
		e := store.Entry{StartMs: start.Add(time.Duration(i) * time.Minute).UnixMilli(), DurMs: 60_000, Size: size}
		if err := c.EnsureDirs(e.Day()); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(c.SegmentPath(e.StartMs))
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(size); err != nil {
			t.Fatal(err)
		}
		f.Close()
		if err := c.Append(e); err != nil {
			t.Fatal(err)
		}
	}
}

// diskOf simulates a disk of `capacity` bytes where only dwnvr writes: free
// is what the cameras don't take yet, and it drops with each deleted
// recording.
func diskOf(st *store.Store, capacity int64, cams ...string) func(string) (int64, error) {
	return func(string) (int64, error) {
		used := int64(0)
		for _, c := range cams {
			used += st.Camera(c).TotalBytes()
		}
		return capacity - used, nil
	}
}

func dayList(st *store.Store, cam string) []string {
	var out []string
	for _, d := range st.Camera(cam).Days() {
		out = append(out, d.Day)
	}
	return out
}

func checkDays(t *testing.T, st *store.Store, cam string, want ...string) {
	t.Helper()
	got := dayList(st, cam)
	if len(got) != len(want) {
		t.Fatalf("%s kept the days %v, want %v", cam, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s kept the days %v, want %v", cam, got, want)
		}
	}
}

// A camera over its quota loses its oldest until it fits, and only it: one
// camera's quota must not delete another's recordings.
func TestQuotaDeletesOnlyFromTheCameraOver(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 3}, {ID: "cam_b", QuotaMB: 10}}
	m, st := newManager(t, 0, &cams)
	record(t, st, "cam_a", daysAgo(0), 5, mb)
	record(t, st, "cam_b", daysAgo(0), 5, mb)

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}

	a := st.Camera("cam_a")
	if got := a.TotalBytes(); got != 3*mb {
		t.Errorf("cam_a takes %d MB, want the quota, 3", got/mb)
	}
	// The first two segments went away, not any two.
	if got, want := a.OldestMs(), daysAgo(0).Add(2*time.Minute).UnixMilli(); got != want {
		t.Errorf("cam_a's oldest starts at %d, want %d (the third)", got, want)
	}
	if got := st.Camera("cam_b").TotalBytes(); got != 5*mb {
		t.Errorf("cam_b takes %d MB, want the 5 intact", got/mb)
	}
}

// A day beyond maxDays goes away whole; the limit day itself stays.
func TestMaxAgeDeletesTheDaysBeyond(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 100, MaxDays: 3}}
	m, st := newManager(t, 0, &cams)
	for d := 5; d >= 0; d-- {
		record(t, st, "cam_a", daysAgo(d), 1, mb)
	}

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	checkDays(t, st, "cam_a", dayOf(3), dayOf(2), dayOf(1), dayOf(0))
}

// maxDays zero means "no age limit": whoever thinks in GB doesn't lose
// recordings for being old.
func TestMaxAgeZeroDeletesNothing(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 100}}
	m, st := newManager(t, 0, &cams)
	for d := 400; d >= 0; d -= 100 {
		record(t, st, "cam_a", daysAgo(d), 1, mb)
	}

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	checkDays(t, st, "cam_a", dayOf(400), dayOf(300), dayOf(200), dayOf(100), dayOf(0))
}

// With the disk below the minimum, the oldest day of any camera goes, one at
// a time, until the disk recovers; the quotas hold nothing back.
func TestTightDiskDeletesTheOldestDayOfAnyCamera(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 100}, {ID: "cam_b", QuotaMB: 100}}
	m, st := newManager(t, 5, &cams)
	for _, d := range []int{2, 1, 0} {
		record(t, st, "cam_a", daysAgo(d), 2, mb)
	}
	for _, d := range []int{3, 0} {
		record(t, st, "cam_b", daysAgo(d), 2, mb)
	}
	// 10 MB taken on a 12 MB disk: 2 left, and the minimum is 5. Deleting
	// day 3 of cam_b gives 4, still too little; day 2 of cam_a gives 6.
	m.free = diskOf(st, 12*mb, "cam_a", "cam_b")

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	checkDays(t, st, "cam_a", dayOf(1), dayOf(0))
	checkDays(t, st, "cam_b", dayOf(0))
	if !m.BelowMinimumSince().IsZero() {
		t.Error("the disk went back to the minimum and the warning stayed")
	}
}

// The quota runs before the disk. It trims segment by segment, and the disk
// deletes a whole day: in the reverse order, a squeeze the quota would solve
// with 1 MB would cost the whole day.
func TestQuotaRunsBeforeDisk(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 5}}
	m, st := newManager(t, 5, &cams)
	for _, d := range []int{2, 1, 0} {
		record(t, st, "cam_a", daysAgo(d), 2, mb)
	}
	// 6 MB taken, 4.5 free: below the minimum of 5 until the quota takes out 1 MB.
	m.free = diskOf(st, 10*mb+mb/2, "cam_a")

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	checkDays(t, st, "cam_a", dayOf(2), dayOf(1), dayOf(0))
	if got := st.Camera("cam_a").TotalBytes(); got != 5*mb {
		t.Errorf("cam_a takes %d MB, want 5: only the quota should have deleted", got/mb)
	}
}

// A camera added or changed with dwnvr running applies on the next pass,
// without a restart.
func TestNewOrChangedCameraAppliesOnTheNextPass(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 10}}
	m, st := newManager(t, 0, &cams)
	record(t, st, "cam_a", daysAgo(0), 5, mb)
	record(t, st, "cam_b", daysAgo(0), 5, mb)

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	if got := st.Camera("cam_a").TotalBytes(); got != 5*mb {
		t.Fatalf("cam_a takes %d MB before changing the quota, want 5", got/mb)
	}

	cams = []config.Camera{{ID: "cam_a", QuotaMB: 2}, {ID: "cam_b", QuotaMB: 3}}
	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	if got := st.Camera("cam_a").TotalBytes(); got != 2*mb {
		t.Errorf("cam_a takes %d MB, want the new quota, 2", got/mb)
	}
	if got := st.Camera("cam_b").TotalBytes(); got != 3*mb {
		t.Errorf("cam_b takes %d MB, want the new camera's quota, 3", got/mb)
	}
}

// minFreeMB zero turns the safety net off: the disk isn't even queried.
func TestNoMinimumDoesNotQueryTheDisk(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 100}}
	m, st := newManager(t, 0, &cams)
	record(t, st, "cam_a", daysAgo(0), 1, mb)

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	if !m.BelowMinimumSince().IsZero() {
		t.Error("without a configured minimum there's no disk warning")
	}
}

// A disk taken by something outside dwnvr: deleting doesn't give the space
// back. The pass deletes everything that belongs to dwnvr and stops, instead
// of spinning forever, and the warning stays on.
func TestDiskThatDoesNotRecoverDeletesEverythingAndStops(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 100}}
	m, st := newManager(t, 5, &cams)
	for _, d := range []int{1, 0} {
		record(t, st, "cam_a", daysAgo(d), 1, mb)
	}
	m.free = func(string) (int64, error) { return mb, nil }

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	checkDays(t, st, "cam_a")
	if got := m.BelowMinimumSince(); !got.Equal(fixedNow) {
		t.Errorf("disk warning since %v, want %v", got, fixedNow)
	}
}

// Recordings of a removed camera don't go even with the disk tight: the live
// camera pays, and the warnings say how much is in orphans. In the end, with
// the registered ones drained, the warning must not say nothing is left.
func TestTightDiskDoesNotDeleteOrphansButWarns(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 100}}
	m, st := newManager(t, 5, &cams)
	var log bytes.Buffer
	m.log = slog.New(slog.NewTextHandler(&log, nil))
	record(t, st, "cam_velha", daysAgo(5), 3, mb)
	for _, d := range []int{1, 0} {
		record(t, st, "cam_a", daysAgo(d), 1, mb)
	}
	m.free = func(string) (int64, error) { return mb, nil }

	if err := m.Enforce(); err != nil {
		t.Fatal(err)
	}
	checkDays(t, st, "cam_a")
	checkDays(t, st, "cam_velha", dayOf(5))

	out := log.String()
	if got := strings.Count(out, "orphans_mb=3"); got != 3 {
		t.Errorf("orphans_mb=3 in %d warnings, want in all 3 (2 evicted days and the end):\n%s", got, out)
	}
	if strings.Contains(out, "nothing left to evict") {
		t.Errorf("the warning said nothing was left, with 3 MB of orphans:\n%s", out)
	}
	if !strings.Contains(out, "only recordings of removed cameras are left") {
		t.Errorf("missing the warning that only orphans are left:\n%s", out)
	}
}

// Without knowing the free space, the pass deletes nothing blindly: it returns the error.
func TestUnreadableDiskReturnsTheError(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 100}}
	m, st := newManager(t, 5, &cams)
	record(t, st, "cam_a", daysAgo(0), 1, mb)
	failure := errors.New("statfs falhou")
	m.free = func(string) (int64, error) { return 0, failure }

	if err := m.Enforce(); !errors.Is(err, failure) {
		t.Fatalf("Enforce returned %v, want the disk error", err)
	}
	checkDays(t, st, "cam_a", dayOf(0))
}

// Run does one pass right at startup, before the first tick, and leaves when
// the context is done.
func TestRunPassesAtStartup(t *testing.T) {
	cams := []config.Camera{{ID: "cam_a", QuotaMB: 1}}
	m, st := newManager(t, 0, &cams)
	record(t, st, "cam_a", daysAgo(0), 3, mb)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	m.Run(ctx)

	if got := st.Camera("cam_a").TotalBytes(); got != mb {
		t.Errorf("cam_a takes %d MB after startup, want the quota, 1", got/mb)
	}
}

// The free space retention reads is a regular user's, and never exceeds
// the capacity.
func TestFreeBytesAndTotalBytes(t *testing.T) {
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
		t.Errorf("free %d, total %d", free, total)
	}
}

// The "since" of the disk below the minimum stays at the first pass that
// saw the problem, and goes away on the first that sees it solved.
func TestBelowMinimumSince(t *testing.T) {
	m := &Manager{}
	t0 := time.Now()

	m.recordDisk(false, t0)
	if !m.BelowMinimumSince().IsZero() {
		t.Fatal("a disk above the minimum shouldn't have a since")
	}

	m.recordDisk(true, t0.Add(time.Minute))
	m.recordDisk(true, t0.Add(2*time.Minute))
	if got := m.BelowMinimumSince(); !got.Equal(t0.Add(time.Minute)) {
		t.Errorf("since %v, want the first pass below (%v)", got, t0.Add(time.Minute))
	}

	m.recordDisk(false, t0.Add(3*time.Minute))
	if !m.BelowMinimumSince().IsZero() {
		t.Error("the disk recovered and since stayed")
	}
}
