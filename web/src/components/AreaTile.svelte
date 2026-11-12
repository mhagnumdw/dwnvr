<!--
  O ladrilho de uma área que se marca e desmarca: a miniatura grande, e o nome
  e o ícone da regra embaixo. É o das áreas do alerta, no cadastro da câmera,
  e o do popover "área" de Detecções, onde a miniatura grande deixa ver até a
  área pequena.

  O quadro vem de quem monta (`fundo`) quando já o tem, como o cadastro; sem
  ele, a miniatura pede a da tira na largura em que é desenhada, para não
  borrar. A regra vai só no ícone; o nome dela fica no aria-label, que o
  toque longo mostra no celular (lib/dicaDoToque.ts).

  Sem `onclick` ele só mostra: é o das áreas no card da câmera, dentro do
  bloco que já é o botão, e por isso não é botão, não marca nem esmaece. Lá, a
  área que está no alerta ganha o sino no canto da miniatura (`alerta`).
-->
<script lang="ts">
  import type { CameraArea } from '../lib/api.ts';
  import { RULES, DEFAULT_RULE } from '../lib/area.ts';
  import Icon from './Icon.svelte';
  import AreaThumbnail from './AreaThumbnail.svelte';
  import FamilyBadge from './FamilyBadge.svelte';

  interface Props {
    cam: string;
    area: CameraArea;
    on?: boolean;
    onclick?: () => void;
    background?: string | undefined;
    alert?: boolean;
  }

  let { cam, area, on = false, onclick, background, alert = false }: Props = $props();

  const rule = $derived(RULES[area.rule ?? DEFAULT_RULE]);
</script>

<svelte:element
  this={onclick ? 'button' : 'span'}
  type={onclick ? 'button' : undefined}
  class="ghost tile"
  class:on
  class:show-only={!onclick}
  role={onclick ? undefined : 'img'}
  aria-pressed={onclick ? on : undefined}
  aria-label="{area.name}, regra {rule.nome}"
  data-toque-longo
  {onclick}
>
  <span class="mini">
    <AreaThumbnail {cam} areas={[area]} drawing="cheia" radius={6} {background} zoom />
    <FamilyBadge families={[]} {alert} />
  </span>
  {#if on}
    <span class="mark"><Icon name="checkmark" size={14} stroke={3} /></span>
  {/if}
  <span class="footer">
    <span class="name">{area.name}</span>
    <Icon name={rule.icon} size={16} />
  </span>
</svelte:element>

<style>
  .tile {
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
  .tile.on {
    color: var(--fg);
    border-color: var(--accent);
    background: var(--ativo-fundo);
  }
  /* A que só mostra: a moldura do botão, sem ser um, e nada esmaecido. */
  .show-only {
    border: 1px solid var(--line);
    border-radius: var(--radius);
    color: var(--fg);
  }
  .mini {
    position: relative;
    opacity: 0.55;
  }
  .on .mini,
  .show-only .mini {
    opacity: 1;
  }
  .mark {
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
  .footer {
    display: flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
    padding: 0 2px;
  }
  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .footer :global(svg) {
    flex: none;
  }
</style>
