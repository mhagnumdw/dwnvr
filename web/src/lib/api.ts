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

let onUnauthorized = () => {
  // ninguém pediu aviso ainda: o App registra o dele ao montar
};

export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

async function request<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch('api/' + path, opts);

  if (res.status === 401) {
    onUnauthorized();
    throw new ApiError('sessão expirada', 401);
  }
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: string };
    throw new ApiError(body.error ?? res.statusText, res.status);
  }
  return (await res.json()) as T;
}

// json lê a resposta de um fetch feito à parte, sem o request(): o público e o
// que tem prazo.
async function json<T>(res: Promise<Response>): Promise<T> {
  return (await (await res).json()) as T;
}

export class ApiError extends Error {
  readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}

// mensagemDe tira o texto de um erro qualquer, para a tela: o `catch` do
// TypeScript recebe `unknown`, porque em JavaScript dá para lançar qualquer
// coisa.
export function mensagemDe(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

export const api = {
  session: () => json<Sessao>(fetch('api/session')),

  // Como session, não passa pelo request(): é público e precisa funcionar na
  // tela de login, onde ainda não há sessão para expirar.
  version: () => json<Versao>(fetch('api/version')),

  // Só para saber se o servidor é alcançável: o corpo não importa, e qualquer
  // resposta, até um erro HTTP, conta. Ver `esperarRede` no Ao vivo.
  ping: (ms: number) =>
    fetch('api/version', { cache: 'no-store', signal: AbortSignal.timeout(ms) }),

  login: async (username: string, password: string) => {
    const res = await fetch('api/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    });
    if (!res.ok) throw new ApiError('usuário ou senha inválidos', res.status);
    return (await res.json()) as ComPessoa;
  },

  logout: () => fetch('api/logout', { method: 'POST' }),

  // A conferência do Ao vivo: o mesmo /api/session, com prazo. A resposta
  // inteira volta, porque o relógio do servidor vem no header `Date` dela.
  conferirSessao: (ms: number) =>
    fetch('api/session', { cache: 'no-store', signal: AbortSignal.timeout(ms) }),

  // Usuários, só do admin. Criar e o link novo devolvem o token do link, que
  // só existe nessa resposta.
  usuarios: () => request<Usuarios>('usuarios'),
  criarUsuario: (usuario: string, nome: string) =>
    request<Convite>('usuarios', {
      method: 'POST',
      body: JSON.stringify({ username: usuario, name: nome }),
    }),
  novoLink: (usuario: string) =>
    request<Convite>('usuarios/link', {
      method: 'POST',
      body: JSON.stringify({ username: usuario }),
    }),
  removerUsuario: (usuario: string) =>
    request<unknown>('usuarios?username=' + encodeURIComponent(usuario), { method: 'DELETE' }),
  tirarAvatarDe: (usuario: string) =>
    request<unknown>('usuarios/avatar?username=' + encodeURIComponent(usuario), {
      method: 'DELETE',
    }),

  // Minha conta: cada um muda o que é seu. Tudo devolve { user } como ficou,
  // para o header se atualizar sem perguntar de novo. A foto vai crua, o JPEG
  // já reduzido pelo navegador (ver lib/avatar.ts).
  conta: () => request<Conta>('conta'),
  trocarSenha: (atual: string, nova: string) =>
    request<ComPessoa>('conta/senha', {
      method: 'POST',
      body: JSON.stringify({ currentPassword: atual, newPassword: nova }),
    }),
  mudarNome: (nome: string) =>
    request<ComPessoa>('conta/nome', { method: 'POST', body: JSON.stringify({ name: nome }) }),
  enviarAvatar: (jpeg: Blob) =>
    request<ComPessoa>('conta/avatar', {
      method: 'PUT',
      headers: { 'Content-Type': 'image/jpeg' },
      body: jpeg,
    }),
  tirarAvatar: () => request<ComPessoa>('conta/avatar', { method: 'DELETE' }),

  // O link de convite: públicos, com o token no corpo, e não na URL, para ele
  // não ficar no log de nenhum proxy. Link vencido ou já usado é 410.
  conferirConvite: (token: string) =>
    request<LinkDeConvite>('convite/conferir', { method: 'POST', body: JSON.stringify({ token }) }),
  definirSenha: (token: string, senha: string) =>
    request<ComPessoa>('convite', {
      method: 'POST',
      body: JSON.stringify({ token, password: senha }),
    }),

  cameras: () => request<RespostaCameras>('cameras'),
  saveCamera: (cam: Camera) =>
    request<unknown>('cameras', { method: 'POST', body: JSON.stringify(cam) }),
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
  probeStream: (name: string) => request<Sonda>('streams/probe?src=' + encodeURIComponent(name)),

  // Faz o go2rtc reler o go2rtc.yaml. Todas as câmeras param por alguns segundos.
  reiniciarGo2rtc: () => request<unknown>('go2rtc/restart', { method: 'POST' }),

  health: () => request<Saude>('health'),

  // Recomeça a contagem de reconexões de todas as câmeras. Não derruba conexão.
  zerarReconexoes: () => request<unknown>('reconnects/reset', { method: 'POST' }),

  // A máquina que grava: temperatura, memória, pressão, storage, go2rtc e os
  // últimos avisos do log. À parte do health porque custa um teste de escrita
  // no disco - só a pede quem abriu o card "Este servidor".
  servidor: () => request<Servidor>('health/servidor'),

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
    request<Eventos>(`rec/events?cam=${encodeURIComponent(cam)}&day=${day}`),

  // A cauda das marcas, pelo mesmo motivo do timelineRange: no dia de hoje a
  // tela pergunta de novo em ciclo, e só quer o que ainda não tem.
  eventsRange: (cam: string, from: number, to: number) =>
    request<Eventos>(`rec/events?cam=${encodeURIComponent(cam)}&from=${from}&to=${to}`),

  // Uma página das detecções de objeto de todas as câmeras, da mais nova para a
  // mais velha. `antes` e `depois` são o cursor (um ou nenhum); `cams` e
  // `familias` nulos querem dizer "todas".
  deteccoes: ({ antes, depois, limite, cams, familias }: PedidoDeDeteccoes) => {
    const q = new URLSearchParams();
    if (antes != null) q.set('antes', String(antes));
    if (depois != null) q.set('depois', String(depois));
    if (limite) q.set('limite', String(limite));
    if (cams?.length) q.set('cams', cams.join(','));
    if (familias?.length) q.set('familias', familias.join(','));
    return request<PaginaDeDeteccoes>(`deteccoes?${q}`);
  },

  // Os dias com pelo menos uma detecção do filtro, em ordem crescente: é o que
  // o calendário da tela de Detecções deixa escolher.
  diasDeDeteccao: ({ cams, familias }: FiltroDeDeteccoes) => {
    const q = new URLSearchParams();
    if (cams?.length) q.set('cams', cams.join(','));
    if (familias?.length) q.set('familias', familias.join(','));
    return request<DiasDeDeteccao>(`deteccoes/dias?${q}`);
  },
};

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
  quadro: (cam: string, t: number) => `api/deteccoes/quadro?cam=${encodeURIComponent(cam)}&t=${t}`,
  export: (cam: string, from: number, to: number) =>
    `api/rec/export?cam=${encodeURIComponent(cam)}&from=${from}&to=${to}`,
  // O live vai para o go2rtc através do proxy do dwnvr, para que a credencial
  // dele nunca chegue ao navegador.
  liveWS: (cam: string) => `${baseWS()}api/live/ws?src=${encodeURIComponent(cam)}`,
  // O aviso de sessão: um websocket mudo que o servidor fecha quando a pessoa
  // é removida ou ganha link novo. Ver `vigiarSessao` no Ao vivo.
  avisoDeSessaoWS: () => `${baseWS()}api/session/ws`,
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

export type Papel = 'admin' | 'viewer';

// Quem está logado. Com a autenticação desligada vem como admin e dono, sem
// usuário nem nome. `avatar` é o id da foto, e só vem com foto.
export interface Pessoa {
  username?: string;
  name?: string;
  role: Papel;
  owner: boolean;
  avatar?: string;
}

export interface Sessao {
  authRequired: boolean;
  authenticated: boolean;
  user?: Pessoa;
}

// O login, a senha pelo convite e as mudanças da Minha conta.
export interface ComPessoa {
  ok: true;
  user: Pessoa;
}

export interface Versao {
  version: string;
  commit: string;
  date: string;
  go: string;
}

export type SituacaoDoUsuario = 'active' | 'invitePending' | 'inviteExpired';

export interface Usuario {
  username: string;
  name: string;
  role: Papel;
  status: SituacaoDoUsuario;
  avatar?: string;
  inviteExpiresAtMs?: number;
  createdAtMs?: number;
  activeSinceMs?: number;
  // Só de quem já usou o dwnvr depois de o campo existir.
  lastAccessMs?: number;
}

// Sem autenticação ligada não há cadastro: vem sem `owner` e com a lista vazia.
export interface Usuarios {
  authRequired: boolean;
  owner?: { username: string; name: string; avatar: string; lastAccessMs?: number };
  users: Usuario[];
  inviteValidityMs: number;
  maxNameLength: number;
  maxUsernameLength: number;
}

// O token só existe nesta resposta: a tela monta o link com ele.
export interface Convite {
  user: Usuario;
  token: string;
}

export interface LinkDeConvite {
  username: string;
  name: string;
  inviteExpiresAtMs: number;
  minPasswordLength: number;
  maxPasswordLength: number;
}

export interface Conta {
  user: Pessoa;
  minPasswordLength: number;
  maxPasswordLength: number;
  maxNameLength: number;
  avatar: RegraDoAvatar;
}

export interface RegraDoAvatar {
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
}

// A câmera da listagem: para o admin, com a pasta das gravações.
export interface CameraCadastrada extends Camera {
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
export interface Orfa {
  id: string;
  bytes: number;
  days: number;
  firstMs: number;
  lastMs: number;
}

// Sem teto, o `max` não vem.
export interface Faixa {
  min: number;
  max?: number;
}

// Os campos da câmera com faixa: o FaixasDaCamera de internal/config/parametros.go.
export type CampoComFaixa =
  'quotaMB' | 'segmentSeconds' | 'maxDays' | 'stallSeconds' | 'detectSensibilidade';
export type Faixas = Partial<Record<CampoComFaixa, Faixa>>;

// O que está no go2rtc.yaml e o go2rtc ainda não carregou. Lista vazia não vem.
export interface DivergenciasDoGo2rtc {
  novas?: string[];
  alteradas?: string[];
  foraDoArquivo?: string[];
}

// O viewer recebe só `cameras` e `detector`; o resto é do admin, e
// `streams` some também quando o go2rtc não responde.
export interface RespostaCameras {
  cameras: CameraCadastrada[];
  detector: boolean;
  padrao?: Camera;
  faixas?: Faixas;
  orphans?: Orfa[];
  streams?: Stream[];
  go2rtcError?: string;
  go2rtcConfigFile?: DivergenciasDoGo2rtc;
}

// `erro` vem quando a sonda falhou: aí `hasAudio` é "não sei", e não "não tem".
export interface Sonda {
  name: string;
  hasAudio: boolean;
  audioCodecs?: string[];
  probed?: boolean;
  erro?: string;
}

// --- saúde

// As fatias somam o `onsets` da câmera. `pedacos` não é fatia: é quantos onsets
// tiveram o vídeo cortado e oferecido à fila.
export interface Funil {
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
export interface CameraNaSaude {
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
  funil?: Funil;
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

export interface Relogio {
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
// objetos configurado; `go2rtc`, enquanto ele responde. `uptime`, `clock` e
// `reconnectsWindowHours` faltam em servidor anterior a eles, para onde a tela
// do `npm run dev` pode estar apontada.
export interface Saude {
  cameras: CameraNaSaude[];
  disk?: Disco;
  uptime?: Uptime;
  detector?: Detector;
  clock?: Relogio;
  reconnectsWindowHours?: number;
  go2rtc?: { error: string; since: string };
}

// --- o servidor (card "Este servidor" do Diagnóstico)
//
// Cada campo some quando a fonte não existe: kernel sem PSI, sistema sem
// cgroup, fora do Linux.

export interface MediasDePressao {
  avg10: number;
  avg60: number;
  avg300: number;
}

export interface Pressao {
  some?: MediasDePressao;
  full?: MediasDePressao;
}

export type RecursoDaPressao = 'cpu' | 'memoria' | 'io';

export interface ProcessoDoServidor {
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

export interface LinhaDoLog {
  em: string;
  nivel: string;
  texto: string;
}

// `coletadoEm` e `processo` são opcionais porque servidor anterior a eles não
// os manda: a tela do `npm run dev` pode estar apontada para um.
export interface Servidor {
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
  processo?: ProcessoDoServidor;
  memoria?: {
    totalBytes: number;
    disponivelBytes: number;
    swapTotalBytes: number;
    swapUsadaBytes: number;
    limiteBytes?: number;
    oomKills?: number;
  };
  pressao?: Partial<Record<RecursoDaPressao, Pressao>>;
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
  log?: { total: number; linhas: LinhaDoLog[] };
}

// --- gravações

export interface ResumoDoDia {
  day: string;
  count: number;
  bytes: number;
  firstMs: number;
  lastMs: number;
  durMs: number;
}

export interface Dias {
  cam: string;
  days: ResumoDoDia[];
}

// [início, fim] de um trecho contíguo, para desenhar a barra.
export type Trecho = [inicioMs: number, fimMs: number];

// [início, duração, índice em `gens`] de um segmento, para tocar.
export type Segmento = [inicioMs: number, duracaoMs: number, gen: number];

// Compacta de propósito: um dia com segmentos de 1 minuto são 1.440 entradas.
export interface Timeline {
  cam: string;
  from: number;
  to: number;
  ranges: Trecho[];
  gens: string[];
  segments: Segmento[];
}

// X1, Y1, X2, Y2 em fração da imagem, de 0 a 1.
export type Caixa = [x1: number, y1: number, x2: number, y2: number];

// Uma marca de objeto. `quadroMs` e `caixa` dizem onde desenhar o objeto sobre
// o vídeo; marca sem os dois não ganha caixa.
export interface ObjetoMarcado {
  instanteMs: number;
  familia: string;
  classe: string;
  score: number;
  quadroMs?: number;
  caixa?: Caixa;
}

export interface Eventos {
  cam: string;
  from: number;
  to: number;
  onsets: number[];
  objetos: ObjetoMarcado[];
}

// --- detecções

export interface ObjetoNoQuadro {
  familia: string;
  classe: string;
  score: number;
  caixa?: Caixa;
}

// Uma olhada do detector numa câmera: o instante do onset e tudo o que ele
// achou naquele quadro.
export interface Deteccao {
  cam: string;
  instanteMs: number;
  quadroMs?: number;
  temQuadro: boolean;
  objetos: ObjetoNoQuadro[];
}

export interface PaginaDeDeteccoes {
  deteccoes: Deteccao[];
  fim: boolean;
}

// Nulo ou ausente é "todas".
export interface FiltroDeDeteccoes {
  cams?: readonly string[] | null;
  familias?: readonly string[] | null;
}

// `antes` e `depois` são o cursor: um ou nenhum.
export interface PedidoDeDeteccoes extends FiltroDeDeteccoes {
  antes?: number | null;
  depois?: number | null;
  limite?: number;
}

export interface DiasDeDeteccao {
  dias: string[];
}
