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

  // A câmera no formulário, com o que a tela sabe do áudio dela. Os campos com
  // `_` não vão para a API.
  type Draft = RegisteredCamera & {
    _new: boolean;
    _hasAudio: boolean;
    _audioCodecs: string[];
    _probing: boolean;
    _probeError: string;
  };

  // De quem são as gravações a apagar: uma câmera cadastrada ou uma órfã.
  interface Target {
    id: string;
    nome: string;
    bytes: number;
    orphan: boolean;
  }

  let editing = $state<Draft | null>(null); // cópia da câmera em edição, ou null
  let removing = $state<RegisteredCamera | null>(null); // câmera aguardando confirmação de remoção
  let deleting = $state<Target | null>(null); // gravações aguardando confirmação de apagamento
  let deleteRecordings = $state(false); // checkbox do diálogo de remoção
  let saving = $state(false);
  let error = $state('');
  // A recusa do servidor ao salvar. Fica dentro do formulário, junto do
  // "salvar": o `error` da página aparece atrás do fundo escuro do Modal.
  let formError = $state('');

  // As abas do formulário. Ele sempre abre na primeira: é onde estão o nome e a
  // cota, o que se procura ao cadastrar. Áreas e Alerta só funcionam com a
  // marcação de movimento ligada e o detector de objetos configurado: sem os
  // dois a câmera não tem detecção, a área não teria o que filtrar e o alerta,
  // o que avisar. Sem eles, as duas abas ficam, e dizem o que falta: escondidas,
  // ninguém saberia que existem. Desligar a marcação não apaga nada.
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
  // No cadastro, o rodapé guia aba a aba e o "salvar" só aparece na última:
  // quem cadastra passa por tudo. Na edição se vai direto a uma coisa, e as
  // abas bastam.
  const tabIndex = $derived(TABS.findIndex((a) => a.id === tab));
  const guided = $derived(!!editing?._new && tabIndex < TABS.length - 1);
  // Depois da troca, o foco vai para o painel novo: o de antes fica `inert` e
  // o botão clicado pode sumir do rodapé, e sem isto o foco cai no <body> e o
  // Tab recomeça do topo. Avançando, vai ao primeiro campo, e quem digitava no
  // Enter segue digitando. Voltando, vai à aba: no celular, o foco no campo de
  // texto abriria o teclado sem ninguém pedir.
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

  // Reinício do go2rtc: ele só lê o go2rtc.yaml quando sobe.
  let confirmingRestart = $state(false);
  let restarting = $state(false);
  // O go2rtc responde ao pedido antes de reiniciar: perguntar logo em seguida
  // ainda pega o processo velho, com a lista velha.
  const RESTART_FIRST_ASK_MS = 1500;
  const RESTART_INTERVAL_MS = 1000;
  // Medido no go2rtc local: volta em menos de 2 s. Passado disto, desiste e
  // deixa a tela mostrar o erro de conexão, que é a verdade.
  const RESTART_GIVE_UP_MS = 20000;

  const registered = $derived(new Set(cameras.list.map((c) => c.id)));
  // Câmera cadastrada que saiu do arquivo para de gravar depois do reinício: é o
  // único efeito do reinício que alguém pode não querer, e o diálogo o repete.
  const willStop = $derived(
    (cameras.go2rtcConfigFile?.foraDoArquivo ?? []).filter((id) => registered.has(id)),
  );

  // Enquanto a sonda não responde, as opções de áudio seguem bloqueadas; se ela
  // FALHAR, elas são liberadas. Não conseguir verificar não é o mesmo que saber
  // que não tem áudio, e bloquear nesse caso impediria de configurar uma câmera
  // boa que só estava fora do ar na hora do cadastro.
  const audioBlocked = $derived(!editing?._hasAudio && !editing?._probeError);

  // `#cams?editar=<id>` abre direto o formulário da câmera: é o link da
  // tela Diagnóstico para quem está com a detecção desligada. O parâmetro é
  // consumido ao abrir, senão recarregar a página reabriria o formulário.
  onMount(async () => {
    const id = currentParams().get('editar');
    if (!cameras.list.length) await loadCameras();
    if (!id) return;
    write({ edit: null });
    const cam = cameras.list.find((c) => c.id === id);
    // Com outro formulário já aberto (a lista pode ter chegado por outro
    // caminho enquanto esta esperava), o pedido de abrir não o substitui.
    if (cam && !editing) edit(cam);
  });

  // O caminho comum para câmera nova é trocar de janela, editar o go2rtc.yaml e
  // voltar: recarregar quando a aba reaparece poupa o clique no botão. Com a aba
  // escondida não há custo nenhum - o evento só dispara na troca.
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

  // estimaDias responde "com esta cota, quantos dias de gravação cabem?" - e o
  // servidor já responde isso em retainDays, na densidade do que está gravado.
  //
  // Aqui só falta o "e se eu puser 40 GB?" do formulário de edição, e para isso
  // basta escalar: a estimativa é linear na cota (dias = cota / consumo por
  // dia), então a razão entre a cota digitada e a gravada dá o número novo sem
  // repetir no JS a conta que o Go faz. O denominador é o st.quotaMB, que é
  // sempre a cota salva - cam.quotaMB é o que está no formulário e se move
  // enquanto se digita.
  function estimateDays(cam: Camera, quotaMB = cam.quotaMB) {
    const st = statusOf(cam.id);
    if (!st?.retainDays || !st.quotaMB || quotaMB === undefined) return undefined;
    return (st.retainDays * quotaMB) / st.quotaMB;
  }

  function newItem(streamName: string) {
    const s = cameras.streams.find((x) => x.name === streamName);
    editing = {
      id: streamName,
      // O nome do stream no go2rtc, sem tratamento nenhum: "cam_frente" fica
      // "cam_frente".
      name: streamName,
      enabled: true,
      audio: 'none',
      quotaMB: 10240,
      segmentSeconds: 30,
      maxDays: 0,
      // Os três vêm dos defaults do servidor, onde a detecção nasce DESLIGADA:
      // quem só grava não paga nada por ela. O literal só cobre resposta sem
      // "padrao".
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

  // `abaInicial`: o bloco de áreas ou de alerta do card abre o formulário
  // direto na aba dele.
  function edit(cam: RegisteredCamera, initialTab = 'gravacao') {
    const s = cameras.streams.find((x) => x.name === cam.id);
    editing = { ...cam, _new: false, ...audioKnown(s) };
    tab = initialTab;
    formError = '';
    openAlert();
    void probeAudio(cam.id);
  }

  // O "onde" e o "quando" do alerta no card, nas palavras da aba Alerta. Área
  // marcada que não existe mais não aparece; os horários vão como a aba os
  // lista ("todo dia · 12:30 às 13:59").
  function alertWhereOnCard(cam: RegisteredCamera) {
    const ids = cam.alert?.areas ?? [];
    if (!ids.length) return 'em qualquer lugar da imagem';
    const names = (cam.areas ?? []).filter((a) => a.id && ids.includes(a.id)).map((a) => a.name);
    return `só em ${names.join(', ')}`;
  }
  // O sino na miniatura: a área está entre as do alerta ligado. O alerta na
  // imagem inteira não marca nenhuma, porque não depende de área.
  const alertInArea = (cam: RegisteredCamera, a: CameraArea) =>
    !!cam.alert?.enabled && !!a.id && !!cam.alert.areas?.includes(a.id);
  function alertWhenOnCard(hs: AlertSchedule[] | undefined) {
    const validItems = fromServer(hs).filter(valid);
    if (!validItems.length) return 'a qualquer hora';
    return validItems
      .map((h) => `${daysSummary(h.days)} · ${hoursSummary(h.from, h.to)}`)
      .join('; ');
  }

  // O que já se sabe do áudio quando o formulário abre. Num stream ocioso isso
  // é sempre "não tem", e é mentira: o go2rtc só lista as trilhas enquanto
  // alguém consome o stream. Por isso sondarAudio() vem logo atrás.
  const audioKnown = (s: Stream | undefined) => ({
    _hasAudio: s?.hasAudio ?? false,
    _audioCodecs: s?.audioCodecs ?? [],
    _probing: false,
    _probeError: '',
  });

  // Pergunta ao servidor se a câmera entrega áudio. Só vale a pena quando a
  // resposta que já se tem é "não": um "sim" veio do medias e não muda.
  //
  // A resposta pode demorar alguns segundos (o servidor abre o stream para
  // descobrir), então o formulário já está na tela quando ela chega - daí a
  // conferência de que ele ainda é o mesmo, que descarta a resposta de um
  // formulário já fechado ou trocado por outro.
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

  // Campo inválido numa aba escondida: o navegador recusa o envio, mas não tem
  // como mostrar o balão num campo invisível, e o "salvar" parece não fazer
  // nada. O primeiro campo inválido traz a aba dele para a frente e mostra o
  // motivo; os seguintes do mesmo envio ficam para o próximo clique.
  async function invalidField(e: Event) {
    const field = e.target as HTMLInputElement;
    const panel = field.closest<HTMLElement>('[role="tabpanel"]');
    const owner = panel?.dataset['aba'];
    if (!owner || owner === tab) return;
    tab = owner;
    await tick();
    field.reportValidity();
  }

  // --- a aba Áreas ----------------------------------------------------------

  // O quadro de fundo das áreas: o keyframe do último segmento, decodificado
  // aqui na resolução da câmera. É o do editor e o das miniaturas das áreas,
  // nesta aba e na Alerta. Só é buscado quando uma delas abre - a Alerta, se
  // a câmera tem área -, e sai da memória quando o formulário fecha.
  let frame = $state<{
    cam: string;
    t: number | null;
    url: string;
    ratio: number;
    fetching: boolean;
    erro: string;
  } | null>(null);
  // Cada busca tem o seu número: a resposta de uma busca já trocada por outra,
  // ou de um formulário já fechado, é descartada.
  let fetchSeq = 0;
  // A URL do quadro da câmera em edição, vazia enquanto ele não chega: o
  // fundo das miniaturas das áreas.
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
    // A câmera nova ainda não gravou, e o servidor nem a conhece.
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
    // O quadro se lê e a busca começa sem acompanhar: a busca que falhou tenta
    // de novo ao entrar numa das abas, e não a cada mudança do quadro, o que
    // repetiria sem parar um pedido que falha. A própria busca lê e grava o
    // quadro antes do primeiro await.
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

  // O editor vem num chunk à parte, como em Detecções.
  let AreaEditor = $state<typeof AreaEditorT | null>(null);
  // A área aberta no editor: o índice dela, ou null para uma nova.
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

  // O id da área nova nasce aqui, e não no servidor, para o alerta poder
  // marcá-la antes de a câmera ser salva. Mesmo formato do servidor: 6 letras
  // minúsculas e dígitos, sem repetir na câmera.
  function newAreaId(areas: CameraArea[]) {
    const alphabet = 'abcdefghijklmnopqrstuvwxyz0123456789';
    for (;;) {
      const b = crypto.getRandomValues(new Uint8Array(6));
      const id = Array.from(b, (v) => alphabet[v % alphabet.length]).join('');
      if (!areas.some((a) => a.id === id)) return id;
    }
  }

  // A área a apagar, à espera da confirmação: o índice dela.
  let deletingArea = $state<number | null>(null);

  const usedInAlert = (id: string | undefined) =>
    !!id &&
    !!editing?.alert?.enabled &&
    alertWhere === 'areas' &&
    !!editing.alert.areas?.includes(id);

  // O "apagar" da lista e o "apagar área" do editor. Os dois confirmam antes:
  // área apagada por engano é área para redesenhar. O editor fica por cima de
  // tudo, então ele fecha antes, e a confirmação aparece sobre o formulário.
  function deleteArea(i: number) {
    openedArea = null;
    deletingArea = i;
  }

  // tiraArea apaga a área e a tira do alerta. O alerta que fica sem as áreas
  // que tinha é desligado, e não passa para a imagem inteira; as famílias e o
  // "só nas áreas marcadas" ficam, para religar escolhendo de novo.
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

  // --- a aba Alerta ---------------------------------------------------------

  // Vem num chunk à parte, que só se baixa quando a aba existe.
  let AlertTab = $state<typeof AlertTabT | null>(null);
  $effect(() => {
    if (withDetection && !AlertTab)
      void import('../components/AlertTab.svelte').then((m) => {
        AlertTab = m.default;
      });
  });

  // Onde o alerta vale, como a aba mostra (ver OndeDoAlerta): "só nas áreas
  // marcadas" sem nenhuma marcada, o salvar recusa.
  let alertWhere = $state<AlertWhere>('tudo');
  let alertError = $state('');
  // Os horários como a aba os edita, em minutos. Com a caixa desmarcada, o
  // salvar apaga o `schedules`: vazio é a qualquer hora.
  let limitSchedules = $state(false);
  let alertSchedules = $state<Schedule[]>([]);

  function openAlert() {
    alertWhere = editing?.alert?.areas?.length ? 'areas' : 'tudo';
    alertSchedules = fromServer(editing?.alert?.schedules);
    limitSchedules = alertSchedules.length > 0;
    alertError = '';
  }

  // "America/Fortaleza (UTC−03:00)": o fuso em que os horários valem, que é o
  // do servidor e não o do navegador.
  const serverTimezone = $derived.by(() => {
    const c = health.clock;
    if (!c) return '';
    const s = Math.abs(c.offsetSeconds);
    const utc = `UTC${c.offsetSeconds < 0 ? '−' : '+'}${hourOfMinute(Math.floor(s / 60))}`;
    return c.zone ? `${c.zone} (${utc})` : utc;
  });

  // O que falta no alerta ligado para ele poder ser salvo. Com a aba escondida
  // não há o que conferir: o alerta fica como estava.
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

  // Quanto o salvar espera pelo servidor. O formulário fica travado enquanto
  // salva, e sem um prazo um servidor parado o prenderia aberto.
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
      // Na imagem inteira, as áreas marcadas antes não valem mais.
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
      // O formulário não fecha durante o envio (cancelar, remover e o Escape
      // esperam), então este é o mesmo que foi enviado.
      formError =
        e instanceof DOMException && e.name === 'TimeoutError'
          ? `O servidor não respondeu em ${SAVE_TIMEOUT_MS / 1000} s. Confira a lista antes de tentar de novo: o salvar pode ter chegado.`
          : messageOf(e);
    } finally {
      saving = false;
    }
  }

  // O tamanho vem da medição do diagnóstico, que é a mesma fonte do chip de
  // disco do card: dizer "apagar 3,2 GB" só ajuda se o número for o que a
  // pessoa já estava vendo.
  function sizeOf(cam: Camera) {
    return statusOf(cam.id)?.diskBytes ?? 0;
  }

  // Um alvo normalizado para que um único diálogo sirva os dois casos: a câmera
  // cadastrada, que tem nome e status, e a órfã, que só tem o que a varredura do
  // disco descobriu.
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
    // Sempre desmarcado ao abrir: preservar as gravações é o padrão, e herdar a
    // marcação de uma remoção anterior apagaria vídeo sem ninguém pedir.
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
      // Pergunta direto à API, sem passar pelo estado: enquanto o go2rtc está
      // fora, a resposta traz go2rtcError, e jogá-la na tela piscaria o aviso
      // de "não foi possível falar com o go2rtc" no meio de um reinício normal.
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
      // As duas: loadCameras refaz a lista de órfãs, loadHealth refaz o chip de
      // disco. Sem a segunda, o card seguiria mostrando os bytes que já foram.
      await loadCameras();
      await loadHealth();
    } catch (e) {
      error = messageOf(e);
    }
  }
</script>

<div class="page">
  <!-- Com a lista sem carregar, o go2rtcError é o da última leitura boa: não
       dá para afirmar que as cadastradas continuam gravando. -->
  {#if cameras.go2rtcError && !cameras.error}
    <p class="card warnbox small">
      Não foi possível falar com o go2rtc: {cameras.go2rtcError}<br />
      As câmeras já cadastradas continuam gravando, mas não dá para descobrir novas.
    </p>
  {/if}

  <!-- Sem isto, a primeira carga que falha deixava a lista vazia e a tela
       dizia "nenhuma câmera cadastrada", que é falso. -->
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
          <!-- Os três num grupo próprio para quebrarem juntos: soltos na linha,
               em tela de celular o "remover" descia sozinho para a esquerda,
               longe dos outros dois e do canto onde o polegar espera achá-lo. -->
          <div class="row actions">
            <button class="ghost small" onclick={() => edit(cam)}>editar</button>
            <!-- Rótulo por extenso: só "gravações" se leria como "ver as gravações". -->
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
          <!-- O par "retido / cabem" é o que torna a cota compreensível: o
               primeiro é o passado que existe, o segundo é o que ela ainda
               comporta. Sozinho, cada um deles engana.
               A explicação é a mesma da coluna equivalente no Diagnóstico, e
               vem de ajudas.ts justamente para não divergir dela. O "Desde"
               entra só aqui e na célula de lá: é por câmera, ao contrário do
               resto do texto, e não caberia no chip sem empurrar os outros. -->
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

        <!-- Onde os arquivos estão de verdade. Toda vez que a pergunta sai do
             navegador - fazer backup, olhar o disco por ssh, entender de onde
             vieram os bytes do chip de uso - é este caminho que se procura, e
             ele não estava escrito em lugar nenhum da interface. Linha própria
             porque é o único chip que pode ficar comprido: junto dos outros ele
             empurraria a retenção para fora do alinhamento a cada nome de pasta. -->
        {#if cam.dir}
          <div class="row wrap chips">
            <span class="chip storage" title="pasta das gravações desta câmera"
              >storage: {cam.dir}</span
            >
          </div>
        {/if}

        <!-- As áreas e o alerta, como as abas deles os mostram: cada bloco é
             um atalho para editar a câmera já na aba dele. Com a mesma
             condição das abas, para o card não falar de áreas que não filtram
             nada nem de alerta que não tem o que avisar. Vêm depois do storage
             porque são os maiores do card: o que é linha curta fica junto. -->
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

  <!-- Sempre visível, mesmo vazio: enquanto este bloco só aparecia quando havia
       stream novo, quem chegava na tela com tudo já cadastrado não encontrava o
       botão de adicionar e concluía que ele não existia. -->
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

    <!-- Depois da lista: quem já entendeu como funciona quer o botão primeiro, e
         quem não entendeu lê a explicação ao lado do que ela explica. -->
    <p class="muted small explain">
      Câmera não se cadastra aqui do zero: o dwnvr grava o que o go2rtc entrega e não guarda
      endereço nem senha de câmera. Declare o stream no <code>go2rtc.yaml</code> e reinicie o go2rtc,
      que só lê o arquivo quando sobe. Aí ele aparece nesta lista - um clique escolhe cota, áudio e retenção,
      e a gravação começa. Vale também para mudar uma câmera que já grava.
    </p>

    <!-- Sem aviso não quer dizer arquivo igual: mudar a fonte ffmpeg de uma
         câmera que está gravando não é detectável (ver go2rtc.Divergencias).
         O reinício fica ao alcance, discreto. -->
    {#if !cameras.go2rtcConfigFile && !cameras.go2rtcError}
      <p class="muted small restart">
        Mudou o <code>go2rtc.yaml</code> e não apareceu aqui?
        <button class="link" onclick={() => (confirmingRestart = true)} disabled={restarting}>
          {restarting ? 'reiniciando…' : 'reiniciar o go2rtc'}
        </button>
      </p>
    {/if}
  </div>

  <!-- Só aparece quando há algo: uma seção vazia permanente sugeriria que sobrar
       gravação órfã é o estado normal, e não é. -->
  {#if cameras.orphans.length}
    <div class="card orphans">
      <h3>Gravações de câmeras removidas</h3>
      <p class="muted small">
        Não contam na cota de ninguém, não abrem na tela de Gravações e a retenção não as alcança,
        nem com o disco cheio - só saem do disco por aqui.
      </p>

      {#each cameras.orphans as o (o.id)}
        <!-- A linha não quebra; quem quebra é o bloco de dados. Assim o botão
             fica sempre no mesmo canto, em vez de descer para a esquerda quando
             os chips não cabem. -->
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
              <!-- Diretório sem índice: sobra de uma evicção ou de uma cópia à
                   mão. Não dá para dizer o tamanho, mas esconder seria pior. -->
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

<!-- O topo dos blocos de áreas e de alerta do card: o ícone, o nome e o
     "editar", que diz o que o clique no bloco faz. -->
{#snippet blockTitle(icon: 'area' | 'bell', name: string)}
  <span class="block-title">
    <Icon name={icon} size={14} />
    {name}
    <span class="ir">editar <Icon name="chevronDir" size={14} /></span>
  </span>
{/snippet}

<!-- O que as abas Áreas e Alerta mostram sem detecção na câmera: o que falta
     e como resolver. A marcação desligada se liga na aba ao lado; o detector de
     objetos é do dwnvr.yaml, e a tela só aponta para a doc. -->
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
  <!-- O Escape do editor de área fecha só o editor: o formulário de baixo
       fica. -->
  <Modal
    variableHeight
    onclose={() => {
      if (!openedArea && !saving) editing = null;
    }}
  >
    <!-- No cadastro, antes da última aba, o envio é o "próximo": o Enter num
         campo avança, e a validação do navegador barra o campo vazio antes de
         sair da aba dele. -->
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

      <!-- Os painéis ficam um em cima do outro na mesma célula do grid, e o
           escondido só some da vista, com altura zero: os campos dele
           continuam no envio, e o formulário tem a altura da aba aberta, sem o
           vão que a maior deixava nas outras. Para as abas não mudarem de
           lugar com isso, o modal tem o alto fixo (o `alturaQueMuda`). -->
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
            <!-- step=1 porque no input nativo o step também é a régua de validação:
                 com step=100 o browser recusava 20480 (20 GiB), que é exatamente o
                 tipo de número que se digita numa cota. O min vem da API: sem
                 resposta, o input fica sem ele e quem recusa é o servidor. -->
            <input
              type="number"
              bind:value={editing.quotaMB}
              min={bounds?.quotaMB?.min}
              step="1"
              required
            />
            <!-- O campo é em MB porque é assim que a cota é guardada, mas quem digita
                 20480 quer saber que isso são 20 GB. -->
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
              <!-- O custo é o que o usuário paga de verdade, e ele é POR CÂMERA:
                   com dez câmeras no nível 4 são 500 olhadas por hora, não 50. -->
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

    <!-- Desmarcado por padrão: apagar horas de vídeo não pode ser efeito
         colateral de um clique em "remover". O rótulo do botão acompanha a
         marcação para que a consequência esteja escrita no que se clica. -->
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
  /* O chip nasce nowrap, e um caminho longo não tem espaço para quebrar: sem
     estas duas linhas ele estoura a largura do card no celular. */
  .chip.storage {
    white-space: normal;
    overflow-wrap: anywhere;
  }
  /* Os dois falam da mesma cota e por isso compartilham a borda; a cor separa
     o que é fato do que é conta: "retido" já aconteceu e fica no texto normal,
     "cabem" é projeção e vai no tom do acento, como toda estimativa da tela. */
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
  /* Os blocos de áreas e de alerta: o botão inteiro leva ao editar, na aba
     de cada um. */
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
  /* Duas colunas no celular: a miniatura de ~160 px é o piso em que a área
     ainda se lê. */
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

  /* O botão fica colado à direita do título, e não na lista: ele vale também
     quando a lista está vazia, que é justamente quando se desconfia dela. A
     margem negativa devolve a altura que o alvo de 44px acrescenta à linha. */
  .help .title {
    justify-content: space-between;
    margin: -10px 0 0;
  }
  .help h3 {
    margin: 0;
  }
  /* Sem borda em repouso: com ela, o botão pesava mais que o título ao lado. A
     borda de :hover e o foco do teclado continuam valendo. */
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
  /* O aviso fica dentro do card, entre o título e a lista: é onde se olha
     depois de editar o arquivo, e é a lista que ele explica. */
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
  /* Com cara de link, porque é a saída de exceção; o alvo de toque vem do
     padding, já que 44px de altura mínima afastariam a linha do texto. */
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

  /* O checkbox dentro do ConfirmDialog: sem a margem ele encosta no parágrafo
     acima e deixa de parecer uma escolha à parte. */
  label.check.delete {
    margin: 10px 0 6px;
    font-size: 14px;
  }

  /* O Modal só entrega a moldura; o espaçamento entre os campos é do formulário. */
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
