// Decoding of the timeline thumbnails.
//
// Each thumbnail is a ONE-frame MP4 - the init plus the first fragment of the
// segment, cut from what is already recorded. The server decodes nothing: what
// turns it into an image is the browser, with hardware acceleration.
//
// The preferred path is WebCodecs, which decodes the keyframe directly. Where
// it does not exist, a hidden <video> does the same job.

import { api, mediaURL } from './api.ts';

const cache = new Map<string, Promise<ImageBitmap | null>>();
const MAX_CACHE = 240; // ~a few strip screens; above that the phone's RAM suffers
const MAX_PARALLEL = 3; // decodes at the same time; the download does not wait
// The larger thumbnails (the `scale` of `thumbnail`): the small area's, and the
// camera card's tile, wider than the strip tile. One or two per camera with an
// area, with slack for the segment swap. Eviction here does not close the
// bitmap (see `thumbnail`).
const MAX_ENLARGED = 12;
const enlarged = new Map<string, Promise<ImageBitmap | null>>();

// Width of the strip's larger tile, in CSS px: the `.tile` of
// ThumbStrip.svelte. The narrow-screen one is smaller, but the rotated phone
// starts using this one, and caching at the smaller would leave the thumbnail
// blurry.
const FRAMELET_WIDTH = 108;

// The `scale` of `thumbnail` to draw the thumbnail at `width` CSS px without
// blur: the area tile on the camera card is wider than the strip tile.
export const scaleToWidth = (width: number) => Math.max(1, Math.ceil(width / FRAMELET_WIDTH));

// The cache stores the ready bitmap, uncompressed: 4 bytes per pixel. At the
// camera's resolution, a 640×360 thumbnail takes ~0.9 MB, and a 1080p one ~8
// MB. Shrunk to the strip tile's width at the screen's density, it comes to
// ~0.23 MB on a density-3 phone, whatever the camera. Measured on Chrome 155 on
// 14/10/2026: shrinking costs 0.1 to 0.5 ms per thumbnail with a GPU, against 4
// to 13 ms for decoding.
function shrunk(sourceWidth: number, density: number): ImageBitmapOptions | undefined {
  const target = Math.ceil(FRAMELET_WIDTH * density);
  // Width only: the height follows the camera's aspect ratio. Enlarging gains
  // no sharpness and uses more memory than the source itself.
  return sourceWidth > target ? { resizeWidth: target, resizeQuality: 'medium' } : undefined;
}

interface Task {
  fn: () => Promise<ImageBitmap>;
  resolve: (b: ImageBitmap) => void;
  reject: (e: unknown) => void;
}

let running = 0;
const queue: Task[] = [];

function schedule(fn: Task['fn']) {
  return new Promise<ImageBitmap>((resolve, reject) => {
    queue.push({ fn, resolve, reject });
    drain();
  });
}

function drain() {
  while (running < MAX_PARALLEL) {
    const task = queue.shift();
    if (!task) return;
    running++;
    void task
      .fn()
      .then(task.resolve, task.reject)
      .finally(() => {
        running--;
        drain();
      });
  }
}

/**
 * Returns an ImageBitmap of a segment's keyframe, or null if it cannot. The
 * result is memoized: scrolling the strip forward and back should not refetch
 * or redecode anything.
 */
// `scale` keeps the bitmap larger than the strip tile, that many times: it is
// the small area's thumbnail, which enlarges up to ZOOM_CEILING times and would
// blur at the strip tile's size. Those go in a separate, short cache
// (MAX_ENLARGED): each weighs up to scale² times the strip tile's, and the
// strip's MAX_CACHE would not hold them.
export function thumbnail(cam: string, t: number, scale = 1): Promise<ImageBitmap | null> {
  // The screen density goes into the key because the stored bitmap was shrunk
  // for it (see `shrunk`): after a browser zoom, or moving the window to
  // another monitor, it would come out stretched on the new tile. The entries
  // of the old density leave through MAX_CACHE, like any other. It is read only
  // once: a zoom between the request and the decoding would store, under this
  // key, a bitmap shrunk for another density.
  const density = (window.devicePixelRatio || 1) * scale;
  const key = `${cam}@${t}@${density}`;
  const where = scale === 1 ? cache : enlarged;
  const kept = where.get(key);
  if (kept) return kept;

  const p = download(mediaURL.thumb(cam, t))
    .then((buf) => schedule(() => decode(buf, density)))
    .catch(() => null);
  where.set(key, p);

  if (where.size > (scale === 1 ? MAX_CACHE : MAX_ENLARGED)) {
    // Map preserves insertion order, so the oldest key leaves first.
    const oldest = where.keys().next().value;
    if (oldest !== undefined) {
      // Closing frees the memory right away, and the strip, with hundreds,
      // needs that. The enlarged one does not: a card with more cameras with
      // areas than MAX_ENLARGED shows them all at the same time, and closing
      // one still on screen would make it ask again, evicting another, without
      // end. Evicted without closing, it leaves memory when the last thumbnail
      // that draws it lets go.
      if (where === cache) void where.get(oldest)?.then((b) => b?.close());
      where.delete(oldest);
    }
  }
  return p;
}

// The start of the camera's last recorded segment, or null if it has no
// recording. It is the most recent frame the server has ready: the segment's
// keyframe.
export async function lastSegment(cam: string): Promise<number | null> {
  const dias = await api.days(cam);
  const day = dias.days.at(-1)?.day;
  if (!day) return null;
  const t = await api.timeline(cam, day);
  return t.segments.at(-1)?.[0] ?? null;
}

// Each camera's lastSegment, stored for a while. The area thumbnails ask for
// the camera's frame one per area - the Detections popover mounts up to 8 per
// camera -, and each request would cost /days and the timeline again. The
// bitmap is not kept here: `thumbnail` stores it, and returns it again if its
// cache evicted it.
const LAST_SEGMENT_VALID_MS = 5 * 60_000;
const ultimos = new Map<string, { to: number; t: Promise<number | null> }>();

export function storedLastSegment(cam: string): Promise<number | null> {
  const now = Date.now();
  const stored = ultimos.get(cam);
  if (stored && stored.to > now) return stored.t;
  const t = lastSegment(cam);
  const entry = { to: now + LAST_SEGMENT_VALID_MS, t };
  ultimos.set(cam, entry);
  // The failure and the "no recording" are not stored: the next request tries
  // again, and a camera that just started recording shows up.
  const forget = () => {
    if (ultimos.get(cam) === entry) ultimos.delete(cam);
  };
  t.then((v) => {
    if (v === null) forget();
  }, forget);
  return t;
}

// Forgets the camera's stored last segment: its recordings were deleted, and it
// no longer exists.
export function forgetLastSegment(cam: string) {
  ultimos.delete(cam);
}

// A segment's keyframe at the camera's resolution, to draw on top of (the area
// editor). Outside the cache and the strip's queue: it is a single frame,
// requested by whoever is waiting for it. The infinite density makes `shrunk`
// not shrink.
export async function fullFrame(cam: string, t: number): Promise<ImageBitmap> {
  return decode(await download(mediaURL.thumb(cam, t)), Infinity);
}

export function clearThumbnails() {
  for (const p of cache.values()) void p.then((b) => b?.close());
  cache.clear();
  for (const p of enlarged.values()) void p.then((b) => b?.close());
  enlarged.clear();
  ultimos.clear();
}

// The download stays outside the queue: waiting on the network does not occupy
// the decoder, and the thumbnails the strip requests together - the ones that
// entered the view - travel together. The low priority lets the player's
// segments go ahead when the connection is HTTP/1.1, with six at a time.
async function download(url: string) {
  const res = await fetch(url, { priority: 'low' });
  // The body of an error is the API's JSON: trying to decode it as video only
  // spends a <video> to reach the same empty thumbnail.
  if (!res.ok) throw new Error(`miniatura: HTTP ${res.status}`);
  return new Uint8Array(await res.arrayBuffer());
}

async function decode(buf: Uint8Array<ArrayBuffer>, density: number) {
  if (typeof VideoDecoder !== 'undefined') {
    try {
      return await decodeWithWebCodecs(buf, density);
    } catch {
      // Falls back to the <video>: some browsers announce WebCodecs but reject
      // the configuration of these cameras.
    }
  }
  return decodeWithVideoElement(buf, density);
}

// --- WebCodecs --------------------------------------------------------------

const CONFIG_BOX: Readonly<Record<string, { codec: string }>> = {
  hvcC: { codec: 'hev1.1.6.L153.B0' },
  avcC: { codec: 'avc1.640029' },
};

function fourcc(buf: Uint8Array, o: number) {
  return String.fromCharCode(...buf.subarray(o, o + 4));
}

async function decodeWithWebCodecs(buf: Uint8Array, density: number): Promise<ImageBitmap> {
  const dv = new DataView(buf.buffer, buf.byteOffset, buf.byteLength);

  // The configuration box is nested inside the moov; scanning by signature
  // avoids descending the whole tree just to find 100 bytes.
  let description: Uint8Array | null = null;
  let codec: string | null = null;
  for (let i = 0; i + 8 <= buf.length && !description; i++) {
    const cfg = CONFIG_BOX[fourcc(buf, i + 4)];
    if (!cfg) continue;
    const size = dv.getUint32(i);
    if (size > 8 && i + size <= buf.length) {
      description = buf.subarray(i + 8, i + size);
      codec = cfg.codec;
    }
  }
  if (!description || !codec) throw new Error('sem configuração de codec');

  // The mdat is a top-level box, so walking the root level is enough.
  let sample: Uint8Array | null = null;
  for (let o = 0; o + 8 <= buf.length;) {
    const size = dv.getUint32(o);
    if (size < 8) break;
    if (fourcc(buf, o + 4) === 'mdat') {
      sample = buf.subarray(o + 8, Math.min(o + size, buf.length));
      break;
    }
    o += size;
  }
  if (!sample) throw new Error('sem mdat');

  const support = await VideoDecoder.isConfigSupported({ codec, description });
  if (!support.supported) throw new Error('codec não suportado');

  return new Promise((resolve, reject) => {
    let done = false;
    const dec = new VideoDecoder({
      output: (frame) => {
        if (done) {
          frame.close();
          return;
        }
        done = true;
        void createImageBitmap(frame, shrunk(frame.displayWidth, density))
          .then(resolve, reject)
          .finally(() => {
            frame.close();
            dec.close();
          });
      },
      error: (e) => {
        if (!done) reject(e);
      },
    });
    dec.configure({ codec, description, optimizeForLatency: true });
    dec.decode(new EncodedVideoChunk({ type: 'key', timestamp: 0, data: sample }));
    dec.flush().catch(() => {
      // the error, if any, already arrived through the decoder's `error`
    });
  });
}

// --- fallback with <video> ------------------------------------------------

// On Android `loadeddata` may arrive before the frame is ready to be copied,
// and createImageBitmap rejects it with "The image source is not usable". On a
// Chrome 155 over http (13/10/2026), 9 of 220 thumbnails became "-" in the
// strip; trying again, 0 of 276, and the second call, 50 ms later, was enough
// for all of them. The following waits are slack, and the last rejection counts
// as a failure.
const FRAME_WAITS_MS = [50, 100, 200, 400];

async function decodeWithVideoElement(buf: Uint8Array<ArrayBuffer>, density: number) {
  const url = URL.createObjectURL(new Blob([buf], { type: 'video/mp4' }));
  const v = document.createElement('video');
  v.muted = true;
  v.playsInline = true;
  v.preload = 'auto';
  v.src = url;

  try {
    await new Promise<void>((res, rej) => {
      v.addEventListener(
        'loadeddata',
        () => {
          res();
        },
        { once: true },
      );
      v.addEventListener(
        'error',
        () => {
          rej(new Error('miniatura ilegível'));
        },
        { once: true },
      );
      setTimeout(() => {
        rej(new Error('tempo esgotado'));
      }, 8000);
    });
    return await frameOf(v, density);
  } finally {
    URL.revokeObjectURL(url);
    v.removeAttribute('src');
    v.load();
  }
}

async function frameOf(v: HTMLVideoElement, density: number) {
  const options = shrunk(v.videoWidth, density);
  for (const ms of FRAME_WAITS_MS) {
    try {
      return await createImageBitmap(v, options);
    } catch {
      await new Promise((r) => setTimeout(r, ms));
    }
  }
  return createImageBitmap(v, options);
}
