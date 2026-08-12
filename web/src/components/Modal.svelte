<!--
  Folha modal: o fundo escurecido, a caixa e as três formas de fechar (clique
  fora, Esc, botão de quem usa). Só a moldura mora aqui - o conteúdo vem por
  snippet, então o mesmo componente serve para o formulário de câmera e para o
  diálogo de confirmação.
-->
<script module>
  // Pilha de modais abertos. O Esc só pode fechar o de cima: com a confirmação
  // aberta sobre o formulário de edição, uma única tecla fechando os dois
  // jogaria fora o formulário que o usuário ainda estava preenchendo.
  const stack = [];
</script>

<script>
  import { onMount } from 'svelte';

  let { onclose = () => {}, children } = $props();

  const self = {};
  onMount(() => {
    stack.push(self);
    return () => stack.splice(stack.indexOf(self), 1);
  });

  function keydown(e) {
    if (e.key === 'Escape' && stack.at(-1) === self) onclose();
  }
</script>

<svelte:window onkeydown={keydown} />

<div
  class="overlay"
  role="presentation"
  onclick={(e) => e.target === e.currentTarget && onclose()}
>
  <div class="card sheet">{@render children?.()}</div>
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
    display: grid;
    /* No celular a folha sobe de baixo, onde o polegar alcança. */
    align-items: end;
    z-index: 30;
  }

  .sheet {
    display: grid;
    gap: 14px;
    width: 100%;
    max-height: 92dvh;
    overflow-y: auto;
    border-radius: var(--radius) var(--radius) 0 0;
    padding-bottom: calc(14px + env(safe-area-inset-bottom));
  }

  @media (min-width: 640px) {
    .overlay { place-items: center; padding: 20px; }
    .sheet { max-width: 460px; border-radius: var(--radius); }
  }
</style>
