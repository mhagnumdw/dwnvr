<!--
  O chip de uma área que se marca e desmarca: a miniatura dela, o nome e o
  ícone da regra. É o do popover "Filtrar por área" de Detecções, com a forma
  do chip do SeletorCameras (borda azul na marcada).

  A regra vai só no ícone, para o chip caber; o nome dela fica no aria-label,
  que o toque longo mostra no celular (lib/dicaDoToque.ts).
-->
<script lang="ts">
  import type { AreaDaCamera } from '../lib/api.ts';
  import { REGRAS, REGRA_PADRAO } from '../lib/area.ts';
  import Icone from './Icone.svelte';
  import MiniaturaDasAreas from './MiniaturaDasAreas.svelte';

  interface Props {
    cam: string;
    area: AreaDaCamera;
    on: boolean;
    onclick: () => void;
  }

  let { cam, area, on, onclick }: Props = $props();

  const regra = $derived(REGRAS[area.rule ?? REGRA_PADRAO]);
</script>

<button
  type="button"
  class="chip-da-area"
  class:on
  aria-pressed={on}
  aria-label="{area.name}, regra {regra.nome}"
  data-toque-longo
  {onclick}
>
  <span class="mini">
    <MiniaturaDasAreas {cam} areas={[area]} largura={48} desenho="cheia" raio={6} zoom />
  </span>
  <span class="nome">{area.name}</span>
  <Icone nome={regra.icone} tamanho={16} />
</button>

<style>
  .chip-da-area {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    min-height: 36px;
    max-width: 100%;
    padding: 3px 10px 3px 4px;
    border-radius: 10px;
    font-weight: 600;
    font-size: 14px;
    color: var(--dim);
  }
  .chip-da-area.on {
    color: var(--fg);
    border-color: var(--accent);
    background: var(--ativo-fundo);
  }
  .mini {
    flex: none;
    opacity: 0.6;
  }
  .on .mini {
    opacity: 1;
  }
  .nome {
    min-width: 0;
    overflow-wrap: anywhere;
    text-align: left;
  }
  .chip-da-area :global(svg) {
    flex: none;
  }
</style>
