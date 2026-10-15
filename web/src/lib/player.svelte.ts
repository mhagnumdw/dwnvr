// Player MSE do dwnvr.
//
// Os segmentos gravados são fMP4 autônomos que começam sempre em t=0, então
// costurá-los numa linha do tempo contínua é só posicionar cada um com
// `timestampOffset`. Nada é remuxado nem decodificado fora do navegador - o
// servidor só entrega bytes.
//
// A referência de tempo é o relógio de parede: `base` é o instante real que
// corresponde a currentTime=0. Assim "pular para 14:32" vira uma conta.

import { mediaURL, mensagemDe, midia } from './api.ts';
import type { Segmento } from './api.ts';

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

// Como passar por cima de um trecho corrompido. Um pacote perdido na rede
// estraga os quadros até o próximo keyframe, e o player não sabe onde ele está
// - então avança de passo em passo até achar um ponto que decodifique. O passo
// é fixo e curto de propósito: dobrá-lo chegava lá em menos tentativas, mas
// passava do ponto bom (medido em 11/09/2026: pousava em 15:27:12 quando
// 15:27:10 já tocava). Passado o teto, desiste do segmento e vai para o começo
// do seguinte, que sempre abre num keyframe.
const PULO_PASSO_MS = 1000;
const PULO_MAX_MS = 10000;

export class Player {
  video: HTMLVideoElement | null = null;

  base = $state(0);
  currentMs = $state(0);
  playing = $state(false);
  buffering = $state(false);
  // Do começo de um seek até a janela à frente estar no buffer, sem o atraso
  // do `buffering`: é por ele que a tira de miniaturas sabe quando sair da
  // frente.
  carregando = $state(false);
  error = $state('');
  rate = $state(1);

  #cam = '';
  #gens: string[] = [];
  #segments: Segmento[] = [];
  #ms: MediaSource | null = null;
  #sb: SourceBuffer | null = null;
  #next = 0;
  #initAppended: string | null = null;
  #pumping: Promise<void> | null = null; // promessa do pump em voo, ou null quando não há nenhum
  #generation = 0; // invalida trabalho em voo depois de um seek
  // Cancela os downloads da geração que acabou de ser invalidada. Sem isto, o
  // pump velho segurava o novo até o segmento que ninguém mais quer terminar
  // de chegar - e ainda dividia a banda com ele.
  #cancela = new AbortController();
  #seeking = false; // seek em voo: `base` e `currentTime` estão dessincronizados
  #avisoTimer: number | undefined; // conta os ms de espera antes de acender o "carregando…"

  #mime: string | null = null; // com que codecs o SourceBuffer atual foi criado
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- cache interno, fora da tela
  #mimes = new Map<string, string>(); // geração -> mime, para não rebuscar o mesmo init
  #boundary: number | null = null; // início do segmento onde as trilhas mudam e é preciso recomeçar
  #pulo: { inicio: number; alvo: number } | null = null; // último pulo sobre dado corrompido, em ms

  attach(video: HTMLVideoElement) {
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
    video.addEventListener('error', this.#onError);
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
    v.removeEventListener('error', this.#onError);
    // Sair da tela larga o segmento que estava chegando: ninguém mais o vê.
    this.#novaGeracao();
    // Um aviso agendado que dispara depois disto acenderia o "carregando…" de
    // um player que não existe mais - e ninguém o apagaria.
    this.#aviso(false);
    // O mesmo vale para o `playing`: os ouvintes já saíram, então a parada
    // logo abaixo não dispara mais 'pause' e a flag ficaria presa em true -
    // e quem desenha um botão a partir dela mostraria ⏸ de um vídeo morto.
    this.playing = false;
    // Soltar o MediaSource e revogar a URL importa: sem isso o buffer de vídeo
    // decodificado fica preso até o coletor de lixo passar, o que num celular
    // significa dezenas de MB.
    try {
      if (this.#ms?.readyState === 'open') this.#ms.endOfStream();
    } catch {
      // o MediaSource já estava fechando: nada a encerrar
    }
    if (v.src.startsWith('blob:')) URL.revokeObjectURL(v.src);
    v.removeAttribute('src');
    v.load();
    this.video = null;
    this.#ms = null;
    this.#sb = null;
  }

  setSource(cam: string, gens: string[], segments: Segmento[]) {
    this.#cam = cam;
    this.#gens = gens;
    this.#segments = segments;
    this.#mimes.clear();
    this.#boundary = null;
    this.#pulo = null;
    this.#novaGeracao();
    // A geração nova acima impede o `finally` de um seek em voo de rodar, então
    // o portão precisa ser aberto aqui: um dia novo sem gravação nenhuma não
    // chama seek(), e o relógio ficaria mudo para sempre.
    this.#seeking = false;
    this.carregando = false;
    this.base = 0;
    this.currentMs = 0;
  }

  // Atualiza a lista de segmentos sem interromper o que está tocando.
  //
  // O dia de hoje cresce enquanto a tela está aberta. Refazer setSource() +
  // seek() a cada atualização recriaria o MediaSource e devolveria o vídeo ao
  // ponto de partida; aqui só a lista muda, e o pump segue de onde parou - o
  // que também faz a reprodução destravar sozinha ao alcançar a ponta viva.
  async updateSegments(gens: string[], segments: Segmento[]) {
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

  #novaGeracao() {
    this.#cancela.abort();
    this.#cancela = new AbortController();
    return ++this.#generation;
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
  #aviso(on: boolean) {
    clearTimeout(this.#avisoTimer);
    this.#avisoTimer = undefined;
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
    void this.#pump();
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
    if (b?.length && this.video && b.end(b.length - 1) - this.video.currentTime > 0.5) return false;

    const ms = this.#boundary;
    this.#boundary = null;
    void this.seek(ms);
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

  // Dado corrompido na gravação - pacote perdido no UDP entre a câmera e o
  // go2rtc - derruba o decodificador do navegador de vez: o <video> entra em
  // erro e nenhum append passa mais. O ffmpeg disfarça o mesmo estrago e segue;
  // o Chrome desiste. Sem isto a reprodução parava ali até alguém clicar mais à
  // frente na timeline.
  //
  // Só o erro de decodificação: os outros não se resolvem pulando - um init que
  // o navegador recusa seria recusado de novo, e o laço não teria fim.
  #onError = () => {
    if (this.video?.error?.code !== MediaError.MEDIA_ERR_DECODE) return;
    if (!this.hasSegments) return;

    // Caiu de novo logo depois do último pulo: é o mesmo trecho estragado, e
    // o teto conta desde onde ele começou. Longe dali é um trecho novo.
    const onde = this.currentMs;
    const ultimo = this.#pulo;
    const inicio = ultimo && onde < ultimo.alvo + PULO_PASSO_MS ? ultimo.inicio : onde;

    let alvo = onde + PULO_PASSO_MS;
    if (alvo - inicio > PULO_MAX_MS) {
      const seguinte = this.#segments[this.#indexAt(onde) + 1];
      if (!seguinte) return;
      alvo = seguinte[0];
    }
    // Depois do fim do que existe não há ponto bom para onde ir. Pedir isso ao
    // seek() o faria voltar ao começo do último segmento, direto para o mesmo
    // trecho estragado.
    if (alvo >= this.lastMs) return;

    this.#pulo = { inicio, alvo };
    void this.seek(alvo);
  };

  /** Índice do último segmento que começa em ms ou antes. */
  #indexAt(ms: number) {
    let lo = 0;
    let hi = this.#segments.length - 1;
    let found = -1;
    while (lo <= hi) {
      const mid = (lo + hi) >> 1;
      const s = this.#segments[mid];
      if (s && s[0] <= ms) {
        found = mid;
        lo = mid + 1;
      } else {
        hi = mid - 1;
      }
    }
    return found;
  }

  async seek(ms: number) {
    const video = this.video;
    let i = Math.max(this.#indexAt(ms), 0);
    const achado = this.#segments[i];
    if (!achado || !video) return;

    // Se o alvo caiu num buraco, começa do próximo segmento que existe.
    const [start, dur] = achado;
    if (ms > start + dur && i + 1 < this.#segments.length) i++;
    const [alvoMs, alvoDur, alvoGen] = this.#segments[i] ?? achado;

    const gen = this.#novaGeracao();
    this.#seeking = true;
    this.carregando = true;
    this.error = '';
    this.#aviso(true);
    // Um seek novo manda em qualquer fronteira que estivesse agendada.
    this.#boundary = null;

    // Onde a reprodução vai mesmo pousar: dentro do segmento, ou no começo
    // dele quando o clique caiu antes do que existe ou depois do que acabou.
    const inside = (ms - alvoMs) / 1000;
    const dentro = inside > 0 && inside < alvoDur / 1000;
    // O marcador vai para lá já neste quadro. Trocar o src zera o
    // `currentTime` e o buffer só chega depois de algumas requisições: sem
    // isto o cursor passaria esse tempo todo parado no início do segmento.
    this.currentMs = dentro ? ms : alvoMs;

    try {
      // A geração é a DO SEGMENTO ALVO, não a primeira do dia: ligar o áudio no
      // meio do dia abre uma geração nova, e criar o SourceBuffer com os codecs
      // da antiga fazia todo appendBuffer falhar.
      await this.#reset(video, alvoMs, this.#genDe(alvoGen), gen);
      if (gen !== this.#generation) return; // outro seek chegou primeiro
      this.#next = i;
      await this.#repump();
      if (gen !== this.#generation) return;

      if (dentro) video.currentTime = inside;
      // Reflete a posição imediatamente: o autoplay pode ser bloqueado pelo
      // navegador, e nesse caso nenhum evento de reprodução viria atualizar o
      // relógio nem o marcador da timeline.
      this.currentMs = this.base + video.currentTime * 1000;
      await video.play().catch(() => {
        // autoplay bloqueado: o relógio já foi posto acima, e o ▶ continua lá
      });
    } catch (e) {
      // O erro de um seek atropelado é da câmera ou do trecho de antes.
      if (gen === this.#generation) this.error = mensagemDe(e);
    } finally {
      // Um seek atropelado não devolve o portão nem apaga o "carregando…":
      // esse estado agora é do seek novo, que ainda está buscando bytes.
      if (gen === this.#generation) {
        this.#seeking = false;
        this.#aviso(false);
      }
    }

    if (gen === this.#generation) void this.#encherEntaoLiberar(gen);
  }

  // O pump do seek mede a janela à frente a partir do 0 do MediaSource novo, e
  // o vídeo só pula para dentro do segmento depois: um clique perto do fim
  // dele deixa poucos segundos no buffer. A tira só sai da frente quando a
  // janela estiver cheia a partir de onde o vídeo está. Fica fora do await do
  // seek, porque quem o espera - a pausa do `paused=1`, a folha da detecção -
  // quer o primeiro quadro, e não a janela cheia.
  async #encherEntaoLiberar(gen: number) {
    if (!this.error) await this.#repump();
    if (gen === this.#generation) this.carregando = false;
  }

  // A geração de init de um segmento, pelo índice que a timeline manda.
  #genDe(indice: number) {
    const g = this.#gens[indice];
    if (g === undefined) throw new Error('segmento sem init na timeline');
    return g;
  }

  // `seek` é o número do seek que pediu a troca.
  async #reset(video: HTMLVideoElement, baseMs: number, gen: string, seek: number) {
    this.base = baseMs;
    this.#next = 0;
    this.#initAppended = null;
    // O buffer velho para de receber já, e não só quando o src trocar: o vídeo
    // antigo continua tocando enquanto o init vem, e um pump que o timeupdate
    // dele disparasse agora leria a lista NOVA a partir do índice 0 - baixando
    // o primeiro segmento do dia para o buffer errado, ou marcando a fronteira
    // de trilhas no começo do dia.
    this.#sb = null;

    const mime = await this.#mimeFor(gen, this.#cancela.signal);
    // Outro seek começou enquanto o init vinha - numa troca de câmera rápida,
    // o da câmera nova, com um init que pode ter chegado antes. O <video> agora
    // é dele: trocar o src aqui derrubaria o que ele já montou, e o player
    // ficaria parado num MediaSource vazio.
    if (seek !== this.#generation) return;
    if (!MediaSource.isTypeSupported(mime)) {
      throw new Error(`este navegador não reproduz ${mime}`);
    }

    // Trocar o src zera o playbackRate. Antes isso só acontecia num seek, onde
    // ninguém repara; agora a travessia de uma fronteira de trilhas recomeça o
    // MediaSource sozinha, e perder o 8× no meio de uma revisão seria um susto.
    const rate = video.playbackRate;

    if (video.src.startsWith('blob:')) URL.revokeObjectURL(video.src);
    const ms = new MediaSource();
    this.#ms = ms;
    video.src = URL.createObjectURL(ms);
    await new Promise<void>((r) => {
      ms.addEventListener(
        'sourceopen',
        () => {
          r();
        },
        { once: true },
      );
    });
    video.playbackRate = rate;

    const sb = ms.addSourceBuffer(mime);
    sb.mode = 'segments';
    this.#sb = sb;
    this.#mime = mime;
  }

  // Os codecs saem do init de verdade, não de um palpite pelo nome da câmera:
  // a mesma instalação mistura H265 com áudio, H265 sem áudio e H264.
  async #mimeFor(gen: string, signal: AbortSignal) {
    const cached = this.#mimes.get(gen);
    if (cached) return cached;

    const buf = new Uint8Array(await midia(mediaURL.init(this.#cam, gen), signal));
    const at = (o: number) => String.fromCharCode(...buf.subarray(o, o + 4));
    const map: Readonly<Record<string, string>> = {
      hev1: 'hev1.1.6.L153.B0',
      hvc1: 'hvc1.1.6.L153.B0',
      avc1: 'avc1.640029',
      fLaC: 'flac',
      Opus: 'opus',
      mp4a: 'mp4a.40.2',
    };
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- Set local, só para juntar
    const codecs = new Set<string>();
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
  #pump(): Promise<void> {
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
  // A espera é curta: a geração nova cancelou o download do pump velho.
  async #repump() {
    await this.#pumping;
    return this.#pump();
  }

  async #pumpOnce() {
    const gen = this.#generation;
    const signal = this.#cancela.signal;

    try {
      while (
        gen === this.#generation &&
        this.#next < this.#segments.length &&
        this.video &&
        this.#bufferedEnd() - this.video.currentTime < this.#lookahead()
      ) {
        const seg = this.#segments[this.#next];
        if (!seg) break;
        const [start, , gi] = seg;
        const g = this.#genDe(gi);

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
          const mime = await this.#mimeFor(g, signal);
          // O mime de uma geração invalidada não diz nada sobre o buffer de
          // agora: compará-lo marcaria a fronteira no player de outro seek.
          if (gen !== this.#generation) break;
          if (mime !== this.#mime) {
            this.#boundary = start;
            break;
          }
          await this.#append(mediaURL.init(this.#cam, g), null, signal);
          if (gen !== this.#generation) break;
          this.#initAppended = g;
        }
        await this.#append(mediaURL.segment(this.#cam, start), (start - this.base) / 1000, signal);
        // Um seek durante o download já apontou `#next` para o segmento dele;
        // avançar aqui faria o pump novo pular justamente esse.
        if (gen !== this.#generation) break;
        this.#next++;
      }
      if (gen === this.#generation) await this.#evict();
    } catch (e) {
      if (gen === this.#generation) this.error = mensagemDe(e);
    }
  }

  async #append(url: string, offset: number | null, signal: AbortSignal) {
    // O SourceBuffer é o de quando o download começou. Um seek no meio dele -
    // o do usuário ou o de #onError - troca o MediaSource, e os bytes que
    // chegassem depois iriam parar no buffer novo, antes do init dele e com o
    // timestampOffset do antigo.
    const ms = this.#ms;
    const sb = this.#sb;
    const buf = await midia(url, signal);
    if (!sb || this.#sb !== sb || ms?.readyState !== 'open') return;
    if (offset !== null) sb.timestampOffset = offset;
    await new Promise<void>((res, rej) => {
      sb.addEventListener(
        'updateend',
        () => {
          res();
        },
        { once: true },
      );
      sb.addEventListener(
        'error',
        () => {
          rej(new Error('falha ao anexar segmento'));
        },
        {
          once: true,
        },
      );
      sb.appendBuffer(buf);
    });
  }

  async #evict() {
    const sb = this.#sb;
    if (!sb || !this.video) return;
    const keepFrom = this.video.currentTime - BEHIND_S;
    if (keepFrom <= 0 || !sb.buffered.length) return;
    const start = sb.buffered.start(0);
    if (keepFrom - start < BEHIND_S) return;
    await new Promise<void>((res) => {
      sb.addEventListener(
        'updateend',
        () => {
          res();
        },
        { once: true },
      );
      sb.remove(start, keepFrom);
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
      if (!this.base && this.hasSegments) void this.seek(this.firstMs);
      else
        this.video.play().catch(() => {
          // o navegador recusou tocar: o botão segue mostrando ▶
        });
    } else {
      this.video.pause();
    }
  }

  // Pausar sem alternar. O toggle() serve ao botão, que sempre sabe em que
  // estado está; quem vem da URL com `paused=1` precisa mandar parar, e mandar
  // DEPOIS que o primeiro seek pousou - porque seek() termina em play().
  pause() {
    this.video?.pause();
  }

  setRate(r: number) {
    if (this.video) this.video.playbackRate = r;
  }
}
