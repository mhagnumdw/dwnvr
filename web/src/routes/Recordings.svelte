<script lang="ts">
  import { onMount, onDestroy, untrack } from 'svelte';
  import { cameras, loadCameras } from '../lib/state.svelte.ts';
  import { paramsAtuais, escrever, cena } from '../lib/rota.svelte.ts';
  import { api, mediaURL, mensagemDe } from '../lib/api.ts';
  import type {
    Eventos,
    ObjetoMarcado,
    Segmento,
    Timeline as LinhaDoTempo,
    Trecho,
  } from '../lib/api.ts';
  import type { DesenhoPorCima } from '../lib/captura.ts';
  import { Player, PULO_RAPIDO_MS } from '../lib/player.svelte.ts';
  import { toqueDuplo, SEQUENCIA_MS } from '../lib/toqueDuplo.ts';
  import type { Lado } from '../lib/toqueDuplo.ts';
  import { fade } from 'svelte/transition';
  import { clearThumbnails } from '../lib/thumbs.ts';
  import { baixarQuadro } from '../lib/captura.ts';
  import { dayKey, parseDay, hhmmss, taxa, diaCurto } from '../lib/format.ts';
  import { FAMILIAS } from '../lib/icones.ts';
  import { telaCheia, alternarTelaCheia, sairDaTelaCheia } from '../lib/telaCheia.svelte.ts';
  import { controlesQueSomem } from '../lib/controlesQueSomem.svelte.ts';
  import { alturaLivre } from '../lib/alturaLivre.ts';
  import { avisar } from '../lib/snackbar.svelte.ts';
  import Timeline from '../components/Timeline.svelte';
  import BotaoTelaCheia from '../components/BotaoTelaCheia.svelte';
  import ThumbStrip from '../components/ThumbStrip.svelte';
  import SemCameras from '../components/SemCameras.svelte';
  import NavDia from '../components/NavDia.svelte';
  import Relogio from '../components/Relogio.svelte';
  import Icone from '../components/Icone.svelte';
  import type CaixasDoVideo from '../components/Caixas.svelte';

  // O que a tela guarda da timeline do dia: o que ela desenha e o que o player
  // toca, e de que câmera ela é.
  type DoDia = Pick<LinhaDoTempo, 'cam' | 'ranges' | 'segments' | 'gens'>;

  // O que a exportação vai pedir, ou por que não dá. O `never` deixa o
  // template ler `?.erro` e `?.inicio` sem saber qual dos dois é.
  type Exportacao =
    { erro: string; inicio?: never; to?: never } | { inicio: number; to: number; erro?: never };

  const player = new Player();

  // Piso e teto do ritmo com que o dia de hoje é relido. Consultar mais rápido
  // que o segmento da câmera é rebaixar a mesma resposta várias vezes; mais
  // devagar que um minuto é deixar a tela velha à toa.
  const POLL_MIN_MS = 10_000;
  const POLL_MAX_MS = 60_000;
  // Segundos de segmento assumidos quando a câmera não informa - é o default do
  // dwnvr.example.yaml. Servidor atual já resolve o campo antes de responder.
  const SEG_PADRAO = 30;
  // Durações oferecidas na exportação. 10 min já é um arquivo grande de se
  // baixar pelo celular, e é também o teto do servidor (maxExportSpan em
  // internal/api/recordings.go): passar daqui exige subir os dois juntos.
  const DURACOES = [1, 2, 3, 5, 10];
  // Velocidades oferecidas. Num const, e não solta no template, porque a URL
  // também precisa dela - para recusar um `rate=999` sem inventar uma segunda
  // lista que um dia discordaria desta.
  const RATES = [0.25, 0.5, 1, 2, 4, 8, 16];
  // Abaixo disso a tela não encolhe mais o vídeo e passa a rolar: numa janela
  // tão baixa, vídeo, controles e timeline não cabem juntos de jeito nenhum.
  // O celular deitado, com a navegação embaixo, deixa uns 270.
  const ALTURA_MINIMA = 240;

  // Lidos uma vez, na inicialização: daqui em diante quem manda é o estado da
  // tela, que escreve de volta na URL.
  const params = paramsAtuais();

  let video = $state<HTMLVideoElement | null>(null);
  let cam = $state('');
  // Num const antes do $state porque o que vem da URL logo abaixo - instante e
  // zoom - são posições DENTRO deste dia, e precisam do dia de partida, não do
  // dia que estiver na tela daqui a pouco.
  const diaInicial = diaDaURL() ?? dayKey();
  let day = $state(diaInicial);
  let daysList = $state<string[]>([]);
  let timeline = $state<DoDia>({ cam: '', ranges: [], segments: [], gens: [] });
  // As marcas de movimento do dia, crescentes - a faixa de calor. Elas vêm num
  // pedido à parte da timeline de propósito: a barra azul é o que a tela existe
  // para mostrar, e uma falha ao ler as marcas não pode deixá-la sem desenhar.
  let eventos = $state<number[]>([]);
  // As marcas de objeto do dia, do mesmo pedido: [{ instanteMs, familia, classe,
  // score }, …].
  let objetos = $state<ObjetoMarcado[]>([]);
  let loading = $state(false);
  let error = $state('');
  // As miniaturas são gosto de quem usa o aparelho, como a duração da
  // exportação: no localStorage, e ligadas até alguém desligar.
  let showThumbs = $state(localStorage.getItem('dwnvr.rec.miniaturas') !== '0');
  // As caixas do detector sobre o vídeo, e os rótulos delas. Ligadas por
  // padrão e guardadas na URL: são parte do que um link mostra. Rótulo sem
  // caixa não existe; um botão só passa pelos três estados.
  let showCaixas = $state(params.get('caixas') !== '0');
  let showRotulos = $state(params.get('rotulos') !== '0');
  let camadaCaixas = $state<{ desenhaEm: DesenhoPorCima } | null>(null);
  // O componente das caixas vem num chunk à parte, baixado só quando o dia tem
  // objeto: quem não usa detecção não paga os bytes dele.
  let Caixas = $state<typeof CaixasDoVideo | null>(null);
  $effect(() => {
    if (objetos.length && !Caixas) {
      void import('../components/Caixas.svelte').then((m) => {
        Caixas = m.default;
      });
    }
  });
  // Segura a escrita na URL até a câmera ter sido resolvida contra o cadastro.
  let montado = $state(false);
  // A duração escolhida sobrevive ao recarregamento, como o layout do Ao Vivo.
  // Valor gravado fora da lista é descartado: ele deixaria o <select> sem
  // opção correspondente, mostrando vazio.
  let exportMin = $state(
    DURACOES.find((m) => m === Number(localStorage.getItem('dwnvr.rec.exportMin'))) ?? 5,
  );
  // Falha de captura tem estado próprio: `error` só é apagado no load()
  // seguinte, e um aviso de captura ficaria colado na tela até trocar de dia.
  let capturaErro = $state('');
  let avisoTimer: number | undefined;

  // --- tela cheia -------------------------------------------------------------
  //
  // Vão para a tela cheia o vídeo, os controles de reprodução e a timeline; o
  // resto da tela fica para trás. Os controles ficam por cima do vídeo e somem
  // sozinhos, ou, FIXOS, embaixo dele, com o vídeo no espaço que sobra.

  let cheia = $state<HTMLDivElement | null>(null);
  const emTelaCheia = $derived(!!cheia && telaCheia.el === cheia);
  // No localStorage, como a duração da exportação: é gosto de quem usa o
  // aparelho, não parte da cena que um link reproduz.
  let controlesFixos = $state(localStorage.getItem('dwnvr.rec.controlesFixos') === '1');
  let sobreControles = false;
  // Eles só somem com o vídeo tocando: quem pausou quer olhar e mexer. Com o
  // mouse em cima deles, também não.
  const controles = controlesQueSomem(
    () => emTelaCheia && player.playing && !controlesFixos && !sobreControles,
  );
  // Fora da tela cheia os controles nunca somem, seja qual for o estado acima.
  const controlesEscondidos = $derived(emTelaCheia && !controles.visiveis);

  $effect(() => {
    localStorage.setItem('dwnvr.rec.controlesFixos', controlesFixos ? '1' : '0');
  });

  // Entrar na tela cheia, dar play, pausar e soltar o alfinete recomeçam a
  // contagem.
  $effect(() => {
    if (!emTelaCheia) return;
    /* eslint-disable @typescript-eslint/no-unused-expressions -- só a leitura, para o $effect rastrear */
    player.playing;
    controlesFixos;
    /* eslint-enable @typescript-eslint/no-unused-expressions */
    untrack(() => {
      controles.mostrar();
    });
  });

  function alternarCheia() {
    if (cheia) alternarTelaCheia(cheia);
  }

  // --- os botões só de ícone --------------------------------------------------
  //
  // No celular miniaturas, caixas e baixar são só ícone, e cada toque se explica
  // numa snackbar. No desktop o rótulo do botão já diz o que ele faz, e o aviso
  // seria repetição: quem decide é o próprio rótulo, visível ou não, para não
  // repetir aqui o breakpoint do CSS.

  let rotuloDoBotao = $state<HTMLElement | null>(null);

  function avisoDoBotao(texto: string) {
    if (rotuloDoBotao && getComputedStyle(rotuloDoBotao).display !== 'none') return;
    avisar({ texto });
  }

  function alternarMiniaturas() {
    showThumbs = !showThumbs;
    avisoDoBotao(showThumbs ? 'Miniaturas exibidas' : 'Miniaturas ocultas');
  }

  // Sem caixas -> caixas -> caixas com rótulos -> sem caixas.
  const ESTADOS_DAS_CAIXAS = ['sem caixas', 'caixas', 'caixas + rótulos'] as const;
  const estadoDasCaixas = $derived(!showCaixas ? 0 : showRotulos ? 2 : 1);

  function trocarCaixas() {
    const proximo = (estadoDasCaixas + 1) % 3;
    showCaixas = proximo > 0;
    showRotulos = proximo === 2;
    avisoDoBotao(
      [
        'Caixas de detecção ocultas',
        'Caixas de detecção exibidas, sem rótulos',
        'Caixas de detecção exibidas, com rótulos',
      ][proximo] ?? '',
    );
  }

  // O menu do baixar e o balão dos gestos: um aberto por vez, e fecham com o
  // clique fora deles ou com Esc.
  let menu = $state<'baixar' | 'gestos' | null>(null);

  function foraDoMenu(e: MouseEvent) {
    if (menu && !(e.target instanceof Element && e.target.closest('.ancora'))) menu = null;
  }

  // --- a altura ----------------------------------------------------------------
  //
  // A tela tem a altura da janela, e o vídeo fica com o que sobra dela: nada de
  // rolagem, e ocultar as miniaturas devolve o espaço ao vídeo quando é a
  // altura que o limita. Com o celular deitado o vídeo vai para o lado, e a
  // altura dele é a da tela menos a da timeline e a da legenda, que ficam
  // embaixo com a largura toda.

  let pagina = $state<HTMLDivElement | null>(null);
  let alturaDaPagina = $state(0);
  let alturaDaLinha = $state(0);
  let alturaDaLegenda = $state(0);

  function medirPagina() {
    // Na tela cheia o palco é dela; a página se mede de novo na volta.
    if (pagina && !emTelaCheia) alturaDaPagina = alturaLivre(pagina, ALTURA_MINIMA);
  }

  $effect(() => {
    if (!pagina) return;
    medirPagina();
    // O cabeçalho cresce quando a faixa de versão nova aparece.
    const ro = new ResizeObserver(medirPagina);
    const topo = document.querySelector('#app > .topo');
    if (topo) ro.observe(topo);
    return () => {
      ro.disconnect();
    };
  });

  // --- pular 10 s ---------------------------------------------------------------
  //
  // Como no YouTube: no celular, duplo toque nas laterais do vídeo e no meio a
  // tela cheia; no desktop, o duplo clique em qualquer ponto é a tela cheia, e
  // os pulos são as setas. A meia-lua do lado do pulo mostra a soma enquanto a
  // sequência está aberta.

  let pulo = $state<{ lado: Lado; soma: number } | null>(null);
  let puloTimer: number | undefined;
  // O Chrome do Android também gera `dblclick` com o duplo toque: sem saber de
  // onde ele veio, o pulo na lateral entraria junto na tela cheia.
  let ultimoPonteiro = '';

  function pularRapido(lado: Lado) {
    player.pular(lado * PULO_RAPIDO_MS);
    const s = PULO_RAPIDO_MS / 1000;
    pulo = { lado, soma: pulo?.lado === lado ? pulo.soma + s : s };
    clearTimeout(puloTimer);
    puloTimer = setTimeout(() => {
      pulo = null;
    }, SEQUENCIA_MS);
  }

  // O toque simples esconde os controles da tela cheia, também pausado; fixos,
  // eles ficam, que é para isso que se fixa.
  const toque = toqueDuplo({
    pular: pularRapido,
    meio: alternarCheia,
    simples: () => {
      if (emTelaCheia && !controlesFixos) controles.toqueSimples();
    },
  });

  // As teclas valem na tela inteira, com ou sem tela cheia. Num campo ou num
  // select, a seta e o espaço são dele; o calendário também usa as setas, e
  // marca a tecla como usada.
  function tecla(e: KeyboardEvent) {
    if (e.defaultPrevented || e.ctrlKey || e.altKey || e.metaKey) return;
    const alvo = e.target instanceof HTMLElement ? e.target : null;
    if (alvo && (alvo.isContentEditable || alvo.closest('input, select, textarea'))) return;

    if (e.key === 'Escape' && menu) {
      menu = null;
      return;
    }
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault();
      pularRapido(e.key === 'ArrowLeft' ? -1 : 1);
    } else if (e.key === ' ') {
      // Fora do player, o espaço num botão continua sendo do botão: o das
      // câmeras, o do dia. Dentro dele, o `preventDefault` também impede o
      // clique - sem isso, com o foco no ▶, o vídeo alternaria duas vezes.
      if (alvo && alvo !== document.body && !cheia?.contains(alvo)) return;
      e.preventDefault();
      if (!e.repeat) player.toggle();
    }
  }

  // --- o que veio da URL ----------------------------------------------------
  //
  // Hora local `HH:MM:SS`, e não epoch: o `day` acima já é uma data local e a
  // timeline inteira é desenhada em hora local, então um epoch seria treze
  // dígitos ilegíveis que ainda assim dependeriam do fuso para casar com o dia.

  function diaDaURL() {
    const d = params.get('day') ?? '';
    // Três perguntas: o formato, se o dia existe - a ida e volta pega o
    // `2026-13-45`, que passa em qualquer teste de formato mas volta escrevendo
    // outra coisa - e se não é no futuro, que é o mesmo teto do calendário.
    // Não existe amanhã para reproduzir.
    return /^\d{4}-\d{2}-\d{2}$/.test(d) && dayKey(parseDay(d)) === d && d <= dayKey() ? d : null;
  }

  function horaMs(hms: string | null | undefined, diaMs: number) {
    const m = /^(\d{1,2}):(\d{2}):(\d{2})$/.exec(hms ?? '');
    if (!m) return null;
    const [h, min, s] = [Number(m[1]), Number(m[2]), Number(m[3])];
    if (h > 23 || min > 59 || s > 59) return null;
    return diaMs + (h * 3600 + min * 60 + s) * 1000;
  }

  const inicioDoDia = parseDay(diaInicial).getTime();

  // Instante inicial. Vale só na PRIMEIRA carga - depois de trocar de dia à
  // mão, o instante de um link velho não descreve mais nada -, e por isso o
  // load() o consome zerando.
  let tPendente = horaMs(params.get('t'), inicioDoDia);
  let pausarNaPrimeira = params.get('paused') === '1';
  const rateDaURL = RATES.find((r) => r === Number(params.get('rate'))) ?? null;

  // Janela da timeline: dois horários do mesmo dia. O fim colado na meia-noite
  // sai como `00:00:00`, então "fim <= início" só pode ser a virada do dia. Uma
  // janela mais curta que o zoom máximo passa por aqui e a própria Timeline
  // reancora no dia inteiro - o piso é regra dela, não desta leitura.
  const zoomDaURL = (() => {
    const [de, ate] = (params.get('zoom') ?? '').split(',');
    const a = horaMs(de, inicioDoDia);
    const b = horaMs(ate, inicioDoDia);
    if (a === null || b === null) return null;
    return { de: a, ate: b <= a ? inicioDoDia + 86400_000 : b };
  })();

  let viewFrom = $state(zoomDaURL?.de ?? 0);
  let viewTo = $state(zoomDaURL?.ate ?? 0);

  // --------------------------------------------------------------------------

  const dayStart = $derived(parseDay(day).getTime());
  const dayEnd = $derived(dayStart + 86400_000);
  const isToday = $derived(day === dayKey());

  // O ritmo vem da câmera selecionada: `segmentSeconds` é configurado por
  // câmera (10s a 300s no cadastro), e um segmento só entra na timeline depois
  // de fechar. Perguntar uma vez por segmento é o passo natural - quem gravou
  // em pedaços de 10s vê gravação nova em ~20s, e quem gravou em pedaços de
  // 2 min não paga oito consultas do dia inteiro para cada uma que muda algo.
  const pollMs = $derived.by(() => {
    const seg = cameras.list.find((c) => c.id === cam)?.segmentSeconds ?? SEG_PADRAO;
    return Math.min(Math.max(seg * 1000, POLL_MIN_MS), POLL_MAX_MS);
  });

  // Decide o que a exportação de `min` minutos vai pedir, e se ela é possível.
  //
  // O filtro repete a regra do store.Range (internal/store/store.go:372):
  // segmento inteiro que se sobrepõe ao intervalo entra. Repetir a regra aqui
  // dá duas coisas que o servidor só diria tarde demais - o início real do
  // clipe, e as duas recusas antecipadas. Elas importam porque o download sai
  // por location.href: um 404 ou um 409 apareceria como texto cru numa aba,
  // sem volta para a tela.
  function planoDe(min: number, from: number): Exportacao | null {
    if (!from) return null;

    // A conta dos `min` minutos parte do início do primeiro segmento, que é o
    // `from` enviado (ver `exportar`), e não do instante: contada do instante,
    // a janela passaria de `min` pelo pedaço do segmento antes dele, e no
    // de 10 min o servidor recusaria com 400 (maxExportSpan).
    const primeiro = timeline.segments.find(([s, d]) => s + d > from);
    if (!primeiro || primeiro[0] >= from + min * 60_000)
      return { erro: 'sem gravação nesse trecho' };
    const to = primeiro[0] + min * 60_000;

    const sel = timeline.segments.filter(([s, d]) => s + d > primeiro[0] && s < to);
    // Um MP4 só pode ter um init, então o servidor recusa o intervalo que muda
    // de geração (internal/api/recordings.go:327).
    if (sel.some(([, , gi]) => gi !== primeiro[2]))
      return { erro: 'o trecho atravessa uma troca de codec' };

    return { inicio: primeiro[0], to };
  }

  // Um plano por duração oferecida: o menu mostra de uma vez quais dá para
  // baixar. Só com o menu aberto - fechado, ninguém olha, e o instante muda
  // várias vezes por segundo.
  const planos = $derived.by(() => {
    if (menu !== 'baixar') return [];
    const from = Math.round(player.currentMs || 0);
    return DURACOES.map((min) => ({ min, plano: planoDe(min, from) }));
  });
  // Por que a menor duração recusada não dá. As maiores recusam pelo mesmo
  // motivo ou por um que só elas têm (a troca de codec mais adiante).
  const recusa = $derived(planos.find((p) => p.plano?.erro));

  // No localStorage, e não na URL: a duração da exportação é parâmetro de uma
  // AÇÃO, não do que se está vendo - não tem o que reproduzir num link.
  $effect(() => {
    localStorage.setItem('dwnvr.rec.exportMin', String(exportMin));
  });
  $effect(() => {
    localStorage.setItem('dwnvr.rec.miniaturas', showThumbs ? '1' : '0');
  });

  // A URL, ao contrário, passa a descrever a cena inteira: câmera, dia,
  // instante, velocidade, pausa, zoom, caixas e rótulos. É isso que faz colar o
  // endereço em outra aba cair no mesmo trecho da mesma câmera.
  //
  // O que está no padrão fica de fora, para o link não carregar o que já vale
  // sem ele. E `paused` sai do estado real do <video>, não de um sinalizador
  // próprio: o navegador pode recusar o autoplay, e a barra de endereços não
  // pode prometer uma reprodução que não está acontecendo.
  $effect(() => {
    if (!montado) return;
    const diaInteiro = !viewTo || (viewFrom <= dayStart && viewTo >= dayEnd);
    escrever({
      cam,
      day,
      t: player.currentMs ? hhmmss(player.currentMs) : null,
      rate: player.rate === 1 ? null : player.rate,
      paused: player.playing ? null : '1',
      zoom: diaInteiro ? null : `${hhmmss(viewFrom)},${hhmmss(viewTo)}`,
      // As miniaturas saíram da URL para o localStorage: um link antigo não
      // carrega mais o `thumbs=0` adiante.
      thumbs: null,
      caixas: showCaixas ? null : '0',
      rotulos: showRotulos ? null : '0',
    });
  });

  // O que o compartilhar diz em palavras: a câmera e o instante. Velocidade,
  // zoom e o resto só o link precisa levar.
  $effect(() => {
    if (!montado || !cam) return;
    const nome = cameras.list.find((c) => c.id === cam)?.name ?? cam;
    const ms = player.currentMs;
    cena.texto = ms
      ? `${nome}, ${diaCurto(ms)} às ${hhmmss(ms)}`
      : `${nome}, ${diaCurto(parseDay(day).getTime())}`;
  });

  onMount(async () => {
    if (!cameras.list.length) await loadCameras();
    // Câmera do link só vale se ainda existir: cadastro some, link fica.
    const daURL = params.get('cam');
    cam =
      daURL !== null && cameras.list.some((c) => c.id === daURL)
        ? daURL
        : (cameras.list[0]?.id ?? '');
    montado = true;
  });

  // Anda pela lista de câmeras dando a volta nas pontas.
  function shiftCam(delta: number) {
    const n = cameras.list.length;
    if (!n) return;
    const i = cameras.list.findIndex((c) => c.id === cam);
    cam = cameras.list[(i + delta + n) % n]?.id ?? cam;
  }

  onDestroy(() => {
    player.destroy();
    clearThumbnails();
    clearTimeout(avisoTimer);
    clearTimeout(puloTimer);
    controles.parar();
  });

  // Ligar o player assim que o <video> existir no DOM.
  $effect(() => {
    if (!video || player.video === video) return;
    player.attach(video);
    // Antes de qualquer seek, e aqui e não no onMount porque só aqui o <video>
    // existe com certeza: trocar o src zera o playbackRate, e o #reset() do
    // player preserva o valor que encontrar - só precisa encontrá-lo já posto.
    if (rateDaURL) player.setRate(rateDaURL);
  });

  // Recarrega ao trocar de câmera ou de dia. Este é o único ponto de busca de
  // dados da tela, o que evita duas telas disputando o mesmo estado.
  //
  // Trocar só de câmera mantém o instante: é o mesmo momento visto de outro
  // ângulo. O untrack impede que o relógio do player vire dependência e
  // recarregue a tela a cada quadro.
  let camCarregada = '';
  $effect(() => {
    if (!cam || !day) return;
    if (camCarregada && camCarregada !== cam) {
      const agora = untrack(() => player.currentMs);
      if (agora) tPendente = agora;
    }
    camCarregada = cam;
    void load(cam, day);
  });

  // Os dias com gravação, uma vez por câmera. É a única leitura deste endpoint
  // na tela: o dia que estreia com a tela aberta entra pelo refresh(), sem
  // custar requisição nenhuma.
  $effect(() => {
    if (cam) void carregarDias(cam);
  });

  async function carregarDias(c: string) {
    try {
      const res = await api.days(c);
      if (c !== cam) return;
      daysList = res.days.map((d) => d.day);
      // Dia que não existe para esta câmera - link antigo, ou a câmera que
      // acabou de ser trocada. O instante que veio da URL descrevia aquele
      // outro dia e não sobrevive à troca.
      if (daysList.length && !daysList.includes(day)) {
        tPendente = null;
        day = daysList.includes(dayKey()) ? dayKey() : (daysList.at(-1) ?? day);
      }
    } catch {
      // Lista vazia é o modo permissivo do seletor: qualquer dia até hoje volta
      // a ser alcançável, como era com o campo de data nativo. Não vira aviso
      // na tela - se o servidor estiver mesmo fora, quem diz isso é o load() da
      // timeline, que é o dado que a pessoa veio ver.
      daysList = [];
    }
  }

  // Hoje estreando no calendário, com a tela já aberta. Local de propósito:
  // o refresh() já sabe o dia, e reler o /api/rec/days a cada segmento fechado
  // seria uma requisição a cada 30s pelo dia inteiro para descobrir algo que a
  // resposta que acabou de chegar já disse.
  //
  // Em modo permissivo a lista fica como está: uma lista de um dia só faria o
  // seletor sair do "tudo liberado" para "só hoje", que é o oposto do que ele
  // deve fazer quando não conhece o histórico.
  function registrarDia(d: string) {
    if (!daysList.length || daysList.includes(d)) return;
    daysList = [...daysList, d].sort();
  }

  // Hoje a gravação continua crescendo enquanto a tela está aberta: sem isto, o
  // dia corrente congela no que existia no instante em que a tela abriu. Dia
  // passado não muda mais, então nem vale a consulta.
  $effect(() => {
    if (!cam || !isToday) return;

    const id = setInterval(() => {
      void refresh();
    }, pollMs);
    // Aba escondida não toca vídeo nem desenha timeline; ao voltar, o que se
    // perdeu é buscado de uma vez em vez de esperar o próximo intervalo.
    const onVisible = () => {
      if (!document.hidden) void refresh();
    };
    document.addEventListener('visibilitychange', onVisible);

    return () => {
      clearInterval(id);
      document.removeEventListener('visibilitychange', onVisible);
    };
  });

  // Cada load() leva um número, e só o mais recente mexe na tela. Duas trocas
  // de câmera rápidas disparam dois pedidos, e nada garante que as respostas
  // voltem na ordem: a atrasada deixaria o player numa câmera e o seletor em
  // outra.
  let carga = 0;
  // A carga cuja timeline está na tela. Atrás de `carga`, ela ainda é a da
  // câmera ou do dia anterior.
  let naTela = 0;

  async function load(c: string, d: string) {
    const minha = ++carga;
    loading = true;
    error = '';
    try {
      const t = await api.timeline(c, d);
      if (minha !== carga) return;
      timeline = t;
      naTela = minha;
      // Dia que o calendário oferecia e voltou vazio: a lista está velha - a
      // retenção apagou esse dia com a tela aberta. Reler conserta a lista e
      // leva para um dia que ainda existe. Só acontece nesse desencontro, e é
      // por isso que o poll não precisa reler nada.
      if (!t.segments.length && daysList.includes(d)) void carregarDias(c);
      player.setSource(c, t.gens, t.segments);
      clearThumbnails();
      // Num dia passado, o mais útil é o começo; hoje, o mais recente. O
      // instante que veio da URL manda em cima disso, mas só desta vez.
      const ponta = d === dayKey() ? t.segments.at(-1) : t.segments[0];
      if (ponta) {
        const alvo = tPendente ?? ponta[0];
        tPendente = null;
        await player.seek(alvo);
        // A câmera ou o dia mudou durante o seek: o pause pararia o novo. E a
        // pausa do link continua valendo para ele, que é o primeiro a tocar.
        if (minha !== carga) return;
        // Depois do seek, nunca antes: ele termina em play(). Também só desta
        // vez - dali em diante o botão é que decide.
        if (pausarNaPrimeira) player.pause();
        pausarNaPrimeira = false;
      }
    } catch (e) {
      if (minha !== carga) return;
      error = mensagemDe(e);
      timeline = { cam: c, ranges: [], segments: [], gens: [] };
      naTela = minha;
    } finally {
      // O "carregando…" é do load() mais recente: o atrasado não o apaga.
      if (minha === carga) loading = false;
    }

    // Depois, e à parte. Câmera com a detecção desligada nem pergunta: quem só
    // grava não paga pedido nenhum pela faixa. O preço é que as marcas de um
    // período em que ela esteve ligada somem da tela junto com o desligar.
    let ev: Pick<Eventos, 'onsets' | 'objetos'> = { onsets: [], objetos: [] };
    if (detectLigado(c)) {
      try {
        ev = await api.events(c, d);
      } catch {
        // Sem marcas a faixa só não aparece; a timeline já está na tela.
      }
    }
    if (minha === carga) {
      eventos = ev.onsets;
      objetos = ev.objetos;
    }
  }

  function detectLigado(c: string) {
    return !!cameras.list.find((x) => x.id === c)?.detect;
  }

  // Costura a cauda recém-buscada no que a tela já tinha.
  //
  // A consulta parcial começa 1ms depois do início do último segmento
  // conhecido, e o servidor devolve tudo que TERMINA depois disso - ou seja,
  // esse último segmento vem de volta junto. A sobreposição de um segmento é
  // de propósito: é ela que revela se a primeira faixa da cauda continua a
  // última faixa antiga, sem precisar repetir aqui a tolerância de emenda que
  // o servidor usa. A decisão já vem tomada dentro da resposta.
  function costurar(atual: DoDia, cauda: DoDia, desdeMs: number): DoDia | null {
    const novos = cauda.segments.filter(([start]) => start > desdeMs);
    if (!novos.length) return null;

    // A tabela de gerações da cauda tem índices próprios, começando do zero:
    // usá-los direto apontaria para a geração errada da lista antiga.
    const gens = [...atual.gens];
    const segments: Segmento[] = [
      ...atual.segments,
      ...novos.flatMap(([start, dur, gi]): Segmento[] => {
        const g = cauda.gens[gi];
        if (g === undefined) return []; // segmento sem init: o servidor nunca manda
        let j = gens.indexOf(g);
        if (j < 0) j = gens.push(g) - 1;
        return [[start, dur, j]];
      }),
    ];

    const ranges = atual.ranges.map(([ini, fim]): Trecho => [ini, fim]);
    for (const [ini, fim] of cauda.ranges) {
      const ultima = ranges.at(-1);
      if (ultima && ini <= ultima[1]) ultima[1] = Math.max(ultima[1], fim);
      else ranges.push([ini, fim]);
    }

    return { ...atual, ranges, segments, gens };
  }

  let refreshing = false;

  async function refresh() {
    // A virada da meia-noite não mexe em `day` sozinha: o dia que era hoje para
    // de crescer e não precisa mais ser relido. E antes de o load() mais
    // recente pôr a timeline dele na tela, a que está lá é a da câmera ou do
    // dia anterior: a cauda seria costurada nela. O load() já traz o dia todo.
    if (refreshing || naTela !== carga || document.hidden || day !== dayKey()) return;
    refreshing = true;
    const c = cam;
    const d = day;
    const base = timeline;
    const ultimo = base.segments.at(-1);
    try {
      // Sem nada em mãos - dia que amanheceu vazio, ou primeira carga que
      // falhou - não há cauda a pedir e vale o dia inteiro.
      const t = ultimo
        ? await api.timelineRange(c, ultimo[0] + 1, dayEnd)
        : await api.timeline(c, d);

      // Câmera, dia ou a própria timeline mudaram durante a busca: quem manda
      // é o load() novo, e costurar por cima misturaria duas câmeras.
      if (c !== cam || d !== day || timeline !== base) return;

      if (!ultimo) {
        if (!t.segments.length) return;
        // Primeira gravação do dia chegando com a tela já aberta: é o mesmo
        // caso do load() inicial, inclusive em ir para o instante mais recente.
        registrarDia(d);
        timeline = t;
        player.setSource(c, t.gens, t.segments);
        const ponta = t.segments.at(-1);
        if (ponta) void player.seek(ponta[0]);
        return;
      }

      // Nada novo fechou desde a última rodada: a tela fica como está, sem
      // trocar objeto nenhum - a tira de miniaturas e a barra não refazem as
      // contas à toa.
      const emendada = costurar(base, t, ultimo[0]);
      if (!emendada) return;

      timeline = emendada;
      void player.updateSegments(emendada.gens, emendada.segments);
    } catch {
      // Atualização de fundo: uma falha passageira não pode apagar da tela a
      // timeline que já estava boa nem interromper a reprodução.
    } finally {
      refreshing = false;
    }

    await caudaDeEventos(c, d);
  }

  // A cauda das marcas, pelo mesmo desenho da cauda da timeline: pede só o
  // pedaço recente, e acrescenta no fim.
  //
  // Uma marca é um instante, não um intervalo: nada dela muda depois de
  // gravada. Mas a de OBJETO chega atrasada - ela leva o instante do onset que
  // a originou e só é gravada depois que o detector olha, com o pedaço
  // esperando a vez na fila. Pedir a partir do último onset perderia para
  // sempre o objeto de um onset que já estava na tela. Por isso a cauda volta
  // ATRASO_DO_OBJETO_MS antes do último onset, e o que já se tinha é filtrado.
  //
  // O atraso máximo sai de internal/detect/parametros.go: cada câmera guarda
  // até PedacosPorCamera (2) na fila, e uma olhada desiste em PrazoDaOlhadaMs
  // (60 s). O pior caso é 2 x câmeras x 60 s, com o detector no limite em
  // todas, e 30 min o cobrem até 15 câmeras. Com o detector são, o atraso é de
  // segundos; a folga custa uns poucos KB de onsets por rodada.
  const ATRASO_DO_OBJETO_MS = 30 * 60_000;

  async function caudaDeEventos(c: string, d: string) {
    if (!detectLigado(c)) return;
    const ultimo = eventos.at(-1) ?? dayStart - 1;
    const desde = Math.max(dayStart, ultimo - ATRASO_DO_OBJETO_MS);
    try {
      const ev = await api.eventsRange(c, desde, dayEnd);
      if (c !== cam || d !== day) return;
      const onsets = ev.onsets.filter((t) => t > ultimo);
      if (onsets.length) eventos = [...eventos, ...onsets];
      // Uma olhada grava no máximo uma marca por família.
      const chave = (o: ObjetoMarcado) => `${o.instanteMs}|${o.familia}`;
      const conhecidos = new Set(objetos.map(chave));
      const novos = ev.objetos.filter((o) => !conhecidos.has(chave(o)));
      if (novos.length) objetos = [...objetos, ...novos];
    } catch {
      // Igual à timeline: falha de fundo não apaga o que já está na tela.
    }
  }

  // O `from` enviado é o início REAL do primeiro segmento, não o instante do
  // cursor. O anterior termina exatamente nesse instante e o teste do servidor
  // é `>`, então ele continua de fora; e o nome do arquivo, que sai do `from`
  // pedido (internal/api/recordings.go:352), passa a casar com o conteúdo.
  function exportar(min: number) {
    const plano = planoDe(min, Math.round(player.currentMs || 0));
    menu = null;
    if (!plano?.inicio) return;
    exportMin = min;
    avisoDoBotao(`Baixando ${min} min de vídeo a partir de ${hhmmss(plano.inicio)}`);
    location.href = mediaURL.export(cam, plano.inicio, plano.to);
  }

  async function capturar() {
    menu = null;
    avisoDoBotao(`Baixando a imagem das ${hhmmss(player.currentMs)}`);
    // O nome segue a mesma convenção da exportação (cam_AAAA-MM-DD_HH-MM-SS),
    // para que imagem e vídeo do mesmo instante fiquem lado a lado na pasta.
    // E o instante é o MOSTRADO, não a hora atual: uma captura de ontem
    // nomeada com hoje não serviria para nada.
    const d = new Date(player.currentMs);
    const nome = `${cam}_${dayKey(d)}_${hhmmss(d.getTime()).replaceAll(':', '-')}.jpg`;
    try {
      // A imagem sai como a tela está: com as caixas acesas, se estiverem ligadas.
      const camada = showCaixas ? camadaCaixas : null;
      await baixarQuadro(video, nome, camada ? camada.desenhaEm : undefined);
    } catch (e) {
      capturaErro = mensagemDe(e);
      clearTimeout(avisoTimer);
      avisoTimer = setTimeout(() => {
        capturaErro = '';
      }, 5000);
    }
  }
</script>

<svelte:window onkeydown={tecla} onclick={foraDoMenu} onresize={medirPagina} />

<div
  class="page"
  bind:this={pagina}
  style:height={alturaDaPagina ? `${alturaDaPagina}px` : null}
  style:--alt-pagina="{alturaDaPagina}px"
  style:--alt-linha="{alturaDaLinha}px"
  style:--alt-legenda="{alturaDaLegenda}px"
>
  {#if !cameras.list.length && !cameras.loading}
    <SemCameras />
  {:else}
    <div class="bar row">
      <div class="row camnav">
        <button
          class="ghost"
          onclick={() => {
            shiftCam(-1);
          }}
          disabled={cameras.list.length < 2}
          aria-label="câmera anterior"
        >
          <Icone nome="chevronEsq" tamanho={18} />
        </button>
        <!-- O blur devolve as setas aos pulos, como no select da velocidade. -->
        <select
          bind:value={cam}
          onchange={(e) => {
            e.currentTarget.blur();
          }}
          aria-label="câmera"
        >
          {#each cameras.list as c (c.id)}
            <option value={c.id}>{c.name}</option>
          {/each}
        </select>
        <button
          class="ghost"
          onclick={() => {
            shiftCam(1);
          }}
          disabled={cameras.list.length < 2}
          aria-label="próxima câmera"
        >
          <Icone nome="chevronDir" tamanho={18} />
        </button>
      </div>

      <NavDia value={day} days={daysList} onchange={(d: string) => (day = d)} />
    </div>

    <!-- O que vai para a tela cheia. Lá dentro, o que não entra (miniaturas e
         os botões de miniaturas, caixas e baixar) sai do DOM em vez de só se
         esconder: as miniaturas não decodificam nada para ninguém ver. -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="cheia"
      class:fs={emTelaCheia}
      class:fixos={controlesFixos}
      class:escondidos={controlesEscondidos}
      bind:this={cheia}
      {@attach (node: HTMLElement) => () => {
        // A de verdade o navegador desfaz sozinho quando o elemento sai da
        // página; a simulada ficaria cobrindo a tela seguinte.
        if (telaCheia.el === node) sairDaTelaCheia();
      }}
      onpointerdowncapture={() => {
        if (emTelaCheia) controles.tocou();
      }}
      onpointermove={(e) => {
        if (emTelaCheia && e.pointerType === 'mouse') controles.mostrar();
      }}
    >
      <div
        class="stage"
        onpointerdown={(e) => (ultimoPonteiro = e.pointerType)}
        onpointerup={toque}
        ondblclick={() => {
          if (ultimoPonteiro !== 'touch') alternarCheia();
        }}
      >
        <video bind:this={video} playsinline controls={false}></video>
        {#if Caixas && showCaixas && objetos.length}
          <Caixas
            bind:this={camadaCaixas}
            {video}
            {objetos}
            currentMs={player.currentMs}
            playing={player.playing}
            rate={player.rate}
            rotulos={showRotulos}
          />
        {/if}
        <!-- Empilhados numa coluna, e não cada um no seu `position: absolute`:
           antes eram três avisos disputando o mesmo canto, e o aviso de captura
           chega justamente quando o "carregando…" tem chance de estar aceso. -->
        <div class="avisos">
          {#if player.buffering || loading}<span class="badge">carregando…</span>{/if}
          {#if !loading && !error && !timeline.segments.length}
            <span class="badge">sem gravação neste dia</span>
          {/if}
          {#if player.error}<span class="badge bad">{player.error}</span>{/if}
          {#if error}<span class="badge bad">{error}</span>{/if}
          {#if capturaErro}<span class="badge bad">{capturaErro}</span>{/if}
        </div>
        {#if pulo}
          <div class="meia-lua" class:direita={pulo.lado > 0} out:fade={{ duration: 250 }}>
            <Icone nome={pulo.lado < 0 ? 'pulaEsq' : 'pulaDir'} tamanho={26} />
            <span>{pulo.soma} segundos</span>
          </div>
        {/if}
        {#if !emTelaCheia}
          <span class="entrar"
            ><BotaoTelaCheia ativa={false} sobreVideo onclick={alternarCheia} /></span
          >
        {/if}
      </div>

      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="dock"
        onpointerenter={(e) => {
          if (e.pointerType === 'mouse') sobreControles = true;
        }}
        onpointerleave={(e) => {
          if (e.pointerType !== 'mouse') return;
          sobreControles = false;
          if (emTelaCheia) controles.mostrar();
        }}
      >
        <div class="controls row">
          <button
            class="primary"
            onclick={() => {
              player.toggle();
            }}
            aria-label={player.playing ? 'pausar' : 'tocar'}
          >
            <Icone nome={player.playing ? 'pausa' : 'play'} tamanho={18} />
          </button>
          <Relogio
            ms={player.currentMs}
            onir={(seg) => {
              void player.seek(dayStart + seg * 1000);
            }}
          />

          <select
            value={player.rate}
            onchange={(e) => {
              player.setRate(Number(e.currentTarget.value));
              // Senão as setas seguintes trocariam a velocidade, e não pulariam.
              e.currentTarget.blur();
            }}
            aria-label="velocidade"
          >
            {#each RATES as r (r)}<option value={r}>{taxa(r)}</option>{/each}
          </select>

          <span class="spacer"></span>
          {#if emTelaCheia}
            <!-- Só o ícone: o `title` explica no desktop, e no celular quem toca
             vê na hora o que mudou. -->
            <button
              class="ghost fixar"
              class:on={controlesFixos}
              onclick={() => (controlesFixos = !controlesFixos)}
              aria-label="fixar os controles"
              aria-pressed={controlesFixos}
              title={controlesFixos
                ? 'soltar os controles: somem sozinhos com o vídeo tocando'
                : 'fixar os controles embaixo do vídeo'}
            >
              <Icone nome="fixar" tamanho={22} />
            </button>
            <BotaoTelaCheia ativa onclick={alternarCheia} />
          {:else}
            <!-- No celular só o ícone, e o toque se explica na snackbar; no
                 desktop o rótulo aparece ao lado. -->
            <button
              class="ghost ib"
              class:on={showThumbs}
              onclick={alternarMiniaturas}
              aria-label={showThumbs ? 'ocultar miniaturas' : 'exibir miniaturas'}
              aria-pressed={showThumbs}
            >
              <Icone nome="miniaturas" tamanho={20} />
              <span class="rotulo" bind:this={rotuloDoBotao}>miniaturas</span>
            </button>
            <!-- Só com objeto no dia, como as cores da legenda: câmera sem detecção
           não tem caixa nenhuma para mostrar. -->
            {#if objetos.length}
              <button
                class="ghost ib"
                class:on={showCaixas}
                onclick={trocarCaixas}
                aria-label="caixas de detecção: {ESTADOS_DAS_CAIXAS[
                  estadoDasCaixas
                ]}. Toque para trocar"
              >
                <Icone nome={showRotulos && showCaixas ? 'caixasRotulo' : 'caixas'} tamanho={20} />
                <span class="rotulo">{ESTADOS_DAS_CAIXAS[estadoDasCaixas]}</span>
              </button>
            {/if}
            <span class="ancora">
              <button
                class="ghost ib"
                onclick={() => (menu = menu === 'baixar' ? null : 'baixar')}
                aria-label="baixar imagem ou vídeo"
                aria-haspopup="menu"
                aria-expanded={menu === 'baixar'}
              >
                <Icone nome="baixar" tamanho={20} />
                <span class="rotulo">baixar</span>
              </button>
              {#if menu === 'baixar'}
                <div class="pop acima" role="menu">
                  <div class="tit">Baixar</div>
                  <button
                    class="imagem"
                    role="menuitem"
                    onclick={capturar}
                    disabled={!player.currentMs}
                  >
                    <Icone nome="imagem" tamanho={18} /> Imagem deste instante
                  </button>
                  <div class="tit">Vídeo a partir deste instante</div>
                  <div class="duracoes">
                    {#each planos as { min, plano } (min)}
                      <button
                        role="menuitem"
                        class:ultima={min === exportMin}
                        onclick={() => {
                          exportar(min);
                        }}
                        disabled={!plano?.inicio}
                        title={plano?.erro}
                      >
                        {min} min
                      </button>
                    {/each}
                  </div>
                  <!-- O botão desabilitado diz que não dá; sozinho, ele não
                       diria por quê. -->
                  {#if recusa?.plano?.erro}
                    <p class="recusa small">
                      {recusa.min === DURACOES[0] ? '' : `de ${recusa.min} min em diante: `}{recusa
                        .plano.erro}
                    </p>
                  {/if}
                </div>
              {/if}
            </span>
          {/if}
        </div>

        {#if showThumbs && !emTelaCheia}
          <!-- A câmera vem da timeline, e não do seletor: ao trocar de câmera
          o seletor muda na hora e os segmentos só quando a timeline nova
          chega. Nesse meio-tempo a tira pedia cada miniatura com a câmera nova
          e o instante da antiga - um 404 por miniatura, na frente da fila das
          certas. -->
          <div class="miniaturas">
            <ThumbStrip
              cam={timeline.cam}
              segments={timeline.segments}
              {objetos}
              currentMs={player.currentMs}
              adiar={player.carregando}
              onseek={(ms: number) => {
                void player.seek(ms);
              }}
            />
          </div>
        {/if}

        <div class="linha" bind:offsetHeight={alturaDaLinha}>
          <Timeline
            ranges={timeline.ranges}
            events={eventos}
            {objetos}
            {dayStart}
            {dayEnd}
            currentMs={player.currentMs}
            bind:viewFrom
            bind:viewTo
            onseek={(ms: number) => {
              void player.seek(ms);
            }}
          />
        </div>
      </div>
    </div>

    <!-- Os gestos ficam no balão da ⓘ: são para quem ainda não os conhece, e
         à vista ocupavam mais linhas que a legenda inteira no celular. O
         balão fica fora da legenda porque, deitado, ela rola para o lado e
         o cortaria. -->
    <div class="rodape ancora" bind:offsetHeight={alturaDaLegenda}>
      <div class="legend muted small row wrap">
        <span><i class="has"></i> com gravação</span>
        <span><i class="gap"></i> sem gravação</span>
        {#if eventos.length}
          <span><i class="heat"></i> movimento</span>
        {/if}
        <!-- As três, e não só as do dia: a legenda explica a cor antes de ela
           aparecer, e não muda de tamanho a cada marca nova. -->
        {#if objetos.length}
          {#each Object.values(FAMILIAS) as f (f.nome)}
            <span><i style:background={f.cor}></i> {f.nome}</span>
          {/each}
        {/if}
        <button
          class="ghost info"
          onclick={() => (menu = menu === 'gestos' ? null : 'gestos')}
          aria-label="gestos da timeline"
          aria-expanded={menu === 'gestos'}
        >
          <Icone nome="info" tamanho={16} />
        </button>
      </div>
      {#if menu === 'gestos'}
        <div class="pop acima gestos" role="dialog" aria-label="gestos da timeline">
          toque para pular · arraste para navegar · duplo toque aproxima, dois dedos afastam · pince
          ou role para dar zoom
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  /* Uma coluna com a altura da janela (medida em `medirPagina`): o vídeo fica
     com o que os outros deixam. */
  .page {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 10px;
    max-width: 1400px;
    margin: 0 auto;
  }

  /* Câmera e dia numa linha só: quem cede é o nome da câmera. */
  .bar {
    flex-wrap: nowrap;
  }
  .bar select {
    max-width: 45vw;
    min-width: 0;
  }
  .camnav {
    gap: 4px;
    min-width: 0;
  }
  .camnav button {
    padding: 9px 6px;
  }

  .stage {
    position: relative;
    background: #000;
    border-radius: var(--radius);
    overflow: hidden;
    /* O duplo toque é nosso: sem zoom do navegador no lugar dele. */
    touch-action: manipulation;
  }

  /* O retorno do pulo de 10 s, como a do YouTube: um arco translúcido colado na
     borda do lado do pulo. Passa os toques adiante, para o vídeo. */
  .meia-lua {
    position: absolute;
    top: -10%;
    bottom: -10%;
    left: 0;
    width: 42%;
    border-radius: 0 50% 50% 0 / 0 50% 50% 0;
    background: rgba(255, 255, 255, 0.12);
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 4px;
    color: #fff;
    font-size: 13px;
    font-weight: 600;
    pointer-events: none;
    user-select: none;
  }
  .meia-lua.direita {
    left: auto;
    right: 0;
    border-radius: 50% 0 0 50% / 50% 0 0 50%;
  }
  video {
    width: 100%;
    height: 100%;
    object-fit: contain;
    display: block;
    background: #000;
  }

  /* Fora da tela cheia o palco cresce até 16:9 pela largura e encolhe para o
     resto caber: quando é a altura que manda, sobra faixa preta dos lados. */
  .cheia:not(.fs) {
    /* Do tamanho do conteúdo, e só encolhe: a sobra fica embaixo da legenda,
       e não entre ela e a timeline. */
    flex: 0 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .cheia:not(.fs) .stage {
    flex: 0 1 auto;
    width: 100%;
    aspect-ratio: 16 / 9;
    min-height: 120px;
  }
  .cheia:not(.fs) .dock {
    flex: none;
    display: grid;
    gap: 8px;
  }
  /* A tira rola por dentro: sem o piso zero, a largura dela abriria a página. */
  .miniaturas {
    min-width: 0;
  }

  /* Uma linha só. O seletor de velocidade enxuga o respiro para os três
     ícones caberem ao lado em 360 px. */
  .controls {
    flex-wrap: nowrap;
    gap: 3px;
  }
  .controls > select {
    padding: 9px 2px 9px 6px;
  }
  .controls .spacer {
    min-width: 0;
  }

  /* Os botões só de ícone: miniaturas, caixas e baixar. O rótulo aparece onde
     cabe (desktop). Marcado, como os botões marcados do Ao vivo. */
  .ib {
    display: inline-grid;
    grid-auto-flow: column;
    align-items: center;
    justify-content: center;
    gap: 7px;
    min-width: 44px;
    padding: 0 10px;
    color: var(--dim);
  }
  .ib.on {
    color: var(--fg);
    border-color: var(--accent);
    background: var(--ativo-fundo);
  }
  .rotulo {
    display: none;
    font-size: 14px;
  }
  @media (min-width: 720px) {
    .rotulo {
      display: inline;
    }
  }

  /* O menu do baixar, no desenho do menu da conta. */
  .ancora {
    position: relative;
  }
  .pop {
    position: absolute;
    z-index: 30;
    right: 0;
    top: calc(100% + 6px);
    width: max-content;
    min-width: 250px;
    max-width: calc(100vw - 24px);
    padding: 6px;
    display: grid;
    gap: 2px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  }
  .pop.acima {
    top: auto;
    bottom: calc(100% + 6px);
  }
  .pop .tit {
    padding: 6px 10px 2px;
    font-size: 12px;
    color: var(--dim);
  }
  /* Com cara de botão, como as durações logo abaixo: solta no menu, sem
     fundo nem borda, ela se lia como um título. */
  .imagem {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    margin: 2px 10px 6px;
    min-height: 38px;
    padding: 4px 10px;
    font-size: 13px;
  }
  .duracoes {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    padding: 2px 10px 6px;
  }
  .duracoes button {
    flex: 1;
    min-height: 38px;
    padding: 4px 10px;
    font-size: 13px;
    white-space: nowrap;
  }
  /* A última duração baixada: só a borda, porque o toque baixa, não escolhe. */
  .duracoes button.ultima {
    border-color: var(--accent);
  }
  .recusa {
    margin: 0;
    padding: 0 10px 6px;
    color: var(--bad);
  }

  /* Celular deitado: vídeo à esquerda, e câmera, dia, controles e miniaturas à
     direita; a timeline e a legenda embaixo com a largura toda, porque é na
     timeline que se navega. A altura do vídeo é a da tela menos as delas. */
  @media (orientation: landscape) and (max-height: 500px) {
    .page {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr);
      grid-template-rows: auto auto 1fr auto auto;
      grid-template-areas: 'stage bar' 'stage ctl' 'stage mini' 'linha linha' 'legenda legenda';
      gap: 8px 10px;
    }
    .cheia:not(.fs),
    .cheia:not(.fs) .dock {
      display: contents;
    }
    .cheia:not(.fs) .stage {
      grid-area: stage;
      width: auto;
      height: calc(var(--alt-pagina) - 20px - var(--alt-linha) - 8px - var(--alt-legenda) - 8px);
      max-width: calc(100vw - 280px);
      min-height: 0;
    }
    .bar {
      grid-area: bar;
      flex-wrap: wrap;
      row-gap: 6px;
    }
    .controls {
      grid-area: ctl;
    }
    .miniaturas {
      grid-area: mini;
      align-self: start;
    }
    .linha {
      grid-area: linha;
    }
    /* Uma linha só, que rola para o lado: quebrada em três, ela tirava do
       vídeo mais da metade da altura. */
    .rodape {
      grid-area: legenda;
    }
    .legend {
      flex-wrap: nowrap;
      overflow-x: auto;
      white-space: nowrap;
      scrollbar-width: none;
      /* Rolando por dentro, a grade a espremeria até zero. */
      min-height: min-content;
    }
    .rotulo {
      display: none;
    }
    /* Os controles ficam no alto: o menu do baixar abre para baixo. */
    .controls .pop.acima {
      top: calc(100% + 6px);
      bottom: auto;
    }
  }

  .entrar {
    position: absolute;
    right: 8px;
    bottom: 8px;
  }

  /* Na tela cheia: sem `position`, porque quem o põe é o navegador (`:fullscreen`)
     ou a regra da simulada no app.css, e ele é o apoio da faixa de controles. */
  .cheia.fs {
    display: flex;
    flex-direction: column;
    gap: 0;
    background: #000;
  }
  .cheia.fs .stage {
    flex: 1;
    min-height: 0;
    border-radius: 0;
  }
  .cheia.fs video {
    width: 100%;
    height: 100%;
    aspect-ratio: auto;
    max-height: none;
  }
  /* Fixos: a faixa embaixo do vídeo, que fica com o que sobra da altura. */
  .cheia.fs .dock {
    gap: 8px;
    padding: 8px max(8px, env(safe-area-inset-right)) max(8px, env(safe-area-inset-bottom))
      max(8px, env(safe-area-inset-left));
    background: var(--bg);
    border-top: 1px solid var(--line);
  }
  /* Soltos: por cima do vídeo, sobre um degradê que segura a leitura num vídeo
     claro. */
  .cheia.fs:not(.fixos) .dock {
    position: absolute;
    inset: auto 0 0 0;
    padding-top: 28px;
    background: linear-gradient(to bottom, transparent, rgba(0, 0, 0, 0.75) 28px);
    border-top: 0;
    transition:
      opacity 0.25s,
      transform 0.25s;
  }
  .cheia.escondidos .dock {
    opacity: 0;
    transform: translateY(12px);
    pointer-events: none;
  }
  .cheia.escondidos {
    cursor: none;
  }

  /* O alfinete, do mesmo tamanho do botão da tela cheia ao lado. */
  .fixar {
    display: grid;
    place-items: center;
    width: 44px;
    padding: 0;
  }
  .fixar.on {
    background: var(--accent);
    border-color: var(--accent);
    color: #fff;
  }

  .avisos {
    position: absolute;
    top: 8px;
    left: 8px;
    right: 8px;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
    /* A coluna cobre a largura do palco para o texto poder quebrar no celular,
       mas não pode virar uma tampa invisível sobre o vídeo. */
    pointer-events: none;
  }

  .badge {
    background: rgba(0, 0, 0, 0.7);
    border: 1px solid var(--line);
    border-radius: 6px;
    padding: 3px 8px;
    font-size: 12px;
  }
  .badge.bad {
    color: var(--bad);
    border-color: #5c2b2b;
  }

  .rodape {
    flex: none;
  }
  .legend {
    gap: 14px;
    margin: 0;
  }
  /* A ⓘ no fim da linha, sem engordá-la: o alvo do toque passa da linha pelas
     margens negativas. */
  .info {
    display: grid;
    place-items: center;
    width: 32px;
    min-height: 32px;
    margin: -6px 0 -6px -8px;
    padding: 0;
    border: 0;
    border-radius: 50%;
    color: var(--dim);
  }
  .info[aria-expanded='true'] {
    color: var(--accent);
  }
  /* O balão abre por cima da timeline, alinhado à esquerda da legenda. */
  .pop.gestos {
    left: 0;
    right: auto;
    min-width: 0;
    width: max-content;
    max-width: min(360px, 100%);
    padding: 10px 12px;
    font-size: 13px;
    line-height: 1.45;
    color: var(--dim);
  }
  .legend i {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: 2px;
    vertical-align: -1px;
  }
  .has {
    background: var(--accent);
  }
  .gap {
    background: #1b1f24;
    border: 1px solid var(--line);
  }
  /* O mesmo âmbar da faixa de calor, em CALOR_COR no Timeline.svelte. */
  .heat {
    background: #d29922;
  }
</style>
