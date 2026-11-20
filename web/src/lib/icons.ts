// The object families and the icons that draw them on the timeline.
//
// The drawings are canvas paths, and not emoji or an icon font: emoji change
// shape on every system and don't accept color, and a font would be one more
// file in a project that ships a single binary. At 14px what decides legibility
// is the silhouette, and here the silhouette is ours.

import { context2d } from './canvas.ts';

export interface Family {
  nome: string;
  color: string;
  priority: number;
}

export type FamilyName = 'person' | 'vehicle' | 'animal';

// The three families, in priority order: `person` is what detection exists to
// find, `animal` is what matters least. The priority decides who stays on top
// when two marks overlap and who gets the icon slot when not all fit. The
// family name is what the server sends in `family`.
export const FAMILIES: Readonly<Record<FamilyName, Family>> = {
  person: { nome: 'pessoa', color: '#3fb950', priority: 0 },
  vehicle: { nome: 'veículo', color: '#a371f7', priority: 1 },
  animal: { nome: 'animal', color: '#db61a2', priority: 2 },
};

// A family the server sends and this screen doesn't know yet is drawn in gray
// and stays behind the others: showing up is better than disappearing.
export const UNKNOWN_FAMILY: Family = { nome: '?', color: '#8b949e', priority: 9 };

// The families in the order the screen lists them.
export const FAMILY_NAMES: readonly FamilyName[] = ['person', 'vehicle', 'animal'];

// hasOwn, and not `in`: `constructor` is on every object, and it isn't a
// family.
export const isFamily = (name: string): name is FamilyName => Object.hasOwn(FAMILIES, name);

export const family = (name: string): Family => (isFamily(name) ? FAMILIES[name] : UNKNOWN_FAMILY);

// The families, without repeats, in priority order.
export const byPriority = (families: readonly string[]) =>
  [...families].sort((a, b) => family(a).priority - family(b).priority);

// The families spelled out, for the name the screen reader reads on a
// thumbnail: "pessoa, animal".
export const familyNames = (families: readonly string[]) =>
  byPriority(families)
    .map((f) => family(f).nome)
    .join(', ');

// The model's classes (COCO names, as the detector returns them) in Portuguese,
// for the pointer hint. It mirrors `ClassFamilies` in
// internal/detect/params.go; a class missing from it shows up with its raw
// name.
export const CLASS_NAME: Readonly<Record<string, string>> = {
  person: 'pessoa',
  bicycle: 'bicicleta',
  car: 'carro',
  motorcycle: 'moto',
  bus: 'ônibus',
  truck: 'caminhão',
  train: 'trem',
  cat: 'gato',
  dog: 'cachorro',
  horse: 'cavalo',
  sheep: 'ovelha',
  cow: 'vaca',
};

// Every icon is drawn in a 0..1 square and scaled by `drawIcon`, so the same
// drawing serves at 14px and at 20px.
type Drawing = (g: CanvasRenderingContext2D) => void;

// Cada linha é um traço do desenho, com o comentário dizendo qual.
// prettier-ignore
const DRAWINGS = {
  person(g: CanvasRenderingContext2D) {
    g.beginPath(); g.arc(0.5, 0.18, 0.16, 0, 7); g.fill();       // head
    g.beginPath();
    g.moveTo(0.5, 0.36); g.lineTo(0.5, 0.68);                    // tronco
    g.moveTo(0.22, 0.48); g.lineTo(0.78, 0.48);                  // arms
    g.moveTo(0.5, 0.68); g.lineTo(0.28, 1);                      // pernas
    g.moveTo(0.5, 0.68); g.lineTo(0.72, 1);
    g.stroke();
  },
  // The wheels STICK OUT below the body line and the cabin is a separate
  // trapezoid: with wheels touching the body, at 14px the car read as an
  // animal.
  car(g: CanvasRenderingContext2D) {
    g.beginPath();
    g.moveTo(0.3, 0.34); g.lineTo(0.38, 0.16); g.lineTo(0.66, 0.16); g.lineTo(0.72, 0.34);
    g.closePath(); g.fill();
    g.beginPath(); g.rect(0.05, 0.34, 0.9, 0.3); g.fill();
    g.beginPath(); g.arc(0.27, 0.72, 0.15, 0, 7); g.fill();
    g.beginPath(); g.arc(0.73, 0.72, 0.15, 0, 7); g.fill();
  },
  // Motorcycle and bicycle are told apart by WEIGHT, because no detail survives
  // at 14px: the motorcycle is solid, the bicycle is hollow.
  motorcycle(g: CanvasRenderingContext2D) {
    g.beginPath(); g.arc(0.21, 0.7, 0.21, 0, 7); g.fill();
    g.beginPath(); g.arc(0.79, 0.7, 0.21, 0, 7); g.fill();
    g.beginPath();
    g.moveTo(0.18, 0.62); g.lineTo(0.42, 0.34); g.lineTo(0.74, 0.34); g.lineTo(0.84, 0.62);
    g.closePath(); g.fill();
    g.beginPath(); g.rect(0.3, 0.14, 0.22, 0.09); g.fill();      // handlebar
  },
  bicycle(g: CanvasRenderingContext2D) {
    g.beginPath(); g.arc(0.19, 0.68, 0.19, 0, 7); g.stroke();
    g.beginPath(); g.arc(0.81, 0.68, 0.19, 0, 7); g.stroke();
    g.beginPath();
    g.moveTo(0.19, 0.68); g.lineTo(0.46, 0.68); g.lineTo(0.6, 0.3);
    g.lineTo(0.81, 0.68); g.moveTo(0.46, 0.68); g.lineTo(0.6, 0.3);
    g.moveTo(0.5, 0.26); g.lineTo(0.7, 0.26);
    g.stroke();
  },
  bus(g: CanvasRenderingContext2D) {
    g.beginPath(); g.rect(0.08, 0.2, 0.84, 0.56); g.fill();
    hollow(g, () => {                                            // janelas
      g.rect(0.16, 0.28, 0.3, 0.2);
      g.rect(0.54, 0.28, 0.3, 0.2);
    });
  },
  truck(g: CanvasRenderingContext2D) {
    g.beginPath(); g.rect(0.04, 0.28, 0.48, 0.44); g.fill();     // trunk
    g.beginPath();
    g.moveTo(0.56, 0.44); g.lineTo(0.78, 0.44); g.lineTo(0.94, 0.6);
    g.lineTo(0.94, 0.72); g.lineTo(0.56, 0.72); g.closePath(); g.fill();
    g.beginPath(); g.arc(0.26, 0.8, 0.12, 0, 7); g.fill();
    g.beginPath(); g.arc(0.78, 0.8, 0.12, 0, 7); g.fill();
  },
  train(g: CanvasRenderingContext2D) {
    g.beginPath(); g.rect(0.14, 0.16, 0.72, 0.52); g.fill();
    hollow(g, () => { g.rect(0.24, 0.26, 0.52, 0.2); });         // window
    g.beginPath(); g.arc(0.3, 0.82, 0.11, 0, 7); g.fill();
    g.beginPath(); g.arc(0.7, 0.82, 0.11, 0, 7); g.fill();
  },
  // Dog and cat are told apart by the whole silhouette, because ears vanish at
  // 14px: the cat has its tail raised, the dog has a short tail and a long
  // snout.
  dog(g: CanvasRenderingContext2D) {
    g.beginPath(); g.rect(0.16, 0.52, 0.48, 0.22); g.fill();     // corpo
    g.beginPath();
    g.moveTo(0.24, 0.74); g.lineTo(0.24, 0.94);
    g.moveTo(0.56, 0.74); g.lineTo(0.56, 0.94);
    g.stroke();
    g.beginPath(); g.arc(0.72, 0.46, 0.15, 0, 7); g.fill();      // head
    g.beginPath(); g.rect(0.8, 0.46, 0.18, 0.1); g.fill();       // focinho
    g.beginPath(); g.moveTo(0.16, 0.56); g.lineTo(0.02, 0.48); g.stroke();
  },
  cat(g: CanvasRenderingContext2D) {
    g.beginPath(); g.rect(0.2, 0.56, 0.42, 0.2); g.fill();
    g.beginPath();
    g.moveTo(0.28, 0.76); g.lineTo(0.28, 0.94);
    g.moveTo(0.56, 0.76); g.lineTo(0.56, 0.94);
    g.stroke();
    g.beginPath(); g.arc(0.72, 0.5, 0.14, 0, 7); g.fill();
    g.beginPath();                                               // orelhas
    g.moveTo(0.61, 0.4); g.lineTo(0.6, 0.24); g.lineTo(0.72, 0.38); g.closePath(); g.fill();
    g.beginPath();
    g.moveTo(0.83, 0.4); g.lineTo(0.86, 0.24); g.lineTo(0.74, 0.38); g.closePath(); g.fill();
    g.beginPath();                                               // rabo erguido
    g.moveTo(0.2, 0.66); g.quadraticCurveTo(0.02, 0.58, 0.08, 0.16); g.stroke();
  },
  // The family's animal is a quadruped with no ears on purpose: when it can't
  // be told apart yet, it must not suggest dog or cat.
  animal(g: CanvasRenderingContext2D) {
    g.beginPath();
    g.moveTo(0.12, 0.84); g.lineTo(0.12, 0.54); g.lineTo(0.62, 0.54);
    g.lineTo(0.62, 0.84); g.moveTo(0.37, 0.54); g.lineTo(0.37, 0.84);
    g.stroke();
    g.beginPath(); g.arc(0.76, 0.42, 0.17, 0, 7); g.fill();
    g.beginPath(); g.moveTo(0.12, 0.54); g.lineTo(0.02, 0.34); g.stroke();
  },
};

export type IconName = keyof typeof DRAWINGS | 'vehicle';

// The family's vehicle is the car, its most common class.
const ICONS: Readonly<Record<IconName, Drawing>> = {
  ...DRAWINGS,
  vehicle: (g) => {
    DRAWINGS.car(g);
  },
};

const isIcon = (name: string): name is IconName => Object.hasOwn(ICONS, name);

// Cut out of the drawing, instead of painting over it in the background color:
// the icon sits on the page, and the hole shows the page, whatever its color.
function hollow(g: CanvasRenderingContext2D, path: () => void) {
  g.save();
  g.globalCompositeOperation = 'destination-out';
  g.beginPath();
  path();
  g.fill();
  g.restore();
}

// The model class -> its own icon. A class with no icon (horse, sheep, cow)
// draws the family's.
const CLASS_ICON: Readonly<Record<string, IconName>> = {
  person: 'person',
  bicycle: 'bicycle',
  car: 'car',
  motorcycle: 'motorcycle',
  bus: 'bus',
  truck: 'truck',
  train: 'train',
  cat: 'cat',
  dog: 'dog',
};

// A family's icon, by the name the server sends. A family this screen doesn't
// know has no icon.
export const familyIcon = (name: string): IconName | null => (isIcon(name) ? name : null);

// The icon name for a mark at the requested level of detail.
export function iconOf(mark: { class: string; family: string }, byClass: boolean): IconName | null {
  const name = byClass ? (CLASS_ICON[mark.class] ?? mark.family) : mark.family;
  return isIcon(name) ? name : null;
}

// Draws an icon with the top-left corner at (x, y) and `lado` px.
export function drawIcon(
  g: CanvasRenderingContext2D,
  name: IconName,
  x: number,
  y: number,
  side: number,
  color: string,
) {
  const f = ICONS[name];
  g.save();
  g.translate(x, y);
  g.scale(side, side);
  g.fillStyle = color;
  g.strokeStyle = color;
  g.lineWidth = 2 / side; // 2px reais, seja qual for a escala
  g.lineJoin = 'round';
  g.lineCap = 'round';
  f(g);
  g.restore();
}

// The same icon as an image, for whoever draws in HTML and not on a canvas: the
// Detections grid puts the icon on each thumbnail, and one <canvas> per icon
// would be one 2D context per thumbnail. Each combination is drawn once, at the
// screen density, and reused by all.
const images = new Map<string, string>();

export function iconURL(name: IconName, side: number, color: string) {
  const dpr = window.devicePixelRatio || 1;
  const key = `${name}|${side}|${color}|${dpr}`;
  let url = images.get(key);
  if (!url) {
    const c = document.createElement('canvas');
    c.width = c.height = Math.ceil(side * dpr);
    const g = context2d(c);
    g.scale(dpr, dpr);
    drawIcon(g, name, 0, 0, side, color);
    url = c.toDataURL();
    images.set(key, url);
  }
  return url;
}
