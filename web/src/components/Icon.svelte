<script lang="ts">
  // Um ícone da interface, pelo nome que ele tem em `lib/iconesUi.ts`. Segue a
  // cor do texto em volta (currentColor), e o tamanho vem de quem usa: em
  // pixels, ou o do texto por padrão.
  import { UI_ICONS, DEFAULT_STROKE, type UiIconName } from '../lib/uiIcons.ts';

  interface Props {
    name: UiIconName;
    size?: number | string;
    // Número, ou um `var(--x, n)` quando a tela casa o traço com os vizinhos.
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
