// The object detector's boxes over the recordings video: how long each one
// stays lit, where its label goes and how everything is drawn.
//
// On a canvas, and not in the DOM, for two reasons: it is the SAME drawing that
// goes into the downloaded image - one piece of code, and the image comes out
// equal to the screen -, and the family icons are already canvas paths
// (icons.ts).
//
// The drawing is the plain rectangle, with the label set apart from the object
// and tied to it by a stroke, so no label covers any object.

import type { Box } from './api.ts';
import { family, iconOf, drawIcon, CLASS_NAME } from './icons.ts';
import type { IconName } from './icons.ts';

interface Point {
  x: number;
  y: number;
}

interface Rect extends Point {
  w: number;
  h: number;
}

// What the boxes read from a mark: those from /api/rec/events and from a
// detection both work.
export interface ObjectDetectorMark {
  family: string;
  class: string;
  score: number;
  box?: Box;
}

// A placed box, in pixels of the displayed frame. `label` and `stroke` only
// exist when they were requested and fit.
export interface PlacedBox<M extends ObjectDetectorMark> {
  mark: M;
  color: string;
  priority: number;
  box: Rect;
  label?: Rect & { text: string; icon: IconName | null };
  stroke?: { from: Point; to: Point };
}

// --- parameters -------------------------------------------------------------

// How long the box stays lit, in CLOCK time and not video time: the box belongs
// to a single frame, and at 8x 1.5 s of video would go by in 190 ms - nobody
// would see it.
export const LIT_MS = 1500;
// The entrance is short so the box doesn't arrive late to the frame it
// describes; the exit is longer, so the eye notices it left.
export const LIGHT_UP_MS = 180;
export const FADE_MS = 450;
// How far a jump may land AFTER the inferred frame and still light the box.
// Tapping the timeline mark jumps to the onset, and the inferred frame is the
// onset itself whenever the peak didn't come after it - without this slack,
// playback would never "pass" through it.
export const LANDING_MS = 300;

// How far the drawn box grows on each side beyond the object detector's box, in
// SCREEN pixels - the same in thumbnail and fullscreen. The stroke sits inside
// the box, so with 0 it covers the object's edge, and on a 20 px cat that is
// half the ears. With 5, 3 px stay free around it. Near the edge the box stops
// at the image: the slack disappears only on that side. 0 goes back to the
// object detector's exact box. The Detections screen grid uses the same value.
export const BOX_SLACK_PX = 5;

// The label geometry.
const OFFSET_PX = 18; // between the label and the box edge
const SLACK_PX = 4; // between a label and what it is avoiding
const FONT = '600 11px system-ui, -apple-system, "Segoe UI", sans-serif';
const LABEL_HEIGHT_PX = 17;
const LABEL_PADDING_PX = 5;
const LABEL_RADIUS_PX = 5;
const ICON_PX = 12;
const ICON_GAP_PX = 4;
const BOX_STROKE_PX = 2;
const DARK = '#0d1117';

// --- placement --------------------------------------------------------------

// place computes, in pixels of the displayed frame (width x height), each
// mark's box, already with BOX_SLACK_PX, and - with `withLabel` - where the
// label goes and the stroke that ties it to the box. A mark without `box` is
// skipped.
//
// `g` is only used to measure text.
export function place<M extends ObjectDetectorMark>(
  g: CanvasRenderingContext2D,
  marks: readonly M[],
  width: number,
  height: number,
  withLabel: boolean,
): PlacedBox<M>[] {
  const fraction = (v: number) => Math.min(1, Math.max(0, v));
  const items = marks.flatMap((m): PlacedBox<M>[] => {
    if (!m.box) return [];
    const [x1, y1, x2, y2] = m.box;
    const x = Math.max(0, fraction(x1) * width - BOX_SLACK_PX);
    const y = Math.max(0, fraction(y1) * height - BOX_SLACK_PX);
    const xf = Math.min(width, fraction(x2) * width + BOX_SLACK_PX);
    const yf = Math.min(height, fraction(y2) * height + BOX_SLACK_PX);
    return [
      {
        mark: m,
        color: family(m.family).color,
        priority: family(m.family).priority,
        box: { x, y, w: xf - x, h: yf - y },
      },
    ];
  });
  if (!withLabel) return items;

  g.save();
  g.font = FONT;
  const boxes = items.map((it) => it.box);
  const placements: Rect[] = [];
  // By family priority: `person` picks its place first, as already happens with
  // the timeline icons.
  for (const it of [...items].sort((a, b) => a.priority - b.priority)) {
    const text = textFor(it.mark);
    const w = LABEL_PADDING_PX * 2 + ICON_PX + ICON_GAP_PX + Math.ceil(g.measureText(text).width);
    const h = LABEL_HEIGHT_PX;

    // The CLOSEST place to the box that fits entirely in the frame and covers
    // no box or other label. If there is none, the closest one that fits.
    let free: { r: Rect; near: number } | null = null;
    let reserve: { r: Rect; near: number } | null = null;
    for (const p of slots(it.box, w, h)) {
      if (p.x < 2 || p.y < 2 || p.x + w > width - 2 || p.y + h > height - 2) continue;
      const r = { x: p.x, y: p.y, w, h };
      const near = distance(r, it.box);
      if (!reserve || near < reserve.near) reserve = { r, near };
      const roomy = {
        x: r.x - SLACK_PX,
        y: r.y - SLACK_PX,
        w: w + 2 * SLACK_PX,
        h: h + 2 * SLACK_PX,
      };
      if (boxes.some((c) => overlaps(roomy, c)) || placements.some((q) => overlaps(roomy, q)))
        continue;
      if (!free || near < free.near) free = { r, near };
    }
    const label = (free ?? reserve)?.r;
    if (!label) continue; // frame smaller than the label itself
    placements.push(label);
    it.label = { ...label, text: text, icon: iconOf(it.mark, true) };
  }

  // The stroke, with all labels placed: the shortest path to the box that does
  // NOT cross another box or label. Crossing isn't forbidden, it is expensive:
  // in a tight frame a stroke still has to come out, and the least bad one
  // wins.
  for (const it of items) {
    const label = it.label;
    if (!label) continue;
    const obstacles = [
      ...boxes.filter((c) => c !== it.box),
      ...placements.filter((q) => q.x !== label.x || q.y !== label.y),
    ];
    let best: { from: Point; to: Point; cost: number } | null = null;
    for (const to of contactPoints(it.box)) {
      // The tip leaves from the label's EDGE, toward the box: aiming at a
      // corner, the stroke would be born loose, in the void of the curvature.
      const from = border(label, to);
      const cost =
        Math.hypot(to.x - from.x, to.y - from.y) +
        (obstacles.some((e) => crosses(from, to, e)) ? 10_000 : 0);
      if (!best || cost < best.cost) best = { from, to, cost };
    }
    if (best) it.stroke = { from: best.from, to: best.to };
  }
  g.restore();
  return items;
}

function textFor(m: ObjectDetectorMark) {
  const name = CLASS_NAME[m.class] ?? m.class;
  return `${name} ${m.score.toFixed(2).replace('.', ',')}`;
}

// The places where a label can sit: on the four sides of the box, sliding along
// the side and moving away little by little. Beside it, at the object's height,
// is usually closer than above it.
function slots(c: Rect, w: number, h: number) {
  const out: Point[] = [];
  for (let step = 0; step < 8; step++) {
    const d = OFFSET_PX + step * 12;
    for (const disp of [0, -1, 1, -2, 2]) {
      const middle = c.y + c.h / 2 - h / 2 + disp * (h + SLACK_PX);
      out.push({ x: c.x - d - w, y: middle }, { x: c.x + c.w + d, y: middle });
      const center = c.x + c.w / 2 - w / 2 + disp * (w / 2 + SLACK_PX);
      out.push({ x: center, y: c.y - d - h }, { x: center, y: c.y + c.h + d });
    }
  }
  return out;
}

const overlaps = (a: Rect, b: Rect) =>
  a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y;

// Distance between two rectangles: zero if they touch.
function distance(a: Rect, b: Rect) {
  const dx = Math.max(b.x - (a.x + a.w), a.x - (b.x + b.w), 0);
  const dy = Math.max(b.y - (a.y + a.h), a.y - (b.y + b.h), 0);
  return Math.hypot(dx, dy);
}

// Where the stroke can touch the box: the middle of each side and the four
// corners.
const contactPoints = (r: Rect): Point[] => [
  { x: r.x + r.w / 2, y: r.y },
  { x: r.x + r.w / 2, y: r.y + r.h },
  { x: r.x, y: r.y + r.h / 2 },
  { x: r.x + r.w, y: r.y + r.h / 2 },
  { x: r.x, y: r.y },
  { x: r.x + r.w, y: r.y },
  { x: r.x, y: r.y + r.h },
  { x: r.x + r.w, y: r.y + r.h },
];

// The point on the rectangle's border in the direction of a target.
function border(r: Rect, target: Point): Point {
  const cx = r.x + r.w / 2;
  const cy = r.y + r.h / 2;
  const dx = target.x - cx;
  const dy = target.y - cy;
  if (!dx && !dy) return { x: cx, y: cy };
  const t = Math.min(
    dx ? r.w / 2 / Math.abs(dx) : Infinity,
    dy ? r.h / 2 / Math.abs(dy) : Infinity,
  );
  return { x: cx + dx * t, y: cy + dy * t };
}

// Does the segment from p to q cut the rectangle? Liang-Barsky clipping: the
// segment lives in t from 0 to 1, and each side of the rectangle narrows that
// range. If a range is left, it crossed.
function crosses(p: Point, q: Point, r: Rect) {
  let t0 = 0;
  let t1 = 1;
  const dx = q.x - p.x;
  const dy = q.y - p.y;
  const sides: [num: number, den: number][] = [
    [p.x - r.x, -dx],
    [r.x + r.w - p.x, dx],
    [p.y - r.y, -dy],
    [r.y + r.h - p.y, dy],
  ];
  for (const [num, den] of sides) {
    if (den === 0) {
      if (num < 0) return false;
      continue;
    }
    const t = num / den;
    if (den < 0) {
      if (t > t1) return false;
      if (t > t0) t0 = t;
    } else {
      if (t < t0) return false;
      if (t < t1) t1 = t;
    }
  }
  return true;
}

// --- drawing ----------------------------------------------------------------

// draw paints the items from `place` onto the context, each with the opacity
// that `alpha(item)` says. The layer order is fixed - strokes, boxes, labels -
// so a label never ends up under another's stroke.
export function draw<M extends ObjectDetectorMark>(
  g: CanvasRenderingContext2D,
  items: readonly PlacedBox<M>[],
  alpha: (it: PlacedBox<M>) => number = () => 1,
) {
  g.save();
  // The lowest-priority family first: `person` ends up on top.
  const order = [...items].sort((a, b) => b.priority - a.priority);

  for (const it of order) {
    if (!it.stroke) continue;
    g.globalAlpha = alpha(it);
    const { from, to } = it.stroke;
    // A dark hairline under the colored one: a 1 px stroke vanishes on both a
    // light wall and the gray of night without it.
    const strokes: [width: number, color: string, opacity: number][] = [
      [2.5, '#000', 0.65],
      [1, it.color, 1],
    ];
    for (const [width, color, opacity] of strokes) {
      g.globalAlpha = alpha(it) * opacity;
      g.strokeStyle = color;
      g.lineWidth = width;
      g.lineCap = 'round';
      g.beginPath();
      g.moveTo(from.x, from.y);
      g.lineTo(to.x, to.y);
      g.stroke();
    }
    g.globalAlpha = alpha(it);
    g.beginPath();
    g.arc(to.x, to.y, 2.5, 0, 7);
    g.fillStyle = it.color;
    g.fill();
    g.lineWidth = 1;
    g.strokeStyle = 'rgba(0,0,0,.65)';
    g.stroke();
  }

  for (const it of order) {
    g.globalAlpha = alpha(it);
    const { x, y, w, h } = it.box;
    g.strokeStyle = it.color;
    g.lineWidth = BOX_STROKE_PX;
    // Inset by half a stroke: the border stays INSIDE the box, like a CSS
    // `border`.
    const m = BOX_STROKE_PX / 2;
    g.strokeRect(x + m, y + m, Math.max(0, w - 2 * m), Math.max(0, h - 2 * m));
  }

  g.font = FONT;
  g.textBaseline = 'middle';
  for (const it of order) {
    if (!it.label) continue;
    g.globalAlpha = alpha(it);
    const { x, y, w, h, text: text, icon } = it.label;
    g.beginPath();
    // Safari before 16 doesn't have roundRect.
    if (typeof g.roundRect === 'function') g.roundRect(x, y, w, h, LABEL_RADIUS_PX);
    else g.rect(x, y, w, h);
    g.fillStyle = it.color;
    g.fill();
    g.lineWidth = 1;
    g.strokeStyle = 'rgba(0,0,0,.6)';
    g.stroke();
    if (icon) drawIcon(g, icon, x + LABEL_PADDING_PX, y + (h - ICON_PX) / 2, ICON_PX, DARK);
    g.fillStyle = DARK;
    g.fillText(text, x + LABEL_PADDING_PX + ICON_PX + ICON_GAP_PX, y + h / 2 + 0.5);
  }
  g.restore();
}
