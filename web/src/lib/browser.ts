// O que o navegador de quem está olhando é e consegue fazer, para a tela de
// Diagnóstico. Tudo sai do próprio navegador: o servidor não sabe nada disso, e
// é justamente o que falta quando alguém diz "no meu celular ficou estranho".
//
// O critério do que entra é explicar um defeito que a app pode ter - layout,
// vídeo, relógio, armazenamento. Não é inventário do aparelho.

// O que só alguns navegadores têm, e que o lib.dom do TypeScript ainda não
// declara. Por isso tudo opcional: a pergunta aqui é justamente se existe.
declare global {
  interface Navigator {
    readonly userAgentData?: {
      getHighEntropyValues?: (hints: string[]) => Promise<{ model?: string }>;
    };
    readonly connection?: { effectiveType?: string; saveData?: boolean };
    readonly deviceMemory?: number;
  }
  interface Window {
    // O MediaSource do Safari no iPhone, que não tem o MediaSource comum.
    readonly ManagedMediaSource?: typeof MediaSource;
  }
}

// Um item dos cards de diagnóstico, o do navegador e o do servidor. `alerta`
// marca o valor que já é, sozinho, uma explicação provável de defeito.
export interface CardItem {
  label: string;
  value: string;
  alert?: boolean;
}

export interface CardGroup {
  title: string;
  items: CardItem[];
}

// Os codecs que o player das gravações de fato pede ao MediaSource: é a mesma
// tabela do #mimeFor em player.svelte.ts, e o live (video-rtc.js) negocia a
// partir de H.264, H.265 e AAC. Um "não" aqui é a explicação direta do "este
// navegador não reproduz" na tela de Gravações.
const CODECS = [
  { id: 'avc1.640029', label: 'H.264' },
  { id: 'hvc1.1.6.L153.B0', label: 'H.265 (hvc1)' },
  { id: 'hev1.1.6.L153.B0', label: 'H.265 (hev1)' },
  { id: 'mp4a.40.2', label: 'AAC' },
  { id: 'opus', label: 'Opus' },
  { id: 'flac', label: 'FLAC' },
];

// O tamanho de letra que a app pede no body (app.css). A escala do texto é
// medida contra ele.
const BASE_FONT_PX = 15;

// Navegador e versão a partir do user agent. A ordem importa: Edge, Samsung e
// Opera também dizem "Chrome/" no UA, então o Chrome é o último a ser tentado.
function uaBrowser(ua: string) {
  const attempts: [RegExp, string][] = [
    [/EdgA?\/([\d.]+)/, 'Edge'],
    [/SamsungBrowser\/([\d.]+)/, 'Samsung Internet'],
    [/OPR\/([\d.]+)/, 'Opera'],
    [/Firefox\/([\d.]+)/, 'Firefox'],
    [/Chrome\/([\d.]+)/, 'Chrome'],
    [/Version\/([\d.]+).*Safari/, 'Safari'],
  ];
  for (const [re, name] of attempts) {
    const m = re.exec(ua);
    if (m?.[1]) return `${name} ${m[1]}`;
  }
  return 'desconhecido';
}

function uaSystem(ua: string) {
  let m = /Android ([\d.]+)/.exec(ua);
  if (m?.[1]) return `Android ${m[1]}`;
  m = /(?:iPhone|iPad).*OS ([\d_]+)/.exec(ua);
  if (m?.[1]) return `iOS ${m[1].replaceAll('_', '.')}`;
  if (ua.includes('Windows')) return 'Windows';
  if (ua.includes('Mac OS X')) return 'macOS';
  if (ua.includes('Linux')) return 'Linux';
  return 'desconhecido';
}

// appInstalado diz se a página roda como o app instalado (PWA), na janela
// própria, sem barra de endereços.
export function appInstalled() {
  return matchMedia('(display-mode: standalone)').matches;
}

// Onde a página está rodando. O WebView é o navegador embutido de outro app
// (abrir o link de dentro do WhatsApp, por exemplo): ele é outro motor, com
// outra versão, e não o Chrome que a pessoa acha que está usando.
function whereRuns(ua: string) {
  if (ua.includes('; wv)')) return 'WebView de outro app';
  if (appInstalled()) return 'app instalado (PWA)';
  return 'aba do navegador';
}

// A escala real do texto, medida e não lida. A fonte grande do Android chega
// ao Chrome de mais de um jeito - ajuste de texto, zoom da página - e o
// getComputedStyle nem sempre enxerga; o tamanho desenhado enxerga todos.
function textScale() {
  const s = document.createElement('span');
  s.textContent = 'M';
  s.style.cssText = `position:absolute;visibility:hidden;font-size:${BASE_FONT_PX}px;line-height:1;padding:0;border:0`;
  document.body.append(s);
  const height = s.getBoundingClientRect().height;
  s.remove();
  return height / BASE_FONT_PX;
}

function supports(mime: string) {
  try {
    const mse = typeof MediaSource === 'undefined' ? window.ManagedMediaSource : MediaSource;
    return mse?.isTypeSupported(mime) ?? false;
  } catch {
    return false;
  }
}

// O localStorage pode existir e jogar ao ser usado: aba anônima de alguns
// navegadores, cota zerada, dado do site bloqueado. Só a escrita prova.
function storageOk() {
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
async function model() {
  if (!isSecureContext) return 'indisponível (http)';
  const uad = navigator.userAgentData;
  if (!uad?.getHighEntropyValues) return 'o navegador não informa';
  try {
    const v = await uad.getHighEntropyValues(['model']);
    // O desktop responde o modelo vazio, e não ausente.
    // eslint-disable-next-line @typescript-eslint/prefer-nullish-coalescing -- o vazio também cai no texto
    return v.model || 'o navegador não informa';
  } catch {
    return 'o navegador não informa';
  }
}

function sim(v: boolean) {
  return v ? 'sim' : 'não';
}

// coletar devolve grupos de itens { rotulo, valor, alerta? }. `alerta` marca o
// valor que já é, sozinho, uma explicação provável de defeito.
export async function collect(): Promise<CardGroup[]> {
  const ua = navigator.userAgent;
  const vv = window.visualViewport;
  const scale = textScale();
  const hasMSE = 'MediaSource' in window || 'ManagedMediaSource' in window;
  const connection = navigator.connection;

  const identity: CardItem[] = [
    { label: 'navegador', value: uaBrowser(ua) },
    { label: 'sistema', value: uaSystem(ua) },
    { label: 'aparelho', value: await model() },
    { label: 'rodando em', value: whereRuns(ua), alert: ua.includes('; wv)') },
  ];

  const screenInfo: CardItem[] = [
    { label: 'área da página', value: `${innerWidth} × ${innerHeight}` },
    ...(vv && (Math.round(vv.width) !== innerWidth || Math.round(vv.height) !== innerHeight)
      ? [{ label: 'área visível', value: `${Math.round(vv.width)} × ${Math.round(vv.height)}` }]
      : []),
    { label: 'densidade de pixels', value: `${devicePixelRatio}×` },
    // Safari anterior ao 16.4 não tem o screen.orientation.
    {
      label: 'orientação',
      value:
        'orientation' in screen
          ? screen.orientation.type
          : innerWidth > innerHeight
            ? 'landscape'
            : 'portrait',
    },
    // Acima de 115% o layout, calibrado em 15px, começa a quebrar linha onde
    // não quebrava.
    { label: 'escala do texto', value: `${Math.round(scale * 100)}%`, alert: scale > 1.15 },
    { label: 'toque', value: sim(matchMedia('(pointer: coarse)').matches) },
    {
      label: 'tema do sistema',
      value: matchMedia('(prefers-color-scheme: light)').matches ? 'claro' : 'escuro',
    },
    ...(matchMedia('(prefers-reduced-motion: reduce)').matches
      ? [{ label: 'animações', value: 'reduzidas' }]
      : []),
  ];

  const video: CardItem[] = [
    { label: 'MediaSource', value: sim(hasMSE), alert: !hasMSE },
    { label: 'WebRTC', value: sim('RTCPeerConnection' in window) },
    ...CODECS.map((c) => {
      const ok = hasMSE && supports(`video/mp4; codecs="${c.id}"`);
      return { label: c.label, value: sim(ok), alert: !ok && c.id === 'avc1.640029' };
    }),
  ];

  const storage = storageOk();
  const environment: CardItem[] = [
    { label: 'endereço seguro (https)', value: sim(isSecureContext) },
    {
      label: 'armazenamento local',
      value: storage ? 'ok' : 'bloqueado',
      alert: !storage,
    },
    { label: 'fuso', value: Intl.DateTimeFormat().resolvedOptions().timeZone || '?' },
    { label: 'idioma', value: navigator.language },
    ...(navigator.deviceMemory
      ? [{ label: 'memória', value: `${navigator.deviceMemory} GB` }]
      : []),
    ...(navigator.hardwareConcurrency
      ? [{ label: 'núcleos', value: String(navigator.hardwareConcurrency) }]
      : []),
    ...(connection?.effectiveType
      ? [
          {
            label: 'rede',
            value: connection.effectiveType + (connection.saveData ? ', economia de dados' : ''),
          },
        ]
      : []),
  ];

  return [
    { title: 'Identidade', items: identity },
    { title: 'Tela', items: screenInfo },
    { title: 'Vídeo', items: video },
    { title: 'Ambiente', items: environment },
  ];
}

// comoTexto põe os grupos numa linha por item, para colar numa conversa.
// `cabecalho` vai antes dos grupos e `rodape` depois - o user agent no card do
// navegador, as linhas do log no do servidor.
export function asText(groups: CardGroup[], header: string[] = [], footer: string[] = []) {
  const rows = [...header];
  for (const g of groups) {
    rows.push('', `[${g.title}]`);
    for (const i of g.items) rows.push(`${i.label}: ${i.value}`);
  }
  if (footer.length) rows.push('', ...footer);
  return rows.join('\n').trim();
}

// copiar põe o texto na área de transferência. A Clipboard API é
// [SecureContext], então em http:// fica o execCommand antigo, que o Chrome
// ainda aceita a partir de um toque. Devolve se conseguiu.
export async function copy(text: string) {
  // Fora do https o clipboard nem existe, apesar do que o lib.dom declara.
  if ('clipboard' in navigator) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // cai para o caminho antigo
    }
  }
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.setAttribute('readonly', '');
  ta.style.cssText = 'position:fixed;top:0;left:0;opacity:0';
  document.body.append(ta);
  ta.select();
  try {
    // eslint-disable-next-line @typescript-eslint/no-deprecated -- é o único caminho fora do https
    return document.execCommand('copy');
  } catch {
    return false;
  } finally {
    ta.remove();
  }
}
