// Cliente da API do dwnvr.
//
// Tudo passa por aqui para que um 401 tenha um único lugar de tratamento: a
// sessão expira em algum momento, e sem isso cada tela precisaria lembrar de
// lidar com isso por conta própria.

let onUnauthorized = () => {};

export function setUnauthorizedHandler(fn) {
  onUnauthorized = fn;
}

async function request(path, opts = {}) {
  const res = await fetch('api/' + path, opts);

  if (res.status === 401) {
    onUnauthorized();
    throw new ApiError('sessão expirada', 401);
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new ApiError(body.error || res.statusText, res.status);
  }
  return res.json();
}

export class ApiError extends Error {
  constructor(message, status) {
    super(message);
    this.status = status;
  }
}

export const api = {
  session: () => fetch('api/session').then((r) => r.json()),

  // Como session, não passa pelo request(): é público e precisa funcionar na
  // tela de login, onde ainda não há sessão para expirar.
  version: () => fetch('api/version').then((r) => r.json()),

  login: async (username, password) => {
    const res = await fetch('api/login', {
      method: 'POST',
      body: JSON.stringify({ username, password }),
    });
    if (!res.ok) throw new ApiError('usuário ou senha inválidos', res.status);
    return res.json();
  },

  logout: () => fetch('api/logout', { method: 'POST' }),

  cameras: () => request('cameras'),
  saveCamera: (cam) =>
    request('cameras', { method: 'POST', body: JSON.stringify(cam) }),
  // As gravações só vão junto se forem pedidas: o padrão é preservá-las.
  deleteCamera: (id, { recordings = false } = {}) =>
    request(
      `cameras?id=${encodeURIComponent(id)}${recordings ? '&recordings=1' : ''}`,
      { method: 'DELETE' },
    ),

  // Serve tanto câmera cadastrada quanto câmera já removida — no segundo caso é
  // o único jeito de alcançar o material, que some do resto da API.
  deleteRecordings: (cam) =>
    request('rec?cam=' + encodeURIComponent(cam), { method: 'DELETE' }),

  health: () => request('health'),

  days: (cam, from, to) =>
    request(`rec/days?cam=${encodeURIComponent(cam)}&from=${from}&to=${to}`),

  timeline: (cam, day) =>
    request(`rec/timeline?cam=${encodeURIComponent(cam)}&day=${day}`),
};

// URLs de mídia são montadas, não buscadas: vão direto num <video>, num <img>
// ou no SourceBuffer.
export const mediaURL = {
  init: (cam, gen) => `api/rec/init?cam=${encodeURIComponent(cam)}&g=${gen}`,
  segment: (cam, t) => `api/rec/seg?cam=${encodeURIComponent(cam)}&t=${t}`,
  thumb: (cam, t) => `api/rec/thumb?cam=${encodeURIComponent(cam)}&t=${t}`,
  export: (cam, from, to) =>
    `api/rec/export?cam=${encodeURIComponent(cam)}&from=${from}&to=${to}`,
  // O live vai para o go2rtc através do proxy do dwnvr, para que a credencial
  // dele nunca chegue ao navegador.
  liveWS: (cam) => {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const base = location.pathname.replace(/[^/]*$/, '');
    return `${proto}//${location.host}${base}api/live/ws?src=${encodeURIComponent(cam)}`;
  },
};
