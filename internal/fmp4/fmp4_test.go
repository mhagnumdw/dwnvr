package fmp4

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// --- construtores de caixas sintéticas --------------------------------------

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

// Valores que o go2rtc realmente escreve (pkg/iso/atoms.go).
const (
	flagsIFrame    = 0x02000000
	flagsNonIFrame = 0x01010000
)

func makeMoov(trackID, timescale uint32, codec string, audio bool) []byte {
	tkhd := box("tkhd", u32(0), u32(0), u32(0), u32(trackID))
	mdhd := box("mdhd", u32(0), u32(0), u32(0), u32(timescale), u32(0))
	hdlr := box("hdlr", u32(0), u32(0), []byte("vide"))
	stsd := box("stsd", u32(0), u32(1), box(codec))
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

// makeMoof monta um moof com um único sample, como o go2rtc faz.
func makeMoof(trackID uint32, dts uint64, sampleFlags uint32, v1 bool) []byte {
	tfhd := box("tfhd", u32(0), u32(trackID))

	var tfdt []byte
	if v1 {
		tfdt = box("tfdt", []byte{1, 0, 0, 0}, u64(dts))
	} else {
		tfdt = box("tfdt", u32(0), u32(uint32(dts)))
	}

	// flags 0x000005 = data-offset-present | first-sample-flags-present
	trun := box("trun", u32(0x000005), u32(1), u32(0), u32(sampleFlags))
	return box("moof", box("mfhd", u32(0), u32(1)), box("traf", tfhd, tfdt, trun))
}

// --- testes -----------------------------------------------------------------

func TestParseMoovExtraiTrilhas(t *testing.T) {
	mv, err := ParseMoov(makeMoov(1, 90000, "hev1", true))
	if err != nil {
		t.Fatalf("ParseMoov: %v", err)
	}
	if len(mv.Tracks) != 2 {
		t.Fatalf("esperava 2 trilhas, veio %d", len(mv.Tracks))
	}

	vt, ok := mv.VideoTrack()
	if !ok {
		t.Fatal("não achou a trilha de vídeo")
	}
	if vt.ID != 1 || vt.Timescale != 90000 || vt.Codec != "hev1" {
		t.Errorf("trilha de vídeo errada: %+v", vt)
	}
	if vt.DefaultSampleFlags != flagsNonIFrame {
		t.Errorf("default_sample_flags do trex = %#x, esperava %#x", vt.DefaultSampleFlags, flagsNonIFrame)
	}
	if !mv.HasAudio() {
		t.Error("HasAudio() devia ser true")
	}
}

// A trilha de vídeo é quem decide onde cortar um segmento. Como o go2rtc marca
// samples de áudio com o mesmo sampleDependsOn2 dos keyframes, confundir as
// trilhas faria todo pacote de áudio parecer um ponto de corte válido.
func TestVideoTrackIgnoraAudio(t *testing.T) {
	mv, err := ParseMoov(makeMoov(1, 90000, "avc1", true))
	if err != nil {
		t.Fatal(err)
	}
	vt, _ := mv.VideoTrack()
	if vt.Handler != HandlerVideo {
		t.Errorf("VideoTrack devolveu handler %q", vt.Handler)
	}
}

func TestParseMoofDetectaKeyframe(t *testing.T) {
	tests := []struct {
		nome     string
		flags    uint32
		keyframe bool
	}{
		{"keyframe do go2rtc", flagsIFrame, true},
		{"frame P do go2rtc", flagsNonIFrame, false},
		{"apenas bit de não-sincronismo", sampleIsNonSync, false},
		{"tudo zerado conta como sync", 0, true},
		{"depends_on=1 sem bit de não-sync", 0x01000000, false},
	}
	for _, tt := range tests {
		t.Run(tt.nome, func(t *testing.T) {
			frag, err := ParseMoof(makeMoof(1, 1000, tt.flags, true), 1)
			if err != nil {
				t.Fatalf("ParseMoof: %v", err)
			}
			if frag.Keyframe != tt.keyframe {
				t.Errorf("flags %#x: Keyframe=%v, esperava %v", tt.flags, frag.Keyframe, tt.keyframe)
			}
			if frag.TrackID != 1 {
				t.Errorf("TrackID=%d", frag.TrackID)
			}
			if !frag.HasBaseTime || frag.BaseDecodeTime != 1000 {
				t.Errorf("tfdt=%d (presente=%v)", frag.BaseDecodeTime, frag.HasBaseTime)
			}
		})
	}
}

// Um moof da trilha de áudio nunca pode ser classificado como keyframe, senão o
// corte de segmento aconteceria fora de um keyframe de vídeo.
func TestParseMoofOutraTrilhaNuncaEhKeyframe(t *testing.T) {
	frag, err := ParseMoof(makeMoof(2, 500, flagsIFrame, true), 1)
	if err != nil {
		t.Fatal(err)
	}
	if frag.Keyframe {
		t.Error("moof da trilha 2 foi classificado como keyframe da trilha 1")
	}
	if frag.TrackID != 2 {
		t.Errorf("TrackID=%d, esperava 2", frag.TrackID)
	}
}

func TestRebaseMoofZeraOSegmento(t *testing.T) {
	for _, v1 := range []bool{true, false} {
		moof := makeMoof(1, 2_160_000, flagsIFrame, v1) // 24s a 90kHz
		if err := RebaseMoof(moof, map[uint32]uint64{1: 2_160_000}); err != nil {
			t.Fatalf("RebaseMoof: %v", err)
		}
		frag, err := ParseMoof(moof, 1)
		if err != nil {
			t.Fatal(err)
		}
		if frag.BaseDecodeTime != 0 {
			t.Errorf("v1=%v: tfdt=%d após rebase, esperava 0", v1, frag.BaseDecodeTime)
		}
	}
}

// O áudio pode estar alguns milissegundos atrás do keyframe de vídeo que abriu
// o segmento. Sem o clamp, o uint64 daria a volta e viraria um timestamp
// astronômico, que quebraria a reprodução.
func TestRebaseMoofNaoFazUnderflow(t *testing.T) {
	moof := makeMoof(1, 100, flagsIFrame, true)
	if err := RebaseMoof(moof, map[uint32]uint64{1: 5000}); err != nil {
		t.Fatal(err)
	}
	frag, _ := ParseMoof(moof, 1)
	if frag.BaseDecodeTime != 0 {
		t.Errorf("tfdt=%d, esperava 0 (clamp)", frag.BaseDecodeTime)
	}
}

func TestRebaseMoofPreservaTamanho(t *testing.T) {
	moof := makeMoof(1, 900000, flagsIFrame, true)
	antes := len(moof)
	if err := RebaseMoof(moof, map[uint32]uint64{1: 90000}); err != nil {
		t.Fatal(err)
	}
	if len(moof) != antes {
		t.Errorf("tamanho mudou de %d para %d; nenhum tamanho de caixa acima seria recalculado", antes, len(moof))
	}
}

// Trilhas com timescales diferentes precisam ser rebaseadas no MESMO instante,
// senão o desalinhamento real entre áudio e vídeo se perde.
func TestScaleTime(t *testing.T) {
	// 2 segundos a 90kHz devem virar 2 segundos a 16kHz.
	if got := ScaleTime(180000, 90000, 16000); got != 32000 {
		t.Errorf("ScaleTime(180000, 90000, 16000) = %d, esperava 32000", got)
	}
	if got := ScaleTime(12345, 90000, 90000); got != 12345 {
		t.Errorf("mesma timescale deveria ser identidade, veio %d", got)
	}
}

func TestReaderCaixaGrande(t *testing.T) {
	// size==1 sinaliza tamanho estendido de 64 bits.
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
		t.Errorf("tipo=%q", typ)
	}
	if len(got) != len(b) {
		t.Errorf("len=%d, esperava %d", len(got), len(b))
	}
}

func TestReaderRejeitaTamanhoInvalido(t *testing.T) {
	// Tamanho menor que o próprio cabeçalho: stream corrompido.
	b := append(u32(4), []byte("moof")...)
	r := NewReader(bytes.NewReader(b))
	if _, _, err := r.NextBox(); err == nil {
		t.Error("esperava erro para caixa com tamanho 4")
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
	// 90000 = 1 segundo
	seg = append(seg, makeMoof(1, 90000, flagsIFrame, true)...)
	seg = append(seg, box("mdat", bytes.Repeat([]byte{3}, 70))...)

	info, err := probeSegment(bytes.NewReader(seg))
	if err != nil {
		t.Fatalf("probeSegment: %v", err)
	}
	if info.InitSize != int64(initSize) {
		t.Errorf("InitSize=%d, esperava %d", info.InitSize, initSize)
	}
	if info.FirstFragSize != int64(firstFrag) {
		t.Errorf("FirstFragSize=%d, esperava %d", info.FirstFragSize, firstFrag)
	}
	if info.Frames != 3 {
		t.Errorf("Frames=%d, esperava 3", info.Frames)
	}
	if info.Keyframes != 2 {
		t.Errorf("Keyframes=%d, esperava 2", info.Keyframes)
	}
	if info.DurationMs != 1000 {
		t.Errorf("DurationMs=%d, esperava 1000", info.DurationMs)
	}
	if info.Gen == "" {
		t.Error("Gen vazio; um segmento órfão recuperado no boot não acharia o init")
	}
}

// O mesmo init tem que produzir sempre a mesma geração, e inits diferentes têm
// que produzir gerações diferentes — é isso que detecta troca de codec.
func TestInitGen(t *testing.T) {
	a := makeMoov(1, 90000, "hev1", false)
	b := makeMoov(1, 90000, "avc1", false)

	if InitGen(a) != InitGen(a) {
		t.Error("InitGen não é determinística")
	}
	if InitGen(a) == InitGen(b) {
		t.Error("inits com codecs diferentes produziram a mesma geração")
	}
}
