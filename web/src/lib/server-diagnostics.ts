// What the recording machine is going through, for the "Este servidor" card on
// Diagnostics. The numbers come from GET /api/health/server; here they become
// the same { title, items } groups as the browser card, with `alert` flagging
// the value that, by itself, is already a probable explanation for a defect.
//
// The alert limits are deliberately wide: the target is small hardware, which
// works near the ceiling day to day, and a red card for nothing teaches people
// to ignore red.

import {
  alertNotifier,
  type LogLine,
  type AlertNotifier,
  type ServerProcess,
  type PressureResource,
  type Server,
} from './api.ts';
import { bytes, duration, timezoneClock } from './format.ts';
import type { CardGroup, CardItem } from './browser.ts';

// Above this most ARM SoCs start throttling the frequency to cool down, and the
// Orange Pi Zero 3 gets there without a heatsink.
const HIGH_TEMPERATURE = 80;

// PSI, 60s average, as % of the time stalled waiting for the resource. Memory
// is the most sensitive: 10% is already the kernel reclaiming pages instead of
// working.
const HIGH_PRESSURE: Readonly<Record<PressureResource, number>> = { cpu: 50, memory: 10, io: 25 };

// A 4 KB fsync above this means a disk in trouble.
const SLOW_WRITE_MS = 1000;

// Fraction of the ceiling - container memory, open files - from which the
// process is one scare away from hitting it.
const NEAR_CEILING = 0.9;

function num(v: number, decimals = 0) {
  return v.toLocaleString('pt-BR', {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  });
}

function machine(m: Server['machine']) {
  const items: CardItem[] = [];
  if (m.board) items.push({ label: 'placa', value: m.board });
  if (m.processor) items.push({ label: 'processador', value: m.processor });
  if (m.kernel) items.push({ label: 'kernel', value: m.kernel });
  // The container's ceiling goes along with the cores, and not on a separate
  // line: it is what says how many dwnvr actually sees.
  const allowed = m.allowedCores
    ? ` (o container pode usar ${num(m.allowedCores, m.allowedCores % 1 ? 1 : 0)})`
    : '';
  items.push({ label: 'núcleos', value: `${m.cores}${allowed}` });
  if (m.load) {
    // Load above the number of cores is a queue: there are processes waiting
    // for CPU.
    items.push({
      label: 'carga (1, 5, 15 min)',
      value: m.load.map((c) => num(c, 2)).join(' · '),
      alert: (m.load[0] ?? 0) > m.cores,
    });
  }
  const sensores = m.sensors ?? [];
  for (const s of sensores) {
    items.push({
      label: sensores.length > 1 ? `temperatura (${s.name})` : 'temperatura',
      value: `${num(s.celsius, 1)} °C`,
      alert: s.celsius >= HIGH_TEMPERATURE,
    });
  }
  if (m.mhz) {
    const max = m.mhzMax ? ` de ${num(m.mhzMax)}` : '';
    items.push({ label: 'frequência da CPU', value: `${num(m.mhz)}${max} MHz` });
  }
  if (m.governor) items.push({ label: 'política de frequência', value: m.governor });
  return items;
}

// processInfo is dwnvr seen by itself. `previous` is the earlier reading, from
// which the current CPU comes; without it, the average since dwnvr booted
// applies.
function processInfo(
  p: ServerProcess,
  cores: number,
  limitBytes: number | undefined,
  previous: ServerProcess | undefined,
) {
  const items: CardItem[] = [];
  if (p.cpuMs != null && p.aliveMs > 0) {
    // A reading from before dwnvr's last boot is no good: the CPU time
    // restarted from zero.
    const a =
      previous?.cpuMs != null && p.aliveMs > previous.aliveMs && p.cpuMs >= previous.cpuMs
        ? { cpuMs: previous.cpuMs, vivoMs: previous.aliveMs }
        : null;
    const used = a ? (p.cpuMs - a.cpuMs) / (p.aliveMs - a.vivoMs) : p.cpuMs / p.aliveMs;
    const when = a ? `últimos ${duration(p.aliveMs - a.vivoMs)}` : 'média desde que subiu';
    // As a fraction of the whole machine, and not of one core, to compare
    // directly with the load and the pressure beside it.
    items.push({
      label: 'CPU',
      value: `${num((used / cores) * 100, 1)}% da máquina (${when})`,
    });
  }
  if (p.memoryBytes) {
    items.push({
      label: 'memória',
      value: limitBytes
        ? `${bytes(p.memoryBytes)} de ${bytes(limitBytes)} do limite`
        : bytes(p.memoryBytes),
      alert: !!limitBytes && p.memoryBytes >= limitBytes * NEAR_CEILING,
    });
  }
  items.push({ label: 'goroutines', value: num(p.goroutines) });
  if (p.openFiles) {
    const l = p.fileLimit;
    items.push({
      label: 'arquivos abertos',
      value: l ? `${num(p.openFiles)} de ${num(l)}` : num(p.openFiles),
      alert: !!l && p.openFiles >= l * NEAR_CEILING,
    });
  }
  if (p.uid != null) items.push({ label: 'usuário e grupo', value: `${p.uid}:${p.gid ?? '?'}` });
  items.push({ label: 'binário', value: `Go ${p.go.replace(/^go/, '')} · ${p.architecture}` });
  return items;
}

// The container limit goes on the process's memory line, which is what hits it;
// here it only appears if that line is missing.
function memory(m: NonNullable<Server['memory']>, processLimit: boolean) {
  const free = m.totalBytes ? m.availableBytes / m.totalBytes : 1;
  const items: CardItem[] = [
    {
      label: 'memória disponível',
      value: `${bytes(m.availableBytes)} de ${bytes(m.totalBytes)}`,
      alert: free < 0.1,
    },
  ];
  if (m.swapTotalBytes) {
    // Swap used by itself is not a problem - the kernel sends there what nobody
    // uses. Half full is what is already really missing memory.
    items.push({
      label: 'swap usada',
      value: `${bytes(m.swapUsedBytes)} de ${bytes(m.swapTotalBytes)}`,
      alert: m.swapUsedBytes > m.swapTotalBytes / 2,
    });
  } else {
    items.push({ label: 'swap', value: 'nenhuma' });
  }
  if (m.limitBytes && !processLimit)
    items.push({ label: 'limite do container', value: bytes(m.limitBytes) });
  if (m.oomKills != null) {
    items.push({
      label: 'processos encerrados por falta de memória (OOM kill)',
      value: m.oomKills
        ? `${m.oomKills} desde que a máquina ligou`
        : 'nenhum desde que a máquina ligou',
      alert: m.oomKills > 0,
    });
  }
  return items;
}

const RESOURCES: [PressureResource, string][] = [
  ['cpu', 'esperando CPU'],
  ['memory', 'esperando memória'],
  ['io', 'esperando disco'],
];

function pressure(p: NonNullable<Server['pressure']>): CardItem[] {
  return RESOURCES.flatMap(([k, label]) => {
    const some = p[k]?.some;
    if (!some) return [];
    return [
      {
        label,
        value: `${num(some.avg60, 1)}% do tempo`,
        alert: some.avg60 >= HIGH_PRESSURE[k],
      },
    ];
  });
}

function storage(s: Server['storage']) {
  const items: CardItem[] = [{ label: 'caminho', value: s.path }];
  if (s.type) items.push({ label: 'sistema de arquivos', value: s.type });
  if (s.origin) items.push({ label: 'dispositivo', value: s.origin });
  if (s.options) {
    items.push({
      label: 'montado como',
      value: s.readOnly ? 'SOMENTE LEITURA' : 'leitura e escrita',
      alert: s.readOnly,
    });
  }
  const e = s.write;
  items.push({
    label: 'teste de escrita',
    value: e.ok ? `ok em ${num(e.ms)} ms` : (e.error ?? 'falhou'),
    alert: !e.ok || e.ms >= SLOW_WRITE_MS,
  });
  return items;
}

function go2rtc(g: Server['go2rtc']) {
  const items: CardItem[] = [
    {
      label: 'responde',
      value: g.ok ? `sim, em ${num(g.ms)} ms` : `não: ${g.error ?? '?'}`,
      alert: !g.ok,
    },
  ];
  if (g.version) items.push({ label: 'versão', value: g.version });
  if (g.url) items.push({ label: 'endereço', value: g.url });
  return items;
}

function detector(d: NonNullable<Server['detector']>) {
  const items: CardItem[] = [
    {
      label: 'responde',
      value: d.ok ? `sim, em ${num(d.ms)} ms` : `não: ${d.error ?? '?'}`,
      alert: !d.ok,
    },
  ];
  if (d.model)
    items.push({
      label: 'modelo',
      value: d.entry ? `${d.model}, entrada ${d.entry}` : d.model,
    });
  if (d.threads) items.push({ label: 'threads', value: String(d.threads) });
  items.push({ label: 'endereço', value: d.url });
  return items;
}

const SAFETY: Record<string, string> = {
  starttls: 'STARTTLS',
  tls: 'TLS direto',
  none: 'sem TLS',
};

// smtp is the smtp section as it ended up in effect; if rejected, only the
// reason. The real check is the test email, right below in the card.
function smtp(s: NonNullable<Server['smtp']>): CardItem[] {
  if (s.error) return [{ label: 'situação', value: `desligado: ${s.error}`, alert: true }];
  return [
    { label: 'servidor', value: `${s.host ?? '?'}:${s.port ?? '?'}` },
    { label: 'segurança', value: SAFETY[s.security ?? ''] ?? s.security ?? '?' },
    { label: 'remetente', value: s.from ?? '?' },
  ];
}

// middle is the deliveries of an alert notifier since dwnvr booted. The last
// one goes on the server's clock, like the card's "Coletado em".
function middle(m: AlertNotifier, offsetSeconds: number): CardItem[] {
  const ms = m.last ? Date.parse(m.last) : NaN;
  const at = isNaN(ms) ? '' : timezoneClock(ms, offsetSeconds);
  return [
    { label: 'enviados', value: num(m.sent) },
    { label: 'falhas', value: num(m.failures) },
    !at
      ? { label: 'último envio', value: 'nenhum ainda' }
      : m.error
        ? { label: 'último envio', value: `falhou em ${at}: ${m.error}`, alert: true }
        : { label: 'último envio', value: `ok em ${at}` },
  ];
}

// groups builds the card. A group with no source - kernel without PSI, server
// outside Linux, installation without an object detector - disappears instead
// of showing up empty. `previous` is the earlier reading, for the process CPU.
export function groups(info: Server, previous: Server | null): CardGroup[] {
  const limit = info.memory?.limitBytes;
  const out: CardGroup[] = [{ title: 'Máquina', items: machine(info.machine) }];
  // A server older than this field doesn't send the process.
  if (info.process) {
    out.push({
      title: 'Processo do dwnvr',
      items: processInfo(info.process, info.machine.cores, limit, previous?.process),
    });
  }
  if (info.memory) {
    out.push({ title: 'Memória', items: memory(info.memory, !!info.process?.memoryBytes) });
  }
  if (info.pressure) {
    const items = pressure(info.pressure);
    if (items.length) out.push({ title: 'Pressão (último minuto)', items });
  }
  out.push({ title: 'Armazenamento', items: storage(info.storage) });
  out.push({ title: 'go2rtc', items: go2rtc(info.go2rtc) });
  if (info.detector) out.push({ title: 'Detector de objetos', items: detector(info.detector) });
  if (info.smtp) out.push({ title: 'E-mail', items: smtp(info.smtp) });
  // One group per notifier: the server's email, above, is not the alert
  // notifier.
  for (const m of info.alerts ?? []) {
    out.push({
      title: `Alertas ${alertNotifier(m.name)}`,
      items: middle(m, info.collectedAt?.offsetSeconds ?? 0),
    });
  }
  return out;
}

// collectedAt says when the server took the reading, on ITS clock and timezone,
// like the "horário do servidor" in the top strip: converted to the reader's
// timezone, it would hide a server with the wrong timezone. A server older than
// this field sends nothing.
//
// The spaces inside the date and the timezone are non-breaking: when it doesn't
// fit on one line, on the phone, the break falls between the time and the
// timezone, and not in the middle of one.
export function collectedAt(c: Server['collectedAt']) {
  if (!c) return '';
  const lido = Date.parse(c.at);
  if (isNaN(lido)) return '';
  const wholeNumber = (s: string) => s.replaceAll(' ', ' ');
  return `${wholeNumber(`Coletado em ${timezoneClock(lido, c.offsetSeconds)}`)} ${wholeNumber(c.zone ?? c.abbreviation)}`;
}

// logLine formats like docker logs would show, with the reader's local time.
export function logLine(l: LogLine) {
  const hour = new Date(l.at).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'medium' });
  return `${hour} ${l.level} ${l.text}`;
}
