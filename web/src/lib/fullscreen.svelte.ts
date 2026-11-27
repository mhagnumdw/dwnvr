// Fullscreen of any element on the page, and not just the <video>: it is what
// brings the controls and the timeline along.
//
// Safari on the iPhone only puts the <video> in fullscreen, and with its native
// player. There, and wherever the browser refuses, the fullscreen is SIMULATED:
// the element gets the `data-simulated-fullscreen` attribute and covers the
// window through CSS (rule in app.css). The browser bar stays visible; the rest
// of the page doesn't.
//
// One element at a time, as in the browser API: that's why the state belongs to
// the module, and users compare `fullscreen.el` with their own element.

const ATTRIBUTE = 'data-simulated-fullscreen';

export const fullscreen = $state<{ el: HTMLElement | null; simulated: boolean }>({
  el: null,
  simulated: false,
});

document.addEventListener('fullscreenchange', () => {
  if (fullscreen.simulated) return;
  fullscreen.el =
    document.fullscreenElement instanceof HTMLElement ? document.fullscreenElement : null;
});

// The real fullscreen's Esc belongs to the browser; the simulated one needs its
// own. `preventDefault` tells whoever listens afterward, like the Modal, that
// the key was already used: without it, the same Esc would also close the sheet
// the fullscreen was in.
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && fullscreen.simulated) {
    e.preventDefault();
    leaveFullscreen();
  }
});

function simulate(el: HTMLElement) {
  el.setAttribute(ATTRIBUTE, '');
  fullscreen.el = el;
  fullscreen.simulated = true;
}

export function enterFullscreen(el: HTMLElement) {
  // From a real fullscreen to another the browser itself switches; from a
  // simulated one, it doesn't.
  if (fullscreen.simulated) leaveFullscreen();
  if (typeof el.requestFullscreen !== 'function') {
    simulate(el);
    return;
  }
  el.requestFullscreen().catch(() => {
    simulate(el);
  });
}

export function leaveFullscreen() {
  if (fullscreen.simulated) {
    fullscreen.el?.removeAttribute(ATTRIBUTE);
    fullscreen.el = null;
    fullscreen.simulated = false;
    return;
  }
  if (document.fullscreenElement) void document.exitFullscreen();
}

export function toggleFullscreen(el: HTMLElement) {
  if (fullscreen.el === el) leaveFullscreen();
  else enterFullscreen(el);
}
