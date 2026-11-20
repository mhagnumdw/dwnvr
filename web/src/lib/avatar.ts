// The photo for My account, shrunk here in the browser: the original, of any
// size, never leaves the device. Only the center square goes to the server, as
// a JPEG of up to `sidePx` pixels. The canvas doesn't copy the EXIF, so the
// photo's GPS stays behind.
//
// The numbers (`sidePx`, `maxBytes`, `quality`) come from GET /api/account: they
// live on the server, which rejects whatever exceeds them.

import type { AvatarRule } from './api.ts';
import { context2d } from './canvas.ts';

// Below this quality the photo already looks bad; if it doesn't fit under the
// ceiling even then, it is better to say it failed than to send a ruined photo.
// In practice it doesn't happen: pure noise, the worst case, fit at quality 80.
const MIN_QUALITY = 50;

function canvas(side: number) {
  const c = document.createElement('canvas');
  c.width = c.height = side;
  return c;
}

function jpeg(c: HTMLCanvasElement, quality: number) {
  return new Promise<Blob | null>((ok) => {
    c.toBlob(ok, 'image/jpeg', quality / 100);
  });
}

// shrinkPhoto returns the JPEG ready for the PUT, or throws an Error with the
// message for the screen.
export async function shrinkPhoto(
  file: Blob,
  { sidePx: side, maxBytes: ceilingBytes, quality: quality }: AvatarRule,
): Promise<Blob> {
  let img: ImageBitmap;
  try {
    // It already comes rotated by the EXIF orientation: that is
    // createImageBitmap's default.
    img = await createImageBitmap(file);
  } catch {
    throw new Error('este aparelho não abriu a imagem escolhida');
  }

  // The center square, with no crop screen.
  const q = Math.min(img.width, img.height);
  let source: CanvasImageSource = img;
  let x = (img.width - q) / 2;
  let y = (img.height - q) / 2;
  let l = q;

  // In half steps: shrinking in one go, from 4000 to 256, leaves the result
  // jagged on every browser's canvas. The first step is also the one that drops
  // the rest of the photo, outside the square.
  while (l / 2 >= side) {
    const n = Math.floor(l / 2);
    const c = canvas(n);
    const ctx = context2d(c);
    ctx.imageSmoothingQuality = 'high';
    ctx.drawImage(source, x, y, l, l, 0, 0, n, n);
    source = c;
    x = y = 0;
    l = n;
  }
  // A photo smaller than the side keeps its size: enlarging doesn't bring
  // sharpness.
  const final = canvas(Math.min(side, l));
  const ctx = context2d(final);
  // JPEG has no transparency: the background of a cropped PNG would come out
  // black.
  ctx.fillStyle = '#fff';
  ctx.fillRect(0, 0, final.width, final.height);
  ctx.imageSmoothingQuality = 'high';
  ctx.drawImage(source, x, y, l, l, 0, 0, final.width, final.height);
  img.close();

  for (let which = quality; which >= MIN_QUALITY; which -= 10) {
    const b = await jpeg(final, which);
    if (b && b.size <= ceilingBytes) return b;
  }
  throw new Error('esta foto não coube no tamanho do avatar: tente outra');
}
