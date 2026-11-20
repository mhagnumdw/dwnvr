// Global state, with Svelte 5 runes.
//
// It is small on purpose: almost everything in this application is local screen
// state. What lives here is what several screens need to see - session, camera
// list and health - and that would be wasteful to fetch again on every
// navigation.

import { api, messageOf } from './api.ts';
import type {
  EmailAlert,
  RegisteredCamera,
  Camera,
  AlertsDropped,
  FailingNotifier,
  Disco,
  Go2rtcDrift,
  Detector,
  BoundsMap,
  HealthCamera,
  Orphan,
  Person,
  Clock,
  Stream,
  Uptime,
} from './api.ts';
import { NOTICES_POLL_MS } from './notices.ts';

export const session = $state<{
  authRequired: boolean;
  authenticated: boolean;
  checked: boolean;
  pessoa: Person | null;
}>({
  authRequired: false,
  authenticated: false,
  checked: false,
  // Who is logged in: { username, name, role, owner }. With authentication off
  // it comes as admin and owner, with no user. Null without a session.
  pessoa: null,
});

// isAdmin decides the tabs, the bell and the pill. Hiding is not security - the
// API rejects the viewer with 403 -, it is just not showing what they cannot
// use.
export function isAdmin() {
  return session.pessoa?.role === 'admin';
}

// sessionDropped leads to the login screen: the session expired, the person was
// removed or got a new link. What finds out is a 401 from the API, or Live's
// check.
export function sessionDropped() {
  session.authenticated = false;
  session.pessoa = null;
  resetWhatWasLoaded();
}

export const cameras = $state<{
  list: RegisteredCamera[];
  streams: Stream[];
  orphans: Orphan[];
  padrao: Camera | null;
  faixas: BoundsMap | null;
  detector: boolean | null;
  go2rtcError: string | null;
  go2rtcConfigFile: Go2rtcDrift | null;
  alertEmail: EmailAlert | null;
  loading: boolean;
  error: string | null;
}>({
  list: [],
  streams: [],
  // Recordings left over from cameras already removed. They come along with the
  // listing because no other endpoint sees a camera with no registry entry.
  orphans: [],
  // Empty camera with the server's defaults, from which a new registration is
  // born.
  padrao: null,
  // { field: { min, max } } of the numeric fields: the same range the API
  // rejects with, so the inputs' min/max do not repeat numbers by hand. Without
  // a ceiling, max is not sent.
  faixas: null,
  // Whether the object detector is configured. Without it there is no
  // Detections tab. Null until the first response: "I don't know yet" is
  // different from "there is none".
  detector: null,
  go2rtcError: null,
  // What is in go2rtc.yaml and go2rtc has not loaded yet: { novas, alteradas,
  // foraDoArquivo }. Null when the two match, or when go2rtc does not let the
  // file be read.
  go2rtcConfigFile: null,
  // Where the alert goes, which the Alert tab shows. Null against a server
  // older than it.
  alertEmail: null,
  loading: true,
  error: null,
});

export const health = $state<HealthState>({
  cameras: [],
  disk: null,
  // { appSeconds, machineSeconds }. Stays null against an old server, which
  // does not answer the field - the status strip simply does not show up.
  uptime: null,
  clock: null,
  // { fila: { agora, cap, pico }, tempos: { analiseMs, esperaMs } }. Null
  // without a configured object detector.
  detector: null,
  // Hours of the recent reconnects window; the number lives on the server
  // (recorder.ReconnectWindow). Null against an old server.
  reconnectsWindowHours: null,
  // { error, since } while go2rtc does not answer dwnvr; null when it answers,
  // and also against an old server.
  go2rtc: null,
  // { error } with the smtp section rejected at boot; null with no problem.
  smtp: null,
  // { error?, warnings? } with a problem in usuarios.json; null with no
  // problem.
  users: null,
  // { total?, warnings?, dropped?, failing? }: the total after some camera
  // turns alerts on, the warnings with problems in the alerts section of
  // dwnvr.yaml, the alerts that did not go out through the notifiers and the
  // notifiers failing; null with none.
  alerts: null,
  updatedAt: 0,
  // When the read started failing, so the screen can say that what it shows is
  // stale; goes back to null on the first good read.
  failingSince: null,
});

// The /api/health as the screen stores it, null where the response did not
// bring the field, plus what the screen itself notes about the reads (updatedAt
// and failingSince).
export interface HealthState {
  cameras: HealthCamera[];
  disk: Disco | null;
  uptime: Uptime | null;
  clock: Clock | null;
  detector: Detector | null;
  reconnectsWindowHours: number | null;
  go2rtc: { error: string; since: string } | null;
  smtp: { error: string } | null;
  users: { error?: string; warnings?: string[] } | null;
  alerts: {
    total?: number;
    warnings?: string[];
    dropped?: AlertsDropped;
    failing?: FailingNotifier[];
  } | null;
  updatedAt: number;
  failingSince: number | null;
}

// Which code the server is running. It does not change while the page is open,
// so it is fetched only once, at boot.
export const build = $state({
  version: '',
  commit: '',
  date: '',
  // The tag of the latest release, when it is newer than the version in use;
  // empty in any other case. It is what lights up the notice in the header.
  newOne: '',
});

// New version notice. The one asking is the browser, straight to the GitHub API
// (open CORS, 60 requests per hour per IP): the server does not open outbound
// connections, gains no goroutine or config, and on a network without internet
// the query just fails silently.
const RELEASE_API = 'https://api.github.com/repos/mhagnumdw/dwnvr/releases/latest';
export const REPO_URL = 'https://github.com/mhagnumdw/dwnvr';
export const RELEASES_URL = `${REPO_URL}/releases`;
export const ISSUES_URL = `${REPO_URL}/issues`;
// The response is kept for this long, including the "no release yet": without
// that, every open tab would spend one request.
const RELEASE_CACHE = 'dwnvr.release';
const RELEASE_CACHE_MS = 12 * 60 * 60 * 1000;

export async function checkSession() {
  try {
    const s = await api.session();
    session.authRequired = s.authRequired;
    session.authenticated = s.authenticated;
    session.pessoa = s.user ?? null;
  } catch {
    session.authenticated = false;
  }
  session.checked = true;
}

export async function logout() {
  try {
    await api.logout();
  } catch {
    // Logging out is, first of all, local: if the request fails, dropping the
    // session here is still what the user asked for - and the signed cookie
    // expires by itself.
  }
  session.authenticated = false;
  session.pessoa = null;
  resetWhatWasLoaded();
}

// resetWhatWasLoaded applies to both exits, the requested one and the dropped
// session's: without it, whoever logs in next in the same tab sees, for a
// moment, the cameras and diagnostics of the previous session.
function resetWhatWasLoaded() {
  cameras.list = [];
  cameras.streams = [];
  cameras.orphans = [];
  cameras.detector = null;
  cameras.go2rtcError = null;
  cameras.go2rtcConfigFile = null;
  cameras.alertEmail = null;
  cameras.loading = true;
  cameras.error = null;
  health.cameras = [];
  health.disk = null;
  health.uptime = null;
  health.clock = null;
  health.detector = null;
  // The header bell reads the notices from here, and the viewer does not reread
  // health: without clearing, they would see the notices of the admin who left.
  health.reconnectsWindowHours = null;
  health.go2rtc = null;
  health.smtp = null;
  health.users = null;
  health.alerts = null;
  health.updatedAt = 0;
  health.failingSince = null;
  healthRequestedAt = 0;
}

export async function loadBuild() {
  try {
    const b = await api.version();
    build.version = b.version;
    build.commit = b.commit;
    build.date = b.date;
  } catch {
    // Version is informational: an old server, without the endpoint, remains
    // usable - the interface just shows nothing.
  }
}

// [major, minor, patch] of 'v0.1.0', and also of 'v0.1.0-3-gabc1234', which is
// a main build after v0.1.0. Null for 'dev' or a bare hash.
function semver(v: string | undefined): number[] | null {
  const m = /^v?(\d+)\.(\d+)\.(\d+)/.exec(v ?? '');
  return m ? m.slice(1).map(Number) : null;
}

// Numeric comparison: as text, 'v0.1.10' would come before 'v0.1.9'.
function newer(a: number[], b: number[]): boolean {
  for (let i = 0; i < 3; i++) {
    const x = a[i] ?? 0;
    const y = b[i] ?? 0;
    if (x !== y) return x > y;
  }
  return false;
}

// The stored tag, or undefined if there is nothing valid within the deadline.
function lerCacheRelease(): string | undefined {
  try {
    const c = JSON.parse(localStorage.getItem(RELEASE_CACHE) ?? 'null') as {
      tag?: unknown;
      em?: unknown;
    } | null;
    if (
      c &&
      typeof c.tag === 'string' &&
      typeof c.em === 'number' &&
      Date.now() - c.em < RELEASE_CACHE_MS
    ) {
      return c.tag;
    }
  } catch {
    // localStorage blocked or content corrupted: counts as empty.
  }
  return undefined;
}

function writeReleaseCache(tag: string) {
  try {
    localStorage.setItem(RELEASE_CACHE, JSON.stringify({ tag, em: Date.now() }));
  } catch {
    // With nowhere to store it, the next open asks again. That is all.
  }
}

// Depends on loadBuild(): without knowing the version in use there is nothing
// to compare. A development build or one with only a hash queries nothing.
export async function checkNewVersion() {
  const current = semver(build.version);
  if (!current) return;
  let tag = lerCacheRelease();
  if (tag === undefined) {
    try {
      const r = await fetch(RELEASE_API);
      // 404 is "no release yet": a valid response, which goes into the cache.
      if (r.ok) tag = ((await r.json()) as { tag_name?: string }).tag_name ?? '';
      else if (r.status === 404) tag = '';
    } catch {
      // No internet, or GitHub is down: left for the next open.
    }
    // Rate limit exceeded and the like also stay out of the cache.
    if (tag === undefined) return;
    writeReleaseCache(tag);
  }
  const lastOne = semver(tag);
  if (lastOne && newer(lastOne, current)) build.newOne = tag;
}

// Numbers the reads of the list, like those of health: the app's opening and
// the Cameras screen's may ask together, and an old response arriving later
// would undo the new one - an old error would light up the notice on top of the
// list that just arrived. The newest response over the last one applied wins.
let requestedCameras = 0;
let appliedCameras = 0;

// loadCameras stores the error instead of throwing it. The error only goes out
// with a good read, and not at the start of the request: this way the notice
// does not blink while the refresh button spins.
export async function loadCameras() {
  const ticket = ++requestedCameras;
  cameras.loading = true;
  try {
    const data = await api.cameras();
    if (ticket < appliedCameras) return;
    appliedCameras = ticket;
    cameras.error = null;
    cameras.list = data.cameras;
    cameras.streams = data.streams ?? [];
    cameras.orphans = data.orphans ?? [];
    cameras.padrao = data.padrao ?? null;
    cameras.faixas = data.faixas ?? null;
    cameras.detector = data.detector;
    cameras.go2rtcError = data.go2rtcError ?? null;
    cameras.go2rtcConfigFile = data.go2rtcConfigFile ?? null;
    cameras.alertEmail = data.alertEmail ?? null;
  } catch (e) {
    if (ticket < appliedCameras) return;
    appliedCameras = ticket;
    cameras.error = messageOf(e);
    // With no response at all, the tab is left out: better than a blank screen
    // waiting forever.
    cameras.detector ??= false;
  } finally {
    // Only the most recent request ends "carregando…": with another in
    // progress, the screen does not yet know what to show.
    if (ticket === requestedCameras) cameras.loading = false;
  }
}

// When the last health read started, from any screen. It is what the header
// bell checks so it does not repeat a recent read, or one in progress.
let healthRequestedAt = 0;

// Numbers the reads so an old response does not undo a new one: the poll and a
// reread requested by the screen may cross, and the old one arriving later
// would erase the new data - or light up "sem resposta" with the server up. The
// newest response over the last one applied wins, and not just the last
// request's: with a slow server, each read taking longer than HEALTH_POLL_MS,
// there would always be a newer request in progress and no response would ever
// get in.
let healthRequested = 0;
let healthApplied = 0;

// How many health reads are in progress. The automatic reads (the poll, the
// bell and the return to the tab) do not start with another pending: with a
// slow server, each tick would add one more request identical to the one it has
// not answered yet. The reread the screen asks for after an action goes out
// anyway, because it wants the post-action state, and the pending one may have
// started before. That is why it is a counter, and not a yes or no: both may be
// in progress together.
let healthReading = 0;

// readHealthIfFree is the automatic read: it only goes out with no other in
// progress.
function readHealthIfFree() {
  if (healthReading === 0) void loadHealth();
}

export async function loadHealth() {
  healthRequestedAt = Date.now();
  const ticket = ++healthRequested;
  healthReading++;
  try {
    const data = await api.health(HEALTH_TIMEOUT_MS);
    if (ticket < healthApplied) return;
    healthApplied = ticket;
    health.cameras = data.cameras;
    health.disk = data.disk ?? null;
    health.uptime = data.uptime ?? null;
    health.clock = data.clock ?? null;
    health.detector = data.detector ?? null;
    health.reconnectsWindowHours = data.reconnectsWindowHours ?? null;
    health.go2rtc = data.go2rtc ?? null;
    health.smtp = data.smtp ?? null;
    health.users = data.users ?? null;
    health.alerts = data.alerts ?? null;
    health.updatedAt = Date.now();
    health.failingSince = null;
  } catch {
    // Health is informational: failing here must not interrupt the use of the
    // screens. It only records since when it fails, so the footer can warn that
    // the data is stale.
    if (ticket < healthApplied) return;
    healthApplied = ticket;
    health.failingSince ??= Date.now();
  } finally {
    healthReading--;
  }
}

// HEALTH_POLL_MS is the only place that decides how often health is reread. All
// the screens that show status use the same rhythm, and the text that announces
// this to the user is derived from here - changing the number here is enough.
export const HEALTH_POLL_MS = 5000;

// HEALTH_TIMEOUT_MS is the deadline of a health read: past it, it counts as a
// failure and lights up the footer's "sem resposta". Two poll intervals: on the
// busy board (cameras recording and the object detector running) the read took
// less than 250 ms, so only a hung server gets here.
export const HEALTH_TIMEOUT_MS = 2 * HEALTH_POLL_MS;

// pollHealth keeps the diagnostics alive while the screen is open, and stops
// when it leaves - there is no point querying the server in the background
// forever. For the same reason, it does not read with the tab hidden: a tab
// forgotten open on Cameras or Diagnostics would ask every intervalMs, day and
// night, and would also keep the admin showing "acesso agora" on the Users
// screen. On return, it reads right away if the data is stale; if the bell just
// asked for the read in the same event, it does not repeat. With a read in
// progress, the tick is skipped: the next one goes out when it returns or hits
// HEALTH_TIMEOUT_MS.
export function pollHealth(intervalMs = HEALTH_POLL_MS) {
  const ler = () => {
    if (!document.hidden) readHealthIfFree();
  };
  const onReturn = () => {
    if (!document.hidden && Date.now() - healthRequestedAt >= intervalMs) readHealthIfFree();
  };
  ler();
  const id = setInterval(ler, intervalMs);
  document.addEventListener('visibilitychange', onReturn);
  return () => {
    clearInterval(id);
    document.removeEventListener('visibilitychange', onReturn);
  };
}

// watchNotices keeps the header bell up to date on the screens that do not
// reread health on their own. It only reads when the last read, by whoever it
// was, is older than NOTICES_POLL_MS: on Diagnostics and Cameras, which reread
// every HEALTH_POLL_MS, no request goes out from here. The tick is short so
// that, when leaving one of those screens, the data does not go almost twice
// the deadline without being reread; the tick itself costs nothing. With the
// tab hidden it does not read, and on return it reads right away if the data is
// stale. Like pollHealth, it does not start a read with another in progress.
export function watchNotices() {
  const maybe = () => {
    if (!document.hidden && Date.now() - healthRequestedAt >= NOTICES_POLL_MS) readHealthIfFree();
  };
  maybe();
  const id = setInterval(maybe, HEALTH_POLL_MS);
  document.addEventListener('visibilitychange', maybe);
  return () => {
    clearInterval(id);
    document.removeEventListener('visibilitychange', maybe);
  };
}
