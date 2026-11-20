// What the viewer's browser is and can do, for the Diagnostics screen. It all
// comes from the browser itself: the server knows none of this, and it is
// exactly what is missing when someone says "it looked weird on my phone".
//
// The criterion for what goes in is explaining a defect the app can have -
// layout, video, clock, storage. It is not an inventory of the device.

// What only some browsers have, and TypeScript's lib.dom doesn't declare yet.
// Hence everything is optional: the question here is precisely whether it
// exists.
declare global {
  interface Navigator {
    readonly userAgentData?: {
      getHighEntropyValues?: (hints: string[]) => Promise<{ model?: string }>;
    };
    readonly connection?: { effectiveType?: string; saveData?: boolean };
    readonly deviceMemory?: number;
  }
  interface Window {
    // Safari's MediaSource on the iPhone, which doesn't have the standard
    // MediaSource.
    readonly ManagedMediaSource?: typeof MediaSource;
  }
}

// An item of the diagnostics cards, the browser's and the server's. `alert`
// flags a value that by itself is already a probable explanation for a defect.
export interface CardItem {
  label: string;
  value: string;
  alert?: boolean;
}

export interface CardGroup {
  title: string;
  items: CardItem[];
}

// The codecs the recordings player actually asks MediaSource for: it is the
// same table as #mimeFor in player.svelte.ts, and live (video-rtc.js)
// negotiates from H.264, H.265 and AAC. A "no" here directly explains "this
// browser can't play" on the Recordings screen.
const CODECS = [
  { id: 'avc1.640029', label: 'H.264' },
  { id: 'hvc1.1.6.L153.B0', label: 'H.265 (hvc1)' },
  { id: 'hev1.1.6.L153.B0', label: 'H.265 (hev1)' },
  { id: 'mp4a.40.2', label: 'AAC' },
  { id: 'opus', label: 'Opus' },
  { id: 'flac', label: 'FLAC' },
];

// The font size the app asks for on the body (app.css). The text scale is
// measured against it.
const BASE_FONT_PX = 15;

// Browser and version from the user agent. The order matters: Edge, Samsung and
// Opera also say "Chrome/" in the UA, so Chrome is the last one tried.
function uaBrowser(ua: string) {
  const attempts: [RegExp, string][] = [
    [/EdgA?\/([\d.]+)/, 'Edge'],
    [/SamsungBrowser\/([\d.]+)/, 'Samsung Internet'],
    [/OPR\/([\d.]+)/, 'Opera'],
    [/Firefox\/([\d.]+)/, 'Firefox'],
    [/Chrome\/([\d.]+)/, 'Chrome'],
    [/Version\/([\d.]+).*Safari/, 'Safari'],
  ];
  for (const [re, name] of attempts) {
    const m = re.exec(ua);
    if (m?.[1]) return `${name} ${m[1]}`;
  }
  return 'desconhecido';
}

function uaSystem(ua: string) {
  let m = /Android ([\d.]+)/.exec(ua);
  if (m?.[1]) return `Android ${m[1]}`;
  m = /(?:iPhone|iPad).*OS ([\d_]+)/.exec(ua);
  if (m?.[1]) return `iOS ${m[1].replaceAll('_', '.')}`;
  if (ua.includes('Windows')) return 'Windows';
  if (ua.includes('Mac OS X')) return 'macOS';
  if (ua.includes('Linux')) return 'Linux';
  return 'desconhecido';
}

// appInstalled says whether the page runs as the installed app (PWA), in its
// own window, with no address bar.
export function appInstalled() {
  return matchMedia('(display-mode: standalone)').matches;
}

// Where the page is running. A WebView is another app's embedded browser
// (opening the link from inside WhatsApp, for example): it is a different
// engine, with a different version, and not the Chrome the person thinks they
// are using.
function whereRuns(ua: string) {
  if (ua.includes('; wv)')) return 'WebView de outro app';
  if (appInstalled()) return 'app instalado (PWA)';
  return 'aba do navegador';
}

// The real text scale, measured and not read. Android's large font reaches
// Chrome in more than one way - text adjustment, page zoom - and
// getComputedStyle doesn't always see it; the drawn size sees all of them.
function textScale() {
  const s = document.createElement('span');
  s.textContent = 'M';
  s.style.cssText = `position:absolute;visibility:hidden;font-size:${BASE_FONT_PX}px;line-height:1;padding:0;border:0`;
  document.body.append(s);
  const height = s.getBoundingClientRect().height;
  s.remove();
  return height / BASE_FONT_PX;
}

function supports(mime: string) {
  try {
    const mse = typeof MediaSource === 'undefined' ? window.ManagedMediaSource : MediaSource;
    return mse?.isTypeSupported(mime) ?? false;
  } catch {
    return false;
  }
}

// localStorage can exist and still throw when used: incognito tab in some
// browsers, zeroed quota, blocked site data. Only a write proves it.
function storageOk() {
  try {
    const k = 'dwnvr.diag.test';
    localStorage.setItem(k, '1');
    localStorage.removeItem(k);
    return true;
  } catch {
    return false;
  }
}

// The device model. Chrome's user agent on Android no longer carries it (it
// only says "Android 10; K"), and the only way, userAgentData, is
// [SecureContext]: on a dwnvr served over http:// it doesn't even exist. So the
// answer tells apart "couldn't because it's http" from "the browser doesn't
// report it".
async function model() {
  if (!isSecureContext) return 'indisponível (http)';
  const uad = navigator.userAgentData;
  if (!uad?.getHighEntropyValues) return 'o navegador não informa';
  try {
    const v = await uad.getHighEntropyValues(['model']);
    // Desktop answers an empty model, and not an absent one.
    // eslint-disable-next-line @typescript-eslint/prefer-nullish-coalescing -- empty also falls back to the text
    return v.model || 'o navegador não informa';
  } catch {
    return 'o navegador não informa';
  }
}

function sim(v: boolean) {
  return v ? 'sim' : 'não';
}

// collect returns groups of items { label, value, alert? }. `alert` flags a
// value that by itself is already a probable explanation for a defect.
export async function collect(): Promise<CardGroup[]> {
  const ua = navigator.userAgent;
  const vv = window.visualViewport;
  const scale = textScale();
  const hasMSE = 'MediaSource' in window || 'ManagedMediaSource' in window;
  const connection = navigator.connection;

  const identity: CardItem[] = [
    { label: 'navegador', value: uaBrowser(ua) },
    { label: 'sistema', value: uaSystem(ua) },
    { label: 'aparelho', value: await model() },
    { label: 'rodando em', value: whereRuns(ua), alert: ua.includes('; wv)') },
  ];

  const screenInfo: CardItem[] = [
    { label: 'área da página', value: `${innerWidth} × ${innerHeight}` },
    ...(vv && (Math.round(vv.width) !== innerWidth || Math.round(vv.height) !== innerHeight)
      ? [{ label: 'área visível', value: `${Math.round(vv.width)} × ${Math.round(vv.height)}` }]
      : []),
    { label: 'densidade de pixels', value: `${devicePixelRatio}×` },
    // Safari before 16.4 doesn't have screen.orientation.
    {
      label: 'orientação',
      value:
        'orientation' in screen
          ? screen.orientation.type
          : innerWidth > innerHeight
            ? 'landscape'
            : 'portrait',
    },
    // Above 115% the layout, calibrated at 15px, starts wrapping lines where it
    // didn't before.
    { label: 'escala do texto', value: `${Math.round(scale * 100)}%`, alert: scale > 1.15 },
    { label: 'toque', value: sim(matchMedia('(pointer: coarse)').matches) },
    {
      label: 'tema do sistema',
      value: matchMedia('(prefers-color-scheme: light)').matches ? 'claro' : 'escuro',
    },
    ...(matchMedia('(prefers-reduced-motion: reduce)').matches
      ? [{ label: 'animações', value: 'reduzidas' }]
      : []),
  ];

  const video: CardItem[] = [
    { label: 'MediaSource', value: sim(hasMSE), alert: !hasMSE },
    { label: 'WebRTC', value: sim('RTCPeerConnection' in window) },
    ...CODECS.map((c) => {
      const ok = hasMSE && supports(`video/mp4; codecs="${c.id}"`);
      return { label: c.label, value: sim(ok), alert: !ok && c.id === 'avc1.640029' };
    }),
  ];

  const storage = storageOk();
  const environment: CardItem[] = [
    { label: 'endereço seguro (https)', value: sim(isSecureContext) },
    {
      label: 'armazenamento local',
      value: storage ? 'ok' : 'bloqueado',
      alert: !storage,
    },
    { label: 'fuso', value: Intl.DateTimeFormat().resolvedOptions().timeZone || '?' },
    { label: 'idioma', value: navigator.language },
    ...(navigator.deviceMemory
      ? [{ label: 'memória', value: `${navigator.deviceMemory} GB` }]
      : []),
    ...(navigator.hardwareConcurrency
      ? [{ label: 'núcleos', value: String(navigator.hardwareConcurrency) }]
      : []),
    ...(connection?.effectiveType
      ? [
          {
            label: 'rede',
            value: connection.effectiveType + (connection.saveData ? ', economia de dados' : ''),
          },
        ]
      : []),
  ];

  return [
    { title: 'Identidade', items: identity },
    { title: 'Tela', items: screenInfo },
    { title: 'Vídeo', items: video },
    { title: 'Ambiente', items: environment },
  ];
}

// asText puts the groups on one line per item, to paste into a chat. `header`
// goes before the groups and `footer` after - the user agent in the browser
// card, the log lines in the server's.
export function asText(groups: CardGroup[], header: string[] = [], footer: string[] = []) {
  const rows = [...header];
  for (const g of groups) {
    rows.push('', `[${g.title}]`);
    for (const i of g.items) rows.push(`${i.label}: ${i.value}`);
  }
  if (footer.length) rows.push('', ...footer);
  return rows.join('\n').trim();
}

// copy puts the text on the clipboard. The Clipboard API is [SecureContext], so
// on http:// the old execCommand is left, which Chrome still accepts from a
// tap. It returns whether it worked.
export async function copy(text: string) {
  // Outside https the clipboard doesn't even exist, despite what lib.dom
  // declares.
  if ('clipboard' in navigator) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // falls back to the old path
    }
  }
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.setAttribute('readonly', '');
  ta.style.cssText = 'position:fixed;top:0;left:0;opacity:0';
  document.body.append(ta);
  ta.select();
  try {
    // eslint-disable-next-line @typescript-eslint/no-deprecated -- it is the only path outside https
    return document.execCommand('copy');
  } catch {
    return false;
  } finally {
    ta.remove();
  }
}
