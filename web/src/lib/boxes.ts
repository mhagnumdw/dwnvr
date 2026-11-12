// As caixas do detector de objetos sobre o vídeo das gravações: quanto tempo
// cada uma fica acesa, onde o rótulo dela vai e como tudo é desenhado.
//
// Em canvas, e não em DOM, por dois motivos: é o MESMO desenho que vai para a
// imagem baixada - um código só, e a imagem sai igual à tela -, e os ícones das
// famílias já são path de canvas (icones.ts).
//
// O desenho é o retângulo simples, com o rótulo afastado do objeto e ligado a
// ele por um traço, para nenhum rótulo cobrir objeto nenhum.

import type { Box } from './api.ts';
import { family, iconOf, drawIcon, CLASS_NAME } from './icons.ts';
import type { IconName } from './icons.ts';

interface Point {
  x: number;
  y: number;
}

interface Rect extends Point {
  w: number;
  h: number;
}

// O que as caixas leem de uma marca: as do /api/rec/events e as de uma
// detecção servem.
export interface ObjectDetectorMark {
  familia: string;
  classe: string;
  score: number;
  caixa?: Box;
}

// Uma caixa disposta, em pixels do quadro mostrado. `rotulo` e `traco` só
// existem quando foram pedidos e couberam.
export interface PlacedBox<M extends ObjectDetectorMark> {
  mark: M;
  color: string;
  priority: number;
  caixa: Rect;
  label?: Rect & { texto: string; icon: IconName | null };
  stroke?: { from: Point; to: Point };
}

// --- parâmetros -------------------------------------------------------------

// Quanto tempo a caixa fica acesa, em tempo de RELÓGIO e não de vídeo: a caixa
// é de um quadro só, e a 8x 1,5 s de vídeo passariam em 190 ms - ninguém veria.
export const LIT_MS = 1500;
// A entrada é curta para a caixa não chegar atrasada ao quadro que ela
// descreve; a saída é mais longa, para o olho perceber que ela foi embora.
export const LIGHT_UP_MS = 180;
export const FADE_MS = 450;
// Quanto um pulo pode pousar DEPOIS do quadro olhado e ainda acender a caixa.
// Tocar a marca da timeline pula para o onset, e o quadro olhado é o próprio
// onset sempre que o pico não veio depois dele - sem esta folga, a reprodução
// nunca "passaria" por ele.
export const LANDING_MS = 300;

// Quanto a caixa desenhada cresce para cada lado além da caixa do detector de
// objetos, em pixel de TELA - o mesmo em miniatura e em tela cheia. O traço
// fica por dentro da caixa, então com 0 ele cobre a beirada do objeto, e num
// gato de 20 px isso é metade das orelhas. Com 5, sobram 3 px livres em volta.
// Perto da borda a caixa para na imagem: a folga some só daquele lado. 0 volta
// à caixa exata do detector de objetos. A grade da tela Detecções usa o mesmo
// valor.
export const BOX_SLACK_PX = 5;

// A geometria do rótulo.
const OFFSET_PX = 18; // entre o rótulo e a borda da caixa
const SLACK_PX = 4; // entre um rótulo e o que ele está evitando
const FONT = '600 11px system-ui, -apple-system, "Segoe UI", sans-serif';
const LABEL_HEIGHT_PX = 17;
const LABEL_PADDING_PX = 5;
const LABEL_RADIUS_PX = 5;
const ICON_PX = 12;
const ICON_GAP_PX = 4;
const BOX_STROKE_PX = 2;
const DARK = '#0d1117';

// --- disposição -------------------------------------------------------------

// dispoe calcula, em pixels do quadro mostrado (largura x altura), a caixa de
// cada marca, já com a CAIXA_FOLGA_PX, e - com `comRotulo` - onde vai o rótulo
// e o traço que o liga à caixa. Marca sem `caixa` não entra.
//
// `g` só é usado para medir o texto.
export function place<M extends ObjectDetectorMark>(
  g: CanvasRenderingContext2D,
  marks: readonly M[],
  width: number,
  height: number,
  withLabel: boolean,
): PlacedBox<M>[] {
  const fraction = (v: number) => Math.min(1, Math.max(0, v));
  const items = marks.flatMap((m): PlacedBox<M>[] => {
    if (!m.caixa) return [];
    const [x1, y1, x2, y2] = m.caixa;
    const x = Math.max(0, fraction(x1) * width - BOX_SLACK_PX);
    const y = Math.max(0, fraction(y1) * height - BOX_SLACK_PX);
    const xf = Math.min(width, fraction(x2) * width + BOX_SLACK_PX);
    const yf = Math.min(height, fraction(y2) * height + BOX_SLACK_PX);
    return [
      {
        mark: m,
        color: family(m.familia).color,
        priority: family(m.familia).priority,
        caixa: { x, y, w: xf - x, h: yf - y },
      },
    ];
  });
  if (!withLabel) return items;

  g.save();
  g.font = FONT;
  const boxes = items.map((it) => it.caixa);
  const placements: Rect[] = [];
  // Pela prioridade da família: `pessoa` escolhe lugar primeiro, como já
  // acontece com os ícones da timeline.
  for (const it of [...items].sort((a, b) => a.priority - b.priority)) {
    const text = textFor(it.mark);
    const w = LABEL_PADDING_PX * 2 + ICON_PX + ICON_GAP_PX + Math.ceil(g.measureText(text).width);
    const h = LABEL_HEIGHT_PX;

    // O lugar mais PERTO da caixa que caiba inteiro no quadro e não cubra
    // caixa nenhuma nem outro rótulo. Não havendo, o mais perto que caiba.
    let free: { r: Rect; near: number } | null = null;
    let reserve: { r: Rect; near: number } | null = null;
    for (const p of slots(it.caixa, w, h)) {
      if (p.x < 2 || p.y < 2 || p.x + w > width - 2 || p.y + h > height - 2) continue;
      const r = { x: p.x, y: p.y, w, h };
      const near = distance(r, it.caixa);
      if (!reserve || near < reserve.near) reserve = { r, near };
      const roomy = {
        x: r.x - SLACK_PX,
        y: r.y - SLACK_PX,
        w: w + 2 * SLACK_PX,
        h: h + 2 * SLACK_PX,
      };
      if (boxes.some((c) => overlaps(roomy, c)) || placements.some((q) => overlaps(roomy, q)))
        continue;
      if (!free || near < free.near) free = { r, near };
    }
    const label = (free ?? reserve)?.r;
    if (!label) continue; // quadro menor que o próprio rótulo
    placements.push(label);
    it.label = { ...label, texto: text, icon: iconOf(it.mark, true) };
  }

  // O traço, com todos os rótulos postos: o caminho mais curto até a caixa que
  // NÃO atravesse outra caixa nem outro rótulo. Atravessar não é proibido, é
  // caro: num quadro apertado ainda tem que sair traço, e sai o menos ruim.
  for (const it of items) {
    const label = it.label;
    if (!label) continue;
    const obstacles = [
      ...boxes.filter((c) => c !== it.caixa),
      ...placements.filter((q) => q.x !== label.x || q.y !== label.y),
    ];
    let best: { from: Point; to: Point; cost: number } | null = null;
    for (const to of contactPoints(it.caixa)) {
      // A ponta sai da BORDA do rótulo, na direção da caixa: mirando um canto,
      // o traço nasceria solto, no vazio da curvatura.
      const from = border(label, to);
      const cost =
        Math.hypot(to.x - from.x, to.y - from.y) +
        (obstacles.some((e) => crosses(from, to, e)) ? 10_000 : 0);
      if (!best || cost < best.cost) best = { from, to, cost };
    }
    if (best) it.stroke = { from: best.from, to: best.to };
  }
  g.restore();
  return items;
}

function textFor(m: ObjectDetectorMark) {
  const name = CLASS_NAME[m.classe] ?? m.classe;
  return `${name} ${m.score.toFixed(2).replace('.', ',')}`;
}

// Os lugares onde um rótulo pode ficar: dos quatro lados da caixa, deslizando
// ao longo do lado e afastando aos poucos. Ao lado, na altura do objeto,
// costuma ser mais perto do que em cima.
function slots(c: Rect, w: number, h: number) {
  const out: Point[] = [];
  for (let step = 0; step < 8; step++) {
    const d = OFFSET_PX + step * 12;
    for (const disp of [0, -1, 1, -2, 2]) {
      const middle = c.y + c.h / 2 - h / 2 + disp * (h + SLACK_PX);
      out.push({ x: c.x - d - w, y: middle }, { x: c.x + c.w + d, y: middle });
      const center = c.x + c.w / 2 - w / 2 + disp * (w / 2 + SLACK_PX);
      out.push({ x: center, y: c.y - d - h }, { x: center, y: c.y + c.h + d });
    }
  }
  return out;
}

const overlaps = (a: Rect, b: Rect) =>
  a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y;

// Distância entre dois retângulos: zero se eles se tocam.
function distance(a: Rect, b: Rect) {
  const dx = Math.max(b.x - (a.x + a.w), a.x - (b.x + b.w), 0);
  const dy = Math.max(b.y - (a.y + a.h), a.y - (b.y + b.h), 0);
  return Math.hypot(dx, dy);
}

// Onde o traço pode encostar na caixa: o meio de cada lado e os quatro cantos.
const contactPoints = (r: Rect): Point[] => [
  { x: r.x + r.w / 2, y: r.y },
  { x: r.x + r.w / 2, y: r.y + r.h },
  { x: r.x, y: r.y + r.h / 2 },
  { x: r.x + r.w, y: r.y + r.h / 2 },
  { x: r.x, y: r.y },
  { x: r.x + r.w, y: r.y },
  { x: r.x, y: r.y + r.h },
  { x: r.x + r.w, y: r.y + r.h },
];

// O ponto da borda do retângulo na direção de um alvo.
function border(r: Rect, target: Point): Point {
  const cx = r.x + r.w / 2;
  const cy = r.y + r.h / 2;
  const dx = target.x - cx;
  const dy = target.y - cy;
  if (!dx && !dy) return { x: cx, y: cy };
  const t = Math.min(
    dx ? r.w / 2 / Math.abs(dx) : Infinity,
    dy ? r.h / 2 / Math.abs(dy) : Infinity,
  );
  return { x: cx + dx * t, y: cy + dy * t };
}

// O segmento de p a q corta o retângulo? Recorte de Liang-Barsky: o segmento
// vive em t de 0 a 1, e cada lado do retângulo aperta essa faixa. Sobrou
// faixa, cruzou.
function crosses(p: Point, q: Point, r: Rect) {
  let t0 = 0;
  let t1 = 1;
  const dx = q.x - p.x;
  const dy = q.y - p.y;
  const sides: [num: number, den: number][] = [
    [p.x - r.x, -dx],
    [r.x + r.w - p.x, dx],
    [p.y - r.y, -dy],
    [r.y + r.h - p.y, dy],
  ];
  for (const [num, den] of sides) {
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
export function draw<M extends ObjectDetectorMark>(
  g: CanvasRenderingContext2D,
  items: readonly PlacedBox<M>[],
  alpha: (it: PlacedBox<M>) => number = () => 1,
) {
  g.save();
  // A família de menos prioridade primeiro: `pessoa` fica por cima.
  const order = [...items].sort((a, b) => b.priority - a.priority);

  for (const it of order) {
    if (!it.stroke) continue;
    g.globalAlpha = alpha(it);
    const { from, to } = it.stroke;
    // Um fio escuro por baixo do colorido: o traço de 1 px some tanto no muro
    // claro quanto no cinza da noite sem ele.
    const strokes: [width: number, color: string, opacity: number][] = [
      [2.5, '#000', 0.65],
      [1, it.color, 1],
    ];
    for (const [width, color, opacity] of strokes) {
      g.globalAlpha = alpha(it) * opacity;
      g.strokeStyle = color;
      g.lineWidth = width;
      g.lineCap = 'round';
      g.beginPath();
      g.moveTo(from.x, from.y);
      g.lineTo(to.x, to.y);
      g.stroke();
    }
    g.globalAlpha = alpha(it);
    g.beginPath();
    g.arc(to.x, to.y, 2.5, 0, 7);
    g.fillStyle = it.color;
    g.fill();
    g.lineWidth = 1;
    g.strokeStyle = 'rgba(0,0,0,.65)';
    g.stroke();
  }

  for (const it of order) {
    g.globalAlpha = alpha(it);
    const { x, y, w, h } = it.caixa;
    g.strokeStyle = it.color;
    g.lineWidth = BOX_STROKE_PX;
    // Recuado meio traço: a borda fica DENTRO da caixa, como um `border` do CSS.
    const m = BOX_STROKE_PX / 2;
    g.strokeRect(x + m, y + m, Math.max(0, w - 2 * m), Math.max(0, h - 2 * m));
  }

  g.font = FONT;
  g.textBaseline = 'middle';
  for (const it of order) {
    if (!it.label) continue;
    g.globalAlpha = alpha(it);
    const { x, y, w, h, texto: text, icon } = it.label;
    g.beginPath();
    // Safari anterior ao 16 não tem o roundRect.
    if (typeof g.roundRect === 'function') g.roundRect(x, y, w, h, LABEL_RADIUS_PX);
    else g.rect(x, y, w, h);
    g.fillStyle = it.color;
    g.fill();
    g.lineWidth = 1;
    g.strokeStyle = 'rgba(0,0,0,.6)';
    g.stroke();
    if (icon) drawIcon(g, icon, x + LABEL_PADDING_PX, y + (h - ICON_PX) / 2, ICON_PX, DARK);
    g.fillStyle = DARK;
    g.fillText(text, x + LABEL_PADDING_PX + ICON_PX + ICON_GAP_PX, y + h / 2 + 0.5);
  }
  g.restore();
}
