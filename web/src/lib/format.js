// Formatadores compartilhados. Ficam num lugar só para que a mesma grandeza
// não apareça escrita de dois jeitos em telas diferentes.

const pad = (n) => String(n).padStart(2, '0');

export function hhmmss(ms) {
  const d = new Date(ms);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

export function hhmm(ms) {
  const d = new Date(ms);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function dayKey(date = new Date()) {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

export function parseDay(key) {
  const [y, m, d] = key.split('-').map(Number);
  return new Date(y, m - 1, d);
}

export function bytes(n) {
  if (!n) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), u.length - 1);
  return `${(n / 1024 ** i).toFixed(i === 0 ? 0 : 1)} ${u[i]}`;
}

export function kbps(v) {
  if (!v) return '—';
  return v >= 1000 ? `${(v / 1000).toFixed(2)} Mbps` : `${Math.round(v)} kbps`;
}

// duracao formata um intervalo pensando em quem lê: dias e horas importam mais
// que precisão de segundos quando se fala de retenção.
export function duracao(ms) {
  if (!ms || ms < 0) return '—';
  const s = ms / 1000;
  if (s < 60) return `${Math.round(s)}s`;
  if (s < 3600) return `${Math.round(s / 60)}min`;
  if (s < 86400) return `${(s / 3600).toFixed(1)}h`;
  return `${(s / 86400).toFixed(1)} dias`;
}

// dias converte a estimativa de retenção em algo compreensível. "20 GB" não
// diz nada a ninguém; "≈ 2,4 dias" diz tudo.
export function dias(n) {
  if (!n || n <= 0) return '—';
  // Cotas pequenas (ou taxas altas) dão frações de hora; arredondar tudo para
  // horas transformaria isso em "0h", que não informa nada.
  if (n < 1 / 24) return `${Math.max(1, Math.round(n * 1440))}min`;
  if (n < 1) return `${(n * 24).toFixed(1)}h`;
  if (n < 10) return `${n.toFixed(1)} dias`;
  return `${Math.round(n)} dias`;
}
