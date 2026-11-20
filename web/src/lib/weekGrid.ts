// The week grid of the alert schedules, with no DOM: only the Alert tab uses
// it, and it comes in its chunk.
//
// The grid works over the "coverage": one byte per minute of the week (10080),
// Sunday 00:00 first. A schedule becomes coverage losslessly; the way back
// generates the canonical list, which is what the screen shows after painting.
import { DAY, newSchedule, valid } from './schedules.ts';
import type { Schedule } from './schedules.ts';
// The day names and the summaries live in schedules.ts, because the camera card
// also shows them; they keep being re-exported from here for whoever already
// asked for them.
export { DIAS, LONG_DAYS, dayName, hoursSummary, daysSummary } from './schedules.ts';

const WEEK = 7 * DAY;

export type Coverage = Uint8Array;

export function coverage(hs: Schedule[]): Coverage {
  const c = new Uint8Array(WEEK);
  for (const h of hs) {
    if (!valid(h)) continue;
    for (const d of h.days) c.fill(1, d * DAY + h.from, d * DAY + h.to + 1);
  }
  return c;
}

// The checked runs between two minutes of the week, [a, b] relative to the
// start, with both ends included.
function sequences(c: Coverage, start: number, size: number): [number, number][] {
  const rs: [number, number][] = [];
  let a = -1;
  for (let m = 0; m < size; m++) {
    const on = c[start + m];
    if (on && a < 0) a = m;
    if (!on && a >= 0) {
      rs.push([a, m - 1]);
      a = -1;
    }
  }
  if (a >= 0) rs.push([a, size - 1]);
  return rs;
}

// The canonical list: each checked run of a day is a schedule, and days with
// the same schedule become a single schedule. A one-minute run (`to` equal to
// `from`) is not a valid schedule and is left out. It is left over when "12:30
// until 13:00" loses hour 12 in the grid: minute 13:00 stays alone.
export function schedulesOf(c: Coverage): Schedule[] {
  const groups = new Map<string, Schedule>();
  for (let d = 0; d < 7; d++)
    for (const [a, b] of sequences(c, d * DAY, DAY)) {
      if (b === a) continue;
      const k = `${a}-${b}`;
      const g = groups.get(k) ?? newSchedule([], a, b);
      g.days.push(d);
      groups.set(k, g);
    }
  // In the order of the first day, and on the same day, of the hour.
  return [...groups.values()].sort(
    (x, y) => (x.days[0] ?? 0) - (y.days[0] ?? 0) || (x.from ?? 0) - (y.from ?? 0),
  );
}

// Checks (or unchecks) the whole hours of the rectangle between two days and
// two hours, in any order.
export function paintHours(
  c: Coverage,
  d0: number,
  h0: number,
  d1: number,
  h1: number,
  mark: boolean,
): Coverage {
  const n = c.slice();
  for (let d = Math.min(d0, d1); d <= Math.max(d0, d1); d++)
    for (let h = Math.min(h0, h1); h <= Math.max(h0, h1); h++)
      n.fill(mark ? 1 : 0, d * DAY + h * 60, d * DAY + h * 60 + 60);
  return n;
}

// The checked clips of an hour, in minutes 0..59 with both ends.
export const clipsOfHour = (c: Coverage, d: number, h: number) =>
  sequences(c, d * DAY + h * 60, 60);

// The whole hour checked is a single clip, from minute 0 to 59.
export const whole = (ps: [number, number][]) =>
  ps.length === 1 && ps[0]?.[0] === 0 && ps[0][1] === 59;

export const fullHour = (c: Coverage, d: number, h: number) => whole(clipsOfHour(c, d, h));
