// A grade da semana dos horários do alerta, sem DOM: só a aba Alerta usa, e
// vem no chunk dela.
//
// A grade trabalha sobre a "cobertura": um byte por minuto da semana (10080),
// domingo 00:00 primeiro. Horário vira cobertura sem perda; a volta gera a
// lista canônica, que é o que a tela mostra depois de pintar.
import { horaDoMinuto } from './format.ts';
import { DIA, novoHorario, valido } from './horarios.ts';
import type { Horario } from './horarios.ts';

const SEMANA = 7 * DIA;
export const DIAS = ['dom', 'seg', 'ter', 'qua', 'qui', 'sex', 'sáb'];
export const DIAS_LONGOS = ['Domingo', 'Segunda', 'Terça', 'Quarta', 'Quinta', 'Sexta', 'Sábado'];

export type Cobertura = Uint8Array;

export function cobertura(hs: Horario[]): Cobertura {
  const c = new Uint8Array(SEMANA);
  for (const h of hs) {
    if (!valido(h)) continue;
    for (const d of h.days) c.fill(1, d * DIA + h.from, d * DIA + h.to + 1);
  }
  return c;
}

// As sequências marcadas entre dois minutos da semana, [a, b] relativos ao
// início, com as duas pontas incluídas.
function sequencias(c: Cobertura, inicio: number, tamanho: number): [number, number][] {
  const rs: [number, number][] = [];
  let a = -1;
  for (let m = 0; m < tamanho; m++) {
    const on = c[inicio + m];
    if (on && a < 0) a = m;
    if (!on && a >= 0) {
      rs.push([a, m - 1]);
      a = -1;
    }
  }
  if (a >= 0) rs.push([a, tamanho - 1]);
  return rs;
}

// A lista canônica: cada sequência marcada de um dia é um horário, e os dias
// com o mesmo horário viram um horário só. A sequência de um minuto só (o `to`
// igual ao `from`) não é horário válido e fica de fora. Ela sobra quando
// "12:30 até 13:00" perde a hora 12 na grade: o minuto 13:00 fica sozinho.
export function horariosDa(c: Cobertura): Horario[] {
  const grupos = new Map<string, Horario>();
  for (let d = 0; d < 7; d++)
    for (const [a, b] of sequencias(c, d * DIA, DIA)) {
      if (b === a) continue;
      const k = `${a}-${b}`;
      const g = grupos.get(k) ?? novoHorario([], a, b);
      g.days.push(d);
      grupos.set(k, g);
    }
  // Na ordem do primeiro dia, e no mesmo dia, da hora.
  return [...grupos.values()].sort(
    (x, y) => (x.days[0] ?? 0) - (y.days[0] ?? 0) || (x.from ?? 0) - (y.from ?? 0),
  );
}

// Marca (ou desmarca) as horas inteiras do retângulo entre dois dias e duas
// horas, em qualquer ordem.
export function pintaHoras(
  c: Cobertura,
  d0: number,
  h0: number,
  d1: number,
  h1: number,
  marca: boolean,
): Cobertura {
  const n = c.slice();
  for (let d = Math.min(d0, d1); d <= Math.max(d0, d1); d++)
    for (let h = Math.min(h0, h1); h <= Math.max(h0, h1); h++)
      n.fill(marca ? 1 : 0, d * DIA + h * 60, d * DIA + h * 60 + 60);
  return n;
}

// Os pedaços marcados de uma hora, em minutos 0..59 com as duas pontas.
export const pedacosDaHora = (c: Cobertura, d: number, h: number) =>
  sequencias(c, d * DIA + h * 60, 60);

// A hora inteira marcada é um pedaço só, do minuto 0 ao 59.
export const inteira = (ps: [number, number][]) =>
  ps.length === 1 && ps[0]?.[0] === 0 && ps[0][1] === 59;

export const horaCheia = (c: Cobertura, d: number, h: number) => inteira(pedacosDaHora(c, d, h));

// "seg" e "Segunda", pelo número do dia.
export const nomeDoDia = (d: number, longo = false) => (longo ? DIAS_LONGOS : DIAS)[d] ?? '';

function juntaComE(partes: string[]) {
  return partes.length > 1
    ? `${partes.slice(0, -1).join(', ')} e ${partes.at(-1)}`
    : (partes[0] ?? '');
}

// "seg a sex", "dom e sáb", "seg, qua e sex", "todo dia".
export function resumoDosDias(days: number[]) {
  const ds = [...new Set(days)].sort((a, b) => a - b);
  if (!ds.length) return 'nenhum dia';
  if (ds.length === 7) return 'todo dia';
  const seqs: [number, number][] = [];
  for (const d of ds) {
    const s = seqs.at(-1);
    if (s && d === s[1] + 1) s[1] = d;
    else seqs.push([d, d]);
  }
  const partes = seqs.flatMap(([a, b]) =>
    b - a >= 2
      ? [`${nomeDoDia(a)} a ${nomeDoDia(b)}`]
      : b > a
        ? [nomeDoDia(a), nomeDoDia(b)]
        : [nomeDoDia(a)],
  );
  return juntaComE(partes);
}

export function resumoDasHoras(from: number, to: number) {
  if (from === 0 && to === DIA - 1) return 'dia inteiro';
  return `${horaDoMinuto(from)} às ${horaDoMinuto(to)}`;
}
