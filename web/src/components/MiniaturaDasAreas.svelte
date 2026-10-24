<!--
  A miniatura das áreas de uma câmera: o keyframe do último segmento com o
  contorno de cada área. É a do chip "2 áreas" do card em Câmeras.

  Usa a miniatura da tira (thumbs.ts), já reduzida e em cache: a imagem é
  pequena, e quem abre a tela com várias câmeras não decodifica nenhuma em
  tamanho cheio. Câmera sem área não monta isto, e não paga nada.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { desenhaContorno } from '../lib/area.ts';
  import type { Ponto } from '../lib/area.ts';
  import { thumbnail, ultimoSegmento } from '../lib/thumbs.ts';

  let { cam, areas }: { cam: string; areas: { points: Ponto[] }[] } = $props();

  // A largura do chip, em px de CSS; a altura segue a proporção da câmera.
  const LARGURA = 56;

  let canvas = $state<HTMLCanvasElement | null>(null);
  let bitmap = $state<ImageBitmap | null>(null);

  onMount(() => {
    let vivo = true;
    void ultimoSegmento(cam)
      .then((t) => (t === null ? null : thumbnail(cam, t)))
      .then((b) => {
        if (vivo) bitmap = b;
      })
      .catch(() => {
        // Sem quadro, o chip fica só com os contornos sobre o fundo escuro.
      });
    return () => {
      vivo = false;
    };
  });

  // Redesenha quando o quadro chega e quando as áreas mudam (um save).
  $effect(() => {
    const c = canvas;
    const g = c?.getContext('2d');
    if (!c || !g) return;
    const dpr = window.devicePixelRatio || 1;
    const proporcao = bitmap ? bitmap.width / bitmap.height : 16 / 9;
    const w = Math.round(LARGURA * dpr);
    const h = Math.round((LARGURA / proporcao) * dpr);
    c.width = w;
    c.height = h;
    g.fillStyle = '#000';
    g.fillRect(0, 0, w, h);
    try {
      if (bitmap) g.drawImage(bitmap, 0, 0, w, h);
    } catch {
      // O cache da tira fecha o bitmap que despeja; fica o fundo escuro.
    }
    for (const a of areas) desenhaContorno(g, a.points, w, h);
  });
</script>

<canvas bind:this={canvas} style:width="{LARGURA}px" aria-hidden="true"></canvas>

<style>
  canvas {
    display: block;
    border-radius: 3px;
  }
</style>
