// The alert schedules as the screen edits them, and the round trip to the
// server. The camera form uses this on open and on save, and the camera card
// summarizes the alert with the day names, so it comes in the main bundle; the
// grid, which only the Alert tab uses, lives in weekGrid.ts, in its own chunk.
//
// The screen edits the time in minutes of the day, with BOTH ends included (the
// whole day is 00:00 to 23:59), and the server stores "HH:MM". No schedule
// crosses midnight: `to` is GREATER than `from`, never equal. A night is two
// schedules, one until 23:59 and another from 00:00 the next day.
import type { AlertSchedule } from './api.ts';
import { hourOfMinute } from './format.ts';

export const DAY = 1440;

// O mesmo teto do internal/config/params.go (MaxAlertSchedules).
export const MAX_SCHEDULES = 84;

// A schedule as the screen edits it: the hours are null with an empty field.
// The key holds the list row in place while typing; a schedule the grid
// rebuilds gets a new key, and the row is born again with its values.
export interface Schedule {
  key: number;
  days: number[];
  from: number | null;
  to: number | null;
}

let lastKey = 0;
export const newSchedule = (
  days: number[] = [],
  from: number | null = null,
  to: number | null = null,
): Schedule => ({
  key: ++lastKey,
  days,
  from,
  to,
});

// Valid: with a day, with both hours and `to` after `from`.
export const valid = (h: Schedule): h is Schedule & { from: number; to: number } =>
  h.days.length > 0 && h.from !== null && h.to !== null && h.to > h.from;

// "HH:MM" in minutes of the day, like the server's MinuteOfDay.
function textMinute(s: string): number | null {
  const m = /^(\d\d):(\d\d)$/.exec(s);
  const hour = Number(m?.[1]);
  const minute = Number(m?.[2]);
  return hour <= 23 && minute <= 59 ? hour * 60 + minute : null;
}

export function fromServer(hs: AlertSchedule[] | undefined): Schedule[] {
  return (hs ?? []).map((h) => newSchedule([...h.days], textMinute(h.from), textMinute(h.to)));
}

// Only the valid ones go: whoever checks what is missing is the save.
export function toServer(hs: Schedule[]): AlertSchedule[] {
  return hs.filter(valid).map((h) => ({
    days: [...h.days].sort((a, b) => a - b),
    from: hourOfMinute(h.from),
    to: hourOfMinute(h.to),
  }));
}

export const DIAS = ['dom', 'seg', 'ter', 'qua', 'qui', 'sex', 'sáb'];
export const LONG_DAYS = ['Domingo', 'Segunda', 'Terça', 'Quarta', 'Quinta', 'Sexta', 'Sábado'];

// "seg" and "Segunda", by day number.
export const dayName = (d: number, long = false) => (long ? LONG_DAYS : DIAS)[d] ?? '';

function joinWithAnd(parts: string[]) {
  return parts.length > 1 ? `${parts.slice(0, -1).join(', ')} e ${parts.at(-1)}` : (parts[0] ?? '');
}

// "seg a sex", "dom e sáb", "seg, qua e sex", "todo dia".
export function daysSummary(days: number[]) {
  const ds = [...new Set(days)].sort((a, b) => a - b);
  if (!ds.length) return 'nenhum dia';
  if (ds.length === 7) return 'todo dia';
  const seqs: [number, number][] = [];
  for (const d of ds) {
    const s = seqs.at(-1);
    if (s && d === s[1] + 1) s[1] = d;
    else seqs.push([d, d]);
  }
  const parts = seqs.flatMap(([a, b]) =>
    b - a >= 2
      ? [`${dayName(a)} a ${dayName(b)}`]
      : b > a
        ? [dayName(a), dayName(b)]
        : [dayName(a)],
  );
  return joinWithAnd(parts);
}

export function hoursSummary(from: number, to: number) {
  if (from === 0 && to === DAY - 1) return 'dia inteiro';
  return `${hourOfMinute(from)} às ${hourOfMinute(to)}`;
}
