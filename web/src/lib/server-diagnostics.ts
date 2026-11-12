// O que a máquina que grava está passando, para o card "Este servidor" do
// Diagnóstico. Os números vêm do GET /api/health/servidor; aqui eles viram os
// mesmos grupos { titulo, itens } do card do navegador, com `alerta` marcando o
// valor que, sozinho, já é explicação provável de defeito.
//
// Os limites de alerta são deliberadamente largos: o alvo é hardware pequeno,
// que trabalha perto do teto no dia a dia, e um card vermelho à toa ensina a
// ignorar o vermelho.

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

// Acima disso a maioria dos SoCs ARM começa a reduzir a frequência para
// esfriar, e o Orange Pi Zero 3 chega lá sem dissipador.
const HIGH_TEMPERATURE = 80;

// PSI, média de 60s, em % do tempo parado esperando o recurso. Memória é o mais
// sensível: 10% já é o kernel recuperando página no lugar de trabalhar.
const HIGH_PRESSURE: Readonly<Record<PressureResource, number>> = { cpu: 50, memoria: 10, io: 25 };

// Um fsync de 4 KB acima disso é disco sofrendo.
const SLOW_WRITE_MS = 1000;

// Fração do teto - memória do container, arquivos abertos - a partir da qual o
// processo está a um susto de bater nele.
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
  // O teto do container vai junto dos núcleos, e não numa linha à parte: é ele
  // que diz quantos o dwnvr enxerga de fato.
  const allowed = m.nucleosLiberados
    ? ` (o container pode usar ${num(m.nucleosLiberados, m.nucleosLiberados % 1 ? 1 : 0)})`
    : '';
  items.push({ label: 'núcleos', value: `${m.nucleos}${allowed}` });
  if (m.carga) {
    // Carga acima do número de núcleos é fila: tem processo esperando CPU.
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

// processo é o dwnvr visto por ele mesmo. `anterior` é a leitura de antes, da
// qual sai a CPU de agora; sem ela, vale a média desde que o dwnvr subiu.
function processInfo(
  p: ServerProcess,
  cores: number,
  limitBytes: number | undefined,
  previous: ServerProcess | undefined,
) {
  const items: CardItem[] = [];
  if (p.cpuMs != null && p.vivoMs > 0) {
    // Leitura de antes da última subida do dwnvr não serve: o tempo de CPU
    // recomeçou do zero.
    const a =
      previous?.cpuMs != null && p.vivoMs > previous.vivoMs && p.cpuMs >= previous.cpuMs
        ? { cpuMs: previous.cpuMs, vivoMs: previous.vivoMs }
        : null;
    const used = a ? (p.cpuMs - a.cpuMs) / (p.vivoMs - a.vivoMs) : p.cpuMs / p.vivoMs;
    const when = a ? `últimos ${duration(p.vivoMs - a.vivoMs)}` : 'média desde que subiu';
    // Em fração da máquina inteira, e não de um núcleo, para comparar direto
    // com a carga e a pressão ao lado.
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

// O limite do container vai na linha de memória do processo, que é quem bate
// nele; aqui ele só aparece se essa linha faltar.
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
    // Swap usada por si só não é problema - o kernel manda para lá o que
    // ninguém usa. Metade cheia é que já é memória faltando de verdade.
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

// smtp é a seção smtp como ficou valendo; recusada, só o motivo. A conferência
// de verdade é o e-mail de teste, logo abaixo no card.
function smtp(s: NonNullable<Server['smtp']>): CardItem[] {
  if (s.error) return [{ label: 'situação', value: `desligado: ${s.error}`, alert: true }];
  return [
    { label: 'servidor', value: `${s.host ?? '?'}:${s.port ?? '?'}` },
    { label: 'segurança', value: SAFETY[s.security ?? ''] ?? s.security ?? '?' },
    { label: 'remetente', value: s.from ?? '?' },
  ];
}

// meio são os envios de um meio do alerta desde que o dwnvr subiu. O último vai
// no relógio do servidor, como o "Coletado em" do card.
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

// grupos monta o card. Grupo sem fonte - kernel sem PSI, servidor fora do
// Linux, instalação sem detector de objetos - some em vez de aparecer vazio.
// `anterior` é a leitura de antes, para a CPU do processo.
export function groups(info: Server, previous: Server | null): CardGroup[] {
  const limit = info.memoria?.limiteBytes;
  const out: CardGroup[] = [{ title: 'Máquina', items: machine(info.maquina) }];
  // Servidor anterior a este campo não manda o processo.
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
  // Um grupo por meio: o e-mail do servidor, acima, não é o meio do alerta.
  for (const m of info.alertas ?? []) {
    out.push({
      title: `Alertas ${alertNotifier(m.nome)}`,
      items: middle(m, info.coletadoEm?.offsetSeconds ?? 0),
    });
  }
  return out;
}

// coletadoEm diz quando o servidor fez a leitura, no relógio e no fuso DELE,
// como o "horário do servidor" da faixa do topo: convertido para o fuso de quem
// lê, esconderia um servidor com fuso errado. Servidor anterior a este campo
// não manda nada.
//
// Os espaços de dentro da data e do fuso são inquebráveis: sem caber numa
// linha, no celular, a quebra cai entre a hora e o fuso, e não no meio de um.
export function collectedAt(c: Server['coletadoEm']) {
  if (!c) return '';
  const lido = Date.parse(c.em);
  if (isNaN(lido)) return '';
  const wholeNumber = (s: string) => s.replaceAll(' ', ' ');
  return `${wholeNumber(`Coletado em ${timezoneClock(lido, c.offsetSeconds)}`)} ${wholeNumber(c.zona ?? c.sigla)}`;
}

// linhaDoLog formata como o docker logs mostraria, com a hora local de quem lê.
export function logLine(l: LogLine) {
  const hour = new Date(l.em).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'medium' });
  return `${hour} ${l.nivel} ${l.texto}`;
}
