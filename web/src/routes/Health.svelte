<script>
  import { onDestroy } from 'svelte';
  import { health, pollHealth, HEALTH_POLL_MS, cameras, build } from '../lib/state.svelte.js';
  import { paramsAtuais, escrever } from '../lib/rota.svelte.js';
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
  import SemCameras from '../components/SemCameras.svelte';
  import { coletar, comoTexto, copiar } from '../lib/navegador.js';

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

  // A ordenação vem da URL quando o link traz uma, e é validada contra as
  // colunas acima - `sort=inventado` cai no padrão em vez de deixar a tabela
  // sem critério nenhum.
  const params = paramsAtuais();
  const sortDaURL = params.get('sort');

  let ordem = $state({
    col: colunas.some((c) => c.id === sortDaURL) ? sortDaURL : 'name',
    asc: params.get('dir') !== 'desc',
  });

  // De volta para a URL, com o padrão - nome, crescente - ficando de fora: o
  // link não precisa carregar o que já vale sem ele.
  $effect(() =>
    escrever({
      sort: ordem.col === 'name' ? null : ordem.col,
      dir: ordem.asc ? null : 'desc',
    }),
  );

  function ordenar(id) {
    if (ordem.col === id) ordem.asc = !ordem.asc;
    else ordem = { col: id, asc: true };
  }

  // As duas tabelas desta tela ordenam igual, então a regra mora num lugar só:
  // a coluna sabe extrair o valor que a ordena, texto passa pelo colator, e o
  // empate cai SEMPRE no nome - senão a tabela dança a cada leitura de 5 s,
  // que é o que acontece quando meia dúzia de câmeras tem o mesmo zero.
  function ordena(itens, colunas, ordem, nomeDe) {
    const col = colunas.find((c) => c.id === ordem.col) ?? colunas[0];
    const dir = ordem.asc ? 1 : -1;
    // Cópia: a origem é estado global e a tela só quer uma visão dele.
    return [...itens].sort((a, b) => {
      const va = col.valor(a);
      const vb = col.valor(b);
      const d = col.texto ? colator.compare(va, vb) : va - vb;
      return d ? d * dir : colator.compare(nomeDe(a), nomeDe(b));
    });
  }

  const linhas = $derived(ordena(health.cameras, colunas, ordem, (c) => c.name || ''));

  // A data vem em ISO/UTC do servidor; aqui interessa a hora local de quem lê.
  // Builds antigos podem não trazê-la, e um "Invalid Date" na tela seria pior
  // que não mostrar nada.
  const compiladoEm = $derived.by(() => {
    const d = new Date(build.date);
    return build.date && !isNaN(d) ? d.toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'short' }) : '';
  });

  // ---- este navegador ---------------------------------------------------
  //
  // Vem fechado e só coleta ao abrir: é consulta de quem está investigando um
  // defeito, e quem só veio ver as câmeras não paga por ela. Aberto, coleta de
  // novo quando a janela muda de tamanho - girar o celular muda a área da
  // página, que é justamente um dos números.
  let navegadorAberto = $state(false);
  let grupos = $state([]);
  let copiado = $state('');

  $effect(() => {
    if (!navegadorAberto) return;
    const recoletar = async () => (grupos = await coletar());
    recoletar();
    let t;
    const mudou = () => {
      clearTimeout(t);
      t = setTimeout(recoletar, 250);
    };
    addEventListener('resize', mudou);
    return () => {
      clearTimeout(t);
      removeEventListener('resize', mudou);
    };
  });

  // O relógio do aparelho contra o do servidor. A hora da resposta, pela
  // latência, chega uns milissegundos atrasada; abaixo de 5s é ruído, e acima
  // é relógio errado, que faz "agora" apontar para outro ponto da timeline.
  const diferencaRelogio = $derived.by(() => {
    const lido = Date.parse(health.clock?.now);
    if (isNaN(lido) || !health.updatedAt) return null;
    const d = Math.round((health.updatedAt - lido) / 1000);
    if (Math.abs(d) < 5) return { valor: 'igual ao do servidor' };
    return { valor: `${duracao(Math.abs(d) * 1000)} ${d > 0 ? 'adiantado' : 'atrasado'}`, alerta: true };
  });

  const gruposComRelogio = $derived(
    grupos.map((g) =>
      g.titulo === 'Ambiente' && diferencaRelogio
        ? { ...g, itens: [...g.itens, { rotulo: 'relógio do aparelho', ...diferencaRelogio }] }
        : g,
    ),
  );

  async function copiarDiagnostico() {
    const cabecalho = [`dwnvr ${build.version || '?'} - diagnóstico do navegador`, new Date().toString()];
    const ok = await copiar(comoTexto(gruposComRelogio, cabecalho));
    copiado = ok ? 'copiado' : 'não deu para copiar';
    setTimeout(() => (copiado = ''), 2500);
  }

  const totalKbps = $derived(health.cameras.reduce((a, c) => a + (c.bitrateKbps || 0), 0));
  const bytesPorDia = $derived(((totalKbps * 1000) / 8) * 86400);
  const desconectadas = $derived(health.cameras.filter((c) => c.enabled && !c.connected));
  const paradas = $derived(health.cameras.filter((c) => c.enabled && c.silent));

  // ---- reconhecimento de objetos ----------------------------------------
  //
  // O desenho é um funil, sobre os números que o servidor dá: o `funil` de
  // cada câmera, contado desde que o dwnvr subiu. Câmera sem detector não traz o
  // campo, e sem nenhuma câmera com ele a seção inteira some, em vez de
  // aparecer zerada e parecer defeito.
  const comFunil = $derived(health.cameras.filter((c) => c.funil));
  // A fila, que é uma só para todas as câmeras, e os tempos das últimas
  // olhadas. Some junto com o detector.
  const detector = $derived(health.detector);

  // Os pedaços do funil de uma câmera. As fatias fecham a conta com os onsets
  // (ver `Funil` em internal/recorder/recorder.go).
  function desfechosDe(c) {
    const f = c.funil;
    return {
      total: c.onsets,
      descartado: f.descartados,
      olhado: f.semObjeto,
      objeto: f.comObjeto,
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
  const DESFECHOS = [
    // A fila guarda poucas marcas por câmera: a que chega com a câmera já no
    // limite é descartada - não vai ao detector nem depois -, e fica só como
    // movimento na timeline.
    { id: 'descartado', cor: '#3d444d', escuro: false, rotulo: 'descartada: fila da câmera cheia' },
    { id: 'olhado', cor: 'var(--accent)', escuro: true, rotulo: 'olhou, nada novo' },
    { id: 'objeto', cor: 'var(--ok)', escuro: true, rotulo: 'achou objeto' },
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
      cor: 'var(--dim)',
      escuro: true,
      rotulo: 'em andamento',
      ajuda: 'Marcas que ainda não têm resultado: esperando o melhor quadro (até 3 s), esperando a vez na fila, ou em análise agora. O chip da fila e as bolinhas da tabela contam só as que esperam a vez.',
      sempre: true,
    },
    { id: 'perdido', cor: 'var(--bad)', escuro: true, rotulo: 'não analisadas' },
  ];

  // A largura da barra em pixels, medida pelo próprio navegador. Sem ela não
  // dá para saber se o número cabe no pedaço: a fatia é uma PORCENTAGEM, e 0,2%
  // são 2px no celular e 2px no desktop - o que muda é só quantos pixels há no
  // total. Nove marcas em quatro mil não cabem em lugar nenhum.
  let larguraFunil = $state(0);

  // Cabe o número dentro do pedaço? 12px em negrito dá ~8px por algarismo, mais
  // uma folga de cada lado para ele não encostar na divisa. Quando não cabe, o
  // número vai para a legenda em vez de ser cortado ao meio - foi assim que um
  // "9" apareceu como meio risco na tela.
  function cabe(p) {
    if (!larguraFunil || !funil) return false;
    return (p.n / funil.total) * larguraFunil >= String(p.n).length * 8 + 8;
  }

  // Os tempos do detector em segundos com uma casa, e não pelo `duracao()`:
  // a espera na fila fica quase sempre abaixo de um segundo, e ele a
  // arredondaria para "0s".
  const segundos = (ms) => `${(ms / 1000).toFixed(1).replace('.', ',')}s`;

  // A soma das câmeras. O servidor manda por câmera, e o total é conta que a
  // tela faz: é uma soma de poucos números por câmera.
  const soma = $derived.by(() => {
    const s = { total: 0, descartado: 0, olhado: 0, objeto: 0, esperando: 0, semVideo: 0, recusados: 0, falhas: 0 };
    for (const c of comFunil) {
      const d = desfechosDe(c);
      for (const k in s) s[k] += d[k];
    }
    return s;
  });

  const funil = $derived.by(() => {
    if (!soma.total) return null;
    const perdidas = soma.semVideo + soma.recusados + soma.falhas;
    const quanto = { ...soma, perdido: perdidas };
    // Só o que existe entra na barra. Na legenda também, menos o que é
    // `sempre`: legenda de pedaço que não foi desenhado manda procurar na tela
    // uma cor que não está lá, mas o "0" dele diz exatamente isso.
    const todos = DESFECHOS.map((d) => ({ ...d, n: quanto[d.id] }));
    const pedacos = todos.filter((d) => d.n > 0);
    const legenda = todos.filter((d) => d.n > 0 || d.sempre);
    return { pedacos, legenda, total: soma.total, perdidas };
  });

  // A quebra do pedaço vermelho, em palavras. Aparece só quando há o que
  // explicar, e cada motivo tem o seu - é aqui que "não analisadas" para de
  // ser um monte só.
  const naoAnalisadas = $derived(
    [
      { n: soma.semVideo, texto: 'sem vídeo inteiro para mandar, logo depois de conectar' },
      { n: soma.recusados, texto: 'vídeo corrompido vindo da câmera' },
      { n: soma.falhas, texto: 'o detector não respondeu' },
    ].filter((x) => x.n > 0),
  );

  // As colunas da tabela de detecção. Mesma forma das de cima: cada uma sabe
  // extrair o valor que a ordena e carrega a própria ajuda, para o cabeçalho e
  // a ordenação não terem como discordar sobre o que a coluna mede.
  //
  // O `??` do acerto não é detalhe: câmera nunca olhada não tem acerto, e sem
  // um valor a ordenação a jogaria em qualquer lugar. -1 põe o "sem dado"
  // antes de todo mundo no crescente.
  const colunasDet = [
    { id: 'nome', rotulo: 'câmera', valor: (c) => c.nome, texto: true },
    {
      id: 'olhadas',
      rotulo: 'olhadas',
      valor: (c) => c.olhadas,
      ajuda: 'Marcas de movimento desta câmera que o detector olhou, desde que o dwnvr subiu.',
    },
    {
      id: 'objeto',
      rotulo: 'com objeto',
      valor: (c) => c.objeto,
      ajuda: 'Destas, quantas viraram marca de pessoa, veículo ou animal na timeline.',
    },
    {
      id: 'acerto',
      rotulo: 'acerto',
      valor: (c) => c.acerto ?? -1,
      ajuda:
        'A fatia das olhadas que achou algo. Perto de 100% costuma ser cena parada com objeto fixo no quadro, e não qualidade.',
    },
    {
      id: 'descartadas',
      rotulo: 'descartadas',
      valor: (c) => c.descartado,
      ajuda:
        'Marcas que chegaram com esta câmera já com o limite dela esperando na fila. Foram descartadas: não vão ao detector nem depois, e ficam só como movimento na timeline. É o freio que impede uma câmera agitada de tomar a vez das outras - ela só perde o que é dela.',
    },
    {
      id: 'perdidas',
      rotulo: 'perdidas',
      valor: (c) => c.perdidas,
      ajuda:
        'Marcas que ficaram sem resposta: sem vídeo inteiro para mandar, vídeo corrompido vindo da câmera, ou o detector que não respondeu.',
    },
    {
      id: 'fila',
      rotulo: 'na fila',
      // Quem mais espera primeiro; o processamento desempata.
      valor: (c) => c.fila.esperando * 2 + (c.fila.olhando ? 1 : 0),
      // Getter, e não texto fixo: o número de lugares vem do servidor, e o
      // cabeçalho relê a dica quando ele chega.
      get ajuda() {
        const n = detector?.fila.porCamera ?? 2;
        return `Os lugares desta câmera na fila: até ${n} marcas esperando a vez (cinza). Com os ${n} ocupados (âmbar), a próxima marca desta câmera é descartada e fica só como movimento na timeline. O ponto azul, à parte, é a câmera cuja imagem o detector está processando agora - essa marca já saiu da fila.`;
      },
    },
  ];

  // Chaves próprias na URL (`dsort`/`ddir`): as duas tabelas da tela ordenam
  // sozinhas, e compartilhar `sort` faria uma mexer na outra.
  const sortDetDaURL = params.get('dsort');
  let ordemDet = $state({
    col: colunasDet.some((c) => c.id === sortDetDaURL) ? sortDetDaURL : 'nome',
    asc: params.get('ddir') !== 'desc',
  });

  $effect(() =>
    escrever({
      dsort: ordemDet.col === 'nome' ? null : ordemDet.col,
      ddir: ordemDet.asc ? null : 'desc',
    }),
  );

  function ordenarDet(id) {
    if (ordemDet.col === id) ordemDet.asc = !ordemDet.asc;
    else ordemDet = { col: id, asc: true };
  }

  // O que uma câmera tem na fila agora: os lugares, ocupados primeiro, e se
  // uma imagem dela está sendo processada. Essa já saiu da fila, então não
  // ocupa lugar - vai à parte. Câmera fora de `cameras` não tem nada.
  function filaDe(id) {
    const n = detector?.fila.porCamera ?? 0;
    const l = detector?.fila.cameras?.[id] ?? { esperando: 0, olhando: false };
    const lugares = Array.from({ length: Math.max(n, l.esperando) }, (_, i) =>
      i < l.esperando ? 'esperando' : 'livre',
    );
    return { ...l, lugares, cheia: n > 0 && l.esperando >= n };
  }

  const detCams = $derived.by(() => {
    const linhas = comFunil.map((c) => {
      const d = desfechosDe(c);
      const olhadas = d.olhado + d.objeto;
      return {
        id: c.id,
        nome: c.name || c.id,
        ...d,
        olhadas,
        perdidas: d.semVideo + d.recusados + d.falhas,
        acerto: olhadas ? Math.round((d.objeto / olhadas) * 100) : null,
        fila: filaDe(c.id),
      };
    });
    return ordena(linhas, colunasDet, ordemDet, (c) => c.nome);
  });

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
      <!-- Duas ausências diferentes moravam no mesmo "sem dados ainda": o
           servidor que ainda não respondeu e a instalação sem câmera nenhuma.
           `updatedAt` só sai de 0 quando uma leitura volta, e a resposta traz
           toda câmera cadastrada - inclusive a desabilitada. -->
      {#if health.updatedAt}
        <SemCameras />
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
  {#if comFunil.length}
    <div class="card deteccao">
      <div class="topo">
        <div class="row wrap">
          <strong>Reconhecimento de objetos</strong>
          <span class="spacer"></span>
          <!-- Os números zeram quando o dwnvr reinicia: sem dizer desde quando
               eles contam, "12 com objeto" não é muito nem pouco. -->
          {#if health.uptime}
            <span class="muted small">desde que o dwnvr subiu, há {duracao(health.uptime.appSeconds * 1000)}</span>
          {/if}
          {#if detector}
            <span
              class="chip mono"
              title="marcas esperando a vez agora, e o teto: {detector.fila.porCamera} por câmera com detecção ligada"
            >
              fila {detector.fila.agora}/{detector.fila.cap}
            </span>
            <span class="chip mono" title="o maior tamanho que a fila já teve desde que o dwnvr subiu">
              pico {detector.fila.pico}
            </span>
          {/if}
        </div>

        {#if funil}
          <!-- Uma barra só conta a história inteira: de tudo que as câmeras
               marcaram, o que foi descartado, o que o detector olhou, o que virou
               objeto e o que ficou sem resposta. É a única forma desta tela de
               responder POR QUE uma marca não virou objeto sem precisar de
               prosa. -->
          <div class="funil" bind:clientWidth={larguraFunil} title="o que aconteceu com cada marca de movimento">
            {#each funil.pedacos as p (p.id)}
              <span
                class="seg"
                class:escuro={p.escuro}
                style:width="{(p.n / funil.total) * 100}%"
                style:background={p.cor}>{cabe(p) ? p.n : ''}</span>
            {/each}
          </div>

          <!-- O número acompanha o rótulo quando não coube no pedaço. Assim
               cada número aparece exatamente uma vez, e nenhum aparece pela
               metade. -->
          <div class="row wrap small muted legend">
            {#each funil.legenda as p (p.id)}
              <span title={p.ajuda}>
                <i style:background={p.cor}></i>
                {p.rotulo}{#if !cabe(p)}{' '}<b class="mono">{p.n}</b>{/if}
              </span>
            {/each}
          </div>

          <div class="row wrap small muted numeros">
            <span><b class="mono">{funil.total}</b> marcas de movimento</span>
            <span><b class="mono">{soma.objeto}</b> viraram objeto na timeline</span>
            <!-- Dois tempos, e não um: analisar é o detector olhando, a espera
                 é quanto a marca ficou parada na fila antes dele - e é a que
                 cresce quando o aparelho não dá conta. Zero é "ainda não
                 olhou nenhuma", e um "0,0s" ali diria que é instantâneo. -->
            {#if detector?.tempos.analiseMs}
              <span>analisar leva <b class="mono">{segundos(detector.tempos.analiseMs)}</b> por marca</span>
              <span>espera de cada marca na fila <b class="mono">{segundos(detector.tempos.esperaMs)}</b></span>
            {/if}
          </div>

          {#if naoAnalisadas.length}
            <p class="small muted quebra">
              das {funil.perdidas} não analisadas:
              {#each naoAnalisadas as m, i}{i ? ' · ' : ' '}<b class="mono">{m.n}</b> {m.texto}{/each}
            </p>
          {/if}
        {:else}
          <p class="small muted quebra">nenhuma marca de movimento desde que o dwnvr subiu</p>
        {/if}
      </div>

      <div class="tabela">
        <div class="thead detgrid row small muted">
          {#each colunasDet as col (col.id)}
            <button
              class="th"
              class:ativa={ordemDet.col === col.id}
              aria-label="ordenar por {col.rotulo}"
              title={col.ajuda}
              onclick={() => ordenarDet(col.id)}
            >
              <span class="rotulo">{col.rotulo}</span>
              <!-- A seta ocupa lugar sempre, senão o cabeçalho pula a cada clique. -->
              <span class="seta">{ordemDet.col === col.id ? (ordemDet.asc ? '▲' : '▼') : ''}</span>
            </button>
          {/each}
        </div>
        {#each detCams as c (c.id)}
          <div class="trow detgrid row">
            <span class="nome">{c.nome}</span>
            <span class="mono" class:zero={!c.olhadas}>{c.olhadas}</span>
            <span class="mono" class:zero={!c.objeto}>{c.objeto}</span>
            <!-- Quase tudo com objeto não é bom sinal: costuma ser um objeto
                 parado dentro do quadro, e aí o reconhecimento está confirmando
                 sempre a mesma coisa em vez de avisar de algo novo. -->
            <span class="mono" class:zero={!c.objeto} class:alerta={c.acerto >= 90}>
              {c.acerto == null ? '-' : c.acerto + '%'}
            </span>
            <span class="mono" class:zero={!c.descartado}>{c.descartado}</span>
            <span class="mono" class:zero={!c.perdidas} class:alerta={c.perdidas > 0}>{c.perdidas}</span>
            <!-- Os lugares da câmera, e não um número: cheio ou vazio se lê
                 de relance. Âmbar quando todos estão ocupados, que é quando a
                 próxima marca dela é descartada. O azul fica à esquerda deles,
                 e separado: a imagem em processamento não está na fila. -->
            <span class="celula-fila">
              {#if c.fila.olhando}
                <span class="processando" title="o detector está processando uma imagem desta câmera agora"></span>
              {/if}
              <span
                class="lugares"
                class:cheia={c.fila.cheia}
                title="{c.fila.esperando} de {c.fila.lugares.length} lugares ocupados"
              >
                {#each c.fila.lugares as l, i (i)}<span class="lugar {l}"></span>{/each}
              </span>
            </span>
          </div>
        {/each}
      </div>
      {#if detector}
        <div class="row wrap small muted legenda-fila">
          <span><i class="lugar livre"></i>lugar livre</span>
          <span><i class="lugar esperando"></i>esperando a vez</span>
          <span><i class="lugar esperando cheio"></i>lugares cheios: a próxima é descartada</span>
          <span><i class="processando"></i>imagem sendo processada pelo detector</span>
        </div>
      {/if}
    </div>
  {/if}

  <!-- Este navegador: o lado de quem olha, que o servidor não enxerga. O
       "copiar" é o que torna a seção útil de longe - a pessoa manda o texto
       numa conversa em vez de descrever o celular. -->
  <div class="card navegador">
    <div class="row">
      <!-- O cabeçalho inteiro é o botão, e não só a seta: no celular é o
           alvo que o dedo acha. O "copiar" fica fora dele, senão tocar para
           copiar também fecharia o card. -->
      <button class="abrir row" onclick={() => (navegadorAberto = !navegadorAberto)} aria-expanded={navegadorAberto}>
        <span class="seta-card">{navegadorAberto ? '▾' : '▸'}</span>
        <strong>Este navegador</strong>
      </button>
      {#if navegadorAberto && grupos.length}
        {#if copiado}<span class="muted small">{copiado}</span>{/if}
        <button onclick={copiarDiagnostico}>copiar</button>
      {/if}
    </div>
    {#if navegadorAberto && grupos.length}
      <div class="grupos">
        {#each gruposComRelogio as g (g.titulo)}
          <section>
            <p class="titulo muted">{g.titulo}</p>
            <dl>
              {#each g.itens as i (i.rotulo)}
                <div class="item small">
                  <dt class="muted">{i.rotulo}</dt>
                  <dd class="mono" class:alerta={i.alerta}>{i.valor}</dd>
                </div>
              {/each}
            </dl>
          </section>
        {/each}
      </div>
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

  /* ---- reconhecimento de objetos ---- */

  /* O card não tem padding próprio: quem espaça é cada metade. A tabela
     precisa encostar na borda para rolar de lado inteira no celular, e o
     resumo precisa do respiro que todo card tem. O gap zerado é o que faz o
     risco divisor colar nas duas metades em vez de flutuar entre elas. */
  .deteccao { padding: 0; gap: 0; }
  .topo { display: grid; gap: 10px; padding: 12px; }
  .tabela { border-top: 1px solid var(--line); overflow-x: auto; }

  /* A barra do funil. Mesma altura da barra de disco não serve: aqui os
     pedaços carregam número dentro, e 12px cortaria o algarismo. */
  .funil {
    display: flex;
    height: 26px;
    border-radius: 6px;
    overflow: hidden;
    background: #1b1f24;
  }
  .funil .seg {
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
  .funil .seg.escuro { color: #0d1117; }

  /* Espalhados na linha inteira no desktop e empilhados no celular, que é o
     que o wrap já faz - o gap é o que impede os números de se colarem. */
  .numeros { gap: 14px; }
  /* O número que não coube no pedaço vem colado no rótulo da legenda, e
     precisa ler como número e não como parte do texto. */
  .legend b { color: var(--fg); }
  .numeros b { color: var(--fg); font-weight: 600; }
  .quebra { margin: 0; }
  .quebra b { color: var(--fg); }

  /* Colunas: câmera, olhadas, com objeto, acerto, descartadas, perdidas, na fila.
     São sete num espaço que já era apertado para seis, então as numéricas vão
     no mínimo que ainda cabe o cabeçalho, e o card rola de lado no celular. */
  /* .thead e .trow também são classe, e a regra deles vem depois nesta folha -
     no empate de especificidade ela ganharia e a grade voltaria a ter as seis
     colunas da tabela de câmeras. Os dois nomes juntos desempatam. */
  .thead.detgrid, .trow.detgrid {
    grid-template-columns:
      minmax(110px, 1.7fr) repeat(2, minmax(62px, 0.9fr)) minmax(58px, 0.8fr)
      repeat(2, minmax(68px, 0.9fr)) minmax(56px, 0.8fr);
  }
  /* Números à direita: é assim que se compara uma coluna de olho. O nome fica
     onde está, à esquerda - e o critério é a POSIÇÃO, não a classe `.nome`,
     que só as linhas têm: por classe, o "câmera" do cabeçalho ia para a
     direita sozinho, desalinhado da coluna que ele nomeia. */
  .detgrid > :not(:first-child) { text-align: right; }
  /* O cabeçalho é botão, e botão é flex: nele o que alinha não é o text-align
     e sim o justify-content, senão rótulo e seta ficam colados à esquerda
     enquanto os números da coluna estão à direita. */
  .thead.detgrid .th:not(:first-child) { justify-content: flex-end; }
  /* Zero é resposta, mas não é notícia: apagado para a linha que tem número
     saltar dentre as que não têm. */
  .zero { color: #4a5058; }
  .alerta { color: var(--warn); }

  /* Os lugares de uma câmera na fila. Do tamanho do .dot global, para ler
     como parente dele; o livre é só o contorno. */
  .lugares { display: inline-flex; gap: 4px; justify-content: flex-end; }
  .lugar {
    display: inline-block;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    border: 1.5px solid #4a5058;
  }
  .lugar.esperando { background: var(--dim); border-color: var(--dim); }
  .cheia .lugar.esperando, .lugar.esperando.cheio { background: var(--warn); border-color: var(--warn); }
  /* A câmera em processamento: fora da fila, então fora dos lugares - um
     ponto à parte, com halo, para não ler como mais um lugar. */
  .celula-fila { display: inline-flex; align-items: center; justify-content: flex-end; }
  .processando {
    display: inline-block;
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--accent);
    box-shadow: 0 0 0 3px #2f81f733;
    margin-right: 9px;
  }
  .legenda-fila { gap: 14px; padding: 8px 12px 10px; border-top: 1px solid #21262d; }
  .legenda-fila .lugar, .legenda-fila .processando { margin-right: 6px; vertical-align: -1px; }

  /* ---- este navegador ---- */

  /* Uma coluna no celular, duas quando cabe. Os grupos não se partem entre
     colunas: "Vídeo" pela metade separaria o H.265 do MediaSource. */
  .grupos { display: grid; gap: 14px; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); }
  .navegador dl { margin: 0; display: grid; gap: 2px; }
  .navegador .titulo { margin: 0 0 4px; text-transform: uppercase; letter-spacing: 0.04em; font-size: 11px; }
  /* Botão sem cara de botão: lê como o título dos outros cards. Os 44px de
     toque ficam; a margem negativa devolve a altura para o card fechado não
     ficar mais alto que os vizinhos. */
  .navegador .abrir {
    flex: 1;
    gap: 8px;
    margin: -8px 0;
    padding: 0;
    background: none;
    border: none;
    color: inherit;
    font: inherit;
    text-align: left;
  }
  .navegador .abrir:hover:not(:disabled) { border-color: transparent; }
  .seta-card { width: 12px; font-size: 12px; color: var(--dim); }
  .navegador .item { display: flex; gap: 10px; justify-content: space-between; }
  .navegador dd { margin: 0; text-align: right; overflow-wrap: anywhere; }

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
