<!--
  The tile of an area that is checked and unchecked: the large thumbnail, and
  the name and the rule icon below. It is the one for the alert areas, in the
  camera form, and for the "área" popover in Detections, where the large
  thumbnail lets you see even the small area.

  The frame comes from whoever mounts it (`background`) when it already has one,
  like the form; without it, the thumbnail requests the strip's one at the width
  it is drawn, so it does not blur. The rule goes only in the icon; its name
  stays in the aria-label, which the long press shows on the phone
  (lib/touchHint.ts).

  Without `onclick` it only displays: it is the one for the areas on the camera
  card, inside the block that is already the button, and so it is not a button,
  does not check and does not dim. There, an area that is in the alert gets the
  bell in the thumbnail corner (`alert`).
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
  aria-label="{area.name}, regra {rule.name}"
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
    background: var(--active-bg);
  }
  /* The display-only one: the button's frame, without being one, and nothing dimmed. */
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
