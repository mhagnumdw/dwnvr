// Decodificação das miniaturas da timeline.
//
// Cada miniatura é um MP4 de UM frame - o init mais o primeiro fragmento do
// segmento, recortados do que já está gravado. O servidor não decodifica nada:
// quem transforma isso em imagem é o navegador, com aceleração de hardware.
//
// O caminho preferido é WebCodecs, que decodifica o keyframe direto. Onde ele
// não existir, um <video> escondido faz o mesmo trabalho.

import { mediaURL } from './api.ts';

const cache = new Map<string, Promise<ImageBitmap | null>>();
const MAX_CACHE = 240; // ~algumas telas de tira; acima disso a RAM do celular sofre
const MAX_PARALLEL = 3; // decodificações ao mesmo tempo; o download não espera

// Largura do quadrinho maior da tira, em px de CSS: o `.tile` do
// ThumbStrip.svelte. O de tela estreita é menor, mas o celular girado passa a
// usar este, e guardar no menor deixaria a miniatura borrada.
const LARGURA_DO_QUADRINHO = 108;

// O cache guarda o bitmap pronto, sem compressão: 4 bytes por pixel. Na
// resolução da câmera, uma miniatura 640×360 ocupa ~0,9 MB, e uma 1080p,
// ~8 MB. Reduzida à largura do quadrinho na densidade da tela, fica em
// ~0,23 MB num celular de densidade 3, qualquer que seja a câmera. Medido num
// Chrome 155 em 14/10/2026: reduzir custa de 0,1 a 0,5 ms por miniatura com
// GPU, contra 4 a 13 ms da decodificação.
function reduzida(larguraDaFonte: number): ImageBitmapOptions | undefined {
  const alvo = Math.ceil(LARGURA_DO_QUADRINHO * (window.devicePixelRatio || 1));
  // Só a largura: a altura segue a proporção da câmera. Ampliar não ganha
  // nitidez e gasta mais memória que a própria fonte.
  return larguraDaFonte > alvo ? { resizeWidth: alvo, resizeQuality: 'medium' } : undefined;
}

interface Tarefa {
  fn: () => Promise<ImageBitmap>;
  resolve: (b: ImageBitmap) => void;
  reject: (e: unknown) => void;
}

let running = 0;
const queue: Tarefa[] = [];

function schedule(fn: Tarefa['fn']) {
  return new Promise<ImageBitmap>((resolve, reject) => {
    queue.push({ fn, resolve, reject });
    drain();
  });
}

function drain() {
  while (running < MAX_PARALLEL) {
    const tarefa = queue.shift();
    if (!tarefa) return;
    running++;
    void tarefa
      .fn()
      .then(tarefa.resolve, tarefa.reject)
      .finally(() => {
        running--;
        drain();
      });
  }
}

/**
 * Devolve um ImageBitmap do keyframe de um segmento, ou null se não der.
 * O resultado é memorizado: rolar a tira para frente e para trás não deve
 * rebuscar nem redecodificar nada.
 */
export function thumbnail(cam: string, t: number): Promise<ImageBitmap | null> {
  const key = `${cam}@${t}`;
  const guardada = cache.get(key);
  if (guardada) return guardada;

  const p = baixar(mediaURL.thumb(cam, t))
    .then((buf) => schedule(() => decode(buf)))
    .catch(() => null);
  cache.set(key, p);

  if (cache.size > MAX_CACHE) {
    // Map preserva a ordem de inserção, então a chave mais antiga sai primeiro.
    const oldest = cache.keys().next().value;
    if (oldest !== undefined) {
      void cache.get(oldest)?.then((b) => b?.close());
      cache.delete(oldest);
    }
  }
  return p;
}

export function clearThumbnails() {
  for (const p of cache.values()) void p.then((b) => b?.close());
  cache.clear();
}

// O download fica fora da fila: esperar a rede não ocupa o decodificador, e as
// miniaturas que a tira pede juntas - as que entraram na vista - viajam juntas.
// A prioridade baixa deixa os segmentos do player passarem na frente quando a
// conexão é HTTP/1.1, com seis por vez.
async function baixar(url: string) {
  const res = await fetch(url, { priority: 'low' });
  // O corpo de um erro é o JSON da API: tentar decodificá-lo como vídeo só
  // gasta um <video> para chegar à mesma miniatura vazia.
  if (!res.ok) throw new Error(`miniatura: HTTP ${res.status}`);
  return new Uint8Array(await res.arrayBuffer());
}

async function decode(buf: Uint8Array<ArrayBuffer>) {
  if (typeof VideoDecoder !== 'undefined') {
    try {
      return await decodeWithWebCodecs(buf);
    } catch {
      // Cai para o <video>: alguns navegadores anunciam WebCodecs mas recusam
      // a configuração destas câmeras.
    }
  }
  return decodeWithVideoElement(buf);
}

// --- WebCodecs --------------------------------------------------------------

const CONFIG_BOX: Readonly<Record<string, { codec: string }>> = {
  hvcC: { codec: 'hev1.1.6.L153.B0' },
  avcC: { codec: 'avc1.640029' },
};

function fourcc(buf: Uint8Array, o: number) {
  return String.fromCharCode(...buf.subarray(o, o + 4));
}

async function decodeWithWebCodecs(buf: Uint8Array): Promise<ImageBitmap> {
  const dv = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);

  // A caixa de configuração fica aninhada dentro do moov; varrer por assinatura
  // evita descer a árvore inteira só para achar 100 bytes.
  let description: Uint8Array | null = null;
  let codec: string | null = null;
  for (let i = 0; i + 8 <= buf.length && !description; i++) {
    const cfg = CONFIG_BOX[fourcc(buf, i + 4)];
    if (!cfg) continue;
    const size = dv.getUint32(i);
    if (size > 8 && i + size <= buf.length) {
      description = buf.subarray(i + 8, i + size);
      codec = cfg.codec;
    }
  }
  if (!description || !codec) throw new Error('sem configuração de codec');

  // O mdat é caixa de topo, então basta percorrer o nível raiz.
  let sample: Uint8Array | null = null;
  for (let o = 0; o + 8 <= buf.length;) {
    const size = dv.getUint32(o);
    if (size < 8) break;
    if (fourcc(buf, o + 4) === 'mdat') {
      sample = buf.subarray(o + 8, Math.min(o + size, buf.length));
      break;
    }
    o += size;
  }
  if (!sample) throw new Error('sem mdat');

  const support = await VideoDecoder.isConfigSupported({ codec, description });
  if (!support.supported) throw new Error('codec não suportado');

  return new Promise((resolve, reject) => {
    let done = false;
    const dec = new VideoDecoder({
      output: (frame) => {
        if (done) {
          frame.close();
          return;
        }
        done = true;
        void createImageBitmap(frame, reduzida(frame.displayWidth))
          .then(resolve, reject)
          .finally(() => {
            frame.close();
            dec.close();
          });
      },
      error: (e) => {
        if (!done) reject(e);
      },
    });
    dec.configure({ codec, description, optimizeForLatency: true });
    dec.decode(new EncodedVideoChunk({ type: 'key', timestamp: 0, data: sample }));
    dec.flush().catch(() => {
      // o erro, se houver, já chegou pelo `error` do decoder
    });
  });
}

// --- alternativa com <video> ------------------------------------------------

// No Android o `loadeddata` pode chegar antes de o quadro estar pronto para ser
// copiado, e o createImageBitmap o recusa com "The image source is not usable".
// Num Chrome 155 por http (13/10/2026), 9 de 220 miniaturas viravam "-" na
// tira; tentando de novo, 0 de 276, e a segunda chamada, 50 ms depois, bastou
// em todas. As esperas seguintes são folga, e a última recusa vale como falha.
const ESPERAS_DO_QUADRO_MS = [50, 100, 200, 400];

async function decodeWithVideoElement(buf: Uint8Array<ArrayBuffer>) {
  const url = URL.createObjectURL(new Blob([buf], { type: 'video/mp4' }));
  const v = document.createElement('video');
  v.muted = true;
  v.playsInline = true;
  v.preload = 'auto';
  v.src = url;

  try {
    await new Promise<void>((res, rej) => {
      v.addEventListener(
        'loadeddata',
        () => {
          res();
        },
        { once: true },
      );
      v.addEventListener(
        'error',
        () => {
          rej(new Error('miniatura ilegível'));
        },
        { once: true },
      );
      setTimeout(() => {
        rej(new Error('tempo esgotado'));
      }, 8000);
    });
    return await quadroDo(v);
  } finally {
    URL.revokeObjectURL(url);
    v.removeAttribute('src');
    v.load();
  }
}

async function quadroDo(v: HTMLVideoElement) {
  const opcoes = reduzida(v.videoWidth);
  for (const ms of ESPERAS_DO_QUADRO_MS) {
    try {
      return await createImageBitmap(v, opcoes);
    } catch {
      await new Promise((r) => setTimeout(r, ms));
    }
  }
  return createImageBitmap(v, opcoes);
}
