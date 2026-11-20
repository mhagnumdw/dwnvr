<!--
  Day picker: for recordings, or detections. `oQue` is the material the marked
  days have, and only changes what the screen reader announces. With `counts`,
  the day shows how much material it has, in place of the dot that only says it
  has some.

  It exists because the native `<input type="date">` can't mark which days have
  material - and it is that marking, not the date choice, that fixes blind
  navigation through the history.

  With an empty day list - a camera that hasn't recorded anything yet, or a
  failed query - it does NOT lock up: it allows any day up to today, exactly as
  the native field did. A fully disabled calendar would be worse than no
  calendar.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import Icon from './Icon.svelte';
  import { dayKey, parseDay, shortDay } from '../lib/format.ts';

  interface Props {
    value?: string;
    days?: string[];
    // Explicit undefined: DayNav passes on its own, which may not have come.
    oQue?: string | undefined;
    // Day -> how much material it has. A day in `days` that is missing here
    // keeps the dot.
    counts?: Record<string, number> | undefined;
    onchange?: (day: string) => void;
    // Announces the opening: whoever passes `counts` takes the chance to
    // refresh them, because the screen stays open and the current day keeps
    // growing.
    onopen?: (() => void) | undefined;
  }

  // A month cell; null is the gap before day 1.
  interface Cell {
    number: number;
    key: string;
    recorded: boolean;
    count: number | undefined;
    selected: boolean;
    today: boolean;
    label: string;
  }

  let {
    value = dayKey(),
    days = [],
    oQue = 'gravação',
    counts,
    onchange,
    onopen,
  }: Props = $props();

  let isOpen = $state(false);
  let rootEl = $state<HTMLDivElement | null>(null);
  let triggerEl = $state<HTMLButtonElement | null>(null);
  let gridEl = $state<HTMLDivElement | null>(null);
  let popoverEl = $state<HTMLDivElement | null>(null);

  // How far the popover had to move off center so it doesn't go past the screen
  // edge. Measured, not solved with a media query: the picker sits in a bar
  // that wraps, so where it lands depends on the width AND on the camera name.
  let offset = $state(0);

  // Reread on every open, not once at mount: this screen stays open 24/7 and
  // crosses midnight. A frozen `today` would leave the current day's outline
  // and the navigation ceiling pointing at yesterday.
  let today = $state(dayKey());

  // The day under the keyboard cursor. It moves through the grid even over days
  // with no recording - crossing a week-long gap with the arrow key has to be
  // possible - and what follows it is the REAL DOM focus, not a class: that is
  // how the screen reader announces each date while navigating.
  let focusedDay = $state<string | null>(null);

  // Visible month. Starts null because only the opening knows which it is - it
  // depends on the selected day at that moment.
  let viewYear = $state<number | null>(null);
  let viewMonth = $state<number | null>(null);

  const MONTHS = [
    'Janeiro',
    'Fevereiro',
    'Março',
    'Abril',
    'Maio',
    'Junho',
    'Julho',
    'Agosto',
    'Setembro',
    'Outubro',
    'Novembro',
    'Dezembro',
  ];
  const WEEK = ['D', 'S', 'T', 'Q', 'Q', 'S', 'S'];

  const pad = (n: number) => String(n).padStart(2, '0');

  const daysSet = $derived(new Set(days));
  // Without a list, everything up to today is valid - see the header.
  const permissive = $derived(days.length === 0);
  const hasRecording = $derived((key: string) => (permissive ? key <= today : daysSet.has(key)));

  // The two ends of what the keyboard can reach. The ceiling is today because
  // there is no tomorrow to play; the floor only exists when there is a list.
  const floor = $derived(permissive ? null : days[0]);

  function clamp(key: string) {
    if (key > today) return today;
    if (floor && key < floor) return floor;
    return key;
  }

  // --- abrir, fechar, focar ---------------------------------------------------

  function goTo(key: string) {
    focusedDay = key;
    const d = parseDay(key);
    viewYear = d.getFullYear();
    viewMonth = d.getMonth();
  }

  // Focus goes to the cell, not to the popover: that is what makes Enter and
  // Space land on the button's native `onclick`, with no key handler in
  // between.
  function focusCell() {
    void tick().then(() => {
      gridEl?.querySelector<HTMLElement>(`[data-dia="${focusedDay ?? ''}"]`)?.focus();
    });
  }

  function openNow() {
    today = dayKey();
    goTo(clamp(value || today));
    offset = 0;
    isOpen = true;
    onopen?.();
    focusCell();
    void tick().then(fitOnScreen);
  }

  function fitOnScreen() {
    if (!popoverEl) return;
    const slack = 10;
    const r = popoverEl.getBoundingClientRect();
    if (r.right > window.innerWidth - slack) offset = window.innerWidth - slack - r.right;
    else if (r.left < slack) offset = slack - r.left;
  }

  function close(returnFocus: boolean) {
    isOpen = false;
    if (returnFocus) {
      void tick().then(() => {
        triggerEl?.focus();
      });
    }
  }

  function toggle() {
    if (isOpen) close(true);
    else openNow();
  }

  function select(key: string) {
    if (!hasRecording(key)) return;
    onchange?.(key);
    close(true);
  }

  // --- navigation
  // --------------------------------------------------------------

  function changeMonth(delta: number, focusNow = false) {
    const d = parseDay(focusedDay ?? today);
    const ano = d.getFullYear();
    const mes = d.getMonth() + delta;
    // Day 31 landing in a 30-day month: left to `Date`, it would roll over to
    // the next month - exactly the jump this button must not make.
    const last = new Date(ano, mes + 1, 0).getDate();
    const target = new Date(ano, mes, Math.min(d.getDate(), last));
    goTo(clamp(dayKey(target)));
    if (focusNow) focusCell();
  }

  function moveFocus(dias: number) {
    const d = parseDay(focusedDay ?? today);
    d.setDate(d.getDate() + dias);
    goTo(clamp(dayKey(d)));
    focusCell();
  }

  // Home and End go to the end of the HISTORY, not the end of the week as the
  // generic calendar pattern dictates: here the question asked all the time is
  // "where does what still exists on disk begin", and the start of the week
  // answers nothing.
  function goToEdge(end: boolean) {
    const monthStart =
      viewYear === null || viewMonth === null ? today : `${viewYear}-${pad(viewMonth + 1)}-01`;
    const key = end ? (days.at(-1) ?? today) : (days[0] ?? monthStart);
    goTo(clamp(key));
    focusCell();
  }

  // Only the movement keys, and on the cell itself: Enter and Space stay with
  // the button, which already handles them on its own. That is what removes the
  // need for any global handler - one of them hijacked Enter on every button in
  // the popover.
  const STEPS: Readonly<Record<string, number>> = {
    ArrowLeft: -1,
    ArrowRight: 1,
    ArrowUp: -7,
    ArrowDown: 7,
  };

  function onCellKeydown(e: KeyboardEvent) {
    const step = STEPS[e.key];
    if (step !== undefined) {
      e.preventDefault();
      moveFocus(step);
      return;
    }
    if (e.key === 'PageUp' || e.key === 'PageDown') {
      e.preventDefault();
      changeMonth(e.key === 'PageUp' ? -1 : 1, true);
      return;
    }
    if (e.key === 'Home' || e.key === 'End') {
      e.preventDefault();
      goToEdge(e.key === 'End');
    }
  }

  // --- what the grid shows ---------------------------------------------------

  const grid = $derived.by(() => {
    if (viewYear === null || viewMonth === null) return [];

    const emptyOnes = new Date(viewYear, viewMonth, 1).getDay();
    const total = new Date(viewYear, viewMonth + 1, 0).getDate();
    const list: (Cell | null)[] = Array.from({ length: emptyOnes }, () => null);

    for (let d = 1; d <= total; d++) {
      const key = `${viewYear}-${pad(viewMonth + 1)}-${pad(d)}`;
      const recorded = hasRecording(key);
      const count = recorded && !permissive ? counts?.[key] : undefined;
      list.push({
        number: d,
        key,
        recorded,
        count,
        selected: key === value,
        today: key === today,
        // The whole state in the label itself, not in a per-cell
        // `aria-pressed`: thirty buttons announcing "not pressed" is noise. In
        // permissive mode, "with recording" would be a lie - nobody knows
        // there.
        label:
          `${d} de ${MONTHS[viewMonth] ?? ''} de ${viewYear}` +
          (permissive ? '' : `, ${recorded ? 'com' : 'sem'} ${oQue}`) +
          (count === undefined ? '' : `: ${count}`) +
          (key === value ? ', selecionado' : ''),
      });
    }
    return list;
  });

  // The full date for the screen reader; on the button, the short one, with the
  // year only when it isn't this year: on a phone the bar shares the line with
  // the camera.
  const wholeDate = $derived.by(() => {
    if (!value) return '--/--/----';
    const [y = '', m = '', d = ''] = value.split('-');
    return `${d}/${m}/${y}`;
  });
  const buttonText = $derived(value ? shortDay(parseDay(value).getTime()) : '--/--');

  const canAdvance = $derived.by(() => {
    if (viewYear === null || viewMonth === null) return false;
    const d = parseDay(today);
    return viewYear < d.getFullYear() || (viewYear === d.getFullYear() && viewMonth < d.getMonth());
  });

  const canGoBack = $derived.by(() => {
    if (viewYear === null || viewMonth === null) return false;
    if (!floor) return true;
    const p = parseDay(floor);
    return viewYear > p.getFullYear() || (viewYear === p.getFullYear() && viewMonth > p.getMonth());
  });

  const mesAno = $derived(
    viewYear === null || viewMonth === null ? '' : `${MONTHS[viewMonth] ?? ''} ${viewYear}`,
  );

  // --- fechar por fora --------------------------------------------------------

  function onWindowClick(e: MouseEvent) {
    // Without returning focus: whoever clicked outside has already chosen where
    // to go.
    if (isOpen && rootEl && e.target instanceof Node && !rootEl.contains(e.target)) close(false);
  }

  function onWindowKeydown(e: KeyboardEvent) {
    if (isOpen && e.key === 'Escape') {
      e.preventDefault();
      close(true);
    }
  }

  // Tabbing out closes, like any non-modal popover. A null `relatedTarget` is
  // focus leaving the whole window (or the popover itself unmounting), and
  // closing then would get in the way of someone who just switched tabs.
  function onFocusOut(e: FocusEvent) {
    if (isOpen && e.relatedTarget instanceof Node && rootEl && !rootEl.contains(e.relatedTarget)) {
      close(false);
    }
  }
</script>

<svelte:window onclick={onWindowClick} onkeydown={onWindowKeydown} />

<div class="day-picker" bind:this={rootEl} onfocusout={onFocusOut}>
  <button
    type="button"
    class="trigger ghost"
    onclick={toggle}
    bind:this={triggerEl}
    aria-label="dia: {wholeDate}"
    aria-haspopup="dialog"
    aria-expanded={isOpen}
  >
    <span class="data mono">{buttonText}</span>
    <span class="icon"><Icon name="calendar" size={14} stroke={2.25} /></span>
  </button>

  {#if isOpen}
    <div
      class="popover card"
      bind:this={popoverEl}
      style="--desloc: {offset}px"
      role="dialog"
      aria-label="calendário: dias com {oQue}"
    >
      <div class="header">
        <button
          type="button"
          class="nav ghost"
          onclick={() => {
            changeMonth(-1);
          }}
          disabled={!canGoBack}
          aria-label="mês anterior"
        >
          <Icon name="chevronEsq" size={18} />
        </button>
        <span class="mes-ano">{mesAno}</span>
        <button
          type="button"
          class="nav ghost"
          onclick={() => {
            changeMonth(1);
          }}
          disabled={!canAdvance}
          aria-label="próximo mês"
        >
          <Icon name="chevronDir" size={18} />
        </button>
      </div>

      <div class="week-days" aria-hidden="true">
        {#each WEEK as s, i (i)}
          <span class="d-week">{s}</span>
        {/each}
      </div>

      <div class="grid" bind:this={gridEl}>
        {#each grid as item, i (i)}
          {#if item === null}
            <span class="cell empty" aria-hidden="true"></span>
          {:else}
            <button
              type="button"
              class="cell day"
              class:recorded={item.recorded}
              class:counted={item.count !== undefined}
              class:today={item.today}
              class:selected={item.selected}
              data-dia={item.key}
              onclick={() => {
                select(item.key);
              }}
              onkeydown={onCellKeydown}
              aria-label={item.label}
              aria-disabled={!item.recorded}
              aria-current={item.today ? 'date' : undefined}
              tabindex={item.key === focusedDay ? 0 : -1}
            >
              <span class="num">{item.number}</span>
              {#if item.count !== undefined}
                <span class="count mono" aria-hidden="true">{item.count}</span>
              {:else if item.recorded && !permissive}
                <span class="point" aria-hidden="true"></span>
              {/if}
            </button>
          {/if}
        {/each}
      </div>

      {#if hasRecording(today)}
        <div class="footer">
          <button
            type="button"
            class="today-btn ghost small"
            onclick={() => {
              select(today);
            }}
          >
            Hoje
          </button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .day-picker {
    position: relative;
    display: inline-block;
  }

  .trigger {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    background: var(--panel-2);
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 9px 12px;
    min-height: 44px;
    color: var(--fg);
  }

  .trigger:hover {
    border-color: var(--accent);
  }

  .data {
    font-size: 14px;
  }

  .icon {
    display: flex;
    opacity: 0.75;
    flex-shrink: 0;
  }

  .popover {
    position: absolute;
    top: calc(100% + 6px);
    left: 50%;
    transform: translateX(calc(-50% + var(--desloc, 0px)));
    z-index: 40;
    width: 280px;
    max-width: calc(100vw - 20px);
    padding: 12px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  }

  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 8px;
  }

  .mes-ano {
    font-weight: 600;
    font-size: 14px;
  }

  .nav {
    padding: 4px 8px;
    min-height: 32px;
  }

  .week-days {
    display: grid;
    grid-template-columns: repeat(7, 1fr);
    text-align: center;
    font-size: 11px;
    color: var(--dim);
    margin-bottom: 4px;
  }

  .d-week {
    padding: 2px 0;
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(7, 1fr);
    gap: 2px;
  }

  .cell {
    aspect-ratio: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 0;
    margin: 0;
    border-radius: 6px;
    border: 1px solid transparent;
    font-size: 13px;
    min-height: unset;
    position: relative;
    background: transparent;
  }

  .cell.empty {
    pointer-events: none;
  }

  /* A day with no recording stays focusable on purpose - that is what lets the
     arrow key cross a gap in the history - but it isn't a click target. */
  .cell.day {
    color: var(--dim);
    opacity: 0.35;
    cursor: not-allowed;
  }

  .cell.day.recorded {
    color: var(--fg);
    opacity: 1;
    cursor: pointer;
    font-weight: 500;
  }

  .cell.day.recorded:hover {
    background: var(--panel-2);
    border-color: var(--line);
  }

  .cell.day.today {
    border-color: var(--dim);
  }

  /* Declared after :hover to win by order, without !important. */
  .cell.day.selected,
  .cell.day.selected:hover {
    background: var(--accent);
    border-color: var(--accent);
    color: #fff;
    font-weight: 600;
  }

  .cell.day.selected .point {
    background: #fff;
  }

  .cell.day.selected .count {
    color: #fff;
  }

  /* Outline outside the cell: inside, it would vanish into the selected day's
     background, which is exactly where focus tends to start. */
  .cell.day:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .point {
    width: 4px;
    height: 4px;
    border-radius: 50%;
    background: var(--accent);
    position: absolute;
    bottom: 3px;
  }

  /* In place of the dot. The day number moves up to make room: 1537, the
     largest day seen in a real installation, fits in the 280 / 7 px cell. */
  .cell.counted .num {
    line-height: 1;
    margin-top: -6px;
  }

  .count {
    position: absolute;
    bottom: 2px;
    font-size: 9px;
    line-height: 1;
    font-weight: 600;
    letter-spacing: -0.2px;
    color: var(--accent);
  }

  .footer {
    display: flex;
    justify-content: center;
    margin-top: 8px;
    padding-top: 8px;
    border-top: 1px solid var(--line);
  }

  .today-btn {
    padding: 4px 12px;
    min-height: 28px;
    color: var(--accent);
  }
</style>
