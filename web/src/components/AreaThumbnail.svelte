<!--
  The thumbnail of a camera's areas: a frame from it with the drawing of each
  area. With several areas and a dashed outline, it is the "2 áreas" chip on the
  card in Cameras and the filter chip in Detections; with a single, filled area,
  it is the image of AreaLabel and AreaTile.

  The frame comes from the strip thumbnail (thumbs.ts), already downscaled and
  cached: whoever opens the screen with several cameras doesn't decode any at
  full size. It has the width of the strip's framelet; to draw larger without
  blurring, whoever already has the full frame passes it in `background` - the
  camera form, which fetches it for the editor. Without `background` and wider
  than the framelet - the camera card's tile - it asks for the larger thumbnail,
  at the width it is drawn. A camera with no area doesn't mount this, and pays
  nothing.

  With `zoom`, a small area (lib/area.ts, `areaView`), which disappears in the
  full frame, gets a zoom pass: it zooms in until the area is framed, stays a
  moment, and goes back to the full frame. Once when the thumbnail appears;
  after that, the zoom stays while the mouse rests over whatever contains it, or
  while a finger holds the long press on it, and returns when the mouse leaves
  or the finger lets go. Whatever contains it asks with `data-toque-longo`
  (lib/touchHint.ts). Whoever asks the system for less motion sees the jump,
  without the transition. Only the small area pays for the larger frame the zoom
  needs.
-->
<script lang="ts">
  import { onDestroy } from 'svelte';
  import {
    drawOutline,
    viewPoints,
    fillArea,
    WHOLE_FRAME,
    areaView,
    ZOOM_CEILING,
  } from '../lib/area.ts';
  import type { Point, Vista } from '../lib/area.ts';
  import { LONG_TOUCH } from '../lib/touchHint.ts';
  import { scaleToWidth, thumbnail, storedLastSegment } from '../lib/thumbs.ts';

  // The zoom: how long it takes to zoom in (and back), how long the pass stays
  // zoomed in, and how long the mouse has to rest over it for the zoom to come.
  const ZOOM_IDA_MS = 700;
  const ZOOM_FICA_MS = 1000;
  const ZOOM_WAIT_MS = 350;
  // And how long it stays after the mouse leaves or the finger lets go: a hand
  // tremor doesn't undo the zoom.
  const ZOOM_EXIT_MS = 500;

  interface Props {
    cam: string;
    areas: readonly { points: readonly Point[] }[];
    // In CSS px; the height follows the camera's aspect ratio. Without it, the
    // thumbnail fills the width of whatever contains it.
    width?: number;
    // `contorno`: the thin dashed line, which doesn't hide the image when there
    // are several areas; `cheia`: the translucent blue of the Areas tab, for a
    // single area.
    drawing?: 'contorno' | 'cheia';
    // The URL of a frame already in hand; empty until it arrives. Without it,
    // the strip thumbnail.
    background?: string | undefined;
    radius?: number;
    // The zoom pass on the small area. Only with a single area.
    zoom?: boolean;
  }

  let {
    cam,
    areas,
    width,
    drawing = 'contorno',
    background,
    radius = 3,
    zoom = false,
  }: Props = $props();

  let canvas = $state<HTMLCanvasElement | null>(null);
  let image = $state<{ source: CanvasImageSource; w: number; h: number } | null>(null);
  // The measured width, when it doesn't come from `width`.
  let measurement = $state(0);
  // Goes up when the bitmap kept here was closed by the cache that evicted it:
  // the thumbnail asks for the frame again.
  let refetch = $state(0);

  // The part of the frame that frames the area, when it is small and there is
  // zoom.
  const target = $derived.by(() => {
    const a = areas[0];
    return zoom && areas.length === 1 && a ? areaView(a.points) : null;
  });
  const enlargeable = $derived(target !== null);

  // The width at which it is drawn; 0 until the measurement arrives.
  const drawnWidth = $derived(width ?? measurement);
  // The strip thumbnail can't take the zoom: the small area's comes larger. Nor
  // the width larger than the framelet's. Both together: when zoomed, the crop
  // of 1/ZOOM_CEILING of the frame takes the whole drawn width, and the frame
  // has to be ZOOM_CEILING times that for the crop not to blur in the wide
  // tile. Derived separately, so resizing only fetches again when the scale
  // changes, not on every pixel.
  const scale = $derived(
    Math.max(
      enlargeable ? ZOOM_CEILING : 1,
      scaleToWidth(enlargeable ? drawnWidth * ZOOM_CEILING : drawnWidth),
    ),
  );
  const noMeasurement = $derived(drawnWidth === 0);

  $effect(() => {
    const c = cam;
    const f = background;
    // With `background` the scale doesn't matter: read only without it, so
    // resizing doesn't reload the frame already in hand.
    const e = f === undefined ? scale : 1;
    // eslint-disable-next-line @typescript-eslint/no-unused-expressions -- only the read, so $effect tracks it
    refetch;
    let isLive = true;
    image = null;
    // Without the measurement, the scale isn't right yet: fetching now would
    // download the thumbnail twice.
    if (f === undefined && noMeasurement) return;
    if (f === undefined) {
      void storedLastSegment(c)
        .then((t) => (t === null ? null : thumbnail(c, t, e)))
        .then((b) => {
          if (isLive && b) image = { source: b, w: b.width, h: b.height };
        })
        .catch(() => {
          // Without a frame, only the drawings remain over the dark background.
        });
    } else if (f) {
      const img = new Image();
      img.onload = () => {
        if (isLive) image = { source: img, w: img.naturalWidth, h: img.naturalHeight };
      };
      img.src = f;
    }
    return () => {
      isLive = false;
    };
  });

  $effect(() => {
    const c = canvas;
    if (width !== undefined || !c) return;
    const ro = new ResizeObserver(() => (measurement = c.clientWidth));
    ro.observe(c);
    return () => {
      ro.disconnect();
    };
  });

  const ratio = $derived(image?.h ? image.w / image.h : 16 / 9);

  // --- o zoom -----------------------------------------------------------------

  // The zoom path, from 0 (the full frame) to 1 (the area's view), and the
  // `amounts` that is drawn: the same path, smoothed at the ends.
  let path = 0;
  let amounts = $state(0);
  let animationFrame = 0;
  // The mouse or finger holding the zoom: it doesn't go back meanwhile.
  let holding = false;
  let back: ReturnType<typeof setTimeout> | undefined;
  let wait: ReturnType<typeof setTimeout> | undefined;

  const suave = (t: number) => (t < 0.5 ? 4 * t ** 3 : 1 - (-2 * t + 2) ** 3 / 2);

  // Takes the zoom to `para`, from wherever it is: going back midway only takes
  // what it already covered. Whoever asks for less motion sees the jump.
  function animate(dest: 0 | 1) {
    cancelAnimationFrame(animationFrame);
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) {
      path = dest;
      amounts = dest;
      animationFrame = 0;
      return;
    }
    let before = performance.now();
    const step = (now: number) => {
      const delta = (now - before) / ZOOM_IDA_MS;
      before = now;
      path = dest ? Math.min(1, path + delta) : Math.max(0, path - delta);
      amounts = suave(path);
      animationFrame = path === dest ? 0 : requestAnimationFrame(step);
    };
    animationFrame = requestAnimationFrame(step);
  }

  // One pass: zooms in, stays ZOOM_FICA_MS and goes back, if nobody holds it.
  function pass() {
    if (!enlargeable) return;
    animate(1);
    clearTimeout(back);
    back = setTimeout(() => {
      if (!holding) animate(0);
    }, ZOOM_IDA_MS + ZOOM_FICA_MS);
  }

  // Holds the zoom while the mouse is over it or the finger is in the long
  // press.
  function holds() {
    if (!enlargeable) return;
    holding = true;
    clearTimeout(back);
    animate(1);
  }
  function release() {
    clearTimeout(wait);
    if (!holding) return;
    holding = false;
    clearTimeout(back);
    back = setTimeout(() => {
      if (!holding) animate(0);
    }, ZOOM_EXIT_MS);
  }

  onDestroy(() => {
    cancelAnimationFrame(animationFrame);
    clearTimeout(back);
    clearTimeout(wait);
  });

  // The first pass, when the thumbnail enters the viewport with the frame
  // already loaded: the hidden camera-form tab, with zero height, doesn't
  // count.
  let isVisible = $state(false);
  let passed = false;
  $effect(() => {
    const c = canvas;
    if (!c || !enlargeable) return;
    const io = new IntersectionObserver(([e]) => (isVisible = !!e?.isIntersecting));
    io.observe(c);
    return () => {
      io.disconnect();
    };
  });
  $effect(() => {
    if (isVisible && image && enlargeable && !passed) {
      passed = true;
      pass();
    }
  });

  // After it, the zoom stays while the mouse rests over whatever contains it -
  // a quick pass, on the way somewhere else, doesn't count - or while a finger
  // holds the long press, and goes back ZOOM_EXIT_MS after it leaves or lets
  // go.
  $effect(() => {
    const host = canvas?.closest('[data-toque-longo]');
    if (!host || !enlargeable) return;
    const enter = (e: Event) => {
      if ((e as PointerEvent).pointerType !== 'mouse') return;
      clearTimeout(wait);
      // Still zoomed in (the mouse came back before it left): hold right away.
      if (path > 0) holds();
      else wait = setTimeout(holds, ZOOM_WAIT_MS);
    };
    const touch = () => {
      holds();
      document.addEventListener('pointerup', release, { once: true });
      document.addEventListener('pointercancel', release, { once: true });
    };
    host.addEventListener('pointerenter', enter);
    host.addEventListener('pointerleave', release);
    host.addEventListener(LONG_TOUCH, touch);
    return () => {
      host.removeEventListener('pointerenter', enter);
      host.removeEventListener('pointerleave', release);
      host.removeEventListener(LONG_TOUCH, touch);
      document.removeEventListener('pointerup', release);
      document.removeEventListener('pointercancel', release);
    };
  });

  const mix = (a: Vista, b: Vista, t: number): Vista => ({
    x: a.x + (b.x - a.x) * t,
    y: a.y + (b.y - a.y) * t,
    w: a.w + (b.w - a.w) * t,
    h: a.h + (b.h - a.h) * t,
  });

  // --- drawing
  // ----------------------------------------------------------------

  // Redraws when the frame arrives, when the width changes, when the areas
  // change (a save) and on every zoom step.
  $effect(() => {
    const c = canvas;
    const g = c?.getContext('2d');
    const wid = width ?? measurement;
    if (!c || !g || !wid) return;
    const dpr = window.devicePixelRatio || 1;
    const w = Math.round(wid * dpr);
    const h = Math.round((wid / ratio) * dpr);
    const v = target && amounts > 0 ? mix(WHOLE_FRAME, target, amounts) : WHOLE_FRAME;
    if (c.width !== w) c.width = w;
    if (c.height !== h) c.height = h;
    g.fillStyle = '#000';
    g.fillRect(0, 0, w, h);
    // The `thumbs.ts` cache closes the strip bitmap it evicts, and all of them
    // in `clearThumbnails`; the closed one has zero width. This thumbnail still
    // kept it: it drops it and asks for the frame again. Until it arrives,
    // `image` stays empty, and the request isn't repeated on every redraw.
    if (image && image.source instanceof ImageBitmap && image.source.width === 0) {
      image = null;
      refetch++;
    }
    try {
      if (image) {
        const { source, w: iw, h: ih } = image;
        g.drawImage(source, v.x * iw, v.y * ih, v.w * iw, v.h * ih, 0, 0, w, h);
      }
    } catch {
      // Without a frame, only the drawings remain over the dark background.
    }
    const draw = drawing === 'cheia' ? fillArea : drawOutline;
    for (const a of areas) draw(g, viewPoints(a.points, v), w, h);
  });
</script>

<canvas
  bind:this={canvas}
  style:width={width === undefined ? '100%' : `${width}px`}
  style:aspect-ratio={ratio}
  style:border-radius="{radius}px"
  aria-hidden="true"
></canvas>

<style>
  canvas {
    display: block;
    flex: none;
    background: #000;
  }
</style>
