// A dica do toque longo, como nos apps do Android: segurar o dedo num botão
// mostra o que ele faz, e soltar depois disso não o aciona. No desktop quem
// faz esse papel é o `title`, que o navegador do celular não mostra.
//
// O texto é o `aria-label` do botão, ou o `title`: o que o leitor de tela já
// lê. Botão sem nenhum dos dois não tem dica. Vale para o app inteiro, com um
// ouvinte só no document, instalado pelo App.
//
// O mesmo toque longo vale para quem o pede com `data-toque-longo`, com dica
// ou sem: o elemento recebe o evento `toquelongo` no mesmo instante em que a
// dica abriria. É a miniatura da área pequena, que amplia de novo (o
// MiniaturaDasAreas). Um gesto só, com o mesmo tempo e a mesma tolerância da
// dica, em vez de dois relógios disputando o dedo.

// Quanto o dedo precisa ficar parado para a dica abrir: o toque longo do
// Android. E quanto ele pode andar sem virar rolagem.
const HOLD_MS = 500;
const TOLERANCE_PX = 10;
// Quanto a dica fica à vista depois de o dedo sair.
const FICA_MS = 1500;
const VAO_PX = 10;

const TARGET = 'button, a[href]';
const LISTENER = '[data-toque-longo]';
export const LONG_TOUCH = 'toquelongo';

function textOf(el: Element) {
  // O aria-label vazio também cai para o title.
  const t = [el.getAttribute('aria-label'), el.getAttribute('title')].find(Boolean) ?? '';
  return t ? t.charAt(0).toUpperCase() + t.slice(1) : '';
}

export function installTouchHint() {
  let timer: number | undefined;
  let someTimer: number | undefined;
  let target: Element | null = null;
  let listener: Element | null = null;
  // O de quem pediu o toque longo neste toque, mesmo depois do aviso: o
  // `contextmenu` que chega atrasado também não abre o menu do sistema nele.
  // O mouse e o teclado o zeram: o clique direito e a tecla de menu, depois
  // de um toque, abrem o menu de sempre.
  let touchListener: Element | null = null;
  let origin = { x: 0, y: 0 };
  // A dica abriu neste toque: o clique que vem ao soltar é engolido.
  let didOpen = false;
  let hint: HTMLDivElement | null = null;

  function showHint(el: Element) {
    const text = textOf(el);
    if (!text) return;
    didOpen = true;
    // O Safari não tem vibração: lá a dica só aparece.
    if ('vibrate' in navigator) navigator.vibrate(10);
    hint ??= Object.assign(document.createElement('div'), { className: 'touch-hint' });
    hint.setAttribute('role', 'tooltip');
    hint.textContent = text;
    // Dentro da tela cheia, quando ela existe: fora dela a dica não aparece.
    (document.fullscreenElement ?? document.body).append(hint);
    hint.hidden = false;
    clearTimeout(someTimer);

    // Acima do botão, onde o dedo não cobre; embaixo, se não couber.
    const r = el.getBoundingClientRect();
    const d = hint.getBoundingClientRect();
    const x = Math.min(innerWidth - d.width - 8, Math.max(8, r.left + r.width / 2 - d.width / 2));
    const above = r.top - d.height - VAO_PX;
    hint.style.left = `${x}px`;
    hint.style.top = `${above >= 8 ? above : r.bottom + VAO_PX}px`;
  }

  function abort() {
    clearTimeout(timer);
    target = null;
    listener = null;
  }

  // O toque longo chegou: a dica, se o botão tem, e o aviso a quem o pediu.
  function held() {
    if (target) showHint(target);
    listener?.dispatchEvent(new CustomEvent(LONG_TOUCH));
    listener = null;
  }

  const onTouch = (e: PointerEvent) => {
    if (e.pointerType !== 'touch') {
      touchListener = null;
      return;
    }
    const from = e.target instanceof Element ? e.target : null;
    const el = from?.closest(TARGET) ?? null;
    didOpen = false;
    target = el && textOf(el) ? el : null;
    listener = from?.closest(LISTENER) ?? null;
    touchListener = listener;
    if (!target && !listener) return;
    origin = { x: e.clientX, y: e.clientY };
    clearTimeout(timer);
    timer = setTimeout(held, HOLD_MS);
  };
  const onMove = (e: PointerEvent) => {
    if (
      (target || listener) &&
      Math.hypot(e.clientX - origin.x, e.clientY - origin.y) > TOLERANCE_PX
    )
      abort();
  };
  const onRelease = () => {
    abort();
    if (!didOpen || !hint) return;
    someTimer = setTimeout(() => {
      if (hint) hint.hidden = true;
    }, FICA_MS);
  };
  // Na fase de captura, antes do botão: depois da dica, soltar o dedo não faz
  // o que o botão faz.
  const onClick = (e: MouseEvent) => {
    if (!didOpen) return;
    didOpen = false;
    e.preventDefault();
    e.stopImmediatePropagation();
  };
  // A tecla de menu depois de um toque: o menu de sempre abre.
  const onKey = () => {
    touchListener = null;
  };
  // O `contextmenu` é o próprio toque longo para o navegador, e pode chegar
  // antes do nosso tempo: a dica abre com ele. E o menu do sistema (abrir o
  // link, copiar), que abriria no mesmo toque, não abre. Só no toque: o
  // clique direito e a tecla de menu continuam abrindo o menu de sempre.
  const onMenu = (e: Event) => {
    const from = e.target instanceof Element ? e.target : null;
    const el = from?.closest(TARGET) ?? null;
    const ofListener = !!touchListener && from?.closest(LISTENER) === touchListener;
    if (!ofListener && (!el || (target !== el && !didOpen))) return;
    e.preventDefault();
    if ((ofListener && listener) || (target === el && !didOpen)) {
      clearTimeout(timer);
      held();
    }
  };

  document.addEventListener('pointerdown', onTouch);
  document.addEventListener('pointermove', onMove);
  document.addEventListener('pointerup', onRelease);
  // O navegador tomou o toque para si (rolagem): a dica, se abriu, some como
  // ao soltar. Não vem clique depois, e o `abriu` que sobra o próximo toque
  // zera.
  document.addEventListener('pointercancel', onRelease);
  document.addEventListener('click', onClick, true);
  document.addEventListener('contextmenu', onMenu);
  document.addEventListener('keydown', onKey, true);

  return () => {
    abort();
    clearTimeout(someTimer);
    hint?.remove();
    document.removeEventListener('pointerdown', onTouch);
    document.removeEventListener('pointermove', onMove);
    document.removeEventListener('pointerup', onRelease);
    document.removeEventListener('pointercancel', onRelease);
    document.removeEventListener('click', onClick, true);
    document.removeEventListener('contextmenu', onMenu);
    document.removeEventListener('keydown', onKey, true);
  };
}
