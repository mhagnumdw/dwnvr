<!--
  The Schedules section of the Alert tab: the box that limits the alert, the
  week grid, and the typed list, which go together. Typing repaints the grid;
  painting rebuilds the whole list in canonical form (lib/schedules.ts).

  The box and the list live in the form, like the alert's "where": the form's
  save is what checks what is missing and deletes the schedules when the box is
  unchecked.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import TimeField from './TimeField.svelte';
  import ScheduleGrid from './ScheduleGrid.svelte';
  import Icon from './Icon.svelte';
  import {
    DIAS,
    LONG_DAYS,
    coverage,
    schedulesOf,
    paintHours,
    hoursSummary,
    daysSummary,
  } from '../lib/weekGrid.ts';
  import { DAY, MAX_SCHEDULES, newSchedule, valid } from '../lib/schedules.ts';
  import type { Schedule } from '../lib/schedules.ts';

  interface Props {
    limits?: boolean;
    schedules?: Schedule[];
    // "America/Fortaleza (UTC−03:00)", from /api/health; empty before the read.
    timezone: string;
    // After a blocked save: the half-filled schedule turns red.
    markIncomplete: boolean;
    onchange: () => void;
  }

  let {
    limits = $bindable(false),
    schedules = $bindable([]),
    timezone,
    markIncomplete,
    onchange,
  }: Props = $props();

  const listCoverage = $derived(coverage(schedules));

  // The row with focus: the "Até" before the "De" only turns red outside it,
  // because mid-typing ("1" on the way to "18:00") it is still provisional.
  let focused = $state<number | null>(null);

  // The schedule under the mouse.
  let sobOMouse = $state<number | null>(null);

  // The grid outlines the schedule under the mouse and, with the mouse over
  // none, the one with focus. The two are kept separate so a mouse that leaves
  // does not erase the outline of the focus that stayed.
  const isHighlighted = $derived(sobOMouse ?? focused);
  const highlight = $derived.by(() => {
    const h = schedules.find((x) => x.key === isHighlighted);
    return h && valid(h) ? coverage([h]) : null;
  });

  // The list was just rebuilt by the grid: the new rows flash.
  let redone = $state(false);

  let list = $state<HTMLElement>();

  function change(newOne: Schedule[], byGrid = false) {
    schedules = newOne;
    redone = byGrid;
    onchange();
  }

  function paint(d0: number, h0: number, d1: number, h1: number, mark: boolean) {
    const c = paintHours(listCoverage, d0, h0, d1, h1, mark);
    // What was half-filled (no day or no time) goes to the end, so it does not
    // vanish.
    change([...schedulesOf(c), ...schedules.filter((h) => !valid(h))], true);
  }

  const swap = (key: number, campos: Partial<Schedule>) =>
    change(schedules.map((h) => (h.key === key ? { ...h, ...campos } : h)));

  function toggleDay(h: Schedule, d: number) {
    const days = h.days.includes(d)
      ? h.days.filter((x) => x !== d)
      : [...h.days, d].sort((a, b) => a - b);
    swap(h.key, { days });
  }

  function remove(key: number) {
    if (sobOMouse === key) sobOMouse = null;
    if (focused === key) focused = null;
    change(schedules.filter((h) => h.key !== key));
  }

  async function add(...newOnes: Schedule[]) {
    change([...schedules, ...newOnes]);
    await tick();
    const lastOne = list?.lastElementChild as HTMLElement | null;
    lastOne?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    // The empty schedule of the "new schedule" already gets focus on the first
    // day.
    if (newOnes.some((h) => !valid(h)))
      lastOne?.querySelector<HTMLElement>('.day')?.focus({ preventScroll: true });
  }

  const WHOLE_DAY = [0, 1, 2, 3, 4, 5, 6];

  function note(h: Schedule) {
    if (valid(h))
      return { bad: false, texto: `${daysSummary(h.days)} · ${hoursSummary(h.from, h.to)}` };
    if (h.from !== null && h.to !== null && h.to <= h.from)
      return {
        bad: focused !== h.key || markIncomplete,
        texto:
          'O "Até" precisa ser depois do "De". Para passar da meia-noite, use dois horários: um até 23:59 e outro a partir de 00:00, no dia seguinte.',
      };
    const noDay = !h.days.length;
    const noHour = h.from === null || h.to === null;
    return {
      bad: markIncomplete,
      texto:
        noDay && noHour
          ? 'Escolha os dias e o horário.'
          : noDay
            ? 'Escolha ao menos um dia.'
            : 'Falta o horário.',
    };
  }
</script>

<section class="schedules" aria-label="os horários do alerta">
  <div class="title"><Icon name="clock" size={16} stroke={2.2} />Horários</div>
  <label class="check">
    <input
      type="checkbox"
      checked={limits}
      onchange={(e) => {
        limits = e.currentTarget.checked;
        onchange();
      }}
    />
    Limitar o alerta a alguns horários
  </label>

  {#if !limits}
    <small class="muted">O alerta vale a qualquer hora.</small>
  {:else}
    <small class="muted timezone">No fuso do servidor{timezone ? `: ${timezone}` : ''}.</small>

    <ScheduleGrid coverage={listCoverage} {highlight} onpaint={paint} />

    <div class="title smaller">
      Os horários{#if schedules.length}<span class="muted">({schedules.length})</span>{/if}
    </div>
    <div class="list" class:redone bind:this={list}>
      {#each schedules as h (h.key)}
        {@const n = note(h)}
        <div
          class="schedule"
          class:incomplete={n.bad}
          role="group"
          aria-label="horário"
          onpointerenter={() => (sobOMouse = h.key)}
          onpointerleave={() => (sobOMouse = null)}
          onfocusin={() => (focused = h.key)}
          onfocusout={() => (focused = null)}
        >
          <div class="dias" role="group" aria-label="os dias">
            {#each DIAS as name, d (d)}
              <button
                type="button"
                class="day"
                title={LONG_DAYS[d]}
                aria-pressed={h.days.includes(d)}
                onclick={() => toggleDay(h, d)}>{name}</button
              >
            {/each}
          </div>
          <div class="hours">
            <TimeField
              label="De"
              value={h.from}
              onchange={(m: number | null) => swap(h.key, { from: m })}
            />
            <TimeField
              label="Até"
              value={h.to}
              onchange={(m: number | null) => swap(h.key, { to: m })}
            />
            <button
              type="button"
              class="ghost remove"
              title="remover este horário"
              aria-label="remover este horário"
              onclick={() => remove(h.key)}
            >
              <Icon name="close" size={18} stroke={2.4} />
            </button>
          </div>
          <div class="note" class:bad={n.bad}>{n.texto}</div>
        </div>
      {:else}
        <p class="muted small empty">Nenhum horário ainda: marque na grade, ou digite um novo.</p>
      {/each}
    </div>

    <div class="actions">
      <button
        type="button"
        class="mais"
        disabled={schedules.length >= MAX_SCHEDULES}
        onclick={() => void add(newSchedule())}
      >
        <Icon name="mais" size={16} stroke={2.4} />novo horário
      </button>
      <span class="space"></span>
      <button
        type="button"
        class="ghost shortcut"
        title="dois horários: todo dia das 22:00 às 23:59 e das 00:00 às 05:59"
        disabled={schedules.length + 2 > MAX_SCHEDULES}
        onclick={() =>
          void add(
            newSchedule([...WHOLE_DAY], 22 * 60, DAY - 1),
            newSchedule([...WHOLE_DAY], 0, 6 * 60 - 1),
          )}>+ toda noite</button
      >
      <button
        type="button"
        class="ghost shortcut"
        title="sábado e domingo, o dia inteiro"
        disabled={schedules.length >= MAX_SCHEDULES}
        onclick={() => void add(newSchedule([0, 6], 0, DAY - 1))}>+ fim de semana</button
      >
    </div>
  {/if}
</section>

<style>
  .schedules {
    display: grid;
    gap: 12px;
    padding-top: 14px;
    border-top: 1px solid var(--line);
  }
  .title {
    display: flex;
    align-items: center;
    gap: 8px;
    font-weight: 600;
    font-size: 14px;
  }
  .title.smaller {
    gap: 6px;
    font-size: 13px;
    margin-bottom: -4px;
  }
  .title .muted {
    font-weight: 400;
  }
  .timezone {
    margin: -4px 0 0;
  }

  label.check {
    display: flex;
    align-items: center;
    gap: 9px;
    color: var(--fg);
    font-size: 15px;
  }
  label.check input {
    width: 18px;
    height: 18px;
    min-height: 0;
    flex: none;
    accent-color: var(--accent);
  }
  small {
    font-size: 12px;
  }

  .list {
    display: grid;
    gap: 8px;
  }
  .list.redone .schedule {
    animation: refeita 0.9s ease-out;
  }
  @keyframes refeita {
    from {
      border-color: var(--accent);
      background: var(--ativo-fundo);
    }
  }
  .empty {
    margin: 0;
  }
  .schedule {
    display: grid;
    gap: 8px;
    padding: 10px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--bg);
  }
  .schedule.incomplete {
    border-color: color-mix(in srgb, var(--bad) 45%, var(--line));
  }
  .dias {
    display: grid;
    grid-template-columns: repeat(7, minmax(0, 1fr));
    gap: 4px;
  }
  .day {
    min-height: 36px;
    padding: 0;
    font-size: 13px;
    color: var(--dim);
    border-radius: 6px;
  }
  .day[aria-pressed='true'] {
    color: var(--fg);
    background: var(--ativo-fundo);
    border-color: var(--accent);
  }
  .hours {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 44px;
    gap: 8px;
    align-items: end;
  }
  .remove {
    padding: 0;
    min-height: 44px;
    display: grid;
    place-items: center;
    color: var(--dim);
  }
  .note {
    margin-top: -2px;
    font-size: 12px;
    color: var(--dim);
  }
  .note.bad {
    color: var(--bad);
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
  }
  .mais {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .space {
    flex: 1;
  }
  .shortcut {
    min-height: 36px;
    padding: 4px 11px;
    font-size: 13px;
    border-radius: 999px;
    color: var(--dim);
  }
</style>
