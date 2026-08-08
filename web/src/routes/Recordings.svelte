<script>
  import { onMount, onDestroy } from 'svelte';
  import { cameras, loadCameras } from '../lib/state.svelte.js';
  import { api, mediaURL } from '../lib/api.js';
  import { Player } from '../lib/player.svelte.js';
  import { clearThumbnails } from '../lib/thumbs.js';
  import { dayKey, parseDay, hhmmss, duracao } from '../lib/format.js';
  import Timeline from '../components/Timeline.svelte';
  import ThumbStrip from '../components/ThumbStrip.svelte';

  const player = new Player();

  let video = $state(null);
  let cam = $state('');
  let day = $state(dayKey());
  let timeline = $state({ ranges: [], segments: [], gens: [] });
  let loading = $state(false);
  let error = $state('');
  let showThumbs = $state(true);

  const dayStart = $derived(parseDay(day).getTime());
  const dayEnd = $derived(dayStart + 86400_000);
  const gravado = $derived(timeline.ranges.reduce((a, [s, e]) => a + (e - s), 0));
  const isToday = $derived(day === dayKey());

  onMount(async () => {
    if (!cameras.list.length) await loadCameras();
    cam = cameras.list[0]?.id ?? '';
  });

  onDestroy(() => {
    player.destroy();
    clearThumbnails();
  });

  // Ligar o player assim que o <video> existir no DOM.
  $effect(() => {
    if (video && player.video !== video) player.attach(video);
  });

  // Recarrega ao trocar de câmera ou de dia. Este é o único ponto de busca de
  // dados da tela, o que evita duas telas disputando o mesmo estado.
  $effect(() => {
    if (cam && day) load(cam, day);
  });

  async function load(c, d) {
    loading = true;
    error = '';
    try {
      const t = await api.timeline(c, d);
      timeline = t;
      player.setSource(c, t.gens, t.segments);
      clearThumbnails();
      // Num dia passado, o mais útil é o começo; hoje, o mais recente.
      if (t.segments.length) {
        player.seek(d === dayKey() ? t.segments.at(-1)[0] : t.segments[0][0]);
      }
    } catch (e) {
      error = e.message;
      timeline = { ranges: [], segments: [], gens: [] };
    } finally {
      loading = false;
    }
  }

  function shiftDay(n) {
    const d = parseDay(day);
    d.setDate(d.getDate() + n);
    day = dayKey(d);
  }

  function exportar() {
    const from = Math.round(player.currentMs || dayStart);
    location.href = mediaURL.export(cam, from, from + 5 * 60_000);
  }
</script>

<div class="page">
  <div class="bar row wrap">
    <select bind:value={cam} aria-label="câmera">
      {#each cameras.list as c (c.id)}
        <option value={c.id}>{c.name}</option>
      {/each}
    </select>

    <div class="row daynav">
      <button class="ghost" onclick={() => shiftDay(-1)} aria-label="dia anterior">‹</button>
      <input type="date" bind:value={day} max={dayKey()} aria-label="dia" />
      <button class="ghost" onclick={() => shiftDay(1)} disabled={isToday} aria-label="próximo dia">›</button>
    </div>

    <span class="spacer"></span>
    <span class="muted small mono">
      {#if loading}carregando…
      {:else if timeline.segments.length}{duracao(gravado)} · {timeline.ranges.length} faixa(s)
      {:else}sem gravação{/if}
    </span>
  </div>

  <div class="stage">
    <!-- svelte-ignore a11y_media_has_caption -->
    <video bind:this={video} playsinline controls={false}></video>
    {#if player.buffering}<span class="badge">carregando…</span>{/if}
    {#if player.error}<span class="badge bad">{player.error}</span>{/if}
    {#if error}<span class="badge bad">{error}</span>{/if}
  </div>

  <div class="controls row wrap">
    <button class="primary" onclick={() => player.toggle()} aria-label="tocar ou pausar">
      {player.playing ? '⏸' : '▶'}
    </button>
    <span class="clock mono">{player.currentMs ? hhmmss(player.currentMs) : '--:--:--'}</span>

    <select
      value={player.rate}
      onchange={(e) => player.setRate(Number(e.currentTarget.value))}
      aria-label="velocidade"
    >
      {#each [0.5, 1, 2, 4, 8] as r}<option value={r}>{r}×</option>{/each}
    </select>

    <span class="spacer"></span>
    <button class="ghost small" onclick={() => (showThumbs = !showThumbs)}>
      {showThumbs ? 'ocultar' : 'mostrar'} miniaturas
    </button>
    <button onclick={exportar} disabled={!player.currentMs}>⤓ exportar 5 min</button>
  </div>

  {#if showThumbs}
    <ThumbStrip
      {cam}
      segments={timeline.segments}
      currentMs={player.currentMs}
      onseek={(ms) => player.seek(ms)}
    />
  {/if}

  <Timeline
    ranges={timeline.ranges}
    {dayStart}
    {dayEnd}
    currentMs={player.currentMs}
    onseek={(ms) => player.seek(ms)}
  />

  <p class="legend muted small row wrap">
    <span><i class="has"></i> com gravação</span>
    <span><i class="gap"></i> sem gravação</span>
    <span>toque para pular · arraste para navegar · pince ou role para dar zoom</span>
  </p>
</div>

<style>
  .page {
    display: grid;
    gap: 10px;
    padding: 10px;
    max-width: 1400px;
    margin: 0 auto;
  }

  .bar select { max-width: 45vw; }
  .daynav { gap: 4px; }
  .daynav button { padding: 9px 12px; }

  .stage {
    position: relative;
    background: #000;
    border-radius: var(--radius);
    overflow: hidden;
  }

  video {
    width: 100%;
    aspect-ratio: 16 / 9;
    /* A timeline é o ponto desta tela e precisa caber junto com o vídeo: sem
       este teto, um 16:9 numa janela larga já empurra a barra para fora da
       vista. */
    max-height: 56dvh;
    object-fit: contain;
    display: block;
    background: #000;
    margin: 0 auto;
  }

  .badge {
    position: absolute;
    top: 8px;
    left: 8px;
    background: rgba(0, 0, 0, 0.7);
    border: 1px solid var(--line);
    border-radius: 6px;
    padding: 3px 8px;
    font-size: 12px;
  }
  .badge.bad { color: var(--bad); border-color: #5c2b2b; }

  .clock { font-size: 16px; }

  .legend { gap: 14px; margin: 0; }
  .legend i {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: 2px;
    vertical-align: -1px;
  }
  .legend i.has { background: var(--accent); }
  .legend i.gap { background: #1b1f24; border: 1px solid var(--line); }
</style>
