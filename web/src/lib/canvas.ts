// context2d returns a canvas's 2D context. The browser only denies it when the
// canvas already has a context of another type, which no canvas on this screen
// has: throwing is better than spreading `?.` through every drawing.
export function context2d(c: HTMLCanvasElement): CanvasRenderingContext2D {
  const g = c.getContext('2d');
  if (!g) throw new Error('o navegador não deu o contexto 2D do canvas');
  return g;
}
