<!--
  The day picker with previous-day and next-day arrows, the same on every screen
  that walks through the history.

  The arrows walk through what EXISTS in the list, skipping the gaps. Without a
  list - the query that has not returned yet, or failed - they walk one day at a
  time up to today: not knowing where there is material must not turn into not
  being able to navigate. It is the same permissive mode as DayPicker.
-->
<script lang="ts">
  import DayPicker from './DayPicker.svelte';
  import Icon from './Icon.svelte';
  import { dayKey, parseDay } from '../lib/format.ts';

  interface Props {
    value: string;
    days?: string[];
    material?: string | undefined;
    counts?: Record<string, number> | undefined;
    onchange: (day: string) => void;
    onopen?: (() => void) | undefined;
  }

  let { value, days = [], material, counts, onchange, onopen }: Props = $props();

  function neighbor(n: number) {
    const d = parseDay(value);
    d.setDate(d.getDate() + n);
    return dayKey(d);
  }

  const previous = $derived(days.length ? days.findLast((d) => d < value) : neighbor(-1));
  const next = $derived(
    days.length ? days.find((d) => d > value) : value < dayKey() ? neighbor(1) : undefined,
  );
</script>

<div class="day-nav">
  <button
    class="ghost"
    onclick={() => {
      if (previous) onchange(previous);
    }}
    disabled={!previous}
    aria-label="dia anterior"
  >
    <Icon name="chevronLeft" size={18} />
  </button>
  <DayPicker {value} {days} {material} {counts} {onchange} {onopen} />
  <button
    class="ghost"
    onclick={() => {
      if (next) onchange(next);
    }}
    disabled={!next}
    aria-label="próximo dia"
  >
    <Icon name="chevronDir" size={18} />
  </button>
</div>

<style>
  .day-nav {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .day-nav > button {
    padding: 9px 6px;
  }
</style>
