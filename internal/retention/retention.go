// Package retention keeps the disk under control by deleting the oldest
// recordings.
//
// There are three limits, applied in this order:
//
//  1. quota in MB per camera - the main one, it's what the user configures
//  2. maximum age per camera - optional, for whoever thinks in days, not GB
//  3. minimum free disk, global - the safety net
//
// The third exists because the sum of the quotas is easy to get wrong: with 9
// cameras each one has a different bitrate, and it's enough for the user to
// overestimate for the disk to fill up. Filling the disk is worse than losing
// old recordings, so this limit ignores the individual quotas and evicts the
// oldest day among the registered cameras.
//
// Recordings of a removed camera (orphans) aren't covered by any of the three,
// not even the third, on purpose: whoever removes the camera without checking
// "also delete the recordings" kept them, and dwnvr doesn't delete on its own
// what the user kept - not even when the alternative is deleting recordings
// of a live camera. What it does is not hide them: the disk warnings say how
// much is in orphans and where to delete them, and the Diagnostics screen
// shows them separately.
package retention

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// Interval is the time between passes. One minute is much more often than
// the quota needs, but keeps the reaction fast when the disk gets tight.
const Interval = time.Minute

type Manager struct {
	cfg   *config.Config
	store *store.Store
	log   *slog.Logger

	// cameras is queried on every pass, not copied at startup: cameras can be
	// added, changed and removed with dwnvr running, and a new quota that only
	// applied after a restart would be a trap.
	cameras func() []config.Camera

	// now and free are the clock and the free-disk reading. Only tests swap
	// them: with them, a day can be aged or the disk tightened without waiting or
	// filling a real disk.
	now  func() time.Time
	free func(path string) (int64, error)

	// belowSince is when the pass first saw the disk below the minimum; zero
	// while it's above. It's the "since" of the Diagnostics warning, accurate to
	// one pass.
	mu         sync.Mutex
	belowSince time.Time
}

func New(cfg *config.Config, st *store.Store, cameras func() []config.Camera, log *slog.Logger) *Manager {
	return &Manager{cfg: cfg, store: st, cameras: cameras, log: log,
		now: time.Now, free: FreeBytes}
}

// resolved returns the current configuration with the defaults already applied.
func (m *Manager) resolved() []config.Camera {
	cams := m.cameras()
	out := make([]config.Camera, 0, len(cams))
	for _, c := range cams {
		out = append(out, m.cfg.Resolve(c))
	}
	return out
}

// Run applies retention periodically until the context is canceled.
func (m *Manager) Run(ctx context.Context) {
	// One pass right at startup: if the process was down with the disk full,
	// there's no point waiting for the first tick to react.
	if err := m.Enforce(); err != nil {
		m.log.Error("retention failed", "err", err)
	}

	t := time.NewTicker(Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := m.Enforce(); err != nil {
				m.log.Error("retention failed", "err", err)
			}
		}
	}
}

// Enforce runs a full pass of the three limits.
func (m *Manager) Enforce() error {
	for _, cam := range m.resolved() {
		if err := m.enforceQuota(cam); err != nil {
			return err
		}
		if err := m.enforceMaxDays(cam); err != nil {
			return err
		}
	}
	return m.enforceFreeSpace()
}

func (m *Manager) enforceQuota(cam config.Camera) error {
	idx := m.store.Camera(cam.ID)
	quota := cam.QuotaMB << 20
	total := idx.TotalBytes()
	if quota <= 0 || total <= quota {
		return nil
	}

	want := total - quota
	freed, err := idx.EvictOldest(want)
	if err != nil {
		return err
	}
	// A full camera deletes each minute's excess, forever: it's the normal
	// regime, and at Info it would be one line per camera per minute. In KB
	// because one minute's excess is one or two segments, and in MB it would be
	// zero.
	attrs := []any{"cam", cam.ID,
		"used_mb", total >> 20, "quota_mb", cam.QuotaMB, "freed_kb", freed >> 10}
	if freed < want {
		m.log.Warn("quota exceeded, but there weren't enough recordings to delete",
			append(attrs, "short_kb", (want-freed)>>10)...)
		return nil
	}
	m.log.Debug("quota exceeded, evicting", attrs...)
	return nil
}

func (m *Manager) enforceMaxDays(cam config.Camera) error {
	if cam.MaxDays <= 0 {
		return nil
	}
	idx := m.store.Camera(cam.ID)
	cutoff := m.now().AddDate(0, 0, -cam.MaxDays).Format(store.DayLayout)

	for _, d := range idx.Days() {
		if d.Day >= cutoff {
			break // Days() comes sorted, so from here on it's all recent
		}
		freed, err := idx.DropDay(d.Day)
		if err != nil {
			return err
		}
		m.log.Info("day past the maximum age, removed",
			"cam", cam.ID, "day", d.Day, "freed_mb", freed>>20)
	}
	return nil
}

// BelowMinimumSince says since when the disk has been below the minimum free
// space, or zero if it isn't.
func (m *Manager) BelowMinimumSince() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.belowSince
}

// recordDisk keeps the disk's transition: only the first pass below the
// minimum marks the instant, and the first above clears it.
func (m *Manager) recordDisk(below bool, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case !below:
		m.belowSince = time.Time{}
	case m.belowSince.IsZero():
		m.belowSince = now
	}
}

// enforceFreeSpace is the global safety net: it evicts the oldest day of any
// registered camera until the disk is back at the acceptable minimum.
func (m *Manager) enforceFreeSpace() error {
	minFree := m.cfg.Storage.MinFreeMB << 20
	if minFree <= 0 {
		m.recordDisk(false, m.now())
		return nil
	}

	// Read once, and only with the disk already below the minimum: with plenty
	// of disk, which is the normal case, the pass doesn't scan storage for
	// orphans.
	orphans := int64(-1)
	withOrphans := func(attrs []any) []any {
		if orphans < 0 {
			orphans = m.orphanBytes()
		}
		if orphans == 0 {
			return attrs
		}
		return append(attrs, "orphans_mb", orphans>>20,
			"delete_at", "Câmeras > Gravações de câmeras removidas")
	}

	for {
		free, err := m.free(m.store.Root())
		if err != nil {
			return err
		}
		m.recordDisk(free < minFree, m.now())
		if free >= minFree {
			return nil
		}

		cam, day, ok := m.oldestDay()
		if !ok {
			attrs := withOrphans([]any{"free_mb", free >> 20, "min_mb", m.cfg.Storage.MinFreeMB})
			msg := "disk below the minimum free space, but there's nothing left to evict"
			if orphans > 0 {
				// The old sentence would be false: there are recordings there, only
				// of removed cameras, which retention doesn't delete.
				msg = "disk below the minimum free space, and only recordings of removed cameras are left, which retention doesn't delete"
			}
			m.log.Warn(msg, attrs...)
			return nil
		}

		freed, err := m.store.Camera(cam).DropDay(day)
		if err != nil {
			return err
		}
		m.log.Warn("disk below the minimum free space, evicting the oldest day",
			withOrphans([]any{"free_mb", free >> 20, "min_mb", m.cfg.Storage.MinFreeMB,
				"cam", cam, "day", day, "freed_mb", freed >> 20})...)

		if freed == 0 {
			// Nothing was freed: without this the loop would spin forever when the
			// space was being taken by something outside dwnvr.
			return nil
		}
	}
}

// orphanBytes sums what's on disk from removed cameras. It only serves the
// warning: a scan failure becomes zero, and the warning goes out without the
// field.
func (m *Manager) orphanBytes() int64 {
	registered := map[string]bool{}
	for _, c := range m.cameras() {
		registered[c.ID] = true
	}
	total, err := m.store.OrphanBytes(registered)
	if err != nil {
		m.log.Error("listing orphan recordings", "err", err)
		return 0
	}
	return total
}

// oldestDay finds the oldest day among the registered cameras.
func (m *Manager) oldestDay() (cam, day string, ok bool) {
	for _, c := range m.resolved() {
		days := m.store.Camera(c.ID).Days()
		if len(days) == 0 {
			continue
		}
		if !ok || days[0].Day < day {
			cam, day, ok = c.ID, days[0].Day, true
		}
	}
	return cam, day, ok
}
