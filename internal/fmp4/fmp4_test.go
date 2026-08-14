package fmp4

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"strings"
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

func u16(v uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, v)
	return b
}

// SPS reais, capturados das câmeras do primeiro deployment. Valem mais que
// qualquer SPS sintético porque foi exatamente aqui que a suposição furou: a
// mesma câmera anuncia 1440p no init e transmite 1080p.
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

// avcC e hvcC montam o registro de configuração que carrega o SPS, no formato
// que o go2rtc escreve.
func avcC(sps []byte) []byte {
	b := []byte{1, sps[1], sps[2], sps[3], 0xff, 0xe1}
	b = append(b, u16(uint16(len(sps)))...)
	b = append(b, sps...)
	return box("avcC", b, []byte{0}) // nenhum PPS
}

func hvcC(sps []byte) []byte {
	b := make([]byte, 22)
	b[0] = 1
	b[21] = 0xf3 // lengthSizeMinusOne = 3, ou seja, prefixo de 4 bytes
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

// visualSampleEntry monta a sample entry de vídeo: 78 bytes de campos fixos
// depois do cabeçalho da caixa (reserved, data_reference_index, dimensões,
// resoluções, compressorname, depth) e então as caixas filhas.
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

// makeMoof monta um moof com um único sample, como o go2rtc faz.
func makeMoof(trackID uint32, dts uint64, sampleFlags uint32, v1 bool) []byte {
	return makeMoofDur(trackID, dts, sampleFlags, v1, 6000)
}

// makeMoofDur inclui a duração do sample no trun, que é como o go2rtc escreve.
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

// A resolução sai do SPS, não dos campos width/height da sample entry. Estes
// SPS vieram das câmeras de verdade, e o resultado esperado é o que o ffmpeg
// decodifica dos mesmos bytes.
func TestSPSSize(t *testing.T) {
	tests := []struct {
		nome  string
		codec string
		sps   []byte
		w, h  uint16
	}{
		{"h264 da porta", "avc1", spsH264720p, 1280, 720},
		{"h265 anunciado no init da cozinha", "hev1", spsH2651440p, 2560, 1440},
		{"h265 in-band que a cozinha realmente transmite", "hev1", spsH2651080p, 1920, 1080},
	}
	for _, tt := range tests {
		t.Run(tt.nome, func(t *testing.T) {
			w, h, ok := SPSSize(tt.codec, tt.sps)
			if !ok {
				t.Fatal("não conseguiu ler o SPS")
			}
			if w != tt.w || h != tt.h {
				t.Errorf("resolução = %dx%d, esperava %dx%d", w, h, tt.w, tt.h)
			}
		})
	}
}

// Um SPS truncado tem que virar "não sei", nunca um número inventado: a tela
// mostra "—" e ninguém é enganado.
func TestSPSSizeTruncadoNaoInventa(t *testing.T) {
	for n := 1; n < len(spsH2651080p); n++ {
		if _, _, ok := SPSSize("hev1", spsH2651080p[:n]); ok {
			// Um prefixo pode até ser legível, mas nunca pode passar por
			// resolução plausível se os campos não chegaram inteiros.
			w, h, _ := SPSSize("hev1", spsH2651080p[:n])
			if w == 1920 && h == 1080 {
				continue // o SPS acabou de ficar completo o bastante
			}
			t.Fatalf("SPS cortado em %d bytes devolveu %dx%d como se fosse válido", n, w, h)
		}
	}
	if _, _, ok := SPSSize("mp4a", spsH264720p); ok {
		t.Error("codec de áudio não devia produzir resolução")
	}
}

// A resolução do init vem do SPS que o hvcC/avcC carrega. A trilha de áudio,
// cuja sample entry tem outro layout, não pode inventar dimensão nenhuma.
func TestParseMoovExtraiResolucaoDoSPS(t *testing.T) {
	mv, err := ParseMoov(makeMoov(1, 90000, "hev1", true))
	if err != nil {
		t.Fatal(err)
	}
	vt, _ := mv.VideoTrack()
	if vt.Width != 2560 || vt.Height != 1440 {
		t.Errorf("resolução = %dx%d, esperava 2560x1440", vt.Width, vt.Height)
	}
	if vt.NALLengthSize != 4 {
		t.Errorf("NALLengthSize = %d, esperava 4", vt.NALLengthSize)
	}
	for _, tr := range mv.Tracks {
		if tr.Handler == HandlerAudio && (tr.Width != 0 || tr.Height != 0) {
			t.Errorf("trilha de áudio veio com dimensão %dx%d", tr.Width, tr.Height)
		}
	}
}

// Uma sample entry sem configuração de codec não pode derrubar a leitura do
// init: sem resolução ainda dá para gravar, e falhar aqui custaria a gravação.
func TestParseMoovSemConfigDeCodec(t *testing.T) {
	tkhd := box("tkhd", u32(0), u32(0), u32(0), u32(1))
	mdhd := box("mdhd", u32(0), u32(0), u32(0), u32(90000), u32(0))
	hdlr := box("hdlr", u32(0), u32(0), []byte("vide"))
	stsd := box("stsd", u32(0), u32(1), box("avc1")) // sem sequer os campos fixos
	trak := box("trak", tkhd, box("mdia", mdhd, hdlr, box("minf", box("stbl", stsd))))

	mv, err := ParseMoov(box("moov", box("mvhd", u32(0)), trak))
	if err != nil {
		t.Fatalf("ParseMoov: %v", err)
	}
	vt, ok := mv.VideoTrack()
	if !ok {
		t.Fatal("não achou a trilha de vídeo")
	}
	if vt.Codec != "avc1" {
		t.Errorf("codec = %q", vt.Codec)
	}
	if vt.Width != 0 || vt.Height != 0 {
		t.Errorf("esperava dimensão zerada, veio %dx%d", vt.Width, vt.Height)
	}
}

// FindSPS acha o parameter set no meio dos NALs do fragmento — o caminho que
// corrige um init mentiroso.
func TestFindSPSInband(t *testing.T) {
	nal := func(b []byte) []byte {
		return append(u32(uint32(len(b))), b...)
	}
	vps := append([]byte{32 << 1, 1}, 0xaa, 0xbb)
	idr := append([]byte{19 << 1, 1}, bytes.Repeat([]byte{0x42}, 64)...)
	mdat := box("mdat", nal(vps), nal(spsH2651080p), nal(idr))

	sps, ok := FindSPS("hev1", BoxPayload(mdat), 4)
	if !ok {
		t.Fatal("não achou o SPS in-band")
	}
	w, h, ok := SPSSize("hev1", sps)
	if !ok || w != 1920 || h != 1080 {
		t.Errorf("SPS in-band deu %dx%d (ok=%v), esperava 1920x1080", w, h, ok)
	}

	// Um fragmento só de dados não pode devolver lixo como se fosse SPS.
	semSPS := box("mdat", nal(idr))
	if _, ok := FindSPS("hev1", BoxPayload(semSPS), 4); ok {
		t.Error("achou SPS onde não há")
	}
	// Nem um comprimento maior que o próprio buffer pode virar leitura fora.
	if _, ok := FindSPS("hev1", []byte{0xff, 0xff, 0xff, 0xff, 1, 2}, 4); ok {
		t.Error("aceitou um NAL que não cabe no fragmento")
	}
}

// O custo precisa caber num hardware modesto: o parse roda uma vez por
// conexão, mas se ele fosse caro isso apareceria aqui.
func BenchmarkSPSSize(b *testing.B) {
	for b.Loop() {
		if _, _, ok := SPSSize("hev1", spsH2651080p); !ok {
			b.Fatal("SPS ilegível")
		}
	}
}

// O pior caso é o SPS chegar depois de todos os outros NALs do fragmento,
// obrigando a varredura a percorrer o keyframe inteiro.
func BenchmarkFindSPSPiorCaso(b *testing.B) {
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
			b.Fatal("não achou")
		}
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

// A duração do fragmento é o que separa "onde o último frame começa" de "onde
// o segmento termina". Ignorá-la fazia cada emenda da exportação colocar o
// segmento seguinte em cima do último frame do anterior, e o DTS regredia.
func TestFragmentEndTime(t *testing.T) {
	frag, err := ParseMoof(makeMoofDur(1, 90000, flagsIFrame, true, 6000), 1)
	if err != nil {
		t.Fatal(err)
	}
	if frag.Duration != 6000 {
		t.Errorf("Duration=%d, esperava 6000", frag.Duration)
	}
	if frag.EndTime() != 96000 {
		t.Errorf("EndTime=%d, esperava 96000 (90000 + 6000)", frag.EndTime())
	}
}

// Com vários samples num fragmento, a duração é a soma de todos.
func TestFragmentDuracaoMultiplosSamples(t *testing.T) {
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
		t.Errorf("Duration=%d, esperava 6000 (1000+2000+3000)", frag.Duration)
	}
}

// Quando o trun não traz durações, vale o default_sample_duration do tfhd.
func TestFragmentDuracaoPeloDefaultDoTfhd(t *testing.T) {
	// tfhd flags 0x8 = default-sample-duration-present
	tfhd := box("tfhd", u32(0x000008), u32(1), u32(1500))
	tfdt := box("tfdt", []byte{1, 0, 0, 0}, u64(0))
	trun := box("trun", u32(0x000001), u32(4), u32(0)) // só data_offset
	moof := box("moof", box("mfhd", u32(0), u32(1)), box("traf", tfhd, tfdt, trun))

	frag, err := ParseMoof(moof, 1)
	if err != nil {
		t.Fatal(err)
	}
	if frag.Duration != 6000 {
		t.Errorf("Duration=%d, esperava 6000 (4 × 1500)", frag.Duration)
	}
}

// Emendar dois segmentos usando o FIM do primeiro tem que produzir DTS
// estritamente crescente — é a invariante que a exportação depende.
func TestEmendaDeSegmentosNaoRegrideDTS(t *testing.T) {
	const dur = 6000
	// Segmento A: frames em 0, 6000, 12000; termina em 18000.
	var aEnd uint64
	for _, dts := range []uint64{0, 6000, 12000} {
		frag, err := ParseMoof(makeMoofDur(1, dts, flagsIFrame, true, dur), 1)
		if err != nil {
			t.Fatal(err)
		}
		aEnd = frag.EndTime()
	}
	if aEnd != 18000 {
		t.Fatalf("fim do segmento A = %d, esperava 18000", aEnd)
	}

	// Segmento B começa em zero e é deslocado pelo fim de A.
	moof := makeMoofDur(1, 0, flagsIFrame, true, dur)
	if err := ShiftMoof(moof, map[uint32]int64{1: int64(aEnd)}); err != nil {
		t.Fatal(err)
	}
	frag, err := ParseMoof(moof, 1)
	if err != nil {
		t.Fatal(err)
	}
	if frag.BaseDecodeTime <= 12000 {
		t.Errorf("primeiro frame de B em %d, deveria vir DEPOIS do último de A (12000)",
			frag.BaseDecodeTime)
	}
	if frag.BaseDecodeTime != 18000 {
		t.Errorf("primeiro frame de B em %d, esperava 18000 (sem buraco nem sobreposição)",
			frag.BaseDecodeTime)
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
	// O último frame começa em 90000 (1s) e dura 6000, então o segmento cobre
	// 96000/90000 = 1,0667s. Reportar 1000 aqui seria omitir o último frame —
	// exatamente o erro que fazia a emenda da exportação regredir o DTS.
	if info.DurationMs != 1066 {
		t.Errorf("DurationMs=%d, esperava 1066 (fim do último frame, não o início dele)",
			info.DurationMs)
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
