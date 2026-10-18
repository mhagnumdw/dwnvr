// A dica do toque longo, como nos apps do Android: segurar o dedo num botão
// mostra o que ele faz, e soltar depois disso não o aciona. No desktop quem
// faz esse papel é o `title`, que o navegador do celular não mostra.
//
// O texto é o `aria-label` do botão, ou o `title`: o que o leitor de tela já
// lê. Botão sem nenhum dos dois não tem dica. Vale para o app inteiro, com um
// ouvinte só no document, instalado pelo App.

// Quanto o dedo precisa ficar parado para a dica abrir: o toque longo do
// Android. E quanto ele pode andar sem virar rolagem.
const SEGURAR_MS = 500;
const TOLERANCIA_PX = 10;
// Quanto a dica fica à vista depois de o dedo sair.
const FICA_MS = 1500;
const VAO_PX = 10;

const ALVO = 'button, a[href]';

function textoDe(el: Element) {
  // O aria-label vazio também cai para o title.
  const t = [el.getAttribute('aria-label'), el.getAttribute('title')].find(Boolean) ?? '';
  return t ? t.charAt(0).toUpperCase() + t.slice(1) : '';
}

export function instalarDicaDoToque() {
  let timer: number | undefined;
  let someTimer: number | undefined;
  let alvo: Element | null = null;
  let origem = { x: 0, y: 0 };
  // A dica abriu neste toque: o clique que vem ao soltar é engolido.
  let abriu = false;
  let dica: HTMLDivElement | null = null;

  function mostra(el: Element) {
    const texto = textoDe(el);
    if (!texto) return;
    abriu = true;
    // O Safari não tem vibração: lá a dica só aparece.
    if ('vibrate' in navigator) navigator.vibrate(10);
    dica ??= Object.assign(document.createElement('div'), { className: 'dica-do-toque' });
    dica.setAttribute('role', 'tooltip');
    dica.textContent = texto;
    // Dentro da tela cheia, quando ela existe: fora dela a dica não aparece.
    (document.fullscreenElement ?? document.body).append(dica);
    dica.hidden = false;
    clearTimeout(someTimer);

    // Acima do botão, onde o dedo não cobre; embaixo, se não couber.
    const r = el.getBoundingClientRect();
    const d = dica.getBoundingClientRect();
    const x = Math.min(innerWidth - d.width - 8, Math.max(8, r.left + r.width / 2 - d.width / 2));
    const acima = r.top - d.height - VAO_PX;
    dica.style.left = `${x}px`;
    dica.style.top = `${acima >= 8 ? acima : r.bottom + VAO_PX}px`;
  }

  function cancela() {
    clearTimeout(timer);
    alvo = null;
  }

  const aoTocar = (e: PointerEvent) => {
    if (e.pointerType !== 'touch') return;
    const el = e.target instanceof Element ? e.target.closest(ALVO) : null;
    abriu = false;
    if (!el || !textoDe(el)) return;
    alvo = el;
    origem = { x: e.clientX, y: e.clientY };
    clearTimeout(timer);
    timer = setTimeout(() => {
      if (alvo) mostra(alvo);
    }, SEGURAR_MS);
  };
  const aoMover = (e: PointerEvent) => {
    if (alvo && Math.hypot(e.clientX - origem.x, e.clientY - origem.y) > TOLERANCIA_PX) cancela();
  };
  const aoSoltar = () => {
    cancela();
    if (!abriu || !dica) return;
    someTimer = setTimeout(() => {
      if (dica) dica.hidden = true;
    }, FICA_MS);
  };
  // Na fase de captura, antes do botão: depois da dica, soltar o dedo não faz
  // o que o botão faz.
  const aoClicar = (e: MouseEvent) => {
    if (!abriu) return;
    abriu = false;
    e.preventDefault();
    e.stopImmediatePropagation();
  };
  // O `contextmenu` é o próprio toque longo para o navegador, e pode chegar
  // antes do nosso tempo: a dica abre com ele. E o menu do sistema (abrir o
  // link, copiar), que abriria no mesmo toque, não abre. Só no toque: o
  // clique direito e a tecla de menu continuam abrindo o menu de sempre.
  const aoMenu = (e: Event) => {
    const el = e.target instanceof Element ? e.target.closest(ALVO) : null;
    if (!el || (alvo !== el && !abriu)) return;
    e.preventDefault();
    if (alvo === el && !abriu) {
      clearTimeout(timer);
      mostra(el);
    }
  };

  document.addEventListener('pointerdown', aoTocar);
  document.addEventListener('pointermove', aoMover);
  document.addEventListener('pointerup', aoSoltar);
  // O navegador tomou o toque para si (rolagem): a dica, se abriu, some como
  // ao soltar. Não vem clique depois, e o `abriu` que sobra o próximo toque
  // zera.
  document.addEventListener('pointercancel', aoSoltar);
  document.addEventListener('click', aoClicar, true);
  document.addEventListener('contextmenu', aoMenu);

  return () => {
    cancela();
    clearTimeout(someTimer);
    dica?.remove();
    document.removeEventListener('pointerdown', aoTocar);
    document.removeEventListener('pointermove', aoMover);
    document.removeEventListener('pointerup', aoSoltar);
    document.removeEventListener('pointercancel', aoSoltar);
    document.removeEventListener('click', aoClicar, true);
    document.removeEventListener('contextmenu', aoMenu);
  };
}
