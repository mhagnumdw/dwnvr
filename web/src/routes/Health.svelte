<script>
  import { onDestroy } from 'svelte';
  import { health, pollHealth, cameras, build } from '../lib/state.svelte.js';
  import { api } from '../lib/api.js';
  import { bytes, bytesDeMB, kbps, dias, duracao, hhmmss } from '../lib/format.js';

  const stop = pollHealth(3000);
  onDestroy(stop);

  const disk = $derived(health.disk);
  const usadoPeloDwnvr = $derived(disk ? disk.dwnvrBytes / disk.totalBytes : 0);
  const usadoPorOutros = $derived(
    disk ? (disk.totalBytes - disk.freeBytes - disk.dwnvrBytes) / disk.totalBytes : 0,
  );

  // Cada coluna sabe extrair o valor que a ordena; assim o cabeçalho e a
  // ordenação não podem discordar sobre o que "disco" significa.
  const colunas = [
    { id: 'name', rotulo: 'câmera', valor: (c) => c.name || '', texto: true },
    { id: 'bitrate', rotulo: 'taxa', valor: (c) => c.bitrateKbps || 0 },
    { id: 'disco', rotulo: 'disco', valor: (c) => c.diskBytes || 0 },
    { id: 'retencao', rotulo: 'retenção', valor: (c) => c.retainDays || 0 },
    { id: 'reconex', rotulo: 'reconex.', valor: (c) => c.reconnects || 0 },
  ];

  // numeric para "cam2" vir antes de "cam10", que é como as câmeras costumam
  // ser nomeadas aqui.
  const colator = new Intl.Collator('pt-BR', { numeric: true, sensitivity: 'base' });

  let ordem = $state({ col: 'name', asc: true });

  function ordenar(id) {
    if (ordem.col === id) ordem.asc = !ordem.asc;
    else ordem = { col: id, asc: true };
  }

  const linhas = $derived.by(() => {
    const col = colunas.find((c) => c.id === ordem.col) ?? colunas[0];
    const dir = ordem.asc ? 1 : -1;
    // Cópia: health.cameras é estado global e a tela só quer uma visão dele.
    return [...health.cameras].sort((a, b) => {
      const va = col.valor(a);
      const vb = col.valor(b);
      const d = col.texto ? colator.compare(va, vb) : va - vb;
      // Empate cai no nome para a tabela não dançar a cada leitura de 3s.
      return d ? d * dir : colator.compare(a.name || '', b.name || '');
    });
  });

  // A data vem em ISO/UTC do servidor; aqui interessa a hora local de quem lê.
  // Builds antigos podem não trazê-la, e um "Invalid Date" na tela seria pior
  // que não mostrar nada.
  const compiladoEm = $derived.by(() => {
    const d = new Date(build.date);
    return build.date && !isNaN(d) ? d.toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' }) : '';
  });

  const totalKbps = $derived(health.cameras.reduce((a, c) => a + (c.bitrateKbps || 0), 0));
  const bytesPorDia = $derived(((totalKbps * 1000) / 8) * 86400);
  const desconectadas = $derived(health.cameras.filter((c) => c.enabled && !c.connected));
  const paradas = $derived(health.cameras.filter((c) => c.enabled && c.silent));

  // Avisos que explicam problemas antes de eles virarem mistério — que foi
  // exatamente o que faltou nos NVRs anteriores.
  const avisos = $derived.by(() => {
    const out = [];
    if (disk?.belowMin) {
      out.push({
        nivel: 'bad',
        texto: `Disco abaixo do mínimo livre (${bytes(disk.freeBytes)}). A retenção está apagando gravações antigas de todas as câmeras.`,
      });
    }
    // "Parada" vem antes de "desconectada" porque é a pergunta que importa: uma
    // conexão de pé que não produz segmento nenhum continua sendo gravação
    // perdida, e foi assim que 9 câmeras passaram horas fora sem ninguém notar.
    for (const c of paradas) {
      const desde = c.lastSegmentAt
        ? `desde ${hhmmss(new Date(c.lastSegmentAt).getTime())} (${duracao(Date.now() - new Date(c.lastSegmentAt).getTime())})`
        : 'e não gravou nada desde que o dwnvr subiu';
      out.push({
        nivel: 'bad',
        texto: `${c.name} NÃO ESTÁ GRAVANDO ${desde}${c.lastError ? `: ${c.lastError}` : ''}`,
      });
    }
    for (const c of desconectadas) {
      if (c.silent) continue; // já avisado acima, e com mais informação
      out.push({ nivel: 'bad', texto: `${c.name} desconectada: ${c.lastError || 'motivo desconhecido'}` });
    }
    for (const s of cameras.streams) {
      if (s.registered && s.transcoding) {
        out.push({
          nivel: 'warn',
          texto: `${s.name} usa uma fonte ffmpeg no go2rtc, ou seja, há transcodificação consumindo CPU.`,
        });
      }
    }
    for (const c of health.cameras) {
      const cfg = cameras.list.find((x) => x.id === c.id);
      if (cfg && cfg.audio !== 'none' && !c.hasAudio) {
        out.push({
          nivel: 'warn',
          texto: `${c.name} está configurada com áudio ${cfg.audio}, mas o stream não entrega trilha de áudio.`,
        });
      }
      if (c.reconnects > 10) {
        out.push({ nivel: 'warn', texto: `${c.name} já reconectou ${c.reconnects} vezes.` });
      }
    }
    if (cameras.go2rtcError) {
      out.push({ nivel: 'bad', texto: `go2rtc inacessível: ${cameras.go2rtcError}` });
    }
    return out;
  });
</script>

<div class="page">
  {#if disk}
    <div class="card">
      <div class="row wrap">
        <strong>Disco</strong>
        <span class="spacer"></span>
        <span class="muted small mono">{bytes(disk.freeBytes)} livres de {bytes(disk.totalBytes)}</span>
      </div>

      <div class="meter" title="azul: gravações do dwnvr; cinza: outros dados">
        <span class="seg dwnvr" style:width="{usadoPeloDwnvr * 100}%"></span>
        <span class="seg outros" style:width="{usadoPorOutros * 100}%"></span>
      </div>

      <div class="row wrap small muted legend">
        <span><i class="dwnvr"></i> dwnvr: {bytes(disk.dwnvrBytes)}</span>
        <span><i class="outros"></i> outros: {bytes(disk.totalBytes - disk.freeBytes - disk.dwnvrBytes)}</span>
        <span class="spacer"></span>
        <span>mínimo livre: {bytesDeMB(disk.minFreeMB)}</span>
      </div>
    </div>
  {/if}

  <div class="card row wrap totais">
    <div><span class="big mono">{health.cameras.filter((c) => c.connected).length}</span><br /><span class="muted small">conectadas</span></div>
    <div><span class="big mono">{kbps(totalKbps)}</span><br /><span class="muted small">taxa somada</span></div>
    <div><span class="big mono">{bytes(bytesPorDia)}</span><br /><span class="muted small">por dia</span></div>
  </div>

  {#if avisos.length}
    <div class="card avisos">
      {#each avisos as a}
        <p class="row {a.nivel}"><span class="dot {a.nivel}"></span>{a.texto}</p>
      {/each}
    </div>
  {:else if health.updatedAt}
    <p class="card ok row"><span class="dot ok"></span>Nenhum problema detectado.</p>
  {/if}

  <div class="table card">
    <div class="thead row small muted">
      {#each colunas as col (col.id)}
        <button
          class="th"
          class:ativa={ordem.col === col.id}
          aria-label="ordenar por {col.rotulo}"
          onclick={() => ordenar(col.id)}
        >
          <span class="rotulo">{col.rotulo}</span>
          <!-- A seta ocupa lugar sempre, senão o cabeçalho pula a cada clique. -->
          <span class="seta">{ordem.col === col.id ? (ordem.asc ? '▲' : '▼') : ''}</span>
        </button>
      {/each}
    </div>
    {#each linhas as c (c.id)}
      <div class="trow row">
        <span class="c-nome row">
          <span class="dot" class:ok={c.connected && !c.silent} class:bad={c.enabled && (c.silent || !c.connected)}></span>
          <span class="nome">{c.name}</span>
          <!-- Conectada mas sem gravar é o estado traiçoeiro: o ponto verde
               dizia "tudo bem" enquanto a câmera não produzia nada. -->
          {#if c.silent}<span class="chip parada">não grava</span>
          {:else if c.lastSegmentAt}<span class="chip">{hhmmss(new Date(c.lastSegmentAt).getTime())}</span>{/if}
          {#if c.hasAudio}<span class="chip">áudio</span>{/if}
        </span>
        <span class="mono">{kbps(c.bitrateKbps)}</span>
        <span class="mono">{bytes(c.diskBytes)}</span>
        <span class="mono">{dias(c.retainDays)}</span>
        <span class="mono">{c.reconnects}</span>
      </div>
    {/each}
    {#if !health.cameras.length}
      <p class="empty">sem dados ainda</p>
    {/if}
  </div>

  <!-- O separador vai como expressão porque o Svelte apara o espaço no início
       de um bloco {#if}, e sem isso sai "3s· última leitura". -->
  <p class="muted small">
    Atualizado a cada 3s{#if health.updatedAt}{' · '}última leitura há {duracao(Date.now() - health.updatedAt)}{/if}
  </p>

  <!-- Único lugar onde a versão aparece no celular: o header com a marca só
       existe a partir de 720px. -->
  {#if build.version}
    <p class="muted small">
      dwnvr <span class="mono">{build.version}</span>{#if compiladoEm}{' · '}compilado em {compiladoEm}{/if}
    </p>
  {/if}
</div>

<style>
  .page {
    display: grid;
    gap: 10px;
    padding: 10px;
    max-width: 900px;
    margin: 0 auto;
  }

  .card { display: grid; gap: 10px; }

  .meter {
    display: flex;
    height: 12px;
    border-radius: 999px;
    overflow: hidden;
    background: #1b1f24;
  }
  .seg.dwnvr { background: var(--accent); }
  .seg.outros { background: #3d444d; }

  .legend { gap: 14px; }
  .legend i { display: inline-block; width: 9px; height: 9px; border-radius: 2px; vertical-align: -1px; }
  .legend i.dwnvr { background: var(--accent); }
  .legend i.outros { background: #3d444d; }

  .totais { justify-content: space-around; text-align: center; gap: 18px; }
  .big { font-size: 22px; font-weight: 600; }

  .avisos p { margin: 0; gap: 8px; align-items: flex-start; font-size: 13px; }
  .avisos p.bad { color: var(--bad); }
  .chip.parada { color: var(--bad); border-color: #5c2b2b; }
  .avisos p.warn { color: var(--warn); }
  .avisos .dot { margin-top: 6px; }
  .card.ok { color: var(--ok); font-size: 13px; gap: 8px; }

  .table { padding: 0; overflow-x: auto; }
  .thead, .trow {
    display: grid;
    grid-template-columns: minmax(130px, 2fr) repeat(4, minmax(70px, 1fr));
    gap: 8px;
    padding: 9px 12px;
    align-items: center;
  }
  .thead { border-bottom: 1px solid var(--line); }
  /* O cabeçalho é botão para funcionar com teclado, mas continua parecendo
     cabeçalho: o estilo global de button não serve aqui. */
  .th {
    display: flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
    min-height: 0;
    padding: 0;
    background: none;
    border: none;
    border-radius: 4px;
    color: inherit;
    font: inherit;
    text-align: left;
  }
  .th:hover:not(:disabled) { border-color: transparent; color: var(--fg); }
  .th.ativa { color: var(--fg); }
  .th .rotulo { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .th .seta { width: 9px; flex: none; font-size: 9px; color: var(--accent); }
  .trow + .trow { border-top: 1px solid #21262d; }
  .c-nome { gap: 7px; min-width: 0; }
  .nome { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
