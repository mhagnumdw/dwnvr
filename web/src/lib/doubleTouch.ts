// Double tap on the video, like YouTube on the phone. The video is split into
// three thirds: on the sides the double tap jumps back or forward, and each
// extra tap on the same side, right after, jumps again; in the middle it
// toggles fullscreen. Without `jump`, the video is a single third, the middle
// one.
//
// A single tap is only confirmed after DOUBLE_MS without another tap: until
// then it might be the first of a double. A tap that joins the jump sequence is
// not a single tap. Any tap that starts before that, even on a button, cancels
// it: whoever uses it calls `cancel` on the `pointerdown` of everything.
//
// The detector is its own, and not the browser's `dblclick`: Android's Chrome
// generates one `dblclick` per pair of taps, but not the follow-up taps of the
// sequence. So it only listens to touch; the mouse stays on `dblclick`.

// Two taps closer than this, in time and on screen, make a double tap.
const DOUBLE_MS = 300;
const DOUBLE_PX = 48;
// For how long, after a jump, one more tap on the same side adds another. The
// half-moon that shows the sum stays visible for that same time.
export const SEQUENCE_MS = 700;

export type Side = -1 | 1;

export function doubleTouch(actions: {
  jump?(side: Side): void;
  middle?(): void;
  single?(): void;
}) {
  let previous: { t: number; x: number; y: number } | null = null;
  let sequence: { side: Side; t: number } | null = null;
  let singleTimer: number | undefined;

  const onRelease = (e: PointerEvent) => {
    if (e.pointerType !== 'touch') return;
    // A tap on a button over the video belongs to the button.
    if (e.target instanceof Element && e.target.closest('button')) return;
    if (!(e.currentTarget instanceof HTMLElement)) return;

    clearTimeout(singleTimer);
    const r = e.currentTarget.getBoundingClientRect();
    const fraction = (e.clientX - r.left) / r.width;
    const side = !actions.jump ? 0 : fraction < 1 / 3 ? -1 : fraction > 2 / 3 ? 1 : 0;
    const t = e.timeStamp;

    if (sequence?.side === side && t - sequence.t < SEQUENCE_MS) {
      sequence.t = t;
      actions.jump?.(side);
      return;
    }
    sequence = null;

    const double =
      previous &&
      t - previous.t < DOUBLE_MS &&
      Math.hypot(e.clientX - previous.x, e.clientY - previous.y) < DOUBLE_PX;
    if (!double) {
      previous = { t, x: e.clientX, y: e.clientY };
      if (actions.single) singleTimer = setTimeout(() => actions.single?.(), DOUBLE_MS);
      return;
    }

    previous = null;
    if (side === 0) {
      actions.middle?.();
    } else {
      sequence = { side, t };
      actions.jump?.(side);
    }
  };

  return Object.assign(onRelease, {
    cancel() {
      clearTimeout(singleTimer);
    },
  });
}
