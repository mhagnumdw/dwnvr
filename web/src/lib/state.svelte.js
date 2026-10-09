// Estado global, com runes do Svelte 5.
//
// É pouca coisa de propósito: quase tudo nesta aplicação é estado local de
// tela. O que vive aqui é o que várias telas precisam enxergar - sessão, lista
// de câmeras e saúde - e que seria desperdício buscar de novo a cada navegação.

import { api } from './api.js';
import { AVISOS_POLL_MS } from './avisos.js';

export const session = $state({
  authRequired: false,
  authenticated: false,
  checked: false,
  // Quem está logado: { usuario, nome, papel, dono }. Com a autenticação
  // desligada vem como admin e dono, sem usuário. Nulo sem sessão.
  pessoa: null,
});

// ehAdmin decide as abas, o sino e a pílula. Esconder não é segurança - a API
// recusa o comum com 403 -, é só não mostrar o que ele não pode usar.
export function ehAdmin() {
  return session.pessoa?.papel === 'admin';
}

// sessaoCaiu leva à tela de login: a sessão venceu, a pessoa foi removida ou
// ganhou link novo. Quem descobre é um 401 da API, ou a conferência do Ao vivo.
export function sessaoCaiu() {
  session.authenticated = false;
  session.pessoa = null;
}

export const cameras = $state({
  list: [],
  streams: [],
  // Gravações que sobraram de câmeras já removidas. Vêm junto com a listagem
  // porque nenhum outro endpoint enxerga câmera sem cadastro.
  orphans: [],
  // Câmera vazia com os defaults do servidor, de onde nasce o cadastro novo.
  padrao: null,
  // { campo: { min, max } } dos campos numéricos: a mesma régua com que a API
  // recusa, para o min/max dos inputs não repetir número à mão. Sem teto, o
  // max não vem.
  faixas: null,
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
  // Horas da janela de reconexões recentes; o número mora no servidor
  // (recorder.JanelaDeReconexoes). Nulo contra servidor antigo.
  reconnectsWindowHours: null,
  // { error, since } enquanto o go2rtc não responde ao dwnvr; nulo quando ele
  // responde, e também contra servidor antigo.
  go2rtc: null,
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
export const REPO_URL = 'https://github.com/mhagnumdw/dwnvr';
export const RELEASES_URL = `${REPO_URL}/releases`;
export const ISSUES_URL = `${REPO_URL}/issues`;
// A resposta fica guardada por este tempo, inclusive o "ainda não há release":
// sem isso, cada aba aberta gastaria uma requisição.
const RELEASE_CACHE = 'dwnvr.release';
const RELEASE_CACHE_MS = 12 * 60 * 60 * 1000;

export async function checkSession() {
  try {
    const s = await api.session();
    session.authRequired = s.authRequired;
    session.authenticated = s.authenticated;
    session.pessoa = s.pessoa ?? null;
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
  session.pessoa = null;
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
  healthPedidoEm = 0;
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
    cameras.faixas = data.faixas ?? null;
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

// Quando a última leitura da saúde começou, de qualquer tela. É o que o sino
// do header consulta para não repetir uma leitura recente, ou uma em curso.
let healthPedidoEm = 0;

export async function loadHealth() {
  healthPedidoEm = Date.now();
  try {
    const data = await api.health();
    health.cameras = data.cameras ?? [];
    health.disk = data.disk ?? null;
    health.uptime = data.uptime ?? null;
    health.clock = data.clock ?? null;
    health.detector = data.detector ?? null;
    health.reconnectsWindowHours = data.reconnectsWindowHours ?? null;
    health.go2rtc = data.go2rtc ?? null;
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

// vigiarAvisos mantém o sino do header em dia nas telas que não releem a saúde
// por conta própria. Só lê quando a última leitura, de quem quer que seja, tem
// mais de AVISOS_POLL_MS: em Diagnóstico e Câmeras, que releem a cada
// HEALTH_POLL_MS, não sai requisição nenhuma daqui. O tique é curto para que,
// saindo de uma dessas telas, o dado não fique quase o dobro do prazo sem ser
// relido; o tique em si não custa nada. Com a aba oculta não lê, e ao voltar
// lê na hora se o dado estiver velho.
export function vigiarAvisos() {
  const talvez = () => {
    if (!document.hidden && Date.now() - healthPedidoEm >= AVISOS_POLL_MS) loadHealth();
  };
  talvez();
  const id = setInterval(talvez, HEALTH_POLL_MS);
  document.addEventListener('visibilitychange', talvez);
  return () => {
    clearInterval(id);
    document.removeEventListener('visibilitychange', talvez);
  };
}
