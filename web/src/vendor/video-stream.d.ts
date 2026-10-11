// Tipos do video-stream.js, que não exporta nada: importá-lo registra o
// elemento <video-stream>. Ver video-rtc.d.ts.

import type { HTMLAttributes } from 'svelte/elements';
import type { VideoRTC } from './video-rtc.js';

declare global {
  interface HTMLElementTagNameMap {
    'video-stream': VideoRTC;
  }
}

declare module 'svelte/elements' {
  export interface SvelteHTMLElements {
    'video-stream': HTMLAttributes<VideoRTC>;
  }
}
