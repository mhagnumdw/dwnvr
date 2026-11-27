<!--
  The area editor: a free-form polygon drawn over a camera frame. Each tap adds
  a vertex, tapping the first one closes the area, and dragging a vertex adjusts
  it.

  On a phone it takes the whole screen: in portrait, the frame on top and the
  controls below; in landscape, the controls in a column on the right. On
  desktop it is a modal.

  It ships in a separate chunk (`import()`), so whoever doesn't draw an area
  doesn't download it. The camera form (Areas tab) opens it with the area name,
  the camera's other areas in gray, "apagar área" and "atualizar o quadro".
  Detections opens it with the camera's areas in gray and, for the admin,
  "salvar na câmera", which asks for the name and saves the area to the camera
  config.
-->
<script lang="ts">
  import { onMount, tick, untrack } from 'svelte';
  import Icon from './Icon.svelte';
  import type { Box } from '../lib/api.ts';
  import { RULES, MIN_POINTS, MAX_POINTS, MAX_AREAS, svgPoints } from '../lib/area.ts';
  import type { Point, Rule } from '../lib/area.ts';

  interface Props {
    camera: string;
    // The background frame: the JPEG of a detection, or the single-frame MP4 of
    // a segment's keyframe. Null while the screen is still looking for which
    // one.
    background: { kind: 'image' | 'video'; url: string } | null;
    // The boxes of the background detection, only to guide the drawing.
    boxes?: { box: Box; color: string }[];
    initial?: { points: Point[]; rule: Rule } | null;
    error?: string;
    // With a string, the name becomes a field, required and different from
    // `namesInUse`.
    name?: string | null;
    namesInUse?: string[];
    // The camera's other areas, in gray, so you don't draw one on top of
    // another by accident.
    otherAreas?: { name: string; points: Point[] }[];
    // When the background frame is from, and how to ask for a newer one.
    frame?: { caption: string; refreshing: boolean; onrefresh: () => void } | null;
    title?: string;
    applyLabel?: string;
    onapply: (points: Point[], rule: Rule, name: string) => void;
    // The "salvar na câmera" action: asks for the name and saves. Rejects with
    // the message the editor shows. `full` means the camera already has
    // MAX_AREAS areas.
    save?: {
      full: boolean;
      onsave: (points: Point[], rule: Rule, name: string) => Promise<void>;
    } | null;
    ondelete?: (() => void) | null;
    onclose: () => void;
  }

  let {
    camera,
    background,
    boxes = [],
    initial = null,
    error = '',
    name = null,
    namesInUse = [],
    otherAreas = [],
    frame = null,
    title = 'Área',
    applyLabel = 'aplicar',
    onapply,
    save = null,
    ondelete = null,
    onclose,
  }: Props = $props();

  // The finger target around a vertex, in screen px: 22 is half of the 44 touch
  // target in app.css.
  const TOUCH_RADIUS_PX = 22;
  // The same breakpoint as the rest of the UI.
  const DESKTOP_PX = 720;

  // The editor starts from the initial value and doesn't follow it afterward.
  const init = untrack(() => initial);
  let points = $state<Point[]>(init ? init.points.map((p): Point => [p[0], p[1]]) : []);
  let isClosed = $state(!!init && init.points.length >= MIN_POINTS);
  let rule = $state<Rule>(init?.rule ?? 'foot');
  // The name is a field from the start in the camera form, and only after
  // "salvar na câmera" in Detections.
  const nameFromStart = untrack(() => name !== null);
  let askingName = $state(false);
  const withName = $derived(nameFromStart || askingName);
  let saving = $state(false);
  let saveError = $state('');
  let currentName = $state(untrack(() => name ?? ''));
  let undoStack = $state<{ points: Point[]; isClosed: boolean }[]>([]);
  let drag: number | null = null;

  let mode = $state<'portrait' | 'landscape' | 'desktop'>('portrait');
  function measure() {
    const w = window.innerWidth;
    const h = window.innerHeight;
    mode = w >= DESKTOP_PX && h >= 520 ? 'desktop' : w > h ? 'landscape' : 'portrait';
  }

  // The stage has the frame's aspect ratio and fits entirely in the remaining
  // space.
  let ratio = $state(16 / 9);
  let space = $state({ w: 0, h: 0 });
  const stage = $derived.by(() => {
    const { w, h } = space;
    if (!w || !h) return { w: 0, h: 0 };
    const pw = Math.min(w, h * ratio);
    return { w: pw, h: pw / ratio };
  });

  const drawn = $derived(isClosed && points.length >= MIN_POINTS);
  // The same rule as the server (config.checkAreas): case-insensitive.
  const nameError = $derived.by(() => {
    if (!withName) return '';
    const n = currentName.trim().toLowerCase();
    if (!n) return 'Dê um nome à área.';
    if (namesInUse.some((u) => u.trim().toLowerCase() === n))
      return 'Já existe uma área com esse nome.';
    return '';
  });
  const validate = $derived(drawn && !nameError);

  let svg = $state<SVGSVGElement | null>(null);

  function point(e: PointerEvent): Point {
    const r = svg?.getBoundingClientRect();
    if (!r?.width || !r.height) return [0, 0];
    const lim = (v: number) => Math.min(1, Math.max(0, v));
    return [lim((e.clientX - r.left) / r.width), lim((e.clientY - r.top) / r.height)];
  }

  // The vertex closest to the touch, within the radius, in screen px.
  function near(p: Point) {
    let best = -1;
    let dist = TOUCH_RADIUS_PX;
    for (const [i, q] of points.entries()) {
      const d = Math.hypot((q[0] - p[0]) * stage.w, (q[1] - p[1]) * stage.h);
      if (d <= dist) {
        dist = d;
        best = i;
      }
    }
    return best;
  }

  function snapshot() {
    undoStack.push({ points: points.map((p): Point => [p[0], p[1]]), isClosed });
  }

  function undo() {
    const s = undoStack.pop();
    if (!s) return;
    points = s.points;
    isClosed = s.isClosed;
  }

  function clear() {
    snapshot();
    points = [];
    isClosed = false;
  }

  function press(e: PointerEvent) {
    e.preventDefault();
    // preventDefault keeps focus where it was: without it, the name field would
    // stay focused, and on a phone every tap would reopen the keyboard.
    if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
    svg?.setPointerCapture(e.pointerId);
    const p = point(e);
    const i = near(p);
    if (!isClosed && i === 0 && points.length >= MIN_POINTS) {
      snapshot();
      isClosed = true;
      return;
    }
    if (i >= 0) {
      snapshot();
      drag = i;
      return;
    }
    if (!isClosed && points.length < MAX_POINTS) {
      snapshot();
      points.push(p);
    }
  }

  function move(e: PointerEvent) {
    if (drag !== null) points[drag] = point(e);
  }

  function release() {
    drag = null;
  }

  function apply() {
    if (!validate) return;
    if (askingName) void saveIt();
    else onapply(points, rule, currentName.trim());
  }

  async function saveIt() {
    if (!save || saving) return;
    saving = true;
    saveError = '';
    try {
      await save.onsave(points, rule, currentName.trim());
    } catch (e) {
      saveError = e instanceof Error ? e.message : String(e);
    } finally {
      saving = false;
    }
  }

  let nameField = $state<HTMLInputElement | null>(null);
  async function askName() {
    askingName = true;
    saveError = '';
    await tick();
    nameField?.focus();
  }

  function nameBack() {
    askingName = false;
    saveError = '';
  }

  function keyHandler(e: KeyboardEvent) {
    if (e.key === 'Escape') onclose();
  }

  const statusText = $derived(
    drawn
      ? saveError || nameError || `${points.length} pontos`
      : points.length < MIN_POINTS
        ? `Toque pelo menos ${MIN_POINTS} pontos.`
        : 'Toque no ponto verde para fechar.',
  );

  // Where the name of a gray area goes: the mean of the vertices, which falls
  // inside any area that isn't very twisted.
  const center = (pts: Point[]): Point => [
    pts.reduce((s, p) => s + p[0], 0) / pts.length,
    pts.reduce((s, p) => s + p[1], 0) / pts.length,
  ];

  // The foot of each box, for the `pisa` rule: the middle of the bottom edge.
  const pe = (c: Box): Point => [(c[0] + c[2]) / 2, Math.min(1, c[3])];

  onMount(() => {
    measure();
    // The screen behind doesn't scroll under the editor.
    const before = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = before;
    };
  });
</script>

<svelte:window onresize={measure} onkeydown={keyHandler} />

<div class="editor {mode}" role="dialog" aria-modal="true" aria-label={title}>
  <div class="window">
    <div class="top">
      <button class="ghost" onclick={onclose} aria-label="cancelar">
        <Icon name="close" size={16} />
      </button>
      <b>{title}</b>
      <span class="muted small camera">{camera}</span>
    </div>

    <div class="space over-video" bind:clientWidth={space.w} bind:clientHeight={space.h}>
      <div class="stage" style:width="{stage.w}px" style:height="{stage.h}px">
        {#if background?.kind === 'image'}
          <img
            src={background.url}
            alt=""
            onload={(e) => {
              const el = e.currentTarget;
              if (el instanceof HTMLImageElement && el.naturalWidth && el.naturalHeight)
                ratio = el.naturalWidth / el.naturalHeight;
            }}
          />
        {:else if background?.kind === 'video'}
          <video
            src={background.url}
            muted
            playsinline
            preload="auto"
            onloadeddata={(e) => {
              const { videoWidth: w, videoHeight: h } = e.currentTarget;
              if (w && h) ratio = w / h;
            }}
          ></video>
        {/if}
        {#if !background}<span class="notice muted small"
            >{error || 'buscando um quadro da câmera…'}</span
          >{/if}
        <svg
          bind:this={svg}
          viewBox="0 0 {stage.w || 1} {stage.h || 1}"
          onpointerdown={press}
          onpointermove={move}
          onpointerup={release}
          onpointercancel={release}
          role="application"
          aria-label="toque para pôr os pontos da área"
        >
          {#each otherAreas as o, i (i)}
            {@const [cx, cy] = center(o.points)}
            <polygon
              points={svgPoints(o.points.map(([x, y]): Point => [x * stage.w, y * stage.h]))}
              class="other"
            />
            <text x={cx * stage.w} y={cy * stage.h} class="label">{o.name}</text>
          {/each}
          {#each boxes as c, i (i)}
            {@const [x1, y1, x2, y2] = c.box}
            <rect
              x={x1 * stage.w}
              y={y1 * stage.h}
              width={(x2 - x1) * stage.w}
              height={(y2 - y1) * stage.h}
              class="box"
              style:stroke={c.color}
            />
            {#if rule === 'foot'}
              {@const [px, py] = pe(c.box)}
              <circle cx={px * stage.w} cy={py * stage.h} r="4" style:fill={c.color} />
            {/if}
          {/each}
          {#if points.length}
            {@const pts = svgPoints(points.map(([x, y]): Point => [x * stage.w, y * stage.h]))}
            {#if isClosed}
              <polygon points={pts} class="area" />
            {:else}
              <polyline points={pts} class="stroke" />
              {#if points.length >= 2}
                {@const a = points[points.length - 1] ?? [0, 0]}
                {@const b = points[0] ?? [0, 0]}
                <line
                  x1={a[0] * stage.w}
                  y1={a[1] * stage.h}
                  x2={b[0] * stage.w}
                  y2={b[1] * stage.h}
                  class="back"
                />
              {/if}
            {/if}
            {#each points as [x, y], i (i)}
              {@const first = !isClosed && i === 0 && points.length >= MIN_POINTS}
              <circle
                cx={x * stage.w}
                cy={y * stage.h}
                r={first ? 11 : 8}
                class="vertex"
                class:first
              />
            {/each}
          {/if}
        </svg>
      </div>
    </div>

    <div class="controls">
      <!-- In landscape the top disappears and the title moves here: cancel
           comes along, or the editor has no way out on a phone. -->
      <div class="title">
        <button class="ghost" onclick={onclose} aria-label="cancelar">
          <Icon name="close" size={16} />
        </button>
        <b>{title}</b>
        <span class="muted small camera">{camera}</span>
      </div>
      {#if frame}
        <div class="frame small">
          <span class="muted">{frame.caption}</span>
          <button class="ghost" onclick={frame.onrefresh} disabled={frame.refreshing}>
            {frame.refreshing ? 'buscando…' : 'atualizar o quadro'}
          </button>
        </div>
      {/if}
      {#if withName}
        <label class="name">
          Nome
          <input
            bind:this={nameField}
            bind:value={currentName}
            oninput={() => (saveError = '')}
            maxlength="40"
            placeholder="Portão, Garagem…"
            onkeydown={(e) => {
              if (e.key === 'Enter') apply();
            }}
          />
        </label>
      {/if}
      <div class="rules" role="radiogroup" aria-label="quando o objeto conta como dentro">
        {#each Object.entries(RULES) as [id, r] (id)}
          <button
            role="radio"
            aria-checked={rule === id}
            class:on={rule === id}
            onclick={() => (rule = id as Rule)}
          >
            <Icon name={r.icon} size={20} />
            {r.name}
          </button>
        {/each}
      </div>
      <p class="hint rule">{RULES[rule].hint}</p>
      <p
        class="status small"
        class:ok={validate && !saveError}
        class:error={drawn && (!!nameError || !!saveError)}
      >
        {statusText}
      </p>
      <div class="actions">
        <button class="ghost" onclick={undo} disabled={!undoStack.length}>desfazer</button>
        <button class="ghost" onclick={clear} disabled={!points.length}>limpar</button>
        {#if ondelete}
          <button class="ghost danger" onclick={ondelete}>apagar área</button>
        {/if}
        <span class="spacer"></span>
        {#if askingName}
          <button class="ghost" onclick={nameBack} disabled={saving}>voltar</button>
          <button class="primary" onclick={apply} disabled={!validate || saving}>
            {saving ? 'salvando…' : 'salvar área'}
          </button>
        {:else}
          {#if save}
            <button
              class="ghost"
              onclick={askName}
              disabled={!drawn || save.full}
              title={save.full
                ? `a câmera já tem ${MAX_AREAS} áreas`
                : 'grava a área no cadastro da câmera, com um nome'}>salvar na câmera</button
            >
          {/if}
          <button class="primary" onclick={apply} disabled={!validate}>{applyLabel}</button>
        {/if}
      </div>
      <p class="hint">
        Toque para pôr um ponto; toque no primeiro para fechar. Arraste um ponto para ajustar.
      </p>
    </div>
  </div>
</div>

<style>
  .editor {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: flex;
    background: var(--bg);
  }

  .window {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }

  .top {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    border-bottom: 1px solid var(--line);
  }
  .top button,
  .title button {
    min-height: 40px;
    padding: 6px 10px;
  }
  .camera {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .space {
    position: relative;
    flex: 1;
    min-height: 0;
    display: grid;
    place-items: center;
    background: #000;
  }

  .stage {
    position: relative;
    touch-action: none;
  }
  .stage img,
  .stage video {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    display: block;
    user-select: none;
    pointer-events: none;
  }
  .notice {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    padding: 12px;
    text-align: center;
  }
  svg {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    touch-action: none;
    cursor: crosshair;
  }

  .box {
    fill: none;
    stroke-width: 1.5;
    opacity: 0.8;
  }
  .area {
    fill: color-mix(in srgb, var(--accent) 28%, transparent);
    stroke: var(--accent);
    stroke-width: 2.5;
    stroke-linejoin: round;
  }
  .stroke {
    fill: none;
    stroke: var(--accent);
    stroke-width: 2.5;
  }
  .back {
    stroke: var(--accent);
    stroke-width: 1.5;
    stroke-dasharray: 5 5;
    opacity: 0.7;
  }
  .other {
    fill: none;
    stroke: #9aa3ad;
    stroke-width: 1.5;
    stroke-dasharray: 6 4;
    opacity: 0.8;
  }
  .label {
    fill: #fff;
    font-size: 12px;
    text-anchor: middle;
    dominant-baseline: middle;
    paint-order: stroke;
    stroke: rgba(0, 0, 0, 0.7);
    stroke-width: 3;
    pointer-events: none;
  }
  .vertex {
    fill: var(--accent);
    stroke: #fff;
    stroke-width: 2;
  }
  .vertex.first {
    fill: var(--ok);
  }

  .controls {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 12px;
    border-top: 1px solid var(--line);
  }
  .title {
    display: none;
  }

  .rules {
    display: inline-flex;
    align-self: flex-start;
    border: 1px solid var(--line);
    border-radius: 8px;
    overflow: hidden;
  }
  .rules button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 7px;
    min-height: 40px;
    padding: 6px 16px;
    border: 0;
    border-radius: 0;
    background: transparent;
    color: var(--dim);
  }
  .rules button.on {
    background: var(--active-bg);
    color: var(--fg);
    box-shadow: inset 0 0 0 1px var(--accent);
  }

  .hint,
  .status {
    margin: 0;
    font-size: 12px;
    line-height: 1.4;
    color: var(--dim);
  }
  .status {
    font-size: 14px;
  }
  /* The two hints have different sizes: the height of two lines is reserved, so
     changing the rule moves neither the frame nor the buttons. */
  .hint.rule {
    min-height: calc(2 * 1.4em);
  }
  .status.ok {
    color: var(--ok);
  }
  .status.error {
    color: var(--warn);
  }

  .actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .frame {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .frame button {
    min-height: 36px;
    padding: 4px 10px;
    margin-left: auto;
  }
  .name {
    display: grid;
    gap: 5px;
    font-size: 13px;
    color: var(--dim);
  }
  .name input {
    width: 100%;
    color: var(--fg);
    font-size: 15px;
  }

  /* Landscape: the frame at full height, and the controls in a column on the
     right, with the title that left the top. */
  .landscape .window {
    flex-direction: row;
  }
  .landscape .top {
    display: none;
  }
  .landscape .controls {
    flex: none;
    width: 220px;
    padding: 8px;
    gap: 8px;
    overflow: auto;
    border-top: 0;
    border-left: 1px solid var(--line);
  }
  /* In the fixed-height column, what doesn't fit has to scroll, not shrink:
     flex shrank first, and the rule selector vanished (it clips what overflows
     its border) and its three-line hint spilled over the situation. */
  .landscape .controls > * {
    flex-shrink: 0;
  }
  .landscape .title {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .landscape .rules {
    align-self: stretch;
  }
  .landscape .rules button {
    flex: 1;
    padding: 6px 8px;
  }
  .landscape .actions {
    flex-direction: column;
  }
  .landscape .spacer {
    display: none;
  }

  /* Desktop: a modal over the screen. */
  .desktop {
    align-items: center;
    justify-content: center;
    background: var(--backdrop);
  }
  .desktop .window {
    flex: none;
    width: min(960px, calc(100vw - 40px));
    height: min(760px, calc(100vh - 40px));
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    overflow: hidden;
  }
  .desktop .controls {
    flex-direction: row;
    flex-wrap: wrap;
    align-items: center;
  }
  .desktop .actions {
    margin-left: auto;
  }
  .desktop .status {
    min-width: 180px;
  }
  /* The rule hint on a line of its own: sharing the line with the name, its
     size changed the field's width and pushed the buttons. */
  .desktop .hint:last-child,
  .desktop .hint.rule,
  .desktop .frame {
    flex-basis: 100%;
  }
  .desktop .hint.rule {
    min-height: 0;
  }
  .desktop .name {
    flex: 1 1 220px;
  }
  /* At the bottom, next to the name field, and not in the middle of the label + field. */
  .desktop .rules {
    align-self: flex-end;
  }
</style>
