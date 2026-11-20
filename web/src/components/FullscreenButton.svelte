<script lang="ts">
  // The button to enter and exit fullscreen: the four corners pointing outward,
  // and inward when it is already on. WHAT goes fullscreen is decided by
  // whoever uses it, through `onclick`.
  import Icon from './Icon.svelte';

  interface Props {
    active: boolean;
    // Over the image it needs its own background: the video can be bright.
    overVideo?: boolean;
    onclick: () => void;
  }

  let { active, overVideo = false, onclick }: Props = $props();
</script>

<!-- Two quick clicks on the button must not reach the video's double click,
     which also toggles fullscreen: that would be three toggles instead of two. -->
<button
  class="ghost"
  class:overVideo
  {onclick}
  ondblclick={(e) => {
    e.stopPropagation();
  }}
  aria-label={active ? 'sair da tela cheia' : 'tela cheia'}
  title={active ? 'sair da tela cheia (Esc)' : 'tela cheia (ou duplo clique no vídeo)'}
>
  <Icon name={active ? 'fullscreenExit' : 'fullscreen'} size={22} />
</button>

<style>
  button {
    display: grid;
    place-items: center;
    width: 44px;
    padding: 0;
  }

  button.overVideo {
    background: rgba(0, 0, 0, 0.55);
    border-color: rgba(255, 255, 255, 0.18);
  }
</style>
