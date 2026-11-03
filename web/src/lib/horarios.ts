// Os horários do alerta como a tela os edita, e a ida e a volta do servidor.
// O formulário da câmera usa isto ao abrir e ao salvar, e por isso vem no
// bundle principal; a grade e os resumos, que só a aba Alerta usa, ficam no
// gradeDaSemana.ts, no chunk dela.
//
// A tela edita o horário em minutos do dia, com as DUAS pontas incluídas (o
// dia inteiro é 00:00 até 23:59), e o servidor guarda "HH:MM". Nenhum horário
// atravessa a meia-noite: o `to` é MAIOR que o `from`, nunca igual. A noite são
// dois horários, um até 23:59 e outro a partir de 00:00 do dia seguinte.
import type { HorarioDoAlerta } from './api.ts';
import { horaDoMinuto } from './format.ts';

export const DIA = 1440;

// O mesmo teto do internal/config/parametros.go (MaxHorariosDoAlerta).
export const MAX_HORARIOS = 84;

// Um horário como a tela o edita: as horas são nulas com o campo vazio. A
// chave segura a linha da lista no lugar enquanto se digita; o horário que a
// grade refaz ganha chave nova, e a linha nasce de novo com os valores dele.
export interface Horario {
  chave: number;
  days: number[];
  from: number | null;
  to: number | null;
}

let ultimaChave = 0;
export const novoHorario = (
  days: number[] = [],
  from: number | null = null,
  to: number | null = null,
): Horario => ({
  chave: ++ultimaChave,
  days,
  from,
  to,
});

// Válido: com dia, com as duas horas e o `to` depois do `from`.
export const valido = (h: Horario): h is Horario & { from: number; to: number } =>
  h.days.length > 0 && h.from !== null && h.to !== null && h.to > h.from;

// "HH:MM" em minutos do dia, como o MinutoDoDia do servidor.
function minutoDoTexto(s: string): number | null {
  const m = /^(\d\d):(\d\d)$/.exec(s);
  const hora = Number(m?.[1]);
  const minuto = Number(m?.[2]);
  return hora <= 23 && minuto <= 59 ? hora * 60 + minuto : null;
}

export function doServidor(hs: HorarioDoAlerta[] | undefined): Horario[] {
  return (hs ?? []).map((h) =>
    novoHorario([...h.days], minutoDoTexto(h.from), minutoDoTexto(h.to)),
  );
}

// Só os válidos vão: quem confere o que falta é o salvar.
export function paraOServidor(hs: Horario[]): HorarioDoAlerta[] {
  return hs.filter(valido).map((h) => ({
    days: [...h.days].sort((a, b) => a - b),
    from: horaDoMinuto(h.from),
    to: horaDoMinuto(h.to),
  }));
}
