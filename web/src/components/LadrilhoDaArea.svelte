<!--
  O ladrilho de uma área que se marca e desmarca: a miniatura grande, e o nome
  e o ícone da regra embaixo. É o das áreas do alerta, no cadastro da câmera,
  onde a miniatura grande deixa ver até a área pequena.

  O quadro vem de quem monta (`fundo`): a miniatura da tira é estreita para o
  ladrilho e borraria. A regra vai só no ícone; o nome dela fica no
  aria-label, que o toque longo mostra no celular (lib/dicaDoToque.ts).
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
    fundo?: string | undefined;
  }

  let { cam, area, on, onclick, fundo }: Props = $props();

  const regra = $derived(REGRAS[area.rule ?? REGRA_PADRAO]);
</script>

<button
  type="button"
  class="ghost ladrilho"
  class:on
  aria-pressed={on}
  aria-label="{area.name}, regra {regra.nome}"
  {onclick}
>
  <span class="mini">
    <MiniaturaDasAreas {cam} areas={[area]} desenho="cheia" raio={6} {fundo} />
  </span>
  {#if on}
    <span class="marca"><Icone nome="marcado" tamanho={14} traco={3} /></span>
  {/if}
  <span class="rodape">
    <span class="nome">{area.name}</span>
    <Icone nome={regra.icone} tamanho={16} />
  </span>
</button>

<style>
  .ladrilho {
    position: relative;
    display: grid;
    gap: 4px;
    min-width: 0;
    min-height: 0;
    padding: 4px;
    text-align: left;
    font-size: 13px;
    font-weight: 600;
    color: var(--dim);
  }
  .ladrilho.on {
    color: var(--fg);
    border-color: var(--accent);
    background: var(--ativo-fundo);
  }
  .mini {
    opacity: 0.55;
  }
  .on .mini {
    opacity: 1;
  }
  .marca {
    position: absolute;
    top: 8px;
    right: 8px;
    display: grid;
    place-items: center;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    background: var(--ok);
    color: #fff;
  }
  .rodape {
    display: flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
    padding: 0 2px;
  }
  .nome {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .rodape :global(svg) {
    flex: none;
  }
</style>
