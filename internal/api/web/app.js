// Tela de gravações do dwnvr.

const $ = id => document.getElementById(id);
const log = m => { $('log').textContent = (m + '\n' + $('log').textContent).slice(0, 2000); };

const state = {
  cam: null,
  day: null,
  ranges: [],
  segments: [],
  gens: [],
  // Janela visível da timeline, em ms. Começa no dia inteiro.
  viewFrom: 0,
  viewTo: 0,
};

const player = new Player($('video'), log);

async function api(path, opts) {
  const r = await fetch('api/' + path, opts);
  if (r.status === 401) { await login(); return api(path, opts); }
  if (!r.ok) throw new Error((await r.json().catch(() => ({}))).error || r.statusText);
  return r.json();
}

// --- sessão -----------------------------------------------------------------

function login() {
  return new Promise(resolve => {
    const dlg = $('login');
    dlg.showModal();
    $('dologin').onclick = async ev => {
      ev.preventDefault();
      const r = await fetch('api/login', {
        method: 'POST',
        body: JSON.stringify({ username: $('user').value, password: $('pass').value }),
      });
      if (r.ok) { dlg.close(); resolve(); }
      else $('loginerr').textContent = 'usuário ou senha inválidos';
    };
  });
}

// --- carga ------------------------------------------------------------------

async function boot() {
  const s = await (await fetch('api/session')).json();
  if (s.authRequired && !s.authenticated) await login();

  const { cameras } = await api('cameras');
  $('cam').innerHTML = cameras
    .map(c => `<option value="${c.id}">${c.name}</option>`).join('');
  if (!cameras.length) { log('nenhuma câmera cadastrada'); return; }

  $('day').value = new Date().toISOString().slice(0, 10);
  $('cam').onchange = loadDay;
  $('day').onchange = loadDay;
  await loadDay();
}

async function loadDay() {
  state.cam = $('cam').value;
  state.day = $('day').value;

  const t = await api(`rec/timeline?cam=${encodeURIComponent(state.cam)}&day=${state.day}`);
  state.ranges = t.ranges;
  state.segments = t.segments;
  state.gens = t.gens;
  state.viewFrom = t.from;
  state.viewTo = t.to;

  player.setSource(state.cam, state.gens, state.segments);

  const total = t.ranges.reduce((a, [s, e]) => a + (e - s), 0);
  $('info').textContent =
    `${t.segments.length} segmentos · ${(total / 3600000).toFixed(1)}h gravadas · ${t.ranges.length} faixa(s)`;
  draw();
}

// --- timeline ---------------------------------------------------------------

const cv = $('timeline');

function draw() {
  const dpr = window.devicePixelRatio || 1;
  const w = cv.clientWidth, h = cv.clientHeight;
  cv.width = w * dpr; cv.height = h * dpr;
  const g = cv.getContext('2d');
  g.scale(dpr, dpr);

  const span = state.viewTo - state.viewFrom;
  const x = ms => (ms - state.viewFrom) / span * w;

  g.fillStyle = '#1b1f24';
  g.fillRect(0, 0, w, h);

  // Faixas com gravação.
  g.fillStyle = '#2f81f7';
  for (const [s, e] of state.ranges) {
    const x0 = Math.max(0, x(s)), x1 = Math.min(w, x(e));
    if (x1 > x0) g.fillRect(x0, 8, Math.max(1, x1 - x0), h - 26);
  }

  // Marcas de hora, ralas o bastante para não virar sopa quando há zoom.
  g.fillStyle = '#8b949e';
  g.font = '10px system-ui, sans-serif';
  const stepMs = niceStep(span, w);
  const first = Math.ceil(state.viewFrom / stepMs) * stepMs;
  for (let t = first; t < state.viewTo; t += stepMs) {
    const px = x(t);
    g.fillRect(px, h - 14, 1, 5);
    g.fillText(fmtTime(t, stepMs), px + 3, h - 4);
  }

  // Posição atual.
  if (player.base) {
    const px = x(player.wallClock());
    if (px >= 0 && px <= w) {
      g.fillStyle = '#f85149';
      g.fillRect(px - 1, 4, 2, h - 18);
    }
  }
}

function niceStep(span, w) {
  const steps = [1e3, 5e3, 15e3, 60e3, 5 * 60e3, 15 * 60e3, 3600e3, 3 * 3600e3, 6 * 3600e3];
  const minPx = 70;
  return steps.find(s => s / span * w >= minPx) || steps[steps.length - 1];
}

function fmtTime(ms, step) {
  const d = new Date(ms);
  const p = n => String(n).padStart(2, '0');
  return step >= 60e3 ? `${p(d.getHours())}:${p(d.getMinutes())}`
                      : `${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

cv.addEventListener('click', ev => {
  const rect = cv.getBoundingClientRect();
  const frac = (ev.clientX - rect.left) / rect.width;
  const ms = state.viewFrom + frac * (state.viewTo - state.viewFrom);
  log('pulando para ' + new Date(ms).toLocaleTimeString());
  player.playFrom(ms);
});

// Zoom centrado no cursor, como um mapa.
cv.addEventListener('wheel', ev => {
  ev.preventDefault();
  const rect = cv.getBoundingClientRect();
  const frac = (ev.clientX - rect.left) / rect.width;
  const span = state.viewTo - state.viewFrom;
  const anchor = state.viewFrom + frac * span;
  const factor = ev.deltaY > 0 ? 1.3 : 1 / 1.3;
  const next = Math.min(86400000, Math.max(30000, span * factor));
  state.viewFrom = anchor - frac * next;
  state.viewTo = state.viewFrom + next;
  draw();
}, { passive: false });

window.addEventListener('resize', draw);

// --- controles --------------------------------------------------------------

$('play').onclick = () => {
  if (player.video.paused) {
    if (!player.base && state.segments.length) player.playFrom(state.segments[0][0]);
    else player.video.play();
    $('play').textContent = '⏸';
  } else {
    player.video.pause();
    $('play').textContent = '▶';
  }
};

$('export').onclick = () => {
  const from = Math.round(player.base ? player.wallClock() : state.viewFrom);
  const to = from + 5 * 60 * 1000;
  location.href = `api/rec/export?cam=${encodeURIComponent(state.cam)}&from=${from}&to=${to}`;
};

setInterval(() => {
  if (!player.base) return;
  $('clock').textContent = new Date(player.wallClock()).toLocaleTimeString();
  draw();
}, 250);

boot().catch(e => log('erro: ' + e.message));
