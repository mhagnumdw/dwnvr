<!--
  O ladrilho de uma área que se marca e desmarca: a miniatura grande, e o nome
  e o ícone da regra embaixo. É o das áreas do alerta, no cadastro da câmera,
  onde a miniatura grande deixa ver até a área pequena.

  O quadro vem de quem monta (`fundo`): a miniatura da tira é estreita para o
  ladrilho e borraria. A regra vai só no ícone; o nome dela fica no
  aria-label, que o toque longo mostra no celular (lib/dicaDoToque.ts).

  Sem `onclick` ele só mostra: é o das áreas no card da câmera, dentro do
  bloco que já é o botão, e por isso não é botão, não marca nem esmaece. Lá, a
  área que está no alerta ganha o sino no canto da miniatura (`alerta`).
-->
<script lang="ts">
  import type { AreaDaCamera } from '../lib/api.ts';
  import { REGRAS, REGRA_PADRAO } from '../lib/area.ts';
  import Icone from './Icone.svelte';
  import MiniaturaDasAreas from './MiniaturaDasAreas.svelte';
  import SeloDeFamilias from './SeloDeFamilias.svelte';

  interface Props {
    cam: string;
    area: AreaDaCamera;
    on?: boolean;
    onclick?: () => void;
    fundo?: string | undefined;
    alerta?: boolean;
  }

  let { cam, area, on = false, onclick, fundo, alerta = false }: Props = $props();

  const regra = $derived(REGRAS[area.rule ?? REGRA_PADRAO]);
</script>

<svelte:element
  this={onclick ? 'button' : 'span'}
  type={onclick ? 'button' : undefined}
  class="ghost ladrilho"
  class:on
  class:so-mostra={!onclick}
  role={onclick ? undefined : 'img'}
  aria-pressed={onclick ? on : undefined}
  aria-label="{area.name}, regra {regra.nome}"
  data-toque-longo
  {onclick}
>
  <span class="mini">
    <MiniaturaDasAreas {cam} areas={[area]} desenho="cheia" raio={6} {fundo} zoom />
    <SeloDeFamilias familias={[]} {alerta} />
  </span>
  {#if on}
    <span class="marca"><Icone nome="marcado" tamanho={14} traco={3} /></span>
  {/if}
  <span class="rodape">
    <span class="nome">{area.name}</span>
    <Icone nome={regra.icone} tamanho={16} />
  </span>
</svelte:element>

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
  /* A que só mostra: a moldura do botão, sem ser um, e nada esmaecido. */
  .so-mostra {
    border: 1px solid var(--line);
    border-radius: var(--radius);
    color: var(--fg);
  }
  .mini {
    position: relative;
    opacity: 0.55;
  }
  .on .mini,
  .so-mostra .mini {
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
