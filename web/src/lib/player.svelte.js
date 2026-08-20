// Player MSE do dwnvr.
//
// Os segmentos gravados são fMP4 autônomos que começam sempre em t=0, então
// costurá-los numa linha do tempo contínua é só posicionar cada um com
// `timestampOffset`. Nada é remuxado nem decodificado fora do navegador - o
// servidor só entrega bytes.
//
// A referência de tempo é o relógio de parede: `base` é o instante real que
// corresponde a currentTime=0. Assim "pular para 14:32" vira uma conta.

import { mediaURL } from './api.js';

// Quanto manter bufferizado à frente e atrás. À frente cobre a latência do
// Wi-Fi; atrás permite recuar alguns segundos sem rebuscar tudo. O de frente é
// a base em 1×: acima disso quem manda é #lookahead().
const AHEAD_S = 25;
const BEHIND_S = 15;

// Quanto uma espera precisa durar para virar "carregando…" na tela. Em 16× num
// celular a reprodução alterna waiting/playing várias vezes por segundo - cada
// micro-espera por bytes ou por decodificação - e o aviso piscava sem parar,
// mais incômodo que a espera que ele anuncia. O desktop nunca chegou a piscar
// porque nunca chega a esperar. Sumir continua sendo imediato: segurar o aviso
// depois que o vídeo voltou seria mentira.
const AVISO_MS = 400;

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
  #pumping = null; // promessa do pump em voo, ou null quando não há nenhum
  #generation = 0; // invalida trabalho em voo depois de um seek
  #seeking = false; // seek em voo: `base` e `currentTime` estão dessincronizados
  #avisoTimer = null; // conta os ms de espera antes de acender o "carregando…"

  #mime = null; // com que codecs o SourceBuffer atual foi criado
  #mimes = new Map(); // geração -> mime, para não rebuscar o mesmo init
  #boundary = null; // início do segmento onde as trilhas mudam e é preciso recomeçar

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
    // Um aviso agendado que dispara depois disto acenderia o "carregando…" de
    // um player que não existe mais - e ninguém o apagaria.
    this.#aviso(false);
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
    this.#mimes.clear();
    this.#boundary = null;
    this.#generation++;
    // O incremento acima impede o `finally` de um seek em voo de rodar, então
    // o portão precisa ser aberto aqui: um dia novo sem gravação nenhuma não
    // chama seek(), e o relógio ficaria mudo para sempre.
    this.#seeking = false;
    this.base = 0;
    this.currentMs = 0;
  }

  // Atualiza a lista de segmentos sem interromper o que está tocando.
  //
  // O dia de hoje cresce enquanto a tela está aberta. Refazer setSource() +
  // seek() a cada atualização recriaria o MediaSource e devolveria o vídeo ao
  // ponto de partida; aqui só a lista muda, e o pump segue de onde parou - o
  // que também faz a reprodução destravar sozinha ao alcançar a ponta viva.
  async updateSegments(gens, segments) {
    // Trocar os arrays no meio de um pump o faria avançar o índice sobre a
    // lista nova. Esperar sai mais barato que invalidar a geração, que abortaria
    // junto um seek em voo; um pump dura o download de poucos segmentos.
    await this.#pumping;

    // Por onde o pump retomaria, em relógio de parede. A retenção pode apagar o
    // começo do dia e deslocar todos os índices, então a posição na lista velha
    // não serve de referência.
    const pending = this.#segments[this.#next];
    const last = this.#segments.at(-1);
    const resumeMs = pending ? pending[0] : last ? last[0] + 1 : 0;

    this.#gens = gens;
    this.#segments = segments;
    const i = segments.findIndex(([start]) => start >= resumeMs);
    this.#next = i < 0 ? segments.length : i;

    await this.#pump();
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

  // Único caminho para o `buffering`: acender é adiado, apagar é imediato.
  #aviso(on) {
    clearTimeout(this.#avisoTimer);
    this.#avisoTimer = null;
    if (!on) {
      this.buffering = false;
      return;
    }
    this.#avisoTimer = setTimeout(() => {
      this.buffering = true;
    }, AVISO_MS);
  }

  #onTime = () => {
    if (!this.video) return;
    // Durante um seek, `base` já é a do segmento alvo enquanto `currentTime`
    // ainda é do que estava tocando - ou 0, logo depois da troca de src.
    // Publicar isso jogaria o marcador da timeline para o início do segmento
    // enquanto a rede entrega os bytes, e só então ele pousaria onde se
    // clicou. Quem fecha o portão é o próprio seek, que já pôs `currentMs` no
    // alvo. O pump continua rodando: é ele que enche o buffer.
    if (!this.#seeking) this.currentMs = this.base + this.video.currentTime * 1000;
    this.#pump();
  };

  #onWaiting = () => {
    this.#aviso(true);
    if (this.#crossBoundary()) return;
    this.#skipGap();
  };

  // Chegou ao fim do que dava para bufferizar antes de uma troca de trilhas: a
  // reprodução continua recomeçando o MediaSource a partir dali. Custa um
  // rebuffer curto, num ponto que só existe quando alguém liga ou desliga o
  // áudio de uma câmera - em troca, a gravação inteira fica navegável.
  #crossBoundary() {
    if (this.#boundary === null) return false;
    // Só cruza quando não há mesmo mais nada à frente: um `waiting` no meio do
    // buffer é rede ruim, não fronteira.
    const b = this.#sb?.buffered;
    if (b?.length && b.end(b.length - 1) - this.video.currentTime > 0.5) return false;

    const ms = this.#boundary;
    this.#boundary = null;
    this.seek(ms);
    return true;
  }

  #onPlaying = () => {
    this.#aviso(false);
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
    this.#seeking = true;
    this.error = '';
    this.#aviso(true);
    // Um seek novo manda em qualquer fronteira que estivesse agendada.
    this.#boundary = null;

    // Onde a reprodução vai mesmo pousar: dentro do segmento, ou no começo
    // dele quando o clique caiu antes do que existe ou depois do que acabou.
    const inside = (ms - this.#segments[i][0]) / 1000;
    const dentro = inside > 0 && inside < this.#segments[i][1] / 1000;
    // O marcador vai para lá já neste quadro. Trocar o src zera o
    // `currentTime` e o buffer só chega depois de algumas requisições: sem
    // isto o cursor passaria esse tempo todo parado no início do segmento.
    this.currentMs = dentro ? ms : this.#segments[i][0];

    try {
      // A geração é a DO SEGMENTO ALVO, não a primeira do dia: ligar o áudio no
      // meio do dia abre uma geração nova, e criar o SourceBuffer com os codecs
      // da antiga fazia todo appendBuffer falhar.
      await this.#reset(this.#segments[i][0], this.#gens[this.#segments[i][2]]);
      if (gen !== this.#generation) return; // outro seek chegou primeiro
      this.#next = i;
      await this.#repump();
      if (gen !== this.#generation) return;

      if (dentro) this.video.currentTime = inside;
      // Reflete a posição imediatamente: o autoplay pode ser bloqueado pelo
      // navegador, e nesse caso nenhum evento de reprodução viria atualizar o
      // relógio nem o marcador da timeline.
      this.currentMs = this.base + this.video.currentTime * 1000;
      await this.video.play().catch(() => {});
    } catch (e) {
      this.error = e.message;
    } finally {
      // Um seek atropelado não devolve o portão nem apaga o "carregando…":
      // esse estado agora é do seek novo, que ainda está buscando bytes.
      if (gen === this.#generation) {
        this.#seeking = false;
        this.#aviso(false);
      }
    }
  }

  async #reset(baseMs, gen) {
    this.base = baseMs;
    this.#next = 0;
    this.#initAppended = null;

    const mime = await this.#mimeFor(gen);
    if (!MediaSource.isTypeSupported(mime)) {
      throw new Error(`este navegador não reproduz ${mime}`);
    }

    // Trocar o src zera o playbackRate. Antes isso só acontecia num seek, onde
    // ninguém repara; agora a travessia de uma fronteira de trilhas recomeça o
    // MediaSource sozinha, e perder o 8× no meio de uma revisão seria um susto.
    const rate = this.video.playbackRate;

    if (this.video.src.startsWith('blob:')) URL.revokeObjectURL(this.video.src);
    this.#ms = new MediaSource();
    this.video.src = URL.createObjectURL(this.#ms);
    await new Promise((r) => this.#ms.addEventListener('sourceopen', r, { once: true }));
    this.video.playbackRate = rate;

    this.#sb = this.#ms.addSourceBuffer(mime);
    this.#sb.mode = 'segments';
    this.#mime = mime;
  }

  // Os codecs saem do init de verdade, não de um palpite pelo nome da câmera:
  // a mesma instalação mistura H265 com áudio, H265 sem áudio e H264.
  async #mimeFor(gen) {
    const cached = this.#mimes.get(gen);
    if (cached) return cached;

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
    const mime = `video/mp4; codecs="${[...codecs].join(',')}"`;
    this.#mimes.set(gen, mime);
    return mime;
  }

  // Quanto pedir à frente agora. A janela é medida em segundos de gravação e
  // gasta em segundos de relógio: em 16× os 25s à frente duram 1,5s de
  // reprodução, e o vídeo passaria o tempo todo esperando bytes. Escalar pela
  // taxa devolve o mesmo fôlego. O teto existe porque a janela também é
  // memória - 16× dela seriam minutos de vídeo decodificado, que num celular é
  // o que derruba a aba. Fica num método, e não numa variável do pump, para que
  // trocar a velocidade no meio de um pump valha já na volta seguinte.
  #lookahead() {
    return AHEAD_S * Math.min(Math.max(this.video?.playbackRate ?? 1, 1), 4);
  }

  #bufferedEnd() {
    const b = this.#sb?.buffered;
    return b?.length ? b.end(b.length - 1) : (this.video?.currentTime ?? 0);
  }

  // Mantém uma janela deslizante à frente da reprodução. Anexar um dia inteiro
  // seriam gigabytes; é a janela que torna a timeline navegável num hardware
  // modesto.
  //
  // Um pump de cada vez: quem chega no meio de outro recebe a promessa do que
  // já está rodando, em vez de sair sondando uma flag para saber quando acabou.
  // Dispensar a chamada é de propósito - o pump em voo enche a mesma janela, e
  // enfileirar um por timeupdate faria a fila crescer sem fim numa rede lenta.
  #pump() {
    if (this.#pumping) return this.#pumping;
    if (!this.#sb || this.#ms?.readyState !== 'open') return Promise.resolve();

    const p = this.#pumpOnce().finally(() => {
      if (this.#pumping === p) this.#pumping = null;
    });
    this.#pumping = p;
    return p;
  }

  // Espera o pump em voo largar o osso e roda outro. É o que seek() precisa:
  // ele acabou de mudar o ponto de retomada, e o pump que começou antes disso
  // não serve - só receber a promessa dele deixaria o vídeo sem nada anexado.
  async #repump() {
    await this.#pumping;
    return this.#pump();
  }

  async #pumpOnce() {
    const gen = this.#generation;

    try {
      while (
        gen === this.#generation &&
        this.#next < this.#segments.length &&
        this.#bufferedEnd() - this.video.currentTime < this.#lookahead()
      ) {
        const [start, , gi] = this.#segments[this.#next];
        const g = this.#gens[gi];

        // O init só precisa ser reanexado quando a geração muda.
        if (this.#initAppended !== g) {
          // Mas a geração nova pode mudar as TRILHAS, não só o SPS: ligar o
          // áudio de uma câmera no meio do dia acrescenta uma trilha de áudio.
          // Um SourceBuffer não aceita isso nem com changeType - o Chrome
          // recusa o append inteiro com "Got unexpected audio track". A única
          // saída é recomeçar o MediaSource, e é o que #boundary agenda.
          //
          // Quando só o SPS mudou (resolução, por exemplo) o mime continua o
          // mesmo e reanexar o init basta: é o caso comum e segue sem emenda.
          const mime = await this.#mimeFor(g);
          if (mime !== this.#mime) {
            this.#boundary = start;
            break;
          }
          await this.#append(mediaURL.init(this.#cam, g), null);
          this.#initAppended = g;
        }
        await this.#append(mediaURL.segment(this.#cam, start), (start - this.base) / 1000);
        this.#next++;
      }
      if (gen === this.#generation) await this.#evict();
    } catch (e) {
      if (gen === this.#generation) this.error = e.message;
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
