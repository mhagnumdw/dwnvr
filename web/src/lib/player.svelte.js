// Player MSE do dwnvr.
//
// Os segmentos gravados são fMP4 autônomos que começam sempre em t=0, então
// costurá-los numa linha do tempo contínua é só posicionar cada um com
// `timestampOffset`. Nada é remuxado nem decodificado fora do navegador — o Pi
// só entrega bytes.
//
// A referência de tempo é o relógio de parede: `base` é o instante real que
// corresponde a currentTime=0. Assim "pular para 14:32" vira uma conta.

import { mediaURL } from './api.js';

// Quanto manter bufferizado à frente e atrás. À frente cobre a latência do
// Wi-Fi; atrás permite recuar alguns segundos sem rebuscar tudo.
const AHEAD_S = 25;
const BEHIND_S = 15;

export class Player {
  /** @type {HTMLVideoElement | null} */
  video = null;

  base = $state(0);
  currentMs = $state(0);
  playing = $state(false);
  buffering = $state(false);
  error = $state('');
  rate = $state(1);

  #cam = null;
  #gens = [];
  #segments = [];
  #ms = null;
  #sb = null;
  #next = 0;
  #initAppended = null;
  #pumping = false;
  #generation = 0; // invalida trabalho em voo depois de um seek

  attach(video) {
    this.video = video;
    video.addEventListener('timeupdate', this.#onTime);
    // 'seeked' importa tanto quanto 'timeupdate': ao pular com o vídeo
    // pausado, nenhum timeupdate acontece e o relógio ficaria parado em
    // --:--:-- mesmo com o frame já na tela.
    video.addEventListener('seeked', this.#onTime);
    video.addEventListener('loadeddata', this.#onTime);
    video.addEventListener('waiting', this.#onWaiting);
    video.addEventListener('playing', this.#onPlaying);
    video.addEventListener('pause', this.#onPause);
    video.addEventListener('ratechange', this.#onRate);
  }

  destroy() {
    const v = this.video;
    if (!v) return;
    v.removeEventListener('timeupdate', this.#onTime);
    v.removeEventListener('seeked', this.#onTime);
    v.removeEventListener('loadeddata', this.#onTime);
    v.removeEventListener('waiting', this.#onWaiting);
    v.removeEventListener('playing', this.#onPlaying);
    v.removeEventListener('pause', this.#onPause);
    v.removeEventListener('ratechange', this.#onRate);
    // Soltar o MediaSource e revogar a URL importa: sem isso o buffer de vídeo
    // decodificado fica preso até o coletor de lixo passar, o que num celular
    // significa dezenas de MB.
    try {
      if (this.#ms?.readyState === 'open') this.#ms.endOfStream();
    } catch {}
    if (v.src.startsWith('blob:')) URL.revokeObjectURL(v.src);
    v.removeAttribute('src');
    v.load();
    this.video = null;
    this.#ms = null;
    this.#sb = null;
  }

  setSource(cam, gens, segments) {
    this.#cam = cam;
    this.#gens = gens;
    this.#segments = segments;
    this.#generation++;
    this.base = 0;
    this.currentMs = 0;
  }

  get hasSegments() {
    return this.#segments.length > 0;
  }

  get firstMs() {
    return this.#segments[0]?.[0] ?? 0;
  }

  get lastMs() {
    const s = this.#segments.at(-1);
    return s ? s[0] + s[1] : 0;
  }

  #onTime = () => {
    if (!this.video) return;
    this.currentMs = this.base + this.video.currentTime * 1000;
    this.#pump();
  };

  #onWaiting = () => {
    this.buffering = true;
    this.#skipGap();
  };

  #onPlaying = () => {
    this.buffering = false;
    this.playing = true;
  };

  #onPause = () => {
    this.playing = false;
  };

  #onRate = () => {
    this.rate = this.video?.playbackRate ?? 1;
  };

  /** Índice do último segmento que começa em ms ou antes. */
  #indexAt(ms) {
    let lo = 0;
    let hi = this.#segments.length - 1;
    let found = -1;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1;
      if (this.#segments[mid][0] <= ms) {
        found = mid;
        lo = mid + 1;
      } else {
        hi = mid - 1;
      }
    }
    return found;
  }

  async seek(ms) {
    if (!this.#segments.length || !this.video) return;

    let i = this.#indexAt(ms);
    if (i < 0) i = 0;
    const [start, dur] = this.#segments[i];
    // Se o alvo caiu num buraco, começa do próximo segmento que existe.
    if (ms > start + dur && i + 1 < this.#segments.length) i++;

    const gen = ++this.#generation;
    this.error = '';
    this.buffering = true;

    try {
      await this.#reset(this.#segments[i][0]);
      if (gen !== this.#generation) return; // outro seek chegou primeiro
      this.#next = i;
      await this.#pump();
      if (gen !== this.#generation) return;

      const inside = (ms - this.#segments[i][0]) / 1000;
      if (inside > 0 && inside < this.#segments[i][1] / 1000) {
        this.video.currentTime = inside;
      }
      // Reflete a posição imediatamente: o autoplay pode ser bloqueado pelo
      // navegador, e nesse caso nenhum evento de reprodução viria atualizar o
      // relógio nem o marcador da timeline.
      this.currentMs = this.base + this.video.currentTime * 1000;
      await this.video.play().catch(() => {});
    } catch (e) {
      this.error = e.message;
    } finally {
      this.buffering = false;
    }
  }

  async #reset(baseMs) {
    this.base = baseMs;
    this.#next = 0;
    this.#initAppended = null;

    const mime = await this.#mimeFor(this.#gens[0]);
    if (!MediaSource.isTypeSupported(mime)) {
      throw new Error(`este navegador não reproduz ${mime}`);
    }

    if (this.video.src.startsWith('blob:')) URL.revokeObjectURL(this.video.src);
    this.#ms = new MediaSource();
    this.video.src = URL.createObjectURL(this.#ms);
    await new Promise((r) => this.#ms.addEventListener('sourceopen', r, { once: true }));

    this.#sb = this.#ms.addSourceBuffer(mime);
    this.#sb.mode = 'segments';
  }

  // Os codecs saem do init de verdade, não de um palpite pelo nome da câmera:
  // a mesma instalação mistura H265 com áudio, H265 sem áudio e H264.
  async #mimeFor(gen) {
    const buf = new Uint8Array(
      await fetch(mediaURL.init(this.#cam, gen)).then((r) => r.arrayBuffer()),
    );
    const at = (o) => String.fromCharCode(buf[o], buf[o + 1], buf[o + 2], buf[o + 3]);
    const map = {
      hev1: 'hev1.1.6.L153.B0',
      hvc1: 'hvc1.1.6.L153.B0',
      avc1: 'avc1.640029',
      fLaC: 'flac',
      Opus: 'opus',
      mp4a: 'mp4a.40.2',
    };
    const codecs = new Set();
    for (let i = 0; i + 8 <= buf.length; i++) {
      const c = map[at(i + 4)];
      if (c) codecs.add(c);
    }
    return `video/mp4; codecs="${[...codecs].join(',')}"`;
  }

  #bufferedEnd() {
    const b = this.#sb?.buffered;
    return b?.length ? b.end(b.length - 1) : (this.video?.currentTime ?? 0);
  }

  // Mantém uma janela deslizante à frente da reprodução. Anexar um dia inteiro
  // seriam gigabytes; é a janela que torna a timeline navegável num Pi.
  async #pump() {
    if (this.#pumping || !this.#sb || this.#ms?.readyState !== 'open') return;
    this.#pumping = true;
    const gen = this.#generation;

    try {
      while (
        gen === this.#generation &&
        this.#next < this.#segments.length &&
        this.#bufferedEnd() - this.video.currentTime < AHEAD_S
      ) {
        const [start, , gi] = this.#segments[this.#next];
        const g = this.#gens[gi];

        // O init só precisa ser reanexado quando a geração muda.
        if (this.#initAppended !== g) {
          await this.#append(mediaURL.init(this.#cam, g), null);
          this.#initAppended = g;
        }
        await this.#append(mediaURL.segment(this.#cam, start), (start - this.base) / 1000);
        this.#next++;
      }
      if (gen === this.#generation) await this.#evict();
    } catch (e) {
      if (gen === this.#generation) this.error = e.message;
    } finally {
      this.#pumping = false;
    }
  }

  async #append(url, offset) {
    const buf = await fetch(url).then((r) => r.arrayBuffer());
    if (this.#ms?.readyState !== 'open') return;
    if (offset !== null) this.#sb.timestampOffset = offset;
    await new Promise((res, rej) => {
      this.#sb.addEventListener('updateend', res, { once: true });
      this.#sb.addEventListener('error', () => rej(new Error('falha ao anexar segmento')), {
        once: true,
      });
      this.#sb.appendBuffer(buf);
    });
  }

  async #evict() {
    const keepFrom = this.video.currentTime - BEHIND_S;
    if (keepFrom <= 0 || !this.#sb.buffered.length) return;
    const start = this.#sb.buffered.start(0);
    if (keepFrom - start < BEHIND_S) return;
    await new Promise((res) => {
      this.#sb.addEventListener('updateend', res, { once: true });
      this.#sb.remove(start, keepFrom);
    });
  }

  // Buracos na gravação travam o vídeo no fim da faixa bufferizada; sem um
  // empurrão ele fica preso ali para sempre.
  #skipGap() {
    const b = this.#sb?.buffered;
    if (!b || !this.video) return;
    for (let i = 0; i < b.length; i++) {
      if (b.start(i) > this.video.currentTime) {
        this.video.currentTime = b.start(i);
        return;
      }
    }
  }

  toggle() {
    if (!this.video) return;
    if (this.video.paused) {
      if (!this.base && this.hasSegments) this.seek(this.firstMs);
      else this.video.play().catch(() => {});
    } else {
      this.video.pause();
    }
  }

  setRate(r) {
    if (this.video) this.video.playbackRate = r;
  }
}
