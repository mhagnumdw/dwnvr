<!--
  The label of an area: its thumbnail, the name and the rule in full words. It
  is the content of the area row in the Areas tab of the camera form and in the
  ⋮ of the Detections card; the surrounding row belongs to whoever uses it,
  because each does something different (there edit and delete, here check the
  filter).
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
    // The name in bold: in the Areas tab, where the area is the subject of the
    // row.
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
      {rule.name}
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
