// Os ícones da interface (botões, menu, setas), todos num viewBox 24×24 para o
// `<Icone>` desenhar com traço `currentColor` e pontas redondas. Os de
// detecção, que são canvas, ficam em `icones.ts`.
//
// São desenhos e não caracteres (◉ ⏱ ⋮ ▶ ⏸ ✕): o caractere muda de forma a cada
// sistema, vira emoji em alguns e não aceita a cor do texto. Cada ícone é a
// lista de peças SVG que o compõem, e não um texto de markup, para o Svelte
// montar os elementos sem `{@html}`.

type Atributos = Readonly<Record<string, string | number>>;
type Peca = readonly [tag: 'path' | 'circle' | 'rect', atributos: Atributos];

const cheio = { fill: 'currentColor', stroke: 'none' } as const;

// Os dois do interruptor "Pausar fora da tela": o glifo é do accent e leva um
// recorte da cor do fundo em volta, para se separar da moldura da tela.
// `style` e não atributos: o `var()` num atributo de apresentação não vale em
// todo navegador.
const glifoDoInterruptor = {
  style: 'fill: var(--accent); stroke: var(--bg); stroke-width: 2; paint-order: stroke',
} as const;
const molduraDaTela: readonly Peca[] = [
  ['rect', { x: 2.5, y: 2.5, width: 15, height: 11, rx: 1.6, 'stroke-width': 1.8 }],
  ['path', { d: 'M5 17h4M5 20.5h3', 'stroke-width': 1.8, 'stroke-dasharray': '1.5 2.5' }],
];

export const ICONES_UI = {
  // Navegação
  aoVivo: [
    ['circle', { cx: 12, cy: 12, r: 2.5, ...cheio }],
    [
      'path',
      {
        d: 'M7.8 7.8a6 6 0 0 0 0 8.4M16.2 7.8a6 6 0 0 1 0 8.4M4.9 4.9a10 10 0 0 0 0 14.2M19.1 4.9a10 10 0 0 1 0 14.2',
      },
    ],
  ],
  relogio: [
    ['circle', { cx: 12, cy: 12, r: 9 }],
    ['path', { d: 'M12 7v5l3 2' }],
  ],
  deteccao: [
    ['rect', { x: 3, y: 3, width: 18, height: 18, rx: 2.5 }],
    ['rect', { x: 8, y: 8, width: 8, height: 8, rx: 1, ...cheio }],
  ],
  lista: [['path', { d: 'M4 6h16M4 12h16M4 18h16' }]],
  usuarios: [
    ['circle', { cx: 9, cy: 8, r: 3.5 }],
    ['path', { d: 'M2.5 20c.8-4 3.3-6 6.5-6s5.7 2 6.5 6' }],
    ['circle', { cx: 17, cy: 9, r: 2.6 }],
    ['path', { d: 'M16 14.2c2.9-.3 5 1.6 5.6 4.8' }],
  ],
  coracao: [
    [
      'path',
      {
        d: 'M12 20s-7.5-4.6-7.5-10.2A4.3 4.3 0 0 1 12 7.2a4.3 4.3 0 0 1 7.5 2.6C19.5 15.4 12 20 12 20z',
      },
    ],
  ],

  // Cabeçalho e avisos
  compartilhar: [
    ['circle', { cx: 18, cy: 5, r: 3 }],
    ['circle', { cx: 6, cy: 12, r: 3 }],
    ['circle', { cx: 18, cy: 19, r: 3 }],
    ['path', { d: 'm8.59 13.51 6.83 3.98' }],
    ['path', { d: 'm15.41 6.51-6.82 3.98' }],
  ],
  sino: [
    ['path', { d: 'M6 9a6 6 0 0 1 12 0c0 5 2 6.5 2 6.5H4S6 14 6 9' }],
    ['path', { d: 'M10.3 19.5a1.9 1.9 0 0 0 3.4 0' }],
  ],
  setaCima: [['path', { d: 'M12 19V5M5 12l7-7 7 7' }]],
  atualizar: [['path', { d: 'M19.5 12a7.5 7.5 0 1 1-2.25-5.4M19.5 3.75v3.75h-3.75' }]],

  // Ao vivo
  camera: [
    ['rect', { x: 2, y: 6, width: 13.5, height: 12, rx: 2 }],
    ['path', { d: 'M15.5 10.5 22 7v10l-6.5-3.5z' }],
  ],
  encaixar: [
    [
      'path',
      {
        d: 'M4 9V5a1 1 0 0 1 1-1h4M15 4h4a1 1 0 0 1 1 1v4M20 15v4a1 1 0 0 1-1 1h-4M9 20H5a1 1 0 0 1-1-1v-4',
      },
    ],
  ],
  telaCheia: [['path', { d: 'M4 9V4h5M15 4h5v5M20 15v5h-5M9 20H4v-5' }]],
  sairTelaCheia: [['path', { d: 'M9 4v5H4M20 9h-5V4M15 20v-5h5M4 15h5v5' }]],
  // A janela grande com a pequena no canto: o desenho que os sistemas usam
  // para PiP.
  pip: [
    ['rect', { x: 3, y: 5, width: 18, height: 14, rx: 2 }],
    ['rect', { x: 12, y: 12, width: 7, height: 5, rx: 1, ...cheio }],
  ],
  // A tela com o vídeo escapando por baixo dela: o ▶ é "toca fora da área
  // visível", e o ❚❚, "pausa lá".
  foraDaTelaToca: [
    ...molduraDaTela,
    ['path', { d: 'M12.6 11.4 23.2 17.3 12.6 23.2z', ...glifoDoInterruptor }],
  ],
  foraDaTelaPausa: [
    ...molduraDaTela,
    ['rect', { x: 13, y: 12, width: 3.4, height: 10.5, rx: 0.6, ...glifoDoInterruptor }],
    ['rect', { x: 18.8, y: 12, width: 3.4, height: 10.5, rx: 0.6, ...glifoDoInterruptor }],
  ],
  chevronEsq: [['path', { d: 'M15 5l-7 7 7 7' }]],
  chevronDir: [['path', { d: 'M9 5l7 7-7 7' }]],
  chevronBaixo: [['path', { d: 'M6 9l6 6 6-6' }]],
  chevronCima: [['path', { d: 'M6 15l6-6 6 6' }]],
  menu: [
    ['circle', { cx: 12, cy: 5, r: 1.7, ...cheio }],
    ['circle', { cx: 12, cy: 12, r: 1.7, ...cheio }],
    ['circle', { cx: 12, cy: 19, r: 1.7, ...cheio }],
  ],

  // Gravações
  fixar: [['path', { d: 'M9 4h6M10 4v6l-3 4h10l-3-4V4M12 14v6' }]],
  play: [['path', { d: 'M7 4.5v15l13-7.5z', ...cheio }]],
  pausa: [
    ['rect', { x: 6, y: 4, width: 4, height: 16, rx: 1, ...cheio }],
    ['rect', { x: 14, y: 4, width: 4, height: 16, rx: 1, ...cheio }],
  ],
  pulaEsq: [['path', { d: 'M20 6l-6 6 6 6M14 6l-6 6 6 6M8 6l-6 6 6 6' }]],
  pulaDir: [['path', { d: 'M4 6l6 6-6 6M10 6l6 6-6 6M16 6l6 6-6 6' }]],
  calendario: [
    ['rect', { x: 3, y: 4.5, width: 18, height: 16.5, rx: 3 }],
    ['path', { d: 'M3 10.5h18M7.5 2.25v4.5M16.5 2.25v4.5' }],
  ],
  // Três quadros lado a lado: a tira de miniaturas.
  miniaturas: [
    ['rect', { x: 2.5, y: 6, width: 5.5, height: 12, rx: 1 }],
    ['rect', { x: 9.25, y: 6, width: 5.5, height: 12, rx: 1 }],
    ['rect', { x: 16, y: 6, width: 5.5, height: 12, rx: 1 }],
  ],
  // A caixa com o rótulo em cima, como o detector as desenha sobre o vídeo. O
  // vão entre os dois é o que o separa de uma pasta.
  caixasRotulo: [
    ['rect', { x: 3.75, y: 10, width: 16.5, height: 10, rx: 1.5 }],
    ['rect', { x: 3, y: 2.5, width: 12, height: 5, rx: 1, ...cheio }],
  ],
  baixar: [['path', { d: 'M12 4v11M7 10.5l5 5 5-5M4.5 19.5h15' }]],
  imagem: [
    ['rect', { x: 3, y: 4.5, width: 18, height: 15, rx: 2 }],
    ['circle', { cx: 8.5, cy: 9.5, r: 1.6 }],
    ['path', { d: 'M21 15.5l-5-5-9 9' }],
  ],
  info: [
    ['circle', { cx: 12, cy: 12, r: 9 }],
    ['path', { d: 'M12 11v5.5' }],
    ['circle', { cx: 12, cy: 7.6, r: 1.1, ...cheio }],
  ],

  // Detecções e formulários
  caixas: [['rect', { x: 3.75, y: 6, width: 16.5, height: 12, rx: 1.5 }]],
  mais: [['path', { d: 'M12 5v14M5 12h14' }]],
  menos: [['path', { d: 'M5 12h14' }]],
  fechar: [['path', { d: 'M6 6l12 12M18 6L6 18' }]],
  olho: [
    ['path', { d: 'M2.25 12S6 5.25 12 5.25 21.75 12 21.75 12 18 18.75 12 18.75 2.25 12 2.25 12z' }],
    ['circle', { cx: 12, cy: 12, r: 3 }],
  ],
  olhoCortado: [
    ['path', { d: 'M2.25 12S6 5.25 12 5.25 21.75 12 21.75 12 18 18.75 12 18.75 2.25 12 2.25 12z' }],
    ['circle', { cx: 12, cy: 12, r: 3 }],
    ['path', { d: 'M3.75 20.25l16.5-16.5' }],
  ],
} as const satisfies Record<string, readonly Peca[]>;

export type NomeDoIconeUi = keyof typeof ICONES_UI;

// Grades 16×16 que viraram 24×24 mantêm o traço em pixels de antes: o `traco`
// de quem usa é o de antes vezes 1,5.
export const TRACO_PADRAO = 2;
