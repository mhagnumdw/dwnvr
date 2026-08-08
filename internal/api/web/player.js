// Player MSE do dwnvr.
//
// Os segmentos gravados são fMP4 autônomos que começam sempre em t=0, então
// costurá-los numa linha do tempo contínua é só posicionar cada um com
// `timestampOffset`. Não há remuxagem nem decodificação fora do navegador.
//
// A referência de tempo é o relógio de parede: `base` é o instante em ms que
// corresponde a currentTime=0. Assim "pular para 14:32" é uma conta, não uma
// busca dentro de arquivo.

const AHEAD_S = 30;   // quanto manter bufferizado à frente
const BEHIND_S = 20;  // quanto manter para trás antes de descartar

class Player {
  constructor(video, log = () => {}) {
    this.video = video;
    this.log = log;
    this.cam = null;
    this.segments = [];   // [startMs, durMs, genIdx]
    this.gens = [];
    this.base = 0;
    this.next = 0;        // índice do próximo segmento a anexar
    this.appending = false;
    this.ms = null;
    this.sb = null;

    video.addEventListener('timeupdate', () => this.pump());
    video.addEventListener('waiting', () => this.onStall());
    video.addEventListener('seeking', () => this.onSeek());
  }

  setSource(cam, gens, segments) {
    this.cam = cam;
    this.gens = gens;
    this.segments = segments;
  }

  // wallClock devolve o instante real correspondente à posição atual.
  wallClock() {
    return this.base + this.video.currentTime * 1000;
  }

  segIndexAt(ms) {
    // Último segmento que começa em ms ou antes; -1 se ms é anterior a tudo.
    let lo = 0, hi = this.segments.length - 1, found = -1;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1;
      if (this.segments[mid][0] <= ms) { found = mid; lo = mid + 1; }
      else hi = mid - 1;
    }
    return found;
  }

  async playFrom(ms) {
    if (!this.segments.length) return;

    let i = this.segIndexAt(ms);
    if (i < 0) i = 0;
    // Se ms caiu num buraco, começa do próximo segmento disponível.
    const [start, dur] = this.segments[i];
    if (ms > start + dur && i + 1 < this.segments.length) i++;

    await this.reset(this.segments[i][0]);
    this.next = i;
    await this.pump();

    // Posiciona dentro do primeiro segmento quando o alvo cai no meio dele.
    const inside = (ms - this.segments[i][0]) / 1000;
    if (inside > 0 && inside < this.segments[i][1] / 1000) {
      this.video.currentTime = inside;
    }
    try { await this.video.play(); } catch (e) { this.log('play(): ' + e.message); }
  }

  async reset(baseMs) {
    this.base = baseMs;
    this.next = 0;
    if (this.ms && this.ms.readyState === 'open') {
      try { this.ms.endOfStream(); } catch {}
    }

    const mime = await this.mimeFor(this.gens[0]);
    if (!MediaSource.isTypeSupported(mime)) {
      this.log('navegador não suporta ' + mime);
      return;
    }

    this.ms = new MediaSource();
    this.video.src = URL.createObjectURL(this.ms);
    await new Promise(r => this.ms.addEventListener('sourceopen', r, { once: true }));

    this.sb = this.ms.addSourceBuffer(mime);
    this.sb.mode = 'segments';
    this.initAppended = null;
  }

  // mimeFor lê o init da geração para descobrir os codecs de verdade, em vez
  // de adivinhar pelo nome da câmera.
  async mimeFor(gen) {
    const buf = new Uint8Array(await (await fetch(
      `api/rec/init?cam=${encodeURIComponent(this.cam)}&g=${gen}`)).arrayBuffer());
    const cc = o => String.fromCharCode(buf[o], buf[o + 1], buf[o + 2], buf[o + 3]);
    const codecs = [];
    for (let i = 0; i + 8 <= buf.length; i++) {
      const t = cc(i + 4);
      if (t === 'hev1') codecs.push('hev1.1.6.L153.B0');
      else if (t === 'hvc1') codecs.push('hvc1.1.6.L153.B0');
      else if (t === 'avc1') codecs.push('avc1.640029');
      else if (t === 'fLaC') codecs.push('flac');
      else if (t === 'Opus') codecs.push('opus');
      else if (t === 'mp4a') codecs.push('mp4a.40.2');
    }
    return 'video/mp4; codecs="' + [...new Set(codecs)].join(',') + '"';
  }

  bufferedEnd() {
    const b = this.sb && this.sb.buffered;
    return b && b.length ? b.end(b.length - 1) : this.video.currentTime;
  }

  // pump mantém a janela de buffer à frente da reprodução. Anexar um dia
  // inteiro seriam gigabytes; a janela deslizante é o que torna a timeline
  // navegável num dispositivo modesto.
  async pump() {
    if (this.appending || !this.sb || this.ms.readyState !== 'open') return;
    this.appending = true;
    try {
      while (this.next < this.segments.length &&
             this.bufferedEnd() - this.video.currentTime < AHEAD_S) {
        const [start, dur, gi] = this.segments[this.next];
        const gen = this.gens[gi];

        // O init só precisa ser anexado quando a geração muda.
        if (this.initAppended !== gen) {
          await this.append(`api/rec/init?cam=${encodeURIComponent(this.cam)}&g=${gen}`, null);
          this.initAppended = gen;
        }
        await this.append(
          `api/rec/seg?cam=${encodeURIComponent(this.cam)}&t=${start}`,
          (start - this.base) / 1000);

        this.next++;
      }
      await this.evict();
    } catch (e) {
      this.log('buffer: ' + e.message);
    } finally {
      this.appending = false;
    }
  }

  async append(url, offset) {
    const buf = await (await fetch(url)).arrayBuffer();
    if (offset !== null) this.sb.timestampOffset = offset;
    await new Promise((res, rej) => {
      this.sb.addEventListener('updateend', res, { once: true });
      this.sb.addEventListener('error', () => rej(new Error('appendBuffer falhou')), { once: true });
      this.sb.appendBuffer(buf);
    });
  }

  async evict() {
    const keepFrom = this.video.currentTime - BEHIND_S;
    if (keepFrom <= 0 || !this.sb.buffered.length) return;
    const start = this.sb.buffered.start(0);
    if (keepFrom - start < BEHIND_S) return;
    await new Promise(res => {
      this.sb.addEventListener('updateend', res, { once: true });
      this.sb.remove(start, keepFrom);
    });
  }

  // onStall pula buracos: quando a gravação tem um vão, o vídeo trava no fim
  // da faixa bufferizada e só sai de lá se alguém o empurrar.
  onStall() {
    const b = this.sb && this.sb.buffered;
    if (!b) return;
    for (let i = 0; i < b.length; i++) {
      if (b.start(i) > this.video.currentTime) {
        this.log(`buraco na gravação, pulando ${(b.start(i) - this.video.currentTime).toFixed(1)}s`);
        this.video.currentTime = b.start(i);
        return;
      }
    }
  }

  onSeek() { this.pump(); }
}
