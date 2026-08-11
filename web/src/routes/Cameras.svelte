<script>
  import { onMount, onDestroy } from 'svelte';
  import { cameras, loadCameras, loadHealth, health, pollHealth } from '../lib/state.svelte.js';
  import { api } from '../lib/api.js';
  import { dias, kbps, bytes, bytesDeMB, resolucao, ddmm } from '../lib/format.js';
  import Modal from '../components/Modal.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';

  let editing = $state(null); // cópia da câmera em edição, ou null
  let removendo = $state(null); // câmera aguardando confirmação de remoção
  let apagando = $state(null); // gravações aguardando confirmação de apagamento
  let apagarGravacoes = $state(false); // checkbox do diálogo de remoção
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

  // O tamanho vem da medição do diagnóstico, que é a mesma fonte do chip de
  // disco do card: dizer "apagar 3,2 GB" só ajuda se o número for o que a
  // pessoa já estava vendo.
  function tamanhoDe(cam) {
    return statusOf(cam.id)?.diskBytes ?? 0;
  }

  // Um alvo normalizado para que um único diálogo sirva os dois casos: a câmera
  // cadastrada, que tem nome e status, e a órfã, que só tem o que a varredura do
  // disco descobriu.
  const alvoDaCamera = (cam) => ({
    id: cam.id,
    nome: cam.name,
    bytes: tamanhoDe(cam),
    orfa: false,
  });

  const alvoDaOrfa = (o) => ({ id: o.id, nome: o.id, bytes: o.bytes, orfa: true });

  function abrirRemocao(cam) {
    // Sempre desmarcado ao abrir: preservar as gravações é o padrão, e herdar a
    // marcação de uma remoção anterior apagaria vídeo sem ninguém pedir.
    apagarGravacoes = false;
    removendo = cam;
  }

  async function confirmarRemocao() {
    const cam = removendo;
    const comGravacoes = apagarGravacoes;
    removendo = null;
    error = '';
    try {
      await api.deleteCamera(cam.id, { recordings: comGravacoes });
      await loadCameras();
      await loadHealth();
    } catch (e) {
      error = e.message;
    }
  }

  async function confirmarApagamento() {
    const alvo = apagando;
    apagando = null;
    error = '';
    try {
      await api.deleteRecordings(alvo.id);
      // As duas: loadCameras refaz a lista de órfãs, loadHealth refaz o chip de
      // disco. Sem a segunda, o card seguiria mostrando os bytes que já foram.
      await loadCameras();
      await loadHealth();
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
          <!-- Os três num grupo próprio para quebrarem juntos: soltos na linha,
               em tela de celular o "remover" descia sozinho para a esquerda,
               longe dos outros dois e do canto onde o polegar espera achá-lo. -->
          <div class="row acoes">
            <button class="ghost small" onclick={() => editar(cam)}>editar</button>
            <!-- Rótulo por extenso: só "gravações" se leria como "ver as gravações". -->
            <button class="ghost small danger" onclick={() => (apagando = alvoDaCamera(cam))}>
              apagar gravações
            </button>
            <button class="ghost small danger" onclick={() => abrirRemocao(cam)}>remover</button>
          </div>
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

        <!-- Onde os arquivos estão de verdade. Toda vez que a pergunta sai do
             navegador — fazer backup, olhar o disco por ssh, entender de onde
             vieram os bytes do chip acima — é este caminho que se procura, e
             ele não estava escrito em lugar nenhum da interface. -->
        {#if cam.dir}
          <code class="caminho muted small" title="pasta das gravações desta câmera">{cam.dir}</code>
        {/if}

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

  <!-- Só aparece quando há algo: uma seção vazia permanente sugeriria que sobrar
       gravação órfã é o estado normal, e não é. -->
  {#if cameras.orphans.length}
    <div class="card orfas">
      <h3>Gravações sem câmera</h3>
      <p class="muted small">
        Material de câmeras que já foram removidas. Ele não conta na cota de ninguém, não abre
        na tela de Gravações e a retenção não o alcança — só sai do disco por aqui.
      </p>

      {#each cameras.orphans as o (o.id)}
        <!-- A linha não quebra; quem quebra é o bloco de dados. Assim o botão
             fica sempre no mesmo canto, em vez de descer para a esquerda quando
             os chips não cabem. -->
        <div class="row orfa">
          <div class="row wrap dados">
            <code>{o.id}</code>
            {#if o.days}
              <span class="chip">{bytes(o.bytes)}</span>
              <span class="chip">
                {ddmm(o.firstMs)} a {ddmm(o.lastMs)} · {o.days} {o.days > 1 ? 'dias' : 'dia'}
              </span>
            {:else}
              <!-- Diretório sem índice: sobra de uma evicção ou de uma cópia à
                   mão. Não dá para dizer o tamanho, mas esconder seria pior. -->
              <span class="chip warn">sem índice</span>
            {/if}
          </div>
          <button class="ghost small danger" onclick={() => (apagando = alvoDaOrfa(o))}>
            apagar
          </button>
        </div>
      {/each}
    </div>
  {/if}
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
              abrirRemocao(editing);
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
  {@const tam = tamanhoDe(removendo)}
  <ConfirmDialog
    title="Remover {removendo.name}?"
    confirmLabel={apagarGravacoes ? `remover e apagar ${bytes(tam)}` : 'remover'}
    danger
    onconfirm={confirmarRemocao}
    oncancel={() => (removendo = null)}
  >
    A câmera sai do dwnvr e para de gravar agora.

    <!-- Desmarcado por padrão: apagar horas de vídeo não pode ser efeito
         colateral de um clique em "remover". O rótulo do botão acompanha a
         marcação para que a consequência esteja escrita no que se clica. -->
    <label class="check apagar">
      <input type="checkbox" bind:checked={apagarGravacoes} />
      apagar também as gravações ({bytes(tam)})
    </label>

    {#if !apagarGravacoes}
      Sem marcar, os arquivos ficam em disco e passam a aparecer aqui em
      <strong>Gravações sem câmera</strong>, de onde dá para apagá-los depois.
    {/if}
  </ConfirmDialog>
{/if}

{#if apagando}
  <ConfirmDialog
    title="Apagar as gravações de {apagando.nome}?"
    confirmLabel={apagando.bytes ? `apagar ${bytes(apagando.bytes)}` : 'apagar'}
    danger
    onconfirm={confirmarApagamento}
    oncancel={() => (apagando = null)}
  >
    {#if apagando.orfa}
      Tudo o que sobrou de <code>{apagando.id}</code> sai do disco, e não há como desfazer.
    {:else}
      Tudo o que essa câmera gravou sai do disco, e não há como desfazer. A câmera continua
      cadastrada e volta a gravar em seguida — a gravação fica interrompida por alguns segundos.
    {/if}
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
  .acoes { gap: 8px; margin-left: auto; }
  .chips { gap: 6px; }
  /* Caminho longo não tem espaço para quebrar: sem isto ele estoura a largura
     do card no celular. */
  .caminho { overflow-wrap: anywhere; }
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

  .orfas { display: grid; gap: 8px; }
  .orfas p { margin: -4px 0 2px; }
  .orfa { gap: 10px; align-items: start; }
  .orfa .dados { gap: 6px; flex: 1; min-width: 0; }
  .orfa code { color: var(--fg); }
  .orfa button { flex: none; }

  /* O checkbox dentro do ConfirmDialog: sem a margem ele encosta no parágrafo
     acima e deixa de parecer uma escolha à parte. */
  label.check.apagar { margin: 10px 0 6px; font-size: 14px; }

  /* O Modal só entrega a moldura; o espaçamento entre os campos é do formulário. */
  .fields { display: grid; gap: 14px; }

  label { display: grid; gap: 5px; font-size: 13px; color: var(--dim); }
  label input:not([type='checkbox']),
  label select { width: 100%; color: var(--fg); font-size: 15px; }
  label.check { display: flex; align-items: center; gap: 9px; color: var(--fg); font-size: 15px; }
  label.check input { width: 18px; height: 18px; min-height: 0; accent-color: var(--accent); }
  small { font-size: 12px; }
</style>
