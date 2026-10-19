// A máscara do horário digitado, a do relógio de Gravações e a dos campos de
// horário de Detecções.
//
// Lê o que foi digitado em grupos: hora, minuto e, quando há, segundo. Hora
// com um dígito completa à esquerda (1 = 01h); minuto e segundo com um dígito
// completam à direita (3 = 30), que é o que se quer dizer ao digitar "17:3".
// Os dois-pontos aparecem sozinhos quando o grupo não aceita mais dígito, e
// ponto, vírgula e espaço valem como dois-pontos: o teclado numérico do
// celular não tem ":".

// 3 é HH:MM:SS; 2 é HH:MM.
export type Grupos = 2 | 3;

const cheio = (g: string[], i: number) => {
  const x = g[i] ?? '';
  return x.length === 2 || (i === 0 && x.length === 1 && +x >= 3);
};

// `apagando` segura os dois-pontos automáticos do fim: sem ele, apagar o ":"
// o faria voltar na mesma hora. `recusou` diz que algum dígito ficou de fora,
// para quem usa sacudir o campo.
export function mascara(bruto: string, apagando: boolean, grupos: Grupos) {
  const ultimo = grupos - 1;
  const g = [''];
  let recusou = false;
  for (const c of bruto.replace(/[.,;\s]/g, ':')) {
    let i = g.length - 1;
    if (c === ':') {
      if (g[i] !== '' && i < ultimo) g.push('');
      continue;
    }
    if (c < '0' || c > '9') continue;
    if (cheio(g, i)) {
      if (i === ultimo) {
        recusou = true;
        continue;
      }
      g.push('');
      i++;
    }
    // Dígito que só formaria horário impossível é recusado aqui, e não na
    // hora de ir: "24" ou o "7" de "17:7" (70 min) nunca chegam a aparecer.
    const novo = (g[i] ?? '') + c;
    const ok = i === 0 ? novo.length === 1 || +novo <= 23 : novo.length === 2 || c <= '5';
    if (!ok) {
      recusou = true;
      continue;
    }
    g[i] = novo;
  }
  const i = g.length - 1;
  if (!apagando && i < ultimo && g[i] !== '' && cheio(g, i)) g.push('');
  return { g, recusou };
}

// O segundo do dia que os grupos descrevem, ou nulo sem hora nenhuma.
export function segundosDoDia(g: string[]) {
  if (!g[0]) return null;
  const dezena = (x: string) => (x.length === 1 ? +x * 10 : +x);
  return +g[0] * 3600 + dezena(g[1] ?? '') * 60 + dezena(g[2] ?? '');
}

// O resto do horário em cinza depois do que já foi digitado: "17:3" ganha
// "0:00" (ou "0" sem os segundos), e dá para ver aonde o Enter leva.
export function fantasma(g: string[], grupos: Grupos) {
  if (!g[0]) return '';
  const i = g.length - 1;
  const ultimo = g[i] ?? '';
  let s = ultimo.length === 0 ? '00' : i > 0 && ultimo.length === 1 ? '0' : '';
  for (let k = i + 1; k < grupos; k++) s += ':00';
  return s;
}

// A sacudida do campo quando um dígito é recusado.
export function sacudir(el: HTMLElement | undefined) {
  el?.animate(
    [
      { transform: 'none' },
      { transform: 'translateX(-2px)' },
      { transform: 'translateX(2px)' },
      { transform: 'none' },
    ],
    { duration: 180 },
  );
}
