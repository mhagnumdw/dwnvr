// Command spike-serve valida o segundo risco do dwnvr: tocar os segmentos
// gravados no navegador via Media Source Extensions, com HEVC.
//
// Serve os segmentos de um diretório e uma página que os costura num único
// <video> — que é exatamente o mecanismo da tela de gravações.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mhagnumdw/dwnvr/internal/fmp4"
)

type segment struct {
	Name       string `json:"name"`
	DurationMs int64  `json:"durationMs"`
	Size       int64  `json:"size"`
	InitSize   int64  `json:"initSize"`
	FirstFrag  int64  `json:"firstFragSize"`
	Keyframes  int    `json:"keyframes"`
	Frames     int    `json:"frames"`
	Codec      string `json:"codec"`
	MimeCodec  string `json:"mimeCodec"`
}

func main() {
	dir := flag.String("dir", "./rec", "diretório com os segmentos .mp4")
	addr := flag.String("addr", ":8099", "endereço de escuta")
	flag.Parse()

	segs, err := scan(*dir)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%d segmentos em %s", len(segs), *dir)
	for _, s := range segs {
		log.Printf("  %-20s %6.2fs %7.2f MB codec=%s init=%dB frag0=%dB kf=%d/%d",
			s.Name, float64(s.DurationMs)/1000, float64(s.Size)/(1<<20),
			s.Codec, s.InitSize, s.FirstFrag, s.Keyframes, s.Frames)
	}

	http.HandleFunc("/segments.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(segs)
	})

	// /seg/ serve o arquivo inteiro; ?fragments=1 pula o init (é assim que a
	// API real vai entregar, com o init servido uma vez só); ?thumb=1 devolve
	// init + primeiro fragmento = um MP4 de um frame.
	http.HandleFunc("/seg/", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(strings.TrimPrefix(r.URL.Path, "/seg/"))
		var meta *segment
		for i := range segs {
			if segs[i].Name == name {
				meta = &segs[i]
			}
		}
		if meta == nil {
			http.NotFound(w, r)
			return
		}
		b, err := os.ReadFile(filepath.Join(*dir, name))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		switch {
		case r.URL.Query().Get("init") == "1":
			b = b[:meta.InitSize]
		case r.URL.Query().Get("fragments") == "1":
			b = b[meta.InitSize:]
		case r.URL.Query().Get("thumb") == "1":
			b = b[:meta.InitSize+meta.FirstFrag]
		}
		_, _ = w.Write(b)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	})

	log.Printf("abra http://localhost%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func scan(dir string) ([]segment, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var segs []segment
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".mp4" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := fmp4.ProbeSegment(path)
		if err != nil {
			log.Printf("aviso: %s ilegível: %v", e.Name(), err)
			continue
		}
		st, _ := os.Stat(path)
		s := segment{
			Name: e.Name(), DurationMs: info.DurationMs, Size: st.Size(),
			InitSize: info.InitSize, FirstFrag: info.FirstFragSize,
			Keyframes: info.Keyframes, Frames: info.Frames,
		}
		if vt, ok := info.Movie.VideoTrack(); ok {
			s.Codec = vt.Codec
			s.MimeCodec = mimeFor(vt.Codec)
		}
		segs = append(segs, s)
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].Name < segs[j].Name })
	return segs, nil
}

func mimeFor(codec string) string {
	switch codec {
	case "hev1":
		return `video/mp4; codecs="hev1.1.6.L153.B0"`
	case "hvc1":
		return `video/mp4; codecs="hvc1.1.6.L153.B0"`
	default:
		return `video/mp4; codecs="avc1.640029"`
	}
}

const page = `<!doctype html><meta charset=utf-8>
<title>dwnvr — spike MSE</title>
<style>
 body{font:14px/1.5 system-ui,sans-serif;background:#111;color:#eee;margin:0;padding:16px}
 video{width:100%;max-width:960px;background:#000;display:block}
 #log{white-space:pre-wrap;font:12px/1.45 ui-monospace,monospace;background:#000;
      padding:10px;border-radius:6px;max-height:40vh;overflow:auto;margin-top:12px}
 .ok{color:#5f5}.bad{color:#f66}.warn{color:#fd0}
 button{font:inherit;padding:6px 12px;margin-right:8px;margin-top:10px}
</style>
<h2>dwnvr — teste de playback HEVC via MSE</h2>
<video id=v controls></video>
<div>
 <button onclick="start()">tocar tudo</button>
 <button onclick="v.currentTime=Math.max(0,v.duration-10)">pular pro fim</button>
 <button onclick="v.currentTime=v.duration/2">pular pro meio</button>
</div>
<div id=log></div>
<script>
const v = document.getElementById('v'), logEl = document.getElementById('log');
const log = (m,c='') => { logEl.innerHTML += '<span class="'+c+'">'+m+'</span>\n';
                          logEl.scrollTop = logEl.scrollHeight; console.log(m); };

// O primeiro diagnóstico é o que decide o desenho da tela de gravações:
// qual string de codec este navegador aceita em MSE.
for (const c of ['hev1.1.6.L153.B0','hvc1.1.6.L153.B0','avc1.640029']) {
  const m = 'video/mp4; codecs="'+c+'"';
  const sup = window.MediaSource && MediaSource.isTypeSupported(m);
  log('isTypeSupported ' + c.padEnd(20) + ' → ' + sup, sup ? 'ok' : 'bad');
}
log('---');

let segs = [];
fetch('/segments.json').then(r=>r.json()).then(s=>{
  segs = s;
  log(s.length + ' segmentos, codec da caixa = ' + s[0].codec);
  log('duração total ' + (s.reduce((a,x)=>a+x.durationMs,0)/1000).toFixed(1) + 's');
  log('clique em "tocar tudo"');
});

function appendAsync(sb, buf){
  return new Promise((res,rej)=>{
    sb.addEventListener('updateend', res, {once:true});
    sb.addEventListener('error', rej, {once:true});
    sb.appendBuffer(buf);
  });
}

async function start(){
  logEl.innerHTML='';
  const mime = segs[0].mimeCodec;
  log('usando ' + mime);
  if (!MediaSource.isTypeSupported(mime)) {
    log('ESTE NAVEGADOR NÃO SUPORTA ESSE CODEC EM MSE', 'bad');
    return;
  }
  const ms = new MediaSource();
  v.src = URL.createObjectURL(ms);
  await new Promise(r => ms.addEventListener('sourceopen', r, {once:true}));

  const sb = ms.addSourceBuffer(mime);
  sb.mode = 'segments';
  let offset = 0;
  const t0 = performance.now();

  for (const s of segs) {
    const r = await fetch('/seg/' + s.name);
    const buf = await r.arrayBuffer();
    // Cada segmento é autônomo (tem o próprio ftyp+moov e começa em t=0),
    // então o timestampOffset é quem o posiciona na linha do tempo.
    sb.timestampOffset = offset;
    try {
      await appendAsync(sb, buf);
    } catch (e) {
      log('FALHA ao anexar ' + s.name + ': ' + (v.error ? v.error.message : e), 'bad');
      return;
    }
    offset += s.durationMs / 1000;
    log('anexado ' + s.name + '  offset=' + offset.toFixed(2) + 's  buffered=' +
        (sb.buffered.length ? sb.buffered.end(sb.buffered.length-1).toFixed(2) : 0) + 's', 'ok');
  }
  ms.endOfStream();
  log('--- ' + segs.length + ' segmentos anexados em ' +
      (performance.now()-t0).toFixed(0) + 'ms, duração ' + v.duration.toFixed(2) + 's', 'ok');
  v.play().then(()=>log('PLAY OK — vídeo rodando','ok'))
          .catch(e=>log('play() falhou: '+e,'warn'));
}

v.addEventListener('error', () => log('erro no <video>: ' +
  (v.error ? v.error.code + ' ' + v.error.message : '?'), 'bad'));
v.addEventListener('playing', () => log('evento: playing','ok'));
v.addEventListener('seeked', () => log('seek → ' + v.currentTime.toFixed(2) + 's','ok'));
v.addEventListener('stalled', () => log('evento: stalled','warn'));
</script>`
