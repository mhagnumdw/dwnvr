package fmp4

import (
	"errors"
	"fmt"
)

// The media handlers dwnvr cares about.
const (
	HandlerVideo = "vide"
	HandlerAudio = "soun"
)

// Track describes one track of the init segment.
type Track struct {
	ID        uint32
	Handler   string // "vide" or "soun"
	Timescale uint32 // units per second of this track's timestamps
	Codec     string // 4CC of the sample entry: hev1, hvc1, avc1, mp4a, fLaC, Opus…

	// Width and Height are the video track's dimensions, read from the SPS the
	// avcC/hvcC carries. They are zero on audio tracks and when the SPS isn't
	// readable.
	//
	// They deliberately don't come from the VisualSampleEntry width/height
	// fields: go2rtc fills those with what the camera ADVERTISES, and there
	// are cameras advertising 2560x1440 while streaming 1920x1080. See sps.go.
	Width  uint16
	Height uint16

	// NALLengthSize is the size of the NAL length prefix, declared in the
	// avcC/hvcC. Needed to scan the NALs of a fragment.
	NALLengthSize int

	// DefaultSampleFlags comes from the trex and is the last resort to
	// classify a sample when the fragment carries no flags of its own.
	DefaultSampleFlags uint32
}

// Movie is the result of reading an init segment (ftyp + moov).
type Movie struct {
	Tracks []Track
}

// VideoTrack returns the first video track. Only it decides where to cut a
// segment: audio samples are also flagged as "depends on no others" (go2rtc
// uses SampleAudio = sampleDependsOn2), so classifying keyframes without
// looking at the track would make every audio packet look like a valid cut
// point.
func (m *Movie) VideoTrack() (Track, bool) {
	for _, t := range m.Tracks {
		if t.Handler == HandlerVideo {
			return t, true
		}
	}
	return Track{}, false
}

func (m *Movie) HasAudio() bool {
	for _, t := range m.Tracks {
		if t.Handler == HandlerAudio {
			return true
		}
	}
	return false
}

// ParseMoov reads the complete moov box (with header) and extracts the tracks.
func ParseMoov(moov []byte) (*Movie, error) {
	hdrLen, size, ok := boxHeaderLen(moov)
	if !ok || boxType(moov) != "moov" {
		return nil, errors.New("fmp4: expected a moov box")
	}

	mv := &Movie{}
	trexFlags := map[uint32]uint32{}

	err := walk(moov[hdrLen:size], func(typ string, body []byte) error {
		switch typ {
		case "trak":
			t, err := parseTrak(body)
			if err != nil {
				return err
			}
			mv.Tracks = append(mv.Tracks, t)
		case "mvex":
			return walk(body, func(typ string, body []byte) error {
				if typ != "trex" {
					return nil
				}
				// trex: version+flags(4), track_ID(4), default_sample_description_index(4),
				//       default_sample_duration(4), default_sample_size(4), default_sample_flags(4)
				id, ok1 := be32(body, 4)
				fl, ok2 := be32(body, 20)
				if ok1 && ok2 {
					trexFlags[id] = fl
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	for i := range mv.Tracks {
		mv.Tracks[i].DefaultSampleFlags = trexFlags[mv.Tracks[i].ID]
	}
	if len(mv.Tracks) == 0 {
		return nil, errors.New("fmp4: moov without any track")
	}
	return mv, nil
}

// visualSampleEntryFixed is the size of the fixed part of a VisualSampleEntry,
// counting the box header: 8 for the header, 8 for the SampleEntry (reserved
// and data_reference_index) and 70 for the visual fields (pre_defined,
// dimensions, resolutions, frame_count, compressorname, depth). The child
// boxes start after that, the avcC/hvcC among them.
const visualSampleEntryFixed = 86

// videoConfig reads the SPS and the NAL prefix size from inside the avcC/hvcC
// of a video sample entry.
//
// An entry without a readable configuration returns zeros: not knowing the
// resolution doesn't invalidate the init, and refusing to record because of
// it would be out of proportion.
func videoConfig(codec string, entry []byte) (w, h uint16, nalLen int) {
	if len(entry) <= visualSampleEntryFixed {
		return 0, 0, 0
	}
	_ = walk(entry[visualSampleEntryFixed:], func(typ string, body []byte) error {
		var sps []byte
		switch typ {
		case "avcC":
			sps, nalLen = avcCSPS(body)
		case "hvcC":
			sps, nalLen = hvcCSPS(body)
		default:
			return nil
		}
		if sps != nil {
			w, h, _ = SPSSize(codec, sps)
		}
		return nil
	})
	return w, h, nalLen
}

// avcCSPS extracts the first SPS from an AVCDecoderConfigurationRecord.
//
//	version(1) profile(1) compat(1) level(1)
//	0b111111|lengthSizeMinusOne(1)  0b111|numOfSPS(1)
//	{ spsLength(2) sps }...
func avcCSPS(b []byte) ([]byte, int) {
	if len(b) < 7 {
		return nil, 0
	}
	nalLen := int(b[4]&0x03) + 1
	if b[5]&0x1f == 0 {
		return nil, nalLen
	}
	n, ok := be16(b, 6)
	if !ok || 8+int(n) > len(b) {
		return nil, nalLen
	}
	return b[8 : 8+int(n)], nalLen
}

// hvcCSPS extracts the SPS from an HEVCDecoderConfigurationRecord, whose
// parameter sets come grouped by NAL type after a 22-byte fixed header.
func hvcCSPS(b []byte) ([]byte, int) {
	if len(b) < 23 {
		return nil, 0
	}
	nalLen := int(b[21]&0x03) + 1

	p := 23
	for arrays := int(b[22]); arrays > 0; arrays-- {
		if p+3 > len(b) {
			return nil, nalLen
		}
		typ := int(b[p] & 0x3f)
		count, _ := be16(b, p+1)
		p += 3
		for ; count > 0; count-- {
			n, ok := be16(b, p)
			if !ok || p+2+int(n) > len(b) {
				return nil, nalLen
			}
			nal := b[p+2 : p+2+int(n)]
			if typ == nalTypeSPS265 {
				return nal, nalLen
			}
			p += 2 + int(n)
		}
	}
	return nil, nalLen
}

func parseTrak(trak []byte) (Track, error) {
	var t Track

	// The sample entry is kept to be read after the walk: its layout depends
	// on the track's handler, and the box order doesn't guarantee the hdlr is
	// seen before the stsd.
	var sampleEntry []byte

	err := walk(trak, func(typ string, body []byte) error {
		switch typ {
		case "tkhd":
			// version 0: creation(4) modification(4) track_ID(4)
			// version 1: creation(8) modification(8) track_ID(4)
			off := 12
			if len(body) > 0 && body[0] == 1 {
				off = 20
			}
			if id, ok := be32(body, off); ok {
				t.ID = id
			}
		case "mdia":
			return walk(body, func(typ string, body []byte) error {
				switch typ {
				case "mdhd":
					// same per-version layout variation as the tkhd
					off := 12
					if len(body) > 0 && body[0] == 1 {
						off = 20
					}
					if ts, ok := be32(body, off); ok {
						t.Timescale = ts
					}
				case "hdlr":
					// version+flags(4), pre_defined(4), handler_type(4)
					if len(body) >= 12 {
						t.Handler = string(body[8:12])
					}
				case "minf":
					return walk(body, func(typ string, body []byte) error {
						if typ != "stbl" {
							return nil
						}
						return walk(body, func(typ string, body []byte) error {
							if typ != "stsd" {
								return nil
							}
							// stsd: version+flags(4), entry_count(4), then the sample entries
							if len(body) >= 16 {
								t.Codec = boxType(body[8:])
								sampleEntry = body[8:]
							}
							return nil
						})
					})
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		return t, err
	}
	if t.Handler == HandlerVideo {
		t.Width, t.Height, t.NALLengthSize = videoConfig(t.Codec, sampleEntry)
	}
	if t.Timescale == 0 {
		return t, fmt.Errorf("fmp4: track %d without timescale", t.ID)
	}
	return t, nil
}
