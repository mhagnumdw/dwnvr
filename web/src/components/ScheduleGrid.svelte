<!--
  The week grid of the alert schedules: on the desktop the days in rows and the
  hours in columns, and on the phone flipped, with the days in columns. The flip
  is CSS only: each cell carries its own row and column for both orientations.

  Clicking an hour checks or unchecks it; dragging with the mouse marks a
  rectangle, in the mode of the first cell; the day and the hour on the edges
  mark the whole row or column. On touch only a still tap marks, because
  dragging scrolls the sheet. Whoever uses it does the painting (`onpaint`): the
  grid only draws the coverage.
-->
<script lang="ts">
  import { DAYS, fullHour, whole, dayName, clipsOfHour } from '../lib/weekGrid.ts';
  import type { Coverage } from '../lib/weekGrid.ts';
  import { hourOfMinute } from '../lib/format.ts';

  interface Props {
    coverage: Coverage;
    // The hours of the schedule under the mouse or the focus in the list,
    // outlined.
    highlight: Coverage | null;
    onpaint: (d0: number, h0: number, d1: number, h1: number, mark: boolean) => void;
  }

  let { coverage, highlight, onpaint }: Props = $props();

  const HOURS = [...Array(24).keys()];
  const hh = (n: number) => String(n).padStart(2, '0');
  // The hours 06, 12 and 18 open a gap: the eye finds morning, afternoon and
  // night without counting little squares.
  const quarto = (h: number) => h === 6 || h === 12 || h === 18;

  function condition(ps: [number, number][], h: number) {
    if (!ps.length) return 'livre';
    if (whole(ps)) return 'marcada';
    const clips = ps.map(
      ([a, b]) => `das ${hourOfMinute(h * 60 + a)} às ${hourOfMinute(h * 60 + b)}`,
    );
    return `marcada ${clips.join(' e ')}`;
  }

  // The marked piece of a half hour, at its position. A single minute would
  // vanish on screen: the piece has a minimum so it can be seen.
  function clips(ps: [number, number][]) {
    return ps
      .flatMap(([a, b]) => {
        let startPct = (a / 60) * 100;
        let end = ((b + 1) / 60) * 100;
        if (end - startPct < 8) {
          const middle = (startPct + end) / 2;
          startPct = Math.max(0, middle - 4);
          end = Math.min(100, middle + 4);
        }
        return [
          `transparent ${startPct}%`,
          `var(--color) ${startPct}%`,
          `var(--color) ${end}%`,
          `transparent ${end}%`,
        ];
      })
      .join(', ');
  }

  const cells = $derived(
    DAYS.map((_, d) =>
      HOURS.map((h) => {
        const ps = clipsOfHour(coverage, d, h);
        const full = whole(ps);
        return {
          ps,
          full,
          partial: ps.length > 0 && !full,
          highlighted: !!highlight && clipsOfHour(highlight, d, h).length > 0,
        };
      }),
    ),
  );
  const fullDay = $derived(cells.map((hs) => hs.every((c) => c.full)));
  const fullHourAllDay = $derived(HOURS.map((h) => cells.every((hs) => hs[h]?.full)));

  // The rectangle the mouse is dragging, and the touch that may still turn into
  // a scroll.
  let drag = $state<{
    d0: number;
    h0: number;
    d1: number;
    h1: number;
    mark: boolean;
    id: number;
  } | null>(null);
  let touch: { d: number; h: number; x: number; y: number; id: number } | null = null;

  const inDrag = (d: number, h: number) =>
    !!drag &&
    d >= Math.min(drag.d0, drag.d1) &&
    d <= Math.max(drag.d0, drag.d1) &&
    h >= Math.min(drag.h0, drag.h1) &&
    h <= Math.max(drag.h0, drag.h1);

  // The line under the grid: what to do, the hour under the mouse, what the
  // drag will mark. When null, the instruction.
  let info = $state<{ strong: string; rest: string } | null>(null);
  const instruction = () =>
    matchMedia('(pointer: coarse)').matches
      ? 'Toque numa hora para marcar ou desmarcar. O dia e a hora nas bordas marcam a coluna ou a linha inteira.'
      : 'Clique numa hora para marcar ou desmarcar, ou arraste para um bloco. O dia e a hora nas bordas marcam a linha ou a coluna inteira.';

  const cellInfo = (d: number, h: number) => {
    info = {
      strong: `${dayName(d, true)}, ${hh(h)}h`,
      rest: ` · ${condition(cells[d]?.[h]?.ps ?? [], h)}`,
    };
  };

  function dragInfo() {
    if (!drag) return;
    const [d0, d1] = [Math.min(drag.d0, drag.d1), Math.max(drag.d0, drag.d1)];
    const [h0, h1] = [Math.min(drag.h0, drag.h1), Math.max(drag.h0, drag.h1)];
    const days = d0 === d1 ? dayName(d0) : `${dayName(d0)} a ${dayName(d1)}`;
    const hours = h0 === h1 ? `${hh(h0)}h` : `${hh(h0)}h a ${hh(h1)}h`;
    info = { strong: drag.mark ? 'marcando' : 'desmarcando', rest: ` ${days} · ${hours}` };
  }

  function paint(d0: number, h0: number, d1: number, h1: number, mark: boolean) {
    onpaint(d0, h0, d1, h1, mark);
    info = { strong: 'A lista foi refeita pela grade', rest: ' · cada dia com as horas dele' };
  }

  function cellOf(target: EventTarget | null) {
    const c = target instanceof Element ? target.closest<HTMLElement>('.cel') : null;
    return c ? { d: Number(c.dataset['d']), h: Number(c.dataset['h']) } : null;
  }

  function press(e: PointerEvent & { currentTarget: HTMLElement }) {
    const c = cellOf(e.target);
    if (!c) return;
    // On touch the browser does the scrolling: only a still tap marks.
    if (e.pointerType === 'touch') {
      touch = { ...c, x: e.clientX, y: e.clientY, id: e.pointerId };
      return;
    }
    if (e.button !== 0) return;
    e.preventDefault();
    drag = {
      d0: c.d,
      h0: c.h,
      d1: c.d,
      h1: c.h,
      mark: !fullHour(coverage, c.d, c.h),
      id: e.pointerId,
    };
    e.currentTarget.setPointerCapture(e.pointerId);
    dragInfo();
  }

  function move(e: PointerEvent) {
    if (drag?.id === e.pointerId) {
      // With the pointer captured, the target is always the grid: the cell
      // comes from the point.
      const c = cellOf(document.elementFromPoint(e.clientX, e.clientY));
      if (c && (c.d !== drag.d1 || c.h !== drag.h1)) {
        drag.d1 = c.d;
        drag.h1 = c.h;
        dragInfo();
      }
      return;
    }
    if (touch && Math.hypot(e.clientX - touch.x, e.clientY - touch.y) > 10) touch = null;
    if (e.pointerType === 'mouse') {
      const c = cellOf(e.target);
      if (c) cellInfo(c.d, c.h);
    }
  }

  function release(e: PointerEvent) {
    if (drag?.id === e.pointerId) {
      const a = drag;
      drag = null;
      paint(a.d0, a.h0, a.d1, a.h1, a.mark);
      return;
    }
    if (touch?.id === e.pointerId) {
      const t = touch;
      touch = null;
      paint(t.d, t.h, t.d, t.h, !fullHour(coverage, t.d, t.h));
      cellInfo(t.d, t.h);
    }
  }

  function abort() {
    drag = null;
    touch = null;
    info = null;
  }
</script>

<div
  class="grid"
  role="application"
  aria-label="as horas da semana"
  onpointerdown={press}
  onpointermove={move}
  onpointerup={release}
  onpointercancel={abort}
  onpointerleave={(e) => {
    if (!drag && e.pointerType === 'mouse') info = null;
  }}
>
  <span class="canto" style:--row-h="1" style:--col-h="1" style:--row-v="1" style:--col-v="1"
  ></span>
  {#each HOURS as h (h)}
    <button
      type="button"
      class="rot hr"
      class:q={quarto(h)}
      class:filled={fullHourAllDay[h]}
      style:--row-h="1"
      style:--col-h={h + 2}
      style:--row-v={h + 2}
      style:--col-v="1"
      title="{hh(h)}h de todos os dias"
      onclick={() => paint(0, h, 6, h, !fullHourAllDay[h])}>{hh(h)}</button
    >
  {/each}
  {#each DAYS as name, d (d)}
    <button
      type="button"
      class="rot d"
      class:filled={fullDay[d]}
      style:--row-h={d + 2}
      style:--col-h="1"
      style:--row-v="1"
      style:--col-v={d + 2}
      title="{dayName(d, true)}: o dia inteiro"
      onclick={() => paint(d, 0, d, 23, !fullDay[d])}>{name}</button
    >
    {#each cells[d] ?? [] as c, h (h)}
      <div
        class="cel"
        class:q={quarto(h)}
        class:on={c.full}
        class:partial={c.partial}
        class:highlight={c.highlighted}
        class:paint={inDrag(d, h) && drag?.mark}
        class:erase={inDrag(d, h) && !drag?.mark}
        style:--row-h={d + 2}
        style:--col-h={h + 2}
        style:--row-v={h + 2}
        style:--col-v={d + 2}
        style:--clips={c.partial ? clips(c.ps) : null}
        data-d={d}
        data-h={h}
        title="{dayName(d, true)}, {hh(h)}h · {condition(c.ps, h)}"
      ></div>
    {/each}
  {/each}
</div>
<p class="info" aria-live="polite">
  {#if info}<b>{info.strong}</b>{info.rest}{:else}{instruction()}{/if}
</p>

<style>
  .grid {
    --color: var(--accent);
    --dir: to right;
    --height: 18px;
    display: grid;
    grid-template-columns: 34px repeat(24, minmax(0, 1fr));
    gap: 2px;
    user-select: none;
    -webkit-user-select: none;
  }
  .grid > * {
    grid-row: var(--row-h);
    grid-column: var(--col-h);
  }

  .rot {
    min-height: 0;
    padding: 0;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--dim);
    font-size: 11px;
    line-height: 1;
    font-variant-numeric: tabular-nums;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .rot.d {
    justify-content: flex-start;
    padding-left: 2px;
    font-size: 12px;
  }
  .rot.hr {
    height: 18px;
    font-size: 10px;
  }
  .rot.filled {
    color: var(--fg);
  }
  @media (hover: hover) {
    .rot:hover {
      color: var(--fg);
      background: var(--panel-2);
    }
  }

  .cel {
    height: var(--height);
    border-radius: 3px;
    background-color: var(--panel-2);
    border: 1px solid var(--line);
    cursor: pointer;
  }
  .cel.on {
    background-color: var(--color);
    border-color: var(--color);
  }
  /* The half hour: only the marked piece is painted, at its position. */
  .cel.partial {
    border-color: var(--color);
    border-style: dashed;
    background-image: linear-gradient(var(--dir), var(--clips));
  }
  .cel.highlight {
    box-shadow: inset 0 0 0 2px var(--fg);
  }
  .cel.paint {
    background-color: color-mix(in srgb, var(--accent) 55%, transparent);
    border-color: color-mix(in srgb, var(--accent) 60%, var(--fg));
  }
  .cel.erase {
    background-color: var(--panel-2);
    background-image: none;
    border-color: var(--bad);
    border-style: dashed;
  }
  .q {
    margin-left: 3px;
  }

  /* The phone: the grid flipped, with the days in columns and the hours in rows. */
  @media (max-width: 639px) {
    .grid {
      --dir: to bottom;
      --height: 22px;
      grid-template-columns: 42px repeat(7, minmax(0, 1fr));
    }
    .grid > * {
      grid-row: var(--row-v);
      grid-column: var(--col-v);
    }
    .rot.d {
      justify-content: center;
      padding: 6px 0;
    }
    .rot.hr {
      height: auto;
      justify-content: flex-end;
      padding-right: 6px;
      font-size: 11px;
    }
    .rot.hr::after {
      content: 'h';
    }
    .q {
      margin-left: 0;
      margin-top: 3px;
    }
  }

  /* Always two lines: the instruction takes two, and the hour hint, one. If the
     height changed mid-drag, the sheet scrolled to the end would jump up, and
     the grid would slip away from the mouse. */
  .info {
    min-height: 3em;
    line-height: 1.5;
    margin: -6px 0 0;
    font-size: 12px;
    color: var(--dim);
  }
  .info b {
    color: var(--fg);
    font-weight: 600;
  }
</style>
