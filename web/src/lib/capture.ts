// Capture of the frame shown in the player, delivered to the user as a file.
//
// There is none of this on the server on purpose: dwnvr never decodes video,
// and here it doesn't need to. The recordings <video> is fed by MSE with bytes
// fetched from the same origin, so the canvas isn't tainted and the frame
// already decoded by the GPU can be read back as pixels.

import { context2d } from './canvas.ts';

// Draws over the frame, at its native size.
export type OverlayDrawing = (g: CanvasRenderingContext2D, width: number, height: number) => void;

// downloadFrame crops the <video>'s current frame and hands it to the user:
// share sheet where available, download where not. It throws if it can't
// capture, so the caller can warn on screen.
//
// `overlay(g, width, height)`, when given, draws over the frame already at
// native size - this is how the detector's boxes get into the image when they
// are turned on in the screen.
export async function downloadFrame(
  video: HTMLVideoElement | null | undefined,
  name: string,
  overlay?: OverlayDrawing,
) {
  // Before the first decoded frame the <video> has no size yet, and capturing
  // then would produce a 0x0 image with no error at all.
  if (!video?.videoWidth || !video.videoHeight) {
    throw new Error('ainda não há quadro para capturar');
  }

  // The size is the camera's NATIVE one, not the screen's: the CSS `max-height:
  // 56dvh` shrinks the video so the timeline fits with it, and a capture at the
  // displayed size would return fewer pixels than the recording has.
  const canvas = document.createElement('canvas');
  canvas.width = video.videoWidth;
  canvas.height = video.videoHeight;
  const g = context2d(canvas);
  g.drawImage(video, 0, 0);
  overlay?.(g, canvas.width, canvas.height);

  // JPEG because the frame came from already-compressed H.264/H.265: PNG would
  // faithfully keep the previous compression's artifacts at several times the
  // size.
  const blob = await new Promise<Blob | null>((resolve) => {
    canvas.toBlob(resolve, 'image/jpeg', 0.92);
  });
  if (!blob) throw new Error('o navegador não conseguiu gerar a imagem');

  const file = new File([blob], name, { type: 'image/jpeg' });

  // On the phone the download lands in a folder nobody opens; the system sheet
  // takes the image to the gallery or to the conversation where it is useful.
  //
  // Where it doesn't exist, download is the way - and it doesn't exist in more
  // places than it seems: the Web Share API is [SecureContext], so on a dwnvr
  // served over http:// not even `navigator.canShare` gets defined, and the
  // phone downloads just like the desktop. It is not a defect, it is the
  // address; see docs/remote-access.md.
  if ('canShare' in navigator && navigator.canShare({ files: [file] })) {
    try {
      await navigator.share({ files: [file] });
      return;
    } catch (e) {
      // Closing the sheet without choosing anything is the user's decision, not
      // a failure: the capture ended here and the screen shouldn't report an
      // error.
      if (e instanceof DOMException && e.name === 'AbortError') return;
      // Anything else - permission denied, type rejected - still has download
      // as the way out.
    }
  }

  const url = URL.createObjectURL(blob);
  try {
    const a = document.createElement('a');
    a.href = url;
    a.download = name;
    a.click();
  } finally {
    // Without this the blob stays in memory until the tab closes, and on a
    // phone watching a 4 MP camera that is heavy.
    URL.revokeObjectURL(url);
  }
}
