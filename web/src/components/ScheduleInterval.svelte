<script lang="ts">
  // The time interval of the Detections screen: off, it is a button with the
  // clock, which opens the "De" and "Até" fields; on, it becomes a chip with
  // the two times and the ✕ that turns it off. Tapping the times on the chip
  // reopens the fields.
  //
  // The times are minutes of the day. An "até" smaller than "de" ends on the
  // next day: 22:00 to 06:00 is the night. The screen decides the day.
  import TimeField from './TimeField.svelte';
  import Icon from './Icon.svelte';
  import { hourOfMinute as hour } from '../lib/format.ts';

  interface Props {
    interval: { from: number; to: number } | null;
    onapply: (from: number, to: number) => void;
    onclear: () => void;
  }

  let { interval, onapply, onclear }: Props = $props();

  let root: HTMLDivElement | undefined;
  let isOpen = $state(false);
  let from = $state<number | null>(null);
  let to = $state<number | null>(null);

  function openFields() {
    if (isOpen) {
      isOpen = false;
      return;
    }
    from = interval?.from ?? null;
    to = interval?.to ?? null;
    isOpen = true;
  }

  function changeFrom(m: number | null) {
    from = m;
  }

  function changeTo(m: number | null) {
    to = m;
  }

  function apply() {
    if (from === null || to === null) return;
    isOpen = false;
    onapply(from, to);
  }

  function fora(e: MouseEvent) {
    if (isOpen && root && !(e.target instanceof Node && root.contains(e.target))) isOpen = false;
  }

  function keyHandler(e: KeyboardEvent) {
    if (e.key === 'Escape') isOpen = false;
  }

  function clear() {
    isOpen = false;
    onclear();
  }
</script>

<svelte:window onclick={fora} onkeydown={keyHandler} />

<div class="interval" class:enabled={interval} bind:this={root}>
  {#if interval}
    <span class="interval-chip">
      <button
        class="ghost hours mono"
        onclick={openFields}
        aria-expanded={isOpen}
        title="mudar o intervalo"
        aria-label="intervalo de {hour(interval.from)} até {hour(interval.to)}; mudar"
      >
        <Icon name="clock" size={14} stroke={2.4} />
        {hour(interval.from)} – {hour(interval.to)}
        {#if interval.to < interval.from}<small>(dia seguinte)</small>{/if}
      </button>
      <button
        class="ghost clear"
        onclick={clear}
        aria-label="limpar o intervalo"
        title="limpar o intervalo"
      >
        <Icon name="close" size={16} stroke={2.2} />
      </button>
    </span>
  {:else}
    <button
      class="ghost open"
      onclick={openFields}
      aria-expanded={isOpen}
      aria-label="intervalo de horário"
      title="mostrar só um intervalo de horário"
    >
      <Icon name="clock" size={16} stroke={2.2} />
      <span class="label">intervalo</span>
    </button>
  {/if}

  {#if isOpen}
    <div class="popover" role="dialog" aria-label="intervalo de horário">
      <div class="fields">
        <TimeField label="De" value={from} onchange={changeFrom} onenter={apply} focusNow />
        <TimeField label="Até" value={to} onchange={changeTo} onenter={apply} />
      </div>
      <p class="hint">
        Se o "até" for menor que o "de", o intervalo vai até o dia seguinte: 22:00 até 06:00 é a
        noite.
      </p>
      <div class="actions">
        {#if interval}<button class="ghost" onclick={clear}>limpar</button>{/if}
        <button class="primary" onclick={apply} disabled={from === null || to === null}
          >aplicar</button
        >
      </div>
    </div>
  {/if}
</div>

<style>
  .interval {
    position: relative;
    flex: none;
  }

  /* The shape of the neighboring buttons in the bar. */
  .open {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 8px 11px;
    color: var(--dim);
  }

  /* On: the bar buttons' "on" (accent border and background). */
  .interval-chip {
    display: inline-flex;
    align-items: stretch;
    border: 1px solid var(--accent);
    border-radius: 8px;
    background: var(--active-bg);
    overflow: hidden;
  }
  .interval-chip button {
    min-height: 42px;
    border: 0;
    border-radius: 0;
  }
  .hours {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 0 4px 0 11px;
    white-space: nowrap;
  }
  .hours small {
    color: var(--dim);
    font-size: 12px;
  }
  .clear {
    padding: 0 10px;
    color: var(--dim);
  }
  @media (hover: hover) {
    .clear:hover {
      color: var(--fg);
    }
  }

  .popover {
    position: absolute;
    top: calc(100% + 6px);
    left: 0;
    z-index: 40;
    width: 260px;
    display: grid;
    gap: 12px;
    padding: 14px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    box-shadow: 0 8px 24px var(--shadow);
  }

  .fields {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
  }

  .hint {
    margin: 0;
    font-size: 12px;
    line-height: 1.4;
    color: var(--dim);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  /* On the phone the button sits near the right edge and the popover would
     spill off the screen: without a position of its own, it attaches to the
     first positioned ancestor - the screen's bar, which is sticky - and opens
     right below it. */
  @media (max-width: 719px) {
    .interval {
      position: static;
    }
    /* Only the icon, like the neighbors in the bar. When on, the chip does not
       fit on the day row and the screen puts it on a row of its own, edge to
       edge. */
    .label {
      display: none;
    }
    .interval-chip {
      width: 100%;
    }
    .hours {
      flex: 1;
    }
    .popover {
      top: 100%;
      left: 10px;
      width: min(300px, calc(100% - 20px));
    }
  }
</style>
