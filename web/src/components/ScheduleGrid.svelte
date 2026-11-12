<!--
  A grade da semana dos horários do alerta: no desktop os dias em linhas e as
  horas em colunas, e no celular virada, com os dias em colunas. A virada é só
  do CSS: cada célula leva a própria linha e coluna das duas orientações.

  Clicar numa hora marca ou desmarca; arrastar com o mouse marca um retângulo,
  no modo da primeira célula; o dia e a hora nas bordas marcam a linha ou a
  coluna inteira. No toque só o toque parado marca, porque arrastar rola a
  folha. Quem pinta é quem usa (`onpinta`): a grade só desenha a cobertura.
-->
<script lang="ts">
  import { DIAS, fullHour, whole, dayName, clipsOfHour } from '../lib/weekGrid.ts';
  import type { Coverage } from '../lib/weekGrid.ts';
  import { hourOfMinute } from '../lib/format.ts';

  interface Props {
    coverage: Coverage;
    // As horas do horário sob o mouse ou o foco na lista, contornadas.
    highlight: Coverage | null;
    onpaint: (d0: number, h0: number, d1: number, h1: number, mark: boolean) => void;
  }

  let { coverage, highlight, onpaint }: Props = $props();

  const HOURS = [...Array(24).keys()];
  const hh = (n: number) => String(n).padStart(2, '0');
  // As horas 06, 12 e 18 abrem um respiro: o olho acha a manhã, a tarde e a
  // noite sem contar quadradinho.
  const quarto = (h: number) => h === 6 || h === 12 || h === 18;

  function condition(ps: [number, number][], h: number) {
    if (!ps.length) return 'livre';
    if (whole(ps)) return 'marcada';
    const clips = ps.map(
      ([a, b]) => `das ${hourOfMinute(h * 60 + a)} às ${hourOfMinute(h * 60 + b)}`,
    );
    return `marcada ${clips.join(' e ')}`;
  }

  // O pedaço marcado de uma hora pela metade, na posição dele. Um minuto só
  // sumiria na tela: o pedaço tem um mínimo para ser visto.
  function clips(ps: [number, number][]) {
    return ps
      .flatMap(([a, b]) => {
        let ini = (a / 60) * 100;
        let end = ((b + 1) / 60) * 100;
        if (end - ini < 8) {
          const middle = (ini + end) / 2;
          ini = Math.max(0, middle - 4);
          end = Math.min(100, middle + 4);
        }
        return [
          `transparent ${ini}%`,
          `var(--cor) ${ini}%`,
          `var(--cor) ${end}%`,
          `transparent ${end}%`,
        ];
      })
      .join(', ');
  }

  const cells = $derived(
    DIAS.map((_, d) =>
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

  // O retângulo que o mouse está arrastando, e o toque que ainda pode virar
  // rolagem.
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

  // A linha embaixo da grade: o que fazer, a hora sob o mouse, o que o arrasto
  // vai marcar. Nula, a instrução.
  let info = $state<{ forte: string; rest: string } | null>(null);
  const instruction = () =>
    matchMedia('(pointer: coarse)').matches
      ? 'Toque numa hora para marcar ou desmarcar. O dia e a hora nas bordas marcam a coluna ou a linha inteira.'
      : 'Clique numa hora para marcar ou desmarcar, ou arraste para um bloco. O dia e a hora nas bordas marcam a linha ou a coluna inteira.';

  const cellInfo = (d: number, h: number) => {
    info = {
      forte: `${dayName(d, true)}, ${hh(h)}h`,
      rest: ` · ${condition(cells[d]?.[h]?.ps ?? [], h)}`,
    };
  };

  function dragInfo() {
    if (!drag) return;
    const [d0, d1] = [Math.min(drag.d0, drag.d1), Math.max(drag.d0, drag.d1)];
    const [h0, h1] = [Math.min(drag.h0, drag.h1), Math.max(drag.h0, drag.h1)];
    const dias = d0 === d1 ? dayName(d0) : `${dayName(d0)} a ${dayName(d1)}`;
    const hours = h0 === h1 ? `${hh(h0)}h` : `${hh(h0)}h a ${hh(h1)}h`;
    info = { forte: drag.mark ? 'marcando' : 'desmarcando', rest: ` ${dias} · ${hours}` };
  }

  function paint(d0: number, h0: number, d1: number, h1: number, mark: boolean) {
    onpaint(d0, h0, d1, h1, mark);
    info = { forte: 'A lista foi refeita pela grade', rest: ' · cada dia com as horas dele' };
  }

  function cellOf(target: EventTarget | null) {
    const c = target instanceof Element ? target.closest<HTMLElement>('.cel') : null;
    return c ? { d: Number(c.dataset['d']), h: Number(c.dataset['h']) } : null;
  }

  function press(e: PointerEvent & { currentTarget: HTMLElement }) {
    const c = cellOf(e.target);
    if (!c) return;
    // No toque quem rola é o navegador: só o toque parado marca.
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
      // Com o ponteiro capturado, o alvo é sempre a grade: a célula vem do ponto.
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
  <span
    class="canto"
    style:--linha-h="1"
    style:--coluna-h="1"
    style:--linha-v="1"
    style:--coluna-v="1"
  ></span>
  {#each HOURS as h (h)}
    <button
      type="button"
      class="rot hr"
      class:q={quarto(h)}
      class:filled={fullHourAllDay[h]}
      style:--linha-h="1"
      style:--coluna-h={h + 2}
      style:--linha-v={h + 2}
      style:--coluna-v="1"
      title="{hh(h)}h de todos os dias"
      onclick={() => paint(0, h, 6, h, !fullHourAllDay[h])}>{hh(h)}</button
    >
  {/each}
  {#each DIAS as name, d (d)}
    <button
      type="button"
      class="rot d"
      class:filled={fullDay[d]}
      style:--linha-h={d + 2}
      style:--coluna-h="1"
      style:--linha-v="1"
      style:--coluna-v={d + 2}
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
        style:--linha-h={d + 2}
        style:--coluna-h={h + 2}
        style:--linha-v={h + 2}
        style:--coluna-v={d + 2}
        style:--pedacos={c.partial ? clips(c.ps) : null}
        data-d={d}
        data-h={h}
        title="{dayName(d, true)}, {hh(h)}h · {condition(c.ps, h)}"
      ></div>
    {/each}
  {/each}
</div>
<p class="info" aria-live="polite">
  {#if info}<b>{info.forte}</b>{info.rest}{:else}{instruction()}{/if}
</p>

<style>
  .grid {
    --cor: var(--accent);
    --dir: to right;
    --alt: 18px;
    display: grid;
    grid-template-columns: 34px repeat(24, minmax(0, 1fr));
    gap: 2px;
    user-select: none;
    -webkit-user-select: none;
  }
  .grid > * {
    grid-row: var(--linha-h);
    grid-column: var(--coluna-h);
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
    height: var(--alt);
    border-radius: 3px;
    background-color: var(--panel-2);
    border: 1px solid var(--line);
    cursor: pointer;
  }
  .cel.on {
    background-color: var(--cor);
    border-color: var(--cor);
  }
  /* A hora pela metade: só o pedaço marcado fica pintado, na posição dele. */
  .cel.partial {
    border-color: var(--cor);
    border-style: dashed;
    background-image: linear-gradient(var(--dir), var(--pedacos));
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

  /* O celular: a grade virada, com os dias em colunas e as horas em linhas. */
  @media (max-width: 639px) {
    .grid {
      --dir: to bottom;
      --alt: 22px;
      grid-template-columns: 42px repeat(7, minmax(0, 1fr));
    }
    .grid > * {
      grid-row: var(--linha-v);
      grid-column: var(--coluna-v);
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

  /* Duas linhas sempre: a instrução ocupa duas, e a dica da hora, uma. Se a
     altura mudasse no meio do arrasto, a folha rolada até o fim subiria, e a
     grade fugiria do mouse. */
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
