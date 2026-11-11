package fmp4

// Reading the resolution from the SPS (Sequence Parameter Set) of H.264 and
// H.265.
//
// It exists because the VisualSampleEntry width/height fields LIE. A camera in
// the first deployment is the real case: it advertises 2560x1440 in the RTSP
// DESCRIBE, go2rtc builds the hvcC from that, and the stream that arrives is
// 1920x1080 with a different in-band SPS. Decoders obey the SPS, so the SPS is
// what tells the truth about what's recorded on disk.
//
// None of this decodes media: the SPS is a ~60-byte header, read once per
// connection.

// NAL types that carry the SPS in each codec.
const (
	nalTypeSPS264 = 7
	nalTypeSPS265 = 33
)

// bitReader reads bit-width fields from an already unescaped RBSP.
type bitReader struct {
	b   []byte
	pos int // in bits
	err bool
}

func (r *bitReader) u(n int) uint32 {
	var v uint32
	for range n {
		if r.pos >= len(r.b)*8 {
			r.err = true
			return v
		}
		bit := (r.b[r.pos>>3] >> (7 - uint(r.pos&7))) & 1
		v = v<<1 | uint32(bit)
		r.pos++
	}
	return v
}

func (r *bitReader) skip(n int) {
	if r.pos+n > len(r.b)*8 {
		r.err = true
		r.pos = len(r.b) * 8
		return
	}
	r.pos += n
}

func (r *bitReader) flag() bool { return r.u(1) == 1 }

// ue reads an unsigned Exp-Golomb integer.
func (r *bitReader) ue() uint32 {
	zeros := 0
	for !r.err && r.u(1) == 0 {
		zeros++
		// A prefix longer than this is not a valid SPS, it's garbage - and
		// without the cap a corrupted stream would hold the loop until the end
		// of the buffer.
		if zeros > 32 {
			r.err = true
			return 0
		}
	}
	if r.err || zeros == 0 {
		return 0
	}
	return uint32(1)<<uint(zeros) - 1 + r.u(zeros)
}

func (r *bitReader) se() int32 {
	k := r.ue()
	if k%2 == 1 {
		return int32(k+1) / 2
	}
	return -int32(k / 2)
}

// unescape removes the emulation prevention bytes (00 00 03 -> 00 00), which
// exist in the NAL so the start code never shows up in the middle of the data.
// Without removing them, the fields come out shifted.
func unescape(nal []byte) []byte {
	out := make([]byte, 0, len(nal))
	zeros := 0
	for _, c := range nal {
		if zeros == 2 && c == 3 {
			zeros = 0
			continue
		}
		if c == 0 {
			zeros++
		} else {
			zeros = 0
		}
		out = append(out, c)
	}
	return out
}

// chromaSub returns SubWidthC and SubHeightC, the factors that convert the
// cropping window (in chroma samples) to luma samples.
func chromaSub(format uint32) (uint32, uint32) {
	switch format {
	case 1: // 4:2:0
		return 2, 2
	case 2: // 4:2:2
		return 2, 1
	default: // monochrome and 4:4:4 don't subsample
		return 1, 1
	}
}

// SPSSize returns the resolution declared in an SPS, with the cropping window
// already taken off. The codec comes from the sample entry 4CC.
func SPSSize(codec string, nal []byte) (w, h uint16, ok bool) {
	switch codec {
	case "avc1", "avc3":
		return h264SPSSize(nal)
	case "hev1", "hvc1":
		return h265SPSSize(nal)
	}
	return 0, 0, false
}

// h264SPSSize reads the resolution of an H.264 SPS (ISO/IEC 14496-10, 7.3.2.1.1).
func h264SPSSize(nal []byte) (uint16, uint16, bool) {
	if len(nal) < 4 {
		return 0, 0, false
	}
	r := &bitReader{b: unescape(nal[1:])} // 1-byte NAL header

	profile := r.u(8)
	r.skip(16) // constraint flags + level_idc
	r.ue()     // seq_parameter_set_id

	chroma := uint32(1) // 4:2:0 is implied by the profiles that don't carry the field
	switch profile {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		chroma = r.ue()
		if chroma == 3 {
			r.skip(1) // separate_colour_plane_flag
		}
		r.ue()        // bit_depth_luma_minus8
		r.ue()        // bit_depth_chroma_minus8
		r.skip(1)     // qpprime_y_zero_transform_bypass_flag
		if r.flag() { // seq_scaling_matrix_present_flag
			lists := 8
			if chroma == 3 {
				lists = 12
			}
			for i := range lists {
				if r.flag() {
					size := 16
					if i >= 6 {
						size = 64
					}
					skipScalingList(r, size)
				}
			}
		}
	}

	r.ue() // log2_max_frame_num_minus4
	switch r.ue() {
	case 0:
		r.ue() // log2_max_pic_order_cnt_lsb_minus4
	case 1:
		r.skip(1) // delta_pic_order_always_zero_flag
		r.se()    // offset_for_non_ref_pic
		r.se()    // offset_for_top_to_bottom_field
		for n := r.ue(); n > 0 && !r.err; n-- {
			r.se()
		}
	}
	r.ue()    // max_num_ref_frames
	r.skip(1) // gaps_in_frame_num_value_allowed_flag

	widthMBs := r.ue() + 1
	heightUnits := r.ue() + 1
	frameMBsOnly := r.flag()
	if !frameMBsOnly {
		r.skip(1) // mb_adaptive_frame_field_flag
	}
	r.skip(1) // direct_8x8_inference_flag

	w := widthMBs * 16
	h := heightUnits * 16
	if !frameMBsOnly {
		// Without frame_mbs_only each map unit is half a frame.
		h *= 2
	}

	if r.flag() { // frame_cropping_flag
		left, right, top, bottom := r.ue(), r.ue(), r.ue(), r.ue()
		cx, cy := chromaSub(chroma)
		if chroma == 0 {
			cx = 1
		}
		if !frameMBsOnly {
			cy *= 2
		}
		w = subCrop(w, cx*(left+right))
		h = subCrop(h, cy*(top+bottom))
	}
	return finish(r, w, h)
}

// skipScalingList consumes a scaling list without keeping it: only the image
// size matters here, but skipping it wrong would misalign everything after.
func skipScalingList(r *bitReader, size int) {
	last, next := int32(8), int32(8)
	for i := 0; i < size && !r.err; i++ {
		if next != 0 {
			next = (last + r.se() + 256) % 256
		}
		if next != 0 {
			last = next
		}
	}
}

// h265SPSSize reads the resolution of an H.265 SPS (ISO/IEC 23008-2, 7.3.2.2.1).
func h265SPSSize(nal []byte) (uint16, uint16, bool) {
	if len(nal) < 8 {
		return 0, 0, false
	}
	r := &bitReader{b: unescape(nal[2:])} // 2-byte NAL header

	r.skip(4) // sps_video_parameter_set_id
	maxSubLayers := int(r.u(3))
	r.skip(1) // sps_temporal_id_nesting_flag
	skipProfileTierLevel(r, maxSubLayers)

	r.ue() // sps_seq_parameter_set_id
	chroma := r.ue()
	if chroma == 3 {
		r.skip(1) // separate_colour_plane_flag
	}

	w := r.ue() // pic_width_in_luma_samples
	h := r.ue() // pic_height_in_luma_samples

	if r.flag() { // conformance_window_flag
		left, right, top, bottom := r.ue(), r.ue(), r.ue(), r.ue()
		cx, cy := chromaSub(chroma)
		w = subCrop(w, cx*(left+right))
		h = subCrop(h, cy*(top+bottom))
	}
	return finish(r, w, h)
}

// skipProfileTierLevel consumes the profile/level block, whose size depends on
// the number of sub-layers. It's the variable-length field that comes before
// the resolution, so getting it wrong means reading dimensions from nowhere.
func skipProfileTierLevel(r *bitReader, maxSubLayers int) {
	r.skip(96) // general profile, tier, compatibility, constraints and level

	if maxSubLayers == 0 {
		return
	}
	var profilePresent, levelPresent [8]bool
	for i := 0; i < maxSubLayers && i < 8; i++ {
		profilePresent[i] = r.flag()
		levelPresent[i] = r.flag()
	}
	for i := maxSubLayers; i < 8; i++ {
		r.skip(2) // reserved_zero_2bits
	}
	for i := 0; i < maxSubLayers && i < 8; i++ {
		if profilePresent[i] {
			r.skip(88)
		}
		if levelPresent[i] {
			r.skip(8)
		}
	}
}

// subCrop takes off the crop without wrapping the integer around: a corrupted
// SPS must not turn into a huge resolution.
func subCrop(v, crop uint32) uint32 {
	if crop >= v {
		return 0
	}
	return v - crop
}

// finish validates the result. A read that overran the buffer produces
// meaningless numbers, and showing those on screen would be worse than showing
// nothing.
func finish(r *bitReader, w, h uint32) (uint16, uint16, bool) {
	const maxDim = 1 << 16
	if r.err || w == 0 || h == 0 || w >= maxDim || h >= maxDim {
		return 0, 0, false
	}
	return uint16(w), uint16(h), true
}

// FindSPS looks for an SPS among a fragment's length-prefixed NALs.
//
// It's where the truth comes in when the init lies: in-band parameter sets
// always come right before the IDR, so looking at the fragment of the first
// keyframe of each connection is enough.
func FindSPS(codec string, data []byte, lengthSize int) ([]byte, bool) {
	if lengthSize < 1 || lengthSize > 4 {
		return nil, false
	}
	want := nalTypeSPS264
	if codec == "hev1" || codec == "hvc1" {
		want = nalTypeSPS265
	}

	for p := 0; p+lengthSize <= len(data); {
		n := 0
		for i := range lengthSize {
			n = n<<8 | int(data[p+i])
		}
		p += lengthSize
		if n <= 0 || p+n > len(data) {
			return nil, false
		}
		nal := data[p : p+n]

		typ := int(nal[0] & 0x1f)
		if want == nalTypeSPS265 {
			typ = int(nal[0]>>1) & 0x3f
		}
		if typ == want {
			return nal, true
		}
		p += n
	}
	return nil, false
}
