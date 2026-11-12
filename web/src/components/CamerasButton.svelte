<!--
  O botão que abre a lista de câmeras, no Ao vivo e em Detecções. Mostra as
  marcadas do total ("5/9"): só a contagem não dizia se havia mais câmeras.
  A barra ocupa o lugar dos parênteses que havia, e o total vem cinza e menor,
  então o botão cresce só ~2px. Com todas marcadas o ícone fica azul.

    <BotaoCameras marcadas={5} total={9} todas={false} onclick={...} />

  No celular fica só o ícone e a contagem; o nome volta do tablet para cima.
  Cada tela casa o botão com os vizinhos pelas custom properties `--traco`
  (espessura do ícone) e `--pad-x`.
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
    // "Todas" não é sempre marcadas === total: em Detecções é o filtro sem
    // restrição, que já inclui a câmera cadastrada depois.
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
  <!-- Filmadora desenhada à mão, no traço dos ícones ao lado: o ☰ dizia
       "menu", e o botão abre a lista de câmeras. -->
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
