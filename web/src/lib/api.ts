// Cliente da API do dwnvr.
//
// Tudo passa por aqui para que um 401 tenha um único lugar de tratamento: a
// sessão expira em algum momento, e sem isso cada tela precisaria lembrar de
// lidar com isso por conta própria.
//
// Os tipos no fim do arquivo são o contrato: o JSON de cada resposta, como as
// structs Go de internal/api o escrevem e o docs/api.md descreve. Campo com
// `omitempty` no Go é opcional aqui; o TypeScript não confere o que chega pela
// rede, então o tipo é a promessa do servidor, e mudar um lado pede mudar o
// outro.

import { areaInQuery } from './area.ts';
import type { FilterArea, Point, Rule } from './area.ts';
import type { FamilyName } from './icons.ts';

let onUnauthorized = () => {
  // ninguém pediu aviso ainda: o App registra o dele ao montar
};

export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

async function request<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch('api/' + path, opts);
  await check(res);
  return (await res.json()) as T;
}

// midia baixa um pedaço de gravação para o player. Passa pelo mesmo conferir()
// do request(): o corpo de um erro não pode chegar ao SourceBuffer como vídeo.
export async function media(url: string, signal: AbortSignal) {
  const res = await fetch(url, { signal });
  await check(res);
  return res.arrayBuffer();
}

// conferir lança o erro de uma resposta antes que alguém leia o corpo. A
// mensagem é a do JSON da API; quando o corpo é outra coisa - o texto do
// http.ServeFile, o HTML de um proxy com o dwnvr reiniciando - fica o status,
// porque o statusText vem vazio no HTTP/2 e uma mensagem vazia some da tela.
async function check(res: Response) {
  if (res.status === 401) {
    onUnauthorized();
    throw new ApiError('sessão expirada', 401);
  }
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: string; fields?: FieldErrors };
    throw new ApiError(body.error ?? `o servidor respondeu ${res.status}`, res.status, body.fields);
  }
}

// json lê a resposta de um fetch feito à parte, sem o request(): o público e o
// que tem prazo.
async function json<T>(res: Promise<Response>): Promise<T> {
  return (await (await res).json()) as T;
}

export class ApiError extends Error {
  readonly status: number;
  // As recusas por campo de um formulário, quando a API as manda (o criar e
  // o trocar e-mail de Usuários). Sem elas, objeto vazio.
  readonly campos: FieldErrors;

  constructor(message: string, status: number, campos: FieldErrors = {}) {
    super(message);
    this.status = status;
    this.campos = campos;
  }
}

// MuitasTentativas é o 429 do login: o endereço passou do limite de
// tentativas, e a próxima só passa daqui a `segundos` (o Retry-After).
export class TooManyAttempts extends ApiError {
  readonly segundos: number;

  constructor(segundos: number) {
    super('muitas tentativas', 429);
    // Sem o header, ou com lixo nele, espera o mínimo: o servidor recusa de
    // novo se ainda for cedo, e manda o número certo.
    this.segundos = Number.isFinite(segundos) && segundos > 0 ? Math.ceil(segundos) : 1;
  }
}

// mensagemDe tira o texto de um erro qualquer, para a tela: o `catch` do
// TypeScript recebe `unknown`, porque em JavaScript dá para lançar qualquer
// coisa.
export function messageOf(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

export const api = {
  session: () => json<Session>(fetch('api/session')),

  // Como session, não passa pelo request(): é público e precisa funcionar na
  // tela de login, onde ainda não há sessão para expirar.
  version: () => json<Version>(fetch('api/version')),

  // Só para saber se o servidor é alcançável: o corpo não importa, e qualquer
  // resposta, até um erro HTTP, conta. Ver `esperarRede` no Ao vivo.
  ping: (ms: number) =>
    fetch('api/version', { cache: 'no-store', signal: AbortSignal.timeout(ms) }),

  login: async (username: string, password: string) => {
    const res = await fetch('api/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    });
    if (res.status === 429) throw new TooManyAttempts(Number(res.headers.get('Retry-After')));
    if (!res.ok) throw new ApiError('usuário ou senha inválidos', res.status);
    return (await res.json()) as WithPerson;
  },

  logout: () => fetch('api/logout', { method: 'POST' }),

  // A conferência do Ao vivo: o mesmo /api/session, com prazo. A resposta
  // inteira volta, porque o relógio do servidor vem no header `Date` dela.
  checkSession: (ms: number) =>
    fetch('api/session', { cache: 'no-store', signal: AbortSignal.timeout(ms) }),

  // Usuários, só do admin. Criar e o link novo devolvem o token do link, que
  // só existe nessa resposta.
  users: () => request<Users>('usuarios'),
  createUser: (user: string, name: string, email: string) =>
    request<Invite>('usuarios', {
      method: 'POST',
      body: JSON.stringify({ username: user, name, email }),
    }),
  // O que o criarUsuario recusaria com estes dados, campo a campo, sem criar
  // ninguém. Vazio é "tudo certo".
  checkNew: (user: string, name: string, email: string) =>
    request<{ fields: FieldErrors }>('usuarios/conferir', {
      method: 'POST',
      body: JSON.stringify({ username: user, name, email }),
    }),
  // Vazio apaga o e-mail.
  changeEmail: (user: string, email: string) =>
    request<{ user: User }>('usuarios/email', {
      method: 'POST',
      body: JSON.stringify({ username: user, email }),
    }),
  // O que o trocarEmail recusaria com este e-mail, sem trocar nada.
  checkEmail: (user: string, email: string) =>
    request<{ fields: FieldErrors }>('usuarios/email/conferir', {
      method: 'POST',
      body: JSON.stringify({ username: user, email }),
    }),
  // O link vai inteiro, como a tela o mostra: o servidor não sabe por qual
  // endereço é acessado, e só confere que é o link em aberto da pessoa.
  sendLink: (user: string, url: string) =>
    request<{ sentTo: string }>('usuarios/link/email', {
      method: 'POST',
      body: JSON.stringify({ username: user, url }),
    }),
  newLink: (user: string) =>
    request<Invite>('usuarios/link', {
      method: 'POST',
      body: JSON.stringify({ username: user }),
    }),
  removeUser: (user: string) =>
    request<unknown>('usuarios?username=' + encodeURIComponent(user), { method: 'DELETE' }),
  removeAvatarOf: (user: string) =>
    request<unknown>('usuarios/avatar?username=' + encodeURIComponent(user), {
      method: 'DELETE',
    }),

  // Minha conta: cada um muda o que é seu. Tudo devolve { user } como ficou,
  // para o header se atualizar sem perguntar de novo. A foto vai crua, o JPEG
  // já reduzido pelo navegador (ver lib/avatar.ts).
  account: () => request<Account>('conta'),
  changePassword: (current: string, newOne: string) =>
    request<WithPerson>('conta/senha', {
      method: 'POST',
      body: JSON.stringify({ currentPassword: current, newPassword: newOne }),
    }),
  changeName: (name: string) =>
    request<WithPerson>('conta/nome', { method: 'POST', body: JSON.stringify({ name }) }),
  uploadAvatar: (jpeg: Blob) =>
    request<WithPerson>('conta/avatar', {
      method: 'PUT',
      headers: { 'Content-Type': 'image/jpeg' },
      body: jpeg,
    }),
  removeAvatar: () => request<WithPerson>('conta/avatar', { method: 'DELETE' }),

  // O link de convite: públicos, com o token no corpo, e não na URL, para ele
  // não ficar no log de nenhum proxy. Link vencido ou já usado é 410.
  checkInvite: (token: string) =>
    request<InviteLink>('convite/conferir', { method: 'POST', body: JSON.stringify({ token }) }),
  setPassword: (token: string, password: string) =>
    request<WithPerson>('convite', {
      method: 'POST',
      body: JSON.stringify({ token, password }),
    }),

  cameras: () => request<CamerasResponse>('cameras'),
  // Devolve a câmera como ficou, já com o id das áreas novas.
  saveCamera: (cam: Camera, signal: AbortSignal | null = null) =>
    request<{ camera: Camera }>('cameras', { method: 'POST', body: JSON.stringify(cam), signal }),
  // As gravações só vão junto se forem pedidas: o padrão é preservá-las.
  deleteCamera: (id: string, { recordings = false } = {}) =>
    request<unknown>(`cameras?id=${encodeURIComponent(id)}${recordings ? '&recordings=1' : ''}`, {
      method: 'DELETE',
    }),

  // Serve tanto câmera cadastrada quanto câmera já removida - no segundo caso é
  // o único jeito de alcançar o material, que some do resto da API.
  deleteRecordings: (cam: string) =>
    request<unknown>('rec?cam=' + encodeURIComponent(cam), { method: 'DELETE' }),

  // Descobre se um stream entrega áudio. É um pedido à parte, e não um campo de
  // cameras(), porque num stream ocioso a resposta custa uma conexão de alguns
  // segundos com a câmera - ver internal/api/probe.go.
  probeStream: (name: string) => request<Probe>('streams/probe?src=' + encodeURIComponent(name)),

  // Faz o go2rtc reler o go2rtc.yaml. Todas as câmeras param por alguns segundos.
  restartGo2rtc: () => request<unknown>('go2rtc/restart', { method: 'POST' }),

  // Com prazo: um pedido que nunca volta seguraria o poll da saúde, que não
  // começa uma leitura com outra em curso. Estourado, vira falha como as outras.
  health: (ms: number) => request<Health>('health', { signal: AbortSignal.timeout(ms) }),

  // Recomeça a contagem de reconexões de todas as câmeras. Não derruba conexão.
  resetReconnects: () => request<unknown>('reconnects/reset', { method: 'POST' }),

  // A máquina que grava: temperatura, memória, pressão, storage, go2rtc e os
  // últimos avisos do log. À parte do health porque custa um teste de escrita
  // no disco - só a pede quem abriu o card "Este servidor".
  server: () => request<Server>('health/servidor'),

  // Um e-mail de teste pela seção smtp. O erro do servidor de e-mail vem como
  // ele respondeu.
  testEmail: (dest: string) =>
    request<{ sentTo: string }>('email/teste', {
      method: 'POST',
      body: JSON.stringify({ to: dest }),
    }),

  days: (cam: string) => request<Dias>('rec/days?cam=' + encodeURIComponent(cam)),

  timeline: (cam: string, day: string) =>
    request<Timeline>(`rec/timeline?cam=${encodeURIComponent(cam)}&day=${day}`),

  // A mesma timeline, mas de um pedaço do dia. O dia de hoje é relido em ciclo
  // enquanto a tela está aberta, e um dia cheio de segmentos de 30s são ~70 KB
  // sem compressão - buscar só a cauda troca isso por algumas centenas de bytes
  // por rodada.
  timelineRange: (cam: string, from: number, to: number) =>
    request<Timeline>(`rec/timeline?cam=${encodeURIComponent(cam)}&from=${from}&to=${to}`),

  // As marcas de movimento do dia - o que a faixa de calor desenha. Câmera com
  // a detecção desligada responde lista vazia, e a faixa nem aparece.
  events: (cam: string, day: string) =>
    request<Events>(`rec/events?cam=${encodeURIComponent(cam)}&day=${day}`),

  // A cauda das marcas, pelo mesmo motivo do timelineRange: no dia de hoje a
  // tela pergunta de novo em ciclo, e só quer o que ainda não tem.
  eventsRange: (cam: string, from: number, to: number) =>
    request<Events>(`rec/events?cam=${encodeURIComponent(cam)}&from=${from}&to=${to}`),

  // Uma página das detecções de objeto de todas as câmeras, da mais nova para a
  // mais velha. `antes` e `depois` são o cursor (um ou nenhum); `cams` e
  // `familias` nulos querem dizer "todas".
  deteccoes: ({ antes: before, depois: after, limite: limit, ...filter }: DetectionsRequest) => {
    const q = filterQuery(filter);
    if (before != null) q.set('antes', String(before));
    if (after != null) q.set('depois', String(after));
    if (limit) q.set('limite', String(limit));
    return request<DetectionsPage>(`deteccoes?${q}`);
  },

  // Os dias com pelo menos uma detecção do filtro, em ordem crescente, e
  // quantas cada um tem: é o que o calendário da tela de Detecções deixa
  // escolher, e o número que ele mostra em cada dia.
  detectionDays: (filter: DetectionsFilter) =>
    request<DetectionDays>(`deteccoes/dias?${filterQuery(filter)}`),
};

// O filtro que a lista e o calendário de Detecções dividem. As áreas restringem
// só a câmera delas; as outras de `cams` seguem inteiras.
function filterQuery({
  cams,
  familias: families,
  area,
  areas,
  soAlertas: onlyAlerts,
}: DetectionsFilter) {
  const q = new URLSearchParams();
  if (cams?.length) q.set('cams', cams.join(','));
  if (families?.length) q.set('familias', families.join(','));
  if (area) {
    q.set('area', areaInQuery(area));
    q.set('rule', area.rule);
  }
  if (areas?.length) q.set('areas', areas.join(','));
  if (onlyAlerts) q.set('alert', '1');
  return q;
}

// URLs de mídia são montadas, não buscadas: vão direto num <video>, num <img>
// ou no SourceBuffer.
export const mediaURL = {
  init: (cam: string, gen: string) => `api/rec/init?cam=${encodeURIComponent(cam)}&g=${gen}`,
  segment: (cam: string, t: number) => `api/rec/seg?cam=${encodeURIComponent(cam)}&t=${t}`,
  thumb: (cam: string, t: number) => `api/rec/thumb?cam=${encodeURIComponent(cam)}&t=${t}`,
  // A foto de alguém, pelo id, que muda a cada troca: o navegador a guarda para
  // sempre.
  avatar: (id: string) => `api/avatar?id=${encodeURIComponent(id)}`,
  // O JPEG que o detector de objetos olhou. `t` é o `instanteMs` da detecção.
  frame: (cam: string, t: number) => `api/deteccoes/quadro?cam=${encodeURIComponent(cam)}&t=${t}`,
  export: (cam: string, from: number, to: number) =>
    `api/rec/export?cam=${encodeURIComponent(cam)}&from=${from}&to=${to}`,
  // O live vai para o go2rtc através do proxy do dwnvr, para que a credencial
  // dele nunca chegue ao navegador.
  liveWS: (cam: string) => `${baseWS()}api/live/ws?src=${encodeURIComponent(cam)}`,
  // O aviso de sessão: um websocket mudo que o servidor fecha quando a pessoa
  // é removida ou ganha link novo. Ver `vigiarSessao` no Ao vivo.
  wsSessionNotice: () => `${baseWS()}api/session/ws`,
};

function baseWS() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const base = location.pathname.replace(/[^/]*$/, '');
  return `${proto}//${location.host}${base}`;
}

// --- o contrato -------------------------------------------------------------
//
// Instante em milissegundos é `number`; instante em texto (RFC 3339, com o
// fuso do servidor) é `string`, e só existe onde a tela quer a hora de lá.

// --- sessão e pessoas

export type Role = 'admin' | 'viewer';

// Quem está logado. Com a autenticação desligada vem como admin e dono, sem
// usuário nem nome. `avatar` é o id da foto, e só vem com foto.
export interface Person {
  username?: string;
  name?: string;
  role: Role;
  owner: boolean;
  avatar?: string;
}

export interface Session {
  authRequired: boolean;
  authenticated: boolean;
  user?: Person;
}

// O login, a senha pelo convite e as mudanças da Minha conta.
export interface WithPerson {
  ok: true;
  user: Person;
}

export interface Version {
  version: string;
  commit: string;
  date: string;
  go: string;
}

export type UserStatus = 'active' | 'invitePending' | 'inviteExpired';

export interface User {
  username: string;
  name: string;
  email?: string;
  role: Role;
  status: UserStatus;
  avatar?: string;
  inviteExpiresAtMs?: number;
  createdAtMs?: number;
  activeSinceMs?: number;
  // Só de quem já usou o dwnvr depois de o campo existir.
  lastAccessMs?: number;
}

// Sem autenticação ligada não há cadastro: vem sem `owner` e com a lista vazia.
export interface Users {
  authRequired: boolean;
  owner?: { username: string; name: string; avatar: string; lastAccessMs?: number };
  users: User[];
  inviteValidityMs: number;
  maxNameLength: number;
  maxUsernameLength: number;
  maxEmailLength: number;
  // Se a seção smtp está ligada. Servidor anterior a ela não manda.
  emailEnabled?: boolean;
}

// Os campos dos formulários de pessoa, com o nome que têm no JSON.
export type PersonField = 'username' | 'name' | 'email';

// As recusas de um formulário, campo a campo: só os campos recusados, com a
// mensagem para pôr embaixo de cada um.
export type FieldErrors = Partial<Record<PersonField, string>>;

// O token só existe nesta resposta: a tela monta o link com ele.
export interface Invite {
  user: User;
  token: string;
}

export interface InviteLink {
  username: string;
  name: string;
  inviteExpiresAtMs: number;
  minPasswordLength: number;
  maxPasswordLength: number;
}

export interface Account {
  user: Person;
  minPasswordLength: number;
  maxPasswordLength: number;
  maxNameLength: number;
  avatar: AvatarRule;
}

export interface AvatarRule {
  sidePx: number;
  maxBytes: number;
  quality: number;
}

// --- câmeras

// Uma câmera como o cameras.json a guarda. Os campos opcionais vêm resolvidos
// pelos defaults do dwnvr.yaml; ausente é "usar o default".
export interface Camera {
  id: string;
  name: string;
  enabled: boolean;
  segmentSeconds?: number;
  quotaMB?: number;
  maxDays?: number;
  audio?: string;
  stallSeconds?: number;
  detect?: boolean;
  detectMecanismo?: string;
  detectSensibilidade?: number;
  areas?: CameraArea[];
  // Só o admin recebe: o viewer não edita câmera.
  alert?: CameraAlert;
}

// Quando uma detecção da câmera vira alerta. Sem `areas`, vale a imagem
// inteira; sem `schedules`, a qualquer hora; desligado, guarda o resto para
// religar.
export interface CameraAlert {
  enabled: boolean;
  families?: FamilyName[];
  areas?: string[];
  schedules?: AlertSchedule[];
}

// Um horário do alerta: os dias, de 0 (domingo) a 6 (sábado), e o De e o Até
// em "HH:MM", com as duas pontas incluídas. O `to` é sempre maior que o
// `from`: a noite são dois horários, um até 23:59 e outro a partir de 00:00.
export interface AlertSchedule {
  days: number[];
  from: string;
  to: string;
}

// Onde o alerta vale, como a aba Alerta mostra. Não vai ao servidor, que só
// guarda as áreas: "só nas áreas marcadas" sem nenhuma marcada é um estado da
// tela, que ele leria como a imagem inteira.
export type AlertWhere = 'tudo' | 'areas';

// Uma área cadastrada na câmera. A área nova da aba Áreas já vai com `id`,
// para o alerta poder marcá-la; a do "salvar na câmera" de Detecções vai sem,
// e o servidor dá um.
export interface CameraArea {
  id?: string;
  name: string;
  rule?: Rule;
  points: Point[];
}

// A câmera da listagem: para o admin, com a pasta das gravações.
export interface RegisteredCamera extends Camera {
  dir?: string;
}

export interface Stream {
  name: string;
  registered: boolean;
  hasAudio: boolean;
  audioCodecs?: string[];
  transcoding: boolean;
}

// Gravações de uma câmera que já foi removida.
export interface Orphan {
  id: string;
  bytes: number;
  days: number;
  firstMs: number;
  lastMs: number;
}

// Sem teto, o `max` não vem.
export interface Bounds {
  min: number;
  max?: number;
}

// Os campos da câmera com faixa: o CameraRanges de internal/config/params.go.
export type FieldWithBounds =
  'quotaMB' | 'segmentSeconds' | 'maxDays' | 'stallSeconds' | 'detectSensibilidade';
export type BoundsMap = Partial<Record<FieldWithBounds, Bounds>>;

// O que está no go2rtc.yaml e o go2rtc ainda não carregou. Lista vazia não vem.
export interface Go2rtcDrift {
  novas?: string[];
  alteradas?: string[];
  foraDoArquivo?: string[];
}

// O viewer recebe só `cameras` e `detector`; o resto é do admin, e
// `streams` some também quando o go2rtc não responde.
export interface CamerasResponse {
  cameras: RegisteredCamera[];
  detector: boolean;
  padrao?: Camera;
  faixas?: BoundsMap;
  orphans?: Orphan[];
  streams?: Stream[];
  go2rtcError?: string;
  go2rtcConfigFile?: Go2rtcDrift;
  alertEmail?: EmailAlert;
}

// O "Para onde vai" da aba Alerta: `to` com o meio de e-mail, e `failingSince`
// e `error` com ele falhando, até o primeiro envio que der certo. Sem o meio,
// `missing` diz as seções do dwnvr.yaml que faltam, e `smtpError`, o motivo da
// seção smtp recusada.
export interface EmailAlert {
  to?: string[];
  failingSince?: string;
  error?: string;
  missing?: ('alerts.email.to' | 'smtp')[];
  smtpError?: string;
}

// `erro` vem quando a sonda falhou: aí `hasAudio` é "não sei", e não "não tem".
export interface Probe {
  name: string;
  hasAudio: boolean;
  audioCodecs?: string[];
  probed?: boolean;
  erro?: string;
}

// --- saúde

// As fatias somam o `onsets` da câmera. `pedacos` não é fatia: é quantos onsets
// tiveram o vídeo cortado e oferecido à fila.
export interface Funnel {
  pedacos: number;
  semVideo: number;
  descartados: number;
  falhas: number;
  recusados: number;
  semObjeto: number;
  comObjeto: number;
  naFila: number;
}

// Uma câmera do /api/health. Câmera desabilitada vem sem `audio`.
export interface HealthCamera {
  id: string;
  name: string;
  enabled: boolean;
  connected: boolean;
  connectedAt?: string;
  bitrateKbps: number;
  bytes: number;
  segments: number;
  reconnects: number;
  lastError?: string;
  videoCodec?: string;
  hasAudio: boolean;
  gen?: string;
  audio?: string;
  transcoding: boolean;
  disconnectedAt?: string;
  reconnectsSince: string;
  reconnectsInWindow: number;
  lastReconnectAt?: string;
  width?: number;
  height?: number;
  lastSegmentAt?: string;
  silent: boolean;
  oldestSegmentAt?: string;
  recordedMs?: number;
  detect: boolean;
  onsets: number;
  funil?: Funnel;
  quotaMB: number;
  diskBytes: number;
  retainDays: number;
}

export interface Disco {
  freeBytes: number;
  totalBytes: number;
  dwnvrBytes: number;
  minFreeMB: number;
  belowMin: boolean;
  orphanBytes?: number;
  belowMinSince?: string;
}

export interface Uptime {
  appSeconds: number;
  machineSeconds?: number;
}

export interface Clock {
  now: string;
  abbr: string;
  offsetSeconds: number;
  zone?: string;
}

// A fila do detector de objetos, uma só para todas as câmeras. `cameras` traz
// só as câmeras com algo esperando ou sendo olhado.
export interface Detector {
  fila: {
    agora: number;
    cap: number;
    pico: number;
    porCamera: number;
    cameras: Record<string, { esperando: number; olhando: boolean }>;
  };
  tempos: { analiseMs: number; esperaMs: number };
  foraDoAr?: { desde: string; erro: string };
}

// `disk` some quando o storage não responde; `detector`, sem detector de
// objetos configurado; `go2rtc`, enquanto ele responde; `alerts`, antes de
// alguma câmera ligar o alerta, se a seção alerts não tem problema. `uptime`,
// `clock` e `reconnectsWindowHours` faltam em servidor anterior a eles, para
// onde a tela do `npm run dev` pode estar apontada.
// Os alertas que não saíram pelos meios, num episódio que acaba uma hora
// depois do último descarte: `queueFull` não couberam na fila de envio, e
// `overLimit` passaram do teto de `limitPerHour`.
export interface AlertsDropped {
  queueFull: number;
  overLimit: number;
  limitPerHour: number;
  since: string;
  lastAt: string;
}

// O meio do alerta pelo nome que o servidor dá a ele, como se lê na frase:
// "Alertas por e-mail". Meio que a tela ainda não conhece sai pelo nome mesmo.
export const ALERT_NOTIFIERS: Readonly<Record<string, string>> = { email: 'por e-mail' };
export const alertNotifier = (name: string) => ALERT_NOTIFIERS[name] ?? `por ${name}`;

// Um meio do alerta falhando desde `since`, com o erro da falha mais recente.
// Some no primeiro envio que der certo.
export interface FailingNotifier {
  name: string;
  since: string;
  error: string;
}

export interface Health {
  cameras: HealthCamera[];
  disk?: Disco;
  uptime?: Uptime;
  detector?: Detector;
  clock?: Clock;
  reconnectsWindowHours?: number;
  go2rtc?: { error: string; since: string };
  // Só com a seção smtp recusada no boot: o e-mail fica desligado.
  smtp?: { error: string };
  // Só com problema no usuarios.json: `error` é o arquivo ilegível no boot, e
  // `warnings`, as entradas que não valem.
  users?: { error?: string; warnings?: string[] };
  // `total` some antes de alguma câmera ligar o alerta; `warnings` são os
  // problemas da seção alerts do dwnvr.yaml, que valem sem câmera nenhuma; e
  // `dropped` só vem durante um episódio de descarte, e `failing`, com algum
  // meio falhando.
  alerts?: {
    total?: number;
    warnings?: string[];
    dropped?: AlertsDropped;
    failing?: FailingNotifier[];
  };
}

// --- o servidor (card "Este servidor" do Diagnóstico)
//
// Cada campo some quando a fonte não existe: kernel sem PSI, sistema sem
// cgroup, fora do Linux.

export interface PressureAverages {
  avg10: number;
  avg60: number;
  avg300: number;
}

export interface Pressure {
  some?: PressureAverages;
  full?: PressureAverages;
}

export type PressureResource = 'cpu' | 'memoria' | 'io';

export interface ServerProcess {
  cpuMs?: number;
  vivoMs: number;
  memoriaBytes?: number;
  goroutines: number;
  arquivosAbertos?: number;
  limiteDeArquivos?: number;
  uid?: number;
  gid?: number;
  go: string;
  arquitetura: string;
}

export interface LogLine {
  em: string;
  nivel: string;
  texto: string;
}

// `coletadoEm` e `processo` são opcionais porque servidor anterior a eles não
// os manda: a tela do `npm run dev` pode estar apontada para um.
export interface Server {
  coletadoEm?: { em: string; sigla: string; offsetSeconds: number; zona?: string };
  maquina: {
    placa?: string;
    processador?: string;
    kernel?: string;
    nucleos: number;
    nucleosLiberados?: number;
    carga?: number[];
    sensores?: { nome: string; celsius: number }[];
    mhz?: number;
    mhzMax?: number;
    governor?: string;
  };
  processo?: ServerProcess;
  memoria?: {
    totalBytes: number;
    disponivelBytes: number;
    swapTotalBytes: number;
    swapUsadaBytes: number;
    limiteBytes?: number;
    oomKills?: number;
  };
  pressao?: Partial<Record<PressureResource, Pressure>>;
  storage: {
    caminho: string;
    tipo?: string;
    origem?: string;
    opcoes?: string;
    somenteLeitura: boolean;
    escrita: { ok: boolean; ms: number; erro?: string };
  };
  go2rtc: { ok: boolean; ms: number; versao?: string; erro?: string; url?: string };
  detector?: {
    ok: boolean;
    ms: number;
    modelo?: string;
    entrada?: string;
    threads?: number;
    erro?: string;
    url: string;
  };
  // Só com a seção smtp no dwnvr.yaml; sem o login e sem a senha. `error` é o
  // motivo de o e-mail estar desligado.
  smtp?: { host?: string; port?: number; security?: string; from?: string; error?: string };
  // Só com meio do alerta e alguma câmera com o alerta ligado. `ultimo` some
  // antes do primeiro envio, e `erro`, com o último envio tendo dado certo.
  alertas?: AlertNotifier[];
  log?: { total: number; linhas: LogLine[] };
}

export interface AlertNotifier {
  nome: string;
  enviados: number;
  falhas: number;
  ultimo?: string;
  erro?: string;
}

// --- gravações

export interface DaySummary {
  day: string;
  count: number;
  bytes: number;
  firstMs: number;
  lastMs: number;
  durMs: number;
}

export interface Dias {
  cam: string;
  days: DaySummary[];
}

// [início, fim] de um trecho contíguo, para desenhar a barra.
export type Span = [startMs: number, endMs: number];

// [início, duração, índice em `gens`] de um segmento, para tocar.
export type Segment = [startMs: number, durationMs: number, gen: number];

// Compacta de propósito: um dia com segmentos de 1 minuto são 1.440 entradas.
export interface Timeline {
  cam: string;
  from: number;
  to: number;
  ranges: Span[];
  gens: string[];
  segments: Segment[];
}

// X1, Y1, X2, Y2 em fração da imagem, de 0 a 1.
export type Box = [x1: number, y1: number, x2: number, y2: number];

// Uma marca de objeto. `quadroMs` e `caixa` dizem onde desenhar o objeto sobre
// o vídeo; marca sem os dois não ganha caixa.
export interface MarkedObject {
  instanteMs: number;
  familia: string;
  classe: string;
  score: number;
  quadroMs?: number;
  caixa?: Box;
}

export interface Events {
  cam: string;
  from: number;
  to: number;
  onsets: number[];
  objetos: MarkedObject[];
}

// --- detecções

export interface ObjectInFrame {
  familia: string;
  classe: string;
  score: number;
  caixa?: Box;
  // O objeto passou na regra de alerta da câmera, na hora em que foi olhado, e
  // caiu nas áreas do alerta destes ids. Sem áreas no alerta, `alertAreas` não
  // vem. O id de uma área apagada desde então continua vindo.
  alert?: boolean;
  alertAreas?: string[];
}

// Uma olhada do detector numa câmera: o instante do onset e tudo o que ele
// achou naquele quadro.
export interface Detection {
  cam: string;
  instanteMs: number;
  quadroMs?: number;
  temQuadro: boolean;
  objetos: ObjectInFrame[];
}

export interface DetectionsPage {
  deteccoes: Detection[];
  fim: boolean;
}

// Nulo ou ausente é "todas".
export interface DetectionsFilter {
  cams?: readonly string[] | null;
  familias?: readonly string[] | null;
  // Na câmera desta área, só os objetos dentro dela. As outras câmeras não
  // mudam.
  area?: FilterArea | null;
  // O mesmo, com as áreas cadastradas: `<cam>/<id>`. As de uma câmera somam,
  // a `area` junto: qualquer uma serve.
  areas?: readonly string[] | null;
  // Só as detecções em que algum objeto que fica alertou; elas vêm inteiras,
  // com o objeto que não alertou.
  soAlertas?: boolean;
}

// `antes` e `depois` são o cursor: um ou nenhum.
export interface DetectionsRequest extends DetectionsFilter {
  antes?: number | null;
  depois?: number | null;
  limite?: number;
}

export interface DetectionDays {
  dias: string[];
  // Dia -> quantas detecções do filtro. Só os dias de `dias`.
  deteccoesPorDia: Record<string, number>;
}
