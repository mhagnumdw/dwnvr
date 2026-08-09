<script>
  import { onMount, onDestroy } from 'svelte';
  import { cameras, loadCameras, health, pollHealth } from '../lib/state.svelte.js';
  import { api } from '../lib/api.js';
  import { dias, kbps, bytes, resolucao } from '../lib/format.js';

  let editing = $state(null); // cópia da câmera em edição, ou null
  let saving = $state(false);
  let error = $state('');

  const novos = $derived(cameras.streams.filter((s) => !s.registered));

  onMount(() => {
    if (!cameras.list.length) loadCameras();
  });

  const stop = pollHealth(5000);
  onDestroy(stop);

  function statusOf(id) {
    return health.cameras.find((h) => h.id === id);
  }

  // A taxa medida é o que transforma uma cota abstrata em tempo de retenção.
  function estimaDias(cam) {
    const rate = statusOf(cam.id)?.bitrateKbps || 0;
    if (!rate) return null;
    const bytesPorDia = ((rate * 1000) / 8) * 86400;
    return (cam.quotaMB * 1024 * 1024) / bytesPorDia;
  }

  function novo(streamName) {
    const s = cameras.streams.find((x) => x.name === streamName);
    editing = {
      id: streamName,
      name: streamName.replace(/^cam[_-]?/, '').replace(/^./, (c) => c.toUpperCase()),
      enabled: true,
      audio: 'none',
      quotaMB: 10240,
      segmentSeconds: 60,
      maxDays: 0,
      _novo: true,
      _hasAudio: s?.hasAudio ?? false,
    };
  }

  function editar(cam) {
    const s = cameras.streams.find((x) => x.name === cam.id);
    editing = { ...cam, _novo: false, _hasAudio: s?.hasAudio ?? false };
  }

  async function salvar() {
    saving = true;
    error = '';
    try {
      const { _novo, _hasAudio, ...cam } = editing;
      await api.saveCamera(cam);
      await loadCameras();
      editing = null;
    } catch (e) {
      error = e.message;
    } finally {
      saving = false;
    }
  }

  async function remover(cam) {
    if (!confirm(`Remover ${cam.name} do dwnvr?\n\nAs gravações já feitas NÃO são apagadas.`)) return;
    try {
      await api.deleteCamera(cam.id);
      await loadCameras();
    } catch (e) {
      error = e.message;
    }
  }
</script>

<div class="page">
  {#if cameras.go2rtcError}
    <p class="card warnbox small">
      Não foi possível falar com o go2rtc: {cameras.go2rtcError}<br />
      As câmeras já cadastradas continuam gravando, mas não dá para descobrir novas.
    </p>
  {/if}

  {#if error}<p class="card errbox small">{error}</p>{/if}

  <div class="list">
    {#each cameras.list as cam (cam.id)}
      {@const st = statusOf(cam.id)}
      {@const d = estimaDias(cam)}
      <div class="card cam">
        <div class="row wrap head">
          <span
            class="dot"
            class:ok={st?.connected}
            class:bad={cam.enabled && st && !st.connected}
          ></span>
          <strong>{cam.name}</strong>
          <code class="muted small">{cam.id}</code>
          <span class="spacer"></span>
          <button class="ghost small" onclick={() => editar(cam)}>editar</button>
        </div>

        <div class="row wrap chips">
          {#if !cam.enabled}<span class="chip">desabilitada</span>{/if}
          <span class="chip">{st?.videoCodec ?? '—'}</span>
          <span class="chip">{resolucao(st?.width, st?.height)}</span>
          <span class="chip">áudio: {cam.audio}</span>
          <span class="chip">{kbps(st?.bitrateKbps)}</span>
          <span class="chip">{bytes(st?.diskBytes ?? 0)} de {cam.quotaMB} MB</span>
          {#if d}<span class="chip retain">≈ {dias(d)} de retenção</span>{/if}
        </div>

        {#if st?.lastError && !st.connected}
          <p class="small err">{st.lastError}</p>
        {/if}
      </div>
    {/each}

    {#if !cameras.list.length && !cameras.loading}
      <p class="empty">Nenhuma câmera cadastrada ainda.</p>
    {/if}
  </div>

  {#if novos.length}
    <div class="card">
      <h3>Disponíveis no go2rtc</h3>
      <p class="muted small">Streams que o go2rtc já serve e que o dwnvr ainda não grava.</p>
      <div class="row wrap">
        {#each novos as s (s.name)}
          <button onclick={() => novo(s.name)}>
            + {s.name}
            {#if s.hasAudio}<span class="chip">áudio</span>{/if}
            {#if s.transcoding}<span class="chip warn">ffmpeg</span>{/if}
          </button>
        {/each}
      </div>
    </div>
  {/if}
</div>

{#if editing}
  <div
    class="overlay"
    role="presentation"
    onclick={(e) => e.target === e.currentTarget && (editing = null)}
  >
    <form class="card sheet" onsubmit={(e) => (e.preventDefault(), salvar())}>
      <h3>{editing._novo ? 'Cadastrar' : 'Editar'} {editing.id}</h3>

      <label>
        Nome
        <input bind:value={editing.name} required />
      </label>

      <label class="check">
        <input type="checkbox" bind:checked={editing.enabled} />
        Gravar esta câmera
      </label>

      <label>
        Áudio
        <select bind:value={editing.audio}>
          <option value="none">nenhum — custo zero</option>
          <option value="flac" disabled={!editing._hasAudio}>
            FLAC — ~0,6% de CPU, +260 kbps de disco
          </option>
          <option value="aac" disabled={!editing._hasAudio}>
            AAC — ~10% de CPU, +64 kbps (exige fonte ffmpeg no go2rtc)
          </option>
        </select>
        {#if !editing._hasAudio}
          <small class="muted">
            Esta câmera não entrega trilha de áudio. Remova o <code>#media=video</code>
            da fonte no go2rtc.yaml para habilitá-la.
          </small>
        {/if}
      </label>

      <label>
        Cota em disco (MB)
        <input type="number" bind:value={editing.quotaMB} min="100" step="100" required />
        {#if estimaDias(editing)}
          <small class="muted">≈ {dias(estimaDias(editing))} na taxa medida agora</small>
        {:else}
          <small class="muted">a estimativa aparece após a primeira medição de taxa</small>
        {/if}
      </label>

      <label>
        Duração do segmento (s)
        <input type="number" bind:value={editing.segmentSeconds} min="10" max="600" step="10" />
        <small class="muted">o corte real espera o próximo keyframe</small>
      </label>

      <div class="row">
        {#if !editing._novo}
          <button
            type="button"
            class="danger"
            onclick={() => {
              const c = editing;
              editing = null;
              remover(c);
            }}>remover</button
          >
        {/if}
        <span class="spacer"></span>
        <button type="button" class="ghost" onclick={() => (editing = null)}>cancelar</button>
        <button class="primary" type="submit" disabled={saving}>
          {saving ? 'salvando…' : 'salvar'}
        </button>
      </div>
    </form>
  </div>
{/if}

<style>
  .page {
    display: grid;
    gap: 10px;
    padding: 10px;
    max-width: 900px;
    margin: 0 auto;
  }

  .list { display: grid; gap: 8px; }
  .cam { display: grid; gap: 8px; }
  .head { gap: 8px; }
  .chips { gap: 6px; }
  .chip.retain { border-color: #1f6feb66; color: var(--accent); }
  .chip.warn { color: var(--warn); }
  .err { color: var(--bad); margin: 0; }

  .warnbox { border-color: #6b5a1f; color: var(--warn); }
  .errbox { border-color: #5c2b2b; color: var(--bad); }

  h3 { margin: 0 0 4px; font-size: 15px; }

  .overlay {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.6);
    display: grid;
    /* No celular a folha sobe de baixo, onde o polegar alcança. */
    align-items: end;
    z-index: 30;
  }

  .sheet {
    display: grid;
    gap: 14px;
    width: 100%;
    max-height: 92dvh;
    overflow-y: auto;
    border-radius: var(--radius) var(--radius) 0 0;
    padding-bottom: calc(14px + env(safe-area-inset-bottom));
  }

  @media (min-width: 640px) {
    .overlay { place-items: center; padding: 20px; }
    .sheet { max-width: 460px; border-radius: var(--radius); }
  }

  label { display: grid; gap: 5px; font-size: 13px; color: var(--dim); }
  label input:not([type='checkbox']),
  label select { width: 100%; color: var(--fg); font-size: 15px; }
  label.check { display: flex; align-items: center; gap: 9px; color: var(--fg); font-size: 15px; }
  label.check input { width: 18px; height: 18px; min-height: 0; accent-color: var(--accent); }
  small { font-size: 12px; }
</style>
