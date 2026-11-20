<script lang="ts">
  // Detections: the object detections from all cameras in a single grid, newest
  // to oldest, grouped by hour.
  //
  // A whole day fits neither in a phone's memory nor on the screen, so nothing
  // here is "everything": the server hands out pages by cursor, the screen
  // keeps only a window of them (MAX_ITEMS), and the DOM only has the rows in
  // view, with one extra screen on each side. The rest is arithmetic: the
  // thumbnail is one column wide with a 16:9 ratio, and the hour header has a
  // fixed height - so every row's position is computed without measuring
  // anything, and the scrollbar has the right size from the first frame.
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

  // One row of the grid: the hour header, or a row of thumbnails. `firstIndex`
  // is the index in `list` of the row's first thumbnail.
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

  // The time range: only the detections between `from` and `to` of the `day`,
  // in minutes of the day, with both end minutes fully included. A `to` smaller
  // than `from` ends on the next day (22:00 to 06:00 is the night).
  interface Interval {
    day: string;
    from: number;
    to: number;
  }

  // The detection that holds the view, and its distance from the top.
  interface Anchor {
    k: string;
    dy: number;
  }

  // --- parameters
  // -------------------------------------------------------------

  // Grid columns: zoom moves between the ends, and the default is what the
  // approved mock showed. On a phone the 3-column thumbnail (103 px) was
  // already rejected as too small; 4 stays as an option for whoever wants to
  // sweep through.
  const COLUMNS = {
    phone: { padrao: 2, min: 1, max: 4 },
    desktop: { padrao: 5, min: 3, max: 8 },
  };
  // The same breakpoint as App.svelte, where the navigation moves up to the
  // header.
  const DESKTOP_PX = 720;
  const HOUR_PX = 40; // the hour header between the groups

  // How many screens of detections to load ahead of what is visible, so
  // scrolling does not reach the edge of what has already arrived. And how many
  // screens of DOM to keep beyond the view: enough that a finger fling does not
  // show a gap.
  const FETCH_SCREENS = 1.5;
  const DOM_SCREENS = 1;
  // How many detections to keep in total. Past that, whatever is far from the
  // view, on the side opposite the scroll, is released - and fetched again if
  // the scroll comes back there.
  const MAX_ITEMS = 600;

  // A new detection arrives by itself while the screen is at the top. The
  // object mark is written after the object detector looks, with the onset
  // instant, so a detection can appear BELOW the top that was already shown:
  // the fetch goes back DELAY_MS before the top to catch those too.
  const NOVAS_MS = 20000;
  const DELAY_MS = 5 * 60 * 1000;

  // The height of each row of the ⋮ menu - the area one, with the thumbnail, is
  // taller - and of the rest of it (borders, padding and separators), to open
  // upward when it does not fit below. And how long the "undo" stays on screen
  // after hiding a camera.
  const MENU_ITEM_PX = 36;
  const MENU_AREA_PX = 56;
  const MENU_BEZEL_PX = 20;
  const UNDO_MS = 6000;
  // How many thumbnails the area filter chip stacks.
  const THUMBNAILS_IN_CHIP = 3;

  const FAMILY_ORDER = FAMILY_NAMES;

  // --- URL state
  // --------------------------------------------------------------

  const params = currentParams();

  const urlIds = <T extends string>(k: string, validItems: readonly T[]): T[] | null =>
    params.has(k) ? params.getAll(k).filter((v): v is T => validItems.some((x) => x === v)) : null;

  // Null is "all": it is the default, and stays out of the link.
  let checkedCams = $state<string[] | null>(null);
  let checkedFamilies = $state<FamilyName[] | null>(urlIds('familias', FAMILY_ORDER));
  let chosenColumns = $state<number | null>(Number(params.get('cols')) || null);
  let withBoxes = $state(params.get('caixas') !== '0');
  let refreshing = $state(params.get('atualizar') !== '0');

  // The range comes from the whole link or not at all: `day`, `from` and `to`.
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
    // The round trip rejects a day that does not exist: 2026-02-31 would become
    // 03/03.
    if (dayKey(parseDay(day)) !== day || from === null || to === null) return null;
    return { day, from, to };
  }
  let interval = $state<Interval | null>(urlInterval());

  // The area drawn on the spot: `area` and `rule`, the same names as the API.
  // It restricts only its own camera; the other checked cameras do not change.
  let area = $state<FilterArea | null>(queryArea(params.get('area'), params.get('rule')));
  // The checked registered areas, `<cam>/<id>`, as in the API: the ones from
  // one camera add up, with the drawn one on top. Checked against the registry
  // when the cameras arrive: a deleted area drops out of the link.
  let checkedAreas = $state<string[]>(params.getAll('areas'));
  // Only the detections with an object that alerted: `alert=1`, as in the API.
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

  // --- the detections
  // ---------------------------------------------------------

  // The loaded window, newest to oldest. `$state.raw` because each page
  // replaces the whole array: a proxy over 600 objects with boxes would be cost
  // with no use.
  let list = $state.raw<Detection[]>([]);
  // Whether there is more beyond each end of the window. Above: a detection
  // newer than the loaded top - after a "go to", or after releasing the top.
  let endAbove = $state(true);
  let endBelow = $state(false);
  let loaded = $state(false);
  let fetchingAbove = false;
  let fetchingBelow = false;
  let error = $state('');
  // The instant of the last "go to", which serves as the cursor when it landed
  // before any detection and the list started empty. Reactive because it is
  // what the clock shows when the list is empty.
  let target = $state<number | null>(null);
  // Invalidates the response of a fetch that no longer describes the screen:
  // new filter, new "go to".
  let generation = 0;

  // Live and Recordings only open for a camera that is still in the registry:
  // an old detection can cite a camera removed since then, and the two screens
  // would swap the unknown id for another camera.
  const exists = (cam: string) => cameras.list.some((c) => c.id === cam);

  const key = (d: Detection) => `${d.cam}|${d.instanteMs}`;

  const names = $derived(Object.fromEntries(cameras.list.map((c) => [c.id, c.name])));

  // The registered areas, by `<cam>/<id>`.
  const registered = $derived(
    new Map(
      cameras.list.flatMap((c) =>
        (c.areas ?? []).flatMap((a) => (a.id ? [[`${c.id}/${a.id}`, { cam: c.id, ...a }]] : [])),
      ),
    ),
  );
  const anyRegistered = $derived(registered.size > 0);

  // The outlines of each camera with an area in the filter: the drawn one and
  // the checked ones.
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

  // The "alerts only" button appears when some camera is alerting, or when it
  // is already turned on by the link. The viewer gets only `enabled` from the
  // alert.
  const anyAlert = $derived(cameras.list.some((c) => c.alert?.enabled));

  const alerted = (d: Detection) => d.objetos.some((o) => o.alert);

  // The areas where the detection alerted, with the current name and outline.
  // One that was deleted since then stays without an outline, as "deleted
  // area".
  function alertAreas(d: Detection) {
    const ids = [...new Set(d.objetos.flatMap((o) => o.alertAreas ?? []))];
    return ids.map((id) => {
      const a = registered.get(`${d.cam}/${id}`);
      return { id, nome: a?.name ?? 'área apagada', points: a?.points ?? null };
    });
  }

  // The outlines over the thumbnail: the filter's and, with "alerts only",
  // those of the alert areas. A registered area that is in both is drawn only
  // once.
  function outlinesOf(d: Detection): readonly (readonly Point[])[] {
    const out: (readonly Point[])[] = [...(outlines[d.cam] ?? [])];
    if (!onlyAlerts) return out;
    for (const a of alertAreas(d)) if (a.points && !out.includes(a.points)) out.push(a.points);
    return out;
  }

  // What goes in the request. Null when the filter left nothing checked, and
  // then the server is not even asked.
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

  // The range in instants: `from` included, `to` excluded (the whole final
  // minute stays inside). Null without a range.
  const limites = $derived.by(() => {
    if (!interval) return null;
    const from = parseDay(interval.day);
    from.setMinutes(interval.from);
    const to = parseDay(interval.day);
    if (interval.to < interval.from) to.setDate(to.getDate() + 1);
    to.setMinutes(interval.to + 1);
    return { from: from.getTime(), to: to.getTime() };
  });

  // The detections of a page that fall in the range, and whether any was left
  // out: one that went past the end says the list ended on that side, because
  // the server delivers in order.
  function isInside(ds: Detection[]) {
    if (!limites) return { ds, wasCut: false };
    const { from, to } = limites;
    const ok = ds.filter((d) => d.instanteMs >= from && d.instanteMs < to);
    return { ds: ok, wasCut: ok.length < ds.length };
  }

  // A page is what fills two screens: slightly more than that is already
  // prefetch.
  function page() {
    const rows = Math.ceil(window.innerHeight / (grid.tileHeight + grid.vao)) + 1;
    return Math.min(200, Math.max(24, rows * columns * 2));
  }

  // merges two slices in order, without repeats: the fetch for new ones
  // deliberately goes back over what we already have, and a page can tie with
  // its neighbor.
  function join(a: Detection[], b: Detection[]) {
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- local Set, only used to merge
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

  // With a range, the fetch starts from its upper end - or from the "go to",
  // which is clamped inside, at least the first minute - and the upper end
  // counts as the top of the list.
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
      // Releases the top, which was left behind. The anchor lives in the view
      // and the view is never in the released part: the surplus is much bigger
      // than one screen.
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
      // The empty list is a "go to" with nothing before it - common at the
      // start of a range. The page arrives above the requested instant, and the
      // view goes to its bottom, where the detection closest to it is.
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

  // The new ones, and the late ones just below the top. Only with the top
  // loaded: outside it, scrolling up is what brings them. With a range that has
  // already ended, and past the slack for late ones, there is nothing to fetch.
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
      // Whoever is looking at the top sees the new item come in; whoever
      // scrolled down must not have the screen pushed by it.
      const a = scrollRange.ini > 0 ? anchor() : null;
      list = join(ds, list);
      // More than 200 in the range: the newest were left out, and scrolling up
      // fetches them as it would after a "go to".
      if (!r.fim && !wasCut) endAbove = false;
      recordDay(dayKey(new Date(newer.instanteMs)));
      if (a) await restoreAnchor(a);
    } catch {
      // The next round tries again; a network hiccup does not become a notice.
    }
  }

  // --- the calendar days -----------------------------------------------------

  // The days with detections for the filter, for the calendar and the arrows.
  // Empty is the picker's permissive mode: until the response arrives, or if it
  // fails, every day up to today is valid.
  let daysWithDetection = $state.raw<string[]>([]);
  // How many detections of the filter each day has, which the calendar shows on
  // the day. It does not follow the time range: it is the whole day.
  let detectionsPerDay = $state.raw<Record<string, number>>({});
  // Holds the first query until the camera from the URL is resolved: before
  // that the filter is "all", and the query would go out for nothing - and,
  // with a cold disk, it is the most expensive one on the screen.
  let resolvedCameras = $state(false);

  $effect(() => {
    if (resolvedCameras) void loadDays(filter);
  });

  // `keep` is the re-read for when the calendar opens: the list already there
  // is from the same filter, and a network hiccup does not swap it for the
  // permissive mode.
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

  // Today debuting in the calendar with the screen open: the new detection
  // already tells the day, without re-reading the list. In permissive mode the
  // list stays empty - a single day would take it out of "everything allowed".
  function recordDay(day: string) {
    if (!daysWithDetection.length || daysWithDetection.includes(day)) return;
    daysWithDetection = [...daysWithDetection, day].sort();
  }

  // Turning it back on fetches right away: waiting for the next round would
  // look like it did not take.
  function toggleRefresh() {
    refreshing = !refreshing;
    if (refreshing) void novas();
  }

  // --- the grid, computed
  // -----------------------------------------------------

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

  // The rows in screen order, each with the `y` where it starts. `rowOf[i]`
  // says which row holds detection i, to find the anchor after a swap.
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

  // The view in px of the list: `ini` is what is just below the bar, `fim` the
  // foot of the window. Re-read on every scroll frame.
  let scrollRange = $state({ ini: 0, fim: 0 });
  let barEl: HTMLDivElement | undefined;
  let barH = $state(0);
  // The App header stays stuck to the top on desktop; this screen's bar sticks
  // right below it.
  let cabH = $state(0);

  function measure() {
    if (!listEl) return;
    const listTop = listEl.getBoundingClientRect().top;
    const ini = cabH + barH - listTop;
    const end = window.innerHeight - listTop;
    if (ini !== scrollRange.ini || end !== scrollRange.fim) scrollRange = { ini, fim: end };
  }

  // The anchor of the last whole view, so a width change (rotating the phone)
  // goes back to the same place. Scrolling is in px, and the width changes the
  // height of everything - and the columns, at the DESKTOP_PX breakpoint. It is
  // only re-read while the window has the width the grid was measured with: in
  // the middle of a rotation the grid is still the old one and the scroll has
  // already been clipped by the browser, and the anchor read there would be the
  // wrong one. And only when the scroll leaves the place where the anchor was
  // set: the 5-column row starts at a different detection, and re-reading the
  // anchor after rotating would make it walk one row per rotation.
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

  // What goes to the DOM. The band moves in STEP_PX steps, not on every pixel:
  // the list of rows is only rebuilt when the scroll crosses a step.
  const STEP_PX = 200;
  const visibleWindow = $derived.by(() => {
    const leftover = window.innerHeight * DOM_SCREENS;
    return {
      from: Math.floor((scrollRange.ini - leftover) / STEP_PX) * STEP_PX,
      to: Math.ceil((scrollRange.fim + leftover) / STEP_PX) * STEP_PX,
    };
  });

  // First row whose end is past `y`: binary search, the rows come in order.
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

  // The row where detection i of `list` lives.
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

  // The first row of thumbnails in view: it is what gives the bar its day and
  // hour, and the header that sticks.
  const atTop = $derived.by(() => {
    for (const row of layout.linhas.slice(firstBelow(scrollRange.ini + 1))) {
      if (row.tipo !== 'grade') continue;
      const det = row.items[0];
      if (det) return { row, det };
    }
    return null;
  });
  // With no detection in view, the "go to" applies - minus 1 ms, because it is
  // the exclusive limit of the fetch: going to day 29 fetches before midnight
  // of day 30, and showing the limit itself made the bar flash day 30 until the
  // response arrived.
  const topMs = $derived(atTop?.det.instanteMs ?? (target != null ? target - 1 : Date.now()));

  // The hour header sticks while its thumbnails go by. It is a single one,
  // outside the grid, because the one inside leaves along with its row.
  const stuckHour = $derived.by(() => {
    if (!atTop || scrollRange.ini <= 0) return null;
    const l = layout.linhas;
    for (let i = layout.rowOf[atTop.row.firstIndex] ?? -1; i >= 0; i--) {
      const row = l[i];
      if (row?.tipo === 'hora') return row.y + row.height <= scrollRange.ini ? row : null;
    }
    return null;
  });

  // The anchor is the detection at the top of the view and its distance from
  // there. Changing what is above - a page that arrived, a released top, new
  // items - moves the `y` of everything; restoring the anchor puts the view
  // back in the same place.
  function anchor(): Anchor | null {
    if (!atTop) return null;
    return { k: key(atTop.det), dy: atTop.row.y - scrollRange.ini };
  }

  async function restoreAnchor(a: Anchor | null) {
    await tick();
    if (!a) return;
    // Again: the end of the range, which appears above the list when the scroll
    // reaches it, pushes the list without changing any row's `y`.
    measure();
    const row = detectionRow(list.findIndex((d) => key(d) === a.k));
    if (!row) return;
    const delta = row.y - a.dy - scrollRange.ini;
    if (Math.abs(delta) >= 1) window.scrollBy(0, delta);
    measure();
  }

  // Zoom and width change the height of everything: the anchor holds the view.
  async function changeColumns(step: number) {
    const a = anchor();
    chosenColumns = Math.min(strip.max, Math.max(strip.min, columns + step));
    await restoreAnchor(a);
    verify();
  }

  // --- the thumbnails
  // ---------------------------------------------------------

  // The aspect ratio of each camera, learned from the first image that arrives.
  // The object's box is a fraction of the IMAGE, and on a 4:3 camera inside a
  // 16:9 thumbnail the image does not fill the whole thumbnail.
  let ratios = $state<Record<string, number>>({});

  function learn(cam: string, img: HTMLImageElement) {
    const { naturalWidth: w, naturalHeight: h } = img;
    if (w && h && ratios[cam] !== w / h) ratios[cam] = w / h;
  }

  // Where the camera's image sits inside the thumbnail, in px.
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

  // The size of the score on the thumbnail, the same as the CSS of `.score`
  // (height) plus some slack for "0,84" (width).
  const SCORE_HEIGHT_PX = { normal: 13, compact: 11 };
  const SCORE_WIDTH_PX = { normal: 30, compact: 26 };

  // On which side OUTSIDE the box the score fits entirely in the thumbnail:
  // above, otherwise below, otherwise beside. Never inside, which covers the
  // object - the head of a person near the top. With no room anywhere (box the
  // size of the image), it goes without the score; it is on the sheet.
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

  // The families of a detection, once each.
  function familiesOf(d: Detection) {
    const out: string[] = [];
    for (const o of d.objetos) if (!out.includes(o.familia)) out.push(o.familia);
    return out;
  }

  const score = (v: number) => v.toFixed(2).replace('.', ',');

  const pct = (v: number) => `${Math.min(1, Math.max(0, v)) * 100}%`;

  // The box with the same slack as the video and the sheet (BOX_SLACK_PX),
  // stopping at the image edge. The slack is in px and the box is a fraction,
  // so the CSS does the adding.
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
    // A target already out of the page is a menu item that changed pages:
    // "Filtrar por área" disappears from under the finger, and the menu has to
    // stay.
    if (
      menu &&
      menuEl &&
      clickTarget?.isConnected &&
      !menuEl.contains(clickTarget) &&
      !clickTarget.closest('.mais')
    )
      menu = null;
  }

  // --- hiding a camera right here
  // ---------------------------------------------

  // The ⋮ menu of a thumbnail. A single one, outside the grid and in `position:
  // fixed`: the thumbnail clips whatever goes past its edge, and the row can
  // leave the DOM in the middle of a scroll. For that very reason it closes
  // when the screen scrolls.
  let menu = $state<{
    k: string;
    det: Detection;
    cam: string;
    instanteMs: number;
    x: number;
    // Where the ⋮ is on screen: the menu opens below it, or above.
    topPos: number;
    base: number;
    // The areas page: the menu content becomes the list of them.
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

  // The menu opens below the ⋮ when it fits, and otherwise on the side with
  // more room. The areas page can exceed both - there are up to 8 - and then it
  // scrolls inside.
  const menuPosition = $derived.by(() => {
    if (!menu) return null;
    const rows = menu.areas ? (area?.cam === menu.cam ? 1 : 0) + 2 : exists(menu.cam) ? 4 : 1;
    const withThumbnail = menu.areas ? areasOf(menu.cam).length : 0;
    const height = rows * MENU_ITEM_PX + withThumbnail * MENU_AREA_PX + MENU_BEZEL_PX;
    const atBottom = window.innerHeight - menu.base;
    const upward = height > atBottom && menu.topPos > atBottom;
    return { upward, max: (upward ? menu.topPos : atBottom) - 8 };
  });

  // The ⋮ "Filtrar por área". With no registered area on the camera there is
  // nothing to choose, and it goes straight to the editor over the frame of
  // that detection, like the sheet's "filtrar por área". With areas, the menu
  // becomes the list of them.
  function filterByMenuArea() {
    if (!menu) return;
    if (areasOf(menu.cam).length) {
      menu.areas = true;
      return;
    }
    drawMenu();
  }

  // Through the ⋮, the list restarts at the instant of that frame, not at the
  // top: the filter was requested on it, as with "Ocultar a câmera".
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

  // The undo notice, in the snackbar: the thumbnail disappears from under the
  // finger, and a wrong tap in the middle of scrolling is easy. Undo is cheaper
  // than asking first.
  let undoNotice: number | undefined;

  // The anchor from `anchor()`, but skipping the camera that is about to leave:
  // the detection that holds the view has to remain in the list after the
  // filter.
  function anchorSkipping(cam: string): Anchor | null {
    if (!atTop) return null;
    for (let i = atTop.row.firstIndex; i < list.length; i++) {
      const det = list[i];
      const row = detectionRow(i);
      if (det && row && det.cam !== cam) return { k: key(det), dy: row.y - scrollRange.ini };
    }
    return null;
  }

  // It only unchecks the camera in this screen's filter, the same as the
  // "câmeras" button. The list does not restart: its detections leave what is
  // already loaded and the view stays where it was, which is the point of
  // hiding without scrolling up to the bar.
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
    // The fetch that was in flight would still bring the camera: it is
    // discarded.
    generation++;
    fetchingAbove = fetchingBelow = false;
    checkedCams = remaining;
    list = list.filter((d) => d.cam !== cam);
    await restoreAnchor(a);
    verify();
  }

  // Undo has to fetch again: what belonged to the camera was already released.
  // The fetch starts from the detection at the top of the view, like a "go to",
  // and what is above arrives with the scroll.
  function undo(before: string[] | null) {
    checkedCams = before;
    const ms = atTop?.det.instanteMs;
    void restart(scrollRange.ini > 0 && ms != null ? ms + 1 : null);
  }

  function keyHandler(e: KeyboardEvent) {
    if (e.key === 'Escape') menu = null;
  }

  // "Go to": the day goes to its end, and the time to the minute typed, both
  // included - the 14:32:07 detection appears when 14:32 is requested.
  const topDay = $derived(dayKey(new Date(topMs)));

  // With a range, changing the day carries the same time over to the other day.
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

  // In a range that crosses midnight, the time before `from` is the next day's.
  // `restart` clamps the instant inside the range.
  function goToHour(segundos: number) {
    const day = parseDay(interval?.day ?? topDay);
    if (interval && interval.to < interval.from && segundos / 60 < interval.from)
      day.setDate(day.getDate() + 1);
    void restart(day.getTime() + (segundos + 60) * 1000);
  }

  // The new range applies to the day that was in view.
  function applyInterval(from: number, to: number) {
    interval = { day: interval?.day ?? topDay, from, to };
    void restart();
  }

  // Goes back to infinite scroll without leaving where it was: the fetch starts
  // from the detection at the top of the view, as in undo.
  function resetInterval() {
    const ms = scrollRange.ini > 0 ? atTop?.det.instanteMs : undefined;
    const topPos = ms != null ? ms + 1 : limites?.to;
    interval = null;
    void restart(topPos != null && topPos <= Date.now() ? topPos : null);
  }

  // --- the area drawn on the spot --------------------------------------------

  // The editor comes in a separate chunk: whoever does not draw an area does
  // not download it.
  let AreaEditor = $state<typeof AreaEditorT | null>(null);
  let editor = $state<{
    cam: string;
    background: { tipo: 'imagem' | 'video'; url: string } | null;
    boxes: { caixa: Box; color: string }[];
    erro: string;
    // Where the list restarts on apply: at the top, or at the frame's instant
    // when the editor came from its ⋮.
    stayAt: number | null;
  } | null>(null);
  let openedArea = $state(false);
  let areaEl = $state<HTMLDivElement | undefined>();

  const boxesOf = (d: Detection) =>
    d.objetos.flatMap((o) => (o.caixa ? [{ caixa: o.caixa, color: family(o.familia).color }] : []));

  // Opens the editor over a frame of the camera: that of the given detection,
  // or that of its most recent detection with a frame, or the keyframe of the
  // last segment when it has no detection at all.
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
    // The editor may have closed, or been opened again, during the fetch. The
    // camera is not enough: reopened on the same one, from a detection's sheet,
    // it already has that detection's frame, and the old fetch would replace
    // it.
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

  // Drawing or marking an area on an unchecked camera is asking to see it: it
  // goes back to the list. Unchecking it afterwards removes it, with or without
  // the area.
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

  // The editor's "salvar na câmera", admin only: it writes the drawn area to
  // the registry and the filter now points to it, as if it had been checked in
  // the popover. It re-reads the registry first, so as not to hand the server
  // back a camera that someone changed after this screen opened.
  async function saveArea(points: Point[], rule: Rule, name: string) {
    if (!editor) return;
    const id = editor.cam;
    // The editor may have closed, or been opened again, during the save.
    const target = editor;
    const stillOpen = () => editor === target;
    try {
      await loadCameras();
      // loadCameras stores the error instead of throwing it: without stopping
      // here, the camera would go from the old copy and undo on the server what
      // changed afterwards.
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

  // Turns a registered area on or off. The popover stays open: choosing several
  // adds up. `stayAt` is the instant where the list restarts, via the ⋮.
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

  // Goes back to the list without leaving where it was, like resetInterval:
  // removing a filter only adds detections.
  function restartInPlace() {
    const ms = scrollRange.ini > 0 ? atTop?.det.instanteMs : undefined;
    return restart(ms != null ? ms + 1 : null);
  }

  // Removes the drawn area; the checked registered ones stay.
  function removeDrawn() {
    area = null;
    void restartInPlace();
  }

  // The chip's ✕: removes all of them.
  function clearArea() {
    area = null;
    checkedAreas = [];
    openedArea = false;
    void restartInPlace();
  }

  // The chip opens the areas popover. With no registered area there is nothing
  // to choose, and it reopens the editor for the drawn area.
  function reopenArea() {
    if (area && !anyRegistered) {
      void openEditor(area.cam);
      return;
    }
    openedArea = !openedArea;
  }

  // The chip's thumbnails, in place of the icon: one per area, stacked, up to
  // THUMBNAILS_IN_CHIP - beyond that the stack only widens the chip.
  const chipThumbnails = $derived.by(() => {
    const out: { cam: string; points: Point[] }[] = [];
    if (area) out.push({ cam: area.cam, points: area.points });
    for (const k of checkedAreas) {
      const a = registered.get(k);
      if (a) out.push({ cam: a.cam, points: a.points });
    }
    return out.slice(0, THUMBNAILS_IN_CHIP);
  });

  // What the chip says: the area's name when there is just one, or how many.
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

  // A target already out of the page is a popover button that moved: the first
  // tap on an area swaps the "área" button for the chip, and the popover goes
  // along.
  function outsideArea(e: MouseEvent) {
    if (!(e.target instanceof Node) || !e.target.isConnected) return;
    if (openedArea && areaEl && !areaEl.contains(e.target)) openedArea = false;
  }

  // --- lifecycle
  // --------------------------------------------------------------

  // New width: the columns and the thumbnail size change together, and the
  // previous anchor puts the view back on the same detection.
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
    // The header and the new-version strip, when it appears: both stick to the
    // top, and this screen's bar sits right under them.
    const cab = document.querySelector<HTMLElement>('#app > .top');
    const measureAll = () => {
      cabH = cab?.offsetHeight ?? 0;
      barH = barEl?.offsetHeight ?? 0;
      const newOne = listEl?.clientWidth ?? 0;
      if (width && newOne !== width) void changeWidth(newOne);
      else width = newOne;
      scrolled();
    };
    // Right now, and not only when the observer answers: the first page is the
    // size of the screen, and without a width its computation comes out wrong.
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
        material="detecção"
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

    <!-- When on, the chips do not fit on the day's row on a phone: they get a
         row of their own, which they split when there are two. -->
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
      <!-- The icon here had a 16 viewBox and a 1.4 stroke: 2.1 is the same
           stroke in the component's 24 viewBox. -->
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

<!-- The areas popover, opened by the "área" button or by the chip. With no
     registered area, it is the choice of which camera to draw on. With areas,
     each camera lists its own, to check one or several, and that camera's
     "desenhar". -->
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

  /* On a phone: day, time and cameras on the first row; families and thumbnail
     controls on the second, all with just the icon. With the name written out
     they need ~470 px on a 370 row. The three columns let each row split in its
     own way: the 1st is the width of the families, the 3rd that of the cameras. */
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
  /* The range and the area turned on: on a phone, a whole 3rd row, which the
     two chips split. */
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
  /* The area tiles, two per row, as in the registry's Alert tab. */
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
  /* The shape of the CameraPicker chip: green dot and blue border on the checked one. */
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

  /* The area chip, with the shape of the range chip. */
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
  /* The thumbnails of the filter's areas, one over the other, each with a
     margin in the chip's color so they do not blend together. */
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

  /* The area outline over the thumbnail: thin and dashed, so it does not cover
     the object. */
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

  /* "Alerts only" turned on in the bell's color, not the families': it is
     another filter, one that says "less", not "these". */
  .alerts.on {
    color: var(--warn);
    border-color: var(--warn);
  }

  /* The thumbnail controls have the shape of the families beside them: the same
     button, the same "on". */
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

  /* --- the grid --- */

  .list {
    position: relative;
    /* The rows are absolute: the browser's scroll anchoring would have nothing
       to rely on, and what holds the view is the screen's anchor. */
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

  /* The header that sticks right below the bar. Zero height in the flow: it
     does not push the grid, it just lies over it. */
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

  /* With "alerts only", an object that did not make it into the alert stays on
     the detection, but dimmed. */
  .box.fora {
    opacity: 0.35;
    border-style: dashed;
  }

  /* The object detector's score, glued to the box and always outside: the side
     comes from scoreSide. The -2px and 2px are the box border. */
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

  /* The ⋮ in the corner, to the right of the family icons and with the same
     pill as them. The pill is small, but the touch target is the whole corner. */
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

  /* The families badge makes room for the ⋮ to its right. */
  .cell :global(.badge) {
    right: 30px;
  }

  /* With a mouse the ⋮ shows only over the thumbnail, and stays while its menu
     is open. On touch there is no hover: it is always in view. */
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

  /* On a small thumbnail the ⋮ cannot be hit with a finger: the action stays on the sheet. */
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

  /* The areas page: the mark, the name and the rule on one row. */
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

  /* The camera name does not fit on a small thumbnail: only the time stays. */
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

  /* The range ends, at the start and end of the list: they say where it stops
     and offer the way back to infinite scroll right there. */
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

    /* From tablet up the names come back, but the phone's two rows stay: on a
       portrait iPad (820 px) the whole bar does not fit on a single row. */
    .bar {
      gap: 10px;
      margin: 0 -18px;
      padding: 10px 18px;
    }

    .bar .label {
      display: inline;
    }

    /* From tablet width up, the chips fit on the day's row. */
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

  /* A single row when everything fits: the whole bar measures ~1210 px. What
     decides is the leftover space, not a width number that gets stale with
     every new button: whatever does not fit (today the cameras button) wraps to
     the 2nd row, on the right, instead of pushing the page sideways. */
  /* With the chips on it no longer fits, and stays on the tablet's two rows.
     The `>` matters: the open camera picker has a `.chips` of its own, and
     without the `>` the bar would go back to the grid just by opening it. */
  @media (min-width: 1060px) {
    /* `justify-content` gathers on the right whatever is left alone on a row
       (the cameras button, when it wraps); the families' `margin-right: auto`
       pushes the controls to the right on the top row. Two `auto` margins would
       split the slack and move the controls away from the cameras. */
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
