<!--
  O seletor de dia com as setas de dia anterior e próximo dia, o mesmo em toda
  tela que anda pelo histórico.

  As setas andam pelo que EXISTE na lista, pulando os buracos. Sem lista - a
  consulta que ainda não voltou, ou que falhou - elas andam de um em um dia até
  hoje: não saber onde há material não pode virar não poder navegar. É o mesmo
  modo permissivo do DayPicker.
-->
<script>
  import DayPicker from './DayPicker.svelte';
  import { dayKey, parseDay } from '../lib/format.js';

  /** @type {{ value: string, days?: string[], oQue?: string, onchange: (dia: string) => void }} */
  let { value, days = [], oQue, onchange } = $props();

  function vizinho(n) {
    const d = parseDay(value);
    d.setDate(d.getDate() + n);
    return dayKey(d);
  }

  const anterior = $derived(days.length ? days.findLast((d) => d < value) : vizinho(-1));
  const proximo = $derived(
    days.length ? days.find((d) => d > value) : value < dayKey() ? vizinho(1) : undefined,
  );
</script>

<div class="nav-dia">
  <button
    class="ghost"
    onclick={() => onchange(anterior)}
    disabled={!anterior}
    aria-label="dia anterior"
  >
    ‹
  </button>
  <DayPicker {value} {days} {oQue} {onchange} />
  <button
    class="ghost"
    onclick={() => onchange(proximo)}
    disabled={!proximo}
    aria-label="próximo dia"
  >
    ›
  </button>
</div>

<style>
  .nav-dia {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .nav-dia > button {
    padding: 9px 12px;
  }
</style>
