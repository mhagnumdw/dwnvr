// dwnvr API client.
//
// Everything goes through here so that a 401 has a single place to be handled:
// the session expires at some point, and without this every screen would have
// to remember to deal with that on its own.
//
// The types at the end of the file are the contract: the JSON of each response,
// as the Go structs in internal/api write it and docs/api.md describes it. A
// field with `omitempty` in Go is optional here; TypeScript does not check what
// arrives over the network, so the type is the server's promise, and changing
// one side means changing the other.

import { areaInQuery } from './area.ts';
import type { FilterArea, Point, Rule } from './area.ts';
import type { FamilyName } from './icons.ts';

let onUnauthorized = () => {
  // nobody has asked for the notice yet: App registers its own on mount
};

export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

async function request<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch('api/' + path, opts);
  await check(res);
  return (await res.json()) as T;
}

// media downloads a chunk of a recording for the player. It goes through the
// same check() as request(): the body of an error must not reach the
// SourceBuffer as video.
export async function media(url: string, signal: AbortSignal) {
  const res = await fetch(url, { signal });
  await check(res);
  return res.arrayBuffer();
}

// check throws the error of a response before anyone reads the body. The
// message is the one from the API's JSON; when the body is something else - the
// text from http.ServeFile, the HTML of a proxy while dwnvr restarts - the
// status is used, because statusText is empty on HTTP/2 and an empty message
// vanishes from the screen.
async function check(res: Response) {
  if (res.status === 401) {
    onUnauthorized();
    throw new ApiError('sessão expirada', 401);
  }
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: string; fields?: FieldErrors };
    throw new ApiError(body.error ?? `o servidor respondeu ${res.status}`, res.status, body.fields);
  }
}

// json reads the response of a fetch made on the side, without request(): the
// public one and the one with a deadline.
async function json<T>(res: Promise<Response>): Promise<T> {
  return (await (await res).json()) as T;
}

export class ApiError extends Error {
  readonly status: number;
  // The per-field rejections of a form, when the API sends them (create and
  // change email in the Users screen). Without them, an empty object.
  readonly fields: FieldErrors;

  constructor(message: string, status: number, fields: FieldErrors = {}) {
    super(message);
    this.status = status;
    this.fields = fields;
  }
}

// TooManyAttempts is the login 429: the address went over the attempts limit,
// and the next one only gets through in `seconds` (the Retry-After).
export class TooManyAttempts extends ApiError {
  readonly seconds: number;

  constructor(seconds: number) {
    super('muitas tentativas', 429);
    // Without the header, or with garbage in it, wait the minimum: the server
    // rejects again if it is still too early, and sends the right number.
    this.seconds = Number.isFinite(seconds) && seconds > 0 ? Math.ceil(seconds) : 1;
  }
}

// messageOf extracts the text of any error, for the screen: TypeScript's
// `catch` receives `unknown`, because in JavaScript you can throw anything.
export function messageOf(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

export const api = {
  session: () => json<Session>(fetch('api/session')),

  // Like session, it does not go through request(): it is public and has to
  // work on the login screen, where there is no session yet to expire.
  version: () => json<Version>(fetch('api/version')),

  // Only to find out whether the server is reachable: the body does not matter,
  // and any response, even an HTTP error, counts. See `waitNetwork` in Live.
  ping: (ms: number) =>
    fetch('api/version', { cache: 'no-store', signal: AbortSignal.timeout(ms) }),

  login: async (username: string, password: string) => {
    const res = await fetch('api/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    });
    if (res.status === 429) throw new TooManyAttempts(Number(res.headers.get('Retry-After')));
    if (!res.ok) throw new ApiError('usuário ou senha inválidos', res.status);
    return (await res.json()) as WithPerson;
  },

  logout: () => fetch('api/logout', { method: 'POST' }),

  // Live's check: the same /api/session, with a deadline. The whole response
  // comes back, because the server's clock is in its `Date` header.
  checkSession: (ms: number) =>
    fetch('api/session', { cache: 'no-store', signal: AbortSignal.timeout(ms) }),

  // Users, admin only. Create and the new link return the link's token, which
  // only exists in that response.
  users: () => request<Users>('users'),
  createUser: (user: string, name: string, email: string) =>
    request<Invite>('users', {
      method: 'POST',
      body: JSON.stringify({ username: user, name, email }),
    }),
  // What createUser would reject with this data, field by field, without
  // creating anyone. Empty means "all good".
  checkNew: (user: string, name: string, email: string) =>
    request<{ fields: FieldErrors }>('users/check', {
      method: 'POST',
      body: JSON.stringify({ username: user, name, email }),
    }),
  // An empty value clears the e-mail.
  changeEmail: (user: string, email: string) =>
    request<{ user: User }>('users/email', {
      method: 'POST',
      body: JSON.stringify({ username: user, email }),
    }),
  // What changeEmail would reject with this email, without changing anything.
  checkEmail: (user: string, email: string) =>
    request<{ fields: FieldErrors }>('users/email/check', {
      method: 'POST',
      body: JSON.stringify({ username: user, email }),
    }),
  // The link goes whole, as the screen shows it: the server does not know which
  // address it is accessed by, and only checks that it is the person's open
  // link.
  sendLink: (user: string, url: string) =>
    request<{ sentTo: string }>('users/link/email', {
      method: 'POST',
      body: JSON.stringify({ username: user, url }),
    }),
  newLink: (user: string) =>
    request<Invite>('users/link', {
      method: 'POST',
      body: JSON.stringify({ username: user }),
    }),
  removeUser: (user: string) =>
    request<unknown>('users?username=' + encodeURIComponent(user), { method: 'DELETE' }),
  removeAvatarOf: (user: string) =>
    request<unknown>('users/avatar?username=' + encodeURIComponent(user), {
      method: 'DELETE',
    }),

  // My account: each person changes what is theirs. Everything returns { user }
  // as it ended up, so the header updates without asking again. The photo goes
  // raw, the JPEG already shrunk by the browser (see lib/avatar.ts).
  account: () => request<Account>('account'),
  changePassword: (current: string, newOne: string) =>
    request<WithPerson>('account/password', {
      method: 'POST',
      body: JSON.stringify({ currentPassword: current, newPassword: newOne }),
    }),
  changeName: (name: string) =>
    request<WithPerson>('account/name', { method: 'POST', body: JSON.stringify({ name }) }),
  uploadAvatar: (jpeg: Blob) =>
    request<WithPerson>('account/avatar', {
      method: 'PUT',
      headers: { 'Content-Type': 'image/jpeg' },
      body: jpeg,
    }),
  removeAvatar: () => request<WithPerson>('account/avatar', { method: 'DELETE' }),

  // The invite link: public, with the token in the body and not in the URL, so
  // it does not end up in any proxy's log. An expired or already used link is
  // 410.
  checkInvite: (token: string) =>
    request<InviteLink>('invite/check', { method: 'POST', body: JSON.stringify({ token }) }),
  setPassword: (token: string, password: string) =>
    request<WithPerson>('invite', {
      method: 'POST',
      body: JSON.stringify({ token, password }),
    }),

  cameras: () => request<CamerasResponse>('cameras'),
  // Returns the camera as it ended up, with the ids of the new areas.
  saveCamera: (cam: Camera, signal: AbortSignal | null = null) =>
    request<{ camera: Camera }>('cameras', { method: 'POST', body: JSON.stringify(cam), signal }),
  // The recordings only go along if requested: the default is to preserve them.
  deleteCamera: (id: string, { recordings = false } = {}) =>
    request<unknown>(`cameras?id=${encodeURIComponent(id)}${recordings ? '&recordings=1' : ''}`, {
      method: 'DELETE',
    }),

  // Serves both a registered camera and one already removed - in the second
  // case it is the only way to reach the material, which vanishes from the rest
  // of the API.
  deleteRecordings: (cam: string) =>
    request<unknown>('rec?cam=' + encodeURIComponent(cam), { method: 'DELETE' }),

  // Finds out whether a stream delivers audio. It is a separate request, and
  // not a field of cameras(), because on an idle stream the response costs a
  // connection of a few seconds to the camera - see internal/api/probe.go.
  probeStream: (name: string) => request<Probe>('streams/probe?src=' + encodeURIComponent(name)),

  // Makes go2rtc reread go2rtc.yaml. All cameras stop for a few seconds.
  restartGo2rtc: () => request<unknown>('go2rtc/restart', { method: 'POST' }),

  // With a deadline: a request that never returns would hold up the health
  // poll, which does not start one read while another is in progress. When it
  // times out, it becomes a failure like the others.
  health: (ms: number) => request<Health>('health', { signal: AbortSignal.timeout(ms) }),

  // Restarts the reconnect count of all cameras. It does not drop any
  // connection.
  resetReconnects: () => request<unknown>('reconnects/reset', { method: 'POST' }),

  // The machine that records: temperature, memory, pressure, storage, go2rtc
  // and the latest log warnings. Separate from health because it costs a write
  // test on the disk - only requested by whoever opened the "Este servidor"
  // card.
  server: () => request<Server>('health/server'),

  // A test email through the smtp section. The email server's error comes back
  // as it answered.
  testEmail: (dest: string) =>
    request<{ sentTo: string }>('email/test', {
      method: 'POST',
      body: JSON.stringify({ to: dest }),
    }),

  days: (cam: string) => request<Days>('rec/days?cam=' + encodeURIComponent(cam)),

  timeline: (cam: string, day: string) =>
    request<Timeline>(`rec/timeline?cam=${encodeURIComponent(cam)}&day=${day}`),

  // The same timeline, but for a piece of the day. Today is reread in a cycle
  // while the screen is open, and a day full of 30s segments is ~70 KB
  // uncompressed - fetching only the tail swaps that for a few hundred bytes
  // per round.
  timelineRange: (cam: string, from: number, to: number) =>
    request<Timeline>(`rec/timeline?cam=${encodeURIComponent(cam)}&from=${from}&to=${to}`),

  // The day's motion marks - what the heat strip draws. A camera with detection
  // off answers an empty list, and the strip does not even show.
  events: (cam: string, day: string) =>
    request<Events>(`rec/events?cam=${encodeURIComponent(cam)}&day=${day}`),

  // The tail of the marks, for the same reason as timelineRange: on today the
  // screen asks again in a cycle, and only wants what it does not have yet.
  eventsRange: (cam: string, from: number, to: number) =>
    request<Events>(`rec/events?cam=${encodeURIComponent(cam)}&from=${from}&to=${to}`),

  // A page of the object detections of all cameras, from newest to oldest.
  // `before` and `after` are the cursor (one or none); null `cams` and
  // `families` mean "all".
  detections: ({ before, after, limit, ...filter }: DetectionsRequest) => {
    const q = filterQuery(filter);
    if (before != null) q.set('before', String(before));
    if (after != null) q.set('after', String(after));
    if (limit) q.set('limit', String(limit));
    return request<DetectionsPage>(`detections?${q}`);
  },

  // The days with at least one detection of the filter, in ascending order, and
  // how many each one has: it is what the Detections screen calendar lets you
  // pick, and the number it shows on each day.
  detectionDays: (filter: DetectionsFilter) =>
    request<DetectionDays>(`detections/days?${filterQuery(filter)}`),
};

// The filter that the Detections list and calendar share. Areas restrict only
// their own camera; the others in `cams` stay whole.
function filterQuery({ cams, families, area, areas, onlyAlerts }: DetectionsFilter) {
  const q = new URLSearchParams();
  if (cams?.length) q.set('cams', cams.join(','));
  if (families?.length) q.set('families', families.join(','));
  if (area) {
    q.set('area', areaInQuery(area));
    q.set('rule', area.rule);
  }
  if (areas?.length) q.set('areas', areas.join(','));
  if (onlyAlerts) q.set('alert', '1');
  return q;
}

// Media URLs are built, not fetched: they go straight into a <video>, an <img>
// or the SourceBuffer.
export const mediaURL = {
  init: (cam: string, gen: string) => `api/rec/init?cam=${encodeURIComponent(cam)}&g=${gen}`,
  segment: (cam: string, t: number) => `api/rec/seg?cam=${encodeURIComponent(cam)}&t=${t}`,
  thumb: (cam: string, t: number) => `api/rec/thumb?cam=${encodeURIComponent(cam)}&t=${t}`,
  // Someone's photo, by id, which changes at every swap: the browser keeps it
  // forever.
  avatar: (id: string) => `api/avatar?id=${encodeURIComponent(id)}`,
  // The JPEG the object detector looked at. `t` is the detection's
  // `atMs`.
  frame: (cam: string, t: number) => `api/detections/frame?cam=${encodeURIComponent(cam)}&t=${t}`,
  export: (cam: string, from: number, to: number) =>
    `api/rec/export?cam=${encodeURIComponent(cam)}&from=${from}&to=${to}`,
  // Live goes to go2rtc through dwnvr's proxy, so that its credential never
  // reaches the browser.
  liveWS: (cam: string) => `${baseWS()}api/live/ws?src=${encodeURIComponent(cam)}`,
  // The session notice: a mute websocket that the server closes when the person
  // is removed or gets a new link. See the session notice in Live
  // (`wsSessionNotice`).
  wsSessionNotice: () => `${baseWS()}api/session/ws`,
};

function baseWS() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const base = location.pathname.replace(/[^/]*$/, '');
  return `${proto}//${location.host}${base}`;
}

// --- the contract
// -------------------------------------------------------------
//
// An instant in milliseconds is `number`; an instant as text (RFC 3339, in the
// server's timezone) is `string`, and only exists where the screen wants the
// time over there.

// --- session and people

export type Role = 'admin' | 'viewer';

// Who is logged in. With authentication off it comes as admin and owner, with
// no user or name. `avatar` is the photo's id, and only comes with a photo.
export interface Person {
  username?: string;
  name?: string;
  role: Role;
  owner: boolean;
  avatar?: string;
}

export interface Session {
  authRequired: boolean;
  authenticated: boolean;
  user?: Person;
}

// The login, the password via invite and the My account changes.
export interface WithPerson {
  ok: true;
  user: Person;
}

export interface Version {
  version: string;
  commit: string;
  date: string;
  go: string;
}

export type UserStatus = 'active' | 'invitePending' | 'inviteExpired';

export interface User {
  username: string;
  name: string;
  email?: string;
  role: Role;
  status: UserStatus;
  avatar?: string;
  inviteExpiresAtMs?: number;
  createdAtMs?: number;
  activeSinceMs?: number;
  // Only for those who have used dwnvr since the field existed.
  lastAccessMs?: number;
}

// Without authentication on there is no registry: it comes without `owner` and
// with an empty list.
export interface Users {
  authRequired: boolean;
  owner?: { username: string; name: string; avatar: string; lastAccessMs?: number };
  users: User[];
  inviteValidityMs: number;
  maxNameLength: number;
  maxUsernameLength: number;
  maxEmailLength: number;
  // Whether the smtp section is on. A server older than it does not send it.
  emailEnabled?: boolean;
}

// The fields of the person forms, with the name they have in the JSON.
export type PersonField = 'username' | 'name' | 'email';

// The rejections of a form, field by field: only the rejected fields, with the
// message to put under each one.
export type FieldErrors = Partial<Record<PersonField, string>>;

// The token only exists in this response: the screen builds the link with it.
export interface Invite {
  user: User;
  token: string;
}

export interface InviteLink {
  username: string;
  name: string;
  inviteExpiresAtMs: number;
  minPasswordLength: number;
  maxPasswordLength: number;
}

export interface Account {
  user: Person;
  minPasswordLength: number;
  maxPasswordLength: number;
  maxNameLength: number;
  avatar: AvatarRule;
}

export interface AvatarRule {
  sidePx: number;
  maxBytes: number;
  quality: number;
}

// --- cameras

// A camera as cameras.json stores it. The optional fields come resolved by the
// defaults of dwnvr.yaml; absent means "use the default".
export interface Camera {
  id: string;
  name: string;
  enabled: boolean;
  segmentSeconds?: number;
  quotaMB?: number;
  maxDays?: number;
  audio?: string;
  stallSeconds?: number;
  detect?: boolean;
  detectMethod?: string;
  detectSensitivity?: number;
  areas?: CameraArea[];
  // Only the admin receives it: the viewer does not edit cameras.
  alert?: CameraAlert;
}

// When a camera detection becomes an alert. Without `areas`, the whole image
// counts; without `schedules`, any time; when off, it keeps the rest so it can
// be turned on again.
export interface CameraAlert {
  enabled: boolean;
  families?: FamilyName[];
  areas?: string[];
  schedules?: AlertSchedule[];
}

// An alert schedule: the days, from 0 (Sunday) to 6 (Saturday), and the "De"
// and "Até" in "HH:MM", with both ends included. `to` is always greater than
// `from`: a night is two schedules, one until 23:59 and another from 00:00.
export interface AlertSchedule {
  days: number[];
  from: string;
  to: string;
}

// Where the alert applies, as the Alert tab shows it. It does not go to the
// server, which only stores the areas: "only in the checked areas" with none
// checked is a screen state, which the server would read as the whole image.
export type AlertWhere = 'all' | 'areas';

// An area registered on the camera. A new area from the Areas tab already goes
// with an `id`, so the alert can mark it; the one from the Detections "salvar
// na câmera" goes without, and the server assigns one.
export interface CameraArea {
  id?: string;
  name: string;
  rule?: Rule;
  points: Point[];
}

// The camera in the listing: for the admin, with the recordings folder.
export interface RegisteredCamera extends Camera {
  dir?: string;
}

export interface Stream {
  name: string;
  registered: boolean;
  hasAudio: boolean;
  audioCodecs?: string[];
  transcoding: boolean;
}

// Recordings of a camera that has already been removed.
export interface Orphan {
  id: string;
  bytes: number;
  days: number;
  firstMs: number;
  lastMs: number;
}

// Without a ceiling, `max` is not sent.
export interface Bounds {
  min: number;
  max?: number;
}

// The camera fields with a range: the CameraRanges of
// internal/config/params.go.
export type FieldWithBounds =
  'quotaMB' | 'segmentSeconds' | 'maxDays' | 'stallSeconds' | 'detectSensitivity';
export type BoundsMap = Partial<Record<FieldWithBounds, Bounds>>;

// What is in go2rtc.yaml and go2rtc has not loaded yet. An empty list is not
// sent.
export interface Go2rtcDrift {
  added?: string[];
  changed?: string[];
  notInFile?: string[];
}

// The viewer receives only `cameras` and `detector`; the rest is the admin's,
// and `streams` also disappears when go2rtc does not answer.
export interface CamerasResponse {
  cameras: RegisteredCamera[];
  detector: boolean;
  defaults?: Camera;
  ranges?: BoundsMap;
  orphans?: Orphan[];
  streams?: Stream[];
  go2rtcError?: string;
  go2rtcConfigFile?: Go2rtcDrift;
  alertEmail?: EmailAlert;
}

// The "Para onde vai" of the Alert tab: `to` with the email notifier, and
// `failingSince` and `error` while it is failing, until the first send that
// works. Without the notifier, `missing` lists the dwnvr.yaml sections that are
// missing, and `smtpError` the reason the smtp section was rejected.
export interface EmailAlert {
  to?: string[];
  failingSince?: string;
  error?: string;
  missing?: ('alerts.email.to' | 'smtp')[];
  smtpError?: string;
}

// `error` comes when the probe failed: then `hasAudio` is "don't know", not
// "doesn't have".
export interface Probe {
  name: string;
  hasAudio: boolean;
  audioCodecs?: string[];
  probed?: boolean;
  error?: string;
}

// --- health

// The slices add up to the camera's `onsets`. `clips` is not a slice: it is
// how many onsets had the video cut and offered to the queue.
export interface Funnel {
  clips: number;
  noVideo: number;
  dropped: number;
  failed: number;
  rejected: number;
  noObject: number;
  withObject: number;
  queued: number;
}

// A camera from /api/health. A disabled camera comes without `audio`.
export interface HealthCamera {
  id: string;
  name: string;
  enabled: boolean;
  connected: boolean;
  connectedAt?: string;
  bitrateKbps: number;
  bytes: number;
  segments: number;
  reconnects: number;
  lastError?: string;
  videoCodec?: string;
  hasAudio: boolean;
  gen?: string;
  audio?: string;
  transcoding: boolean;
  disconnectedAt?: string;
  reconnectsSince: string;
  reconnectsInWindow: number;
  lastReconnectAt?: string;
  width?: number;
  height?: number;
  lastSegmentAt?: string;
  silent: boolean;
  oldestSegmentAt?: string;
  recordedMs?: number;
  detect: boolean;
  onsets: number;
  funnel?: Funnel;
  quotaMB: number;
  diskBytes: number;
  retainDays: number;
}

export interface Disco {
  freeBytes: number;
  totalBytes: number;
  dwnvrBytes: number;
  minFreeMB: number;
  belowMin: boolean;
  orphanBytes?: number;
  belowMinSince?: string;
}

export interface Uptime {
  appSeconds: number;
  machineSeconds?: number;
}

export interface Clock {
  now: string;
  abbr: string;
  offsetSeconds: number;
  zone?: string;
}

// The object detector queue, a single one for all cameras. `cameras` carries
// only the cameras with something waiting or being inferred.
export interface Detector {
  queue: {
    length: number;
    cap: number;
    peak: number;
    perCamera: number;
    cameras: Record<string, { waiting: number; busy: boolean }>;
  };
  times: { inferenceMs: number; waitMs: number };
  outage?: { since: string; error: string };
}

// `disk` disappears when storage does not answer; `detector`, without a
// configured object detector; `go2rtc`, while it answers; `alerts`, before any
// camera turns alerts on, if the alerts section has no problem. `uptime`,
// `clock` and `reconnectsWindowHours` are missing on a server older than them,
// which the `npm run dev` screen may be pointed at.
// The alerts that did not go out through the notifiers, in an episode that ends
// one hour after the last drop: `queueFull` did not fit in the send queue, and
// `overLimit` went over the `limitPerHour` ceiling.
export interface AlertsDropped {
  queueFull: number;
  overLimit: number;
  limitPerHour: number;
  since: string;
  lastAt: string;
}

// The alert notifier by the name the server gives it, as it reads in the
// sentence: "Alertas por e-mail". A notifier the screen does not know yet goes
// out by its own name.
export const ALERT_NOTIFIERS: Readonly<Record<string, string>> = { email: 'por e-mail' };
export const alertNotifier = (name: string) => ALERT_NOTIFIERS[name] ?? `por ${name}`;

// An alert notifier failing since `since`, with the error of the most recent
// failure. It disappears on the first send that works.
export interface FailingNotifier {
  name: string;
  since: string;
  error: string;
}

export interface Health {
  cameras: HealthCamera[];
  disk?: Disco;
  uptime?: Uptime;
  detector?: Detector;
  clock?: Clock;
  reconnectsWindowHours?: number;
  go2rtc?: { error: string; since: string };
  // Only with the smtp section rejected at boot: email stays off.
  smtp?: { error: string };
  // Only with a problem in users.json: `error` is the file unreadable at
  // boot, and `warnings` the entries that are not valid.
  users?: { error?: string; warnings?: string[] };
  // `total` disappears before any camera turns alerts on; `warnings` are the
  // problems in the alerts section of dwnvr.yaml, which apply even with no
  // camera; `dropped` only comes during a drop episode, and `failing` with some
  // notifier failing.
  alerts?: {
    total?: number;
    warnings?: string[];
    dropped?: AlertsDropped;
    failing?: FailingNotifier[];
  };
}

// --- the server (the "Este servidor" card of Diagnostics)
//
// Each field disappears when the source does not exist: kernel without PSI,
// system without cgroup, outside Linux.

export interface PressureAverages {
  avg10: number;
  avg60: number;
  avg300: number;
}

export interface Pressure {
  some?: PressureAverages;
  full?: PressureAverages;
}

export type PressureResource = 'cpu' | 'memory' | 'io';

export interface ServerProcess {
  cpuMs?: number;
  aliveMs: number;
  memoryBytes?: number;
  goroutines: number;
  openFiles?: number;
  fileLimit?: number;
  uid?: number;
  gid?: number;
  go: string;
  architecture: string;
}

export interface LogLine {
  at: string;
  level: string;
  text: string;
}

// `collectedAt` and `process` are optional because a server older than them
// does not send them: the `npm run dev` screen may be pointed at one.
export interface Server {
  collectedAt?: { at: string; abbreviation: string; offsetSeconds: number; zone?: string };
  machine: {
    board?: string;
    processor?: string;
    kernel?: string;
    cores: number;
    allowedCores?: number;
    load?: number[];
    sensors?: { name: string; celsius: number }[];
    mhz?: number;
    mhzMax?: number;
    governor?: string;
  };
  process?: ServerProcess;
  memory?: {
    totalBytes: number;
    availableBytes: number;
    swapTotalBytes: number;
    swapUsedBytes: number;
    limitBytes?: number;
    oomKills?: number;
  };
  pressure?: Partial<Record<PressureResource, Pressure>>;
  storage: {
    path: string;
    type?: string;
    origin?: string;
    options?: string;
    readOnly: boolean;
    write: { ok: boolean; ms: number; error?: string };
  };
  go2rtc: { ok: boolean; ms: number; version?: string; error?: string; url?: string };
  detector?: {
    ok: boolean;
    ms: number;
    model?: string;
    inputSize?: string;
    threads?: number;
    error?: string;
    url: string;
  };
  // Only with the smtp section in dwnvr.yaml; without the login and the
  // password. `error` is the reason email is off.
  smtp?: { host?: string; port?: number; security?: string; from?: string; error?: string };
  // Only with an alert notifier and some camera with alerts on. `last`
  // disappears before the first send, and `error` when the last send worked.
  alerts?: AlertNotifier[];
  log?: { total: number; lines: LogLine[] };
}

export interface AlertNotifier {
  name: string;
  sent: number;
  failures: number;
  last?: string;
  error?: string;
}

// --- recordings

export interface DaySummary {
  day: string;
  count: number;
  bytes: number;
  firstMs: number;
  lastMs: number;
  durMs: number;
}

export interface Days {
  cam: string;
  days: DaySummary[];
}

// [start, end] of a contiguous stretch, to draw the bar.
export type Span = [startMs: number, endMs: number];

// [start, duration, index into `gens`] of a segment, to play.
export type Segment = [startMs: number, durationMs: number, gen: number];

// Compact on purpose: a day with 1-minute segments is 1,440 entries.
export interface Timeline {
  cam: string;
  from: number;
  to: number;
  ranges: Span[];
  gens: string[];
  segments: Segment[];
}

// X1, Y1, X2, Y2 as a fraction of the image, from 0 to 1.
export type Box = [x1: number, y1: number, x2: number, y2: number];

// An object mark. `frameMs` and `box` say where to draw the object over the
// video; a mark without both gets no box.
export interface MarkedObject {
  atMs: number;
  family: string;
  class: string;
  score: number;
  frameMs?: number;
  box?: Box;
}

export interface Events {
  cam: string;
  from: number;
  to: number;
  onsets: number[];
  objects: MarkedObject[];
}

// --- detections

export interface ObjectInFrame {
  family: string;
  class: string;
  score: number;
  box?: Box;
  // The object passed the camera's alert rule, at the time it was inferred, and
  // fell into the alert areas with these ids. Without areas in the alert,
  // `alertAreas` is not sent. The id of an area deleted since then keeps
  // coming.
  alert?: boolean;
  alertAreas?: string[];
}

// An inference of the detector on a camera: the onset instant and everything it
// found in that frame.
export interface Detection {
  cam: string;
  atMs: number;
  frameMs?: number;
  hasFrame: boolean;
  objects: ObjectInFrame[];
}

export interface DetectionsPage {
  detections: Detection[];
  end: boolean;
}

// Null or absent means "all".
export interface DetectionsFilter {
  cams?: readonly string[] | null;
  families?: readonly string[] | null;
  // On this area's camera, only the objects inside it. The other cameras do not
  // change.
  area?: FilterArea | null;
  // The same, with the registered areas: `<cam>/<id>`. Those of one camera add
  // up, the `area` too: any one will do.
  areas?: readonly string[] | null;
  // Only the detections where some object that stays raised an alert; they come
  // whole, including the object that did not alert.
  onlyAlerts?: boolean;
}

// `before` and `after` are the cursor: one or none.
export interface DetectionsRequest extends DetectionsFilter {
  before?: number | null;
  after?: number | null;
  limit?: number;
}

export interface DetectionDays {
  days: string[];
  // Day -> how many detections of the filter. Only the days in `days`.
  detectionsPerDay: Record<string, number>;
}
