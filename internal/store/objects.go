package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"sync"
)

// dayObjects is what's memoized from an events file: only the object marks,
// and how far the file has been read.
type dayObjects struct {
	// info is the file as it was on the last read. A different inode means it
	// was rewritten (retention trims with a rename), and then what's memoized no
	// longer holds.
	info os.FileInfo
	// consumed is how far the file has been read: always the end of a whole
	// line, never the middle of the one the recorder may be writing.
	consumed int64
	objects  []Event
}

// objectsMemo keeps the days already read, per camera. It stays outside
// Camera.mu on purpose: reading an events file while holding that lock would
// stall the recorder, which takes it on every segment.
type objectsMemo struct {
	mu   sync.Mutex
	days map[string]*dayObjects
}

// objectMarker is what tells an object line from an onset line without
// decoding the JSON: the onset line only has `instanteMs`. In a day there are
// thousands of onsets for dozens of objects, and Unmarshal is what costs.
var objectMarker = []byte(`"familia"`)

// DayObjects returns a day's object marks, in instant order. It's what the
// Detections screen pages through.
//
// The read is memoized per day, and that's what keeps scrolling cheap:
// without it, each page rereads and decodes thousands of onsets from nine
// cameras to find a few dozen marks. A past day is read only once. The current
// day grows, and then only the new part of the file is read - it's
// append-only.
//
// The returned slice is read-only, and stays valid afterwards: the next read
// builds a new slice instead of touching this one.
func (c *Camera) DayObjects(day string) ([]Event, error) {
	c.memo.mu.Lock()
	defer c.memo.mu.Unlock()

	path := c.EventsPath(day)
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		// A day without detection, or already taken by retention.
		delete(c.memo.days, day)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	m := c.memo.days[day]
	if m != nil && os.SameFile(m.info, info) && info.Size() == m.consumed {
		return m.objects, nil
	}
	if m == nil || !os.SameFile(m.info, info) || info.Size() < m.consumed {
		m = &dayObjects{}
	}

	fresh, consumed, err := readObjects(path, m.consumed)
	if err != nil {
		return nil, err
	}
	if len(fresh) > 0 {
		// A new slice, not an append to the old one: whoever got the previous one
		// may be reading it, and sorting in place would move things under them. The
		// order has to be redone because the mark goes into the file when the object
		// detector answers, and the queue may answer out of the onsets' order.
		all := make([]Event, 0, len(m.objects)+len(fresh))
		all = append(append(all, m.objects...), fresh...)
		sort.SliceStable(all, func(i, j int) bool { return all[i].AtMs < all[j].AtMs })
		m = &dayObjects{objects: all}
	}
	m.info = info
	m.consumed = consumed
	if c.memo.days == nil {
		c.memo.days = map[string]*dayObjects{}
	}
	c.memo.days[day] = m
	return m.objects, nil
}

// forgetObjects releases from memory the marks of a deleted day, or of all
// of them with an empty day. Without this, a day taken by retention stayed
// memoized until the process restarted: it leaves Days(), and DayObjects,
// which is what notices the missing file, is never called for it again.
func (c *Camera) forgetObjects(day string) {
	c.memo.mu.Lock()
	defer c.memo.mu.Unlock()
	if day == "" {
		c.memo.days = nil
		return
	}
	delete(c.memo.days, day)
}

// readObjects reads the object marks from the file starting at `since`,
// and returns how far it read. The last line without '\n' is left for next
// time: it's the one the recorder may be in the middle of writing.
func readObjects(path string, since int64) ([]Event, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, since, err
	}
	defer f.Close()
	if _, err := f.Seek(since, io.SeekStart); err != nil {
		return nil, since, err
	}

	var out []Event
	consumed := since
	r := bufio.NewReaderSize(f, 64<<10)
	for {
		line, err := r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// A line longer than 64 KB is no mark at all: it's garbage from a crash.
			// Drop it to its end.
			consumed += int64(len(line))
			for errors.Is(err, bufio.ErrBufferFull) {
				line, err = r.ReadSlice('\n')
				consumed += int64(len(line))
			}
			continue
		}
		if err == io.EOF {
			return out, consumed, nil // what's left has no '\n' yet
		}
		if err != nil {
			return nil, since, err
		}
		consumed += int64(len(line))
		if !bytes.Contains(line, objectMarker) {
			continue
		}
		var ev Event
		// Same tolerance as LoadEvents: a crash leaves a cut line or null bytes,
		// and the line goes away.
		if json.Unmarshal(line, &ev) != nil || !ev.IsObject() {
			continue
		}
		out = append(out, ev)
	}
}
