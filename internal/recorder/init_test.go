package recorder

import (
	"bytes"
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/fmp4"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// SPS reais de H.265 (os mesmos do teste do fmp4): o anunciado no init e o
// que a câmera de fato transmite.
var (
	spsDeclared1440p = fromHex("42010101400000030000030000030000030099a001402005a1fe5aee46c1ae5504")
	spsInband1080p   = fromHex("420101014000000300900000030000030096a003c08010e59e96e44a5780a7010202e10000030001000003000f5087fde100040160000c042d7c201040")
)

func fromHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// comTamanho prefixa cada NAL com o tamanho em 4 bytes, como no mdat.
func withSize(nals ...[]byte) []byte {
	var out []byte
	for _, n := range nals {
		out = append(out, u32(uint32(len(n)))...)
		out = append(out, n...)
	}
	return out
}

// moovH265 é um moov de vídeo H.265 cujo hvcC carrega o SPS anunciado.
func moovH265() []byte {
	cfg := make([]byte, 22)
	cfg[0], cfg[21] = 1, 0xf3 // prefixo de NAL de 4 bytes
	cfg = append(cfg, 1, 33)
	cfg = append(cfg, 0, 1, 0, byte(len(spsDeclared1440p)))
	cfg = append(cfg, spsDeclared1440p...)
	input := box("hev1", make([]byte, 78), box("hvcC", cfg))
	video := track(videoID, videoTimescale, "vide", input)
	trex := box("trex", u32(0), u32(videoID), u32(1), u32(0), u32(0), u32(flagsPFrame))
	return box("moov", box("mvhd", u32(0)), video, box("mvex", trex))
}

// O init que vai para o disco, e para o começo de cada segmento, já é o
// corrigido: é ele que o celular usa para preparar o decodificador.
func TestInitWrittenWithTheInbandSPS(t *testing.T) {
	body := append(box("ftyp", []byte("isom")), moovH265()...)
	vps := []byte{32 << 1, 1, 0xaa, 0xbb}
	pps := []byte{34 << 1, 1, 0xcc}
	idr := append([]byte{19 << 1, 1}, bytes.Repeat([]byte{0x42}, 64)...)
	body = append(body, moof(videoID, 0, flagsIFrame, 6000)...)
	body = append(body, box("mdat", withSize(vps, spsInband1080p, pps, idr))...)
	for i := uint64(1); i < 4; i++ {
		body = append(body, moof(videoID, i*6000, flagsPFrame, 6000)...)
		body = append(body, mdat(40)...)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	idx := store.New(dir).Camera("cam_teste")
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30}
	r := newRecorder(cam, go2rtc.New(config.Go2RTC{URL: srv.URL}), idx, discardLogger())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = r.session(ctx)

	inits, _ := filepath.Glob(filepath.Join(idx.InitDir(), "*"))
	if len(inits) != 1 {
		t.Fatalf("esperava 1 init em disco, achei %v", inits)
	}
	init, err := os.ReadFile(inits[0])
	if err != nil {
		t.Fatal(err)
	}
	_, vt, err := readInit(init)
	if err != nil {
		t.Fatal(err)
	}
	if vt.Width != 1920 || vt.Height != 1080 {
		t.Errorf("init em disco diz %dx%d, esperava 1920x1080", vt.Width, vt.Height)
	}
	if fmp4.InitGen(init) != r.Status().Gen {
		t.Error("a geração publicada não é a do init gravado")
	}
	if st := r.Status(); st.Width != 1920 || st.Height != 1080 {
		t.Errorf("status diz %dx%d, esperava 1920x1080", st.Width, st.Height)
	}

	segs, _ := filepath.Glob(filepath.Join(dir, "cam_teste", "2*", "*.mp4"))
	if len(segs) != 1 {
		t.Fatalf("esperava 1 segmento em disco, achei %v", segs)
	}
	seg, err := os.ReadFile(segs[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(seg, init) {
		t.Error("o segmento não começa pelo init corrigido")
	}
	// Os 4 quadros foram para o segmento: o keyframe que esperou o mdat não
	// se perdeu.
	if n := bytes.Count(seg, []byte("moof")); n != 4 {
		t.Errorf("%d quadros no segmento, esperava 4", n)
	}
}
