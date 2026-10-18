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
  import Icone from './Icone.svelte';
  import BotaoTelaCheia from './BotaoTelaCheia.svelte';
  import type CaixasDoVideo from './Caixas.svelte';
  import { api, mediaURL, mensagemDe } from '../lib/api.ts';
  import type { Deteccao } from '../lib/api.ts';
  import { contexto2d } from '../lib/canvas.ts';
  import { Player } from '../lib/player.svelte.ts';
  import { dispoe, desenha } from '../lib/caixas.ts';
  import { familia, iconeDa, iconeURL, NOME_DA_CLASSE } from '../lib/icones.ts';
  import { hhmmss, ddmm } from '../lib/format.ts';
  import { linkGravacoes } from '../lib/rota.svelte.ts';
  import { telaCheia, alternarTelaCheia, sairDaTelaCheia } from '../lib/telaCheia.svelte.ts';

  // O trecho: um pouco antes do onset, para ver o objeto entrar, e o bastante
  // depois para ver o que ele fez. Os mesmos números do mock aprovado.
  const ANTES_MS = 2000;
  const DEPOIS_MS = 6000;

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
  const LARGURA_MAX_PX = 880;
  const LARGURA_MIN_PX = 360;
  const RESTO_PX = 188;
  const RESPIRO_PX = 30;
  const largura = `clamp(${LARGURA_MIN_PX}px, calc((var(--altura-max) - ${RESTO_PX}px) * 16 / 9 + ${RESPIRO_PX}px), ${LARGURA_MAX_PX}px)`;

  interface Props {
    det: Deteccao;
    camera: string;
    onclose: () => void;
    // Nulo é "não há": sem detecção anterior, ou a câmera já oculta.
    onanterior?: (() => void) | null;
    onproxima?: (() => void) | null;
    onocultar?: (() => void) | null;
  }

  let {
    det,
    camera,
    onclose,
    onanterior = null,
    onproxima = null,
    onocultar = null,
  }: Props = $props();

  let palco = $state<HTMLDivElement | null>(null);
  let img = $state<HTMLImageElement | null>(null);
  let canvas = $state<HTMLCanvasElement | null>(null);
  let video = $state<HTMLVideoElement | null>(null);

  const player = new Player();
  let tocando = $state(false);
  // O vídeo já pintou o primeiro quadro. Entre o toque no play e esse quadro
  // passam uns 300ms buscando bytes, e nesse intervalo o `<video>` ainda é
  // transparente: é ele quem decide quando o quadro parado sai de cena, não o
  // clique. Trocar no clique deixava aparecer a foto sem as caixas no meio do
  // caminho - o quadro com as caixas fica até o vídeo ter o que mostrar.
  let videoPintou = $state(false);
  let erro = $state('');
  let Caixas = $state<typeof CaixasDoVideo | null>(null);

  const objetos = $derived(
    [...det.objetos].sort(
      (a, b) => familia(a.familia).prioridade - familia(b.familia).prioridade || b.score - a.score,
    ),
  );
  // As caixas do vídeo acendem quando a reprodução passa pelo quadro olhado,
  // e a camada de Gravações acha esse quadro em `quadroMs` de cada marca.
  const marcas = $derived.by(() => {
    const quadroMs = det.quadroMs;
    return quadroMs === undefined ? [] : det.objetos.map((o) => ({ ...o, quadroMs }));
  });

  const link = $derived(linkGravacoes(det.cam, det.instanteMs));

  const score = (v: number) => v.toFixed(2).replace('.', ',');

  // --- o quadro parado, com as caixas e os rótulos ---------------------------

  // Onde a imagem está dentro do palco: `object-fit: contain` deixa faixas
  // pretas quando a câmera não é 16:9, e a fração da caixa é da imagem.
  function pintaQuadro() {
    if (!canvas || !palco) return;
    const dpr = window.devicePixelRatio || 1;
    const w = palco.clientWidth;
    const h = palco.clientHeight;
    canvas.width = Math.round(w * dpr);
    canvas.height = Math.round(h * dpr);
    const g = contexto2d(canvas);
    g.setTransform(dpr, 0, 0, dpr, 0, 0);
    g.clearRect(0, 0, w, h);
    const iw = img?.naturalWidth;
    const ih = img?.naturalHeight;
    if (!iw || !ih) return;
    const s = Math.min(w / iw, h / ih);
    g.translate((w - iw * s) / 2, (h - ih * s) / 2);
    desenha(g, dispoe(g, det.objetos, iw * s, ih * s, true));
  }

  onMount(() => {
    const ro = new ResizeObserver(pintaQuadro);
    if (palco) ro.observe(palco);
    return () => {
      ro.disconnect();
      player.destroy();
    };
  });

  // --- o trecho ---------------------------------------------------------------

  const fimMs = $derived(det.instanteMs + DEPOIS_MS);
  const inicioMs = $derived(det.instanteMs - ANTES_MS);

  async function tocar() {
    if (tocando) {
      player.toggle();
      return;
    }
    tocando = true;
    erro = '';
    if (!Caixas) {
      void import('./Caixas.svelte').then((m) => {
        Caixas = m.default;
      });
    }
    try {
      const t = await api.timelineRange(det.cam, inicioMs, fimMs);
      if (!video) return; // a folha fechou durante a busca
      if (!t.segments.length) {
        erro = 'sem gravação neste trecho';
        tocando = false;
        return;
      }
      player.attach(video);
      player.setSource(det.cam, t.gens, t.segments);
      await player.seek(inicioMs);
    } catch (e) {
      erro = mensagemDe(e);
      tocando = false;
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
    if (tocando && player.playing && player.currentMs >= fimMs) {
      player.destroy();
      tocando = false;
      videoPintou = false;
    }
  });

  const progresso = $derived(
    tocando ? Math.max(0, Math.min(1, (player.currentMs - inicioMs) / (fimMs - inicioMs))) : 0,
  );

  // --- a tela cheia -----------------------------------------------------------
  //
  // Vai para a tela cheia só o palco: o quadro, o vídeo, as caixas e a barra
  // do trecho. O duplo clique chega depois de dois cliques simples, e cada um
  // tocaria ou pausaria: o segundo é ignorado, e o primeiro, se pausou, é
  // desfeito no duplo. Assim o duplo clique nunca pausa - com o trecho parado,
  // ele já começa a tocar na tela cheia.

  const emTelaCheia = $derived(!!palco && telaCheia.el === palco);
  let pausouNoClique = false;

  function clique(e: MouseEvent) {
    if (e.detail > 1) return;
    pausouNoClique = player.playing;
    void tocar();
  }

  function alternarCheia() {
    if (palco) alternarTelaCheia(palco);
  }

  function duploClique() {
    alternarCheia();
    if (pausouNoClique && !player.playing) player.toggle();
    pausouNoClique = false;
  }

  // Na tela cheia as setas não trocam de detecção: a folha seguinte é outra,
  // e o palco que estava na tela cheia sairia dela junto com a anterior.
  function tecla(e: KeyboardEvent) {
    if (emTelaCheia) return;
    if (e.key === 'ArrowLeft' && onanterior) onanterior();
    else if (e.key === 'ArrowRight' && onproxima) onproxima();
  }
</script>

<svelte:window onkeydown={tecla} />

<Modal {onclose} {largura} reservaRolagem centralizada>
  <div class="cab">
    <span class="hora mono">{hhmmss(det.instanteMs)}</span>
    <span class="muted small">{ddmm(det.instanteMs)}</span>
    <span class="muted">·</span>
    <span class="camera">{camera}</span>
    <span class="spacer"></span>
    <button class="ghost fechar" onclick={onclose} aria-label="fechar">
      <Icone nome="fechar" tamanho={16} />
    </button>
  </div>

  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="palco"
    class:fs={emTelaCheia}
    bind:this={palco}
    ondblclick={duploClique}
    {@attach (node: HTMLElement) => () => {
      // A de verdade o navegador desfaz sozinho quando o elemento sai da
      // página; a simulada ficaria cobrindo a tela depois da folha fechar.
      if (telaCheia.el === node) sairDaTelaCheia();
    }}
  >
    {#if det.temQuadro}
      <img
        bind:this={img}
        src={mediaURL.quadro(det.cam, det.instanteMs)}
        alt="quadro da detecção das {hhmmss(det.instanteMs)}"
        onload={pintaQuadro}
      />
    {/if}
    <canvas bind:this={canvas} class:escondido={videoPintou}></canvas>
    {#if tocando}
      <video bind:this={video} playsinline onloadeddata={() => (videoPintou = true)}></video>
      {#if Caixas && video}
        <Caixas
          {video}
          objetos={marcas}
          currentMs={player.currentMs}
          playing={player.playing}
          rate={player.rate}
        />
      {/if}
      <span class="badge hora-video mono">{hhmmss(player.currentMs || det.instanteMs)}</span>
      <span class="progresso" style:width="{progresso * 100}%"></span>
    {/if}
    <button
      class="toque"
      onclick={clique}
      aria-label={tocando ? 'pausar ou continuar' : `tocar o trecho das ${hhmmss(det.instanteMs)}`}
    ></button>
    {#if !player.playing}
      <span class="play" aria-hidden="true">
        <Icone nome="play" tamanho={22} />
      </span>
    {/if}
    <span class="tela-cheia"
      ><BotaoTelaCheia ativa={emTelaCheia} sobreVideo onclick={alternarCheia} /></span
    >
    <div class="avisos">
      {#if player.buffering}<span class="badge">carregando…</span>{/if}
      {#if player.error}<span class="badge bad">{player.error}</span>{/if}
      {#if erro}<span class="badge bad">{erro}</span>{/if}
    </div>
  </div>

  <div class="objetos small">
    {#each objetos as o, i (i)}
      {@const icone = iconeDa(o, true) ?? iconeDa(o, false)}
      <span class="objeto">
        {#if icone}<img
            src={iconeURL(icone, 14, familia(o.familia).cor)}
            alt=""
            width="14"
            height="14"
          />{/if}
        <span>{NOME_DA_CLASSE[o.classe] ?? o.classe} {score(o.score)}</span>
      </span>
    {/each}
    <!-- Mora aqui, e não no cabeçalho: lá ele deixava pouco espaço e o nome
         da câmera saía cortado no celular. -->
    {#if onocultar}
      <button
        class="ghost ocultar"
        onclick={onocultar}
        aria-label="ocultar a câmera {camera} desta tela"
      >
        ocultar câmera
      </button>
    {/if}
  </div>

  <div class="acoes">
    <button class="ghost" onclick={onanterior} disabled={!onanterior}>‹ anterior</button>
    <button class="ghost" onclick={onproxima} disabled={!onproxima}>próxima ›</button>
    <a href={link}>abrir em Gravações ›</a>
  </div>
</Modal>

<style>
  .cab {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .hora {
    font-size: 20px;
    font-weight: 600;
  }

  .camera {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .fechar,
  .ocultar {
    min-height: 36px;
    padding: 4px 10px;
  }

  .ocultar {
    flex: none;
    margin-left: auto;
    color: var(--dim);
    font-size: 13px;
  }

  .palco {
    position: relative;
    aspect-ratio: 16 / 9;
    background: #000;
    border-radius: 6px;
    overflow: hidden;
  }

  /* Sem `position`, e sem o 16:9: quem dá os dois é o navegador (`:fullscreen`)
     ou a regra da simulada no app.css. */
  .palco.fs {
    border-radius: 0;
  }

  .palco img,
  .palco video,
  .palco canvas {
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

  .toque {
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
  .tela-cheia {
    position: absolute;
    right: 8px;
    bottom: 8px;
  }

  .palco.fs .tela-cheia {
    right: max(10px, env(safe-area-inset-right));
    bottom: max(10px, env(safe-area-inset-bottom));
  }

  .progresso {
    position: absolute;
    left: 0;
    bottom: 0;
    height: 3px;
    background: var(--accent);
    pointer-events: none;
  }

  .hora-video {
    position: absolute;
    right: 8px;
    top: 8px;
    pointer-events: none;
  }

  .avisos {
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

  .objetos {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 14px;
  }

  .objeto {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  .objeto img {
    display: block;
  }

  .acoes {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .acoes a {
    margin-left: auto;
    text-decoration: none;
  }
</style>
