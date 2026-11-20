// Controls over the video, in fullscreen, that disappear on their own after a
// while with no touch or mouse movement, and come back with the next one.
// Whoever uses it calls `show` on every touch and says, through `canVanish`,
// whether at that moment they may vanish: the paused recordings video and the
// open Live menu hold the controls in view. The question is asked at vanish
// time, not at touch time.
//
// On the phone a single tap on the image also hides them, like YouTube, and
// then it applies even with the video paused: whoever taps to see a clean image
// wants it clean. `pointerdown` already showed them by the time the single tap
// is confirmed, so the question is whether they were visible BEFORE it
// (`touched`).

// How long the controls stay in view after the last touch.
export const CONTROLS_VANISH_MS = 3000;

export function vanishingControls(canVanish: () => boolean) {
  let visibleOnes = $state(true);
  let timer: number | undefined;
  let visibleOnTouch = false;

  return {
    get visibleOnes() {
      return visibleOnes;
    },
    // Shows and rearms the vanishing.
    show() {
      visibleOnes = true;
      clearTimeout(timer);
      timer = setTimeout(() => {
        if (canVanish()) visibleOnes = false;
      }, CONTROLS_VANISH_MS);
    },
    // On a touch's `pointerdown`, in place of `show`.
    touched() {
      visibleOnTouch = visibleOnes;
      this.show();
    },
    // The single tap was confirmed: what was already visible before it
    // disappears, and what was hidden stays in view, as `touched` left it.
    singleTouch() {
      if (!visibleOnTouch) return;
      clearTimeout(timer);
      visibleOnes = false;
    },
    // For whoever leaves the screen: the pending vanish no longer fires.
    stop() {
      clearTimeout(timer);
    },
  };
}
