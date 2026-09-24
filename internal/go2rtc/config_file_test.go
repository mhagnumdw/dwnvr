package go2rtc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

const arquivoDeTeste = `
streams:
  cam_rtsp: rtsp://admin:senha@192.168.0.61:554/onvif1
  cam_trocada: rtsp://admin:senha@192.168.0.99:554/onvif1
  cam_ffmpeg_ativa: ffmpeg:virtual?video=testsrc&size=640x360#video=h264
  cam_duas_fontes:
    - rtsp://192.168.0.62/main
    - ffmpeg:cam_duas_fontes#audio=aac
  cam_com_parametro: rtsp://192.168.0.63/main#backchannel=0
  cam_nova: rtsp://192.168.0.64/main
  cam_vazia:
rtsp:
  listen: ":8554"
`

func TestDivergencias(t *testing.T) {
	rodando := map[string]Stream{
		"cam_rtsp":    {Producers: []Producer{{URL: "rtsp://admin:senha@192.168.0.61:554/onvif1"}}},
		"cam_trocada": {Producers: []Producer{{URL: "rtsp://admin:senha@192.168.0.60:554/onvif1"}}},
		// Em uso, o produtor ffmpeg traz o comando expandido e o url vazio:
		// não há como comparar, e a câmera não pode aparecer como alterada.
		"cam_ffmpeg_ativa": {Producers: []Producer{{}}},
		"cam_duas_fontes": {Producers: []Producer{
			{URL: "rtsp://192.168.0.62/main"},
			{URL: "ffmpeg:cam_duas_fontes#audio=aac"},
		}},
		"cam_com_parametro": {Producers: []Producer{{URL: "rtsp://192.168.0.63/main"}}},
		"cam_vazia":         {},
		"cam_removida":      {Producers: []Producer{{URL: "rtsp://192.168.0.65/main"}}},
	}

	d, err := Divergencias([]byte(arquivoDeTeste), rodando)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"cam_nova"}; !slices.Equal(d.Novas, want) {
		t.Errorf("Novas = %v, want %v", d.Novas, want)
	}
	if want := []string{"cam_trocada"}; !slices.Equal(d.Alteradas, want) {
		t.Errorf("Alteradas = %v, want %v", d.Alteradas, want)
	}
	if want := []string{"cam_removida"}; !slices.Equal(d.ForaDoArquivo, want) {
		t.Errorf("ForaDoArquivo = %v, want %v", d.ForaDoArquivo, want)
	}
}

func TestDivergenciasFonteAMais(t *testing.T) {
	arquivo := "streams:\n  cam:\n    - rtsp://a/main\n    - rtsp://a/sub\n"
	rodando := map[string]Stream{"cam": {Producers: []Producer{{URL: "rtsp://a/main"}}}}

	d, err := Divergencias([]byte(arquivo), rodando)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(d.Alteradas, []string{"cam"}) {
		t.Errorf("Alteradas = %v, want [cam]", d.Alteradas)
	}
}

func TestDivergenciasIgual(t *testing.T) {
	arquivo := "streams:\n  cam: rtsp://a/main\n"
	rodando := map[string]Stream{"cam": {Producers: []Producer{{URL: "rtsp://a/main"}}}}

	d, err := Divergencias([]byte(arquivo), rodando)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Vazia() {
		t.Errorf("divergência onde não há: %+v", d)
	}
}

func TestDivergenciasArquivoIlegivel(t *testing.T) {
	if _, err := Divergencias([]byte("streams: [nao: fecha"), nil); err == nil {
		t.Error("yaml quebrado deveria dar erro")
	}
}

func TestReiniciarUsaPOST(t *testing.T) {
	var metodo, caminho string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metodo, caminho = r.Method, r.URL.Path
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if err := c.Reiniciar(context.Background()); err != nil {
		t.Fatal(err)
	}
	if metodo != http.MethodPost || caminho != "/api/restart" {
		t.Errorf("pedido = %s %s, want POST /api/restart", metodo, caminho)
	}
}
