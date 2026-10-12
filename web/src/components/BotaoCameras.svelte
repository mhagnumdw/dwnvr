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
  let {
    marcadas,
    total,
    todas,
    aberto,
    onclick,
  }: {
    marcadas: number;
    total: number;
    // "Todas" não é sempre marcadas === total: em Detecções é o filtro sem
    // restrição, que já inclui a câmera cadastrada depois.
    todas: boolean;
    aberto?: boolean;
    onclick: () => void;
  } = $props();
</script>

<button
  class="ghost"
  class:todas
  {onclick}
  aria-expanded={aberto}
  aria-label={todas ? `câmeras: todas as ${total}` : `câmeras: ${marcadas} de ${total}`}
>
  <!-- Filmadora desenhada à mão, no traço dos ícones ao lado: o ☰ dizia
       "menu", e o botão abre a lista de câmeras. -->
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <rect x="2" y="6" width="13.5" height="12" rx="2" />
    <path d="M15.5 10.5 22 7v10l-6.5-3.5z" stroke-linejoin="round" />
  </svg>
  <span class="texto">câmeras</span>
  <span class="mono">{marcadas}<span class="total">/{total}</span></span>
</button>

<style>
  button {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 9px var(--pad-x, 12px);
    white-space: nowrap;
  }
  svg {
    display: block;
    width: 16px;
    height: 16px;
    flex: none;
    fill: none;
    stroke: currentColor;
    stroke-width: var(--traco, 1.8);
  }
  .todas svg {
    stroke: var(--accent);
  }
  .total {
    color: var(--dim);
    font-size: 12px;
  }
  .texto {
    display: none;
  }
  @media (min-width: 720px) {
    .texto {
      display: inline;
    }
  }
</style>
