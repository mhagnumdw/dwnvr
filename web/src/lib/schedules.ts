// Os horários do alerta como a tela os edita, e a ida e a volta do servidor.
// O formulário da câmera usa isto ao abrir e ao salvar, e o card da câmera
// resume o alerta com os nomes dos dias, e por isso vem no bundle principal; a
// grade, que só a aba Alerta usa, fica no gradeDaSemana.ts, no chunk dela.
//
// A tela edita o horário em minutos do dia, com as DUAS pontas incluídas (o
// dia inteiro é 00:00 até 23:59), e o servidor guarda "HH:MM". Nenhum horário
// atravessa a meia-noite: o `to` é MAIOR que o `from`, nunca igual. A noite são
// dois horários, um até 23:59 e outro a partir de 00:00 do dia seguinte.
import type { AlertSchedule } from './api.ts';
import { hourOfMinute } from './format.ts';

export const DAY = 1440;

// O mesmo teto do internal/config/params.go (MaxAlertSchedules).
export const MAX_SCHEDULES = 84;

// Um horário como a tela o edita: as horas são nulas com o campo vazio. A
// chave segura a linha da lista no lugar enquanto se digita; o horário que a
// grade refaz ganha chave nova, e a linha nasce de novo com os valores dele.
export interface Schedule {
  key: number;
  days: number[];
  from: number | null;
  to: number | null;
}

let lastKey = 0;
export const newSchedule = (
  days: number[] = [],
  from: number | null = null,
  to: number | null = null,
): Schedule => ({
  key: ++lastKey,
  days,
  from,
  to,
});

// Válido: com dia, com as duas horas e o `to` depois do `from`.
export const valid = (h: Schedule): h is Schedule & { from: number; to: number } =>
  h.days.length > 0 && h.from !== null && h.to !== null && h.to > h.from;

// "HH:MM" em minutos do dia, como o MinuteOfDay do servidor.
function textMinute(s: string): number | null {
  const m = /^(\d\d):(\d\d)$/.exec(s);
  const hour = Number(m?.[1]);
  const minute = Number(m?.[2]);
  return hour <= 23 && minute <= 59 ? hour * 60 + minute : null;
}

export function fromServer(hs: AlertSchedule[] | undefined): Schedule[] {
  return (hs ?? []).map((h) => newSchedule([...h.days], textMinute(h.from), textMinute(h.to)));
}

// Só os válidos vão: quem confere o que falta é o salvar.
export function toServer(hs: Schedule[]): AlertSchedule[] {
  return hs.filter(valid).map((h) => ({
    days: [...h.days].sort((a, b) => a - b),
    from: hourOfMinute(h.from),
    to: hourOfMinute(h.to),
  }));
}

export const DIAS = ['dom', 'seg', 'ter', 'qua', 'qui', 'sex', 'sáb'];
export const LONG_DAYS = ['Domingo', 'Segunda', 'Terça', 'Quarta', 'Quinta', 'Sexta', 'Sábado'];

// "seg" e "Segunda", pelo número do dia.
export const dayName = (d: number, long = false) => (long ? LONG_DAYS : DIAS)[d] ?? '';

function joinWithAnd(parts: string[]) {
  return parts.length > 1 ? `${parts.slice(0, -1).join(', ')} e ${parts.at(-1)}` : (parts[0] ?? '');
}

// "seg a sex", "dom e sáb", "seg, qua e sex", "todo dia".
export function daysSummary(days: number[]) {
  const ds = [...new Set(days)].sort((a, b) => a - b);
  if (!ds.length) return 'nenhum dia';
  if (ds.length === 7) return 'todo dia';
  const seqs: [number, number][] = [];
  for (const d of ds) {
    const s = seqs.at(-1);
    if (s && d === s[1] + 1) s[1] = d;
    else seqs.push([d, d]);
  }
  const parts = seqs.flatMap(([a, b]) =>
    b - a >= 2
      ? [`${dayName(a)} a ${dayName(b)}`]
      : b > a
        ? [dayName(a), dayName(b)]
        : [dayName(a)],
  );
  return joinWithAnd(parts);
}

export function hoursSummary(from: number, to: number) {
  if (from === 0 && to === DAY - 1) return 'dia inteiro';
  return `${hourOfMinute(from)} às ${hourOfMinute(to)}`;
}
