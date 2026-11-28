<!--
  The sheet of a detection, opened by tapping the thumbnail on the Detections
  screen.

  It shows the frame the object detector looked at, large and with the box
  labels, and plays the clip around it only when asked: BEFORE_MS before the
  onset until AFTER_MS after. The player is the same as Recordings, with the
  segment list cut to that clip - so it downloads nothing beyond it.
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
  import { family, iconOf, iconURL, CLASS_NAME, pageColor } from '../lib/icons.ts';
  import { hhmmss, ddmm } from '../lib/format.ts';
  import { recordingsLink } from '../lib/route.svelte.ts';
  import { fullscreen, toggleFullscreen, leaveFullscreen } from '../lib/fullscreen.svelte.ts';

  // The clip: a bit before the onset, to see the object come in, and enough
  // after to see what it did. The same numbers as the approved mock.
  const BEFORE_MS = 2000;
  const AFTER_MS = 6000;

  // The sheet width. The stage is 16:9 and is what grows the most, so in a
  // short window it is the width that has to give way for the frame to fit
  // entirely - the ceiling comes from the free height, not the other way
  // around. `REST_PX` is what the rest of the sheet takes in height (header 36
  // + objects 36, the height of the hide button + actions 44 + three gaps of 14
  // + 28 of padding + 2 of border) and `BREATHING_PX` is what is left on the
  // sides of the stage (28 of padding + 2 of border), which goes into the width
  // but not into the stage. Without the button, when the camera is already
  // hidden, the objects row is 19.5 and the sum comes out long - the sheet ends
  // up slightly narrower, which does no harm.
  //
  // The math has to come out exactly right, rounding up: with the sheet 1px
  // above the ceiling, the scrollbar appears, eats the width, the stage
  // shrinks, the sheet fits, the bar goes away - and the cycle repeats every
  // frame, with the sheet jittering. That is what happened with 169 and 28,
  // which forgot the border and the half pixel of the objects. The Modal's
  // `scrollReserve` cuts the cycle for good, for when the sheet really does
  // scroll.
  //
  // The floor exists because the math has no bottom: in a very short window it
  // asked for a sheet narrower than the button row itself, and then the stage
  // overflowed to the sides.
  //
  // An object list that wraps onto two lines makes the math come up short, and
  // so does the floor. In both cases the Modal's `grid-auto-rows` makes the
  // sheet scroll, which is the worst case - and not the frame covering the
  // buttons, which was the defect.
  //
  // The detection that alerted gets the "alertou" block above the objects, and
  // the math adds its height: `BLOCK_PX` is the block with the title (14 of gap
  // + 2 of border + 16 of padding + 21 of title) and `BLOCK_ROW_PX`, each of
  // its objects (20 for the row + 4 of gap).
  const MAX_WIDTH_PX = 880;
  const MIN_WIDTH_PX = 360;
  const REST_PX = 188;
  const BREATHING_PX = 30;
  const BLOCK_PX = 53;
  const BLOCK_ROW_PX = 24;
  // The opacity of what stays in the frame but isn't the subject: the object
  // that wasn't part of the alert, and the areas other than the highlighted
  // one.
  const DIMMED = 0.35;

  interface Props {
    det: Detection;
    camera: string;
    onclose: () => void;
    // Null means "there isn't one": no previous detection, or the camera is
    // already hidden.
    onprevious?: (() => void) | null;
    onnext?: (() => void) | null;
    onhide?: (() => void) | null;
    // The filter's areas on this camera: the outline goes over the frame, and
    // says why the detection got in.
    areas?: readonly (readonly Point[])[] | null;
    // The areas where the detection alerted, by the id saved in the mark, with
    // the current name and outline; null `points` means the area has been
    // deleted since.
    alertAreas?: readonly { id: string; name: string; points: readonly Point[] | null }[];
    // Opens the area editor over this frame. Null without a frame.
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
  // The video has painted its first frame. Between the tap on play and that
  // frame, about 300ms go by fetching bytes, and in that interval the `<video>`
  // is still transparent: it is the video that decides when the still frame
  // leaves the scene, not the click. Switching on click let the photo without
  // the boxes show halfway through - the frame with the boxes stays until the
  // video has something to show.
  let videoPainted = $state(false);
  let error = $state('');
  let Boxes = $state<typeof VideoBoxes | null>(null);

  const objects = $derived(
    [...det.objects].sort(
      (a, b) => family(a.family).priority - family(b.family).priority || b.score - a.score,
    ),
  );
  // With an alert, the "alertou" block carries the objects that passed the
  // rule, and the others stay below, as "não entrou no alerta".
  const ofAlert = $derived(objects.filter((o) => o.alert));
  const outsideAlert = $derived(objects.filter((o) => !o.alert));
  const alerted = $derived(ofAlert.length > 0);
  const alertArea = (id: string) => alertAreas.find((a) => a.id === id);

  // The alert area highlighted in the frame by tapping its chip: the others are
  // dimmed, and the same tap shows all of them again. It doesn't carry over to
  // the next detection through the arrows: the sheet remounts for each one (the
  // `{#key}` in Detections).
  let highlighted = $state<string | null>(null);
  const toggleHighlight = (id: string) => {
    highlighted = highlighted === id ? null : id;
  };

  const width = $derived.by(() => {
    const rest = REST_PX + (alerted ? BLOCK_PX + ofAlert.length * BLOCK_ROW_PX : 0);
    return `clamp(${MIN_WIDTH_PX}px, calc((var(--max-height) - ${rest}px) * 16 / 9 + ${BREATHING_PX}px), ${MAX_WIDTH_PX}px)`;
  });
  // The video's boxes light up when playback passes the inspected frame, and
  // the Recordings layer finds that frame in each mark's `frameMs`.
  const marks = $derived.by(() => {
    const frameMs = det.frameMs;
    return frameMs === undefined ? [] : det.objects.map((o) => ({ ...o, frameMs: frameMs }));
  });

  const link = $derived(recordingsLink(det.cam, det.atMs));

  const score = (v: number) => v.toFixed(2).replace('.', ',');

  // --- the still frame, with the boxes and labels ----------------------------

  // Where the image is inside the stage: `object-fit: contain` leaves black
  // bars when the camera isn't 16:9, and the box fraction is relative to the
  // image.
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
    // The outline of the filter's areas and the alert's areas, once each. The
    // highlighted one goes full, on top, and the others, dimmed.
    const outlines = [...(areas ?? [])];
    for (const a of alertAreas)
      if (a.points && !outlines.includes(a.points)) outlines.push(a.points);
    const emphasis = highlighted === null ? null : (alertArea(highlighted)?.points ?? null);
    g.globalAlpha = emphasis ? DIMMED : 1;
    for (const a of outlines) if (a !== emphasis) drawOutline(g, a, iw * s, ih * s);
    g.globalAlpha = 1;
    if (emphasis) fillArea(g, emphasis, iw * s, ih * s);
    // The object that wasn't part of the alert stays in the frame, but dimmed.
    draw(g, place(g, det.objects, iw * s, ih * s, true), (it) =>
      alerted && !it.mark.alert ? DIMMED : 1,
    );
  }

  // Repaints when the highlight changes (and with whatever else the frame
  // reads).
  $effect(paintFrame);

  onMount(() => {
    const ro = new ResizeObserver(paintFrame);
    if (stage) ro.observe(stage);
    return () => {
      ro.disconnect();
      player.destroy();
    };
  });

  // --- the clip -----------------------------------------------------------------

  const endMs = $derived(det.atMs + AFTER_MS);
  const startMs = $derived(det.atMs - BEFORE_MS);

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
      if (!video) return; // the sheet closed during the lookup
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

  // Past the end of the clip: it ends there and the sheet goes back to the
  // detection's frame, as when it opened. Stopping is mandatory - without it
  // the player would keep requesting the following segments, which the cut list
  // doesn't have, and would end up waiting. Going back to the frame is what
  // makes the clip repeatable: the player leaves the scene, and the next play
  // retraces the same path as the first time.
  //
  // Just pausing wasn't enough, for two reasons. The clock stopped at the end,
  // and the next play relit `playing` with the position still past the end -
  // this same effect paused on the next frame, and the clip only played again
  // by closing and reopening the sheet. And the `<video>` stayed on top of the
  // `<img>` showing the last frame: switching the src to restart left it empty
  // for an instant, and the still frame flashed underneath before the video
  // came in.
  //
  // Releasing the player also gives back the memory of the decoded video
  // between one playback and the next, which on a phone is tens of MB.
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

  // --- fullscreen
  // -------------------------------------------------------------
  //
  // Only the stage goes fullscreen: the frame, the video, the boxes and the
  // clip bar. The double click arrives after two single clicks, and each would
  // play or pause: the second is ignored, and the first, if it paused, is
  // undone on the double. That way the double click never pauses - with the
  // clip stopped, it starts playing in fullscreen.

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

  // In fullscreen the arrows don't switch detections: the next sheet is a
  // different one, and the stage that was fullscreen would leave it together
  // with the previous one.
  function keyHandler(e: KeyboardEvent) {
    if (inFullscreen) return;
    if (e.key === 'ArrowLeft' && onprevious) onprevious();
    else if (e.key === 'ArrowRight' && onnext) onnext();
  }
</script>

<svelte:window onkeydown={keyHandler} />

<Modal {onclose} {width} scrollReserve centered>
  <div class="cab">
    <span class="hour mono">{hhmmss(det.atMs)}</span>
    <span class="muted small">{ddmm(det.atMs)}</span>
    <span class="muted">·</span>
    <span class="camera">{camera}</span>
    <span class="spacer"></span>
    <button class="ghost close" onclick={onclose} aria-label="fechar">
      <Icon name="close" size={16} />
    </button>
  </div>

  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="stage over-video"
    class:fs={inFullscreen}
    bind:this={stage}
    ondblclick={doubleClick}
    {@attach (node: HTMLElement) => () => {
      // A de verdade o navegador desfaz sozinho quando o elemento sai da
      // página; a simulada ficaria cobrindo a tela depois da folha fechar.
      if (fullscreen.el === node) leaveFullscreen();
    }}
  >
    {#if det.hasFrame}
      <img
        bind:this={img}
        src={mediaURL.frame(det.cam, det.atMs)}
        alt="quadro da detecção das {hhmmss(det.atMs)}"
        onload={paintFrame}
      />
    {/if}
    <canvas bind:this={canvas} class:hidden={videoPainted}></canvas>
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
      <span class="badge video-hour mono">{hhmmss(player.currentMs || det.atMs)}</span>
      <span class="progress" style:width="{progress * 100}%"></span>
    {/if}
    <button
      class="touch"
      onclick={clique}
      aria-label={playingNow ? 'pausar ou continuar' : `tocar o trecho das ${hhmmss(det.atMs)}`}
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
                src={iconURL(icon, 14, pageColor(family(o.family)))}
                alt=""
                width="14"
                height="14"
              />{/if}
            <span>{CLASS_NAME[o.class] ?? o.class} {score(o.score)}</span>
          </span>
          {#each o.alertAreas ?? [] as id (id)}
            {@const a = alertArea(id)}
            {#if a?.points && det.hasFrame}
              <button
                type="button"
                class="chip-area"
                class:on={highlighted === id}
                aria-pressed={highlighted === id}
                aria-label={highlighted === id
                  ? `${a.name}: mostrar todas as áreas`
                  : `${a.name}: realçar no quadro`}
                onclick={() => toggleHighlight(id)}>{a.name}</button
              >
            {:else}
              <span class="chip-area">{a?.name ?? 'área apagada'}</span>
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
            src={iconURL(icon, 14, pageColor(family(o.family)))}
            alt=""
            width="14"
            height="14"
          />{/if}
        <span>{CLASS_NAME[o.class] ?? o.class} {score(o.score)}</span>
      </span>
    {/each}
    {#if alerted && outsideAlert.length}
      <span class="muted">não entrou no alerta</span>
    {/if}
    <!-- Lives here, not in the header: there it left little space and the
         camera name came out cut off on a phone. -->
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

  /* The two buttons stay together, on the right. */
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

  /* Without `position`, and without the 16:9: the browser provides both
     (`:fullscreen`), or the simulated-fullscreen rule in app.css. */
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

  canvas.hidden {
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

  /* Always visible, with a mouse or on touch: here the video is the subject of
     the sheet, not one among several, as in Live. */
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
    border-color: var(--bad-line);
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

  /* In the bell's color (--warn), like "só alertas" in Detections. */
  .alert-block {
    display: grid;
    gap: 4px;
    padding: 8px 10px;
    border: 1px solid color-mix(in srgb, var(--warn) 40%, transparent);
    border-radius: 8px;
    background: color-mix(in srgb, var(--warn) 6%, transparent);
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

  /* The dashes are those of the area outline over the frame. */
  .chip-area {
    padding: 0 7px;
    border: 1px dashed var(--accent);
    border-radius: 999px;
    font-size: 12px;
    white-space: nowrap;
  }
  /* The chip that highlights the area: the size of the previous one, so the
     sheet width math keeps holding, with a finger target larger than it. */
  button.chip-area {
    position: relative;
    min-height: 0;
    line-height: inherit;
    background: transparent;
    color: inherit;
  }
  /* Not going past half the space between the block's rows: one row's target
     doesn't cover the chip of the row below. */
  button.chip-area::after {
    content: '';
    position: absolute;
    inset: -2px -3px;
  }
  button.chip-area.on {
    border-style: solid;
    background: var(--active-bg);
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
