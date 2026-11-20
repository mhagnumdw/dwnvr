<script lang="ts">
  import { onMount, onDestroy, tick, untrack } from 'svelte';
  import {
    cameras,
    loadCameras,
    loadHealth,
    health,
    pollHealth,
    REPO_URL,
  } from '../lib/state.svelte.ts';
  import { api, messageOf } from '../lib/api.ts';
  import type {
    CameraArea,
    Camera,
    AlertSchedule,
    RegisteredCamera,
    AlertWhere,
    Orphan,
    Stream,
  } from '../lib/api.ts';
  import { currentParams, write } from '../lib/route.svelte.ts';
  import {
    hhmm,
    dias,
    kbps,
    bytes,
    mbToBytes,
    resolution,
    ddmm,
    duration,
    recordedWithGap,
    hourOfMinute,
  } from '../lib/format.ts';
  import { HELP_RETAINED, HELP_FIT, HELP_SENSITIVITY, LEVELS, level } from '../lib/help.ts';
  import Modal from '../components/Modal.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import NoCameras from '../components/NoCameras.svelte';
  import Icon from '../components/Icon.svelte';
  import HealthFooter from '../components/HealthFooter.svelte';
  import Tabs from '../components/Tabs.svelte';
  import AreaLabel from '../components/AreaLabel.svelte';
  import AreaTile from '../components/AreaTile.svelte';
  import type AreaEditorT from '../components/AreaEditor.svelte';
  import type AlertTabT from '../components/AlertTab.svelte';
  import { family, familyIcon, iconURL, byPriority } from '../lib/icons.ts';
  import { DEFAULT_RULE, MAX_AREAS, registryPoints } from '../lib/area.ts';
  import {
    MAX_SCHEDULES,
    fromServer,
    toServer,
    hoursSummary,
    daysSummary,
    valid,
  } from '../lib/schedules.ts';
  import type { Schedule } from '../lib/schedules.ts';
  import type { Point, Rule } from '../lib/area.ts';
  import { forgetLastSegment, fullFrame, lastSegment } from '../lib/thumbs.ts';

  // The camera in the form, plus what the screen knows about its audio. Fields
  // starting with `_` are not sent to the API.
  type Draft = RegisteredCamera & {
    _new: boolean;
    _hasAudio: boolean;
    _audioCodecs: string[];
    _probing: boolean;
    _probeError: string;
  };

  // Whose recordings are about to be deleted: a registered camera or an orphan.
  interface Target {
    id: string;
    nome: string;
    bytes: number;
    orphan: boolean;
  }

  let editing = $state<Draft | null>(null); // copy of the camera being edited, or null
  let removing = $state<RegisteredCamera | null>(null); // camera awaiting removal confirmation
  let deleting = $state<Target | null>(null); // recordings awaiting deletion confirmation
  let deleteRecordings = $state(false); // checkbox in the removal dialog
  let saving = $state(false);
  let error = $state('');
  // The server's rejection on save. It lives inside the form, next to "salvar":
  // the page's `error` shows up behind the Modal's dark backdrop.
  let formError = $state('');

  // The form tabs. It always opens on the first one: that's where the name and
  // the quota are, which is what you look for when registering. "Áreas" and
  // "Alerta" only work with motion marking on and the object detector
  // configured: without both, the camera has no detection, so an area would
  // have nothing to filter and an alert nothing to report. Without them the two
  // tabs stay, and say what's missing: hidden, nobody would know they exist.
  // Turning marking off deletes nothing.
  const withDetection = $derived(!!editing?.detect && !!cameras.detector);
  const TABS = $derived([
    { id: 'gravacao', label: 'Gravação' },
    { id: 'deteccao', label: 'Detecção' },
    {
      id: 'areas',
      label: editing?.areas?.length ? `Áreas (${editing.areas.length})` : 'Áreas',
    },
    { id: 'alerta', label: 'Alerta' },
  ]);
  let tab = $state('gravacao');
  // When registering, the footer walks through the tabs one by one and "salvar"
  // only shows on the last: whoever registers goes through everything. When
  // editing you go straight to one thing, and the tabs are enough.
  const tabIndex = $derived(TABS.findIndex((a) => a.id === tab));
  const guided = $derived(!!editing?._new && tabIndex < TABS.length - 1);
  // After the switch, focus goes to the new panel: the previous one becomes
  // `inert` and the clicked button may vanish from the footer, and without this
  // focus falls to <body> and Tab restarts from the top. Moving forward, it
  // goes to the first field, so whoever was typing and pressing Enter keeps
  // typing. Moving back, it goes to the tab: on a phone, focusing the text
  // field would open the keyboard unasked.
  async function goTo(step: number) {
    tab = TABS[tabIndex + step]?.id ?? tab;
    await tick();
    const target =
      step > 0
        ? document
            .getElementById(`cam-painel-${tab}`)
            ?.querySelector<HTMLElement>('input, select, textarea, button')
        : document.getElementById(`cam-aba-${tab}`);
    target?.focus();
  }

  const newOnes = $derived(cameras.streams.filter((s) => !s.registered));
  const bounds = $derived(cameras.faixas);

  // go2rtc restart: it only reads go2rtc.yaml at startup.
  let confirmingRestart = $state(false);
  let restarting = $state(false);
  // go2rtc answers the request before restarting: asking right afterward still
  // hits the old process, with the old list.
  const RESTART_FIRST_ASK_MS = 1500;
  const RESTART_INTERVAL_MS = 1000;
  // Measured on a local go2rtc: it's back in under 2 s. Past that, give up and
  // let the screen show the connection error, which is the truth.
  const RESTART_GIVE_UP_MS = 20000;

  const registered = $derived(new Set(cameras.list.map((c) => c.id)));
  // A registered camera that left the file stops recording after the restart:
  // it's the only effect of the restart that someone might not want, and the
  // dialog repeats it.
  const willStop = $derived(
    (cameras.go2rtcConfigFile?.foraDoArquivo ?? []).filter((id) => registered.has(id)),
  );

  // While the probe hasn't answered, the audio options stay locked; if it
  // FAILS, they are unlocked. Not being able to check is not the same as
  // knowing there's no audio, and locking in that case would prevent
  // configuring a good camera that was just offline when it was registered.
  const audioBlocked = $derived(!editing?._hasAudio && !editing?._probeError);

  // `#cams?editar=<id>` opens the camera's form directly: it's the link on the
  // Diagnostics screen for someone who has detection turned off. The parameter
  // is consumed on open, otherwise reloading the page would reopen the form.
  onMount(async () => {
    const id = currentParams().get('editar');
    if (!cameras.list.length) await loadCameras();
    if (!id) return;
    write({ edit: null });
    const cam = cameras.list.find((c) => c.id === id);
    // With another form already open (the list may have arrived by another path
    // while this one was waiting), the open request does not replace it.
    if (cam && !editing) edit(cam);
  });

  // The common path for a new camera is to switch windows, edit go2rtc.yaml and
  // come back: reloading when the tab reappears saves a button click. With the
  // tab hidden there's no cost at all - the event only fires on the switch.
  function onReturnToTab() {
    if (!document.hidden) void loadCameras();
  }
  onMount(() => {
    document.addEventListener('visibilitychange', onReturnToTab);
    return () => {
      document.removeEventListener('visibilitychange', onReturnToTab);
    };
  });

  const stop = pollHealth();
  onDestroy(stop);

  function statusOf(id: string) {
    return health.cameras.find((h) => h.id === id);
  }

  // estimateDays answers "with this quota, how many days of recording fit?" -
  // and the server already answers that in retainDays, at the density of what
  // is recorded.
  //
  // Here we only lack the "what if I set 40 GB?" of the edit form, and scaling
  // is enough for that: the estimate is linear in the quota (days = quota /
  // consumption per day), so the ratio between the typed quota and the recorded
  // one gives the new number without repeating in JS the math that Go does. The
  // denominator is st.quotaMB, which is always the saved quota - cam.quotaMB is
  // what's in the form and moves while you type.
  function estimateDays(cam: Camera, quotaMB = cam.quotaMB) {
    const st = statusOf(cam.id);
    if (!st?.retainDays || !st.quotaMB || quotaMB === undefined) return undefined;
    return (st.retainDays * quotaMB) / st.quotaMB;
  }

  function newItem(streamName: string) {
    const s = cameras.streams.find((x) => x.name === streamName);
    editing = {
      id: streamName,
      // The stream name in go2rtc, untouched: "cam_frente" stays "cam_frente".
      name: streamName,
      enabled: true,
      audio: 'none',
      quotaMB: 10240,
      segmentSeconds: 30,
      maxDays: 0,
      // All three come from the server defaults, where detection starts OFF:
      // whoever only records pays nothing for it. The literal only covers a
      // response without "padrao".
      detect: cameras.padrao?.detect ?? false,
      detectMecanismo: cameras.padrao?.detectMecanismo ?? 'kleinberg-p',
      detectSensibilidade: cameras.padrao?.detectSensibilidade ?? 5,
      _new: true,
      ...audioKnown(s),
    };
    tab = 'gravacao';
    formError = '';
    openAlert();
    void probeAudio(streamName);
  }

  // `initialTab`: the card's areas or alert block opens the form directly on
  // its tab.
  function edit(cam: RegisteredCamera, initialTab = 'gravacao') {
    const s = cameras.streams.find((x) => x.name === cam.id);
    editing = { ...cam, _new: false, ...audioKnown(s) };
    tab = initialTab;
    formError = '';
    openAlert();
    void probeAudio(cam.id);
  }

  // The alert's "where" and "when" on the card, in the words of the "Alerta"
  // tab. A marked area that no longer exists is not shown; schedules go as the
  // tab lists them ("todo dia · 12:30 às 13:59").
  function alertWhereOnCard(cam: RegisteredCamera) {
    const ids = cam.alert?.areas ?? [];
    if (!ids.length) return 'em qualquer lugar da imagem';
    const names = (cam.areas ?? []).filter((a) => a.id && ids.includes(a.id)).map((a) => a.name);
    return `só em ${names.join(', ')}`;
  }
  // The bell on the thumbnail: the area is among those of the enabled alert. An
  // alert on the whole image marks none, because it doesn't depend on an area.
  const alertInArea = (cam: RegisteredCamera, a: CameraArea) =>
    !!cam.alert?.enabled && !!a.id && !!cam.alert.areas?.includes(a.id);
  function alertWhenOnCard(hs: AlertSchedule[] | undefined) {
    const validItems = fromServer(hs).filter(valid);
    if (!validItems.length) return 'a qualquer hora';
    return validItems
      .map((h) => `${daysSummary(h.days)} · ${hoursSummary(h.from, h.to)}`)
      .join('; ');
  }

  // What's already known about the audio when the form opens. On an idle stream
  // that is always "none", and it's a lie: go2rtc only lists the tracks while
  // someone is consuming the stream. That's why probeAudio() comes right after.
  const audioKnown = (s: Stream | undefined) => ({
    _hasAudio: s?.hasAudio ?? false,
    _audioCodecs: s?.audioCodecs ?? [],
    _probing: false,
    _probeError: '',
  });

  // Asks the server whether the camera delivers audio. Only worth it when the
  // answer we already have is "no": a "yes" came from medias and doesn't
  // change.
  //
  // The answer can take a few seconds (the server opens the stream to find
  // out), so the form is already on screen when it arrives - hence the check
  // that it's still the same form, which discards the answer for a form that
  // was closed or replaced by another.
  async function probeAudio(streamName: string) {
    const form = editing;
    if (!form || form._hasAudio) return;
    form._probing = true;
    try {
      const r = await api.probeStream(streamName);
      if (editing !== form) return;
      form._hasAudio = r.hasAudio;
      form._audioCodecs = r.audioCodecs ?? [];
      form._probeError = r.erro ?? '';
    } catch (e) {
      if (editing !== form) return;
      form._probeError = messageOf(e);
    } finally {
      if (editing === form) form._probing = false;
    }
  }

  // Invalid field on a hidden tab: the browser rejects the submit, but it can't
  // show the bubble on an invisible field, and "salvar" seems to do nothing.
  // The first invalid field brings its tab to the front and shows the reason;
  // the following ones from the same submit wait for the next click.
  async function invalidField(e: Event) {
    const field = e.target as HTMLInputElement;
    const panel = field.closest<HTMLElement>('[role="tabpanel"]');
    const owner = panel?.dataset['aba'];
    if (!owner || owner === tab) return;
    tab = owner;
    await tick();
    field.reportValidity();
  }

  // --- the Areas tab --------------------------------------------------------

  // The background frame for the areas: the keyframe of the last segment,
  // decoded here at the camera's resolution. It's used by the editor and by the
  // area thumbnails, on this tab and on "Alerta". It's only fetched when one of
  // them opens - "Alerta", if the camera has an area - and is dropped from
  // memory when the form closes.
  let frame = $state<{
    cam: string;
    t: number | null;
    url: string;
    ratio: number;
    fetching: boolean;
    erro: string;
  } | null>(null);
  // Each fetch gets its own number: the response of a fetch already replaced by
  // another, or of a form already closed, is discarded.
  let fetchSeq = 0;
  // The URL of the frame of the camera being edited, empty until it arrives:
  // the background of the area thumbnails.
  const cameraFrame = $derived(editing && frame?.cam === editing.id ? frame.url : '');

  function releaseFrame() {
    fetchSeq++;
    if (frame?.url) URL.revokeObjectURL(frame.url);
    frame = null;
  }

  async function frameUrl(bmp: ImageBitmap) {
    const c = document.createElement('canvas');
    c.width = bmp.width;
    c.height = bmp.height;
    c.getContext('2d')?.drawImage(bmp, 0, 0);
    const blob = await new Promise<Blob | null>((r) => {
      c.toBlob(r, 'image/jpeg', 0.9);
    });
    if (!blob) throw new Error('não deu para guardar o quadro');
    return URL.createObjectURL(blob);
  }

  async function fetchFrame(cam: string, newItem: boolean) {
    const mine = ++fetchSeq;
    const before = frame?.cam === cam ? frame : null;
    if (!before && frame?.url) URL.revokeObjectURL(frame.url);
    frame = {
      cam,
      t: before?.t ?? null,
      url: before?.url ?? '',
      ratio: before?.ratio ?? 16 / 9,
      fetching: true,
      erro: '',
    };
    const q = frame;
    // The new camera hasn't recorded yet, and the server doesn't even know it.
    if (newItem) {
      q.fetching = false;
      q.erro =
        'Esta câmera ainda não gravou nada. Salve, e depois do primeiro minuto de gravação volte aqui, em editar, na aba Áreas, para desenhar sobre a imagem dela.';
      return;
    }
    try {
      const t = await lastSegment(cam);
      if (t === null) throw new Error('esta câmera ainda não tem gravação para desenhar em cima');
      const bmp = await fullFrame(cam, t);
      const ratio = bmp.width / bmp.height;
      const url = await frameUrl(bmp).finally(() => {
        bmp.close();
      });
      if (mine !== fetchSeq) {
        URL.revokeObjectURL(url);
        return;
      }
      if (q.url) URL.revokeObjectURL(q.url);
      q.t = t;
      q.url = url;
      q.ratio = ratio;
    } catch (e) {
      if (mine === fetchSeq) q.erro = messageOf(e);
    } finally {
      if (mine === fetchSeq) q.fetching = false;
    }
  }

  $effect(() => {
    const cam = editing?.id;
    const withThumbnails = tab === 'areas' || (tab === 'alerta' && !!editing?.areas?.length);
    if (!withThumbnails || !withDetection || !cam) return;
    const newItem = !!editing?._new;
    // The frame is read and the fetch starts untracked: a fetch that failed
    // retries when entering one of the tabs, and not on every frame change,
    // which would endlessly repeat a request that fails. The fetch itself reads
    // and writes the frame before the first await.
    untrack(() => {
      const q = frame;
      if (q?.cam !== cam || (q.erro && !q.fetching)) void fetchFrame(cam, newItem);
    });
  });
  $effect(() => {
    if (!editing) releaseFrame();
  });
  onDestroy(releaseFrame);

  const frameCaption = (t: number | null) => (t === null ? '' : `quadro de ${ddmm(t)}, ${hhmm(t)}`);

  // The editor comes in a separate chunk, as in Detections.
  let AreaEditor = $state<typeof AreaEditorT | null>(null);
  // The area open in the editor: its index, or null for a new one.
  let openedArea = $state<{ index: number | null } | null>(null);

  function openArea(index: number | null) {
    if (!AreaEditor)
      void import('../components/AreaEditor.svelte').then((m) => {
        AreaEditor = m.default;
      });
    openedArea = { index };
  }

  function applyArea(points: Point[], rule: Rule, name: string) {
    if (!editing || !openedArea) return;
    const areas = editing.areas ?? [];
    const i = openedArea.index;
    const newOne: CameraArea = {
      ...(i === null ? { id: newAreaId(areas) } : areas[i]),
      name,
      rule,
      points: registryPoints(points),
    };
    editing.areas = i === null ? [...areas, newOne] : areas.map((a, j) => (j === i ? newOne : a));
    openedArea = null;
  }

  // The new area's id is born here, not on the server, so the alert can mark it
  // before the camera is saved. Same format as the server: 6 lowercase letters
  // and digits, no repeats within the camera.
  function newAreaId(areas: CameraArea[]) {
    const alphabet = 'abcdefghijklmnopqrstuvwxyz0123456789';
    for (;;) {
      const b = crypto.getRandomValues(new Uint8Array(6));
      const id = Array.from(b, (v) => alphabet[v % alphabet.length]).join('');
      if (!areas.some((a) => a.id === id)) return id;
    }
  }

  // The area to delete, awaiting confirmation: its index.
  let deletingArea = $state<number | null>(null);

  const usedInAlert = (id: string | undefined) =>
    !!id &&
    !!editing?.alert?.enabled &&
    alertWhere === 'areas' &&
    !!editing.alert.areas?.includes(id);

  // The list's "apagar" and the editor's "apagar área". Both confirm first: an
  // area deleted by mistake is an area to redraw. The editor sits on top of
  // everything, so it closes first, and the confirmation appears over the form.
  function deleteArea(i: number) {
    openedArea = null;
    deletingArea = i;
  }

  // removeArea deletes the area and removes it from the alert. An alert left
  // without the areas it had is turned off, and does not fall back to the whole
  // image; the families and the "só nas áreas marcadas" stay, so it can be
  // turned back on by choosing again.
  function removeArea(i: number) {
    if (!editing) return;
    const id = editing.areas?.[i]?.id;
    editing.areas = (editing.areas ?? []).filter((_, j) => j !== i);
    const al = editing.alert;
    if (id && al?.areas?.includes(id)) {
      const remain = al.areas.filter((x) => x !== id);
      editing.alert = {
        ...al,
        areas: remain,
        enabled: al.enabled && (alertWhere === 'tudo' || remain.length > 0),
      };
    }
  }

  // --- the Alerta tab -------------------------------------------------------

  // Comes in a separate chunk, only downloaded when the tab exists.
  let AlertTab = $state<typeof AlertTabT | null>(null);
  $effect(() => {
    if (withDetection && !AlertTab)
      void import('../components/AlertTab.svelte').then((m) => {
        AlertTab = m.default;
      });
  });

  // Where the alert applies, as the tab shows it (see AlertWhere): "só nas
  // áreas marcadas" with none marked, and saving rejects.
  let alertWhere = $state<AlertWhere>('tudo');
  let alertError = $state('');
  // The schedules as the tab edits them, in minutes. With the box unchecked,
  // saving deletes `schedules`: empty means any time.
  let limitSchedules = $state(false);
  let alertSchedules = $state<Schedule[]>([]);

  function openAlert() {
    alertWhere = editing?.alert?.areas?.length ? 'areas' : 'tudo';
    alertSchedules = fromServer(editing?.alert?.schedules);
    limitSchedules = alertSchedules.length > 0;
    alertError = '';
  }

  // "America/Fortaleza (UTC−03:00)": the time zone the schedules apply in,
  // which is the server's and not the browser's.
  const serverTimezone = $derived.by(() => {
    const c = health.clock;
    if (!c) return '';
    const s = Math.abs(c.offsetSeconds);
    const utc = `UTC${c.offsetSeconds < 0 ? '−' : '+'}${hourOfMinute(Math.floor(s / 60))}`;
    return c.zone ? `${c.zone} (${utc})` : utc;
  });

  // What's missing in an enabled alert for it to be saved. With the tab hidden
  // there's nothing to check: the alert stays as it was.
  function missingInAlert() {
    const al = editing?.alert;
    if (!withDetection || !al?.enabled) return '';
    if (!al.families?.length) return 'Marque ao menos uma família para o alerta.';
    if (alertWhere === 'areas' && !al.areas?.length)
      return 'Marque ao menos uma área para o alerta, ou escolha em qualquer lugar da imagem.';
    if (limitSchedules) {
      if (alertSchedules.some((h) => !valid(h)))
        return 'Há um horário sem dia, sem hora ou com o "Até" que não fica depois do "De": corrija ou remova.';
      if (!alertSchedules.length)
        return 'Marque ao menos um horário, ou desmarque "Limitar o alerta a alguns horários".';
      if (alertSchedules.length > MAX_SCHEDULES)
        return `São até ${MAX_SCHEDULES} horários por câmera: junte ou remova alguns.`;
    }
    return '';
  }

  // How long saving waits for the server. The form stays locked while saving,
  // and without a deadline a stalled server would keep it open forever.
  const SAVE_TIMEOUT_MS = 30_000;

  async function save() {
    if (!editing) return;
    formError = '';
    const missing = missingInAlert();
    if (missing) {
      alertError = missing;
      tab = 'alerta';
      return;
    }
    saving = true;
    try {
      const { _new, _hasAudio, _audioCodecs, _probing, _probeError, ...cam } = editing;
      // On the whole image, the areas marked before no longer apply.
      if (cam.alert && alertWhere === 'tudo') cam.alert = { ...cam.alert, areas: [] };
      if (cam.alert)
        cam.alert = {
          ...cam.alert,
          schedules: limitSchedules ? toServer(alertSchedules) : [],
        };
      await api.saveCamera(cam, AbortSignal.timeout(SAVE_TIMEOUT_MS));
      await loadCameras();
      editing = null;
    } catch (e) {
      // The form doesn't close during submit (cancel, remove and Escape wait),
      // so this is the same one that was sent.
      formError =
        e instanceof DOMException && e.name === 'TimeoutError'
          ? `O servidor não respondeu em ${SAVE_TIMEOUT_MS / 1000} s. Confira a lista antes de tentar de novo: o salvar pode ter chegado.`
          : messageOf(e);
    } finally {
      saving = false;
    }
  }

  // The size comes from the diagnostics measurement, the same source as the
  // card's disk chip: saying "apagar 3,2 GB" only helps if the number is the
  // one the person was already seeing.
  function sizeOf(cam: Camera) {
    return statusOf(cam.id)?.diskBytes ?? 0;
  }

  // A normalized target so a single dialog serves both cases: the registered
  // camera, which has a name and status, and the orphan, which only has what
  // the disk scan found.
  const cameraTarget = (cam: RegisteredCamera): Target => ({
    id: cam.id,
    nome: cam.name,
    bytes: sizeOf(cam),
    orphan: false,
  });

  const orphanTarget = (o: Orphan): Target => ({
    id: o.id,
    nome: o.id,
    bytes: o.bytes,
    orphan: true,
  });

  function openRemoval(cam: RegisteredCamera) {
    // Always unchecked on open: preserving the recordings is the default, and
    // inheriting the checkbox from a previous removal would delete video nobody
    // asked to delete.
    deleteRecordings = false;
    removing = cam;
  }

  async function confirmRemoval() {
    const cam = removing;
    if (!cam) return;
    const withRecordings = deleteRecordings;
    removing = null;
    error = '';
    try {
      await api.deleteCamera(cam.id, { recordings: withRecordings });
      await loadCameras();
      await loadHealth();
    } catch (e) {
      error = messageOf(e);
    }
  }

  const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));

  async function restartGo2rtc() {
    confirmingRestart = false;
    restarting = true;
    error = '';
    try {
      await api.restartGo2rtc();
      // Asks the API directly, bypassing the state: while go2rtc is down, the
      // response carries go2rtcError, and putting it on screen would flash the
      // "não foi possível falar com o go2rtc" warning in the middle of a normal
      // restart.
      const giveUp = Date.now() + RESTART_GIVE_UP_MS;
      await wait(RESTART_FIRST_ASK_MS);
      while (Date.now() < giveUp) {
        const data = await api.cameras().catch(() => null);
        if (data && !data.go2rtcError) break;
        await wait(RESTART_INTERVAL_MS);
      }
    } catch (e) {
      error = messageOf(e);
    } finally {
      await loadCameras();
      await loadHealth();
      restarting = false;
    }
  }

  async function confirmDeletion() {
    const target = deleting;
    if (!target) return;
    deleting = null;
    error = '';
    try {
      await api.deleteRecordings(target.id);
      forgetLastSegment(target.id);
      // Both: loadCameras rebuilds the orphan list, loadHealth rebuilds the
      // disk chip. Without the second, the card would keep showing the bytes
      // that are already gone.
      await loadCameras();
      await loadHealth();
    } catch (e) {
      error = messageOf(e);
    }
  }
</script>

<div class="page">
  <!-- With the list not loaded, go2rtcError is from the last good read: we
       can't claim the registered cameras are still recording. -->
  {#if cameras.go2rtcError && !cameras.error}
    <p class="card warnbox small">
      Não foi possível falar com o go2rtc: {cameras.go2rtcError}<br />
      As câmeras já cadastradas continuam gravando, mas não dá para descobrir novas.
    </p>
  {/if}

  <!-- Without this, the first load that fails left the list empty and the
       screen said "nenhuma câmera cadastrada", which is false. -->
  {#if cameras.error}
    <p class="card errbox small">Não deu para carregar as câmeras: {cameras.error}</p>
  {/if}

  {#if error}<p class="card errbox small">{error}</p>{/if}

  <div class="list">
    {#each cameras.list as cam (cam.id)}
      {@const st = statusOf(cam.id)}
      <div class="card cam">
        <div class="row wrap head">
          <span class="dot" class:ok={st?.connected} class:bad={cam.enabled && st && !st.connected}
          ></span>
          <strong>{cam.name}</strong>
          <code class="muted small">{cam.id}</code>
          <span class="spacer"></span>
          <!-- The three in their own group so they wrap together: loose in the
               row, on a phone screen "remover" dropped alone to the left, far
               from the other two and from the corner where the thumb expects to
               find it. -->
          <div class="row actions">
            <button class="ghost small" onclick={() => edit(cam)}>editar</button>
            <!-- Full label: just "gravações" would read as "see the recordings". -->
            <button class="ghost small danger" onclick={() => (deleting = cameraTarget(cam))}>
              apagar gravações
            </button>
            <button class="ghost small danger" onclick={() => openRemoval(cam)}>remover</button>
          </div>
        </div>

        <div class="row wrap chips">
          {#if !cam.enabled}<span class="chip">desabilitada</span>{/if}
          <span class="chip">{st?.videoCodec ?? '-'}</span>
          <span class="chip">{resolution(st?.width, st?.height)}</span>
          <span class="chip">áudio: {cam.audio}</span>
          {#if cam.detect}
            <span class="chip" title={HELP_SENSITIVITY}>
              movimento: {level(cam.detectSensibilidade)?.label ?? cam.detectSensibilidade}
            </span>
          {/if}
          <span class="chip">{kbps(st?.bitrateKbps)}</span>
          <span class="chip">{bytes(st?.diskBytes ?? 0)} de {mbToBytes(cam.quotaMB)}</span>
          <!-- The "retido / cabem" pair is what makes the quota understandable:
               the first is the past that exists, the second is what it still
               has room for. Alone, each one misleads. The explanation is the
               same as the equivalent column in Diagnostics, and comes from
               ajudas.ts precisely so it doesn't diverge from it. The "Desde"
               goes only here and in the cell there: it's per camera, unlike the
               rest of the text, and wouldn't fit in the chip without pushing
               the others. -->
          {#if st?.oldestSegmentAt}
            {@const since = new Date(st.oldestSegmentAt)}
            {@const retained = Date.now() - since.getTime()}
            {@const recorded = recordedWithGap(retained, st.recordedMs)}
            <span class="chip retained" title="{HELP_RETAINED} Desde {ddmm(since.getTime())}">
              retido: {duration(retained)}{#if recorded}
                · gravado {duration(recorded)}{/if}
            </span>
          {/if}
          {#if st?.retainDays}
            <span class="chip retain" title={HELP_FIT}>cabem ≈ {dias(st.retainDays)}</span>
          {/if}
        </div>

        <!-- Where the files really are. Whenever the question leaves the
             browser - making a backup, looking at the disk over ssh, working
             out where the usage chip's bytes came from - this is the path you
             look for, and it wasn't written anywhere in the interface. Its own
             row because it's the only chip that can get long: next to the
             others it would push the retention out of alignment with every
             folder name. -->
        {#if cam.dir}
          <div class="row wrap chips">
            <span class="chip storage" title="pasta das gravações desta câmera"
              >storage: {cam.dir}</span
            >
          </div>
        {/if}

        <!-- The areas and the alert, as their tabs show them: each block is a
             shortcut to edit the camera already on its tab. Same condition as
             the tabs, so the card doesn't talk about areas that filter nothing
             or an alert with nothing to report. They come after the storage
             because they are the largest on the card: short rows stay together. -->
        {#if cam.detect && cameras.detector}
          {#if cam.areas?.length}
            <button type="button" class="ghost block" onclick={() => edit(cam, 'areas')}>
              {@render blockTitle('area', 'Áreas')}
              <span class="card-areas">
                {#each cam.areas as a, i (a.id ?? i)}
                  <AreaTile cam={cam.id} area={a} alert={alertInArea(cam, a)} />
                {/each}
              </span>
            </button>
          {/if}
          {#if cam.alert}
            {@const al = cam.alert}
            <button type="button" class="ghost block" onclick={() => edit(cam, 'alerta')}>
              {@render blockTitle('bell', 'Alerta')}
              {#if al.enabled}
                <span class="alert-rows">
                  <span class="row">
                    {#each byPriority(al.families ?? []) as f (f)}
                      {@const ic = familyIcon(f)}
                      <span class="family">
                        {#if ic}<img
                            src={iconURL(ic, 14, family(f).color)}
                            alt=""
                            width="14"
                            height="14"
                          />{/if}
                        {family(f).nome}
                      </span>
                    {/each}
                  </span>
                  <span class="row">
                    <Icon name="area" size={16} />
                    {alertWhereOnCard(cam)}
                  </span>
                  <span class="row">
                    <Icon name="clock" size={16} />
                    {alertWhenOnCard(al.schedules)}
                  </span>
                </span>
              {:else}
                <span class="muted">desligado</span>
              {/if}
            </button>
          {/if}
        {/if}

        {#if st?.lastError && !st.connected}
          <p class="small err">{st.lastError}</p>
        {/if}
      </div>
    {/each}

    {#if !cameras.list.length && !cameras.loading && !cameras.error}
      <NoCameras link={false} />
    {/if}
  </div>

  <!-- Always visible, even when empty: while this block only appeared when
       there was a new stream, whoever reached the screen with everything
       already registered didn't find the add button and concluded it didn't
       exist. -->
  <div class="card help">
    <div class="row title">
      <h3>Disponíveis no go2rtc</h3>
      <button
        class="ghost refresh"
        class:spinning={cameras.loading}
        onclick={loadCameras}
        disabled={cameras.loading}
        aria-label="atualizar"
        title="perguntar de novo ao go2rtc quais streams ele serve"
      >
        <Icon name="refresh" size={16} stroke={2.4} />
      </button>
    </div>

    {#if cameras.go2rtcConfigFile}
      {@const arq = cameras.go2rtcConfigFile}
      <div class="card warnbox notice">
        <p class="small">
          O <code>go2rtc.yaml</code> mudou depois que o go2rtc subiu. Ele só lê o arquivo quando sobe,
          então estas mudanças ainda não valem:
        </p>
        <ul class="small">
          {#each arq.novas ?? [] as id (id)}
            <li>
              <code>{id}</code>
              <span class="muted">nova - aparece para cadastrar depois do reinício</span>
            </li>
          {/each}
          {#each arq.alteradas ?? [] as id (id)}
            <li><code>{id}</code> <span class="muted">configuração mudou</span></li>
          {/each}
          {#each arq.foraDoArquivo ?? [] as id (id)}
            <li>
              <code>{id}</code>
              <span class="muted">
                {registered.has(id)
                  ? 'saiu do arquivo - para de gravar depois do reinício'
                  : 'saiu do arquivo - some desta lista depois do reinício'}
              </span>
            </li>
          {/each}
        </ul>
        <div class="row action">
          <button class="warn" onclick={() => (confirmingRestart = true)} disabled={restarting}>
            {restarting ? 'reiniciando…' : 'reiniciar go2rtc'}
          </button>
        </div>
      </div>
    {/if}

    {#if newOnes.length}
      <div class="row wrap">
        {#each newOnes as s (s.name)}
          <button title="Clique para adicionar" onclick={() => newItem(s.name)}>
            + {s.name}
            {#if s.hasAudio}<span class="chip">áudio</span>{/if}
            {#if s.transcoding}<span class="chip warn">ffmpeg</span>{/if}
          </button>
        {/each}
      </div>
    {:else if cameras.error}
      <p class="muted small empty">Não dá para listar os streams sem resposta do dwnvr.</p>
    {:else if cameras.go2rtcError}
      <p class="muted small empty">
        Não dá para listar os streams enquanto o go2rtc não responder.
      </p>
    {:else}
      <p class="muted small empty">
        Nenhum stream novo - o dwnvr já grava todos os que o go2rtc serve.
      </p>
    {/if}

    <!-- After the list: whoever already understands how it works wants the
         button first, and whoever doesn't reads the explanation next to what it
         explains. -->
    <p class="muted small explain">
      Câmera não se cadastra aqui do zero: o dwnvr grava o que o go2rtc entrega e não guarda
      endereço nem senha de câmera. Declare o stream no <code>go2rtc.yaml</code> e reinicie o go2rtc,
      que só lê o arquivo quando sobe. Aí ele aparece nesta lista - um clique escolhe cota, áudio e retenção,
      e a gravação começa. Vale também para mudar uma câmera que já grava.
    </p>

    <!-- No warning doesn't mean the file is the same: changing the ffmpeg
         source of a camera that is recording is not detectable (see
         go2rtc.FindDrift). The restart stays within reach, discreet. -->
    {#if !cameras.go2rtcConfigFile && !cameras.go2rtcError}
      <p class="muted small restart">
        Mudou o <code>go2rtc.yaml</code> e não apareceu aqui?
        <button class="link" onclick={() => (confirmingRestart = true)} disabled={restarting}>
          {restarting ? 'reiniciando…' : 'reiniciar o go2rtc'}
        </button>
      </p>
    {/if}
  </div>

  <!-- Only shows when there's something: a permanently empty section would
       suggest that leftover orphan recordings are the normal state, and they're
       not. -->
  {#if cameras.orphans.length}
    <div class="card orphans">
      <h3>Gravações de câmeras removidas</h3>
      <p class="muted small">
        Não contam na cota de ninguém, não abrem na tela de Gravações e a retenção não as alcança,
        nem com o disco cheio - só saem do disco por aqui.
      </p>

      {#each cameras.orphans as o (o.id)}
        <!-- The row doesn't wrap; the data block does. That way the button
             always stays in the same corner, instead of dropping to the left
             when the chips don't fit. -->
        <div class="row orphan">
          <div class="row wrap data">
            <code>{o.id}</code>
            {#if o.days}
              <span class="chip">{bytes(o.bytes)}</span>
              <span class="chip">
                {ddmm(o.firstMs)} a {ddmm(o.lastMs)} · {o.days}
                {o.days > 1 ? 'dias' : 'dia'}
              </span>
            {:else}
              <!-- Directory without an index: leftover from an eviction or a
                   manual copy. We can't tell the size, but hiding it would be
                   worse. -->
              <span class="chip warn">sem índice</span>
            {/if}
          </div>
          <button class="ghost small danger" onclick={() => (deleting = orphanTarget(o))}>
            apagar
          </button>
        </div>
      {/each}
    </div>
  {/if}

  <HealthFooter />
</div>

<!-- The top of the card's areas and alert blocks: the icon, the name and the
     "editar", which says what clicking the block does. -->
{#snippet blockTitle(icon: 'area' | 'bell', name: string)}
  <span class="block-title">
    <Icon name={icon} size={14} />
    {name}
    <span class="ir">editar <Icon name="chevronDir" size={14} /></span>
  </span>
{/snippet}

<!-- What the "Áreas" and "Alerta" tabs show when the camera has no detection:
     what's missing and how to fix it. Marking, when off, is turned on in the
     adjacent tab; the object detector lives in dwnvr.yaml, and the screen only
     points to the docs. -->
{#snippet noDetection(toUse: string, unused: string, stored: string)}
  <div class="no-detection">
    {#if !cameras.detector}
      <p class="small">
        O detector de objetos não está configurado no <code>dwnvr.yaml</code>. Sem ele não há
        detecção de objetos, e {unused}.
      </p>
      <a class="small" href="{REPO_URL}#ligar-o-detector-de-objetos" target="_blank" rel="noopener"
        >como ligar o detector de objetos</a
      >
    {:else}
      <p class="small">
        Esta câmera está sem detecção. Ligue <b>Marcar movimento na timeline</b> na aba Detecção
        para
        {toUse}.
      </p>
      <button type="button" class="ghost small" onclick={() => (tab = 'deteccao')}>
        ir para a aba Detecção
      </button>
    {/if}
    {#if stored}<p class="muted small">{stored}</p>{/if}
  </div>
{/snippet}

{#if editing}
  <!-- Escape in the area editor closes only the editor: the form below stays. -->
  <Modal
    variableHeight
    onclose={() => {
      if (!openedArea && !saving) editing = null;
    }}
  >
    <!-- When registering, before the last tab, submit means "next": Enter in a
         field advances, and the browser's validation blocks an empty field
         before leaving its tab. -->
    <form
      class="fields"
      onsubmit={(e) => {
        e.preventDefault();
        void (guided ? goTo(1) : save());
      }}
      oninvalidcapture={invalidField}
    >
      <h3>{editing._new ? 'Cadastrar' : 'Editar'} {editing.id}</h3>

      <Tabs tabs={TABS} bind:active={tab} prefix="cam" />

      <!-- The panels stack in the same grid cell, and the hidden one just
           disappears from view, with zero height: its fields still go in the
           submit, and the form has the height of the open tab, without the gap
           the tallest one left on the others. So the tabs don't move around
           because of this, the modal has a fixed height (`variableHeight`). -->
      <div class="panels">
        <div
          class="fields"
          role="tabpanel"
          id="cam-painel-gravacao"
          aria-labelledby="cam-aba-gravacao"
          data-aba="gravacao"
          inert={tab !== 'gravacao'}
        >
          <label>
            Nome
            <input bind:value={editing.name} required />
          </label>

          <label class="check">
            <input type="checkbox" bind:checked={editing.enabled} />
            Gravar esta câmera
          </label>

          <label>
            Áudio
            <select bind:value={editing.audio}>
              <option value="none">nenhum - custo zero</option>
              <option value="flac" disabled={audioBlocked}>
                FLAC - ~0,6% de CPU, +260 kbps de disco
              </option>
              <option value="aac" disabled={audioBlocked}>
                AAC - ~10% de CPU, +64 kbps (exige fonte ffmpeg no go2rtc)
              </option>
            </select>
            {#if editing._probing}
              <small class="muted">Verificando se esta câmera entrega áudio...</small>
            {:else if editing._probeError}
              <small class="muted">
                Não deu para verificar o áudio desta câmera ({editing._probeError}). As opções estão
                liberadas; o Diagnóstico avisa se o áudio não chegar.
              </small>
            {:else if !editing._hasAudio}
              <small class="muted">
                Esta câmera não entrega trilha de áudio. Remova o <code>#media=video</code>
                da fonte no go2rtc.yaml para habilitá-la.
              </small>
            {:else if editing._audioCodecs.length}
              <small class="muted">
                Esta câmera entrega {editing._audioCodecs.join(', ')}.
              </small>
            {/if}
          </label>

          <label>
            Cota em disco (MB)
            <!-- step=1 because on a native input the step is also the
                 validation ruler: with step=100 the browser rejected 20480 (20
                 GiB), which is exactly the kind of number you type for a quota.
                 The min comes from the API: without a response, the input has
                 no min and the server is the one that rejects. -->
            <input
              type="number"
              bind:value={editing.quotaMB}
              min={bounds?.quotaMB?.min}
              step="1"
              required
            />
            <!-- The field is in MB because that's how the quota is stored, but
                 whoever types 20480 wants to know that's 20 GB. -->
            <small class="muted">
              = {mbToBytes(editing.quotaMB)} ·
              {#if estimateDays(editing, editing.quotaMB)}
                ≈ {dias(estimateDays(editing, editing.quotaMB))} no consumo médio medido
              {:else}
                a estimativa aparece após a primeira medição de taxa
              {/if}
            </small>
          </label>

          <label>
            Duração do segmento (s)
            <input
              type="number"
              bind:value={editing.segmentSeconds}
              min={bounds?.segmentSeconds?.min}
              max={bounds?.segmentSeconds?.max}
              step="10"
            />
            <small class="muted">o corte real espera o próximo keyframe</small>
          </label>
        </div>

        <div
          class="fields"
          role="tabpanel"
          id="cam-painel-deteccao"
          aria-labelledby="cam-aba-deteccao"
          data-aba="deteccao"
          inert={tab !== 'deteccao'}
        >
          <label class="check">
            <input type="checkbox" bind:checked={editing.detect} />
            Marcar movimento na timeline
          </label>

          {#if editing.detect}
            <label>
              Sensibilidade
              <select bind:value={editing.detectSensibilidade}>
                {#each LEVELS as n, i (i)}
                  <option value={i + 1}>{i + 1} - {n.label}</option>
                {/each}
              </select>
              <!-- The cost is what the user really pays, and it's PER CAMERA:
                   with ten cameras at level 4 that's 500 inferences per hour,
                   not 50. -->
              <small class="muted">
                ≈ {level(editing.detectSensibilidade)?.perHour} marcas por hora nesta câmera
              </small>
            </label>

            <label>
              Mecanismo
              <select bind:value={editing.detectMecanismo}>
                <option value="kleinberg-p">estatístico (recomendado)</option>
                <option value="periodico">periódico</option>
              </select>
              <small class="muted">
                {editing.detectMecanismo === 'periodico'
                  ? 'dispara em intervalo fixo, ignorando a imagem - custo previsível, mas pega menos'
                  : 'dispara quando a imagem destoa do normal da própria câmera'}
              </small>
            </label>
          {/if}
        </div>

        <div
          class="fields"
          role="tabpanel"
          id="cam-painel-areas"
          aria-labelledby="cam-aba-areas"
          data-aba="areas"
          inert={tab !== 'areas'}
        >
          {#if !withDetection}
            {@const n = editing.areas?.length ?? 0}
            {@render noDetection(
              'usar as áreas',
              'as áreas não têm o que filtrar',
              n === 0
                ? ''
                : n === 1
                  ? 'A área desta câmera fica guardada.'
                  : `As ${n} áreas desta câmera ficam guardadas.`,
            )}
          {:else}
            {@const areas = editing.areas ?? []}
            <p class="muted small explain-areas">
              Pedaços da imagem com nome, como "Portão". Em Detecções, qualquer pessoa filtra por
              eles. Se a câmera for movida, redesenhe.
            </p>
            {#if frame?.erro}
              <p class="small areas-notice">{frame.erro}</p>
            {/if}
            {#if areas.length}
              <div class="areas">
                {#each areas as a, i (a.id ?? `nova-${i}`)}
                  <div class="area-item">
                    <AreaLabel cam={editing.id} area={a} width={96} bold background={cameraFrame} />
                    <button
                      type="button"
                      class="ghost small"
                      onclick={() => openArea(i)}
                      disabled={!frame || frame.fetching}>editar</button
                    >
                    <button type="button" class="ghost small danger" onclick={() => deleteArea(i)}>
                      apagar
                    </button>
                  </div>
                {/each}
              </div>
            {/if}
            {#if !editing._new}
              <button
                type="button"
                class="ghost new-area"
                onclick={() => openArea(null)}
                disabled={!frame?.url || areas.length >= MAX_AREAS}
              >
                <Icon name="mais" size={16} stroke={2.4} />nova área
              </button>
              {#if areas.length >= MAX_AREAS}
                <small class="muted">Até {MAX_AREAS} áreas por câmera.</small>
              {/if}
            {/if}
          {/if}
        </div>

        <div
          class="fields"
          role="tabpanel"
          id="cam-painel-alerta"
          aria-labelledby="cam-aba-alerta"
          data-aba="alerta"
          inert={tab !== 'alerta'}
        >
          {#if !withDetection}
            {@render noDetection(
              'usar o alerta',
              'o alerta não tem o que avisar',
              editing.alert?.enabled
                ? 'O alerta desta câmera fica guardado, e volta a valer com a detecção.'
                : '',
            )}
          {:else if AlertTab}
            <AlertTab
              bind:alert={editing.alert}
              bind:where={alertWhere}
              bind:limitSchedules
              bind:schedules={alertSchedules}
              bind:error={alertError}
              areas={editing.areas ?? []}
              cam={editing.id}
              frame={cameraFrame}
              timezone={serverTimezone}
              byEmail={cameras.alertEmail}
            />
          {/if}
        </div>
      </div>

      {#if formError}<p class="small form-error" role="alert">{formError}</p>{/if}
      <div class="row">
        {#if editing._new}
          {#if tabIndex > 0}
            <button type="button" class="ghost step" onclick={() => void goTo(-1)}>
              <Icon name="chevronEsq" size={16} stroke={2.4} />voltar
            </button>
          {/if}
        {:else}
          <button
            type="button"
            class="danger"
            disabled={saving}
            onclick={() => {
              if (editing) openRemoval(editing);
              editing = null;
            }}>remover</button
          >
        {/if}
        <span class="spacer"></span>
        <button type="button" class="ghost" disabled={saving} onclick={() => (editing = null)}
          >cancelar</button
        >
        {#if guided}
          <button class="primary step" type="submit">
            próximo<Icon name="chevronDir" size={16} stroke={2.4} />
          </button>
        {:else}
          <button class="primary" type="submit" disabled={saving}>
            {saving ? 'salvando…' : 'salvar'}
          </button>
        {/if}
      </div>
    </form>
  </Modal>

  {#if openedArea && AreaEditor}
    {@const areas = editing.areas ?? []}
    {@const i = openedArea.index}
    {@const current = i === null ? null : areas[i]}
    <AreaEditor
      camera={editing.name}
      title={current ? 'Editar área' : 'Nova área'}
      applyLabel={current ? 'salvar' : 'criar área'}
      background={frame?.url ? { tipo: 'imagem', url: frame.url } : null}
      error={frame?.erro ?? ''}
      initial={current ? { points: current.points, rule: current.rule ?? DEFAULT_RULE } : null}
      name={current?.name ?? ''}
      namesInUse={areas.filter((_, j) => j !== i).map((a) => a.name)}
      otherAreas={areas.filter((_, j) => j !== i).map((a) => ({ name: a.name, points: a.points }))}
      frame={frame
        ? {
            caption: frameCaption(frame.t),
            refreshing: frame.fetching,
            onrefresh: () => {
              if (editing) void fetchFrame(editing.id, false);
            },
          }
        : null}
      onapply={applyArea}
      ondelete={current && i !== null ? () => deleteArea(i) : null}
      onclose={() => (openedArea = null)}
    />
  {/if}

  {#if deletingArea !== null}
    {@const i = deletingArea}
    {@const area = editing.areas?.[i]}
    {@const inAlert = usedInAlert(area?.id)}
    {@const lastOne = inAlert && (editing.alert?.areas ?? []).length <= 1}
    <ConfirmDialog
      title="Apagar a área {area?.name ?? ''}?"
      confirmLabel={lastOne ? 'apagar e desligar o alerta' : 'apagar área'}
      danger
      onconfirm={() => {
        removeArea(i);
        deletingArea = null;
      }}
      oncancel={() => (deletingArea = null)}
    >
      {#if lastOne}
        Ela é a única área do alerta desta câmera. Apagá-la <strong>desliga o alerta</strong>, que
        não passa a valer na imagem inteira. As famílias ficam guardadas, para religar.
      {:else if inAlert}
        O alerta desta câmera usa esta área, e ela sai dele também. As outras áreas do alerta
        continuam.
      {:else}
        Ela sai da câmera quando você salvar. Até lá, cancelar o formulário a traz de volta.
      {/if}
    </ConfirmDialog>
  {/if}
{/if}

{#if removing}
  {@const tam = sizeOf(removing)}
  <ConfirmDialog
    title="Remover {removing.name}?"
    confirmLabel={deleteRecordings ? `remover e apagar ${bytes(tam)}` : 'remover'}
    danger
    onconfirm={confirmRemoval}
    oncancel={() => (removing = null)}
  >
    A câmera sai do dwnvr e para de gravar agora.

    <!-- Unchecked by default: deleting hours of video can't be a side effect of
         clicking "remover". The button label follows the checkbox so the
         consequence is written on what you click. -->
    <label class="check delete">
      <input type="checkbox" bind:checked={deleteRecordings} />
      apagar também as gravações ({bytes(tam)})
    </label>

    {#if !deleteRecordings}
      Sem marcar, os arquivos ficam em disco e passam a aparecer aqui em
      <strong>Gravações de câmeras removidas</strong>, de onde dá para apagá-los depois.
    {/if}
  </ConfirmDialog>
{/if}

{#if confirmingRestart}
  {@const activeCount = cameras.list.filter((c) => c.enabled).length}
  <ConfirmDialog
    title="Reiniciar o go2rtc?"
    confirmLabel="reiniciar"
    onconfirm={restartGo2rtc}
    oncancel={() => (confirmingRestart = false)}
  >
    Ele volta a ler o <code>go2rtc.yaml</code>.
    {activeCount === 1
      ? 'Enquanto isso, a câmera para de gravar por alguns segundos e volta sozinha.'
      : `Enquanto isso, as ${activeCount} câmeras param de gravar por alguns segundos e voltam sozinhas.`}
    {#each willStop as id (id)}
      <p class="will-stop">
        <code>{id}</code> saiu do arquivo: depois do reinício ela para de gravar.
      </p>
    {/each}
  </ConfirmDialog>
{/if}

{#if deleting}
  <ConfirmDialog
    title="Apagar as gravações de {deleting.nome}?"
    confirmLabel={deleting.bytes ? `apagar ${bytes(deleting.bytes)}` : 'apagar'}
    danger
    onconfirm={confirmDeletion}
    oncancel={() => (deleting = null)}
  >
    {#if deleting.orphan}
      Tudo o que sobrou de <code>{deleting.id}</code> sai do disco, e não há como desfazer.
    {:else}
      Tudo o que essa câmera gravou sai do disco, e não há como desfazer. A câmera continua
      cadastrada e volta a gravar em seguida - a gravação fica interrompida por alguns segundos.
    {/if}
  </ConfirmDialog>
{/if}

<style>
  .page {
    display: grid;
    gap: 10px;
    padding: 10px;
    max-width: 900px;
    margin: 0 auto;
  }

  .list {
    display: grid;
    gap: 8px;
  }
  .cam {
    display: grid;
    gap: 8px;
  }
  .head {
    gap: 8px;
  }
  .actions {
    gap: 8px;
    margin-left: auto;
  }
  .chips {
    gap: 6px;
  }
  /* The chip starts out nowrap, and a long path has no room to wrap: without
     these two lines it overflows the card's width on phones. */
  .chip.storage {
    white-space: normal;
    overflow-wrap: anywhere;
  }
  /* Both talk about the same quota and so share the border; the color separates
     what is fact from what is calculation: "retido" already happened and stays
     in the normal text, "cabem" is a projection and goes in the accent tone,
     like every estimate on the screen. */
  .chip.retained,
  .chip.retain {
    border-color: #1f6feb66;
  }
  .chip.retain {
    color: var(--accent);
  }
  .chip.warn {
    color: var(--warn);
  }
  /* The areas and alert blocks: the whole button leads to edit, on each one's
     tab. */
  .block {
    display: grid;
    gap: 8px;
    width: 100%;
    padding: 10px;
    text-align: left;
    background: var(--panel-2);
    font-weight: normal;
  }
  .block:hover {
    border-color: var(--accent);
  }
  .block-title {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--dim);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .block-title .ir {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    margin-left: auto;
    text-transform: none;
    letter-spacing: 0;
  }
  /* Two columns on phones: the ~160 px thumbnail is the floor at which the area
     is still readable. */
  .card-areas {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    gap: 8px;
  }
  .alert-rows {
    display: grid;
    gap: 4px;
    font-size: 14px;
  }
  .alert-rows .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
  .alert-rows .row > :global(svg) {
    color: var(--dim);
  }
  .family {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 2px 9px;
    border: 1px solid var(--line);
    border-radius: 999px;
    font-size: 13px;
    font-weight: 600;
  }
  .err {
    color: var(--bad);
    margin: 0;
  }

  .warnbox {
    border-color: #6b5a1f;
    color: var(--warn);
  }
  .errbox {
    border-color: #5c2b2b;
    color: var(--bad);
  }
  .form-error {
    color: var(--bad);
  }

  h3 {
    margin: 0 0 4px;
    font-size: 15px;
  }

  /* The button sits next to the title, not in the list: it also applies when
     the list is empty, which is exactly when you suspect it. The negative
     margin gives back the height the 44px target adds to the row. */
  .help .title {
    justify-content: space-between;
    margin: -10px 0 0;
  }
  .help h3 {
    margin: 0;
  }
  /* No border at rest: with it, the button weighed more than the title beside
     it. The :hover border and the keyboard focus still apply. */
  .refresh {
    display: inline-flex;
    align-items: center;
    padding: 0 12px;
    color: var(--dim);
    border-color: transparent;
  }
  .refresh.spinning :global(svg) {
    animation: girar 0.8s linear infinite;
  }
  @keyframes girar {
    to {
      transform: rotate(360deg);
    }
  }
  /* The warning sits inside the card, between the title and the list: it's
     where you look after editing the file, and it's the list it explains. */
  .notice {
    display: grid;
    gap: 10px;
    margin: 0 0 12px;
    padding: 12px;
  }
  .notice p {
    margin: 0;
  }
  .notice ul {
    margin: 0;
    padding-left: 18px;
    color: var(--fg);
  }
  .notice li {
    margin: 2px 0;
  }
  .notice li .muted {
    font-size: 12px;
  }
  .notice .action {
    justify-content: flex-end;
  }
  button.warn {
    color: var(--warn);
    border-color: #6b5a1f;
    white-space: nowrap;
  }
  .restart {
    margin: 10px 0 0;
  }
  /* Link-like, because it's the exception exit; the touch target comes from the
     padding, since a 44px minimum height would push the line away from the
     text. */
  button.link {
    background: none;
    border: 0;
    min-height: 0;
    padding: 6px 2px;
    color: var(--accent);
    text-decoration: underline;
    font-size: inherit;
  }
  .will-stop {
    margin: 8px 0 0;
    color: var(--warn);
  }
  .help .empty {
    margin: 0;
  }
  .help .explain {
    margin: 12px 0 0;
  }
  .help code {
    color: var(--fg);
  }

  .orphans {
    display: grid;
    gap: 8px;
  }
  .orphans p {
    margin: -4px 0 2px;
  }
  .orphan {
    gap: 10px;
    align-items: start;
  }
  .orphan .data {
    gap: 6px;
    flex: 1;
    min-width: 0;
  }
  .orphan code {
    color: var(--fg);
  }
  .orphan button {
    flex: none;
  }

  /* The checkbox inside ConfirmDialog: without the margin it hugs the paragraph
     above and stops looking like a separate choice. */
  label.check.delete {
    margin: 10px 0 6px;
    font-size: 14px;
  }

  /* Modal only provides the frame; the spacing between fields belongs to the form. */
  .fields {
    display: grid;
    gap: 14px;
    align-content: start;
  }
  .panels {
    display: grid;
  }
  .explain-areas,
  .areas-notice {
    margin: 0;
  }
  .no-detection {
    display: grid;
    gap: 10px;
    justify-items: start;
  }
  .no-detection p {
    margin: 0;
  }
  .areas-notice {
    color: var(--warn);
  }
  .areas {
    display: grid;
    gap: 8px;
  }
  .area-item {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  button.new-area {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    justify-self: start;
  }
  button.step {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .panels > * {
    grid-area: 1 / 1;
  }
  .panels > [inert] {
    visibility: hidden;
    height: 0;
    overflow: hidden;
  }

  label {
    display: grid;
    gap: 5px;
    font-size: 13px;
    color: var(--dim);
  }
  label input:not([type='checkbox']),
  label select {
    width: 100%;
    color: var(--fg);
    font-size: 15px;
  }
  label.check {
    display: flex;
    align-items: center;
    gap: 9px;
    color: var(--fg);
    font-size: 15px;
  }
  label.check input {
    width: 18px;
    height: 18px;
    min-height: 0;
    accent-color: var(--accent);
  }
  small {
    font-size: 12px;
  }
</style>
