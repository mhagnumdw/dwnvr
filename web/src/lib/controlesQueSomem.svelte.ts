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
export const CONTROLES_SOMEM_MS = 3000;

export function controlesQueSomem(podeSumir: () => boolean) {
  let visiveis = $state(true);
  let timer: number | undefined;
  let visiveisNoToque = false;

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
    // No `pointerdown` de um toque, no lugar do `mostrar`.
    tocou() {
      visiveisNoToque = visiveis;
      this.mostrar();
    },
    // O toque simples se confirmou: some o que já estava à vista antes dele, e
    // o que estava escondido fica à vista, como o `tocou` deixou.
    toqueSimples() {
      if (!visiveisNoToque) return;
      clearTimeout(timer);
      visiveis = false;
    },
    // Para quem sai da tela: o sumiço pendente não dispara mais.
    parar() {
      clearTimeout(timer);
    },
  };
}
