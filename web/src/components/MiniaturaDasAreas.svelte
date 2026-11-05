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
    desenhaContorno,
    pontosNaVista,
    preencheArea,
    QUADRO_INTEIRO,
    vistaDaArea,
    ZOOM_TETO,
  } from '../lib/area.ts';
  import type { Ponto, Vista } from '../lib/area.ts';
  import { TOQUE_LONGO } from '../lib/dicaDoToque.ts';
  import { escalaParaLargura, thumbnail, ultimoSegmentoGuardado } from '../lib/thumbs.ts';

  // O zoom: quanto leva para aproximar (e para voltar), quanto a passada
  // fica aproximada, e quanto o mouse tem de parar em cima para ele vir.
  const ZOOM_IDA_MS = 700;
  const ZOOM_FICA_MS = 1000;
  const ZOOM_ESPERA_MS = 350;
  // E quanto ele fica depois que o mouse sai ou o dedo solta: um tremor da
  // mão não desfaz o zoom.
  const ZOOM_SAIDA_MS = 500;

  interface Props {
    cam: string;
    areas: readonly { points: readonly Ponto[] }[];
    // Em px de CSS; a altura segue a proporção da câmera. Sem ela, a
    // miniatura ocupa a largura de quem a contém.
    largura?: number;
    // `contorno`: o tracejado fino, que não esconde a imagem quando há várias
    // áreas; `cheia`: o azul translúcido da aba Áreas, para uma área só.
    desenho?: 'contorno' | 'cheia';
    // A URL de um quadro já em mãos; vazia enquanto ele não chega. Sem ela, a
    // miniatura da tira.
    fundo?: string | undefined;
    raio?: number;
    // A passada de zoom na área pequena. Só com uma área.
    zoom?: boolean;
  }

  let {
    cam,
    areas,
    largura,
    desenho = 'contorno',
    fundo,
    raio = 3,
    zoom = false,
  }: Props = $props();

  let canvas = $state<HTMLCanvasElement | null>(null);
  let imagem = $state<{ fonte: CanvasImageSource; w: number; h: number } | null>(null);
  // A largura medida, quando ela não vem de `largura`.
  let medida = $state(0);
  // Sobe quando o bitmap guardado aqui foi fechado pelo cache que o despejou:
  // a miniatura pede o quadro de novo.
  let rebusca = $state(0);

  // O pedaço do quadro que enquadra a área, quando ela é pequena e há zoom.
  const alvo = $derived.by(() => {
    const a = areas[0];
    return zoom && areas.length === 1 && a ? vistaDaArea(a.points) : null;
  });
  const ampliavel = $derived(alvo !== null);

  // A largura em que ela é desenhada; 0 enquanto a medida não chega.
  const larguraDesenhada = $derived(largura ?? medida);
  // A miniatura da tira não aguenta o zoom: a da área pequena vem maior. Nem
  // a largura maior que a do quadrinho. As duas juntas: no zoom, o recorte de
  // 1/ZOOM_TETO do quadro ocupa a largura desenhada inteira, e o quadro tem de
  // ter ZOOM_TETO vezes ela para o recorte não borrar no ladrilho largo.
  // Derivada à parte, para o redimensionar só buscar de novo quando a escala
  // muda, e não a cada pixel.
  const escala = $derived(
    Math.max(
      ampliavel ? ZOOM_TETO : 1,
      escalaParaLargura(ampliavel ? larguraDesenhada * ZOOM_TETO : larguraDesenhada),
    ),
  );
  const semMedida = $derived(larguraDesenhada === 0);

  $effect(() => {
    const c = cam;
    const f = fundo;
    // Com `fundo` a escala não conta: lida só sem ele, o redimensionar não
    // recarrega o quadro que já está em mãos.
    const e = f === undefined ? escala : 1;
    // eslint-disable-next-line @typescript-eslint/no-unused-expressions -- só a leitura, para o $effect rastrear
    rebusca;
    let vivo = true;
    imagem = null;
    // Sem a medida, a escala ainda não é a certa: buscar agora baixaria a
    // miniatura duas vezes.
    if (f === undefined && semMedida) return;
    if (f === undefined) {
      void ultimoSegmentoGuardado(c)
        .then((t) => (t === null ? null : thumbnail(c, t, e)))
        .then((b) => {
          if (vivo && b) imagem = { fonte: b, w: b.width, h: b.height };
        })
        .catch(() => {
          // Sem quadro, ficam só os desenhos sobre o fundo escuro.
        });
    } else if (f) {
      const img = new Image();
      img.onload = () => {
        if (vivo) imagem = { fonte: img, w: img.naturalWidth, h: img.naturalHeight };
      };
      img.src = f;
    }
    return () => {
      vivo = false;
    };
  });

  $effect(() => {
    const c = canvas;
    if (largura !== undefined || !c) return;
    const ro = new ResizeObserver(() => (medida = c.clientWidth));
    ro.observe(c);
    return () => {
      ro.disconnect();
    };
  });

  const proporcao = $derived(imagem?.h ? imagem.w / imagem.h : 16 / 9);

  // --- o zoom -----------------------------------------------------------------

  // O caminho do zoom, de 0 (o quadro inteiro) a 1 (a vista da área), e o
  // `quanto` que se desenha: o mesmo caminho, suavizado nas pontas.
  let caminho = 0;
  let quanto = $state(0);
  let quadroDaAnimacao = 0;
  // O mouse ou o dedo segurando o zoom: ele não volta enquanto isso.
  let segurando = false;
  let volta: ReturnType<typeof setTimeout> | undefined;
  let espera: ReturnType<typeof setTimeout> | undefined;

  const suave = (t: number) => (t < 0.5 ? 4 * t ** 3 : 1 - (-2 * t + 2) ** 3 / 2);

  // Leva o zoom até `para`, de onde ele estiver: voltar no meio da ida leva
  // só o que já andou. Quem pede menos movimento vê o salto.
  function anima(para: 0 | 1) {
    cancelAnimationFrame(quadroDaAnimacao);
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) {
      caminho = para;
      quanto = para;
      quadroDaAnimacao = 0;
      return;
    }
    let antes = performance.now();
    const passo = (agora: number) => {
      const anda = (agora - antes) / ZOOM_IDA_MS;
      antes = agora;
      caminho = para ? Math.min(1, caminho + anda) : Math.max(0, caminho - anda);
      quanto = suave(caminho);
      quadroDaAnimacao = caminho === para ? 0 : requestAnimationFrame(passo);
    };
    quadroDaAnimacao = requestAnimationFrame(passo);
  }

  // Uma passada: aproxima, fica ZOOM_FICA_MS e volta, se ninguém segurar.
  function passada() {
    if (!ampliavel) return;
    anima(1);
    clearTimeout(volta);
    volta = setTimeout(() => {
      if (!segurando) anima(0);
    }, ZOOM_IDA_MS + ZOOM_FICA_MS);
  }

  // Segura o zoom enquanto o mouse está por cima ou o dedo no toque longo.
  function segura() {
    if (!ampliavel) return;
    segurando = true;
    clearTimeout(volta);
    anima(1);
  }
  function solta() {
    clearTimeout(espera);
    if (!segurando) return;
    segurando = false;
    clearTimeout(volta);
    volta = setTimeout(() => {
      if (!segurando) anima(0);
    }, ZOOM_SAIDA_MS);
  }

  onDestroy(() => {
    cancelAnimationFrame(quadroDaAnimacao);
    clearTimeout(volta);
    clearTimeout(espera);
  });

  // A primeira passada, quando a miniatura entra na vista com o quadro já
  // carregado: a aba escondida do cadastro, com altura zero, não conta.
  let visivel = $state(false);
  let passou = false;
  $effect(() => {
    const c = canvas;
    if (!c || !ampliavel) return;
    const io = new IntersectionObserver(([e]) => (visivel = !!e?.isIntersecting));
    io.observe(c);
    return () => {
      io.disconnect();
    };
  });
  $effect(() => {
    if (visivel && imagem && ampliavel && !passou) {
      passou = true;
      passada();
    }
  });

  // Depois dela, o zoom fica enquanto o mouse para em cima de quem a contém
  // - passada rápida, a caminho de outro lugar, não conta - ou enquanto o
  // dedo segura o toque longo, e volta ZOOM_SAIDA_MS depois que ele sai ou
  // solta.
  $effect(() => {
    const host = canvas?.closest('[data-toque-longo]');
    if (!host || !ampliavel) return;
    const entra = (e: Event) => {
      if ((e as PointerEvent).pointerType !== 'mouse') return;
      clearTimeout(espera);
      // Ainda com zoom (o mouse voltou antes de ele sair), segura já.
      if (caminho > 0) segura();
      else espera = setTimeout(segura, ZOOM_ESPERA_MS);
    };
    const toque = () => {
      segura();
      document.addEventListener('pointerup', solta, { once: true });
      document.addEventListener('pointercancel', solta, { once: true });
    };
    host.addEventListener('pointerenter', entra);
    host.addEventListener('pointerleave', solta);
    host.addEventListener(TOQUE_LONGO, toque);
    return () => {
      host.removeEventListener('pointerenter', entra);
      host.removeEventListener('pointerleave', solta);
      host.removeEventListener(TOQUE_LONGO, toque);
      document.removeEventListener('pointerup', solta);
      document.removeEventListener('pointercancel', solta);
    };
  });

  const mistura = (a: Vista, b: Vista, t: number): Vista => ({
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
    const larg = largura ?? medida;
    if (!c || !g || !larg) return;
    const dpr = window.devicePixelRatio || 1;
    const w = Math.round(larg * dpr);
    const h = Math.round((larg / proporcao) * dpr);
    const v = alvo && quanto > 0 ? mistura(QUADRO_INTEIRO, alvo, quanto) : QUADRO_INTEIRO;
    if (c.width !== w) c.width = w;
    if (c.height !== h) c.height = h;
    g.fillStyle = '#000';
    g.fillRect(0, 0, w, h);
    // O cache de `thumbs.ts` fecha o bitmap da tira que despeja, e todos no
    // `clearThumbnails`; o fechado fica com largura zero. Esta miniatura ainda
    // o guardava: sai e pede o quadro de novo. Enquanto ele não chega,
    // `imagem` fica vazia, e o pedido não se repete a cada redesenho.
    if (imagem && imagem.fonte instanceof ImageBitmap && imagem.fonte.width === 0) {
      imagem = null;
      rebusca++;
    }
    try {
      if (imagem) {
        const { fonte, w: iw, h: ih } = imagem;
        g.drawImage(fonte, v.x * iw, v.y * ih, v.w * iw, v.h * ih, 0, 0, w, h);
      }
    } catch {
      // Sem quadro, ficam só os desenhos sobre o fundo escuro.
    }
    const desenha = desenho === 'cheia' ? preencheArea : desenhaContorno;
    for (const a of areas) desenha(g, pontosNaVista(a.points, v), w, h);
  });
</script>

<canvas
  bind:this={canvas}
  style:width={largura === undefined ? '100%' : `${largura}px`}
  style:aspect-ratio={proporcao}
  style:border-radius="{raio}px"
  aria-hidden="true"
></canvas>

<style>
  canvas {
    display: block;
    flex: none;
    background: #000;
  }
</style>
