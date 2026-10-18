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
  import Icone from './Icone.svelte';
  import { dayKey, parseDay } from '../lib/format.ts';

  interface Props {
    value: string;
    days?: string[];
    oQue?: string | undefined;
    onchange: (dia: string) => void;
  }

  let { value, days = [], oQue, onchange }: Props = $props();

  function vizinho(n: number) {
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
    onclick={() => {
      if (anterior) onchange(anterior);
    }}
    disabled={!anterior}
    aria-label="dia anterior"
  >
    <Icone nome="chevronEsq" tamanho={18} />
  </button>
  <DayPicker {value} {days} {oQue} {onchange} />
  <button
    class="ghost"
    onclick={() => {
      if (proximo) onchange(proximo);
    }}
    disabled={!proximo}
    aria-label="próximo dia"
  >
    <Icone nome="chevronDir" tamanho={18} />
  </button>
</div>

<style>
  .nav-dia {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .nav-dia > button {
    padding: 9px 6px;
  }
</style>
