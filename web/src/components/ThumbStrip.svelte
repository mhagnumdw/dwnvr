<script lang="ts">
  import { SvelteSet } from 'svelte/reactivity';
  import type { Segmento } from '../lib/api.ts';
  import { contexto2d } from '../lib/canvas.ts';
  import { thumbnail } from '../lib/thumbs.ts';
  import { hhmmss } from '../lib/format.ts';

  interface Props {
    cam: string;
    segments?: Segmento[];
    currentMs?: number;
    onseek?: (ms: number) => void;
  }

  let { cam, segments = [], currentMs = 0, onseek }: Props = $props();

  // Quantas miniaturas mostrar. Mais que isso vira desfile de imagens que
  // ninguém consegue distinguir, e cada uma custa um decode.
  const MAX_TILES = 40;

  // Quanto além da parte visível a tira já pede, de cada lado, em fração da
  // largura dela: a miniatura fica pronta antes de a rolagem chegar, sem pedir
  // as 40 da janela.
  const FOLGA = '50%';

  // Uma janela contígua de 40 segmentos em torno do instante atual - e não uma
  // amostragem espaçada do dia. Um dia real tem ~2300 segmentos de ~33s, então
  // este é o caminho que roda sempre; a lista inteira só aparece numa câmera
  // recém-instalada. A janela desliza junto com currentMs, o que deixa o
  // segmento ativo parado no índice 20 enquanto as miniaturas passam por baixo.
  const tiles = $derived.by(() => {
    if (!segments.length) return [];
    if (segments.length <= MAX_TILES) return segments;

    const center = segments.findIndex(([s, d]) => currentMs >= s && currentMs < s + d);
    const anchor = center >= 0 ? center : 0;
    const half = Math.floor(MAX_TILES / 2);
    const start = Math.max(0, anchor - half);
    return segments.slice(start, start + MAX_TILES);
  });

  let strip = $state<HTMLDivElement | null>(null);

  // Só o início do segmento ativo, e não currentMs: currentMs muda várias vezes
  // por segundo durante a reprodução, e o que interessa aqui muda uma vez a cada
  // segmento. Sem esse filtro o efeito abaixo rodaria à toa o tempo todo.
  const activeStart = $derived.by(() => {
    const seg = tiles.find(([t, dur]) => currentMs >= t && currentMs < t + dur);
    return seg ? seg[0] : -1;
  });

  // Depois de um seek pela timeline, a janela é refatiada em torno do novo
  // instante e o tile ativo cai no índice 20 - a uns 2280px do início, muito
  // além da parte visível. A tira ficava então sem nenhum destaque à vista, sem
  // dar nem indício de para que lado rolar.
  $effect(() => {
    if (activeStart < 0 || !strip) return;

    const tile = strip.querySelector('.tile.active');
    if (!tile) return;

    const t = tile.getBoundingClientRect();
    const s = strip.getBoundingClientRect();
    // Já visível: não mexer. É o que mantém a tira quieta durante a reprodução,
    // já que o ativo fica parado no mesmo ponto enquanto a janela desliza.
    if (t.left >= s.left && t.right <= s.right) return;

    // Mexer só no scrollLeft da tira. scrollIntoView() rolaria também a página,
    // que não tem nada a ver com isto.
    strip.scrollLeft += t.left - s.left - (s.width - t.width) / 2;
  });

  // A tira só pede a miniatura que entra na vista, mais a FOLGA, e o resto
  // espera a rolagem chegar perto. Antes ela pedia as 40 da janela na ordem do
  // DOM, da esquerda para a direita, e as visíveis - em volta do ativo, no
  // índice 20, ou no fim da tira no dia de hoje - chegavam por último.
  //
  // As pedidas recomeçam a cada carga de câmera ou de dia, junto com o cache
  // que o Recordings limpa nessa hora. O `segments` muda de identidade a cada
  // poll do dia de hoje, por isso a chave é o primeiro instante, e não a lista.
  const carga = $derived(`${cam}@${segments[0]?.[0] ?? ''}`);
  const pedidas = $derived({ carga, instantes: new SvelteSet<number>() });

  // O slice da janela é novo a cada atualização do currentMs, mas ela só
  // desliza uma vez por segmento: é aí que vale observar os tiles de novo.
  const janela = $derived(tiles.length ? `${cam}@${tiles[0]?.[0]}+${tiles.length}` : '');

  // O primeiro aviso de um IntersectionObserver traz de uma vez todos os tiles
  // já à vista. Ele roda depois do efeito acima, que já pôs o ativo na vista.
  $effect(() => {
    if (!strip || !janela) return;
    const io = new IntersectionObserver(
      (entradas) => {
        pedir(
          entradas
            .filter((e) => e.isIntersecting)
            .map((e) => Number((e.target as HTMLElement).dataset['t'])),
        );
      },
      { root: strip, rootMargin: `0px ${FOLGA}` },
    );
    for (const tile of strip.querySelectorAll('.tile')) io.observe(tile);
    return () => {
      io.disconnect();
    };
  });

  // Do ativo para fora: a fila do thumbs.ts decodifica na ordem em que os
  // downloads terminam, e a primeira que importa é a do ativo. A distância é
  // até o segmento, e não até o começo dele, para o ativo valer zero.
  function pedir(novas: number[]) {
    const longe = ([s, d]: Segmento) =>
      currentMs < s ? s - currentMs : Math.max(0, currentMs - s - d);
    const fila = tiles
      .filter(([t]) => novas.includes(t) && !pedidas.instantes.has(t))
      .sort((a, b) => longe(a) - longe(b));
    for (const [t] of fila) {
      void thumbnail(cam, t);
      pedidas.instantes.add(t);
    }
  }

  // A roda do mouse é vertical; a tira rola na horizontal. Sem esta tradução o
  // navegador rola a página e a tira fica inalcançável para quem não tem trackpad.
  function onWheel(ev: WheelEvent & { currentTarget: HTMLDivElement }) {
    const el = ev.currentTarget;
    if (el.scrollWidth <= el.clientWidth) return; // nada a rolar
    if (Math.abs(ev.deltaX) > Math.abs(ev.deltaY)) return; // gesto horizontal: o nativo já resolve

    // deltaMode: 0 px (Chrome), 1 linhas (Firefox), 2 páginas.
    const step = ev.deltaY * (ev.deltaMode === 1 ? 32 : ev.deltaMode === 2 ? el.clientWidth : 1);

    // Quem limita é o navegador: scrollLeft é fracionário e scrollWidth vem
    // arredondado, então um limite calculado aqui erra a ponta por uma fração de
    // pixel - e a tira prenderia a rolagem da página para sempre no fim.
    const before = el.scrollLeft;
    el.scrollLeft = before + step;
    if (el.scrollLeft === before) return; // já na ponta: devolve a rolagem à página

    ev.preventDefault();
  }
</script>

<div class="strip" bind:this={strip} onwheel={onWheel}>
  {#each tiles as [t, dur] (t)}
    {@const active = currentMs >= t && currentMs < t + dur}
    <button
      class="tile"
      class:active
      data-t={t}
      onclick={() => {
        onseek?.(t);
      }}
      title={hhmmss(t)}
    >
      {#if pedidas.instantes.has(t)}
        {#await thumbnail(cam, t) then bitmap}
          {#if bitmap}
            <canvas
              {@attach (node: HTMLCanvasElement) => {
                // Desenhar num canvas do tamanho exibido, e não escalar um
                // <img>, evita guardar um bitmap 1080p por miniatura na memória
                // do celular.
                const w = node.clientWidth * (window.devicePixelRatio || 1);
                const h = node.clientHeight * (window.devicePixelRatio || 1);
                node.width = w;
                node.height = h;
                contexto2d(node).drawImage(bitmap, 0, 0, w, h);
              }}
            ></canvas>
          {:else}
            <span class="fail">-</span>
          {/if}
        {/await}
      {/if}
      <span class="time mono">{hhmmss(t)}</span>
    </button>
  {/each}

  {#if !tiles.length}
    <span class="muted small">sem gravação neste dia</span>
  {/if}
</div>

<style>
  .strip {
    display: flex;
    gap: 6px;
    overflow-x: auto;
    padding: 4px 2px 8px;
    scroll-snap-type: x proximity;
    /* Rolagem horizontal fluida no toque, sem prender a rolagem da página. */
    overscroll-behavior-x: contain;
  }

  .tile {
    position: relative;
    flex: 0 0 auto;
    width: 108px;
    height: 62px;
    padding: 0;
    min-height: 0;
    border-radius: 6px;
    overflow: hidden;
    background: #000;
    scroll-snap-align: center;
    display: grid;
    place-items: center;
  }

  @media (max-width: 520px) {
    .tile {
      width: 88px;
      height: 52px;
    }
  }

  .tile.active {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent);
  }

  canvas {
    width: 100%;
    height: 100%;
    display: block;
    object-fit: cover;
  }

  .time {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    font-size: 10px;
    background: rgba(0, 0, 0, 0.62);
    color: #fff;
    padding: 1px 0;
  }

  .fail {
    color: var(--dim);
  }
</style>
