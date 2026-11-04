<!--
  A miniatura das áreas de uma câmera: um quadro dela com o desenho de cada
  área. Com várias áreas e o contorno tracejado, é o chip "2 áreas" do card em
  Câmeras e o chip do filtro em Detecções; com uma área só, cheia, é a imagem
  do ChipDaArea, do RotuloDaArea e do LadrilhoDaArea.

  O quadro vem da miniatura da tira (thumbs.ts), já reduzida e em cache: quem
  abre a tela com várias câmeras não decodifica nenhuma em tamanho cheio. Ela
  tem a largura do quadrinho da tira; para desenhar maior sem borrar, quem já
  tem o quadro cheio o passa em `fundo` - o cadastro da câmera, que o busca
  para o editor. Câmera sem área não monta isto, e não paga nada.
-->
<script lang="ts">
  import { desenhaContorno, preencheArea } from '../lib/area.ts';
  import type { Ponto } from '../lib/area.ts';
  import { thumbnail, ultimoSegmentoGuardado } from '../lib/thumbs.ts';

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
  }

  let { cam, areas, largura, desenho = 'contorno', fundo, raio = 3 }: Props = $props();

  let canvas = $state<HTMLCanvasElement | null>(null);
  let imagem = $state<{ fonte: CanvasImageSource; w: number; h: number } | null>(null);
  // A largura medida, quando ela não vem de `largura`.
  let medida = $state(0);

  $effect(() => {
    const c = cam;
    const f = fundo;
    let vivo = true;
    imagem = null;
    if (f === undefined) {
      void ultimoSegmentoGuardado(c)
        .then((t) => (t === null ? null : thumbnail(c, t)))
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

  // Redesenha quando o quadro chega, quando a largura muda e quando as áreas
  // mudam (um save).
  $effect(() => {
    const c = canvas;
    const g = c?.getContext('2d');
    const larg = largura ?? medida;
    if (!c || !g || !larg) return;
    const dpr = window.devicePixelRatio || 1;
    const w = Math.round(larg * dpr);
    const h = Math.round((larg / proporcao) * dpr);
    c.width = w;
    c.height = h;
    g.fillStyle = '#000';
    g.fillRect(0, 0, w, h);
    try {
      if (imagem) g.drawImage(imagem.fonte, 0, 0, w, h);
    } catch {
      // O cache da tira fecha o bitmap que despeja; fica o fundo escuro.
    }
    const desenha = desenho === 'cheia' ? preencheArea : desenhaContorno;
    for (const a of areas) desenha(g, a.points, w, h);
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
