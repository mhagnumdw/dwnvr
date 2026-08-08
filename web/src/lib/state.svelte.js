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
  go2rtcError: null,
  loading: true,
  error: null,
});

export const health = $state({
  cameras: [],
  disk: null,
  updatedAt: 0,
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

export async function loadCameras() {
  cameras.loading = true;
  cameras.error = null;
  try {
    const data = await api.cameras();
    cameras.list = data.cameras ?? [];
    cameras.streams = data.streams ?? [];
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
