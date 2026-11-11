package fmp4

import "errors"

// sample_flags bits defined in ISO/IEC 14496-12 §8.8.3.1, read as a
// big-endian uint32:
//
//	bits 6-7  sample_depends_on        (2 = depends on no others = keyframe)
//	bit  15   sample_is_non_sync_sample (1 = NOT a sync point)
//
// go2rtc writes exactly these values in pkg/iso/atoms.go:
//
//	SampleVideoIFrame    = 0x2000000
//	SampleVideoNonIFrame = 0x1000000 | 0x10000
const (
	sampleIsNonSync    = 0x00010000
	sampleDependsOnMsk = 0x03000000
	sampleDependsOnNo  = 0x02000000 // depends on no other samples
)

// isSyncSample decides whether a sample is a valid entry point for decoding.
//
// The primary test is the sample_is_non_sync_sample bit, which is what the
// standard defines. Some muxers leave that bit clear on everything and only
// fill in sample_depends_on; so, when the non-sync bit is clear, we still
// require depends_on not to say explicitly "depends on others".
func isSyncSample(flags uint32) bool {
	if flags&sampleIsNonSync != 0 {
		return false
	}
	if dep := flags & sampleDependsOnMsk; dep != 0 && dep != sampleDependsOnNo {
		return false
	}
	return true
}

// Fragment summarizes a moof+mdat pair: enough to decide where to cut a
// segment and to feed the index, without looking at a single media byte.
type Fragment struct {
	TrackID uint32
	// BaseDecodeTime is the tfdt, in the track's timescale.
	BaseDecodeTime uint64
	HasBaseTime    bool
	// Keyframe is only true for the video track given to ParseMoof.
	Keyframe bool
	// SampleCount is how many samples the fragment carries (go2rtc emits 1).
	SampleCount uint32

	// Duration is the sum of the sample durations, in the track's timescale.
	//
	// Without it we only know WHEN the last frame starts, not when it ends.
	// The difference is one frame - irrelevant for display, decisive for
	// splicing segments: without adding it, the next segment starts on top of
	// the previous one's last frame instead of after it, and the DTS goes
	// backwards at the splice.
	Duration uint64
}

// EndTime is the instant the fragment ends, in the track's timescale.
func (f Fragment) EndTime() uint64 { return f.BaseDecodeTime + f.Duration }

var errNoTraf = errors.New("fmp4: moof without traf")

// ParseMoof reads a complete moof box (with header) and summarizes the traf of
// track videoTrackID. If the moof belongs to another track (audio), it returns
// the TrackID found with Keyframe=false.
func ParseMoof(moof []byte, videoTrackID uint32) (Fragment, error) {
	var f Fragment

	hdrLen, size, ok := boxHeaderLen(moof)
	if !ok || boxType(moof) != "moof" {
		return f, errors.New("fmp4: expected a moof box")
	}

	found := false
	err := walk(moof[hdrLen:size], func(typ string, body []byte) error {
		if typ != "traf" {
			return nil
		}
		found = true
		return parseTraf(body, videoTrackID, &f)
	})
	if err != nil {
		return f, err
	}
	if !found {
		return f, errNoTraf
	}
	return f, nil
}

func parseTraf(traf []byte, videoTrackID uint32, f *Fragment) error {
	var (
		defaultSampleFlags uint32
		haveDefaultFlags   bool
		trunFlags          uint32
		haveTrunFlags      bool
		defaultSampleDur   uint32
	)

	err := walk(traf, func(typ string, body []byte) error {
		switch typ {
		case "tfhd":
			// version(1) + flags(3), track_ID(4), then optional fields in the
			// order of the flag bits.
			id, ok := be32(body, 4)
			if !ok {
				return errors.New("fmp4: truncated tfhd")
			}
			f.TrackID = id

			flags, _ := be32(body, 0)
			flags &= 0x00FFFFFF
			off := 8
			if flags&0x000001 != 0 { // base_data_offset
				off += 8
			}
			if flags&0x000002 != 0 { // sample_description_index
				off += 4
			}
			if flags&0x000008 != 0 { // default_sample_duration
				if v, ok := be32(body, off); ok {
					defaultSampleDur = v
				}
				off += 4
			}
			if flags&0x000010 != 0 { // default_sample_size
				off += 4
			}
			if flags&0x000020 != 0 { // default_sample_flags
				if v, ok := be32(body, off); ok {
					defaultSampleFlags, haveDefaultFlags = v, true
				}
			}

		case "tfdt":
			if len(body) == 0 {
				return errors.New("fmp4: empty tfdt")
			}
			if body[0] == 1 {
				if v, ok := be64(body, 4); ok {
					f.BaseDecodeTime, f.HasBaseTime = v, true
				}
			} else {
				if v, ok := be32(body, 4); ok {
					f.BaseDecodeTime, f.HasBaseTime = uint64(v), true
				}
			}

		case "trun":
			flags, ok := be32(body, 0)
			if !ok {
				return errors.New("fmp4: truncated trun")
			}
			flags &= 0x00FFFFFF
			count, _ := be32(body, 4)
			f.SampleCount = count

			off := 8
			if flags&0x000001 != 0 { // data_offset
				off += 4
			}
			if flags&0x000004 != 0 { // first_sample_flags
				if v, ok := be32(body, off); ok {
					trunFlags, haveTrunFlags = v, true
				}
				off += 4
			}
			// Walks the sample table once, adding up durations and - if they
			// didn't come in first_sample_flags - taking the flags of the
			// first sample, which is what classifies the fragment.
			entrySize := 0
			for _, f := range []uint32{0x000100, 0x000200, 0x000400, 0x000800} {
				if flags&f != 0 {
					entrySize += 4
				}
			}
			for i := range count {
				p := off + int(i)*entrySize
				if flags&0x000100 != 0 { // sample_duration
					if v, ok := be32(body, p); ok {
						f.Duration += uint64(v)
					}
					p += 4
				} else {
					f.Duration += uint64(defaultSampleDur)
				}
				if flags&0x000200 != 0 { // sample_size
					p += 4
				}
				if i == 0 && !haveTrunFlags && flags&0x000400 != 0 {
					if v, ok := be32(body, p); ok {
						trunFlags, haveTrunFlags = v, true
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if f.TrackID != videoTrackID {
		return nil
	}

	switch {
	case haveTrunFlags:
		f.Keyframe = isSyncSample(trunFlags)
	case haveDefaultFlags:
		f.Keyframe = isSyncSample(defaultSampleFlags)
	default:
		// With no flags at all in the fragment the decision would fall to the
		// trex; the caller handles that by passing the track default via
		// DefaultSampleFlags.
		f.Keyframe = false
	}
	return nil
}
