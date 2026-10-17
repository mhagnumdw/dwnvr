// Duplo toque no vídeo, como no YouTube do celular. O vídeo é dividido em três
// terços: nas laterais o duplo toque pula para trás ou para a frente, e cada
// toque a mais no mesmo lado, logo em seguida, pula de novo; no meio ele
// alterna a tela cheia. O toque simples não faz nada aqui - quem mostra os
// controles é o `pointerdown` de quem usa.
//
// O detector é próprio, e não o `dblclick` do navegador: o Chrome do Android
// gera um `dblclick` por par de toques, mas não os toques seguidos da
// sequência. Por isso ele só ouve o toque; o mouse continua no `dblclick`.

// Dois toques mais próximos que isso, no tempo e na tela, formam um duplo toque.
const DUPLO_MS = 300;
const DUPLO_PX = 48;
// Por quanto tempo, depois de um pulo, mais um toque no mesmo lado soma outro.
// A meia-lua que mostra a soma fica à vista por esse mesmo tempo.
export const SEQUENCIA_MS = 700;

export type Lado = -1 | 1;

export function toqueDuplo(acoes: { pular(lado: Lado): void; meio(): void }) {
  let anterior: { t: number; x: number; y: number } | null = null;
  let sequencia: { lado: Lado; t: number } | null = null;

  return (e: PointerEvent) => {
    if (e.pointerType !== 'touch') return;
    // O toque num botão por cima do vídeo é do botão.
    if (e.target instanceof Element && e.target.closest('button')) return;
    if (!(e.currentTarget instanceof HTMLElement)) return;

    const r = e.currentTarget.getBoundingClientRect();
    const fracao = (e.clientX - r.left) / r.width;
    const lado = fracao < 1 / 3 ? -1 : fracao > 2 / 3 ? 1 : 0;
    const t = e.timeStamp;

    if (sequencia?.lado === lado && t - sequencia.t < SEQUENCIA_MS) {
      sequencia.t = t;
      acoes.pular(lado);
      return;
    }
    sequencia = null;

    const duplo =
      anterior &&
      t - anterior.t < DUPLO_MS &&
      Math.hypot(e.clientX - anterior.x, e.clientY - anterior.y) < DUPLO_PX;
    if (!duplo) {
      anterior = { t, x: e.clientX, y: e.clientY };
      return;
    }

    anterior = null;
    if (lado === 0) {
      acoes.meio();
    } else {
      sequencia = { lado, t };
      acoes.pular(lado);
    }
  };
}
