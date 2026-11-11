package fmp4

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
)

// --- synthetic box builders -------------------------------------------------

func box(typ string, parts ...[]byte) []byte {
	var body []byte
	for _, p := range parts {
		body = append(body, p...)
	}
	out := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(out[0:4], uint32(8+len(body)))
	copy(out[4:8], typ)
	return append(out, body...)
}

func u32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func u64(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

// Values go2rtc actually writes (pkg/iso/atoms.go).
const (
	flagsIFrame    = 0x02000000
	flagsNonIFrame = 0x01010000
)

func u16(v uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return b
}

// Real SPSs, captured from the cameras of the first deployment. They're worth
// more than any synthetic SPS because this is exactly where the assumption
// broke: the same camera advertises 1440p in the init and streams 1080p.
var (
	spsH264720p  = mustHex("6742001f95a814016e40")
	spsH2651440p = mustHex("42010101400000030000030000030000030099a001402005a1fe5aee46c1ae5504")
	spsH2651080p = mustHex("420101014000000300900000030000030096a003c08010e59e96e44a5780a7010202e10000030001000003000f5087fde100040160000c042d7c201040")
)

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// avcC and hvcC build the configuration record that carries the SPS, in the
// format go2rtc writes.
func avcC(sps []byte) []byte {
	b := []byte{1, sps[1], sps[2], sps[3], 0xff, 0xe1}
	b = append(b, u16(uint16(len(sps)))...)
	b = append(b, sps...)
	return box("avcC", b, []byte{0}) // no PPS
}

func hvcC(sps []byte) []byte {
	b := make([]byte, 22)
	b[0] = 1
	b[21] = 0xf3 // lengthSizeMinusOne = 3, that is, a 4-byte prefix
	b = append(b, 1, 33)
	b = append(b, u16(1)...)
	b = append(b, u16(uint16(len(sps)))...)
	b = append(b, sps...)
	return box("hvcC", b)
}

func configFor(codec string) []byte {
	if strings.HasPrefix(codec, "hev") || strings.HasPrefix(codec, "hvc") {
		return hvcC(spsH2651440p)
	}
	return avcC(spsH264720p)
}

// visualSampleEntry builds the video sample entry: 78 bytes of fixed fields
// after the box header (reserved, data_reference_index, dimensions,
// resolutions, compressorname, depth) and then the child boxes.
func visualSampleEntry(codec string, children ...[]byte) []byte {
	parts := append([][]byte{make([]byte, 78)}, children...)
	return box(codec, parts...)
}

func makeMoov(trackID, timescale uint32, codec string, audio bool) []byte {
	tkhd := box("tkhd", u32(0), u32(0), u32(0), u32(trackID))
	mdhd := box("mdhd", u32(0), u32(0), u32(0), u32(timescale), u32(0))
	hdlr := box("hdlr", u32(0), u32(0), []byte("vide"))
	stsd := box("stsd", u32(0), u32(1), visualSampleEntry(codec, configFor(codec)))
	stbl := box("stbl", stsd)
	minf := box("minf", stbl)
	mdia := box("mdia", mdhd, hdlr, minf)
	trak := box("trak", tkhd, mdia)

	trex := box("trex", u32(0), u32(trackID), u32(1), u32(0), u32(0), u32(flagsNonIFrame))
	children := [][]byte{box("mvhd", u32(0)), trak}

	if audio {
		atkhd := box("tkhd", u32(0), u32(0), u32(0), u32(trackID+1))
		amdhd := box("mdhd", u32(0), u32(0), u32(0), u32(16000), u32(0))
		ahdlr := box("hdlr", u32(0), u32(0), []byte("soun"))
		astsd := box("stsd", u32(0), u32(1), box("fLaC"))
		atrak := box("trak", atkhd, box("mdia", amdhd, ahdlr, box("minf", box("stbl", astsd))))
		children = append(children, atrak)
		trex = append(trex, box("trex", u32(0), u32(trackID+1), u32(1), u32(0), u32(0), u32(0))...)
	}

	children = append(children, box("mvex", trex))
	return box("moov", children...)
}

// makeMoof builds a moof with a single sample, as go2rtc does.
func makeMoof(trackID uint32, dts uint64, sampleFlags uint32, v1 bool) []byte {
	return makeMoofDur(trackID, dts, sampleFlags, v1, 6000)
}

// makeMoofDur includes the sample duration in the trun, which is how go2rtc writes it.
func makeMoofDur(trackID uint32, dts uint64, sampleFlags uint32, v1 bool, dur uint32) []byte {
	tfhd := box("tfhd", u32(0), u32(trackID))

	var tfdt []byte
	if v1 {
		tfdt = box("tfdt", []byte{1, 0, 0, 0}, u64(dts))
	} else {
		tfdt = box("tfdt", u32(0), u32(uint32(dts)))
	}

	// flags 0x000105 = data-offset | first-sample-flags | sample-duration
	trun := box("trun", u32(0x000105), u32(1), u32(0), u32(sampleFlags), u32(dur))
	return box("moof", box("mfhd", u32(0), u32(1)), box("traf", tfhd, tfdt, trun))
}

// --- tests ------------------------------------------------------------------

func TestParseMoovReadsTracks(t *testing.T) {
	mv, err := ParseMoov(makeMoov(1, 90000, "hev1", true))
	if err != nil {
		t.Fatalf("ParseMoov: %v", err)
	}
	if len(mv.Tracks) != 2 {
		t.Fatalf("want 2 tracks, got %d", len(mv.Tracks))
	}

	vt, ok := mv.VideoTrack()
	if !ok {
		t.Fatal("video track not found")
	}
	if vt.ID != 1 || vt.Timescale != 90000 || vt.Codec != "hev1" {
		t.Errorf("wrong video track: %+v", vt)
	}
	if vt.DefaultSampleFlags != flagsNonIFrame {
		t.Errorf("trex default_sample_flags = %#x, want %#x", vt.DefaultSampleFlags, flagsNonIFrame)
	}
	if !mv.HasAudio() {
		t.Error("HasAudio() should be true")
	}
}

// The resolution comes from the SPS, not from the sample entry width/height
// fields. These SPSs came from real cameras, and the expected result is what
// ffmpeg decodes from the same bytes.
func TestSPSSize(t *testing.T) {
	tests := []struct {
		name  string
		codec string
		sps   []byte
		w, h  uint16
	}{
		{"h264 from the door camera", "avc1", spsH264720p, 1280, 720},
		{"h265 advertised in the init", "hev1", spsH2651440p, 2560, 1440},
		{"h265 in-band that the camera actually streams", "hev1", spsH2651080p, 1920, 1080},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, h, ok := SPSSize(tt.codec, tt.sps)
			if !ok {
				t.Fatal("couldn't read the SPS")
			}
			if w != tt.w || h != tt.h {
				t.Errorf("resolution = %dx%d, want %dx%d", w, h, tt.w, tt.h)
			}
		})
	}
}

// A truncated SPS must become "don't know", never a made-up number: the screen
// shows "-" and nobody is misled.
func TestSPSSizeTruncatedDoesNotGuess(t *testing.T) {
	for n := 1; n < len(spsH2651080p); n++ {
		if _, _, ok := SPSSize("hev1", spsH2651080p[:n]); ok {
			// A prefix may even be readable, but must never pass for a
			// plausible resolution if the fields didn't arrive whole.
			w, h, _ := SPSSize("hev1", spsH2651080p[:n])
			if w == 1920 && h == 1080 {
				continue // the SPS has just become complete enough
			}
			t.Fatalf("SPS cut at %d bytes returned %dx%d as if it were valid", n, w, h)
		}
	}
	if _, _, ok := SPSSize("mp4a", spsH264720p); ok {
		t.Error("an audio codec should not produce a resolution")
	}
}

// The init's resolution comes from the SPS the hvcC/avcC carries. The audio
// track, whose sample entry has another layout, must not make up any
// dimension.
func TestParseMoovReadsResolutionFromSPS(t *testing.T) {
	mv, err := ParseMoov(makeMoov(1, 90000, "hev1", true))
	if err != nil {
		t.Fatal(err)
	}
	vt, _ := mv.VideoTrack()
	if vt.Width != 2560 || vt.Height != 1440 {
		t.Errorf("resolution = %dx%d, want 2560x1440", vt.Width, vt.Height)
	}
	if vt.NALLengthSize != 4 {
		t.Errorf("NALLengthSize = %d, want 4", vt.NALLengthSize)
	}
	for _, tr := range mv.Tracks {
		if tr.Handler == HandlerAudio && (tr.Width != 0 || tr.Height != 0) {
			t.Errorf("audio track came with dimensions %dx%d", tr.Width, tr.Height)
		}
	}
}

// A sample entry without codec configuration must not break reading the init:
// recording still works without the resolution, and failing here would cost
// the recording.
func TestParseMoovWithoutCodecConfig(t *testing.T) {
	tkhd := box("tkhd", u32(0), u32(0), u32(0), u32(1))
	mdhd := box("mdhd", u32(0), u32(0), u32(0), u32(90000), u32(0))
	hdlr := box("hdlr", u32(0), u32(0), []byte("vide"))
	stsd := box("stsd", u32(0), u32(1), box("avc1")) // not even the fixed fields
	trak := box("trak", tkhd, box("mdia", mdhd, hdlr, box("minf", box("stbl", stsd))))

	mv, err := ParseMoov(box("moov", box("mvhd", u32(0)), trak))
	if err != nil {
		t.Fatalf("ParseMoov: %v", err)
	}
	vt, ok := mv.VideoTrack()
	if !ok {
		t.Fatal("video track not found")
	}
	if vt.Codec != "avc1" {
		t.Errorf("codec = %q", vt.Codec)
	}
	if vt.Width != 0 || vt.Height != 0 {
		t.Errorf("want zero dimensions, got %dx%d", vt.Width, vt.Height)
	}
}

// FindSPS finds the parameter set among the fragment's NALs - the path that
// fixes a lying init.
func TestFindSPSInband(t *testing.T) {
	nal := func(b []byte) []byte {
		return append(u32(uint32(len(b))), b...)
	}
	vps := append([]byte{32 << 1, 1}, 0xaa, 0xbb)
	idr := append([]byte{19 << 1, 1}, bytes.Repeat([]byte{0x42}, 64)...)
	mdat := box("mdat", nal(vps), nal(spsH2651080p), nal(idr))

	sps, ok := FindSPS("hev1", BoxPayload(mdat), 4)
	if !ok {
		t.Fatal("in-band SPS not found")
	}
	w, h, ok := SPSSize("hev1", sps)
	if !ok || w != 1920 || h != 1080 {
		t.Errorf("in-band SPS gave %dx%d (ok=%v), want 1920x1080", w, h, ok)
	}

	// A data-only fragment must not return garbage as if it were an SPS.
	noSPS := box("mdat", nal(idr))
	if _, ok := FindSPS("hev1", BoxPayload(noSPS), 4); ok {
		t.Error("found an SPS where there is none")
	}
	// Nor may a length larger than the buffer itself turn into an out-of-bounds read.
	if _, ok := FindSPS("hev1", []byte{0xff, 0xff, 0xff, 0xff, 1, 2}, 4); ok {
		t.Error("accepted a NAL that doesn't fit in the fragment")
	}
}

// The cost must fit modest hardware: the parse runs once per connection, but
// if it were expensive it would show up here.
func BenchmarkSPSSize(b *testing.B) {
	for b.Loop() {
		if _, _, ok := SPSSize("hev1", spsH2651080p); !ok {
			b.Fatal("unreadable SPS")
		}
	}
}

// The worst case is the SPS arriving after all the other NALs of the
// fragment, forcing the scan through the whole keyframe.
func BenchmarkFindSPSWorstCase(b *testing.B) {
	nal := func(x []byte) []byte { return append(u32(uint32(len(x))), x...) }
	parts := [][]byte{}
	for range 32 {
		parts = append(parts, nal(append([]byte{1 << 1, 1}, bytes.Repeat([]byte{0x42}, 8<<10)...)))
	}
	parts = append(parts, nal(spsH2651080p))
	mdat := BoxPayload(box("mdat", parts...))
	b.SetBytes(int64(len(mdat)))
	for b.Loop() {
		if _, ok := FindSPS("hev1", mdat, 4); !ok {
			b.Fatal("not found")
		}
	}
}

// The video track decides where to cut a segment. Since go2rtc flags audio
// samples with the same sampleDependsOn2 as keyframes, mixing up the tracks
// would make every audio packet look like a valid cut point.
func TestVideoTrackIgnoresAudio(t *testing.T) {
	mv, err := ParseMoov(makeMoov(1, 90000, "avc1", true))
	if err != nil {
		t.Fatal(err)
	}
	vt, _ := mv.VideoTrack()
	if vt.Handler != HandlerVideo {
		t.Errorf("VideoTrack returned handler %q", vt.Handler)
	}
}

func TestParseMoofDetectsKeyframe(t *testing.T) {
	tests := []struct {
		name     string
		flags    uint32
		keyframe bool
	}{
		{"go2rtc keyframe", flagsIFrame, true},
		{"go2rtc P frame", flagsNonIFrame, false},
		{"only the non-sync bit", sampleIsNonSync, false},
		{"all zeros counts as sync", 0, true},
		{"depends_on=1 without the non-sync bit", 0x01000000, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frag, err := ParseMoof(makeMoof(1, 1000, tt.flags, true), 1)
			if err != nil {
				t.Fatalf("ParseMoof: %v", err)
			}
			if frag.Keyframe != tt.keyframe {
				t.Errorf("flags %#x: Keyframe=%v, want %v", tt.flags, frag.Keyframe, tt.keyframe)
			}
			if frag.TrackID != 1 {
				t.Errorf("TrackID=%d", frag.TrackID)
			}
			if !frag.HasBaseTime || frag.BaseDecodeTime != 1000 {
				t.Errorf("tfdt=%d (present=%v)", frag.BaseDecodeTime, frag.HasBaseTime)
			}
		})
	}
}

// A moof from the audio track must never be classified as a keyframe, or the
// segment cut would happen outside a video keyframe.
func TestParseMoofOtherTrackIsNeverKeyframe(t *testing.T) {
	frag, err := ParseMoof(makeMoof(2, 500, flagsIFrame, true), 1)
	if err != nil {
		t.Fatal(err)
	}
	if frag.Keyframe {
		t.Error("moof from track 2 was classified as a keyframe of track 1")
	}
	if frag.TrackID != 2 {
		t.Errorf("TrackID=%d, want 2", frag.TrackID)
	}
}

// The fragment duration is what separates "where the last frame starts" from
// "where the segment ends". Ignoring it made every export splice put the next
// segment on top of the previous one's last frame, and the DTS went backwards.
func TestFragmentEndTime(t *testing.T) {
	frag, err := ParseMoof(makeMoofDur(1, 90000, flagsIFrame, true, 6000), 1)
	if err != nil {
		t.Fatal(err)
	}
	if frag.Duration != 6000 {
		t.Errorf("Duration=%d, want 6000", frag.Duration)
	}
	if frag.EndTime() != 96000 {
		t.Errorf("EndTime=%d, want 96000 (90000 + 6000)", frag.EndTime())
	}
}

// With several samples in a fragment, the duration is the sum of all of them.
func TestFragmentDurationMultipleSamples(t *testing.T) {
	tfhd := box("tfhd", u32(0), u32(1))
	tfdt := box("tfdt", []byte{1, 0, 0, 0}, u64(0))
	// flags 0x000301 = data-offset | sample-duration | sample-size
	trun := box("trun", u32(0x000301), u32(3), u32(0),
		u32(1000), u32(10), u32(2000), u32(20), u32(3000), u32(30))
	moof := box("moof", box("mfhd", u32(0), u32(1)), box("traf", tfhd, tfdt, trun))

	frag, err := ParseMoof(moof, 1)
	if err != nil {
		t.Fatal(err)
	}
	if frag.SampleCount != 3 {
		t.Errorf("SampleCount=%d", frag.SampleCount)
	}
	if frag.Duration != 6000 {
		t.Errorf("Duration=%d, want 6000 (1000+2000+3000)", frag.Duration)
	}
}

// When the trun carries no durations, the tfhd default_sample_duration applies.
func TestFragmentDurationFromTfhdDefault(t *testing.T) {
	// tfhd flags 0x8 = default-sample-duration-present
	tfhd := box("tfhd", u32(0x000008), u32(1), u32(1500))
	tfdt := box("tfdt", []byte{1, 0, 0, 0}, u64(0))
	trun := box("trun", u32(0x000001), u32(4), u32(0)) // only data_offset
	moof := box("moof", box("mfhd", u32(0), u32(1)), box("traf", tfhd, tfdt, trun))

	frag, err := ParseMoof(moof, 1)
	if err != nil {
		t.Fatal(err)
	}
	if frag.Duration != 6000 {
		t.Errorf("Duration=%d, want 6000 (4 × 1500)", frag.Duration)
	}
}

// Splicing two segments using the END of the first must produce strictly
// increasing DTS - it's the invariant the export depends on.
func TestSegmentSpliceDoesNotRegressDTS(t *testing.T) {
	const dur = 6000
	// Segment A: frames at 0, 6000, 12000; ends at 18000.
	var aEnd uint64
	for _, dts := range []uint64{0, 6000, 12000} {
		frag, err := ParseMoof(makeMoofDur(1, dts, flagsIFrame, true, dur), 1)
		if err != nil {
			t.Fatal(err)
		}
		aEnd = frag.EndTime()
	}
	if aEnd != 18000 {
		t.Fatalf("end of segment A = %d, want 18000", aEnd)
	}

	// Segment B starts at zero and is shifted by the end of A.
	moof := makeMoofDur(1, 0, flagsIFrame, true, dur)
	if err := ShiftMoof(moof, map[uint32]int64{1: int64(aEnd)}); err != nil {
		t.Fatal(err)
	}
	frag, err := ParseMoof(moof, 1)
	if err != nil {
		t.Fatal(err)
	}
	if frag.BaseDecodeTime <= 12000 {
		t.Errorf("first frame of B at %d, should come AFTER the last of A (12000)",
			frag.BaseDecodeTime)
	}
	if frag.BaseDecodeTime != 18000 {
		t.Errorf("first frame of B at %d, want 18000 (no gap or overlap)",
			frag.BaseDecodeTime)
	}
}

func TestRebaseMoofZeroesSegment(t *testing.T) {
	for _, v1 := range []bool{true, false} {
		moof := makeMoof(1, 2_160_000, flagsIFrame, v1) // 24s at 90kHz
		if err := RebaseMoof(moof, map[uint32]uint64{1: 2_160_000}); err != nil {
			t.Fatalf("RebaseMoof: %v", err)
		}
		frag, err := ParseMoof(moof, 1)
		if err != nil {
			t.Fatal(err)
		}
		if frag.BaseDecodeTime != 0 {
			t.Errorf("v1=%v: tfdt=%d after rebase, want 0", v1, frag.BaseDecodeTime)
		}
	}
}

// Audio may be a few milliseconds behind the video keyframe that opened the
// segment. Without the clamp, the uint64 would wrap around into an
// astronomical timestamp, which would break playback.
func TestRebaseMoofDoesNotUnderflow(t *testing.T) {
	moof := makeMoof(1, 100, flagsIFrame, true)
	if err := RebaseMoof(moof, map[uint32]uint64{1: 5000}); err != nil {
		t.Fatal(err)
	}
	frag, _ := ParseMoof(moof, 1)
	if frag.BaseDecodeTime != 0 {
		t.Errorf("tfdt=%d, want 0 (clamp)", frag.BaseDecodeTime)
	}
}

func TestRebaseMoofKeepsSize(t *testing.T) {
	moof := makeMoof(1, 900000, flagsIFrame, true)
	before := len(moof)
	if err := RebaseMoof(moof, map[uint32]uint64{1: 90000}); err != nil {
		t.Fatal(err)
	}
	if len(moof) != before {
		t.Errorf("size changed from %d to %d; no box size above it would be recomputed", before, len(moof))
	}
}

// Tracks with different timescales must be rebased at the SAME instant, or the
// real offset between audio and video is lost.
func TestScaleTime(t *testing.T) {
	// 2 seconds at 90kHz must become 2 seconds at 16kHz.
	if got := ScaleTime(180000, 90000, 16000); got != 32000 {
		t.Errorf("ScaleTime(180000, 90000, 16000) = %d, want 32000", got)
	}
	if got := ScaleTime(12345, 90000, 90000); got != 12345 {
		t.Errorf("same timescale should be identity, got %d", got)
	}
}

func TestReaderLargeBox(t *testing.T) {
	// size==1 signals a 64-bit extended size.
	payload := bytes.Repeat([]byte{0xAB}, 32)
	var b []byte
	b = append(b, u32(1)...)
	b = append(b, []byte("mdat")...)
	b = append(b, u64(uint64(16+len(payload)))...)
	b = append(b, payload...)

	r := NewReader(bytes.NewReader(b))
	typ, got, err := r.NextBox()
	if err != nil {
		t.Fatalf("NextBox: %v", err)
	}
	if typ != "mdat" {
		t.Errorf("type=%q", typ)
	}
	if len(got) != len(b) {
		t.Errorf("len=%d, want %d", len(got), len(b))
	}
}

func TestReaderRejectsInvalidSize(t *testing.T) {
	// Size smaller than the header itself: corrupted stream.
	b := append(u32(4), []byte("moof")...)
	r := NewReader(bytes.NewReader(b))
	if _, _, err := r.NextBox(); err == nil {
		t.Error("want an error for a box of size 4")
	}
}

func TestProbeSegment(t *testing.T) {
	var seg []byte
	ftyp := box("ftyp", []byte("iso5"), u32(512))
	moov := makeMoov(1, 90000, "hev1", false)
	seg = append(seg, ftyp...)
	seg = append(seg, moov...)
	initSize := len(seg)

	frag0 := append(makeMoof(1, 0, flagsIFrame, true), box("mdat", bytes.Repeat([]byte{1}, 100))...)
	firstFrag := len(frag0)
	seg = append(seg, frag0...)
	seg = append(seg, makeMoof(1, 6000, flagsNonIFrame, true)...)
	seg = append(seg, box("mdat", bytes.Repeat([]byte{2}, 50))...)
	// 90000 = 1 second
	seg = append(seg, makeMoof(1, 90000, flagsIFrame, true)...)
	seg = append(seg, box("mdat", bytes.Repeat([]byte{3}, 70))...)

	info, err := probeSegment(bytes.NewReader(seg))
	if err != nil {
		t.Fatalf("probeSegment: %v", err)
	}
	if info.InitSize != int64(initSize) {
		t.Errorf("InitSize=%d, want %d", info.InitSize, initSize)
	}
	if info.FirstFragSize != int64(firstFrag) {
		t.Errorf("FirstFragSize=%d, want %d", info.FirstFragSize, firstFrag)
	}
	if info.Frames != 3 {
		t.Errorf("Frames=%d, want 3", info.Frames)
	}
	if info.Keyframes != 2 {
		t.Errorf("Keyframes=%d, want 2", info.Keyframes)
	}
	// The last frame starts at 90000 (1s) and lasts 6000, so the segment covers
	// 96000/90000 = 1.0667s. Reporting 1000 here would leave out the last frame -
	// exactly the bug that made the export splice move the DTS backwards.
	if info.DurationMs != 1066 {
		t.Errorf("DurationMs=%d, want 1066 (end of the last frame, not its start)",
			info.DurationMs)
	}
	if info.Gen == "" {
		t.Error("empty Gen; an orphan segment recovered at boot wouldn't find the init")
	}
}

// The same init must always produce the same generation, and different inits
// must produce different generations - that's what detects a codec change.
func TestInitGen(t *testing.T) {
	a := makeMoov(1, 90000, "hev1", false)
	b := makeMoov(1, 90000, "avc1", false)

	if g1, g2 := InitGen(a), InitGen(a); g1 != g2 {
		t.Error("InitGen is not deterministic")
	}
	if InitGen(a) == InitGen(b) {
		t.Error("inits with different codecs produced the same generation")
	}
}
