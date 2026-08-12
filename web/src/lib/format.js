// Formatadores compartilhados. Ficam num lugar só para que a mesma grandeza
// não apareça escrita de dois jeitos em telas diferentes.

const pad = (n) => String(n).padStart(2, '0');

// num escreve número no padrão daqui: vírgula decimal e sem zero à direita
// ("20 GB", não "20,00 GB"). Instanciar Intl a cada chamada é caro e estes
// formatadores aparecem em tela que atualiza de 3 em 3 segundos, então cacheia.
const nfs = new Map();
function num(v, casas) {
  let f = nfs.get(casas);
  if (!f) nfs.set(casas, (f = new Intl.NumberFormat('pt-BR', { maximumFractionDigits: casas })));
  return f.format(v);
}

export function hhmmss(ms) {
  const d = new Date(ms);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

export function hhmm(ms) {
  const d = new Date(ms);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// ddmm é a data curta dos intervalos ("14/07 a 11/08"). O ano fica de fora
// porque os intervalos que aparecem na interface são de dias ou semanas, e
// repeti-lo em toda ponta só ocuparia espaço.
export function ddmm(ms) {
  const d = new Date(ms);
  return `${pad(d.getDate())}/${pad(d.getMonth() + 1)}`;
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
  const v = n / 1024 ** i;
  // Passando de 100 na unidade a casa decimal só polui: "761 MB" basta.
  return `${num(v, i === 0 || v >= 100 ? 0 : 2)} ${u[i]}`;
}

// bytesDeMB existe porque cota e mínimo livre são configurados em MB. Mostrar o
// número cru deixava "20480 MB" na tela ao lado de um "3,62 GB" - mesma
// grandeza, duas unidades, que é justamente o que este arquivo evita.
export function bytesDeMB(mb) {
  return bytes((mb || 0) * 1024 ** 2);
}

export function kbps(v) {
  if (!v) return '-';
  return v >= 1000 ? `${num(v / 1000, 2)} Mbps` : `${num(Math.round(v), 0)} kbps`;
}

// resolucao mostra o que está sendo gravado de fato, lido do init do stream.
// Só existe depois da primeira conexão - antes disso não há o que afirmar.
export function resolucao(w, h) {
  if (!w || !h) return '-';
  return `${w}×${h}`;
}

// duracao formata um intervalo pensando em quem lê: sempre duas casas de
// grandeza, a maior e a seguinte.
//
// "7h 17min" e não "7,3h" porque a fração decimal de hora obriga quem lê a
// converter de cabeça - e essas durações são lidas no meio de um diagnóstico,
// onde ninguém quer fazer conta. A unidade menor some quando é zero, para não
// deixar "7h 0min" na tela.
export function duracao(ms) {
  if (!ms || ms < 0) return '-';
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;

  // O arredondamento acontece uma vez só, no minuto, e o resto é montado a
  // partir dele: arredondar hora e minuto em separado é o que produz "7h 60min".
  const totalMin = Math.round(s / 60);
  if (totalMin < 60) return `${totalMin}min`;

  const totalH = Math.floor(totalMin / 60);
  const min = totalMin % 60;
  if (totalH < 24) return min ? `${totalH}h ${min}min` : `${totalH}h`;

  const d = Math.floor(totalH / 24);
  const h = totalH % 24;
  return `${d} ${d === 1 ? 'dia' : 'dias'}${h ? ` ${h}h` : ''}`;
}

// dias converte a estimativa de retenção em algo compreensível. "20 GB" não
// diz nada a ninguém; "≈ 2,4 dias" diz tudo.
export function dias(n) {
  if (!n || n <= 0) return '-';
  // Cotas pequenas (ou taxas altas) dão frações de hora; arredondar tudo para
  // horas transformaria isso em "0h", que não informa nada.
  if (n < 1 / 24) return `${Math.max(1, Math.round(n * 1440))}min`;
  if (n < 1) return `${num(n * 24, 1)}h`;
  if (n < 10) return `${num(n, 1)} dias`;
  return `${Math.round(n)} dias`;
}
