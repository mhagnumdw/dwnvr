package fmp4

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// SPS e PPS reais de uma câmera H.264 do primeiro deployment: o init anuncia
// 320x180 e cada keyframe traz, in-band, o SPS de 640x360 que ela transmite.
var (
	spsH264Anunciado180p = mustHex("6742001495a85067e740")
	spsH264Inband360p    = mustHex("274de0288d680a02ff9610000003001000000301e0f1422a")
	ppsH264Inband        = mustHex("28ee02d248")
)

// nalComTamanho prefixa o NAL com o tamanho em 4 bytes, como no mdat.
func nalComTamanho(nals ...[]byte) []byte {
	var out []byte
	for _, n := range nals {
		out = append(out, u32(uint32(len(n)))...)
		out = append(out, n...)
	}
	return out
}

// keyframeH265 é o conteúdo do mdat de um keyframe com os parameter sets
// in-band antes do IDR.
func keyframeH265(sps []byte) []byte {
	vps := []byte{32 << 1, 1, 0xaa, 0xbb}
	pps := []byte{34 << 1, 1, 0xcc}
	idr := append([]byte{19 << 1, 1}, bytes.Repeat([]byte{0x42}, 64)...)
	return nalComTamanho(vps, sps, pps, idr)
}

func trilhaDeVideo(t *testing.T, init []byte) Track {
	t.Helper()
	moov := init[bytes.Index(init, []byte("moov"))-4:]
	mv, err := ParseMoov(moov)
	if err != nil {
		t.Fatalf("init ilegível: %v", err)
	}
	vt, ok := mv.VideoTrack()
	if !ok {
		t.Fatal("init sem trilha de vídeo")
	}
	return vt
}

// O caso da câmera que anuncia o stream principal e transmite o substream:
// o init sai com o SPS in-band, e o resto do moov como estava.
func TestCorrigeInitTrocaOSPSDoInit(t *testing.T) {
	ftyp := box("ftyp", []byte("isom"))
	moov := makeMoov(1, 90000, "hev1", true)
	init := append(append([]byte(nil), ftyp...), moov...)
	vt := trilhaDeVideo(t, init)

	novo, ok := CorrigeInit(init, vt, keyframeH265(spsH2651080p))
	if !ok {
		t.Fatal("não corrigiu um init de 2560x1440 com SPS in-band de 1920x1080")
	}
	if !bytes.HasPrefix(novo, ftyp) {
		t.Error("o ftyp não ficou no começo, como estava")
	}

	nvt := trilhaDeVideo(t, novo)
	if nvt.Width != 1920 || nvt.Height != 1080 {
		t.Errorf("init corrigido diz %dx%d, esperava 1920x1080", nvt.Width, nvt.Height)
	}
	if nvt.NALLengthSize != vt.NALLengthSize || nvt.Timescale != vt.Timescale || nvt.ID != vt.ID {
		t.Errorf("a trilha mudou além do SPS: antes %+v, depois %+v", vt, nvt)
	}

	// Os três parameter sets in-band estão no hvcC, e o SPS anunciado saiu.
	if !bytes.Contains(novo, spsH2651080p) || bytes.Contains(novo, spsH2651440p) {
		t.Error("o hvcC não ficou com o SPS in-band, e só com ele")
	}
	for _, ps := range [][]byte{{32 << 1, 1, 0xaa, 0xbb}, {34 << 1, 1, 0xcc}} {
		if !bytes.Contains(novo, ps) {
			t.Errorf("parameter set %x não foi para o hvcC", ps)
		}
	}

	// A sample entry diz a resolução nova: bytes 24 a 27 depois do cabeçalho.
	i := bytes.Index(novo, []byte("hev1")) + 4
	if w, h := binary.BigEndian.Uint16(novo[i+24:]), binary.BigEndian.Uint16(novo[i+26:]); w != 1920 || h != 1080 {
		t.Errorf("sample entry diz %dx%d, esperava 1920x1080", w, h)
	}

	// A trilha de áudio vai byte a byte como veio.
	audio := moov[bytes.LastIndex(moov, []byte("trak"))-4:]
	audio = audio[:binary.BigEndian.Uint32(audio)]
	if !bytes.Contains(novo, audio) {
		t.Error("a trilha de áudio mudou")
	}
}

// O H.264 passa pelo mesmo caminho, com o avcC.
func TestCorrigeInitH264(t *testing.T) {
	cfg := []byte{1, spsH264Anunciado180p[1], spsH264Anunciado180p[2], spsH264Anunciado180p[3], 0xff, 0xe1}
	cfg = append(cfg, u16(uint16(len(spsH264Anunciado180p)))...)
	cfg = append(cfg, spsH264Anunciado180p...)
	cfg = append(cfg, 1, 0, 4, 0x68, 0xce, 0x3c, 0x80)

	tkhd := box("tkhd", make([]byte, tkhdCorpoV0)) // versão 0
	binary.BigEndian.PutUint32(tkhd[8+12:], 1)     // track_ID
	mdhd := box("mdhd", u32(0), u32(0), u32(0), u32(90000), u32(0))
	hdlr := box("hdlr", u32(0), u32(0), []byte("vide"))
	stsd := box("stsd", u32(0), u32(1), visualSampleEntry("avc1", box("avcC", cfg)))
	trak := box("trak", tkhd, box("mdia", mdhd, hdlr, box("minf", box("stbl", stsd))))
	init := box("moov", box("mvhd", u32(0)), trak)
	vt := trilhaDeVideo(t, init)
	if vt.Width != 320 || vt.Height != 180 {
		t.Fatalf("init de partida diz %dx%d, esperava 320x180", vt.Width, vt.Height)
	}

	idr := append([]byte{0x65}, bytes.Repeat([]byte{0x42}, 64)...)
	novo, ok := CorrigeInit(init, vt, nalComTamanho(spsH264Inband360p, ppsH264Inband, idr))
	if !ok {
		t.Fatal("não corrigiu o init H.264")
	}
	nvt := trilhaDeVideo(t, novo)
	if nvt.Width != 640 || nvt.Height != 360 {
		t.Errorf("init corrigido diz %dx%d, esperava 640x360", nvt.Width, nvt.Height)
	}
	if !bytes.Contains(novo, ppsH264Inband) || bytes.Contains(novo, []byte{0x68, 0xce, 0x3c, 0x80}) {
		t.Error("o avcC não ficou com o PPS in-band, e só com ele")
	}
	// Perfil e nível do registro passam a ser os do SPS in-band.
	c := novo[bytes.Index(novo, []byte("avcC"))+4:]
	if c[1] != spsH264Inband360p[1] || c[3] != spsH264Inband360p[3] {
		t.Errorf("perfil %d e nível %d no avcC, o SPS diz %d e %d",
			c[1], c[3], spsH264Inband360p[1], spsH264Inband360p[3])
	}

	// O tkhd também diz a resolução nova, em ponto fixo 16.16.
	k := bytes.Index(novo, []byte("tkhd")) + 4
	if w, h := binary.BigEndian.Uint32(novo[k+tkhdCorpoV0-8:]), binary.BigEndian.Uint32(novo[k+tkhdCorpoV0-4:]); w != 640<<16 || h != 360<<16 {
		t.Errorf("tkhd diz %dx%d, esperava 640x360", w>>16, h>>16)
	}
}

// Sem o que corrigir, o init da câmera vale: nada muda a geração de quem já
// grava certo.
func TestCorrigeInitSemDivergenciaNaoMexe(t *testing.T) {
	init := append(box("ftyp", []byte("isom")), makeMoov(1, 90000, "hev1", false)...)
	vt := trilhaDeVideo(t, init)

	if _, ok := CorrigeInit(init, vt, keyframeH265(spsH2651440p)); ok {
		t.Error("corrigiu um init cujo SPS tem a resolução do in-band")
	}
	idr := append([]byte{19 << 1, 1}, bytes.Repeat([]byte{0x42}, 64)...)
	if _, ok := CorrigeInit(init, vt, nalComTamanho(idr)); ok {
		t.Error("corrigiu sem nenhum SPS in-band")
	}
	if _, ok := CorrigeInit(init, vt, []byte{0xff, 0xff, 0xff, 0xff, 1}); ok {
		t.Error("corrigiu a partir de um mdat malformado")
	}
	audio := vt
	audio.Codec = "mp4a"
	if _, ok := CorrigeInit(init, audio, keyframeH265(spsH2651080p)); ok {
		t.Error("corrigiu uma trilha que não é H.264 nem H.265")
	}
}
