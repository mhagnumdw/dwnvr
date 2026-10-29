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
  g.beginPath();
  for (const [i, [x, y]] of pontos.entries()) {
    if (i) g.lineTo(x * w, y * h);
    else g.moveTo(x * w, y * h);
  }
  g.closePath();
  g.setLineDash([6, 4]);
  g.lineWidth = 1.5;
  g.strokeStyle = getComputedStyle(document.documentElement).getPropertyValue('--accent').trim();
  g.stroke();
  g.restore();
}
