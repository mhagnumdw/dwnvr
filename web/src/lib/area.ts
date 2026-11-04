// A área: um polígono desenhado sobre a imagem de uma câmera, com X e Y em
// fração do quadro, de 0 a 1, como a caixa do objeto. Ela vale sobre o quadro
// da detecção e sobre o keyframe do segmento, em qualquer resolução.
//
// Quem decide se um objeto está dentro é o servidor (internal/area): a tela
// só desenha a área e a manda na query.

export type Ponto = [x: number, y: number];

// As regras, com o valor do JSON e da query (en-US), o nome e o ícone na tela.
export const REGRAS = {
  foot: {
    nome: 'pisa',
    icone: 'regraPisa',
    dica: 'conta o pé: o meio da borda de baixo da caixa tem de estar dentro. Desenhe a área no chão.',
  },
  touch: {
    nome: 'encosta',
    icone: 'regraEncosta',
    dica: 'basta a caixa tocar a área. Pega também quem só passa na frente dela.',
  },
} as const;
export type Regra = keyof typeof REGRAS;

// A padrão, a mesma do RegraDaAreaPadrao do servidor.
export const REGRA_PADRAO: Regra = 'foot';

// Os mesmos limites do internal/area/parametros.go (MAX_AREAS é o
// MaxAreasPorCamera).
export const MIN_PONTOS = 3;
export const MAX_PONTOS = 20;
export const MAX_AREAS = 8;

// O zoom da miniatura da área pequena. É pequena a área cujo retângulo tem
// menos de AREA_PEQUENA do quadro na largura e na altura: só com as duas o
// zoom vale a pena, porque a área inteira tem de caber nele (uma faixa
// comprida e fina não deixa ampliar). O enquadramento é o retângulo com
// ZOOM_MARGEM de folga em volta, e amplia no máximo ZOOM_TETO vezes.
export const AREA_PEQUENA = 0.2;
export const ZOOM_MARGEM = 1.6;
export const ZOOM_TETO = 4;

export interface AreaDoFiltro {
  cam: string;
  pontos: Ponto[];
  regra: Regra;
}

const ehRegra = (v: string | null): v is Regra => v !== null && Object.hasOwn(REGRAS, v);

// O `area=` da query: `<cam>:<x>,<y>,...`, em milésimos do quadro. Milésimo
// sobra para o dedo e deixa a URL curta.
export function areaNaQuery(a: AreaDoFiltro) {
  const n = (v: number) => Math.round(Math.min(1, Math.max(0, v)) * 1000);
  return `${a.cam}:${a.pontos.map(([x, y]) => `${n(x)},${n(y)}`).join(',')}`;
}

// A volta: nulo quando o link não traz uma área válida, e aí a tela abre sem
// filtro de área, em vez de mostrar um erro do servidor.
export function areaDaQuery(area: string | null, regra: string | null): AreaDoFiltro | null {
  if (!area) return null;
  const i = area.lastIndexOf(':');
  if (i <= 0) return null;
  const numeros = area
    .slice(i + 1)
    .split(',')
    .map(Number);
  if (numeros.length % 2 || numeros.some((v) => !Number.isInteger(v) || v < 0 || v > 1000))
    return null;
  const pontos: Ponto[] = [];
  for (let j = 0; j < numeros.length; j += 2)
    pontos.push([(numeros[j] ?? 0) / 1000, (numeros[j + 1] ?? 0) / 1000]);
  if (pontos.length < MIN_PONTOS || pontos.length > MAX_PONTOS) return null;
  return { cam: area.slice(0, i), pontos, regra: ehRegra(regra) ? regra : REGRA_PADRAO };
}

// Os pontos como vão para o cameras.json: 4 casas bastam ao dedo (um décimo
// de pixel num quadro de 1080p) e deixam o arquivo legível.
export const pontosDoCadastro = (pontos: readonly Ponto[]) =>
  pontos.map(([x, y]): Ponto => [Math.round(x * 1e4) / 1e4, Math.round(y * 1e4) / 1e4]);

// Os pontos no `points` de um <polygon> com viewBox 0 0 1 1.
export const pontosSVG = (pontos: readonly Ponto[]) =>
  pontos.map(([x, y]) => `${x},${y}`).join(' ');

// O caminho do polígono num canvas já transladado para a imagem, de largura
// `w` e altura `h`.
function caminho(g: CanvasRenderingContext2D, pontos: readonly Ponto[], w: number, h: number) {
  g.beginPath();
  for (const [i, [x, y]] of pontos.entries()) {
    if (i) g.lineTo(x * w, y * h);
    else g.moveTo(x * w, y * h);
  }
  g.closePath();
}

const azul = () => getComputedStyle(document.documentElement).getPropertyValue('--accent').trim();

// O contorno da área num canvas que já está transladado para a imagem, de
// largura `w` e altura `h`: fino e tracejado, como nas miniaturas.
export function desenhaContorno(
  g: CanvasRenderingContext2D,
  pontos: readonly Ponto[],
  w: number,
  h: number,
) {
  if (pontos.length < MIN_PONTOS) return;
  g.save();
  caminho(g, pontos, w, h);
  g.setLineDash([6, 4]);
  g.lineWidth = 1.5;
  g.strokeStyle = azul();
  g.stroke();
  g.restore();
}

// A área cheia: o azul translúcido e o traço contínuo da lista da aba Áreas.
// É o desenho de uma área só, na miniatura dela e realçada no quadro da
// detecção, onde o tracejado fino se perde na imagem.
export function preencheArea(
  g: CanvasRenderingContext2D,
  pontos: readonly Ponto[],
  w: number,
  h: number,
) {
  if (pontos.length < MIN_PONTOS) return;
  g.save();
  caminho(g, pontos, w, h);
  g.fillStyle = 'rgba(47, 129, 247, 0.22)';
  g.fill();
  g.lineWidth = 1.5;
  g.strokeStyle = azul();
  g.stroke();
  g.restore();
}

// Um pedaço do quadro, em frações dele.
export interface Vista {
  x: number;
  y: number;
  w: number;
  h: number;
}

export const QUADRO_INTEIRO: Vista = { x: 0, y: 0, w: 1, h: 1 };

// O pedaço do quadro que enquadra a área pequena, ou nulo se ela não é
// pequena. Largura e altura saem na mesma fração, para a vista manter a
// proporção do quadro; o centro é o da área, puxado para dentro dele.
export function vistaDaArea(pontos: readonly Ponto[]): Vista | null {
  if (pontos.length < MIN_PONTOS) return null;
  const xs = pontos.map(([x]) => x);
  const ys = pontos.map(([, y]) => y);
  const [x0, x1, y0, y1] = [Math.min(...xs), Math.max(...xs), Math.min(...ys), Math.max(...ys)];
  if (x1 - x0 >= AREA_PEQUENA || y1 - y0 >= AREA_PEQUENA) return null;
  const lado = Math.max((x1 - x0) * ZOOM_MARGEM, (y1 - y0) * ZOOM_MARGEM, 1 / ZOOM_TETO);
  const dentro = (c: number) => Math.min(Math.max(c - lado / 2, 0), 1 - lado);
  return { x: dentro((x0 + x1) / 2), y: dentro((y0 + y1) / 2), w: lado, h: lado };
}

// Os pontos da área na vista: frações da vista, e não do quadro.
export const pontosNaVista = (pontos: readonly Ponto[], v: Vista): Ponto[] =>
  pontos.map(([x, y]) => [(x - v.x) / v.w, (y - v.y) / v.h]);
