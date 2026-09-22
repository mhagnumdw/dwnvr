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

  // `largura` é o teto no desktop: o formulário lê bem em 460px, e a folha de
  // uma detecção mostra um quadro de câmera, que pede mais. Um número vale em
  // px; uma string entra como está, para quem precisa de uma conta - a folha
  // da detecção amarra a largura à altura livre (`--altura-max`), porque o
  // quadro dela é 16:9 e numa janela baixa é a largura que tem de ceder.
  //
  // `reservaRolagem` deixa o espaço da barra de rolagem sempre guardado, dos
  // dois lados para o conteúdo seguir centrado. Serve a quem tem a altura
  // presa à largura, como o palco 16:9 da detecção: sem a reserva, a barra que
  // aparece estreita o conteúdo, ele fica mais baixo, a barra some, ele volta
  // a crescer - e a folha vibra entre os dois estados a cada quadro. Com a
  // reserva a largura não depende da barra e o ciclo não tem por onde começar.
  // No celular a barra flutua sobre o conteúdo e a reserva não ocupa nada.
  let { onclose = () => {}, largura = 460, reservaRolagem = false, children } = $props();
  const larguraCSS = $derived(typeof largura === 'number' ? `${largura}px` : largura);

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
  <div class="card sheet" class:reserva-rolagem={reservaRolagem} style:--largura={larguraCSS}>{@render children?.()}</div>
</div>

<style>
  .overlay {
    /* Até onde a folha pode crescer. Mora aqui, e não solto no `max-height`,
       porque quem calcula a própria largura a partir da altura livre precisa
       do mesmo número - é o caso da folha de uma detecção. */
    --altura-max: 92dvh;

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
    max-height: var(--altura-max);
    /* Passando do teto, o grid espreme as linhas para caber. Um filho que se
       desenha por conta própria não encolhe junto e passa a pintar por cima do
       que vem depois - foi o que aconteceu com o palco 16:9 da detecção, que
       tem `overflow: hidden` e por isso aceita ser espremido até zero. Com as
       linhas em `min-content` ninguém é espremido e a folha rola. */
    grid-auto-rows: min-content;
    overflow-y: auto;
    border-radius: var(--radius) var(--radius) 0 0;
    padding-bottom: calc(14px + env(safe-area-inset-bottom));
  }

  .sheet.reserva-rolagem {
    scrollbar-gutter: stable both-edges;
  }

  @media (min-width: 640px) {
    .overlay { place-items: center; padding: 20px; }
    .sheet { max-width: var(--largura); border-radius: var(--radius); }
  }
</style>
