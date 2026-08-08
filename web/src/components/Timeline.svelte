<script>
  import { hhmm, hhmmss } from '../lib/format.js';

  let {
    ranges = [],       // [[inícioMs, fimMs], …] faixas com gravação
    dayStart = 0,
    dayEnd = 0,
    currentMs = 0,
    onseek = () => {},
  } = $props();

  // Janela visível. Começa no dia inteiro e é o único estado que o zoom e o
  // arraste alteram.
  let viewFrom = $state(0);
  let viewTo = $state(0);
  let canvas;
  let width = $state(0);
  let hoverMs = $state(null);

  const MIN_SPAN = 20_000; // 20s de zoom máximo

  // Reancorar quando o dia muda; comparar os limites evita reancorar a cada
  // repintura e perder o zoom que o usuário acabou de dar.
  $effect(() => {
    if (dayStart && (viewFrom < dayStart || viewTo > dayEnd || viewTo === 0)) {
      viewFrom = dayStart;
      viewTo = dayEnd;
    }
  });

  const span = $derived(Math.max(1, viewTo - viewFrom));
  const toX = (ms) => ((ms - viewFrom) / span) * width;
  const toMs = (x) => viewFrom + (x / Math.max(1, width)) * span;

  // Passo das marcas de hora: escolhido para que os rótulos nunca se toquem,
  // seja num celular de 360px ou num monitor largo.
  const step = $derived.by(() => {
    const opts = [1e3, 5e3, 15e3, 60e3, 3e5, 9e5, 18e5, 36e5, 108e5, 216e5];
    const minPx = width < 500 ? 56 : 78;
    return opts.find((s) => (s / span) * width >= minPx) ?? opts.at(-1);
  });

  $effect(() => {
    // Redesenha sempre que qualquer uma destas mudar.
    ranges; viewFrom; viewTo; currentMs; width; hoverMs;
    draw();
  });

  function draw() {
    if (!canvas || !width) return;
    const dpr = window.devicePixelRatio || 1;
    const h = canvas.clientHeight;
    canvas.width = width * dpr;
    canvas.height = h * dpr;

    const g = canvas.getContext('2d');
    g.setTransform(dpr, 0, 0, dpr, 0, 0);

    const barTop = 6;
    const barH = h - 26;

    g.fillStyle = '#1b1f24';
    g.fillRect(0, barTop, width, barH);

    g.fillStyle = '#2f81f7';
    for (const [s, e] of ranges) {
      const x0 = Math.max(0, toX(s));
      const x1 = Math.min(width, toX(e));
      // Mínimo de 2px: uma gravação curta não pode desaparecer da barra só
      // porque o zoom está afastado.
      if (x1 > 0 && x0 < width) g.fillRect(x0, barTop, Math.max(2, x1 - x0), barH);
    }

    g.fillStyle = '#8b949e';
    g.font = '10px system-ui, sans-serif';
    g.textBaseline = 'top';
    const first = Math.ceil(viewFrom / step) * step;
    for (let t = first; t < viewTo; t += step) {
      const x = toX(t);
      g.fillRect(x, h - 18, 1, 5);
      g.fillText(step >= 60e3 ? hhmm(t) : hhmmss(t), x + 3, h - 13);
    }

    if (hoverMs !== null) {
      const x = toX(hoverMs);
      g.fillStyle = 'rgba(230,237,243,.35)';
      g.fillRect(x, barTop, 1, barH);
    }

    if (currentMs) {
      const x = toX(currentMs);
      if (x >= -2 && x <= width + 2) {
        g.fillStyle = '#f85149';
        g.fillRect(x - 1.5, barTop - 3, 3, barH + 6);
      }
    }
  }

  // --- interação -----------------------------------------------------------
  //
  // Ponteiros ativos: um arrasta a janela, dois pinçam para dar zoom. Usar
  // Pointer Events cobre mouse, dedo e caneta com o mesmo código.
  const pointers = new Map();
  let dragged = false;
  let pinchStart = null;

  function localX(ev) {
    return ev.clientX - canvas.getBoundingClientRect().left;
  }

  function onPointerDown(ev) {
    canvas.setPointerCapture(ev.pointerId);
    pointers.set(ev.pointerId, localX(ev));
    dragged = false;
    if (pointers.size === 2) {
      const [a, b] = [...pointers.values()];
      pinchStart = { dist: Math.abs(a - b), from: viewFrom, to: viewTo };
    }
  }

  function onPointerMove(ev) {
    if (!pointers.has(ev.pointerId)) {
      if (ev.pointerType === 'mouse') hoverMs = toMs(localX(ev));
      return;
    }
    const x = localX(ev);
    const prev = pointers.get(ev.pointerId);
    pointers.set(ev.pointerId, x);

    if (pointers.size === 2 && pinchStart) {
      const [a, b] = [...pointers.values()];
      const dist = Math.max(1, Math.abs(a - b));
      const factor = pinchStart.dist / dist;
      const anchor = (pinchStart.from + pinchStart.to) / 2;
      zoomTo(anchor, (pinchStart.to - pinchStart.from) * factor);
      dragged = true;
      return;
    }

    const dx = x - prev;
    if (Math.abs(dx) > 1) dragged = true;
    pan((-dx / Math.max(1, width)) * span);
  }

  function onPointerUp(ev) {
    pointers.delete(ev.pointerId);
    pinchStart = null;
    // Um arraste não deve virar um salto: só o toque limpo navega.
    if (!dragged) onseek(Math.round(toMs(localX(ev))));
  }

  function onWheel(ev) {
    ev.preventDefault();
    zoomTo(toMs(localX(ev)), span * (ev.deltaY > 0 ? 1.35 : 1 / 1.35), localX(ev) / width);
  }

  function zoomTo(anchorMs, nextSpan, frac = 0.5) {
    const total = dayEnd - dayStart;
    const s = Math.min(total, Math.max(MIN_SPAN, nextSpan));
    viewFrom = anchorMs - frac * s;
    viewTo = viewFrom + s;
    clamp();
  }

  function pan(deltaMs) {
    viewFrom += deltaMs;
    viewTo += deltaMs;
    clamp();
  }

  // A janela nunca sai do dia: navegar para o vazio confunde mais que ajuda.
  function clamp() {
    const s = viewTo - viewFrom;
    if (viewFrom < dayStart) {
      viewFrom = dayStart;
      viewTo = dayStart + s;
    }
    if (viewTo > dayEnd) {
      viewTo = dayEnd;
      viewFrom = dayEnd - s;
    }
  }

  export function reset() {
    viewFrom = dayStart;
    viewTo = dayEnd;
  }
</script>

<div class="wrapper" bind:clientWidth={width}>
  <canvas
    bind:this={canvas}
    onpointerdown={onPointerDown}
    onpointermove={onPointerMove}
    onpointerup={onPointerUp}
    onpointercancel={onPointerUp}
    onpointerleave={() => (hoverMs = null)}
    onwheel={onWheel}
  ></canvas>

  {#if hoverMs !== null}
    <span class="tip mono" style:left="{Math.min(Math.max(toX(hoverMs), 28), width - 28)}px">
      {hhmmss(hoverMs)}
    </span>
  {/if}
</div>

<style>
  .wrapper {
    position: relative;
    width: 100%;
  }

  canvas {
    width: 100%;
    height: 62px;
    display: block;
    border-radius: 8px;
    cursor: crosshair;
    /* Impede o navegador de rolar a página enquanto se arrasta a timeline. */
    touch-action: none;
  }

  .tip {
    position: absolute;
    top: -22px;
    transform: translateX(-50%);
    background: var(--panel-2);
    border: 1px solid var(--line);
    border-radius: 6px;
    padding: 1px 6px;
    font-size: 11px;
    pointer-events: none;
    white-space: nowrap;
  }
</style>
