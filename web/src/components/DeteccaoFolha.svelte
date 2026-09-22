<!--
  A folha de uma detecção, aberta pelo toque na miniatura da tela Detecções.

  Mostra o quadro que o detector de objetos olhou, grande e com os rótulos das
  caixas, e toca o trecho em volta dele só quando pedido: ANTES_MS antes do
  onset até DEPOIS_MS depois. O player é o mesmo de Gravações, com a lista de
  segmentos cortada nesse trecho - então ele não baixa nada além dele.
-->
<script>
  import { onMount } from 'svelte';
  import Modal from './Modal.svelte';
  import { api, mediaURL } from '../lib/api.js';
  import { Player } from '../lib/player.svelte.js';
  import { dispoe, desenha } from '../lib/caixas.js';
  import { familia, iconeDa, iconeURL, NOME_DA_CLASSE } from '../lib/icones.js';
  import { hhmmss, ddmm, dayKey } from '../lib/format.js';

  // O trecho: um pouco antes do onset, para ver o objeto entrar, e o bastante
  // depois para ver o que ele fez. Os mesmos números do mock aprovado.
  const ANTES_MS = 2000;
  const DEPOIS_MS = 6000;

  let { det, camera, onclose, onanterior = null, onproxima = null } = $props();

  let palco;
  let img = $state(null);
  let canvas = $state(null);
  let video = $state(null);

  const player = new Player();
  let tocando = $state(false);
  let erro = $state('');
  let Caixas = $state(null);

  const objetos = $derived(
    [...det.objetos].sort(
      (a, b) => familia(a.familia).prioridade - familia(b.familia).prioridade || b.score - a.score,
    ),
  );
  // As caixas do vídeo acendem quando a reprodução passa pelo quadro olhado,
  // e a camada de Gravações acha esse quadro em `quadroMs` de cada marca.
  const marcas = $derived(det.objetos.map((o) => ({ ...o, quadroMs: det.quadroMs })));

  const link = $derived(
    `#rec?cam=${encodeURIComponent(det.cam)}&day=${dayKey(new Date(det.instanteMs))}&t=${hhmmss(det.instanteMs)}`,
  );

  const score = (v) => v.toFixed(2).replace('.', ',');

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
    const g = canvas.getContext('2d');
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
    ro.observe(palco);
    return () => {
      ro.disconnect();
      player.destroy();
    };
  });

  // --- o trecho ---------------------------------------------------------------

  async function tocar() {
    if (tocando) {
      player.toggle();
      return;
    }
    tocando = true;
    erro = '';
    if (!Caixas) import('./Caixas.svelte').then((m) => (Caixas = m.default));
    try {
      const inicio = det.instanteMs - ANTES_MS;
      const t = await api.timelineRange(det.cam, inicio, det.instanteMs + DEPOIS_MS);
      if (!video) return; // a folha fechou durante a busca
      if (!t.segments.length) {
        erro = 'sem gravação neste trecho';
        tocando = false;
        return;
      }
      player.attach(video);
      player.setSource(det.cam, t.gens, t.segments);
      await player.seek(inicio);
    } catch (e) {
      erro = e.message;
      tocando = false;
    }
  }

  // Passou do fim do trecho: para ali. Sem isto o player seguiria pedindo os
  // segmentos seguintes, que a lista cortada nem tem - e ficaria esperando.
  $effect(() => {
    if (tocando && player.playing && player.currentMs >= det.instanteMs + DEPOIS_MS) player.pause();
  });

  const progresso = $derived(
    tocando
      ? Math.max(0, Math.min(1, (player.currentMs - det.instanteMs + ANTES_MS) / (ANTES_MS + DEPOIS_MS)))
      : 0,
  );

  function tecla(e) {
    if (e.key === 'ArrowLeft' && onanterior) onanterior();
    else if (e.key === 'ArrowRight' && onproxima) onproxima();
  }
</script>

<svelte:window onkeydown={tecla} />

<Modal {onclose} largura={880}>
  <div class="cab">
    <span class="hora mono">{hhmmss(det.instanteMs)}</span>
    <span class="muted small">{ddmm(det.instanteMs)}</span>
    <span class="muted">·</span>
    <span class="camera">{camera}</span>
    <span class="spacer"></span>
    <button class="ghost fechar" onclick={onclose} aria-label="fechar">✕</button>
  </div>

  <div class="palco" bind:this={palco}>
    {#if det.temQuadro}
      <img
        bind:this={img}
        src={mediaURL.quadro(det.cam, det.instanteMs)}
        alt="quadro da detecção das {hhmmss(det.instanteMs)}"
        onload={pintaQuadro}
      />
    {/if}
    <canvas bind:this={canvas} class:escondido={tocando}></canvas>
    {#if tocando}
      <!-- svelte-ignore a11y_media_has_caption -->
      <video bind:this={video} playsinline></video>
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
      onclick={tocar}
      aria-label={tocando ? 'pausar ou continuar' : `tocar o trecho das ${hhmmss(det.instanteMs)}`}
    ></button>
    {#if !player.playing}
      <span class="play" aria-hidden="true">
        <svg viewBox="0 0 24 24"><path d="M7 4.5v15l13-7.5z" /></svg>
      </span>
    {/if}
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
        {#if icone}<img src={iconeURL(icone, 14, familia(o.familia).cor)} alt="" width="14" height="14" />{/if}
        <span>{NOME_DA_CLASSE[o.classe] ?? o.classe} {score(o.score)}</span>
      </span>
    {/each}
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

  .fechar {
    min-height: 36px;
    padding: 4px 10px;
  }

  .palco {
    position: relative;
    aspect-ratio: 16 / 9;
    background: #000;
    border-radius: 6px;
    overflow: hidden;
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
  }

  .play svg {
    width: 22px;
    height: 22px;
    margin-left: 3px;
    fill: #fff;
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

  .badge.bad { color: var(--bad); border-color: #5c2b2b; }

  .objetos {
    display: flex;
    flex-wrap: wrap;
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
