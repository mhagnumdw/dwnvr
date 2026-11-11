package fmp4

import (
	"bytes"
	"encoding/binary"
)

// Fixing an init that describes a stream other than the one being sent.
//
// The hvcC/avcC that go2rtc writes may not have the stream's parameter sets.
// With H.265, when the camera doesn't send them in the SDP, go2rtc writes a
// fixed SPS from its own code that describes 2560x1440
// (docs/go2rtc-h265-parameter-sets.md); with H.264, some cameras have an SDP
// describing another size. The real ones come in-band, before each IDR.
// Desktop Chrome obeys the in-band ones. The hardware decoder on some phones
// sets itself up from the init and draws the 640x360 frame in the corner of a
// 2560x1440 surface: the image comes out at 1/4 of the size, stuck in the top
// left corner.
//
// The fix replaces the init's parameter sets with the in-band ones and
// corrects the dimensions in the sample entry and in the tkhd. The media bytes
// stay untouched.

// NAL types of the parameter sets. Each codec's SPS is already in sps.go.
const (
	nalTypePPS264 = 8
	nalTypeVPS265 = 32
	nalTypePPS265 = 34
)

// tkhdBodyV0 and tkhdBodyV1 are the size of the tkhd body up to the end of
// width/height, its last two fields (16.16 each), in versions 0 and 1.
const (
	tkhdBodyV0 = 84
	tkhdBodyV1 = 96
)

// FixInit returns the init with the parameter sets that came in-band in the
// keyframe, when their SPS resolution disagrees with the init's. With no
// in-band SPS, or with the same resolution, it returns false and the original
// init stands.
//
// keyframe is the mdat content of the connection's first keyframe, where the
// parameter sets show up right before the IDR.
//
// It only fixes when the resolution disagrees, not when the init lacks a PPS:
// the decoder already handles the in-band PPS, and touching every init would
// change the generation of every camera with no defect to fix.
func FixInit(init []byte, vt Track, keyframe []byte) ([]byte, bool) {
	hevc := vt.Codec == "hev1" || vt.Codec == "hvc1"
	if !hevc && vt.Codec != "avc1" && vt.Codec != "avc3" {
		return nil, false
	}

	ps := parameterSets(hevc, keyframe, vt.NALLengthSize)
	if len(ps.sps) == 0 {
		return nil, false
	}
	w, h, ok := SPSSize(vt.Codec, ps.sps[0])
	if !ok || (w == vt.Width && h == vt.Height) {
		return nil, false
	}

	c := &initFix{codec: vt.Codec, hevc: hevc, ps: ps, w: w, h: h}
	var out []byte
	for off := 0; off+8 <= len(init); {
		hdrLen, size, ok := boxHeaderLen(init[off:])
		if !ok {
			return nil, false
		}
		box := init[off : off+size]
		if boxType(box) == "moov" {
			rebuilt, changed := c.box(box, hdrLen)
			if c.failed {
				return nil, false
			}
			if !changed {
				return nil, false
			}
			box = rebuilt
		}
		out = append(out, box...)
		off += size
	}
	return out, true
}

// inbandSets are the in-band parameter sets, in the order they came.
type inbandSets struct {
	vps, sps, pps [][]byte
}

// parameterSets collects the keyframe's parameter sets. It stops at the first
// picture NAL: they always come before it, and the rest of the mdat is the
// picture itself.
func parameterSets(hevc bool, data []byte, lengthSize int) inbandSets {
	var ps inbandSets
	if lengthSize < 1 || lengthSize > 4 {
		return ps
	}
	for p := 0; p+lengthSize <= len(data); {
		n := 0
		for i := range lengthSize {
			n = n<<8 | int(data[p+i])
		}
		p += lengthSize
		if n <= 0 || p+n > len(data) {
			return ps
		}
		nal := data[p : p+n]
		p += n

		if hevc {
			switch typ := int(nal[0]>>1) & 0x3f; {
			case typ == nalTypeVPS265:
				ps.vps = appendUnique(ps.vps, nal)
			case typ == nalTypeSPS265:
				ps.sps = appendUnique(ps.sps, nal)
			case typ == nalTypePPS265:
				ps.pps = appendUnique(ps.pps, nal)
			case typ < nalTypeVPS265: // picture (VCL)
				return ps
			}
			continue
		}
		switch typ := int(nal[0] & 0x1f); {
		case typ == nalTypeSPS264:
			ps.sps = appendUnique(ps.sps, nal)
		case typ == nalTypePPS264:
			ps.pps = appendUnique(ps.pps, nal)
		case typ >= 1 && typ <= 5: // picture (VCL)
			return ps
		}
	}
	return ps
}

// appendUnique keeps a copy of the NAL, without repeating an equal one: the
// camera may repeat the parameter set, and the configuration record doesn't
// need two.
func appendUnique(list [][]byte, nal []byte) [][]byte {
	for _, j := range list {
		if bytes.Equal(j, nal) {
			return list
		}
	}
	return append(list, bytes.Clone(nal))
}

// initFix rebuilds the moov going down only the path to the hvcC/avcC. Each
// box on the path is rebuilt with its new size; the others are copied as they
// came.
type initFix struct {
	codec  string
	hevc   bool
	ps     inbandSets
	w, h   uint16
	failed bool
}

// box returns the rebuilt box and whether anything changed inside it.
func (c *initFix) box(b []byte, hdrLen int) ([]byte, bool) {
	typ := boxType(b)
	_, size, _ := boxHeaderLen(b)
	payload := b[hdrLen:size]

	switch typ {
	case "moov", "trak", "mdia", "minf", "stbl":
		rebuilt, changed := c.container(typ, nil, payload)
		if !changed {
			return b, false
		}
		if typ == "trak" {
			c.tkhdDimensions(rebuilt)
		}
		return rebuilt, true
	case "stsd":
		// version+flags(4) and entry_count(4) before the sample entries
		if len(payload) < 8 {
			return b, false
		}
		rebuilt, changed := c.container(typ, payload[:8], payload[8:])
		if !changed {
			return b, false
		}
		return rebuilt, true
	case c.codec:
		// The VisualSampleEntry fixed fields come before the child boxes;
		// width and height are in bytes 24 to 27 of them.
		fixed := visualSampleEntryFixed - 8
		if len(payload) < fixed {
			return b, false
		}
		prefix := bytes.Clone(payload[:fixed])
		binary.BigEndian.PutUint16(prefix[24:], c.w)
		binary.BigEndian.PutUint16(prefix[26:], c.h)
		return c.container(typ, prefix, payload[fixed:])
	case "hvcC":
		rebuilt := c.hvcC(payload)
		if rebuilt == nil {
			c.failed = true
			return b, false
		}
		return boxWith(typ, rebuilt), true
	case "avcC":
		rebuilt := c.avcC(payload)
		if rebuilt == nil {
			c.failed = true
			return b, false
		}
		return boxWith(typ, rebuilt), true
	}
	return b, false
}

// container rebuilds a box with the prefix and the children, fixing each child,
// and says whether any child changed. If none did, the caller keeps the
// original box, byte for byte.
func (c *initFix) container(typ string, prefix, children []byte) ([]byte, bool) {
	payload := append([]byte(nil), prefix...)
	changed := false
	for off := 0; off+8 <= len(children); {
		hdrLen, size, ok := boxHeaderLen(children[off:])
		if !ok {
			c.failed = true
			return nil, false
		}
		rebuilt, m := c.box(children[off:off+size], hdrLen)
		if c.failed {
			return nil, false
		}
		changed = changed || m
		payload = append(payload, rebuilt...)
		off += size
	}
	return boxWith(typ, payload), changed
}

// tkhdDimensions sets width and height in the track's tkhd, in place. They are
// the last two fields of the body, in 16.16 fixed point.
func (c *initFix) tkhdDimensions(trak []byte) {
	_ = walk(trak[8:], func(typ string, payload []byte) error {
		if typ != "tkhd" || len(payload) < 1 {
			return nil
		}
		end := tkhdBodyV0
		if payload[0] == 1 {
			end = tkhdBodyV1
		}
		if len(payload) < end {
			return nil
		}
		binary.BigEndian.PutUint32(payload[end-8:], uint32(c.w)<<16)
		binary.BigEndian.PutUint32(payload[end-4:], uint32(c.h)<<16)
		return nil
	})
}

// hvcC rebuilds the HEVCDecoderConfigurationRecord with the in-band parameter
// sets. The 22 header bytes stay, except profile, tier and level, which become
// those of the new SPS. Arrays of other types (SEI) stay as they came.
//
//	configurationVersion(1) general_profile_space|tier|profile_idc(1)
//	compatibility_flags(4) constraint_flags(6) level_idc(1) ...(9)
//	lengthSizeMinusOne at byte 21, numOfArrays at 22
//	{ completeness|NAL_unit_type(1) numNalus(2) { nalUnitLength(2) nal }... }...
func (c *initFix) hvcC(old []byte) []byte {
	if len(old) < 23 {
		return nil
	}
	rebuilt := bytes.Clone(old[:22])

	// The H.265 SPS, after the 2-byte NAL header and 1 byte of ids, carries
	// the same 12 profile and level bytes as the record, in the same order.
	if sps := unescape(c.ps.sps[0]); len(sps) >= 15 {
		copy(rebuilt[1:13], sps[3:15])
	}

	// Reads the old arrays: the first byte of each type is kept, so as not to
	// change array_completeness, and arrays of other types go along whole.
	heads := map[int]byte{}
	var others [][]byte
	p := 23
	for n := int(old[22]); n > 0; n-- {
		if p+3 > len(old) {
			return nil
		}
		start := p
		typ := int(old[p] & 0x3f)
		heads[typ] = old[p]
		count, _ := be16(old, p+1)
		p += 3
		for ; count > 0; count-- {
			size, ok := be16(old, p)
			if !ok || p+2+int(size) > len(old) {
				return nil
			}
			p += 2 + int(size)
		}
		if typ != nalTypeVPS265 && typ != nalTypeSPS265 && typ != nalTypePPS265 {
			others = append(others, old[start:p])
		}
	}

	var arrays []byte
	num := 0
	for _, a := range []struct {
		typ  int
		nals [][]byte
	}{{nalTypeVPS265, c.ps.vps}, {nalTypeSPS265, c.ps.sps}, {nalTypePPS265, c.ps.pps}} {
		if len(a.nals) == 0 {
			continue
		}
		head, ok := heads[a.typ]
		if !ok {
			// In hvc1 the parameter sets can only be in the init; in hev1
			// they may also come in-band.
			head = byte(a.typ)
			if c.codec == "hvc1" {
				head |= 0x80
			}
		}
		arrays = append(arrays, head)
		arrays = binary.BigEndian.AppendUint16(arrays, uint16(len(a.nals)))
		for _, nal := range a.nals {
			arrays = binary.BigEndian.AppendUint16(arrays, uint16(len(nal)))
			arrays = append(arrays, nal...)
		}
		num++
	}
	for _, o := range others {
		arrays = append(arrays, o...)
		num++
	}

	rebuilt = append(rebuilt, byte(num))
	return append(rebuilt, arrays...)
}

// avcC rebuilds the AVCDecoderConfigurationRecord with the in-band parameter sets.
//
//	version(1) profile(1) compat(1) level(1)
//	0b111111|lengthSizeMinusOne(1)  0b111|numOfSPS(1) { spsLength(2) sps }...
//	numOfPPS(1) { ppsLength(2) pps }...
//	and, in the High profiles, 4 bytes of chroma and bit depth (plus extensions)
func (c *initFix) avcC(old []byte) []byte {
	if len(old) < 7 {
		return nil
	}
	sps := c.ps.sps[0]
	if len(sps) < 4 {
		return nil
	}
	rebuilt := []byte{1, sps[1], sps[2], sps[3], old[4], 0xe0 | byte(len(c.ps.sps))}
	for _, nal := range c.ps.sps {
		rebuilt = binary.BigEndian.AppendUint16(rebuilt, uint16(len(nal)))
		rebuilt = append(rebuilt, nal...)
	}
	rebuilt = append(rebuilt, byte(len(c.ps.pps)))
	for _, nal := range c.ps.pps {
		rebuilt = binary.BigEndian.AppendUint16(rebuilt, uint16(len(nal)))
		rebuilt = append(rebuilt, nal...)
	}

	switch sps[1] {
	case 100, 110, 122, 144:
		// The High profiles require the chroma and bit depth block. The init's
		// one is used, if any; otherwise, 8-bit 4:2:0, without extensions.
		if tail := avcCTail(old); len(tail) > 0 {
			rebuilt = append(rebuilt, tail...)
		} else {
			rebuilt = append(rebuilt, 0xfd, 0xf8, 0xf8, 0)
		}
	}
	return rebuilt
}

// avcCTail returns what comes after the PPS list of an avcC: the High
// profiles block, when present.
func avcCTail(b []byte) []byte {
	p := 5
	for range 2 { // SPS and then PPS
		if p >= len(b) {
			return nil
		}
		n := int(b[p])
		if p == 5 {
			n &= 0x1f
		}
		p++
		for ; n > 0; n-- {
			size, ok := be16(b, p)
			if !ok || p+2+int(size) > len(b) {
				return nil
			}
			p += 2 + int(size)
		}
	}
	return b[p:]
}

// boxWith builds a box with an 8-byte header. The init fits comfortably in
// the 32-bit size.
func boxWith(typ string, payload []byte) []byte {
	out := make([]byte, 8, 8+len(payload))
	binary.BigEndian.PutUint32(out, uint32(8+len(payload)))
	copy(out[4:], typ)
	return append(out, payload...)
}
