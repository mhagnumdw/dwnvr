<script lang="ts">
  // The object detector's boxes layer, on top of the recordings video.
  //
  // A box belongs to ONE frame - the one the detector looked at, `frameMs` - so
  // it isn't on screen all the time: it lights up when playback passes that
  // frame, stays lit LIT_MS and fades out. With the video paused, the lit box
  // stays lit, and starts counting time again when playback resumes.
  //
  // The layout and drawing live in lib/boxes.ts, which the frame capture also
  // uses: this layer only decides WHICH boxes are lit and how much.
  import { onMount, untrack } from 'svelte';
  import type { Box } from '../lib/api.ts';
  import { context2d } from '../lib/canvas.ts';
  import { place, draw, LIGHT_UP_MS, LIT_MS, FADE_MS, LANDING_MS } from '../lib/boxes.ts';
  import type { PlacedBox, ObjectDetectorMark } from '../lib/boxes.ts';

  // A mark with the instant of the inspected frame, when there is one: the ones
  // from /api/rec/events, and the objects of a detection with its `frameMs`.
  type Mark = ObjectDetectorMark & { quadroMs?: number };

  interface Props {
    video: HTMLVideoElement; // o <video> do palco
    objects?: Mark[]; // the day's marks, as /api/rec/events delivers them
    currentMs?: number;
    playing?: boolean;
    rate?: number;
    labels?: boolean;
  }

  // The mark that has something to light up: the box and the inspected frame.
  type LightableMark = Mark & { caixa: Box; quadroMs: number };

  interface Lit {
    mark: LightableMark;
    desde: number;
  }

  let {
    video,
    objects = [],
    currentMs = 0,
    playing = false,
    rate = 1,
    labels = true,
  }: Props = $props();

  let canvas: HTMLCanvasElement | undefined;
  // Where the <video> is in the stage. It is centered and has a maximum height,
  // so it doesn't always start at the stage's corner.
  let element = $state({ left: 0, top: 0, w: 0, h: 0 });
  // And where the IMAGE is inside it: with `object-fit: contain`, a camera that
  // isn't 16:9 gets black bars, and the box fraction is relative to the image.
  let image = { x: 0, y: 0, w: 0, h: 0 };

  // The lit boxes: key -> { mark, since }, with `since` on the
  // performance.now() clock. Deliberately not reactive: the canvas does the
  // drawing, and redrawing is requested by hand.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- the canvas does the drawing
  const lit = new Map<string, Lit>();
  let pausedSince: number | null = null;
  let items: PlacedBox<LightableMark>[] = [];
  let placed = '';
  let requestNo = 0;

  // An inference writes one row per object, and two of the same family in the
  // same frame can only be told apart by the box. Frame and family aren't
  // enough: the second person would overwrite the first, and only one box would
  // light up.
  const key = (o: LightableMark) => `${o.quadroMs}|${o.familia}|${o.caixa.join(',')}`;
  const LIFE_MS = LIGHT_UP_MS + LIT_MS + FADE_MS;

  // Only the marks that have a box and the inspected frame's instant: without
  // both there is nothing to light up, nor when.
  const withBox = $derived(objects.filter((o): o is LightableMark => !!o.caixa && !!o.quadroMs));

  // --- when to light up -----------------------------------------------------

  let lastMs = 0;
  let lastClock = 0;

  $effect(() => {
    const t = currentMs;
    untrack(() => {
      advance(t);
    });
  });

  function advance(t: number) {
    const now = performance.now();
    // While playing, the video moves between two updates about the clock time
    // times the speed. Moving much more than that, or backward, is a JUMP -
    // seek, tap on the timeline, day switch - and a jump doesn't "pass" through
    // any frame: skipping an hour must not light up the boxes of the whole
    // hour.
    const elapsed = Math.min(now - lastClock, 2000);
    const leap = t - lastMs;
    const moved =
      playing && lastClock > 0 && leap >= 0 && leap <= elapsed * Math.max(1, rate) * 1.5 + 500;

    if (moved) {
      for (const o of withBox) {
        if (o.quadroMs > lastMs && o.quadroMs <= t) lit.set(key(o), { mark: o, desde: now });
      }
    } else {
      // What was lit belonged to the clip we jumped from.
      lit.clear();
      for (const o of withBox) {
        if (o.quadroMs <= t && o.quadroMs >= t - LANDING_MS)
          lit.set(key(o), { mark: o, desde: now });
      }
    }
    lastMs = t;
    lastClock = now;
    agenda();
  }

  $effect(() => {
    const playingNow = playing;
    untrack(() => {
      const now = performance.now();
      if (!playingNow && pausedSince === null) {
        deleteExpired(now);
        pausedSince = now;
      } else if (playingNow && pausedSince !== null) {
        // Paused time doesn't count: the box resumes from where it was.
        for (const a of lit.values()) a.desde += now - pausedSince;
        pausedSince = null;
      }
      lastClock = now;
      agenda();
    });
  });

  $effect(() => {
    // eslint-disable-next-line @typescript-eslint/no-unused-expressions -- only the read, so $effect tracks it
    labels;
    untrack(agenda);
  });

  function deleteExpired(now: number) {
    for (const [k, a] of lit) if (now - a.desde >= LIFE_MS) lit.delete(k);
  }

  function alpha(a: Lit | undefined, now: number) {
    if (!a) return 0;
    if (pausedSince !== null) return 1;
    const age = now - a.desde;
    if (age < LIGHT_UP_MS) return age / LIGHT_UP_MS;
    if (age < LIGHT_UP_MS + LIT_MS) return 1;
    return Math.max(0, 1 - (age - LIGHT_UP_MS - LIT_MS) / FADE_MS);
  }

  // --- drawing --------------------------------------------------------------

  function agenda() {
    if (!requestNo) requestNo = requestAnimationFrame(frame);
  }

  function frame() {
    requestNo = 0;
    const now = performance.now();
    if (pausedSince === null) deleteExpired(now);
    paint(now);
    // Only requests the next frame while some box is changing opacity. Paused,
    // or with no box lit, the layer costs nothing.
    if (lit.size && pausedSince === null) agenda();
  }

  // The layout is only redone when the lit set, the size or the labels change:
  // redoing it on every frame would make a label switch sides in the middle of
  // its neighbor's fade-out.
  function placement(g: CanvasRenderingContext2D) {
    const k = `${[...lit.keys()].sort().join(',')}|${image.w}x${image.h}|${labels}`;
    if (k !== placed) {
      items = place(
        g,
        [...lit.values()].map((a) => a.mark),
        image.w,
        image.h,
        labels,
      );
      placed = k;
    }
    return items;
  }

  function paint(now: number) {
    if (!canvas) return;
    const dpr = window.devicePixelRatio || 1;
    const { w, h } = element;
    const cw = Math.round(w * dpr);
    const ch = Math.round(h * dpr);
    if (canvas.width !== cw || canvas.height !== ch) {
      canvas.width = cw;
      canvas.height = ch;
    }
    const g = context2d(canvas);
    g.setTransform(dpr, 0, 0, dpr, 0, 0);
    g.clearRect(0, 0, w, h);
    if (!lit.size || !image.w) return;
    g.translate(image.x, image.y);
    draw(g, placement(g), (it) => alpha(lit.get(key(it.mark)), now));
  }

  // drawOn paints the lit boxes now over the downloaded image, which has the
  // camera's NATIVE size. The layout is the screen's, just scaled up: the image
  // comes out as the screen is, with the label in the same place and at the
  // same relative size. Opacity goes full - a half-faded box in a photo looks
  // like a defect, not a fade.
  export function drawOn(g: CanvasRenderingContext2D, width: number, height: number) {
    if (!lit.size) return;
    if (!image.w) {
      draw(
        g,
        place(
          g,
          [...lit.values()].map((a) => a.mark),
          width,
          height,
          labels,
        ),
      );
      return;
    }
    g.save();
    g.scale(width / image.w, height / image.h);
    draw(g, placement(g));
    g.restore();
  }

  // --- size -----------------------------------------------------------------

  function measure() {
    const w = video.clientWidth;
    const h = video.clientHeight;
    element = { left: video.offsetLeft, top: video.offsetTop, w, h };
    const vw = video.videoWidth;
    const vh = video.videoHeight;
    if (!vw || !vh || !w || !h) {
      image = { x: 0, y: 0, w: 0, h: 0 };
    } else {
      const s = Math.min(w / vw, h / vh);
      image = { x: (w - vw * s) / 2, y: (h - vh * s) / 2, w: vw * s, h: vh * s };
    }
    agenda();
  }

  onMount(() => {
    const ro = new ResizeObserver(measure);
    ro.observe(video);
    // The resolution arrives after the element, and changes when the camera
    // changes.
    video.addEventListener('loadedmetadata', measure);
    video.addEventListener('resize', measure);
    measure();
    return () => {
      ro.disconnect();
      video.removeEventListener('loadedmetadata', measure);
      video.removeEventListener('resize', measure);
      cancelAnimationFrame(requestNo);
    };
  });
</script>

<canvas
  bind:this={canvas}
  style:left="{element.left}px"
  style:top="{element.top}px"
  style:width="{element.w}px"
  style:height="{element.h}px"
></canvas>

<style>
  canvas {
    position: absolute;
    pointer-events: none;
  }
</style>
