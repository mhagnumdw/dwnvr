// Hash routing, with the screen state alongside it.
//
// The hash is `#<route>?<query>`: the route picks the screen, the query
// describes what is being viewed inside it. That is what makes copying the
// address bar and pasting it in another tab land on the same scene - the same
// cameras, the same instant, the same zoom - instead of the generic screen.
//
// It stays a hash, and not a real path, for a practical reason: the fragment
// never leaves the browser, so no new route has to exist on the server side.
// internal/api/web.go only serves files, and a real `/rec` would require
// returning index.html for every path that does not exist.

import { dayKey, hhmmss } from './format.ts';

export const DEFAULT_ROUTE = 'live';

// Floor between two writes to the address bar. The player's instant changes
// several times a second, and Safari rejects more than 100 replaceState in 30s;
// one second leaves ~30 inside the window it counts, with plenty of margin.
const FLOOR_MS = 1000;

function parts(hash: string) {
  const raw = hash.replace(/^#/, '');
  const i = raw.indexOf('?');
  return i < 0
    ? { id: raw || DEFAULT_ROUTE, query: '' }
    : { id: raw.slice(0, i) || DEFAULT_ROUTE, query: raw.slice(i + 1) };
}

// Reading is done by URLSearchParams, which is what the platform already
// brings: it decodes, tolerates an invalid sequence from a hand-written URL and
// resolves a repeated parameter with getAll. WRITING is custom only because of
// two characters: its toString() percent-encodes `:` and `,`, and
// `t=14%3A32%3A07` would throw away the readability that is the reason for
// writing the time this way. Both are allowed in a query by RFC 3986, and
// reading accepts both forms - so an old link or one copied from elsewhere
// stays valid.
const enc = (s: string) => encodeURIComponent(s).replaceAll('%3A', ':').replaceAll('%2C', ',');

function join(sp: URLSearchParams) {
  const out: string[] = [];
  for (const [k, v] of sp) out.push(`${enc(k)}=${enc(v)}`);
  return out.join('&');
}

export const routeState = $state({
  id: parts(location.hash).id,
  // The whole hash, as it was at the last REAL navigation. It is the key that
  // remounts the screen: the writes here use replaceState, which does not fire
  // hashchange, so they do not touch this value and do not remount anything.
  hash: location.hash,
});

// scene says in words what the screen shows ("frente, 14/10 às 04:48:05"), for
// the share text. Only the screens that show a scene fill it in; switching
// screens clears it, so one screen's text does not come out in another's link.
export const scene = $state({ text: '' });

// currentParams reads the query as of now. The screens call it once, on
// initialization: the URL is the source of the FIRST read, and from then on the
// screen state rules, writing back.
export function currentParams() {
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- single read, on initialization
  return new URLSearchParams(parts(location.hash).query);
}

let pending: URLSearchParams | null = null; // what has not yet reached the address bar
let timer: number | undefined;
let lastWrite = 0;

// write merges a handful of keys into what is already in the URL.
//
// - null or undefined deletes the key. That is how what is at the default stays
//   out of the link.
// - an array becomes a repeated parameter (`cams=a&cams=b`), which is how an
//   HTML form serializes multiple choice. With no separator there is nothing to
//   confuse: the camera id comes from the stream name in go2rtc and may contain
//   a comma.
// - an empty array writes the key with no value, because "no camera checked" is
//   a choice, different from having no opinion at all about the selection.
// The value of a URL key: see write().
export type ValueInURL = string | number | boolean | readonly string[] | null | undefined;

export function write(patch: Record<string, ValueInURL>) {
  const sp = pending ?? currentParams();
  for (const [k, v] of Object.entries(patch)) {
    sp.delete(k);
    if (v === null || v === undefined) continue;
    if (!isList(v)) sp.set(k, String(v));
    else if (!v.length) sp.set(k, '');
    else for (const item of v) sp.append(k, item);
  }
  pending = sp;

  if (timer) return;
  const wait = FLOOR_MS - (Date.now() - lastWrite);
  if (wait <= 0) apply();
  else timer = setTimeout(apply, wait);
}

// Array.isArray does not narrow `readonly string[]`: see
// microsoft/TypeScript#17002.
function isList(v: ValueInURL): v is readonly string[] {
  return Array.isArray(v);
}

function apply() {
  timer = undefined;
  if (!pending) return;
  const query = join(pending);
  pending = null;

  const target = `#${routeState.id}${query ? `?${query}` : ''}`;
  // Nothing to do if the address bar already says so, and during playback that
  // is the common case: `t` is written in seconds, so most frames do not change
  // a single letter. With no write, the floor does not run either - the next
  // second shows up right away, without waiting for the interval.
  if (location.hash === target) return;

  lastWrite = Date.now();
  try {
    history.replaceState(history.state, '', target);
  } catch {
    // Safari throws when it goes past the replaceState limit. Losing a write to
    // the address bar must not bring the screen down: the next one fixes it.
  }
}

// screenLink is the link of the scene that is on screen now, to send to
// someone. It reads the pending write before the address bar: with the 1s
// floor, location.href may be one second behind the player. It writes nothing.
export function screenLink() {
  const query = join(pending ?? currentParams());
  return `${location.origin}${location.pathname}#${routeState.id}${query ? `?${query}` : ''}`;
}

// fixRoute swaps the route name in the address bar, when the screen that opened
// is not the requested one (see App). The query stays: either it is already the
// one the open screen wrote, or it is harmless garbage from the requested link.
// No hashchange and no touching routeState.hash, so the screen does not
// remount.
export function fixRoute(id: string) {
  routeState.id = id;
  const query = parts(location.hash).query;
  try {
    history.replaceState(history.state, '', `#${id}${query ? `?${query}` : ''}`);
  } catch {
    // Safari's limit, as in apply: the screen's next write already goes out
    // with the right name, because it reads routeState.id.
  }
}

addEventListener('hashchange', () => {
  // The screen that is leaving may have left a scheduled write. It describes
  // the old route, and landing on the new route would be pure dirt -
  // `#live?cam=x&day=…`.
  clearTimeout(timer);
  timer = undefined;
  pending = null;
  scene.text = '';

  routeState.id = parts(location.hash).id;
  routeState.hash = location.hash;
});

// recordingsLink takes Recordings already to the camera and instant of a
// detection. It is the same link in the modal and in the ⋮ menu of the
// Detections screen.
export function recordingsLink(cam: string, instantMs: number) {
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- single-read date, nobody observes it
  return `#rec?cam=${encodeURIComponent(cam)}&day=${dayKey(new Date(instantMs))}&t=${hhmmss(instantMs)}`;
}
