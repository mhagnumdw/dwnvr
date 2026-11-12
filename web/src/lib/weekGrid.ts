// A grade da semana dos horários do alerta, sem DOM: só a aba Alerta usa, e
// vem no chunk dela.
//
// A grade trabalha sobre a "cobertura": um byte por minuto da semana (10080),
// domingo 00:00 primeiro. Horário vira cobertura sem perda; a volta gera a
// lista canônica, que é o que a tela mostra depois de pintar.
import { DAY, newSchedule, valid } from './schedules.ts';
import type { Schedule } from './schedules.ts';
// Os nomes dos dias e os resumos moram em horarios.ts, porque o card da
// câmera também os mostra; seguem saindo daqui para quem já os pedia.
export { DIAS, LONG_DAYS, dayName, hoursSummary, daysSummary } from './schedules.ts';

const WEEK = 7 * DAY;

export type Coverage = Uint8Array;

export function coverage(hs: Schedule[]): Coverage {
  const c = new Uint8Array(WEEK);
  for (const h of hs) {
    if (!valid(h)) continue;
    for (const d of h.days) c.fill(1, d * DAY + h.from, d * DAY + h.to + 1);
  }
  return c;
}

// As sequências marcadas entre dois minutos da semana, [a, b] relativos ao
// início, com as duas pontas incluídas.
function sequences(c: Coverage, start: number, size: number): [number, number][] {
  const rs: [number, number][] = [];
  let a = -1;
  for (let m = 0; m < size; m++) {
    const on = c[start + m];
    if (on && a < 0) a = m;
    if (!on && a >= 0) {
      rs.push([a, m - 1]);
      a = -1;
    }
  }
  if (a >= 0) rs.push([a, size - 1]);
  return rs;
}

// A lista canônica: cada sequência marcada de um dia é um horário, e os dias
// com o mesmo horário viram um horário só. A sequência de um minuto só (o `to`
// igual ao `from`) não é horário válido e fica de fora. Ela sobra quando
// "12:30 até 13:00" perde a hora 12 na grade: o minuto 13:00 fica sozinho.
export function schedulesOf(c: Coverage): Schedule[] {
  const groups = new Map<string, Schedule>();
  for (let d = 0; d < 7; d++)
    for (const [a, b] of sequences(c, d * DAY, DAY)) {
      if (b === a) continue;
      const k = `${a}-${b}`;
      const g = groups.get(k) ?? newSchedule([], a, b);
      g.days.push(d);
      groups.set(k, g);
    }
  // Na ordem do primeiro dia, e no mesmo dia, da hora.
  return [...groups.values()].sort(
    (x, y) => (x.days[0] ?? 0) - (y.days[0] ?? 0) || (x.from ?? 0) - (y.from ?? 0),
  );
}

// Marca (ou desmarca) as horas inteiras do retângulo entre dois dias e duas
// horas, em qualquer ordem.
export function paintHours(
  c: Coverage,
  d0: number,
  h0: number,
  d1: number,
  h1: number,
  mark: boolean,
): Coverage {
  const n = c.slice();
  for (let d = Math.min(d0, d1); d <= Math.max(d0, d1); d++)
    for (let h = Math.min(h0, h1); h <= Math.max(h0, h1); h++)
      n.fill(mark ? 1 : 0, d * DAY + h * 60, d * DAY + h * 60 + 60);
  return n;
}

// Os pedaços marcados de uma hora, em minutos 0..59 com as duas pontas.
export const clipsOfHour = (c: Coverage, d: number, h: number) =>
  sequences(c, d * DAY + h * 60, 60);

// A hora inteira marcada é um pedaço só, do minuto 0 ao 59.
export const whole = (ps: [number, number][]) =>
  ps.length === 1 && ps[0]?.[0] === 0 && ps[0][1] === 59;

export const fullHour = (c: Coverage, d: number, h: number) => whole(clipsOfHour(c, d, h));
