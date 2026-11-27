// Types for video-rtc.js, handwritten: the .js is go2rtc's, copied as it came,
// and has to stay updatable with a `curl`. Only what dwnvr uses is here,
// with the types taken from the file's own JSDoc comments.

export declare class VideoRTC extends HTMLElement {
  /** Grace period, in ms, between leaving the screen and closing the connection. */
  DISCONNECT_TIMEOUT: number;
  /** Minimum interval, in ms, between one connection attempt and the next. */
  RECONNECT_TIMEOUT: number;
  /** Accepted modes, in order of preference: `webrtc`, `mse`, `hls`, `mjpeg`. */
  mode: string;
  /** What to ask go2rtc for: `video`, `audio`, `microphone`. */
  media: string;
  /** Keeps playing off screen. */
  background: boolean;
  /** Visible fraction of the player needed to play; 0 turns the rule off. */
  visibilityThreshold: number;
  /** Stops playing while the tab is hidden. */
  visibilityCheck: boolean;
  /** Created on the first `connectedCallback`: null before that. */
  video: HTMLVideoElement | null;
  /** The address of go2rtc's websocket. Accepts a path starting with `/`. */
  set src(value: string | URL);

  connectedCallback(): void;
  disconnectedCallback(): void;
  /** Opens the connection; `true` if it opened, `false` if there was nothing to open. */
  onconnect(): boolean;
  /** Closes the websocket and WebRTC, and releases the video. */
  ondisconnect(): void;
  /** The websocket closed; `true` if another attempt was scheduled. */
  onclose(): boolean;
}
