// Duplo toque no vídeo, como no YouTube do celular. O vídeo é dividido em três
// terços: nas laterais o duplo toque pula para trás ou para a frente, e cada
// toque a mais no mesmo lado, logo em seguida, pula de novo; no meio ele
// alterna a tela cheia. Sem `pular`, o vídeo é um terço só, o do meio.
//
// O toque simples só se confirma depois de DUPLO_MS sem outro toque: até lá
// ele pode ser o primeiro de um duplo. O toque que entra na sequência de pulos
// não é simples. Qualquer toque que comece antes disso, até num botão, o
// cancela: quem usa chama `cancelar` no `pointerdown` de tudo.
//
// O detector é próprio, e não o `dblclick` do navegador: o Chrome do Android
// gera um `dblclick` por par de toques, mas não os toques seguidos da
// sequência. Por isso ele só ouve o toque; o mouse continua no `dblclick`.

// Dois toques mais próximos que isso, no tempo e na tela, formam um duplo toque.
const DOUBLE_MS = 300;
const DOUBLE_PX = 48;
// Por quanto tempo, depois de um pulo, mais um toque no mesmo lado soma outro.
// A meia-lua que mostra a soma fica à vista por esse mesmo tempo.
export const SEQUENCE_MS = 700;

export type Side = -1 | 1;

export function doubleTouch(actions: {
  jump?(side: Side): void;
  middle?(): void;
  simples?(): void;
}) {
  let previous: { t: number; x: number; y: number } | null = null;
  let sequence: { side: Side; t: number } | null = null;
  let simplesTimer: number | undefined;

  const onRelease = (e: PointerEvent) => {
    if (e.pointerType !== 'touch') return;
    // O toque num botão por cima do vídeo é do botão.
    if (e.target instanceof Element && e.target.closest('button')) return;
    if (!(e.currentTarget instanceof HTMLElement)) return;

    clearTimeout(simplesTimer);
    const r = e.currentTarget.getBoundingClientRect();
    const fraction = (e.clientX - r.left) / r.width;
    const side = !actions.jump ? 0 : fraction < 1 / 3 ? -1 : fraction > 2 / 3 ? 1 : 0;
    const t = e.timeStamp;

    if (sequence?.side === side && t - sequence.t < SEQUENCE_MS) {
      sequence.t = t;
      actions.jump?.(side);
      return;
    }
    sequence = null;

    const double =
      previous &&
      t - previous.t < DOUBLE_MS &&
      Math.hypot(e.clientX - previous.x, e.clientY - previous.y) < DOUBLE_PX;
    if (!double) {
      previous = { t, x: e.clientX, y: e.clientY };
      if (actions.simples) simplesTimer = setTimeout(() => actions.simples?.(), DOUBLE_MS);
      return;
    }

    previous = null;
    if (side === 0) {
      actions.middle?.();
    } else {
      sequence = { side, t };
      actions.jump?.(side);
    }
  };

  return Object.assign(onRelease, {
    cancel() {
      clearTimeout(simplesTimer);
    },
  });
}
