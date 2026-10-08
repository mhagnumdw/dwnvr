<script>
  import { onMount } from 'svelte';
  import '../vendor/video-stream.js';
  import { cameras, loadCameras } from '../lib/state.svelte.js';
  import { paramsAtuais, escrever } from '../lib/rota.svelte.js';
  import { api, mediaURL } from '../lib/api.js';
  import { baixarQuadro } from '../lib/captura.js';
  import { dayKey, hhmmss } from '../lib/format.js';
  import SemCameras from '../components/SemCameras.svelte';

  const STORAGE_KEY = 'dwnvr.live.selection';
  const LAYOUT_KEY = 'dwnvr.live.layout';
  const SEMPRE_KEY = 'dwnvr.live.sempreLigadas';

  const COLUNAS = [1, 2, 3];
  // 'fit' é só mais um modo ao lado das colunas: como um exclui o outro por
  // construção, escolher 2× desliga o encaixar sem nenhum código para isso.
  const MODOS = ['fit', ...COLUNAS];

  // Precisa bater com o `gap` da grade no CSS: é o espaço que a conta do
  // encaixe desconta antes de dividir o que sobra entre os tiles.
  const GAP = 8;
  const PROPORCAO = 16 / 9;

  // Quanto tempo um tile rolado para fora da tela segue ligado antes do corte.
  // Longa de propósito: voltar para onde se estava há pouco não pode custar a
  // espera por um keyframe. Ver `soNaTela`.
  const CARENCIA_ROLAGEM_MS = 30_000;

  // Intervalo mínimo entre uma tentativa de conexão e a seguinte, crescendo a
  // cada falha seguida e voltando ao primeiro quando o vídeo toca. O player do
  // go2rtc usa 15 s fixos. Ver `religarRapido`.
  const RELIGAR_MS = [500, 1000, 2000, 5000];

  // Quanto cada sondagem do servidor espera antes de desistir, ao voltar de
  // uma aba oculta. Ver `esperarRede`.
  const SONDA_MS = 2000;

  // Lida uma vez, na inicialização: daqui em diante quem manda é o estado da
  // tela, que escreve de volta na URL.
  const params = paramsAtuais();

  let selected = $state(new Set());
  let modo = $state(lerModo());
  let showPicker = $state(false);
  let palco = $state(null);
  let encaixe = $state({ cols: 1, w: 0 });
  // Segura a escrita na URL até a seleção ter sido lida - ver o efeito no fim
  // do bloco.
  let montado = $state(false);

  const visible = $derived(cameras.list.filter((c) => selected.has(c.id)));
  const tudoMarcado = $derived(
    cameras.list.length > 0 && selected.size === cameras.list.length,
  );

  // A URL vem primeiro, o localStorage depois, e só então o palpite pela
  // largura da tela. A URL descreve ESTE link; o localStorage, o hábito deste
  // navegador. Ler dali nunca grava aqui - senão abrir o link de outra pessoa
  // viraria a preferência de quem abriu.
  //
  // Tudo isso na inicialização, e não no onMount: começar sempre em 2× e
  // corrigir depois faria a grade piscar no celular, onde o padrão é 1×.
  function lerModo() {
    const daURL = params.get('view');
    if (daURL === 'fit') return 'fit';
    if (COLUNAS.includes(Number(daURL))) return Number(daURL);
    return lerLayout();
  }

  function lerLayout() {
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
    const ids = daURL ?? JSON.parse(localStorage.getItem(STORAGE_KEY) || 'null');
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

  // O componente do go2rtc cria o <video> com controls=true. Numa grade ao
  // vivo isso mostra uma barra de progresso que não significa nada - não há
  // linha do tempo para percorrer - e ainda aparece em umas câmeras e não em
  // outras, conforme o modo negociado.
  function hideNativeControls(node) {
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

  function fullscreen(el) {
    if (document.fullscreenElement) document.exitFullscreen();
    else el.requestFullscreen?.().catch(() => {});
  }

  // --- o menu do ⋮ de cada tile -----------------------------------------------

  // Um aberto por vez, pela câmera dele. Fica DENTRO do tile, e não num
  // `position: fixed` como o das Detecções: em tela cheia o navegador só mostra
  // o elemento que foi para a tela cheia, e um menu fora dele sumiria ali.
  let menu = $state(null);
  // O <video-stream> de cada câmera, para a captura achar o <video> dele. Não
  // precisa ser reativo: só é lido no clique.
  const players = {};
  // Falha de captura ou de PiP por câmera, mostrada no próprio tile por alguns
  // segundos.
  const avisos = $state({});
  const avisoTimer = {};

  function avisar(cam, texto) {
    avisos[cam] = texto;
    clearTimeout(avisoTimer[cam]);
    avisoTimer[cam] = setTimeout(() => delete avisos[cam], 5000);
  }

  function foraDoMenu(e) {
    if (menu && !e.target.closest?.('.opcoes')) menu = null;
  }

  function tecla(e) {
    if (e.key === 'Escape') menu = null;
  }

  // O quadro é o que está na tela, e o nome leva a hora do clique: no live os
  // dois diferem só pela latência do stream. Mesma convenção das Gravações
  // (cam_AAAA-MM-DD_HH-MM-SS), para imagem de live e de gravação ficarem lado a
  // lado na pasta.
  async function capturar(cam) {
    menu = null;
    const d = new Date();
    const nome = `${cam}_${dayKey(d)}_${hhmmss(d.getTime()).replaceAll(':', '-')}.jpg`;
    try {
      await baixarQuadro(players[cam]?.video, nome);
    } catch (e) {
      avisar(cam, e.message);
    }
  }

  // O mesmo que desmarcar a câmera na lista: grava a seleção e a URL acompanha.
  function remover(cam) {
    menu = null;
    toggle(cam);
  }

  // --- picture-in-picture ------------------------------------------------------

  // O Firefox não tem a API (tem só o botão dele, desenhado sobre o vídeo): lá
  // o botão nem aparece.
  const PIP = document.pictureInPictureEnabled === true;
  // A câmera que está na janela flutuante. O navegador deixa uma por vez.
  let pipCam = $state(null);

  async function alternarPip(cam) {
    const video = players[cam]?.video;
    try {
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
  function manterNoPip(node, cam) {
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
  let rede = null;

  function aoMudarVisibilidade() {
    if (document.hidden || rede) return;
    rede = esperarRede().finally(() => (rede = null));
  }

  // Na fase de captura da `window`, para valer antes do listener que cada
  // player põe no `document` e que é quem religa as câmeras.
  onMount(() => {
    window.addEventListener('visibilitychange', aoMudarVisibilidade, true);
    return () => window.removeEventListener('visibilitychange', aoMudarVisibilidade, true);
  });

  // Qualquer resposta serve, até um erro HTTP: o que importa é o servidor ter
  // sido alcançado. Desiste se a aba voltar a ficar oculta.
  async function esperarRede() {
    for (let i = 0; !document.hidden; i++) {
      try {
        await api.ping(SONDA_MS);
        return;
      } catch {
        await new Promise((r) => setTimeout(r, RELIGAR_MS[Math.min(i, RELIGAR_MS.length - 1)]));
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
  function religarRapido(node) {
    const conectar = node.onconnect.bind(node);
    node.onconnect = () => {
      if (document.hidden && document.pictureInPictureElement !== node.video) return false;
      if (!rede) return conectar();
      rede.then(() => {
        if (!document.hidden && node.isConnected && (sempreLigadas || !foraDaTela.has(node))) conectar();
      });
      return false;
    };
    let falhas = 0;
    const fechou = node.onclose.bind(node);
    node.onclose = () => {
      node.RECONNECT_TIMEOUT = RELIGAR_MS[Math.min(falhas, RELIGAR_MS.length - 1)];
      const religando = fechou();
      if (religando) falhas++;
      return religando;
    };
    // O `playing` sai do <video>, que não o propaga: só a fase de captura o
    // pega aqui, como em `manterNoPip`.
    node.addEventListener('playing', () => (falhas = 0), true);
  }

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
  // se o botão "manter ligadas" aparece.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- quem a tela lê é o `quantasFora`
  const foraDaTela = new Set();
  const corte = new WeakMap();
  let quantasFora = $state(0);

  // Opt-in de quem prefere não esperar o keyframe ao rolar, e aceita pagar em
  // bateria. Do aparelho, como o layout: vai no localStorage, nunca na URL.
  let sempreLigadas = $state(localStorage.getItem(SEMPRE_KEY) === '1');

  function agendarCorte(node) {
    clearTimeout(corte.get(node));
    const espera = Math.max(0, CARENCIA_ROLAGEM_MS - node.DISCONNECT_TIMEOUT);
    corte.set(node, setTimeout(() => node.disconnectedCallback(), espera));
  }

  const observador = new IntersectionObserver((entradas) => {
    for (const { target: node, isIntersecting } of entradas) {
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
  function soNaTela(node) {
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
  function setSelection(next) {
    selected = next;
    localStorage.setItem(STORAGE_KEY, JSON.stringify([...next]));
  }

  function toggle(id) {
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- vira o `selected` inteiro, sem mutar o antigo
    const next = new Set(selected);
    next.has(id) ? next.delete(id) : next.add(id);
    setSelection(next);
  }

  function todas() {
    setSelection(tudoMarcado ? new Set() : new Set(cameras.list.map((c) => c.id)));
  }

  function setModo(m) {
    modo = m;
    localStorage.setItem(LAYOUT_KEY, String(m));
  }

  // Maior tile 16:9 que cabe: para cada número de colunas, a largura do tile é
  // limitada ou pela largura disponível, ou pela altura das linhas que sobram.
  // Como a altura entra na conta, a grade escolhida nunca transborda.
  function melhorEncaixe(n, W, H) {
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
    if (!palco) return;
    if (modo !== 'fit' || !visible.length) {
      palco.style.height = '';
      return;
    }

    // O palco vai do seu topo até o fim da janela, menos o que já está
    // reservado abaixo dele: o respiro da página e o espaço que o `main` guarda
    // para a navegação inferior não cobrir o conteúdo. Esse padding do `main` é
    // zero no desktop, onde a navegação sobe - lê-lo evita repetir aqui, em JS,
    // o breakpoint que já está no CSS.
    const abaixo = (el) => (el ? parseFloat(getComputedStyle(el).paddingBottom) || 0 : 0);
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
    void [modo, visible.length, showPicker];
    medir();
  });
</script>

<svelte:window
  onresize={medir}
  onorientationchange={medir}
  onpointerdown={foraDoMenu}
  onkeydown={tecla}
/>

<div class="page" class:fit={modo === 'fit'}>
  <div class="row wrap">
    <button class="ghost" onclick={() => (showPicker = !showPicker)}>
      ☰ câmeras ({selected.size})
    </button>

    <div class="row modos" role="group" aria-label="layout">
      {#each MODOS as m (m)}
        <button
          class="ghost"
          class:on={modo === m}
          aria-pressed={modo === m}
          title={m === 'fit' ? 'encaixar todas na tela' : `${m} coluna${m > 1 ? 's' : ''}`}
          onclick={() => setModo(m)}
        >
          {#if m === 'fit'}
            <!-- Cantos de enquadramento desenhados à mão: o caractere ⛶ não
                 existe nas fontes do sistema no Linux nem no Android. -->
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M4 9V5a1 1 0 0 1 1-1h4M15 4h4a1 1 0 0 1 1 1v4M20 15v4a1 1 0 0 1-1 1h-4M9 20H5a1 1 0 0 1-1-1v-4"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
          {:else}
            {m}×
          {/if}
        </button>
      {/each}
    </div>

    <!-- Só aparece quando há o que manter: com tudo à vista - o encaixar,
         ou poucas câmeras - não há tile fora da tela para cortar. -->
    {#if quantasFora > 0}
      <button
        class="ghost sempre"
        class:on={sempreLigadas}
        aria-pressed={sempreLigadas}
        aria-label="manter ligadas as câmeras fora da tela"
        title={sempreLigadas
          ? 'câmeras fora da tela seguem ligadas (gasta mais bateria)'
          : `câmeras fora da tela desligam após ${CARENCIA_ROLAGEM_MS / 1000} s`}
        onclick={alternarSempre}
      >
        <!-- A tela com o vídeo escapando por baixo dela: "toca fora da área
             visível". O recorte em volta do ▶ é da cor do fundo. -->
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <rect x="2.5" y="2.5" width="15" height="11" rx="1.6" class="moldura" />
          <path d="M5 17h4M5 20.5h3" class="rastro" />
          <path d="M12.6 11.4 23.2 17.3 12.6 23.2z" class="play" />
        </svg>
      </button>
    {/if}

    <span class="spacer"></span>
    {#if selected.size > 4}
      <span class="chip" title="cada stream é decodificado pelo seu aparelho, não pelo servidor">
        ⚠ {selected.size} streams simultâneos
      </span>
    {/if}
  </div>

  {#if showPicker}
    <div class="card selecao">
      {#if cameras.list.length}
        <div class="row">
          <button class="ghost" onclick={todas}>
            {tudoMarcado ? 'limpar' : `todas (${cameras.list.length})`}
          </button>
        </div>
        <div class="lista">
          {#each cameras.list as c (c.id)}
            <label class="row">
              <input type="checkbox" checked={selected.has(c.id)} onchange={() => toggle(c.id)} />
              <span>{c.name}</span>
            </label>
          {/each}
        </div>
      {:else}
        <p class="muted small">nenhuma câmera cadastrada</p>
      {/if}
    </div>
  {/if}

  <div class="palco" class:fit={modo === 'fit'} bind:this={palco}>
    <div
      class="grid"
      class:fit={modo === 'fit'}
      style:--cols={modo === 'fit' ? encaixe.cols : modo}
      style:--tile-w="{Math.floor(encaixe.w)}px"
    >
      {#each visible as c (c.id)}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="tile" ondblclick={(e) => fullscreen(e.currentTarget)} title="duplo clique: tela cheia">
          <!-- O componente do go2rtc negocia WebRTC/MSE sozinho. A mídia vai
               direto do navegador ao go2rtc; o dwnvr só faz proxy da
               sinalização, para que a credencial não chegue ao navegador. -->
          <video-stream
            {@attach (node) => {
              node.mode = 'webrtc,mse';
              // Antes do `src`, que já abre a primeira conexão.
              religarRapido(node);
              node.src = mediaURL.liveWS(c.id);
              hideNativeControls(node);
              if (PIP) manterNoPip(node, c.id);
              const largar = soNaTela(node);
              players[c.id] = node;
              // Ao desmontar, o custom element fecha a conexão sozinho no
              // disconnectedCallback - nada a limpar aqui além da referência
              // e do observer.
              return () => {
                largar();
                delete players[c.id];
              };
            }}
          ></video-stream>
          <!-- O duplo clique do tile é a tela cheia: dois toques rápidos no
               botão do PiP não podem virar isso. -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="rotulo" ondblclick={(e) => e.stopPropagation()}>
            <span class="name">{c.name}</span>
            {#if PIP}
              <button
                class="pip"
                class:on={pipCam === c.id}
                onclick={() => alternarPip(c.id)}
                aria-label="{pipCam === c.id ? 'fechar' : 'abrir'} {c.name} em janela flutuante"
                aria-pressed={pipCam === c.id}
                title={pipCam === c.id ? 'fechar a janela flutuante' : 'abrir em janela flutuante'}
              >
                <!-- A janela grande com a pequena no canto: o desenho que os
                     sistemas usam para PiP. -->
                <svg viewBox="0 0 24 24" aria-hidden="true">
                  <rect x="3" y="5" width="18" height="14" rx="2" class="moldura" />
                  <rect x="12" y="12" width="7" height="5" rx="1" class="janela" />
                </svg>
              </button>
            {/if}
          </div>

          {#if avisos[c.id]}<span class="badge">{avisos[c.id]}</span>{/if}

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
            ><span aria-hidden="true">⋮</span></button>

            {#if menu === c.id}
              <!-- Links de verdade, e não botões que trocam o hash: assim o
                   clique do meio e o "abrir em nova aba" também funcionam. -->
              <div class="menu" role="menu">
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
  .page.fit { max-width: none; }

  .modos button { padding: 8px 11px; }
  .modos button.on { color: var(--fg); border-color: var(--accent); }

  .modos svg {
    display: block;
    width: 17px;
    height: 17px;
    fill: none;
    stroke: currentColor;
    stroke-width: 2;
  }

  .sempre { padding: 8px 11px; }
  .sempre.on { border-color: var(--accent); }
  .sempre svg { display: block; width: 20px; height: 20px; }
  .sempre .moldura { fill: none; stroke: currentColor; stroke-width: 1.8; }
  .sempre .rastro {
    fill: none;
    stroke: currentColor;
    stroke-width: 1.8;
    stroke-linecap: round;
    stroke-dasharray: 1.5 2.5;
  }
  .sempre .play {
    fill: var(--accent);
    stroke: var(--bg);
    stroke-width: 2;
    stroke-linejoin: round;
    paint-order: stroke;
  }

  .selecao { display: grid; gap: 10px; }

  .lista {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    gap: 6px 14px;
  }

  .lista label {
    gap: 8px;
    cursor: pointer;
    padding: 4px 0;
  }

  .lista input { min-height: 0; width: 18px; height: 18px; accent-color: var(--accent); }

  /* Recorta a grade no modo encaixar: a altura em pixels vem do `medir`, e sem
     isto um arredondamento para cima devolveria a rolagem que o modo evita. */
  .palco.fit { overflow: hidden; }

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
    max-width: calc(100% - 16px);
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

  /* Mesma altura e mesmo fundo do ⋮, para os dois controles do tile serem da
     mesma família. */
  .pip {
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

  .pip svg {
    display: block;
    width: 16px;
    height: 16px;
  }

  .pip .moldura { fill: none; stroke: currentColor; stroke-width: 2; }
  .pip .janela { fill: currentColor; }

  .pip:focus-visible { outline: 2px solid var(--accent); }
  .pip.on { color: var(--accent); outline: 2px solid var(--accent); }

  /* Como o ⋮: com mouse aparece só sobre o tile, e fica enquanto a câmera está
     no PiP; no toque fica sempre à vista. */
  @media (hover: hover) {
    .pip { opacity: 0; }
    .tile:hover .pip,
    .pip:focus-visible,
    .pip.on { opacity: 1; }
    .tile:hover .pip { background: rgba(13, 17, 23, 0.95); }
  }

  /* O player do go2rtc escreve o modo (loading, RTC, MSE) no canto de cima à
     direita, que é o lugar do ⋮: o texto anda para a esquerda dele, sem tocar
     no arquivo copiado. */
  .tile :global(video-stream .info) { padding-right: 44px; }

  /* Embaixo à direita: em cima à esquerda o player do go2rtc escreve os erros
     dele, e embaixo à esquerda está o nome. */
  .badge {
    position: absolute;
    right: 8px;
    bottom: 8px;
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

  /* Sem caixa própria: o ⋮ e o menu se posicionam contra o tile, e é isso que
     deixa a altura do menu ser medida pela do tile. O div existe só para
     segurar o duplo clique. */
  .opcoes { display: contents; }

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
    border-radius: 6px;
    background: rgba(13, 17, 23, 0.8);
    font-size: 15px;
    font-weight: 700;
    line-height: 20px;
    text-align: center;
    color: var(--fg);
  }

  .mais:focus-visible { outline: none; }
  .mais:focus-visible span,
  .mais[aria-expanded='true'] span { outline: 2px solid var(--accent); }

  /* Com mouse o ⋮ aparece só sobre o tile, e fica enquanto o menu dele está
     aberto. No toque não há hover: ele fica sempre à vista. */
  @media (hover: hover) {
    .mais { opacity: 0; }
    .tile:hover .mais,
    .mais:focus-visible,
    .mais[aria-expanded='true'] { opacity: 1; }
    .tile:hover .mais span { background: rgba(13, 17, 23, 0.95); }
  }

  /* Dentro do tile, que corta o que passa da borda: num tile muito baixo o
     menu rola em vez de sumir pela metade. */
  .menu {
    position: absolute;
    top: 30px;
    right: 4px;
    z-index: 1;
    max-height: calc(100% - 38px);
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
    .menu button:hover { background: var(--panel-2); }
  }
</style>
