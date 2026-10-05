// O que a interface promete de layout, numa largura dada. Fica aqui, e não no
// teste, para o teste seguir em linha reta: a regra com `if` é do apoio.

/** A quebra em que a navegação muda de lugar (web/src/App.svelte). */
export const QUEBRA_DA_NAVEGACAO = 720;

/**
 * Onde a barra de navegação tem que estar: abaixo da quebra, colada embaixo,
 * no alcance do polegar; a partir dela, no topo, dentro do cabeçalho.
 */
export function navegacaoEsperada(viewport: { width: number; height: number }, alturaDaBarra: number) {
  return viewport.width < QUEBRA_DA_NAVEGACAO
    ? { onde: 'colada embaixo', y: viewport.height - alturaDaBarra, folga: 1 }
    : { onde: 'no topo', y: 0, folga: 20 };
}
