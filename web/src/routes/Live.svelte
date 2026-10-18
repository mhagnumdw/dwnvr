<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import '../vendor/video-stream.js';
  import { VideoRTC } from '../vendor/video-rtc.js';
  import { cameras, loadCameras, session, sessaoCaiu } from '../lib/state.svelte.ts';
  import { paramsAtuais, escrever, cena } from '../lib/rota.svelte.ts';
  import { api, mediaURL, mensagemDe } from '../lib/api.ts';
  import type { Sessao } from '../lib/api.ts';
  import { baixarQuadro } from '../lib/captura.ts';
  import { dayKey, hhmmss } from '../lib/format.ts';
  import SemCameras from '../components/SemCameras.svelte';
  import BotaoCameras from '../components/BotaoCameras.svelte';
  import SeletorCameras from '../components/SeletorCameras.svelte';
  import BotaoTelaCheia from '../components/BotaoTelaCheia.svelte';
  import { telaCheia, entrarEmTelaCheia, sairDaTelaCheia } from '../lib/telaCheia.svelte.ts';
  import { controlesQueSomem } from '../lib/controlesQueSomem.svelte.ts';
  import { telaNova } from '../lib/versao-tela.svelte.ts';
  import Icone from '../components/Icone.svelte';

  const STORAGE_KEY = 'dwnvr.live.selection';
  const LAYOUT_KEY = 'dwnvr.live.layout';
  const SEMPRE_KEY = 'dwnvr.live.sempreLigadas';

  // Quantas colunas, ou encaixar tudo na tela.
  type Modo = 'fit' | number;

  const COLUNAS = [1, 2, 3];
  // 'fit' é só mais um modo ao lado das colunas: como um exclui o outro por
  // construção, escolher 2× desliga o encaixar sem nenhum código para isso.
  const MODOS: Modo[] = ['fit', ...COLUNAS];

  // Precisa bater com o `gap` da grade no CSS: é o espaço que a conta do
  // encaixe desconta antes de dividir o que sobra entre os tiles.
  const GAP = 8;
  const PROPORCAO = 16 / 9;

  // Até quantas câmeras o texto do compartilhar diz pelo nome.
  const NOMES_NO_TEXTO = 3;

  // Quanto tempo um tile rolado para fora da tela segue ligado antes do corte.
  // Longa de propósito: voltar para onde se estava há pouco não pode custar a
  // espera por um keyframe. Ver `soNaTela`.
  const CARENCIA_ROLAGEM_MS = 30_000;

  // Intervalo mínimo entre uma tentativa de conexão e a seguinte, crescendo a
  // cada falha seguida e voltando ao primeiro quando o vídeo toca. O player do
  // go2rtc usa 15 s fixos. Ver `religarRapido`.
  const RELIGAR_MS = [500, 1000, 2000, 5000];
  const RELIGAR_MAX_MS = 5000;
  // A espera depois de `n` falhas seguidas: a última se repete.
  const religarEm = (n: number) => RELIGAR_MS[Math.min(n, RELIGAR_MS.length - 1)] ?? RELIGAR_MAX_MS;

  // Quanto cada sondagem do servidor espera antes de desistir, ao voltar de
  // uma aba oculta. Ver `esperarRede`.
  const SONDA_MS = 2000;

  // De quanto em quanto tempo a tela confere a sessão com o vídeo à vista, e
  // quanto cada conferência espera a resposta. Ver `conferirSessao`.
  const CONFERIR_SESSAO_MS = 60_000;
  const PRAZO_CONFERENCIA_MS = 10_000;
  // O tique que decide se está na hora de conferir, e o intervalo entre as
  // tentativas enquanto a sessão não se confirma.
  const TIQUE_CONFERENCIA_MS = 5000;

  // Na tela cheia: quanto dura a câmera entrando de lado (e a que volta para
  // o lugar, num deslize curto demais).
  const TROCA_MS = 220;
  // O deslize troca de câmera ao passar de DESLIZE_PX, ou de DESLIZE_FRACAO
  // da largura da tela se ela for estreita. Antes de EIXO_PX o dedo ainda não
  // disse se vai para o lado ou para cima.
  const DESLIZE_PX = 80;
  const DESLIZE_FRACAO = 0.2;
  const EIXO_PX = 8;

  // Lida uma vez, na inicialização: daqui em diante quem manda é o estado da
  // tela, que escreve de volta na URL.
  const params = paramsAtuais();

  let selected = $state(new Set<string>());
  let modo = $state<Modo>(lerModo());
  let showPicker = $state(false);
  let palco = $state<HTMLDivElement | null>(null);
  let encaixe = $state({ cols: 1, w: 0 });
  // Segura a escrita na URL até a seleção ter sido lida - ver o efeito no fim
  // do bloco.
  let montado = $state(false);

  const visible = $derived(cameras.list.filter((c) => selected.has(c.id)));
  const tudoMarcado = $derived(cameras.list.length > 0 && selected.size === cameras.list.length);

  // A URL vem primeiro, o localStorage depois, e só então o palpite pela
  // largura da tela. A URL descreve ESTE link; o localStorage, o hábito deste
  // navegador. Ler dali nunca grava aqui - senão abrir o link de outra pessoa
  // viraria a preferência de quem abriu.
  //
  // Tudo isso na inicialização, e não no onMount: começar sempre em 2× e
  // corrigir depois faria a grade piscar no celular, onde o padrão é 1×.
  function lerModo(): Modo {
    const daURL = params.get('view');
    if (daURL === 'fit') return 'fit';
    if (COLUNAS.includes(Number(daURL))) return Number(daURL);
    return lerLayout();
  }

  function lerLayout(): Modo {
    const salvo = localStorage.getItem(LAYOUT_KEY);
    if (salvo === 'fit') return 'fit';
    if (COLUNAS.includes(Number(salvo))) return Number(salvo);
    return matchMedia('(min-width: 640px)').matches ? 2 : 1;
  }

  onMount(async () => {
    if (!cameras.list.length) await loadCameras();

    // `cams=` vazio é uma escolha - "nenhuma câmera" -, e a chave ausente é
    // falta de opinião: aí vale o que este navegador usou por último.
    const daURL = params.has('cams') ? params.getAll('cams').filter(Boolean) : null;
    const ids = daURL ?? selecaoSalva();
    if (ids) {
      // Só mantém câmeras que ainda existem: uma seleção salva - ou um link
      // antigo - pode citar uma câmera removida do cadastro desde então.
      const querem = new Set(ids);
      selected = new Set(cameras.list.filter((c) => querem.has(c.id)).map((c) => c.id));
    }
    if (!selected.size && !daURL) {
      // Duas por padrão: abrir nove streams 1080p de uma vez trava celular.
      selected = new Set(cameras.list.slice(0, 2).map((c) => c.id));
    }
    montado = true;
  });

  // A seleção que este navegador usou por último. Conteúdo estragado vale como
  // nenhum.
  function selecaoSalva(): string[] | null {
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
    if (!montado) return;
    escrever({
      // Na ordem do cadastro, e não na de inserção do Set: desmarcar e marcar
      // de novo reescreveria a URL sem nada ter mudado na tela.
      cams: cameras.list.filter((c) => selected.has(c.id)).map((c) => c.id),
      // Sempre escrito, mesmo no padrão: o padrão daqui depende da largura da
      // tela, então omiti-lo faria o mesmo link abrir diferente no celular.
      view: modo,
    });
  });

  // O que o compartilhar diz em palavras: as câmeras marcadas, pelo nome até
  // NOMES_NO_TEXTO; acima disso só a conta, que a lista não caberia num assunto.
  $effect(() => {
    if (!montado) return;
    const nomes = cameras.list.filter((c) => selected.has(c.id)).map((c) => c.name);
    if (!nomes.length) cena.texto = '';
    else if (nomes.length <= NOMES_NO_TEXTO) cena.texto = `Ao vivo: ${nomes.join(', ')}`;
    else cena.texto = `Ao vivo: ${nomes.length} câmeras`;
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
  const emTelaCheia = $derived(!!palco && telaCheia.el === palco);
  // A câmera na tela, pelo id: tirar outra da seleção não a troca de lugar.
  let atual = $state<string | null>(null);
  const idxAtual = $derived(visible.findIndex((c) => c.id === atual));
  // Com duas câmeras a anterior e a próxima são a mesma: ela espera do lado
  // de onde vai entrar.
  let lado = $state<1 | -1>(1);
  // Quanto o carrossel está deslocado, em px, e se o deslocamento anima.
  let arrasto = $state(0);
  let animando = $state(false);

  // O nome e a posição ficam; setas, ⋮, PiP e o botão de sair somem. Não
  // somem com o menu aberto nem com o mouse em cima de um deles.
  let sobreControles = false;
  const controles = controlesQueSomem(() => emTelaCheia && !menu && !sobreControles);

  type Papel = 'atual' | 'ant' | 'prox' | 'fora';

  function papel(i: number): Papel {
    const n = visible.length;
    if (i === idxAtual) return 'atual';
    if (n === 2) return lado === 1 ? 'prox' : 'ant';
    if (i === (idxAtual + 1) % n) return 'prox';
    if (i === (idxAtual - 1 + n) % n) return 'ant';
    return 'fora';
  }

  function alternarCheia(cam: string) {
    if (!palco) return;
    if (emTelaCheia) {
      // Dois deslizes seguidos rápidos também fazem um duplo clique.
      if (Date.now() - arrastouEm > 2 * TROCA_MS) sairDaTelaCheia();
      return;
    }
    atual = cam;
    menu = null;
    entrarEmTelaCheia(palco);
  }

  // A próxima (1) ou a anterior (-1), em roda: da última volta à primeira.
  function ir(d: 1 | -1) {
    const n = visible.length;
    if (animando || n < 2 || !palco) return;
    menu = null;
    lado = d;
    animando = true;
    arrasto = -d * palco.clientWidth;
    setTimeout(() => {
      // Tudo no mesmo tique: a câmera que entrou vira a atual no mesmo quadro
      // em que o carrossel volta ao zero, sem animar a volta.
      animando = false;
      arrasto = 0;
      atual = visible[(idxAtual + d + n) % n]?.id ?? atual;
    }, TROCA_MS);
  }

  // O deslize. O eixo se decide nos primeiros EIXO_PX: para cima ou para
  // baixo não é com a tela. Começar num botão ou no menu também não.
  let gesto: { id: number; x: number; y: number; eixo: 'x' | 'y' | null } | null = null;
  let arrastouEm = 0;

  function gestoComeca(e: PointerEvent) {
    if (!emTelaCheia || animando || visible.length < 2) return;
    if (e.target instanceof Element && e.target.closest('button, a, .menu')) return;
    gesto = { id: e.pointerId, x: e.clientX, y: e.clientY, eixo: null };
  }

  function gestoMove(e: PointerEvent) {
    if (emTelaCheia && e.pointerType === 'mouse') {
      sobreControles = e.target instanceof Element && !!e.target.closest('button, a, .menu');
      controles.mostrar();
    }
    if (gesto?.id !== e.pointerId || !palco) return;
    const dx = e.clientX - gesto.x;
    const dy = e.clientY - gesto.y;
    if (!gesto.eixo && Math.hypot(dx, dy) > EIXO_PX) {
      gesto.eixo = Math.abs(dx) > Math.abs(dy) ? 'x' : 'y';
      if (gesto.eixo === 'x') palco.setPointerCapture(e.pointerId);
    }
    if (gesto.eixo !== 'x') return;
    if (visible.length === 2) lado = dx < 0 ? 1 : -1;
    arrasto = dx;
  }

  function gestoAcaba(e: PointerEvent) {
    if (gesto?.id !== e.pointerId || !palco) return;
    const { eixo, x } = gesto;
    gesto = null;
    if (eixo !== 'x') return;
    arrastouEm = Date.now();
    const dx = e.clientX - x;
    const limite = Math.min(DESLIZE_PX, palco.clientWidth * DESLIZE_FRACAO);
    if (e.type === 'pointerup' && Math.abs(dx) > limite) {
      ir(dx < 0 ? 1 : -1);
      return;
    }
    // Curto demais: a câmera volta para o lugar.
    animando = true;
    arrasto = 0;
    setTimeout(() => (animando = false), TROCA_MS);
  }

  onMount(() => () => {
    controles.parar();
  });

  // Entrar e trocar de câmera mostram os controles e recomeçam a contagem.
  $effect(() => {
    if (!emTelaCheia) return;
    // eslint-disable-next-line @typescript-eslint/no-unused-expressions -- só a leitura, para o $effect rastrear
    atual;
    untrack(() => {
      controles.mostrar();
    });
  });

  // A câmera da tela sumiu da seleção por outro caminho, ou o acesso não se
  // confirmou e a grade se esvaziou: a tela cheia ficaria preta.
  $effect(() => {
    if (emTelaCheia && (idxAtual < 0 || !acessoConfirmado)) sairDaTelaCheia();
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
  const avisos = $state<Record<string, string>>({});
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- os timers, que a tela não lê
  const avisoTimer = new Map<string, number>();

  function avisar(cam: string, texto: string) {
    avisos[cam] = texto;
    clearTimeout(avisoTimer.get(cam));
    avisoTimer.set(
      cam,
      setTimeout(() => (avisos[cam] = ''), 5000),
    );
  }

  function foraDoMenu(e: MouseEvent) {
    if (menu && !(e.target instanceof Element && e.target.closest('.opcoes'))) menu = null;
  }

  function tecla(e: KeyboardEvent) {
    if (e.key === 'Escape') menu = null;
    if (emTelaCheia && (e.key === 'ArrowRight' || e.key === 'ArrowLeft')) {
      e.preventDefault();
      ir(e.key === 'ArrowRight' ? 1 : -1);
    }
  }

  // Entre o ⋮ e o menu, e entre o menu e a borda da tela.
  const MENU_FOLGA = 6;
  const MENU_BORDA = 8;

  // O tile corta o que passa da borda dele, e no celular, com duas colunas, ele
  // tem uns 180px de altura: o menu virava uma caixinha com rolagem. Por isso o
  // menu é `position: fixed`, que escapa do corte mesmo dentro do tile, posto
  // aqui junto do ⋮. Abre para baixo; se não couber e houver mais espaço
  // acima, para cima; e só rola se não couber em nenhum dos dois.
  function posicionaMenu(el: HTMLElement) {
    const marca = el.parentElement?.querySelector('.mais span');
    if (!marca) return;
    const r = marca.getBoundingClientRect();
    const altura = el.offsetHeight;
    const abaixo = window.innerHeight - r.bottom - MENU_FOLGA - MENU_BORDA;
    const acima = r.top - MENU_FOLGA - MENU_BORDA;
    const paraCima = altura > abaixo && acima > abaixo;
    const cabe = paraCima ? acima : abaixo;
    el.style.right = `${document.documentElement.clientWidth - r.right}px`;
    el.style.top = `${paraCima ? r.top - MENU_FOLGA - Math.min(altura, cabe) : r.bottom + MENU_FOLGA}px`;
    el.style.maxHeight = `${cabe}px`;
  }

  // Preso à tela e não ao tile, o menu ficaria para trás quando a página rola.
  function fechaMenu() {
    menu = null;
  }

  // O quadro é o que está na tela, e o nome leva a hora do clique: no live os
  // dois diferem só pela latência do stream. Mesma convenção das Gravações
  // (cam_AAAA-MM-DD_HH-MM-SS), para imagem de live e de gravação ficarem lado a
  // lado na pasta.
  async function capturar(cam: string) {
    menu = null;
    const d = new Date();
    const nome = `${cam}_${dayKey(d)}_${hhmmss(d.getTime()).replaceAll(':', '-')}.jpg`;
    try {
      await baixarQuadro(players.get(cam)?.video, nome);
    } catch (e) {
      avisar(cam, mensagemDe(e));
    }
  }

  // O mesmo que desmarcar a câmera na lista: grava a seleção e a URL acompanha.
  // Na tela cheia, a próxima entra no lugar; se era a única, a tela cheia
  // fecha.
  function remover(cam: string) {
    menu = null;
    if (emTelaCheia && cam === atual) {
      const n = visible.length;
      if (n < 2) sairDaTelaCheia();
      else atual = visible[(idxAtual + 1) % n]?.id ?? null;
    }
    toggle(cam);
  }

  // --- picture-in-picture ------------------------------------------------------

  // O Firefox não tem a API (tem só o botão dele, desenhado sobre o vídeo): lá
  // o botão nem aparece.
  const PIP = document.pictureInPictureEnabled;
  // A câmera que está na janela flutuante. O navegador deixa uma por vez.
  let pipCam = $state<string | null>(null);

  async function alternarPip(cam: string) {
    const video = players.get(cam)?.video;
    try {
      if (!video) throw new Error('sem vídeo');
      if (document.pictureInPictureElement === video) await document.exitPictureInPicture();
      else await video.requestPictureInPicture();
    } catch {
      // O motivo comum é o vídeo ainda não ter começado: sem a primeira imagem
      // o navegador recusa a janela.
      avisar(cam, 'o vídeo ainda não começou');
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
  function manterNoPip(node: VideoRTC, cam: string) {
    const desligar = node.disconnectedCallback.bind(node);
    node.disconnectedCallback = () => {
      if (node.isConnected && document.pictureInPictureElement === node.video) return;
      desligar();
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
        if (document.hidden || (!sempreLigadas && foraDaTela.has(node))) desligar();
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
  let rede: Promise<void> | null = null;

  function aoMudarVisibilidade() {
    visivel = !document.hidden;
    if (document.hidden || rede) return;
    rede = esperarRede().finally(() => (rede = null));
  }

  // Na fase de captura da `window`, para valer antes do listener que cada
  // player põe no `document` e que é quem religa as câmeras.
  onMount(() => {
    window.addEventListener('visibilitychange', aoMudarVisibilidade, true);
    return () => {
      window.removeEventListener('visibilitychange', aoMudarVisibilidade, true);
    };
  });

  // Qualquer resposta serve, até um erro HTTP: o que importa é o servidor ter
  // sido alcançado. Desiste se a aba voltar a ficar oculta.
  async function esperarRede() {
    for (let i = 0; !document.hidden; i++) {
      try {
        await api.ping(SONDA_MS);
        return;
      } catch {
        await new Promise((r) => setTimeout(r, religarEm(i)));
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
  function religarRapido(node: VideoRTC) {
    const conectar = node.onconnect.bind(node);
    node.onconnect = () => {
      if (document.hidden && document.pictureInPictureElement !== node.video) return false;
      if (!rede) return conectar();
      void rede.then(() => {
        if (!document.hidden && node.isConnected && (sempreLigadas || !foraDaTela.has(node)))
          conectar();
      });
      return false;
    };
    let falhas = 0;
    const fechou = node.onclose.bind(node);
    node.onclose = () => {
      node.RECONNECT_TIMEOUT = religarEm(falhas);
      const religando = fechou();
      if (religando) falhas++;
      // Caiu e não voltou: pode ser a sessão. O websocket recusado não diz o
      // motivo ao navegador, então quem responde é o /api/session.
      if (falhas >= 2) conferirSessao();
      return religando;
    };
    // O `playing` sai do <video>, que não o propaga: só a fase de captura o
    // pega aqui, como em `manterNoPip`.
    node.addEventListener('playing', () => (falhas = 0), true);
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
  // não respondeu - para o vídeo até confirmar: na dúvida, fecha.

  // Com o vídeo à vista: aba visível, ou uma câmera no PiP.
  let visivel = $state(!document.hidden);
  const ativo = $derived(visivel || pipCam !== null);

  let acessoConfirmado = $state(true);
  let ultimaConferencia = Date.now();
  let conferindo: Promise<void> | null = null;

  // fecharTudo corta o vídeo de todas as câmeras na hora. Desmontar o tile
  // também corta, mas só depois dos 5 s de carência do player, e o WebRTC
  // seguiria recebendo vídeo nesse meio-tempo.
  function fecharTudo() {
    for (const node of players.values()) node.ondisconnect();
  }

  function conferirSessao() {
    if (!session.authRequired) return;
    conferindo ??= (async () => {
      try {
        const r = await api.conferirSessao(PRAZO_CONFERENCIA_MS);
        const s = (await r.json()) as Sessao;
        if (!s.authenticated) {
          fecharTudo();
          sessaoCaiu();
        } else acessoConfirmado = true;
      } catch {
        fecharTudo();
        acessoConfirmado = false;
      } finally {
        ultimaConferencia = Date.now();
        conferindo = null;
      }
    })();
  }

  // Enquanto `rede` espera o servidor responder na volta da aba oculta, não
  // confere: a rede ainda suspensa daria um "não confirmou" falso, e o vídeo
  // pararia à toa.
  $effect(() => {
    if (!session.authRequired || !ativo) return;
    const id = setInterval(() => {
      if (rede) return;
      if (!acessoConfirmado || Date.now() - ultimaConferencia >= CONFERIR_SESSAO_MS)
        conferirSessao();
    }, TIQUE_CONFERENCIA_MS);
    return () => {
      clearInterval(id);
    };
  });

  // O aviso de sessão fica aberto só com o vídeo à vista. Fechado sem a tela
  // pedir, confere a sessão e reabre, com a mesma espera crescente das
  // câmeras.
  $effect(() => {
    if (!session.authRequired || !ativo) return;
    let ws: WebSocket | undefined;
    let timer: number | undefined;
    let falhas = 0;
    let vivo = true;
    const abrir = () => {
      ws = new WebSocket(mediaURL.avisoDeSessaoWS());
      ws.onopen = () => (falhas = 0);
      ws.onclose = () => {
        if (!vivo) return;
        conferirSessao();
        timer = setTimeout(abrir, religarEm(falhas++));
      };
    };
    abrir();
    return () => {
      vivo = false;
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
  const foraDaTela = new Set<VideoRTC>();
  const corte = new WeakMap<VideoRTC, number>();
  let quantasFora = $state(0);

  // Opt-in de quem prefere não esperar o keyframe ao rolar, e aceita pagar em
  // bateria. Do aparelho, como o layout: vai no localStorage, nunca na URL.
  let sempreLigadas = $state(localStorage.getItem(SEMPRE_KEY) === '1');

  function agendarCorte(node: VideoRTC) {
    clearTimeout(corte.get(node));
    const espera = Math.max(0, CARENCIA_ROLAGEM_MS - node.DISCONNECT_TIMEOUT);
    corte.set(
      node,
      setTimeout(() => {
        node.disconnectedCallback();
      }, espera),
    );
  }

  const observador = new IntersectionObserver((entradas) => {
    for (const { target: node, isIntersecting } of entradas) {
      if (!(node instanceof VideoRTC)) continue; // só os players são observados
      clearTimeout(corte.get(node));
      if (isIntersecting) {
        foraDaTela.delete(node);
        node.connectedCallback();
      } else {
        foraDaTela.add(node);
        if (!sempreLigadas) agendarCorte(node);
      }
    }
    quantasFora = foraDaTela.size;
  });

  // Ao voltar de uma aba oculta, o player religa chamando o
  // `connectedCallback` da instância - de todos os tiles, inclusive os fora da
  // tela, que o observer não revisita porque nada mudou para ele. A guarda é
  // aqui.
  function soNaTela(node: VideoRTC) {
    const ligar = node.connectedCallback.bind(node);
    node.connectedCallback = () => {
      if (sempreLigadas || !foraDaTela.has(node)) ligar();
    };
    observador.observe(node);
    return () => {
      clearTimeout(corte.get(node));
      observador.unobserve(node);
      foraDaTela.delete(node);
      quantasFora = foraDaTela.size;
    };
  }

  // Ligar religa na hora quem já foi cortado; desligar dá aos que estão fora
  // a mesma carência de quem acabou de sair da tela.
  function alternarSempre() {
    sempreLigadas = !sempreLigadas;
    localStorage.setItem(SEMPRE_KEY, sempreLigadas ? '1' : '0');
    for (const node of foraDaTela) {
      if (sempreLigadas) {
        clearTimeout(corte.get(node));
        node.connectedCallback();
      } else {
        agendarCorte(node);
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

  function todas() {
    setSelection(tudoMarcado ? new Set<string>() : new Set(cameras.list.map((c) => c.id)));
  }

  function setModo(m: Modo) {
    modo = m;
    localStorage.setItem(LAYOUT_KEY, String(m));
  }

  // Maior tile 16:9 que cabe: para cada número de colunas, a largura do tile é
  // limitada ou pela largura disponível, ou pela altura das linhas que sobram.
  // Como a altura entra na conta, a grade escolhida nunca transborda.
  function melhorEncaixe(n: number, W: number, H: number) {
    let melhor = { cols: 1, w: 0 };
    for (let cols = 1; cols <= n; cols++) {
      const linhas = Math.ceil(n / cols);
      const w = Math.min(
        (W - GAP * (cols - 1)) / cols,
        ((H - GAP * (linhas - 1)) / linhas) * PROPORCAO,
      );
      if (w > melhor.w) melhor = { cols, w };
    }
    return melhor;
  }

  function medir() {
    // Na tela cheia quem dá a medida do palco é ela; a grade se mede de novo
    // na volta.
    if (!palco || emTelaCheia) return;
    if (modo !== 'fit' || !visible.length) {
      palco.style.height = '';
      return;
    }

    // O palco vai do seu topo até o fim da janela, menos o que já está
    // reservado abaixo dele: o respiro da página e o espaço que o `main` guarda
    // para a navegação inferior não cobrir o conteúdo. Esse padding do `main` é
    // zero no desktop, onde a navegação sobe - lê-lo evita repetir aqui, em JS,
    // o breakpoint que já está no CSS.
    const abaixo = (el: Element | null) =>
      el ? parseFloat(getComputedStyle(el).paddingBottom) || 0 : 0;
    const topo = palco.getBoundingClientRect().top;
    const H = Math.max(
      80,
      window.innerHeight - topo - abaixo(palco.parentElement) - abaixo(palco.closest('main')),
    );

    palco.style.height = `${H}px`;
    encaixe = melhorEncaixe(visible.length, palco.clientWidth, H);
  }

  $effect(() => {
    // Leituras explícitas porque `medir` sai cedo antes de tocar nelas: são o
    // que muda a altura disponível ou a quantidade de tiles.
    /* eslint-disable @typescript-eslint/no-unused-expressions -- só a leitura, para o $effect rastrear */
    modo;
    visible.length;
    showPicker;
    emTelaCheia;
    // O interruptor entra na barra e pode quebrar uma linha nova.
    quantasFora;
    // A faixa de versão nova desce o palco, e nenhum resize avisa.
    telaNova.disponivel;
    /* eslint-enable @typescript-eslint/no-unused-expressions */
    medir();
  });
</script>

<svelte:window
  onresize={medir}
  onorientationchange={medir}
  onscroll={fechaMenu}
  onpointerdown={foraDoMenu}
  onkeydown={tecla}
/>

<div class="page" class:fit={modo === 'fit'}>
  <div class="barra">
    <BotaoCameras
      marcadas={selected.size}
      total={cameras.list.length}
      todas={tudoMarcado}
      aberto={showPicker}
      onclick={() => (showPicker = !showPicker)}
      --pad-x="10px"
    />

    <div class="controles">
      <div class="row modos" role="group" aria-label="layout">
        {#each MODOS as m (m)}
          <button
            class="ghost"
            class:on={modo === m}
            aria-pressed={modo === m}
            title={m === 'fit' ? 'encaixar todas na tela' : `${m} coluna${m > 1 ? 's' : ''}`}
            aria-label={m === 'fit' ? 'encaixar todas na tela' : undefined}
            onclick={() => setModo(m)}
          >
            {#if m === 'fit'}
              <Icone nome="encaixar" tamanho={17} />
              <span class="texto-botao">Encaixar</span>
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
          class="ghost pausar"
          class:on={!sempreLigadas}
          aria-pressed={!sempreLigadas}
          aria-label="pausar câmeras fora da tela"
          title={sempreLigadas
            ? 'câmeras fora da tela seguem ligadas (gasta mais bateria)'
            : `câmeras fora da tela pausam após ${CARENCIA_ROLAGEM_MS / 1000} s`}
          onclick={alternarSempre}
        >
          <Icone nome={sempreLigadas ? 'foraDaTelaToca' : 'foraDaTelaPausa'} tamanho={20} />
          <span class="trilho"></span>
          <span class="texto-botao">Pausar fora da tela</span>
        </button>
      {/if}
    </div>
  </div>

  {#if showPicker}
    <div class="card">
      <SeletorCameras
        cameras={cameras.list}
        marcada={(id: string) => selected.has(id)}
        todas={tudoMarcado}
        ontoggle={toggle}
        ontodas={todas}
      />
    </div>
  {/if}

  {#if !acessoConfirmado}
    <p class="card small semacesso">
      Não deu para confirmar o seu acesso com o dwnvr, e o vídeo fica parado até confirmar. Tentando
      de novo…
    </p>
  {/if}

  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="palco"
    class:fit={modo === 'fit'}
    class:fs={emTelaCheia}
    class:escondidos={emTelaCheia && !controles.visiveis}
    style:--troca="{TROCA_MS}ms"
    bind:this={palco}
    {@attach (node: HTMLElement) => () => {
      // A de verdade o navegador desfaz sozinho quando o elemento sai da
      // página; a simulada ficaria cobrindo a tela seguinte, como ao abrir as
      // gravações pelo ⋮.
      if (telaCheia.el === node) sairDaTelaCheia();
    }}
    onpointerdowncapture={() => {
      if (emTelaCheia) controles.mostrar();
    }}
    onpointerdown={gestoComeca}
    onpointermove={gestoMove}
    onpointerup={gestoAcaba}
    onpointercancel={gestoAcaba}
  >
    <div
      class="grid"
      class:fit={modo === 'fit'}
      class:animando
      style:--cols={modo === 'fit' ? encaixe.cols : modo}
      style:--tile-w="{Math.floor(encaixe.w)}px"
      style:transform={emTelaCheia && arrasto ? `translateX(${arrasto}px)` : null}
    >
      {#each acessoConfirmado ? visible : [] as c, i (c.id)}
        {@const p = emTelaCheia ? papel(i) : null}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="tile"
          class:ant={p === 'ant'}
          class:prox={p === 'prox'}
          class:fora={p === 'fora'}
          ondblclick={() => alternarCheia(c.id)}
          title={emTelaCheia ? undefined : 'duplo clique: tela cheia'}
        >
          <!-- O componente do go2rtc negocia WebRTC/MSE sozinho. A mídia vai
               direto do navegador ao go2rtc; o dwnvr só faz proxy da
               sinalização, para que a credencial não chegue ao navegador. -->
          <video-stream
            {@attach (node: VideoRTC) => {
              node.mode = 'webrtc,mse';
              // Antes do `src`, que já abre a primeira conexão.
              religarRapido(node);
              node.src = mediaURL.liveWS(c.id);
              hideNativeControls(node);
              if (PIP) manterNoPip(node, c.id);
              const largar = soNaTela(node);
              players.set(c.id, node);
              // Ao desmontar, o custom element fecha a conexão sozinho no
              // disconnectedCallback - nada a limpar aqui além da referência
              // e do observer. Quando a sessão cai, quem fecha na hora é o
              // `fecharTudo`.
              return () => {
                largar();
                players.delete(c.id);
              };
            }}
          ></video-stream>
          <!-- O duplo clique do tile é a tela cheia: dois toques rápidos no
               botão do PiP não podem virar isso. -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="rotulo" ondblclick={(e) => e.stopPropagation()}>
            <span class="name"
              >{c.name}{#if emTelaCheia}<span class="pos">{i + 1}/{visible.length}</span>{/if}</span
            >
            {#if PIP}
              <button
                class="pip"
                class:on={pipCam === c.id}
                onclick={() => alternarPip(c.id)}
                aria-label="{pipCam === c.id ? 'fechar' : 'abrir'} {c.name} em janela flutuante"
                aria-pressed={pipCam === c.id}
                title={pipCam === c.id ? 'fechar a janela flutuante' : 'abrir em janela flutuante'}
              >
                <Icone nome="pip" tamanho={16} />
              </button>
            {/if}
          </div>

          {#if avisos[c.id]}<span class="badge">{avisos[c.id]}</span>{/if}

          <!-- Na tela cheia este canto é do botão de sair, que é do palco. Dois
               cliques rápidos nele não podem chegar ao duplo clique do tile:
               seriam três trocas em vez de duas. -->
          {#if !emTelaCheia}
            <button
              class="cheia"
              onclick={() => {
                alternarCheia(c.id);
              }}
              ondblclick={(e) => {
                e.stopPropagation();
              }}
              aria-label="{c.name} em tela cheia"
              title="tela cheia (ou duplo clique no vídeo)"
            >
              <Icone nome="telaCheia" tamanho={16} />
            </button>
          {/if}

          <!-- O duplo clique do tile é a tela cheia: dois toques rápidos no ⋮
               ou no menu não podem virar isso. -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="opcoes" ondblclick={(e) => e.stopPropagation()}>
            <button
              class="mais"
              onclick={() => (menu = menu === c.id ? null : c.id)}
              aria-label="opções da câmera {c.name}"
              aria-haspopup="menu"
              aria-expanded={menu === c.id}
              ><span aria-hidden="true"><Icone nome="menu" tamanho="100%" /></span></button
            >

            {#if menu === c.id}
              <!-- Links de verdade, e não botões que trocam o hash: assim o
                   clique do meio e o "abrir em nova aba" também funcionam. -->
              <div class="menu" role="menu" {@attach posicionaMenu}>
                <a role="menuitem" href="#rec?cam={encodeURIComponent(c.id)}">Ver gravações</a>
                {#if cameras.detector}
                  <a role="menuitem" href="#detection?cams={encodeURIComponent(c.id)}">
                    Ver detecções
                  </a>
                {/if}
                <button class="ghost" role="menuitem" onclick={() => capturar(c.id)}>
                  Baixar imagem agora
                </button>
                <button class="ghost" role="menuitem" onclick={() => remover(c.id)}>
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
    {#if emTelaCheia}
      {#if visible.length > 1}
        <button
          class="ghost seta esq"
          onclick={() => {
            ir(-1);
          }}
          aria-label="câmera anterior"
          title="câmera anterior (←)"
        >
          <Icone nome="chevronEsq" tamanho={22} />
        </button>
        <button
          class="ghost seta dir"
          onclick={() => {
            ir(1);
          }}
          aria-label="próxima câmera"
          title="próxima câmera (→)"
        >
          <Icone nome="chevronDir" tamanho={22} />
        </button>
      {/if}
      <span class="sair"><BotaoTelaCheia ativa sobreVideo onclick={sairDaTelaCheia} /></span>
    {/if}
  </div>

  {#if !visible.length && !showPicker}
    {#if cameras.list.length}
      <p class="empty">Selecione as câmeras para visualizar.</p>
    {:else if !cameras.loading}
      <!-- Sem a guarda do `loading`, o "nenhuma câmera" piscava a cada abertura
           da tela, no intervalo em que a lista ainda estava sendo buscada. -->
      <SemCameras />
    {/if}
  {/if}
</div>

<style>
  .semacesso {
    color: var(--warn);
  }

  .page {
    display: grid;
    gap: 10px;
    padding: 10px;
    max-width: 1600px;
    margin: 0 auto;
  }

  /* No encaixar quem limita o tile é a altura da janela, já na conta do
     `melhorEncaixe`: o teto de largura só tiraria espaço em tela mais alta,
     como a 16:10 de notebook. Nas colunas fixas o teto fica: lá a altura não
     entra na conta, e o tile cresceria com a largura até passar da janela. */
  .page.fit {
    max-width: none;
  }

  .barra {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    gap: 10px;
  }
  .controles {
    display: contents;
  }

  /* O segmentado: encaixar e as colunas coladas num bloco só. */
  .modos {
    gap: 0;
  }
  .modos button {
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
  .modos button:first-child {
    margin-left: 0;
    border-radius: 8px 0 0 8px;
  }
  .modos button:last-child {
    border-radius: 0 8px 8px 0;
  }
  .modos button.on {
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
  .pausar {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
  }
  .pausar.on {
    border-color: var(--accent);
  }
  .trilho {
    width: 30px;
    height: 17px;
    border-radius: 9px;
    background: var(--line);
    position: relative;
    flex: none;
  }
  .trilho::after {
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
  .pausar.on .trilho {
    background: var(--accent);
  }
  .pausar.on .trilho::after {
    left: 15px;
  }

  /* Os textos dos botões só aparecem onde cabem. */
  .texto-botao {
    display: none;
  }

  @media (min-width: 720px) {
    .controles {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 10px;
    }
    .sep {
      display: block;
    }
    .texto-botao {
      display: inline;
    }
  }

  /* Recorta a grade no modo encaixar: a altura em pixels vem do `medir`, e sem
     isto um arredondamento para cima devolveria a rolagem que o modo evita. */
  .palco.fit {
    overflow: hidden;
  }

  .grid {
    display: grid;
    gap: 8px;
    grid-template-columns: repeat(var(--cols), minmax(0, 1fr));
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
  .rotulo {
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
  .cheia {
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
  .cheia:focus-visible {
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
    .cheia {
      opacity: 0;
    }
    .tile:hover .pip,
    .tile:hover .cheia,
    .pip:focus-visible,
    .cheia:focus-visible,
    .pip.on {
      opacity: 1;
    }
    .tile:hover .pip,
    .tile:hover .cheia {
      background: rgba(13, 17, 23, 0.95);
    }
  }

  /* Embaixo à direita, na linha do nome e do PiP. */
  .cheia {
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
  .opcoes {
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
  .palco.fs {
    overflow: hidden;
    background: #000;
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
  }

  .palco.fs .grid {
    display: block;
    position: absolute;
    inset: 0;
    height: auto;
  }
  .grid.animando {
    transition: transform var(--troca) ease-out;
  }

  /* `left`, e não `transform`, para pôr as vizinhas dos lados: tile com
     transform vira a referência do `position: fixed`, e o menu do ⋮ abriria
     fora do lugar. */
  .palco.fs .tile {
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    aspect-ratio: auto;
    border-radius: 0;
  }
  .palco.fs .tile.ant {
    left: -100%;
  }
  .palco.fs .tile.prox {
    left: 100%;
  }
  .palco.fs .tile.fora {
    display: none;
  }

  /* Os controles do tile, maiores: aqui é a tela inteira, e no celular o dedo. */
  .palco.fs .rotulo {
    left: max(10px, env(safe-area-inset-left));
    bottom: max(10px, env(safe-area-inset-bottom));
  }
  .palco.fs .name {
    padding: 4px 10px;
    font-size: 14px;
  }
  .pos {
    margin-left: 6px;
    color: var(--dim);
    font-variant-numeric: tabular-nums;
  }
  .palco.fs .pip {
    width: 34px;
    height: 28px;
  }
  .palco.fs .pip :global(svg) {
    width: 18px;
    height: 18px;
  }
  .palco.fs .mais {
    width: 52px;
    height: 48px;
    padding: max(8px, env(safe-area-inset-top)) max(8px, env(safe-area-inset-right)) 0 0;
  }
  .palco.fs .mais span {
    width: 36px;
    height: 32px;
    padding: 6px;
  }
  /* Embaixo à direita está o botão de sair. */
  .palco.fs .badge {
    right: max(10px, env(safe-area-inset-right));
    bottom: calc(max(10px, env(safe-area-inset-bottom)) + 54px);
  }

  /* Mesmo fundo do botão de sair (`sobreVideo`): a imagem pode ser clara. */
  .seta {
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
  .seta.esq {
    left: max(8px, env(safe-area-inset-left));
  }
  .seta.dir {
    right: max(8px, env(safe-area-inset-right));
  }

  .sair {
    position: absolute;
    z-index: 1;
    right: max(10px, env(safe-area-inset-right));
    bottom: max(10px, env(safe-area-inset-bottom));
  }

  /* O nome e a posição ficam; o resto some. No fim do bloco para ganhar do
     `.tile:hover`, que tem o mesmo peso. */
  .palco.fs .seta,
  .palco.fs .sair,
  .palco.fs .mais,
  .palco.fs .pip {
    transition: opacity 0.25s;
  }
  .palco.escondidos .seta,
  .palco.escondidos .sair,
  .palco.escondidos .mais,
  .palco.escondidos .pip {
    opacity: 0;
    pointer-events: none;
  }
  .palco.escondidos {
    cursor: none;
  }
</style>
