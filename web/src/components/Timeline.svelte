<script lang="ts">
  import type { MarkedObject, Span } from '../lib/api.ts';
  import { context2d } from '../lib/canvas.ts';
  import { tokenColor } from '../lib/theme.ts';
  import { hhmm, hhmmss } from '../lib/format.ts';
  import { family, iconOf, drawIcon, CLASS_NAME } from '../lib/icons.ts';
  import type { IconName } from '../lib/icons.ts';

  interface Props {
    ranges?: Span[];
    events?: number[];
    objects?: MarkedObject[];
    dayStart?: number;
    dayEnd?: number;
    currentMs?: number;
    onseek?: (ms: number) => void;
    viewFrom?: number;
    viewTo?: number;
  }

  // Marks from the same instant, which become ONE split mark.
  interface Group {
    t: number;
    marks: MarkedObject[];
    priority: number;
  }

  let {
    ranges = [], // [[startMs, endMs], …] ranges with recording
    // Instants at which the trigger saw motion, ascending. Empty means "there
    // is nothing to show" - either because the camera has detection off, or
    // because the day was quiet - and in that case the heat strip is neither
    // drawn NOR takes up height: a permanently empty strip would be worse than
    // nothing for whoever doesn't use detection.
    events = [],
    // The marks the detector confirmed: [{ instantMs, family, class }, …], in
    // any order. A mark is an INSTANT - the onset that originated it - not an
    // interval: the detector looks once per arrival, so it doesn't know when
    // the object left. Empty follows the heat strip's rule: neither drawn nor
    // takes up height.
    objects = [],
    dayStart = 0,
    dayEnd = 0,
    currentMs = 0,
    onseek,
    // Visible window: the only state that zoom and drag change. It goes up to
    // the caller because it is what goes into the URL - the link has to
    // reproduce the zoom, not just the instant. Zero in both means "not chosen
    // yet", and is what makes the effect below anchor to the whole day.
    viewFrom = $bindable(0),
    viewTo = $bindable(0),
  }: Props = $props();

  let canvas: HTMLCanvasElement | undefined;
  let width = $state(0);
  let hoverMs = $state<number | null>(null);
  // The mark under the pointer, when it is within a target of it: it is what
  // the hint describes and what the pointer line points at.
  let hoverGroup = $state<Group | null>(null);

  const MIN_SPAN = 20_000; // 20s max zoom

  // The STACK, from top to bottom: object, video, motion - each height means
  // one thing only. The bar and ruler numbers are the ones it has always had;
  // the heat strip enters from below and the object one from above, each only
  // when there is something to show.
  const TOP_PX = 6;
  // Where the icon rests, above the object line. Fixed, not the icon's size:
  // the class icon is larger than the family one, and with the zone growing the
  // bar would drop in the middle of a zoom.
  const ZONE_PX = 24;
  const OBJ_PX = 10; // the object line
  const BAR_PX = 36;
  const GAP_PX = 3; // breathing room between the bar and each neighboring strip
  const HEAT_PX = 10;
  const RULER_PX = 20; // what is left at the bottom, for the time labels

  // The drawing of the object mark: a tick on the line, and the icon on top
  // ALWAYS when it fits. A camera marks dozens of objects per day, not
  // hundreds, and the icon fits during a good part of the day. What doesn't fit
  // stays as just the tick - the color already tells the family.
  const TICK_PX = 3;
  const FAMILY_ICON_PX = 14;
  // At 20px the class icon tells a motorcycle from a bicycle and a cat from a
  // dog; at 14px they become the same drawing. That is why the class only comes
  // in up close.
  const CLASS_ICON_PX = 20;
  const CLASS_UNTIL_MS = 20 * 60_000;
  const ICON_SLACK_PX = 4; // breathing room between two neighboring icons
  // Distance at which a touch "snaps" to a mark and goes to its instant,
  // instead of the touched pixel. A finger is imprecise, a mouse isn't - the
  // same reasoning as the drag threshold further down.
  const MOUSE_TARGET_PX = 8;
  const FINGER_TARGET_PX = 16;

  // The colors come from the theme's tokens (lib/theme.ts), read once per token
  // in each drawing: a day has thousands of ticks, and the theme can change
  // with the screen open.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- a cache of the drawing, which the screen does not read
  const colors = new Map<string, string>();
  function color(token: `--${string}`): string {
    let v = colors.get(token);
    if (v === undefined) {
      v = canvas ? tokenColor(canvas, token) : '';
      colors.set(token, v);
    }
    return v;
  }

  // The intensity is ADAPTIVE: the peak of the visible window becomes the top
  // of the scale. The alternative was a fixed scale, comparable across screens
  // and cameras - it lost because on a quiet camera it paints the whole day the
  // same gray, which is exactly when the strip needed to be saying something.
  // The price, accepted: the same color means different densities at different
  // zooms.
  //
  // The alpha floor exists so a column with ONE mark still shows up; the square
  // root opens up the middle of the scale, where the difference between 2 and 4
  // marks would be lost if the ramp were linear. The color is --warn.
  const HEAT_ALPHA_MIN = 0.3;

  // Re-anchor when the day changes; comparing the bounds avoids re-anchoring on
  // every repaint and losing the zoom the user just applied. The span floor
  // plays the same role for a window coming from the URL: `zoomTo` never
  // produces less than MIN_SPAN, but a hand-edited link does - and then the max
  // zoom, which is this timeline's rule, would hold only for whoever arrived by
  // clicking.
  $effect(() => {
    const visibleWindow = viewTo - viewFrom;
    if (
      dayStart &&
      (viewFrom < dayStart || viewTo > dayEnd || viewTo === 0 || visibleWindow < MIN_SPAN)
    ) {
      viewFrom = dayStart;
      viewTo = dayEnd;
    }
  });

  const hasHeat = $derived(events.length > 0);
  const hasObjects = $derived(objects.length > 0);
  const objTop = TOP_PX + ZONE_PX;
  const barTop = $derived(hasObjects ? objTop + OBJ_PX + GAP_PX : TOP_PX);
  const canvasHeight = $derived(barTop + BAR_PX + (hasHeat ? GAP_PX + HEAT_PX : 0) + RULER_PX);

  // The marks grouped by instant, ascending. An inference can confirm more than
  // one family in the same frame - person and motorcycle arrive together, and
  // on a street that is common - and the group is drawn as ONE split mark, not
  // as two that cover each other. Within the group, the highest-priority family
  // comes first.
  //
  // The timeline marks arrival per family: two people at the same instant are
  // two recorded rows, but one `person` arrival, with the highest confidence -
  // its class is the one the icon and tooltip show.
  const groups = $derived.by((): Group[] => {
    const out: { t: number; marks: MarkedObject[] }[] = [];
    for (const o of [...objects].sort((a, b) => a.atMs - b.atMs)) {
      const ult = out.at(-1);
      if (ult?.t !== o.atMs) {
        out.push({ t: o.atMs, marks: [o] });
        continue;
      }
      const i = ult.marks.findIndex((m) => m.family === o.family);
      const same = ult.marks[i];
      if (!same) ult.marks.push(o);
      else if (o.score > same.score) ult.marks[i] = o;
    }
    return out.map((gr) => {
      const priorities = gr.marks.map((m) => family(m.family).priority);
      gr.marks.sort((a, b) => family(a.family).priority - family(b.family).priority);
      return { ...gr, priority: Math.min(...priorities) };
    });
  });

  const span = $derived(Math.max(1, viewTo - viewFrom));
  const toX = (ms: number) => ((ms - viewFrom) / span) * width;
  const toMs = (x: number) => viewFrom + (x / Math.max(1, width)) * span;

  // Step of the hour marks: chosen so the labels never touch, whether on a
  // 360px phone or a wide monitor.
  const step = $derived.by(() => {
    const larger = 216e5;
    const opts = [1e3, 5e3, 15e3, 60e3, 3e5, 9e5, 18e5, 36e5, 108e5, larger];
    const minPx = width < 500 ? 56 : 78;
    return opts.find((s) => (s / span) * width >= minPx) ?? larger;
  });

  $effect(() => {
    // Redraws whenever any of these changes.
    /* eslint-disable @typescript-eslint/no-unused-expressions -- only the read, so $effect tracks it */
    ranges;
    events;
    groups;
    viewFrom;
    viewTo;
    currentMs;
    width;
    hoverMs;
    canvasHeight;
    /* eslint-enable @typescript-eslint/no-unused-expressions */
    draw();
  });

  /*
   * How many marks fall in each pixel column of the visible window.
   *
   * Counting per column, rather than drawing mark by mark, is what lets the
   * strip handle the whole day: at 1200px a day is 72 s per column, and one
   * stroke per mark would turn into a solid blur that can't tell "someone
   * passed by" from "it rained all afternoon". The DENSITY is the information;
   * the loose stroke isn't.
   *
   * The binary search avoids walking the thousands of marks of the day on every
   * repaint - and there is a repaint on every pixel of dragging.
   */
  function countPerColumn() {
    const cols = new Int32Array(Math.max(1, Math.ceil(width)));
    for (let i = lowerLimit(events, viewFrom); i < events.length; i++) {
      const e = events[i];
      if (e === undefined || e >= viewTo) break;
      const x = Math.floor(toX(e));
      if (x >= 0 && x < cols.length) cols[x] = (cols[x] ?? 0) + 1;
    }
    return cols;
  }

  function lowerLimit(arr: number[], v: number) {
    let lo = 0;
    let hi = arr.length;
    while (lo < hi) {
      const m = (lo + hi) >> 1;
      if ((arr[m] ?? v) < v) lo = m + 1;
      else hi = m;
    }
    return lo;
  }

  /*
   * A group's tick on the object line. Two families at the same instant split
   * the tick into stripes, the highest-priority one on top.
   */
  function tick(g: CanvasRenderingContext2D, gr: Group) {
    const x = toX(gr.t) - TICK_PX / 2;
    const h = OBJ_PX / gr.marks.length;
    gr.marks.forEach((m, i) => {
      g.fillStyle = color(family(m.family).token);
      g.fillRect(x, objTop + i * h, TICK_PX, h);
    });
  }

  /*
   * Where each icon goes, with no two overlapping.
   *
   * It walks by PRIORITY, not by time: `person` picks its place before all the
   * others, so a neighboring cat never takes a person's icon. A group of two
   * families asks for room for both icons side by side; without space, it tries
   * at least the main family's. What didn't fit stays in the tick.
   */
  function fit(visibleOnes: Group[], side: number, byClass: boolean) {
    const order = [...visibleOnes].sort((a, b) => a.priority - b.priority || a.t - b.t);
    const occupied: [number, number][] = [];
    const placements: { name: IconName; x: number; color: string }[] = [];
    const free = (x0: number, x1: number) =>
      occupied.every(([a, b]) => x1 + ICON_SLACK_PX <= a || x0 >= b + ICON_SLACK_PX);
    for (const gr of order) {
      const attempts = gr.marks.length > 1 ? [gr.marks, gr.marks.slice(0, 1)] : [gr.marks];
      for (const marks of attempts) {
        const wid = marks.length * side + (marks.length - 1) * 2;
        const x0 = toX(gr.t) - wid / 2;
        if (!free(x0, x0 + wid)) continue;
        occupied.push([x0, x0 + wid]);
        marks.forEach((m, i) => {
          const name = iconOf(m, byClass);
          if (name)
            placements.push({ name, x: x0 + i * (side + 2), color: color(family(m.family).token) });
        });
        break;
      }
    }
    return placements;
  }

  function drawObjects(g: CanvasRenderingContext2D) {
    g.fillStyle = color('--track');
    g.fillRect(0, objTop, width, OBJ_PX);

    // A little beyond the edges, so the icon of a mark just outside the window
    // comes in halfway instead of jumping in all at once.
    const visibleOnes = groups.filter((gr) => {
      const x = toX(gr.t);
      return x > -CLASS_ICON_PX && x < width + CLASS_ICON_PX;
    });

    // Lowest priority first: where two ticks fall on the same pixel, the
    // `person` one is painted last, and stays on top.
    for (const gr of [...visibleOnes].sort((a, b) => b.priority - a.priority)) tick(g, gr);

    const byClass = span <= CLASS_UNTIL_MS;
    const side = byClass ? CLASS_ICON_PX : FAMILY_ICON_PX;
    for (const p of fit(visibleOnes, side, byClass)) {
      drawIcon(g, p.name, p.x, objTop - side - 2, side, p.color);
    }
  }

  /* The mark within `alvo` px of x, or null. On a tie, the highest-priority one. */
  function nearGroup(x: number, target: number) {
    let best: Group | null = null;
    let dist = Infinity;
    for (const gr of groups) {
      const d = Math.abs(toX(gr.t) - x);
      if (d > target) continue;
      if (d < dist || (d === dist && best && gr.priority < best.priority)) {
        best = gr;
        dist = d;
      }
    }
    return best;
  }

  function describe(gr: Group) {
    return gr.marks.map((m) => CLASS_NAME[m.class] ?? m.class).join(' + ');
  }

  function draw() {
    if (!canvas || !width) return;
    const dpr = window.devicePixelRatio || 1;
    const h = canvasHeight;
    canvas.width = width * dpr;
    canvas.height = h * dpr;

    const g = context2d(canvas);
    g.setTransform(dpr, 0, 0, dpr, 0, 0);
    colors.clear();

    const barH = BAR_PX;
    const heatTop = barTop + barH + GAP_PX;
    // The cursor line and the pointer line cross the whole stack: they mark ONE
    // instant, and an instant is worth the same in all three layers.
    const stackTop = hasObjects ? objTop : barTop;
    const stackH = (hasHeat ? heatTop + HEAT_PX : barTop + barH) - stackTop;

    if (hasObjects) drawObjects(g);

    g.fillStyle = color('--track');
    g.fillRect(0, barTop, width, barH);

    g.fillStyle = color('--accent');
    for (const [s, e] of ranges) {
      const x0 = Math.max(0, toX(s));
      const x1 = Math.min(width, toX(e));
      // Minimum of 2px: a short recording can't vanish from the bar just
      // because the zoom is zoomed out.
      if (x1 > 0 && x0 < width) g.fillRect(x0, barTop, Math.max(2, x1 - x0), barH);
    }

    if (hasHeat) {
      const cols = countPerColumn();
      let ceiling = 1;
      for (const c of cols) if (c > ceiling) ceiling = c;

      g.fillStyle = color('--track');
      g.fillRect(0, heatTop, width, HEAT_PX);
      g.fillStyle = color('--warn');
      for (let x = 0; x < cols.length; x++) {
        const c = cols[x];
        if (!c) continue;
        g.globalAlpha = HEAT_ALPHA_MIN + (1 - HEAT_ALPHA_MIN) * Math.sqrt(Math.min(1, c / ceiling));
        g.fillRect(x, heatTop, 1, HEAT_PX);
      }
      g.globalAlpha = 1;
    }

    g.fillStyle = color('--dim');
    g.font = '10px system-ui, sans-serif';
    g.textBaseline = 'top';
    const first = Math.ceil(viewFrom / step) * step;
    for (let t = first; t < viewTo; t += step) {
      const x = toX(t);
      g.fillRect(x, h - 18, 1, 5);
      g.fillText(step >= 60e3 ? hhmm(t) : hhmmss(t), x + 3, h - 13);
    }

    if (hoverMs !== null) {
      const x = toX(hoverMs);
      g.fillStyle = color('--fg');
      g.globalAlpha = 0.35;
      g.fillRect(x, stackTop, 1, stackH);
      g.globalAlpha = 1;
    }

    if (currentMs) {
      const x = toX(currentMs);
      if (x >= -2 && x <= width + 2) {
        g.fillStyle = color('--bad');
        g.fillRect(x - 1.5, stackTop - 3, 3, stackH + 6);
      }
    }
  }

  // --- interaction ---------------------------------------------------------
  //
  // Active pointers: one drags the window, two pinch to zoom. Using Pointer
  // Events covers mouse, finger and pen with the same code. Each pointer also
  // keeps its initial x: the drag threshold needs the accumulated
  // displacement, not the last event's step.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- gesture state, not screen state
  const pointers = new Map<number, { x: number; x0: number }>();
  let dragged = false;
  let pinchStart: {
    dist: number;
    from: number;
    to: number;
    mid: number;
    anchorMs: number;
    t: number;
    moved: boolean;
  } | null = null;
  let lastTap: { t: number; x: number } | null = null; // the last clean tap, for the double tap
  let swallowUp = false; // the 2nd finger's up must not become a seek

  const DOUBLE_TAP_MS = 300;
  const DOUBLE_TAP_PX = 32;
  const ZOOM_STEP = 3; // a deliberate gesture deserves a bigger jump than the wheel
  // Wheel zoom proportional to deltaY: a mouse click (~100px) gives the 1.35
  // step, and the touchpad, which sends dozens of small deltas, adds up to the
  // same zoom in proportion to the finger movement instead of a full step per
  // event.
  const WHEEL_ZOOM_PER_PX = Math.log(1.35) / 100;
  const WHEEL_PX_PER_LINE = 33; // Firefox may send the delta in lines
  const WHEEL_DELTA_MAX_PX = 300; // an isolated event never jumps more than ~2.5x or ~¼ of the screen

  function localX(ev: MouseEvent) {
    return ev.clientX - (canvas?.getBoundingClientRect().left ?? 0);
  }

  function onPointerDown(ev: PointerEvent) {
    canvas?.setPointerCapture(ev.pointerId);
    const x = localX(ev);
    pointers.set(ev.pointerId, { x, x0: x });
    dragged = false;
    const [a, b] = [...pointers.values()];
    if (pointers.size === 2 && a && b) {
      const mid = (a.x + b.x) / 2;
      pinchStart = {
        dist: Math.abs(a.x - b.x),
        from: viewFrom,
        to: viewTo,
        mid,
        anchorMs: toMs(mid), // the instant under the fingers is what stays still
        t: ev.timeStamp,
        moved: false,
      };
      lastTap = null; // two fingers never continue a double tap
    }
  }

  function onPointerMove(ev: PointerEvent) {
    const p = pointers.get(ev.pointerId);
    if (!p) {
      if (ev.pointerType === 'mouse') {
        const x = localX(ev);
        hoverGroup = nearGroup(x, MOUSE_TARGET_PX);
        hoverMs = hoverGroup ? hoverGroup.t : toMs(x);
      }
      return;
    }
    const x = localX(ev);
    const prev = p.x;
    p.x = x;

    const [a, b] = [...pointers.values()];
    if (pointers.size === 2 && pinchStart && a && b) {
      const dist = Math.max(1, Math.abs(a.x - b.x));
      const mid = (a.x + b.x) / 2;
      if (Math.abs(dist - pinchStart.dist) > 8 || Math.abs(mid - pinchStart.mid) > 8) {
        pinchStart.moved = true;
      }
      const factor = pinchStart.dist / dist;
      // Anchor between the fingers, as the wheel anchors on the cursor. Using
      // the live mid as the fraction gives drag-while-pinching for free.
      zoomTo(pinchStart.anchorMs, (pinchStart.to - pinchStart.from) * factor, mid / width);
      dragged = true;
      return;
    }

    const dx = x - prev;
    // A finger trembles: 1px would count as a drag and cancel the seek. A mouse
    // is precise.
    const slop = ev.pointerType === 'mouse' ? 2 : 8;
    if (Math.abs(x - p.x0) > slop) dragged = true;
    pan((-dx / Math.max(1, width)) * span);
  }

  function onPointerUp(ev: PointerEvent) {
    const p = pointers.get(ev.pointerId);
    const x = p ? p.x : localX(ev);
    pointers.delete(ev.pointerId);

    // Quick tap with two fingers, without pinching or dragging: zooms out one
    // step.
    if (pinchStart) {
      const { mid, moved, t } = pinchStart;
      pinchStart = null;
      lastTap = null;
      swallowUp = true;
      if (!moved && ev.timeStamp - t < DOUBLE_TAP_MS) {
        zoomTo(toMs(mid), span * ZOOM_STEP, mid / width);
      }
      return;
    }
    if (swallowUp) {
      swallowUp = pointers.size > 0;
      return;
    }

    // A drag must not become a jump: only a clean tap navigates.
    if (dragged) {
      lastTap = null;
      return;
    }

    // Double tap: the 1st already jumped to the point, the 2nd zooms in
    // anchored there - so no gesture needs to wait to find out whether a second
    // tap is coming.
    if (
      lastTap &&
      ev.timeStamp - lastTap.t < DOUBLE_TAP_MS &&
      Math.abs(x - lastTap.x) < DOUBLE_TAP_PX
    ) {
      lastTap = null;
      zoomTo(toMs(x), span / ZOOM_STEP, x / width);
      return;
    }

    // On desktop the wheel already handles zoom, and repeated clicking to
    // adjust the position must not turn into a surprise zoom.
    if (ev.pointerType !== 'mouse') lastTap = { t: ev.timeStamp, x };
    // A tap that lands on an object mark goes to ITS instant: hitting the pixel
    // of a 3px tick with a finger can't be the condition for reaching it.
    const gr = nearGroup(x, ev.pointerType === 'mouse' ? MOUSE_TARGET_PX : FINGER_TARGET_PX);
    onseek?.(gr ? gr.t : Math.round(toMs(x)));
  }

  function onWheel(ev: WheelEvent) {
    ev.preventDefault();
    let dx = wheelPx(ev, ev.deltaX);
    let dy = wheelPx(ev, ev.deltaY);
    // Shift + wheel is the ordinary mouse's horizontal; not every browser
    // already delivers it swapped to deltaX.
    if (ev.shiftKey && dx === 0) [dx, dy] = [dy, 0];

    // A finger never moves straight: applying both axes together would make the
    // window zoom when you only want to move in time. Each event counts for its
    // dominant axis.
    if (Math.abs(dx) > Math.abs(dy)) {
      // Panning is proportional to the zoom: the on-screen displacement becomes
      // time at the same scale as the drag, so the content follows the finger.
      pan((dx / Math.max(1, width)) * span);
      return;
    }
    const x = localX(ev);
    zoomTo(toMs(x), span * Math.exp(dy * WHEEL_ZOOM_PER_PX), x / width);
  }

  function wheelPx(ev: WheelEvent, d: number) {
    if (ev.deltaMode === WheelEvent.DOM_DELTA_LINE) d *= WHEEL_PX_PER_LINE;
    else if (ev.deltaMode === WheelEvent.DOM_DELTA_PAGE) d *= width;
    return Math.max(-WHEEL_DELTA_MAX_PX, Math.min(WHEEL_DELTA_MAX_PX, d));
  }

  function zoomTo(anchorMs: number, nextSpan: number, frac = 0.5) {
    const total = dayEnd - dayStart;
    const s = Math.min(total, Math.max(MIN_SPAN, nextSpan));
    viewFrom = anchorMs - frac * s;
    viewTo = viewFrom + s;
    clamp();
  }

  function pan(deltaMs: number) {
    viewFrom += deltaMs;
    viewTo += deltaMs;
    clamp();
  }

  // The window never leaves the day: navigating into the void confuses more
  // than it helps.
  function clamp() {
    const s = viewTo - viewFrom;
    if (viewFrom < dayStart) {
      viewFrom = dayStart;
      viewTo = dayStart + s;
    }
    if (viewTo > dayEnd) {
      viewTo = dayEnd;
      viewFrom = dayEnd - s;
    }
  }

  export function reset() {
    viewFrom = dayStart;
    viewTo = dayEnd;
  }
</script>

<div class="wrapper" bind:clientWidth={width}>
  <canvas
    bind:this={canvas}
    style:height="{canvasHeight}px"
    onpointerdown={onPointerDown}
    onpointermove={onPointerMove}
    onpointerup={onPointerUp}
    onpointercancel={onPointerUp}
    onpointerleave={() => {
      hoverMs = null;
      hoverGroup = null;
    }}
    onwheel={onWheel}
  ></canvas>

  {#if hoverMs !== null}
    <!-- The edge margin grows with the text: a mark's hint is longer than a
         time's, and would spill out the side. -->
    {@const margin = hoverGroup ? 90 : 28}
    <span
      class="tip mono"
      style:left="{Math.min(Math.max(toX(hoverMs), margin), width - margin)}px"
    >
      {hhmmss(hoverMs)}{#if hoverGroup}&nbsp;· {describe(hoverGroup)}{/if}
    </span>
  {/if}
</div>

<style>
  .wrapper {
    position: relative;
    width: 100%;
  }

  canvas {
    width: 100%;
    /* The height comes from the script: it changes depending on whether the
       heat strip is present. */
    display: block;
    border-radius: 8px;
    cursor: crosshair;
    /* Stops the browser from scrolling the page while the timeline is being dragged. */
    touch-action: none;
  }

  .tip {
    position: absolute;
    top: -22px;
    transform: translateX(-50%);
    background: var(--panel-2);
    border: 1px solid var(--line);
    border-radius: 6px;
    padding: 1px 6px;
    font-size: 11px;
    pointer-events: none;
    white-space: nowrap;
  }
</style>
