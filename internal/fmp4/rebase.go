package fmp4

import (
	"encoding/binary"
	"errors"
)

// RebaseMoof rewrites, in place, the tfdt of each traf by subtracting
// bases[trackID].
//
// It's what makes each segment a self-contained file. go2rtc delivers a
// continuous tfdt since the start of the connection, so without this the
// second segment would start at t=24s, the third at t=48s and so on: the file
// would open, but advertise an ever-growing duration and a gap at the start.
//
// The box keeps its original size and version - only the value changes -, so
// no box size above it needs to be recomputed.
func RebaseMoof(moof []byte, bases map[uint32]uint64) error {
	deltas := make(map[uint32]int64, len(bases))
	for id, base := range bases {
		deltas[id] = -int64(base)
	}
	return ShiftMoof(moof, deltas)
}

// ShiftMoof adds a signed offset to the tfdt of each traf, in place.
//
// It's the recording mechanism seen from the other side: recording subtracts to
// zero the segment, exporting adds to splice segments into one continuous
// timeline. Each track has its own delta because the timescales differ.
func ShiftMoof(moof []byte, deltas map[uint32]int64) error {
	hdrLen, size, ok := boxHeaderLen(moof)
	if !ok || boxType(moof) != "moof" {
		return errors.New("fmp4: expected a moof box")
	}

	return walk(moof[hdrLen:size], func(typ string, traf []byte) error {
		if typ != "traf" {
			return nil
		}

		var (
			trackID uint32
			tfdt    []byte
		)
		// walk returns subslices of the same array, so writing to tfdt already
		// changes the original buffer.
		if err := walk(traf, func(typ string, body []byte) error {
			switch typ {
			case "tfhd":
				if id, ok := be32(body, 4); ok {
					trackID = id
				}
			case "tfdt":
				tfdt = body
			}
			return nil
		}); err != nil {
			return err
		}

		if tfdt == nil {
			return nil
		}
		delta, ok := deltas[trackID]
		if !ok {
			return nil
		}
		return shiftTfdt(tfdt, delta)
	})
}

func shiftTfdt(body []byte, delta int64) error {
	if len(body) < 4 {
		return errors.New("fmp4: truncated tfdt")
	}
	if body[0] == 1 {
		v, ok := be64(body, 4)
		if !ok {
			return errors.New("fmp4: truncated tfdt v1")
		}
		binary.BigEndian.PutUint64(body[4:12], addClamp(v, delta))
		return nil
	}
	v, ok := be32(body, 4)
	if !ok {
		return errors.New("fmp4: truncated tfdt v0")
	}
	binary.BigEndian.PutUint32(body[4:8], uint32(addClamp(uint64(v), delta)))
	return nil
}

// addClamp avoids underflow: the audio track may be a few milliseconds behind
// the video keyframe that opened the segment, and a negative uint64 would turn
// into an astronomical timestamp.
func addClamp(v uint64, delta int64) uint64 {
	if delta < 0 {
		d := uint64(-delta)
		if v < d {
			return 0
		}
		return v - d
	}
	return v + uint64(delta)
}

// ScaleTime converts an instant from one timescale to another. It derives each
// track's base from the video keyframe that opened the segment, keeping the
// real offset between audio and video instead of zeroing both tracks
// independently.
func ScaleTime(t uint64, from, to uint32) uint64 {
	if from == 0 || from == to {
		return t
	}
	return uint64(float64(t) / float64(from) * float64(to))
}
