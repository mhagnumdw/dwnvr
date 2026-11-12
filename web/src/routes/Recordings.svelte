<script lang="ts">
  import { onMount, onDestroy, untrack } from 'svelte';
  import { cameras, loadCameras } from '../lib/state.svelte.ts';
  import { currentParams, write, scene } from '../lib/route.svelte.ts';
  import { api, mediaURL, messageOf } from '../lib/api.ts';
  import type {
    Events,
    MarkedObject,
    Segment,
    Timeline as TimelineData,
    Span,
  } from '../lib/api.ts';
  import type { OverlayDrawing } from '../lib/capture.ts';
  import { Player, FAST_JUMP_MS } from '../lib/player.svelte.ts';
  import { doubleTouch, SEQUENCE_MS } from '../lib/doubleTouch.ts';
  import type { Side } from '../lib/doubleTouch.ts';
  import { fade } from 'svelte/transition';
  import { clearThumbnails } from '../lib/thumbs.ts';
  import { downloadFrame } from '../lib/capture.ts';
  import { dayKey, parseDay, hhmmss, taxa, shortDay } from '../lib/format.ts';
  import { FAMILIES } from '../lib/icons.ts';
  import { fullscreen, toggleFullscreen, leaveFullscreen } from '../lib/fullscreen.svelte.ts';
  import { vanishingControls } from '../lib/vanishingControls.svelte.ts';
  import { freeHeight } from '../lib/freeHeight.ts';
  import { SWITCH_MS, AXIS_PX, swipeLimit } from '../lib/swipe.ts';
  import { notify } from '../lib/snackbar.svelte.ts';
  import Timeline from '../components/Timeline.svelte';
  import FullscreenButton from '../components/FullscreenButton.svelte';
  import ThumbStrip from '../components/ThumbStrip.svelte';
  import NoCameras from '../components/NoCameras.svelte';
  import DayNav from '../components/DayNav.svelte';
  import Clock from '../components/Clock.svelte';
  import Icon from '../components/Icon.svelte';
  import type VideoBoxes from '../components/Boxes.svelte';

  // O que a tela guarda da timeline do dia: o que ela desenha e o que o player
  // toca, e de que câmera ela é.
  type DayData = Pick<TimelineData, 'cam' | 'ranges' | 'segments' | 'gens'>;

  // O que a exportação vai pedir, ou por que não dá. O `never` deixa o
  // template ler `?.erro` e `?.inicio` sem saber qual dos dois é.
  type ExportPlan =
    { erro: string; start?: never; to?: never } | { start: number; to: number; erro?: never };

  const player = new Player();

  // Piso e teto do ritmo com que o dia de hoje é relido. Consultar mais rápido
  // que o segmento da câmera é rebaixar a mesma resposta várias vezes; mais
  // devagar que um minuto é deixar a tela velha à toa.
  const POLL_MIN_MS = 10_000;
  const POLL_MAX_MS = 60_000;
  // Segundos de segmento assumidos quando a câmera não informa - é o default do
  // dwnvr.example.yaml. Servidor atual já resolve o campo antes de responder.
  const DEFAULT_SEG = 30;
  // Durações oferecidas na exportação. 10 min já é um arquivo grande de se
  // baixar pelo celular, e é também o teto do servidor (maxExportSpan em
  // internal/api/recordings.go): passar daqui exige subir os dois juntos.
  const DURATIONS = [1, 2, 3, 5, 10];
  // Velocidades oferecidas. Num const, e não solta no template, porque a URL
  // também precisa dela - para recusar um `rate=999` sem inventar uma segunda
  // lista que um dia discordaria desta.
  const RATES = [0.25, 0.5, 1, 2, 4, 8, 16];
  // Abaixo disso a tela não encolhe mais o vídeo e passa a rolar: numa janela
  // tão baixa, vídeo, controles e timeline não cabem juntos de jeito nenhum.
  // O celular deitado, com a navegação embaixo, deixa uns 270.
  const MIN_HEIGHT = 240;
  // Abaixo disso a página não estreita mais para acompanhar o vídeo: os
  // controles precisam de espaço numa linha só, com os rótulos (720 px), e o
  // vídeo volta a ganhar faixa preta dos lados, como numa janela baixa.
  const MIN_WIDTH = 720;
  const RATIO = 16 / 9;

  // Lidos uma vez, na inicialização: daqui em diante quem manda é o estado da
  // tela, que escreve de volta na URL.
  const params = currentParams();

  let video = $state<HTMLVideoElement | null>(null);
  let cam = $state('');
  // Num const antes do $state porque o que vem da URL logo abaixo - instante e
  // zoom - são posições DENTRO deste dia, e precisam do dia de partida, não do
  // dia que estiver na tela daqui a pouco.
  const initialDay = urlDay() ?? dayKey();
  let day = $state(initialDay);
  let daysList = $state<string[]>([]);
  let timeline = $state<DayData>({ cam: '', ranges: [], segments: [], gens: [] });
  // As marcas de movimento do dia, crescentes - a faixa de calor. Elas vêm num
  // pedido à parte da timeline de propósito: a barra azul é o que a tela existe
  // para mostrar, e uma falha ao ler as marcas não pode deixá-la sem desenhar.
  let events = $state<number[]>([]);
  // As marcas de objeto do dia, do mesmo pedido: [{ instanteMs, familia, classe,
  // score }, …].
  let objects = $state<MarkedObject[]>([]);
  let loading = $state(false);
  let error = $state('');
  // As miniaturas são gosto de quem usa o aparelho, como a duração da
  // exportação: no localStorage, e ligadas até alguém desligar.
  let showThumbs = $state(localStorage.getItem('dwnvr.rec.miniaturas') !== '0');
  // As caixas do detector sobre o vídeo, e os rótulos delas. Ligadas por
  // padrão e guardadas na URL: são parte do que um link mostra. Rótulo sem
  // caixa não existe; um botão só passa pelos três estados.
  let showBoxes = $state(params.get('caixas') !== '0');
  let showLabels = $state(params.get('rotulos') !== '0');
  let boxesLayer = $state<{ drawOn: OverlayDrawing } | null>(null);
  // O componente das caixas vem num chunk à parte, baixado só quando o dia tem
  // objeto: quem não usa detecção não paga os bytes dele.
  let Boxes = $state<typeof VideoBoxes | null>(null);
  $effect(() => {
    if (objects.length && !Boxes) {
      void import('../components/Boxes.svelte').then((m) => {
        Boxes = m.default;
      });
    }
  });
  // Segura a escrita na URL até a câmera ter sido resolvida contra o cadastro.
  let mounted = $state(false);
  // A duração escolhida sobrevive ao recarregamento, como o layout do Ao Vivo.
  // Valor gravado fora da lista é descartado: ele deixaria o <select> sem
  // opção correspondente, mostrando vazio.
  let exportMin = $state(
    DURATIONS.find((m) => m === Number(localStorage.getItem('dwnvr.rec.exportMin'))) ?? 5,
  );
  // Falha de captura tem estado próprio: `error` só é apagado no load()
  // seguinte, e um aviso de captura ficaria colado na tela até trocar de dia.
  let captureError = $state('');
  let noticeTimer: number | undefined;

  // --- tela cheia -------------------------------------------------------------
  //
  // Vão para a tela cheia o vídeo, os controles de reprodução e a timeline; o
  // resto da tela fica para trás. Os controles ficam por cima do vídeo e somem
  // sozinhos, ou, FIXOS, embaixo dele, com o vídeo no espaço que sobra.

  let full = $state<HTMLDivElement | null>(null);
  const inFullscreen = $derived(!!full && fullscreen.el === full);
  // No localStorage, como a duração da exportação: é gosto de quem usa o
  // aparelho, não parte da cena que um link reproduz.
  let pinnedControls = $state(localStorage.getItem('dwnvr.rec.controlesFixos') === '1');
  let overControls = false;
  // Eles só somem com o vídeo tocando: quem pausou quer olhar e mexer. Com o
  // mouse em cima deles, também não.
  const controls = vanishingControls(
    () => inFullscreen && player.playing && !pinnedControls && !overControls,
  );
  // Fora da tela cheia os controles nunca somem, seja qual for o estado acima.
  const hiddenControls = $derived(inFullscreen && !controls.visibleOnes);

  $effect(() => {
    localStorage.setItem('dwnvr.rec.controlesFixos', pinnedControls ? '1' : '0');
  });

  // Entrar na tela cheia, dar play, pausar, soltar o alfinete e trocar de
  // câmera recomeçam a contagem. A troca mostra junto o nome da câmera nova.
  $effect(() => {
    if (!inFullscreen) return;
    /* eslint-disable @typescript-eslint/no-unused-expressions -- só a leitura, para o $effect rastrear */
    player.playing;
    pinnedControls;
    cam;
    /* eslint-enable @typescript-eslint/no-unused-expressions */
    untrack(() => {
      controls.show();
    });
  });

  function toggleFull() {
    if (full) toggleFullscreen(full);
  }

  // --- os botões só de ícone --------------------------------------------------
  //
  // No celular miniaturas, caixas e baixar são só ícone, e cada toque se explica
  // numa snackbar. No desktop o rótulo do botão já diz o que ele faz, e o aviso
  // seria repetição: quem decide é o próprio rótulo, visível ou não, para não
  // repetir aqui o breakpoint do CSS.

  let buttonLabel = $state<HTMLElement | null>(null);

  function buttonNotice(text: string) {
    if (buttonLabel && getComputedStyle(buttonLabel).display !== 'none') return;
    notify({ texto: text });
  }

  function toggleThumbnails() {
    showThumbs = !showThumbs;
    buttonNotice(showThumbs ? 'Miniaturas exibidas' : 'Miniaturas ocultas');
  }

  // Sem caixas -> caixas -> caixas com rótulos -> sem caixas.
  const BOXES_STATES = ['sem caixas', 'caixas', 'caixas + rótulos'] as const;
  const boxesState = $derived(!showBoxes ? 0 : showLabels ? 2 : 1);

  function toggleBoxes() {
    const next = (boxesState + 1) % 3;
    showBoxes = next > 0;
    showLabels = next === 2;
    buttonNotice(
      [
        'Caixas de detecção ocultas',
        'Caixas de detecção exibidas, sem rótulos',
        'Caixas de detecção exibidas, com rótulos',
      ][next] ?? '',
    );
  }

  // O menu do baixar e o balão dos gestos: um aberto por vez, e fecham com o
  // clique fora deles ou com Esc.
  let menu = $state<'baixar' | 'gestos' | null>(null);

  function outsideMenu(e: MouseEvent) {
    if (menu && !(e.target instanceof Element && e.target.closest('.anchor'))) menu = null;
  }

  // --- a altura ----------------------------------------------------------------
  //
  // A tela tem a altura da janela, e o vídeo fica com o que sobra dela: nada de
  // rolagem, e ocultar as miniaturas devolve o espaço ao vídeo quando é a
  // altura que o limita. Com o celular deitado o vídeo vai para o lado, e a
  // altura dele é a da tela menos a da timeline e a da legenda, que ficam
  // embaixo com a largura toda.

  let page = $state<HTMLDivElement | null>(null);
  let pageHeight = $state(0);
  let rowHeight = $state(0);
  let captionHeight = $state(0);

  // E a largura acompanha o vídeo: a página inteira fica com a largura que
  // o vídeo, em 16:9, tem na altura que sobra para ele, e centralizada. Em tela
  // larga, como a ultrawide, o vídeo cresce até a altura da janela, sem teto
  // fixo e sem faixa preta dos lados; a barra, os controles e a timeline vêm
  // junto, alinhados a ele.
  let pageWidth = $state(0);

  function measurePage() {
    // Na tela cheia o palco é dela; a página se mede de novo na volta.
    if (!page || inFullscreen) return;
    pageHeight = freeHeight(page, MIN_HEIGHT);
    measureWidth();
  }

  // O que não é vídeo (padding, barra, controles, miniaturas, timeline,
  // legenda e os gaps) tem altura que não depende da largura, a não ser a
  // legenda, que quebra linha em janela estreita: a conta converge, porque
  // estreitar só faz quebrar mais. Medido do topo da página ao fim do último
  // filho, e não pela altura dela, que é fixa e deixa a sobra embaixo.
  function measureWidth() {
    if (!page || !stage || inFullscreen) return;
    const last = page.lastElementChild;
    if (!last) return;
    const style = getComputedStyle(page);
    const content =
      last.getBoundingClientRect().bottom -
      page.getBoundingClientRect().top +
      parseFloat(style.paddingBottom);
    // A altura do palco com fração, como a do resto: o `offsetHeight`
    // arredonda, e o pixel de diferença faria a largura alternar entre dois
    // valores (achado do Greptile no PR #74).
    const others = content - stage.getBoundingClientRect().height;
    const horizontal = parseFloat(style.paddingLeft) + parseFloat(style.paddingRight);
    const width = Math.floor((pageHeight - others) * RATIO + horizontal);
    pageWidth = Math.max(MIN_WIDTH, width);
  }

  $effect(() => {
    if (!page) return;
    measurePage();
    // O cabeçalho cresce quando a faixa de versão nova aparece.
    const ro = new ResizeObserver(measurePage);
    const topPos = document.querySelector('#app > .top');
    if (topPos) ro.observe(topPos);
    return () => {
      ro.disconnect();
    };
  });

  // Ocultar as miniaturas, a legenda ganhar as famílias ou quebrar linha muda
  // a altura do que não é vídeo, e com ela a largura. O palco fica de fora:
  // ele muda justamente por causa desta conta.
  $effect(() => {
    if (!page || !stage) return;
    const ro = new ResizeObserver(() => {
      measureWidth();
    });
    for (const child of page.children) if (!child.contains(stage)) ro.observe(child);
    const dock = stage.parentElement?.querySelector('.dock');
    if (dock) ro.observe(dock);
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

  let jumpHint = $state<{ side: Side; sum: number } | null>(null);
  let jumpTimer: number | undefined;
  // O Chrome do Android também gera `dblclick` com o duplo toque: sem saber de
  // onde ele veio, o pulo na lateral entraria junto na tela cheia.
  let lastPointer = '';

  function fastJump(side: Side) {
    player.jump(side * FAST_JUMP_MS);
    const s = FAST_JUMP_MS / 1000;
    jumpHint = { side, sum: jumpHint?.side === side ? jumpHint.sum + s : s };
    clearTimeout(jumpTimer);
    jumpTimer = setTimeout(() => {
      jumpHint = null;
    }, SEQUENCE_MS);
  }

  // O toque simples esconde os controles da tela cheia, também pausado; fixos,
  // eles ficam, que é para isso que se fixa.
  const touch = doubleTouch({
    jump: fastJump,
    middle: toggleFull,
    simples: () => {
      if (inFullscreen && !pinnedControls) controls.singleTouch();
    },
  });

  // --- deslizar para trocar de câmera -----------------------------------------
  //
  // O gesto da tela cheia do Ao vivo, aqui dentro e fora dela: o vídeo é
  // sempre de uma câmera só. Ele segue o dedo (ou o mouse) e, passado o limite,
  // sai pelo lado; a câmera nova aparece no lugar, sem deslizar de novo, quando
  // o load() dela termina. Até lá o vídeo fica escondido, com o "carregando…"
  // no palco preto: mostrá-lo antes seria mostrar o último quadro da câmera
  // que saiu, que é o que o <video> ainda tem. Um deslize curto demais volta
  // para o lugar.
  //
  // O eixo se decide nos primeiros EIXO_PX: na vertical o dedo rola a página.
  // Começar num botão não vale.

  let stage = $state<HTMLDivElement | null>(null);
  // Quanto o vídeo está deslocado, em px, e se o deslocamento anima.
  let drag = $state(0);
  let animating = $state(false);
  // A câmera saiu pelo lado e a nova ainda carrega.
  let switching = $state(false);
  let gesture: { id: number; x: number; y: number; axis: 'x' | 'y' | null } | null = null;
  // Dois arrastos seguidos rápidos com o mouse também fazem um duplo clique.
  let draggedAt = 0;

  // A próxima (1) ou a anterior (-1), como as setas da barra.
  function ir(d: Side) {
    if (animating || cameras.list.length < 2 || !stage) return;
    menu = null;
    // A seta da faixa tocada com outro dedo arrastando o vídeo: o arrasto
    // para aqui, senão ele mexeria no vídeo no meio da troca.
    letGo();
    // Uma já saiu e a outra ainda não apareceu: não há o que tirar da tela.
    if (switching) {
      drag = 0;
      shiftCam(d);
      return;
    }
    animating = true;
    drag = -d * stage.clientWidth;
    setTimeout(() => {
      // No mesmo tique: o vídeo some no quadro em que volta ao zero.
      animating = false;
      drag = 0;
      switching = true;
      shiftCam(d);
    }, SWITCH_MS);
  }

  // A câmera nova carregou (ou falhou, ou não tem gravação no dia): aparece.
  $effect(() => {
    if (switching && !loading && timeline.cam === cam) switching = false;
  });

  // O ponteiro cujo gesto foi largado no meio: o soltar dele não é um toque.
  // O `pointerdown` seguinte com o mesmo id o desmarca, porque o mouse repete
  // o id e o soltar fora da janela nunca chega aqui.
  let released: number | null = null;

  function letGo() {
    if (gesture) released = gesture.id;
    gesture = null;
  }

  // Larga o gesto em curso e devolve o vídeo ao lugar.
  function abandon() {
    letGo();
    if (!drag) return;
    animating = true;
    drag = 0;
    setTimeout(() => (animating = false), SWITCH_MS);
  }

  function gestureStart(e: PointerEvent) {
    lastPointer = e.pointerType;
    if (e.pointerId === released) released = null;
    // Um segundo dedo no meio do gesto é pinça, e não deslize: sem isto o
    // gesto passaria para ele e o vídeo ficaria parado onde o primeiro o deixou.
    if (gesture && gesture.id !== e.pointerId) {
      abandon();
      return;
    }
    if (e.button !== 0 || animating || cameras.list.length < 2) return;
    if (e.target instanceof Element && e.target.closest('button, a')) return;
    gesture = { id: e.pointerId, x: e.clientX, y: e.clientY, axis: null };
  }

  function gestureMove(e: PointerEvent) {
    if (gesture?.id !== e.pointerId || !stage) return;
    // O botão foi solto fora da janela antes de o eixo se decidir (só então o
    // palco captura o ponteiro): o vídeo não pode seguir o mouse solto.
    if (e.pointerType === 'mouse' && !(e.buttons & 1)) {
      abandon();
      return;
    }
    const dx = e.clientX - gesture.x;
    const dy = e.clientY - gesture.y;
    if (!gesture.axis && Math.hypot(dx, dy) > AXIS_PX) {
      gesture.axis = Math.abs(dx) > Math.abs(dy) ? 'x' : 'y';
      if (gesture.axis === 'x') stage.setPointerCapture(e.pointerId);
    }
    if (gesture.axis === 'x') drag = dx;
  }

  // Diz se foi um deslize: aí o soltar não é um toque, nem para o pulo de
  // 10 s nem para esconder os controles.
  function gestureEnd(e: PointerEvent) {
    if (gesture?.id !== e.pointerId || !stage) return false;
    const { axis, x } = gesture;
    gesture = null;
    if (axis !== 'x') return false;
    draggedAt = Date.now();
    const dx = e.clientX - x;
    if (e.type === 'pointerup' && Math.abs(dx) > swipeLimit(stage.clientWidth)) {
      ir(dx < 0 ? 1 : -1);
      return true;
    }
    animating = true;
    drag = 0;
    setTimeout(() => (animating = false), SWITCH_MS);
    return true;
  }

  // O nome e a posição da câmera, na tela cheia: lá não há a barra com o
  // seletor para dizer em qual câmera se está depois de deslizar.
  const camPosition = $derived(cameras.list.findIndex((c) => c.id === cam));

  // As teclas valem na tela inteira, com ou sem tela cheia. Num campo ou num
  // select, a seta e o espaço são dele; o calendário também usa as setas, e
  // marca a tecla como usada.
  function keyHandler(e: KeyboardEvent) {
    if (e.defaultPrevented || e.ctrlKey || e.altKey || e.metaKey) return;
    const target = e.target instanceof HTMLElement ? e.target : null;
    if (target && (target.isContentEditable || target.closest('input, select, textarea'))) return;

    if (e.key === 'Escape' && menu) {
      menu = null;
      return;
    }
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault();
      fastJump(e.key === 'ArrowLeft' ? -1 : 1);
    } else if (e.key === ' ') {
      // Fora do player, o espaço num botão continua sendo do botão: o das
      // câmeras, o do dia. Dentro dele, o `preventDefault` também impede o
      // clique - sem isso, com o foco no ▶, o vídeo alternaria duas vezes.
      if (target && target !== document.body && !full?.contains(target)) return;
      e.preventDefault();
      if (!e.repeat) player.toggle();
    }
  }

  // --- o que veio da URL ----------------------------------------------------
  //
  // Hora local `HH:MM:SS`, e não epoch: o `day` acima já é uma data local e a
  // timeline inteira é desenhada em hora local, então um epoch seria treze
  // dígitos ilegíveis que ainda assim dependeriam do fuso para casar com o dia.

  function urlDay() {
    const d = params.get('day') ?? '';
    // Três perguntas: o formato, se o dia existe - a ida e volta pega o
    // `2026-13-45`, que passa em qualquer teste de formato mas volta escrevendo
    // outra coisa - e se não é no futuro, que é o mesmo teto do calendário.
    // Não existe amanhã para reproduzir.
    return /^\d{4}-\d{2}-\d{2}$/.test(d) && dayKey(parseDay(d)) === d && d <= dayKey() ? d : null;
  }

  function hourMs(hms: string | null | undefined, dayMs: number) {
    const m = /^(\d{1,2}):(\d{2}):(\d{2})$/.exec(hms ?? '');
    if (!m) return null;
    const [h, min, s] = [Number(m[1]), Number(m[2]), Number(m[3])];
    if (h > 23 || min > 59 || s > 59) return null;
    return dayMs + (h * 3600 + min * 60 + s) * 1000;
  }

  const dayStartMs = parseDay(initialDay).getTime();

  // Instante inicial. Vale só na PRIMEIRA carga - depois de trocar de dia à
  // mão, o instante de um link velho não descreve mais nada -, e por isso o
  // load() o consome zerando.
  let tPending = hourMs(params.get('t'), dayStartMs);
  let pauseOnFirst = params.get('paused') === '1';
  const urlRate = RATES.find((r) => r === Number(params.get('rate'))) ?? null;

  // Janela da timeline: dois horários do mesmo dia. O fim colado na meia-noite
  // sai como `00:00:00`, então "fim <= início" só pode ser a virada do dia. Uma
  // janela mais curta que o zoom máximo passa por aqui e a própria Timeline
  // reancora no dia inteiro - o piso é regra dela, não desta leitura.
  const urlZoom = (() => {
    const [from, to] = (params.get('zoom') ?? '').split(',');
    const a = hourMs(from, dayStartMs);
    const b = hourMs(to, dayStartMs);
    if (a === null || b === null) return null;
    return { from: a, to: b <= a ? dayStartMs + 86400_000 : b };
  })();

  let viewFrom = $state(urlZoom?.from ?? 0);
  let viewTo = $state(urlZoom?.to ?? 0);

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
    const seg = cameras.list.find((c) => c.id === cam)?.segmentSeconds ?? DEFAULT_SEG;
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
  function planOf(min: number, from: number): ExportPlan | null {
    if (!from) return null;

    // A conta dos `min` minutos parte do início do primeiro segmento, que é o
    // `from` enviado (ver `exportar`), e não do instante: contada do instante,
    // a janela passaria de `min` pelo pedaço do segmento antes dele, e no
    // de 10 min o servidor recusaria com 400 (maxExportSpan).
    const first = timeline.segments.find(([s, d]) => s + d > from);
    if (!first || first[0] >= from + min * 60_000) return { erro: 'sem gravação nesse trecho' };
    const to = first[0] + min * 60_000;

    const sel = timeline.segments.filter(([s, d]) => s + d > first[0] && s < to);
    // Um MP4 só pode ter um init, então o servidor recusa o intervalo que muda
    // de geração (internal/api/recordings.go:327).
    if (sel.some(([, , gi]) => gi !== first[2]))
      return { erro: 'o trecho atravessa uma troca de codec' };

    return { start: first[0], to };
  }

  // Um plano por duração oferecida: o menu mostra de uma vez quais dá para
  // baixar. Só com o menu aberto - fechado, ninguém olha, e o instante muda
  // várias vezes por segundo.
  const planos = $derived.by(() => {
    if (menu !== 'baixar') return [];
    const from = Math.round(player.currentMs || 0);
    return DURATIONS.map((min) => ({ min, plano: planOf(min, from) }));
  });
  // Por que a menor duração recusada não dá. As maiores recusam pelo mesmo
  // motivo ou por um que só elas têm (a troca de codec mais adiante).
  const fieldError = $derived(planos.find((p) => p.plano?.erro));

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
    if (!mounted) return;
    const wholeDay = !viewTo || (viewFrom <= dayStart && viewTo >= dayEnd);
    write({
      cam,
      day,
      t: player.currentMs ? hhmmss(player.currentMs) : null,
      rate: player.rate === 1 ? null : player.rate,
      paused: player.playing ? null : '1',
      zoom: wholeDay ? null : `${hhmmss(viewFrom)},${hhmmss(viewTo)}`,
      // As miniaturas saíram da URL para o localStorage: um link antigo não
      // carrega mais o `thumbs=0` adiante.
      thumbs: null,
      boxes: showBoxes ? null : '0',
      labels: showLabels ? null : '0',
    });
  });

  // O que o compartilhar diz em palavras: a câmera e o instante. Velocidade,
  // zoom e o resto só o link precisa levar.
  $effect(() => {
    if (!mounted || !cam) return;
    const name = cameras.list.find((c) => c.id === cam)?.name ?? cam;
    const ms = player.currentMs;
    scene.texto = ms
      ? `${name}, ${shortDay(ms)} às ${hhmmss(ms)}`
      : `${name}, ${shortDay(parseDay(day).getTime())}`;
  });

  onMount(async () => {
    if (!cameras.list.length) await loadCameras();
    // Câmera do link só vale se ainda existir: cadastro some, link fica.
    const fromURL = params.get('cam');
    cam =
      fromURL !== null && cameras.list.some((c) => c.id === fromURL)
        ? fromURL
        : (cameras.list[0]?.id ?? '');
    mounted = true;
  });

  // Troca de dia pelo calendário mantendo o horário e o zoom: a posição do
  // player e a janela da timeline são instantes do dia que sai, então viram o
  // mesmo horário no dia que entra. Sem isto o load() cai no começo do dia e a
  // Timeline, vendo a janela fora do dia novo, reancora no dia inteiro.
  //
  // Fora daqui (dia que não existe para a câmera, em carregarDias) fica o
  // comportamento antigo: lá o horário descrevia outro dia e não vale.
  function changeDay(newItem: string) {
    if (newItem === day) return;
    const displacement = parseDay(newItem).getTime() - dayStart;
    // O alvo ainda pendente (link com `t=`, ou uma troca anterior cujo load()
    // não chegou) já é um instante do dia que sai; o player ainda está num dia
    // mais antigo que ele.
    const now = tPending ?? player.currentMs;
    if (now) tPending = now + displacement;
    // Janela ainda não ancorada (0) fica como está: a Timeline a ancora.
    if (viewTo) {
      viewFrom += displacement;
      viewTo += displacement;
    }
    day = newItem;
  }

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
    clearTimeout(noticeTimer);
    clearTimeout(jumpTimer);
    controls.stop();
  });

  // Ligar o player assim que o <video> existir no DOM.
  $effect(() => {
    if (!video || player.video === video) return;
    player.attach(video);
    // Antes de qualquer seek, e aqui e não no onMount porque só aqui o <video>
    // existe com certeza: trocar o src zera o playbackRate, e o #reset() do
    // player preserva o valor que encontrar - só precisa encontrá-lo já posto.
    if (urlRate) player.setRate(urlRate);
  });

  // Recarrega ao trocar de câmera ou de dia. Este é o único ponto de busca de
  // dados da tela, o que evita duas telas disputando o mesmo estado.
  //
  // Trocar só de câmera mantém o instante: é o mesmo momento visto de outro
  // ângulo. O untrack impede que o relógio do player vire dependência e
  // recarregue a tela a cada quadro.
  let camLoaded = '';
  $effect(() => {
    if (!cam || !day) return;
    if (camLoaded && camLoaded !== cam) {
      const now = untrack(() => player.currentMs);
      if (now) tPending = now;
    }
    camLoaded = cam;
    void load(cam, day);
  });

  // Os dias com gravação, uma vez por câmera. É a única leitura deste endpoint
  // na tela: o dia que estreia com a tela aberta entra pelo refresh(), sem
  // custar requisição nenhuma.
  $effect(() => {
    if (cam) void loadDaysData(cam);
  });

  async function loadDaysData(c: string) {
    try {
      const res = await api.days(c);
      if (c !== cam) return;
      daysList = res.days.map((d) => d.day);
      // Dia que não existe para esta câmera - link antigo, ou a câmera que
      // acabou de ser trocada. O instante que veio da URL descrevia aquele
      // outro dia e não sobrevive à troca.
      if (daysList.length && !daysList.includes(day)) {
        tPending = null;
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
  function registerDay(d: string) {
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
  let loadAvg = 0;
  // A carga cuja timeline está na tela. Atrás de `carga`, ela ainda é a da
  // câmera ou do dia anterior.
  let onScreen = 0;

  async function load(c: string, d: string) {
    const mine = ++loadAvg;
    loading = true;
    error = '';
    try {
      const t = await api.timeline(c, d);
      if (mine !== loadAvg) return;
      timeline = t;
      onScreen = mine;
      // Dia que o calendário oferecia e voltou vazio: a lista está velha - a
      // retenção apagou esse dia com a tela aberta. Reler conserta a lista e
      // leva para um dia que ainda existe. Só acontece nesse desencontro, e é
      // por isso que o poll não precisa reler nada.
      if (!t.segments.length && daysList.includes(d)) void loadDaysData(c);
      player.setSource(c, t.gens, t.segments);
      clearThumbnails();
      // Num dia passado, o mais útil é o começo; hoje, o mais recente. O
      // instante que veio da URL manda em cima disso, mas só desta vez.
      const edge = d === dayKey() ? t.segments.at(-1) : t.segments[0];
      if (edge) {
        const target = tPending ?? edge[0];
        tPending = null;
        await player.seek(target);
        // A câmera ou o dia mudou durante o seek: o pause pararia o novo. E a
        // pausa do link continua valendo para ele, que é o primeiro a tocar.
        if (mine !== loadAvg) return;
        // Depois do seek, nunca antes: ele termina em play(). Também só desta
        // vez - dali em diante o botão é que decide.
        if (pauseOnFirst) player.pause();
        pauseOnFirst = false;
      }
    } catch (e) {
      if (mine !== loadAvg) return;
      error = messageOf(e);
      timeline = { cam: c, ranges: [], segments: [], gens: [] };
      onScreen = mine;
    } finally {
      // O "carregando…" é do load() mais recente: o atrasado não o apaga.
      if (mine === loadAvg) loading = false;
    }

    // Depois, e à parte. Câmera com a detecção desligada nem pergunta: quem só
    // grava não paga pedido nenhum pela faixa. O preço é que as marcas de um
    // período em que ela esteve ligada somem da tela junto com o desligar.
    let ev: Pick<Events, 'onsets' | 'objetos'> = { onsets: [], objetos: [] };
    if (detectEnabled(c)) {
      try {
        ev = await api.events(c, d);
      } catch {
        // Sem marcas a faixa só não aparece; a timeline já está na tela.
      }
    }
    if (mine === loadAvg) {
      events = ev.onsets;
      objects = ev.objetos;
    }
  }

  function detectEnabled(c: string) {
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
  function stitch(current: DayData, tail: DayData, sinceMs: number): DayData | null {
    const newOnes = tail.segments.filter(([start]) => start > sinceMs);
    if (!newOnes.length) return null;

    // A tabela de gerações da cauda tem índices próprios, começando do zero:
    // usá-los direto apontaria para a geração errada da lista antiga.
    const gens = [...current.gens];
    const segments: Segment[] = [
      ...current.segments,
      ...newOnes.flatMap(([start, dur, gi]): Segment[] => {
        const g = tail.gens[gi];
        if (g === undefined) return []; // segmento sem init: o servidor nunca manda
        let j = gens.indexOf(g);
        if (j < 0) j = gens.push(g) - 1;
        return [[start, dur, j]];
      }),
    ];

    const ranges = current.ranges.map(([ini, end]): Span => [ini, end]);
    for (const [ini, end] of tail.ranges) {
      const lastOne = ranges.at(-1);
      if (lastOne && ini <= lastOne[1]) lastOne[1] = Math.max(lastOne[1], end);
      else ranges.push([ini, end]);
    }

    return { ...current, ranges, segments, gens };
  }

  let refreshing = false;

  async function refresh() {
    // A virada da meia-noite não mexe em `day` sozinha: o dia que era hoje para
    // de crescer e não precisa mais ser relido. E antes de o load() mais
    // recente pôr a timeline dele na tela, a que está lá é a da câmera ou do
    // dia anterior: a cauda seria costurada nela. O load() já traz o dia todo.
    if (refreshing || onScreen !== loadAvg || document.hidden || day !== dayKey()) return;
    refreshing = true;
    const c = cam;
    const d = day;
    const base = timeline;
    const last = base.segments.at(-1);
    try {
      // Sem nada em mãos - dia que amanheceu vazio, ou primeira carga que
      // falhou - não há cauda a pedir e vale o dia inteiro.
      const t = last ? await api.timelineRange(c, last[0] + 1, dayEnd) : await api.timeline(c, d);

      // Câmera, dia ou a própria timeline mudaram durante a busca: quem manda
      // é o load() novo, e costurar por cima misturaria duas câmeras.
      if (c !== cam || d !== day || timeline !== base) return;

      if (!last) {
        if (!t.segments.length) return;
        // Primeira gravação do dia chegando com a tela já aberta: é o mesmo
        // caso do load() inicial, inclusive em ir para o instante mais recente.
        registerDay(d);
        timeline = t;
        player.setSource(c, t.gens, t.segments);
        const edge = t.segments.at(-1);
        if (edge) void player.seek(edge[0]);
        return;
      }

      // Nada novo fechou desde a última rodada: a tela fica como está, sem
      // trocar objeto nenhum - a tira de miniaturas e a barra não refazem as
      // contas à toa.
      const joined = stitch(base, t, last[0]);
      if (!joined) return;

      timeline = joined;
      void player.updateSegments(joined.gens, joined.segments);
    } catch {
      // Atualização de fundo: uma falha passageira não pode apagar da tela a
      // timeline que já estava boa nem interromper a reprodução.
    } finally {
      refreshing = false;
    }

    await eventsTail(c, d);
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
  // O atraso máximo sai de internal/detect/params.go: cada câmera guarda
  // até ClipsPerCamera (2) na fila, e uma olhada desiste em InferenceTimeoutMs
  // (60 s). O pior caso é 2 x câmeras x 60 s, com o detector no limite em
  // todas, e 30 min o cobrem até 15 câmeras. Com o detector são, o atraso é de
  // segundos; a folga custa uns poucos KB de onsets por rodada.
  const OBJECT_DELAY_MS = 30 * 60_000;

  async function eventsTail(c: string, d: string) {
    if (!detectEnabled(c)) return;
    const last = events.at(-1) ?? dayStart - 1;
    const since = Math.max(dayStart, last - OBJECT_DELAY_MS);
    try {
      const ev = await api.eventsRange(c, since, dayEnd);
      if (c !== cam || d !== day) return;
      const onsets = ev.onsets.filter((t) => t > last);
      if (onsets.length) events = [...events, ...onsets];
      // Uma olhada grava uma linha por objeto, e duas da mesma família no
      // mesmo instante só se distinguem pela caixa. Instante e família não
      // bastam: a rodada que caísse entre as duas gravações perderia a segunda.
      const key = (o: MarkedObject) =>
        `${o.instanteMs}|${o.classe}|${o.score}|${o.caixa?.join(',') ?? ''}`;
      const known = new Set(objects.map(key));
      const newOnes = ev.objetos.filter((o) => !known.has(key(o)));
      if (newOnes.length) objects = [...objects, ...newOnes];
    } catch {
      // Igual à timeline: falha de fundo não apaga o que já está na tela.
    }
  }

  // O `from` enviado é o início REAL do primeiro segmento, não o instante do
  // cursor. O anterior termina exatamente nesse instante e o teste do servidor
  // é `>`, então ele continua de fora; e o nome do arquivo, que sai do `from`
  // pedido (internal/api/recordings.go:352), passa a casar com o conteúdo.
  function exportClip(min: number) {
    const plano = planOf(min, Math.round(player.currentMs || 0));
    menu = null;
    if (!plano?.start) return;
    exportMin = min;
    buttonNotice(`Baixando ${min} min de vídeo a partir de ${hhmmss(plano.start)}`);
    location.href = mediaURL.export(cam, plano.start, plano.to);
  }

  async function capture() {
    menu = null;
    buttonNotice(`Baixando a imagem das ${hhmmss(player.currentMs)}`);
    // O nome segue a mesma convenção da exportação (cam_AAAA-MM-DD_HH-MM-SS),
    // para que imagem e vídeo do mesmo instante fiquem lado a lado na pasta.
    // E o instante é o MOSTRADO, não a hora atual: uma captura de ontem
    // nomeada com hoje não serviria para nada.
    const d = new Date(player.currentMs);
    const name = `${cam}_${dayKey(d)}_${hhmmss(d.getTime()).replaceAll(':', '-')}.jpg`;
    try {
      // A imagem sai como a tela está: com as caixas acesas, se estiverem ligadas.
      const layer = showBoxes ? boxesLayer : null;
      await downloadFrame(video, name, layer ? layer.drawOn : undefined);
    } catch (e) {
      captureError = messageOf(e);
      clearTimeout(noticeTimer);
      noticeTimer = setTimeout(() => {
        captureError = '';
      }, 5000);
    }
  }
</script>

<svelte:window onkeydown={keyHandler} onclick={outsideMenu} onresize={measurePage} />

<div
  class="page"
  bind:this={page}
  style:height={pageHeight ? `${pageHeight}px` : null}
  style:--alt-pagina="{pageHeight}px"
  style:--larg-pagina={pageWidth ? `${pageWidth}px` : null}
  style:--alt-linha="{rowHeight}px"
  style:--alt-legenda="{captionHeight}px"
>
  {#if !cameras.list.length && cameras.error}
    <!-- A lista vazia porque a carga falhou não é "nenhuma câmera". -->
    <p class="empty">Não deu para carregar as câmeras: {cameras.error}</p>
  {:else if !cameras.list.length && !cameras.loading}
    <NoCameras />
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
          <Icon name="chevronEsq" size={18} />
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
          <Icon name="chevronDir" size={18} />
        </button>
      </div>

      <DayNav value={day} days={daysList} onchange={changeDay} />
    </div>

    <!-- O que vai para a tela cheia. Lá dentro, o que não entra (miniaturas e
         os botões de miniaturas, caixas e baixar) sai do DOM em vez de só se
         esconder: as miniaturas não decodificam nada para ninguém ver. -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="full"
      class:fs={inFullscreen}
      class:pinned={pinnedControls}
      class:escondidos={hiddenControls}
      bind:this={full}
      {@attach (node: HTMLElement) => () => {
        // A de verdade o navegador desfaz sozinho quando o elemento sai da
        // página; a simulada ficaria cobrindo a tela seguinte.
        if (fullscreen.el === node) leaveFullscreen();
      }}
      onpointerdowncapture={() => {
        if (!inFullscreen) return;
        touch.cancel();
        controls.touched();
      }}
      onpointermove={(e) => {
        if (inFullscreen && e.pointerType === 'mouse') controls.show();
      }}
    >
      <div
        class="stage"
        class:animating
        class:switching
        class:shift={animating || drag !== 0}
        style:--troca="{SWITCH_MS}ms"
        style:--arrasto="{drag}px"
        bind:this={stage}
        onpointerdown={gestureStart}
        onpointermove={gestureMove}
        onpointerup={(e) => {
          if (e.pointerId === released) {
            released = null;
            return;
          }
          if (!gestureEnd(e)) touch(e);
        }}
        onpointercancel={(e) => {
          if (e.pointerId === released) released = null;
          gestureEnd(e);
        }}
        ondblclick={() => {
          if (lastPointer !== 'touch' && Date.now() - draggedAt > 2 * SWITCH_MS) toggleFull();
        }}
      >
        <video bind:this={video} playsinline controls={false}></video>
        {#if Boxes && showBoxes && objects.length}
          <Boxes
            bind:this={boxesLayer}
            {video}
            {objects}
            currentMs={player.currentMs}
            playing={player.playing}
            rate={player.rate}
            labels={showLabels}
          />
        {/if}
        <!-- Empilhados numa coluna, e não cada um no seu `position: absolute`:
           antes eram três avisos disputando o mesmo canto, e o aviso de captura
           chega justamente quando o "carregando…" tem chance de estar aceso. -->
        <div class="notices">
          {#if player.buffering || loading}<span class="badge">carregando…</span>{/if}
          {#if !loading && !error && !timeline.segments.length}
            <span class="badge">sem gravação neste dia</span>
          {/if}
          {#if player.error}<span class="badge bad">{player.error}</span>{/if}
          {#if error}<span class="badge bad">{error}</span>{/if}
          {#if captureError}<span class="badge bad">{captureError}</span>{/if}
        </div>
        {#if jumpHint}
          <div class="half-moon" class:right={jumpHint.side > 0} out:fade={{ duration: 250 }}>
            <Icon name={jumpHint.side < 0 ? 'skipLeft' : 'skipRight'} size={26} />
            <span>{jumpHint.sum} segundos</span>
          </div>
        {/if}
        {#if !inFullscreen}
          <span class="enter"
            ><FullscreenButton active={false} overVideo onclick={toggleFull} /></span
          >
        {/if}
      </div>

      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="dock"
        onpointerenter={(e) => {
          if (e.pointerType === 'mouse') overControls = true;
        }}
        onpointerleave={(e) => {
          if (e.pointerType !== 'mouse') return;
          overControls = false;
          if (inFullscreen) controls.show();
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
            <Icon name={player.playing ? 'pause' : 'play'} size={18} />
          </button>
          <Clock
            ms={player.currentMs}
            ongo={(seg) => {
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

          <!-- Na tela cheia não há a barra de cima: a câmera e as setas dela
               vêm para cá, e não para as laterais do vídeo, que no celular são
               do duplo toque que pula 10 s. Ficam nesta linha quando cabem, e
               numa linha própria acima dela quando não (o CSS decide pela
               largura, e não pelo nome: trocar de câmera não mexe na faixa). -->
          {#if inFullscreen && camPosition >= 0}
            <div class="camnav navfs">
              <button
                class="ghost"
                onclick={() => {
                  ir(-1);
                }}
                disabled={cameras.list.length < 2}
                aria-label="câmera anterior"
              >
                <Icon name="chevronEsq" size={18} />
              </button>
              <!-- A posição fica fora do nome: o "…" corta só o nome. -->
              <span class="cam-name">{cameras.list[camPosition]?.name}</span>
              <span class="pos">{camPosition + 1}/{cameras.list.length}</span>
              <button
                class="ghost"
                onclick={() => {
                  ir(1);
                }}
                disabled={cameras.list.length < 2}
                aria-label="próxima câmera"
              >
                <Icon name="chevronDir" size={18} />
              </button>
            </div>
          {/if}

          <span class="spacer"></span>
          {#if inFullscreen}
            <!-- Só o ícone: o `title` explica no desktop, e no celular quem toca
             vê na hora o que mudou. -->
            <button
              class="ghost pin"
              class:on={pinnedControls}
              onclick={() => (pinnedControls = !pinnedControls)}
              aria-label="fixar os controles"
              aria-pressed={pinnedControls}
              title={pinnedControls
                ? 'soltar os controles: somem sozinhos com o vídeo tocando'
                : 'fixar os controles embaixo do vídeo'}
            >
              <Icon name="pin" size={22} />
            </button>
            <FullscreenButton active onclick={toggleFull} />
          {:else}
            <!-- No celular só o ícone, e o toque se explica na snackbar; no
                 desktop o rótulo aparece ao lado. -->
            <button
              class="ghost ib"
              class:on={showThumbs}
              onclick={toggleThumbnails}
              aria-label={showThumbs ? 'ocultar miniaturas' : 'exibir miniaturas'}
              aria-pressed={showThumbs}
            >
              <Icon name="thumbnails" size={20} />
              <span class="label" bind:this={buttonLabel}>miniaturas</span>
            </button>
            <!-- Só com objeto no dia, como as cores da legenda: câmera sem detecção
           não tem caixa nenhuma para mostrar. -->
            {#if objects.length}
              <button
                class="ghost ib"
                class:on={showBoxes}
                onclick={toggleBoxes}
                aria-label="caixas de detecção: {BOXES_STATES[boxesState]}. Toque para trocar"
              >
                <Icon name={showLabels && showBoxes ? 'boxesLabel' : 'boxes'} size={20} />
                <span class="label">{BOXES_STATES[boxesState]}</span>
              </button>
            {/if}
            <span class="anchor">
              <button
                class="ghost ib"
                onclick={() => (menu = menu === 'baixar' ? null : 'baixar')}
                aria-label="baixar imagem ou vídeo"
                aria-haspopup="menu"
                aria-expanded={menu === 'baixar'}
              >
                <Icon name="download" size={20} />
                <span class="label">baixar</span>
              </button>
              {#if menu === 'baixar'}
                <div class="pop above" role="menu">
                  <div class="tit">Baixar</div>
                  <button
                    class="image"
                    role="menuitem"
                    onclick={capture}
                    disabled={!player.currentMs}
                  >
                    <Icon name="image" size={18} /> Imagem deste instante
                  </button>
                  <div class="tit">Vídeo a partir deste instante</div>
                  <div class="durations">
                    {#each planos as { min, plano } (min)}
                      <button
                        role="menuitem"
                        class:last={min === exportMin}
                        onclick={() => {
                          exportClip(min);
                        }}
                        disabled={!plano?.start}
                        title={plano?.erro}
                      >
                        {min} min
                      </button>
                    {/each}
                  </div>
                  <!-- O botão desabilitado diz que não dá; sozinho, ele não
                       diria por quê. -->
                  {#if fieldError?.plano?.erro}
                    <p class="field-error small">
                      {fieldError.min === DURATIONS[0]
                        ? ''
                        : `de ${fieldError.min} min em diante: `}{fieldError.plano.erro}
                    </p>
                  {/if}
                </div>
              {/if}
            </span>
          {/if}
        </div>

        {#if showThumbs && !inFullscreen}
          <!-- A câmera vem da timeline, e não do seletor: ao trocar de câmera
          o seletor muda na hora e os segmentos só quando a timeline nova
          chega. Nesse meio-tempo a tira pedia cada miniatura com a câmera nova
          e o instante da antiga - um 404 por miniatura, na frente da fila das
          certas. -->
          <div class="thumbnails">
            <ThumbStrip
              cam={timeline.cam}
              segments={timeline.segments}
              {objects}
              currentMs={player.currentMs}
              defer={player.loading}
              onseek={(ms: number) => {
                void player.seek(ms);
              }}
            />
          </div>
        {/if}

        <div class="row" bind:offsetHeight={rowHeight}>
          <Timeline
            ranges={timeline.ranges}
            {events}
            {objects}
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
    <div class="footer anchor" bind:offsetHeight={captionHeight}>
      <div class="legend muted small row wrap">
        <span><i class="has"></i> com gravação</span>
        <span><i class="gap"></i> sem gravação</span>
        {#if events.length}
          <span><i class="heat"></i> movimento</span>
        {/if}
        <!-- As três, e não só as do dia: a legenda explica a cor antes de ela
           aparecer, e não muda de tamanho a cada marca nova. -->
        {#if objects.length}
          {#each Object.values(FAMILIES) as f (f.nome)}
            <span><i style:background={f.color}></i> {f.nome}</span>
          {/each}
        {/if}
        <button
          class="ghost info"
          onclick={() => (menu = menu === 'gestos' ? null : 'gestos')}
          aria-label="gestos da timeline"
          aria-expanded={menu === 'gestos'}
        >
          <Icon name="info" size={16} />
        </button>
      </div>
      {#if menu === 'gestos'}
        <div class="pop above gestures" role="dialog" aria-label="gestos da timeline">
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
    /* A largura do vídeo na altura que sobra (medida em `medirLargura`). */
    max-width: var(--larg-pagina, none);
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
    /* O duplo toque e o deslize para o lado são nossos: o navegador fica só
       com a rolagem vertical e a pinça. */
    touch-action: pan-y pinch-zoom;
    user-select: none;
    -webkit-user-select: none;
  }

  /* O que desliza com o dedo: o vídeo e as caixas por cima dele. Os avisos, a
     meia-lua e o botão de tela cheia ficam. O `transform` só existe durante o
     gesto: parado, o vídeo não ganha camada própria à toa. */
  .stage.shift > video,
  .stage.shift > :global(canvas) {
    transform: translateX(var(--arrasto));
  }
  .stage.animating > video,
  .stage.animating > :global(canvas) {
    transition: transform var(--troca) ease-out;
  }
  .stage.switching > video,
  .stage.switching > :global(canvas) {
    visibility: hidden;
  }

  /* O retorno do pulo de 10 s, como a do YouTube: um arco translúcido colado na
     borda do lado do pulo. Passa os toques adiante, para o vídeo. */
  .half-moon {
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
  .half-moon.right {
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
  .full:not(.fs) {
    /* Do tamanho do conteúdo, e só encolhe: a sobra fica embaixo da legenda,
       e não entre ela e a timeline. */
    flex: 0 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .full:not(.fs) .stage {
    flex: 0 1 auto;
    width: 100%;
    aspect-ratio: 16 / 9;
    min-height: 120px;
  }
  .full:not(.fs) .dock {
    flex: none;
    display: grid;
    gap: 8px;
  }
  /* A tira rola por dentro: sem o piso zero, a largura dela abriria a página. */
  .thumbnails {
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
  .label {
    display: none;
    font-size: 14px;
  }
  @media (min-width: 720px) {
    .label {
      display: inline;
    }
  }

  /* O menu do baixar, no desenho do menu da conta. */
  .anchor {
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
  .pop.above {
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
  .image {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    margin: 2px 10px 6px;
    min-height: 38px;
    padding: 4px 10px;
    font-size: 13px;
  }
  .durations {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    padding: 2px 10px 6px;
  }
  .durations button {
    flex: 1;
    min-height: 38px;
    padding: 4px 10px;
    font-size: 13px;
    white-space: nowrap;
  }
  /* A última duração baixada: só a borda, porque o toque baixa, não escolhe. */
  .durations button.last {
    border-color: var(--accent);
  }
  .field-error {
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
      /* Aqui o vídeo vai para o lado: a conta da largura não se aplica. */
      max-width: none;
    }
    .full:not(.fs),
    .full:not(.fs) .dock {
      display: contents;
    }
    .full:not(.fs) .stage {
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
    .thumbnails {
      grid-area: mini;
      align-self: start;
    }
    .row {
      grid-area: linha;
    }
    /* Uma linha só, que rola para o lado: quebrada em três, ela tirava do
       vídeo mais da metade da altura. */
    .footer {
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
    .label {
      display: none;
    }
    /* Os controles ficam no alto: o menu do baixar abre para baixo. */
    .controls .pop.above {
      top: calc(100% + 6px);
      bottom: auto;
    }
  }

  .enter {
    position: absolute;
    right: 8px;
    bottom: 8px;
  }

  /* Na tela cheia: sem `position`, porque quem o põe é o navegador (`:fullscreen`)
     ou a regra da simulada no app.css, e ele é o apoio da faixa de controles. */
  .full.fs {
    display: flex;
    flex-direction: column;
    gap: 0;
    background: #000;
  }
  .full.fs .stage {
    flex: 1;
    min-height: 0;
    border-radius: 0;
  }
  .full.fs video {
    width: 100%;
    height: 100%;
    aspect-ratio: auto;
    max-height: none;
  }
  /* Fixos: a faixa embaixo do vídeo, que fica com o que sobra da altura. */
  .full.fs .dock {
    gap: 8px;
    padding: 8px max(8px, env(safe-area-inset-right)) max(8px, env(safe-area-inset-bottom))
      max(8px, env(safe-area-inset-left));
    background: var(--bg);
    border-top: 1px solid var(--line);
  }
  /* Soltos: por cima do vídeo, sobre um degradê que segura a leitura num vídeo
     claro. */
  .full.fs:not(.pinned) .dock {
    position: absolute;
    inset: auto 0 0 0;
    padding-top: 28px;
    background: linear-gradient(to bottom, transparent, rgba(0, 0, 0, 0.75) 28px);
    border-top: 0;
    transition:
      opacity 0.25s,
      transform 0.25s;
  }
  .full.escondidos .dock {
    opacity: 0;
    transform: translateY(12px);
    pointer-events: none;
  }
  .full.escondidos {
    cursor: none;
  }

  /* O alfinete, do mesmo tamanho do botão da tela cheia ao lado. */
  .pin {
    display: grid;
    place-items: center;
    width: 44px;
    padding: 0;
  }
  .pin.on {
    background: var(--accent);
    border-color: var(--accent);
    color: #fff;
  }

  .notices {
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

  /* A câmera na faixa da tela cheia, com as setas da barra de cima. Na mesma
     linha do ▶ quando a faixa tem 520 px; abaixo disso ela vai para uma
     linha própria, centrada, acima dos controles. O corte é pela largura, e
     não pelo nome, para a faixa não mudar ao trocar de câmera. */
  .full.fs .dock {
    container-type: inline-size;
  }
  .navfs {
    display: flex;
    align-items: center;
    flex: 0 1 auto;
    min-width: 0;
  }
  .navfs button {
    flex: none;
  }
  .cam-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    padding: 0 4px;
  }
  /* Fora do nome e sem encolher: um nome longo corta antes dela. */
  .pos {
    flex: none;
    margin-left: 2px;
    padding-right: 4px;
    color: var(--dim);
    font-variant-numeric: tabular-nums;
  }
  /* 520 px: ▶, relógio, velocidade, uns 10 caracteres de nome com as setas,
     alfinete e sair. */
  @container (max-width: 519.98px) {
    .controls:has(.navfs) {
      flex-wrap: wrap;
      row-gap: 4px;
    }
    .navfs {
      order: -1;
      flex: 1 0 100%;
      justify-content: center;
    }
  }
  .footer {
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
  .pop.gestures {
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
