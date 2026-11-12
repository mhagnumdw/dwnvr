<script lang="ts">
  // A camada das caixas do detector de objetos, por cima do vídeo das gravações.
  //
  // A caixa é de UM quadro - o que o detector olhou, `quadroMs` -, e por isso
  // não fica na tela o tempo todo: ela acende quando a reprodução passa por esse
  // quadro, fica acesa ACESA_MS e esmaece. Com o vídeo pausado, a caixa acesa
  // fica acesa, e volta a contar o tempo quando a reprodução volta.
  //
  // A disposição e o desenho moram em lib/caixas.ts, que a captura de quadro
  // também usa: esta camada só decide QUAIS caixas estão acesas e quanto.
  import { onMount, untrack } from 'svelte';
  import type { Box } from '../lib/api.ts';
  import { context2d } from '../lib/canvas.ts';
  import { place, draw, LIGHT_UP_MS, LIT_MS, FADE_MS, LANDING_MS } from '../lib/boxes.ts';
  import type { PlacedBox, ObjectDetectorMark } from '../lib/boxes.ts';

  // Uma marca com o instante do quadro olhado, quando houver: as do
  // /api/rec/events, e os objetos de uma detecção com o `quadroMs` dela.
  type Mark = ObjectDetectorMark & { quadroMs?: number };

  interface Props {
    video: HTMLVideoElement; // o <video> do palco
    objects?: Mark[]; // as marcas do dia, como o /api/rec/events as entrega
    currentMs?: number;
    playing?: boolean;
    rate?: number;
    labels?: boolean;
  }

  // A marca que tem o que acender: a caixa e o quadro olhado.
  type LightableMark = Mark & { caixa: Box; quadroMs: number };

  interface Lit {
    mark: LightableMark;
    desde: number;
  }

  let {
    video,
    objects = [],
    currentMs = 0,
    playing = false,
    rate = 1,
    labels = true,
  }: Props = $props();

  let canvas: HTMLCanvasElement | undefined;
  // Onde o <video> está no palco. Ele é centralizado e tem altura máxima, então
  // nem sempre começa no canto do palco.
  let element = $state({ left: 0, top: 0, w: 0, h: 0 });
  // E onde a IMAGEM está dentro dele: com `object-fit: contain`, uma câmera que
  // não seja 16:9 ganha faixas pretas, e a fração da caixa é da imagem.
  let image = { x: 0, y: 0, w: 0, h: 0 };

  // As caixas acesas: chave -> { marca, desde }, com `desde` no relógio de
  // performance.now(). Sem reatividade de propósito: quem desenha é o canvas,
  // e redesenhar é pedido à mão.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- quem desenha é o canvas
  const lit = new Map<string, Lit>();
  let pausedSince: number | null = null;
  let items: PlacedBox<LightableMark>[] = [];
  let placed = '';
  let requestNo = 0;

  // Uma olhada grava uma linha por objeto, e duas da mesma família no mesmo
  // quadro só se distinguem pela caixa. Quadro e família não bastam: a segunda
  // pessoa sobrescreveria a primeira, e só uma caixa acenderia.
  const key = (o: LightableMark) => `${o.quadroMs}|${o.familia}|${o.caixa.join(',')}`;
  const LIFE_MS = LIGHT_UP_MS + LIT_MS + FADE_MS;

  // Só as marcas que têm caixa e o instante do quadro olhado: sem os dois não
  // há o que acender, nem quando.
  const withBox = $derived(objects.filter((o): o is LightableMark => !!o.caixa && !!o.quadroMs));

  // --- quando acender -------------------------------------------------------

  let lastMs = 0;
  let lastClock = 0;

  $effect(() => {
    const t = currentMs;
    untrack(() => {
      advance(t);
    });
  });

  function advance(t: number) {
    const now = performance.now();
    // Reproduzindo, o vídeo anda entre duas atualizações mais ou menos o tempo
    // de relógio vezes a velocidade. Andar muito mais que isso, ou para trás, é
    // um PULO - seek, toque na timeline, troca de dia -, e pulo não "passa" por
    // quadro nenhum: pular uma hora não pode acender as caixas da hora inteira.
    const elapsed = Math.min(now - lastClock, 2000);
    const leap = t - lastMs;
    const moved =
      playing && lastClock > 0 && leap >= 0 && leap <= elapsed * Math.max(1, rate) * 1.5 + 500;

    if (moved) {
      for (const o of withBox) {
        if (o.quadroMs > lastMs && o.quadroMs <= t) lit.set(key(o), { mark: o, desde: now });
      }
    } else {
      // O que estava aceso era do trecho de onde se saiu.
      lit.clear();
      for (const o of withBox) {
        if (o.quadroMs <= t && o.quadroMs >= t - LANDING_MS)
          lit.set(key(o), { mark: o, desde: now });
      }
    }
    lastMs = t;
    lastClock = now;
    agenda();
  }

  $effect(() => {
    const playingNow = playing;
    untrack(() => {
      const now = performance.now();
      if (!playingNow && pausedSince === null) {
        deleteExpired(now);
        pausedSince = now;
      } else if (playingNow && pausedSince !== null) {
        // O tempo parado não conta: a caixa retoma de onde estava.
        for (const a of lit.values()) a.desde += now - pausedSince;
        pausedSince = null;
      }
      lastClock = now;
      agenda();
    });
  });

  $effect(() => {
    // eslint-disable-next-line @typescript-eslint/no-unused-expressions -- só a leitura, para o $effect rastrear
    labels;
    untrack(agenda);
  });

  function deleteExpired(now: number) {
    for (const [k, a] of lit) if (now - a.desde >= LIFE_MS) lit.delete(k);
  }

  function alpha(a: Lit | undefined, now: number) {
    if (!a) return 0;
    if (pausedSince !== null) return 1;
    const age = now - a.desde;
    if (age < LIGHT_UP_MS) return age / LIGHT_UP_MS;
    if (age < LIGHT_UP_MS + LIT_MS) return 1;
    return Math.max(0, 1 - (age - LIGHT_UP_MS - LIT_MS) / FADE_MS);
  }

  // --- desenho --------------------------------------------------------------

  function agenda() {
    if (!requestNo) requestNo = requestAnimationFrame(frame);
  }

  function frame() {
    requestNo = 0;
    const now = performance.now();
    if (pausedSince === null) deleteExpired(now);
    paint(now);
    // Só pede o próximo quadro enquanto alguma caixa muda de opacidade. Parado,
    // ou sem caixa acesa, a camada não gasta nada.
    if (lit.size && pausedSince === null) agenda();
  }

  // A disposição só é refeita quando muda o conjunto aceso, o tamanho ou os
  // rótulos: refazer a cada quadro faria um rótulo trocar de lado no meio do
  // esmaecimento do vizinho.
  function placement(g: CanvasRenderingContext2D) {
    const k = `${[...lit.keys()].sort().join(',')}|${image.w}x${image.h}|${labels}`;
    if (k !== placed) {
      items = place(
        g,
        [...lit.values()].map((a) => a.mark),
        image.w,
        image.h,
        labels,
      );
      placed = k;
    }
    return items;
  }

  function paint(now: number) {
    if (!canvas) return;
    const dpr = window.devicePixelRatio || 1;
    const { w, h } = element;
    const cw = Math.round(w * dpr);
    const ch = Math.round(h * dpr);
    if (canvas.width !== cw || canvas.height !== ch) {
      canvas.width = cw;
      canvas.height = ch;
    }
    const g = context2d(canvas);
    g.setTransform(dpr, 0, 0, dpr, 0, 0);
    g.clearRect(0, 0, w, h);
    if (!lit.size || !image.w) return;
    g.translate(image.x, image.y);
    draw(g, placement(g), (it) => alpha(lit.get(key(it.mark)), now));
  }

  // desenhaEm pinta as caixas acesas agora sobre a imagem baixada, que tem o
  // tamanho NATIVO da câmera. A disposição é a da tela, só ampliada: a imagem
  // sai como a tela está, com o rótulo no mesmo lugar e do mesmo tamanho
  // relativo. A opacidade vai cheia - uma caixa pela metade numa foto parece
  // defeito, não esmaecimento.
  export function drawOn(g: CanvasRenderingContext2D, width: number, height: number) {
    if (!lit.size) return;
    if (!image.w) {
      draw(
        g,
        place(
          g,
          [...lit.values()].map((a) => a.mark),
          width,
          height,
          labels,
        ),
      );
      return;
    }
    g.save();
    g.scale(width / image.w, height / image.h);
    draw(g, placement(g));
    g.restore();
  }

  // --- tamanho --------------------------------------------------------------

  function measure() {
    const w = video.clientWidth;
    const h = video.clientHeight;
    element = { left: video.offsetLeft, top: video.offsetTop, w, h };
    const vw = video.videoWidth;
    const vh = video.videoHeight;
    if (!vw || !vh || !w || !h) {
      image = { x: 0, y: 0, w: 0, h: 0 };
    } else {
      const s = Math.min(w / vw, h / vh);
      image = { x: (w - vw * s) / 2, y: (h - vh * s) / 2, w: vw * s, h: vh * s };
    }
    agenda();
  }

  onMount(() => {
    const ro = new ResizeObserver(measure);
    ro.observe(video);
    // A resolução chega depois do elemento, e muda quando a câmera muda.
    video.addEventListener('loadedmetadata', measure);
    video.addEventListener('resize', measure);
    measure();
    return () => {
      ro.disconnect();
      video.removeEventListener('loadedmetadata', measure);
      video.removeEventListener('resize', measure);
      cancelAnimationFrame(requestNo);
    };
  });
</script>

<canvas
  bind:this={canvas}
  style:left="{element.left}px"
  style:top="{element.top}px"
  style:width="{element.w}px"
  style:height="{element.h}px"
></canvas>

<style>
  canvas {
    position: absolute;
    pointer-events: none;
  }
</style>
