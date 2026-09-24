// O que o navegador de quem está olhando é e consegue fazer, para a tela de
// Diagnóstico. Tudo sai do próprio navegador: o servidor não sabe nada disso, e
// é justamente o que falta quando alguém diz "no meu celular ficou estranho".
//
// O critério do que entra é explicar um defeito que a app pode ter - layout,
// vídeo, relógio, armazenamento. Não é inventário do aparelho.

// Os codecs que o player das gravações de fato pede ao MediaSource: é a mesma
// tabela do #mimeFor em player.svelte.js, e o live (video-rtc.js) negocia a
// partir de H.264, H.265 e AAC. Um "não" aqui é a explicação direta do "este
// navegador não reproduz" na tela de Gravações.
const CODECS = [
  { id: 'avc1.640029', rotulo: 'H.264' },
  { id: 'hvc1.1.6.L153.B0', rotulo: 'H.265 (hvc1)' },
  { id: 'hev1.1.6.L153.B0', rotulo: 'H.265 (hev1)' },
  { id: 'mp4a.40.2', rotulo: 'AAC' },
  { id: 'opus', rotulo: 'Opus' },
  { id: 'flac', rotulo: 'FLAC' },
];

// O tamanho de letra que a app pede no body (app.css). A escala do texto é
// medida contra ele.
const FONTE_BASE_PX = 15;

// Navegador e versão a partir do user agent. A ordem importa: Edge, Samsung e
// Opera também dizem "Chrome/" no UA, então o Chrome é o último a ser tentado.
function navegadorDoUA(ua) {
  const tentativas = [
    [/EdgA?\/([\d.]+)/, 'Edge'],
    [/SamsungBrowser\/([\d.]+)/, 'Samsung Internet'],
    [/OPR\/([\d.]+)/, 'Opera'],
    [/Firefox\/([\d.]+)/, 'Firefox'],
    [/Chrome\/([\d.]+)/, 'Chrome'],
    [/Version\/([\d.]+).*Safari/, 'Safari'],
  ];
  for (const [re, nome] of tentativas) {
    const m = ua.match(re);
    if (m) return `${nome} ${m[1]}`;
  }
  return 'desconhecido';
}

function sistemaDoUA(ua) {
  let m = ua.match(/Android ([\d.]+)/);
  if (m) return `Android ${m[1]}`;
  m = ua.match(/(?:iPhone|iPad).*OS ([\d_]+)/);
  if (m) return `iOS ${m[1].replaceAll('_', '.')}`;
  if (/Windows/.test(ua)) return 'Windows';
  if (/Mac OS X/.test(ua)) return 'macOS';
  if (/Linux/.test(ua)) return 'Linux';
  return 'desconhecido';
}

// Onde a página está rodando. O WebView é o navegador embutido de outro app
// (abrir o link de dentro do WhatsApp, por exemplo): ele é outro motor, com
// outra versão, e não o Chrome que a pessoa acha que está usando.
function ondeRoda(ua) {
  if (/; wv\)/.test(ua)) return 'WebView de outro app';
  if (matchMedia('(display-mode: standalone)').matches) return 'app instalado (PWA)';
  return 'aba do navegador';
}

// A escala real do texto, medida e não lida. A fonte grande do Android chega
// ao Chrome de mais de um jeito - ajuste de texto, zoom da página - e o
// getComputedStyle nem sempre enxerga; o tamanho desenhado enxerga todos.
function escalaDoTexto() {
  const s = document.createElement('span');
  s.textContent = 'M';
  s.style.cssText = `position:absolute;visibility:hidden;font-size:${FONTE_BASE_PX}px;line-height:1;padding:0;border:0`;
  document.body.append(s);
  const altura = s.getBoundingClientRect().height;
  s.remove();
  return altura / FONTE_BASE_PX;
}

function suporta(mime) {
  try {
    return (window.MediaSource ?? window.ManagedMediaSource)?.isTypeSupported(mime) ?? false;
  } catch {
    return false;
  }
}

// O localStorage pode existir e jogar ao ser usado: aba anônima de alguns
// navegadores, cota zerada, dado do site bloqueado. Só a escrita prova.
function armazenamentoOk() {
  try {
    const k = 'dwnvr.diag.teste';
    localStorage.setItem(k, '1');
    localStorage.removeItem(k);
    return true;
  } catch {
    return false;
  }
}

// O modelo do aparelho. O user agent do Chrome no Android não o traz mais (diz
// só "Android 10; K"), e o único caminho, o userAgentData, é [SecureContext]:
// num dwnvr servido em http:// ele nem existe. Por isso a resposta distingue
// "não deu porque é http" de "o navegador não informa".
async function modelo() {
  if (!isSecureContext) return 'indisponível (http)';
  const uad = navigator.userAgentData;
  if (!uad?.getHighEntropyValues) return 'o navegador não informa';
  try {
    const v = await uad.getHighEntropyValues(['model']);
    return v.model || 'o navegador não informa';
  } catch {
    return 'o navegador não informa';
  }
}

function sim(v) {
  return v ? 'sim' : 'não';
}

// coletar devolve grupos de itens { rotulo, valor, alerta? }. `alerta` marca o
// valor que já é, sozinho, uma explicação provável de defeito.
export async function coletar() {
  const ua = navigator.userAgent;
  const vv = window.visualViewport;
  const escala = escalaDoTexto();
  const temMSE = 'MediaSource' in window || 'ManagedMediaSource' in window;
  const conexao = navigator.connection;

  const identidade = [
    { rotulo: 'navegador', valor: navegadorDoUA(ua) },
    { rotulo: 'sistema', valor: sistemaDoUA(ua) },
    { rotulo: 'aparelho', valor: await modelo() },
    { rotulo: 'rodando em', valor: ondeRoda(ua), alerta: /; wv\)/.test(ua) },
  ];

  const tela = [
    { rotulo: 'área da página', valor: `${innerWidth} × ${innerHeight}` },
    ...(vv && (Math.round(vv.width) !== innerWidth || Math.round(vv.height) !== innerHeight)
      ? [{ rotulo: 'área visível', valor: `${Math.round(vv.width)} × ${Math.round(vv.height)}` }]
      : []),
    { rotulo: 'densidade de pixels', valor: `${devicePixelRatio}×` },
    { rotulo: 'orientação', valor: screen.orientation?.type ?? (innerWidth > innerHeight ? 'landscape' : 'portrait') },
    // Acima de 115% o layout, calibrado em 15px, começa a quebrar linha onde
    // não quebrava.
    { rotulo: 'escala do texto', valor: `${Math.round(escala * 100)}%`, alerta: escala > 1.15 },
    { rotulo: 'toque', valor: sim(matchMedia('(pointer: coarse)').matches) },
    { rotulo: 'tema do sistema', valor: matchMedia('(prefers-color-scheme: light)').matches ? 'claro' : 'escuro' },
    ...(matchMedia('(prefers-reduced-motion: reduce)').matches
      ? [{ rotulo: 'animações', valor: 'reduzidas' }]
      : []),
  ];

  const video = [
    { rotulo: 'MediaSource', valor: sim(temMSE), alerta: !temMSE },
    { rotulo: 'WebRTC', valor: sim('RTCPeerConnection' in window) },
    ...CODECS.map((c) => {
      const ok = temMSE && suporta(`video/mp4; codecs="${c.id}"`);
      return { rotulo: c.rotulo, valor: sim(ok), alerta: !ok && c.id === 'avc1.640029' };
    }),
  ];

  const armazenamento = armazenamentoOk();
  const ambiente = [
    { rotulo: 'endereço seguro (https)', valor: sim(isSecureContext) },
    { rotulo: 'armazenamento local', valor: armazenamento ? 'ok' : 'bloqueado', alerta: !armazenamento },
    { rotulo: 'fuso', valor: Intl.DateTimeFormat().resolvedOptions().timeZone || '?' },
    { rotulo: 'idioma', valor: navigator.language },
    ...(navigator.deviceMemory ? [{ rotulo: 'memória', valor: `${navigator.deviceMemory} GB` }] : []),
    ...(navigator.hardwareConcurrency ? [{ rotulo: 'núcleos', valor: String(navigator.hardwareConcurrency) }] : []),
    ...(conexao?.effectiveType
      ? [{ rotulo: 'rede', valor: conexao.effectiveType + (conexao.saveData ? ', economia de dados' : '') }]
      : []),
  ];

  return [
    { titulo: 'Identidade', itens: identidade },
    { titulo: 'Tela', itens: tela },
    { titulo: 'Vídeo', itens: video },
    { titulo: 'Ambiente', itens: ambiente },
  ];
}

// comoTexto põe os grupos numa linha por item, para colar numa conversa.
// `cabecalho` vai antes dos grupos e `rodape` depois - o user agent no card do
// navegador, as linhas do log no do servidor.
export function comoTexto(grupos, cabecalho = [], rodape = []) {
  const linhas = [...cabecalho];
  for (const g of grupos) {
    linhas.push('', `[${g.titulo}]`);
    for (const i of g.itens) linhas.push(`${i.rotulo}: ${i.valor}`);
  }
  if (rodape.length) linhas.push('', ...rodape);
  return linhas.join('\n').trim();
}

// copiar põe o texto na área de transferência. A Clipboard API é
// [SecureContext], então em http:// fica o execCommand antigo, que o Chrome
// ainda aceita a partir de um toque. Devolve se conseguiu.
export async function copiar(texto) {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(texto);
      return true;
    } catch {
      // cai para o caminho antigo
    }
  }
  const ta = document.createElement('textarea');
  ta.value = texto;
  ta.setAttribute('readonly', '');
  ta.style.cssText = 'position:fixed;top:0;left:0;opacity:0';
  document.body.append(ta);
  ta.select();
  let ok = false;
  try {
    ok = document.execCommand('copy');
  } catch {
    ok = false;
  }
  ta.remove();
  return ok;
}
