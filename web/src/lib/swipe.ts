// The sideways swipe that switches cameras: in Live's fullscreen and on the
// Recordings video. The numbers are the same on both screens, so the finger
// doesn't have to learn two gestures.

// How long the camera takes to leave or enter sideways, and the one that goes
// back in place on a swipe that is too short.
export const SWITCH_MS = 220;
// The swipe switches camera once it passes SWIPE_PX, or SWIPE_FRACTION of the
// width if it is narrow. Before AXIS_PX the finger hasn't yet said whether it
// is going sideways or up.
export const SWIPE_PX = 80;
export const SWIPE_FRACTION = 0.2;
export const AXIS_PX = 8;

// From how many px, in an area this wide, the swipe switches camera.
export function swipeLimit(width: number) {
  return Math.min(SWIPE_PX, width * SWIPE_FRACTION);
}
