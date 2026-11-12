<!--
  O rótulo de uma área: a miniatura dela, o nome e a regra por extenso. É o
  conteúdo da linha da área na aba Áreas do cadastro e no ⋮ do card de
  Detecções; a linha em volta é de quem usa, porque cada uma faz uma coisa (lá
  editar e apagar, aqui marcar o filtro).
-->
<script lang="ts">
  import type { CameraArea } from '../lib/api.ts';
  import { RULES, DEFAULT_RULE } from '../lib/area.ts';
  import Icon from './Icon.svelte';
  import AreaThumbnail from './AreaThumbnail.svelte';

  interface Props {
    cam: string;
    area: CameraArea;
    width?: number;
    // O nome em negrito: na aba Áreas, onde a área é o assunto da linha.
    bold?: boolean;
    background?: string | undefined;
  }

  let { cam, area, width = 64, bold = false, background }: Props = $props();

  const rule = $derived(RULES[area.rule ?? DEFAULT_RULE]);
</script>

<span class="label" data-toque-longo>
  <AreaThumbnail {cam} areas={[area]} {width} drawing="cheia" radius={4} {background} zoom />
  <span class="text">
    <span class="name" class:bold>{area.name}</span>
    <span class="muted small rule">
      <Icon name={rule.icon} size={16} />
      {rule.nome}
    </span>
  </span>
</span>

<style>
  .label {
    display: flex;
    flex: 1;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }
  .text {
    display: grid;
    min-width: 0;
    line-height: 1.35;
  }
  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .bold {
    font-weight: 700;
  }
  .rule {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
</style>
