package fmp4

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// Real SPS and PPS from an H.264 camera in the first deployment: the init
// advertises 320x180 and every keyframe carries, in-band, the 640x360 SPS it
// actually sends.
var (
	spsH264Declared180p = mustHex("6742001495a85067e740")
	spsH264Inband360p   = mustHex("274de0288d680a02ff9610000003001000000301e0f1422a")
	ppsH264Inband       = mustHex("28ee02d248")
)

// nalWithLength prefixes each NAL with its 4-byte length, as in the mdat.
func nalWithLength(nals ...[]byte) []byte {
	var out []byte
	for _, n := range nals {
		out = append(out, u32(uint32(len(n)))...)
		out = append(out, n...)
	}
	return out
}

// keyframeH265 is the mdat content of a keyframe with the in-band parameter
// sets before the IDR.
func keyframeH265(sps []byte) []byte {
	vps := []byte{32 << 1, 1, 0xaa, 0xbb}
	pps := []byte{34 << 1, 1, 0xcc}
	idr := append([]byte{19 << 1, 1}, bytes.Repeat([]byte{0x42}, 64)...)
	return nalWithLength(vps, sps, pps, idr)
}

func videoTrackOf(t *testing.T, init []byte) Track {
	t.Helper()
	moov := init[bytes.Index(init, []byte("moov"))-4:]
	mv, err := ParseMoov(moov)
	if err != nil {
		t.Fatalf("unreadable init: %v", err)
	}
	vt, ok := mv.VideoTrack()
	if !ok {
		t.Fatal("init without a video track")
	}
	return vt
}

// The camera that advertises the main stream and sends the substream: the
// init comes out with the in-band SPS, and the rest of the moov as it was.
func TestFixInitReplacesInitSPS(t *testing.T) {
	ftyp := box("ftyp", []byte("isom"))
	moov := makeMoov(1, 90000, "hev1", true)
	init := append(append([]byte(nil), ftyp...), moov...)
	vt := videoTrackOf(t, init)

	rebuilt, ok := FixInit(init, vt, keyframeH265(spsH2651080p))
	if !ok {
		t.Fatal("didn't fix a 2560x1440 init with a 1920x1080 in-band SPS")
	}
	if !bytes.HasPrefix(rebuilt, ftyp) {
		t.Error("the ftyp is no longer at the start, where it was")
	}

	nvt := videoTrackOf(t, rebuilt)
	if nvt.Width != 1920 || nvt.Height != 1080 {
		t.Errorf("fixed init says %dx%d, want 1920x1080", nvt.Width, nvt.Height)
	}
	if nvt.NALLengthSize != vt.NALLengthSize || nvt.Timescale != vt.Timescale || nvt.ID != vt.ID {
		t.Errorf("the track changed beyond the SPS: before %+v, after %+v", vt, nvt)
	}

	// The three in-band parameter sets are in the hvcC, and the advertised SPS
	// is gone.
	if !bytes.Contains(rebuilt, spsH2651080p) || bytes.Contains(rebuilt, spsH2651440p) {
		t.Error("the hvcC doesn't have the in-band SPS, and only it")
	}
	for _, ps := range [][]byte{{32 << 1, 1, 0xaa, 0xbb}, {34 << 1, 1, 0xcc}} {
		if !bytes.Contains(rebuilt, ps) {
			t.Errorf("parameter set %x didn't go into the hvcC", ps)
		}
	}

	// The sample entry has the new resolution: bytes 24 to 27 after the header.
	i := bytes.Index(rebuilt, []byte("hev1")) + 4
	if w, h := binary.BigEndian.Uint16(rebuilt[i+24:]), binary.BigEndian.Uint16(rebuilt[i+26:]); w != 1920 || h != 1080 {
		t.Errorf("sample entry says %dx%d, want 1920x1080", w, h)
	}

	// The audio track goes along byte for byte as it came.
	audio := moov[bytes.LastIndex(moov, []byte("trak"))-4:]
	audio = audio[:binary.BigEndian.Uint32(audio)]
	if !bytes.Contains(rebuilt, audio) {
		t.Error("the audio track changed")
	}
}

// H.264 goes down the same path, with the avcC.
func TestFixInitH264(t *testing.T) {
	cfg := []byte{1, spsH264Declared180p[1], spsH264Declared180p[2], spsH264Declared180p[3], 0xff, 0xe1}
	cfg = append(cfg, u16(uint16(len(spsH264Declared180p)))...)
	cfg = append(cfg, spsH264Declared180p...)
	cfg = append(cfg, 1, 0, 4, 0x68, 0xce, 0x3c, 0x80)

	tkhd := box("tkhd", make([]byte, tkhdBodyV0)) // version 0
	binary.BigEndian.PutUint32(tkhd[8+12:], 1)    // track_ID
	mdhd := box("mdhd", u32(0), u32(0), u32(0), u32(90000), u32(0))
	hdlr := box("hdlr", u32(0), u32(0), []byte("vide"))
	stsd := box("stsd", u32(0), u32(1), visualSampleEntry("avc1", box("avcC", cfg)))
	trak := box("trak", tkhd, box("mdia", mdhd, hdlr, box("minf", box("stbl", stsd))))
	init := box("moov", box("mvhd", u32(0)), trak)
	vt := videoTrackOf(t, init)
	if vt.Width != 320 || vt.Height != 180 {
		t.Fatalf("starting init says %dx%d, want 320x180", vt.Width, vt.Height)
	}

	idr := append([]byte{0x65}, bytes.Repeat([]byte{0x42}, 64)...)
	rebuilt, ok := FixInit(init, vt, nalWithLength(spsH264Inband360p, ppsH264Inband, idr))
	if !ok {
		t.Fatal("didn't fix the H.264 init")
	}
	nvt := videoTrackOf(t, rebuilt)
	if nvt.Width != 640 || nvt.Height != 360 {
		t.Errorf("fixed init says %dx%d, want 640x360", nvt.Width, nvt.Height)
	}
	if !bytes.Contains(rebuilt, ppsH264Inband) || bytes.Contains(rebuilt, []byte{0x68, 0xce, 0x3c, 0x80}) {
		t.Error("the avcC doesn't have the in-band PPS, and only it")
	}
	// The record's profile and level become those of the in-band SPS.
	c := rebuilt[bytes.Index(rebuilt, []byte("avcC"))+4:]
	if c[1] != spsH264Inband360p[1] || c[3] != spsH264Inband360p[3] {
		t.Errorf("profile %d and level %d in the avcC, the SPS says %d and %d",
			c[1], c[3], spsH264Inband360p[1], spsH264Inband360p[3])
	}

	// The tkhd also has the new resolution, in 16.16 fixed point.
	k := bytes.Index(rebuilt, []byte("tkhd")) + 4
	if w, h := binary.BigEndian.Uint32(rebuilt[k+tkhdBodyV0-8:]), binary.BigEndian.Uint32(rebuilt[k+tkhdBodyV0-4:]); w != 640<<16 || h != 360<<16 {
		t.Errorf("tkhd says %dx%d, want 640x360", w>>16, h>>16)
	}
}

// With nothing to fix, the camera's init stands: nothing changes the
// generation of a camera that already records right.
func TestFixInitLeavesMatchingInitAlone(t *testing.T) {
	init := append(box("ftyp", []byte("isom")), makeMoov(1, 90000, "hev1", false)...)
	vt := videoTrackOf(t, init)

	if _, ok := FixInit(init, vt, keyframeH265(spsH2651440p)); ok {
		t.Error("fixed an init whose SPS has the in-band resolution")
	}
	idr := append([]byte{19 << 1, 1}, bytes.Repeat([]byte{0x42}, 64)...)
	if _, ok := FixInit(init, vt, nalWithLength(idr)); ok {
		t.Error("fixed without any in-band SPS")
	}
	if _, ok := FixInit(init, vt, []byte{0xff, 0xff, 0xff, 0xff, 1}); ok {
		t.Error("fixed from a malformed mdat")
	}
	audio := vt
	audio.Codec = "mp4a"
	if _, ok := FixInit(init, audio, keyframeH265(spsH2651080p)); ok {
		t.Error("fixed a track that is neither H.264 nor H.265")
	}
}
