<script lang="ts">
  import { onMount, onDestroy, untrack } from 'svelte';
  import { cameras, loadCameras } from '../lib/state.svelte.ts';
  import { currentParams, write, scene } from '../lib/route.svelte.ts';
  import { api, mediaURL, messageOf } from '../lib/api.ts';
  import type {
    Events,
    MarkedObject,
    Segment,
    Timeline as TimelineData,
    Span,
  } from '../lib/api.ts';
  import type { OverlayDrawing } from '../lib/capture.ts';
  import { Player, FAST_JUMP_MS } from '../lib/player.svelte.ts';
  import { doubleTouch, SEQUENCE_MS } from '../lib/doubleTouch.ts';
  import type { Side } from '../lib/doubleTouch.ts';
  import { fade } from 'svelte/transition';
  import { clearThumbnails } from '../lib/thumbs.ts';
  import { downloadFrame } from '../lib/capture.ts';
  import { dayKey, parseDay, hhmmss, taxa, shortDay } from '../lib/format.ts';
  import { FAMILIES } from '../lib/icons.ts';
  import { fullscreen, toggleFullscreen, leaveFullscreen } from '../lib/fullscreen.svelte.ts';
  import { vanishingControls } from '../lib/vanishingControls.svelte.ts';
  import { freeHeight } from '../lib/freeHeight.ts';
  import { SWITCH_MS, AXIS_PX, swipeLimit } from '../lib/swipe.ts';
  import { notify } from '../lib/snackbar.svelte.ts';
  import Timeline from '../components/Timeline.svelte';
  import FullscreenButton from '../components/FullscreenButton.svelte';
  import ThumbStrip from '../components/ThumbStrip.svelte';
  import NoCameras from '../components/NoCameras.svelte';
  import DayNav from '../components/DayNav.svelte';
  import Clock from '../components/Clock.svelte';
  import Icon from '../components/Icon.svelte';
  import type VideoBoxes from '../components/Boxes.svelte';

  // What the screen keeps of the day's timeline: what it draws, what the player
  // plays, and which camera it belongs to.
  type DayData = Pick<TimelineData, 'cam' | 'ranges' | 'segments' | 'gens'>;

  // What the export will request, or why it can't. The `never` lets the
  // template read `?.error` and `?.start` without knowing which of the two it
  // is.
  type ExportPlan =
    { error: string; start?: never; to?: never } | { start: number; to: number; error?: never };

  const player = new Player();

  // Floor and ceiling of the pace at which today is re-read. Querying faster
  // than the camera's segment means re-downloading the same response several
  // times; slower than a minute leaves the screen stale for no reason.
  const POLL_MIN_MS = 10_000;
  const POLL_MAX_MS = 60_000;
  // Segment seconds assumed when the camera doesn't report them - it's the
  // default in dwnvr.example.yaml. The current server already resolves the
  // field before answering.
  const DEFAULT_SEG = 30;
  // Durations offered for export. 10 min is already a big file to download over
  // a phone, and it's also the server's ceiling (maxExportSpan in
  // internal/api/recordings.go): going past it requires raising both together.
  const DURATIONS = [1, 2, 3, 5, 10];
  // Speeds offered. In a const, and not loose in the template, because the URL
  // also needs it - to reject a `rate=999` without inventing a second list that
  // would someday disagree with this one.
  const RATES = [0.25, 0.5, 1, 2, 4, 8, 16];
  // Below this the screen stops shrinking the video and starts scrolling: in a
  // window this short, video, controls and timeline don't fit together no
  // matter what. A phone in landscape, with the navigation at the bottom,
  // leaves about 270.
  const MIN_HEIGHT = 240;
  // Below this the page stops narrowing to follow the video: the controls need
  // room on a single row, with their labels (720 px), and the video gets black
  // bars on the sides again, as in a short window.
  const MIN_WIDTH = 720;
  const RATIO = 16 / 9;

  // Read once, at initialization: from here on the screen state is in charge,
  // and it writes back to the URL.
  const params = currentParams();

  let video = $state<HTMLVideoElement | null>(null);
  let cam = $state('');
  // In a const before the $state because what comes from the URL right below -
  // instant and zoom - are positions WITHIN this day, and need the starting
  // day, not the day that will be on screen in a moment.
  const initialDay = urlDay() ?? dayKey();
  let day = $state(initialDay);
  let daysList = $state<string[]>([]);
  let timeline = $state<DayData>({ cam: '', ranges: [], segments: [], gens: [] });
  // The day's motion marks, ascending - the heat strip. They come in a separate
  // timeline request on purpose: the blue bar is what the screen exists to
  // show, and a failure reading the marks must not leave it undrawn.
  let events = $state<number[]>([]);
  // The day's object marks, from the same request: [{ atMs, family,
  // class, score }, …].
  let objects = $state<MarkedObject[]>([]);
  let loading = $state(false);
  let error = $state('');
  // Thumbnails are a matter of taste of whoever uses the device, like the
  // export duration: in localStorage, and on until someone turns them off.
  let showThumbs = $state(localStorage.getItem('dwnvr.rec.thumbnails') !== '0');
  // The detector's boxes over the video, and their labels. On by default and
  // kept in the URL: they're part of what a link shows. A label without a box
  // doesn't exist; a single button cycles through the three states.
  let showBoxes = $state(params.get('boxes') !== '0');
  let showLabels = $state(params.get('labels') !== '0');
  let boxesLayer = $state<{ drawOn: OverlayDrawing } | null>(null);
  // The boxes component comes in a separate chunk, downloaded only when the day
  // has an object: whoever doesn't use detection doesn't pay for its bytes.
  let Boxes = $state<typeof VideoBoxes | null>(null);
  $effect(() => {
    if (objects.length && !Boxes) {
      void import('../components/Boxes.svelte').then((m) => {
        Boxes = m.default;
      });
    }
  });
  // Holds the URL write until the camera has been resolved against the
  // registry.
  let mounted = $state(false);
  // The chosen duration survives a reload, like the Live layout. A stored value
  // outside the list is discarded: it would leave the <select> with no matching
  // option, showing empty.
  let exportMin = $state(
    DURATIONS.find((m) => m === Number(localStorage.getItem('dwnvr.rec.exportMin'))) ?? 5,
  );
  // A capture failure has its own state: `error` is only cleared on the next
  // load(), and a capture warning would stay stuck on screen until the day
  // changed.
  let captureError = $state('');
  let noticeTimer: number | undefined;

  // --- fullscreen
  // -------------------------------------------------------------
  //
  // The video, the playback controls and the timeline go fullscreen; the rest
  // of the screen stays behind. The controls sit over the video and disappear
  // on their own, or, PINNED, below it, with the video in the remaining space.

  let full = $state<HTMLDivElement | null>(null);
  const inFullscreen = $derived(!!full && fullscreen.el === full);
  // In localStorage, like the export duration: it's a matter of taste of
  // whoever uses the device, not part of the scene a link replays.
  let pinnedControls = $state(localStorage.getItem('dwnvr.rec.fixedControls') === '1');
  let overControls = false;
  // They only disappear while the video is playing: whoever paused wants to
  // look and interact. With the mouse over them, they stay too.
  const controls = vanishingControls(
    () => inFullscreen && player.playing && !pinnedControls && !overControls,
  );
  // Outside fullscreen the controls never disappear, whatever the state above.
  const hiddenControls = $derived(inFullscreen && !controls.visibleOnes);

  $effect(() => {
    localStorage.setItem('dwnvr.rec.fixedControls', pinnedControls ? '1' : '0');
  });

  // Entering fullscreen, play, pause, unpinning and switching camera restart
  // the countdown. The switch also shows the new camera's name.
  $effect(() => {
    if (!inFullscreen) return;
    /* eslint-disable @typescript-eslint/no-unused-expressions -- read only, so the $effect tracks it */
    player.playing;
    pinnedControls;
    cam;
    /* eslint-enable @typescript-eslint/no-unused-expressions */
    untrack(() => {
      controls.show();
    });
  });

  function toggleFull() {
    if (full) toggleFullscreen(full);
  }

  // --- icon-only buttons --------------------------------------------------
  //
  // On phones, thumbnails, boxes and download are icon-only, and each tap
  // explains itself in a snackbar. On desktop the button label already says
  // what it does, and the notice would be repetition: the label itself decides,
  // visible or not, so the CSS breakpoint isn't repeated here.

  let buttonLabel = $state<HTMLElement | null>(null);

  function buttonNotice(text: string) {
    if (buttonLabel && getComputedStyle(buttonLabel).display !== 'none') return;
    notify({ text: text });
  }

  function toggleThumbnails() {
    showThumbs = !showThumbs;
    buttonNotice(showThumbs ? 'Miniaturas exibidas' : 'Miniaturas ocultas');
  }

  // No boxes -> boxes -> boxes with labels -> no boxes.
  const BOXES_STATES = ['sem caixas', 'caixas', 'caixas + rótulos'] as const;
  const boxesState = $derived(!showBoxes ? 0 : showLabels ? 2 : 1);

  function toggleBoxes() {
    const next = (boxesState + 1) % 3;
    showBoxes = next > 0;
    showLabels = next === 2;
    buttonNotice(
      [
        'Caixas de detecção ocultas',
        'Caixas de detecção exibidas, sem rótulos',
        'Caixas de detecção exibidas, com rótulos',
      ][next] ?? '',
    );
  }

  // The download menu and the gestures bubble: one open at a time, and they
  // close on a click outside them or on Esc.
  let menu = $state<'baixar' | 'gestos' | null>(null);

  function outsideMenu(e: MouseEvent) {
    if (menu && !(e.target instanceof Element && e.target.closest('.anchor'))) menu = null;
  }

  // --- the height ---------------------------------------------------------
  //
  // The screen has the window's height, and the video gets what's left of it:
  // no scrolling, and hiding the thumbnails gives the space back to the video
  // when height is what limits it. With the phone in landscape the video moves
  // to the side, and its height is the screen's minus the timeline's and the
  // legend's, which stay at the bottom at full width.

  let page = $state<HTMLDivElement | null>(null);
  let pageHeight = $state(0);
  let rowHeight = $state(0);
  let captionHeight = $state(0);

  // And the width follows the video: the whole page gets the width the video
  // has, at 16:9, in the height left for it, and is centered. On a wide screen,
  // like an ultrawide, the video grows up to the window's height, with no fixed
  // ceiling and no black bars on the sides; the bar, the controls and the
  // timeline come along, aligned with it.
  let pageWidth = $state(0);

  function measurePage() {
    // In fullscreen the stage is fullscreen's; the page measures itself again
    // on the way back.
    if (!page || inFullscreen) return;
    pageHeight = freeHeight(page, MIN_HEIGHT);
    measureWidth();
  }

  // What isn't video (padding, bar, controls, thumbnails, timeline, legend and
  // the gaps) has a height that doesn't depend on the width, except the legend,
  // which wraps in a narrow window: the computation converges, because
  // narrowing only makes it wrap more. Measured from the top of the page to the
  // end of the last child, and not by the page's height, which is fixed and
  // leaves the leftover at the bottom.
  function measureWidth() {
    if (!page || !stage || inFullscreen) return;
    const last = page.lastElementChild;
    if (!last) return;
    const style = getComputedStyle(page);
    const content =
      last.getBoundingClientRect().bottom -
      page.getBoundingClientRect().top +
      parseFloat(style.paddingBottom);
    // The stage height with its fraction, like the rest: `offsetHeight` rounds,
    // and the one-pixel difference would make the width flip between two values
    // (Greptile finding on PR #74).
    const others = content - stage.getBoundingClientRect().height;
    const horizontal = parseFloat(style.paddingLeft) + parseFloat(style.paddingRight);
    const width = Math.floor((pageHeight - others) * RATIO + horizontal);
    pageWidth = Math.max(MIN_WIDTH, width);
  }

  $effect(() => {
    if (!page) return;
    measurePage();
    // The header grows when the new-version banner appears.
    const ro = new ResizeObserver(measurePage);
    const topPos = document.querySelector('#app > .top');
    if (topPos) ro.observe(topPos);
    return () => {
      ro.disconnect();
    };
  });

  // Hiding the thumbnails, the legend gaining families or wrapping changes the
  // height of what isn't video, and with it the width. The stage is left out:
  // it changes precisely because of this computation.
  $effect(() => {
    if (!page || !stage) return;
    const ro = new ResizeObserver(() => {
      measureWidth();
    });
    for (const child of page.children) if (!child.contains(stage)) ro.observe(child);
    const dock = stage.parentElement?.querySelector('.dock');
    if (dock) ro.observe(dock);
    return () => {
      ro.disconnect();
    };
  });

  // --- skip 10 s
  // ---------------------------------------------------------------
  //
  // Like YouTube: on a phone, double tap on the video's sides and in the middle
  // fullscreen; on desktop, double click anywhere is fullscreen, and the skips
  // are the arrow keys. The half-moon on the skip side shows the running total
  // while the sequence is open.

  let jumpHint = $state<{ side: Side; sum: number } | null>(null);
  let jumpTimer: number | undefined;
  // Chrome on Android also generates `dblclick` on a double tap: without
  // knowing where it came from, the side skip would enter fullscreen as well.
  let lastPointer = '';

  function fastJump(side: Side) {
    player.jump(side * FAST_JUMP_MS);
    const s = FAST_JUMP_MS / 1000;
    jumpHint = { side, sum: jumpHint?.side === side ? jumpHint.sum + s : s };
    clearTimeout(jumpTimer);
    jumpTimer = setTimeout(() => {
      jumpHint = null;
    }, SEQUENCE_MS);
  }

  // A single tap hides the fullscreen controls, even when paused; when pinned,
  // they stay, which is what pinning is for.
  const touch = doubleTouch({
    jump: fastJump,
    middle: toggleFull,
    single: () => {
      if (inFullscreen && !pinnedControls) controls.singleTouch();
    },
  });

  // --- swipe to switch camera --------------------------------------------
  //
  // The Live fullscreen gesture, here both inside and outside fullscreen: the
  // video is always from a single camera. It follows the finger (or the mouse)
  // and, past the threshold, leaves through the side; the new camera appears in
  // its place, without swiping again, when its load() finishes. Until then the
  // video stays hidden, with "carregando…" on the black stage: showing it
  // earlier would show the last frame of the camera that left, which is what
  // the <video> still has. A swipe that's too short snaps back.
  //
  // The axis is decided in the first AXIS_PX: vertically the finger scrolls the
  // page. Starting on a button doesn't count.

  let stage = $state<HTMLDivElement | null>(null);
  // How far the video is displaced, in px, and whether the displacement
  // animates.
  let drag = $state(0);
  let animating = $state(false);
  // The camera left through the side and the new one is still loading.
  let switching = $state(false);
  let gesture: { id: number; x: number; y: number; axis: 'x' | 'y' | null } | null = null;
  // Two quick drags in a row with the mouse also make a double click.
  let draggedAt = 0;

  // The next (1) or the previous (-1), like the bar's arrows.
  function ir(d: Side) {
    if (animating || cameras.list.length < 2 || !stage) return;
    menu = null;
    // The strip's arrow tapped with another finger while dragging the video:
    // the drag stops here, otherwise it would move the video in the middle of
    // the switch.
    letGo();
    // One has already left and the other hasn't appeared yet: nothing to take
    // off the screen.
    if (switching) {
      drag = 0;
      shiftCam(d);
      return;
    }
    animating = true;
    drag = -d * stage.clientWidth;
    setTimeout(() => {
      // In the same tick: the video disappears in the frame where it returns to
      // zero.
      animating = false;
      drag = 0;
      switching = true;
      shiftCam(d);
    }, SWITCH_MS);
  }

  // The new camera loaded (or failed, or has no recording that day): it
  // appears.
  $effect(() => {
    if (switching && !loading && timeline.cam === cam) switching = false;
  });

  // The pointer whose gesture was dropped midway: its release is not a tap. The
  // next `pointerdown` with the same id clears it, because the mouse reuses the
  // id and a release outside the window never reaches here.
  let released: number | null = null;

  function letGo() {
    if (gesture) released = gesture.id;
    gesture = null;
  }

  // Drops the gesture in progress and puts the video back in place.
  function abandon() {
    letGo();
    if (!drag) return;
    animating = true;
    drag = 0;
    setTimeout(() => (animating = false), SWITCH_MS);
  }

  function gestureStart(e: PointerEvent) {
    lastPointer = e.pointerType;
    if (e.pointerId === released) released = null;
    // A second finger in the middle of the gesture is a pinch, not a swipe:
    // without this the gesture would pass to it and the video would stay where
    // the first finger left it.
    if (gesture && gesture.id !== e.pointerId) {
      abandon();
      return;
    }
    if (e.button !== 0 || animating || cameras.list.length < 2) return;
    if (e.target instanceof Element && e.target.closest('button, a')) return;
    gesture = { id: e.pointerId, x: e.clientX, y: e.clientY, axis: null };
  }

  function gestureMove(e: PointerEvent) {
    if (gesture?.id !== e.pointerId || !stage) return;
    // The button was released outside the window before the axis was decided
    // (only then does the stage capture the pointer): the video must not follow
    // a released mouse.
    if (e.pointerType === 'mouse' && !(e.buttons & 1)) {
      abandon();
      return;
    }
    const dx = e.clientX - gesture.x;
    const dy = e.clientY - gesture.y;
    if (!gesture.axis && Math.hypot(dx, dy) > AXIS_PX) {
      gesture.axis = Math.abs(dx) > Math.abs(dy) ? 'x' : 'y';
      if (gesture.axis === 'x') stage.setPointerCapture(e.pointerId);
    }
    if (gesture.axis === 'x') drag = dx;
  }

  // Tells whether it was a swipe: if so, the release is not a tap, neither for
  // the 10 s skip nor for hiding the controls.
  function gestureEnd(e: PointerEvent) {
    if (gesture?.id !== e.pointerId || !stage) return false;
    const { axis, x } = gesture;
    gesture = null;
    if (axis !== 'x') return false;
    draggedAt = Date.now();
    const dx = e.clientX - x;
    if (e.type === 'pointerup' && Math.abs(dx) > swipeLimit(stage.clientWidth)) {
      ir(dx < 0 ? 1 : -1);
      return true;
    }
    animating = true;
    drag = 0;
    setTimeout(() => (animating = false), SWITCH_MS);
    return true;
  }

  // The camera's name and position, in fullscreen: there's no bar with the
  // selector there to say which camera you're on after swiping.
  const camPosition = $derived(cameras.list.findIndex((c) => c.id === cam));

  // The keys work on the whole screen, with or without fullscreen. In a field
  // or a select, the arrow and the space belong to it; the calendar also uses
  // the arrows, and marks the key as used.
  function keyHandler(e: KeyboardEvent) {
    if (e.defaultPrevented || e.ctrlKey || e.altKey || e.metaKey) return;
    const target = e.target instanceof HTMLElement ? e.target : null;
    if (target && (target.isContentEditable || target.closest('input, select, textarea'))) return;

    if (e.key === 'Escape' && menu) {
      menu = null;
      return;
    }
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault();
      fastJump(e.key === 'ArrowLeft' ? -1 : 1);
    } else if (e.key === ' ') {
      // Outside the player, space on a button still belongs to the button: the
      // cameras' one, the day's one. Inside it, `preventDefault` also blocks
      // the click - without that, with focus on the ▶, the video would toggle
      // twice.
      if (target && target !== document.body && !full?.contains(target)) return;
      e.preventDefault();
      if (!e.repeat) player.toggle();
    }
  }

  // --- what came from the URL -----------------------------------------------
  //
  // Local time `HH:MM:SS`, not epoch: the `day` above is already a local date
  // and the whole timeline is drawn in local time, so an epoch would be
  // thirteen unreadable digits that would still depend on the time zone to
  // match the day.

  function urlDay() {
    const d = params.get('day') ?? '';
    // Three questions: the format, whether the day exists - the round trip
    // catches `2026-13-45`, which passes any format test but comes back as
    // something else - and whether it isn't in the future, which is the same
    // ceiling as the calendar. There's no tomorrow to replay.
    return /^\d{4}-\d{2}-\d{2}$/.test(d) && dayKey(parseDay(d)) === d && d <= dayKey() ? d : null;
  }

  function hourMs(hms: string | null | undefined, dayMs: number) {
    const m = /^(\d{1,2}):(\d{2}):(\d{2})$/.exec(hms ?? '');
    if (!m) return null;
    const [h, min, s] = [Number(m[1]), Number(m[2]), Number(m[3])];
    if (h > 23 || min > 59 || s > 59) return null;
    return dayMs + (h * 3600 + min * 60 + s) * 1000;
  }

  const dayStartMs = parseDay(initialDay).getTime();

  // Initial instant. Only valid on the FIRST load - after switching days by
  // hand, an old link's instant no longer describes anything -, and that's why
  // load() consumes it by zeroing it.
  let tPending = hourMs(params.get('t'), dayStartMs);
  let pauseOnFirst = params.get('paused') === '1';
  const urlRate = RATES.find((r) => r === Number(params.get('rate'))) ?? null;

  // Timeline window: two times on the same day. An end stuck to midnight comes
  // out as `00:00:00`, so "end <= start" can only be the day rollover. A window
  // shorter than the maximum zoom gets through here and Timeline itself
  // re-anchors to the whole day - the floor is its rule, not this parsing's.
  const urlZoom = (() => {
    const [from, to] = (params.get('zoom') ?? '').split(',');
    const a = hourMs(from, dayStartMs);
    const b = hourMs(to, dayStartMs);
    if (a === null || b === null) return null;
    return { from: a, to: b <= a ? dayStartMs + 86400_000 : b };
  })();

  let viewFrom = $state(urlZoom?.from ?? 0);
  let viewTo = $state(urlZoom?.to ?? 0);

  // --------------------------------------------------------------------------

  const dayStart = $derived(parseDay(day).getTime());
  const dayEnd = $derived(dayStart + 86400_000);
  const isToday = $derived(day === dayKey());

  // The pace comes from the selected camera: `segmentSeconds` is configured per
  // camera (10s to 300s at registration), and a segment only enters the
  // timeline after it closes. Asking once per segment is the natural step -
  // whoever recorded in 10s pieces sees new recording in ~20s, and whoever
  // recorded in 2 min pieces doesn't pay eight whole-day queries for each one
  // that changes something.
  const pollMs = $derived.by(() => {
    const seg = cameras.list.find((c) => c.id === cam)?.segmentSeconds ?? DEFAULT_SEG;
    return Math.min(Math.max(seg * 1000, POLL_MIN_MS), POLL_MAX_MS);
  });

  // Decides what an export of `min` minutes will request, and whether it's
  // possible.
  //
  // The filter repeats the rule of store.Range (internal/store/store.go:372): a
  // whole segment that overlaps the interval is included. Repeating the rule
  // here gives two things the server would only say too late - the real start
  // of the clip, and the two early rejections. They matter because the download
  // goes through location.href: a 404 or a 409 would show up as raw text in a
  // tab, with no way back to the screen.
  function planOf(min: number, from: number): ExportPlan | null {
    if (!from) return null;

    // The count of `min` minutes starts at the beginning of the first segment,
    // which is the `from` sent (see `exportClip`), and not at the instant:
    // counted from the instant, the window would exceed `min` by the part of
    // the segment before it, and for the 10 min one the server would reject
    // with 400 (maxExportSpan).
    const first = timeline.segments.find(([s, d]) => s + d > from);
    if (!first || first[0] >= from + min * 60_000) return { error: 'sem gravação nesse trecho' };
    const to = first[0] + min * 60_000;

    const sel = timeline.segments.filter(([s, d]) => s + d > first[0] && s < to);
    // An MP4 can only have one init, so the server rejects an interval that
    // changes generation (internal/api/recordings.go:327).
    if (sel.some(([, , gi]) => gi !== first[2]))
      return { error: 'o trecho atravessa uma troca de codec' };

    return { start: first[0], to };
  }

  // One plan per offered duration: the menu shows at once which ones can be
  // downloaded. Only with the menu open - closed, nobody looks, and the instant
  // changes several times per second.
  const plans = $derived.by(() => {
    if (menu !== 'baixar') return [];
    const from = Math.round(player.currentMs || 0);
    return DURATIONS.map((min) => ({ min, plan: planOf(min, from) }));
  });
  // Why the shortest duration is rejected and can't be done. The longer ones
  // reject for the same reason or for one only they have (the codec change
  // further ahead).
  const fieldError = $derived(plans.find((p) => p.plan?.error));

  // In localStorage, not the URL: the export duration is a parameter of an
  // ACTION, not of what you're viewing - there's nothing to replay in a link.
  $effect(() => {
    localStorage.setItem('dwnvr.rec.exportMin', String(exportMin));
  });
  $effect(() => {
    localStorage.setItem('dwnvr.rec.thumbnails', showThumbs ? '1' : '0');
  });

  // The URL, by contrast, now describes the whole scene: camera, day, instant,
  // speed, pause, zoom, boxes and labels. That's what makes pasting the address
  // in another tab land on the same stretch of the same camera.
  //
  // Whatever is at its default stays out, so the link doesn't carry what
  // already applies without it. And `paused` comes from the real state of the
  // <video>, not from a flag of its own: the browser may refuse autoplay, and
  // the address bar must not promise a playback that isn't happening.
  $effect(() => {
    if (!mounted) return;
    const wholeDay = !viewTo || (viewFrom <= dayStart && viewTo >= dayEnd);
    write({
      cam,
      day,
      t: player.currentMs ? hhmmss(player.currentMs) : null,
      rate: player.rate === 1 ? null : player.rate,
      paused: player.playing ? null : '1',
      zoom: wholeDay ? null : `${hhmmss(viewFrom)},${hhmmss(viewTo)}`,
      // Thumbnails moved from the URL to localStorage: an old link no longer
      // carries `thumbs=0` forward.
      thumbs: null,
      boxes: showBoxes ? null : '0',
      labels: showLabels ? null : '0',
    });
  });

  // What share says in words: the camera and the instant. Speed, zoom and the
  // rest only the link needs to carry.
  $effect(() => {
    if (!mounted || !cam) return;
    const name = cameras.list.find((c) => c.id === cam)?.name ?? cam;
    const ms = player.currentMs;
    scene.text = ms
      ? `${name}, ${shortDay(ms)} às ${hhmmss(ms)}`
      : `${name}, ${shortDay(parseDay(day).getTime())}`;
  });

  onMount(async () => {
    if (!cameras.list.length) await loadCameras();
    // The link's camera only counts if it still exists: registration goes away,
    // the link stays.
    const fromURL = params.get('cam');
    cam =
      fromURL !== null && cameras.list.some((c) => c.id === fromURL)
        ? fromURL
        : (cameras.list[0]?.id ?? '');
    mounted = true;
  });

  // Switching day through the calendar keeping the time and the zoom: the
  // player's position and the timeline's window are instants of the day being
  // left, so they become the same time of day on the day being entered. Without
  // this load() falls to the start of the day and Timeline, seeing the window
  // outside the new day, re-anchors to the whole day.
  //
  // Outside here (a day that doesn't exist for the camera, in loadDaysData) the
  // old behavior stays: there the time described another day and doesn't apply.
  function changeDay(newItem: string) {
    if (newItem === day) return;
    const displacement = parseDay(newItem).getTime() - dayStart;
    // The target still pending (a link with `t=`, or an earlier switch whose
    // load() hasn't arrived) is already an instant of the day being left; the
    // player is still on a day older than it.
    const now = tPending ?? player.currentMs;
    if (now) tPending = now + displacement;
    // A window not yet anchored (0) stays as it is: Timeline anchors it.
    if (viewTo) {
      viewFrom += displacement;
      viewTo += displacement;
    }
    day = newItem;
  }

  // Walks the camera list wrapping around at the ends.
  function shiftCam(delta: number) {
    const n = cameras.list.length;
    if (!n) return;
    const i = cameras.list.findIndex((c) => c.id === cam);
    cam = cameras.list[(i + delta + n) % n]?.id ?? cam;
  }

  onDestroy(() => {
    player.destroy();
    clearThumbnails();
    clearTimeout(noticeTimer);
    clearTimeout(jumpTimer);
    controls.stop();
  });

  // Hook up the player as soon as the <video> exists in the DOM.
  $effect(() => {
    if (!video || player.video === video) return;
    player.attach(video);
    // Before any seek, and here and not in onMount because only here does the
    // <video> certainly exist: changing the src resets playbackRate, and the
    // player's #reset() preserves the value it finds - it just needs to find it
    // already set.
    if (urlRate) player.setRate(urlRate);
  });

  // Reloads on camera or day change. This is the screen's only point of data
  // fetching, which avoids two screens fighting over the same state.
  //
  // Switching only the camera keeps the instant: it's the same moment seen from
  // another angle. The untrack keeps the player's clock from becoming a
  // dependency and reloading the screen on every frame.
  let camLoaded = '';
  $effect(() => {
    if (!cam || !day) return;
    if (camLoaded && camLoaded !== cam) {
      const now = untrack(() => player.currentMs);
      if (now) tPending = now;
    }
    camLoaded = cam;
    void load(cam, day);
  });

  // The days with recordings, once per camera. It's the only read of this
  // endpoint on the screen: the day that debuts while the screen is open comes
  // in through refresh(), costing no request at all.
  $effect(() => {
    if (cam) void loadDaysData(cam);
  });

  async function loadDaysData(c: string) {
    try {
      const res = await api.days(c);
      if (c !== cam) return;
      daysList = res.days.map((d) => d.day);
      // A day that doesn't exist for this camera - an old link, or the camera
      // that was just switched. The instant that came from the URL described
      // that other day and doesn't survive the switch.
      if (daysList.length && !daysList.includes(day)) {
        tPending = null;
        day = daysList.includes(dayKey()) ? dayKey() : (daysList.at(-1) ?? day);
      }
    } catch {
      // An empty list is the picker's permissive mode: any day up to today
      // becomes reachable again, as it was with the native date field. It
      // doesn't become a warning on screen - if the server really is down, the
      // timeline's load() says so, which is the data the person came to see.
      daysList = [];
    }
  }

  // Today debuting in the calendar, with the screen already open. Local on
  // purpose: refresh() already knows the day, and re-reading /api/rec/days on
  // every closed segment would be a request every 30s for the whole day to find
  // out something the response that just arrived already said.
  //
  // In permissive mode the list stays as it is: a one-day list would move the
  // picker from "everything allowed" to "only today", the opposite of what it
  // should do when it doesn't know the history.
  function registerDay(d: string) {
    if (!daysList.length || daysList.includes(d)) return;
    daysList = [...daysList, d].sort();
  }

  // Today the recording keeps growing while the screen is open: without this,
  // the current day freezes at what existed when the screen opened. A past day
  // doesn't change anymore, so the query isn't even worth it.
  $effect(() => {
    if (!cam || !isToday) return;

    const id = setInterval(() => {
      void refresh();
    }, pollMs);
    // A hidden tab doesn't play video or draw the timeline; on return, what was
    // missed is fetched at once instead of waiting for the next interval.
    const onVisible = () => {
      if (!document.hidden) void refresh();
    };
    document.addEventListener('visibilitychange', onVisible);

    return () => {
      clearInterval(id);
      document.removeEventListener('visibilitychange', onVisible);
    };
  });

  // Each load() gets a number, and only the most recent one touches the screen.
  // Two quick camera switches fire two requests, and nothing guarantees the
  // responses come back in order: the late one would leave the player on one
  // camera and the selector on another.
  let loadAvg = 0;
  // The load whose timeline is on screen. Behind `loadAvg`, it's still the
  // previous camera's or day's.
  let onScreen = 0;

  async function load(c: string, d: string) {
    const mine = ++loadAvg;
    loading = true;
    error = '';
    try {
      const t = await api.timeline(c, d);
      if (mine !== loadAvg) return;
      timeline = t;
      onScreen = mine;
      // A day the calendar offered that came back empty: the list is stale -
      // retention deleted that day with the screen open. Re-reading fixes the
      // list and leads to a day that still exists. It only happens in that
      // mismatch, which is why the poll doesn't need to re-read anything.
      if (!t.segments.length && daysList.includes(d)) void loadDaysData(c);
      player.setSource(c, t.gens, t.segments);
      clearThumbnails();
      // On a past day, the start is the most useful; today, the most recent.
      // The instant that came from the URL takes priority over that, but only
      // this once.
      const edge = d === dayKey() ? t.segments.at(-1) : t.segments[0];
      if (edge) {
        const target = tPending ?? edge[0];
        tPending = null;
        await player.seek(target);
        // The camera or the day changed during the seek: the pause would stop
        // the new one. And the link's pause still applies to it, since it's the
        // first to play.
        if (mine !== loadAvg) return;
        // After the seek, never before: it ends in play(). Also only this once
        // - from then on the button decides.
        if (pauseOnFirst) player.pause();
        pauseOnFirst = false;
      }
    } catch (e) {
      if (mine !== loadAvg) return;
      error = messageOf(e);
      timeline = { cam: c, ranges: [], segments: [], gens: [] };
      onScreen = mine;
    } finally {
      // The "carregando…" belongs to the most recent load(): the late one
      // doesn't clear it.
      if (mine === loadAvg) loading = false;
    }

    // Afterward, and separately. A camera with detection off doesn't even ask:
    // whoever only records pays no request for the strip. The price is that the
    // marks from a period when it was on disappear from the screen together
    // with turning it off.
    let ev: Pick<Events, 'onsets' | 'objects'> = { onsets: [], objects: [] };
    if (detectEnabled(c)) {
      try {
        ev = await api.events(c, d);
      } catch {
        // Without marks the strip just doesn't appear; the timeline is already
        // on screen.
      }
    }
    if (mine === loadAvg) {
      events = ev.onsets;
      objects = ev.objects;
    }
  }

  function detectEnabled(c: string) {
    return !!cameras.list.find((x) => x.id === c)?.detect;
  }

  // Stitches the freshly fetched tail onto what the screen already had.
  //
  // The partial query starts 1ms after the start of the last known segment, and
  // the server returns everything that ENDS after that - meaning that last
  // segment comes back too. The one-segment overlap is on purpose: it reveals
  // whether the tail's first strip continues the last old strip, without
  // repeating here the stitching tolerance the server uses. The decision
  // already comes made inside the response.
  function stitch(current: DayData, tail: DayData, sinceMs: number): DayData | null {
    const newOnes = tail.segments.filter(([start]) => start > sinceMs);
    if (!newOnes.length) return null;

    // The tail's generation table has its own indexes, starting from zero:
    // using them directly would point to the wrong generation in the old list.
    const gens = [...current.gens];
    const segments: Segment[] = [
      ...current.segments,
      ...newOnes.flatMap(([start, dur, gi]): Segment[] => {
        const g = tail.gens[gi];
        if (g === undefined) return []; // segment without init: the server never sends one
        let j = gens.indexOf(g);
        if (j < 0) j = gens.push(g) - 1;
        return [[start, dur, j]];
      }),
    ];

    const ranges = current.ranges.map(([begin, end]): Span => [begin, end]);
    for (const [begin, end] of tail.ranges) {
      const lastOne = ranges.at(-1);
      if (lastOne && begin <= lastOne[1]) lastOne[1] = Math.max(lastOne[1], end);
      else ranges.push([begin, end]);
    }

    return { ...current, ranges, segments, gens };
  }

  let refreshing = false;

  async function refresh() {
    // The midnight rollover doesn't change `day` by itself: the day that was
    // today stops growing and doesn't need to be re-read. And before the most
    // recent load() puts its timeline on screen, what's there is the previous
    // camera's or day's: the tail would be stitched onto it. load() already
    // brings the whole day.
    if (refreshing || onScreen !== loadAvg || document.hidden || day !== dayKey()) return;
    refreshing = true;
    const c = cam;
    const d = day;
    const base = timeline;
    const last = base.segments.at(-1);
    try {
      // With nothing in hand - a day that dawned empty, or a first load that
      // failed - there's no tail to ask for and the whole day applies.
      const t = last ? await api.timelineRange(c, last[0] + 1, dayEnd) : await api.timeline(c, d);

      // Camera, day or the timeline itself changed during the fetch: the new
      // load() is in charge, and stitching over it would mix two cameras.
      if (c !== cam || d !== day || timeline !== base) return;

      if (!last) {
        if (!t.segments.length) return;
        // The day's first recording arriving with the screen already open: it's
        // the same case as the initial load(), including going to the most
        // recent instant.
        registerDay(d);
        timeline = t;
        player.setSource(c, t.gens, t.segments);
        const edge = t.segments.at(-1);
        if (edge) void player.seek(edge[0]);
        return;
      }

      // Nothing new closed since the last round: the screen stays as it is,
      // without swapping any object - the thumbnail strip and the bar don't
      // redo their math for nothing.
      const joined = stitch(base, t, last[0]);
      if (!joined) return;

      timeline = joined;
      void player.updateSegments(joined.gens, joined.segments);
    } catch {
      // Background update: a transient failure must not erase the timeline that
      // was already good from the screen nor interrupt playback.
    } finally {
      refreshing = false;
    }

    await eventsTail(c, d);
  }

  // The marks' tail, with the same design as the timeline's tail: it requests
  // only the recent piece, and appends at the end.
  //
  // A mark is an instant, not an interval: nothing about it changes after it's
  // recorded. But the OBJECT mark arrives late - it carries the instant of the
  // onset that originated it and is only recorded after the object detector
  // looks, with the clip waiting its turn in the queue. Requesting from the
  // last onset would lose forever the object of an onset that was already on
  // screen. That's why the tail goes back OBJECT_DELAY_MS before the last
  // onset, and what was already there is filtered out.
  //
  // The maximum delay comes from internal/detect/params.go: each camera holds
  // up to ClipsPerCamera (2) in the queue, and an inference gives up at
  // InferenceTimeoutMs (60 s). The worst case is 2 x cameras x 60 s, with the
  // detector at its limit on all of them, and 30 min covers it up to 15
  // cameras. With a healthy detector, the delay is seconds; the slack costs a
  // few KB of onsets per round.
  const OBJECT_DELAY_MS = 30 * 60_000;

  async function eventsTail(c: string, d: string) {
    if (!detectEnabled(c)) return;
    const last = events.at(-1) ?? dayStart - 1;
    const since = Math.max(dayStart, last - OBJECT_DELAY_MS);
    try {
      const ev = await api.eventsRange(c, since, dayEnd);
      if (c !== cam || d !== day) return;
      const onsets = ev.onsets.filter((t) => t > last);
      if (onsets.length) events = [...events, ...onsets];
      // An inference writes one row per object, and two of the same family at
      // the same instant are only told apart by the box. Instant and family
      // aren't enough: a round that fell between the two writes would lose the
      // second.
      const key = (o: MarkedObject) => `${o.atMs}|${o.class}|${o.score}|${o.box?.join(',') ?? ''}`;
      const known = new Set(objects.map(key));
      const newOnes = ev.objects.filter((o) => !known.has(key(o)));
      if (newOnes.length) objects = [...objects, ...newOnes];
    } catch {
      // Same as the timeline: a background failure doesn't erase what's already
      // on screen.
    }
  }

  // The `from` sent is the REAL start of the first segment, not the cursor's
  // instant. The previous one ends exactly at that instant and the server's
  // test is `>`, so it stays out; and the file name, which comes from the
  // requested `from` (internal/api/recordings.go:352), now matches the content.
  function exportClip(min: number) {
    const plan = planOf(min, Math.round(player.currentMs || 0));
    menu = null;
    if (!plan?.start) return;
    exportMin = min;
    buttonNotice(`Baixando ${min} min de vídeo a partir de ${hhmmss(plan.start)}`);
    location.href = mediaURL.export(cam, plan.start, plan.to);
  }

  async function capture() {
    menu = null;
    buttonNotice(`Baixando a imagem das ${hhmmss(player.currentMs)}`);
    // The name follows the same convention as the export
    // (cam_AAAA-MM-DD_HH-MM-SS), so the image and the video of the same instant
    // sit side by side in the folder. And the instant is the one SHOWN, not the
    // current time: a capture from yesterday named with today would be useless.
    const d = new Date(player.currentMs);
    const name = `${cam}_${dayKey(d)}_${hhmmss(d.getTime()).replaceAll(':', '-')}.jpg`;
    try {
      // The image comes out as the screen is: with the boxes lit, if they're
      // on.
      const layer = showBoxes ? boxesLayer : null;
      await downloadFrame(video, name, layer ? layer.drawOn : undefined);
    } catch (e) {
      captureError = messageOf(e);
      clearTimeout(noticeTimer);
      noticeTimer = setTimeout(() => {
        captureError = '';
      }, 5000);
    }
  }
</script>

<svelte:window onkeydown={keyHandler} onclick={outsideMenu} onresize={measurePage} />

<div
  class="page"
  bind:this={page}
  style:height={pageHeight ? `${pageHeight}px` : null}
  style:--page-height="{pageHeight}px"
  style:--page-width={pageWidth ? `${pageWidth}px` : null}
  style:--row-height="{rowHeight}px"
  style:--caption-height="{captionHeight}px"
>
  {#if !cameras.list.length && cameras.error}
    <!-- An empty list because the load failed is not "nenhuma câmera". -->
    <p class="empty">Não deu para carregar as câmeras: {cameras.error}</p>
  {:else if !cameras.list.length && !cameras.loading}
    <NoCameras />
  {:else}
    <div class="bar row">
      <div class="row camnav">
        <button
          class="ghost"
          onclick={() => {
            shiftCam(-1);
          }}
          disabled={cameras.list.length < 2}
          aria-label="câmera anterior"
        >
          <Icon name="chevronLeft" size={18} />
        </button>
        <!-- The blur gives the arrows back to the skips, as in the speed select. -->
        <select
          bind:value={cam}
          onchange={(e) => {
            e.currentTarget.blur();
          }}
          aria-label="câmera"
        >
          {#each cameras.list as c (c.id)}
            <option value={c.id}>{c.name}</option>
          {/each}
        </select>
        <button
          class="ghost"
          onclick={() => {
            shiftCam(1);
          }}
          disabled={cameras.list.length < 2}
          aria-label="próxima câmera"
        >
          <Icon name="chevronDir" size={18} />
        </button>
      </div>

      <DayNav value={day} days={daysList} onchange={changeDay} />
    </div>

    <!-- What goes fullscreen. Inside, what doesn't make it in (thumbnails and
         the thumbnail, box and download buttons) is removed from the DOM
         instead of just hidden: the thumbnails don't decode anything for nobody
         to see. -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="full"
      class:fs={inFullscreen}
      class:pinned={pinnedControls}
      class:hidden={hiddenControls}
      bind:this={full}
      {@attach (node: HTMLElement) => () => {
        // A de verdade o navegador desfaz sozinho quando o elemento sai da
        // página; a simulada ficaria cobrindo a tela seguinte.
        if (fullscreen.el === node) leaveFullscreen();
      }}
      onpointerdowncapture={() => {
        if (!inFullscreen) return;
        touch.cancel();
        controls.touched();
      }}
      onpointermove={(e) => {
        if (inFullscreen && e.pointerType === 'mouse') controls.show();
      }}
    >
      <div
        class="stage"
        class:animating
        class:switching
        class:shift={animating || drag !== 0}
        style:--swap="{SWITCH_MS}ms"
        style:--drag="{drag}px"
        bind:this={stage}
        onpointerdown={gestureStart}
        onpointermove={gestureMove}
        onpointerup={(e) => {
          if (e.pointerId === released) {
            released = null;
            return;
          }
          if (!gestureEnd(e)) touch(e);
        }}
        onpointercancel={(e) => {
          if (e.pointerId === released) released = null;
          gestureEnd(e);
        }}
        ondblclick={() => {
          if (lastPointer !== 'touch' && Date.now() - draggedAt > 2 * SWITCH_MS) toggleFull();
        }}
      >
        <video bind:this={video} playsinline controls={false}></video>
        {#if Boxes && showBoxes && objects.length}
          <Boxes
            bind:this={boxesLayer}
            {video}
            {objects}
            currentMs={player.currentMs}
            playing={player.playing}
            rate={player.rate}
            labels={showLabels}
          />
        {/if}
        <!-- Stacked in a column, instead of each in its own `position: absolute`:
           before, three notices fought for the same corner, and the capture
           notice arrives precisely when "carregando…" may be lit. -->
        <div class="notices">
          {#if player.buffering || loading}<span class="badge">carregando…</span>{/if}
          {#if !loading && !error && !timeline.segments.length}
            <span class="badge">sem gravação neste dia</span>
          {/if}
          {#if player.error}<span class="badge bad">{player.error}</span>{/if}
          {#if error}<span class="badge bad">{error}</span>{/if}
          {#if captureError}<span class="badge bad">{captureError}</span>{/if}
        </div>
        {#if jumpHint}
          <div class="half-moon" class:right={jumpHint.side > 0} out:fade={{ duration: 250 }}>
            <Icon name={jumpHint.side < 0 ? 'skipLeft' : 'skipRight'} size={26} />
            <span>{jumpHint.sum} segundos</span>
          </div>
        {/if}
        {#if !inFullscreen}
          <span class="enter"
            ><FullscreenButton active={false} overVideo onclick={toggleFull} /></span
          >
        {/if}
      </div>

      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="dock"
        onpointerenter={(e) => {
          if (e.pointerType === 'mouse') overControls = true;
        }}
        onpointerleave={(e) => {
          if (e.pointerType !== 'mouse') return;
          overControls = false;
          if (inFullscreen) controls.show();
        }}
      >
        <div class="controls row">
          <button
            class="primary"
            onclick={() => {
              player.toggle();
            }}
            aria-label={player.playing ? 'pausar' : 'tocar'}
          >
            <Icon name={player.playing ? 'pause' : 'play'} size={18} />
          </button>
          <Clock
            ms={player.currentMs}
            ongo={(seg) => {
              void player.seek(dayStart + seg * 1000);
            }}
          />

          <select
            value={player.rate}
            onchange={(e) => {
              player.setRate(Number(e.currentTarget.value));
              // Senão as setas seguintes trocariam a velocidade, e não pulariam.
              e.currentTarget.blur();
            }}
            aria-label="velocidade"
          >
            {#each RATES as r (r)}<option value={r}>{taxa(r)}</option>{/each}
          </select>

          <!-- In fullscreen there's no top bar: the camera and its arrows come
               here, and not to the video's sides, which on a phone belong to
               the double tap that skips 10 s. They stay on this row when they
               fit, and on a row of their own above it when they don't (the CSS
               decides by width, not by name: switching camera doesn't move the
               strip). -->
          {#if inFullscreen && camPosition >= 0}
            <div class="camnav navfs">
              <button
                class="ghost"
                onclick={() => {
                  ir(-1);
                }}
                disabled={cameras.list.length < 2}
                aria-label="câmera anterior"
              >
                <Icon name="chevronLeft" size={18} />
              </button>
              <!-- The position stays outside the name: the "…" truncates only the name. -->
              <span class="cam-name">{cameras.list[camPosition]?.name}</span>
              <span class="pos">{camPosition + 1}/{cameras.list.length}</span>
              <button
                class="ghost"
                onclick={() => {
                  ir(1);
                }}
                disabled={cameras.list.length < 2}
                aria-label="próxima câmera"
              >
                <Icon name="chevronDir" size={18} />
              </button>
            </div>
          {/if}

          <span class="spacer"></span>
          {#if inFullscreen}
            <!-- Icon only: the `title` explains on desktop, and on a phone whoever
             taps sees right away what changed. -->
            <button
              class="ghost pin"
              class:on={pinnedControls}
              onclick={() => (pinnedControls = !pinnedControls)}
              aria-label="fixar os controles"
              aria-pressed={pinnedControls}
              title={pinnedControls
                ? 'soltar os controles: somem sozinhos com o vídeo tocando'
                : 'fixar os controles embaixo do vídeo'}
            >
              <Icon name="pin" size={22} />
            </button>
            <FullscreenButton active onclick={toggleFull} />
          {:else}
            <!-- On a phone only the icon, and the tap explains itself in the
                 snackbar; on desktop the label shows beside it. -->
            <button
              class="ghost ib"
              class:on={showThumbs}
              onclick={toggleThumbnails}
              aria-label={showThumbs ? 'ocultar miniaturas' : 'exibir miniaturas'}
              aria-pressed={showThumbs}
            >
              <Icon name="thumbnails" size={20} />
              <span class="label" bind:this={buttonLabel}>miniaturas</span>
            </button>
            <!-- Only with an object in the day, like the legend colors: a camera
           without detection has no box to show. -->
            {#if objects.length}
              <button
                class="ghost ib"
                class:on={showBoxes}
                onclick={toggleBoxes}
                aria-label="caixas de detecção: {BOXES_STATES[boxesState]}. Toque para trocar"
              >
                <Icon name={showLabels && showBoxes ? 'boxesLabel' : 'boxes'} size={20} />
                <span class="label">{BOXES_STATES[boxesState]}</span>
              </button>
            {/if}
            <span class="anchor">
              <button
                class="ghost ib"
                onclick={() => (menu = menu === 'baixar' ? null : 'baixar')}
                aria-label="baixar imagem ou vídeo"
                aria-haspopup="menu"
                aria-expanded={menu === 'baixar'}
              >
                <Icon name="download" size={20} />
                <span class="label">baixar</span>
              </button>
              {#if menu === 'baixar'}
                <div class="pop above" role="menu">
                  <div class="title">Baixar</div>
                  <button
                    class="image"
                    role="menuitem"
                    onclick={capture}
                    disabled={!player.currentMs}
                  >
                    <Icon name="image" size={18} /> Imagem deste instante
                  </button>
                  <div class="title">Vídeo a partir deste instante</div>
                  <div class="durations">
                    {#each plans as { min, plan } (min)}
                      <button
                        role="menuitem"
                        class:last={min === exportMin}
                        onclick={() => {
                          exportClip(min);
                        }}
                        disabled={!plan?.start}
                        title={plan?.error}
                      >
                        {min} min
                      </button>
                    {/each}
                  </div>
                  <!-- The disabled button says it can't be done; alone, it
                       wouldn't say why. -->
                  {#if fieldError?.plan?.error}
                    <p class="field-error small">
                      {fieldError.min === DURATIONS[0]
                        ? ''
                        : `de ${fieldError.min} min em diante: `}{fieldError.plan.error}
                    </p>
                  {/if}
                </div>
              {/if}
            </span>
          {/if}
        </div>

        {#if showThumbs && !inFullscreen}
          <!-- The camera comes from the timeline, not the selector: when switching
          camera the selector changes immediately and the segments only when the
          new timeline arrives. In the meantime the strip requested each
          thumbnail with the new camera and the old one's instant - a 404 per
          thumbnail, ahead of the good ones in the queue. -->
          <div class="thumbnails">
            <ThumbStrip
              cam={timeline.cam}
              segments={timeline.segments}
              {objects}
              currentMs={player.currentMs}
              defer={player.loading}
              onseek={(ms: number) => {
                void player.seek(ms);
              }}
            />
          </div>
        {/if}

        <div class="row" bind:offsetHeight={rowHeight}>
          <Timeline
            ranges={timeline.ranges}
            {events}
            {objects}
            {dayStart}
            {dayEnd}
            currentMs={player.currentMs}
            bind:viewFrom
            bind:viewTo
            onseek={(ms: number) => {
              void player.seek(ms);
            }}
          />
        </div>
      </div>
    </div>

    <!-- The gestures live in the ⓘ bubble: they're for whoever doesn't know
         them yet, and in plain view they took more rows than the whole legend
         on a phone. The bubble sits outside the legend because, in landscape,
         the legend scrolls sideways and would clip it. -->
    <div class="footer anchor" bind:offsetHeight={captionHeight}>
      <div class="legend muted small row wrap">
        <span><i class="has"></i> com gravação</span>
        <span><i class="gap"></i> sem gravação</span>
        {#if events.length}
          <span><i class="heat"></i> movimento</span>
        {/if}
        <!-- All three, and not just the day's: the legend explains the color
           before it appears, and doesn't change size with every new mark. -->
        {#if objects.length}
          {#each Object.values(FAMILIES) as f (f.name)}
            <span><i style:background={f.color}></i> {f.name}</span>
          {/each}
        {/if}
        <button
          class="ghost info"
          onclick={() => (menu = menu === 'gestos' ? null : 'gestos')}
          aria-label="gestos da timeline"
          aria-expanded={menu === 'gestos'}
        >
          <Icon name="info" size={16} />
        </button>
      </div>
      {#if menu === 'gestos'}
        <div class="pop above gestures" role="dialog" aria-label="gestos da timeline">
          toque para pular · arraste para navegar · duplo toque aproxima, dois dedos afastam · pince
          ou role para dar zoom
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  /* A column with the window's height (measured in `measurePage`): the video
     gets what the others leave. */
  .page {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 10px;
    /* The video's width at the height that's left (measured in `measureWidth`). */
    max-width: var(--page-width, none);
    margin: 0 auto;
  }

  /* Camera and day on a single row: the camera name is the one that yields. */
  .bar {
    flex-wrap: nowrap;
  }
  .bar select {
    max-width: 45vw;
    min-width: 0;
  }
  .camnav {
    gap: 4px;
    min-width: 0;
  }
  .camnav button {
    padding: 9px 6px;
  }

  .stage {
    position: relative;
    background: #000;
    border-radius: var(--radius);
    overflow: hidden;
    /* The double tap and the sideways swipe are ours: the browser keeps only
       vertical scrolling and pinch. */
    touch-action: pan-y pinch-zoom;
    user-select: none;
    -webkit-user-select: none;
  }

  /* What slides with the finger: the video and the boxes over it. The notices,
     the half-moon and the fullscreen button stay. The `transform` only exists
     during the gesture: at rest, the video doesn't get a layer of its own for
     nothing. */
  .stage.shift > video,
  .stage.shift > :global(canvas) {
    transform: translateX(var(--drag));
  }
  .stage.animating > video,
  .stage.animating > :global(canvas) {
    transition: transform var(--swap) ease-out;
  }
  .stage.switching > video,
  .stage.switching > :global(canvas) {
    visibility: hidden;
  }

  /* The 10 s skip feedback, like YouTube's: a translucent arc stuck to the edge
     on the skip side. It passes touches through, to the video. */
  .half-moon {
    position: absolute;
    top: -10%;
    bottom: -10%;
    left: 0;
    width: 42%;
    border-radius: 0 50% 50% 0 / 0 50% 50% 0;
    background: rgba(255, 255, 255, 0.12);
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 4px;
    color: #fff;
    font-size: 13px;
    font-weight: 600;
    pointer-events: none;
    user-select: none;
  }
  .half-moon.right {
    left: auto;
    right: 0;
    border-radius: 50% 0 0 50% / 50% 0 0 50%;
  }
  video {
    width: 100%;
    height: 100%;
    object-fit: contain;
    display: block;
    background: #000;
  }

  /* Outside fullscreen the stage grows to 16:9 by width and shrinks for the
     rest to fit: when height is what rules, black bars appear on the sides. */
  .full:not(.fs) {
    /* Sized to its content, and it only shrinks: the leftover goes below the
       legend, not between it and the timeline. */
    flex: 0 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .full:not(.fs) .stage {
    flex: 0 1 auto;
    width: 100%;
    aspect-ratio: 16 / 9;
    min-height: 120px;
  }
  .full:not(.fs) .dock {
    flex: none;
    display: grid;
    gap: 8px;
  }
  /* The strip scrolls inside: without the zero floor, its width would open up the page. */
  .thumbnails {
    min-width: 0;
  }

  /* A single row. The speed select trims its padding so the three icons fit
     beside it at 360 px. */
  .controls {
    flex-wrap: nowrap;
    gap: 3px;
  }
  .controls > select {
    padding: 9px 2px 9px 6px;
  }
  .controls .spacer {
    min-width: 0;
  }

  /* The icon-only buttons: thumbnails, boxes and download. The label appears
     where it fits (desktop). Pressed state, like the Live pressed buttons. */
  .ib {
    display: inline-grid;
    grid-auto-flow: column;
    align-items: center;
    justify-content: center;
    gap: 7px;
    min-width: 44px;
    padding: 0 10px;
    color: var(--dim);
  }
  .ib.on {
    color: var(--fg);
    border-color: var(--accent);
    background: var(--active-bg);
  }
  .label {
    display: none;
    font-size: 14px;
  }
  @media (min-width: 720px) {
    .label {
      display: inline;
    }
  }

  /* The download menu, in the design of the account menu. */
  .anchor {
    position: relative;
  }
  .pop {
    position: absolute;
    z-index: 30;
    right: 0;
    top: calc(100% + 6px);
    width: max-content;
    min-width: 250px;
    max-width: calc(100vw - 24px);
    padding: 6px;
    display: grid;
    gap: 2px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  }
  .pop.above {
    top: auto;
    bottom: calc(100% + 6px);
  }
  .pop .title {
    padding: 6px 10px 2px;
    font-size: 12px;
    color: var(--dim);
  }
  /* Button-like, like the durations right below: loose in the menu, with no
     background or border, it read as a heading. */
  .image {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    margin: 2px 10px 6px;
    min-height: 38px;
    padding: 4px 10px;
    font-size: 13px;
  }
  .durations {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    padding: 2px 10px 6px;
  }
  .durations button {
    flex: 1;
    min-height: 38px;
    padding: 4px 10px;
    font-size: 13px;
    white-space: nowrap;
  }
  /* The last downloaded duration: border only, because the tap downloads, it doesn't choose. */
  .durations button.last {
    border-color: var(--accent);
  }
  .field-error {
    margin: 0;
    padding: 0 10px 6px;
    color: var(--bad);
  }

  /* Phone in landscape: video on the left, and camera, day, controls and
     thumbnails on the right; the timeline and the legend at the bottom at full
     width, because the timeline is where you navigate. The video's height is
     the screen's minus theirs. */
  @media (orientation: landscape) and (max-height: 500px) {
    .page {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr);
      grid-template-rows: auto auto 1fr auto auto;
      grid-template-areas: 'stage bar' 'stage ctl' 'stage mini' 'timeline timeline' 'caption caption';
      gap: 8px 10px;
      /* Here the video goes to the side: the width computation doesn't apply. */
      max-width: none;
    }
    .full:not(.fs),
    .full:not(.fs) .dock {
      display: contents;
    }
    .full:not(.fs) .stage {
      grid-area: stage;
      width: auto;
      height: calc(
        var(--page-height) - 20px - var(--row-height) - 8px - var(--caption-height) - 8px
      );
      max-width: calc(100vw - 280px);
      min-height: 0;
    }
    .bar {
      grid-area: bar;
      flex-wrap: wrap;
      row-gap: 6px;
    }
    .controls {
      grid-area: ctl;
    }
    .thumbnails {
      grid-area: mini;
      align-self: start;
    }
    .row {
      grid-area: timeline;
    }
    /* A single row, scrolling sideways: wrapped into three, it took more than
       half the height from the video. */
    .footer {
      grid-area: caption;
    }
    .legend {
      flex-wrap: nowrap;
      overflow-x: auto;
      white-space: nowrap;
      scrollbar-width: none;
      /* Scrolling inside, the grid would squeeze it down to zero. */
      min-height: min-content;
    }
    .label {
      display: none;
    }
    /* The controls are at the top: the download menu opens downward. */
    .controls .pop.above {
      top: calc(100% + 6px);
      bottom: auto;
    }
  }

  .enter {
    position: absolute;
    right: 8px;
    bottom: 8px;
  }

  /* In fullscreen: without `position`, because the browser (`:fullscreen`) or
     the simulated-fullscreen rule in app.css sets it, and it anchors the
     controls strip. */
  .full.fs {
    display: flex;
    flex-direction: column;
    gap: 0;
    background: #000;
  }
  .full.fs .stage {
    flex: 1;
    min-height: 0;
    border-radius: 0;
  }
  .full.fs video {
    width: 100%;
    height: 100%;
    aspect-ratio: auto;
    max-height: none;
  }
  /* Pinned: the strip below the video, which gets what's left of the height. */
  .full.fs .dock {
    gap: 8px;
    padding: 8px max(8px, env(safe-area-inset-right)) max(8px, env(safe-area-inset-bottom))
      max(8px, env(safe-area-inset-left));
    background: var(--bg);
    border-top: 1px solid var(--line);
  }
  /* Loose: over the video, on a gradient that keeps it readable on a bright
     video. */
  .full.fs:not(.pinned) .dock {
    position: absolute;
    inset: auto 0 0 0;
    padding-top: 28px;
    background: linear-gradient(to bottom, transparent, rgba(0, 0, 0, 0.75) 28px);
    border-top: 0;
    transition:
      opacity 0.25s,
      transform 0.25s;
  }
  .full.hidden .dock {
    opacity: 0;
    transform: translateY(12px);
    pointer-events: none;
  }
  .full.hidden {
    cursor: none;
  }

  /* The pin, the same size as the fullscreen button beside it. */
  .pin {
    display: grid;
    place-items: center;
    width: 44px;
    padding: 0;
  }
  .pin.on {
    background: var(--accent);
    border-color: var(--accent);
    color: #fff;
  }

  .notices {
    position: absolute;
    top: 8px;
    left: 8px;
    right: 8px;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
    /* The column covers the stage's width so the text can wrap on a phone, but
       it must not become an invisible lid over the video. */
    pointer-events: none;
  }

  .badge {
    background: rgba(0, 0, 0, 0.7);
    border: 1px solid var(--line);
    border-radius: 6px;
    padding: 3px 8px;
    font-size: 12px;
  }
  .badge.bad {
    color: var(--bad);
    border-color: #5c2b2b;
  }

  /* The camera in the fullscreen strip, with the top bar's arrows. On the same
     row as the ▶ when the strip is 520 px; below that it goes on a row of its
     own, centered, above the controls. The cutoff is by width, not by name, so
     the strip doesn't change when switching camera. */
  .full.fs .dock {
    container-type: inline-size;
  }
  .navfs {
    display: flex;
    align-items: center;
    flex: 0 1 auto;
    min-width: 0;
  }
  .navfs button {
    flex: none;
  }
  .cam-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    padding: 0 4px;
  }
  /* Outside the name and without shrinking: a long name is truncated before it. */
  .pos {
    flex: none;
    margin-left: 2px;
    padding-right: 4px;
    color: var(--dim);
    font-variant-numeric: tabular-nums;
  }
  /* 520 px: ▶, clock, speed, about 10 characters of name with the arrows, pin
     and exit. */
  @container (max-width: 519.98px) {
    .controls:has(.navfs) {
      flex-wrap: wrap;
      row-gap: 4px;
    }
    .navfs {
      order: -1;
      flex: 1 0 100%;
      justify-content: center;
    }
  }
  .footer {
    flex: none;
  }
  .legend {
    gap: 14px;
    margin: 0;
  }
  /* The ⓘ at the end of the row, without fattening it: the touch target extends
     past the row through the negative margins. */
  .info {
    display: grid;
    place-items: center;
    width: 32px;
    min-height: 32px;
    margin: -6px 0 -6px -8px;
    padding: 0;
    border: 0;
    border-radius: 50%;
    color: var(--dim);
  }
  .info[aria-expanded='true'] {
    color: var(--accent);
  }
  /* The bubble opens above the timeline, aligned to the left of the legend. */
  .pop.gestures {
    left: 0;
    right: auto;
    min-width: 0;
    width: max-content;
    max-width: min(360px, 100%);
    padding: 10px 12px;
    font-size: 13px;
    line-height: 1.45;
    color: var(--dim);
  }
  .legend i {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: 2px;
    vertical-align: -1px;
  }
  .has {
    background: var(--accent);
  }
  .gap {
    background: #1b1f24;
    border: 1px solid var(--line);
  }
  /* The same amber as the heat strip, in HEAT_COLOR in Timeline.svelte. */
  .heat {
    background: #d29922;
  }
</style>
