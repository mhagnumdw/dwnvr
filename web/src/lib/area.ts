// The area: a polygon drawn over a camera's image, with X and Y as a fraction
// of the frame, from 0 to 1, like the object's box. It applies to the detection
// frame and to the segment's keyframe, at any resolution.
//
// The server decides whether an object is inside (internal/area): the screen
// only draws the area and sends it in the query.

import { tokenColor } from './theme.ts';

export type Point = [x: number, y: number];

// The rules, with the JSON and query value (en-US), the name and the icon on
// the screen.
export const RULES = {
  foot: {
    name: 'pisa',
    icon: 'ruleStepsOn',
    hint: 'conta o pé: o meio da borda de baixo da caixa tem de estar dentro. Desenhe a área no chão.',
  },
  touch: {
    name: 'encosta',
    icon: 'ruleTouches',
    hint: 'basta a caixa tocar a área. Pega também quem só passa na frente dela.',
  },
} as const;
export type Rule = keyof typeof RULES;

// The default, the same as the server's area.DefaultRule.
export const DEFAULT_RULE: Rule = 'foot';

// The same limits as internal/area/params.go (MAX_AREAS is MaxAreasPerCamera).
export const MIN_POINTS = 3;
export const MAX_POINTS = 20;
export const MAX_AREAS = 8;

// The zoom of the small area's thumbnail. An area is small when its rectangle
// is smaller than SMALL_AREA of the frame in both width and height: only with
// both is the zoom worth it, because the whole area has to fit in it (a long,
// thin strip can't be enlarged). The framing is the rectangle with ZOOM_MARGIN
// of slack around it, and it zooms in at most ZOOM_CEILING times.
export const SMALL_AREA = 0.2;
export const ZOOM_MARGIN = 1.6;
export const ZOOM_CEILING = 4;

export interface FilterArea {
  cam: string;
  points: Point[];
  rule: Rule;
}

const isRule = (v: string | null): v is Rule => v !== null && Object.hasOwn(RULES, v);

// The `area=` of the query: `<cam>:<x>,<y>,...`, in thousandths of the frame. A
// thousandth is more than a finger needs and keeps the URL short.
export function areaInQuery(a: FilterArea) {
  const n = (v: number) => Math.round(Math.min(1, Math.max(0, v)) * 1000);
  return `${a.cam}:${a.points.map(([x, y]) => `${n(x)},${n(y)}`).join(',')}`;
}

// The way back: null when the link doesn't carry a valid area, and then the
// screen opens without an area filter, instead of showing a server error.
export function queryArea(area: string | null, rule: string | null): FilterArea | null {
  if (!area) return null;
  const i = area.lastIndexOf(':');
  if (i <= 0) return null;
  const numbers = area
    .slice(i + 1)
    .split(',')
    .map(Number);
  if (numbers.length % 2 || numbers.some((v) => !Number.isInteger(v) || v < 0 || v > 1000))
    return null;
  const points: Point[] = [];
  for (let j = 0; j < numbers.length; j += 2)
    points.push([(numbers[j] ?? 0) / 1000, (numbers[j + 1] ?? 0) / 1000]);
  if (points.length < MIN_POINTS || points.length > MAX_POINTS) return null;
  return { cam: area.slice(0, i), points, rule: isRule(rule) ? rule : DEFAULT_RULE };
}

// The points as they go into cameras.json: 4 decimals are plenty for a finger
// (a tenth of a pixel on a 1080p frame) and keep the file readable.
export const registryPoints = (points: readonly Point[]) =>
  points.map(([x, y]): Point => [Math.round(x * 1e4) / 1e4, Math.round(y * 1e4) / 1e4]);

// The points for the `points` of a <polygon> with viewBox 0 0 1 1.
export const svgPoints = (points: readonly Point[]) =>
  points.map(([x, y]) => `${x},${y}`).join(' ');

// The polygon's path on a canvas already translated to the image, with width
// `w` and height `h`.
function path(g: CanvasRenderingContext2D, points: readonly Point[], w: number, h: number) {
  g.beginPath();
  for (const [i, [x, y]] of points.entries()) {
    if (i) g.lineTo(x * w, y * h);
    else g.moveTo(x * w, y * h);
  }
  g.closePath();
}

// The theme's accent, read on the canvas being painted: over the video it is the
// dark palette's (lib/theme.ts).
const blue = (g: CanvasRenderingContext2D) => tokenColor(g.canvas, '--accent');

// The area's outline on a canvas already translated to the image, with width
// `w` and height `h`: thin and dashed, like in the thumbnails.
export function drawOutline(
  g: CanvasRenderingContext2D,
  points: readonly Point[],
  w: number,
  h: number,
) {
  if (points.length < MIN_POINTS) return;
  g.save();
  path(g, points, w, h);
  g.setLineDash([6, 4]);
  g.lineWidth = 1.5;
  g.strokeStyle = blue(g);
  g.stroke();
  g.restore();
}

// The filled area: the translucent blue and the solid stroke of the list in the
// Áreas tab. It is the drawing of a single area, in its thumbnail and
// highlighted on the detection frame, where the thin dashed line gets lost in
// the image.
export function fillArea(
  g: CanvasRenderingContext2D,
  points: readonly Point[],
  w: number,
  h: number,
) {
  if (points.length < MIN_POINTS) return;
  g.save();
  path(g, points, w, h);
  g.fillStyle = blue(g);
  g.globalAlpha = 0.22;
  g.fill();
  g.globalAlpha = 1;
  g.lineWidth = 1.5;
  g.strokeStyle = blue(g);
  g.stroke();
  g.restore();
}

// A region of the frame, as fractions of it.
export interface Vista {
  x: number;
  y: number;
  w: number;
  h: number;
}

export const WHOLE_FRAME: Vista = { x: 0, y: 0, w: 1, h: 1 };

// The region of the frame that frames the small area, or null if it isn't
// small. Width and height use the same fraction, so the view keeps the frame's
// aspect ratio; the center is the area's, pulled inside the frame.
export function areaView(points: readonly Point[]): Vista | null {
  if (points.length < MIN_POINTS) return null;
  const xs = points.map(([x]) => x);
  const ys = points.map(([, y]) => y);
  const [x0, x1, y0, y1] = [Math.min(...xs), Math.max(...xs), Math.min(...ys), Math.max(...ys)];
  if (x1 - x0 >= SMALL_AREA || y1 - y0 >= SMALL_AREA) return null;
  const side = Math.max((x1 - x0) * ZOOM_MARGIN, (y1 - y0) * ZOOM_MARGIN, 1 / ZOOM_CEILING);
  const isInside = (c: number) => Math.min(Math.max(c - side / 2, 0), 1 - side);
  return { x: isInside((x0 + x1) / 2), y: isInside((y0 + y1) / 2), w: side, h: side };
}

// The area's points in the view: fractions of the view, not of the frame.
export const viewPoints = (points: readonly Point[], v: Vista): Point[] =>
  points.map(([x, y]) => [(x - v.x) / v.w, (y - v.y) / v.h]);
