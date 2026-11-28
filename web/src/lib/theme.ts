// The theme's colors for whoever paints outside CSS: a canvas, or an icon
// turned into an image. The value is the CSS token from app.css, read on the
// element being painted: inside `.over-video` it is the dark palette, the same
// in every theme, and on the page it is the theme's.
//
// Read at drawing time, and not once at load: the theme can change with the
// screen open.
export function tokenColor(el: Element, token: `--${string}`): string {
  return getComputedStyle(el).getPropertyValue(token).trim();
}
