<script lang="ts">
  // O botão de entrar e sair da tela cheia: os quatro cantos apontando para
  // fora, e para dentro quando ela já está ligada. Quem decide o QUE vai para a
  // tela cheia é quem usa, pelo `onclick`.
  import Icon from './Icon.svelte';

  interface Props {
    active: boolean;
    // Sobre a imagem precisa de fundo próprio: o vídeo pode ser claro.
    overVideo?: boolean;
    onclick: () => void;
  }

  let { active, overVideo = false, onclick }: Props = $props();
</script>

<!-- Dois cliques rápidos no botão não podem chegar ao duplo clique do vídeo,
     que também alterna a tela cheia: seriam três trocas em vez de duas. -->
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
