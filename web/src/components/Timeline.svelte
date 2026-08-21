<script>
  import { hhmm, hhmmss } from '../lib/format.js';

  let {
    ranges = [],       // [[inícioMs, fimMs], …] faixas com gravação
    dayStart = 0,
    dayEnd = 0,
    currentMs = 0,
    onseek = () => {},
    // Janela visível: o único estado que o zoom e o arraste alteram. Sobe para
    // quem chama porque é ela que vai para a URL - o link precisa reproduzir o
    // zoom, não só o instante. Zero nos dois quer dizer "ainda não escolhida", e
    // é o que faz o efeito abaixo ancorar no dia inteiro.
    viewFrom = $bindable(0),
    viewTo = $bindable(0),
  } = $props();

  let canvas;
  let width = $state(0);
  let hoverMs = $state(null);

  const MIN_SPAN = 20_000; // 20s de zoom máximo

  // Reancorar quando o dia muda; comparar os limites evita reancorar a cada
  // repintura e perder o zoom que o usuário acabou de dar. O piso do span faz o
  // mesmo papel para uma janela vinda da URL: o `zoomTo` nunca produz menos que
  // MIN_SPAN, mas um link editado à mão produz - e aí o zoom máximo, que é
  // regra desta timeline, passaria a valer só para quem chegou clicando.
  $effect(() => {
    const janela = viewTo - viewFrom;
    if (dayStart && (viewFrom < dayStart || viewTo > dayEnd || viewTo === 0 || janela < MIN_SPAN)) {
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
  // Cada ponteiro guarda também o x inicial: o limiar de arraste precisa do
  // deslocamento acumulado, não do passo do último evento.
  const pointers = new Map();
  let dragged = false;
  let pinchStart = null;
  let lastTap = null;     // { t, x } do último toque limpo, para o duplo toque
  let swallowUp = false;  // o up do 2º dedo não pode virar seek

  const DOUBLE_TAP_MS = 300;
  const DOUBLE_TAP_PX = 32;
  const ZOOM_STEP = 3; // um gesto deliberado merece um salto maior que o da roda

  function localX(ev) {
    return ev.clientX - canvas.getBoundingClientRect().left;
  }

  function onPointerDown(ev) {
    canvas.setPointerCapture(ev.pointerId);
    const x = localX(ev);
    pointers.set(ev.pointerId, { x, x0: x });
    dragged = false;
    if (pointers.size === 2) {
      const [a, b] = [...pointers.values()];
      const mid = (a.x + b.x) / 2;
      pinchStart = {
        dist: Math.abs(a.x - b.x),
        from: viewFrom,
        to: viewTo,
        mid,
        anchorMs: toMs(mid), // o instante sob os dedos é o que fica parado
        t: ev.timeStamp,
        moved: false,
      };
      lastTap = null; // dois dedos nunca continuam um duplo toque
    }
  }

  function onPointerMove(ev) {
    const p = pointers.get(ev.pointerId);
    if (!p) {
      if (ev.pointerType === 'mouse') hoverMs = toMs(localX(ev));
      return;
    }
    const x = localX(ev);
    const prev = p.x;
    p.x = x;

    if (pointers.size === 2 && pinchStart) {
      const [a, b] = [...pointers.values()];
      const dist = Math.max(1, Math.abs(a.x - b.x));
      const mid = (a.x + b.x) / 2;
      if (Math.abs(dist - pinchStart.dist) > 8 || Math.abs(mid - pinchStart.mid) > 8) {
        pinchStart.moved = true;
      }
      const factor = pinchStart.dist / dist;
      // Ancorar entre os dedos, como a roda ancora no cursor. Usar o mid vivo
      // como fração dá o arrastar-enquanto-pinça de brinde.
      zoomTo(pinchStart.anchorMs, (pinchStart.to - pinchStart.from) * factor, mid / width);
      dragged = true;
      return;
    }

    const dx = x - prev;
    // Dedo treme: 1px marcaria arraste e cancelaria o seek. Mouse é preciso.
    const slop = ev.pointerType === 'mouse' ? 2 : 8;
    if (Math.abs(x - p.x0) > slop) dragged = true;
    pan((-dx / Math.max(1, width)) * span);
  }

  function onPointerUp(ev) {
    const p = pointers.get(ev.pointerId);
    const x = p ? p.x : localX(ev);
    pointers.delete(ev.pointerId);

    // Toque rápido com dois dedos, sem pinçar nem arrastar: afasta um passo.
    if (pinchStart) {
      const { mid, moved, t } = pinchStart;
      pinchStart = null;
      lastTap = null;
      swallowUp = true;
      if (!moved && ev.timeStamp - t < DOUBLE_TAP_MS) {
        zoomTo(toMs(mid), span * ZOOM_STEP, mid / width);
      }
      return;
    }
    if (swallowUp) {
      swallowUp = pointers.size > 0;
      return;
    }

    // Um arraste não deve virar um salto: só o toque limpo navega.
    if (dragged) {
      lastTap = null;
      return;
    }

    // Duplo toque: o 1º já pulou para o ponto, o 2º aproxima ancorado ali -
    // por isso nenhum gesto precisa esperar para saber se vem um segundo toque.
    if (lastTap && ev.timeStamp - lastTap.t < DOUBLE_TAP_MS
        && Math.abs(x - lastTap.x) < DOUBLE_TAP_PX) {
      lastTap = null;
      zoomTo(toMs(x), span / ZOOM_STEP, x / width);
      return;
    }

    // No desktop a roda já resolve o zoom, e clicar repetido para ajustar a
    // posição não pode virar zoom surpresa.
    if (ev.pointerType !== 'mouse') lastTap = { t: ev.timeStamp, x };
    onseek(Math.round(toMs(x)));
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
