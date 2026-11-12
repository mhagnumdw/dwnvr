<!--
  A folha de uma detecção, aberta pelo toque na miniatura da tela Detecções.

  Mostra o quadro que o detector de objetos olhou, grande e com os rótulos das
  caixas, e toca o trecho em volta dele só quando pedido: ANTES_MS antes do
  onset até DEPOIS_MS depois. O player é o mesmo de Gravações, com a lista de
  segmentos cortada nesse trecho - então ele não baixa nada além dele.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';
  import FullscreenButton from './FullscreenButton.svelte';
  import type VideoBoxes from './Boxes.svelte';
  import { api, mediaURL, messageOf } from '../lib/api.ts';
  import type { Detection } from '../lib/api.ts';
  import { context2d } from '../lib/canvas.ts';
  import { Player } from '../lib/player.svelte.ts';
  import { place, draw } from '../lib/boxes.ts';
  import { drawOutline, fillArea } from '../lib/area.ts';
  import type { Point } from '../lib/area.ts';
  import { family, iconOf, iconURL, CLASS_NAME } from '../lib/icons.ts';
  import { hhmmss, ddmm } from '../lib/format.ts';
  import { recordingsLink } from '../lib/route.svelte.ts';
  import { fullscreen, toggleFullscreen, leaveFullscreen } from '../lib/fullscreen.svelte.ts';

  // O trecho: um pouco antes do onset, para ver o objeto entrar, e o bastante
  // depois para ver o que ele fez. Os mesmos números do mock aprovado.
  const BEFORE_MS = 2000;
  const AFTER_MS = 6000;

  // A largura da folha. O palco é 16:9 e é o que mais cresce, então numa
  // janela baixa é a largura que precisa ceder para o quadro caber inteiro -
  // o teto sai da altura livre, e não o contrário. `RESTO_PX` é o que o resto
  // da folha ocupa em altura (cabeçalho 36 + objetos 36, a altura do botão
  // de ocultar + ações 44 + três espaços de 14 + 28 de padding + 2 de borda)
  // e `RESPIRO_PX` é o que fica dos lados do palco (28 de padding + 2 de
  // borda), que entra na largura mas não no palco. Sem o botão, quando a
  // câmera já está oculta, a linha dos objetos tem 19,5 e a conta sobra - a
  // folha sai um pouco mais estreita, o que não faz mal.
  //
  // A conta precisa fechar certa, arredondando para cima: com a folha 1px
  // acima do teto aparece a barra de rolagem, ela come a largura, o palco encolhe,
  // a folha cabe, a barra some - e o ciclo se repete a cada quadro, com a
  // folha vibrando. Foi o que aconteceu com 169 e 28, que esqueciam a borda e
  // o meio pixel dos objetos. O `reservaRolagem` do Modal corta o ciclo de vez,
  // para quando a folha rola de verdade.
  //
  // O piso existe porque a conta não tem fundo: numa janela muito baixa ela
  // pedia uma folha mais estreita que a própria fileira de botões, e aí o
  // palco é que transbordava para os lados.
  //
  // Uma lista de objetos que quebre em duas linhas deixa a conta curta, e o
  // piso também. Nos dois casos o `grid-auto-rows` do Modal põe a folha para
  // rolar, que é o pior caso - e não o quadro cobrindo os botões, que era o
  // defeito.
  //
  // A detecção que alertou ganha o bloco "alertou" acima dos objetos, e a
  // conta soma a altura dele: `BLOCO_PX` é o bloco com o título (14 de espaço
  // + 2 de borda + 16 de padding + 21 do título) e `LINHA_DO_BLOCO_PX`, cada
  // objeto dele (20 da linha + 4 de espaço).
  const MAX_WIDTH_PX = 880;
  const MIN_WIDTH_PX = 360;
  const REST_PX = 188;
  const BREATHING_PX = 30;
  const BLOCK_PX = 53;
  const BLOCK_ROW_PX = 24;
  // A opacidade do que fica no quadro mas não é o assunto: o objeto que não
  // entrou no alerta, e as áreas fora da realçada.
  const DIMMED = 0.35;

  interface Props {
    det: Detection;
    camera: string;
    onclose: () => void;
    // Nulo é "não há": sem detecção anterior, ou a câmera já oculta.
    onprevious?: (() => void) | null;
    onnext?: (() => void) | null;
    onhide?: (() => void) | null;
    // As áreas do filtro nesta câmera: o contorno vai sobre o quadro, e diz
    // por que a detecção entrou.
    areas?: readonly (readonly Point[])[] | null;
    // As áreas em que a detecção alertou, pelo id gravado na marca, com o
    // nome e o contorno de agora; `pontos` nulo é a área apagada desde então.
    alertAreas?: readonly { id: string; nome: string; points: readonly Point[] | null }[];
    // Abre o editor de área sobre este quadro. Nulo sem quadro.
    onarea?: (() => void) | null;
  }

  let {
    det,
    camera,
    onclose,
    onprevious = null,
    onnext = null,
    onhide = null,
    areas = null,
    alertAreas = [],
    onarea = null,
  }: Props = $props();

  let stage = $state<HTMLDivElement | null>(null);
  let img = $state<HTMLImageElement | null>(null);
  let canvas = $state<HTMLCanvasElement | null>(null);
  let video = $state<HTMLVideoElement | null>(null);

  const player = new Player();
  let playingNow = $state(false);
  // O vídeo já pintou o primeiro quadro. Entre o toque no play e esse quadro
  // passam uns 300ms buscando bytes, e nesse intervalo o `<video>` ainda é
  // transparente: é ele quem decide quando o quadro parado sai de cena, não o
  // clique. Trocar no clique deixava aparecer a foto sem as caixas no meio do
  // caminho - o quadro com as caixas fica até o vídeo ter o que mostrar.
  let videoPainted = $state(false);
  let error = $state('');
  let Boxes = $state<typeof VideoBoxes | null>(null);

  const objects = $derived(
    [...det.objetos].sort(
      (a, b) => family(a.familia).priority - family(b.familia).priority || b.score - a.score,
    ),
  );
  // Com alerta, o bloco "alertou" leva os objetos que passaram na regra, e os
  // outros ficam embaixo, como "não entrou no alerta".
  const ofAlert = $derived(objects.filter((o) => o.alert));
  const outsideAlert = $derived(objects.filter((o) => !o.alert));
  const alerted = $derived(ofAlert.length > 0);
  const alertArea = (id: string) => alertAreas.find((a) => a.id === id);

  // A área do alerta realçada no quadro pelo toque no chip dela: as outras
  // ficam apagadas, e o mesmo toque volta a mostrar todas. Não passa à
  // próxima detecção pelas setas: a folha remonta a cada uma (o `{#key}` de
  // Detecções).
  let highlighted = $state<string | null>(null);
  const toggleHighlight = (id: string) => {
    highlighted = highlighted === id ? null : id;
  };

  const width = $derived.by(() => {
    const rest = REST_PX + (alerted ? BLOCK_PX + ofAlert.length * BLOCK_ROW_PX : 0);
    return `clamp(${MIN_WIDTH_PX}px, calc((var(--altura-max) - ${rest}px) * 16 / 9 + ${BREATHING_PX}px), ${MAX_WIDTH_PX}px)`;
  });
  // As caixas do vídeo acendem quando a reprodução passa pelo quadro olhado,
  // e a camada de Gravações acha esse quadro em `quadroMs` de cada marca.
  const marks = $derived.by(() => {
    const frameMs = det.quadroMs;
    return frameMs === undefined ? [] : det.objetos.map((o) => ({ ...o, quadroMs: frameMs }));
  });

  const link = $derived(recordingsLink(det.cam, det.instanteMs));

  const score = (v: number) => v.toFixed(2).replace('.', ',');

  // --- o quadro parado, com as caixas e os rótulos ---------------------------

  // Onde a imagem está dentro do palco: `object-fit: contain` deixa faixas
  // pretas quando a câmera não é 16:9, e a fração da caixa é da imagem.
  function paintFrame() {
    if (!canvas || !stage) return;
    const dpr = window.devicePixelRatio || 1;
    const w = stage.clientWidth;
    const h = stage.clientHeight;
    canvas.width = Math.round(w * dpr);
    canvas.height = Math.round(h * dpr);
    const g = context2d(canvas);
    g.setTransform(dpr, 0, 0, dpr, 0, 0);
    g.clearRect(0, 0, w, h);
    const iw = img?.naturalWidth;
    const ih = img?.naturalHeight;
    if (!iw || !ih) return;
    const s = Math.min(w / iw, h / ih);
    g.translate((w - iw * s) / 2, (h - ih * s) / 2);
    // O contorno das áreas do filtro e das do alerta, uma vez cada. A
    // realçada vai cheia, por cima, e as outras, apagadas.
    const outlines = [...(areas ?? [])];
    for (const a of alertAreas)
      if (a.points && !outlines.includes(a.points)) outlines.push(a.points);
    const emphasis = highlighted === null ? null : (alertArea(highlighted)?.points ?? null);
    g.globalAlpha = emphasis ? DIMMED : 1;
    for (const a of outlines) if (a !== emphasis) drawOutline(g, a, iw * s, ih * s);
    g.globalAlpha = 1;
    if (emphasis) fillArea(g, emphasis, iw * s, ih * s);
    // O objeto que não entrou no alerta fica no quadro, mas apagado.
    draw(g, place(g, det.objetos, iw * s, ih * s, true), (it) =>
      alerted && !it.mark.alert ? DIMMED : 1,
    );
  }

  // Repinta quando o realce muda (e com o que mais o quadro lê).
  $effect(paintFrame);

  onMount(() => {
    const ro = new ResizeObserver(paintFrame);
    if (stage) ro.observe(stage);
    return () => {
      ro.disconnect();
      player.destroy();
    };
  });

  // --- o trecho ---------------------------------------------------------------

  const endMs = $derived(det.instanteMs + AFTER_MS);
  const startMs = $derived(det.instanteMs - BEFORE_MS);

  async function play() {
    if (playingNow) {
      player.toggle();
      return;
    }
    playingNow = true;
    error = '';
    if (!Boxes) {
      void import('./Boxes.svelte').then((m) => {
        Boxes = m.default;
      });
    }
    try {
      const t = await api.timelineRange(det.cam, startMs, endMs);
      if (!video) return; // a folha fechou durante a busca
      if (!t.segments.length) {
        error = 'sem gravação neste trecho';
        playingNow = false;
        return;
      }
      player.attach(video);
      player.setSource(det.cam, t.gens, t.segments);
      await player.seek(startMs);
    } catch (e) {
      error = messageOf(e);
      playingNow = false;
    }
  }

  // Passou do fim do trecho: acaba ali e a folha volta ao quadro da detecção,
  // como quando abriu. Parar é obrigatório - sem isso o player seguiria
  // pedindo os segmentos seguintes, que a lista cortada nem tem, e ficaria
  // esperando. Voltar ao quadro é o que torna o trecho repetível: o player
  // sai de cena, e o play seguinte refaz o mesmo caminho da primeira vez.
  //
  // Só pausar não bastava por dois motivos. O relógio parava no fim, e o play
  // seguinte reacendia `playing` com a posição ainda passada do fim - este
  // mesmo efeito pausava no quadro seguinte, e o trecho só tocava de novo
  // fechando e reabrindo a folha. E o `<video>` ficava por cima do `<img>`
  // exibindo o último quadro: trocar o src para recomeçar o deixava vazio por
  // um instante, e o quadro parado piscava por baixo antes do vídeo entrar.
  //
  // Soltar o player também devolve a memória do vídeo decodificado entre uma
  // reprodução e outra, que num celular são dezenas de MB.
  $effect(() => {
    if (playingNow && player.playing && player.currentMs >= endMs) {
      player.destroy();
      playingNow = false;
      videoPainted = false;
    }
  });

  const progress = $derived(
    playingNow ? Math.max(0, Math.min(1, (player.currentMs - startMs) / (endMs - startMs))) : 0,
  );

  // --- a tela cheia -----------------------------------------------------------
  //
  // Vai para a tela cheia só o palco: o quadro, o vídeo, as caixas e a barra
  // do trecho. O duplo clique chega depois de dois cliques simples, e cada um
  // tocaria ou pausaria: o segundo é ignorado, e o primeiro, se pausou, é
  // desfeito no duplo. Assim o duplo clique nunca pausa - com o trecho parado,
  // ele já começa a tocar na tela cheia.

  const inFullscreen = $derived(!!stage && fullscreen.el === stage);
  let pausedOnClick = false;

  function clique(e: MouseEvent) {
    if (e.detail > 1) return;
    pausedOnClick = player.playing;
    void play();
  }

  function toggleFull() {
    if (stage) toggleFullscreen(stage);
  }

  function doubleClick() {
    toggleFull();
    if (pausedOnClick && !player.playing) player.toggle();
    pausedOnClick = false;
  }

  // Na tela cheia as setas não trocam de detecção: a folha seguinte é outra,
  // e o palco que estava na tela cheia sairia dela junto com a anterior.
  function keyHandler(e: KeyboardEvent) {
    if (inFullscreen) return;
    if (e.key === 'ArrowLeft' && onprevious) onprevious();
    else if (e.key === 'ArrowRight' && onnext) onnext();
  }
</script>

<svelte:window onkeydown={keyHandler} />

<Modal {onclose} {width} scrollReserve centered>
  <div class="cab">
    <span class="hour mono">{hhmmss(det.instanteMs)}</span>
    <span class="muted small">{ddmm(det.instanteMs)}</span>
    <span class="muted">·</span>
    <span class="camera">{camera}</span>
    <span class="spacer"></span>
    <button class="ghost close" onclick={onclose} aria-label="fechar">
      <Icon name="close" size={16} />
    </button>
  </div>

  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="stage"
    class:fs={inFullscreen}
    bind:this={stage}
    ondblclick={doubleClick}
    {@attach (node: HTMLElement) => () => {
      // A de verdade o navegador desfaz sozinho quando o elemento sai da
      // página; a simulada ficaria cobrindo a tela depois da folha fechar.
      if (fullscreen.el === node) leaveFullscreen();
    }}
  >
    {#if det.temQuadro}
      <img
        bind:this={img}
        src={mediaURL.frame(det.cam, det.instanteMs)}
        alt="quadro da detecção das {hhmmss(det.instanteMs)}"
        onload={paintFrame}
      />
    {/if}
    <canvas bind:this={canvas} class:escondido={videoPainted}></canvas>
    {#if playingNow}
      <video bind:this={video} playsinline onloadeddata={() => (videoPainted = true)}></video>
      {#if Boxes && video}
        <Boxes
          {video}
          objects={marks}
          currentMs={player.currentMs}
          playing={player.playing}
          rate={player.rate}
        />
      {/if}
      <span class="badge video-hour mono">{hhmmss(player.currentMs || det.instanteMs)}</span>
      <span class="progress" style:width="{progress * 100}%"></span>
    {/if}
    <button
      class="touch"
      onclick={clique}
      aria-label={playingNow
        ? 'pausar ou continuar'
        : `tocar o trecho das ${hhmmss(det.instanteMs)}`}
    ></button>
    {#if !player.playing}
      <span class="play" aria-hidden="true">
        <Icon name="play" size={22} />
      </span>
    {/if}
    <span class="fullscreen"
      ><FullscreenButton active={inFullscreen} overVideo onclick={toggleFull} /></span
    >
    <div class="notices">
      {#if player.buffering}<span class="badge">carregando…</span>{/if}
      {#if player.error}<span class="badge bad">{player.error}</span>{/if}
      {#if error}<span class="badge bad">{error}</span>{/if}
    </div>
  </div>

  {#if alerted}
    <div class="alert-block small">
      <span class="title"><Icon name="bell" size={16} stroke={2.4} />alertou</span>
      {#each ofAlert as o, i (i)}
        {@const icon = iconOf(o, true) ?? iconOf(o, false)}
        <span class="alert-row">
          <span class="object">
            {#if icon}<img
                src={iconURL(icon, 14, family(o.familia).color)}
                alt=""
                width="14"
                height="14"
              />{/if}
            <span>{CLASS_NAME[o.classe] ?? o.classe} {score(o.score)}</span>
          </span>
          {#each o.alertAreas ?? [] as id (id)}
            {@const a = alertArea(id)}
            {#if a?.points && det.temQuadro}
              <button
                type="button"
                class="chip-area"
                class:on={highlighted === id}
                aria-pressed={highlighted === id}
                aria-label={highlighted === id
                  ? `${a.nome}: mostrar todas as áreas`
                  : `${a.nome}: realçar no quadro`}
                onclick={() => toggleHighlight(id)}>{a.nome}</button
              >
            {:else}
              <span class="chip-area">{a?.nome ?? 'área apagada'}</span>
            {/if}
          {:else}
            <span class="muted">imagem inteira</span>
          {/each}
        </span>
      {/each}
    </div>
  {/if}

  <div class="objects small">
    {#each alerted ? outsideAlert : objects as o, i (i)}
      {@const icon = iconOf(o, true) ?? iconOf(o, false)}
      <span class="object">
        {#if icon}<img
            src={iconURL(icon, 14, family(o.familia).color)}
            alt=""
            width="14"
            height="14"
          />{/if}
        <span>{CLASS_NAME[o.classe] ?? o.classe} {score(o.score)}</span>
      </span>
    {/each}
    {#if alerted && outsideAlert.length}
      <span class="muted">não entrou no alerta</span>
    {/if}
    <!-- Mora aqui, e não no cabeçalho: lá ele deixava pouco espaço e o nome
         da câmera saía cortado no celular. -->
    {#if onarea}
      <button
        class="ghost hide"
        onclick={onarea}
        aria-label="filtrar as detecções da câmera {camera} por uma área desenhada sobre este quadro"
      >
        filtrar por área
      </button>
    {/if}
    {#if onhide}
      <button
        class="ghost hide"
        class:together={onarea}
        onclick={onhide}
        aria-label="ocultar a câmera {camera} desta tela"
      >
        ocultar câmera
      </button>
    {/if}
  </div>

  <div class="actions">
    <button class="ghost" onclick={onprevious} disabled={!onprevious}>‹ anterior</button>
    <button class="ghost" onclick={onnext} disabled={!onnext}>próxima ›</button>
    <a href={link}>abrir em Gravações ›</a>
  </div>
</Modal>

<style>
  .cab {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .hour {
    font-size: 20px;
    font-weight: 600;
  }

  .camera {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .close,
  .hide {
    min-height: 36px;
    padding: 4px 10px;
  }

  .hide {
    flex: none;
    margin-left: auto;
    color: var(--dim);
    font-size: 13px;
  }

  /* Os dois botões ficam juntos, à direita. */
  .hide.together {
    margin-left: 0;
  }

  .stage {
    position: relative;
    aspect-ratio: 16 / 9;
    background: #000;
    border-radius: 6px;
    overflow: hidden;
  }

  /* Sem `position`, e sem o 16:9: quem dá os dois é o navegador (`:fullscreen`)
     ou a regra da simulada no app.css. */
  .stage.fs {
    border-radius: 0;
  }

  .stage img,
  .stage video,
  .stage canvas {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: contain;
    display: block;
  }

  canvas.escondido {
    display: none;
  }

  .touch {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    min-height: 0;
    padding: 0;
    border: 0;
    border-radius: 0;
    background: transparent;
  }

  .play {
    position: absolute;
    left: 50%;
    top: 50%;
    transform: translate(-50%, -50%);
    width: 56px;
    height: 56px;
    border-radius: 50%;
    display: grid;
    place-items: center;
    background: rgba(13, 17, 23, 0.72);
    border: 1px solid rgba(230, 237, 243, 0.35);
    pointer-events: none;
    color: #fff;
  }

  .play :global(svg) {
    margin-left: 3px;
  }

  /* Sempre à vista, com mouse ou no toque: aqui o vídeo é o assunto da folha,
     e não um entre vários, como no Ao vivo. */
  .fullscreen {
    position: absolute;
    right: 8px;
    bottom: 8px;
  }

  .stage.fs .fullscreen {
    right: max(10px, env(safe-area-inset-right));
    bottom: max(10px, env(safe-area-inset-bottom));
  }

  .progress {
    position: absolute;
    left: 0;
    bottom: 0;
    height: 3px;
    background: var(--accent);
    pointer-events: none;
  }

  .video-hour {
    position: absolute;
    right: 8px;
    top: 8px;
    pointer-events: none;
  }

  .notices {
    position: absolute;
    left: 50%;
    bottom: 12px;
    transform: translateX(-50%);
    display: grid;
    justify-items: center;
    gap: 4px;
    pointer-events: none;
  }

  .badge {
    background: rgba(0, 0, 0, 0.7);
    border: 1px solid var(--line);
    border-radius: 6px;
    padding: 3px 8px;
    font-size: 12px;
    white-space: nowrap;
  }

  .badge.bad {
    color: var(--bad);
    border-color: #5c2b2b;
  }

  .objects {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 14px;
  }

  .object {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  .object img {
    display: block;
  }

  /* Na cor do sino (o --warn, #d29922), como o "só alertas" de Detecções. */
  .alert-block {
    display: grid;
    gap: 4px;
    padding: 8px 10px;
    border: 1px solid rgba(210, 153, 34, 0.4);
    border-radius: 8px;
    background: rgba(210, 153, 34, 0.06);
  }

  .title {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--warn);
    font-weight: 600;
    font-size: 14px;
  }

  .alert-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }

  /* O tracejado é o do contorno da área sobre o quadro. */
  .chip-area {
    padding: 0 7px;
    border: 1px dashed var(--accent);
    border-radius: 999px;
    font-size: 12px;
    white-space: nowrap;
  }
  /* O chip que realça a área: do tamanho do de antes, para a conta da largura
     da folha continuar valendo, com o alvo do dedo maior que ele. */
  button.chip-area {
    position: relative;
    min-height: 0;
    line-height: inherit;
    background: transparent;
    color: inherit;
  }
  /* Sem passar da metade do espaço entre as linhas do bloco: o alvo de uma
     não cobre o chip da de baixo. */
  button.chip-area::after {
    content: '';
    position: absolute;
    inset: -2px -3px;
  }
  button.chip-area.on {
    border-style: solid;
    background: var(--ativo-fundo);
  }

  .actions {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .actions a {
    margin-left: auto;
    text-decoration: none;
  }
</style>
