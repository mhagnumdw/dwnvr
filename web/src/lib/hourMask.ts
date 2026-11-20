// The mask for the typed time, the one on the Recordings clock and on the time
// fields of Detections.
//
// It reads what was typed in groups: hour, minute and, when present, second. An
// hour with one digit is completed on the left (1 = 01h); minute and second
// with one digit are completed on the right (3 = 30), which is what you mean
// when typing "17:3". The colons appear on their own when the group accepts no
// more digits, and period, comma and space count as colons: the phone's numeric
// keyboard has no ":".

// 3 is HH:MM:SS; 2 is HH:MM.
export type Groups = 2 | 3;

const isFull = (g: string[], i: number) => {
  const x = g[i] ?? '';
  return x.length === 2 || (i === 0 && x.length === 1 && +x >= 3);
};

// `deleting` holds back the automatic trailing colons: without it, deleting the
// ":" would bring it right back. `rejected` says some digit was left out, so
// whoever uses it can shake the field.
export function mask(raw: string, deleting: boolean, groups: Groups) {
  const last = groups - 1;
  const g = [''];
  let rejected = false;
  for (const c of raw.replace(/[.,;\s]/g, ':')) {
    let i = g.length - 1;
    if (c === ':') {
      if (g[i] !== '' && i < last) g.push('');
      continue;
    }
    if (c < '0' || c > '9') continue;
    if (isFull(g, i)) {
      if (i === last) {
        rejected = true;
        continue;
      }
      g.push('');
      i++;
    }
    // A digit that would only form an impossible time is rejected here, and not
    // at submit time: "24" or the "7" of "17:7" (70 min) never even appear.
    const newItem = (g[i] ?? '') + c;
    const ok = i === 0 ? newItem.length === 1 || +newItem <= 23 : newItem.length === 2 || c <= '5';
    if (!ok) {
      rejected = true;
      continue;
    }
    g[i] = newItem;
  }
  const i = g.length - 1;
  if (!deleting && i < last && g[i] !== '' && isFull(g, i)) g.push('');
  return { g, rejected };
}

// The second of the day that the groups describe, or null with no hour at all.
export function secondsOfDay(g: string[]) {
  if (!g[0]) return null;
  const tens = (x: string) => (x.length === 1 ? +x * 10 : +x);
  return +g[0] * 3600 + tens(g[1] ?? '') * 60 + tens(g[2] ?? '');
}

// The rest of the time in gray after what was already typed: "17:3" gets "0:00"
// (or "0" without seconds), and you can see where Enter leads.
export function ghost(g: string[], groups: Groups) {
  if (!g[0]) return '';
  const i = g.length - 1;
  const last = g[i] ?? '';
  let s = last.length === 0 ? '00' : i > 0 && last.length === 1 ? '0' : '';
  for (let k = i + 1; k < groups; k++) s += ':00';
  return s;
}

// The field shake when a digit is rejected.
export function shake(el: HTMLElement | undefined) {
  el?.animate(
    [
      { transform: 'none' },
      { transform: 'translateX(-2px)' },
      { transform: 'translateX(2px)' },
      { transform: 'none' },
    ],
    { duration: 180 },
  );
}
