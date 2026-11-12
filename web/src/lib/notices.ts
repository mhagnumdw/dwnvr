// Os avisos do diagnóstico: o card Avisos da tela Diagnóstico e o sino do
// header contam com a mesma função, então os dois não têm como discordar.
//
// Função pura sobre o `health` de `state.svelte.ts`: chamada dentro de um
// $derived, o Svelte rastreia os campos que ela lê.

import { alertNotifier, type HealthCamera, type AlertsDropped } from './api.ts';
import { bytes, mbToBytes, duration, hhmmss, ddmm } from './format.ts';
import type { HealthState } from './state.svelte.ts';

export type NoticeLevel = 'bad' | 'warn';

export interface Notice {
  nivel: NoticeLevel;
  texto: string;
}

// De quanto em quanto tempo o header relê a saúde para o sino, fora das telas
// que já a releem a cada HEALTH_POLL_MS. Um problema novo aparece no sino em
// até este tempo; mais curto, cada aba aberta no ao vivo pagaria uma
// requisição a mais por intervalo só para manter um número.
export const NOTICES_POLL_MS = 30000;

const MS_HOUR = 3600e3;

// "Agora" pelo relógio do servidor, que é quem carimbou os instantes dos
// avisos: com o relógio do aparelho errado, "desde 13:58 (12min)" viraria
// "(3h12min)". Cai no do aparelho contra servidor antigo, sem o campo.
export function serverNow(health: HealthState) {
  const lido = health.clock ? Date.parse(health.clock.now) : NaN;
  return isNaN(lido) ? health.updatedAt || Date.now() : lido;
}

// quando escreve um instante: só a hora se é de hoje, com a data na frente se
// não é - "13:58:12" de ontem leria como de hoje.
const ofToday = (ms: number, now: number) =>
  new Date(now).toDateString() === new Date(ms).toDateString();
export const when = (ms: number, now: number) =>
  ofToday(ms, now) ? hhmmss(ms) : `${ddmm(ms)} ${hhmmss(ms)}`;

// desde é o "desde quando" de todo aviso que depende de tempo, no formato do
// "NÃO ESTÁ GRAVANDO": "desde 13:58:12 (12min)".
export function since(iso: string, now: number) {
  const ms = Date.parse(iso);
  return `desde ${when(ms, now)} (${duration(now - ms)})`;
}

const perHour = (v: number) => v.toLocaleString('pt-BR', { maximumFractionDigits: v < 10 ? 1 : 0 });

// A mensagem de reconexão diz o total, desde quando ele conta, a taxa, o
// recente e a última: "24 vezes" sozinho não diz se é muito.
function reconnections(c: HealthCamera, health: HealthState, now: number) {
  const start = Date.parse(c.reconnectsSince);
  if (isNaN(start)) return `${c.name} já reconectou ${c.reconnects} vezes.`; // servidor antigo

  // Menos de um minuto entre a subida do dwnvr e o início da contagem é a
  // mesma coisa; depois disso, a contagem foi zerada ou a câmera reiniciada.
  const boot = health.uptime ? now - health.uptime.appSeconds * 1000 : NaN;
  const origin =
    Math.abs(start - boot) < 60e3 ? 'desde que o dwnvr subiu' : `desde ${when(start, now)}`;

  const counting = now - start;
  const between = [duration(counting)];
  // Com menos de uma hora de contagem, "por hora" extrapola demais.
  if (counting >= MS_HOUR)
    between.push(`~${perHour(c.reconnects / (counting / MS_HOUR))} por hora`);

  let text = `${c.name} reconectou ${c.reconnects} vezes ${origin} (${between.join(', ')})`;
  // Antes de a contagem cobrir a janela inteira, o recente é o próprio total.
  const visibleWindow = health.reconnectsWindowHours;
  if (visibleWindow && counting >= visibleWindow * MS_HOUR) {
    text += `, ${c.reconnectsInWindow} nas últimas ${visibleWindow}h`;
  }
  const lastOne = c.lastReconnectAt ? Date.parse(c.lastReconnectAt) : NaN;
  if (!isNaN(lastOne)) {
    text += ofToday(lastOne, now)
      ? `, a última às ${hhmmss(lastOne)}`
      : `, a última em ${ddmm(lastOne)} às ${hhmmss(lastOne)}`;
  }
  return text + '.';
}

// Avisos que explicam problemas antes de eles virarem mistério - que foi
// exatamente o que faltou nos NVRs anteriores. Devolve [{ nivel, texto }], com
// nivel 'bad' ou 'warn'.
export function noticesOf(health: HealthState): Notice[] {
  const now = serverNow(health);
  const disk = health.disk;
  const out: Notice[] = [];
  if (disk?.belowMin) {
    const whenDisk = disk.belowMinSince ? ` ${since(disk.belowMinSince, now)}` : '';
    out.push({
      nivel: 'bad',
      texto: `Disco abaixo do mínimo livre (${mbToBytes(disk.minFreeMB)})${whenDisk}: restam ${bytes(disk.freeBytes)}. A retenção está apagando gravações antigas de todas as câmeras.`,
    });
  }
  // "Parada" vem antes de "desconectada" porque é a pergunta que importa: uma
  // conexão de pé que não produz segmento nenhum continua sendo gravação
  // perdida, e foi assim que 9 câmeras passaram horas fora sem ninguém notar.
  for (const c of health.cameras) {
    if (!c.enabled || !c.silent) continue;
    const whenStopped = c.lastSegmentAt
      ? since(c.lastSegmentAt, now)
      : 'e não gravou nada desde que o dwnvr subiu';
    out.push({
      nivel: 'bad',
      texto: `${c.name} NÃO ESTÁ GRAVANDO ${whenStopped}${c.lastError ? `: ${c.lastError}` : ''}`,
    });
  }
  for (const c of health.cameras) {
    if (!c.enabled || c.connected) continue;
    if (c.silent) continue; // já avisado acima, e com mais informação
    const whenDown = c.disconnectedAt ? ` ${since(c.disconnectedAt, now)}` : '';
    out.push({
      nivel: 'bad',
      texto: `${c.name} desconectada${whenDown}: ${c.lastError ?? 'motivo desconhecido'}`,
    });
  }
  // Os dois avisos de configuração também vêm do /api/health, e não da lista
  // de câmeras, que a tela Diagnóstico nunca relê: o go2rtc.yaml editado e o
  // go2rtc reiniciado por fora apareceriam só depois de passar pela tela
  // Câmeras.
  for (const c of health.cameras) {
    if (c.transcoding) {
      out.push({
        nivel: 'warn',
        texto: `${c.name} usa uma fonte ffmpeg no go2rtc, ou seja, há transcodificação consumindo CPU.`,
      });
    }
  }
  for (const c of health.cameras) {
    // O audio só vem de câmera com recorder: a desabilitada não tem trilha a
    // cobrar.
    if (c.audio && c.audio !== 'none' && !c.hasAudio) {
      out.push({
        nivel: 'warn',
        texto: `${c.name} está configurada com áudio ${c.audio}, mas o stream não entrega trilha de áudio.`,
      });
    }
    if (c.reconnects > 10) {
      out.push({ nivel: 'warn', texto: reconnections(c, health, now) });
    }
  }
  // Vem do /api/health, relido a cada poucos segundos, e não do
  // cameras.go2rtcError, que a tela Diagnóstico nunca relê: o go2rtc caindo
  // com ela aberta tem que aparecer, e sumir quando ele volta.
  if (health.go2rtc) {
    out.push({
      nivel: 'bad',
      texto: `go2rtc inacessível ${since(health.go2rtc.since, now)}: ${health.go2rtc.error}`,
    });
  }
  // Com o detector de objetos fora, a gravação e as marcas de movimento
  // seguem normais: sem este aviso, a falta das marcas de objeto só se nota
  // depois, na timeline, e as marcas desse intervalo não voltam.
  if (health.detector?.foraDoAr) {
    const f = health.detector.foraDoAr;
    out.push({
      nivel: 'bad',
      texto: `Detector de objetos inacessível ${since(f.desde, now)}: ${f.erro}. As câmeras seguem gravando e marcando movimento, mas nenhuma marca de objeto é criada enquanto isso.`,
    });
  }
  // A seção smtp recusada no boot desliga o e-mail até alguém corrigir o
  // dwnvr.yaml e reiniciar: sem este aviso, só o log diria. Amarelo, porque a
  // gravação e a detecção seguem normais.
  if (health.smtp) {
    out.push({
      nivel: 'warn',
      texto: `E-mail desligado: ${health.smtp.error}. Corrija o dwnvr.yaml e reinicie o dwnvr.`,
    });
  }
  // O usuarios.json editado à mão. Ilegível é vermelho: só o dono entra. A
  // entrada que não vale é amarela, uma linha por problema.
  if (health.users?.error) {
    out.push({
      nivel: 'bad',
      texto: `Cadastro de usuários ilegível: ${health.users.error}. Só o dono do dwnvr.yaml entra até o arquivo ser corrigido e o dwnvr reiniciado.`,
    });
  }
  for (const w of health.users?.warnings ?? []) {
    out.push({
      nivel: 'warn',
      texto: `Cadastro de usuários: ${w}. Corrija o usuarios.json e reinicie o dwnvr.`,
    });
  }
  // A seção alerts do dwnvr.yaml: o endereço que ficou de fora da lista, ou a
  // lista sem a seção smtp. Amarelo, como a smtp recusada: o alerta segue
  // gravado na detecção.
  for (const w of health.alerts?.warnings ?? []) {
    out.push({
      nivel: 'warn',
      texto: `Alertas por e-mail: ${w}. Corrija o dwnvr.yaml e reinicie o dwnvr.`,
    });
  }
  // O meio do alerta falhando: vermelho, porque ninguém está sendo avisado, e
  // a gravação seguir não muda isso. Some no primeiro envio que der certo.
  for (const f of health.alerts?.failing ?? []) {
    out.push({
      nivel: 'bad',
      texto: `Alertas ${alertNotifier(f.name)} falhando ${since(f.since, now)}: ${f.error}. As detecções seguem gravadas, mas ninguém está sendo avisado.`,
    });
  }
  // Os alertas que não saíram pelos meios, por teto ou fila cheia. Amarelo:
  // eles estão gravados na detecção. Some uma hora depois do último descarte,
  // e não no envio seguinte: o descarte é um evento, e não um estado.
  if (health.alerts?.dropped) {
    out.push({ nivel: 'warn', texto: droppedAlerts(health.alerts.dropped, now) });
  }
  return out;
}

// "12 alertas não foram enviados desde 14:02:10 (5min), o último às 14:07:31:
// passaram do teto de 15 por hora." Com os dois motivos, cada um com a conta;
// com um alerta só, no singular e sem o "desde".
function droppedAlerts(d: AlertsDropped, now: number) {
  const total = d.queueFull + d.overLimit;
  const one = (n: number, singular: string, plural: string) => (n === 1 ? singular : plural);
  const ceiling = (n: number) =>
    `${one(n, 'passou', 'passaram')} do teto de ${d.limitPerHour} por hora`;
  const reason =
    d.queueFull === 0
      ? ceiling(total)
      : d.overLimit === 0
        ? 'a fila de envio encheu'
        : `${d.overLimit} ${ceiling(d.overLimit)}, e ${d.queueFull} não ${one(d.queueFull, 'coube', 'couberam')} na fila de envio`;
  const oQue =
    total === 1
      ? `1 alerta não foi enviado, às ${when(Date.parse(d.since), now)}`
      : `${total} alertas não foram enviados ${since(d.since, now)}, o último às ${when(Date.parse(d.lastAt), now)}`;
  const where = one(total, 'Ele está gravado', 'Eles estão gravados');
  return `${oQue}: ${reason}. ${where} em Detecções, no filtro só alertas.`;
}

// O que o sino precisa: quantos, e a cor do pior. Um vermelho basta para o
// sino ficar vermelho; só amarelos, ele fica amarelo e mais discreto.
export function noticesSummary(notices: Notice[]): { total: number; nivel: NoticeLevel | null } {
  return {
    total: notices.length,
    nivel: notices.some((a) => a.nivel === 'bad') ? 'bad' : notices.length ? 'warn' : null,
  };
}
