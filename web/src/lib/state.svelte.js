// Estado global, com runes do Svelte 5.
//
// É pouca coisa de propósito: quase tudo nesta aplicação é estado local de
// tela. O que vive aqui é o que várias telas precisam enxergar — sessão, lista
// de câmeras e saúde — e que seria desperdício buscar de novo a cada navegação.

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
  go2rtcError: null,
  loading: true,
  error: null,
});

export const health = $state({
  cameras: [],
  disk: null,
  // { appSeconds, machineSeconds }. Fica nulo contra servidor antigo, que não
  // responde o campo — a faixa de estado simplesmente não aparece.
  uptime: null,
  updatedAt: 0,
});

// Qual código o servidor está rodando. Não muda enquanto a página está aberta,
// então é buscado uma vez só, no boot.
export const build = $state({
  version: '',
  commit: '',
  date: '',
});

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
    // aqui ainda é o que o usuário pediu — e o cookie assinado expira sozinho.
  }
  session.authenticated = false;
  // Zera o que já foi carregado: sem isto, quem entrar em seguida vê por um
  // instante as câmeras e o diagnóstico da sessão anterior.
  cameras.list = [];
  cameras.streams = [];
  cameras.orphans = [];
  cameras.go2rtcError = null;
  cameras.loading = true;
  cameras.error = null;
  health.cameras = [];
  health.disk = null;
  health.uptime = null;
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
    // usável — a interface apenas não mostra nada.
  }
}

export async function loadCameras() {
  cameras.loading = true;
  cameras.error = null;
  try {
    const data = await api.cameras();
    cameras.list = data.cameras ?? [];
    cameras.streams = data.streams ?? [];
    cameras.orphans = data.orphans ?? [];
    cameras.go2rtcError = data.go2rtcError ?? null;
  } catch (e) {
    cameras.error = e.message;
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
    health.updatedAt = Date.now();
  } catch {
    // Saúde é informativo: falhar aqui não pode interromper o uso das telas.
  }
}

// pollHealth mantém o diagnóstico vivo enquanto a tela estiver aberta, e para
// quando ela sai — não faz sentido consultar o Pi de fundo para sempre.
export function pollHealth(intervalMs = 5000) {
  loadHealth();
  const id = setInterval(loadHealth, intervalMs);
  return () => clearInterval(id);
}
