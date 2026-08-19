<script>
  import { onMount, onDestroy } from 'svelte';
  import { cameras, loadCameras } from '../lib/state.svelte.js';
  import { api, mediaURL } from '../lib/api.js';
  import { Player } from '../lib/player.svelte.js';
  import { clearThumbnails } from '../lib/thumbs.js';
  import { baixarQuadro } from '../lib/captura.js';
  import { dayKey, parseDay, hhmmss, duracao } from '../lib/format.js';
  import Timeline from '../components/Timeline.svelte';
  import ThumbStrip from '../components/ThumbStrip.svelte';

  const player = new Player();

  // Piso e teto do ritmo com que o dia de hoje é relido. Consultar mais rápido
  // que o segmento da câmera é rebaixar a mesma resposta várias vezes; mais
  // devagar que um minuto é deixar a tela velha à toa.
  const POLL_MIN_MS = 10_000;
  const POLL_MAX_MS = 60_000;
  // Segundos de segmento assumidos quando a câmera não informa - é o default do
  // dwnvr.example.yaml. Servidor atual já resolve o campo antes de responder.
  const SEG_PADRAO = 30;

  let video = $state(null);
  let cam = $state('');
  let day = $state(dayKey());
  let timeline = $state({ ranges: [], segments: [], gens: [] });
  let loading = $state(false);
  let error = $state('');
  let showThumbs = $state(true);
  // Falha de captura tem estado próprio: `error` só é apagado no load()
  // seguinte, e um aviso de captura ficaria colado na tela até trocar de dia.
  let capturaErro = $state('');
  let avisoTimer;

  const dayStart = $derived(parseDay(day).getTime());
  const dayEnd = $derived(dayStart + 86400_000);
  const gravado = $derived(timeline.ranges.reduce((a, [s, e]) => a + (e - s), 0));
  const isToday = $derived(day === dayKey());

  // O ritmo vem da câmera selecionada: `segmentSeconds` é configurado por
  // câmera (10s a 600s no cadastro), e um segmento só entra na timeline depois
  // de fechar. Perguntar uma vez por segmento é o passo natural - quem gravou
  // em pedaços de 10s vê gravação nova em ~20s, e quem gravou em pedaços de
  // 2 min não paga oito consultas do dia inteiro para cada uma que muda algo.
  const pollMs = $derived.by(() => {
    const seg = cameras.list.find((c) => c.id === cam)?.segmentSeconds || SEG_PADRAO;
    return Math.min(Math.max(seg * 1000, POLL_MIN_MS), POLL_MAX_MS);
  });

  onMount(async () => {
    if (!cameras.list.length) await loadCameras();
    cam = cameras.list[0]?.id ?? '';
  });

  onDestroy(() => {
    player.destroy();
    clearThumbnails();
    clearTimeout(avisoTimer);
  });

  // Ligar o player assim que o <video> existir no DOM.
  $effect(() => {
    if (video && player.video !== video) player.attach(video);
  });

  // Recarrega ao trocar de câmera ou de dia. Este é o único ponto de busca de
  // dados da tela, o que evita duas telas disputando o mesmo estado.
  $effect(() => {
    if (cam && day) load(cam, day);
  });

  // Hoje a gravação continua crescendo enquanto a tela está aberta: sem isto, o
  // dia corrente congela no que existia no instante em que a tela abriu. Dia
  // passado não muda mais, então nem vale a consulta.
  $effect(() => {
    if (!cam || !isToday) return;

    const id = setInterval(refresh, pollMs);
    // Aba escondida não toca vídeo nem desenha timeline; ao voltar, o que se
    // perdeu é buscado de uma vez em vez de esperar o próximo intervalo.
    const onVisible = () => {
      if (!document.hidden) refresh();
    };
    document.addEventListener('visibilitychange', onVisible);

    return () => {
      clearInterval(id);
      document.removeEventListener('visibilitychange', onVisible);
    };
  });

  async function load(c, d) {
    loading = true;
    error = '';
    try {
      const t = await api.timeline(c, d);
      timeline = t;
      player.setSource(c, t.gens, t.segments);
      clearThumbnails();
      // Num dia passado, o mais útil é o começo; hoje, o mais recente.
      if (t.segments.length) {
        player.seek(d === dayKey() ? t.segments.at(-1)[0] : t.segments[0][0]);
      }
    } catch (e) {
      error = e.message;
      timeline = { ranges: [], segments: [], gens: [] };
    } finally {
      loading = false;
    }
  }

  // Costura a cauda recém-buscada no que a tela já tinha.
  //
  // A consulta parcial começa 1ms depois do início do último segmento
  // conhecido, e o servidor devolve tudo que TERMINA depois disso - ou seja,
  // esse último segmento vem de volta junto. A sobreposição de um segmento é
  // de propósito: é ela que revela se a primeira faixa da cauda continua a
  // última faixa antiga, sem precisar repetir aqui a tolerância de emenda que
  // o servidor usa. A decisão já vem tomada dentro da resposta.
  function costurar(atual, cauda, desdeMs) {
    const novos = cauda.segments.filter(([start]) => start > desdeMs);
    if (!novos.length) return null;

    // A tabela de gerações da cauda tem índices próprios, começando do zero:
    // usá-los direto apontaria para a geração errada da lista antiga.
    const gens = [...atual.gens];
    const segments = [
      ...atual.segments,
      ...novos.map(([start, dur, gi]) => {
        const g = cauda.gens[gi];
        let j = gens.indexOf(g);
        if (j < 0) j = gens.push(g) - 1;
        return [start, dur, j];
      }),
    ];

    const ranges = atual.ranges.map((r) => [...r]);
    for (const [ini, fim] of cauda.ranges) {
      const ultima = ranges.at(-1);
      if (ultima && ini <= ultima[1]) ultima[1] = Math.max(ultima[1], fim);
      else ranges.push([ini, fim]);
    }

    return { ...atual, ranges, segments, gens };
  }

  let refreshing = false;

  async function refresh() {
    // A virada da meia-noite não mexe em `day` sozinha: o dia que era hoje para
    // de crescer e não precisa mais ser relido.
    if (refreshing || document.hidden || day !== dayKey()) return;
    refreshing = true;
    const c = cam;
    const d = day;
    const base = timeline;
    const ultimo = base.segments.at(-1);
    try {
      // Sem nada em mãos - dia que amanheceu vazio, ou primeira carga que
      // falhou - não há cauda a pedir e vale o dia inteiro.
      const t = ultimo
        ? await api.timelineRange(c, ultimo[0] + 1, dayEnd)
        : await api.timeline(c, d);

      // Câmera, dia ou a própria timeline mudaram durante a busca: quem manda
      // é o load() novo, e costurar por cima misturaria duas câmeras.
      if (c !== cam || d !== day || timeline !== base) return;

      if (!ultimo) {
        if (!t.segments.length) return;
        // Primeira gravação do dia chegando com a tela já aberta: é o mesmo
        // caso do load() inicial, inclusive em ir para o instante mais recente.
        timeline = t;
        player.setSource(c, t.gens, t.segments);
        player.seek(t.segments.at(-1)[0]);
        return;
      }

      // Nada novo fechou desde a última rodada: a tela fica como está, sem
      // trocar objeto nenhum - a tira de miniaturas e a barra não refazem as
      // contas à toa.
      const emendada = costurar(base, t, ultimo[0]);
      if (!emendada) return;

      timeline = emendada;
      player.updateSegments(emendada.gens, emendada.segments);
    } catch {
      // Atualização de fundo: uma falha passageira não pode apagar da tela a
      // timeline que já estava boa nem interromper a reprodução.
    } finally {
      refreshing = false;
    }
  }

  function shiftDay(n) {
    const d = parseDay(day);
    d.setDate(d.getDate() + n);
    day = dayKey(d);
  }

  function exportar() {
    const from = Math.round(player.currentMs || dayStart);
    location.href = mediaURL.export(cam, from, from + 5 * 60_000);
  }

  async function capturar() {
    // O nome segue a mesma convenção da exportação (cam_AAAA-MM-DD_HH-MM-SS),
    // para que imagem e vídeo do mesmo instante fiquem lado a lado na pasta.
    // E o instante é o MOSTRADO, não a hora atual: uma captura de ontem
    // nomeada com hoje não serviria para nada.
    const d = new Date(player.currentMs);
    const nome = `${cam}_${dayKey(d)}_${hhmmss(d.getTime()).replaceAll(':', '-')}.jpg`;
    try {
      await baixarQuadro(video, nome);
    } catch (e) {
      capturaErro = e.message;
      clearTimeout(avisoTimer);
      avisoTimer = setTimeout(() => (capturaErro = ''), 5000);
    }
  }
</script>

<div class="page">
  <div class="bar row wrap">
    <select bind:value={cam} aria-label="câmera">
      {#each cameras.list as c (c.id)}
        <option value={c.id}>{c.name}</option>
      {/each}
    </select>

    <div class="row daynav">
      <button class="ghost" onclick={() => shiftDay(-1)} aria-label="dia anterior">‹</button>
      <input type="date" bind:value={day} max={dayKey()} aria-label="dia" />
      <button class="ghost" onclick={() => shiftDay(1)} disabled={isToday} aria-label="próximo dia">›</button>
    </div>

    <span class="spacer"></span>
    <span class="muted small mono">
      {#if loading}carregando…
      {:else if timeline.segments.length}{duracao(gravado)} · {timeline.ranges.length} faixa(s)
      {:else}sem gravação{/if}
    </span>
  </div>

  <div class="stage">
    <!-- svelte-ignore a11y_media_has_caption -->
    <video bind:this={video} playsinline controls={false}></video>
    <!-- Empilhados numa coluna, e não cada um no seu `position: absolute`:
         antes eram três avisos disputando o mesmo canto, e o aviso de captura
         chega justamente quando o "carregando…" tem chance de estar aceso. -->
    <div class="avisos">
      {#if player.buffering}<span class="badge">carregando…</span>{/if}
      {#if player.error}<span class="badge bad">{player.error}</span>{/if}
      {#if error}<span class="badge bad">{error}</span>{/if}
      {#if capturaErro}<span class="badge bad">{capturaErro}</span>{/if}
    </div>
  </div>

  <div class="controls row wrap">
    <button class="primary" onclick={() => player.toggle()} aria-label="tocar ou pausar">
      {player.playing ? '⏸' : '▶'}
    </button>
    <span class="clock mono">{player.currentMs ? hhmmss(player.currentMs) : '--:--:--'}</span>

    <select
      value={player.rate}
      onchange={(e) => player.setRate(Number(e.currentTarget.value))}
      aria-label="velocidade"
    >
      {#each [0.5, 1, 2, 4, 8] as r}<option value={r}>{r}×</option>{/each}
    </select>

    <span class="spacer"></span>
    <button class="ghost small" onclick={() => (showThumbs = !showThumbs)}>
      {showThumbs ? 'ocultar' : 'mostrar'} miniaturas
    </button>
    <button
      onclick={capturar}
      disabled={!player.currentMs}
      title="Baixar a imagem do instante mostrado"
    >
      ⤓ imagem
    </button>
    <button onclick={exportar} disabled={!player.currentMs}>⤓ exportar 5 min</button>
  </div>

  {#if showThumbs}
    <ThumbStrip
      {cam}
      segments={timeline.segments}
      currentMs={player.currentMs}
      onseek={(ms) => player.seek(ms)}
    />
  {/if}

  <Timeline
    ranges={timeline.ranges}
    {dayStart}
    {dayEnd}
    currentMs={player.currentMs}
    onseek={(ms) => player.seek(ms)}
  />

  <p class="legend muted small row wrap">
    <span><i class="has"></i> com gravação</span>
    <span><i class="gap"></i> sem gravação</span>
    <span>
      toque para pular · arraste para navegar · duplo toque aproxima, dois dedos
      afastam · pince ou role para dar zoom
    </span>
  </p>
</div>

<style>
  .page {
    display: grid;
    gap: 10px;
    padding: 10px;
    max-width: 1400px;
    margin: 0 auto;
  }

  .bar select { max-width: 45vw; }
  .daynav { gap: 4px; }
  .daynav button { padding: 9px 12px; }

  .stage {
    position: relative;
    background: #000;
    border-radius: var(--radius);
    overflow: hidden;
  }

  video {
    width: 100%;
    aspect-ratio: 16 / 9;
    /* A timeline é o ponto desta tela e precisa caber junto com o vídeo: sem
       este teto, um 16:9 numa janela larga já empurra a barra para fora da
       vista. */
    max-height: 56dvh;
    object-fit: contain;
    display: block;
    background: #000;
    margin: 0 auto;
  }

  .avisos {
    position: absolute;
    top: 8px;
    left: 8px;
    right: 8px;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
    /* A coluna cobre a largura do palco para o texto poder quebrar no celular,
       mas não pode virar uma tampa invisível sobre o vídeo. */
    pointer-events: none;
  }

  .badge {
    background: rgba(0, 0, 0, 0.7);
    border: 1px solid var(--line);
    border-radius: 6px;
    padding: 3px 8px;
    font-size: 12px;
  }
  .badge.bad { color: var(--bad); border-color: #5c2b2b; }

  .clock { font-size: 16px; }

  .legend { gap: 14px; margin: 0; }
  .legend i {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: 2px;
    vertical-align: -1px;
  }
  .legend i.has { background: var(--accent); }
  .legend i.gap { background: #1b1f24; border: 1px solid var(--line); }
</style>
