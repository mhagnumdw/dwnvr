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
  import { DIAS, horaCheia, inteira, nomeDoDia, pedacosDaHora } from '../lib/gradeDaSemana.ts';
  import type { Cobertura } from '../lib/gradeDaSemana.ts';
  import { horaDoMinuto } from '../lib/format.ts';

  interface Props {
    cobertura: Cobertura;
    // As horas do horário sob o mouse ou o foco na lista, contornadas.
    realce: Cobertura | null;
    onpinta: (d0: number, h0: number, d1: number, h1: number, marca: boolean) => void;
  }

  let { cobertura, realce, onpinta }: Props = $props();

  const HORAS = [...Array(24).keys()];
  const hh = (n: number) => String(n).padStart(2, '0');
  // As horas 06, 12 e 18 abrem um respiro: o olho acha a manhã, a tarde e a
  // noite sem contar quadradinho.
  const quarto = (h: number) => h === 6 || h === 12 || h === 18;

  function estado(ps: [number, number][], h: number) {
    if (!ps.length) return 'livre';
    if (inteira(ps)) return 'marcada';
    const pedacos = ps.map(
      ([a, b]) => `das ${horaDoMinuto(h * 60 + a)} às ${horaDoMinuto(h * 60 + b)}`,
    );
    return `marcada ${pedacos.join(' e ')}`;
  }

  // O pedaço marcado de uma hora pela metade, na posição dele. Um minuto só
  // sumiria na tela: o pedaço tem um mínimo para ser visto.
  function pedacos(ps: [number, number][]) {
    return ps
      .flatMap(([a, b]) => {
        let ini = (a / 60) * 100;
        let fim = ((b + 1) / 60) * 100;
        if (fim - ini < 8) {
          const meio = (ini + fim) / 2;
          ini = Math.max(0, meio - 4);
          fim = Math.min(100, meio + 4);
        }
        return [
          `transparent ${ini}%`,
          `var(--cor) ${ini}%`,
          `var(--cor) ${fim}%`,
          `transparent ${fim}%`,
        ];
      })
      .join(', ');
  }

  const celulas = $derived(
    DIAS.map((_, d) =>
      HORAS.map((h) => {
        const ps = pedacosDaHora(cobertura, d, h);
        const cheia = inteira(ps);
        return {
          ps,
          cheia,
          parcial: ps.length > 0 && !cheia,
          realcada: !!realce && pedacosDaHora(realce, d, h).length > 0,
        };
      }),
    ),
  );
  const diaCheio = $derived(celulas.map((hs) => hs.every((c) => c.cheia)));
  const horaCheiaTodoDia = $derived(HORAS.map((h) => celulas.every((hs) => hs[h]?.cheia)));

  // O retângulo que o mouse está arrastando, e o toque que ainda pode virar
  // rolagem.
  let arrasto = $state<{
    d0: number;
    h0: number;
    d1: number;
    h1: number;
    marca: boolean;
    id: number;
  } | null>(null);
  let toque: { d: number; h: number; x: number; y: number; id: number } | null = null;

  const noArrasto = (d: number, h: number) =>
    !!arrasto &&
    d >= Math.min(arrasto.d0, arrasto.d1) &&
    d <= Math.max(arrasto.d0, arrasto.d1) &&
    h >= Math.min(arrasto.h0, arrasto.h1) &&
    h <= Math.max(arrasto.h0, arrasto.h1);

  // A linha embaixo da grade: o que fazer, a hora sob o mouse, o que o arrasto
  // vai marcar. Nula, a instrução.
  let info = $state<{ forte: string; resto: string } | null>(null);
  const instrucao = () =>
    matchMedia('(pointer: coarse)').matches
      ? 'Toque numa hora para marcar ou desmarcar. O dia e a hora nas bordas marcam a coluna ou a linha inteira.'
      : 'Clique numa hora para marcar ou desmarcar, ou arraste para um bloco. O dia e a hora nas bordas marcam a linha ou a coluna inteira.';

  const infoDaCelula = (d: number, h: number) => {
    info = {
      forte: `${nomeDoDia(d, true)}, ${hh(h)}h`,
      resto: ` · ${estado(celulas[d]?.[h]?.ps ?? [], h)}`,
    };
  };

  function infoDoArrasto() {
    if (!arrasto) return;
    const [d0, d1] = [Math.min(arrasto.d0, arrasto.d1), Math.max(arrasto.d0, arrasto.d1)];
    const [h0, h1] = [Math.min(arrasto.h0, arrasto.h1), Math.max(arrasto.h0, arrasto.h1)];
    const dias = d0 === d1 ? nomeDoDia(d0) : `${nomeDoDia(d0)} a ${nomeDoDia(d1)}`;
    const horas = h0 === h1 ? `${hh(h0)}h` : `${hh(h0)}h a ${hh(h1)}h`;
    info = { forte: arrasto.marca ? 'marcando' : 'desmarcando', resto: ` ${dias} · ${horas}` };
  }

  function pinta(d0: number, h0: number, d1: number, h1: number, marca: boolean) {
    onpinta(d0, h0, d1, h1, marca);
    info = { forte: 'A lista foi refeita pela grade', resto: ' · cada dia com as horas dele' };
  }

  function celulaDe(alvo: EventTarget | null) {
    const c = alvo instanceof Element ? alvo.closest<HTMLElement>('.cel') : null;
    return c ? { d: Number(c.dataset['d']), h: Number(c.dataset['h']) } : null;
  }

  function aperta(e: PointerEvent & { currentTarget: HTMLElement }) {
    const c = celulaDe(e.target);
    if (!c) return;
    // No toque quem rola é o navegador: só o toque parado marca.
    if (e.pointerType === 'touch') {
      toque = { ...c, x: e.clientX, y: e.clientY, id: e.pointerId };
      return;
    }
    if (e.button !== 0) return;
    e.preventDefault();
    arrasto = {
      d0: c.d,
      h0: c.h,
      d1: c.d,
      h1: c.h,
      marca: !horaCheia(cobertura, c.d, c.h),
      id: e.pointerId,
    };
    e.currentTarget.setPointerCapture(e.pointerId);
    infoDoArrasto();
  }

  function move(e: PointerEvent) {
    if (arrasto?.id === e.pointerId) {
      // Com o ponteiro capturado, o alvo é sempre a grade: a célula vem do ponto.
      const c = celulaDe(document.elementFromPoint(e.clientX, e.clientY));
      if (c && (c.d !== arrasto.d1 || c.h !== arrasto.h1)) {
        arrasto.d1 = c.d;
        arrasto.h1 = c.h;
        infoDoArrasto();
      }
      return;
    }
    if (toque && Math.hypot(e.clientX - toque.x, e.clientY - toque.y) > 10) toque = null;
    if (e.pointerType === 'mouse') {
      const c = celulaDe(e.target);
      if (c) infoDaCelula(c.d, c.h);
    }
  }

  function solta(e: PointerEvent) {
    if (arrasto?.id === e.pointerId) {
      const a = arrasto;
      arrasto = null;
      pinta(a.d0, a.h0, a.d1, a.h1, a.marca);
      return;
    }
    if (toque?.id === e.pointerId) {
      const t = toque;
      toque = null;
      pinta(t.d, t.h, t.d, t.h, !horaCheia(cobertura, t.d, t.h));
      infoDaCelula(t.d, t.h);
    }
  }

  function cancela() {
    arrasto = null;
    toque = null;
    info = null;
  }
</script>

<div
  class="grade"
  role="application"
  aria-label="as horas da semana"
  onpointerdown={aperta}
  onpointermove={move}
  onpointerup={solta}
  onpointercancel={cancela}
  onpointerleave={(e) => {
    if (!arrasto && e.pointerType === 'mouse') info = null;
  }}
>
  <span
    class="canto"
    style:--linha-h="1"
    style:--coluna-h="1"
    style:--linha-v="1"
    style:--coluna-v="1"
  ></span>
  {#each HORAS as h (h)}
    <button
      type="button"
      class="rot hr"
      class:q={quarto(h)}
      class:cheio={horaCheiaTodoDia[h]}
      style:--linha-h="1"
      style:--coluna-h={h + 2}
      style:--linha-v={h + 2}
      style:--coluna-v="1"
      title="{hh(h)}h de todos os dias"
      onclick={() => pinta(0, h, 6, h, !horaCheiaTodoDia[h])}>{hh(h)}</button
    >
  {/each}
  {#each DIAS as nome, d (d)}
    <button
      type="button"
      class="rot d"
      class:cheio={diaCheio[d]}
      style:--linha-h={d + 2}
      style:--coluna-h="1"
      style:--linha-v="1"
      style:--coluna-v={d + 2}
      title="{nomeDoDia(d, true)}: o dia inteiro"
      onclick={() => pinta(d, 0, d, 23, !diaCheio[d])}>{nome}</button
    >
    {#each celulas[d] ?? [] as c, h (h)}
      <div
        class="cel"
        class:q={quarto(h)}
        class:on={c.cheia}
        class:parcial={c.parcial}
        class:realce={c.realcada}
        class:pinta={noArrasto(d, h) && arrasto?.marca}
        class:apaga={noArrasto(d, h) && !arrasto?.marca}
        style:--linha-h={d + 2}
        style:--coluna-h={h + 2}
        style:--linha-v={h + 2}
        style:--coluna-v={d + 2}
        style:--pedacos={c.parcial ? pedacos(c.ps) : null}
        data-d={d}
        data-h={h}
        title="{nomeDoDia(d, true)}, {hh(h)}h · {estado(c.ps, h)}"
      ></div>
    {/each}
  {/each}
</div>
<p class="info" aria-live="polite">
  {#if info}<b>{info.forte}</b>{info.resto}{:else}{instrucao()}{/if}
</p>

<style>
  .grade {
    --cor: var(--accent);
    --dir: to right;
    --alt: 18px;
    display: grid;
    grid-template-columns: 34px repeat(24, minmax(0, 1fr));
    gap: 2px;
    user-select: none;
    -webkit-user-select: none;
  }
  .grade > * {
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
  .rot.cheio {
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
  .cel.parcial {
    border-color: var(--cor);
    border-style: dashed;
    background-image: linear-gradient(var(--dir), var(--pedacos));
  }
  .cel.realce {
    box-shadow: inset 0 0 0 2px var(--fg);
  }
  .cel.pinta {
    background-color: color-mix(in srgb, var(--accent) 55%, transparent);
    border-color: color-mix(in srgb, var(--accent) 60%, var(--fg));
  }
  .cel.apaga {
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
    .grade {
      --dir: to bottom;
      --alt: 22px;
      grid-template-columns: 42px repeat(7, minmax(0, 1fr));
    }
    .grade > * {
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
