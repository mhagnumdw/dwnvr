<!--
  Modal sheet: the dimmed backdrop, the box and the three ways to close (click
  outside, Esc, a button from whoever uses it). Only the frame lives here - the
  content comes through a snippet, so the same component serves the camera form
  and the confirmation dialog.
-->
<script module lang="ts">
  // Stack of open modals. Esc can only close the top one: with the confirmation
  // open over the edit form, a single key closing both would throw away the
  // form the user was still filling in.
  const stack: object[] = [];
</script>

<script lang="ts">
  import { onMount } from 'svelte';
  import type { Snippet } from 'svelte';

  interface Props {
    onclose?: () => void;
    width?: number | string;
    scrollReserve?: boolean;
    centered?: boolean;
    variableHeight?: boolean;
    children?: Snippet;
  }

  // `width` is the ceiling on desktop: the form reads well at 460px, and a
  // detection's sheet shows a camera frame, which asks for more. A number is in
  // px; a string goes in as is, for whoever needs a calculation - the detection
  // sheet ties the width to the free height (`--max-height`), because its frame
  // is 16:9 and in a short window it is the width that has to give way.
  //
  // `scrollReserve` always keeps the scrollbar's space reserved, on both sides
  // so the content stays centered. It serves whoever has the height tied to the
  // width, like the detection's 16:9 stage: without the reserve, the bar that
  // appears narrows the content, it gets shorter, the bar goes away, it grows
  // back - and the sheet jitters between the two states on every frame. With
  // the reserve the width doesn't depend on the bar and the cycle has nowhere
  // to start. On a phone the bar floats over the content and the reserve takes
  // up nothing.
  //
  // `centered` opens the sheet in the middle of the screen on a phone too. The
  // bottom sheet suits a form, where the thumb reaches the fields; the
  // detection's sheet is for looking at the frame, and with weeks of use in the
  // middle it proved better.
  //
  // `variableHeight` is for whoever changes height while the sheet is open,
  // like the camera form, in tabs of different heights: the top of the sheet,
  // where the tabs are, can't move on every switch. On desktop the sheet is
  // pinned to the top of the screen, instead of the middle, and only the bottom
  // moves. On a phone, which slides up from the bottom, it opens at maximum
  // height, and what is left over stays empty below the content.
  let {
    onclose,
    width = 460,
    scrollReserve = false,
    centered = false,
    variableHeight = false,
    children,
  }: Props = $props();
  const cssWidth = $derived(typeof width === 'number' ? `${width}px` : width);

  const self = {};
  onMount(() => {
    stack.push(self);
    return () => {
      stack.splice(stack.indexOf(self), 1);
    };
  });

  // With something in the sheet fullscreen, Esc belongs to it: it leaves
  // fullscreen and keeps the sheet open. The real one shows up in
  // `fullscreenElement`; the simulated one marks the key as already handled
  // (lib/fullscreen.svelte.ts).
  function keydown(e: KeyboardEvent) {
    if (e.defaultPrevented || document.fullscreenElement) return;
    if (e.key === 'Escape' && stack.at(-1) === self) onclose?.();
  }
</script>

<svelte:window onkeydown={keydown} />

<div
  class="overlay"
  class:centered
  class:variable-height={variableHeight}
  role="presentation"
  onclick={(e) => {
    if (e.target === e.currentTarget) onclose?.();
  }}
>
  <div class="card sheet" class:scroll-reserve={scrollReserve} style:--width={cssWidth}>
    {@render children?.()}
  </div>
</div>

<style>
  .overlay {
    /* How far the sheet can grow. Lives here, not loose in `max-height`,
       because whoever computes its own width from the free height needs the
       same number - that is the case of the detection sheet. */
    --max-height: 92dvh;

    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
    display: grid;
    /* On a phone the sheet slides up from the bottom, where the thumb reaches. */
    align-items: end;
    z-index: 30;
  }

  .sheet {
    display: grid;
    gap: 14px;
    width: 100%;
    max-height: var(--max-height);
    /* Past the ceiling, the grid squeezes the rows to fit. A child that draws
       itself doesn't shrink along and ends up painting over what comes after -
       that is what happened with the detection's 16:9 stage, which has
       `overflow: hidden` and so accepts being squeezed down to zero. With the
       rows at `min-content` nobody is squeezed and the sheet scrolls. */
    grid-auto-rows: min-content;
    overflow-y: auto;
    border-radius: var(--radius) var(--radius) 0 0;
    padding-bottom: calc(14px + env(safe-area-inset-bottom));
  }

  .variable-height .sheet {
    height: var(--max-height);
  }

  .centered {
    place-items: center;
    padding: 8px;
  }

  .centered .sheet {
    border-radius: var(--radius);
    padding-bottom: 14px;
  }

  .sheet.scroll-reserve {
    scrollbar-gutter: stable both-edges;
  }

  @media (min-width: 640px) {
    .overlay {
      place-items: center;
      padding: 20px;
    }
    .sheet {
      max-width: var(--width);
      border-radius: var(--radius);
    }
    .overlay.variable-height {
      align-items: start;
      padding-top: 6dvh;
    }
    .variable-height .sheet {
      height: auto;
    }
  }
</style>
