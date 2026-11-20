// The long-press hint, like in Android apps: holding a finger on a button shows
// what it does, and releasing afterward doesn't trigger it. On desktop the
// `title` plays that role, and the phone's browser doesn't show it.
//
// The text is the button's `aria-label`, or the `title`: what the screen reader
// already reads. A button with neither has no hint. It applies to the whole
// app, with a single listener on the document, installed by App.
//
// The same long press applies to whoever asks for it with `data-toque-longo`,
// with a hint or without: the element receives the `toquelongo` event at the
// very moment the hint would open. It is the small area's thumbnail, which
// zooms again (the AreaThumbnail). A single gesture, with the same timing and
// the same tolerance as the hint, instead of two clocks fighting over the
// finger.

// How long the finger has to stay still for the hint to open: Android's long
// press. And how far it can move without turning into a scroll.
const HOLD_MS = 500;
const TOLERANCE_PX = 10;
// How long the hint stays visible after the finger leaves.
const FICA_MS = 1500;
const VAO_PX = 10;

const TARGET = 'button, a[href]';
const LISTENER = '[data-toque-longo]';
export const LONG_TOUCH = 'toquelongo';

function textOf(el: Element) {
  // An empty aria-label also falls back to the title.
  const t = [el.getAttribute('aria-label'), el.getAttribute('title')].find(Boolean) ?? '';
  return t ? t.charAt(0).toUpperCase() + t.slice(1) : '';
}

export function installTouchHint() {
  let timer: number | undefined;
  let someTimer: number | undefined;
  let target: Element | null = null;
  let listener: Element | null = null;
  // The one of whoever requested the long press on this touch, even after the
  // notice: the `contextmenu` that arrives late doesn't open the system menu on
  // it either. The mouse and the keyboard reset it: the right click and the
  // menu key, after a touch, open the usual menu.
  let touchListener: Element | null = null;
  let origin = { x: 0, y: 0 };
  // The hint opened on this touch: the click that comes on release is
  // swallowed.
  let didOpen = false;
  let hint: HTMLDivElement | null = null;

  function showHint(el: Element) {
    const text = textOf(el);
    if (!text) return;
    didOpen = true;
    // Safari has no vibration: there the hint just appears.
    if ('vibrate' in navigator) navigator.vibrate(10);
    hint ??= Object.assign(document.createElement('div'), { className: 'touch-hint' });
    hint.setAttribute('role', 'tooltip');
    hint.textContent = text;
    // Inside the fullscreen element, when there is one: outside it the hint
    // doesn't show.
    (document.fullscreenElement ?? document.body).append(hint);
    hint.hidden = false;
    clearTimeout(someTimer);

    // Above the button, where the finger doesn't cover it; below, if it doesn't
    // fit.
    const r = el.getBoundingClientRect();
    const d = hint.getBoundingClientRect();
    const x = Math.min(innerWidth - d.width - 8, Math.max(8, r.left + r.width / 2 - d.width / 2));
    const above = r.top - d.height - VAO_PX;
    hint.style.left = `${x}px`;
    hint.style.top = `${above >= 8 ? above : r.bottom + VAO_PX}px`;
  }

  function abort() {
    clearTimeout(timer);
    target = null;
    listener = null;
  }

  // The long press arrived: the hint, if the button has one, and the notice to
  // whoever requested it.
  function held() {
    if (target) showHint(target);
    listener?.dispatchEvent(new CustomEvent(LONG_TOUCH));
    listener = null;
  }

  const onTouch = (e: PointerEvent) => {
    if (e.pointerType !== 'touch') {
      touchListener = null;
      return;
    }
    const from = e.target instanceof Element ? e.target : null;
    const el = from?.closest(TARGET) ?? null;
    didOpen = false;
    target = el && textOf(el) ? el : null;
    listener = from?.closest(LISTENER) ?? null;
    touchListener = listener;
    if (!target && !listener) return;
    origin = { x: e.clientX, y: e.clientY };
    clearTimeout(timer);
    timer = setTimeout(held, HOLD_MS);
  };
  const onMove = (e: PointerEvent) => {
    if (
      (target || listener) &&
      Math.hypot(e.clientX - origin.x, e.clientY - origin.y) > TOLERANCE_PX
    )
      abort();
  };
  const onRelease = () => {
    abort();
    if (!didOpen || !hint) return;
    someTimer = setTimeout(() => {
      if (hint) hint.hidden = true;
    }, FICA_MS);
  };
  // In the capture phase, before the button: after the hint, releasing the
  // finger doesn't do what the button does.
  const onClick = (e: MouseEvent) => {
    if (!didOpen) return;
    didOpen = false;
    e.preventDefault();
    e.stopImmediatePropagation();
  };
  // The menu key after a touch: the usual menu opens.
  const onKey = () => {
    touchListener = null;
  };
  // For the browser, `contextmenu` is the long press itself, and it may arrive
  // before our timer: the hint opens with it. And the system menu (open the
  // link, copy), which would open on the same touch, doesn't. Only on touch:
  // the right click and the menu key still open the usual menu.
  const onMenu = (e: Event) => {
    const from = e.target instanceof Element ? e.target : null;
    const el = from?.closest(TARGET) ?? null;
    const ofListener = !!touchListener && from?.closest(LISTENER) === touchListener;
    if (!ofListener && (!el || (target !== el && !didOpen))) return;
    e.preventDefault();
    if ((ofListener && listener) || (target === el && !didOpen)) {
      clearTimeout(timer);
      held();
    }
  };

  document.addEventListener('pointerdown', onTouch);
  document.addEventListener('pointermove', onMove);
  document.addEventListener('pointerup', onRelease);
  // The browser took the touch for itself (scrolling): the hint, if it opened,
  // disappears as on release. No click follows, and the leftover `didOpen` is
  // reset by the next touch.
  document.addEventListener('pointercancel', onRelease);
  document.addEventListener('click', onClick, true);
  document.addEventListener('contextmenu', onMenu);
  document.addEventListener('keydown', onKey, true);

  return () => {
    abort();
    clearTimeout(someTimer);
    hint?.remove();
    document.removeEventListener('pointerdown', onTouch);
    document.removeEventListener('pointermove', onMove);
    document.removeEventListener('pointerup', onRelease);
    document.removeEventListener('pointercancel', onRelease);
    document.removeEventListener('click', onClick, true);
    document.removeEventListener('contextmenu', onMenu);
    document.removeEventListener('keydown', onKey, true);
  };
}
