<script lang="ts">
  // Um ícone da interface, pelo nome que ele tem em `lib/iconesUi.ts`. Segue a
  // cor do texto em volta (currentColor), e o tamanho vem de quem usa: em
  // pixels, ou o do texto por padrão.
  import { ICONES_UI, TRACO_PADRAO, type NomeDoIconeUi } from '../lib/iconesUi.ts';

  interface Props {
    nome: NomeDoIconeUi;
    tamanho?: number | string;
    // Número, ou um `var(--x, n)` quando a tela casa o traço com os vizinhos.
    traco?: number | string;
  }

  let { nome, tamanho = '1em', traco = TRACO_PADRAO }: Props = $props();
</script>

<svg
  width={tamanho}
  height={tamanho}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  style:stroke-width={traco}
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
>
  {#each ICONES_UI[nome] as [tag, atributos] (atributos)}
    <svelte:element this={tag} {...atributos} />
  {/each}
</svg>

<style>
  svg {
    display: block;
    flex: none;
  }
</style>
