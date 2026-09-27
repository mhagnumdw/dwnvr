// Estado global, com runes do Svelte 5.
//
// É pouca coisa de propósito: quase tudo nesta aplicação é estado local de
// tela. O que vive aqui é o que várias telas precisam enxergar - sessão, lista
// de câmeras e saúde - e que seria desperdício buscar de novo a cada navegação.

import { api } from './api.js';

export const session = $state({
  authRequired: false,
  authenticated: false,
  checked: false,
});

export const cameras = $state({
  list: [],
  streams: [],
  // Gravações que sobraram de câmeras já removidas. Vêm junto com a listagem
  // porque nenhum outro endpoint enxerga câmera sem cadastro.
  orphans: [],
  // Câmera vazia com os defaults do servidor, de onde nasce o cadastro novo.
  padrao: null,
  // Se o detector de objetos está configurado. Sem ele não há aba Detecções.
  // Nulo até a primeira resposta: "ainda não sei" é diferente de "não tem".
  detector: null,
  go2rtcError: null,
  // O que está no go2rtc.yaml e o go2rtc ainda não carregou: { novas,
  // alteradas, foraDoArquivo }. Nulo quando os dois batem, ou quando o go2rtc
  // não deixa ler o arquivo.
  go2rtcConfigFile: null,
  loading: true,
  error: null,
});

export const health = $state({
  cameras: [],
  disk: null,
  // { appSeconds, machineSeconds }. Fica nulo contra servidor antigo, que não
  // responde o campo - a faixa de estado simplesmente não aparece.
  uptime: null,
  clock: null,
  // { fila: { agora, cap, pico }, tempos: { analiseMs, esperaMs } }. Nulo sem
  // detector de objetos configurado.
  detector: null,
  updatedAt: 0,
});

// Qual código o servidor está rodando. Não muda enquanto a página está aberta,
// então é buscado uma vez só, no boot.
export const build = $state({
  version: '',
  commit: '',
  date: '',
  // A tag da última release, quando ela é mais nova que a versão em uso; vazio
  // em qualquer outro caso. É o que acende o aviso no header.
  nova: '',
});

// Aviso de versão nova. Quem pergunta é o navegador, direto à API do GitHub
// (CORS aberto, 60 requisições por hora por IP): o servidor não abre conexão
// para fora, não ganha goroutine nem config, e numa rede sem internet a
// consulta só falha em silêncio.
const RELEASE_API = 'https://api.github.com/repos/mhagnumdw/dwnvr/releases/latest';
export const RELEASES_URL = 'https://github.com/mhagnumdw/dwnvr/releases';
// A resposta fica guardada por este tempo, inclusive o "ainda não há release":
// sem isso, cada aba aberta gastaria uma requisição.
const RELEASE_CACHE = 'dwnvr.release';
const RELEASE_CACHE_MS = 12 * 60 * 60 * 1000;

export async function checkSession() {
  try {
    const s = await api.session();
    session.authRequired = s.authRequired;
    session.authenticated = s.authenticated;
  } catch {
    session.authenticated = false;
  }
  session.checked = true;
}

export async function logout() {
  try {
    await api.logout();
  } catch {
    // Sair é, antes de tudo, local: se a requisição falhar, derrubar a sessão
    // aqui ainda é o que o usuário pediu - e o cookie assinado expira sozinho.
  }
  session.authenticated = false;
  // Zera o que já foi carregado: sem isto, quem entrar em seguida vê por um
  // instante as câmeras e o diagnóstico da sessão anterior.
  cameras.list = [];
  cameras.streams = [];
  cameras.orphans = [];
  cameras.detector = null;
  cameras.go2rtcError = null;
  cameras.go2rtcConfigFile = null;
  cameras.loading = true;
  cameras.error = null;
  health.cameras = [];
  health.disk = null;
  health.uptime = null;
  health.clock = null;
  health.detector = null;
  health.updatedAt = 0;
}

export async function loadBuild() {
  try {
    const b = await api.version();
    build.version = b.version ?? '';
    build.commit = b.commit ?? '';
    build.date = b.date ?? '';
  } catch {
    // Versão é informativa: um servidor antigo, sem o endpoint, continua
    // usável - a interface apenas não mostra nada.
  }
}

// [maior, menor, patch] de 'v0.1.0', e também de 'v0.1.0-3-gabc1234', que é
// um build da main depois da v0.1.0. Nulo para 'dev' ou um hash solto.
function semver(v) {
  const m = /^v?(\d+)\.(\d+)\.(\d+)/.exec(v ?? '');
  return m ? m.slice(1).map(Number) : null;
}

// Comparação numérica: como texto, 'v0.1.10' viria antes de 'v0.1.9'.
function maisNova(a, b) {
  for (let i = 0; i < 3; i++) if (a[i] !== b[i]) return a[i] > b[i];
  return false;
}

// A tag guardada, ou undefined se não há nada válido dentro do prazo.
function lerCacheRelease() {
  try {
    const c = JSON.parse(localStorage.getItem(RELEASE_CACHE));
    if (c && typeof c.tag === 'string' && Date.now() - c.em < RELEASE_CACHE_MS) return c.tag;
  } catch {
    // localStorage bloqueado ou conteúdo estragado: vale como vazio.
  }
  return undefined;
}

function gravarCacheRelease(tag) {
  try {
    localStorage.setItem(RELEASE_CACHE, JSON.stringify({ tag, em: Date.now() }));
  } catch {
    // Sem onde guardar, a próxima abertura pergunta de novo. Só isso.
  }
}

// Depende de loadBuild(): sem saber a versão em uso não há o que comparar.
// Build de desenvolvimento ou só com hash não consulta nada.
export async function checarNovaVersao() {
  const atual = semver(build.version);
  if (!atual) return;
  let tag = lerCacheRelease();
  if (tag === undefined) {
    try {
      const r = await fetch(RELEASE_API);
      // 404 é "ainda não há release": resposta válida, que vai para o cache.
      if (r.ok) tag = (await r.json()).tag_name ?? '';
      else if (r.status === 404) tag = '';
    } catch {
      // Sem internet, ou o GitHub fora: fica para a próxima abertura.
    }
    // Limite de requisições estourado e afins também ficam sem cache.
    if (tag === undefined) return;
    gravarCacheRelease(tag);
  }
  const ultima = semver(tag);
  if (ultima && maisNova(ultima, atual)) build.nova = tag;
}

export async function loadCameras() {
  cameras.loading = true;
  cameras.error = null;
  try {
    const data = await api.cameras();
    cameras.list = data.cameras ?? [];
    cameras.streams = data.streams ?? [];
    cameras.orphans = data.orphans ?? [];
    cameras.padrao = data.padrao ?? null;
    cameras.detector = data.detector === true;
    cameras.go2rtcError = data.go2rtcError ?? null;
    cameras.go2rtcConfigFile = data.go2rtcConfigFile ?? null;
  } catch (e) {
    cameras.error = e.message;
    // Sem resposta nenhuma, a aba fica de fora: melhor que a tela em branco
    // esperando para sempre.
    cameras.detector ??= false;
  } finally {
    cameras.loading = false;
  }
}

export async function loadHealth() {
  try {
    const data = await api.health();
    health.cameras = data.cameras ?? [];
    health.disk = data.disk ?? null;
    health.uptime = data.uptime ?? null;
    health.clock = data.clock ?? null;
    health.detector = data.detector ?? null;
    health.updatedAt = Date.now();
  } catch {
    // Saúde é informativo: falhar aqui não pode interromper o uso das telas.
  }
}

// HEALTH_POLL_MS é o único lugar que decide de quanto em quanto tempo a saúde
// é relida. Todas as telas que mostram status usam o mesmo ritmo, e o texto que
// anuncia isso ao usuário é derivado daqui - mudar o número aqui basta.
export const HEALTH_POLL_MS = 5000;

// pollHealth mantém o diagnóstico vivo enquanto a tela estiver aberta, e para
// quando ela sai - não faz sentido consultar o servidor de fundo para sempre.
export function pollHealth(intervalMs = HEALTH_POLL_MS) {
  loadHealth();
  const id = setInterval(loadHealth, intervalMs);
  return () => clearInterval(id);
}
