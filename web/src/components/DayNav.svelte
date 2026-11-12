<!--
  O seletor de dia com as setas de dia anterior e próximo dia, o mesmo em toda
  tela que anda pelo histórico.

  As setas andam pelo que EXISTE na lista, pulando os buracos. Sem lista - a
  consulta que ainda não voltou, ou que falhou - elas andam de um em um dia até
  hoje: não saber onde há material não pode virar não poder navegar. É o mesmo
  modo permissivo do DayPicker.
-->
<script lang="ts">
  import DayPicker from './DayPicker.svelte';
  import Icon from './Icon.svelte';
  import { dayKey, parseDay } from '../lib/format.ts';

  interface Props {
    value: string;
    days?: string[];
    oQue?: string | undefined;
    counts?: Record<string, number> | undefined;
    onchange: (day: string) => void;
    onopen?: (() => void) | undefined;
  }

  let { value, days = [], oQue, counts, onchange, onopen }: Props = $props();

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
    <Icon name="chevronEsq" size={18} />
  </button>
  <DayPicker {value} {days} {oQue} {counts} {onchange} {onopen} />
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
