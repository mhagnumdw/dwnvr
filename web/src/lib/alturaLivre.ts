// A altura que um elemento pode ter para terminar junto com a janela: do topo
// dele até o fim dela, menos o que fica reservado abaixo dele - o respiro de
// cada pai até o `main`, e o espaço que o `main` guarda para a navegação
// inferior não cobrir o conteúdo. Esse padding do `main` é zero no desktop,
// onde a navegação sobe: lê-lo evita repetir aqui, em JS, o breakpoint que já
// está no CSS.
//
// Medida em JS porque o CSS sozinho não chega nela: o `#app` só tem altura
// mínima, e o cabeçalho, com ou sem a faixa de versão nova, não tem altura fixa.
export function alturaLivre(el: Element, minimo = 80) {
  let reservado = 0;
  const main = el.closest('main');
  for (let p = el.parentElement; p; p = p.parentElement) {
    reservado += parseFloat(getComputedStyle(p).paddingBottom) || 0;
    if (p === main) break;
  }
  return Math.max(minimo, window.innerHeight - el.getBoundingClientRect().top - reservado);
}
