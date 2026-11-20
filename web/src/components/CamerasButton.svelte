<!--
  The button that opens the camera list, in Live and Detections. It shows the
  checked cameras out of the total ("5/9"): the count alone did not say whether
  there were more cameras. The slash takes the place of the parentheses that
  used to be there, and the total comes in gray and smaller, so the button grows
  only ~2px. With all checked, the icon turns blue.

  <CamerasButton checkedList={5} total={9} all={false} onclick={...} />

  On the phone only the icon and the count remain; the name comes back from the
  tablet up. Each screen matches the button to its neighbors through the custom
  properties `--stroke` (icon thickness) and `--pad-x`.
-->
<script lang="ts">
  import Icon from './Icon.svelte';

  let {
    checkedList,
    total,
    all,
    isOpen,
    onclick,
  }: {
    checkedList: number;
    total: number;
    // "All" is not always checked === total: in Detections it is the
    // unrestricted filter, which already includes a camera registered later.
    all: boolean;
    isOpen?: boolean;
    onclick: () => void;
  } = $props();
</script>

<button
  class="ghost"
  class:all
  {onclick}
  aria-expanded={isOpen}
  aria-label={all ? `câmeras: todas as ${total}` : `câmeras: ${checkedList} de ${total}`}
>
  <!-- Camcorder drawn by hand, in the stroke of the icons beside it: the ☰ said
       "menu", and the button opens the camera list. -->
  <Icon name="camera" size={16} stroke="var(--stroke, 1.8)" />
  <span class="text">câmeras</span>
  <span class="mono">{checkedList}<span class="total">/{total}</span></span>
</button>

<style>
  button {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 9px var(--pad-x, 12px);
    white-space: nowrap;
  }
  .all :global(svg) {
    color: var(--accent);
  }
  .total {
    color: var(--dim);
    font-size: 12px;
  }
  .text {
    display: none;
  }
  @media (min-width: 720px) {
    .text {
      display: inline;
    }
  }
</style>
