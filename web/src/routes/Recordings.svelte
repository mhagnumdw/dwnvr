<script>
  import { onMount, onDestroy } from 'svelte';
  import { cameras, loadCameras } from '../lib/state.svelte.js';
  import { paramsAtuais, escrever } from '../lib/rota.svelte.js';
  import { api, mediaURL } from '../lib/api.js';
  import { Player } from '../lib/player.svelte.js';
  import { clearThumbnails } from '../lib/thumbs.js';
  import { baixarQuadro } from '../lib/captura.js';
  import { dayKey, parseDay, hhmmss, duracao, taxa } from '../lib/format.js';
  import Timeline from '../components/Timeline.svelte';
  import ThumbStrip from '../components/ThumbStrip.svelte';
  import SemCameras from '../components/SemCameras.svelte';
  import DayPicker from '../components/DayPicker.svelte';

  const player = new Player();

  // Piso e teto do ritmo com que o dia de hoje é relido. Consultar mais rápido
  // que o segmento da câmera é rebaixar a mesma resposta várias vezes; mais
  // devagar que um minuto é deixar a tela velha à toa.
  const POLL_MIN_MS = 10_000;
  const POLL_MAX_MS = 60_000;
  // Segundos de segmento assumidos quando a câmera não informa - é o default do
  // dwnvr.example.yaml. Servidor atual já resolve o campo antes de responder.
  const SEG_PADRAO = 30;
  // Durações oferecidas na exportação. O servidor aceita até 6h (maxExportSpan
  // em internal/api/recordings.go), então o teto daqui é escolha de interface:
  // 10 min já é um arquivo grande de se baixar pelo celular.
  const DURACOES = [1, 2, 3, 5, 10];
  // Velocidades oferecidas. Num const, e não solta no template, porque a URL
  // também precisa dela - para recusar um `rate=999` sem inventar uma segunda
  // lista que um dia discordaria desta.
  const RATES = [0.25, 0.5, 1, 2, 4, 8, 16];

  // Lidos uma vez, na inicialização: daqui em diante quem manda é o estado da
  // tela, que escreve de volta na URL.
  const params = paramsAtuais();

  let video = $state(null);
  let cam = $state('');
  // Num const antes do $state porque o que vem da URL logo abaixo - instante e
  // zoom - são posições DENTRO deste dia, e precisam do dia de partida, não do
  // dia que estiver na tela daqui a pouco.
  const diaInicial = diaDaURL() ?? dayKey();
  let day = $state(diaInicial);
  let daysList = $state([]);
  let timeline = $state({ ranges: [], segments: [], gens: [] });
  let loading = $state(false);
  let error = $state('');
  let showThumbs = $state(params.get('thumbs') !== '0');
  // As setas andam pelo que EXISTE, pulando os buracos do histórico. Sem lista
  // - câmera nova, ou a consulta que falhou - elas voltam a andar de um em um
  // dia até hoje, que é o comportamento antigo: não saber onde há gravação não
  // pode virar não poder navegar.
  const podeRecuarDia = $derived(
    daysList.length ? daysList.some((d) => d < day) : true,
  );
  const podeAvancarDia = $derived(
    daysList.length ? daysList.some((d) => d > day) : day < dayKey(),
  );
  // Segura a escrita na URL até a câmera ter sido resolvida contra o cadastro.
  let montado = $state(false);
  // A duração escolhida sobrevive ao recarregamento, como o layout do Ao Vivo.
  // Valor gravado fora da lista é descartado: ele deixaria o <select> sem
  // opção correspondente, mostrando vazio.
  let exportMin = $state(
    DURACOES.find((m) => m === Number(localStorage.getItem('dwnvr.rec.exportMin'))) ?? 5,
  );
  // Falha de captura tem estado próprio: `error` só é apagado no load()
  // seguinte, e um aviso de captura ficaria colado na tela até trocar de dia.
  let capturaErro = $state('');
  let avisoTimer;

  // --- o que veio da URL ----------------------------------------------------
  //
  // Hora local `HH:MM:SS`, e não epoch: o `day` acima já é uma data local e a
  // timeline inteira é desenhada em hora local, então um epoch seria treze
  // dígitos ilegíveis que ainda assim dependeriam do fuso para casar com o dia.

  function diaDaURL() {
    const d = params.get('day') ?? '';
    // Três perguntas: o formato, se o dia existe - a ida e volta pega o
    // `2026-13-45`, que passa em qualquer teste de formato mas volta escrevendo
    // outra coisa - e se não é no futuro, que é o mesmo teto do calendário.
    // Não existe amanhã para reproduzir.
    return /^\d{4}-\d{2}-\d{2}$/.test(d) && dayKey(parseDay(d)) === d && d <= dayKey()
      ? d
      : null;
  }

  function horaMs(hms, diaMs) {
    const m = /^(\d{1,2}):(\d{2}):(\d{2})$/.exec(hms ?? '');
    if (!m || +m[1] > 23 || +m[2] > 59 || +m[3] > 59) return null;
    return diaMs + (+m[1] * 3600 + +m[2] * 60 + +m[3]) * 1000;
  }

  const inicioDoDia = parseDay(diaInicial).getTime();

  // Instante inicial. Vale só na PRIMEIRA carga - depois de trocar de dia à
  // mão, o instante de um link velho não descreve mais nada -, e por isso o
  // load() o consome zerando.
  let tPendente = horaMs(params.get('t'), inicioDoDia);
  let pausarNaPrimeira = params.get('paused') === '1';
  const rateDaURL = RATES.find((r) => r === Number(params.get('rate'))) ?? null;

  // Janela da timeline: dois horários do mesmo dia. O fim colado na meia-noite
  // sai como `00:00:00`, então "fim <= início" só pode ser a virada do dia. Uma
  // janela mais curta que o zoom máximo passa por aqui e a própria Timeline
  // reancora no dia inteiro - o piso é regra dela, não desta leitura.
  const zoomDaURL = (() => {
    const [de, ate] = (params.get('zoom') ?? '').split(',');
    const a = horaMs(de, inicioDoDia);
    const b = horaMs(ate, inicioDoDia);
    if (a === null || b === null) return null;
    return { de: a, ate: b <= a ? inicioDoDia + 86400_000 : b };
  })();

  let viewFrom = $state(zoomDaURL?.de ?? 0);
  let viewTo = $state(zoomDaURL?.ate ?? 0);

  // --------------------------------------------------------------------------

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

  // Decide o que a exportação vai pedir, e se ela é possível.
  //
  // O filtro repete a regra do store.Range (internal/store/store.go:372):
  // segmento inteiro que se sobrepõe ao intervalo entra. Repetir a regra aqui
  // dá duas coisas que o servidor só diria tarde demais - o início real do
  // clipe, e as duas recusas antecipadas. Elas importam porque o download sai
  // por location.href: um 404 ou um 409 apareceria como texto cru numa aba,
  // sem volta para a tela.
  const exportacao = $derived.by(() => {
    const from = Math.round(player.currentMs || 0);
    if (!from) return null;
    const to = from + exportMin * 60_000;

    const sel = timeline.segments.filter(([s, d]) => s + d > from && s < to);
    if (!sel.length) return { erro: 'sem gravação nesse trecho' };
    // Um MP4 só pode ter um init, então o servidor recusa o intervalo que muda
    // de geração (internal/api/recordings.go:327).
    if (sel.some(([, , gi]) => gi !== sel[0][2]))
      return { erro: 'o trecho atravessa uma troca de codec' };

    return { inicio: sel[0][0], to };
  });

  // No localStorage, e não na URL: a duração da exportação é parâmetro de uma
  // AÇÃO, não do que se está vendo - não tem o que reproduzir num link.
  $effect(() => localStorage.setItem('dwnvr.rec.exportMin', exportMin));

  // A URL, ao contrário, passa a descrever a cena inteira: câmera, dia,
  // instante, velocidade, pausa, zoom e miniaturas. É isso que faz colar o
  // endereço em outra aba cair no mesmo trecho da mesma câmera.
  //
  // O que está no padrão fica de fora, para o link não carregar o que já vale
  // sem ele. E `paused` sai do estado real do <video>, não de um sinalizador
  // próprio: o navegador pode recusar o autoplay, e a barra de endereços não
  // pode prometer uma reprodução que não está acontecendo.
  $effect(() => {
    if (!montado) return;
    const diaInteiro = !viewTo || (viewFrom <= dayStart && viewTo >= dayEnd);
    escrever({
      cam,
      day,
      t: player.currentMs ? hhmmss(player.currentMs) : null,
      rate: player.rate === 1 ? null : player.rate,
      paused: player.playing ? null : '1',
      zoom: diaInteiro ? null : `${hhmmss(viewFrom)},${hhmmss(viewTo)}`,
      thumbs: showThumbs ? null : '0',
    });
  });

  onMount(async () => {
    if (!cameras.list.length) await loadCameras();
    // Câmera do link só vale se ainda existir: cadastro some, link fica.
    const daURL = params.get('cam');
    cam = cameras.list.some((c) => c.id === daURL) ? daURL : (cameras.list[0]?.id ?? '');
    montado = true;
  });

  onDestroy(() => {
    player.destroy();
    clearThumbnails();
    clearTimeout(avisoTimer);
  });

  // Ligar o player assim que o <video> existir no DOM.
  $effect(() => {
    if (!video || player.video === video) return;
    player.attach(video);
    // Antes de qualquer seek, e aqui e não no onMount porque só aqui o <video>
    // existe com certeza: trocar o src zera o playbackRate, e o #reset() do
    // player preserva o valor que encontrar - só precisa encontrá-lo já posto.
    if (rateDaURL) player.setRate(rateDaURL);
  });

  // Recarrega ao trocar de câmera ou de dia. Este é o único ponto de busca de
  // dados da tela, o que evita duas telas disputando o mesmo estado.
  $effect(() => {
    if (cam && day) load(cam, day);
  });

  // Os dias com gravação, uma vez por câmera. É a única leitura deste endpoint
  // na tela: o dia que estreia com a tela aberta entra pelo refresh(), sem
  // custar requisição nenhuma.
  $effect(() => {
    if (cam) carregarDias(cam);
  });

  async function carregarDias(c) {
    try {
      const res = await api.days(c);
      if (c !== cam) return;
      daysList = (res.days || []).map((d) => d.day);
      // Dia que não existe para esta câmera - link antigo, ou a câmera que
      // acabou de ser trocada. O instante que veio da URL descrevia aquele
      // outro dia e não sobrevive à troca.
      if (daysList.length && !daysList.includes(day)) {
        tPendente = null;
        day = daysList.includes(dayKey()) ? dayKey() : daysList.at(-1);
      }
    } catch {
      // Lista vazia é o modo permissivo do seletor: qualquer dia até hoje volta
      // a ser alcançável, como era com o campo de data nativo. Não vira aviso
      // na tela - se o servidor estiver mesmo fora, quem diz isso é o load() da
      // timeline, que é o dado que a pessoa veio ver.
      daysList = [];
    }
  }

  // Hoje estreando no calendário, com a tela já aberta. Local de propósito:
  // o refresh() já sabe o dia, e reler o /api/rec/days a cada segmento fechado
  // seria uma requisição a cada 30s pelo dia inteiro para descobrir algo que a
  // resposta que acabou de chegar já disse.
  //
  // Em modo permissivo a lista fica como está: uma lista de um dia só faria o
  // seletor sair do "tudo liberado" para "só hoje", que é o oposto do que ele
  // deve fazer quando não conhece o histórico.
  function registrarDia(d) {
    if (!daysList.length || daysList.includes(d)) return;
    daysList = [...daysList, d].sort();
  }

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
      // Dia que o calendário oferecia e voltou vazio: a lista está velha - a
      // retenção apagou esse dia com a tela aberta. Reler conserta a lista e
      // leva para um dia que ainda existe. Só acontece nesse desencontro, e é
      // por isso que o poll não precisa reler nada.
      if (!t.segments.length && daysList.includes(d)) carregarDias(c);
      player.setSource(c, t.gens, t.segments);
      clearThumbnails();
      // Num dia passado, o mais útil é o começo; hoje, o mais recente. O
      // instante que veio da URL manda em cima disso, mas só desta vez.
      if (t.segments.length) {
        const alvo = tPendente ?? (d === dayKey() ? t.segments.at(-1)[0] : t.segments[0][0]);
        tPendente = null;
        await player.seek(alvo);
        // Depois do seek, nunca antes: ele termina em play(). Também só desta
        // vez - dali em diante o botão é que decide.
        if (pausarNaPrimeira) player.pause();
        pausarNaPrimeira = false;
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
        registrarDia(d);
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
    if (daysList.length) {
      if (n < 0) {
        const anterior = daysList.filter((d) => d < day).at(-1);
        if (anterior) day = anterior;
      } else if (n > 0) {
        const proximo = daysList.find((d) => d > day);
        if (proximo) day = proximo;
      }
    } else {
      const d = parseDay(day);
      d.setDate(d.getDate() + n);
      day = dayKey(d);
    }
  }

  // O `from` enviado é o início REAL do primeiro segmento, não o instante do
  // cursor. O conjunto de segmentos é o mesmo - o anterior termina exatamente
  // nesse instante e o teste do servidor é `>`, então ele continua de fora - e
  // em troca o nome do arquivo, que sai do `from` pedido
  // (internal/api/recordings.go:352), passa a casar com o conteúdo.
  function exportar() {
    if (!exportacao?.inicio) return;
    location.href = mediaURL.export(cam, exportacao.inicio, exportacao.to);
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
  {#if !cameras.list.length && !cameras.loading}
    <SemCameras />
  {:else}
    <div class="bar row wrap">
      <select bind:value={cam} aria-label="câmera">
        {#each cameras.list as c (c.id)}
          <option value={c.id}>{c.name}</option>
        {/each}
      </select>

      <div class="row daynav">
        <button
          class="ghost"
          onclick={() => shiftDay(-1)}
          disabled={!podeRecuarDia}
          aria-label="dia anterior"
        >
          ‹
        </button>
        <DayPicker value={day} days={daysList} onchange={(d) => (day = d)} />
        <button
          class="ghost"
          onclick={() => shiftDay(1)}
          disabled={!podeAvancarDia}
          aria-label="próximo dia"
        >
          ›
        </button>
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
        {#each RATES as r (r)}<option value={r}>{taxa(r)}</option>{/each}
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
      <span class="split">
        <button onclick={exportar} disabled={!exportacao?.inicio}>⤓ exportar</button>
        <select
          bind:value={exportMin}
          aria-label="duração da exportação"
          title="A partir do instante mostrado"
        >
          {#each DURACOES as m (m)}<option value={m}>{m} min</option>{/each}
        </select>
      </span>
    </div>

    <!-- Só aparece quando há o que explicar. O botão desabilitado diz que não
         dá; sozinho, ele não diria por quê. -->
    {#if exportacao?.erro}
      <p class="exportinfo small mono">⤓ {exportacao.erro}</p>
    {/if}

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
      bind:viewFrom
      bind:viewTo
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
  {/if}
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

  /* Botão e seletor são um controle só. Separados eles se leem como duas
     funções; colados, voltam a dizer "exportar 5 min", que era o rótulo antigo.
     O min-height iguala o select ao botão: no app.css só `button` tem os 44px,
     e a diferença de 4px vira um degrau visível quando os dois encostam. */
  .split { display: inline-flex; }
  .split > * { border-radius: 0; margin: 0; position: relative; }
  .split > button { border-radius: 8px 0 0 8px; }
  .split > select {
    border-radius: 0 8px 8px 0;
    margin-left: -1px;
    min-height: 44px;
    padding-right: 8px;
  }
  /* A sobreposição de 1px esconderia a borda destacada do vizinho de baixo. */
  .split > :hover:not(:disabled),
  .split > :focus-visible { z-index: 1; }

  .exportinfo { margin: -4px 0 0; color: var(--bad); }

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
