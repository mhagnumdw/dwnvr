// The diagnostics notices: the Avisos card on the Diagnostics screen and the
// header bell both rely on the same function, so the two can't disagree.
//
// A pure function over the `health` from `state.svelte.ts`: called inside a
// $derived, Svelte tracks the fields it reads.

import { alertNotifier, type HealthCamera, type AlertsDropped } from './api.ts';
import { bytes, mbToBytes, duration, hhmmss, ddmm } from './format.ts';
import type { HealthState } from './state.svelte.ts';

export type NoticeLevel = 'bad' | 'warn';

export interface Notice {
  nivel: NoticeLevel;
  texto: string;
}

// How often the header rereads health for the bell, outside the screens that
// already reread it every HEALTH_POLL_MS. A new problem shows up on the bell
// within this time; shorter, every tab open on Live would pay for one more
// request per interval just to keep a number.
export const NOTICES_POLL_MS = 30000;

const MS_HOUR = 3600e3;

// "Now" by the server's clock, which is what stamped the notices' instants:
// with the device clock wrong, "since 13:58 (12min)" would become "(3h12min)".
// It falls back to the device's against an old server, without the field.
export function serverNow(health: HealthState) {
  const lido = health.clock ? Date.parse(health.clock.now) : NaN;
  return isNaN(lido) ? health.updatedAt || Date.now() : lido;
}

// when writes an instant: just the time if it is today, with the date in front
// if not - "13:58:12" from yesterday would read as today's.
const ofToday = (ms: number, now: number) =>
  new Date(now).toDateString() === new Date(ms).toDateString();
export const when = (ms: number, now: number) =>
  ofToday(ms, now) ? hhmmss(ms) : `${ddmm(ms)} ${hhmmss(ms)}`;

// since is the "since when" of every time-dependent notice, in the format of
// "NÃO ESTÁ GRAVANDO": "desde 13:58:12 (12min)".
export function since(iso: string, now: number) {
  const ms = Date.parse(iso);
  return `desde ${when(ms, now)} (${duration(now - ms)})`;
}

const perHour = (v: number) => v.toLocaleString('pt-BR', { maximumFractionDigits: v < 10 ? 1 : 0 });

// The reconnection message states the total, since when it counts, the rate,
// the recent count and the last one: "24 vezes" alone doesn't say whether that
// is a lot.
function reconnections(c: HealthCamera, health: HealthState, now: number) {
  const start = Date.parse(c.reconnectsSince);
  if (isNaN(start)) return `${c.name} já reconectou ${c.reconnects} vezes.`; // old server

  // Less than a minute between dwnvr boot and the start of the count is the
  // same thing; beyond that, the count was reset or the camera restarted.
  const boot = health.uptime ? now - health.uptime.appSeconds * 1000 : NaN;
  const origin =
    Math.abs(start - boot) < 60e3 ? 'desde que o dwnvr subiu' : `desde ${when(start, now)}`;

  const counting = now - start;
  const between = [duration(counting)];
  // With less than an hour of count, "per hour" extrapolates too much.
  if (counting >= MS_HOUR)
    between.push(`~${perHour(c.reconnects / (counting / MS_HOUR))} por hora`);

  let text = `${c.name} reconectou ${c.reconnects} vezes ${origin} (${between.join(', ')})`;
  // Before the count covers the whole window, the recent count is the total
  // itself.
  const visibleWindow = health.reconnectsWindowHours;
  if (visibleWindow && counting >= visibleWindow * MS_HOUR) {
    text += `, ${c.reconnectsInWindow} nas últimas ${visibleWindow}h`;
  }
  const lastOne = c.lastReconnectAt ? Date.parse(c.lastReconnectAt) : NaN;
  if (!isNaN(lastOne)) {
    text += ofToday(lastOne, now)
      ? `, a última às ${hhmmss(lastOne)}`
      : `, a última em ${ddmm(lastOne)} às ${hhmmss(lastOne)}`;
  }
  return text + '.';
}

// Notices that explain problems before they become a mystery - which is exactly
// what was missing in the previous NVRs. Returns [{ level, text }], with level
// 'bad' or 'warn'.
export function noticesOf(health: HealthState): Notice[] {
  const now = serverNow(health);
  const disk = health.disk;
  const out: Notice[] = [];
  if (disk?.belowMin) {
    const whenDisk = disk.belowMinSince ? ` ${since(disk.belowMinSince, now)}` : '';
    out.push({
      nivel: 'bad',
      texto: `Disco abaixo do mínimo livre (${mbToBytes(disk.minFreeMB)})${whenDisk}: restam ${bytes(disk.freeBytes)}. A retenção está apagando gravações antigas de todas as câmeras.`,
    });
  }
  // "Stalled" comes before "disconnected" because it is the question that
  // matters: a live connection that produces no segment at all is still a lost
  // recording, and that is how 9 cameras spent hours down with nobody noticing.
  for (const c of health.cameras) {
    if (!c.enabled || !c.silent) continue;
    const whenStopped = c.lastSegmentAt
      ? since(c.lastSegmentAt, now)
      : 'e não gravou nada desde que o dwnvr subiu';
    out.push({
      nivel: 'bad',
      texto: `${c.name} NÃO ESTÁ GRAVANDO ${whenStopped}${c.lastError ? `: ${c.lastError}` : ''}`,
    });
  }
  for (const c of health.cameras) {
    if (!c.enabled || c.connected) continue;
    if (c.silent) continue; // already warned above, and with more information
    const whenDown = c.disconnectedAt ? ` ${since(c.disconnectedAt, now)}` : '';
    out.push({
      nivel: 'bad',
      texto: `${c.name} desconectada${whenDown}: ${c.lastError ?? 'motivo desconhecido'}`,
    });
  }
  // The two configuration notices also come from /api/health, and not from the
  // camera list, which the Diagnostics screen never rereads: the edited
  // go2rtc.yaml and the go2rtc restarted from outside would only show up after
  // going through the Cameras screen.
  for (const c of health.cameras) {
    if (c.transcoding) {
      out.push({
        nivel: 'warn',
        texto: `${c.name} usa uma fonte ffmpeg no go2rtc, ou seja, há transcodificação consumindo CPU.`,
      });
    }
  }
  for (const c of health.cameras) {
    // Audio only comes from a camera with a recorder: a disabled one has no
    // track to check.
    if (c.audio && c.audio !== 'none' && !c.hasAudio) {
      out.push({
        nivel: 'warn',
        texto: `${c.name} está configurada com áudio ${c.audio}, mas o stream não entrega trilha de áudio.`,
      });
    }
    if (c.reconnects > 10) {
      out.push({ nivel: 'warn', texto: reconnections(c, health, now) });
    }
  }
  // It comes from /api/health, reread every few seconds, and not from
  // cameras.go2rtcError, which the Diagnostics screen never rereads: go2rtc
  // going down with it open has to show up, and disappear when it comes back.
  if (health.go2rtc) {
    out.push({
      nivel: 'bad',
      texto: `go2rtc inacessível ${since(health.go2rtc.since, now)}: ${health.go2rtc.error}`,
    });
  }
  // With the object detector down, recording and the motion marks continue
  // normally: without this notice, the missing object marks are only noticed
  // later, on the timeline, and the marks from that interval don't come back.
  if (health.detector?.outage) {
    const f = health.detector.outage;
    out.push({
      nivel: 'bad',
      texto: `Detector de objetos inacessível ${since(f.since, now)}: ${f.error}. As câmeras seguem gravando e marcando movimento, mas nenhuma marca de objeto é criada enquanto isso.`,
    });
  }
  // The smtp section rejected at boot turns email off until someone fixes
  // dwnvr.yaml and restarts: without this notice, only the log would say.
  // Yellow, because recording and detection continue normally.
  if (health.smtp) {
    out.push({
      nivel: 'warn',
      texto: `E-mail desligado: ${health.smtp.error}. Corrija o dwnvr.yaml e reinicie o dwnvr.`,
    });
  }
  // The usuarios.json edited by hand. Unreadable is red: only the owner gets
  // in. An entry that isn't valid is yellow, one line per problem.
  if (health.users?.error) {
    out.push({
      nivel: 'bad',
      texto: `Cadastro de usuários ilegível: ${health.users.error}. Só o dono do dwnvr.yaml entra até o arquivo ser corrigido e o dwnvr reiniciado.`,
    });
  }
  for (const w of health.users?.warnings ?? []) {
    out.push({
      nivel: 'warn',
      texto: `Cadastro de usuários: ${w}. Corrija o usuarios.json e reinicie o dwnvr.`,
    });
  }
  // The alerts section of dwnvr.yaml: the address that was left out of the
  // list, or the list without the smtp section. Yellow, like the rejected smtp:
  // the alert is still recorded in the detection.
  for (const w of health.alerts?.warnings ?? []) {
    out.push({
      nivel: 'warn',
      texto: `Alertas por e-mail: ${w}. Corrija o dwnvr.yaml e reinicie o dwnvr.`,
    });
  }
  // The alert notifier failing: red, because nobody is being notified, and
  // recording continuing doesn't change that. It goes away on the first
  // delivery that works.
  for (const f of health.alerts?.failing ?? []) {
    out.push({
      nivel: 'bad',
      texto: `Alertas ${alertNotifier(f.name)} falhando ${since(f.since, now)}: ${f.error}. As detecções seguem gravadas, mas ninguém está sendo avisado.`,
    });
  }
  // The alerts that didn't go out through the notifiers, because of the ceiling
  // or a full queue. Yellow: they are recorded in the detection. It goes away
  // one hour after the last drop, and not on the next delivery: a drop is an
  // event, not a state.
  if (health.alerts?.dropped) {
    out.push({ nivel: 'warn', texto: droppedAlerts(health.alerts.dropped, now) });
  }
  return out;
}

// "12 alertas não foram enviados desde 14:02:10 (5min), o último às 14:07:31:
// passaram do teto de 15 por hora." With both reasons, each with its own count;
// with a single alert, in the singular and without the "desde".
function droppedAlerts(d: AlertsDropped, now: number) {
  const total = d.queueFull + d.overLimit;
  const one = (n: number, singular: string, plural: string) => (n === 1 ? singular : plural);
  const ceiling = (n: number) =>
    `${one(n, 'passou', 'passaram')} do teto de ${d.limitPerHour} por hora`;
  const reason =
    d.queueFull === 0
      ? ceiling(total)
      : d.overLimit === 0
        ? 'a fila de envio encheu'
        : `${d.overLimit} ${ceiling(d.overLimit)}, e ${d.queueFull} não ${one(d.queueFull, 'coube', 'couberam')} na fila de envio`;
  const what =
    total === 1
      ? `1 alerta não foi enviado, às ${when(Date.parse(d.since), now)}`
      : `${total} alertas não foram enviados ${since(d.since, now)}, o último às ${when(Date.parse(d.lastAt), now)}`;
  const where = one(total, 'Ele está gravado', 'Eles estão gravados');
  return `${what}: ${reason}. ${where} em Detecções, no filtro só alertas.`;
}

// What the bell needs: how many, and the color of the worst. One red is enough
// for the bell to turn red; with only yellows, it turns yellow and more
// discreet.
export function noticesSummary(notices: Notice[]): { total: number; nivel: NoticeLevel | null } {
  return {
    total: notices.length,
    nivel: notices.some((a) => a.nivel === 'bad') ? 'bad' : notices.length ? 'warn' : null,
  };
}
