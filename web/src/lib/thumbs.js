// Decodificação das miniaturas da timeline.
//
// Cada miniatura é um MP4 de UM frame — o init mais o primeiro fragmento do
// segmento, recortados do que já está gravado. O Pi não decodifica nada: quem
// transforma isso em imagem é o navegador, com aceleração de hardware.
//
// O caminho preferido é WebCodecs, que decodifica o keyframe direto. Onde ele
// não existir, um <video> escondido faz o mesmo trabalho.

import { mediaURL } from './api.js';

const cache = new Map();
const MAX_CACHE = 240; // ~algumas telas de tira; acima disso a RAM do celular sofre
const MAX_PARALLEL = 3;

let running = 0;
const queue = [];

function schedule(fn) {
  return new Promise((resolve, reject) => {
    queue.push({ fn, resolve, reject });
    drain();
  });
}

function drain() {
  while (running < MAX_PARALLEL && queue.length) {
    const { fn, resolve, reject } = queue.shift();
    running++;
    fn()
      .then(resolve, reject)
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
export function thumbnail(cam, t) {
  const key = `${cam}@${t}`;
  if (cache.has(key)) return cache.get(key);

  const p = schedule(() => decode(mediaURL.thumb(cam, t))).catch(() => null);
  cache.set(key, p);

  if (cache.size > MAX_CACHE) {
    // Map preserva a ordem de inserção, então a chave mais antiga sai primeiro.
    const oldest = cache.keys().next().value;
    cache.get(oldest)?.then((b) => b?.close?.());
    cache.delete(oldest);
  }
  return p;
}

export function clearThumbnails() {
  for (const p of cache.values()) p?.then?.((b) => b?.close?.());
  cache.clear();
}

async function decode(url) {
  const buf = new Uint8Array(await fetch(url).then((r) => r.arrayBuffer()));
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

const CONFIG_BOX = {
  hvcC: { codec: 'hev1.1.6.L153.B0' },
  avcC: { codec: 'avc1.640029' },
};

function fourcc(buf, o) {
  return String.fromCharCode(buf[o], buf[o + 1], buf[o + 2], buf[o + 3]);
}

async function decodeWithWebCodecs(buf) {
  const dv = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);

  // A caixa de configuração fica aninhada dentro do moov; varrer por assinatura
  // evita descer a árvore inteira só para achar 100 bytes.
  let description = null;
  let codec = null;
  for (let i = 0; i + 8 <= buf.length && !description; i++) {
    const cfg = CONFIG_BOX[fourcc(buf, i + 4)];
    if (!cfg) continue;
    const size = dv.getUint32(i);
    if (size > 8 && i + size <= buf.length) {
      description = buf.subarray(i + 8, i + size);
      codec = cfg.codec;
    }
  }
  if (!description) throw new Error('sem configuração de codec');

  // O mdat é caixa de topo, então basta percorrer o nível raiz.
  let sample = null;
  for (let o = 0; o + 8 <= buf.length; ) {
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
      output: async (frame) => {
        if (done) return frame.close();
        done = true;
        try {
          resolve(await createImageBitmap(frame));
        } catch (e) {
          reject(e);
        } finally {
          frame.close();
          dec.close();
        }
      },
      error: (e) => {
        if (!done) reject(e);
      },
    });
    dec.configure({ codec, description, optimizeForLatency: true });
    dec.decode(new EncodedVideoChunk({ type: 'key', timestamp: 0, data: sample }));
    dec.flush().catch(() => {});
  });
}

// --- alternativa com <video> ------------------------------------------------

async function decodeWithVideoElement(buf) {
  const url = URL.createObjectURL(new Blob([buf], { type: 'video/mp4' }));
  const v = document.createElement('video');
  v.muted = true;
  v.playsInline = true;
  v.preload = 'auto';
  v.src = url;

  try {
    await new Promise((res, rej) => {
      v.addEventListener('loadeddata', res, { once: true });
      v.addEventListener('error', () => rej(new Error('miniatura ilegível')), { once: true });
      setTimeout(() => rej(new Error('tempo esgotado')), 8000);
    });
    return await createImageBitmap(v);
  } finally {
    URL.revokeObjectURL(url);
    v.removeAttribute('src');
    v.load();
  }
}
