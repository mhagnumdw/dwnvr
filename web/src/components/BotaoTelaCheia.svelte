<script lang="ts">
  // O botão de entrar e sair da tela cheia: os quatro cantos apontando para
  // fora, e para dentro quando ela já está ligada. Quem decide o QUE vai para a
  // tela cheia é quem usa, pelo `onclick`.
  interface Props {
    ativa: boolean;
    // Sobre a imagem precisa de fundo próprio: o vídeo pode ser claro.
    sobreVideo?: boolean;
    onclick: () => void;
  }

  let { ativa, sobreVideo = false, onclick }: Props = $props();
</script>

<!-- Dois cliques rápidos no botão não podem chegar ao duplo clique do vídeo,
     que também alterna a tela cheia: seriam três trocas em vez de duas. -->
<button
  class="ghost"
  class:sobreVideo
  {onclick}
  ondblclick={(e) => {
    e.stopPropagation();
  }}
  aria-label={ativa ? 'sair da tela cheia' : 'tela cheia'}
  title={ativa ? 'sair da tela cheia (Esc)' : 'tela cheia (ou duplo clique no vídeo)'}
>
  <svg viewBox="0 0 24 24" aria-hidden="true">
    {#if ativa}
      <path d="M9 4v5H4M20 9h-5V4M15 20v-5h5M4 15h5v5" />
    {:else}
      <path d="M4 9V4h5M15 4h5v5M20 15v5h-5M9 20H4v-5" />
    {/if}
  </svg>
</button>

<style>
  button {
    display: grid;
    place-items: center;
    width: 44px;
    padding: 0;
  }

  button.sobreVideo {
    background: rgba(0, 0, 0, 0.55);
    border-color: rgba(255, 255, 255, 0.18);
  }

  svg {
    width: 22px;
    height: 22px;
    fill: none;
    stroke: currentColor;
    stroke-width: 2;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
</style>
