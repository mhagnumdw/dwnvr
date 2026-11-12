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

const ATTRIBUTE = 'data-tela-cheia-simulada';

export const fullscreen = $state<{ el: HTMLElement | null; simulated: boolean }>({
  el: null,
  simulated: false,
});

document.addEventListener('fullscreenchange', () => {
  if (fullscreen.simulated) return;
  fullscreen.el =
    document.fullscreenElement instanceof HTMLElement ? document.fullscreenElement : null;
});

// O Esc da tela cheia de verdade é do navegador; a simulada precisa do seu. O
// `preventDefault` avisa quem ouve depois, como o Modal, que a tecla já foi
// usada: sem ele, o mesmo Esc fecharia a folha onde a tela cheia estava.
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && fullscreen.simulated) {
    e.preventDefault();
    leaveFullscreen();
  }
});

function simulate(el: HTMLElement) {
  el.setAttribute(ATTRIBUTE, '');
  fullscreen.el = el;
  fullscreen.simulated = true;
}

export function enterFullscreen(el: HTMLElement) {
  // Da tela cheia de verdade para outra o próprio navegador troca; da
  // simulada, não.
  if (fullscreen.simulated) leaveFullscreen();
  if (typeof el.requestFullscreen !== 'function') {
    simulate(el);
    return;
  }
  el.requestFullscreen().catch(() => {
    simulate(el);
  });
}

export function leaveFullscreen() {
  if (fullscreen.simulated) {
    fullscreen.el?.removeAttribute(ATTRIBUTE);
    fullscreen.el = null;
    fullscreen.simulated = false;
    return;
  }
  if (document.fullscreenElement) void document.exitFullscreen();
}

export function toggleFullscreen(el: HTMLElement) {
  if (fullscreen.el === el) leaveFullscreen();
  else enterFullscreen(el);
}
