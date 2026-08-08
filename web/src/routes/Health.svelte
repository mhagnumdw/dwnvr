<script>
  import { onDestroy } from 'svelte';
  import { health, pollHealth, cameras } from '../lib/state.svelte.js';
  import { api } from '../lib/api.js';
  import { bytes, kbps, dias, duracao } from '../lib/format.js';

  const stop = pollHealth(3000);
  onDestroy(stop);

  const disk = $derived(health.disk);
  const usadoPeloDwnvr = $derived(disk ? disk.dwnvrBytes / disk.totalBytes : 0);
  const usadoPorOutros = $derived(
    disk ? (disk.totalBytes - disk.freeBytes - disk.dwnvrBytes) / disk.totalBytes : 0,
  );

  const totalKbps = $derived(health.cameras.reduce((a, c) => a + (c.bitrateKbps || 0), 0));
  const gbPorDia = $derived(((totalKbps * 1000) / 8) * 86400 / 1024 ** 3);
  const desconectadas = $derived(health.cameras.filter((c) => c.enabled && !c.connected));

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
    for (const c of desconectadas) {
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
        <span>mínimo livre: {disk.minFreeMB} MB</span>
      </div>
    </div>
  {/if}

  <div class="card row wrap totais">
    <div><span class="big mono">{health.cameras.filter((c) => c.connected).length}</span><br /><span class="muted small">conectadas</span></div>
    <div><span class="big mono">{kbps(totalKbps)}</span><br /><span class="muted small">taxa somada</span></div>
    <div><span class="big mono">{gbPorDia.toFixed(1)} GB</span><br /><span class="muted small">por dia</span></div>
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
      <span class="c-nome">câmera</span>
      <span>taxa</span>
      <span>disco</span>
      <span>retenção</span>
      <span>reconex.</span>
    </div>
    {#each health.cameras as c (c.id)}
      <div class="trow row">
        <span class="c-nome row">
          <span class="dot" class:ok={c.connected} class:bad={c.enabled && !c.connected}></span>
          <span class="nome">{c.name}</span>
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

  <p class="muted small">
    Atualizado a cada 3s{#if health.updatedAt} · última leitura há {duracao(Date.now() - health.updatedAt)}{/if}
  </p>
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
  .trow + .trow { border-top: 1px solid #21262d; }
  .c-nome { gap: 7px; min-width: 0; }
  .nome { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
