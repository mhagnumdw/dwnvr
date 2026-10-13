// Tela cheia de um elemento qualquer da página, e não só do <video>: é ela que
// leva junto os controles e a timeline.
//
// O Safari do iPhone só põe em tela cheia o <video>, e com o player nativo
// dele. Lá, e onde o navegador recusar, a tela cheia é SIMULADA: o elemento
// ganha o atributo `data-tela-cheia-simulada` e cobre a janela por CSS (regra
// no app.css). A barra do navegador continua à vista; o resto da página, não.
//
// Um elemento por vez, como na API do navegador: por isso o estado é do
// módulo, e quem usa compara `telaCheia.el` com o próprio elemento.

const ATRIBUTO = 'data-tela-cheia-simulada';

export const telaCheia = $state<{ el: HTMLElement | null; simulada: boolean }>({
  el: null,
  simulada: false,
});

document.addEventListener('fullscreenchange', () => {
  if (telaCheia.simulada) return;
  telaCheia.el =
    document.fullscreenElement instanceof HTMLElement ? document.fullscreenElement : null;
});

// O Esc da tela cheia de verdade é do navegador; a simulada precisa do seu. O
// `preventDefault` avisa quem ouve depois, como o Modal, que a tecla já foi
// usada: sem ele, o mesmo Esc fecharia a folha onde a tela cheia estava.
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && telaCheia.simulada) {
    e.preventDefault();
    sairDaTelaCheia();
  }
});

function simular(el: HTMLElement) {
  el.setAttribute(ATRIBUTO, '');
  telaCheia.el = el;
  telaCheia.simulada = true;
}

export function entrarEmTelaCheia(el: HTMLElement) {
  // Da tela cheia de verdade para outra o próprio navegador troca; da
  // simulada, não.
  if (telaCheia.simulada) sairDaTelaCheia();
  if (typeof el.requestFullscreen !== 'function') {
    simular(el);
    return;
  }
  el.requestFullscreen().catch(() => {
    simular(el);
  });
}

export function sairDaTelaCheia() {
  if (telaCheia.simulada) {
    telaCheia.el?.removeAttribute(ATRIBUTO);
    telaCheia.el = null;
    telaCheia.simulada = false;
    return;
  }
  if (document.fullscreenElement) void document.exitFullscreen();
}

export function alternarTelaCheia(el: HTMLElement) {
  if (telaCheia.el === el) sairDaTelaCheia();
  else entrarEmTelaCheia(el);
}
