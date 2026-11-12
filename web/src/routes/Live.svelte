<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import '../vendor/video-stream.js';
  import { VideoRTC } from '../vendor/video-rtc.js';
  import { cameras, loadCameras, session, sessionDropped } from '../lib/state.svelte.ts';
  import { currentParams, write, scene } from '../lib/route.svelte.ts';
  import { api, mediaURL, messageOf } from '../lib/api.ts';
  import type { Session } from '../lib/api.ts';
  import { downloadFrame } from '../lib/capture.ts';
  import { dayKey, hhmmss } from '../lib/format.ts';
  import NoCameras from '../components/NoCameras.svelte';
  import CamerasButton from '../components/CamerasButton.svelte';
  import CameraPicker from '../components/CameraPicker.svelte';
  import FullscreenButton from '../components/FullscreenButton.svelte';
  import { fullscreen, enterFullscreen, leaveFullscreen } from '../lib/fullscreen.svelte.ts';
  import { vanishingControls } from '../lib/vanishingControls.svelte.ts';
  import { doubleTouch } from '../lib/doubleTouch.ts';
  import { freeHeight } from '../lib/freeHeight.ts';
  import { newScreen } from '../lib/screen-version.svelte.ts';
  import { SWITCH_MS, AXIS_PX, swipeLimit } from '../lib/swipe.ts';
  import Icon from '../components/Icon.svelte';

  const STORAGE_KEY = 'dwnvr.live.selection';
  const LAYOUT_KEY = 'dwnvr.live.layout';
  const ALWAYS_KEY = 'dwnvr.live.sempreLigadas';

  // Quantas colunas, ou encaixar tudo na tela.
  type Mode = 'fit' | number;

  const COLUMNS = [1, 2, 3];
  // 'fit' é só mais um modo ao lado das colunas: como um exclui o outro por
  // construção, escolher 2× desliga o encaixar sem nenhum código para isso.
  const MODES: Mode[] = ['fit', ...COLUMNS];

  // Precisa bater com o `gap` da grade no CSS: é o espaço que a conta do
  // encaixe desconta antes de dividir o que sobra entre os tiles.
  const GAP = 8;
  const RATIO = 16 / 9;
  // Precisa bater com o `gap` da `.page` no CSS: o respiro entre a barra e o
  // palco, que a altura das colunas desconta.
  const PAGE_GAP = 10;
  // Abaixo disso o tile fica pequeno demais para ver a câmera: a grade passa
  // a rolar em vez de encolher mais.
  const MIN_COLUMNS_HEIGHT = 180;

  // Até quantas câmeras o texto do compartilhar diz pelo nome.
  const NAMES_IN_TEXT = 3;

  // Quanto tempo um tile rolado para fora da tela segue ligado antes do corte.
  // Longa de propósito: voltar para onde se estava há pouco não pode custar a
  // espera por um keyframe. Ver `soNaTela`.
  const SCROLL_GRACE_MS = 30_000;

  // Intervalo mínimo entre uma tentativa de conexão e a seguinte, crescendo a
  // cada falha seguida e voltando ao primeiro quando o vídeo toca. O player do
  // go2rtc usa 15 s fixos. Ver `religarRapido`.
  const RECONNECT_MS = [500, 1000, 2000, 5000];
  const RECONNECT_MAX_MS = 5000;
  // A espera depois de `n` falhas seguidas: a última se repete.
  const reconnectAt = (n: number) =>
    RECONNECT_MS[Math.min(n, RECONNECT_MS.length - 1)] ?? RECONNECT_MAX_MS;

  // Quanto cada sondagem do servidor espera antes de desistir, ao voltar de
  // uma aba oculta. Ver `esperarRede`.
  const PROBE_MS = 2000;

  // De quanto em quanto tempo a tela confere a sessão com o vídeo à vista, e
  // quanto cada conferência espera a resposta. Ver `conferirSessao`.
  const CHECK_SESSION_MS = 60_000;
  const CHECK_TIMEOUT_MS = 10_000;
  // O tique que decide se está na hora da conferência de rotina.
  const CHECK_TICK_MS = 5000;
  // Enquanto a sessão não se confirma: quantos segundos do fim de uma
  // tentativa até a próxima, os da contagem que o aviso mostra.
  const RETRY_WAIT_S = 5;

  // Lida uma vez, na inicialização: daqui em diante quem manda é o estado da
  // tela, que escreve de volta na URL.
  const params = currentParams();

  let selected = $state(new Set<string>());
  let mode = $state<Mode>(readMode());
  let showPicker = $state(false);
  let stage = $state<HTMLDivElement | null>(null);
  let fitting = $state({ cols: 1, w: 0 });
  // Nas colunas, a altura que um tile pode ter sem passar da janela: é ela, e
  // não uma largura fixa, que limita a grade - ver `larguraDasColunas`.
  let columnsHeight = $state(0);
  let bar = $state<HTMLDivElement | null>(null);
  // Segura a escrita na URL até a seleção ter sido lida - ver o efeito no fim
  // do bloco.
  let mounted = $state(false);

  const visible = $derived(cameras.list.filter((c) => selected.has(c.id)));
  const allChecked = $derived(cameras.list.length > 0 && selected.size === cameras.list.length);

  // A URL vem primeiro, o localStorage depois, e só então o palpite pela
  // largura da tela. A URL descreve ESTE link; o localStorage, o hábito deste
  // navegador. Ler dali nunca grava aqui - senão abrir o link de outra pessoa
  // viraria a preferência de quem abriu.
  //
  // Tudo isso na inicialização, e não no onMount: começar sempre em 2× e
  // corrigir depois faria a grade piscar no celular, onde o padrão é 1×.
  function readMode(): Mode {
    const fromURL = params.get('view');
    if (fromURL === 'fit') return 'fit';
    if (COLUMNS.includes(Number(fromURL))) return Number(fromURL);
    return lerLayout();
  }

  function lerLayout(): Mode {
    const saved = localStorage.getItem(LAYOUT_KEY);
    if (saved === 'fit') return 'fit';
    if (COLUMNS.includes(Number(saved))) return Number(saved);
    return matchMedia('(min-width: 640px)').matches ? 2 : 1;
  }

  onMount(async () => {
    if (!cameras.list.length) await loadCameras();

    // `cams=` vazio é uma escolha - "nenhuma câmera" -, e a chave ausente é
    // falta de opinião: aí vale o que este navegador usou por último.
    const fromURL = params.has('cams') ? params.getAll('cams').filter(Boolean) : null;
    const ids = fromURL ?? savedSelection();
    if (ids) {
      // Só mantém câmeras que ainda existem: uma seleção salva - ou um link
      // antigo - pode citar uma câmera removida do cadastro desde então.
      const wanting = new Set(ids);
      selected = new Set(cameras.list.filter((c) => wanting.has(c.id)).map((c) => c.id));
    }
    if (!selected.size && !fromURL) {
      // Duas por padrão: abrir nove streams 1080p de uma vez trava celular.
      selected = new Set(cameras.list.slice(0, 2).map((c) => c.id));
    }
    mounted = true;
  });

  // A seleção que este navegador usou por último. Conteúdo estragado vale como
  // nenhum.
  function savedSelection(): string[] | null {
    try {
      const v: unknown = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? 'null');
      return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : null;
    } catch {
      return null;
    }
  }

  // A URL passa a dizer o que está na tela, e é isso que faz colar o endereço
  // em outra aba cair na mesma grade. O `montado` segura a escrita até a
  // seleção ter sido lida: sem ele o primeiro quadro publicaria `cams=` vazio,
  // apagando da barra de endereços justamente o que ainda ia ser lido dela.
  $effect(() => {
    if (!mounted) return;
    write({
      // Na ordem do cadastro, e não na de inserção do Set: desmarcar e marcar
      // de novo reescreveria a URL sem nada ter mudado na tela.
      cams: cameras.list.filter((c) => selected.has(c.id)).map((c) => c.id),
      // Sempre escrito, mesmo no padrão: o padrão daqui depende da largura da
      // tela, então omiti-lo faria o mesmo link abrir diferente no celular.
      view: mode,
    });
  });

  // O que o compartilhar diz em palavras: as câmeras marcadas, pelo nome até
  // NOMES_NO_TEXTO; acima disso só a conta, que a lista não caberia num assunto.
  $effect(() => {
    if (!mounted) return;
    const names = cameras.list.filter((c) => selected.has(c.id)).map((c) => c.name);
    if (!names.length) scene.texto = '';
    else if (names.length <= NAMES_IN_TEXT) scene.texto = `Ao vivo: ${names.join(', ')}`;
    else scene.texto = `Ao vivo: ${names.length} câmeras`;
  });

  // O componente do go2rtc cria o <video> com controls=true. Numa grade ao
  // vivo isso mostra uma barra de progresso que não significa nada - não há
  // linha do tempo para percorrer - e ainda aparece em umas câmeras e não em
  // outras, conforme o modo negociado.
  function hideNativeControls(node: VideoRTC) {
    const apply = () => {
      if (node.video) {
        node.video.controls = false;
        return true;
      }
      return false;
    };
    // O <video> nasce no connectedCallback, que pode não ter rodado ainda.
    if (!apply()) requestAnimationFrame(apply);
  }

  // --- a tela cheia --------------------------------------------------------------

  // Vai para a tela cheia o palco inteiro, e não o tile: é ele que fica parado
  // enquanto as câmeras trocam. Lá dentro a grade vira um carrossel - a câmera
  // atual ocupa a tela e as vizinhas esperam dos lados, prontas para entrar com
  // o dedo -, e as outras saem de cena com `display: none`.
  //
  // São os mesmos tiles da grade, com o mesmo player: entrar e sair não
  // reconecta nada. Quem sai de cena cai na regra do `soNaTela`, como um tile
  // rolado para fora: segue ligado por CARENCIA_ROLAGEM_MS (ou sempre, com o
  // "Pausar fora da tela" desligado) e depois é cortado. Por isso a troca é na hora logo
  // depois de entrar, e espera um keyframe depois disso.
  const inFullscreen = $derived(!!stage && fullscreen.el === stage);
  // A câmera na tela, pelo id: tirar outra da seleção não a troca de lugar.
  let current = $state<string | null>(null);
  const currentIdx = $derived(visible.findIndex((c) => c.id === current));
  // Com duas câmeras a anterior e a próxima são a mesma: ela espera do lado
  // de onde vai entrar.
  let side = $state<1 | -1>(1);
  // Quanto o carrossel está deslocado, em px, e se o deslocamento anima.
  let drag = $state(0);
  let animating = $state(false);

  // O nome e a posição ficam; setas, ⋮, PiP e o botão de sair somem. Não
  // somem com o menu aberto nem com o mouse em cima de um deles.
  let overControls = false;
  const controls = vanishingControls(() => inFullscreen && !menu && !overControls);

  type Role = 'atual' | 'ant' | 'prox' | 'fora';

  function role(i: number): Role {
    const n = visible.length;
    if (i === currentIdx) return 'atual';
    if (n === 2) return side === 1 ? 'prox' : 'ant';
    if (i === (currentIdx + 1) % n) return 'prox';
    if (i === (currentIdx - 1 + n) % n) return 'ant';
    return 'fora';
  }

  function toggleFull(cam: string) {
    if (!stage) return;
    if (inFullscreen) {
      // Dois deslizes seguidos rápidos também fazem um duplo clique.
      if (Date.now() - draggedAt > 2 * SWITCH_MS) leaveFullscreen();
      return;
    }
    current = cam;
    menu = null;
    enterFullscreen(stage);
  }

  // A próxima (1) ou a anterior (-1), em roda: da última volta à primeira.
  function ir(d: 1 | -1) {
    const n = visible.length;
    if (animating || n < 2 || !stage) return;
    menu = null;
    side = d;
    animating = true;
    drag = -d * stage.clientWidth;
    setTimeout(() => {
      // Tudo no mesmo tique: a câmera que entrou vira a atual no mesmo quadro
      // em que o carrossel volta ao zero, sem animar a volta.
      animating = false;
      drag = 0;
      current = visible[(currentIdx + d + n) % n]?.id ?? current;
    }, SWITCH_MS);
  }

  // O deslize. O eixo se decide nos primeiros EIXO_PX: para cima ou para
  // baixo não é com a tela. Começar num botão ou no menu também não.
  let gesture: { id: number; x: number; y: number; axis: 'x' | 'y' | null } | null = null;
  let draggedAt = 0;

  function gestureStart(e: PointerEvent) {
    if (!inFullscreen || animating || visible.length < 2) return;
    if (e.target instanceof Element && e.target.closest('button, a, .menu')) return;
    gesture = { id: e.pointerId, x: e.clientX, y: e.clientY, axis: null };
  }

  function gestureMove(e: PointerEvent) {
    if (inFullscreen && e.pointerType === 'mouse') {
      overControls = e.target instanceof Element && !!e.target.closest('button, a, .menu');
      controls.show();
    }
    if (gesture?.id !== e.pointerId || !stage) return;
    const dx = e.clientX - gesture.x;
    const dy = e.clientY - gesture.y;
    if (!gesture.axis && Math.hypot(dx, dy) > AXIS_PX) {
      gesture.axis = Math.abs(dx) > Math.abs(dy) ? 'x' : 'y';
      if (gesture.axis === 'x') stage.setPointerCapture(e.pointerId);
    }
    if (gesture.axis !== 'x') return;
    if (visible.length === 2) side = dx < 0 ? 1 : -1;
    drag = dx;
  }

  function gestureEnd(e: PointerEvent) {
    if (gesture?.id !== e.pointerId || !stage) return;
    const { axis, x } = gesture;
    gesture = null;
    if (axis !== 'x') return;
    draggedAt = Date.now();
    const dx = e.clientX - x;
    if (e.type === 'pointerup' && Math.abs(dx) > swipeLimit(stage.clientWidth)) {
      ir(dx < 0 ? 1 : -1);
      return;
    }
    // Curto demais: a câmera volta para o lugar.
    animating = true;
    drag = 0;
    setTimeout(() => (animating = false), SWITCH_MS);
  }

  // O toque simples na imagem esconde os controles, como nas gravações. Só o
  // toque que não andou: o deslize troca de câmera. O duplo toque é do
  // `dblclick` do tile, que o Chrome do Android também gera, e o toque que
  // fecha o menu só fecha o menu.
  let touchAt: { id: number; x: number; y: number; menu: boolean } | null = null;
  const touch = doubleTouch({
    simples: () => {
      if (inFullscreen) controls.singleTouch();
    },
  });

  function touchStart(e: PointerEvent) {
    touchAt =
      e.pointerType === 'touch'
        ? { id: e.pointerId, x: e.clientX, y: e.clientY, menu: !!menu }
        : null;
  }

  function touchEnd(e: PointerEvent) {
    if (touchAt?.id !== e.pointerId) return;
    const { x, y, menu: withMenu } = touchAt;
    touchAt = null;
    if (withMenu || Math.hypot(e.clientX - x, e.clientY - y) > AXIS_PX) return;
    if (e.target instanceof Element && e.target.closest('button, a, .menu')) return;
    touch(e);
  }

  onMount(() => () => {
    controls.stop();
  });

  // Entrar e trocar de câmera mostram os controles e recomeçam a contagem.
  $effect(() => {
    if (!inFullscreen) return;
    // eslint-disable-next-line @typescript-eslint/no-unused-expressions -- só a leitura, para o $effect rastrear
    current;
    untrack(() => {
      controls.show();
    });
  });

  // A câmera da tela sumiu da seleção por outro caminho, ou o acesso não se
  // confirmou e a grade se esvaziou: a tela cheia ficaria preta.
  $effect(() => {
    if (inFullscreen && (currentIdx < 0 || !accessConfirmed)) leaveFullscreen();
  });

  // --- o menu do ⋮ de cada tile -----------------------------------------------

  // Um aberto por vez, pela câmera dele. Fica DENTRO do tile, e não fora da
  // grade como o das Detecções: em tela cheia o navegador só mostra o elemento
  // que foi para a tela cheia, e um menu fora dele sumiria ali.
  let menu = $state<string | null>(null);
  // O <video-stream> de cada câmera, para a captura achar o <video> dele. Não
  // precisa ser reativo: só é lido no clique.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- só é lido no clique
  const players = new Map<string, VideoRTC>();
  // Falha de captura ou de PiP por câmera, mostrada no próprio tile por alguns
  // segundos. Vazio é sem aviso.
  const notices = $state<Record<string, string>>({});
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- os timers, que a tela não lê
  const noticeTimer = new Map<string, number>();

  function notify(cam: string, text: string) {
    notices[cam] = text;
    clearTimeout(noticeTimer.get(cam));
    noticeTimer.set(
      cam,
      setTimeout(() => (notices[cam] = ''), 5000),
    );
  }

  function outsideMenu(e: MouseEvent) {
    if (menu && !(e.target instanceof Element && e.target.closest('.options'))) menu = null;
  }

  function keyHandler(e: KeyboardEvent) {
    if (e.key === 'Escape') menu = null;
    if (inFullscreen && (e.key === 'ArrowRight' || e.key === 'ArrowLeft')) {
      e.preventDefault();
      ir(e.key === 'ArrowRight' ? 1 : -1);
    }
  }

  // Entre o ⋮ e o menu, e entre o menu e a borda da tela.
  const MENU_SLACK = 6;
  const MENU_BORDER = 8;

  // O tile corta o que passa da borda dele, e no celular, com duas colunas, ele
  // tem uns 180px de altura: o menu virava uma caixinha com rolagem. Por isso o
  // menu é `position: fixed`, que escapa do corte mesmo dentro do tile, posto
  // aqui junto do ⋮. Abre para baixo; se não couber e houver mais espaço
  // acima, para cima; e só rola se não couber em nenhum dos dois.
  function positionMenu(el: HTMLElement) {
    const mark = el.parentElement?.querySelector('.mais span');
    if (!mark) return;
    const r = mark.getBoundingClientRect();
    const height = el.offsetHeight;
    const below = window.innerHeight - r.bottom - MENU_SLACK - MENU_BORDER;
    const above = r.top - MENU_SLACK - MENU_BORDER;
    const upward = height > below && above > below;
    const fits = upward ? above : below;
    el.style.right = `${document.documentElement.clientWidth - r.right}px`;
    el.style.top = `${upward ? r.top - MENU_SLACK - Math.min(height, fits) : r.bottom + MENU_SLACK}px`;
    el.style.maxHeight = `${fits}px`;
  }

  // Preso à tela e não ao tile, o menu ficaria para trás quando a página rola.
  function closeMenu() {
    menu = null;
  }

  // O quadro é o que está na tela, e o nome leva a hora do clique: no live os
  // dois diferem só pela latência do stream. Mesma convenção das Gravações
  // (cam_AAAA-MM-DD_HH-MM-SS), para imagem de live e de gravação ficarem lado a
  // lado na pasta.
  async function capture(cam: string) {
    menu = null;
    const d = new Date();
    const name = `${cam}_${dayKey(d)}_${hhmmss(d.getTime()).replaceAll(':', '-')}.jpg`;
    try {
      await downloadFrame(players.get(cam)?.video, name);
    } catch (e) {
      notify(cam, messageOf(e));
    }
  }

  // O mesmo que desmarcar a câmera na lista: grava a seleção e a URL acompanha.
  // Na tela cheia, a próxima entra no lugar; se era a única, a tela cheia
  // fecha.
  function remove(cam: string) {
    menu = null;
    if (inFullscreen && cam === current) {
      const n = visible.length;
      if (n < 2) leaveFullscreen();
      else current = visible[(currentIdx + 1) % n]?.id ?? null;
    }
    toggle(cam);
  }

  // --- picture-in-picture ------------------------------------------------------

  // O Firefox não tem a API (tem só o botão dele, desenhado sobre o vídeo): lá
  // o botão nem aparece.
  const PIP = document.pictureInPictureEnabled;
  // A câmera que está na janela flutuante. O navegador deixa uma por vez.
  let pipCam = $state<string | null>(null);

  async function togglePip(cam: string) {
    const video = players.get(cam)?.video;
    try {
      if (!video) throw new Error('sem vídeo');
      if (document.pictureInPictureElement === video) await document.exitPictureInPicture();
      else await video.requestPictureInPicture();
    } catch {
      // O motivo comum é o vídeo ainda não ter começado: sem a primeira imagem
      // o navegador recusa a janela.
      notify(cam, 'o vídeo ainda não começou');
    }
  }

  // O player do go2rtc derruba a conexão 5 s depois de a aba ficar oculta, e
  // aba oculta é justamente o caso de uso do PiP: minimizar o navegador
  // congelaria a janelinha. O corte passa pelo `disconnectedCallback` da
  // instância, então é ali que se segura a câmera que está no PiP - sem mexer
  // no arquivo copiado. Quando o tile sai da tela de verdade (trocar de rota,
  // desmarcar a câmera), o navegador chama o do protótipo, e a conexão fecha
  // como sempre.
  //
  // As outras câmeras continuam sendo cortadas: com a aba oculta, só a do PiP
  // segue puxando vídeo.
  function keepInPip(node: VideoRTC, cam: string) {
    const turnOff = node.disconnectedCallback.bind(node);
    node.disconnectedCallback = () => {
      if (node.isConnected && document.pictureInPictureElement === node.video) return;
      turnOff();
    };
    // Os eventos saem do <video>, que nasce depois e não os propaga para cima:
    // escutar na fase de captura do <video-stream> os pega mesmo assim.
    node.addEventListener('enterpictureinpicture', () => (pipCam = cam), true);
    node.addEventListener(
      'leavepictureinpicture',
      () => {
        if (pipCam === cam) pipCam = null;
        // Fechar o PiP com a aba ainda oculta, ou com o tile rolado para fora
        // da tela, aplica o corte que foi segurado. A volta é do próprio
        // player (aba) ou do `soNaTela` (rolagem).
        if (document.hidden || (!alwaysOn && offScreen.has(node))) turnOff();
      },
      true,
    );
  }

  // --- religar rápido -----------------------------------------------------------

  // Medido no celular (Android, Chrome, captura do chrome://net-export): com a
  // aba oculta o Android corta a rede do Chrome, e uma conexão aberta nesse
  // meio-tempo perde o SYN. O TCP só o reenvia em 1, 3, 7, 15, 31 e 63 s, e a
  // capturada abriu aos 64 s. Pior: o Chrome só deixa um WebSocket por vez em
  // "conectando" para o mesmo servidor, e as 9 câmeras que tentaram ao voltar
  // ficaram na fila atrás dela, sem nem começar o próprio TCP. O player do
  // go2rtc religa sozinho com a aba oculta (WebRTC que cai, o `onclose`), e é
  // daí que vinham os ~17 s de tela preta.
  //
  // Daí as três peças:
  // - com a aba oculta, nenhuma câmera abre conexão - só a do PiP, que é vista;
  // - ao voltar, nenhuma tenta antes de o servidor responder a um `fetch`
  //   (`esperarRede`): logo na volta a rede ainda está suspensa, e as 9
  //   tentativas falhavam em menos de 50 ms, caindo nos 15 s do player;
  // - quando uma conexão cai de verdade, a próxima tentativa sai logo, no
  //   lugar desses 15 s fixos.

  // A espera em curso ao voltar de uma aba oculta, ou null quando não há.
  let network: Promise<void> | null = null;

  function onVisibilityChange() {
    isVisible = !document.hidden;
    if (document.hidden || network) return;
    network = waitNetwork().finally(() => (network = null));
  }

  // Na fase de captura da `window`, para valer antes do listener que cada
  // player põe no `document` e que é quem religa as câmeras.
  onMount(() => {
    window.addEventListener('visibilitychange', onVisibilityChange, true);
    return () => {
      window.removeEventListener('visibilitychange', onVisibilityChange, true);
    };
  });

  // Qualquer resposta serve, até um erro HTTP: o que importa é o servidor ter
  // sido alcançado. Desiste se a aba voltar a ficar oculta.
  async function waitNetwork() {
    for (let i = 0; !document.hidden; i++) {
      try {
        await api.ping(PROBE_MS);
        return;
      } catch {
        await new Promise((r) => setTimeout(r, reconnectAt(i)));
      }
    }
  }

  // O `onconnect` é por onde o player abre toda conexão. Com a aba oculta ele
  // não faz nada: ao voltar, o próprio player chama o `connectedCallback`, que
  // passa por aqui de novo. Enquanto a espera da volta corre, ele só agenda:
  // quando ela acaba, a câmera conecta se ainda deve - aba visível e tile na
  // tela, a mesma regra do `soNaTela`.
  //
  // O player só lê o `RECONNECT_TIMEOUT` no `onclose`, para agendar a próxima
  // tentativa: trocar o valor logo antes, na instância, basta - sem mexer no
  // arquivo copiado. A conta de quanto esperar continua sendo a dele, a partir
  // do início da tentativa que falhou. A sequência só volta ao começo quando o
  // vídeo toca, e não quando a conexão abre: um servidor que aceita e derruba
  // logo em seguida ficaria sendo tentado a cada meio segundo.
  function reconnectFast(node: VideoRTC) {
    const connect = node.onconnect.bind(node);
    node.onconnect = () => {
      if (document.hidden && document.pictureInPictureElement !== node.video) return false;
      if (!network) return connect();
      void network.then(() => {
        if (!document.hidden && node.isConnected && (alwaysOn || !offScreen.has(node))) connect();
      });
      return false;
    };
    let failures = 0;
    const closed = node.onclose.bind(node);
    node.onclose = () => {
      node.RECONNECT_TIMEOUT = reconnectAt(failures);
      const reconnecting = closed();
      if (reconnecting) failures++;
      // Caiu e não voltou: pode ser a sessão. O websocket recusado não diz o
      // motivo ao navegador, então quem responde é o /api/session.
      if (failures >= 2) checkSession();
      return reconnecting;
    };
    // O `playing` sai do <video>, que não o propaga: só a fase de captura o
    // pega aqui, como em `manterNoPip`.
    node.addEventListener('playing', () => (failures = 0), true);
  }

  // --- a sessão ----------------------------------------------------------------

  // Quem é removido, ou ganha link novo, perde a sessão na hora: o servidor
  // recusa toda requisição dela e fecha os websockets do ao vivo. Isso basta
  // para o MSE, cujo vídeo vem por dentro do websocket. O WebRTC não: o player
  // fecha o websocket assim que o vídeo toca, e a mídia segue direto do go2rtc
  // pela UDP, onde o dwnvr não alcança. Quem fecha o WebRTC é esta tela, e por
  // três caminhos:
  // - o aviso de sessão, um websocket mudo que o servidor fecha ao derrubar a
  //   pessoa: ao vê-lo fechar, a tela confere a sessão na hora;
  // - a conferência a cada CONFERIR_SESSAO_MS com o vídeo à vista, que é a
  //   rede de segurança se o aviso se perder;
  // - a câmera que caiu e não voltou, em `religarRapido`.
  // Sessão que não vale leva ao login. Sessão que não se confirma - o dwnvr
  // não respondeu - para o vídeo até confirmar: na dúvida, fecha. Enquanto não
  // confirma, o aviso conta os segundos até a próxima tentativa e deixa tentar
  // na hora.

  // Com o vídeo à vista: aba visível, ou uma câmera no PiP.
  let isVisible = $state(!document.hidden);
  const isActive = $derived(isVisible || pipCam !== null);

  let accessConfirmed = $state(true);
  let lastCheck = Date.now();
  let checking: Promise<void> | null = null;
  // O mesmo `conferindo`, visível para o aviso: a promise não é reativa.
  let checkingNow = $state(false);
  // Uma tentativa esperando a `rede` antes de conferir. Fica separada: uma
  // conferência de outra origem pode terminar antes de a espera acabar.
  let waitingNetwork = $state(false);
  // O aviso diz "Conferindo…" e a contagem para enquanto qualquer uma corre.
  const busy = $derived(checkingNow || waitingNetwork);
  // Segundos até a próxima tentativa, enquanto o acesso não se confirma.
  let lacking = $state(RETRY_WAIT_S);
  // O acesso voltou depois de uma falha: só o leitor de tela precisa disso, o
  // aviso que some já diz o mesmo a quem vê.
  let accessRestored = $state(false);

  // fecharTudo corta o vídeo de todas as câmeras na hora. Desmontar o tile
  // também corta, mas só depois dos 5 s de carência do player, e o WebRTC
  // seguiria recebendo vídeo nesse meio-tempo.
  function closeAll() {
    for (const node of players.values()) node.ondisconnect();
  }

  function checkSession() {
    if (!session.authRequired) return;
    checking ??= (async () => {
      checkingNow = true;
      try {
        const r = await api.checkSession(CHECK_TIMEOUT_MS);
        const s = (await r.json()) as Session;
        if (!s.authenticated) {
          closeAll();
          sessionDropped();
        } else {
          if (!accessConfirmed) accessRestored = true;
          accessConfirmed = true;
        }
      } catch {
        closeAll();
        accessConfirmed = false;
        accessRestored = false;
      } finally {
        lastCheck = Date.now();
        checking = null;
        checkingNow = false;
        // A contagem recomeça do fim da tentativa, venha ela de onde vier:
        // da contagem, do botão, do aviso de sessão ou de uma câmera.
        lacking = RETRY_WAIT_S;
      }
    })();
  }

  // Enquanto `rede` espera o servidor responder na volta da aba oculta, não
  // confere: a rede ainda suspensa daria um "não confirmou" falso, e o vídeo
  // pararia à toa. A tentativa espera por ela, e o aviso já diz "Conferindo…".
  // A espera também acaba se a aba voltar a ficar oculta: aí não confere, e a
  // contagem segue na próxima volta. Uma espera por vez. Também não confere se
  // a tela já saiu, como no logout: a resposta de uma conferência órfã,
  // chegando depois de um login novo, derrubaria a sessão nova.
  let liveScreen = true;
  onMount(() => () => {
    liveScreen = false;
  });
  function retry() {
    if (!network) return checkSession();
    if (waitingNetwork) return;
    waitingNetwork = true;
    void network.then(() => {
      waitingNetwork = false;
      if (liveScreen && isActive) checkSession();
    });
  }

  // A conferência de rotina, com o acesso confirmado.
  $effect(() => {
    if (!session.authRequired || !isActive || !accessConfirmed) return;
    const id = setInterval(() => {
      if (network) return;
      if (Date.now() - lastCheck >= CHECK_SESSION_MS) checkSession();
    }, CHECK_TICK_MS);
    return () => {
      clearInterval(id);
    };
  });

  // A contagem até a próxima tentativa, com o acesso não confirmado. Um passo
  // por segundo: cada mudança de `faltam` refaz o efeito, que agenda o próximo.
  // Para durante uma tentativa e com o vídeo fora da vista.
  $effect(() => {
    if (!session.authRequired || !isActive || accessConfirmed || busy) return;
    const n = lacking;
    const id = setTimeout(() => {
      if (n > 1) lacking = n - 1;
      else retry();
    }, 1000);
    return () => {
      clearTimeout(id);
    };
  });

  // O aviso de sessão fica aberto só com o vídeo à vista. Fechado sem a tela
  // pedir, confere a sessão e reabre, com a mesma espera crescente das
  // câmeras.
  $effect(() => {
    if (!session.authRequired || !isActive) return;
    let ws: WebSocket | undefined;
    let timer: number | undefined;
    let failures = 0;
    let isLive = true;
    const openNow = () => {
      ws = new WebSocket(mediaURL.wsSessionNotice());
      ws.onopen = () => (failures = 0);
      ws.onclose = () => {
        if (!isLive) return;
        checkSession();
        timer = setTimeout(openNow, reconnectAt(failures++));
      };
    };
    openNow();
    return () => {
      isLive = false;
      clearTimeout(timer);
      ws?.close();
    };
  });

  // --- só decodifica o que está na tela ----------------------------------------

  // Com a grade mais alta que a janela (1× no celular), o tile rolado para fora
  // seguiria puxando e decodificando vídeo que ninguém vê. O player do go2rtc
  // tem isso embutido (`visibilityThreshold`), mas o observer dele nasce no
  // primeiro `connectedCallback`, que já rodou quando o `{@attach}` chega - a
  // opção passada ali não teria efeito. Por isso o observer é daqui.
  //
  // O corte espera a `CARENCIA_ROLAGEM_MS` e passa pelo `disconnectedCallback`
  // da instância, que traz a guarda do PiP e mais a carência própria do player
  // (`DISCONNECT_TIMEOUT`) - descontada aqui, para o total ser o parâmetro. A
  // aba oculta não passa por aqui e segue com os 5 s do player. Só o tile que
  // some por inteiro é cortado: tile cortado pela metade segue ligado.
  //
  // Um Set comum, e não WeakSet, porque o tamanho importa: é ele que decide
  // se o interruptor "Pausar fora da tela" aparece.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- quem a tela lê é o `quantasFora`
  const offScreen = new Set<VideoRTC>();
  const cut = new WeakMap<VideoRTC, number>();
  let quantasFora = $state(0);

  // Opt-in de quem prefere não esperar o keyframe ao rolar, e aceita pagar em
  // bateria. Do aparelho, como o layout: vai no localStorage, nunca na URL.
  let alwaysOn = $state(localStorage.getItem(ALWAYS_KEY) === '1');

  function scheduleCut(node: VideoRTC) {
    clearTimeout(cut.get(node));
    const wait = Math.max(0, SCROLL_GRACE_MS - node.DISCONNECT_TIMEOUT);
    cut.set(
      node,
      setTimeout(() => {
        node.disconnectedCallback();
      }, wait),
    );
  }

  const observer = new IntersectionObserver((entries) => {
    for (const { target: node, isIntersecting } of entries) {
      if (!(node instanceof VideoRTC)) continue; // só os players são observados
      clearTimeout(cut.get(node));
      if (isIntersecting) {
        offScreen.delete(node);
        node.connectedCallback();
      } else {
        offScreen.add(node);
        if (!alwaysOn) scheduleCut(node);
      }
    }
    quantasFora = offScreen.size;
  });

  // Ao voltar de uma aba oculta, o player religa chamando o
  // `connectedCallback` da instância - de todos os tiles, inclusive os fora da
  // tela, que o observer não revisita porque nada mudou para ele. A guarda é
  // aqui.
  function onlyOnScreen(node: VideoRTC) {
    const turnOn = node.connectedCallback.bind(node);
    node.connectedCallback = () => {
      if (alwaysOn || !offScreen.has(node)) turnOn();
    };
    observer.observe(node);
    return () => {
      clearTimeout(cut.get(node));
      observer.unobserve(node);
      offScreen.delete(node);
      quantasFora = offScreen.size;
    };
  }

  // Ligar religa na hora quem já foi cortado; desligar dá aos que estão fora
  // a mesma carência de quem acabou de sair da tela.
  function toggleAlways() {
    alwaysOn = !alwaysOn;
    localStorage.setItem(ALWAYS_KEY, alwaysOn ? '1' : '0');
    for (const node of offScreen) {
      if (alwaysOn) {
        clearTimeout(cut.get(node));
        node.connectedCallback();
      } else {
        scheduleCut(node);
      }
    }
  }

  // Estes dois são os ÚNICOS que gravam no localStorage, e é de propósito: o
  // que vai para lá é o que o usuário escolheu clicando, nunca o que veio de um
  // link. É o que impede o `#live?view=3` de alguém de virar o seu padrão.
  //
  // Reatribui em vez de mutar: runes não observam mudança dentro de um Set.
  function setSelection(next: Set<string>) {
    selected = next;
    localStorage.setItem(STORAGE_KEY, JSON.stringify([...next]));
  }

  function toggle(id: string) {
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- vira o `selected` inteiro, sem mutar o antigo
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setSelection(next);
  }

  function all() {
    setSelection(allChecked ? new Set<string>() : new Set(cameras.list.map((c) => c.id)));
  }

  function setMode(m: Mode) {
    mode = m;
    localStorage.setItem(LAYOUT_KEY, String(m));
  }

  // Maior tile 16:9 que cabe: para cada número de colunas, a largura do tile é
  // limitada ou pela largura disponível, ou pela altura das linhas que sobram.
  // Como a altura entra na conta, a grade escolhida nunca transborda.
  function bestFit(n: number, W: number, H: number) {
    let best = { cols: 1, w: 0 };
    for (let cols = 1; cols <= n; cols++) {
      const rows = Math.ceil(n / cols);
      const w = Math.min((W - GAP * (cols - 1)) / cols, ((H - GAP * (rows - 1)) / rows) * RATIO);
      if (w > best.w) best = { cols, w };
    }
    return best;
  }

  function gauge() {
    // Na tela cheia quem dá a medida do palco é ela; a grade se mede de novo
    // na volta.
    if (!stage || inFullscreen) return;
    if (mode !== 'fit' || !visible.length) {
      stage.style.height = '';
      // Medida de logo abaixo da barra, não do palco: a lista de câmeras e o
      // aviso de acesso empurram o palco para baixo, e abri-los não pode
      // encolher as câmeras. A rolagem entra de volta porque a medida é do
      // topo da página, não do ponto onde o usuário está. O piso evita tile
      // minúsculo em janela muito baixa, como o celular deitado.
      if (bar) {
        const free = freeHeight(bar, 0) - window.scrollY - bar.offsetHeight - PAGE_GAP;
        columnsHeight = Math.max(MIN_COLUMNS_HEIGHT, free);
      }
      return;
    }

    // O palco vai do seu topo até o fim da janela.
    const H = freeHeight(stage);

    stage.style.height = `${H}px`;
    fitting = bestFit(visible.length, stage.clientWidth, H);
  }

  // Nas colunas a largura vai até onde o tile, em 16:9, ainda cabe na altura
  // da janela: em tela larga, como a ultrawide, a grade usa a largura toda, e
  // no 1× o tile não fica mais alto que a tela. Na tela cheia quem manda é ela.
  const columnsWidth = $derived(
    mode === 'fit' || inFullscreen || columnsHeight <= 0
      ? null
      : `${Math.floor(mode * columnsHeight * RATIO + GAP * (mode - 1))}px`,
  );

  $effect(() => {
    // Leituras explícitas porque `medir` sai cedo antes de tocar nelas: são o
    // que muda a altura disponível ou a quantidade de tiles.
    /* eslint-disable @typescript-eslint/no-unused-expressions -- só a leitura, para o $effect rastrear */
    mode;
    visible.length;
    showPicker;
    inFullscreen;
    // O interruptor entra na barra e pode quebrar uma linha nova.
    quantasFora;
    // A faixa de versão nova desce o palco, e nenhum resize avisa.
    newScreen.available;
    /* eslint-enable @typescript-eslint/no-unused-expressions */
    gauge();
  });
</script>

<svelte:window
  onresize={gauge}
  onorientationchange={gauge}
  onscroll={closeMenu}
  onpointerdown={outsideMenu}
  onkeydown={keyHandler}
/>

<div class="page">
  <div class="bar" bind:this={bar}>
    <CamerasButton
      checkedList={selected.size}
      total={cameras.list.length}
      all={allChecked}
      isOpen={showPicker}
      onclick={() => (showPicker = !showPicker)}
      --pad-x="10px"
    />

    <div class="controls">
      <div class="row modes" role="group" aria-label="layout">
        {#each MODES as m (m)}
          <button
            class="ghost"
            class:on={mode === m}
            aria-pressed={mode === m}
            title={m === 'fit' ? 'encaixar todas na tela' : `${m} coluna${m > 1 ? 's' : ''}`}
            aria-label={m === 'fit' ? 'encaixar todas na tela' : undefined}
            onclick={() => setMode(m)}
          >
            {#if m === 'fit'}
              <Icon name="fitToScreen" size={17} />
              <span class="button-text">Encaixar</span>
            {:else}
              {m}×
            {/if}
          </button>
        {/each}
      </div>

      <!-- Só aparece quando há o que pausar: com tudo à vista - o encaixar,
           ou poucas câmeras - não há tile fora da tela para cortar. -->
      {#if quantasFora > 0}
        <span class="sep"></span>
        <button
          class="ghost pause"
          class:on={!alwaysOn}
          aria-pressed={!alwaysOn}
          aria-label="pausar câmeras fora da tela"
          title={alwaysOn
            ? 'câmeras fora da tela seguem ligadas (gasta mais bateria)'
            : `câmeras fora da tela pausam após ${SCROLL_GRACE_MS / 1000} s`}
          onclick={toggleAlways}
        >
          <Icon name={alwaysOn ? 'offScreenPlay' : 'offScreenPause'} size={20} />
          <span class="track"></span>
          <span class="button-text">Pausar fora da tela</span>
        </button>
      {/if}
    </div>
  </div>

  {#if showPicker}
    <div class="card">
      <CameraPicker
        cameras={cameras.list}
        checked={(id: string) => selected.has(id)}
        all={allChecked}
        ontoggle={toggle}
        onall={all}
        error={cameras.error}
      />
    </div>
  {/if}

  <!-- Para o leitor de tela, só as mudanças de estado: o número da contagem
       fica fora, senão ele seria anunciado a cada segundo. Sempre montada e
       vazia até a primeira falha: o leitor anuncia mudança dentro de uma
       região que já existe, e não o texto de uma que acabou de nascer. -->
  <span class="read-only" role="status">
    {accessConfirmed
      ? accessRestored
        ? 'Acesso ao dwnvr confirmado.'
        : ''
      : busy
        ? 'Conferindo o acesso ao dwnvr.'
        : `Não deu para confirmar o acesso ao dwnvr. Nova tentativa em ${RETRY_WAIT_S} segundos.`}
  </span>

  {#if !accessConfirmed}
    <div class="card small no-access">
      <p>Não deu para confirmar o seu acesso com o dwnvr, e o vídeo fica parado até confirmar.</p>
      <div class="attempt">
        <span>{busy ? 'Conferindo…' : `Nova tentativa em ${lacking} s`}</span>
        <button type="button" disabled={busy} onclick={retry}> Tentar agora </button>
      </div>
    </div>
  {/if}

  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="stage"
    class:fit={mode === 'fit'}
    class:fs={inFullscreen}
    class:escondidos={inFullscreen && !controls.visibleOnes}
    style:--troca="{SWITCH_MS}ms"
    bind:this={stage}
    {@attach (node: HTMLElement) => () => {
      // A de verdade o navegador desfaz sozinho quando o elemento sai da
      // página; a simulada ficaria cobrindo a tela seguinte, como ao abrir as
      // gravações pelo ⋮.
      if (fullscreen.el === node) leaveFullscreen();
    }}
    onpointerdowncapture={(e) => {
      if (!inFullscreen) return;
      touch.cancel();
      touchStart(e);
      controls.touched();
    }}
    onpointerdown={gestureStart}
    onpointermove={gestureMove}
    onpointerup={(e) => {
      touchEnd(e);
      gestureEnd(e);
    }}
    onpointercancel={gestureEnd}
  >
    <div
      class="grid"
      class:fit={mode === 'fit'}
      class:animating
      style:--cols={mode === 'fit' ? fitting.cols : mode}
      style:--tile-w="{Math.floor(fitting.w)}px"
      style:max-width={columnsWidth}
      style:transform={inFullscreen && drag ? `translateX(${drag}px)` : null}
    >
      {#each accessConfirmed ? visible : [] as c, i (c.id)}
        {@const p = inFullscreen ? role(i) : null}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="tile"
          class:ant={p === 'ant'}
          class:next={p === 'prox'}
          class:fora={p === 'fora'}
          ondblclick={() => toggleFull(c.id)}
          title={inFullscreen ? undefined : 'duplo clique: tela cheia'}
        >
          <!-- O componente do go2rtc negocia WebRTC/MSE sozinho. A mídia vai
               direto do navegador ao go2rtc; o dwnvr só faz proxy da
               sinalização, para que a credencial não chegue ao navegador. -->
          <video-stream
            {@attach (node: VideoRTC) => {
              node.mode = 'webrtc,mse';
              // Antes do `src`, que já abre a primeira conexão.
              reconnectFast(node);
              node.src = mediaURL.liveWS(c.id);
              hideNativeControls(node);
              if (PIP) keepInPip(node, c.id);
              const letGo = onlyOnScreen(node);
              players.set(c.id, node);
              // Ao desmontar, o custom element fecha a conexão sozinho no
              // disconnectedCallback - nada a limpar aqui além da referência
              // e do observer. Quando a sessão cai, quem fecha na hora é o
              // `fecharTudo`.
              return () => {
                letGo();
                players.delete(c.id);
              };
            }}
          ></video-stream>
          <!-- O duplo clique do tile é a tela cheia: dois toques rápidos no
               botão do PiP não podem virar isso. -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="label" ondblclick={(e) => e.stopPropagation()}>
            <span class="name"
              >{c.name}{#if inFullscreen}<span class="pos">{i + 1}/{visible.length}</span
                >{/if}</span
            >
            {#if PIP}
              <button
                class="pip"
                class:on={pipCam === c.id}
                onclick={() => togglePip(c.id)}
                aria-label="{pipCam === c.id ? 'fechar' : 'abrir'} {c.name} em janela flutuante"
                aria-pressed={pipCam === c.id}
                title={pipCam === c.id ? 'fechar a janela flutuante' : 'abrir em janela flutuante'}
              >
                <Icon name="pip" size={16} />
              </button>
            {/if}
          </div>

          {#if notices[c.id]}<span class="badge">{notices[c.id]}</span>{/if}

          <!-- Na tela cheia este canto é do botão de sair, que é do palco. Dois
               cliques rápidos nele não podem chegar ao duplo clique do tile:
               seriam três trocas em vez de duas. -->
          {#if !inFullscreen}
            <button
              class="full"
              onclick={() => {
                toggleFull(c.id);
              }}
              ondblclick={(e) => {
                e.stopPropagation();
              }}
              aria-label="{c.name} em tela cheia"
              title="tela cheia (ou duplo clique no vídeo)"
            >
              <Icon name="fullscreen" size={16} />
            </button>
          {/if}

          <!-- O duplo clique do tile é a tela cheia: dois toques rápidos no ⋮
               ou no menu não podem virar isso. -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="options" ondblclick={(e) => e.stopPropagation()}>
            <button
              class="mais"
              onclick={() => (menu = menu === c.id ? null : c.id)}
              aria-label="opções da câmera {c.name}"
              aria-haspopup="menu"
              aria-expanded={menu === c.id}
              ><span aria-hidden="true"><Icon name="menu" size="100%" /></span></button
            >

            {#if menu === c.id}
              <!-- Links de verdade, e não botões que trocam o hash: assim o
                   clique do meio e o "abrir em nova aba" também funcionam. -->
              <div class="menu" role="menu" {@attach positionMenu}>
                <a role="menuitem" href="#rec?cam={encodeURIComponent(c.id)}">Ver gravações</a>
                {#if cameras.detector}
                  <a role="menuitem" href="#detection?cams={encodeURIComponent(c.id)}">
                    Ver detecções
                  </a>
                {/if}
                <button class="ghost" role="menuitem" onclick={() => capture(c.id)}>
                  Baixar imagem agora
                </button>
                <button class="ghost" role="menuitem" onclick={() => remove(c.id)}>
                  Remover da visualização
                </button>
              </div>
            {/if}
          </div>
        </div>
      {/each}
    </div>

    <!-- Fora do carrossel: ficam paradas enquanto as câmeras deslizam. O nome,
         o PiP e o ⋮ são os do próprio tile, e vão junto com a imagem. -->
    {#if inFullscreen}
      {#if visible.length > 1}
        <button
          class="ghost arrow esq"
          onclick={() => {
            ir(-1);
          }}
          aria-label="câmera anterior"
          title="câmera anterior (←)"
        >
          <Icon name="chevronEsq" size={22} />
        </button>
        <button
          class="ghost arrow dir"
          onclick={() => {
            ir(1);
          }}
          aria-label="próxima câmera"
          title="próxima câmera (→)"
        >
          <Icon name="chevronDir" size={22} />
        </button>
      {/if}
      <span class="leave"><FullscreenButton active overVideo onclick={leaveFullscreen} /></span>
    {/if}
  </div>

  {#if !visible.length && !showPicker}
    {#if cameras.list.length}
      <p class="empty">Selecione as câmeras para visualizar.</p>
    {:else if cameras.error}
      <!-- A lista vazia porque a carga falhou não é "nenhuma câmera". -->
      <p class="empty">Não deu para carregar as câmeras: {cameras.error}</p>
    {:else if !cameras.loading}
      <!-- Sem a guarda do `loading`, o "nenhuma câmera" piscava a cada abertura
           da tela, no intervalo em que a lista ainda estava sendo buscada. -->
      <NoCameras />
    {/if}
  {/if}
</div>

<style>
  .no-access {
    color: var(--warn);
    display: grid;
    gap: 10px;
  }
  .no-access p {
    margin: 0;
  }
  .attempt {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  /* O número muda a cada segundo: com largura fixa, o texto não treme. */
  .attempt span {
    font-variant-numeric: tabular-nums;
  }
  /* Fora da tela, mas lido pelo leitor de tela. */
  .read-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }

  .page {
    display: grid;
    gap: 10px;
    padding: 10px;
  }

  /* Sem teto de largura na página: com ele só no modo encaixar, a barra
     pulava de lugar ao trocar de modo em janela larga. Quem limita o tile é a
     altura da janela - no encaixar pela conta do `melhorEncaixe`, nas colunas
     pelo `max-width` da grade. */

  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    gap: 10px;
  }
  .controls {
    display: contents;
  }

  /* O segmentado: encaixar e as colunas coladas num bloco só. */
  .modes {
    gap: 0;
  }
  .modes button {
    padding: 8px 12px;
    min-width: 44px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    border-radius: 0;
    margin-left: -1px;
    color: var(--dim);
  }
  .modes button:first-child {
    margin-left: 0;
    border-radius: 8px 0 0 8px;
  }
  .modes button:last-child {
    border-radius: 0 8px 8px 0;
  }
  .modes button.on {
    color: var(--fg);
    border-color: var(--accent);
    background: var(--ativo-fundo);
    position: relative;
    z-index: 1;
  }

  .sep {
    display: none;
    width: 1px;
    align-self: stretch;
    background: var(--line);
    margin: 4px 2px;
  }

  /* O interruptor: ligado é "pausa lá fora", o padrão. */
  .pause {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
  }
  .pause.on {
    border-color: var(--accent);
  }
  .track {
    width: 30px;
    height: 17px;
    border-radius: 9px;
    background: var(--line);
    position: relative;
    flex: none;
  }
  .track::after {
    content: '';
    position: absolute;
    top: 2px;
    left: 2px;
    width: 13px;
    height: 13px;
    border-radius: 50%;
    background: var(--fg);
    transition: left 0.12s;
  }
  .pause.on .track {
    background: var(--accent);
  }
  .pause.on .track::after {
    left: 15px;
  }

  /* Os textos dos botões só aparecem onde cabem. */
  .button-text {
    display: none;
  }

  @media (min-width: 720px) {
    .controls {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 10px;
    }
    .sep {
      display: block;
    }
    .button-text {
      display: inline;
    }
  }

  /* Recorta a grade no modo encaixar: a altura em pixels vem do `medir`, e sem
     isto um arredondamento para cima devolveria a rolagem que o modo evita. */
  .stage.fit {
    overflow: hidden;
  }

  .grid {
    display: grid;
    gap: 8px;
    grid-template-columns: repeat(var(--cols), minmax(0, 1fr));
    width: 100%;
    margin: 0 auto;
  }

  /* No encaixar a largura do tile é calculada, não distribuída entre as
     colunas: é ela que impede a grade de passar da altura da janela. */
  .grid.fit {
    grid-template-columns: repeat(var(--cols), var(--tile-w));
    justify-content: center;
    align-content: center;
    height: 100%;
  }

  .tile {
    position: relative;
    background: #000;
    border-radius: var(--radius);
    overflow: hidden;
    aspect-ratio: 16 / 9;
  }

  .tile :global(video-stream),
  .tile :global(video) {
    width: 100%;
    height: 100%;
    display: block;
    object-fit: contain;
  }

  /* Nome e botão do PiP juntos no canto de baixo à esquerda. */
  .label {
    position: absolute;
    left: 8px;
    bottom: 8px;
    display: flex;
    align-items: center;
    gap: 4px;
    /* Até o botão de tela cheia, à direita. */
    max-width: calc(100% - 50px);
  }

  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    background: rgba(0, 0, 0, 0.6);
    border-radius: 6px;
    padding: 2px 8px;
    font-size: 12px;
    pointer-events: none;
  }

  /* Mesma altura e mesmo fundo do ⋮, para os controles do tile serem da mesma
     família. */
  .pip,
  .full {
    display: flex;
    align-items: center;
    justify-content: center;
    flex: none;
    min-height: 0;
    width: 26px;
    height: 20px;
    padding: 0;
    border: 0;
    border-radius: 6px;
    background: rgba(13, 17, 23, 0.8);
    color: var(--fg);
  }

  .pip:focus-visible,
  .full:focus-visible {
    outline: 2px solid var(--accent);
  }
  .pip.on {
    color: var(--accent);
    outline: 2px solid var(--accent);
  }

  /* Como o ⋮: com mouse aparecem só sobre o tile, e o PiP fica enquanto a
     câmera está nele; no toque ficam sempre à vista. */
  @media (hover: hover) {
    .pip,
    .full {
      opacity: 0;
    }
    .tile:hover .pip,
    .tile:hover .full,
    .pip:focus-visible,
    .full:focus-visible,
    .pip.on {
      opacity: 1;
    }
    .tile:hover .pip,
    .tile:hover .full {
      background: rgba(13, 17, 23, 0.95);
    }
  }

  /* Embaixo à direita, na linha do nome e do PiP. */
  .full {
    position: absolute;
    right: 8px;
    bottom: 8px;
  }

  /* O player do go2rtc escreve o modo (loading, RTC, MSE) no canto de cima à
     direita, que é o lugar do ⋮: o texto anda para a esquerda dele, sem tocar
     no arquivo copiado. */
  .tile :global(video-stream .info) {
    padding-right: 44px;
  }

  /* Embaixo à direita, acima do botão de tela cheia: em cima à esquerda o
     player do go2rtc escreve os erros dele, e embaixo à esquerda está o nome. */
  .badge {
    position: absolute;
    right: 8px;
    bottom: 32px;
    max-width: calc(100% - 120px);
    background: rgba(0, 0, 0, 0.7);
    border: 1px solid #5c2b2b;
    border-radius: 6px;
    padding: 3px 8px;
    font-size: 12px;
    color: var(--bad);
    pointer-events: none;
  }

  /* --- o ⋮: mesma medida e mesmo desenho do das miniaturas das Detecções --- */

  /* Sem caixa própria: o ⋮ se posiciona contra o tile. O div existe só para
     segurar o duplo clique. */
  .options {
    display: contents;
  }

  .mais {
    position: absolute;
    top: 0;
    right: 0;
    display: flex;
    justify-content: flex-end;
    align-items: flex-start;
    min-height: 0;
    width: 40px;
    height: 36px;
    padding: 4px 4px 0 0;
    border: 0;
    background: none;
  }

  .mais span {
    display: block;
    width: 22px;
    height: 20px;
    padding: 2px;
    border-radius: 6px;
    background: rgba(13, 17, 23, 0.8);
    color: var(--fg);
  }

  .mais:focus-visible {
    outline: none;
  }
  .mais:focus-visible span,
  .mais[aria-expanded='true'] span {
    outline: 2px solid var(--accent);
  }

  /* Com mouse o ⋮ aparece só sobre o tile, e fica enquanto o menu dele está
     aberto. No toque não há hover: ele fica sempre à vista. */
  @media (hover: hover) {
    .mais {
      opacity: 0;
    }
    .tile:hover .mais,
    .mais:focus-visible,
    .mais[aria-expanded='true'] {
      opacity: 1;
    }
    .tile:hover .mais span {
      background: rgba(13, 17, 23, 0.95);
    }
  }

  /* `top`, `right` e `max-height` vêm do `posicionaMenu`. Acima do cabeçalho e
     da navegação inferior, que são 20. */
  .menu {
    position: fixed;
    z-index: 30;
    overflow-y: auto;
    padding: 4px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  }

  .menu a,
  .menu button {
    display: flex;
    align-items: center;
    width: 100%;
    min-height: 40px;
    padding: 0 12px;
    border: 0;
    border-radius: 6px;
    color: var(--fg);
    font-size: 14px;
    text-align: left;
    text-decoration: none;
    white-space: nowrap;
  }

  @media (hover: hover) {
    .menu a:hover,
    .menu button:hover {
      background: var(--panel-2);
    }
  }

  /* --- a tela cheia --------------------------------------------------------- */

  /* Sem `position`: quem o põe é o navegador (`:fullscreen`) ou a regra da
     simulada no app.css, e ele é o apoio das setas e do botão de sair. O
     deslize é todo da tela: sem `touch-action` o navegador o tomaria. */
  .stage.fs {
    overflow: hidden;
    background: #000;
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
  }

  .stage.fs .grid {
    display: block;
    position: absolute;
    inset: 0;
    height: auto;
  }
  .grid.animating {
    transition: transform var(--troca) ease-out;
  }

  /* `left`, e não `transform`, para pôr as vizinhas dos lados: tile com
     transform vira a referência do `position: fixed`, e o menu do ⋮ abriria
     fora do lugar. */
  .stage.fs .tile {
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    aspect-ratio: auto;
    border-radius: 0;
  }
  .stage.fs .tile.ant {
    left: -100%;
  }
  .stage.fs .tile.next {
    left: 100%;
  }
  .stage.fs .tile.fora {
    display: none;
  }

  /* Os controles do tile, maiores: aqui é a tela inteira, e no celular o dedo. */
  .stage.fs .label {
    left: max(10px, env(safe-area-inset-left));
    bottom: max(10px, env(safe-area-inset-bottom));
  }
  .stage.fs .name {
    padding: 4px 10px;
    font-size: 14px;
  }
  .pos {
    margin-left: 6px;
    color: var(--dim);
    font-variant-numeric: tabular-nums;
  }
  .stage.fs .pip {
    width: 34px;
    height: 28px;
  }
  .stage.fs .pip :global(svg) {
    width: 18px;
    height: 18px;
  }
  .stage.fs .mais {
    width: 52px;
    height: 48px;
    padding: max(8px, env(safe-area-inset-top)) max(8px, env(safe-area-inset-right)) 0 0;
  }
  .stage.fs .mais span {
    width: 36px;
    height: 32px;
    padding: 6px;
  }
  /* Embaixo à direita está o botão de sair. */
  .stage.fs .badge {
    right: max(10px, env(safe-area-inset-right));
    bottom: calc(max(10px, env(safe-area-inset-bottom)) + 54px);
  }

  /* Mesmo fundo do botão de sair (`sobreVideo`): a imagem pode ser clara. */
  .arrow {
    position: absolute;
    top: 50%;
    z-index: 1;
    display: grid;
    place-items: center;
    width: 44px;
    height: 56px;
    padding: 0;
    transform: translateY(-50%);
    background: rgba(0, 0, 0, 0.55);
    border-color: rgba(255, 255, 255, 0.18);
  }
  .arrow.esq {
    left: max(8px, env(safe-area-inset-left));
  }
  .arrow.dir {
    right: max(8px, env(safe-area-inset-right));
  }

  .leave {
    position: absolute;
    z-index: 1;
    right: max(10px, env(safe-area-inset-right));
    bottom: max(10px, env(safe-area-inset-bottom));
  }

  /* O nome e a posição ficam; o resto some. No fim do bloco para ganhar do
     `.tile:hover`, que tem o mesmo peso. */
  .stage.fs .arrow,
  .stage.fs .leave,
  .stage.fs .mais,
  .stage.fs .pip {
    transition: opacity 0.25s;
  }
  .stage.escondidos .arrow,
  .stage.escondidos .leave,
  .stage.escondidos .mais,
  .stage.escondidos .pip {
    opacity: 0;
    pointer-events: none;
  }
  .stage.escondidos {
    cursor: none;
  }
</style>
