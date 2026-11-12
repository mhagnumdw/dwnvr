// Controles por cima do vídeo, na tela cheia, que somem sozinhos depois de um
// tempo sem toque nem movimento do mouse, e voltam com o próximo. Quem usa
// chama `mostrar` a cada toque e diz, pelo `podeSumir`, se naquele instante
// eles podem sumir: o vídeo pausado das gravações e o menu aberto do Ao vivo
// seguram os controles à vista. A pergunta é feita na hora do sumiço, e não
// na do toque.
//
// No celular o toque simples na imagem também os esconde, como no YouTube, e
// aí vale mesmo com o vídeo pausado: quem toca para ver a imagem limpa quer
// ela limpa. O `pointerdown` já os mostrou quando o toque simples se confirma,
// por isso a pergunta é se estavam à vista ANTES dele (`tocou`).

// Quanto os controles ficam à vista depois do último toque.
export const CONTROLS_VANISH_MS = 3000;

export function vanishingControls(canVanish: () => boolean) {
  let visibleOnes = $state(true);
  let timer: number | undefined;
  let visibleOnTouch = false;

  return {
    get visibleOnes() {
      return visibleOnes;
    },
    // Mostra e rearma o sumiço.
    show() {
      visibleOnes = true;
      clearTimeout(timer);
      timer = setTimeout(() => {
        if (canVanish()) visibleOnes = false;
      }, CONTROLS_VANISH_MS);
    },
    // No `pointerdown` de um toque, no lugar do `mostrar`.
    touched() {
      visibleOnTouch = visibleOnes;
      this.show();
    },
    // O toque simples se confirmou: some o que já estava à vista antes dele, e
    // o que estava escondido fica à vista, como o `tocou` deixou.
    singleTouch() {
      if (!visibleOnTouch) return;
      clearTimeout(timer);
      visibleOnes = false;
    },
    // Para quem sai da tela: o sumiço pendente não dispara mais.
    stop() {
      clearTimeout(timer);
    },
  };
}
