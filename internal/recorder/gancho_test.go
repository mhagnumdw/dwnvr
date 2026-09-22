package recorder

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mhagnumdw/dwnvr/internal/config"
	"github.com/mhagnumdw/dwnvr/internal/detect"
	"github.com/mhagnumdw/dwnvr/internal/go2rtc"
	"github.com/mhagnumdw/dwnvr/internal/store"
)

// --- caixas sintéticas ------------------------------------------------------
//
// O mínimo de fMP4 para o laço de gravação rodar: um moov com trilha de vídeo e
// trilha de áudio, e pares moof+mdat das duas. É o suficiente para reproduzir a
// armadilha que este teste existe para guardar - o fragmento de áudio chega
// exatamente igual ao de vídeo.

const (
	flagsQuadroI = 0x02000000
	flagsQuadroP = 0x01010000

	idVideo, idAudio = uint32(1), uint32(2)
	timescaleVideo   = uint32(90000)
)

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

func caixa(typ string, partes ...[]byte) []byte {
	var corpo []byte
	for _, p := range partes {
		corpo = append(corpo, p...)
	}
	out := make([]byte, 8, 8+len(corpo))
	binary.BigEndian.PutUint32(out[0:4], uint32(8+len(corpo)))
	copy(out[4:8], typ)
	return append(out, corpo...)
}

func trilha(id, timescale uint32, tipo string, amostra []byte) []byte {
	return caixa("trak",
		caixa("tkhd", u32(0), u32(0), u32(0), u32(id)),
		caixa("mdia",
			caixa("mdhd", u32(0), u32(0), u32(0), u32(timescale), u32(0)),
			caixa("hdlr", u32(0), u32(0), []byte(tipo)),
			caixa("minf", caixa("stbl", caixa("stsd", u32(0), u32(1), amostra)))))
}

func moovComAudio() []byte {
	// 78 bytes de campos fixos é o cabeçalho da sample entry de vídeo.
	video := trilha(idVideo, timescaleVideo, "vide", caixa("avc1", make([]byte, 78)))
	audio := trilha(idAudio, 16000, "soun", caixa("fLaC"))
	trex := append(
		caixa("trex", u32(0), u32(idVideo), u32(1), u32(0), u32(0), u32(flagsQuadroP)),
		caixa("trex", u32(0), u32(idAudio), u32(1), u32(0), u32(0), u32(0))...)
	return caixa("moov", caixa("mvhd", u32(0)), video, audio, caixa("mvex", trex))
}

func moof(id uint32, dts uint64, flags uint32, dur uint32) []byte {
	return caixa("moof",
		caixa("mfhd", u32(0), u32(1)),
		caixa("traf",
			caixa("tfhd", u32(0), u32(id)),
			caixa("tfdt", []byte{1, 0, 0, 0}, u64(dts)),
			// 0x000105 = data-offset | first-sample-flags | sample-duration
			caixa("trun", u32(0x000105), u32(1), u32(0), u32(flags), u32(dur))))
}

// mdat de `n` bytes de carga, para que len(box) - que é o sinal do gatilho -
// seja n+8.
func mdat(n int) []byte { return caixa("mdat", make([]byte, n)) }

// --- o espião ---------------------------------------------------------------

// espiao é um Mecanismo que não decide nada: só anota tudo o que o gancho lhe
// entrega. É como se vê, de fora, o que o laço de gravação está considerando
// "um quadro de vídeo".
type espiao struct {
	instantes []int64
	tamanhos  []int
	chaves    []bool
	zeros     int
}

func (e *espiao) Nome() string { return "espiao" }
func (e *espiao) Zera()        { e.zeros++ }

func (e *espiao) Quadro(instanteMs int64, bytes int, keyframe bool) detect.Veredito {
	e.instantes = append(e.instantes, instanteMs)
	e.tamanhos = append(e.tamanhos, bytes)
	e.chaves = append(e.chaves, keyframe)
	return detect.Veredito{}
}

// gravaStream sobe um go2rtc de mentira que serve `corpo` e encerra, roda uma
// sessão inteira do recorder contra ele e devolve o que o gancho viu.
func gravaStream(t *testing.T, corpo []byte) (*espiao, *store.Camera) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(corpo)
	}))
	t.Cleanup(srv.Close)

	idx := store.New(t.TempDir()).Camera("cam_teste")
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30}
	r := newRecorder(cam, go2rtc.New(config.Go2RTC{URL: srv.URL}), idx, mudo())

	// O espião entra no lugar do gatilho de verdade. O pedido pendente que o
	// newRecorder deixou é descartado, senão o primeiro quadro o substituiria.
	esp := &espiao{}
	r.detectPedido.Store(nil)
	r.mec = esp

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.session(ctx) // termina em EOF quando o corpo acaba
	return esp, idx
}

// TestGanchoIgnoraOsQuadrosDeAudio guarda a armadilha do `keyframePending`:
// fragmento de áudio chega exatamente igual ao de vídeo, e os samples de áudio
// também vêm marcados como sync. Sem o filtro de trilha, cada pacote de áudio
// entraria na estatística como um frame I gigante - e a régua da câmera passaria
// a ser a do áudio.
func TestGanchoIgnoraOsQuadrosDeAudio(t *testing.T) {
	var corpo []byte
	corpo = append(corpo, caixa("ftyp", []byte("isom"))...)
	corpo = append(corpo, moovComAudio()...)

	// Vídeo: um quadro I de 5.000 bytes e dois P de 100. Entre eles, dois
	// pacotes de áudio de 900 bytes cada.
	corpo = append(corpo, moof(idVideo, 0, flagsQuadroI, 6000)...)
	corpo = append(corpo, mdat(5000)...)
	corpo = append(corpo, moof(idAudio, 0, 0, 1024)...)
	corpo = append(corpo, mdat(900)...)
	corpo = append(corpo, moof(idVideo, 6000, flagsQuadroP, 6000)...)
	corpo = append(corpo, mdat(100)...)
	corpo = append(corpo, moof(idAudio, 1024, 0, 1024)...)
	corpo = append(corpo, mdat(900)...)
	corpo = append(corpo, moof(idVideo, 12000, flagsQuadroP, 6000)...)
	corpo = append(corpo, mdat(100)...)

	esp, _ := gravaStream(t, corpo)

	if len(esp.tamanhos) != 3 {
		t.Fatalf("o gancho viu %d quadros, esperava os 3 de vídeo: %v",
			len(esp.tamanhos), esp.tamanhos)
	}
	esperados := []int{5008, 108, 108}
	for i, want := range esperados {
		if esp.tamanhos[i] != want {
			t.Errorf("quadro %d: %d bytes, esperado %d - áudio vazou para o gatilho",
				i, esp.tamanhos[i], want)
		}
	}
	// Só o primeiro é frame I. Se o keyframe do vídeo sobrevivesse ao mdat de
	// áudio, o quadro seguinte também apareceria como chave.
	if !esp.chaves[0] || esp.chaves[1] || esp.chaves[2] {
		t.Errorf("marcação de frame I errada: %v", esp.chaves)
	}
}

// TestGanchoAncoraNoRelogioDaTimeline: o instante de cada quadro é o início do
// segmento (relógio de parede) mais o tempo de mídia decorrido dentro dele. É a
// mesma conta usada para medir o gatilho, e é o que faz a marca
// cair em cima do vídeo que ela aponta.
func TestGanchoAncoraNoRelogioDaTimeline(t *testing.T) {
	var corpo []byte
	corpo = append(corpo, caixa("ftyp", []byte("isom"))...)
	corpo = append(corpo, moovComAudio()...)
	// Quadros a cada 6000/90000 s = 66,7 ms.
	for i := range 5 {
		flags := uint32(flagsQuadroP)
		if i == 0 {
			flags = flagsQuadroI
		}
		corpo = append(corpo, moof(idVideo, uint64(i)*6000, flags, 6000)...)
		corpo = append(corpo, mdat(100)...)
	}

	antes := time.Now().UnixMilli()
	esp, _ := gravaStream(t, corpo)
	depois := time.Now().UnixMilli()

	if len(esp.instantes) != 5 {
		t.Fatalf("esperava 5 quadros, veio %d", len(esp.instantes))
	}
	// O primeiro quadro abre o segmento, então cai no relógio de parede.
	if esp.instantes[0] < antes || esp.instantes[0] > depois {
		t.Errorf("primeiro quadro em %d, fora da janela [%d, %d]",
			esp.instantes[0], antes, depois)
	}
	// Os seguintes andam com o relógio de MÍDIA, e não com o de chegada: 66 ms
	// cada, mesmo que a rede tenha entregado os cinco de uma vez.
	for i := 1; i < len(esp.instantes); i++ {
		got := esp.instantes[i] - esp.instantes[0]
		want := int64(i) * 6000 * 1000 / int64(timescaleVideo)
		if got != want {
			t.Errorf("quadro %d a %d ms do início, esperado %d ms", i, got, want)
		}
	}
}

// TestGanchoGravaOOnsetNoArquivoDoDia fecha a ponta: o que o mecanismo aprova
// vira linha em disco, no dia do próprio instante.
func TestGanchoGravaOOnsetNoArquivoDoDia(t *testing.T) {
	var corpo []byte
	corpo = append(corpo, caixa("ftyp", []byte("isom"))...)
	corpo = append(corpo, moovComAudio()...)
	corpo = append(corpo, moof(idVideo, 0, flagsQuadroI, 6000)...)
	corpo = append(corpo, mdat(100)...)
	corpo = append(corpo, moof(idVideo, 6000, flagsQuadroP, 6000)...)
	corpo = append(corpo, mdat(100)...)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(corpo)
	}))
	defer srv.Close()

	idx := store.New(t.TempDir()).Camera("cam_teste")
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30}
	r := newRecorder(cam, go2rtc.New(config.Go2RTC{URL: srv.URL}), idx, mudo())
	r.detectPedido.Store(nil)
	r.mec = sempreDispara{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.session(ctx)

	if got := r.onsets.Load(); got != 2 {
		t.Fatalf("contador de onsets = %d, esperado 2", got)
	}
	evs, err := idx.LoadEventos(time.Now().Format(store.DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 {
		t.Fatalf("esperava 2 marcas no arquivo do dia, veio %d", len(evs))
	}
}

type sempreDispara struct{}

func (sempreDispara) Nome() string { return "sempre" }
func (sempreDispara) Zera()        {}
func (sempreDispara) Quadro(int64, int, bool) detect.Veredito {
	return detect.Veredito{Onset: true, Candidato: true}
}

// TestTrocaDeDetectEmPlenoVooNaoCorre guarda a promessa do pedeDetect: a API
// muda a sensibilidade de uma câmera enquanto o laço de gravação está no meio
// de uma sessão, sem lock compartilhado e sem reconectar. Rodado com -race, é
// o que prova que a passagem pelo ponteiro atômico basta.
func TestTrocaDeDetectEmPlenoVooNaoCorre(t *testing.T) {
	var cabeca []byte
	cabeca = append(cabeca, caixa("ftyp", []byte("isom"))...)
	cabeca = append(cabeca, moovComAudio()...)

	// Um go2rtc que entrega quadros devagar, para a sessão ainda estar viva
	// enquanto as trocas acontecem.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(cabeca)
		fl, _ := w.(http.Flusher)
		for i := range 300 {
			flags := uint32(flagsQuadroP)
			if i%30 == 0 {
				flags = flagsQuadroI
			}
			w.Write(moof(idVideo, uint64(i)*6000, flags, 6000))
			w.Write(mdat(100 + i%7))
			if fl != nil {
				fl.Flush()
			}
			if r.Context().Err() != nil {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}))
	defer srv.Close()

	idx := store.New(t.TempDir()).Camera("cam_teste")
	ligada := true
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30,
		Detect: &ligada, DetectMecanismo: "kleinberg-p", DetectSensibilidade: 4}
	r := newRecorder(cam, go2rtc.New(config.Go2RTC{URL: srv.URL}), idx, mudo())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fim := make(chan struct{})
	go func() {
		defer close(fim)
		r.session(ctx)
	}()

	// Do lado da "API": liga, desliga, troca mecanismo e nível, sem parar.
	desligada := false
	for i := range 200 {
		c := cam
		switch i % 3 {
		case 0:
			c.Detect = &desligada
		case 1:
			c.DetectMecanismo, c.DetectSensibilidade = "periodico", 1+i%5
		default:
			c.DetectSensibilidade = 1 + i%5
		}
		r.pedeDetect(c)
		time.Sleep(200 * time.Microsecond)
	}
	<-fim
}

// roteiro é um Mecanismo que devolve vereditos escritos à mão, um por quadro de
// vídeo. É como se escolhe, de fora, onde o pedaço tem que ser cortado.
type roteiro struct {
	vereditos []detect.Veredito
	i         int
}

func (r *roteiro) Nome() string { return "roteiro" }
func (r *roteiro) Zera()        {}
func (r *roteiro) Quadro(int64, int, bool) detect.Veredito {
	var v detect.Veredito
	if r.i < len(r.vereditos) {
		v = r.vereditos[r.i]
	}
	r.i++
	return v
}

// TestGanchoCortaOPedacoComoFoiParaODisco fecha a ponta do recortador dentro do
// laço de gravação: o pedaço é o init da conexão seguido dos fragmentos de
// VÍDEO do frame I até o quadro a olhar, com os mesmos bytes que o segmento
// gravou - o áudio fica de fora, e o candidato que ficou no GOP velho
// sobrevive ao frame I seguinte.
func TestGanchoCortaOPedacoComoFoiParaODisco(t *testing.T) {
	cabeca := append(caixa("ftyp", []byte("isom")), moovComAudio()...)
	corpo := append([]byte(nil), cabeca...)
	// Quadros de vídeo: I P P P I P. Áudio entre eles.
	chaves := []bool{true, false, false, false, true, false}
	var fragsVideo [][]byte
	for i, chave := range chaves {
		flags := uint32(flagsQuadroP)
		if chave {
			flags = flagsQuadroI
		}
		m, d := moof(idVideo, uint64(i)*6000, flags, 6000), mdat(100+i)
		corpo = append(corpo, m...)
		corpo = append(corpo, d...)
		fragsVideo = append(fragsVideo, append(append([]byte(nil), m...), d...))
		corpo = append(corpo, moof(idAudio, uint64(i)*1024, 0, 1024)...)
		corpo = append(corpo, mdat(50)...)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(corpo)
	}))
	defer srv.Close()

	dir := t.TempDir()
	idx := store.New(dir).Camera("cam_teste")
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30}
	r := newRecorder(cam, go2rtc.New(config.Go2RTC{URL: srv.URL}), idx, mudo())
	r.detectPedido.Store(nil)
	// Onset no quadro 1, pico no 2, frame I no 4 dentro da janela, corte no 5.
	r.mec = &roteiro{vereditos: []detect.Veredito{
		{}, {Onset: true, Candidato: true}, {Candidato: true}, {}, {}, {Corta: true},
	}}
	var saiu []detect.Pedaco
	r.pedacos = func(p detect.Pedaco) { saiu = append(saiu, p) }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.session(ctx)

	if len(saiu) != 1 {
		t.Fatalf("%d pedaços, esperado 1", len(saiu))
	}
	p := saiu[0]
	if !bytes.HasPrefix(p.Fmp4, cabeca) {
		t.Fatal("o pedaço não começa pelo init da conexão")
	}
	fragmentos := p.Fmp4[len(cabeca):]

	// Os fragmentos são os que foram para o disco, byte a byte - o moof já
	// rebaseado. É o que faz o pedaço decodificar igual ao segmento gravado.
	segs, _ := filepath.Glob(filepath.Join(dir, "cam_teste", "2*", "*.mp4"))
	if len(segs) != 1 {
		t.Fatalf("esperava 1 segmento em disco, achei %v", segs)
	}
	gravado, err := os.ReadFile(segs[0])
	if err != nil {
		t.Fatal(err)
	}
	// No disco o áudio fica entre um quadro e outro; no pedaço, não. Então a
	// conferência é caixa a caixa: cada moof+mdat do pedaço está no segmento.
	var caixas [][]byte
	for resto := fragmentos; len(resto) >= 8; {
		n := int(binary.BigEndian.Uint32(resto))
		if n < 8 || n > len(resto) {
			t.Fatalf("caixa de %d bytes num resto de %d", n, len(resto))
		}
		caixas, resto = append(caixas, resto[:n]), resto[n:]
	}
	// Exatamente os de vídeo do frame I ao pico: quadros 0, 1 e 2, sem áudio.
	if len(caixas) != 6 {
		t.Fatalf("%d caixas no pedaço, esperado 6 (moof+mdat de três quadros)", len(caixas))
	}
	for i := 0; i < len(caixas); i += 2 {
		par := append(append([]byte(nil), caixas[i]...), caixas[i+1]...)
		if !bytes.Contains(gravado, par) {
			t.Errorf("quadro %d do pedaço não tem os bytes que o segmento gravou", i/2)
		}
		if len(par) != len(fragsVideo[i/2]) {
			t.Errorf("quadro %d do pedaço com %d bytes, o de vídeo tem %d", i/2, len(par), len(fragsVideo[i/2]))
		}
	}
	// O onset é o quadro 1 e o pico é o 2, no relógio de mídia truncado em ms.
	ms := func(q int64) int64 { return q * 6000 * 1000 / int64(timescaleVideo) }
	if p.QuadroMs-p.OnsetMs != ms(2)-ms(1) {
		t.Errorf("onset %d e quadro %d: o quadro a olhar devia ser o seguinte ao onset",
			p.OnsetMs, p.QuadroMs)
	}
}

// TestGanchoSemDestinoNaoGuardaGOP: quem não instalou o detector não paga a
// cópia do GOP.
func TestGanchoSemDestinoNaoGuardaGOP(t *testing.T) {
	var corpo []byte
	corpo = append(corpo, caixa("ftyp", []byte("isom"))...)
	corpo = append(corpo, moovComAudio()...)
	corpo = append(corpo, moof(idVideo, 0, flagsQuadroI, 6000)...)
	corpo = append(corpo, mdat(100)...)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(corpo)
	}))
	defer srv.Close()

	idx := store.New(t.TempDir()).Camera("cam_teste")
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30}
	r := newRecorder(cam, go2rtc.New(config.Go2RTC{URL: srv.URL}), idx, mudo())
	r.detectPedido.Store(nil)
	r.mec = sempreDispara{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.session(ctx)
	if r.recorte != nil {
		t.Error("sem destino para o pedaço, o recortador não devia existir")
	}
}

// detectorQueVePessoa responde sempre a mesma pessoa, no mesmo lugar.
type detectorQueVePessoa struct{}

func (detectorQueVePessoa) Nome() string { return "pessoa" }
func (detectorQueVePessoa) Olha(context.Context, detect.Pedaco) (detect.Visao, error) {
	return detect.Visao{
		Achados: []detect.Achado{{Classe: "person", Score: 0.87654, Caixa: [4]float64{0.1, 0.1, 0.2, 0.4}}},
		Quadro:  []byte("\xff\xd8jpeg-de-teste\xff\xd9"),
	}, nil
}

// TestDoOnsetAMarcaDeObjeto fecha o caminho inteiro do lado do dwnvr: o
// onset vira pedaço, o pedaço passa pela fila e pelo detector, o rastreio
// deixa a primeira pessoa virar marca - e não a segunda, no mesmo lugar -, e a
// marca vai para o arquivo do dia no instante do ONSET. O funil fecha a conta.
func TestDoOnsetAMarcaDeObjeto(t *testing.T) {
	// O stream chega em duas metades, com uma folga entre elas: o segundo
	// onset precisa encontrar o primeiro já fora da fila, sendo olhado - o
	// teste não depende de quantos lugares a câmera tem.
	var metades [2][]byte
	metades[0] = append(caixa("ftyp", []byte("isom")), moovComAudio()...)
	for i := range 6 {
		flags := uint32(flagsQuadroP)
		if i%3 == 0 {
			flags = flagsQuadroI
		}
		metades[i/3] = append(metades[i/3], moof(idVideo, uint64(i)*6000, flags, 6000)...)
		metades[i/3] = append(metades[i/3], mdat(100)...)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(metades[0])
		w.(http.Flusher).Flush()
		time.Sleep(200 * time.Millisecond)
		w.Write(metades[1])
	}))
	defer srv.Close()

	idx := store.New(t.TempDir()).Camera("cam_teste")
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30}
	r := newRecorder(cam, go2rtc.New(config.Go2RTC{URL: srv.URL}), idx, mudo())
	r.detectPedido.Store(nil)
	// Dois onsets, cada um com a janela fechando no quadro seguinte.
	r.mec = &roteiro{vereditos: []detect.Veredito{
		{}, {Onset: true, Candidato: true}, {Corta: true},
		{}, {Onset: true, Candidato: true}, {Corta: true},
	}}
	olhadas := make(chan detect.Olhada, 4)
	fila := detect.NovaFila(detectorQueVePessoa{}, func(o detect.Olhada) {
		r.olhou(o)
		olhadas <- o
	}, mudo())
	r.pedacos = r.ofereceA(fila)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go fila.Roda(ctx)

	r.session(ctx)
	var onsets, quadros []int64
	for range 2 {
		select {
		case o := <-olhadas:
			onsets = append(onsets, o.Pedaco.OnsetMs)
			quadros = append(quadros, o.Pedaco.QuadroMs)
		case <-ctx.Done():
			t.Fatal("a fila não devolveu as duas olhadas")
		}
	}

	evs, err := idx.LoadEventos(time.Now().Format(store.DayLayout))
	if err != nil {
		t.Fatal(err)
	}
	var objetos []store.Evento
	for _, ev := range evs {
		if ev.EhObjeto() {
			objetos = append(objetos, ev)
		}
	}
	if len(objetos) != 1 {
		t.Fatalf("%d marcas de objeto, esperada 1: a pessoa parada no mesmo lugar não pode marcar de novo", len(objetos))
	}
	// A caixa vai à parte: é ponteiro, e o != compararia o endereço.
	got := objetos[0]
	caixa := got.Caixa
	got.Caixa = nil
	jpeg := []byte("\xff\xd8jpeg-de-teste\xff\xd9")
	want := store.Evento{InstanteMs: onsets[0], Familia: "pessoa", Classe: "person", Score: 0.877,
		QuadroMs: quadros[0], TemQuadro: true}
	if got != want {
		t.Errorf("marca %+v, esperado %+v", got, want)
	}
	if caixa == nil || *caixa != [4]float64{0.1, 0.1, 0.2, 0.4} {
		t.Errorf("caixa %v, esperada a do detector, [0.1 0.1 0.2 0.4]", caixa)
	}

	// O quadro que a tela de Detecções mostra: o arquivo tem o nome do
	// instante da marca e os bytes que o detector mandou.
	lido, err := os.ReadFile(idx.QuadroPath(time.Now().Format(store.DayLayout), got.InstanteMs))
	if err != nil {
		t.Fatalf("lendo o quadro da marca: %v", err)
	}
	if !bytes.Equal(lido, jpeg) {
		t.Errorf("quadro gravado %q, esperado %q", lido, jpeg)
	}

	f := r.funil()
	if f.Pedacos != 2 || f.ComObjeto != 1 || f.SemObjeto != 1 || f.NaFila != 0 || f.SemVideo != 0 {
		t.Errorf("funil %+v: esperava 2 pedaços, 1 com objeto, 1 sem, nada na fila nem sem vídeo", *f)
	}
}

// TestRecortadorNasceEMorreComADeteccao: com o detector configurado, a câmera
// com a detecção desligada não guarda GOP, e desligar a detecção devolve os
// buffers. O que já foi contado como sem vídeo não zera na troca.
func TestRecortadorNasceEMorreComADeteccao(t *testing.T) {
	idx := store.New(t.TempDir()).Camera("cam_teste")
	desligada, ligada := false, true
	cam := config.Camera{ID: "cam_teste", Name: "Teste", Enabled: true, SegmentSeconds: 30,
		Detect: &desligada, DetectMecanismo: "kleinberg-p", DetectSensibilidade: 4}
	r := newRecorder(cam, nil, idx, mudo())
	r.pedacos = func(detect.Pedaco) {}
	r.initAtual = []byte("init")
	quadro := func(ms int64, v detect.Veredito) {
		if v != (detect.Veredito{}) {
			r.mec = &roteiro{vereditos: []detect.Veredito{v}}
		}
		r.movimento(ms, false, moof(idVideo, 0, flagsQuadroP, 6000), mdat(100))
	}

	quadro(0, detect.Veredito{})
	if r.recorte != nil {
		t.Fatal("detecção desligada e o recortador nasceu")
	}

	cam.Detect = &ligada
	r.pedeDetect(cam)
	quadro(40, detect.Veredito{})
	if r.recorte == nil {
		t.Fatal("detecção ligada e o recortador não nasceu")
	}
	// Um onset sem frame I guardado: quando a detecção desligar, ele fica
	// sem vídeo.
	quadro(80, detect.Veredito{Onset: true, Candidato: true})

	cam.Detect = &desligada
	r.pedeDetect(cam)
	quadro(120, detect.Veredito{})
	if r.recorte != nil {
		t.Fatal("detecção desligada e o recortador continua vivo")
	}
	if n := r.semVideo.Load(); n != 1 {
		t.Fatalf("semVideo %d, esperado 1", n)
	}

	cam.Detect = &ligada
	r.pedeDetect(cam)
	quadro(160, detect.Veredito{})
	if n := r.semVideo.Load(); n != 1 {
		t.Errorf("semVideo %d depois de religar, esperado o 1 de antes", n)
	}
}

// TestFunilFechaAConta: NaFila é o que sobra dos onsets depois das fatias com
// desfecho, e nunca fica negativo.
func TestFunilFechaAConta(t *testing.T) {
	r := &Recorder{pedacos: func(detect.Pedaco) {}}
	r.onsets.Add(7)
	r.semVideo.Add(1)
	r.descartados.Add(1)
	r.comObjeto.Add(2)
	if f := r.funil(); f.NaFila != 3 {
		t.Errorf("naFila %d, esperado 3: 7 onsets, 4 com desfecho", f.NaFila)
	}
	r.semObjeto.Add(5)
	if f := r.funil(); f.NaFila != 0 {
		t.Errorf("naFila %d, esperado 0 com mais desfechos que onsets", f.NaFila)
	}
	if (&Recorder{}).funil() != nil {
		t.Error("sem detector, o funil tinha que ser nil")
	}
}
