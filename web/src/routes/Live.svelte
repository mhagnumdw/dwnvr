<script>
  import { onMount } from 'svelte';
  import '../vendor/video-stream.js';
  import { cameras, loadCameras } from '../lib/state.svelte.js';
  import { mediaURL } from '../lib/api.js';

  const STORAGE_KEY = 'dwnvr.live.selection';
  const LAYOUT_KEY = 'dwnvr.live.layout';

  const COLUNAS = [1, 2, 3];
  // 'fit' é só mais um modo ao lado das colunas: como um exclui o outro por
  // construção, escolher 2× desliga o encaixar sem nenhum código para isso.
  const MODOS = ['fit', ...COLUNAS];

  // Precisa bater com o `gap` da grade no CSS: é o espaço que a conta do
  // encaixe desconta antes de dividir o que sobra entre os tiles.
  const GAP = 8;
  const PROPORCAO = 16 / 9;

  let selected = $state(new Set());
  let modo = $state(lerLayout());
  let showPicker = $state(false);
  let palco = $state(null);
  let encaixe = $state({ cols: 1, w: 0 });

  const visible = $derived(cameras.list.filter((c) => selected.has(c.id)));
  const tudoMarcado = $derived(
    cameras.list.length > 0 && selected.size === cameras.list.length,
  );

  // Lido na inicialização, não no onMount: começar sempre em 2× e corrigir
  // depois faria a grade piscar no celular, onde o padrão é 1×.
  function lerLayout() {
    const salvo = localStorage.getItem(LAYOUT_KEY);
    if (salvo === 'fit') return 'fit';
    if (COLUNAS.includes(Number(salvo))) return Number(salvo);
    return matchMedia('(min-width: 640px)').matches ? 2 : 1;
  }

  onMount(async () => {
    if (!cameras.list.length) await loadCameras();

    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved) {
      // Só mantém câmeras que ainda existem: uma seleção salva pode citar uma
      // câmera que foi removida do cadastro desde então.
      const ids = new Set(JSON.parse(saved));
      selected = new Set(cameras.list.filter((c) => ids.has(c.id)).map((c) => c.id));
    }
    if (!selected.size) {
      // Duas por padrão: abrir nove streams 1080p de uma vez trava celular.
      selected = new Set(cameras.list.slice(0, 2).map((c) => c.id));
    }
  });

  // O componente do go2rtc cria o <video> com controls=true. Numa grade ao
  // vivo isso mostra uma barra de progresso que não significa nada — não há
  // linha do tempo para percorrer — e ainda aparece em umas câmeras e não em
  // outras, conforme o modo negociado.
  function hideNativeControls(node) {
    const apply = () => {
      if (node.video) {
        node.video.controls = false;
        return true;
      }
      return false;
    };
    // O <video> nasce no connectedCallback, que pode não ter rodado ainda.
    if (!apply()) requestAnimationFrame(apply);
  }

  function fullscreen(el) {
    if (document.fullscreenElement) document.exitFullscreen();
    else el.requestFullscreen?.().catch(() => {});
  }

  // Reatribui em vez de mutar: runes não observam mudança dentro de um Set.
  function setSelection(next) {
    selected = next;
    localStorage.setItem(STORAGE_KEY, JSON.stringify([...next]));
  }

  function toggle(id) {
    const next = new Set(selected);
    next.has(id) ? next.delete(id) : next.add(id);
    setSelection(next);
  }

  function todas() {
    setSelection(tudoMarcado ? new Set() : new Set(cameras.list.map((c) => c.id)));
  }

  function setModo(m) {
    modo = m;
    localStorage.setItem(LAYOUT_KEY, String(m));
  }

  // Maior tile 16:9 que cabe: para cada número de colunas, a largura do tile é
  // limitada ou pela largura disponível, ou pela altura das linhas que sobram.
  // Como a altura entra na conta, a grade escolhida nunca transborda.
  function melhorEncaixe(n, W, H) {
    let melhor = { cols: 1, w: 0 };
    for (let cols = 1; cols <= n; cols++) {
      const linhas = Math.ceil(n / cols);
      const w = Math.min(
        (W - GAP * (cols - 1)) / cols,
        ((H - GAP * (linhas - 1)) / linhas) * PROPORCAO,
      );
      if (w > melhor.w) melhor = { cols, w };
    }
    return melhor;
  }

  function medir() {
    if (!palco) return;
    if (modo !== 'fit' || !visible.length) {
      palco.style.height = '';
      return;
    }

    // O palco vai do seu topo até o fim da janela, menos o que já está
    // reservado abaixo dele: o respiro da página e o espaço que o `main` guarda
    // para a navegação inferior não cobrir o conteúdo. Esse padding do `main` é
    // zero no desktop, onde a navegação sobe — lê-lo evita repetir aqui, em JS,
    // o breakpoint que já está no CSS.
    const abaixo = (el) => (el ? parseFloat(getComputedStyle(el).paddingBottom) || 0 : 0);
    const topo = palco.getBoundingClientRect().top;
    const H = Math.max(
      80,
      window.innerHeight - topo - abaixo(palco.parentElement) - abaixo(palco.closest('main')),
    );

    palco.style.height = `${H}px`;
    encaixe = melhorEncaixe(visible.length, palco.clientWidth, H);
  }

  $effect(() => {
    // Leituras explícitas porque `medir` sai cedo antes de tocar nelas: são o
    // que muda a altura disponível ou a quantidade de tiles.
    void [modo, visible.length, showPicker];
    medir();
  });
</script>

<svelte:window onresize={medir} onorientationchange={medir} />

<div class="page">
  <div class="row wrap">
    <button class="ghost" onclick={() => (showPicker = !showPicker)}>
      ☰ câmeras ({selected.size})
    </button>

    <div class="row modos" role="group" aria-label="layout">
      {#each MODOS as m (m)}
        <button
          class="ghost"
          class:on={modo === m}
          aria-pressed={modo === m}
          title={m === 'fit' ? 'encaixar todas na tela' : `${m} coluna${m > 1 ? 's' : ''}`}
          onclick={() => setModo(m)}
        >
          {#if m === 'fit'}
            <!-- Cantos de enquadramento desenhados à mão: o caractere ⛶ não
                 existe nas fontes do sistema no Linux nem no Android. -->
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                d="M4 9V5a1 1 0 0 1 1-1h4M15 4h4a1 1 0 0 1 1 1v4M20 15v4a1 1 0 0 1-1 1h-4M9 20H5a1 1 0 0 1-1-1v-4"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
          {:else}
            {m}×
          {/if}
        </button>
      {/each}
    </div>

    <span class="spacer"></span>
    {#if selected.size > 4}
      <span class="chip" title="cada stream é decodificado pelo seu aparelho, não pelo Pi">
        ⚠ {selected.size} streams simultâneos
      </span>
    {/if}
  </div>

  {#if showPicker}
    <div class="card selecao">
      {#if cameras.list.length}
        <div class="row">
          <button class="ghost" onclick={todas}>
            {tudoMarcado ? 'limpar' : `todas (${cameras.list.length})`}
          </button>
        </div>
        <div class="lista">
          {#each cameras.list as c (c.id)}
            <label class="row">
              <input type="checkbox" checked={selected.has(c.id)} onchange={() => toggle(c.id)} />
              <span>{c.name}</span>
            </label>
          {/each}
        </div>
      {:else}
        <p class="muted small">nenhuma câmera cadastrada</p>
      {/if}
    </div>
  {/if}

  <div class="palco" class:fit={modo === 'fit'} bind:this={palco}>
    <div
      class="grid"
      class:fit={modo === 'fit'}
      style:--cols={modo === 'fit' ? encaixe.cols : modo}
      style:--tile-w="{Math.floor(encaixe.w)}px"
    >
      {#each visible as c (c.id)}
        <!-- svelte-ignore a11y_no_static_element_interactions, a11y_click_events_have_key_events -->
        <div class="tile" ondblclick={(e) => fullscreen(e.currentTarget)} title="duplo clique: tela cheia">
          <!-- O componente do go2rtc negocia WebRTC/MSE sozinho. A mídia vai
               direto do navegador ao go2rtc; o dwnvr só faz proxy da
               sinalização, para que a credencial não chegue ao navegador. -->
          <video-stream
            {@attach (node) => {
              node.mode = 'webrtc,mse';
              node.src = mediaURL.liveWS(c.id);
              hideNativeControls(node);
              // Ao desmontar, o custom element fecha a conexão sozinho no
              // disconnectedCallback — nada a limpar aqui.
            }}
          ></video-stream>
          <span class="name">{c.name}</span>
        </div>
      {/each}
    </div>
  </div>

  {#if !visible.length && !showPicker}
    <p class="empty">
      {cameras.list.length ? 'Selecione as câmeras para visualizar.' : 'Nenhuma câmera cadastrada.'}
    </p>
  {/if}
</div>

<style>
  .page {
    display: grid;
    gap: 10px;
    padding: 10px;
    max-width: 1600px;
    margin: 0 auto;
  }

  .modos button { padding: 8px 11px; }
  .modos button.on { color: var(--fg); border-color: var(--accent); }

  .modos svg {
    display: block;
    width: 17px;
    height: 17px;
    fill: none;
    stroke: currentColor;
    stroke-width: 2;
  }

  .selecao { display: grid; gap: 10px; }

  .lista {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    gap: 6px 14px;
  }

  .lista label {
    gap: 8px;
    cursor: pointer;
    padding: 4px 0;
  }

  .lista input { min-height: 0; width: 18px; height: 18px; accent-color: var(--accent); }

  /* Recorta a grade no modo encaixar: a altura em pixels vem do `medir`, e sem
     isto um arredondamento para cima devolveria a rolagem que o modo evita. */
  .palco.fit { overflow: hidden; }

  .grid {
    display: grid;
    gap: 8px;
    grid-template-columns: repeat(var(--cols), minmax(0, 1fr));
  }

  /* No encaixar a largura do tile é calculada, não distribuída entre as
     colunas: é ela que impede a grade de passar da altura da janela. */
  .grid.fit {
    grid-template-columns: repeat(var(--cols), var(--tile-w));
    justify-content: center;
    align-content: center;
    height: 100%;
  }

  .tile {
    position: relative;
    background: #000;
    border-radius: var(--radius);
    overflow: hidden;
    aspect-ratio: 16 / 9;
  }

  .tile :global(video-stream),
  .tile :global(video) {
    width: 100%;
    height: 100%;
    display: block;
    object-fit: contain;
  }

  .name {
    position: absolute;
    left: 8px;
    bottom: 8px;
    background: rgba(0, 0, 0, 0.6);
    border-radius: 6px;
    padding: 2px 8px;
    font-size: 12px;
    pointer-events: none;
  }
</style>
