<script lang="ts">
  import { onDestroy } from 'svelte';
  import {
    health,
    pollHealth,
    loadHealth,
    isAdmin,
    build,
    REPO_URL,
    RELEASES_URL,
    ISSUES_URL,
  } from '../lib/state.svelte.ts';
  import { currentParams, write } from '../lib/route.svelte.ts';
  import { api, messageOf } from '../lib/api.ts';
  import type { HealthCamera, Funnel, Server } from '../lib/api.ts';
  import {
    bytes,
    mbToBytes,
    kbps,
    formatDays,
    duration,
    hhmmss,
    ddmm,
    timezoneClock,
    recordedWithGap,
  } from '../lib/format.ts';
  import { HELP_RETAINED, HELP_FIT } from '../lib/help.ts';
  import { noticesOf, serverNow, when } from '../lib/notices.ts';
  import NoCameras from '../components/NoCameras.svelte';
  import { collect, asText } from '../lib/browser.ts';
  import type { CardGroup } from '../lib/browser.ts';
  import { groups as groupsFromServer, logLine, collectedAt } from '../lib/server-diagnostics.ts';
  import DiagnosticsCard from '../components/DiagnosticsCard.svelte';
  import type EmailTestT from '../components/EmailTest.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import GitHubMark from '../components/GitHubMark.svelte';
  import Icon from '../components/Icon.svelte';
  import HealthFooter from '../components/HealthFooter.svelte';

  // A table column: it knows how to extract the value that sorts it. Text goes
  // through the collator; numbers, through subtraction.
  interface ColumnBase {
    id: string;
    label: string;
    help?: string;
  }
  type Column<T> = ColumnBase &
    ({ text: true; value: (c: T) => string } | { text?: false; value: (c: T) => number });
  // Never empty: the first one is the fallback for when the URL asks for one
  // that does not exist.
  type Columns<T> = readonly [Column<T>, ...Column<T>[]];

  interface Order {
    col: string;
    asc: boolean;
  }

  // Camera with the object detector funnel, which only comes when the detector
  // is configured.
  type WithFunnel = HealthCamera & { funnel: Funnel };

  // The viewer sees only "Este navegador" and the version: server health
  // belongs to the admin, and the viewer's screen does not even request it. The
  // role does not change while the screen is open - signing out tears
  // everything down.
  const admin = isAdmin();
  if (admin) onDestroy(pollHealth());

  const disk = $derived(health.disk);

  // The seconds advance in HEALTH_POLL_MS steps, together with the read, and
  // not one by one: counting time here just to fill the gap would give a clock
  // driven by the browser, which is the clock this field exists to NOT show.
  // What appears is always an instant the server reported.
  //
  // The abbreviation ("-03") does not identify any region - it applies to São
  // Paulo, Buenos Aires and a handful of other places - so the IANA name is
  // what goes on screen when the server can find it, and the abbreviation goes
  // in the title. A server without /etc/timezone sends only the abbreviation,
  // and then it takes the IANA name's place.
  const clock = $derived.by(() => {
    const c = health.clock;
    if (!c) return null;
    const lido = Date.parse(c.now);
    if (isNaN(lido)) return null;
    return {
      when: timezoneClock(lido, c.offsetSeconds),
      timezone: c.zone ?? c.abbr,
      abbreviation: c.zone ? c.abbr : undefined,
    };
  });
  // Orphans are left out of the "outros" count: they are dwnvr recordings, just
  // from a removed camera, and no retention limit deletes them.
  const orphansBytes = $derived(disk?.orphanBytes ?? 0);
  const othersBytes = $derived(
    disk ? disk.totalBytes - disk.freeBytes - disk.dwnvrBytes - orphansBytes : 0,
  );
  const usedByDwnvr = $derived(disk ? disk.dwnvrBytes / disk.totalBytes : 0);
  const usedByOrphans = $derived(disk ? orphansBytes / disk.totalBytes : 0);
  const usedByOthers = $derived(disk ? othersBytes / disk.totalBytes : 0);

  // retainedMs is the real retention: from the oldest segment on disk until
  // now.
  //
  // The server sends the instant, not the days already counted, because with it
  // the same response serves both the table ("12 dias 4h") and the cameras card
  // ("desde 31/07"). A camera that never recorded does not send the field, and
  // the zero becomes "-" in duration() instead of "0s".
  function retainedMs(c: HealthCamera) {
    return c.oldestSegmentAt ? Date.now() - new Date(c.oldestSegmentAt).getTime() : 0;
  }

  // The header explains WHAT the column measures, once; the cell answers SINCE
  // WHEN, which is per camera. undefined and not '' because Svelte omits the
  // whole attribute that way, and an empty title would leave the cell with a
  // hint underline and no hint.
  function sinceTitle(c: HealthCamera) {
    if (!c.oldestSegmentAt) return undefined;
    const since = `Desde ${ddmm(new Date(c.oldestSegmentAt).getTime())}`;
    const recorded = recordedWithGap(retainedMs(c), c.recordedMs);
    return recorded ? `${since}. ${duration(retainedMs(c) - recorded)} sem gravar.` : since;
  }

  // Each column knows how to extract the value that sorts it; this way the
  // header and the sorting cannot disagree about what "disco" means.
  //
  // The help text lives here, and not loose in the template, because it is the
  // same thing: the label fits in one word, and none of them ("retenção",
  // "reconex.") says by itself what is being measured.
  const columns: Columns<HealthCamera> = [
    {
      id: 'name',
      label: 'câmera',
      value: (c) => c.name,
      text: true,
      help: 'Nome da câmera. O ponto mostra o estado: verde gravando, vermelho parada ou desconectada. O horário ao lado é o do último segmento gravado.',
    },
    {
      id: 'bitrate',
      label: 'taxa',
      value: (c) => c.bitrateKbps || 0,
      help: 'Taxa de bits que está chegando da câmera agora, medida no fluxo gravado.',
    },
    {
      id: 'disk',
      label: 'disco',
      value: (c) => c.diskBytes || 0,
      help: 'Espaço que as gravações desta câmera ocupam hoje, somando todos os dias em disco, e a cota configurada para ela.',
    },
    {
      id: 'retained',
      label: 'retido',
      value: (c) => retainedMs(c),
      help: HELP_RETAINED,
    },
    {
      id: 'fit',
      label: 'cabem',
      value: (c) => c.retainDays || 0,
      help: HELP_FIT,
    },
    {
      id: 'reconnects',
      label: 'reconex.',
      value: (c) => c.reconnects || 0,
      help: 'Quantas vezes o dwnvr precisou reabrir a conexão desde o início da contagem: a subida do dwnvr, ou o último zerar. Número alto indica enlace instável com a câmera.',
    },
  ];

  // numeric so "cam2" comes before "cam10", which is how cameras are usually
  // named here.
  const collator = new Intl.Collator('pt-BR', { numeric: true, sensitivity: 'base' });

  // The sort order comes from the URL when the link carries one, and is
  // validated against the columns above - `sort=inventado` falls back to the
  // default instead of leaving the table with no criterion at all.
  const params = currentParams();
  const urlSort = params.get('sort');

  let order = $state<Order>({
    col: urlSort !== null && columns.some((c) => c.id === urlSort) ? urlSort : 'name',
    asc: params.get('dir') !== 'desc',
  });

  // Back to the URL, leaving out the default - name, ascending: the link does
  // not need to carry what already applies without it.
  $effect(() => {
    write({
      sort: order.col === 'name' ? null : order.col,
      dir: order.asc ? null : 'desc',
    });
  });

  function sortBy(id: string) {
    if (order.col === id) order.asc = !order.asc;
    else order = { col: id, asc: true };
  }

  // Both tables on this screen sort the same way, so the rule lives in one
  // place: the column knows how to extract the value that sorts it, text goes
  // through the collator, and ties ALWAYS fall back to the name - otherwise the
  // table jumps around on every 5 s read, which is what happens when half a
  // dozen cameras share the same zero.
  function sortRows<T>(
    items: readonly T[],
    columns: Columns<T>,
    order: Order,
    nameOf: (c: T) => string,
  ) {
    const col = columns.find((c) => c.id === order.col) ?? columns[0];
    const dir = order.asc ? 1 : -1;
    // Copy: the source is global state and the screen only wants a view of it.
    return [...items].sort((a, b) => {
      const d = col.text
        ? collator.compare(col.value(a), col.value(b))
        : col.value(a) - col.value(b);
      return d ? d * dir : collator.compare(nameOf(a), nameOf(b));
    });
  }

  const rows = $derived(sortRows(health.cameras, columns, order, (c) => c.name));

  // The date comes from the server in ISO/UTC; what matters here is the local
  // time of whoever is reading. Old builds may not send it, and an "Invalid
  // Date" on screen would be worse than showing nothing.
  const compiledAt = $derived.by(() => {
    const d = new Date(build.date);
    return build.date && !isNaN(d.getTime())
      ? d.toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' })
      : '';
  });

  // ---- this browser ---------------------------------------------------
  //
  // Starts closed and only collects when opened: it is a lookup for someone
  // investigating a defect, and whoever only came to watch the cameras does not
  // pay for it. When open, it collects again when the window is resized -
  // rotating the phone changes the page area, which is exactly one of the
  // numbers.
  // It changes through the card's `bind:isOpen`: the type is explicit,
  // otherwise it would be the `false` of the initial value.
  let browserOpened = $state<boolean>(false);
  let groups = $state<CardGroup[]>([]);

  $effect(() => {
    if (!browserOpened) return;
    const recollect = () => {
      void collect().then((g) => {
        groups = g;
      });
    };
    recollect();
    let t: number | undefined;
    const changed = () => {
      clearTimeout(t);
      t = setTimeout(recollect, 250);
    };
    addEventListener('resize', changed);
    return () => {
      clearTimeout(t);
      removeEventListener('resize', changed);
    };
  });

  // The server time for the viewer, who does not read /api/health: the Date
  // header of any response, read when the card opens. { server, here } in ms.
  let headerHour = $state<{ server: number; here: number } | null>(null);

  $effect(() => {
    if (admin || !browserOpened) return;
    let isLive = true;
    api
      .ping(5000)
      .then((r) => {
        const server = Date.parse(r.headers.get('Date') ?? '');
        if (isLive && !isNaN(server)) headerHour = { server, here: Date.now() };
      })
      .catch(() => {
        // Without a response, the clock line just does not appear.
      });
    return () => {
      isLive = false;
    };
  });

  // The device clock against the server's. The response time, because of
  // latency, arrives a few milliseconds late, and the Date header still comes
  // without milliseconds; below 5s it is noise, and above it the clock is
  // wrong, which makes "now" point to a different spot on the timeline.
  const clockDifference = $derived.by((): { value: string; alert?: boolean } | null => {
    const lido = admin ? (health.clock ? Date.parse(health.clock.now) : NaN) : headerHour?.server;
    const here = admin ? health.updatedAt : headerHour?.here;
    if (!lido || isNaN(lido) || !here) return null;
    const d = Math.round((here - lido) / 1000);
    if (Math.abs(d) < 5) return { value: 'igual ao do servidor' };
    return {
      value: `${duration(Math.abs(d) * 1000)} ${d > 0 ? 'adiantado' : 'atrasado'}`,
      alert: true,
    };
  });

  const groupsWithClock = $derived(
    groups.map((g) =>
      g.title === 'Ambiente' && clockDifference
        ? { ...g, items: [...g.items, { label: 'relógio do aparelho', ...clockDifference }] }
        : g,
    ),
  );

  function browserText() {
    const header = [
      `dwnvr ${build.version || '?'} - diagnóstico do navegador`,
      new Date().toString(),
    ];
    return asText(groupsWithClock, header, [`user agent: ${navigator.userAgent}`]);
  }

  // ---- this server ----------------------------------------------------
  //
  // Same treatment as the browser: closed, and it only asks the server while
  // the card is open. The read costs a write test on storage and a round trip
  // to go2rtc and the object detector, so the pace is slower than the rest of
  // the screen - temperature and pressure change over tens of seconds, not 5.
  //
  // The previous read is kept because dwnvr's CPU comes from the difference
  // between the two. Closing the card forgets both: reopened an hour later, the
  // figure would be the average of that hour, not of now.
  const SERVER_POLL_MS = 15000;
  let serverOpened = $state<boolean>(false);
  let server = $state<Server | null>(null);
  let previousServer = $state<Server | null>(null);
  let serverError = $state('');

  $effect(() => {
    if (!serverOpened) return;
    let isLive = true;
    // A background tab does not ask: each read writes to storage, and a card
    // forgotten open in a hidden tab would do that every 15 s forever. When the
    // tab becomes visible again, it reads right away instead of waiting for the
    // next tick.
    const read = async () => {
      if (document.hidden) return;
      try {
        const r = await api.server();
        if (isLive) {
          previousServer = server;
          server = r;
          serverError = '';
        }
      } catch (e) {
        if (isLive) serverError = messageOf(e);
      }
    };
    const readNow = () => {
      void read();
    };
    readNow();
    const id = setInterval(readNow, SERVER_POLL_MS);
    document.addEventListener('visibilitychange', readNow);
    return () => {
      isLive = false;
      clearInterval(id);
      document.removeEventListener('visibilitychange', readNow);
      server = previousServer = null;
    };
  });

  const serverGroups = $derived(server ? groupsFromServer(server, previousServer) : []);

  // The email test is a separate chunk: it only downloads with the smtp section
  // on.
  const emailEnabled = $derived(!!server?.smtp && !server.smtp.error);
  let EmailTest = $state<typeof EmailTestT | null>(null);
  $effect(() => {
    if (emailEnabled && !EmailTest) {
      void import('../components/EmailTest.svelte').then((m) => {
        EmailTest = m.default;
      });
    }
  });
  const serverLog = $derived(server?.log);
  const serverCollected = $derived(server ? collectedAt(server.collectedAt) : '');

  // "Copy" carries the time of the collection, not the time the button was
  // tapped: the text describes that read. The non-breaking spaces in the
  // subtitle go back to ordinary spaces in the pasted text.
  function serverText() {
    const collected = serverCollected.replaceAll(' ', ' ');
    const header = [
      `dwnvr ${build.version || '?'} - diagnóstico do servidor`,
      collected || new Date().toString(),
    ];
    const log = serverLog?.lines.length
      ? [
          `[Últimos avisos e erros do log: ${serverLog.lines.length} de ${serverLog.total}]`,
          ...serverLog.lines.map(logLine),
        ]
      : [];
    return asText(serverGroups, header, log);
  }

  const totalKbps = $derived(health.cameras.reduce((a, c) => a + (c.bitrateKbps || 0), 0));
  const bytesPerDay = $derived(((totalKbps * 1000) / 8) * 86400);

  // ---- object recognition ----------------------------------------
  //
  // The design is a funnel, built on the numbers the server provides: each
  // camera's `funnel`, counted since dwnvr started. A camera without a detector
  // does not send the field, and when no camera has it the whole section
  // disappears, instead of showing up zeroed and looking like a defect.
  const withFunnel = $derived(
    health.cameras.filter((c): c is WithFunnel => c.funnel !== undefined),
  );
  // The queue, which is a single one for all cameras, and the times of the
  // latest inferences. It disappears together with the detector.
  const detector = $derived(health.detector);

  // The pieces of a camera's funnel. The slices add up to the onsets (see
  // `Funnel` in internal/recorder/recorder.go).
  interface Outcomes {
    total: number;
    dropped: number;
    inferred: number;
    object: number;
    waiting: number;
    noVideo: number;
    rejected: number;
    failed: number;
  }

  const OUTCOME_KEYS = [
    'total',
    'dropped',
    'inferred',
    'object',
    'waiting',
    'noVideo',
    'rejected',
    'failed',
  ] as const satisfies readonly (keyof Outcomes)[];

  function outcomesOf(c: WithFunnel): Outcomes {
    const f = c.funnel;
    return {
      total: c.onsets,
      dropped: f.dropped,
      inferred: f.noObject,
      object: f.withObject,
      waiting: f.queued,
      noVideo: f.noVideo,
      rejected: f.rejected,
      failed: f.failed,
    };
  }

  // The funnel is the whole story of one motion mark, and each piece is a
  // DIFFERENT outcome. Not adding everything into a single "perdidas" is the
  // reason this bar exists: "descartada" is the queue acting as a deliberate
  // brake, "olhou, nada novo" includes the car that was already parked, and
  // only red means a mark was left without an answer.
  // The color of each outcome lives next to its label, and goes to the screen
  // in `style` instead of becoming a class. There are two reasons: the bar and
  // the legend read the SAME definition, which is what keeps the legend's
  // little square from disagreeing with the bar's piece; and Svelte removes
  // from the CSS every rule it does not see used in the template - a class
  // built at runtime, like `s-{id}`, it does not see, and the bar would come
  // out with no color at all.
  //
  // `dark` says whether the number inside the piece goes in dark text: only the
  // three strong-colored pieces have the contrast for that; on the two grays it
  // would vanish.
  //
  // `always` keeps the outcome in the legend even when zero: "na fila" is a
  // state of right now, and goes from 0 to 1 and back every few seconds -
  // appearing and disappearing, the whole legend would jump.
  interface Outcome {
    id: Exclude<keyof Outcomes, 'total' | 'noVideo' | 'rejected' | 'failed'> | 'lost';
    color: string;
    dark: boolean;
    label: string;
    help?: string;
    always?: boolean;
  }

  const OUTCOMES: Outcome[] = [
    // The queue holds few marks per camera: one that arrives with the camera
    // already at the limit is discarded - it does not go to the detector now or
    // later - and stays only as motion on the timeline.
    { id: 'dropped', color: '#3d444d', dark: false, label: 'descartada: fila da câmera cheia' },
    { id: 'inferred', color: 'var(--accent)', dark: true, label: 'olhou, nada novo' },
    { id: 'object', color: 'var(--ok)', dark: true, label: 'achou objeto' },
    // Light gray, and not another shade of the dark one: a dark gray ends up
    // almost identical to the `#3d444d` of discarded, and the two pieces blur
    // together. A truly new color does not work either - purple and pink are
    // already vehicle and animal on the timeline, and repeating them here would
    // invent a kinship that does not exist.
    //
    // "Em andamento", and not "na fila": the piece gathers everything that has
    // no result yet - the mark waiting for the peak to be cut, the ones waiting
    // their turn and the one being analyzed. Called "na fila", it matched
    // neither the queue chip nor the dots in the table, which count only the
    // ones waiting their turn.
    {
      id: 'waiting',
      color: 'var(--dim)',
      dark: true,
      label: 'em andamento',
      help: 'Marcas que ainda não têm resultado: esperando o melhor quadro (até 3 s), esperando a vez na fila, ou em análise agora. O chip da fila e as bolinhas da tabela contam só as que esperam a vez.',
      always: true,
    },
    { id: 'lost', color: 'var(--bad)', dark: true, label: 'não analisadas' },
  ];

  // The bar width in pixels, measured by the browser itself. Without it there
  // is no way to know whether the number fits in the piece: the slice is a
  // PERCENTAGE, and 0.2% is 2px on the phone and 2px on the desktop - what
  // changes is only how many pixels there are in total. Nine marks out of four
  // thousand fit nowhere.
  let funnelWidth = $state(0);

  // Does the number fit inside the piece? 12px bold is about 8px per digit,
  // plus some slack on each side so it does not touch the edge. When it does
  // not fit, the number goes to the legend instead of being cut in half - that
  // is how a "9" ended up looking like half a stroke on screen.
  function fits(p: { n: number }) {
    if (!funnelWidth || !funnel) return false;
    return (p.n / funnel.total) * funnelWidth >= String(p.n).length * 8 + 8;
  }

  // The detector times in seconds with one decimal, and not through
  // `duration()`: the queue wait is almost always under one second, and it
  // would round it to "0s".
  const seconds = (ms: number) => `${(ms / 1000).toFixed(1).replace('.', ',')}s`;

  // The sum over the cameras. The server sends per camera, and the total is
  // arithmetic the screen does: it is a sum of a few numbers per camera.
  const sum = $derived.by(() => {
    const s: Outcomes = {
      total: 0,
      dropped: 0,
      inferred: 0,
      object: 0,
      waiting: 0,
      noVideo: 0,
      rejected: 0,
      failed: 0,
    };
    for (const c of withFunnel) {
      const d = outcomesOf(c);
      for (const k of OUTCOME_KEYS) s[k] += d[k];
    }
    return s;
  });

  const funnel = $derived.by(() => {
    if (!sum.total) return null;
    const lostCount = sum.noVideo + sum.rejected + sum.failed;
    const amounts = { ...sum, lost: lostCount };
    // Only what exists goes in the bar. In the legend too, except what is
    // `always`: a legend entry for a piece that was not drawn sends the reader
    // looking for a color that is not there, but its "0" says exactly that.
    const allItems = OUTCOMES.map((d) => ({ ...d, n: amounts[d.id] }));
    const clips = allItems.filter((d) => d.n > 0);
    const caption = allItems.filter((d) => d.n > 0 || d.always === true);
    return { clips, caption, total: sum.total, lostCount };
  });

  // The breakdown of the red piece, in words. It only appears when there is
  // something to explain, and each reason has its own entry - this is where
  // "não analisadas" stops being a single lump.
  const unanalyzed = $derived(
    [
      { n: sum.noVideo, text: 'sem vídeo inteiro para mandar, logo depois de conectar' },
      { n: sum.rejected, text: 'vídeo corrompido vindo da câmera' },
      { n: sum.failed, text: 'o detector não respondeu' },
    ].filter((x) => x.n > 0),
  );

  // The columns of the detection table. Same shape as the ones above: each
  // knows how to extract the value that sorts it and carries its own help text,
  // so the header and the sorting cannot disagree about what the column
  // measures.
  //
  // The `??` on the hit rate is not a detail: a camera never inferred has no
  // hit rate, and without a value the sorting would throw it anywhere. -1 puts
  // "sem dado" before everyone else in ascending order.
  const detColumns: Columns<DetRow> = [
    { id: 'name', label: 'câmera', value: (c) => c.name, text: true },
    {
      id: 'inferences',
      label: 'olhadas',
      value: (c) => c.inferences,
      help: 'Marcas de movimento desta câmera que o detector olhou, desde que o dwnvr subiu.',
    },
    {
      id: 'object',
      label: 'com objeto',
      value: (c) => c.object,
      help: 'Destas, quantas viraram marca de pessoa, veículo ou animal na timeline.',
    },
    {
      id: 'hitRate',
      label: 'acerto',
      value: (c) => c.hit ?? -1,
      help: 'A fatia das olhadas que achou algo. Perto de 100% costuma ser cena parada com objeto fixo no quadro, e não qualidade.',
    },
    {
      id: 'dropped',
      label: 'descartadas',
      value: (c) => c.dropped,
      help: 'Marcas que chegaram com esta câmera já com o limite dela esperando na fila. Foram descartadas: não vão ao detector nem depois, e ficam só como movimento na timeline. É o freio que impede uma câmera agitada de tomar a vez das outras - ela só perde o que é dela.',
    },
    {
      id: 'lost',
      label: 'perdidas',
      value: (c) => c.lostCount,
      help: 'Marcas que ficaram sem resposta: sem vídeo inteiro para mandar, vídeo corrompido vindo da câmera, ou o detector que não respondeu.',
    },
    {
      id: 'queue',
      label: 'na fila',
      // Whoever waits the most comes first; processing breaks ties.
      value: (c) => c.queue.waiting * 2 + (c.queue.busy ? 1 : 0),
      // A getter, and not fixed text: the number of slots comes from the
      // server, and the header rereads the hint when it arrives.
      get help() {
        const n = detector?.queue.perCamera ?? 2;
        return `Os lugares desta câmera na fila: até ${n} marcas esperando a vez (cinza). Com os ${n} ocupados (âmbar), a próxima marca desta câmera é descartada e fica só como movimento na timeline. O ponto azul, à parte, é a câmera cuja imagem o detector está processando agora - essa marca já saiu da fila.`;
      },
    },
  ];

  // Own keys in the URL (`dsort`/`ddir`): the two tables on the screen sort
  // independently, and sharing `sort` would make one affect the other.
  const urlSortDet = params.get('dsort');
  let detOrder = $state<Order>({
    col: urlSortDet !== null && detColumns.some((c) => c.id === urlSortDet) ? urlSortDet : 'name',
    asc: params.get('ddir') !== 'desc',
  });

  $effect(() => {
    write({
      dsort: detOrder.col === 'name' ? null : detOrder.col,
      ddir: detOrder.asc ? null : 'desc',
    });
  });

  function sortDet(id: string) {
    if (detOrder.col === id) detOrder.asc = !detOrder.asc;
    else detOrder = { col: id, asc: true };
  }

  // What a camera has in the queue right now: the slots, occupied first, and
  // whether an image of it is being processed. That one has already left the
  // queue, so it does not take a slot - it goes separately. A camera not in
  // `cameras` has nothing.
  function queueOf(id: string) {
    const n = detector?.queue.perCamera ?? 0;
    const l = detector?.queue.cameras[id] ?? { waiting: 0, busy: false };
    const slots = Array.from({ length: Math.max(n, l.waiting) }, (_, i) =>
      i < l.waiting ? 'waiting' : 'free',
    );
    return { ...l, slots, full: n > 0 && l.waiting >= n };
  }

  // One row of the detection table.
  function detRow(c: WithFunnel) {
    const d = outcomesOf(c);
    const inferences = d.inferred + d.object;
    return {
      id: c.id,
      name: c.name || c.id,
      ...d,
      inferences,
      lostCount: d.noVideo + d.rejected + d.failed,
      hit: inferences ? Math.round((d.object / inferences) * 100) : null,
      queue: queueOf(c.id),
      enabled: c.detect,
    };
  }
  type DetRow = ReturnType<typeof detRow>;

  const detCams = $derived.by(() => {
    const rows = withFunnel.map(detRow);
    // The server sends every camera's funnel as soon as there is a detector,
    // whether or not its motion detection is on. When off, it marks nothing,
    // and the zeroed row would read as "saw nothing". It stays in the table,
    // because the numbers survive being turned off mid-day, but in a separate
    // group, at the end: the chosen sort order applies within each group.
    const sorted = sortRows(rows, detColumns, detOrder, (c) => c.name);
    return {
      enabledCams: sorted.filter((c) => c.enabled),
      disabledCams: sorted.filter((c) => !c.enabled),
    };
  });

  // ---- notices ------------------------------------------------------------

  // The list lives in lib/notices.ts, which the header bell also uses: both
  // count with the same rule.
  const now = $derived(serverNow(health));
  const notices = $derived(noticesOf(health));

  // The reset only appears when there is something to reset.
  const hasReconnection = $derived(health.cameras.some((c) => c.reconnects > 0));

  // "contando desde" in the title only when all online cameras count from the
  // same point - dwnvr startup, or the last reset. A camera restarted by a
  // config change counts from its own restart, and then each notice's own since
  // is what applies.
  const countingSince = $derived.by(() => {
    const starts = health.cameras
      .map((c) => Date.parse(c.reconnectsSince))
      .filter((t) => !isNaN(t) && t > 0);
    if (!starts.length) return null;
    const min = Math.min(...starts);
    return Math.max(...starts) - min < 60e3 ? min : null;
  });

  let confirmingReset = $state(false);
  let resetting = $state(false);
  let resetError = $state('');

  async function resetReconnects() {
    confirmingReset = false;
    resetting = true;
    resetError = '';
    try {
      await api.resetReconnects();
      await loadHealth();
    } catch (e) {
      resetError = messageOf(e);
    } finally {
      resetting = false;
    }
  }
</script>

<div class="page">
  {#if admin}
    <!-- The two uptimes sit side by side on purpose: the comparison between
         them is what diagnoses. Equal, nothing restarted; only dwnvr's short,
         it was a deploy or a process crash; both short, the machine rebooted -
         and then "reconex. 0" and "não gravou nada desde que o dwnvr subiu",
         just below, stop being a mystery.

         The whole thing disappears against an old server, which does not answer
         the field. -->
    {#if health.uptime}
      <div class="card row wrap statusbar small">
        <span class="dot ok"></span>
        <span class="muted">dwnvr no ar há</span>
        <span class="mono">{duration(health.uptime.appSeconds * 1000)}</span>
        {#if health.uptime.machineSeconds}
          <span class="muted">·</span>
          <span class="muted">máquina há</span>
          <span class="mono">{duration(health.uptime.machineSeconds * 1000)}</span>
        {/if}
        <!-- Server time in the server's time zone, and not the browser's: that
             is how a wrong clock or TZ is found on the machine that records,
             which is the one that stamps the segment names. -->
        {#if clock}
          <span class="muted">·</span>
          <span class="muted">horário do servidor</span>
          <span class="mono" title={clock.abbreviation}>{clock.when} {clock.timezone}</span>
        {/if}
      </div>
    {/if}

    {#if disk}
      <div class="card">
        <div class="row wrap">
          <strong>Disco</strong>
          <span class="spacer"></span>
          <span class="muted small mono"
            >{bytes(disk.freeBytes)} livres de {bytes(disk.totalBytes)}</span
          >
        </div>

        <div
          class="meter"
          title="azul: gravações do dwnvr; amarelo: gravações de câmeras removidas; cinza: outros dados"
        >
          <span class="seg dwnvr" style:width="{usedByDwnvr * 100}%"></span>
          <span
            class="seg orphans"
            class:visible={orphansBytes > 0}
            style:width="{usedByOrphans * 100}%"
          ></span>
          <span class="seg others" style:width="{usedByOthers * 100}%"></span>
        </div>

        <div class="row wrap small muted legend">
          <span><i class="dwnvr"></i> dwnvr: {bytes(disk.dwnvrBytes)}</span>
          <!-- Only with an orphan: a permanent legend would suggest that
               leftover recordings without a camera are normal. Whoever deletes
               them is the Cameras screen, not retention. -->
          {#if orphansBytes > 0}
            <span
              title="Gravações de câmeras removidas. A retenção não as apaga; apague em Câmeras."
            >
              <i class="orphans"></i> câmeras removidas: {bytes(orphansBytes)}
            </span>
          {/if}
          <span><i class="others"></i> outros: {bytes(othersBytes)}</span>
          <span class="spacer"></span>
          <span>mínimo livre: {mbToBytes(disk.minFreeMB)}</span>
        </div>
      </div>
    {/if}

    <div class="card row wrap totals">
      <div>
        <span class="big mono">{health.cameras.filter((c) => c.connected).length}</span><br /><span
          class="muted small">conectadas</span
        >
      </div>
      <div>
        <span class="big mono">{kbps(totalKbps)}</span><br /><span class="muted small"
          >taxa somada</span
        >
      </div>
      <div>
        <span class="big mono">{bytes(bytesPerDay)}</span><br /><span class="muted small"
          >por dia</span
        >
      </div>
    </div>

    <!-- The card exists even with no notice at all, showing "nenhum problema":
         the area does not change its look, and the reset stays at hand for the
         reconex. column. -->
    {#if notices.length || health.updatedAt}
      <div class="card notices">
        <div class="row wrap">
          <strong>Avisos</strong>
          {#if notices.length}<span class="muted small mono">{notices.length}</span>{/if}
          <span class="spacer"></span>
          {#if hasReconnection}
            {#if countingSince}
              <span class="muted small">reconexões contadas desde {when(countingSince, now)}</span>
            {/if}
            <button
              class="ghost small"
              title="Zera a contagem de reconexões de todas as câmeras"
              onclick={() => (confirmingReset = true)}
              disabled={resetting}
            >
              zerar reconexões
            </button>
          {/if}
        </div>
        {#if resetError}
          <p class="row bad">
            <span class="dot bad"></span>Não foi possível zerar as reconexões: {resetError}
          </p>
        {/if}
        <!-- eslint-disable-next-line svelte/require-each-key -- text only, no per-row state; and two notices can have the same text, which would break the key -->
        {#each notices as a}
          <p class="row {a.level}"><span class="dot {a.level}"></span>{a.text}</p>
        {:else}
          <p class="row ok"><span class="dot ok"></span>Nenhum problema detectado.</p>
        {/each}
      </div>
    {/if}

    {#if confirmingReset}
      <ConfirmDialog
        title="Zerar a contagem de reconexões?"
        confirmLabel="zerar"
        onconfirm={resetReconnects}
        oncancel={() => (confirmingReset = false)}
      >
        Vale para todas as câmeras: a coluna <strong>reconex.</strong> volta a zero e a contagem recomeça
        agora. Nenhuma conexão cai. Não tem como desfazer.
      </ConfirmDialog>
    {/if}

    <!-- Sorting on the phone: the column header does not exist there, so each
         column becomes a chip. Same state and same URL as the desktop header. -->
    {#snippet chipsOrder(cols: readonly ColumnBase[], o: Order, sortedBy: (id: string) => void)}
      <div class="order row wrap small">
        <span class="muted">ordenar</span>
        {#each cols as col (col.id)}
          <button
            class="chip"
            class:active={o.col === col.id}
            aria-label="ordenar por {col.label}"
            title={col.help}
            onclick={() => {
              sortedBy(col.id);
            }}
          >
            {col.label}{#if o.col === col.id}<span class="arrow"
                ><Icon name={o.asc ? 'chevronUp' : 'chevronDown'} size={12} stroke={2.5} /></span
              >{/if}
          </button>
        {/each}
      </div>
    {/snippet}

    <!-- The label of each value on the phone card, taken from the same column
         that names the chip. On desktop it goes away: there the header names
         it. -->
    {#snippet label(cols: readonly ColumnBase[], o: Order, id: string)}
      <span class="rot-cel" class:active={o.col === id}>{cols.find((c) => c.id === id)?.label}</span
      >
    {/snippet}

    <div class="table card">
      {@render chipsOrder(columns, order, sortBy)}
      <div class="thead row small muted">
        {#each columns as col (col.id)}
          <!-- The title goes on the whole button, not just the label: the mouse
               hover area is the header cell, which is where the person pauses
               to decide whether to click. -->
          <button
            class="th"
            class:active={order.col === col.id}
            aria-label="ordenar por {col.label}"
            title={col.help}
            onclick={() => {
              sortBy(col.id);
            }}
          >
            <span class="label">{col.label}</span>
            <!-- The arrow always takes up space, otherwise the header jumps on each click. -->
            <span class="arrow">
              {#if order.col === col.id}
                <Icon name={order.asc ? 'chevronUp' : 'chevronDown'} size={12} stroke={2.5} />
              {/if}
            </span>
          </button>
        {/each}
      </div>
      {#each rows as c (c.id)}
        <div class="trow row">
          <span class="c-name row">
            <span
              class="dot"
              class:ok={c.connected && !c.silent}
              class:bad={c.enabled && (c.silent || !c.connected)}
            ></span>
            <span class="name">{c.name}</span>
            <!-- Connected but not recording is the treacherous state: the green
                 dot said "all good" while the camera produced nothing. -->
            {#if c.silent}<span class="chip stopped">não grava</span>
            {:else if c.lastSegmentAt}<span class="chip"
                >{hhmmss(new Date(c.lastSegmentAt).getTime())}</span
              >{/if}
            {#if c.hasAudio}<span class="chip">áudio</span>{/if}
          </span>
          <span class="mono">{@render label(columns, order, 'bitrate')}{kbps(c.bitrateKbps)}</span>
          <!-- Usage and quota together, as in the chip on the Cameras screen:
               the number alone does not say whether it is a lot or a little for
               this camera. -->
          <span class="mono"
            >{@render label(columns, order, 'disk')}<span class="whole">{bytes(c.diskBytes)}</span>
            de <span class="whole">{mbToBytes(c.quotaMB)}</span></span
          >
          <!-- The recorded amount goes on its own line, and only with a gap: on
               a camera without failures the cell stays as it always was, and
               the extra line is itself the sign that something was missing. -->
          <span class="mono" title={sinceTitle(c)}>
            {@render label(columns, order, 'retained')}{duration(retainedMs(c))}
            {#if recordedWithGap(retainedMs(c), c.recordedMs)}
              <br /><span class="muted small">{duration(c.recordedMs)} grav.</span>
            {/if}
          </span>
          <span class="mono">{@render label(columns, order, 'fit')}{formatDays(c.retainDays)}</span>
          <span class="mono">{@render label(columns, order, 'reconnects')}{c.reconnects}</span>
        </div>
      {/each}
      {#if !health.cameras.length}
        <!-- Two different absences used to live in the same "sem dados ainda":
             the server that has not answered yet and the installation with no
             camera at all. `updatedAt` only leaves 0 when a read comes back,
             and the response carries every registered camera - including the
             disabled one. -->
        {#if health.updatedAt}
          <NoCameras />
        {:else}
          <p class="empty">sem dados ainda</p>
        {/if}
      {/if}
    </div>

    <!-- Object recognition. Disappears entirely when no camera has a detector
         configured.

         A single card, not two: the funnel and the table are the same thing
         seen from far and from near. Two cards would suggest two subjects; what
         separates the two halves here is a rule, not a gap. -->
    {#if withFunnel.length}
      <div class="card detection">
        <div class="top">
          <div class="row wrap">
            <strong>Reconhecimento de objetos</strong>
            <span class="spacer"></span>
            <!-- The numbers reset when dwnvr restarts: without saying since
                 when they count, "12 com objeto" is neither a lot nor a little. -->
            {#if health.uptime}
              <span class="muted small"
                >desde que o dwnvr subiu, há {duration(health.uptime.appSeconds * 1000)}</span
              >
            {/if}
            {#if detector}
              <span
                class="chip mono"
                title="marcas esperando a vez agora, e o teto: {detector.queue
                  .perCamera} por câmera com detecção ligada"
              >
                fila {detector.queue.length}/{detector.queue.cap}
              </span>
              <span
                class="chip mono"
                title="o maior tamanho que a fila já teve desde que o dwnvr subiu"
              >
                pico {detector.queue.peak}
              </span>
            {/if}
          </div>

          {#if funnel}
            <!-- A single bar tells the whole story: of everything the cameras
                 marked, what was discarded, what the detector looked at, what
                 became an object and what was left without an answer. It is
                 this screen's only way to answer WHY a mark did not become an
                 object without needing prose. -->
            <div
              class="funnel"
              bind:clientWidth={funnelWidth}
              title="o que aconteceu com cada marca de movimento"
            >
              {#each funnel.clips as p (p.id)}
                <span
                  class="seg"
                  class:dark={p.dark}
                  style:width="{(p.n / funnel.total) * 100}%"
                  style:background={p.color}>{fits(p) ? p.n : ''}</span
                >
              {/each}
            </div>

            <!-- The number accompanies the label when it did not fit in the
                 piece. This way each number appears exactly once, and none
                 appears cut in half. -->
            <div class="row wrap small muted legend">
              {#each funnel.caption as p (p.id)}
                <span title={p.help}>
                  <i style:background={p.color}></i>
                  {p.label}{#if !fits(p)}{' '}<b class="mono">{p.n}</b>{/if}
                </span>
              {/each}
            </div>

            <div class="row wrap small muted numbers">
              <span><b class="mono">{funnel.total}</b> marcas de movimento</span>
              <span><b class="mono">{sum.object}</b> viraram objeto na timeline</span>
              <!-- Two times, not one: analyzing is the detector looking, the
                   wait is how long the mark sat in the queue before it - and
                   that is the one that grows when the device cannot keep up.
                   Zero means "ainda não olhou nenhuma", and a "0,0s" there
                   would say it is instantaneous. -->
              {#if detector?.times.inferenceMs}
                <span
                  >analisar leva <b class="mono">{seconds(detector.times.inferenceMs)}</b> por marca</span
                >
                <span
                  >espera de cada marca na fila <b class="mono">{seconds(detector.times.waitMs)}</b
                  ></span
                >
              {/if}
            </div>

            {#if unanalyzed.length}
              <p class="small muted wrap">
                das {funnel.lostCount} não analisadas:
                {#each unanalyzed as m, i (m.text)}{i ? ' · ' : ' '}<b class="mono">{m.n}</b>
                  {m.text}{/each}
              </p>
            {/if}
          {:else}
            <p class="small muted wrap">nenhuma marca de movimento desde que o dwnvr subiu</p>
          {/if}
        </div>

        <div class="det-table">
          {@render chipsOrder(detColumns, detOrder, sortDet)}
          <div class="thead detgrid row small muted">
            {#each detColumns as col (col.id)}
              <button
                class="th"
                class:active={detOrder.col === col.id}
                aria-label="ordenar por {col.label}"
                title={col.help}
                onclick={() => {
                  sortDet(col.id);
                }}
              >
                <span class="label">{col.label}</span>
                <!-- The arrow always takes up space, otherwise the header jumps on each click. -->
                <span class="arrow">
                  {#if detOrder.col === col.id}
                    <Icon
                      name={detOrder.asc ? 'chevronUp' : 'chevronDown'}
                      size={12}
                      stroke={2.5}
                    />
                  {/if}
                </span>
              </button>
            {/each}
          </div>
          {#snippet tableRow(c: DetRow)}
            <div class="trow detgrid row" class:disabled={!c.enabled}>
              {#if c.enabled}
                <span class="name">{c.name}</span>
              {:else}
                <span class="c-disabled row">
                  <span class="name">{c.name}</span>
                  <a class="small" href="#cams?edit={encodeURIComponent(c.id)}">ligar</a>
                </span>
              {/if}
              <span class="mono" class:zero={!c.inferences}
                >{@render label(detColumns, detOrder, 'inferences')}{c.inferences}</span
              >
              <span class="mono" class:zero={!c.object}
                >{@render label(detColumns, detOrder, 'object')}{c.object}</span
              >
              <!-- Almost everything having an object is not a good sign: it is
                   usually a stationary object inside the frame, and then
                   recognition is confirming the same thing over and over
                   instead of warning about something new. -->
              <span class="mono" class:zero={!c.object} class:alert={c.hit !== null && c.hit >= 90}>
                {@render label(detColumns, detOrder, 'hitRate')}{c.hit === null ? '-' : `${c.hit}%`}
              </span>
              <span class="mono" class:zero={!c.dropped}
                >{@render label(detColumns, detOrder, 'dropped')}{c.dropped}</span
              >
              <span class="mono" class:zero={!c.lostCount} class:alert={c.lostCount > 0}
                >{@render label(detColumns, detOrder, 'lost')}{c.lostCount}</span
              >
              <!-- The camera's slots, and not a number: full or empty reads at
                   a glance. Amber when all are occupied, which is when its next
                   mark is discarded. Blue sits to their left, and separate: the
                   image being processed is not in the queue. Without detection
                   the camera does not enter the queue, and empty slots there
                   would say it is waiting its turn. -->
              <span class="queue-cell">
                {#if c.enabled}
                  {#if c.queue.busy}
                    <span
                      class="processing"
                      title="o detector está processando uma imagem desta câmera agora"
                    ></span>
                  {/if}
                  <span
                    class="slots"
                    class:full={c.queue.full}
                    title="{c.queue.waiting} de {c.queue.slots.length} lugares ocupados"
                  >
                    {#each c.queue.slots as l, i (i)}<span class="slot {l}"></span>{/each}
                  </span>
                {/if}
              </span>
            </div>
          {/snippet}
          {#each detCams.enabledCams as c (c.id)}{@render tableRow(c)}{/each}
          {#if detCams.disabledCams.length}
            <div class="group small muted">detecção de movimento desligada</div>
            {#each detCams.disabledCams as c (c.id)}{@render tableRow(c)}{/each}
          {/if}
        </div>
        {#if detector}
          <div class="row wrap small muted queue-caption">
            <span><i class="slot free"></i>lugar livre</span>
            <span><i class="slot waiting"></i>esperando a vez</span>
            <span><i class="slot waiting filled"></i>lugares cheios: a próxima é descartada</span>
            <span><i class="processing"></i>imagem sendo processada pelo detector</span>
          </div>
        {/if}
      </div>
    {/if}
  {/if}

  <!-- This browser: the side of whoever is looking, which the server cannot
       see. "Copy" is what makes the section useful from afar - the person sends
       the text in a chat instead of describing the phone. -->
  <DiagnosticsCard
    title="Este navegador"
    groups={groupsWithClock}
    bind:isOpen={browserOpened}
    text={browserText}
  />

  {#if admin}
    <!-- This server: the machine that records, seen from the inside. It answers
         "does the machine have the headroom to record?", which the cameras
         table above does not answer - it only shows the consequence. -->
    <DiagnosticsCard
      title="Este servidor"
      subtitle={serverCollected}
      groups={serverGroups}
      bind:isOpen={serverOpened}
      text={serverText}
    >
      {#if emailEnabled && EmailTest}<EmailTest />{/if}
      {#if serverLog}
        <section class="log">
          <p class="title muted">
            Últimos avisos e erros do log
            {#if serverLog.total > serverLog.lines.length}({serverLog.lines.length} de {serverLog.total}){/if}
          </p>
          {#if serverLog.lines.length}
            <ol class="mono small">
              {#each serverLog.lines as l, i (i)}
                <li class:error={l.level === 'ERROR'}>{logLine(l)}</li>
              {/each}
            </ol>
          {:else}
            <p class="muted small">Nenhum desde que o dwnvr subiu.</p>
          {/if}
        </section>
      {/if}
    </DiagnosticsCard>
    {#if serverOpened && serverError}
      <p class="muted small">Não deu para ler o servidor: {serverError}</p>
    {/if}

    <HealthFooter />
  {/if}

  <!-- The only place where the version in use appears outside the login screen,
       and therefore where people look for the version: the new one, when there
       is one, shows up next to it, besides the header pill. The links apply
       even without a version - an old build does not carry it, and the
       repository stays at the same address. -->
  <div class="card about">
    <p class="row wrap version">
      <strong>dwnvr</strong>
      {#if build.version}
        <span class="muted small"
          ><span class="mono">{build.version}</span>{#if compiledAt}{' · '}compilado em {compiledAt}{/if}{#if build.newOne}{' · '}<a
              href={RELEASES_URL}
              target="_blank"
              rel="noopener">{build.newOne} disponível</a
            >{/if}</span
        >
      {/if}
    </p>
    <div class="links">
      <a href={REPO_URL} target="_blank" rel="noopener"><GitHubMark />Código no GitHub</a>
      <a href={RELEASES_URL} target="_blank" rel="noopener">Notas de versão</a>
      <a href={ISSUES_URL} target="_blank" rel="noopener">Reportar problema</a>
    </div>
  </div>
</div>

<style>
  .page {
    display: grid;
    gap: 10px;
    padding: 10px;
    max-width: 900px;
    margin: 0 auto;
  }

  /* The :not(.row) matters: this rule is scoped, and scoping in Svelte adds a
     class to the selector - so it beats the global .row on specificity. Without
     the exception, every card that declares itself a row (the totals, "nenhum
     problema", the status strip) silently became a column. */
  .card:not(.row) {
    display: grid;
    gap: 10px;
  }

  /* Shorter than the other cards: it is quick-read context, and should not
     compete for space with the disk just below. */
  .statusbar {
    padding: 8px 12px;
  }

  .meter {
    display: flex;
    height: 12px;
    border-radius: 999px;
    overflow: hidden;
    background: #1b1f24;
  }
  .seg.dwnvr {
    background: var(--accent);
  }
  .seg.orphans {
    background: var(--warn);
  }
  /* An orphan of a few MB on a disk of hundreds of GB would give a 0px stretch:
     the legend's yellow would have no match in the bar. */
  .seg.orphans.visible {
    min-width: 4px;
  }
  .seg.others {
    background: #3d444d;
  }

  .legend {
    gap: 14px;
  }
  .legend i {
    display: inline-block;
    width: 9px;
    height: 9px;
    border-radius: 2px;
    vertical-align: -1px;
  }
  .legend i.dwnvr {
    background: var(--accent);
  }
  .legend i.orphans {
    background: var(--warn);
  }
  .legend i.others {
    background: #3d444d;
  }

  .totals {
    justify-content: space-around;
    text-align: center;
    gap: 18px;
  }
  .big {
    font-size: 22px;
    font-weight: 600;
  }

  /* ---- about ---- */

  .about .version {
    margin: 0;
    gap: 4px 8px;
  }

  /* Button-like, and not a link loose in a sentence: they are destinations, and
     on the phone the finger needs a target. */
  .about .links {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .about .links a {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-height: 34px;
    padding: 6px 12px;
    border-radius: 8px;
    border: 1px solid var(--line);
    background: var(--panel-2);
    color: var(--fg);
    text-decoration: none;
    font-size: 13px;
  }
  @media (hover: hover) {
    .about .links a:hover {
      border-color: var(--accent);
    }
  }

  .notices p {
    margin: 0;
    gap: 8px;
    align-items: flex-start;
    font-size: 13px;
  }
  .notices p.bad {
    color: var(--bad);
  }
  .chip.stopped {
    color: var(--bad);
    border-color: #5c2b2b;
  }
  .notices p.warn {
    color: var(--warn);
  }
  .notices .dot {
    margin-top: 6px;
  }
  .notices p.ok {
    color: var(--ok);
  }

  /* ---- object recognition ---- */

  /* The card has no padding of its own: each half provides the spacing. The
     table needs to touch the edge to scroll sideways across its full width on
     the phone, and the summary needs the breathing room every card has. The
     zeroed gap is what makes the divider rule hug both halves instead of
     floating between them. */
  .detection {
    padding: 0;
    gap: 0;
  }
  .top {
    display: grid;
    gap: 10px;
    padding: 12px;
  }
  .det-table {
    border-top: 1px solid var(--line);
    overflow-x: auto;
  }

  /* The funnel bar. The same height as the disk bar will not do: here the
     pieces carry a number inside, and 12px would cut the digit. */
  .funnel {
    display: flex;
    height: 26px;
    border-radius: 6px;
    overflow: hidden;
    background: #1b1f24;
  }
  .funnel .seg {
    display: flex;
    /* A piece that exists has to show: 9 marks out of 4 thousand give a
       half-pixel slice, and the legend would point to a color that is not in
       the bar. The error this introduces in the width is smaller than the
       border. */
    min-width: 4px;
    align-items: center;
    justify-content: center;
    overflow: hidden;
    font-size: 12px;
    font-weight: 600;
    color: var(--dim);
  }
  /* Dark text only where the background is strong enough to carry it. */
  .funnel .seg.dark {
    color: #0d1117;
  }

  /* Spread across the whole row on desktop and stacked on the phone, which is
     what wrap already does - the gap is what keeps the numbers from sticking
     together. */
  .numbers {
    gap: 14px;
  }
  /* The number that did not fit in the piece comes attached to the legend
     label, and needs to read as a number and not as part of the text. */
  .legend b {
    color: var(--fg);
  }
  .numbers b {
    color: var(--fg);
    font-weight: 600;
  }
  .wrap {
    margin: 0;
  }
  .wrap b {
    color: var(--fg);
  }

  /* On the phone card, the queue goes in the corner of the name row: it is what
     changes from one moment to the next, and stays in view without searching. */
  .trow.detgrid > :first-child {
    grid-column: 1 / 3;
    grid-row: 1;
  }
  .trow.detgrid > .queue-cell {
    grid-column: 3;
    grid-row: 1;
    justify-self: end;
    align-self: center;
  }

  @media (min-width: 720px) {
    /* Columns: camera, inferences, with object, hit rate, discarded, lost, in
       queue. That is seven in a space that was already tight for six, so the
       numeric ones go at the minimum that still fits the header. */
    /* .thead and .trow are also classes, and their rule comes later in this
       sheet - on a specificity tie it would win and the grid would go back to
       the six columns of the cameras table. Both names together break the tie. */
    .thead.detgrid,
    .trow.detgrid {
      grid-template-columns:
        minmax(110px, 1.7fr) repeat(2, minmax(62px, 0.9fr)) minmax(58px, 0.8fr)
        repeat(2, minmax(68px, 0.9fr)) minmax(56px, 0.8fr);
    }
    .trow.detgrid > :first-child,
    .trow.detgrid > .queue-cell {
      grid-column: auto;
      grid-row: auto;
    }
    /* Numbers on the right: that is how a column is compared by eye. The name
       stays where it is, on the left - and the criterion is POSITION, not the
       `.name` class, which only the rows have: by class, the header's "câmera"
       would move right on its own, misaligned from the column it names. */
    .detgrid > :not(:first-child) {
      text-align: right;
    }
    /* The header is a button, and a button is flex: there what aligns is not
       text-align but justify-content, otherwise label and arrow stick to the
       left while the column's numbers are on the right. */
    .thead.detgrid .th:not(:first-child) {
      justify-content: flex-end;
    }
  }
  /* Zero is an answer, but not news: dimmed so the row that has a number stands
     out from the ones that do not. */
  .zero {
    color: #4a5058;
  }
  /* The camera without detection: the name stays legible, because it is what
     says whom the notice is about, and the numbers recede. */
  .c-disabled {
    gap: 8px;
    min-width: 0;
  }
  .disabled > :not(:first-child) {
    opacity: 0.5;
  }
  /* The label of the group of cameras without detection, in the tone of the
     headers: it is a division of the table, not one more row of it. */
  .group {
    padding: 12px 12px 6px;
    border-top: 1px solid var(--line);
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-size: 11px;
  }
  .alert {
    color: var(--warn);
  }

  /* A camera's queue slots. The size of the global .dot, to read as its
     relative; the free one is just an outline. */
  .slots {
    display: inline-flex;
    gap: 4px;
    justify-content: flex-end;
  }
  .slot {
    display: inline-block;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    border: 1.5px solid #4a5058;
  }
  .slot.waiting {
    background: var(--dim);
    border-color: var(--dim);
  }
  .full .slot.waiting,
  .slot.waiting.filled {
    background: var(--warn);
    border-color: var(--warn);
  }
  /* The camera being processed: outside the queue, so outside the slots - a
     separate dot, with a halo, so it does not read as one more slot. */
  .queue-cell {
    display: inline-flex;
    align-items: center;
    justify-content: flex-end;
  }
  .processing {
    display: inline-block;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--accent);
    box-shadow: 0 0 0 3px #2f81f733;
    margin-right: 9px;
  }
  .queue-caption {
    gap: 14px;
    padding: 8px 12px 10px;
    border-top: 1px solid #21262d;
  }
  .queue-caption .slot,
  .queue-caption .processing {
    margin-right: 6px;
    vertical-align: -1px;
  }

  /* ---- this server ---- */

  /* The log stays as a running list and not as label and value: each line is
     long, and right-aligned it would be unreadable. It scrolls inside so that
     50 lines do not push the rest of the screen. */
  .log .title {
    margin: 0 0 4px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-size: 11px;
  }
  .log ol {
    margin: 0;
    padding: 0;
    list-style: none;
    display: grid;
    gap: 4px;
    max-height: 260px;
    overflow-y: auto;
    overflow-wrap: anywhere;
  }
  .log li {
    color: var(--warn);
  }
  .log li.error {
    color: var(--bad);
  }

  .table {
    padding: 0;
    overflow-x: auto;
  }
  /* On the phone the two tables do not fit in columns and used to scroll
     sideways. Each row becomes a card: the name on top, and the values in a
     three-column grid, each with the column label above it. The grid is the
     same in every card, so a value is always in the same place - scanning down
     the "reconex." of each camera is still reading a column. With no header,
     sorting moves to the chips. */
  .thead,
  .trow {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 6px 10px;
    padding: 10px 12px;
    align-items: start;
  }
  .trow > :first-child {
    grid-column: 1 / -1;
  }
  /* The disk is the widest value ("19,93 GB de 20 GB") and takes the biggest
     share; if it still does not fit, it breaks before the "de", and not in the
     middle of a number with its unit. */
  .table .trow {
    grid-template-columns: minmax(0, 0.8fr) minmax(0, 1.3fr) minmax(0, 1fr);
  }
  .whole {
    white-space: nowrap;
  }
  .thead {
    display: none;
    border-bottom: 1px solid var(--line);
  }
  .rot-cel {
    display: block;
    font-size: 11px;
    line-height: 1.3;
    color: var(--dim);
  }
  .rot-cel.active {
    color: var(--accent);
  }
  .order {
    gap: 6px;
    padding: 10px 12px;
    border-bottom: 1px solid var(--line);
  }
  /* A chip and not a full button: below the global button's 44px, because there
     are seven side by side and they would wrap onto three lines. */
  .order .chip {
    min-height: 32px;
    padding: 4px 11px;
    color: var(--dim);
  }
  .order .chip.active {
    border-color: var(--accent);
    color: var(--fg);
    background: #2f81f71f;
  }
  .order .arrow {
    display: flex;
    color: var(--accent);
  }

  @media (min-width: 720px) {
    .order,
    .rot-cel {
      display: none;
    }
    .thead {
      display: grid;
    }
    .thead,
    .trow,
    .table .trow {
      /* Columns: name, rate, disk, retained, fit, reconex. The disk gets its
         own track because it carries two quantities ("12,91 GB de 20 GB") and
         in a uniform repeat it wrapped onto two lines before the others needed
         to. */
      grid-template-columns:
        minmax(130px, 2fr) minmax(70px, 1fr) minmax(125px, 1.6fr)
        repeat(2, minmax(80px, 1.1fr)) minmax(60px, 0.8fr);
      gap: 8px;
      padding: 9px 12px;
      align-items: center;
    }
    .trow > :first-child {
      grid-column: auto;
    }
  }
  /* The header is a button so it works with the keyboard, but still looks like
     a header: the global button style does not suit here. */
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
  .th:hover:not(:disabled) {
    border-color: transparent;
    color: var(--fg);
  }
  .th.active {
    color: var(--fg);
  }
  /* The dotted underline is the mark that there is an explanation there:
     without it the title exists and nobody finds out that pausing the mouse is
     enough. It stays in the line's color to hint, not to compete with the
     label. */
  .th .label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-decoration: underline dotted var(--line);
    text-underline-offset: 3px;
  }
  .th:hover .label,
  .th.active .label {
    text-decoration-color: var(--dim);
  }
  .th .arrow {
    display: flex;
    width: 12px;
    flex: none;
    color: var(--accent);
  }
  .trow + .trow {
    border-top: 1px solid #21262d;
  }
  .c-name {
    gap: 7px;
    min-width: 0;
  }
  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
