<script lang="ts">
  // Detecções: as detecções de objeto de todas as câmeras numa grade só, da
  // mais nova para a mais velha, agrupadas por hora.
  //
  // O dia inteiro não cabe na memória de um celular nem na tela, então nada
  // aqui é "tudo": o servidor entrega páginas por cursor, a tela guarda só uma
  // janela delas (MAX_ITENS) e o DOM só tem as linhas que estão à vista, com
  // uma tela de sobra para cada lado. O resto é conta: a miniatura tem largura
  // de coluna e proporção 16:9, e o cabeçalho da hora tem altura fixa - então
  // a posição de cada linha se calcula sem medir nada, e a barra de rolagem
  // tem o tamanho certo desde o primeiro quadro.
  import { onMount, tick } from 'svelte';
  import DayNav from '../components/DayNav.svelte';
  import Clock from '../components/Clock.svelte';
  import ScheduleInterval from '../components/ScheduleInterval.svelte';
  import DetectionSheet from '../components/DetectionSheet.svelte';
  import CamerasButton from '../components/CamerasButton.svelte';
  import CameraPicker from '../components/CameraPicker.svelte';
  import Icon from '../components/Icon.svelte';
  import FamilyBadge from '../components/FamilyBadge.svelte';
  import AreaTile from '../components/AreaTile.svelte';
  import AreaLabel from '../components/AreaLabel.svelte';
  import AreaThumbnail from '../components/AreaThumbnail.svelte';
  import type AreaEditorT from '../components/AreaEditor.svelte';
  import { api, mediaURL, messageOf } from '../lib/api.ts';
  import type { Box, Detection, DetectionsFilter } from '../lib/api.ts';
  import { cameras, isAdmin, loadCameras } from '../lib/state.svelte.ts';
  import { currentParams, write, recordingsLink } from '../lib/route.svelte.ts';
  import { FAMILIES, FAMILY_NAMES, family, iconURL, familyNames } from '../lib/icons.ts';
  import type { FamilyName } from '../lib/icons.ts';
  import { BOX_SLACK_PX } from '../lib/boxes.ts';
  import { hhmmss, ddmm, dayKey, parseDay, hourOfMinute } from '../lib/format.ts';
  import { notify, closeNotice } from '../lib/snackbar.svelte.ts';
  import {
    RULES,
    MAX_AREAS,
    queryArea,
    areaInQuery,
    registryPoints,
    svgPoints,
  } from '../lib/area.ts';
  import type { FilterArea, Point, Rule } from '../lib/area.ts';
  import { lastSegment } from '../lib/thumbs.ts';

  // Uma linha da grade: o cabeçalho da hora, ou uma fileira de miniaturas.
  // `primeira` é o índice na `lista` da primeira miniatura da fileira.
  interface HourRow {
    tipo: 'hora';
    k: string;
    y: number;
    height: number;
    label: string;
    day: string;
  }
  interface GridRow {
    tipo: 'grade';
    k: string;
    y: number;
    height: number;
    firstIndex: number;
    items: Detection[];
  }
  type Row = HourRow | GridRow;

  // O intervalo de horário: só as detecções entre `de` e `ate` do `dia`, em
  // minutos do dia e com os dois minutos inteiros incluídos. O `ate` menor que
  // o `de` termina no dia seguinte (22:00 até 06:00 é a noite).
  interface Interval {
    day: string;
    from: number;
    to: number;
  }

  // A detecção que segura a vista, e a distância dela até o topo.
  interface Anchor {
    k: string;
    dy: number;
  }

  // --- parâmetros -------------------------------------------------------------

  // Colunas da grade: o zoom anda entre as pontas, e o padrão é o que o mock
  // aprovado mostrava. No celular a miniatura de 3 colunas (103 px) já foi
  // recusada como pequena demais; 4 fica como opção de quem quer varrer.
  const COLUMNS = {
    phone: { padrao: 2, min: 1, max: 4 },
    desktop: { padrao: 5, min: 3, max: 8 },
  };
  // A mesma quebra do App.svelte, onde a navegação sobe para o cabeçalho.
  const DESKTOP_PX = 720;
  const HOUR_PX = 40; // o cabeçalho da hora entre os grupos

  // Quantas telas de detecção carregar à frente do que se vê, para a rolagem
  // não alcançar a borda do que já chegou. E quantas telas de DOM manter além
  // da vista: o bastante para um arremesso de dedo não mostrar buraco.
  const FETCH_SCREENS = 1.5;
  const DOM_SCREENS = 1;
  // Quantas detecções guardar ao todo. Passando disso, o que ficou longe da
  // vista, do lado oposto ao da rolagem, é solto - e é buscado de novo se a
  // rolagem voltar até lá.
  const MAX_ITEMS = 600;

  // A detecção nova chega sozinha enquanto a tela está no topo. A marca de
  // objeto é gravada depois que o detector olha, com o instante do onset, então
  // uma detecção pode nascer ABAIXO do topo que já se mostrou: a busca volta
  // ATRASO_MS antes do topo para pegar essas também.
  const NOVAS_MS = 20000;
  const DELAY_MS = 5 * 60 * 1000;

  // A altura de cada linha do menu do ⋮ - a da área, com a miniatura, é mais
  // alta -, e a do resto dele (bordas, padding e separadores), para abrir para
  // cima quando não cabe embaixo. E quanto tempo o "desfazer" fica na tela
  // depois de ocultar uma câmera.
  const MENU_ITEM_PX = 36;
  const MENU_AREA_PX = 56;
  const MENU_BEZEL_PX = 20;
  const UNDO_MS = 6000;
  // Quantas miniaturas o chip do filtro de área empilha.
  const THUMBNAILS_IN_CHIP = 3;

  const FAMILY_ORDER = FAMILY_NAMES;

  // --- estado da URL ----------------------------------------------------------

  const params = currentParams();

  const urlIds = <T extends string>(k: string, validItems: readonly T[]): T[] | null =>
    params.has(k) ? params.getAll(k).filter((v): v is T => validItems.some((x) => x === v)) : null;

  // Nulo é "todas": é o padrão, e fica fora do link.
  let checkedCams = $state<string[] | null>(null);
  let checkedFamilies = $state<FamilyName[] | null>(urlIds('familias', FAMILY_ORDER));
  let chosenColumns = $state<number | null>(Number(params.get('cols')) || null);
  let withBoxes = $state(params.get('caixas') !== '0');
  let refreshing = $state(params.get('atualizar') !== '0');

  // O intervalo vem do link inteiro ou não vem: `dia`, `de` e `ate`.
  const urlMinute = (k: string) => {
    const m = /^(\d{2}):(\d{2})$/.exec(params.get(k) ?? '');
    if (!m) return null;
    const h = Number(m[1]);
    const min = Number(m[2]);
    return h < 24 && min < 60 ? h * 60 + min : null;
  };
  function urlInterval(): Interval | null {
    const day = params.get('dia') ?? '';
    const from = urlMinute('de');
    const to = urlMinute('ate');
    // A ida e volta recusa o dia que não existe: 2026-02-31 viraria 03/03.
    if (dayKey(parseDay(day)) !== day || from === null || to === null) return null;
    return { day, from, to };
  }
  let interval = $state<Interval | null>(urlInterval());

  // A área desenhada na hora: `area` e `rule`, os mesmos nomes da API. Ela
  // restringe só a câmera dela; as outras câmeras marcadas não mudam.
  let area = $state<FilterArea | null>(queryArea(params.get('area'), params.get('rule')));
  // As áreas cadastradas marcadas, `<cam>/<id>`, como a API: as de uma câmera
  // somam, a desenhada junto. Conferidas no cadastro quando as câmeras chegam:
  // a área apagada sai do link.
  let checkedAreas = $state<string[]>(params.getAll('areas'));
  // Só as detecções com objeto que alertou: `alert=1`, como a API.
  let onlyAlerts = $state(params.get('alert') === '1');

  let desktop = $state(window.innerWidth >= DESKTOP_PX);
  const strip = $derived(desktop ? COLUMNS.desktop : COLUMNS.phone);
  const columns = $derived(Math.min(strip.max, Math.max(strip.min, chosenColumns ?? strip.padrao)));

  $effect(() => {
    write({
      cams: checkedCams,
      familias: checkedFamilies,
      cols: chosenColumns,
      boxes: withBoxes ? null : '0',
      refresh: refreshing ? null : '0',
      day: interval?.day,
      from: interval ? hourOfMinute(interval.from) : null,
      to: interval ? hourOfMinute(interval.to) : null,
      area: area ? areaInQuery(area) : null,
      rule: area?.rule ?? null,
      areas: checkedAreas.length ? checkedAreas : null,
      alert: onlyAlerts ? '1' : null,
    });
  });

  // --- as detecções -----------------------------------------------------------

  // A janela carregada, da mais nova para a mais velha. `$state.raw` porque
  // cada página troca o array inteiro: proxy em 600 objetos com caixas seria
  // custo sem uso.
  let list = $state.raw<Detection[]>([]);
  // Se há mais além de cada ponta da janela. Acima: detecção mais nova que o
  // topo carregado - depois de um "ir para", ou depois de soltar o topo.
  let endAbove = $state(true);
  let endBelow = $state(false);
  let loaded = $state(false);
  let fetchingAbove = false;
  let fetchingBelow = false;
  let error = $state('');
  // O instante do último "ir para", que serve de cursor quando ele caiu antes
  // de qualquer detecção e a lista começou vazia. Reativo porque é ele que o
  // relógio mostra com a lista vazia.
  let target = $state<number | null>(null);
  // Invalida a resposta de uma busca que já não descreve a tela: filtro novo,
  // "ir para" novo.
  let generation = 0;

  // O ao vivo e Gravações só abrem para câmera que ainda está no cadastro: uma
  // detecção antiga pode citar uma câmera removida desde então, e as duas telas
  // trocariam o id desconhecido por outra câmera.
  const exists = (cam: string) => cameras.list.some((c) => c.id === cam);

  const key = (d: Detection) => `${d.cam}|${d.instanteMs}`;

  const names = $derived(Object.fromEntries(cameras.list.map((c) => [c.id, c.name])));

  // As áreas cadastradas, por `<cam>/<id>`.
  const registered = $derived(
    new Map(
      cameras.list.flatMap((c) =>
        (c.areas ?? []).flatMap((a) => (a.id ? [[`${c.id}/${a.id}`, { cam: c.id, ...a }]] : [])),
      ),
    ),
  );
  const anyRegistered = $derived(registered.size > 0);

  // Os contornos de cada câmera com área no filtro: a desenhada e as marcadas.
  const outlines = $derived.by(() => {
    const out: Record<string, Point[][]> = {};
    const sum = (cam: string, p: Point[]) => (out[cam] ??= []).push(p);
    if (area) sum(area.cam, area.points);
    for (const k of checkedAreas) {
      const a = registered.get(k);
      if (a) sum(a.cam, a.points);
    }
    return out;
  });
  const withArea = $derived(area !== null || checkedAreas.length > 0);

  // O botão "só alertas" aparece com alguma câmera alertando, ou com ele já
  // ligado pelo link. O viewer recebe do alerta só o `enabled`.
  const anyAlert = $derived(cameras.list.some((c) => c.alert?.enabled));

  const alerted = (d: Detection) => d.objetos.some((o) => o.alert);

  // As áreas em que a detecção alertou, com o nome e o contorno de agora. A
  // que foi apagada desde então fica sem contorno, como "área apagada".
  function alertAreas(d: Detection) {
    const ids = [...new Set(d.objetos.flatMap((o) => o.alertAreas ?? []))];
    return ids.map((id) => {
      const a = registered.get(`${d.cam}/${id}`);
      return { id, nome: a?.name ?? 'área apagada', points: a?.points ?? null };
    });
  }

  // Os contornos sobre a miniatura: os do filtro e, com o "só alertas", os das
  // áreas do alerta. A área cadastrada que está nos dois desenha uma vez só.
  function outlinesOf(d: Detection): readonly (readonly Point[])[] {
    const out: (readonly Point[])[] = [...(outlines[d.cam] ?? [])];
    if (!onlyAlerts) return out;
    for (const a of alertAreas(d)) if (a.points && !out.includes(a.points)) out.push(a.points);
    return out;
  }

  // O que vai no pedido. Nulo quando o filtro não deixou nada marcado, e aí nem
  // se pergunta ao servidor.
  const filter = $derived.by((): DetectionsFilter | null => {
    if (checkedCams?.length === 0 || checkedFamilies?.length === 0) return null;
    return {
      cams: checkedCams,
      familias: checkedFamilies,
      area,
      areas: checkedAreas,
      soAlertas: onlyAlerts,
    };
  });

  // O intervalo em instantes: `de` incluído, `ate` excluído (o minuto final
  // inteiro fica dentro). Nulo sem intervalo.
  const limites = $derived.by(() => {
    if (!interval) return null;
    const from = parseDay(interval.day);
    from.setMinutes(interval.from);
    const to = parseDay(interval.day);
    if (interval.to < interval.from) to.setDate(to.getDate() + 1);
    to.setMinutes(interval.to + 1);
    return { from: from.getTime(), to: to.getTime() };
  });

  // As detecções de uma página que caem no intervalo, e se alguma ficou de
  // fora: a que passou da ponta diz que a lista acabou daquele lado, porque o
  // servidor entrega em ordem.
  function isInside(ds: Detection[]) {
    if (!limites) return { ds, wasCut: false };
    const { from, to } = limites;
    const ok = ds.filter((d) => d.instanteMs >= from && d.instanteMs < to);
    return { ds: ok, wasCut: ok.length < ds.length };
  }

  // Uma página é o que enche duas telas: pouco mais que isso já é o prefetch.
  function page() {
    const rows = Math.ceil(window.innerHeight / (grid.tileHeight + grid.vao)) + 1;
    return Math.min(200, Math.max(24, rows * columns * 2));
  }

  // junta duas fatias em ordem, sem repetir: a busca de novas volta de
  // propósito sobre o que já se tem, e uma página pode empatar com a vizinha.
  function join(a: Detection[], b: Detection[]) {
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- Set local, só para juntar
    const seen = new Set<string>();
    const out: Detection[] = [];
    for (const d of [...a, ...b].sort(
      (x, y) => y.instanteMs - x.instanteMs || (x.cam < y.cam ? -1 : 1),
    )) {
      const k = key(d);
      if (seen.has(k)) continue;
      seen.add(k);
      out.push(d);
    }
    return out;
  }

  // Com intervalo, a busca parte da ponta de cima dele - ou do "ir para", que
  // fica preso dentro, no mínimo o primeiro minuto -, e a ponta de cima vale
  // como o topo da lista.
  async function restart(requestNo: number | null = null) {
    const g = ++generation;
    const before = limites
      ? Math.min(Math.max(requestNo ?? limites.to, limites.from + 60000), limites.to)
      : requestNo;
    target = before;
    list = [];
    endAbove = before === null || (limites !== null && before >= limites.to);
    endBelow = false;
    loaded = false;
    error = '';
    isOpen = null;
    fetchingAbove = fetchingBelow = false;
    window.scrollTo(0, 0);
    if (!filter) {
      endBelow = true;
      loaded = true;
      return;
    }
    try {
      const r = await api.deteccoes({ ...filter, antes: before, limite: page() });
      if (g !== generation) return;
      const { ds, wasCut } = isInside(r.deteccoes);
      list = ds;
      endBelow = r.fim || wasCut;
      loaded = true;
      await tick();
      verify();
    } catch (e) {
      if (g === generation) error = messageOf(e);
    }
  }

  async function below() {
    const last = list.at(-1);
    if (!last) return;
    fetchingBelow = true;
    const g = generation;
    try {
      const r = await api.deteccoes({ ...filter, antes: last.instanteMs, limite: page() });
      if (g !== generation) return;
      const a = anchor();
      const { ds, wasCut } = isInside(r.deteccoes);
      let newOne = join(list, ds);
      endBelow = r.fim || wasCut;
      // Solta o topo, que ficou para trás. A âncora mora na vista e a vista
      // nunca está no pedaço solto: a sobra é bem maior que uma tela.
      if (newOne.length > MAX_ITEMS) {
        newOne = newOne.slice(newOne.length - MAX_ITEMS);
        endAbove = false;
      }
      list = newOne;
      await restoreAnchor(a);
    } catch (e) {
      if (g === generation) error = messageOf(e);
    } finally {
      if (g === generation) fetchingBelow = false;
    }
    verify();
  }

  async function above() {
    fetchingAbove = true;
    const g = generation;
    try {
      const after = list[0]?.instanteMs ?? target;
      const r = await api.deteccoes({ ...filter, depois: after, limite: page() });
      if (g !== generation) return;
      const a = anchor();
      const { ds, wasCut } = isInside(r.deteccoes);
      let newOne = join(ds, list);
      endAbove = r.fim || wasCut;
      if (newOne.length > MAX_ITEMS) {
        newOne = newOne.slice(0, MAX_ITEMS);
        endBelow = false;
      }
      // A lista vazia é um "ir para" sem nada antes dele - comum no começo de
      // um intervalo. A página chega acima do instante pedido, e a vista vai ao
      // pé dela, onde está a detecção mais perto dele.
      const empty = !list.length;
      list = newOne;
      if (empty) {
        await tick();
        window.scrollTo(0, document.documentElement.scrollHeight);
        measure();
      } else {
        await restoreAnchor(a);
      }
    } catch (e) {
      if (g === generation) error = messageOf(e);
    } finally {
      if (g === generation) fetchingAbove = false;
    }
    verify();
  }

  // As novas, e as atrasadas logo abaixo do topo. Só com o topo carregado: fora
  // dele, a rolagem para cima é que as traz. Com um intervalo que já acabou, e
  // passada a folga das atrasadas, não há o que buscar.
  async function novas() {
    if (!refreshing || document.hidden || !loaded || !endAbove || !filter || fetchingAbove) return;
    if (limites && Date.now() > limites.to + DELAY_MS) return;
    const g = generation;
    const topPos = list[0]?.instanteMs ?? Math.min(Date.now(), limites?.to ?? Infinity);
    try {
      const r = await api.deteccoes({ ...filter, depois: topPos - DELAY_MS, limite: 200 });
      const { ds, wasCut } = isInside(r.deteccoes);
      const newer = ds[0];
      if (g !== generation || !newer) return;
      // Quem está olhando o topo vê a novidade entrar; quem rolou para baixo
      // não pode ter a tela empurrada por ela.
      const a = scrollRange.ini > 0 ? anchor() : null;
      list = join(ds, list);
      // Mais de 200 no intervalo: as mais novas ficaram de fora, e a rolagem
      // para cima as busca como faria depois de um "ir para".
      if (!r.fim && !wasCut) endAbove = false;
      recordDay(dayKey(new Date(newer.instanteMs)));
      if (a) await restoreAnchor(a);
    } catch {
      // A próxima rodada tenta de novo; um soluço da rede não vira aviso.
    }
  }

  // --- os dias do calendário -------------------------------------------------

  // Os dias com detecção do filtro, para o calendário e as setas. Vazia é o
  // modo permissivo do seletor: até a resposta chegar, ou se ela falhar, todo
  // dia até hoje vale.
  let daysWithDetection = $state.raw<string[]>([]);
  // Quantas detecções do filtro cada dia tem, que o calendário mostra no dia.
  // Não segue o intervalo de horário: é o dia inteiro.
  let detectionsPerDay = $state.raw<Record<string, number>>({});
  // Segura a primeira consulta até a câmera da URL ser resolvida: antes disso o
  // filtro é "todas", e a consulta sairia à toa - e, com o disco frio, é a mais
  // cara da tela.
  let resolvedCameras = $state(false);

  $effect(() => {
    if (resolvedCameras) void loadDays(filter);
  });

  // `manter` é a releitura de quando o calendário abre: a lista que já está lá
  // é do mesmo filtro, e um soluço da rede não a troca pelo modo permissivo.
  async function loadDays(f: DetectionsFilter | null, keep = false) {
    if (!f) {
      daysWithDetection = [];
      detectionsPerDay = {};
      return;
    }
    try {
      const r = await api.detectionDays(f);
      if (f !== filter) return;
      daysWithDetection = r.dias;
      detectionsPerDay = r.deteccoesPorDia;
    } catch {
      if (f !== filter || keep) return;
      daysWithDetection = [];
      detectionsPerDay = {};
    }
  }

  // Hoje estreando no calendário com a tela aberta: a detecção nova já diz o
  // dia, sem reler a lista. Em modo permissivo a lista fica vazia - um dia só a
  // tiraria do "tudo liberado".
  function recordDay(day: string) {
    if (!daysWithDetection.length || daysWithDetection.includes(day)) return;
    daysWithDetection = [...daysWithDetection, day].sort();
  }

  // Religar busca na hora: esperar a rodada seguinte pareceria que não pegou.
  function toggleRefresh() {
    refreshing = !refreshing;
    if (refreshing) void novas();
  }

  // --- a grade, calculada -----------------------------------------------------

  let listEl: HTMLDivElement | undefined;
  let width = $state(0);

  const grid = $derived.by(() => {
    const vao = desktop ? 8 : 6;
    const tileWidth = Math.max(0, (width - vao * (columns - 1)) / columns);
    return { vao, tileWidth, tileHeight: (tileWidth * 9) / 16 };
  });

  const hourKey = (ms: number) => {
    const d = new Date(ms);
    return `${dayKey(d)}T${d.getHours()}`;
  };
  const WEEK = ['dom', 'seg', 'ter', 'qua', 'qui', 'sex', 'sáb'];
  const longDay = (ms: number) => `${WEEK[new Date(ms).getDay()]}, ${ddmm(ms)}`;

  // As linhas na ordem da tela, cada uma com o `y` onde começa. `linhaDe[i]`
  // diz em que linha mora a detecção i, para achar a âncora depois de uma troca.
  const layout = $derived.by(() => {
    const { vao, tileHeight } = grid;
    const rows: Row[] = [];
    const rowOf: number[] = [];
    let y = 0;
    let hour: string | null = null;
    let current: GridRow | null = null;
    for (const [i, det] of list.entries()) {
      const ms = det.instanteMs;
      const h = hourKey(ms);
      if (h !== hour) {
        const d = new Date(ms);
        rows.push({
          tipo: 'hora',
          k: `h${h}`,
          y,
          height: HOUR_PX,
          label: `${String(d.getHours()).padStart(2, '0')}h`,
          day: longDay(ms),
        });
        y += HOUR_PX;
        hour = h;
        current = null;
      }
      if (!current || current.items.length === columns) {
        current = {
          tipo: 'grade',
          k: `g${key(det)}`,
          y,
          height: tileHeight,
          firstIndex: i,
          items: [],
        };
        rows.push(current);
        y += tileHeight + vao;
      }
      current.items.push(det);
      rowOf[i] = rows.length - 1;
    }
    return { linhas: rows, rowOf, height: y };
  });

  // --- rolagem ----------------------------------------------------------------

  // A vista em px da lista: `ini` é o que está logo abaixo da barra, `fim` o pé
  // da janela. Relida a cada quadro de rolagem.
  let scrollRange = $state({ ini: 0, fim: 0 });
  let barEl: HTMLDivElement | undefined;
  let barH = $state(0);
  // O cabeçalho do App fica grudado no topo no desktop; a barra desta tela
  // gruda logo abaixo dele.
  let cabH = $state(0);

  function measure() {
    if (!listEl) return;
    const listTop = listEl.getBoundingClientRect().top;
    const ini = cabH + barH - listTop;
    const end = window.innerHeight - listTop;
    if (ini !== scrollRange.ini || end !== scrollRange.fim) scrollRange = { ini, fim: end };
  }

  // A âncora da última vista inteira, para a troca de largura (girar o
  // celular) voltar ao mesmo lugar. A rolagem é em px, e a largura muda a
  // altura de tudo - e as colunas, na quebra do DESKTOP_PX. Só é relida
  // enquanto a janela tem a largura com que a grade foi medida: no meio da
  // rotação a grade ainda é a velha e a rolagem já foi cortada pelo navegador,
  // e a âncora lida ali seria a errada. E só quando a rolagem sai do lugar
  // onde a âncora foi posta: a linha de 5 colunas começa noutra detecção, e
  // reler a âncora depois de girar a faria andar uma linha a cada giro.
  let viewAnchor: Anchor | null = null;
  let gridWindow = window.innerWidth;
  let anchorY = 0;

  let requestNo = 0;
  function scrolled() {
    if (requestNo) return;
    requestNo = requestAnimationFrame(() => {
      requestNo = 0;
      measure();
      if (window.innerWidth === gridWindow && window.scrollY !== anchorY) {
        viewAnchor = scrollRange.ini > 0 ? anchor() : null;
        anchorY = window.scrollY;
      }
      verify();
    });
  }

  function verify() {
    if (!loaded || !filter || error) return;
    const screenInfo = window.innerHeight;
    if (
      !endBelow &&
      !fetchingBelow &&
      list.length &&
      layout.height - scrollRange.fim < screenInfo * FETCH_SCREENS
    ) {
      void below();
    }
    if (!endAbove && !fetchingAbove && scrollRange.ini < screenInfo * FETCH_SCREENS) void above();
  }

  // O que vai para o DOM. A faixa anda em degraus de DEGRAU_PX, e não a cada
  // pixel: a lista de linhas só é refeita quando a rolagem cruza um degrau.
  const STEP_PX = 200;
  const visibleWindow = $derived.by(() => {
    const leftover = window.innerHeight * DOM_SCREENS;
    return {
      from: Math.floor((scrollRange.ini - leftover) / STEP_PX) * STEP_PX,
      to: Math.ceil((scrollRange.fim + leftover) / STEP_PX) * STEP_PX,
    };
  });

  // Primeira linha cujo fim passa de `y`: busca binária, as linhas vêm em ordem.
  function firstBelow(y: number) {
    const l = layout.linhas;
    let lo = 0;
    let hi = l.length;
    while (lo < hi) {
      const m = (lo + hi) >> 1;
      const row = l[m];
      if (row && row.y + row.height <= y) lo = m + 1;
      else hi = m;
    }
    return lo;
  }

  // A linha onde mora a detecção i da `lista`.
  function detectionRow(i: number) {
    const li = layout.rowOf[i];
    return li === undefined ? undefined : layout.linhas[li];
  }

  const vistas = $derived.by(() => {
    const { from, to } = visibleWindow;
    const out: Row[] = [];
    for (const row of layout.linhas.slice(firstBelow(from))) {
      if (row.y >= to) break;
      out.push(row);
    }
    return out;
  });

  // A primeira linha de miniaturas à vista: é ela que dá o dia e a hora da
  // barra, e o cabeçalho que gruda.
  const atTop = $derived.by(() => {
    for (const row of layout.linhas.slice(firstBelow(scrollRange.ini + 1))) {
      if (row.tipo !== 'grade') continue;
      const det = row.items[0];
      if (det) return { row, det };
    }
    return null;
  });
  // Sem detecção à vista, vale o "ir para" - menos 1 ms, porque ele é o limite
  // exclusivo da busca: ir para o dia 29 busca antes da meia-noite do dia 30, e
  // mostrar o próprio limite fazia a barra piscar o dia 30 até a resposta chegar.
  const topMs = $derived(atTop?.det.instanteMs ?? (target != null ? target - 1 : Date.now()));

  // O cabeçalho da hora gruda enquanto as miniaturas dela passam. É um só,
  // fora da grade, porque o de dentro vai embora com a linha dele.
  const stuckHour = $derived.by(() => {
    if (!atTop || scrollRange.ini <= 0) return null;
    const l = layout.linhas;
    for (let i = layout.rowOf[atTop.row.firstIndex] ?? -1; i >= 0; i--) {
      const row = l[i];
      if (row?.tipo === 'hora') return row.y + row.height <= scrollRange.ini ? row : null;
    }
    return null;
  });

  // A âncora é a detecção no topo da vista e a distância dela até lá. Trocar
  // o que está acima - página que chegou, topo solto, novas - mexe no `y` de
  // tudo; restaurar a âncora devolve a vista ao mesmo lugar.
  function anchor(): Anchor | null {
    if (!atTop) return null;
    return { k: key(atTop.det), dy: atTop.row.y - scrollRange.ini };
  }

  async function restoreAnchor(a: Anchor | null) {
    await tick();
    if (!a) return;
    // De novo: a ponta do intervalo, que aparece acima da lista quando a
    // rolagem chega nela, empurra a lista sem mudar o `y` de linha nenhuma.
    measure();
    const row = detectionRow(list.findIndex((d) => key(d) === a.k));
    if (!row) return;
    const delta = row.y - a.dy - scrollRange.ini;
    if (Math.abs(delta) >= 1) window.scrollBy(0, delta);
    measure();
  }

  // Zoom e largura mudam a altura de tudo: a âncora segura a vista.
  async function changeColumns(step: number) {
    const a = anchor();
    chosenColumns = Math.min(strip.max, Math.max(strip.min, columns + step));
    await restoreAnchor(a);
    verify();
  }

  // --- as miniaturas ----------------------------------------------------------

  // A proporção de cada câmera, aprendida da primeira imagem que chega. A
  // caixa do objeto é uma fração da IMAGEM, e numa câmera 4:3 dentro de uma
  // miniatura 16:9 a imagem não ocupa a miniatura inteira.
  let ratios = $state<Record<string, number>>({});

  function learn(cam: string, img: HTMLImageElement) {
    const { naturalWidth: w, naturalHeight: h } = img;
    if (w && h && ratios[cam] !== w / h) ratios[cam] = w / h;
  }

  // Onde a imagem da câmera fica dentro da miniatura, em px.
  function imageRect(cam: string) {
    const { tileWidth: W, tileHeight: H } = grid;
    const p = ratios[cam] ?? 16 / 9;
    const w = p >= W / H ? W : H * p;
    const h = p >= W / H ? W / p : H;
    return { x: (W - w) / 2, y: (H - h) / 2, w, h };
  }

  function imageArea(cam: string) {
    const { x, y, w, h } = imageRect(cam);
    return `left:${x}px;top:${y}px;width:${w}px;height:${h}px`;
  }

  // O tamanho do score na miniatura, o mesmo do CSS de `.score` (altura) e uma
  // folga para "0,84" (largura).
  const SCORE_HEIGHT_PX = { normal: 13, compact: 11 };
  const SCORE_WIDTH_PX = { normal: 30, compact: 26 };

  // Em que lado de FORA da caixa o score cabe inteiro na miniatura: em cima,
  // senão embaixo, senão ao lado. Nunca por dentro, que cobre o objeto - a
  // cabeça de uma pessoa perto do topo. Sem lugar nenhum (caixa do tamanho da
  // imagem), fica sem score; ele está na folha.
  function scoreSide(cam: string, box: Box, compact: boolean) {
    const { tileWidth: W, tileHeight: H } = grid;
    const img = imageRect(cam);
    const border = (v: number, total: number) => Math.min(1, Math.max(0, v)) * total;
    const x1 = img.x + Math.max(0, border(box[0], img.w) - BOX_SLACK_PX);
    const y1 = img.y + Math.max(0, border(box[1], img.h) - BOX_SLACK_PX);
    const x2 = img.x + Math.min(img.w, border(box[2], img.w) + BOX_SLACK_PX);
    const y2 = img.y + Math.min(img.h, border(box[3], img.h) + BOX_SLACK_PX);
    const h = compact ? SCORE_HEIGHT_PX.compact : SCORE_HEIGHT_PX.normal;
    const w = compact ? SCORE_WIDTH_PX.compact : SCORE_WIDTH_PX.normal;
    if (y1 >= h) return 'above';
    if (y2 + h <= H) return 'below';
    if (x2 + w <= W) return 'right';
    if (x1 >= w) return 'left';
    return null;
  }

  // As famílias de uma detecção, uma vez cada.
  function familiesOf(d: Detection) {
    const out: string[] = [];
    for (const o of d.objetos) if (!out.includes(o.familia)) out.push(o.familia);
    return out;
  }

  const score = (v: number) => v.toFixed(2).replace('.', ',');

  const pct = (v: number) => `${Math.min(1, Math.max(0, v)) * 100}%`;

  // A caixa com a mesma folga do vídeo e da folha (CAIXA_FOLGA_PX), parando na
  // borda da imagem. A folga é em px e a caixa em fração, então quem soma é o
  // CSS.
  const start = (v: number) => `max(0px, calc(${pct(v)} - ${BOX_SLACK_PX}px))`;
  const size = (from: number, to: number) =>
    `calc(min(100%, calc(${pct(to)} + ${BOX_SLACK_PX}px)) - ${start(from)})`;

  // --- a folha ----------------------------------------------------------------

  let isOpen = $state<string | null>(null);
  const openIndex = $derived(isOpen ? list.findIndex((d) => key(d) === isOpen) : -1);
  const openedDet = $derived(list[openIndex]);

  // --- a barra ----------------------------------------------------------------

  let openedCameras = $state(false);
  let camsEl: HTMLDivElement | undefined;

  const checked = (id: string) => checkedCams === null || checkedCams.includes(id);

  function toggleCamera(id: string) {
    const currentIds = checkedCams ?? cameras.list.map((c) => c.id);
    const novas = currentIds.includes(id)
      ? currentIds.filter((x) => x !== id)
      : [...currentIds, id];
    checkedCams = novas.length === cameras.list.length ? null : novas;
    void restart();
  }

  function allCameras() {
    checkedCams = checkedCams === null ? [] : null;
    void restart();
  }

  function toggleFamily(id: FamilyName) {
    const currentIds = checkedFamilies ?? FAMILY_ORDER;
    const novas = currentIds.includes(id)
      ? currentIds.filter((x) => x !== id)
      : [...currentIds, id];
    checkedFamilies =
      novas.length === FAMILY_ORDER.length ? null : FAMILY_ORDER.filter((f) => novas.includes(f));
    void restart();
  }

  function toggleAlerts() {
    onlyAlerts = !onlyAlerts;
    void restart();
  }

  const nChecked = $derived(checkedCams?.length ?? cameras.list.length);

  function outsideCameras(e: MouseEvent) {
    const clickTarget = e.target instanceof Element ? e.target : null;
    if (openedCameras && camsEl && !camsEl.contains(clickTarget)) openedCameras = false;
    // O alvo já fora da página é um item do menu que trocou de página: o
    // "Filtrar por área" some debaixo do dedo, e o menu tem de ficar.
    if (
      menu &&
      menuEl &&
      clickTarget?.isConnected &&
      !menuEl.contains(clickTarget) &&
      !clickTarget.closest('.mais')
    )
      menu = null;
  }

  // --- ocultar uma câmera daqui mesmo -----------------------------------------

  // O menu do ⋮ de uma miniatura. Um só, fora da grade e em `position: fixed`:
  // a miniatura corta o que passa da borda dela, e a linha pode sair do DOM
  // no meio da rolagem. Por isso mesmo ele fecha quando a tela rola.
  let menu = $state<{
    k: string;
    det: Detection;
    cam: string;
    instanteMs: number;
    x: number;
    // Onde o ⋮ está na tela: o menu abre embaixo dele, ou em cima.
    topPos: number;
    base: number;
    // A página das áreas: o conteúdo do menu vira a lista delas.
    areas: boolean;
  } | null>(null);
  let menuEl = $state<HTMLDivElement | null>(null);

  function openMenu(d: Detection, ev: MouseEvent & { currentTarget: HTMLElement }) {
    if (menu?.k === key(d)) {
      menu = null;
      return;
    }
    const r = ev.currentTarget.getBoundingClientRect();
    menu = {
      k: key(d),
      det: d,
      cam: d.cam,
      instanteMs: d.instanteMs,
      x: window.innerWidth - r.right,
      topPos: r.top,
      base: r.bottom,
      areas: false,
    };
  }

  const areasOf = (cam: string) => cameras.list.find((c) => c.id === cam)?.areas ?? [];

  // O menu abre embaixo do ⋮ quando cabe, e senão do lado que tem mais espaço.
  // A página das áreas pode passar dos dois - são até 8 -, e aí rola dentro.
  const menuPosition = $derived.by(() => {
    if (!menu) return null;
    const rows = menu.areas ? (area?.cam === menu.cam ? 1 : 0) + 2 : exists(menu.cam) ? 4 : 1;
    const withThumbnail = menu.areas ? areasOf(menu.cam).length : 0;
    const height = rows * MENU_ITEM_PX + withThumbnail * MENU_AREA_PX + MENU_BEZEL_PX;
    const atBottom = window.innerHeight - menu.base;
    const upward = height > atBottom && menu.topPos > atBottom;
    return { upward, max: (upward ? menu.topPos : atBottom) - 8 };
  });

  // O "Filtrar por área" do ⋮. Sem área cadastrada na câmera não há o que
  // escolher, e ele vai direto ao editor sobre o quadro daquela detecção, como
  // o "filtrar por área" da folha. Com área, o menu vira a lista delas.
  function filterByMenuArea() {
    if (!menu) return;
    if (areasOf(menu.cam).length) {
      menu.areas = true;
      return;
    }
    drawMenu();
  }

  // Pelo ⋮, a lista recomeça no instante daquele quadro, e não no topo: foi
  // sobre ele que se pediu o filtro, como no "Ocultar a câmera".
  function drawMenu() {
    const m = menu;
    if (!m) return;
    menu = null;
    void openEditor(m.cam, m.det, m.instanteMs + 1);
  }

  function toggleMenuArea(k: string) {
    const m = menu;
    if (!m) return;
    menu = null;
    toggleArea(k, m.instanteMs + 1);
  }

  function removeMenuDrawn() {
    const m = menu;
    if (!m) return;
    menu = null;
    area = null;
    void restart(m.instanteMs + 1);
  }

  // O aviso de desfazer, na snackbar: a miniatura some debaixo do dedo, e um
  // toque errado no meio da rolagem é fácil. Desfazer sai mais barato que
  // perguntar antes.
  let undoNotice: number | undefined;

  // A âncora de `ancora()`, mas pulando a câmera que vai sair: a detecção que
  // segura a vista precisa continuar na lista depois do filtro.
  function anchorSkipping(cam: string): Anchor | null {
    if (!atTop) return null;
    for (let i = atTop.row.firstIndex; i < list.length; i++) {
      const det = list[i];
      const row = detectionRow(i);
      if (det && row && det.cam !== cam) return { k: key(det), dy: row.y - scrollRange.ini };
    }
    return null;
  }

  // Só desmarca a câmera no filtro desta tela, o mesmo do botão "câmeras". A
  // lista não recomeça: as detecções dela saem do que já está carregado e a
  // vista fica onde estava, que é o ponto de ocultar sem subir até a barra.
  async function hideCamera(cam: string) {
    menu = null;
    isOpen = null;
    const before = checkedCams;
    const remaining = (checkedCams ?? cameras.list.map((c) => c.id)).filter((x) => x !== cam);

    undoNotice = notify(
      {
        forte: names[cam] ?? cam,
        texto: 'oculta',
        action: {
          label: 'desfazer',
          fazer: () => {
            undo(before);
          },
        },
      },
      UNDO_MS,
    );

    if (!remaining.length) {
      checkedCams = remaining;
      void restart();
      return;
    }
    const a = anchorSkipping(cam);
    // A busca que estava no ar ainda traria a câmera: ela é descartada.
    generation++;
    fetchingAbove = fetchingBelow = false;
    checkedCams = remaining;
    list = list.filter((d) => d.cam !== cam);
    await restoreAnchor(a);
    verify();
  }

  // Desfazer precisa buscar de novo: o que era da câmera já foi solto. A busca
  // parte da detecção do topo da vista, como um "ir para", e o que está acima
  // chega com a rolagem.
  function undo(before: string[] | null) {
    checkedCams = before;
    const ms = atTop?.det.instanteMs;
    void restart(scrollRange.ini > 0 && ms != null ? ms + 1 : null);
  }

  function keyHandler(e: KeyboardEvent) {
    if (e.key === 'Escape') menu = null;
  }

  // "Ir para": o dia vai ao fim dele, e o horário ao minuto digitado, os dois
  // incluídos - a detecção de 14:32:07 aparece quando se pede 14:32.
  const topDay = $derived(dayKey(new Date(topMs)));

  // Com intervalo, trocar de dia leva o mesmo horário para o outro dia.
  function goToDay(dayKeyValue: string) {
    if (interval) {
      interval = { ...interval, day: dayKeyValue };
      void restart();
      return;
    }
    const d = parseDay(dayKeyValue);
    d.setDate(d.getDate() + 1);
    void restart(dayKeyValue === dayKey() ? null : d.getTime());
  }

  // Num intervalo que vira a meia-noite, o horário antes do `de` é o do dia
  // seguinte. O recomeca prende o instante dentro do intervalo.
  function goToHour(segundos: number) {
    const day = parseDay(interval?.day ?? topDay);
    if (interval && interval.to < interval.from && segundos / 60 < interval.from)
      day.setDate(day.getDate() + 1);
    void restart(day.getTime() + (segundos + 60) * 1000);
  }

  // O intervalo novo vale para o dia que estava à vista.
  function applyInterval(from: number, to: number) {
    interval = { day: interval?.day ?? topDay, from, to };
    void restart();
  }

  // Volta à rolagem infinita sem sair de onde estava: a busca parte da
  // detecção do topo da vista, como no desfazer.
  function resetInterval() {
    const ms = scrollRange.ini > 0 ? atTop?.det.instanteMs : undefined;
    const topPos = ms != null ? ms + 1 : limites?.to;
    interval = null;
    void restart(topPos != null && topPos <= Date.now() ? topPos : null);
  }

  // --- a área desenhada na hora ----------------------------------------------

  // O editor vem num chunk à parte: quem não desenha área não o baixa.
  let AreaEditor = $state<typeof AreaEditorT | null>(null);
  let editor = $state<{
    cam: string;
    background: { tipo: 'imagem' | 'video'; url: string } | null;
    boxes: { caixa: Box; color: string }[];
    erro: string;
    // Onde a lista recomeça ao aplicar: no topo, ou no instante do quadro
    // quando o editor veio do ⋮ dele.
    stayAt: number | null;
  } | null>(null);
  let openedArea = $state(false);
  let areaEl = $state<HTMLDivElement | undefined>();

  const boxesOf = (d: Detection) =>
    d.objetos.flatMap((o) => (o.caixa ? [{ caixa: o.caixa, color: family(o.familia).color }] : []));

  // Abre o editor sobre um quadro da câmera: o da detecção dada, ou o da mais
  // recente dela com quadro, ou o keyframe do último segmento quando ela não
  // tem detecção nenhuma.
  async function openEditor(
    cam: string,
    det: Detection | null = null,
    stayAt: number | null = null,
  ) {
    openedArea = false;
    isOpen = null;
    if (!AreaEditor)
      void import('../components/AreaEditor.svelte').then((m) => {
        AreaEditor = m.default;
      });
    if (det?.temQuadro) {
      editor = {
        cam,
        background: { tipo: 'imagem', url: mediaURL.frame(cam, det.instanteMs) },
        boxes: boxesOf(det),
        erro: '',
        stayAt,
      };
      return;
    }
    editor = { cam, background: null, boxes: [], erro: '', stayAt };
    // O editor pode ter fechado, ou ter sido aberto de novo, durante a busca. A
    // câmera não basta: reaberto na mesma, pela folha de uma detecção, ele já
    // tem o quadro dela, e a busca velha o trocaria.
    const target = editor;
    const stillOpen = () => (editor === target ? target : null);
    try {
      const r = await api.deteccoes({ cams: [cam], limite: 20 });
      const recent = r.deteccoes.find((d) => d.temQuadro);
      let ed = stillOpen();
      if (!ed) return;
      if (recent) {
        ed.background = { tipo: 'imagem', url: mediaURL.frame(cam, recent.instanteMs) };
        ed.boxes = boxesOf(recent);
        return;
      }
      const last = await lastSegment(cam);
      ed = stillOpen();
      if (!ed) return;
      if (last === null) {
        ed.erro = 'esta câmera ainda não tem gravação para desenhar em cima';
        return;
      }
      ed.background = { tipo: 'video', url: mediaURL.thumb(cam, last) };
    } catch (e) {
      const ed = stillOpen();
      if (ed) ed.erro = `não deu para buscar um quadro: ${messageOf(e)}`;
    }
  }

  // Desenhar ou marcar uma área numa câmera desmarcada é pedir para vê-la: ela
  // volta à lista. Desmarcá-la depois a tira, com a área ou sem.
  function markCamera(cam: string) {
    if (checkedCams && !checkedCams.includes(cam)) {
      const novas = [...checkedCams, cam];
      checkedCams = novas.length === cameras.list.length ? null : novas;
    }
  }

  function applyArea(points: Point[], rule: Rule) {
    if (!editor) return;
    const { cam, stayAt } = editor;
    area = { cam, points, rule };
    editor = null;
    markCamera(cam);
    void restart(stayAt);
  }

  // O "salvar na câmera" do editor, só do admin: grava a área desenhada no
  // cadastro e o filtro passa a apontar para ela, como se tivesse sido
  // marcada no popover. Relê o cadastro antes, para não devolver ao servidor
  // uma câmera que alguém mudou depois que esta tela abriu.
  async function saveArea(points: Point[], rule: Rule, name: string) {
    if (!editor) return;
    const id = editor.cam;
    // O editor pode ter fechado, ou ter sido aberto de novo, durante a gravação.
    const target = editor;
    const stillOpen = () => editor === target;
    try {
      await loadCameras();
      // O loadCameras guarda o erro em vez de lançá-lo: sem parar aqui, a
      // câmera iria da cópia velha e desfaria no servidor o que mudou depois.
      if (cameras.error) throw new Error(cameras.error);
      const registry = cameras.list.find((c) => c.id === id);
      if (!registry) throw new Error('a câmera saiu do cadastro');
      const { dir: _dir, ...cam } = registry;
      const newOne = { name, rule, points: registryPoints(points) };
      const r = await api.saveCamera({ ...cam, areas: [...(cam.areas ?? []), newOne] });
      const saveIt = r.camera.areas?.find((a) => a.name === name);
      await loadCameras();
      if (stillOpen()) editor = null;
      if (area?.cam === id) area = null;
      if (saveIt?.id) {
        checkedAreas = [...checkedAreas, `${id}/${saveIt.id}`];
        markCamera(id);
      }
      void restart(target.stayAt);
    } catch (e) {
      throw new Error(`não deu para salvar: ${messageOf(e)}`, { cause: e });
    }
  }

  // Liga ou desliga uma área cadastrada. O popover fica aberto: escolher
  // várias soma. `ficarEm` é o instante onde a lista recomeça, pelo ⋮.
  function toggleArea(k: string, stayAt: number | null = null) {
    const a = registered.get(k);
    if (!a) return;
    if (checkedAreas.includes(k)) {
      checkedAreas = checkedAreas.filter((x) => x !== k);
      void (stayAt === null ? restartInPlace() : restart(stayAt));
      return;
    }
    checkedAreas = [...checkedAreas, k];
    markCamera(a.cam);
    void restart(stayAt);
  }

  // Volta à lista sem sair de onde estava, como o limpaIntervalo: tirar
  // filtro só acrescenta detecções.
  function restartInPlace() {
    const ms = scrollRange.ini > 0 ? atTop?.det.instanteMs : undefined;
    return restart(ms != null ? ms + 1 : null);
  }

  // Tira a área desenhada; as cadastradas marcadas ficam.
  function removeDrawn() {
    area = null;
    void restartInPlace();
  }

  // O ✕ do chip: tira todas.
  function clearArea() {
    area = null;
    checkedAreas = [];
    openedArea = false;
    void restartInPlace();
  }

  // O chip abre o popover das áreas. Sem área cadastrada não há o que
  // escolher, e ele reabre o editor da área desenhada.
  function reopenArea() {
    if (area && !anyRegistered) {
      void openEditor(area.cam);
      return;
    }
    openedArea = !openedArea;
  }

  // As miniaturas do chip, no lugar do ícone: uma por área, empilhadas, até
  // MINIATURAS_NO_CHIP - além disso a pilha só alarga o chip.
  const chipThumbnails = $derived.by(() => {
    const out: { cam: string; points: Point[] }[] = [];
    if (area) out.push({ cam: area.cam, points: area.points });
    for (const k of checkedAreas) {
      const a = registered.get(k);
      if (a) out.push({ cam: a.cam, points: a.points });
    }
    return out.slice(0, THUMBNAILS_IN_CHIP);
  });

  // O que o chip diz: o nome da área quando é uma só, ou quantas.
  const chipLabel = $derived.by(() => {
    const n = checkedAreas.length + (area ? 1 : 0);
    if (n !== 1) return { nome: `${n} áreas`, where: '' };
    if (area) return { nome: names[area.cam] ?? area.cam, where: 'área' };
    const a = registered.get(checkedAreas[0] ?? '');
    return { nome: a?.name ?? '', where: a ? (names[a.cam] ?? a.cam) : '' };
  });

  function cliqueFora(e: MouseEvent) {
    outsideCameras(e);
    outsideArea(e);
  }

  // O alvo já fora da página é um botão do popover que mudou de lugar: o
  // primeiro toque numa área troca o botão "área" pelo chip, e o popover vai
  // junto.
  function outsideArea(e: MouseEvent) {
    if (!(e.target instanceof Node) || !e.target.isConnected) return;
    if (openedArea && areaEl && !areaEl.contains(e.target)) openedArea = false;
  }

  // --- ciclo de vida ----------------------------------------------------------

  // Largura nova: as colunas e o tamanho da miniatura mudam juntos, e a
  // âncora de antes devolve a vista à mesma detecção.
  async function changeWidth(newOne: number) {
    const a = viewAnchor;
    width = newOne;
    desktop = window.innerWidth >= DESKTOP_PX;
    gridWindow = window.innerWidth;
    await restoreAnchor(a);
    anchorY = window.scrollY;
    verify();
  }

  onMount(() => {
    // O header e a faixa de versão nova, quando ela aparece: os dois grudam no
    // topo, e a barra desta tela se encosta embaixo deles.
    const cab = document.querySelector<HTMLElement>('#app > .top');
    const measureAll = () => {
      cabH = cab?.offsetHeight ?? 0;
      barH = barEl?.offsetHeight ?? 0;
      const newOne = listEl?.clientWidth ?? 0;
      if (width && newOne !== width) void changeWidth(newOne);
      else width = newOne;
      scrolled();
    };
    // Já agora, e não só quando o observador responder: a primeira página é
    // do tamanho da tela, e sem largura a conta dela sai errada.
    measureAll();
    const ro = new ResizeObserver(measureAll);
    if (cab) ro.observe(cab);
    if (barEl) ro.observe(barEl);
    if (listEl) ro.observe(listEl);

    void (async () => {
      if (!cameras.list.length) await loadCameras();
      checkedCams = urlIds(
        'cams',
        cameras.list.map((c) => c.id),
      );
      if (area && !exists(area.cam)) area = null;
      checkedAreas = checkedAreas.filter((k) => registered.has(k));
      resolvedCameras = true;
      await restart();
    })();

    const t = setInterval(() => {
      void novas();
    }, NOVAS_MS);
    return () => {
      ro.disconnect();
      clearInterval(t);
      closeNotice(undoNotice);
      cancelAnimationFrame(requestNo);
      generation++;
    };
  });
</script>

<svelte:window
  onscroll={() => {
    menu = null;
    scrolled();
  }}
  onresize={scrolled}
  onclick={cliqueFora}
  onkeydown={keyHandler}
/>

<div class="screen" style:--cab-h="{cabH}px" style:--barra-h="{barH}px">
  <div class="bar" bind:this={barEl}>
    <div class="when">
      <DayNav
        value={interval?.day ?? topDay}
        days={daysWithDetection}
        oQue="detecção"
        counts={detectionsPerDay}
        onchange={goToDay}
        onopen={() => {
          // A contagem de hoje cresce com a tela aberta, e a lista só é lida
          // quando o filtro muda.
          if (resolvedCameras) void loadDays(filter, true);
        }}
      />
      <Clock ms={topMs} ongo={goToHour} segundos={false} />
      {#if !interval}
        <ScheduleInterval interval={null} onapply={applyInterval} onclear={resetInterval} />
      {/if}
      {#if !withArea}
        <div class="area-button anchor-area" bind:this={areaEl}>
          <button
            class="ghost open-area"
            onclick={() => (openedArea = !openedArea)}
            aria-expanded={openedArea}
            aria-label="filtrar por área"
            title="mostrar só as detecções dentro de uma área da imagem"
          >
            <Icon name="area" size={16} stroke={2.2} />
            <span class="label">área</span>
          </button>
          {#if openedArea}
            {@render areasPopover()}
          {/if}
        </div>
      {/if}
    </div>

    <!-- Ligados, os chips não cabem na linha do dia no celular: ganham uma
         linha só deles, que dividem quando são dois. -->
    {#if interval !== null || withArea}
      <div class="chips">
        {#if interval}
          <div class="slot-chip">
            <ScheduleInterval {interval} onapply={applyInterval} onclear={resetInterval} />
          </div>
        {/if}
        {#if withArea}
          <div class="slot-chip anchor-area with-chip" bind:this={areaEl}>
            <span class="chip-area">
              <button
                class="ghost reopen"
                onclick={reopenArea}
                aria-expanded={anyRegistered ? openedArea : undefined}
                title={area && !anyRegistered
                  ? `mudar a área (${RULES[area.rule].nome})`
                  : 'mudar as áreas'}
                aria-label="{chipLabel.nome}{chipLabel.where ? `, ${chipLabel.where}` : ''}; mudar"
              >
                <span class="stack">
                  {#each chipThumbnails as m, i (i)}
                    <AreaThumbnail cam={m.cam} areas={[m]} width={40} drawing="cheia" />
                  {:else}
                    <Icon name="area" size={14} stroke={2.4} />
                  {/each}
                </span>
                <span class="area-name">{chipLabel.nome}</span>
                {#if chipLabel.where}
                  <span class="muted where-area">· {chipLabel.where}</span>
                {/if}
              </button>
              <button
                class="ghost clear"
                onclick={clearArea}
                aria-label="limpar as áreas"
                title="limpar as áreas"
              >
                <Icon name="close" size={16} stroke={2.2} />
              </button>
            </span>
            {#if openedArea}
              {@render areasPopover()}
            {/if}
          </div>
        {/if}
      </div>
    {/if}

    <div class="families" role="group" aria-label="famílias">
      {#each FAMILY_ORDER as f (f)}
        {@const on = checkedFamilies === null || checkedFamilies.includes(f)}
        <button
          class="ghost family"
          class:on
          aria-pressed={on}
          aria-label={FAMILIES[f].nome}
          title={on ? `ocultar ${FAMILIES[f].nome}` : `mostrar ${FAMILIES[f].nome}`}
          onclick={() => toggleFamily(f)}
        >
          <img src={iconURL(f, 14, FAMILIES[f].color)} alt="" width="14" height="14" />
          <span class="label">{FAMILIES[f].nome}</span>
        </button>
      {/each}
      {#if anyAlert || onlyAlerts}
        <button
          class="ghost family alerts"
          class:on={onlyAlerts}
          aria-pressed={onlyAlerts}
          aria-label="só alertas"
          title={onlyAlerts
            ? 'mostrar todas as detecções'
            : 'mostrar só as detecções que alertaram'}
          onclick={toggleAlerts}
        >
          <Icon name="bell" size={14} stroke={2.4} />
          <span class="label">só alertas</span>
        </button>
      {/if}
    </div>

    <div class="controls" role="group" aria-label="miniaturas">
      <button
        class="ghost"
        class:on={withBoxes}
        aria-pressed={withBoxes}
        aria-label="caixas"
        title={withBoxes ? 'esconder as caixas dos objetos' : 'mostrar as caixas dos objetos'}
        onclick={() => (withBoxes = !withBoxes)}
      >
        <Icon name="boxes" size={14} stroke={2.4} />
        <span class="label">caixas</span>
      </button>
      <button
        class="ghost"
        class:on={refreshing}
        aria-pressed={refreshing}
        aria-label="atualizar"
        title={refreshing
          ? 'parar de buscar sozinho as detecções novas'
          : `buscar sozinho as detecções novas, a cada ${NOVAS_MS / 1000} s`}
        onclick={toggleRefresh}
      >
        <Icon name="refresh" size={14} stroke={2.4} />
        <span class="label">atualizar</span>
      </button>
      <span class="spacer"></span>
      <button
        class="ghost zoom"
        onclick={() => changeColumns(1)}
        disabled={columns >= strip.max}
        title="miniaturas menores"
        aria-label="miniaturas menores"
      >
        <Icon name="minus" size={18} />
      </button>
      <button
        class="ghost zoom"
        onclick={() => changeColumns(-1)}
        disabled={columns <= strip.min}
        title="miniaturas maiores"
        aria-label="miniaturas maiores"
      >
        <Icon name="mais" size={18} />
      </button>
    </div>

    <div class="cams" bind:this={camsEl}>
      <!-- O ícone daqui tinha 16 de viewBox e traço 1.4: o 2.1 é o mesmo
           traço no viewBox 24 do componente. -->
      <CamerasButton
        checkedList={nChecked}
        total={cameras.list.length}
        all={checkedCams === null}
        isOpen={openedCameras}
        onclick={() => (openedCameras = !openedCameras)}
        --stroke="2.1"
      />
      {#if openedCameras}
        <div class="popover">
          <CameraPicker
            cameras={cameras.list}
            {checked}
            all={checkedCams === null}
            ontoggle={toggleCamera}
            onall={allCameras}
            error={cameras.error}
          />
        </div>
      {/if}
    </div>
  </div>

  {#if limites && interval && endAbove && loaded && list.length}
    <div class="edge">
      <span>fim do intervalo <strong class="mono">{hourOfMinute(interval.to)}</strong></span>
      <span>· {longDay(limites.to - 1)}</span>
      <button class="ghost" onclick={resetInterval}>limpar intervalo</button>
    </div>
  {/if}

  {#if stuckHour}
    <div class="stuck" aria-hidden="true">
      <div class="hour">
        <strong class="mono">{stuckHour.label}</strong>
        <span class="muted small">{stuckHour.day}</span>
      </div>
    </div>
  {/if}

  <div
    class="list"
    class:no-boxes={!withBoxes}
    style:height="{layout.height}px"
    style:--cols={columns}
    style:--vao="{grid.vao}px"
    bind:this={listEl}
  >
    {#each vistas as l (l.k)}
      {#if l.tipo === 'hora'}
        <div class="hour" style:transform="translateY({l.y}px)">
          <strong class="mono">{l.label}</strong>
          <span class="muted small">{l.day}</span>
        </div>
      {:else if l.tipo === 'grade'}
        <div
          class="row"
          class:compact={columns >= (desktop ? 7 : 4)}
          style:transform="translateY({l.y}px)"
          style:height="{l.height}px"
        >
          {#each l.items as d (key(d))}
            <div class="cell">
              <button
                class="tile"
                onclick={() => (isOpen = key(d))}
                aria-label="abrir {hhmmss(d.instanteMs)}, {names[d.cam] ?? d.cam}, {familyNames(
                  familiesOf(d),
                )}"
              >
                <span class="image" style={imageArea(d.cam)}>
                  {#if d.temQuadro}
                    <img
                      src={mediaURL.frame(d.cam, d.instanteMs)}
                      alt=""
                      decoding="async"
                      onload={(e) => {
                        if (e.currentTarget instanceof HTMLImageElement)
                          learn(d.cam, e.currentTarget);
                      }}
                    />
                  {/if}
                  {#if outlinesOf(d).length}
                    <svg
                      class="outline"
                      viewBox="0 0 1 1"
                      preserveAspectRatio="none"
                      aria-hidden="true"
                    >
                      {#each outlinesOf(d) as p, j (j)}
                        <polygon points={svgPoints(p)} />
                      {/each}
                    </svg>
                  {/if}
                  {#each d.objetos as o, j (j)}
                    {#if o.caixa}
                      {@const fora = onlyAlerts && !o.alert}
                      {@const side =
                        !fora && scoreSide(d.cam, o.caixa, columns >= (desktop ? 7 : 4))}
                      <span
                        class="box"
                        class:fora
                        style:left={start(o.caixa[0])}
                        style:top={start(o.caixa[1])}
                        style:width={size(o.caixa[0], o.caixa[2])}
                        style:height={size(o.caixa[1], o.caixa[3])}
                        style:border-color={family(o.familia).color}
                      >
                        {#if side}
                          <span class="score mono {side}" style:background={family(o.familia).color}
                            >{score(o.score)}</span
                          >
                        {/if}
                      </span>
                    {/if}
                  {/each}
                </span>
                <FamilyBadge families={familiesOf(d)} alert={alerted(d)} />
                <span class="caption">
                  <span class="mono">{hhmmss(d.instanteMs)}</span>
                  <span class="name">{names[d.cam] ?? d.cam}</span>
                  {#if onlyAlerts}
                    {@const alertNames = alertAreas(d).map((a) => a.nome)}
                    {#if alertNames.length}
                      <span class="alert-area">{alertNames.join(', ')}</span>
                    {/if}
                  {/if}
                </span>
              </button>
              <button
                class="mais"
                onclick={(e: MouseEvent & { currentTarget: HTMLButtonElement }) => {
                  openMenu(d, e);
                }}
                aria-label="opções da câmera {names[d.cam] ?? d.cam}"
                aria-haspopup="menu"
                aria-expanded={menu?.k === key(d)}
                ><span aria-hidden="true"><Icon name="menu" size="100%" /></span></button
              >
            </div>
          {/each}
        </div>
      {/if}
    {/each}
  </div>

  {#if error}
    <p class="empty">Não deu para buscar as detecções: {error}</p>
  {:else if !loaded}
    <p class="empty">carregando…</p>
  {:else if !filter}
    <p class="empty">
      Nenhuma {checkedCams?.length === 0 ? 'câmera marcada' : 'família marcada'}.
    </p>
  {:else if !list.length && endBelow && endAbove && interval}
    <div class="empty">
      <p>
        Nenhuma detecção entre {hourOfMinute(interval.from)} e {hourOfMinute(interval.to)}.
      </p>
      <button class="ghost" onclick={resetInterval}>limpar intervalo</button>
    </div>
  {:else if !list.length && endBelow && endAbove}
    <p class="empty">Nenhuma detecção com esses filtros.</p>
  {:else if endBelow && list.length && limites && interval}
    <div class="edge">
      <span>início do intervalo <strong class="mono">{hourOfMinute(interval.from)}</strong></span>
      <span>· {longDay(limites.from)}</span>
      <button class="ghost" onclick={resetInterval}>limpar intervalo</button>
    </div>
  {:else if endBelow && list.length}
    <p class="end muted small">Não há detecções mais antigas.</p>
  {:else if !endBelow}
    <p class="end muted small">carregando…</p>
  {/if}
</div>

{#if openedDet}
  {@const d = openedDet}
  {@const previous = list[openIndex + 1]}
  {@const goNext = list[openIndex - 1]}
  {#key isOpen}
    <DetectionSheet
      det={d}
      camera={names[d.cam] ?? d.cam}
      onclose={() => (isOpen = null)}
      onprevious={previous ? () => (isOpen = key(previous)) : null}
      onnext={goNext ? () => (isOpen = key(goNext)) : null}
      onhide={() => {
        void hideCamera(d.cam);
      }}
      areas={outlines[d.cam] ?? null}
      alertAreas={alertAreas(d)}
      onarea={() => void openEditor(d.cam, d)}
    />
  {/key}
{/if}

<!-- O popover das áreas, aberto pelo botão "área" ou pelo chip. Sem área
     cadastrada, é a escolha da câmera para desenhar. Com elas, cada câmera
     lista as suas, para marcar uma ou várias, e o "desenhar" daquela
     câmera. -->
{#snippet areasPopover()}
  <div class="popover popover-area" role="dialog" aria-label="áreas">
    {#if !anyRegistered}
      <p class="muted small">Desenhar a área sobre a imagem de qual câmera?</p>
      <div class="cams-list">
        {#each cameras.list as c (c.id)}
          <button class="ghost" onclick={() => void openEditor(c.id)}>{c.name}</button>
        {/each}
      </div>
    {:else}
      <p class="muted small">Só as detecções dentro das áreas marcadas. Várias somam.</p>
      <div class="cams-list">
        {#each cameras.list as c (c.id)}
          <div class="areas-cam">
            <span class="cam-title">{c.name}</span>
            {#if c.areas?.length}
              <div class="tiles">
                {#each c.areas as a (a.id)}
                  {@const k = `${c.id}/${a.id}`}
                  {@const on = checkedAreas.includes(k)}
                  <AreaTile cam={c.id} area={a} {on} onclick={() => toggleArea(k)} />
                {/each}
              </div>
            {/if}
            <div class="areachips">
              {#if area?.cam === c.id}
                <button
                  class="areachip on"
                  aria-pressed="true"
                  title="tirar a área desenhada"
                  onclick={removeDrawn}
                >
                  desenhada
                </button>
              {/if}
              <button class="ghost draw" onclick={() => void openEditor(c.id)}>
                <Icon name="area" size={14} stroke={2.2} />
                {area?.cam === c.id ? 'redesenhar' : 'desenhar'}
              </button>
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </div>
{/snippet}

{#if editor && AreaEditor}
  {@const fromCamera = cameras.list.find((c) => c.id === editor?.cam)?.areas ?? []}
  <AreaEditor
    camera={names[editor.cam] ?? editor.cam}
    background={editor.background}
    boxes={editor.boxes}
    error={editor.erro}
    initial={area?.cam === editor.cam ? area : null}
    otherAreas={fromCamera}
    namesInUse={fromCamera.map((a) => a.name)}
    save={isAdmin() ? { full: fromCamera.length >= MAX_AREAS, onsave: saveArea } : null}
    onapply={applyArea}
    onclose={() => (editor = null)}
  />
{/if}

{#if menu && menuPosition}
  {@const m = menu}
  {@const p = menuPosition}
  <div
    class="menu"
    role="menu"
    bind:this={menuEl}
    style:right="{m.x}px"
    style:top={p.upward ? null : `${m.base + 4}px`}
    style:bottom={p.upward ? `${window.innerHeight - m.topPos + 4}px` : null}
    style:max-height="{p.max}px"
  >
    {#if m.areas}
      <button class="ghost back" role="menuitem" onclick={() => (m.areas = false)}>
        <Icon name="chevronEsq" size={14} stroke={2.4} />
        Filtrar por área · <strong>{names[m.cam] ?? m.cam}</strong>
      </button>
      <hr />
      {#each areasOf(m.cam) as a (a.id)}
        {@const k = `${m.cam}/${a.id}`}
        {@const on = checkedAreas.includes(k)}
        <button
          class="ghost menu-area"
          role="menuitemcheckbox"
          aria-checked={on}
          onclick={() => toggleMenuArea(k)}
        >
          <span class="mark">
            {#if on}<Icon name="checkmark" size={14} stroke={2.6} />{/if}
          </span>
          <AreaLabel cam={m.cam} area={a} />
        </button>
      {/each}
      {#if area?.cam === m.cam}
        <button
          class="ghost menu-area"
          role="menuitemcheckbox"
          aria-checked="true"
          onclick={removeMenuDrawn}
        >
          <span class="mark"><Icon name="checkmark" size={14} stroke={2.6} /></span>
          <span class="name">desenhada</span>
        </button>
      {/if}
      <hr />
      <button class="ghost draw" role="menuitem" onclick={drawMenu}>
        <Icon name="area" size={14} stroke={2.2} />
        {area?.cam === m.cam ? 'Redesenhar' : 'Desenhar'} sobre este quadro
      </button>
    {:else}
      {#if exists(m.cam)}
        <a
          class="item"
          role="menuitem"
          href="#live?cams={encodeURIComponent(m.cam)}"
          onclick={() => (menu = null)}
        >
          Ir para o ao vivo de <strong>{names[m.cam] ?? m.cam}</strong>
        </a>
        <a
          class="item"
          role="menuitem"
          href={recordingsLink(m.cam, m.instanteMs)}
          onclick={() => (menu = null)}
        >
          Abrir em Gravações
        </a>
        <button class="ghost filter-area" role="menuitem" onclick={filterByMenuArea}>
          Filtrar por área
          {#if areasOf(m.cam).length}
            <Icon name="chevronDir" size={14} stroke={2.4} />
          {/if}
        </button>
      {/if}
      <button
        class="ghost"
        role="menuitem"
        onclick={() => {
          void hideCamera(m.cam);
        }}
      >
        Ocultar a câmera <strong>{names[m.cam] ?? m.cam}</strong>
      </button>
    {/if}
  </div>
{/if}

<style>
  .screen {
    padding: 0 10px 8px;
  }

  /* --- a barra --- */

  .bar {
    position: sticky;
    top: var(--cab-h);
    z-index: 10;
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: center;
    gap: 8px;
    margin: 0 -10px;
    padding: 10px;
    background: var(--bg);
    border-bottom: 1px solid var(--line);
  }

  .when {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  /* No celular: dia, hora e câmeras na primeira linha; famílias e controles
     das miniaturas na segunda, todos só com o ícone. Com o nome escrito eles
     pedem ~470 px numa linha de 370. As três colunas deixam cada linha se
     dividir do seu jeito: a 1ª é a largura das famílias, a 3ª a das câmeras. */
  .when {
    grid-column: 1 / 3;
    grid-row: 1;
  }
  .cams {
    grid-column: 3;
    grid-row: 1;
    position: relative;
  }
  .families {
    grid-column: 1;
    grid-row: 2;
  }
  .controls {
    grid-column: 2 / 4;
    grid-row: 2;
  }
  .bar .label {
    display: none;
  }
  /* O intervalo e a área ligados: no celular, uma 3ª linha inteira, que os
     dois chips dividem. */
  .chips {
    grid-column: 1 / 4;
    grid-row: 3;
    display: flex;
    gap: 6px;
  }
  .slot-chip {
    flex: 1;
    min-width: 0;
  }

  .area-button {
    flex: none;
  }
  .with-chip {
    display: flex;
  }
  .with-chip .chip-area {
    flex: 1;
    min-width: 0;
  }
  .open-area {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 8px 11px;
    color: var(--dim);
  }
  .popover-area {
    left: 10px;
    right: auto;
    top: 100%;
    width: min(360px, calc(100% - 20px));
  }
  .popover-area p {
    margin: 0;
  }
  .cams-list {
    display: grid;
    gap: 4px;
    max-height: 50vh;
    overflow: auto;
  }
  .cams-list > button {
    text-align: left;
    border: 0;
  }
  .areas-cam {
    display: grid;
    gap: 6px;
    padding: 6px 0;
  }
  .areas-cam + .areas-cam {
    border-top: 1px solid var(--line);
  }
  .cam-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--dim);
  }
  /* Os ladrilhos das áreas, dois por linha, como na aba Alerta do cadastro. */
  .tiles {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 6px;
  }
  .areachips {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  /* A forma do chip do SeletorCameras: ponto verde e borda azul na marcada. */
  .areachip {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    min-height: 36px;
    padding: 0 12px;
    border-radius: 10px;
    font-weight: 600;
    font-size: 14px;
    color: var(--dim);
    max-width: 100%;
    overflow-wrap: anywhere;
  }
  .areachip::before {
    content: '';
    flex: none;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--line);
  }
  .areachip.on {
    color: var(--fg);
    border-color: var(--accent);
    background: var(--ativo-fundo);
  }
  .areachip.on::before {
    background: var(--ok);
  }
  .draw {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-height: 36px;
    padding: 0 10px;
    border: 0;
    color: var(--dim);
  }

  /* O chip da área, com a forma do chip do intervalo. */
  .chip-area {
    display: inline-flex;
    align-items: stretch;
    border: 1px solid var(--accent);
    border-radius: 8px;
    background: var(--ativo-fundo);
    overflow: hidden;
  }
  .chip-area button {
    min-height: 42px;
    border: 0;
    border-radius: 0;
  }
  .reopen {
    flex: 1;
    min-width: 0;
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 0 4px 0 11px;
    white-space: nowrap;
  }
  /* As miniaturas das áreas do filtro, uma sobre a outra, cada uma com um
     respiro da cor do chip para não se fundirem. */
  .stack {
    display: inline-flex;
    flex: none;
  }
  .stack > :global(canvas + canvas) {
    margin-left: -16px;
    box-shadow: -2px 0 0 var(--ativo-fundo);
  }
  .area-name,
  .where-area {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .chip-area .clear {
    padding: 0 10px;
    color: var(--dim);
  }

  /* O contorno da área sobre a miniatura: fino e tracejado, para não cobrir
     o objeto. */
  .outline {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    pointer-events: none;
  }
  .outline polygon {
    fill: none;
    stroke: var(--accent);
    stroke-width: 1.5;
    stroke-dasharray: 5 3;
    vector-effect: non-scaling-stroke;
  }

  .families {
    display: flex;
    gap: 6px;
  }

  .family {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 7px;
    padding: 8px 11px;
    color: var(--dim);
  }

  .family.on {
    color: var(--fg);
    border-color: var(--accent);
  }
  .family img {
    display: block;
    flex: none;
  }
  .family:not(.on) img {
    opacity: 0.4;
  }

  /* O "só alertas" ligado na cor do sino, e não na das famílias: é outro
     filtro, que diz "menos", e não "estas". */
  .alerts.on {
    color: var(--warn);
    border-color: var(--warn);
  }

  /* Os controles das miniaturas têm a forma das famílias ao lado: o mesmo
     botão, o mesmo "ligado". */
  .controls {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .controls button {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 8px 11px;
    color: var(--dim);
  }

  .controls button.on {
    color: var(--fg);
    border-color: var(--accent);
  }

  .controls .zoom {
    justify-content: center;
    min-width: 40px;
    color: var(--fg);
  }

  .popover {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    z-index: 40;
    width: min(300px, calc(100vw - 20px));
    display: grid;
    gap: 10px;
    padding: 12px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  }

  /* --- a grade --- */

  .list {
    position: relative;
    /* As linhas são absolutas: a ancoragem de rolagem do navegador não teria
       em que se apoiar, e quem segura a vista é a âncora da tela. */
    overflow-anchor: none;
  }

  .hour {
    position: absolute;
    left: -10px;
    right: -10px;
    height: 40px;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 12px;
    background: var(--bg);
  }

  /* O cabeçalho que gruda logo abaixo da barra. Altura zero no fluxo: ele não
     empurra a grade, só se deita sobre ela. */
  .stuck {
    position: sticky;
    top: calc(var(--cab-h) + var(--barra-h));
    z-index: 5;
    height: 0;
  }

  .row {
    position: absolute;
    left: 0;
    right: 0;
    display: grid;
    grid-template-columns: repeat(var(--cols), minmax(0, 1fr));
    gap: var(--vao);
  }

  .cell {
    position: relative;
    min-width: 0;
    height: 100%;
  }

  .tile {
    position: relative;
    display: block;
    width: 100%;
    height: 100%;
    min-height: 0;
    padding: 0;
    border: 0;
    border-radius: 8px;
    overflow: hidden;
    background: #000;
    text-align: left;
  }

  .image {
    position: absolute;
  }

  .image img {
    display: block;
    width: 100%;
    height: 100%;
  }

  .box {
    position: absolute;
    border: 2px solid;
    pointer-events: none;
  }

  /* Com o "só alertas", o objeto que não entrou no alerta fica na detecção,
     mas apagado. */
  .box.fora {
    opacity: 0.35;
    border-style: dashed;
  }

  /* O score do detector de objetos, colado na caixa e sempre do lado de fora:
     o lado vem de ladoDoScore. Os -2px e os 2px são a borda da caixa. */
  .score {
    position: absolute;
    padding: 0 3px;
    font:
      600 10px/13px system-ui,
      -apple-system,
      'Segoe UI',
      sans-serif;
    color: #0d1117;
    white-space: nowrap;
  }

  .score.above {
    left: -2px;
    bottom: 100%;
    border-radius: 3px 3px 3px 0;
  }

  .score.below {
    left: -2px;
    top: 100%;
    border-radius: 0 3px 3px 3px;
  }

  .score.right {
    left: calc(100% + 2px);
    top: -2px;
    border-radius: 0 3px 3px 3px;
  }

  .score.left {
    right: calc(100% + 2px);
    top: -2px;
    border-radius: 3px 0 3px 3px;
  }

  .compact .score {
    font-size: 9px;
    line-height: 11px;
    padding: 0 2px;
  }

  .no-boxes .box {
    display: none;
  }

  /* O ⋮ no canto, à direita dos ícones das famílias e com a mesma pastilha
     deles. A pastilha é pequena, mas o alvo do toque é o canto inteiro. */
  .mais {
    position: absolute;
    top: 0;
    right: 0;
    min-height: 0;
    width: 40px;
    height: 36px;
    padding: 4px 4px 0 0;
    border: 0;
    background: none;
    display: flex;
    justify-content: flex-end;
    align-items: flex-start;
  }

  .mais span {
    display: block;
    width: 22px;
    height: 20px;
    padding: 2px;
    border-radius: 6px;
    background: rgba(13, 17, 23, 0.8);
    color: var(--fg);
  }

  .mais:focus-visible {
    outline: none;
  }
  .mais:focus-visible span,
  .mais[aria-expanded='true'] span {
    outline: 2px solid var(--accent);
  }

  /* O selo das famílias abre espaço para o ⋮ à direita dele. */
  .cell :global(.badge) {
    right: 30px;
  }

  /* Com mouse o ⋮ aparece só sobre a miniatura, e fica enquanto o menu dele
     está aberto. No toque não há hover: ele fica sempre à vista. */
  @media (hover: hover) {
    .mais {
      opacity: 0;
    }
    .cell:hover .mais,
    .mais:focus-visible,
    .mais[aria-expanded='true'] {
      opacity: 1;
    }
    .cell:hover .mais span {
      background: rgba(13, 17, 23, 0.95);
    }
  }

  /* Na miniatura pequena o ⋮ não se acerta com o dedo: a ação fica na folha. */
  .compact .mais {
    display: none;
  }
  .compact :global(.badge) {
    right: 4px;
  }

  .menu {
    position: fixed;
    z-index: 40;
    max-width: calc(100vw - 20px);
    overflow-y: auto;
    padding: 4px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  }

  .menu button,
  .menu .item {
    display: block;
    width: 100%;
    border: 0;
    text-align: left;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .menu .item {
    box-sizing: border-box;
    padding: 8px 10px;
    color: inherit;
    text-decoration: none;
  }

  .menu hr {
    margin: 4px 0;
    border: 0;
    border-top: 1px solid var(--line);
  }

  /* A página das áreas: a marca, o nome e a regra numa linha. */
  .menu .back,
  .menu .menu-area,
  .menu .filter-area,
  .menu .draw {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .menu .back {
    color: var(--dim);
  }

  .menu .filter-area :global(svg) {
    margin-left: auto;
    color: var(--dim);
  }

  .menu .mark {
    display: flex;
    flex: none;
    width: 14px;
    color: var(--ok);
  }

  .menu .menu-area .name {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  @media (hover: hover) {
    .menu button:hover,
    .menu .item:hover {
      background: var(--panel-2);
    }
  }

  .caption {
    position: absolute;
    left: 4px;
    bottom: 4px;
    max-width: calc(100% - 8px);
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 0 6px;
    border-radius: 6px;
    background: rgba(13, 17, 23, 0.8);
    font-size: 11px;
    line-height: 18px;
    color: var(--fg);
  }

  .name,
  .alert-area {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .alert-area {
    color: var(--warn);
  }

  /* Na miniatura pequena não cabe o nome da câmera: fica só a hora. */
  .compact .name,
  .compact .alert-area {
    display: none;
  }
  .compact .caption {
    font-size: 10px;
    line-height: 16px;
    padding: 0 4px;
  }
  .compact :global(.badge) {
    padding: 2px 3px;
  }
  .compact :global(.badge img) {
    width: 10px;
    height: 10px;
  }

  .end {
    text-align: center;
    padding: 16px 0;
  }

  /* As pontas do intervalo, no começo e no fim da lista: dizem onde ela para
     e oferecem a volta à rolagem infinita ali mesmo. */
  .edge {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: center;
    gap: 4px 8px;
    margin: 10px 0;
    padding: 10px 12px;
    border: 1px dashed var(--line);
    border-radius: var(--radius);
    color: var(--dim);
    font-size: 13px;
    text-align: center;
  }
  .edge strong {
    color: var(--fg);
    font-weight: 600;
  }
  .edge button,
  .empty button {
    min-height: 34px;
    padding: 4px 12px;
    font-size: 13px;
    color: var(--accent);
  }
  .empty p {
    margin: 0 0 12px;
  }

  @media (min-width: 720px) {
    .screen {
      padding: 0 18px 8px;
    }

    /* Do tablet para cima os nomes voltam, mas as duas linhas do celular
       continuam: no iPad em pé (820 px) a barra inteira não cabe numa linha
       só. */
    .bar {
      gap: 10px;
      margin: 0 -18px;
      padding: 10px 18px;
    }

    .bar .label {
      display: inline;
    }

    /* Da largura do tablet para cima, os chips cabem na linha do dia. */
    .bar:has(> .chips) .when {
      grid-column: 1;
    }
    .chips {
      grid-column: 2;
      grid-row: 1;
      justify-self: start;
    }
    .slot-chip {
      flex: none;
    }
    .anchor-area {
      position: relative;
    }
    .popover-area {
      left: 0;
      top: calc(100% + 6px);
    }

    .hour {
      left: -18px;
      right: -18px;
      padding: 0 20px;
    }
    .caption {
      font-size: 12px;
      line-height: 20px;
    }
  }

  /* Linha única quando tudo cabe: a barra inteira mede ~1210 px. Quem decide
     é o espaço que sobra, e não um número de largura que envelhece a cada
     botão novo: o que não cabe (hoje o botão das câmeras) quebra para a 2ª
     linha, à direita, no lugar de empurrar a página para o lado. */
  /* Com os chips ligados ela não cabe mais, e fica nas duas linhas do
     tablet. O `>` vale: o seletor de câmeras aberto tem um `.chips` dele, e
     sem o `>` a barra voltaria para o grid só por abri-lo. */
  @media (min-width: 1060px) {
    /* O `justify-content` junta à direita o que sobra sozinho numa linha (o
       botão das câmeras, quando quebra); a `margin-right: auto` das famílias
       empurra os controles para a direita na linha de cima. Duas margens
       `auto` dividiriam a folga e afastariam os controles das câmeras. */
    .bar:not(:has(> .chips)) {
      display: flex;
      flex-wrap: wrap;
      justify-content: flex-end;
    }
    .bar:not(:has(> .chips)) .families {
      margin-right: auto;
    }
    .bar:not(:has(> .chips)) .controls {
      margin-left: 0;
    }
    .when {
      flex: none;
    }
    .controls {
      margin-left: auto;
    }
  }
</style>
