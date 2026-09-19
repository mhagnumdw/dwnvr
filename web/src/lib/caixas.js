// As caixas do detector de objetos sobre o vídeo das gravações: quanto tempo
// cada uma fica acesa, onde o rótulo dela vai e como tudo é desenhado.
//
// Em canvas, e não em DOM, por dois motivos: é o MESMO desenho que vai para a
// imagem baixada - um código só, e a imagem sai igual à tela -, e os ícones das
// famílias já são path de canvas (icones.js).
//
// O desenho é o retângulo simples, com o rótulo afastado do objeto e ligado a
// ele por um traço, para nenhum rótulo cobrir objeto nenhum.

import { familia, iconeDa, desenhaIcone, NOME_DA_CLASSE } from './icones.js';

// --- parâmetros -------------------------------------------------------------

// Quanto tempo a caixa fica acesa, em tempo de RELÓGIO e não de vídeo: a caixa
// é de um quadro só, e a 8x 1,5 s de vídeo passariam em 190 ms - ninguém veria.
export const ACESA_MS = 1500;
// A entrada é curta para a caixa não chegar atrasada ao quadro que ela
// descreve; a saída é mais longa, para o olho perceber que ela foi embora.
export const ACENDER_MS = 180;
export const ESMAECER_MS = 450;
// Quanto um pulo pode pousar DEPOIS do quadro olhado e ainda acender a caixa.
// Tocar a marca da timeline pula para o onset, e o quadro olhado é o próprio
// onset sempre que o pico não veio depois dele - sem esta folga, a reprodução
// nunca "passaria" por ele.
export const POUSO_MS = 300;

// A geometria do rótulo.
const AFASTAMENTO_PX = 18; // entre o rótulo e a borda da caixa
const FOLGA_PX = 4;        // entre um rótulo e o que ele está evitando
const FONTE = '600 11px system-ui, -apple-system, "Segoe UI", sans-serif';
const ROTULO_ALTURA_PX = 17;
const ROTULO_PADDING_PX = 5;
const ROTULO_RAIO_PX = 5;
const ICONE_PX = 12;
const ICONE_VAO_PX = 4;
const CAIXA_TRACO_PX = 2;
const ESCURO = '#0d1117';

// --- disposição -------------------------------------------------------------

// dispoe calcula, em pixels do quadro mostrado (largura x altura), a caixa de
// cada marca e - com `comRotulo` - onde vai o rótulo e o traço que o liga à
// caixa. Marca sem `caixa` não entra.
//
// `g` só é usado para medir o texto.
export function dispoe(g, marcas, largura, altura, comRotulo) {
  const itens = marcas
    .filter((m) => m.caixa)
    .map((m) => {
      const [x1, y1, x2, y2] = m.caixa.map((v) => Math.min(1, Math.max(0, v)));
      return {
        marca: m,
        cor: familia(m.familia).cor,
        prioridade: familia(m.familia).prioridade,
        caixa: { x: x1 * largura, y: y1 * altura, w: (x2 - x1) * largura, h: (y2 - y1) * altura },
      };
    });
  if (!comRotulo) return itens;

  g.save();
  g.font = FONTE;
  const caixas = itens.map((it) => it.caixa);
  const postos = [];
  // Pela prioridade da família: `pessoa` escolhe lugar primeiro, como já
  // acontece com os ícones da timeline.
  for (const it of [...itens].sort((a, b) => a.prioridade - b.prioridade)) {
    const texto = textoDo(it.marca);
    const w = ROTULO_PADDING_PX * 2 + ICONE_PX + ICONE_VAO_PX + Math.ceil(g.measureText(texto).width);
    const h = ROTULO_ALTURA_PX;

    // O lugar mais PERTO da caixa que caiba inteiro no quadro e não cubra
    // caixa nenhuma nem outro rótulo. Não havendo, o mais perto que caiba.
    let livre = null;
    let reserva = null;
    for (const p of lugares(it.caixa, w, h)) {
      if (p.x < 2 || p.y < 2 || p.x + w > largura - 2 || p.y + h > altura - 2) continue;
      const r = { x: p.x, y: p.y, w, h };
      const perto = distancia(r, it.caixa);
      if (!reserva || perto < reserva.perto) reserva = { r, perto };
      const folgado = { x: r.x - FOLGA_PX, y: r.y - FOLGA_PX, w: w + 2 * FOLGA_PX, h: h + 2 * FOLGA_PX };
      if (caixas.some((c) => sobrepoe(folgado, c)) || postos.some((q) => sobrepoe(folgado, q))) continue;
      if (!livre || perto < livre.perto) livre = { r, perto };
    }
    const rotulo = (livre ?? reserva)?.r;
    if (!rotulo) continue; // quadro menor que o próprio rótulo
    postos.push(rotulo);
    it.rotulo = { ...rotulo, texto, icone: iconeDa(it.marca, true) };
  }

  // O traço, com todos os rótulos postos: o caminho mais curto até a caixa que
  // NÃO atravesse outra caixa nem outro rótulo. Atravessar não é proibido, é
  // caro: num quadro apertado ainda tem que sair traço, e sai o menos ruim.
  for (const it of itens) {
    if (!it.rotulo) continue;
    const estorvos = [
      ...caixas.filter((c) => c !== it.caixa),
      ...postos.filter((q) => q.x !== it.rotulo.x || q.y !== it.rotulo.y),
    ];
    let melhor = null;
    for (const ate of encostos(it.caixa)) {
      // A ponta sai da BORDA do rótulo, na direção da caixa: mirando um canto,
      // o traço nasceria solto, no vazio da curvatura.
      const de = borda(it.rotulo, ate);
      const custo = Math.hypot(ate.x - de.x, ate.y - de.y) +
        (estorvos.some((e) => cruza(de, ate, e)) ? 10_000 : 0);
      if (!melhor || custo < melhor.custo) melhor = { de, ate, custo };
    }
    it.traco = { de: melhor.de, ate: melhor.ate };
  }
  g.restore();
  return itens;
}

function textoDo(m) {
  const nome = NOME_DA_CLASSE[m.classe] ?? m.classe;
  return `${nome} ${m.score.toFixed(2).replace('.', ',')}`;
}

// Os lugares onde um rótulo pode ficar: dos quatro lados da caixa, deslizando
// ao longo do lado e afastando aos poucos. Ao lado, na altura do objeto,
// costuma ser mais perto do que em cima.
function lugares(c, w, h) {
  const out = [];
  for (let passo = 0; passo < 8; passo++) {
    const d = AFASTAMENTO_PX + passo * 12;
    for (const desl of [0, -1, 1, -2, 2]) {
      const meio = c.y + c.h / 2 - h / 2 + desl * (h + FOLGA_PX);
      out.push({ x: c.x - d - w, y: meio }, { x: c.x + c.w + d, y: meio });
      const centro = c.x + c.w / 2 - w / 2 + desl * (w / 2 + FOLGA_PX);
      out.push({ x: centro, y: c.y - d - h }, { x: centro, y: c.y + c.h + d });
    }
  }
  return out;
}

const sobrepoe = (a, b) =>
  a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y;

// Distância entre dois retângulos: zero se eles se tocam.
function distancia(a, b) {
  const dx = Math.max(b.x - (a.x + a.w), a.x - (b.x + b.w), 0);
  const dy = Math.max(b.y - (a.y + a.h), a.y - (b.y + b.h), 0);
  return Math.hypot(dx, dy);
}

// Onde o traço pode encostar na caixa: o meio de cada lado e os quatro cantos.
const encostos = (r) => [
  { x: r.x + r.w / 2, y: r.y }, { x: r.x + r.w / 2, y: r.y + r.h },
  { x: r.x, y: r.y + r.h / 2 }, { x: r.x + r.w, y: r.y + r.h / 2 },
  { x: r.x, y: r.y }, { x: r.x + r.w, y: r.y },
  { x: r.x, y: r.y + r.h }, { x: r.x + r.w, y: r.y + r.h },
];

// O ponto da borda do retângulo na direção de um alvo.
function borda(r, alvo) {
  const cx = r.x + r.w / 2;
  const cy = r.y + r.h / 2;
  const dx = alvo.x - cx;
  const dy = alvo.y - cy;
  if (!dx && !dy) return { x: cx, y: cy };
  const t = Math.min(dx ? r.w / 2 / Math.abs(dx) : Infinity, dy ? r.h / 2 / Math.abs(dy) : Infinity);
  return { x: cx + dx * t, y: cy + dy * t };
}

// O segmento de p a q corta o retângulo? Recorte de Liang-Barsky: o segmento
// vive em t de 0 a 1, e cada lado do retângulo aperta essa faixa. Sobrou
// faixa, cruzou.
function cruza(p, q, r) {
  let t0 = 0;
  let t1 = 1;
  const dx = q.x - p.x;
  const dy = q.y - p.y;
  for (const [num, den] of [
    [p.x - r.x, -dx], [r.x + r.w - p.x, dx],
    [p.y - r.y, -dy], [r.y + r.h - p.y, dy],
  ]) {
    if (den === 0) {
      if (num < 0) return false;
      continue;
    }
    const t = num / den;
    if (den < 0) {
      if (t > t1) return false;
      if (t > t0) t0 = t;
    } else {
      if (t < t0) return false;
      if (t < t1) t1 = t;
    }
  }
  return true;
}

// --- desenho ----------------------------------------------------------------

// desenha pinta os itens de `dispoe` no contexto, cada um com a opacidade que
// `alfa(item)` disser. A ordem das camadas é fixa - traços, caixas, rótulos -
// para um rótulo nunca ficar embaixo do traço de outro.
export function desenha(g, itens, alfa = () => 1) {
  g.save();
  // A família de menos prioridade primeiro: `pessoa` fica por cima.
  const ordem = [...itens].sort((a, b) => b.prioridade - a.prioridade);

  for (const it of ordem) {
    if (!it.traco) continue;
    g.globalAlpha = alfa(it);
    const { de, ate } = it.traco;
    // Um fio escuro por baixo do colorido: o traço de 1 px some tanto no muro
    // claro quanto no cinza da noite sem ele.
    for (const [largura, cor, opacidade] of [[2.5, '#000', 0.65], [1, it.cor, 1]]) {
      g.globalAlpha = alfa(it) * opacidade;
      g.strokeStyle = cor;
      g.lineWidth = largura;
      g.lineCap = 'round';
      g.beginPath();
      g.moveTo(de.x, de.y);
      g.lineTo(ate.x, ate.y);
      g.stroke();
    }
    g.globalAlpha = alfa(it);
    g.beginPath();
    g.arc(ate.x, ate.y, 2.5, 0, 7);
    g.fillStyle = it.cor;
    g.fill();
    g.lineWidth = 1;
    g.strokeStyle = 'rgba(0,0,0,.65)';
    g.stroke();
  }

  for (const it of ordem) {
    g.globalAlpha = alfa(it);
    const { x, y, w, h } = it.caixa;
    g.strokeStyle = it.cor;
    g.lineWidth = CAIXA_TRACO_PX;
    // Recuado meio traço: a borda fica DENTRO da caixa, como um `border` do CSS.
    const m = CAIXA_TRACO_PX / 2;
    g.strokeRect(x + m, y + m, Math.max(0, w - 2 * m), Math.max(0, h - 2 * m));
  }

  g.font = FONTE;
  g.textBaseline = 'middle';
  for (const it of ordem) {
    if (!it.rotulo) continue;
    g.globalAlpha = alfa(it);
    const { x, y, w, h, texto, icone } = it.rotulo;
    g.beginPath();
    if (g.roundRect) g.roundRect(x, y, w, h, ROTULO_RAIO_PX);
    else g.rect(x, y, w, h);
    g.fillStyle = it.cor;
    g.fill();
    g.lineWidth = 1;
    g.strokeStyle = 'rgba(0,0,0,.6)';
    g.stroke();
    if (icone) desenhaIcone(g, icone, x + ROTULO_PADDING_PX, y + (h - ICONE_PX) / 2, ICONE_PX, ESCURO);
    g.fillStyle = ESCURO;
    g.fillText(texto, x + ROTULO_PADDING_PX + ICONE_PX + ICONE_VAO_PX, y + h / 2 + 0.5);
  }
  g.restore();
}
