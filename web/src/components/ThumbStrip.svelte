<script lang="ts">
  import { untrack } from 'svelte';
  import { SvelteSet } from 'svelte/reactivity';
  import type { MarkedObject, Segment } from '../lib/api.ts';
  import { context2d } from '../lib/canvas.ts';
  import { thumbnail } from '../lib/thumbs.ts';
  import { hhmmss } from '../lib/format.ts';
  import FamilyBadge from './FamilyBadge.svelte';
  import { familyNames } from '../lib/icons.ts';

  interface Props {
    cam: string;
    segments?: Segment[];
    // The day's object marks: each thumbnail shows the families of its clip.
    objects?: MarkedObject[];
    currentMs?: number;
    defer?: boolean;
    onseek?: (ms: number) => void;
  }

  let { cam, segments = [], objects = [], currentMs = 0, defer = false, onseek }: Props = $props();

  // How many thumbnails to show. More than that becomes a parade of images
  // nobody can tell apart, and each one costs a decode.
  const MAX_TILES = 40;

  // How far beyond the visible part the strip already asks, on each side, as a
  // fraction of its width: the thumbnail is ready before the scroll gets there,
  // without asking for all 40 in the window.
  const SLACK = '50%';

  // A contiguous window of 40 segments around the current instant - and not a
  // spaced sample of the day. A real day has ~2300 segments of ~33s, so this is
  // the path that always runs; the whole list only shows up on a freshly
  // installed camera. The window slides along with currentMs, which keeps the
  // active segment still at index 20 while the thumbnails pass underneath.
  const tiles = $derived.by(() => {
    if (!segments.length) return [];
    if (segments.length <= MAX_TILES) return segments;

    const center = segments.findIndex(([s, d]) => currentMs >= s && currentMs < s + d);
    const anchor = center >= 0 ? center : 0;
    const half = Math.floor(MAX_TILES / 2);
    const start = Math.max(0, anchor - half);
    return segments.slice(start, start + MAX_TILES);
  });

  // The families of each thumbnail, by segment start. One pass over the day's
  // marks, with each one's thumbnail found by binary search among the window's
  // 40: redone once per segment, not per frame.
  const familiesPerTile = $derived.by(() => {
    const perTile: Record<number, string[]> = {};
    const first = tiles[0];
    const last = tiles.at(-1);
    if (!first || !last || !objects.length) return perTile;
    const end = last[0] + last[1];
    for (const o of objects) {
      if (o.instanteMs < first[0] || o.instanteMs >= end) continue;
      let lo = 0;
      let hi = tiles.length - 1;
      while (lo < hi) {
        const m = (lo + hi + 1) >> 1;
        if ((tiles[m]?.[0] ?? Infinity) <= o.instanteMs) lo = m;
        else hi = m - 1;
      }
      const tile = tiles[lo];
      // In a gap between two segments, the mark belongs to no thumbnail.
      if (!tile || o.instanteMs >= tile[0] + tile[1]) continue;
      const fams = (perTile[tile[0]] ??= []);
      if (!fams.includes(o.familia)) fams.push(o.familia);
    }
    return perTile;
  });

  let strip = $state<HTMLDivElement | null>(null);

  // Only the active segment's start, not currentMs: currentMs changes several
  // times per second during playback, and what matters here changes once per
  // segment. Without this filter the effect below would run needlessly all the
  // time.
  const activeStart = $derived.by(() => {
    const seg = tiles.find(([t, dur]) => currentMs >= t && currentMs < t + dur);
    return seg ? seg[0] : -1;
  });

  // After a seek on the timeline, the window is re-sliced around the new
  // instant and the active tile lands at index 20 - about 2280px from the
  // start, far beyond the visible part. The strip was then left with no
  // highlight in view, with not even a hint of which way to scroll.
  $effect(() => {
    if (activeStart < 0 || !strip) return;

    const tile = strip.querySelector('.tile.active');
    if (!tile) return;

    const t = tile.getBoundingClientRect();
    const s = strip.getBoundingClientRect();
    // Already visible: leave it alone. That is what keeps the strip still
    // during playback, since the active one stays at the same spot while the
    // window slides.
    if (t.left >= s.left && t.right <= s.right) return;

    // Only touch the strip's scrollLeft. scrollIntoView() would scroll the page
    // too, which has nothing to do with this.
    strip.scrollLeft += t.left - s.left - (s.width - t.width) / 2;
  });

  // The strip only asks for the thumbnail that enters the viewport, plus the
  // SLACK, and the rest waits for the scroll to get close. Before, it asked for
  // all 40 in the window in DOM order, left to right, and the visible ones -
  // around the active one, at index 20, or at the end of the strip on today's
  // date - arrived last.
  //
  // The requested ones start over on every camera or day load, along with the
  // cache that Recordings clears at that moment. `segments` changes identity on
  // every poll of today, which is why the key is the first instant, not the
  // list.
  const loadAvg = $derived(`${cam}@${segments[0]?.[0] ?? ''}`);
  const requestedKeys = $derived({
    carga: loadAvg,
    instantes: new SvelteSet<number>(),
    // Plain Set on purpose: only `defer` triggers the release.
    postponed: new Set<number>(),
  });

  // The window's slice is new on every currentMs update, but it only slides
  // once per segment: that is when it pays to observe the tiles again.
  const visibleWindow = $derived(tiles.length ? `${cam}@${tiles[0]?.[0]}+${tiles.length}` : '');

  // The first notification from an IntersectionObserver brings all the tiles
  // already in view at once. It runs after the effect above, which has already
  // put the active one in view.
  $effect(() => {
    if (!strip || !visibleWindow) return;
    const io = new IntersectionObserver(
      (entries) => {
        const novas: number[] = [];
        for (const e of entries) {
          const t = Number((e.target as HTMLElement).dataset['t']);
          if (e.isIntersecting) novas.push(t);
          else requestedKeys.postponed.delete(t); // left the viewport before its turn
        }
        request(novas);
      },
      { root: strip, rootMargin: `0px ${SLACK}` },
    );
    for (const tile of strip.querySelectorAll('.tile')) io.observe(tile);
    return () => {
      io.disconnect();
    };
  });

  // From the active one outward: the thumbs.ts queue decodes in the order the
  // downloads finish, and the first that matters is the active one's. The
  // distance is to the segment, not to its start, so the active one is worth
  // zero.
  function request(novas: number[]) {
    if (defer) {
      for (const t of novas) requestedKeys.postponed.add(t);
      return;
    }
    const far = ([s, d]: Segment) =>
      currentMs < s ? s - currentMs : Math.max(0, currentMs - s - d);
    const queue = tiles
      .filter(([t]) => novas.includes(t) && !requestedKeys.instantes.has(t))
      .sort((a, b) => far(a) - far(b));
    for (const [t] of queue) {
      void thumbnail(cam, t);
      requestedKeys.instantes.add(t);
    }
  }

  // While the player fetches the clip it is about to play, the strip asks for
  // nothing: thumbs.ts's `priority: 'low'` only orders the browser's queue, and
  // what has already gone out shares the bandwidth with the segment - which has
  // to arrive whole before the first frame. What came into view in the meantime
  // is held back and goes out together once the video has something to show.
  $effect(() => {
    if (defer || !requestedKeys.postponed.size) return;
    const queue = [...requestedKeys.postponed];
    requestedKeys.postponed.clear();
    untrack(() => {
      request(queue);
    });
  });

  // The mouse wheel is vertical; the strip scrolls horizontally. Without this
  // translation the browser scrolls the page and the strip is unreachable for
  // whoever has no trackpad.
  function onWheel(ev: WheelEvent & { currentTarget: HTMLDivElement }) {
    const el = ev.currentTarget;
    if (el.scrollWidth <= el.clientWidth) return; // nothing to scroll
    if (Math.abs(ev.deltaX) > Math.abs(ev.deltaY)) return; // horizontal gesture: the native one already handles it

    // deltaMode: 0 px (Chrome), 1 lines (Firefox), 2 pages.
    const step = ev.deltaY * (ev.deltaMode === 1 ? 32 : ev.deltaMode === 2 ? el.clientWidth : 1);

    // The browser enforces the limit: scrollLeft is fractional and scrollWidth
    // comes rounded, so a limit computed here is off at the end by a fraction
    // of a pixel - and the strip would trap the page's scroll at the end
    // forever.
    const before = el.scrollLeft;
    el.scrollLeft = before + step;
    if (el.scrollLeft === before) return; // already at the end: hand the scroll back to the page

    ev.preventDefault();
  }
</script>

<div class="strip" bind:this={strip} onwheel={onWheel}>
  {#each tiles as [t, dur] (t)}
    {@const active = currentMs >= t && currentMs < t + dur}
    <button
      class="tile"
      class:active
      data-t={t}
      onclick={() => {
        onseek?.(t);
      }}
      title={hhmmss(t)}
      aria-label={[hhmmss(t), familyNames(familiesPerTile[t] ?? [])].filter(Boolean).join(', ')}
    >
      {#if requestedKeys.instantes.has(t)}
        {#await thumbnail(cam, t) then bitmap}
          {#if bitmap}
            <canvas
              {@attach (node: HTMLCanvasElement) => {
                // O bitmap já vem do thumbs.ts na largura do quadrinho maior;
                // o canvas, do tamanho exibido, ajusta ao quadrinho de agora.
                const w = node.clientWidth * (window.devicePixelRatio || 1);
                const h = node.clientHeight * (window.devicePixelRatio || 1);
                node.width = w;
                node.height = h;
                context2d(node).drawImage(bitmap, 0, 0, w, h);
              }}
            ></canvas>
          {:else}
            <span class="fail">-</span>
          {/if}
        {/await}
      {/if}
      <FamilyBadge families={familiesPerTile[t] ?? []} />
      <span class="time mono">{hhmmss(t)}</span>
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
    /* Smooth horizontal scrolling on touch, without trapping the page's scroll. */
    overscroll-behavior-x: contain;
  }

  /* The width is thumbs.ts's FRAMELET_WIDTH: if you change it here, change it there. */
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
    .tile {
      width: 88px;
      height: 52px;
    }
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

  .fail {
    color: var(--dim);
  }
</style>
