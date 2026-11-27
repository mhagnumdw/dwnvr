// Types for video-stream.js, which exports nothing: importing it registers the
// <video-stream> element. See video-rtc.d.ts.

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
