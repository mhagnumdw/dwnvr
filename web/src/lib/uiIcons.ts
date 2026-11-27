// The interface icons (buttons, menu, arrows), all in a 24×24 viewBox for
// `<Icon>` to draw with a `currentColor` stroke and round caps. The detection
// ones, which are canvas, live in `icons.ts`.
//
// They are drawings and not characters (◉ ⏱ ⋮ ▶ ⏸ ✕): a character changes shape
// on every system, turns into an emoji on some and doesn't accept the text
// color. Each icon is the list of SVG pieces that compose it, and not a markup
// string, so Svelte builds the elements without `{@html}`.

type Attributes = Readonly<Record<string, string | number>>;
type Piece = readonly [tag: 'path' | 'circle' | 'rect', attributes: Attributes];

const isFull = { fill: 'currentColor', stroke: 'none' } as const;

// The two for the "Pausar fora da tela" switch: the glyph is the accent's and
// carries a cutout in the background color around it, to separate from the
// screen frame. `style` and not attributes: `var()` in a presentation attribute
// isn't valid in every browser.
const switchGlyph = {
  style: 'fill: var(--accent); stroke: var(--bg); stroke-width: 2; paint-order: stroke',
} as const;
// The area in the rule icons, with the editor polygon's colors: the blue
// doesn't follow the text color, so the area is recognized without a legend.
const editorArea = { style: 'fill: var(--accent); fill-opacity: 0.32; stroke: none' } as const;
const areaBorder = { style: 'stroke: var(--accent)' } as const;
const screenBezel: readonly Piece[] = [
  ['rect', { x: 2.5, y: 2.5, width: 15, height: 11, rx: 1.6, 'stroke-width': 1.8 }],
  ['path', { d: 'M5 17h4M5 20.5h3', 'stroke-width': 1.8, 'stroke-dasharray': '1.5 2.5' }],
];

export const UI_ICONS = {
  // Navigation
  live: [
    ['circle', { cx: 12, cy: 12, r: 2.5, ...isFull }],
    [
      'path',
      {
        d: 'M7.8 7.8a6 6 0 0 0 0 8.4M16.2 7.8a6 6 0 0 1 0 8.4M4.9 4.9a10 10 0 0 0 0 14.2M19.1 4.9a10 10 0 0 1 0 14.2',
      },
    ],
  ],
  clock: [
    ['circle', { cx: 12, cy: 12, r: 9 }],
    ['path', { d: 'M12 7v5l3 2' }],
  ],
  detection: [
    ['rect', { x: 3, y: 3, width: 18, height: 18, rx: 2.5 }],
    ['rect', { x: 8, y: 8, width: 8, height: 8, rx: 1, ...isFull }],
  ],
  list: [['path', { d: 'M4 6h16M4 12h16M4 18h16' }]],
  users: [
    ['circle', { cx: 9, cy: 8, r: 3.5 }],
    ['path', { d: 'M2.5 20c.8-4 3.3-6 6.5-6s5.7 2 6.5 6' }],
    ['circle', { cx: 17, cy: 9, r: 2.6 }],
    ['path', { d: 'M16 14.2c2.9-.3 5 1.6 5.6 4.8' }],
  ],
  heart: [
    [
      'path',
      {
        d: 'M12 20s-7.5-4.6-7.5-10.2A4.3 4.3 0 0 1 12 7.2a4.3 4.3 0 0 1 7.5 2.6C19.5 15.4 12 20 12 20z',
      },
    ],
  ],

  // Header and notices
  share: [
    ['circle', { cx: 18, cy: 5, r: 3 }],
    ['circle', { cx: 6, cy: 12, r: 3 }],
    ['circle', { cx: 18, cy: 19, r: 3 }],
    ['path', { d: 'm8.59 13.51 6.83 3.98' }],
    ['path', { d: 'm15.41 6.51-6.82 3.98' }],
  ],
  bell: [
    ['path', { d: 'M6 9a6 6 0 0 1 12 0c0 5 2 6.5 2 6.5H4S6 14 6 9' }],
    ['path', { d: 'M10.3 19.5a1.9 1.9 0 0 0 3.4 0' }],
  ],
  arrowUp: [['path', { d: 'M12 19V5M5 12l7-7 7 7' }]],
  refresh: [['path', { d: 'M19.5 12a7.5 7.5 0 1 1-2.25-5.4M19.5 3.75v3.75h-3.75' }]],

  // Live
  camera: [
    ['rect', { x: 2, y: 6, width: 13.5, height: 12, rx: 2 }],
    ['path', { d: 'M15.5 10.5 22 7v10l-6.5-3.5z' }],
  ],
  fitToScreen: [
    [
      'path',
      {
        d: 'M4 9V5a1 1 0 0 1 1-1h4M15 4h4a1 1 0 0 1 1 1v4M20 15v4a1 1 0 0 1-1 1h-4M9 20H5a1 1 0 0 1-1-1v-4',
      },
    ],
  ],
  fullscreen: [['path', { d: 'M4 9V4h5M15 4h5v5M20 15v5h-5M9 20H4v-5' }]],
  fullscreenExit: [['path', { d: 'M9 4v5H4M20 9h-5V4M15 20v-5h5M4 15h5v5' }]],
  // The big window with the small one in the corner: the drawing systems use
  // for PiP.
  pip: [
    ['rect', { x: 3, y: 5, width: 18, height: 14, rx: 2 }],
    ['rect', { x: 12, y: 12, width: 7, height: 5, rx: 1, ...isFull }],
  ],
  // The screen with the video escaping below it: ▶ is "plays outside the
  // visible area", and ❚❚ is "pauses there".
  offScreenPlay: [
    ...screenBezel,
    ['path', { d: 'M12.6 11.4 23.2 17.3 12.6 23.2z', ...switchGlyph }],
  ],
  offScreenPause: [
    ...screenBezel,
    ['rect', { x: 13, y: 12, width: 3.4, height: 10.5, rx: 0.6, ...switchGlyph }],
    ['rect', { x: 18.8, y: 12, width: 3.4, height: 10.5, rx: 0.6, ...switchGlyph }],
  ],
  chevronLeft: [['path', { d: 'M15 5l-7 7 7 7' }]],
  chevronDir: [['path', { d: 'M9 5l7 7-7 7' }]],
  chevronDown: [['path', { d: 'M6 9l6 6 6-6' }]],
  chevronUp: [['path', { d: 'M6 15l6-6 6 6' }]],
  menu: [
    ['circle', { cx: 12, cy: 5, r: 1.7, ...isFull }],
    ['circle', { cx: 12, cy: 12, r: 1.7, ...isFull }],
    ['circle', { cx: 12, cy: 19, r: 1.7, ...isFull }],
  ],

  // Recordings
  pin: [['path', { d: 'M9 4h6M10 4v6l-3 4h10l-3-4V4M12 14v6' }]],
  play: [['path', { d: 'M7 4.5v15l13-7.5z', ...isFull }]],
  pause: [
    ['rect', { x: 6, y: 4, width: 4, height: 16, rx: 1, ...isFull }],
    ['rect', { x: 14, y: 4, width: 4, height: 16, rx: 1, ...isFull }],
  ],
  skipLeft: [['path', { d: 'M20 6l-6 6 6 6M14 6l-6 6 6 6M8 6l-6 6 6 6' }]],
  skipRight: [['path', { d: 'M4 6l6 6-6 6M10 6l6 6-6 6M16 6l6 6-6 6' }]],
  calendar: [
    ['rect', { x: 3, y: 4.5, width: 18, height: 16.5, rx: 3 }],
    ['path', { d: 'M3 10.5h18M7.5 2.25v4.5M16.5 2.25v4.5' }],
  ],
  // Three frames side by side: the thumbnail strip.
  thumbnails: [
    ['rect', { x: 2.5, y: 6, width: 5.5, height: 12, rx: 1 }],
    ['rect', { x: 9.25, y: 6, width: 5.5, height: 12, rx: 1 }],
    ['rect', { x: 16, y: 6, width: 5.5, height: 12, rx: 1 }],
  ],
  // The box with the label on top, like the detector draws them over the video.
  // The gap between the two is what sets it apart from a folder.
  boxesLabel: [
    ['rect', { x: 3.75, y: 10, width: 16.5, height: 10, rx: 1.5 }],
    ['rect', { x: 3, y: 2.5, width: 12, height: 5, rx: 1, ...isFull }],
  ],
  download: [['path', { d: 'M12 4v11M7 10.5l5 5 5-5M4.5 19.5h15' }]],
  image: [
    ['rect', { x: 3, y: 4.5, width: 18, height: 15, rx: 2 }],
    ['circle', { cx: 8.5, cy: 9.5, r: 1.6 }],
    ['path', { d: 'M21 15.5l-5-5-9 9' }],
  ],
  info: [
    ['circle', { cx: 12, cy: 12, r: 9 }],
    ['path', { d: 'M12 11v5.5' }],
    ['circle', { cx: 12, cy: 7.6, r: 1.1, ...isFull }],
  ],

  // Detections and forms
  boxes: [['rect', { x: 3.75, y: 6, width: 16.5, height: 12, rx: 1.5 }]],
  // The area: a polygon with three of the vertices marked, as in the editor.
  area: [
    ['path', { d: 'M4 8l7-5 9 6-3 11H6z', 'stroke-linejoin': 'round' }],
    ['circle', { cx: 4, cy: 8, r: 1.6, ...isFull }],
    ['circle', { cx: 20, cy: 9, r: 1.6, ...isFull }],
    ['circle', { cx: 6, cy: 20, r: 1.6, ...isFull }],
  ],
  // The area's two rules. The area is the editor's translucent blue, and its
  // stroke is interrupted where the person passes, so the body doesn't merge
  // into the border. `ruleStepsOn`: the area lying flat in perspective, it is
  // the floor, and the person standing on it; `ruleTouches`: the person with a
  // hand on the area.
  ruleStepsOn: [
    ['path', { d: 'M5.5 13.5h13l4 7.5h-21z', ...editorArea }],
    ['path', { d: 'M7.8 13.5H5.5l-4 7.5h21l-4-7.5h-2.3', ...areaBorder }],
    ['circle', { cx: 12, cy: 2.98, r: 1.98, ...isFull }],
    ['rect', { x: 9.93, y: 5.59, width: 4.14, height: 6.84, rx: 1.26, ...isFull }],
    ['rect', { x: 8.4, y: 6.04, width: 1.26, height: 5.94, rx: 0.63, ...isFull }],
    ['rect', { x: 14.34, y: 6.04, width: 1.26, height: 5.94, rx: 0.63, ...isFull }],
    ['rect', { x: 9.93, y: 10.9, width: 1.8, height: 8.1, rx: 0.9, ...isFull }],
    ['rect', { x: 12.27, y: 10.9, width: 1.8, height: 8.1, rx: 0.9, ...isFull }],
  ],
  ruleTouches: [
    ['rect', { x: 13, y: 3.5, width: 8.5, height: 17, rx: 1.5, ...editorArea }],
    [
      'path',
      {
        d: 'M13 5.34V5a1.5 1.5 0 0 1 1.5-1.5h5.5A1.5 1.5 0 0 1 21.5 5v14a1.5 1.5 0 0 1-1.5 1.5h-5.5A1.5 1.5 0 0 1 13 19v-7.75',
        ...areaBorder,
      },
    ],
    ['circle', { cx: 6.8, cy: 4.59, r: 2.09, ...isFull }],
    ['rect', { x: 4.62, y: 7.34, width: 4.37, height: 7.22, rx: 1.33, ...isFull }],
    ['rect', { x: 3, y: 7.82, width: 1.33, height: 6.27, rx: 0.66, ...isFull }],
    ['rect', { x: 8.22, y: 7.44, width: 5.51, height: 1.71, rx: 0.85, ...isFull }],
    ['rect', { x: 4.62, y: 12.95, width: 1.9, height: 8.55, rx: 0.95, ...isFull }],
    ['rect', { x: 7.08, y: 12.95, width: 1.9, height: 8.55, rx: 0.95, ...isFull }],
  ],
  plus: [['path', { d: 'M12 5v14M5 12h14' }]],
  minus: [['path', { d: 'M5 12h14' }]],
  close: [['path', { d: 'M6 6l12 12M18 6L6 18' }]],
  checkmark: [['path', { d: 'M5 12.5l4.5 4.5L19 7.5' }]],
  eye: [
    ['path', { d: 'M2.25 12S6 5.25 12 5.25 21.75 12 21.75 12 18 18.75 12 18.75 2.25 12 2.25 12z' }],
    ['circle', { cx: 12, cy: 12, r: 3 }],
  ],
  eyeOff: [
    ['path', { d: 'M2.25 12S6 5.25 12 5.25 21.75 12 21.75 12 18 18.75 12 18.75 2.25 12 2.25 12z' }],
    ['circle', { cx: 12, cy: 12, r: 3 }],
    ['path', { d: 'M3.75 20.25l16.5-16.5' }],
  ],
} as const satisfies Record<string, readonly Piece[]>;

export type UiIconName = keyof typeof UI_ICONS;

// 16×16 grids that became 24×24 keep the stroke in the old pixels: the user's
// `stroke` is the old one times 1.5.
export const DEFAULT_STROKE = 2;
