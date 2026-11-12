<!--
  A miniatura das áreas de uma câmera: um quadro dela com o desenho de cada
  área. Com várias áreas e o contorno tracejado, é o chip "2 áreas" do card em
  Câmeras e o chip do filtro em Detecções; com uma área só, cheia, é a imagem
  do RotuloDaArea e do LadrilhoDaArea.

  O quadro vem da miniatura da tira (thumbs.ts), já reduzida e em cache: quem
  abre a tela com várias câmeras não decodifica nenhuma em tamanho cheio. Ela
  tem a largura do quadrinho da tira; para desenhar maior sem borrar, quem já
  tem o quadro cheio o passa em `fundo` - o cadastro da câmera, que o busca
  para o editor. Sem `fundo` e mais larga que o quadrinho - o ladrilho do card
  da câmera -, ela pede a miniatura maior, na largura em que é desenhada. Câmera sem área não monta isto, e não paga nada.

  Com `zoom`, a área pequena (lib/area.ts, `vistaDaArea`), que no quadro
  inteiro some, ganha uma passada de zoom: aproxima até enquadrá-la, fica um
  instante e volta ao quadro inteiro. Uma vez quando a miniatura aparece;
  depois, o zoom fica enquanto o mouse para em cima de quem a contém, ou
  enquanto o dedo segura o toque longo nele, e volta quando ele sai ou solta.
  Quem a contém pede com `data-toque-longo` (lib/dicaDoToque.ts). Quem pede
  menos movimento ao sistema vê o salto, sem a transição. Só a área pequena paga o quadro maior
  que o zoom pede.
-->
<script lang="ts">
  import { onDestroy } from 'svelte';
  import {
    drawOutline,
    viewPoints,
    fillArea,
    WHOLE_FRAME,
    areaView,
    ZOOM_CEILING,
  } from '../lib/area.ts';
  import type { Point, Vista } from '../lib/area.ts';
  import { LONG_TOUCH } from '../lib/touchHint.ts';
  import { scaleToWidth, thumbnail, storedLastSegment } from '../lib/thumbs.ts';

  // O zoom: quanto leva para aproximar (e para voltar), quanto a passada
  // fica aproximada, e quanto o mouse tem de parar em cima para ele vir.
  const ZOOM_IDA_MS = 700;
  const ZOOM_FICA_MS = 1000;
  const ZOOM_WAIT_MS = 350;
  // E quanto ele fica depois que o mouse sai ou o dedo solta: um tremor da
  // mão não desfaz o zoom.
  const ZOOM_EXIT_MS = 500;

  interface Props {
    cam: string;
    areas: readonly { points: readonly Point[] }[];
    // Em px de CSS; a altura segue a proporção da câmera. Sem ela, a
    // miniatura ocupa a largura de quem a contém.
    width?: number;
    // `contorno`: o tracejado fino, que não esconde a imagem quando há várias
    // áreas; `cheia`: o azul translúcido da aba Áreas, para uma área só.
    drawing?: 'contorno' | 'cheia';
    // A URL de um quadro já em mãos; vazia enquanto ele não chega. Sem ela, a
    // miniatura da tira.
    background?: string | undefined;
    radius?: number;
    // A passada de zoom na área pequena. Só com uma área.
    zoom?: boolean;
  }

  let {
    cam,
    areas,
    width,
    drawing = 'contorno',
    background,
    radius = 3,
    zoom = false,
  }: Props = $props();

  let canvas = $state<HTMLCanvasElement | null>(null);
  let image = $state<{ source: CanvasImageSource; w: number; h: number } | null>(null);
  // A largura medida, quando ela não vem de `largura`.
  let measurement = $state(0);
  // Sobe quando o bitmap guardado aqui foi fechado pelo cache que o despejou:
  // a miniatura pede o quadro de novo.
  let refetch = $state(0);

  // O pedaço do quadro que enquadra a área, quando ela é pequena e há zoom.
  const target = $derived.by(() => {
    const a = areas[0];
    return zoom && areas.length === 1 && a ? areaView(a.points) : null;
  });
  const enlargeable = $derived(target !== null);

  // A largura em que ela é desenhada; 0 enquanto a medida não chega.
  const drawnWidth = $derived(width ?? measurement);
  // A miniatura da tira não aguenta o zoom: a da área pequena vem maior. Nem
  // a largura maior que a do quadrinho. As duas juntas: no zoom, o recorte de
  // 1/ZOOM_TETO do quadro ocupa a largura desenhada inteira, e o quadro tem de
  // ter ZOOM_TETO vezes ela para o recorte não borrar no ladrilho largo.
  // Derivada à parte, para o redimensionar só buscar de novo quando a escala
  // muda, e não a cada pixel.
  const scale = $derived(
    Math.max(
      enlargeable ? ZOOM_CEILING : 1,
      scaleToWidth(enlargeable ? drawnWidth * ZOOM_CEILING : drawnWidth),
    ),
  );
  const noMeasurement = $derived(drawnWidth === 0);

  $effect(() => {
    const c = cam;
    const f = background;
    // Com `fundo` a escala não conta: lida só sem ele, o redimensionar não
    // recarrega o quadro que já está em mãos.
    const e = f === undefined ? scale : 1;
    // eslint-disable-next-line @typescript-eslint/no-unused-expressions -- só a leitura, para o $effect rastrear
    refetch;
    let isLive = true;
    image = null;
    // Sem a medida, a escala ainda não é a certa: buscar agora baixaria a
    // miniatura duas vezes.
    if (f === undefined && noMeasurement) return;
    if (f === undefined) {
      void storedLastSegment(c)
        .then((t) => (t === null ? null : thumbnail(c, t, e)))
        .then((b) => {
          if (isLive && b) image = { source: b, w: b.width, h: b.height };
        })
        .catch(() => {
          // Sem quadro, ficam só os desenhos sobre o fundo escuro.
        });
    } else if (f) {
      const img = new Image();
      img.onload = () => {
        if (isLive) image = { source: img, w: img.naturalWidth, h: img.naturalHeight };
      };
      img.src = f;
    }
    return () => {
      isLive = false;
    };
  });

  $effect(() => {
    const c = canvas;
    if (width !== undefined || !c) return;
    const ro = new ResizeObserver(() => (measurement = c.clientWidth));
    ro.observe(c);
    return () => {
      ro.disconnect();
    };
  });

  const ratio = $derived(image?.h ? image.w / image.h : 16 / 9);

  // --- o zoom -----------------------------------------------------------------

  // O caminho do zoom, de 0 (o quadro inteiro) a 1 (a vista da área), e o
  // `quanto` que se desenha: o mesmo caminho, suavizado nas pontas.
  let path = 0;
  let amounts = $state(0);
  let animationFrame = 0;
  // O mouse ou o dedo segurando o zoom: ele não volta enquanto isso.
  let holding = false;
  let back: ReturnType<typeof setTimeout> | undefined;
  let wait: ReturnType<typeof setTimeout> | undefined;

  const suave = (t: number) => (t < 0.5 ? 4 * t ** 3 : 1 - (-2 * t + 2) ** 3 / 2);

  // Leva o zoom até `para`, de onde ele estiver: voltar no meio da ida leva
  // só o que já andou. Quem pede menos movimento vê o salto.
  function animate(dest: 0 | 1) {
    cancelAnimationFrame(animationFrame);
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) {
      path = dest;
      amounts = dest;
      animationFrame = 0;
      return;
    }
    let before = performance.now();
    const step = (now: number) => {
      const delta = (now - before) / ZOOM_IDA_MS;
      before = now;
      path = dest ? Math.min(1, path + delta) : Math.max(0, path - delta);
      amounts = suave(path);
      animationFrame = path === dest ? 0 : requestAnimationFrame(step);
    };
    animationFrame = requestAnimationFrame(step);
  }

  // Uma passada: aproxima, fica ZOOM_FICA_MS e volta, se ninguém segurar.
  function pass() {
    if (!enlargeable) return;
    animate(1);
    clearTimeout(back);
    back = setTimeout(() => {
      if (!holding) animate(0);
    }, ZOOM_IDA_MS + ZOOM_FICA_MS);
  }

  // Segura o zoom enquanto o mouse está por cima ou o dedo no toque longo.
  function holds() {
    if (!enlargeable) return;
    holding = true;
    clearTimeout(back);
    animate(1);
  }
  function release() {
    clearTimeout(wait);
    if (!holding) return;
    holding = false;
    clearTimeout(back);
    back = setTimeout(() => {
      if (!holding) animate(0);
    }, ZOOM_EXIT_MS);
  }

  onDestroy(() => {
    cancelAnimationFrame(animationFrame);
    clearTimeout(back);
    clearTimeout(wait);
  });

  // A primeira passada, quando a miniatura entra na vista com o quadro já
  // carregado: a aba escondida do cadastro, com altura zero, não conta.
  let isVisible = $state(false);
  let passed = false;
  $effect(() => {
    const c = canvas;
    if (!c || !enlargeable) return;
    const io = new IntersectionObserver(([e]) => (isVisible = !!e?.isIntersecting));
    io.observe(c);
    return () => {
      io.disconnect();
    };
  });
  $effect(() => {
    if (isVisible && image && enlargeable && !passed) {
      passed = true;
      pass();
    }
  });

  // Depois dela, o zoom fica enquanto o mouse para em cima de quem a contém
  // - passada rápida, a caminho de outro lugar, não conta - ou enquanto o
  // dedo segura o toque longo, e volta ZOOM_SAIDA_MS depois que ele sai ou
  // solta.
  $effect(() => {
    const host = canvas?.closest('[data-toque-longo]');
    if (!host || !enlargeable) return;
    const enter = (e: Event) => {
      if ((e as PointerEvent).pointerType !== 'mouse') return;
      clearTimeout(wait);
      // Ainda com zoom (o mouse voltou antes de ele sair), segura já.
      if (path > 0) holds();
      else wait = setTimeout(holds, ZOOM_WAIT_MS);
    };
    const touch = () => {
      holds();
      document.addEventListener('pointerup', release, { once: true });
      document.addEventListener('pointercancel', release, { once: true });
    };
    host.addEventListener('pointerenter', enter);
    host.addEventListener('pointerleave', release);
    host.addEventListener(LONG_TOUCH, touch);
    return () => {
      host.removeEventListener('pointerenter', enter);
      host.removeEventListener('pointerleave', release);
      host.removeEventListener(LONG_TOUCH, touch);
      document.removeEventListener('pointerup', release);
      document.removeEventListener('pointercancel', release);
    };
  });

  const mix = (a: Vista, b: Vista, t: number): Vista => ({
    x: a.x + (b.x - a.x) * t,
    y: a.y + (b.y - a.y) * t,
    w: a.w + (b.w - a.w) * t,
    h: a.h + (b.h - a.h) * t,
  });

  // --- o desenho --------------------------------------------------------------

  // Redesenha quando o quadro chega, quando a largura muda, quando as áreas
  // mudam (um save) e a cada passo do zoom.
  $effect(() => {
    const c = canvas;
    const g = c?.getContext('2d');
    const wid = width ?? measurement;
    if (!c || !g || !wid) return;
    const dpr = window.devicePixelRatio || 1;
    const w = Math.round(wid * dpr);
    const h = Math.round((wid / ratio) * dpr);
    const v = target && amounts > 0 ? mix(WHOLE_FRAME, target, amounts) : WHOLE_FRAME;
    if (c.width !== w) c.width = w;
    if (c.height !== h) c.height = h;
    g.fillStyle = '#000';
    g.fillRect(0, 0, w, h);
    // O cache de `thumbs.ts` fecha o bitmap da tira que despeja, e todos no
    // `clearThumbnails`; o fechado fica com largura zero. Esta miniatura ainda
    // o guardava: sai e pede o quadro de novo. Enquanto ele não chega,
    // `imagem` fica vazia, e o pedido não se repete a cada redesenho.
    if (image && image.source instanceof ImageBitmap && image.source.width === 0) {
      image = null;
      refetch++;
    }
    try {
      if (image) {
        const { source, w: iw, h: ih } = image;
        g.drawImage(source, v.x * iw, v.y * ih, v.w * iw, v.h * ih, 0, 0, w, h);
      }
    } catch {
      // Sem quadro, ficam só os desenhos sobre o fundo escuro.
    }
    const draw = drawing === 'cheia' ? fillArea : drawOutline;
    for (const a of areas) draw(g, viewPoints(a.points, v), w, h);
  });
</script>

<canvas
  bind:this={canvas}
  style:width={width === undefined ? '100%' : `${width}px`}
  style:aspect-ratio={ratio}
  style:border-radius="{radius}px"
  aria-hidden="true"
></canvas>

<style>
  canvas {
    display: block;
    flex: none;
    background: #000;
  }
</style>
