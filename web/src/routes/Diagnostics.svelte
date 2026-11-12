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
    dias,
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

  // Uma coluna das tabelas: sabe extrair o valor que a ordena. Texto passa
  // pelo colator; número, pela subtração.
  interface ColumnBase {
    id: string;
    label: string;
    help?: string;
  }
  type Column<T> = ColumnBase &
    ({ texto: true; value: (c: T) => string } | { texto?: false; value: (c: T) => number });
  // Nunca vazia: a primeira é a de quando a URL pede uma que não existe.
  type Columns<T> = readonly [Column<T>, ...Column<T>[]];

  interface Order {
    col: string;
    asc: boolean;
  }

  // Câmera com o funil do detector de objetos, que só vem com ele configurado.
  type WithFunnel = HealthCamera & { funil: Funnel };

  // O viewer vê só "Este navegador" e a versão: a saúde do servidor é
  // do admin, e a tela dele nem a pede. O papel não muda com a tela aberta -
  // sair desmonta tudo.
  const admin = isAdmin();
  if (admin) onDestroy(pollHealth());

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
  const clock = $derived.by(() => {
    const c = health.clock;
    if (!c) return null;
    const lido = Date.parse(c.now);
    if (isNaN(lido)) return null;
    return {
      when: timezoneClock(lido, c.offsetSeconds),
      timezone: c.zone ?? c.abbr,
      sigla: c.zone ? c.abbr : undefined,
    };
  });
  // As órfãs saem da conta de "outros": são gravação do dwnvr, só que de câmera
  // removida, e nenhum limite da retenção as apaga.
  const orphansBytes = $derived(disk?.orphanBytes ?? 0);
  const othersBytes = $derived(
    disk ? disk.totalBytes - disk.freeBytes - disk.dwnvrBytes - orphansBytes : 0,
  );
  const usedByDwnvr = $derived(disk ? disk.dwnvrBytes / disk.totalBytes : 0);
  const usedByOrphans = $derived(disk ? orphansBytes / disk.totalBytes : 0);
  const usedByOthers = $derived(disk ? othersBytes / disk.totalBytes : 0);

  // retidoMs é a retenção real: do segmento mais antigo em disco até agora.
  //
  // O servidor manda o instante, e não os dias já contados, porque com ele a
  // mesma resposta serve à tabela ("12 dias 4h") e ao card de câmeras ("desde
  // 31/07"). Câmera que nunca gravou não traz o campo, e o zero vira "-" no
  // duracao() em vez de "0s".
  function retainedMs(c: HealthCamera) {
    return c.oldestSegmentAt ? Date.now() - new Date(c.oldestSegmentAt).getTime() : 0;
  }

  // O cabeçalho explica O QUE a coluna mede, uma vez só; a célula responde
  // DESDE QUANDO, que é por câmera. undefined e não '' porque o Svelte omite o
  // atributo inteiro assim, e um title vazio deixaria a célula com sublinhado
  // de dica sem dica nenhuma.
  function sinceTitle(c: HealthCamera) {
    if (!c.oldestSegmentAt) return undefined;
    const since = `Desde ${ddmm(new Date(c.oldestSegmentAt).getTime())}`;
    const recorded = recordedWithGap(retainedMs(c), c.recordedMs);
    return recorded ? `${since}. ${duration(retainedMs(c) - recorded)} sem gravar.` : since;
  }

  // Cada coluna sabe extrair o valor que a ordena; assim o cabeçalho e a
  // ordenação não podem discordar sobre o que "disco" significa.
  //
  // A ajuda fica aqui junto, e não solta no template, porque é a mesma coisa: o
  // rótulo cabe em uma palavra e nenhuma delas ("retenção", "reconex.") diz
  // sozinha o que está sendo medido.
  const columns: Columns<HealthCamera> = [
    {
      id: 'name',
      label: 'câmera',
      value: (c) => c.name,
      texto: true,
      help: 'Nome da câmera. O ponto mostra o estado: verde gravando, vermelho parada ou desconectada. O horário ao lado é o do último segmento gravado.',
    },
    {
      id: 'bitrate',
      label: 'taxa',
      value: (c) => c.bitrateKbps || 0,
      help: 'Taxa de bits que está chegando da câmera agora, medida no fluxo gravado.',
    },
    {
      id: 'disco',
      label: 'disco',
      value: (c) => c.diskBytes || 0,
      help: 'Espaço que as gravações desta câmera ocupam hoje, somando todos os dias em disco, e a cota configurada para ela.',
    },
    {
      id: 'retido',
      label: 'retido',
      value: (c) => retainedMs(c),
      help: HELP_RETAINED,
    },
    {
      id: 'cabem',
      label: 'cabem',
      value: (c) => c.retainDays || 0,
      help: HELP_FIT,
    },
    {
      id: 'reconex',
      label: 'reconex.',
      value: (c) => c.reconnects || 0,
      help: 'Quantas vezes o dwnvr precisou reabrir a conexão desde o início da contagem: a subida do dwnvr, ou o último zerar. Número alto indica enlace instável com a câmera.',
    },
  ];

  // numeric para "cam2" vir antes de "cam10", que é como as câmeras costumam
  // ser nomeadas aqui.
  const collator = new Intl.Collator('pt-BR', { numeric: true, sensitivity: 'base' });

  // A ordenação vem da URL quando o link traz uma, e é validada contra as
  // colunas acima - `sort=inventado` cai no padrão em vez de deixar a tabela
  // sem critério nenhum.
  const params = currentParams();
  const urlSort = params.get('sort');

  let order = $state<Order>({
    col: urlSort !== null && columns.some((c) => c.id === urlSort) ? urlSort : 'name',
    asc: params.get('dir') !== 'desc',
  });

  // De volta para a URL, com o padrão - nome, crescente - ficando de fora: o
  // link não precisa carregar o que já vale sem ele.
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

  // As duas tabelas desta tela ordenam igual, então a regra mora num lugar só:
  // a coluna sabe extrair o valor que a ordena, texto passa pelo colator, e o
  // empate cai SEMPRE no nome - senão a tabela dança a cada leitura de 5 s,
  // que é o que acontece quando meia dúzia de câmeras tem o mesmo zero.
  function sortRows<T>(
    items: readonly T[],
    columns: Columns<T>,
    order: Order,
    nameOf: (c: T) => string,
  ) {
    const col = columns.find((c) => c.id === order.col) ?? columns[0];
    const dir = order.asc ? 1 : -1;
    // Cópia: a origem é estado global e a tela só quer uma visão dele.
    return [...items].sort((a, b) => {
      const d = col.texto
        ? collator.compare(col.value(a), col.value(b))
        : col.value(a) - col.value(b);
      return d ? d * dir : collator.compare(nameOf(a), nameOf(b));
    });
  }

  const rows = $derived(sortRows(health.cameras, columns, order, (c) => c.name));

  // A data vem em ISO/UTC do servidor; aqui interessa a hora local de quem lê.
  // Builds antigos podem não trazê-la, e um "Invalid Date" na tela seria pior
  // que não mostrar nada.
  const compiledAt = $derived.by(() => {
    const d = new Date(build.date);
    return build.date && !isNaN(d.getTime())
      ? d.toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' })
      : '';
  });

  // ---- este navegador ---------------------------------------------------
  //
  // Vem fechado e só coleta ao abrir: é consulta de quem está investigando um
  // defeito, e quem só veio ver as câmeras não paga por ela. Aberto, coleta de
  // novo quando a janela muda de tamanho - girar o celular muda a área da
  // página, que é justamente um dos números.
  // Muda pelo `bind:aberto` do card: o tipo vai explícito, senão ele seria
  // o `false` do valor inicial.
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

  // A hora do servidor para o viewer, que não lê o /api/health: o header Date
  // de uma resposta qualquer, lido ao abrir o card. { servidor, aqui } em ms.
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
        // Sem resposta, a linha do relógio só não aparece.
      });
    return () => {
      isLive = false;
    };
  });

  // O relógio do aparelho contra o do servidor. A hora da resposta, pela
  // latência, chega uns milissegundos atrasada, e a do header Date ainda vem
  // sem os milissegundos; abaixo de 5s é ruído, e acima é relógio errado, que
  // faz "agora" apontar para outro ponto da timeline.
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

  // ---- este servidor ----------------------------------------------------
  //
  // Mesmo trato do navegador: fechado, e só pergunta ao servidor com o card
  // aberto. A leitura custa um teste de escrita no storage e uma ida ao
  // go2rtc e ao detector de objetos, então o ritmo é mais lento que o do resto
  // da tela - temperatura e pressão mudam em dezenas de segundos, não em 5.
  //
  // A leitura de antes fica guardada porque a CPU do dwnvr sai da diferença
  // entre as duas. Fechar o card esquece ambas: reaberto uma hora depois, a
  // conta seria a média dessa hora, e não a de agora.
  const SERVER_POLL_MS = 15000;
  let serverOpened = $state<boolean>(false);
  let server = $state<Server | null>(null);
  let previousServer = $state<Server | null>(null);
  let serverError = $state('');

  $effect(() => {
    if (!serverOpened) return;
    let isLive = true;
    // Aba em segundo plano não pergunta: cada leitura grava no storage, e um
    // card esquecido aberto numa aba escondida faria isso a cada 15 s para
    // sempre. Ao voltar para a aba, lê na hora em vez de esperar o próximo tique.
    const ler = async () => {
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
    const lerJa = () => {
      void ler();
    };
    lerJa();
    const id = setInterval(lerJa, SERVER_POLL_MS);
    document.addEventListener('visibilitychange', lerJa);
    return () => {
      isLive = false;
      clearInterval(id);
      document.removeEventListener('visibilitychange', lerJa);
      server = previousServer = null;
    };
  });

  const serverGroups = $derived(server ? groupsFromServer(server, previousServer) : []);

  // O teste de e-mail é chunk à parte: só baixa com a seção smtp ligada.
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
  const serverCollected = $derived(server ? collectedAt(server.coletadoEm) : '');

  // O "copiar" leva a hora da coleta, e não a de quando se tocou no botão: o
  // texto descreve aquela leitura. Os espaços inquebráveis do subtítulo voltam
  // a ser espaço comum no texto colado.
  function serverText() {
    const collected = serverCollected.replaceAll(' ', ' ');
    const header = [
      `dwnvr ${build.version || '?'} - diagnóstico do servidor`,
      collected || new Date().toString(),
    ];
    const log = serverLog?.linhas.length
      ? [
          `[Últimos avisos e erros do log: ${serverLog.linhas.length} de ${serverLog.total}]`,
          ...serverLog.linhas.map(logLine),
        ]
      : [];
    return asText(serverGroups, header, log);
  }

  const totalKbps = $derived(health.cameras.reduce((a, c) => a + (c.bitrateKbps || 0), 0));
  const bytesPerDay = $derived(((totalKbps * 1000) / 8) * 86400);

  // ---- reconhecimento de objetos ----------------------------------------
  //
  // O desenho é um funil, sobre os números que o servidor dá: o `funil` de
  // cada câmera, contado desde que o dwnvr subiu. Câmera sem detector não traz o
  // campo, e sem nenhuma câmera com ele a seção inteira some, em vez de
  // aparecer zerada e parecer defeito.
  const withFunnel = $derived(health.cameras.filter((c): c is WithFunnel => c.funil !== undefined));
  // A fila, que é uma só para todas as câmeras, e os tempos das últimas
  // olhadas. Some junto com o detector.
  const detector = $derived(health.detector);

  // Os pedaços do funil de uma câmera. As fatias fecham a conta com os onsets
  // (ver `Funil` em internal/recorder/recorder.go).
  interface Outcomes {
    total: number;
    dropped: number;
    inferred: number;
    object: number;
    esperando: number;
    semVideo: number;
    recusados: number;
    falhas: number;
  }

  const OUTCOME_KEYS = [
    'total',
    'dropped',
    'inferred',
    'object',
    'esperando',
    'semVideo',
    'recusados',
    'falhas',
  ] as const satisfies readonly (keyof Outcomes)[];

  function outcomesOf(c: WithFunnel): Outcomes {
    const f = c.funil;
    return {
      total: c.onsets,
      dropped: f.descartados,
      inferred: f.semObjeto,
      object: f.comObjeto,
      esperando: f.naFila,
      semVideo: f.semVideo,
      recusados: f.recusados,
      falhas: f.falhas,
    };
  }

  // O funil é a história inteira de uma marca de movimento, e cada pedaço é um
  // desfecho DIFERENTE. Não somar tudo num "perdidas" só é a razão de esta
  // barra existir: "descartada" é a fila fazendo o papel de freio de propósito,
  // "olhou, nada novo" inclui o carro que já estava estacionado, e só o
  // vermelho quer dizer que uma marca ficou sem resposta.
  // A cor de cada desfecho mora junto do rótulo, e vai para a tela em `style`
  // em vez de virar classe. São duas razões: a barra e a legenda passam a ler
  // a MESMA definição, que é o que impede o quadradinho da legenda de
  // discordar do pedaço da barra; e o Svelte apaga do CSS toda regra que ele
  // não vê usada no template - uma classe montada em tempo de execução, como
  // `s-{id}`, ele não vê, e a barra sairia sem cor nenhuma.
  //
  // `escuro` diz se o número dentro do pedaço vai em texto escuro: só os três
  // pedaços de cor forte têm contraste para isso; nos dois cinzas ele sumiria.
  //
  // `sempre` mantém o desfecho na legenda mesmo zerado: "na fila" é estado de
  // agora, e vai de 0 a 1 e volta a cada poucos segundos - sumindo e voltando,
  // a legenda inteira pulava.
  interface Outcome {
    id: Exclude<keyof Outcomes, 'total' | 'semVideo' | 'recusados' | 'falhas'> | 'lost';
    color: string;
    dark: boolean;
    label: string;
    help?: string;
    always?: boolean;
  }

  const OUTCOMES: Outcome[] = [
    // A fila guarda poucas marcas por câmera: a que chega com a câmera já no
    // limite é descartada - não vai ao detector nem depois -, e fica só como
    // movimento na timeline.
    { id: 'dropped', color: '#3d444d', dark: false, label: 'descartada: fila da câmera cheia' },
    { id: 'inferred', color: 'var(--accent)', dark: true, label: 'olhou, nada novo' },
    { id: 'object', color: 'var(--ok)', dark: true, label: 'achou objeto' },
    // Cinza claro, e não outro tom do escuro: um cinza escuro fica quase igual
    // ao `#3d444d` do descartado, e os dois pedaços colam num borrão. Cor
    // nova de verdade também não serve - roxo e rosa já são veículo e animal
    // na timeline, e repeti-las aqui inventaria um parentesco que não existe.
    //
    // "Em andamento", e não "na fila": o pedaço junta tudo o que ainda não tem
    // resultado - a marca esperando o pico para ser cortada, as que esperam a
    // vez e a que está em análise. Chamado de "na fila", ele não batia com o
    // chip da fila nem com as bolinhas da tabela, que contam só as que
    // esperam a vez.
    {
      id: 'esperando',
      color: 'var(--dim)',
      dark: true,
      label: 'em andamento',
      help: 'Marcas que ainda não têm resultado: esperando o melhor quadro (até 3 s), esperando a vez na fila, ou em análise agora. O chip da fila e as bolinhas da tabela contam só as que esperam a vez.',
      always: true,
    },
    { id: 'lost', color: 'var(--bad)', dark: true, label: 'não analisadas' },
  ];

  // A largura da barra em pixels, medida pelo próprio navegador. Sem ela não
  // dá para saber se o número cabe no pedaço: a fatia é uma PORCENTAGEM, e 0,2%
  // são 2px no celular e 2px no desktop - o que muda é só quantos pixels há no
  // total. Nove marcas em quatro mil não cabem em lugar nenhum.
  let funnelWidth = $state(0);

  // Cabe o número dentro do pedaço? 12px em negrito dá ~8px por algarismo, mais
  // uma folga de cada lado para ele não encostar na divisa. Quando não cabe, o
  // número vai para a legenda em vez de ser cortado ao meio - foi assim que um
  // "9" apareceu como meio risco na tela.
  function fits(p: { n: number }) {
    if (!funnelWidth || !funnel) return false;
    return (p.n / funnel.total) * funnelWidth >= String(p.n).length * 8 + 8;
  }

  // Os tempos do detector em segundos com uma casa, e não pelo `duracao()`:
  // a espera na fila fica quase sempre abaixo de um segundo, e ele a
  // arredondaria para "0s".
  const segundos = (ms: number) => `${(ms / 1000).toFixed(1).replace('.', ',')}s`;

  // A soma das câmeras. O servidor manda por câmera, e o total é conta que a
  // tela faz: é uma soma de poucos números por câmera.
  const sum = $derived.by(() => {
    const s: Outcomes = {
      total: 0,
      dropped: 0,
      inferred: 0,
      object: 0,
      esperando: 0,
      semVideo: 0,
      recusados: 0,
      falhas: 0,
    };
    for (const c of withFunnel) {
      const d = outcomesOf(c);
      for (const k of OUTCOME_KEYS) s[k] += d[k];
    }
    return s;
  });

  const funnel = $derived.by(() => {
    if (!sum.total) return null;
    const lostCount = sum.semVideo + sum.recusados + sum.falhas;
    const amounts = { ...sum, lost: lostCount };
    // Só o que existe entra na barra. Na legenda também, menos o que é
    // `sempre`: legenda de pedaço que não foi desenhado manda procurar na tela
    // uma cor que não está lá, mas o "0" dele diz exatamente isso.
    const allItems = OUTCOMES.map((d) => ({ ...d, n: amounts[d.id] }));
    const clips = allItems.filter((d) => d.n > 0);
    const caption = allItems.filter((d) => d.n > 0 || d.always === true);
    return { pedacos: clips, caption, total: sum.total, lostCount };
  });

  // A quebra do pedaço vermelho, em palavras. Aparece só quando há o que
  // explicar, e cada motivo tem o seu - é aqui que "não analisadas" para de
  // ser um monte só.
  const unanalyzed = $derived(
    [
      { n: sum.semVideo, texto: 'sem vídeo inteiro para mandar, logo depois de conectar' },
      { n: sum.recusados, texto: 'vídeo corrompido vindo da câmera' },
      { n: sum.falhas, texto: 'o detector não respondeu' },
    ].filter((x) => x.n > 0),
  );

  // As colunas da tabela de detecção. Mesma forma das de cima: cada uma sabe
  // extrair o valor que a ordena e carrega a própria ajuda, para o cabeçalho e
  // a ordenação não terem como discordar sobre o que a coluna mede.
  //
  // O `??` do acerto não é detalhe: câmera nunca olhada não tem acerto, e sem
  // um valor a ordenação a jogaria em qualquer lugar. -1 põe o "sem dado"
  // antes de todo mundo no crescente.
  const detColumns: Columns<DetRow> = [
    { id: 'nome', label: 'câmera', value: (c) => c.nome, texto: true },
    {
      id: 'olhadas',
      label: 'olhadas',
      value: (c) => c.inferences,
      help: 'Marcas de movimento desta câmera que o detector olhou, desde que o dwnvr subiu.',
    },
    {
      id: 'objeto',
      label: 'com objeto',
      value: (c) => c.object,
      help: 'Destas, quantas viraram marca de pessoa, veículo ou animal na timeline.',
    },
    {
      id: 'acerto',
      label: 'acerto',
      value: (c) => c.hit ?? -1,
      help: 'A fatia das olhadas que achou algo. Perto de 100% costuma ser cena parada com objeto fixo no quadro, e não qualidade.',
    },
    {
      id: 'descartadas',
      label: 'descartadas',
      value: (c) => c.dropped,
      help: 'Marcas que chegaram com esta câmera já com o limite dela esperando na fila. Foram descartadas: não vão ao detector nem depois, e ficam só como movimento na timeline. É o freio que impede uma câmera agitada de tomar a vez das outras - ela só perde o que é dela.',
    },
    {
      id: 'perdidas',
      label: 'perdidas',
      value: (c) => c.lostCount,
      help: 'Marcas que ficaram sem resposta: sem vídeo inteiro para mandar, vídeo corrompido vindo da câmera, ou o detector que não respondeu.',
    },
    {
      id: 'fila',
      label: 'na fila',
      // Quem mais espera primeiro; o processamento desempata.
      value: (c) => c.fila.esperando * 2 + (c.fila.olhando ? 1 : 0),
      // Getter, e não texto fixo: o número de lugares vem do servidor, e o
      // cabeçalho relê a dica quando ele chega.
      get help() {
        const n = detector?.fila.porCamera ?? 2;
        return `Os lugares desta câmera na fila: até ${n} marcas esperando a vez (cinza). Com os ${n} ocupados (âmbar), a próxima marca desta câmera é descartada e fica só como movimento na timeline. O ponto azul, à parte, é a câmera cuja imagem o detector está processando agora - essa marca já saiu da fila.`;
      },
    },
  ];

  // Chaves próprias na URL (`dsort`/`ddir`): as duas tabelas da tela ordenam
  // sozinhas, e compartilhar `sort` faria uma mexer na outra.
  const urlSortDet = params.get('dsort');
  let detOrder = $state<Order>({
    col: urlSortDet !== null && detColumns.some((c) => c.id === urlSortDet) ? urlSortDet : 'nome',
    asc: params.get('ddir') !== 'desc',
  });

  $effect(() => {
    write({
      dsort: detOrder.col === 'nome' ? null : detOrder.col,
      ddir: detOrder.asc ? null : 'desc',
    });
  });

  function sortDet(id: string) {
    if (detOrder.col === id) detOrder.asc = !detOrder.asc;
    else detOrder = { col: id, asc: true };
  }

  // O que uma câmera tem na fila agora: os lugares, ocupados primeiro, e se
  // uma imagem dela está sendo processada. Essa já saiu da fila, então não
  // ocupa lugar - vai à parte. Câmera fora de `cameras` não tem nada.
  function queueOf(id: string) {
    const n = detector?.fila.porCamera ?? 0;
    const l = detector?.fila.cameras[id] ?? { esperando: 0, olhando: false };
    const slots = Array.from({ length: Math.max(n, l.esperando) }, (_, i) =>
      i < l.esperando ? 'waiting' : 'free',
    );
    return { ...l, slots, full: n > 0 && l.esperando >= n };
  }

  // Uma linha da tabela de detecção.
  function detRow(c: WithFunnel) {
    const d = outcomesOf(c);
    const inferences = d.inferred + d.object;
    return {
      id: c.id,
      nome: c.name || c.id,
      ...d,
      inferences,
      lostCount: d.semVideo + d.recusados + d.falhas,
      hit: inferences ? Math.round((d.object / inferences) * 100) : null,
      fila: queueOf(c.id),
      enabled: c.detect,
    };
  }
  type DetRow = ReturnType<typeof detRow>;

  const detCams = $derived.by(() => {
    const rows = withFunnel.map(detRow);
    // O servidor manda o funil de toda câmera assim que existe detector, com a
    // detecção de movimento dela ligada ou não. Desligada ela não marca nada, e
    // a linha zerada leria como "não viu nada". Fica na tabela, porque os
    // números sobrevivem a desligar no meio do dia, mas num grupo à parte, no
    // fim: a ordenação escolhida vale dentro de cada grupo.
    const sorted = sortRows(rows, detColumns, detOrder, (c) => c.nome);
    return {
      enabledCams: sorted.filter((c) => c.enabled),
      disabledCams: sorted.filter((c) => !c.enabled),
    };
  });

  // ---- avisos ------------------------------------------------------------

  // A lista mora em lib/avisos.ts, que o sino do header também usa: os dois
  // contam com a mesma regra.
  const now = $derived(serverNow(health));
  const notices = $derived(noticesOf(health));

  // O zerar só aparece quando há o que zerar.
  const hasReconnection = $derived(health.cameras.some((c) => c.reconnects > 0));

  // "contando desde" no título só quando todas as câmeras no ar contam do mesmo
  // ponto - a subida do dwnvr, ou o último zerar. Câmera reiniciada por mudança
  // de configuração conta do próprio reinício, e aí o desde de cada aviso é o
  // que vale.
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
        <span class="mono">{duration(health.uptime.appSeconds * 1000)}</span>
        {#if health.uptime.machineSeconds}
          <span class="muted">·</span>
          <span class="muted">máquina há</span>
          <span class="mono">{duration(health.uptime.machineSeconds * 1000)}</span>
        {/if}
        <!-- Horário do servidor no fuso do servidor, e não no do navegador: é
             assim que se descobre relógio ou TZ errado na máquina que grava, que
             é a que carimba o nome dos segmentos. -->
        {#if clock}
          <span class="muted">·</span>
          <span class="muted">horário do servidor</span>
          <span class="mono" title={clock.sigla}>{clock.when} {clock.timezone}</span>
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
          <!-- Só com órfã: a legenda permanente sugeriria que sobrar gravação sem
               câmera é o normal. Quem apaga é a tela de Câmeras, não a retenção. -->
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

    <!-- O card existe também sem aviso nenhum, com o "nenhum problema": a área
         não muda de cara, e o zerar continua à mão para a coluna reconex. -->
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
        <!-- eslint-disable-next-line svelte/require-each-key -- só texto, sem estado por linha; e dois avisos podem ter o mesmo texto, o que quebraria a chave -->
        {#each notices as a}
          <p class="row {a.nivel}"><span class="dot {a.nivel}"></span>{a.texto}</p>
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

    <!-- A ordenação no celular: o cabeçalho de colunas não existe lá, então cada
         coluna vira um chip. Mesmo estado e mesma URL do cabeçalho do desktop. -->
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

    <!-- O rótulo de cada valor no cartão do celular, tirado da mesma coluna que
         nomeia o chip. No desktop some: lá quem nomeia é o cabeçalho. -->
    {#snippet label(cols: readonly ColumnBase[], o: Order, id: string)}
      <span class="rot-cel" class:active={o.col === id}>{cols.find((c) => c.id === id)?.label}</span
      >
    {/snippet}

    <div class="table card">
      {@render chipsOrder(columns, order, sortBy)}
      <div class="thead row small muted">
        {#each columns as col (col.id)}
          <!-- O title fica no botão inteiro, e não só no rótulo: a área de
               passagem do mouse é a célula do cabeçalho, que é onde a pessoa
               para para decidir se clica. -->
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
            <!-- A seta ocupa lugar sempre, senão o cabeçalho pula a cada clique. -->
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
            <!-- Conectada mas sem gravar é o estado traiçoeiro: o ponto verde
                 dizia "tudo bem" enquanto a câmera não produzia nada. -->
            {#if c.silent}<span class="chip stopped">não grava</span>
            {:else if c.lastSegmentAt}<span class="chip"
                >{hhmmss(new Date(c.lastSegmentAt).getTime())}</span
              >{/if}
            {#if c.hasAudio}<span class="chip">áudio</span>{/if}
          </span>
          <span class="mono">{@render label(columns, order, 'bitrate')}{kbps(c.bitrateKbps)}</span>
          <!-- Uso e cota juntos, como no chip da tela de Câmeras: o número
               sozinho não diz se é muito ou pouco para esta câmera. -->
          <span class="mono"
            >{@render label(columns, order, 'disco')}<span class="whole">{bytes(c.diskBytes)}</span>
            de <span class="whole">{mbToBytes(c.quotaMB)}</span></span
          >
          <!-- O gravado vai em linha própria, e só com buraco: na câmera sem
               falha a célula fica como sempre foi, e a linha a mais é o próprio
               sinal de que algo faltou. -->
          <span class="mono" title={sinceTitle(c)}>
            {@render label(columns, order, 'retido')}{duration(retainedMs(c))}
            {#if recordedWithGap(retainedMs(c), c.recordedMs)}
              <br /><span class="muted small">{duration(c.recordedMs)} grav.</span>
            {/if}
          </span>
          <span class="mono">{@render label(columns, order, 'cabem')}{dias(c.retainDays)}</span>
          <span class="mono">{@render label(columns, order, 'reconex')}{c.reconnects}</span>
        </div>
      {/each}
      {#if !health.cameras.length}
        <!-- Duas ausências diferentes moravam no mesmo "sem dados ainda": o
             servidor que ainda não respondeu e a instalação sem câmera nenhuma.
             `updatedAt` só sai de 0 quando uma leitura volta, e a resposta traz
             toda câmera cadastrada - inclusive a desabilitada. -->
        {#if health.updatedAt}
          <NoCameras />
        {:else}
          <p class="empty">sem dados ainda</p>
        {/if}
      {/if}
    </div>

    <!-- Reconhecimento de objetos. Some inteiro quando nenhuma câmera tem
         detector configurado.

         Um card só, e não dois: o funil e a tabela são a mesma coisa vista de
         longe e de perto. Dois cards sugeriam dois assuntos; o que separa os
         dois aqui é um risco, não um vão. -->
    {#if withFunnel.length}
      <div class="card detection">
        <div class="top">
          <div class="row wrap">
            <strong>Reconhecimento de objetos</strong>
            <span class="spacer"></span>
            <!-- Os números zeram quando o dwnvr reinicia: sem dizer desde quando
                 eles contam, "12 com objeto" não é muito nem pouco. -->
            {#if health.uptime}
              <span class="muted small"
                >desde que o dwnvr subiu, há {duration(health.uptime.appSeconds * 1000)}</span
              >
            {/if}
            {#if detector}
              <span
                class="chip mono"
                title="marcas esperando a vez agora, e o teto: {detector.fila
                  .porCamera} por câmera com detecção ligada"
              >
                fila {detector.fila.agora}/{detector.fila.cap}
              </span>
              <span
                class="chip mono"
                title="o maior tamanho que a fila já teve desde que o dwnvr subiu"
              >
                pico {detector.fila.pico}
              </span>
            {/if}
          </div>

          {#if funnel}
            <!-- Uma barra só conta a história inteira: de tudo que as câmeras
                 marcaram, o que foi descartado, o que o detector olhou, o que virou
                 objeto e o que ficou sem resposta. É a única forma desta tela de
                 responder POR QUE uma marca não virou objeto sem precisar de
                 prosa. -->
            <div
              class="funnel"
              bind:clientWidth={funnelWidth}
              title="o que aconteceu com cada marca de movimento"
            >
              {#each funnel.pedacos as p (p.id)}
                <span
                  class="seg"
                  class:dark={p.dark}
                  style:width="{(p.n / funnel.total) * 100}%"
                  style:background={p.color}>{fits(p) ? p.n : ''}</span
                >
              {/each}
            </div>

            <!-- O número acompanha o rótulo quando não coube no pedaço. Assim
                 cada número aparece exatamente uma vez, e nenhum aparece pela
                 metade. -->
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
              <!-- Dois tempos, e não um: analisar é o detector olhando, a espera
                   é quanto a marca ficou parada na fila antes dele - e é a que
                   cresce quando o aparelho não dá conta. Zero é "ainda não
                   olhou nenhuma", e um "0,0s" ali diria que é instantâneo. -->
              {#if detector?.tempos.analiseMs}
                <span
                  >analisar leva <b class="mono">{segundos(detector.tempos.analiseMs)}</b> por marca</span
                >
                <span
                  >espera de cada marca na fila <b class="mono"
                    >{segundos(detector.tempos.esperaMs)}</b
                  ></span
                >
              {/if}
            </div>

            {#if unanalyzed.length}
              <p class="small muted wrap">
                das {funnel.lostCount} não analisadas:
                {#each unanalyzed as m, i (m.texto)}{i ? ' · ' : ' '}<b class="mono">{m.n}</b>
                  {m.texto}{/each}
              </p>
            {/if}
          {:else}
            <p class="small muted wrap">nenhuma marca de movimento desde que o dwnvr subiu</p>
          {/if}
        </div>

        <div class="table">
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
                <!-- A seta ocupa lugar sempre, senão o cabeçalho pula a cada clique. -->
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
                <span class="name">{c.nome}</span>
              {:else}
                <span class="c-disabled row">
                  <span class="name">{c.nome}</span>
                  <a class="small" href="#cams?editar={encodeURIComponent(c.id)}">ligar</a>
                </span>
              {/if}
              <span class="mono" class:zero={!c.inferences}
                >{@render label(detColumns, detOrder, 'olhadas')}{c.inferences}</span
              >
              <span class="mono" class:zero={!c.object}
                >{@render label(detColumns, detOrder, 'objeto')}{c.object}</span
              >
              <!-- Quase tudo com objeto não é bom sinal: costuma ser um objeto
                   parado dentro do quadro, e aí o reconhecimento está confirmando
                   sempre a mesma coisa em vez de avisar de algo novo. -->
              <span class="mono" class:zero={!c.object} class:alert={c.hit !== null && c.hit >= 90}>
                {@render label(detColumns, detOrder, 'acerto')}{c.hit === null ? '-' : `${c.hit}%`}
              </span>
              <span class="mono" class:zero={!c.dropped}
                >{@render label(detColumns, detOrder, 'descartadas')}{c.dropped}</span
              >
              <span class="mono" class:zero={!c.lostCount} class:alert={c.lostCount > 0}
                >{@render label(detColumns, detOrder, 'perdidas')}{c.lostCount}</span
              >
              <!-- Os lugares da câmera, e não um número: cheio ou vazio se lê
                   de relance. Âmbar quando todos estão ocupados, que é quando a
                   próxima marca dela é descartada. O azul fica à esquerda deles,
                   e separado: a imagem em processamento não está na fila. Sem
                   detecção a câmera não entra na fila, e lugares vazios ali
                   diriam que ela está esperando a vez. -->
              <span class="queue-cell">
                {#if c.enabled}
                  {#if c.fila.olhando}
                    <span
                      class="processing"
                      title="o detector está processando uma imagem desta câmera agora"
                    ></span>
                  {/if}
                  <span
                    class="slots"
                    class:full={c.fila.full}
                    title="{c.fila.esperando} de {c.fila.slots.length} lugares ocupados"
                  >
                    {#each c.fila.slots as l, i (i)}<span class="slot {l}"></span>{/each}
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

  <!-- Este navegador: o lado de quem olha, que o servidor não enxerga. O
       "copiar" é o que torna a seção útil de longe - a pessoa manda o texto
       numa conversa em vez de descrever o celular. -->
  <DiagnosticsCard
    title="Este navegador"
    groups={groupsWithClock}
    bind:isOpen={browserOpened}
    text={browserText}
  />

  {#if admin}
    <!-- Este servidor: a máquina que grava, vista de dentro. Responde "a
         máquina tem fôlego para gravar?", que a tabela de câmeras acima não
         responde - ela só mostra a consequência. -->
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
            {#if serverLog.total > serverLog.linhas.length}({serverLog.linhas.length} de {serverLog.total}){/if}
          </p>
          {#if serverLog.linhas.length}
            <ol class="mono small">
              {#each serverLog.linhas as l, i (i)}
                <li class:error={l.nivel === 'ERROR'}>{logLine(l)}</li>
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

  <!-- Único lugar onde a versão em uso aparece fora da tela de login, e por
       isso é onde se procura versão: a nova, quando há, entra ao lado dela,
       além da pílula do header. Os links valem mesmo sem versão - build
       antigo não a traz, e o repositório continua no mesmo endereço. -->
  <div class="card about">
    <p class="row wrap versao">
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

  /* O :not(.row) importa: esta regra é escopada, e escopo no Svelte acrescenta
     uma classe ao seletor - ou seja, ela ganha do .row global por
     especificidade. Sem a ressalva, todo card que se declara linha (os totais,
     o "nenhum problema", a faixa de estado) virava coluna calado. */
  .card:not(.row) {
    display: grid;
    gap: 10px;
  }

  /* Mais baixa que os outros cards: é contexto de leitura rápida, não deve
     disputar espaço com o disco logo abaixo. */
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
  /* Órfã de poucos MB num disco de centenas de GB daria um trecho de 0px: o
     amarelo da legenda não teria par na barra. */
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

  /* ---- sobre ---- */

  .about .versao {
    margin: 0;
    gap: 4px 8px;
  }

  /* Com cara de botão, e não de link solto numa frase: são destinos, e no
     celular o dedo precisa de alvo. */
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

  /* ---- reconhecimento de objetos ---- */

  /* O card não tem padding próprio: quem espaça é cada metade. A tabela
     precisa encostar na borda para rolar de lado inteira no celular, e o
     resumo precisa do respiro que todo card tem. O gap zerado é o que faz o
     risco divisor colar nas duas metades em vez de flutuar entre elas. */
  .detection {
    padding: 0;
    gap: 0;
  }
  .top {
    display: grid;
    gap: 10px;
    padding: 12px;
  }
  .table {
    border-top: 1px solid var(--line);
    overflow-x: auto;
  }

  /* A barra do funil. Mesma altura da barra de disco não serve: aqui os
     pedaços carregam número dentro, e 12px cortaria o algarismo. */
  .funnel {
    display: flex;
    height: 26px;
    border-radius: 6px;
    overflow: hidden;
    background: #1b1f24;
  }
  .funnel .seg {
    display: flex;
    /* Pedaço que existe tem de aparecer: 9 marcas em 4 mil dão fatia de meio
       pixel, e a legenda ficaria apontando para uma cor que não está na
       barra. O erro que isso introduz na largura é menor que a borda. */
    min-width: 4px;
    align-items: center;
    justify-content: center;
    overflow: hidden;
    font-size: 12px;
    font-weight: 600;
    color: var(--dim);
  }
  /* Texto escuro só onde o fundo é forte o bastante para sustentá-lo. */
  .funnel .seg.dark {
    color: #0d1117;
  }

  /* Espalhados na linha inteira no desktop e empilhados no celular, que é o
     que o wrap já faz - o gap é o que impede os números de se colarem. */
  .numbers {
    gap: 14px;
  }
  /* O número que não coube no pedaço vem colado no rótulo da legenda, e
     precisa ler como número e não como parte do texto. */
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

  /* No cartão do celular, a fila vai no canto da linha do nome: é o que
     muda de um instante para outro, e fica à vista sem procurar. */
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
    /* Colunas: câmera, olhadas, com objeto, acerto, descartadas, perdidas, na
       fila. São sete num espaço que já era apertado para seis, então as
       numéricas vão no mínimo que ainda cabe o cabeçalho. */
    /* .thead e .trow também são classe, e a regra deles vem depois nesta folha -
       no empate de especificidade ela ganharia e a grade voltaria a ter as seis
       colunas da tabela de câmeras. Os dois nomes juntos desempatam. */
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
    /* Números à direita: é assim que se compara uma coluna de olho. O nome fica
       onde está, à esquerda - e o critério é a POSIÇÃO, não a classe `.nome`,
       que só as linhas têm: por classe, o "câmera" do cabeçalho ia para a
       direita sozinho, desalinhado da coluna que ele nomeia. */
    .detgrid > :not(:first-child) {
      text-align: right;
    }
    /* O cabeçalho é botão, e botão é flex: nele o que alinha não é o text-align
       e sim o justify-content, senão rótulo e seta ficam colados à esquerda
       enquanto os números da coluna estão à direita. */
    .thead.detgrid .th:not(:first-child) {
      justify-content: flex-end;
    }
  }
  /* Zero é resposta, mas não é notícia: apagado para a linha que tem número
     saltar dentre as que não têm. */
  .zero {
    color: #4a5058;
  }
  /* A câmera sem detecção: o nome continua legível, porque é ele que diz de
     quem é o aviso, e os números recuam. */
  .c-disabled {
    gap: 8px;
    min-width: 0;
  }
  .disabled > :not(:first-child) {
    opacity: 0.5;
  }
  /* O rótulo do grupo das câmeras sem detecção, no tom dos cabeçalhos: é
     divisão da tabela, não mais uma linha dela. */
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

  /* Os lugares de uma câmera na fila. Do tamanho do .dot global, para ler
     como parente dele; o livre é só o contorno. */
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
  /* A câmera em processamento: fora da fila, então fora dos lugares - um
     ponto à parte, com halo, para não ler como mais um lugar. */
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

  /* ---- este servidor ---- */

  /* O log fica em lista corrida e não em rótulo e valor: cada linha é longa,
     e alinhada à direita ficaria ilegível. Rola por dentro para 50 linhas não
     empurrarem o resto da tela. */
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
  /* No celular as duas tabelas não cabem em colunas e rolavam de lado. Cada
     linha vira um cartão: o nome em cima, e os valores numa grade de três,
     cada um com o rótulo da coluna em cima dele. A grade é a mesma em todos
     os cartões, então um valor fica sempre no mesmo lugar - descer o olho
     pelo "reconex." de cada câmera continua sendo ler uma coluna. Sem
     cabeçalho, a ordenação vai para os chips. */
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
  /* O disco é o valor mais largo ("19,93 GB de 20 GB") e leva a fatia maior;
     se ainda assim não couber, quebra antes do "de", e não no meio de um
     número com a unidade. */
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
  /* Chip e não botão cheio: abaixo dos 44px do botão global, porque são
     sete lado a lado e quebrariam em três linhas. */
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
    .trow > :first-child {
      grid-column: auto;
    }
  }
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
  .th:hover:not(:disabled) {
    border-color: transparent;
    color: var(--fg);
  }
  .th.active {
    color: var(--fg);
  }
  /* O sublinhado pontilhado é a marca de que há explicação ali: sem ele o
     title existe e ninguém descobre que basta parar o mouse. Fica na cor da
     linha para insinuar, não para competir com o rótulo. */
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
