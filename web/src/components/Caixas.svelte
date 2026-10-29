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
  import type { Caixa } from '../lib/api.ts';
  import { contexto2d } from '../lib/canvas.ts';
  import { dispoe, desenha, ACENDER_MS, ACESA_MS, ESMAECER_MS, POUSO_MS } from '../lib/caixas.ts';
  import type { CaixaDisposta, MarcaDoDetector } from '../lib/caixas.ts';

  // Uma marca com o instante do quadro olhado, quando houver: as do
  // /api/rec/events, e os objetos de uma detecção com o `quadroMs` dela.
  type Marca = MarcaDoDetector & { quadroMs?: number };

  interface Props {
    video: HTMLVideoElement; // o <video> do palco
    objetos?: Marca[]; // as marcas do dia, como o /api/rec/events as entrega
    currentMs?: number;
    playing?: boolean;
    rate?: number;
    rotulos?: boolean;
  }

  // A marca que tem o que acender: a caixa e o quadro olhado.
  type MarcaAcendivel = Marca & { caixa: Caixa; quadroMs: number };

  interface Acesa {
    marca: MarcaAcendivel;
    desde: number;
  }

  let {
    video,
    objetos = [],
    currentMs = 0,
    playing = false,
    rate = 1,
    rotulos = true,
  }: Props = $props();

  let canvas: HTMLCanvasElement | undefined;
  // Onde o <video> está no palco. Ele é centralizado e tem altura máxima, então
  // nem sempre começa no canto do palco.
  let elemento = $state({ left: 0, top: 0, w: 0, h: 0 });
  // E onde a IMAGEM está dentro dele: com `object-fit: contain`, uma câmera que
  // não seja 16:9 ganha faixas pretas, e a fração da caixa é da imagem.
  let imagem = { x: 0, y: 0, w: 0, h: 0 };

  // As caixas acesas: chave -> { marca, desde }, com `desde` no relógio de
  // performance.now(). Sem reatividade de propósito: quem desenha é o canvas,
  // e redesenhar é pedido à mão.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- quem desenha é o canvas
  const acesas = new Map<string, Acesa>();
  let pausadaDesde: number | null = null;
  let itens: CaixaDisposta<MarcaAcendivel>[] = [];
  let disposto = '';
  let pedido = 0;

  // Uma olhada grava uma linha por objeto, e duas da mesma família no mesmo
  // quadro só se distinguem pela caixa. Quadro e família não bastam: a segunda
  // pessoa sobrescreveria a primeira, e só uma caixa acenderia.
  const chave = (o: MarcaAcendivel) => `${o.quadroMs}|${o.familia}|${o.caixa.join(',')}`;
  const VIDA_MS = ACENDER_MS + ACESA_MS + ESMAECER_MS;

  // Só as marcas que têm caixa e o instante do quadro olhado: sem os dois não
  // há o que acender, nem quando.
  const comCaixa = $derived(objetos.filter((o): o is MarcaAcendivel => !!o.caixa && !!o.quadroMs));

  // --- quando acender -------------------------------------------------------

  let ultimoMs = 0;
  let ultimoRelogio = 0;

  $effect(() => {
    const t = currentMs;
    untrack(() => {
      avanca(t);
    });
  });

  function avanca(t: number) {
    const agora = performance.now();
    // Reproduzindo, o vídeo anda entre duas atualizações mais ou menos o tempo
    // de relógio vezes a velocidade. Andar muito mais que isso, ou para trás, é
    // um PULO - seek, toque na timeline, troca de dia -, e pulo não "passa" por
    // quadro nenhum: pular uma hora não pode acender as caixas da hora inteira.
    const decorrido = Math.min(agora - ultimoRelogio, 2000);
    const salto = t - ultimoMs;
    const andou =
      playing &&
      ultimoRelogio > 0 &&
      salto >= 0 &&
      salto <= decorrido * Math.max(1, rate) * 1.5 + 500;

    if (andou) {
      for (const o of comCaixa) {
        if (o.quadroMs > ultimoMs && o.quadroMs <= t)
          acesas.set(chave(o), { marca: o, desde: agora });
      }
    } else {
      // O que estava aceso era do trecho de onde se saiu.
      acesas.clear();
      for (const o of comCaixa) {
        if (o.quadroMs <= t && o.quadroMs >= t - POUSO_MS)
          acesas.set(chave(o), { marca: o, desde: agora });
      }
    }
    ultimoMs = t;
    ultimoRelogio = agora;
    agenda();
  }

  $effect(() => {
    const tocando = playing;
    untrack(() => {
      const agora = performance.now();
      if (!tocando && pausadaDesde === null) {
        apagaVencidas(agora);
        pausadaDesde = agora;
      } else if (tocando && pausadaDesde !== null) {
        // O tempo parado não conta: a caixa retoma de onde estava.
        for (const a of acesas.values()) a.desde += agora - pausadaDesde;
        pausadaDesde = null;
      }
      ultimoRelogio = agora;
      agenda();
    });
  });

  $effect(() => {
    // eslint-disable-next-line @typescript-eslint/no-unused-expressions -- só a leitura, para o $effect rastrear
    rotulos;
    untrack(agenda);
  });

  function apagaVencidas(agora: number) {
    for (const [k, a] of acesas) if (agora - a.desde >= VIDA_MS) acesas.delete(k);
  }

  function alfa(a: Acesa | undefined, agora: number) {
    if (!a) return 0;
    if (pausadaDesde !== null) return 1;
    const idade = agora - a.desde;
    if (idade < ACENDER_MS) return idade / ACENDER_MS;
    if (idade < ACENDER_MS + ACESA_MS) return 1;
    return Math.max(0, 1 - (idade - ACENDER_MS - ACESA_MS) / ESMAECER_MS);
  }

  // --- desenho --------------------------------------------------------------

  function agenda() {
    if (!pedido) pedido = requestAnimationFrame(quadro);
  }

  function quadro() {
    pedido = 0;
    const agora = performance.now();
    if (pausadaDesde === null) apagaVencidas(agora);
    pinta(agora);
    // Só pede o próximo quadro enquanto alguma caixa muda de opacidade. Parado,
    // ou sem caixa acesa, a camada não gasta nada.
    if (acesas.size && pausadaDesde === null) agenda();
  }

  // A disposição só é refeita quando muda o conjunto aceso, o tamanho ou os
  // rótulos: refazer a cada quadro faria um rótulo trocar de lado no meio do
  // esmaecimento do vizinho.
  function disposicao(g: CanvasRenderingContext2D) {
    const k = `${[...acesas.keys()].sort().join(',')}|${imagem.w}x${imagem.h}|${rotulos}`;
    if (k !== disposto) {
      itens = dispoe(
        g,
        [...acesas.values()].map((a) => a.marca),
        imagem.w,
        imagem.h,
        rotulos,
      );
      disposto = k;
    }
    return itens;
  }

  function pinta(agora: number) {
    if (!canvas) return;
    const dpr = window.devicePixelRatio || 1;
    const { w, h } = elemento;
    const cw = Math.round(w * dpr);
    const ch = Math.round(h * dpr);
    if (canvas.width !== cw || canvas.height !== ch) {
      canvas.width = cw;
      canvas.height = ch;
    }
    const g = contexto2d(canvas);
    g.setTransform(dpr, 0, 0, dpr, 0, 0);
    g.clearRect(0, 0, w, h);
    if (!acesas.size || !imagem.w) return;
    g.translate(imagem.x, imagem.y);
    desenha(g, disposicao(g), (it) => alfa(acesas.get(chave(it.marca)), agora));
  }

  // desenhaEm pinta as caixas acesas agora sobre a imagem baixada, que tem o
  // tamanho NATIVO da câmera. A disposição é a da tela, só ampliada: a imagem
  // sai como a tela está, com o rótulo no mesmo lugar e do mesmo tamanho
  // relativo. A opacidade vai cheia - uma caixa pela metade numa foto parece
  // defeito, não esmaecimento.
  export function desenhaEm(g: CanvasRenderingContext2D, largura: number, altura: number) {
    if (!acesas.size) return;
    if (!imagem.w) {
      desenha(
        g,
        dispoe(
          g,
          [...acesas.values()].map((a) => a.marca),
          largura,
          altura,
          rotulos,
        ),
      );
      return;
    }
    g.save();
    g.scale(largura / imagem.w, altura / imagem.h);
    desenha(g, disposicao(g));
    g.restore();
  }

  // --- tamanho --------------------------------------------------------------

  function mede() {
    const w = video.clientWidth;
    const h = video.clientHeight;
    elemento = { left: video.offsetLeft, top: video.offsetTop, w, h };
    const vw = video.videoWidth;
    const vh = video.videoHeight;
    if (!vw || !vh || !w || !h) {
      imagem = { x: 0, y: 0, w: 0, h: 0 };
    } else {
      const s = Math.min(w / vw, h / vh);
      imagem = { x: (w - vw * s) / 2, y: (h - vh * s) / 2, w: vw * s, h: vh * s };
    }
    agenda();
  }

  onMount(() => {
    const ro = new ResizeObserver(mede);
    ro.observe(video);
    // A resolução chega depois do elemento, e muda quando a câmera muda.
    video.addEventListener('loadedmetadata', mede);
    video.addEventListener('resize', mede);
    mede();
    return () => {
      ro.disconnect();
      video.removeEventListener('loadedmetadata', mede);
      video.removeEventListener('resize', mede);
      cancelAnimationFrame(pedido);
    };
  });
</script>

<canvas
  bind:this={canvas}
  style:left="{elemento.left}px"
  style:top="{elemento.top}px"
  style:width="{elemento.w}px"
  style:height="{elemento.h}px"
></canvas>

<style>
  canvas {
    position: absolute;
    pointer-events: none;
  }
</style>
