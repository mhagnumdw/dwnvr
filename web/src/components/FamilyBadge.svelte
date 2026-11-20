<!--
  The object family icons in a thumbnail corner: one per family present, in
  priority order. It is the same badge in the Detections grid and in the
  Recordings thumbnail strip. Whoever uses it gives the thumbnail `position:
  relative` and puts the families in the button name (`familyNames`): the icons
  are images without text, and the name is what the screen reader reads. In
  Detections, the detection that alerted gets the bell before the families; on
  the camera card, the alert area gets only the bell, with no family.
-->
<script lang="ts">
  import Icon from './Icon.svelte';
  import { family, familyIcon, iconURL, byPriority } from '../lib/icons.ts';

  interface Props {
    // The families, without repetition; the badge sets the order.
    families: readonly string[];
    // Some object in the detection alerted.
    alert?: boolean;
  }

  let { families, alert = false }: Props = $props();

  const order = $derived(byPriority(families));
</script>

{#if order.length || alert}
  <span class="badge">
    {#if alert}<span class="bell"><Icon name="bell" size={12} stroke={2.4} /></span>{/if}
    {#each order as f (f)}
      {@const icon = familyIcon(f)}
      {#if icon}<img src={iconURL(icon, 12, family(f).color)} alt="" width="12" height="12" />{/if}
    {/each}
  </span>
{/if}

<style>
  .badge {
    position: absolute;
    right: 4px;
    top: 4px;
    display: inline-flex;
    gap: 4px;
    padding: 3px 4px;
    border-radius: 6px;
    background: rgba(13, 17, 23, 0.8);
  }

  img {
    display: block;
  }

  .bell {
    color: var(--warn);
  }
</style>
