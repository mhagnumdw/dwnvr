// What the recording machine is going through, for the "Este servidor" card on
// Diagnostics. The numbers come from GET /api/health/servidor; here they become
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
const HIGH_PRESSURE: Readonly<Record<PressureResource, number>> = { cpu: 50, memoria: 10, io: 25 };

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

function machine(m: Server['maquina']) {
  const items: CardItem[] = [];
  if (m.placa) items.push({ label: 'placa', value: m.placa });
  if (m.processador) items.push({ label: 'processador', value: m.processador });
  if (m.kernel) items.push({ label: 'kernel', value: m.kernel });
  // The container's ceiling goes along with the cores, and not on a separate
  // line: it is what says how many dwnvr actually sees.
  const allowed = m.nucleosLiberados
    ? ` (o container pode usar ${num(m.nucleosLiberados, m.nucleosLiberados % 1 ? 1 : 0)})`
    : '';
  items.push({ label: 'núcleos', value: `${m.nucleos}${allowed}` });
  if (m.carga) {
    // Load above the number of cores is a queue: there are processes waiting
    // for CPU.
    items.push({
      label: 'carga (1, 5, 15 min)',
      value: m.carga.map((c) => num(c, 2)).join(' · '),
      alert: (m.carga[0] ?? 0) > m.nucleos,
    });
  }
  const sensores = m.sensores ?? [];
  for (const s of sensores) {
    items.push({
      label: sensores.length > 1 ? `temperatura (${s.nome})` : 'temperatura',
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
  if (p.cpuMs != null && p.vivoMs > 0) {
    // A reading from before dwnvr's last boot is no good: the CPU time
    // restarted from zero.
    const a =
      previous?.cpuMs != null && p.vivoMs > previous.vivoMs && p.cpuMs >= previous.cpuMs
        ? { cpuMs: previous.cpuMs, vivoMs: previous.vivoMs }
        : null;
    const used = a ? (p.cpuMs - a.cpuMs) / (p.vivoMs - a.vivoMs) : p.cpuMs / p.vivoMs;
    const when = a ? `últimos ${duration(p.vivoMs - a.vivoMs)}` : 'média desde que subiu';
    // As a fraction of the whole machine, and not of one core, to compare
    // directly with the load and the pressure beside it.
    items.push({
      label: 'CPU',
      value: `${num((used / cores) * 100, 1)}% da máquina (${when})`,
    });
  }
  if (p.memoriaBytes) {
    items.push({
      label: 'memória',
      value: limitBytes
        ? `${bytes(p.memoriaBytes)} de ${bytes(limitBytes)} do limite`
        : bytes(p.memoriaBytes),
      alert: !!limitBytes && p.memoriaBytes >= limitBytes * NEAR_CEILING,
    });
  }
  items.push({ label: 'goroutines', value: num(p.goroutines) });
  if (p.arquivosAbertos) {
    const l = p.limiteDeArquivos;
    items.push({
      label: 'arquivos abertos',
      value: l ? `${num(p.arquivosAbertos)} de ${num(l)}` : num(p.arquivosAbertos),
      alert: !!l && p.arquivosAbertos >= l * NEAR_CEILING,
    });
  }
  if (p.uid != null) items.push({ label: 'usuário e grupo', value: `${p.uid}:${p.gid ?? '?'}` });
  items.push({ label: 'binário', value: `Go ${p.go.replace(/^go/, '')} · ${p.arquitetura}` });
  return items;
}

// The container limit goes on the process's memory line, which is what hits it;
// here it only appears if that line is missing.
function memory(m: NonNullable<Server['memoria']>, processLimit: boolean) {
  const free = m.totalBytes ? m.disponivelBytes / m.totalBytes : 1;
  const items: CardItem[] = [
    {
      label: 'memória disponível',
      value: `${bytes(m.disponivelBytes)} de ${bytes(m.totalBytes)}`,
      alert: free < 0.1,
    },
  ];
  if (m.swapTotalBytes) {
    // Swap used by itself is not a problem - the kernel sends there what nobody
    // uses. Half full is what is already really missing memory.
    items.push({
      label: 'swap usada',
      value: `${bytes(m.swapUsadaBytes)} de ${bytes(m.swapTotalBytes)}`,
      alert: m.swapUsadaBytes > m.swapTotalBytes / 2,
    });
  } else {
    items.push({ label: 'swap', value: 'nenhuma' });
  }
  if (m.limiteBytes && !processLimit)
    items.push({ label: 'limite do container', value: bytes(m.limiteBytes) });
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
  ['memoria', 'esperando memória'],
  ['io', 'esperando disco'],
];

function pressure(p: NonNullable<Server['pressao']>): CardItem[] {
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
  const items: CardItem[] = [{ label: 'caminho', value: s.caminho }];
  if (s.tipo) items.push({ label: 'sistema de arquivos', value: s.tipo });
  if (s.origem) items.push({ label: 'dispositivo', value: s.origem });
  if (s.opcoes) {
    items.push({
      label: 'montado como',
      value: s.somenteLeitura ? 'SOMENTE LEITURA' : 'leitura e escrita',
      alert: s.somenteLeitura,
    });
  }
  const e = s.escrita;
  items.push({
    label: 'teste de escrita',
    value: e.ok ? `ok em ${num(e.ms)} ms` : (e.erro ?? 'falhou'),
    alert: !e.ok || e.ms >= SLOW_WRITE_MS,
  });
  return items;
}

function go2rtc(g: Server['go2rtc']) {
  const items: CardItem[] = [
    {
      label: 'responde',
      value: g.ok ? `sim, em ${num(g.ms)} ms` : `não: ${g.erro ?? '?'}`,
      alert: !g.ok,
    },
  ];
  if (g.versao) items.push({ label: 'versão', value: g.versao });
  if (g.url) items.push({ label: 'endereço', value: g.url });
  return items;
}

function detector(d: NonNullable<Server['detector']>) {
  const items: CardItem[] = [
    {
      label: 'responde',
      value: d.ok ? `sim, em ${num(d.ms)} ms` : `não: ${d.erro ?? '?'}`,
      alert: !d.ok,
    },
  ];
  if (d.modelo)
    items.push({
      label: 'modelo',
      value: d.entrada ? `${d.modelo}, entrada ${d.entrada}` : d.modelo,
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
  const ms = m.ultimo ? Date.parse(m.ultimo) : NaN;
  const at = isNaN(ms) ? '' : timezoneClock(ms, offsetSeconds);
  return [
    { label: 'enviados', value: num(m.enviados) },
    { label: 'falhas', value: num(m.falhas) },
    !at
      ? { label: 'último envio', value: 'nenhum ainda' }
      : m.erro
        ? { label: 'último envio', value: `falhou em ${at}: ${m.erro}`, alert: true }
        : { label: 'último envio', value: `ok em ${at}` },
  ];
}

// groups builds the card. A group with no source - kernel without PSI, server
// outside Linux, installation without an object detector - disappears instead
// of showing up empty. `previous` is the earlier reading, for the process CPU.
export function groups(info: Server, previous: Server | null): CardGroup[] {
  const limit = info.memoria?.limiteBytes;
  const out: CardGroup[] = [{ title: 'Máquina', items: machine(info.maquina) }];
  // A server older than this field doesn't send the process.
  if (info.processo) {
    out.push({
      title: 'Processo do dwnvr',
      items: processInfo(info.processo, info.maquina.nucleos, limit, previous?.processo),
    });
  }
  if (info.memoria) {
    out.push({ title: 'Memória', items: memory(info.memoria, !!info.processo?.memoriaBytes) });
  }
  if (info.pressao) {
    const items = pressure(info.pressao);
    if (items.length) out.push({ title: 'Pressão (último minuto)', items });
  }
  out.push({ title: 'Armazenamento', items: storage(info.storage) });
  out.push({ title: 'go2rtc', items: go2rtc(info.go2rtc) });
  if (info.detector) out.push({ title: 'Detector de objetos', items: detector(info.detector) });
  if (info.smtp) out.push({ title: 'E-mail', items: smtp(info.smtp) });
  // One group per notifier: the server's email, above, is not the alert
  // notifier.
  for (const m of info.alertas ?? []) {
    out.push({
      title: `Alertas ${alertNotifier(m.nome)}`,
      items: middle(m, info.coletadoEm?.offsetSeconds ?? 0),
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
export function collectedAt(c: Server['coletadoEm']) {
  if (!c) return '';
  const lido = Date.parse(c.em);
  if (isNaN(lido)) return '';
  const wholeNumber = (s: string) => s.replaceAll(' ', ' ');
  return `${wholeNumber(`Coletado em ${timezoneClock(lido, c.offsetSeconds)}`)} ${wholeNumber(c.zona ?? c.sigla)}`;
}

// logLine formats like docker logs would show, with the reader's local time.
export function logLine(l: LogLine) {
  const hour = new Date(l.em).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'medium' });
  return `${hour} ${l.nivel} ${l.texto}`;
}
