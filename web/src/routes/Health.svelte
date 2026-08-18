<script>
  import { onDestroy } from 'svelte';
  import { health, pollHealth, HEALTH_POLL_MS, cameras, build } from '../lib/state.svelte.js';
  import { api } from '../lib/api.js';
  import {
    bytes,
    bytesDeMB,
    kbps,
    dias,
    duracao,
    hhmmss,
    ddmm,
    relogioDeFuso,
  } from '../lib/format.js';
  import { AJUDA_RETIDO, AJUDA_CABEM } from '../lib/ajudas.js';

  const stop = pollHealth();
  onDestroy(stop);

  const disk = $derived(health.disk);

  // Os segundos andam de HEALTH_POLL_MS em HEALTH_POLL_MS, junto com a leitura,
  // e não de um em um: contar o tempo aqui só para preencher o intervalo daria
  // um relógio movido pelo navegador, que é o relógio que este campo existe
  // para NÃO mostrar. O que aparece é sempre um instante que o servidor disse.
  //
  // A abreviação ("-03") não identifica região nenhuma - vale para São Paulo,
  // Buenos Aires e mais um punhado de lugares -, então o nome IANA é o que vai
  // na tela quando o servidor consegue descobri-lo, e a sigla fica no title.
  // Servidor sem /etc/timezone manda só a sigla, e aí ela assume o lugar.
  const relogio = $derived.by(() => {
    const c = health.clock;
    if (!c) return null;
    const lido = Date.parse(c.now);
    if (isNaN(lido)) return null;
    return {
      quando: relogioDeFuso(lido, c.offsetSeconds),
      fuso: c.zone || c.abbr,
      sigla: c.zone ? c.abbr : undefined,
    };
  });
  const usadoPeloDwnvr = $derived(disk ? disk.dwnvrBytes / disk.totalBytes : 0);
  const usadoPorOutros = $derived(
    disk ? (disk.totalBytes - disk.freeBytes - disk.dwnvrBytes) / disk.totalBytes : 0,
  );

  // retidoMs é a retenção real: do segmento mais antigo em disco até agora.
  //
  // O servidor manda o instante, e não os dias já contados, porque com ele a
  // mesma resposta serve à tabela ("12 dias 4h") e ao card de câmeras ("desde
  // 31/07"). Câmera que nunca gravou não traz o campo, e o zero vira "-" no
  // duracao() em vez de "0s".
  function retidoMs(c) {
    return c.oldestSegmentAt ? Date.now() - new Date(c.oldestSegmentAt).getTime() : 0;
  }

  // O cabeçalho explica O QUE a coluna mede, uma vez só; a célula responde
  // DESDE QUANDO, que é por câmera. undefined e não '' porque o Svelte omite o
  // atributo inteiro assim, e um title vazio deixaria a célula com sublinhado
  // de dica sem dica nenhuma.
  function desdeTitulo(c) {
    return c.oldestSegmentAt ? `Desde ${ddmm(new Date(c.oldestSegmentAt).getTime())}` : undefined;
  }

  // Cada coluna sabe extrair o valor que a ordena; assim o cabeçalho e a
  // ordenação não podem discordar sobre o que "disco" significa.
  //
  // A ajuda fica aqui junto, e não solta no template, porque é a mesma coisa: o
  // rótulo cabe em uma palavra e nenhuma delas ("retenção", "reconex.") diz
  // sozinha o que está sendo medido.
  const colunas = [
    {
      id: 'name',
      rotulo: 'câmera',
      valor: (c) => c.name || '',
      texto: true,
      ajuda:
        'Nome da câmera. O ponto mostra o estado: verde gravando, vermelho parada ou desconectada. O horário ao lado é o do último segmento gravado.',
    },
    {
      id: 'bitrate',
      rotulo: 'taxa',
      valor: (c) => c.bitrateKbps || 0,
      ajuda: 'Taxa de bits que está chegando da câmera agora, medida no fluxo gravado.',
    },
    {
      id: 'disco',
      rotulo: 'disco',
      valor: (c) => c.diskBytes || 0,
      ajuda:
        'Espaço que as gravações desta câmera ocupam hoje, somando todos os dias em disco, e a cota configurada para ela.',
    },
    {
      id: 'retido',
      rotulo: 'retido',
      valor: (c) => retidoMs(c),
      ajuda: AJUDA_RETIDO,
    },
    {
      id: 'cabem',
      rotulo: 'cabem',
      valor: (c) => c.retainDays || 0,
      ajuda: AJUDA_CABEM,
    },
    {
      id: 'reconex',
      rotulo: 'reconex.',
      valor: (c) => c.reconnects || 0,
      ajuda:
        'Quantas vezes o dwnvr precisou reabrir a conexão desde que subiu. Número alto indica enlace instável com a câmera.',
    },
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

  // Avisos que explicam problemas antes de eles virarem mistério - que foi
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
  <!-- Os dois uptimes ficam lado a lado de propósito: é a comparação entre
       eles que diagnostica. Iguais, nada reiniciou; só o do dwnvr curto, foi
       deploy ou queda do processo; os dois curtos, a máquina reiniciou - e aí
       "reconex. 0" e "não gravou nada desde que o dwnvr subiu", logo abaixo,
       deixam de ser mistério.

       Some inteira contra servidor antigo, que não responde o campo. -->
  {#if health.uptime}
    <div class="card row wrap statusbar small">
      <span class="dot ok"></span>
      <span class="muted">dwnvr no ar há</span>
      <span class="mono">{duracao(health.uptime.appSeconds * 1000)}</span>
      {#if health.uptime.machineSeconds}
        <span class="muted">·</span>
        <span class="muted">máquina há</span>
        <span class="mono">{duracao(health.uptime.machineSeconds * 1000)}</span>
      {/if}
      <!-- Horário do servidor no fuso do servidor, e não no do navegador: é
           assim que se descobre relógio ou TZ errado na máquina que grava, que
           é a que carimba o nome dos segmentos. -->
      {#if relogio}
        <span class="muted">·</span>
        <span class="muted">horário do servidor</span>
        <span class="mono" title={relogio.sigla}>{relogio.quando} {relogio.fuso}</span>
      {/if}
    </div>
  {/if}

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
        <!-- O title fica no botão inteiro, e não só no rótulo: a área de
             passagem do mouse é a célula do cabeçalho, que é onde a pessoa
             para para decidir se clica. -->
        <button
          class="th"
          class:ativa={ordem.col === col.id}
          aria-label="ordenar por {col.rotulo}"
          title={col.ajuda}
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
        <!-- Uso e cota juntos, como no chip da tela de Câmeras: o número
             sozinho não diz se é muito ou pouco para esta câmera. -->
        <span class="mono">{bytes(c.diskBytes)} de {bytesDeMB(c.quotaMB)}</span>
        <span class="mono" title={desdeTitulo(c)}>{duracao(retidoMs(c))}</span>
        <span class="mono">{dias(c.retainDays)}</span>
        <span class="mono">{c.reconnects}</span>
      </div>
    {/each}
    {#if !health.cameras.length}
      <p class="empty">sem dados ainda</p>
    {/if}
  </div>

  <!-- O separador vai como expressão porque o Svelte apara o espaço no início
       de um bloco {#if}, e sem isso sai "5s· última leitura". -->
  <p class="muted small">
    Atualizado a cada {HEALTH_POLL_MS / 1000}s{#if health.updatedAt}{' · '}última leitura há {duracao(Date.now() - health.updatedAt)}{/if}
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

  /* O :not(.row) importa: esta regra é escopada, e escopo no Svelte acrescenta
     uma classe ao seletor - ou seja, ela ganha do .row global por
     especificidade. Sem a ressalva, todo card que se declara linha (os totais,
     o "nenhum problema", a faixa de estado) virava coluna calado. */
  .card:not(.row) { display: grid; gap: 10px; }

  /* Mais baixa que os outros cards: é contexto de leitura rápida, não deve
     disputar espaço com o disco logo abaixo. */
  .statusbar { padding: 8px 12px; }

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
    /* Colunas: nome, taxa, disco, retido, cabem, reconex. O disco tem faixa
       própria porque carrega duas grandezas ("12,91 GB de 20 GB") e no repeat
       uniforme ele quebrava em duas linhas antes das outras precisarem. */
    grid-template-columns:
      minmax(130px, 2fr) minmax(70px, 1fr) minmax(125px, 1.6fr)
      repeat(2, minmax(80px, 1.1fr)) minmax(60px, 0.8fr);
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
  /* O sublinhado pontilhado é a marca de que há explicação ali: sem ele o
     title existe e ninguém descobre que basta parar o mouse. Fica na cor da
     linha para insinuar, não para competir com o rótulo. */
  .th .rotulo {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-decoration: underline dotted var(--line);
    text-underline-offset: 3px;
  }
  .th:hover .rotulo, .th.ativa .rotulo { text-decoration-color: var(--dim); }
  .th .seta { width: 9px; flex: none; font-size: 9px; color: var(--accent); }
  .trow + .trow { border-top: 1px solid #21262d; }
  .c-nome { gap: 7px; min-width: 0; }
  .nome { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
