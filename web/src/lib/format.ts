// Shared formatters. They live in one place so that the same quantity does not
// show up written two ways on different screens.

const pad = (n: number) => String(n).padStart(2, '0');

// num writes a number in the local style: decimal comma and no trailing zero
// ("20 GB", not "20,00 GB"). Instantiating Intl on every call is expensive and
// these formatters show up on a screen that updates every 3 seconds, so it
// caches.
const nfs = new Map<number, Intl.NumberFormat>();
function num(v: number, decimals: number) {
  let f = nfs.get(decimals);
  if (!f)
    nfs.set(decimals, (f = new Intl.NumberFormat('pt-BR', { maximumFractionDigits: decimals })));
  return f.format(v);
}

// taxa writes the player's playback speed ("0,25×", "1×", "16×"). It lives here
// for the same reason as the others: the decimal comma of the rest of the
// interface - the raw `{r}×` put "0.5×" on a whole Portuguese screen.
export function taxa(r: number) {
  return `${num(r, 2)}×`;
}

export function hhmmss(ms: number) {
  const d = new Date(ms);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

export function hhmm(ms: number) {
  const d = new Date(ms);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// hourOfMinute writes a minute of the day (0 to 1439) as HH:MM: it is how the
// times of the Detections interval are stored.
export function hourOfMinute(m: number) {
  return `${pad(Math.floor(m / 60))}:${pad(m % 60)}`;
}

// ddmm is the short date of the intervals ("14/07 a 11/08"). The year is left
// out because the intervals shown in the interface span days or weeks, and
// repeating it at every end would only take up space.
export function ddmm(ms: number) {
  const d = new Date(ms);
  return `${pad(d.getDate())}/${pad(d.getMonth() + 1)}`;
}

// shortDay is the day for a message that gets stored: "14/10", and with the
// year only when it is not this year. Never "yesterday", which tomorrow is
// already a lie.
export function shortDay(ms: number, now = Date.now()) {
  const year = new Date(ms).getFullYear();
  return year === new Date(now).getFullYear() ? ddmm(ms) : `${ddmm(ms)}/${year}`;
}

// dateTime is the full instant, "10/10/2026 14:32", for what may have happened
// months ago.
export function dateTime(ms: number) {
  const d = new Date(ms);
  return `${ddmm(ms)}/${d.getFullYear()} ${hhmm(ms)}`;
}

// todayYesterday is the recent instant read at a glance: "hoje 08:12", "ontem
// 22:47", and the full dateTime from the day before yesterday backwards.
export function todayYesterday(ms: number, now: number) {
  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  const day = new Date(ms).toDateString();
  if (day === new Date(now).toDateString()) return `hoje ${hhmm(ms)}`;
  if (day === yesterday.toDateString()) return `ontem ${hhmm(ms)}`;
  return dateTime(ms);
}

// timezoneClock writes an instant in the timezone of ANOTHER machine: it takes
// the epoch in milliseconds and that timezone's offset in seconds, and returns
// "18/08/2026 18:14:57".
//
// The offset is added to the epoch and the reading comes out through the UTC
// getters. It looks like a trick and it is: Date only knows how to read in the
// browser's timezone, so shifting the instant by the other offset and then
// reading in UTC cancels exactly the conversion we do not want. And we do not
// want it because the only use of this is to show another machine's clock -
// converted, it would become the time the reader already has on their own
// wrist, which answers nothing.
export function timezoneClock(epochMs: number, offsetSeconds: number) {
  const d = new Date(epochMs + offsetSeconds * 1000);
  const data = `${pad(d.getUTCDate())}/${pad(d.getUTCMonth() + 1)}/${d.getUTCFullYear()}`;
  const hour = `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}`;
  return `${data} ${hour}`;
}

export function dayKey(date = new Date()) {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

// Text outside the format gives an invalid date, which the caller checks with
// isNaN.
export function parseDay(key: string) {
  const [y = NaN, m = NaN, d = NaN] = key.split('-').map(Number);
  return new Date(y, m - 1, d);
}

export function bytes(n: number | undefined) {
  if (!n) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'] as const;
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), u.length - 1);
  const v = n / 1024 ** i;
  // Above 100 in the unit the decimal place is just noise: "761 MB" is enough.
  return `${num(v, i === 0 || v >= 100 ? 0 : 2)} ${u[i] ?? 'B'}`;
}

// mbToBytes exists because quota and minimum free are configured in MB. Showing
// the raw number left "20480 MB" on screen next to a "3,62 GB" - same quantity,
// two units, which is exactly what this file avoids.
export function mbToBytes(mb: number | undefined) {
  return bytes((mb ?? 0) * 1024 ** 2);
}

export function kbps(v: number | undefined) {
  if (!v) return '-';
  return v >= 1000 ? `${num(v / 1000, 2)} Mbps` : `${num(Math.round(v), 0)} kbps`;
}

// resolution shows what is actually being recorded, read from the stream's
// init. It only exists after the first connection - before that there is
// nothing to state.
export function resolution(w: number | undefined, h: number | undefined) {
  if (!w || !h) return '-';
  return `${w}×${h}`;
}

// duration formats an interval with the reader in mind: always two orders of
// magnitude, the largest and the next one.
//
// "7h 17min" and not "7,3h" because a decimal fraction of an hour makes the
// reader convert in their head - and these durations are read in the middle of
// a diagnosis, where nobody wants to do math. The smaller unit disappears when
// it is zero, so as not to leave "7h 0min" on screen.
export function duration(ms: number | undefined) {
  if (!ms || ms < 0) return '-';
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;

  // Rounding happens once, at the minute, and the rest is built from it:
  // rounding hour and minute separately is what produces "7h 60min".
  const totalMin = Math.round(s / 60);
  if (totalMin < 60) return `${totalMin}min`;

  const totalH = Math.floor(totalMin / 60);
  const min = totalMin % 60;
  if (totalH < 24) return min ? `${totalH}h ${min}min` : `${totalH}h`;

  const d = Math.floor(totalH / 24);
  const h = totalH % 24;
  return `${d} ${d === 1 ? 'dia' : 'dias'}${h ? ` ${h}h` : ''}`;
}

// MIN_GAP_MS is the shortfall from which "gravado" appears next to "retido".
// Below it the difference always exists and is not a gap: the segment being
// recorded right now has not entered the index yet, and each reconnect loses a
// few seconds. One hour is far above that noise and well below any outage worth
// seeing.
export const MIN_GAP_MS = 3600000;

// recordedWithGap returns the video that actually exists (recordedMs from
// /api/health) when it is below the retained by more than MIN_GAP_MS, and 0
// when not: a camera without a failure shows only the retained, as it always
// did.
// The retained comes in ready because each screen already computes it for
// itself.
export function recordedWithGap(retainedMs: number | undefined, recordedMs: number | undefined) {
  if (!retainedMs || !recordedMs) return 0;
  return retainedMs - recordedMs > MIN_GAP_MS ? recordedMs : 0;
}

// formatDays turns the retention estimate into something understandable. "20 GB"
// tells nobody anything; "≈ 8 dias 5h" tells everything.
export function formatDays(n: number | undefined) {
  // The estimate arrives from the server in fractional days, but it is read
  // right next to "retido", which comes out of duration() in days and hours.
  // Writing one as a fraction ("8,2 dias") and the other in hours ("8 dias 5h")
  // makes the reader convert in their head just to compare the two - and
  // comparing the two is the only reason the pair exists. So the formatting is
  // the same, and duration() already handles the hour fractions that small
  // quotas produce better than this did.
  return duration((n ?? 0) * 86400000);
}
