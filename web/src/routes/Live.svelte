<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import '../vendor/video-stream.js';
  import { VideoRTC } from '../vendor/video-rtc.js';
  import { cameras, loadCameras, session, sessionDropped } from '../lib/state.svelte.ts';
  import { currentParams, write, scene } from '../lib/route.svelte.ts';
  import { api, mediaURL, messageOf } from '../lib/api.ts';
  import type { Session } from '../lib/api.ts';
  import { downloadFrame } from '../lib/capture.ts';
  import { dayKey, hhmmss } from '../lib/format.ts';
  import NoCameras from '../components/NoCameras.svelte';
  import CamerasButton from '../components/CamerasButton.svelte';
  import CameraPicker from '../components/CameraPicker.svelte';
  import FullscreenButton from '../components/FullscreenButton.svelte';
  import { fullscreen, enterFullscreen, leaveFullscreen } from '../lib/fullscreen.svelte.ts';
  import { vanishingControls } from '../lib/vanishingControls.svelte.ts';
  import { doubleTouch } from '../lib/doubleTouch.ts';
  import { freeHeight } from '../lib/freeHeight.ts';
  import { newScreen } from '../lib/screen-version.svelte.ts';
  import { SWITCH_MS, AXIS_PX, swipeLimit } from '../lib/swipe.ts';
  import Icon from '../components/Icon.svelte';

  const STORAGE_KEY = 'dwnvr.live.selection';
  const LAYOUT_KEY = 'dwnvr.live.layout';
  const ALWAYS_KEY = 'dwnvr.live.alwaysOn';

  // How many columns, or fit everything on screen.
  type Mode = 'fit' | number;

  const COLUMNS = [1, 2, 3];
  // 'fit' is just one more mode beside the columns: since one excludes the
  // other by construction, choosing 2× turns off fit with no code for it.
  const MODES: Mode[] = ['fit', ...COLUMNS];

  // Must match the grid's `gap` in the CSS: it is the space the fit computation
  // subtracts before dividing what is left among the tiles.
  const GAP = 8;
  const RATIO = 16 / 9;
  // Must match the `.page` `gap` in the CSS: the breathing room between the bar
  // and the stage, which the columns' height subtracts.
  const PAGE_GAP = 10;
  // Below this the tile gets too small to see the camera: the grid scrolls
  // instead of shrinking further.
  const MIN_COLUMNS_HEIGHT = 180;

  // Up to how many cameras the share text names by name.
  const NAMES_IN_TEXT = 3;

  // How long a tile scrolled off screen stays on before the cut. Long on
  // purpose: going back to where you were a moment ago must not cost a wait for
  // a keyframe. See `onlyOnScreen`.
  const SCROLL_GRACE_MS = 30_000;

  // Minimum interval between one connection attempt and the next, growing with
  // each consecutive failure and going back to the first when the video plays.
  // The go2rtc player uses a fixed 15 s. See `reconnectFast`.
  const RECONNECT_MS = [500, 1000, 2000, 5000];
  const RECONNECT_MAX_MS = 5000;
  // The wait after `n` consecutive failures: the last one repeats.
  const reconnectAt = (n: number) =>
    RECONNECT_MS[Math.min(n, RECONNECT_MS.length - 1)] ?? RECONNECT_MAX_MS;

  // How long each server probe waits before giving up, when coming back from a
  // hidden tab. See `waitNetwork`.
  const PROBE_MS = 2000;

  // How often the screen checks the session while the video is in view, and how
  // long each check waits for the response. See `checkSession`.
  const CHECK_SESSION_MS = 60_000;
  const CHECK_TIMEOUT_MS = 10_000;
  // The tick that decides whether it is time for the routine check.
  const CHECK_TICK_MS = 5000;
  // While the session is not confirmed: how many seconds from the end of an
  // attempt to the next one, the ones in the countdown the notice shows.
  const RETRY_WAIT_S = 5;

  // Read once, at initialization: from here on the screen state is in charge,
  // and it writes back to the URL.
  const params = currentParams();

  let selected = $state(new Set<string>());
  let mode = $state<Mode>(readMode());
  let showPicker = $state(false);
  let stage = $state<HTMLDivElement | null>(null);
  let fitting = $state({ cols: 1, w: 0 });
  // In columns, the height a tile can have without exceeding the window: it,
  // and not a fixed width, is what limits the grid - see `columnsWidth`.
  let columnsHeight = $state(0);
  let bar = $state<HTMLDivElement | null>(null);
  // Holds the URL write until the selection has been read - see the effect at
  // the end of the block.
  let mounted = $state(false);

  const visible = $derived(cameras.list.filter((c) => selected.has(c.id)));
  const allChecked = $derived(cameras.list.length > 0 && selected.size === cameras.list.length);

  // The URL comes first, then localStorage, and only then the guess by screen
  // width. The URL describes THIS link; localStorage, this browser's habit.
  // Reading from there never writes here - otherwise opening someone else's
  // link would become the opener's preference.
  //
  // All of this at initialization, not in onMount: always starting at 2× and
  // correcting afterward would make the grid flicker on the phone, where the
  // default is 1×.
  function readMode(): Mode {
    const fromURL = params.get('view');
    if (fromURL === 'fit') return 'fit';
    if (COLUMNS.includes(Number(fromURL))) return Number(fromURL);
    return readLayout();
  }

  function readLayout(): Mode {
    const saved = localStorage.getItem(LAYOUT_KEY);
    if (saved === 'fit') return 'fit';
    if (COLUMNS.includes(Number(saved))) return Number(saved);
    return matchMedia('(min-width: 640px)').matches ? 2 : 1;
  }

  onMount(async () => {
    if (!cameras.list.length) await loadCameras();

    // An empty `cams=` is a choice - "no cameras" -, and a missing key is no
    // opinion: then what this browser used last applies.
    const fromURL = params.has('cams') ? params.getAll('cams').filter(Boolean) : null;
    const ids = fromURL ?? savedSelection();
    if (ids) {
      // Only keeps cameras that still exist: a saved selection - or an old link
      // - can cite a camera removed from the registry since then.
      const wanting = new Set(ids);
      selected = new Set(cameras.list.filter((c) => wanting.has(c.id)).map((c) => c.id));
    }
    if (!selected.size && !fromURL) {
      // Two by default: opening nine 1080p streams at once freezes a phone.
      selected = new Set(cameras.list.slice(0, 2).map((c) => c.id));
    }
    mounted = true;
  });

  // The selection this browser used last. Corrupted content counts as none.
  function savedSelection(): string[] | null {
    try {
      const v: unknown = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? 'null');
      return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : null;
    } catch {
      return null;
    }
  }

  // The URL now says what is on screen, and that is what makes pasting the
  // address in another tab land on the same grid. `mounted` holds the write
  // until the selection has been read: without it the first frame would publish
  // an empty `cams=`, erasing from the address bar exactly what was about to be
  // read from it.
  $effect(() => {
    if (!mounted) return;
    write({
      // In registry order, not the Set's insertion order: unchecking and
      // checking again would rewrite the URL with nothing having changed on
      // screen.
      cams: cameras.list.filter((c) => selected.has(c.id)).map((c) => c.id),
      // Always written, even for the default: the default here depends on the
      // screen width, so omitting it would make the same link open differently
      // on a phone.
      view: mode,
    });
  });

  // What the share says in words: the checked cameras, by name up to
  // NAMES_IN_TEXT; above that just the count, because the list would not fit in
  // a subject line.
  $effect(() => {
    if (!mounted) return;
    const names = cameras.list.filter((c) => selected.has(c.id)).map((c) => c.name);
    if (!names.length) scene.texto = '';
    else if (names.length <= NAMES_IN_TEXT) scene.texto = `Ao vivo: ${names.join(', ')}`;
    else scene.texto = `Ao vivo: ${names.length} câmeras`;
  });

  // The go2rtc component creates the <video> with controls=true. In a live grid
  // that shows a progress bar that means nothing - there is no timeline to go
  // through - and it even shows up on some cameras and not others, depending on
  // the negotiated mode.
  function hideNativeControls(node: VideoRTC) {
    const apply = () => {
      if (node.video) {
        node.video.controls = false;
        return true;
      }
      return false;
    };
    // The <video> is born in connectedCallback, which may not have run yet.
    if (!apply()) requestAnimationFrame(apply);
  }

  // --- full screen
  // ---------------------------------------------------------------

  // The whole stage goes full screen, not the tile: it is what stays still
  // while the cameras change. In there the grid becomes a carousel - the
  // current camera fills the screen and the neighbors wait at the sides, ready
  // to come in with a swipe -, and the others leave the scene with `display:
  // none`.
  //
  // They are the same tiles as in the grid, with the same player: entering and
  // leaving reconnects nothing. Whatever leaves the scene falls under the
  // `onlyOnScreen` rule, like a tile scrolled off screen: it stays on for
  // SCROLL_GRACE_MS (or always, with "Pausar fora da tela" off) and is then
  // cut. That is why the switch is immediate right after entering, and waits
  // for a keyframe after that.
  const inFullscreen = $derived(!!stage && fullscreen.el === stage);
  // The camera on screen, by id: removing another one from the selection does
  // not move it.
  let current = $state<string | null>(null);
  const currentIdx = $derived(visible.findIndex((c) => c.id === current));
  // With two cameras the previous and the next are the same one: it waits on
  // the side it will come in from.
  let side = $state<1 | -1>(1);
  // How far the carousel is shifted, in px, and whether the shift animates.
  let drag = $state(0);
  let animating = $state(false);

  // The name and the position stay; arrows, ⋮, PiP and the exit button
  // disappear. They do not disappear with the menu open or with the mouse over
  // one of them.
  let overControls = false;
  const controls = vanishingControls(() => inFullscreen && !menu && !overControls);

  type Role = 'atual' | 'ant' | 'prox' | 'fora';

  function role(i: number): Role {
    const n = visible.length;
    if (i === currentIdx) return 'atual';
    if (n === 2) return side === 1 ? 'prox' : 'ant';
    if (i === (currentIdx + 1) % n) return 'prox';
    if (i === (currentIdx - 1 + n) % n) return 'ant';
    return 'fora';
  }

  function toggleFull(cam: string) {
    if (!stage) return;
    if (inFullscreen) {
      // Two quick swipes in a row also make a double click.
      if (Date.now() - draggedAt > 2 * SWITCH_MS) leaveFullscreen();
      return;
    }
    current = cam;
    menu = null;
    enterFullscreen(stage);
  }

  // The next (1) or the previous (-1), wrapping around: from the last back to
  // the first.
  function ir(d: 1 | -1) {
    const n = visible.length;
    if (animating || n < 2 || !stage) return;
    menu = null;
    side = d;
    animating = true;
    drag = -d * stage.clientWidth;
    setTimeout(() => {
      // All in the same tick: the camera that came in becomes the current one
      // in the same frame in which the carousel goes back to zero, without
      // animating the return.
      animating = false;
      drag = 0;
      current = visible[(currentIdx + d + n) % n]?.id ?? current;
    }, SWITCH_MS);
  }

  // The swipe. The axis is decided in the first AXIS_PX: up or down is not for
  // the screen. Starting on a button or on the menu is not either.
  let gesture: { id: number; x: number; y: number; axis: 'x' | 'y' | null } | null = null;
  let draggedAt = 0;

  function gestureStart(e: PointerEvent) {
    if (!inFullscreen || animating || visible.length < 2) return;
    if (e.target instanceof Element && e.target.closest('button, a, .menu')) return;
    gesture = { id: e.pointerId, x: e.clientX, y: e.clientY, axis: null };
  }

  function gestureMove(e: PointerEvent) {
    if (inFullscreen && e.pointerType === 'mouse') {
      overControls = e.target instanceof Element && !!e.target.closest('button, a, .menu');
      controls.show();
    }
    if (gesture?.id !== e.pointerId || !stage) return;
    const dx = e.clientX - gesture.x;
    const dy = e.clientY - gesture.y;
    if (!gesture.axis && Math.hypot(dx, dy) > AXIS_PX) {
      gesture.axis = Math.abs(dx) > Math.abs(dy) ? 'x' : 'y';
      if (gesture.axis === 'x') stage.setPointerCapture(e.pointerId);
    }
    if (gesture.axis !== 'x') return;
    if (visible.length === 2) side = dx < 0 ? 1 : -1;
    drag = dx;
  }

  function gestureEnd(e: PointerEvent) {
    if (gesture?.id !== e.pointerId || !stage) return;
    const { axis, x } = gesture;
    gesture = null;
    if (axis !== 'x') return;
    draggedAt = Date.now();
    const dx = e.clientX - x;
    if (e.type === 'pointerup' && Math.abs(dx) > swipeLimit(stage.clientWidth)) {
      ir(dx < 0 ? 1 : -1);
      return;
    }
    // Too short: the camera goes back to its place.
    animating = true;
    drag = 0;
    setTimeout(() => (animating = false), SWITCH_MS);
  }

  // A simple tap on the image hides the controls, as in Recordings. Only a tap
  // that did not move: the swipe switches camera. The double tap belongs to the
  // tile's `dblclick`, which Android's Chrome also generates, and the tap that
  // closes the menu only closes the menu.
  let touchAt: { id: number; x: number; y: number; menu: boolean } | null = null;
  const touch = doubleTouch({
    simples: () => {
      if (inFullscreen) controls.singleTouch();
    },
  });

  function touchStart(e: PointerEvent) {
    touchAt =
      e.pointerType === 'touch'
        ? { id: e.pointerId, x: e.clientX, y: e.clientY, menu: !!menu }
        : null;
  }

  function touchEnd(e: PointerEvent) {
    if (touchAt?.id !== e.pointerId) return;
    const { x, y, menu: withMenu } = touchAt;
    touchAt = null;
    if (withMenu || Math.hypot(e.clientX - x, e.clientY - y) > AXIS_PX) return;
    if (e.target instanceof Element && e.target.closest('button, a, .menu')) return;
    touch(e);
  }

  onMount(() => () => {
    controls.stop();
  });

  // Entering and switching camera show the controls and restart the countdown.
  $effect(() => {
    if (!inFullscreen) return;
    // eslint-disable-next-line @typescript-eslint/no-unused-expressions -- read only, so the $effect tracks it
    current;
    untrack(() => {
      controls.show();
    });
  });

  // The screen's camera disappeared from the selection by another path, or
  // access was not confirmed and the grid emptied: the full screen would go
  // black.
  $effect(() => {
    if (inFullscreen && (currentIdx < 0 || !accessConfirmed)) leaveFullscreen();
  });

  // --- each tile's ⋮ menu
  // -----------------------------------------------------

  // One open at a time, by its camera. It lives INSIDE the tile, not outside
  // the grid like the one in Detections: in full screen the browser only shows
  // the element that went full screen, and a menu outside it would vanish
  // there.
  let menu = $state<string | null>(null);
  // Each camera's <video-stream>, so the capture can find its <video>. It does
  // not need to be reactive: it is only read on click.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- only read on click
  const players = new Map<string, VideoRTC>();
  // Capture or PiP failure per camera, shown on the tile itself for a few
  // seconds. Empty means no notice.
  const notices = $state<Record<string, string>>({});
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- the timers, which the screen does not read
  const noticeTimer = new Map<string, number>();

  function notify(cam: string, text: string) {
    notices[cam] = text;
    clearTimeout(noticeTimer.get(cam));
    noticeTimer.set(
      cam,
      setTimeout(() => (notices[cam] = ''), 5000),
    );
  }

  function outsideMenu(e: MouseEvent) {
    if (menu && !(e.target instanceof Element && e.target.closest('.options'))) menu = null;
  }

  function keyHandler(e: KeyboardEvent) {
    if (e.key === 'Escape') menu = null;
    if (inFullscreen && (e.key === 'ArrowRight' || e.key === 'ArrowLeft')) {
      e.preventDefault();
      ir(e.key === 'ArrowRight' ? 1 : -1);
    }
  }

  // Between the ⋮ and the menu, and between the menu and the screen edge.
  const MENU_SLACK = 6;
  const MENU_BORDER = 8;

  // The tile clips whatever goes past its edge, and on a phone, with two
  // columns, it is about 180px tall: the menu became a little box with
  // scrolling. That is why the menu is `position: fixed`, which escapes the
  // clip even inside the tile, placed here next to the ⋮. It opens downward; if
  // it does not fit and there is more room above, upward; and it only scrolls
  // if it fits in neither.
  function positionMenu(el: HTMLElement) {
    const mark = el.parentElement?.querySelector('.mais span');
    if (!mark) return;
    const r = mark.getBoundingClientRect();
    const height = el.offsetHeight;
    const below = window.innerHeight - r.bottom - MENU_SLACK - MENU_BORDER;
    const above = r.top - MENU_SLACK - MENU_BORDER;
    const upward = height > below && above > below;
    const fits = upward ? above : below;
    el.style.right = `${document.documentElement.clientWidth - r.right}px`;
    el.style.top = `${upward ? r.top - MENU_SLACK - Math.min(height, fits) : r.bottom + MENU_SLACK}px`;
    el.style.maxHeight = `${fits}px`;
  }

  // Pinned to the screen and not to the tile, the menu would stay behind when
  // the page scrolls.
  function closeMenu() {
    menu = null;
  }

  // The frame is what is on screen, and the name carries the click time: in
  // live the two differ only by the stream latency. Same convention as
  // Recordings (cam_YYYY-MM-DD_HH-MM-SS), so live and recording images sit side
  // by side in the folder.
  async function capture(cam: string) {
    menu = null;
    const d = new Date();
    const name = `${cam}_${dayKey(d)}_${hhmmss(d.getTime()).replaceAll(':', '-')}.jpg`;
    try {
      await downloadFrame(players.get(cam)?.video, name);
    } catch (e) {
      notify(cam, messageOf(e));
    }
  }

  // The same as unchecking the camera in the list: it saves the selection and
  // the URL follows. In full screen, the next one takes its place; if it was
  // the only one, full screen closes.
  function remove(cam: string) {
    menu = null;
    if (inFullscreen && cam === current) {
      const n = visible.length;
      if (n < 2) leaveFullscreen();
      else current = visible[(currentIdx + 1) % n]?.id ?? null;
    }
    toggle(cam);
  }

  // --- picture-in-picture ------------------------------------------------------

  // Firefox does not have the API (only its own button, drawn over the video):
  // there the button does not even appear.
  const PIP = document.pictureInPictureEnabled;
  // The camera that is in the floating window. The browser allows one at a
  // time.
  let pipCam = $state<string | null>(null);

  async function togglePip(cam: string) {
    const video = players.get(cam)?.video;
    try {
      if (!video) throw new Error('sem vídeo');
      if (document.pictureInPictureElement === video) await document.exitPictureInPicture();
      else await video.requestPictureInPicture();
    } catch {
      // The usual reason is the video not having started yet: without the first
      // image the browser refuses the window.
      notify(cam, 'o vídeo ainda não começou');
    }
  }

  // The go2rtc player drops the connection 5 s after the tab becomes hidden,
  // and a hidden tab is exactly the PiP use case: minimizing the browser would
  // freeze the little window. The cut goes through the instance's
  // `disconnectedCallback`, so that is where the camera in PiP is held -
  // without touching the copied file. When the tile leaves the screen for real
  // (changing route, unchecking the camera), the browser calls the prototype's,
  // and the connection closes as usual.
  //
  // The other cameras keep being cut: with the tab hidden, only the PiP one
  // keeps pulling video.
  function keepInPip(node: VideoRTC, cam: string) {
    const turnOff = node.disconnectedCallback.bind(node);
    node.disconnectedCallback = () => {
      if (node.isConnected && document.pictureInPictureElement === node.video) return;
      turnOff();
    };
    // The events come out of the <video>, which is born later and does not
    // propagate them upward: listening in the capture phase of the
    // <video-stream> catches them anyway.
    node.addEventListener('enterpictureinpicture', () => (pipCam = cam), true);
    node.addEventListener(
      'leavepictureinpicture',
      () => {
        if (pipCam === cam) pipCam = null;
        // Closing PiP with the tab still hidden, or with the tile scrolled off
        // screen, applies the cut that was held. The return comes from the
        // player itself (tab) or from `onlyOnScreen` (scroll).
        if (document.hidden || (!alwaysOn && offScreen.has(node))) turnOff();
      },
      true,
    );
  }

  // --- reconnect fast
  // -----------------------------------------------------------

  // Measured on the phone (Android, Chrome, chrome://net-export capture): with
  // the tab hidden Android cuts Chrome's network, and a connection opened in
  // that meantime loses its SYN. TCP only resends it at 1, 3, 7, 15, 31 and 63
  // s, and the captured one opened at 64 s. Worse: Chrome only lets one
  // WebSocket at a time be "connecting" to the same server, and the 9 cameras
  // that tried on return queued behind it, without even starting their own TCP.
  // The go2rtc player reconnects by itself with the tab hidden (WebRTC that
  // drops, the `onclose`), and that is where the ~17 s of black screen came
  // from.
  //
  // Hence the three pieces:
  // - with the tab hidden, no camera opens a connection - only the PiP one,
  //   which is being watched;
  // - on return, none tries before the server answers a `fetch`
  //   (`waitNetwork`): right on return the network is still suspended, and the
  //   9 attempts failed in under 50 ms, falling into the player's 15 s;
  // - when a connection really drops, the next attempt goes out right away,
  //   instead of those fixed 15 s.

  // The wait in progress on return from a hidden tab, or null when there is
  // none.
  let network: Promise<void> | null = null;

  function onVisibilityChange() {
    isVisible = !document.hidden;
    if (document.hidden || network) return;
    network = waitNetwork().finally(() => (network = null));
  }

  // In the capture phase of `window`, to take effect before the listener each
  // player puts on `document`, which is what reconnects the cameras.
  onMount(() => {
    window.addEventListener('visibilitychange', onVisibilityChange, true);
    return () => {
      window.removeEventListener('visibilitychange', onVisibilityChange, true);
    };
  });

  // Any response will do, even an HTTP error: what matters is that the server
  // was reached. It gives up if the tab becomes hidden again.
  async function waitNetwork() {
    for (let i = 0; !document.hidden; i++) {
      try {
        await api.ping(PROBE_MS);
        return;
      } catch {
        await new Promise((r) => setTimeout(r, reconnectAt(i)));
      }
    }
  }

  // `onconnect` is how the player opens every connection. With the tab hidden
  // it does nothing: on return, the player itself calls `connectedCallback`,
  // which goes through here again. While the wait on return is running, it only
  // schedules: when it ends, the camera connects if it still should - tab
  // visible and tile on screen, the same rule as `onlyOnScreen`.
  //
  // The player only reads `RECONNECT_TIMEOUT` in `onclose`, to schedule the
  // next attempt: changing the value just before, on the instance, is enough -
  // without touching the copied file. The computation of how long to wait
  // remains the player's own, from the start of the attempt that failed. The
  // sequence only goes back to the beginning when the video plays, and not when
  // the connection opens: a server that accepts and drops right after would end
  // up being tried every half second.
  function reconnectFast(node: VideoRTC) {
    const connect = node.onconnect.bind(node);
    node.onconnect = () => {
      if (document.hidden && document.pictureInPictureElement !== node.video) return false;
      if (!network) return connect();
      void network.then(() => {
        if (!document.hidden && node.isConnected && (alwaysOn || !offScreen.has(node))) connect();
      });
      return false;
    };
    let failures = 0;
    const closed = node.onclose.bind(node);
    node.onclose = () => {
      node.RECONNECT_TIMEOUT = reconnectAt(failures);
      const reconnecting = closed();
      if (reconnecting) failures++;
      // Dropped and did not come back: it may be the session. A refused
      // websocket does not tell the browser the reason, so the one that answers
      // is /api/session.
      if (failures >= 2) checkSession();
      return reconnecting;
    };
    // `playing` comes out of the <video>, which does not propagate it: only the
    // capture phase catches it here, as in `keepInPip`.
    node.addEventListener('playing', () => (failures = 0), true);
  }

  // --- the session
  // -------------------------------------------------------------

  // Whoever is removed, or gets a new link, loses the session right away: the
  // server refuses every request of it and closes the live websockets. That is
  // enough for MSE, whose video comes inside the websocket. Not for WebRTC: the
  // player closes the websocket as soon as the video plays, and the media goes
  // straight from go2rtc over UDP, where dwnvr cannot reach. What closes WebRTC
  // is this screen, by three paths:
  // - the session notice, a mute websocket that the server closes when it kicks
  //   the person out: on seeing it close, the screen checks the session right
  //   away;
  // - the check every CHECK_SESSION_MS with the video in view, which is the
  //   safety net if the notice gets lost;
  // - the camera that dropped and did not come back, in `reconnectFast`.
  // A session that is not valid leads to the login. A session that cannot be
  // confirmed - dwnvr did not answer - stops the video until it is confirmed:
  // when in doubt, close. While unconfirmed, the notice counts the seconds to
  // the next attempt and lets you try right away.

  // With the video in view: tab visible, or a camera in PiP.
  let isVisible = $state(!document.hidden);
  const isActive = $derived(isVisible || pipCam !== null);

  let accessConfirmed = $state(true);
  let lastCheck = Date.now();
  let checking: Promise<void> | null = null;
  // The same `checking`, visible to the notice: the promise is not reactive.
  let checkingNow = $state(false);
  // An attempt waiting for the `network` before checking. It is kept separate:
  // a check from another source can finish before the wait ends.
  let waitingNetwork = $state(false);
  // The notice says "Conferindo…" and the countdown stops while any one is
  // running.
  const busy = $derived(checkingNow || waitingNetwork);
  // Seconds to the next attempt, while access is not confirmed.
  let lacking = $state(RETRY_WAIT_S);
  // Access came back after a failure: only the screen reader needs this, the
  // notice that disappears already says the same to anyone who sees it.
  let accessRestored = $state(false);

  // closeAll cuts the video of all cameras right away. Unmounting the tile also
  // cuts, but only after the player's 5 s grace, and WebRTC would keep
  // receiving video in the meantime.
  function closeAll() {
    for (const node of players.values()) node.ondisconnect();
  }

  function checkSession() {
    if (!session.authRequired) return;
    checking ??= (async () => {
      checkingNow = true;
      try {
        const r = await api.checkSession(CHECK_TIMEOUT_MS);
        const s = (await r.json()) as Session;
        if (!s.authenticated) {
          closeAll();
          sessionDropped();
        } else {
          if (!accessConfirmed) accessRestored = true;
          accessConfirmed = true;
        }
      } catch {
        closeAll();
        accessConfirmed = false;
        accessRestored = false;
      } finally {
        lastCheck = Date.now();
        checking = null;
        checkingNow = false;
        // The countdown restarts from the end of the attempt, wherever it comes
        // from: the countdown, the button, the session notice or a camera.
        lacking = RETRY_WAIT_S;
      }
    })();
  }

  // While `network` waits for the server to answer on return from a hidden tab,
  // it does not check: the still-suspended network would give a false "not
  // confirmed", and the video would stop for nothing. The attempt waits for it,
  // and the notice already says "Conferindo…". The wait also ends if the tab
  // becomes hidden again: then it does not check, and the countdown continues
  // on the next return. One wait at a time. It also does not check if the
  // screen has already left, as on logout: the response of an orphan check,
  // arriving after a new login, would bring down the new session.
  let liveScreen = true;
  onMount(() => () => {
    liveScreen = false;
  });
  function retry() {
    if (!network) return checkSession();
    if (waitingNetwork) return;
    waitingNetwork = true;
    void network.then(() => {
      waitingNetwork = false;
      if (liveScreen && isActive) checkSession();
    });
  }

  // The routine check, with access confirmed.
  $effect(() => {
    if (!session.authRequired || !isActive || !accessConfirmed) return;
    const id = setInterval(() => {
      if (network) return;
      if (Date.now() - lastCheck >= CHECK_SESSION_MS) checkSession();
    }, CHECK_TICK_MS);
    return () => {
      clearInterval(id);
    };
  });

  // The countdown to the next attempt, with access not confirmed. One step per
  // second: each change of `lacking` reruns the effect, which schedules the
  // next one. It stops during an attempt and with the video out of view.
  $effect(() => {
    if (!session.authRequired || !isActive || accessConfirmed || busy) return;
    const n = lacking;
    const id = setTimeout(() => {
      if (n > 1) lacking = n - 1;
      else retry();
    }, 1000);
    return () => {
      clearTimeout(id);
    };
  });

  // The session notice stays open only with the video in view. Closed without
  // the screen asking, it checks the session and reopens, with the same growing
  // wait as the cameras.
  $effect(() => {
    if (!session.authRequired || !isActive) return;
    let ws: WebSocket | undefined;
    let timer: number | undefined;
    let failures = 0;
    let isLive = true;
    const openNow = () => {
      ws = new WebSocket(mediaURL.wsSessionNotice());
      ws.onopen = () => (failures = 0);
      ws.onclose = () => {
        if (!isLive) return;
        checkSession();
        timer = setTimeout(openNow, reconnectAt(failures++));
      };
    };
    openNow();
    return () => {
      isLive = false;
      clearTimeout(timer);
      ws?.close();
    };
  });

  // --- only decode what is on screen
  // -------------------------------------------

  // With the grid taller than the window (1× on a phone), a tile scrolled off
  // screen would keep pulling and decoding video nobody sees. The go2rtc player
  // has this built in (`visibilityThreshold`), but its observer is created in
  // the first `connectedCallback`, which has already run by the time
  // `{@attach}` arrives - the option passed there would have no effect. That
  // is why the observer is here.
  //
  // The cut waits for `SCROLL_GRACE_MS` and goes through the instance's
  // `disconnectedCallback`, which brings the PiP guard plus the player's own
  // grace (`DISCONNECT_TIMEOUT`) - discounted here, so the total is the
  // parameter. A hidden tab does not go through here and keeps the player's
  // 5 s. Only a tile that disappears entirely is cut: a half-cut tile stays on.
  //
  // A plain Set, not a WeakSet, because the size matters: it decides whether
  // the "Pausar fora da tela" switch appears.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- what the screen reads is `quantasFora`
  const offScreen = new Set<VideoRTC>();
  const cut = new WeakMap<VideoRTC, number>();
  let quantasFora = $state(0);

  // Opt-in for whoever prefers not to wait for the keyframe when scrolling, and
  // accepts paying for it in battery. Per device, like the layout: it goes in
  // localStorage, never in the URL.
  let alwaysOn = $state(localStorage.getItem(ALWAYS_KEY) === '1');

  function scheduleCut(node: VideoRTC) {
    clearTimeout(cut.get(node));
    const wait = Math.max(0, SCROLL_GRACE_MS - node.DISCONNECT_TIMEOUT);
    cut.set(
      node,
      setTimeout(() => {
        node.disconnectedCallback();
      }, wait),
    );
  }

  const observer = new IntersectionObserver((entries) => {
    for (const { target: node, isIntersecting } of entries) {
      if (!(node instanceof VideoRTC)) continue; // only the players are observed
      clearTimeout(cut.get(node));
      if (isIntersecting) {
        offScreen.delete(node);
        node.connectedCallback();
      } else {
        offScreen.add(node);
        if (!alwaysOn) scheduleCut(node);
      }
    }
    quantasFora = offScreen.size;
  });

  // On return from a hidden tab, the player reconnects by calling the
  // instance's `connectedCallback` - of all tiles, including those off screen,
  // which the observer does not revisit because nothing changed for it. The
  // guard is here.
  function onlyOnScreen(node: VideoRTC) {
    const turnOn = node.connectedCallback.bind(node);
    node.connectedCallback = () => {
      if (alwaysOn || !offScreen.has(node)) turnOn();
    };
    observer.observe(node);
    return () => {
      clearTimeout(cut.get(node));
      observer.unobserve(node);
      offScreen.delete(node);
      quantasFora = offScreen.size;
    };
  }

  // Turning it on reconnects right away whoever was already cut; turning it off
  // gives those off screen the same grace as whoever just left the screen.
  function toggleAlways() {
    alwaysOn = !alwaysOn;
    localStorage.setItem(ALWAYS_KEY, alwaysOn ? '1' : '0');
    for (const node of offScreen) {
      if (alwaysOn) {
        clearTimeout(cut.get(node));
        node.connectedCallback();
      } else {
        scheduleCut(node);
      }
    }
  }

  // These two are the ONLY ones that write to localStorage, and that is on
  // purpose: what goes there is what the user chose by clicking, never what
  // came from a link. That is what keeps someone's `#live?view=3` from becoming
  // your default.
  //
  // Reassigns instead of mutating: runes do not observe changes inside a Set.
  function setSelection(next: Set<string>) {
    selected = next;
    localStorage.setItem(STORAGE_KEY, JSON.stringify([...next]));
  }

  function toggle(id: string) {
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- becomes the whole `selected`, without mutating the old one
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    setSelection(next);
  }

  function all() {
    setSelection(allChecked ? new Set<string>() : new Set(cameras.list.map((c) => c.id)));
  }

  function setMode(m: Mode) {
    mode = m;
    localStorage.setItem(LAYOUT_KEY, String(m));
  }

  // Largest 16:9 tile that fits: for each number of columns, the tile width is
  // limited either by the available width or by the height of the remaining
  // rows. Since the height is part of the computation, the chosen grid never
  // overflows.
  function bestFit(n: number, W: number, H: number) {
    let best = { cols: 1, w: 0 };
    for (let cols = 1; cols <= n; cols++) {
      const rows = Math.ceil(n / cols);
      const w = Math.min((W - GAP * (cols - 1)) / cols, ((H - GAP * (rows - 1)) / rows) * RATIO);
      if (w > best.w) best = { cols, w };
    }
    return best;
  }

  function gauge() {
    // In full screen it is the full screen that gives the stage's measure; the
    // grid is measured again on return.
    if (!stage || inFullscreen) return;
    if (mode !== 'fit' || !visible.length) {
      stage.style.height = '';
      // Measured from just below the bar, not from the stage: the camera list
      // and the access notice push the stage down, and opening them must not
      // shrink the cameras. The scroll is added back because the measure is
      // from the top of the page, not from where the user is. The floor avoids
      // a tiny tile in a very short window, like a phone on its side.
      if (bar) {
        const free = freeHeight(bar, 0) - window.scrollY - bar.offsetHeight - PAGE_GAP;
        columnsHeight = Math.max(MIN_COLUMNS_HEIGHT, free);
      }
      return;
    }

    // The stage goes from its top to the end of the window.
    const H = freeHeight(stage);

    stage.style.height = `${H}px`;
    fitting = bestFit(visible.length, stage.clientWidth, H);
  }

  // In columns the width goes as far as the tile, at 16:9, still fits the
  // window height: on a wide screen, like an ultrawide, the grid uses the full
  // width, and at 1× the tile is not taller than the screen. In full screen it
  // is the full screen that rules.
  const columnsWidth = $derived(
    mode === 'fit' || inFullscreen || columnsHeight <= 0
      ? null
      : `${Math.floor(mode * columnsHeight * RATIO + GAP * (mode - 1))}px`,
  );

  $effect(() => {
    // Explicit reads because `gauge` returns early before touching them: they
    // are what changes the available height or the number of tiles.
    /* eslint-disable @typescript-eslint/no-unused-expressions -- read only, so the $effect tracks it */
    mode;
    visible.length;
    showPicker;
    inFullscreen;
    // The switch goes into the bar and can wrap onto a new row.
    quantasFora;
    // The new-version strip pushes the stage down, and no resize announces it.
    newScreen.available;
    /* eslint-enable @typescript-eslint/no-unused-expressions */
    gauge();
  });
</script>

<svelte:window
  onresize={gauge}
  onorientationchange={gauge}
  onscroll={closeMenu}
  onpointerdown={outsideMenu}
  onkeydown={keyHandler}
/>

<div class="page">
  <div class="bar" bind:this={bar}>
    <CamerasButton
      checkedList={selected.size}
      total={cameras.list.length}
      all={allChecked}
      isOpen={showPicker}
      onclick={() => (showPicker = !showPicker)}
      --pad-x="10px"
    />

    <div class="controls">
      <div class="row modes" role="group" aria-label="layout">
        {#each MODES as m (m)}
          <button
            class="ghost"
            class:on={mode === m}
            aria-pressed={mode === m}
            title={m === 'fit' ? 'encaixar todas na tela' : `${m} coluna${m > 1 ? 's' : ''}`}
            aria-label={m === 'fit' ? 'encaixar todas na tela' : undefined}
            onclick={() => setMode(m)}
          >
            {#if m === 'fit'}
              <Icon name="fitToScreen" size={17} />
              <span class="button-text">Encaixar</span>
            {:else}
              {m}×
            {/if}
          </button>
        {/each}
      </div>

      <!-- Only appears when there is something to pause: with everything in
           view - fit, or few cameras - there is no off-screen tile to cut. -->
      {#if quantasFora > 0}
        <span class="sep"></span>
        <button
          class="ghost pause"
          class:on={!alwaysOn}
          aria-pressed={!alwaysOn}
          aria-label="pausar câmeras fora da tela"
          title={alwaysOn
            ? 'câmeras fora da tela seguem ligadas (gasta mais bateria)'
            : `câmeras fora da tela pausam após ${SCROLL_GRACE_MS / 1000} s`}
          onclick={toggleAlways}
        >
          <Icon name={alwaysOn ? 'offScreenPlay' : 'offScreenPause'} size={20} />
          <span class="track"></span>
          <span class="button-text">Pausar fora da tela</span>
        </button>
      {/if}
    </div>
  </div>

  {#if showPicker}
    <div class="card">
      <CameraPicker
        cameras={cameras.list}
        checked={(id: string) => selected.has(id)}
        all={allChecked}
        ontoggle={toggle}
        onall={all}
        error={cameras.error}
      />
    </div>
  {/if}

  <!-- For the screen reader, only the state changes: the countdown number stays
       out, otherwise it would be announced every second. Always mounted and
       empty until the first failure: the reader announces a change inside a
       region that already exists, not the text of one that was just born. -->
  <span class="read-only" role="status">
    {accessConfirmed
      ? accessRestored
        ? 'Acesso ao dwnvr confirmado.'
        : ''
      : busy
        ? 'Conferindo o acesso ao dwnvr.'
        : `Não deu para confirmar o acesso ao dwnvr. Nova tentativa em ${RETRY_WAIT_S} segundos.`}
  </span>

  {#if !accessConfirmed}
    <div class="card small no-access">
      <p>Não deu para confirmar o seu acesso com o dwnvr, e o vídeo fica parado até confirmar.</p>
      <div class="attempt">
        <span>{busy ? 'Conferindo…' : `Nova tentativa em ${lacking} s`}</span>
        <button type="button" disabled={busy} onclick={retry}> Tentar agora </button>
      </div>
    </div>
  {/if}

  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="stage"
    class:fit={mode === 'fit'}
    class:fs={inFullscreen}
    class:escondidos={inFullscreen && !controls.visibleOnes}
    style:--troca="{SWITCH_MS}ms"
    bind:this={stage}
    {@attach (node: HTMLElement) => () => {
      // A de verdade o navegador desfaz sozinho quando o elemento sai da
      // página; a simulada ficaria cobrindo a tela seguinte, como ao abrir as
      // gravações pelo ⋮.
      if (fullscreen.el === node) leaveFullscreen();
    }}
    onpointerdowncapture={(e) => {
      if (!inFullscreen) return;
      touch.cancel();
      touchStart(e);
      controls.touched();
    }}
    onpointerdown={gestureStart}
    onpointermove={gestureMove}
    onpointerup={(e) => {
      touchEnd(e);
      gestureEnd(e);
    }}
    onpointercancel={gestureEnd}
  >
    <div
      class="grid"
      class:fit={mode === 'fit'}
      class:animating
      style:--cols={mode === 'fit' ? fitting.cols : mode}
      style:--tile-w="{Math.floor(fitting.w)}px"
      style:max-width={columnsWidth}
      style:transform={inFullscreen && drag ? `translateX(${drag}px)` : null}
    >
      {#each accessConfirmed ? visible : [] as c, i (c.id)}
        {@const p = inFullscreen ? role(i) : null}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="tile"
          class:ant={p === 'ant'}
          class:next={p === 'prox'}
          class:fora={p === 'fora'}
          ondblclick={() => toggleFull(c.id)}
          title={inFullscreen ? undefined : 'duplo clique: tela cheia'}
        >
          <!-- The go2rtc component negotiates WebRTC/MSE by itself. The media
               goes straight from the browser to go2rtc; dwnvr only proxies the
               signaling, so the credential does not reach the browser. -->
          <video-stream
            {@attach (node: VideoRTC) => {
              node.mode = 'webrtc,mse';
              // Antes do `src`, que já abre a primeira conexão.
              reconnectFast(node);
              node.src = mediaURL.liveWS(c.id);
              hideNativeControls(node);
              if (PIP) keepInPip(node, c.id);
              const letGo = onlyOnScreen(node);
              players.set(c.id, node);
              // Ao desmontar, o custom element fecha a conexão sozinho no
              // disconnectedCallback - nada a limpar aqui além da referência
              // e do observer. Quando a sessão cai, quem fecha na hora é o
              // `fecharTudo`.
              return () => {
                letGo();
                players.delete(c.id);
              };
            }}
          ></video-stream>
          <!-- The tile's double click is full screen: two quick taps on the PiP
               button must not become that. -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="label" ondblclick={(e) => e.stopPropagation()}>
            <span class="name"
              >{c.name}{#if inFullscreen}<span class="pos">{i + 1}/{visible.length}</span
                >{/if}</span
            >
            {#if PIP}
              <button
                class="pip"
                class:on={pipCam === c.id}
                onclick={() => togglePip(c.id)}
                aria-label="{pipCam === c.id ? 'fechar' : 'abrir'} {c.name} em janela flutuante"
                aria-pressed={pipCam === c.id}
                title={pipCam === c.id ? 'fechar a janela flutuante' : 'abrir em janela flutuante'}
              >
                <Icon name="pip" size={16} />
              </button>
            {/if}
          </div>

          {#if notices[c.id]}<span class="badge">{notices[c.id]}</span>{/if}

          <!-- In full screen this corner belongs to the exit button, which
               belongs to the stage. Two quick clicks on it must not reach the
               tile's double click: they would be three switches instead of two. -->
          {#if !inFullscreen}
            <button
              class="full"
              onclick={() => {
                toggleFull(c.id);
              }}
              ondblclick={(e) => {
                e.stopPropagation();
              }}
              aria-label="{c.name} em tela cheia"
              title="tela cheia (ou duplo clique no vídeo)"
            >
              <Icon name="fullscreen" size={16} />
            </button>
          {/if}

          <!-- The tile's double click is full screen: two quick taps on the ⋮
               or on the menu must not become that. -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="options" ondblclick={(e) => e.stopPropagation()}>
            <button
              class="mais"
              onclick={() => (menu = menu === c.id ? null : c.id)}
              aria-label="opções da câmera {c.name}"
              aria-haspopup="menu"
              aria-expanded={menu === c.id}
              ><span aria-hidden="true"><Icon name="menu" size="100%" /></span></button
            >

            {#if menu === c.id}
              <!-- Real links, not buttons that change the hash: that way the
                   middle click and "open in new tab" also work. -->
              <div class="menu" role="menu" {@attach positionMenu}>
                <a role="menuitem" href="#rec?cam={encodeURIComponent(c.id)}">Ver gravações</a>
                {#if cameras.detector}
                  <a role="menuitem" href="#detection?cams={encodeURIComponent(c.id)}">
                    Ver detecções
                  </a>
                {/if}
                <button class="ghost" role="menuitem" onclick={() => capture(c.id)}>
                  Baixar imagem agora
                </button>
                <button class="ghost" role="menuitem" onclick={() => remove(c.id)}>
                  Remover da visualização
                </button>
              </div>
            {/if}
          </div>
        </div>
      {/each}
    </div>

    <!-- Outside the carousel: they stay still while the cameras slide. The
         name, the PiP and the ⋮ are the tile's own, and go along with the
         image. -->
    {#if inFullscreen}
      {#if visible.length > 1}
        <button
          class="ghost arrow esq"
          onclick={() => {
            ir(-1);
          }}
          aria-label="câmera anterior"
          title="câmera anterior (←)"
        >
          <Icon name="chevronEsq" size={22} />
        </button>
        <button
          class="ghost arrow dir"
          onclick={() => {
            ir(1);
          }}
          aria-label="próxima câmera"
          title="próxima câmera (→)"
        >
          <Icon name="chevronDir" size={22} />
        </button>
      {/if}
      <span class="leave"><FullscreenButton active overVideo onclick={leaveFullscreen} /></span>
    {/if}
  </div>

  {#if !visible.length && !showPicker}
    {#if cameras.list.length}
      <p class="empty">Selecione as câmeras para visualizar.</p>
    {:else if cameras.error}
      <!-- An empty list because the load failed is not "no cameras". -->
      <p class="empty">Não deu para carregar as câmeras: {cameras.error}</p>
    {:else if !cameras.loading}
      <!-- Without the `loading` guard, "nenhuma câmera" flashed on every
           opening of the screen, in the interval while the list was still being
           fetched. -->
      <NoCameras />
    {/if}
  {/if}
</div>

<style>
  .no-access {
    color: var(--warn);
    display: grid;
    gap: 10px;
  }
  .no-access p {
    margin: 0;
  }
  .attempt {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }
  /* The number changes every second: with a fixed width, the text does not jitter. */
  .attempt span {
    font-variant-numeric: tabular-nums;
  }
  /* Off screen, but read by the screen reader. */
  .read-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }

  .page {
    display: grid;
    gap: 10px;
    padding: 10px;
  }

  /* No width cap on the page: with it only in fit mode, the bar jumped around
     when switching modes in a wide window. What limits the tile is the window
     height - in fit by the computation of `bestFit`, in columns by the grid's
     `max-width`. */

  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    gap: 10px;
  }
  .controls {
    display: contents;
  }

  /* The segmented control: fit and the columns glued into a single block. */
  .modes {
    gap: 0;
  }
  .modes button {
    padding: 8px 12px;
    min-width: 44px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    border-radius: 0;
    margin-left: -1px;
    color: var(--dim);
  }
  .modes button:first-child {
    margin-left: 0;
    border-radius: 8px 0 0 8px;
  }
  .modes button:last-child {
    border-radius: 0 8px 8px 0;
  }
  .modes button.on {
    color: var(--fg);
    border-color: var(--accent);
    background: var(--ativo-fundo);
    position: relative;
    z-index: 1;
  }

  .sep {
    display: none;
    width: 1px;
    align-self: stretch;
    background: var(--line);
    margin: 4px 2px;
  }

  /* The switch: on is "pause out there", the default. */
  .pause {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
  }
  .pause.on {
    border-color: var(--accent);
  }
  .track {
    width: 30px;
    height: 17px;
    border-radius: 9px;
    background: var(--line);
    position: relative;
    flex: none;
  }
  .track::after {
    content: '';
    position: absolute;
    top: 2px;
    left: 2px;
    width: 13px;
    height: 13px;
    border-radius: 50%;
    background: var(--fg);
    transition: left 0.12s;
  }
  .pause.on .track {
    background: var(--accent);
  }
  .pause.on .track::after {
    left: 15px;
  }

  /* The button texts only show where they fit. */
  .button-text {
    display: none;
  }

  @media (min-width: 720px) {
    .controls {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 10px;
    }
    .sep {
      display: block;
    }
    .button-text {
      display: inline;
    }
  }

  /* Clips the grid in fit mode: the height in pixels comes from `gauge`, and
     without this a round-up would bring back the scroll that the mode avoids. */
  .stage.fit {
    overflow: hidden;
  }

  .grid {
    display: grid;
    gap: 8px;
    grid-template-columns: repeat(var(--cols), minmax(0, 1fr));
    width: 100%;
    margin: 0 auto;
  }

  /* In fit the tile width is computed, not distributed among the columns: it is
     what keeps the grid from exceeding the window height. */
  .grid.fit {
    grid-template-columns: repeat(var(--cols), var(--tile-w));
    justify-content: center;
    align-content: center;
    height: 100%;
  }

  .tile {
    position: relative;
    background: #000;
    border-radius: var(--radius);
    overflow: hidden;
    aspect-ratio: 16 / 9;
  }

  .tile :global(video-stream),
  .tile :global(video) {
    width: 100%;
    height: 100%;
    display: block;
    object-fit: contain;
  }

  /* Name and PiP button together in the bottom left corner. */
  .label {
    position: absolute;
    left: 8px;
    bottom: 8px;
    display: flex;
    align-items: center;
    gap: 4px;
    /* Up to the full screen button, on the right. */
    max-width: calc(100% - 50px);
  }

  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    background: rgba(0, 0, 0, 0.6);
    border-radius: 6px;
    padding: 2px 8px;
    font-size: 12px;
    pointer-events: none;
  }

  /* Same height and same background as the ⋮, so the tile's controls are of the
     same family. */
  .pip,
  .full {
    display: flex;
    align-items: center;
    justify-content: center;
    flex: none;
    min-height: 0;
    width: 26px;
    height: 20px;
    padding: 0;
    border: 0;
    border-radius: 6px;
    background: rgba(13, 17, 23, 0.8);
    color: var(--fg);
  }

  .pip:focus-visible,
  .full:focus-visible {
    outline: 2px solid var(--accent);
  }
  .pip.on {
    color: var(--accent);
    outline: 2px solid var(--accent);
  }

  /* Like the ⋮: with a mouse they show only over the tile, and PiP stays while
     the camera is in it; on touch they are always in view. */
  @media (hover: hover) {
    .pip,
    .full {
      opacity: 0;
    }
    .tile:hover .pip,
    .tile:hover .full,
    .pip:focus-visible,
    .full:focus-visible,
    .pip.on {
      opacity: 1;
    }
    .tile:hover .pip,
    .tile:hover .full {
      background: rgba(13, 17, 23, 0.95);
    }
  }

  /* Bottom right, on the row of the name and PiP. */
  .full {
    position: absolute;
    right: 8px;
    bottom: 8px;
  }

  /* The go2rtc player writes the mode (loading, RTC, MSE) in the top right
     corner, which is the ⋮'s place: the text moves to the left of it, without
     touching the copied file. */
  .tile :global(video-stream .info) {
    padding-right: 44px;
  }

  /* Bottom right, above the full screen button: top left is where the go2rtc
     player writes its errors, and bottom left is the name. */
  .badge {
    position: absolute;
    right: 8px;
    bottom: 32px;
    max-width: calc(100% - 120px);
    background: rgba(0, 0, 0, 0.7);
    border: 1px solid #5c2b2b;
    border-radius: 6px;
    padding: 3px 8px;
    font-size: 12px;
    color: var(--bad);
    pointer-events: none;
  }

  /* --- the ⋮: same size and same design as the Detections thumbnails' --- */

  /* No box of its own: the ⋮ is positioned against the tile. The div exists
     only to hold the double click. */
  .options {
    display: contents;
  }

  .mais {
    position: absolute;
    top: 0;
    right: 0;
    display: flex;
    justify-content: flex-end;
    align-items: flex-start;
    min-height: 0;
    width: 40px;
    height: 36px;
    padding: 4px 4px 0 0;
    border: 0;
    background: none;
  }

  .mais span {
    display: block;
    width: 22px;
    height: 20px;
    padding: 2px;
    border-radius: 6px;
    background: rgba(13, 17, 23, 0.8);
    color: var(--fg);
  }

  .mais:focus-visible {
    outline: none;
  }
  .mais:focus-visible span,
  .mais[aria-expanded='true'] span {
    outline: 2px solid var(--accent);
  }

  /* With a mouse the ⋮ shows only over the tile, and stays while its menu is
     open. On touch there is no hover: it is always in view. */
  @media (hover: hover) {
    .mais {
      opacity: 0;
    }
    .tile:hover .mais,
    .mais:focus-visible,
    .mais[aria-expanded='true'] {
      opacity: 1;
    }
    .tile:hover .mais span {
      background: rgba(13, 17, 23, 0.95);
    }
  }

  /* `top`, `right` and `max-height` come from `positionMenu`. Above the header
     and the bottom navigation, which are 20. */
  .menu {
    position: fixed;
    z-index: 30;
    overflow-y: auto;
    padding: 4px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  }

  .menu a,
  .menu button {
    display: flex;
    align-items: center;
    width: 100%;
    min-height: 40px;
    padding: 0 12px;
    border: 0;
    border-radius: 6px;
    color: var(--fg);
    font-size: 14px;
    text-align: left;
    text-decoration: none;
    white-space: nowrap;
  }

  @media (hover: hover) {
    .menu a:hover,
    .menu button:hover {
      background: var(--panel-2);
    }
  }

  /* --- full screen ---------------------------------------------------------- */

  /* No `position`: the browser sets it (`:fullscreen`) or the simulated one's
     rule in app.css, and it is the support for the arrows and the exit button.
     The swipe belongs entirely to the screen: without `touch-action` the
     browser would take it. */
  .stage.fs {
    overflow: hidden;
    background: #000;
    touch-action: none;
    user-select: none;
    -webkit-user-select: none;
  }

  .stage.fs .grid {
    display: block;
    position: absolute;
    inset: 0;
    height: auto;
  }
  .grid.animating {
    transition: transform var(--troca) ease-out;
  }

  /* `left`, not `transform`, to place the neighbors at the sides: a tile with a
     transform becomes the reference of `position: fixed`, and the ⋮ menu would
     open out of place. */
  .stage.fs .tile {
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    aspect-ratio: auto;
    border-radius: 0;
  }
  .stage.fs .tile.ant {
    left: -100%;
  }
  .stage.fs .tile.next {
    left: 100%;
  }
  .stage.fs .tile.fora {
    display: none;
  }

  /* The tile's controls, bigger: here it is the whole screen, and on a phone, the finger. */
  .stage.fs .label {
    left: max(10px, env(safe-area-inset-left));
    bottom: max(10px, env(safe-area-inset-bottom));
  }
  .stage.fs .name {
    padding: 4px 10px;
    font-size: 14px;
  }
  .pos {
    margin-left: 6px;
    color: var(--dim);
    font-variant-numeric: tabular-nums;
  }
  .stage.fs .pip {
    width: 34px;
    height: 28px;
  }
  .stage.fs .pip :global(svg) {
    width: 18px;
    height: 18px;
  }
  .stage.fs .mais {
    width: 52px;
    height: 48px;
    padding: max(8px, env(safe-area-inset-top)) max(8px, env(safe-area-inset-right)) 0 0;
  }
  .stage.fs .mais span {
    width: 36px;
    height: 32px;
    padding: 6px;
  }
  /* Bottom right is the exit button. */
  .stage.fs .badge {
    right: max(10px, env(safe-area-inset-right));
    bottom: calc(max(10px, env(safe-area-inset-bottom)) + 54px);
  }

  /* Same background as the exit button (`overVideo`): the image can be bright. */
  .arrow {
    position: absolute;
    top: 50%;
    z-index: 1;
    display: grid;
    place-items: center;
    width: 44px;
    height: 56px;
    padding: 0;
    transform: translateY(-50%);
    background: rgba(0, 0, 0, 0.55);
    border-color: rgba(255, 255, 255, 0.18);
  }
  .arrow.esq {
    left: max(8px, env(safe-area-inset-left));
  }
  .arrow.dir {
    right: max(8px, env(safe-area-inset-right));
  }

  .leave {
    position: absolute;
    z-index: 1;
    right: max(10px, env(safe-area-inset-right));
    bottom: max(10px, env(safe-area-inset-bottom));
  }

  /* The name and the position stay; the rest disappears. At the end of the
     block to win over `.tile:hover`, which has the same weight. */
  .stage.fs .arrow,
  .stage.fs .leave,
  .stage.fs .mais,
  .stage.fs .pip {
    transition: opacity 0.25s;
  }
  .stage.escondidos .arrow,
  .stage.escondidos .leave,
  .stage.escondidos .mais,
  .stage.escondidos .pip {
    opacity: 0;
    pointer-events: none;
  }
  .stage.escondidos {
    cursor: none;
  }
</style>
