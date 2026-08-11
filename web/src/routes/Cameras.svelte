<script>
  import { onMount, onDestroy } from 'svelte';
  import { cameras, loadCameras, health, pollHealth } from '../lib/state.svelte.js';
  import { api } from '../lib/api.js';
  import { dias, kbps, bytes, bytesDeMB, resolucao } from '../lib/format.js';
  import Modal from '../components/Modal.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';

  let editing = $state(null); // cópia da câmera em edição, ou null
  let removendo = $state(null); // câmera aguardando confirmação de remoção
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

  async function confirmarRemocao() {
    const cam = removendo;
    removendo = null;
    error = '';
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
          <button class="ghost small danger" onclick={() => (removendo = cam)}>remover</button>
        </div>

        <div class="row wrap chips">
          {#if !cam.enabled}<span class="chip">desabilitada</span>{/if}
          <span class="chip">{st?.videoCodec ?? '—'}</span>
          <span class="chip">{resolucao(st?.width, st?.height)}</span>
          <span class="chip">áudio: {cam.audio}</span>
          <span class="chip">{kbps(st?.bitrateKbps)}</span>
          <span class="chip">{bytes(st?.diskBytes ?? 0)} de {bytesDeMB(cam.quotaMB)}</span>
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

  <!-- Sempre visível, mesmo vazio: enquanto este bloco só aparecia quando havia
       stream novo, quem chegava na tela com tudo já cadastrado não encontrava o
       botão de adicionar e concluía que ele não existia. -->
  <div class="card ajuda">
    <h3>Disponíveis no go2rtc</h3>

    {#if novos.length}
      <div class="row wrap">
        {#each novos as s (s.name)}
          <button title="Clique para adicionar" onclick={() => novo(s.name)}>
            + {s.name}
            {#if s.hasAudio}<span class="chip">áudio</span>{/if}
            {#if s.transcoding}<span class="chip warn">ffmpeg</span>{/if}
          </button>
        {/each}
      </div>
    {:else if cameras.go2rtcError}
      <p class="muted small vazio">Não dá para listar os streams enquanto o go2rtc não responder.</p>
    {:else}
      <p class="muted small vazio">Nenhum stream novo — o dwnvr já grava todos os que o go2rtc serve.</p>
    {/if}

    <!-- Depois da lista: quem já entendeu como funciona quer o botão primeiro, e
         quem não entendeu lê a explicação ao lado do que ela explica. -->
    <p class="muted small explica">
      Câmera não se cadastra aqui do zero: o dwnvr grava o que o go2rtc entrega e não guarda
      endereço nem senha de câmera. Declare o stream no <code>go2rtc.yaml</code> e ele aparece
      nesta lista — um clique escolhe cota, áudio e retenção, e a gravação começa.
    </p>
  </div>
</div>

{#if editing}
  <Modal onclose={() => (editing = null)}>
    <form class="fields" onsubmit={(e) => (e.preventDefault(), salvar())}>
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
        <!-- O campo é em MB porque é assim que a cota é guardada, mas quem digita
             20480 quer saber que isso são 20 GB. -->
        <small class="muted">
          = {bytesDeMB(editing.quotaMB)} ·
          {#if estimaDias(editing)}
            ≈ {dias(estimaDias(editing))} na taxa medida agora
          {:else}
            a estimativa aparece após a primeira medição de taxa
          {/if}
        </small>
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
              removendo = editing;
              editing = null;
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
  </Modal>
{/if}

{#if removendo}
  <ConfirmDialog
    title="Remover {removendo.name}?"
    confirmLabel="remover"
    danger
    onconfirm={confirmarRemocao}
    oncancel={() => (removendo = null)}
  >
    A câmera sai do dwnvr e para de gravar agora. As gravações já feitas
    <strong>não</strong> são apagadas — elas continuam em disco até você apagar o diretório
    <code>{removendo.id}</code> na mão.
  </ConfirmDialog>
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

  .ajuda h3 { margin: 0 0 10px; }
  .ajuda .vazio { margin: 0; }
  .ajuda .explica { margin: 12px 0 0; }
  .ajuda code { color: var(--fg); }

  /* O Modal só entrega a moldura; o espaçamento entre os campos é do formulário. */
  .fields { display: grid; gap: 14px; }

  label { display: grid; gap: 5px; font-size: 13px; color: var(--dim); }
  label input:not([type='checkbox']),
  label select { width: 100%; color: var(--fg); font-size: 15px; }
  label.check { display: flex; align-items: center; gap: 9px; color: var(--fg); font-size: 15px; }
  label.check input { width: 18px; height: 18px; min-height: 0; accent-color: var(--accent); }
  small { font-size: 12px; }
</style>
