<script lang="ts">
  // A UI icon, by the name it has in `lib/uiIcons.ts`. It follows the
  // surrounding text color (currentColor), and the size comes from whoever uses
  // it: in pixels, or the text size by default.
  import { UI_ICONS, DEFAULT_STROKE, type UiIconName } from '../lib/uiIcons.ts';

  interface Props {
    name: UiIconName;
    size?: number | string;
    // A number, or a `var(--x, n)` when the screen matches the stroke to its
    // neighbors.
    stroke?: number | string;
  }

  let { name, size = '1em', stroke = DEFAULT_STROKE }: Props = $props();
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  style:stroke-width={stroke}
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
>
  {#each UI_ICONS[name] as [tag, attributes] (attributes)}
    <svelte:element this={tag} {...attributes} />
  {/each}
</svg>

<style>
  svg {
    display: block;
    flex: none;
  }
</style>
