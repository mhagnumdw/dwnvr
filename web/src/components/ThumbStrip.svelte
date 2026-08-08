<script>
  import { thumbnail } from '../lib/thumbs.js';
  import { hhmm, hhmmss } from '../lib/format.js';

  let {
    cam,
    segments = [],   // [[inícioMs, duraçãoMs, geração], …]
    currentMs = 0,
    onseek = () => {},
  } = $props();

  // Quantas miniaturas mostrar. Mais que isso vira desfile de imagens que
  // ninguém consegue distinguir, e cada uma custa um decode.
  const MAX_TILES = 40;

  // Escolhe segmentos em torno do instante atual, espaçados de forma uniforme
  // — assim a tira cobre um intervalo útil mesmo quando há 1440 segmentos.
  const tiles = $derived.by(() => {
    if (!segments.length) return [];
    if (segments.length <= MAX_TILES) return segments;

    const center = segments.findIndex(([s, d]) => currentMs >= s && currentMs < s + d);
    const anchor = center >= 0 ? center : 0;
    const half = Math.floor(MAX_TILES / 2);
    let start = Math.max(0, anchor - half);
    return segments.slice(start, start + MAX_TILES);
  });
</script>

<div class="strip">
  {#each tiles as [t, dur] (t)}
    {@const active = currentMs >= t && currentMs < t + dur}
    <button class="tile" class:active onclick={() => onseek(t)} title={hhmmss(t)}>
      {#await thumbnail(cam, t) then bitmap}
        {#if bitmap}
          <canvas
            {@attach (node) => {
              // Desenhar num canvas do tamanho exibido, e não escalar um
              // <img>, evita guardar um bitmap 1080p por miniatura na memória
              // do celular.
              const w = node.clientWidth * (window.devicePixelRatio || 1);
              const h = node.clientHeight * (window.devicePixelRatio || 1);
              node.width = w;
              node.height = h;
              node.getContext('2d').drawImage(bitmap, 0, 0, w, h);
            }}
          ></canvas>
        {:else}
          <span class="fail">—</span>
        {/if}
      {/await}
      <span class="time mono">{hhmm(t)}</span>
    </button>
  {/each}

  {#if !tiles.length}
    <span class="muted small">sem gravação neste dia</span>
  {/if}
</div>

<style>
  .strip {
    display: flex;
    gap: 6px;
    overflow-x: auto;
    padding: 4px 2px 8px;
    scroll-snap-type: x proximity;
    /* Rolagem horizontal fluida no toque, sem prender a rolagem da página. */
    overscroll-behavior-x: contain;
  }

  .tile {
    position: relative;
    flex: 0 0 auto;
    width: 108px;
    height: 62px;
    padding: 0;
    min-height: 0;
    border-radius: 6px;
    overflow: hidden;
    background: #000;
    scroll-snap-align: center;
    display: grid;
    place-items: center;
  }

  @media (max-width: 520px) {
    .tile { width: 88px; height: 52px; }
  }

  .tile.active {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent);
  }

  canvas {
    width: 100%;
    height: 100%;
    display: block;
    object-fit: cover;
  }

  .time {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    font-size: 10px;
    background: rgba(0, 0, 0, 0.62);
    color: #fff;
    padding: 1px 0;
  }

  .fail { color: var(--dim); }
</style>
