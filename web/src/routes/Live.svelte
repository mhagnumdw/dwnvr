<script>
  import { onMount } from 'svelte';
  import '../vendor/video-stream.js';
  import { cameras, loadCameras } from '../lib/state.svelte.js';
  import { mediaURL } from '../lib/api.js';

  const STORAGE_KEY = 'dwnvr.live.selection';

  let selected = $state(new Set());
  let cols = $state(2);
  let showPicker = $state(false);

  const visible = $derived(cameras.list.filter((c) => selected.has(c.id)));

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
    showPicker = selected.size === 0;
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

  function toggle(id) {
    const next = new Set(selected);
    next.has(id) ? next.delete(id) : next.add(id);
    selected = next;
    localStorage.setItem(STORAGE_KEY, JSON.stringify([...next]));
  }
</script>

<div class="page">
  <div class="bar row wrap">
    <button class="ghost" onclick={() => (showPicker = !showPicker)}>
      ☰ câmeras ({selected.size})
    </button>

    <div class="row cols" role="group" aria-label="colunas">
      {#each [1, 2, 3] as n}
        <button class="ghost" class:on={cols === n} onclick={() => (cols = n)}>{n}×</button>
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
    <div class="card picker">
      {#each cameras.list as c (c.id)}
        <label class="row">
          <input type="checkbox" checked={selected.has(c.id)} onchange={() => toggle(c.id)} />
          <span>{c.name}</span>
        </label>
      {/each}
      {#if !cameras.list.length}
        <p class="muted small">nenhuma câmera cadastrada</p>
      {/if}
    </div>
  {/if}

  <div class="grid" style:--cols={cols}>
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

  {#if !visible.length && !showPicker}
    <p class="empty">Selecione as câmeras para visualizar.</p>
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

  .cols button { padding: 8px 11px; }
  .cols button.on { color: var(--fg); border-color: var(--accent); }

  .picker {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    gap: 6px 14px;
  }

  .picker label {
    gap: 8px;
    cursor: pointer;
    padding: 4px 0;
  }

  .picker input { min-height: 0; width: 18px; height: 18px; accent-color: var(--accent); }

  .grid {
    display: grid;
    /* No celular a grade é sempre de uma coluna: dois vídeos lado a lado num
       telefone não mostram nada útil de nenhum dos dois. */
    grid-template-columns: 1fr;
    gap: 8px;
  }

  @media (min-width: 640px) {
    .grid { grid-template-columns: repeat(var(--cols), 1fr); }
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
