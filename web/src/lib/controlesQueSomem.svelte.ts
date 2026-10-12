// Controles por cima do vídeo, na tela cheia, que somem sozinhos depois de um
// tempo sem toque nem movimento do mouse, e voltam com o próximo. Quem usa
// chama `mostrar` a cada toque e diz, pelo `podeSumir`, se naquele instante
// eles podem sumir: o vídeo pausado das gravações e o menu aberto do Ao vivo
// seguram os controles à vista. A pergunta é feita na hora do sumiço, e não
// na do toque.

// Quanto os controles ficam à vista depois do último toque.
export const CONTROLES_SOMEM_MS = 3000;

export function controlesQueSomem(podeSumir: () => boolean) {
  let visiveis = $state(true);
  let timer: number | undefined;

  return {
    get visiveis() {
      return visiveis;
    },
    // Mostra e rearma o sumiço.
    mostrar() {
      visiveis = true;
      clearTimeout(timer);
      timer = setTimeout(() => {
        if (podeSumir()) visiveis = false;
      }, CONTROLES_SOMEM_MS);
    },
    // Para quem sai da tela: o sumiço pendente não dispara mais.
    parar() {
      clearTimeout(timer);
    },
  };
}
