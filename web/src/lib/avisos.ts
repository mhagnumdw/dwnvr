// Os avisos do diagnóstico: o card Avisos da tela Diagnóstico e o sino do
// header contam com a mesma função, então os dois não têm como discordar.
//
// Função pura sobre o `health` de `state.svelte.ts`: chamada dentro de um
// $derived, o Svelte rastreia os campos que ela lê.

import { meioDoAlerta, type CameraNaSaude, type DescarteDosAlertas } from './api.ts';
import { bytes, bytesDeMB, duracao, hhmmss, ddmm } from './format.ts';
import type { EstadoDaSaude } from './state.svelte.ts';

export type NivelDoAviso = 'bad' | 'warn';

export interface Aviso {
  nivel: NivelDoAviso;
  texto: string;
}

// De quanto em quanto tempo o header relê a saúde para o sino, fora das telas
// que já a releem a cada HEALTH_POLL_MS. Um problema novo aparece no sino em
// até este tempo; mais curto, cada aba aberta no ao vivo pagaria uma
// requisição a mais por intervalo só para manter um número.
export const AVISOS_POLL_MS = 30000;

const MS_HORA = 3600e3;

// "Agora" pelo relógio do servidor, que é quem carimbou os instantes dos
// avisos: com o relógio do aparelho errado, "desde 13:58 (12min)" viraria
// "(3h12min)". Cai no do aparelho contra servidor antigo, sem o campo.
export function agoraDoServidor(health: EstadoDaSaude) {
  const lido = health.clock ? Date.parse(health.clock.now) : NaN;
  return isNaN(lido) ? health.updatedAt || Date.now() : lido;
}

// quando escreve um instante: só a hora se é de hoje, com a data na frente se
// não é - "13:58:12" de ontem leria como de hoje.
const deHoje = (ms: number, agora: number) =>
  new Date(agora).toDateString() === new Date(ms).toDateString();
export const quando = (ms: number, agora: number) =>
  deHoje(ms, agora) ? hhmmss(ms) : `${ddmm(ms)} ${hhmmss(ms)}`;

// desde é o "desde quando" de todo aviso que depende de tempo, no formato do
// "NÃO ESTÁ GRAVANDO": "desde 13:58:12 (12min)".
function desde(iso: string, agora: number) {
  const ms = Date.parse(iso);
  return `desde ${quando(ms, agora)} (${duracao(agora - ms)})`;
}

const porHora = (v: number) => v.toLocaleString('pt-BR', { maximumFractionDigits: v < 10 ? 1 : 0 });

// A mensagem de reconexão diz o total, desde quando ele conta, a taxa, o
// recente e a última: "24 vezes" sozinho não diz se é muito.
function reconexoes(c: CameraNaSaude, health: EstadoDaSaude, agora: number) {
  const inicio = Date.parse(c.reconnectsSince);
  if (isNaN(inicio)) return `${c.name} já reconectou ${c.reconnects} vezes.`; // servidor antigo

  // Menos de um minuto entre a subida do dwnvr e o início da contagem é a
  // mesma coisa; depois disso, a contagem foi zerada ou a câmera reiniciada.
  const subida = health.uptime ? agora - health.uptime.appSeconds * 1000 : NaN;
  const origem =
    Math.abs(inicio - subida) < 60e3 ? 'desde que o dwnvr subiu' : `desde ${quando(inicio, agora)}`;

  const contando = agora - inicio;
  const entre = [duracao(contando)];
  // Com menos de uma hora de contagem, "por hora" extrapola demais.
  if (contando >= MS_HORA) entre.push(`~${porHora(c.reconnects / (contando / MS_HORA))} por hora`);

  let texto = `${c.name} reconectou ${c.reconnects} vezes ${origem} (${entre.join(', ')})`;
  // Antes de a contagem cobrir a janela inteira, o recente é o próprio total.
  const janela = health.reconnectsWindowHours;
  if (janela && contando >= janela * MS_HORA) {
    texto += `, ${c.reconnectsInWindow} nas últimas ${janela}h`;
  }
  const ultima = c.lastReconnectAt ? Date.parse(c.lastReconnectAt) : NaN;
  if (!isNaN(ultima)) {
    texto += deHoje(ultima, agora)
      ? `, a última às ${hhmmss(ultima)}`
      : `, a última em ${ddmm(ultima)} às ${hhmmss(ultima)}`;
  }
  return texto + '.';
}

// Avisos que explicam problemas antes de eles virarem mistério - que foi
// exatamente o que faltou nos NVRs anteriores. Devolve [{ nivel, texto }], com
// nivel 'bad' ou 'warn'.
export function avisosDe(health: EstadoDaSaude): Aviso[] {
  const agora = agoraDoServidor(health);
  const disk = health.disk;
  const out: Aviso[] = [];
  if (disk?.belowMin) {
    const quandoDisco = disk.belowMinSince ? ` ${desde(disk.belowMinSince, agora)}` : '';
    out.push({
      nivel: 'bad',
      texto: `Disco abaixo do mínimo livre (${bytesDeMB(disk.minFreeMB)})${quandoDisco}: restam ${bytes(disk.freeBytes)}. A retenção está apagando gravações antigas de todas as câmeras.`,
    });
  }
  // "Parada" vem antes de "desconectada" porque é a pergunta que importa: uma
  // conexão de pé que não produz segmento nenhum continua sendo gravação
  // perdida, e foi assim que 9 câmeras passaram horas fora sem ninguém notar.
  for (const c of health.cameras) {
    if (!c.enabled || !c.silent) continue;
    const quandoParou = c.lastSegmentAt
      ? desde(c.lastSegmentAt, agora)
      : 'e não gravou nada desde que o dwnvr subiu';
    out.push({
      nivel: 'bad',
      texto: `${c.name} NÃO ESTÁ GRAVANDO ${quandoParou}${c.lastError ? `: ${c.lastError}` : ''}`,
    });
  }
  for (const c of health.cameras) {
    if (!c.enabled || c.connected) continue;
    if (c.silent) continue; // já avisado acima, e com mais informação
    const quandoCaiu = c.disconnectedAt ? ` ${desde(c.disconnectedAt, agora)}` : '';
    out.push({
      nivel: 'bad',
      texto: `${c.name} desconectada${quandoCaiu}: ${c.lastError ?? 'motivo desconhecido'}`,
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
      out.push({ nivel: 'warn', texto: reconexoes(c, health, agora) });
    }
  }
  // Vem do /api/health, relido a cada poucos segundos, e não do
  // cameras.go2rtcError, que a tela Diagnóstico nunca relê: o go2rtc caindo
  // com ela aberta tem que aparecer, e sumir quando ele volta.
  if (health.go2rtc) {
    out.push({
      nivel: 'bad',
      texto: `go2rtc inacessível ${desde(health.go2rtc.since, agora)}: ${health.go2rtc.error}`,
    });
  }
  // Com o detector de objetos fora, a gravação e as marcas de movimento
  // seguem normais: sem este aviso, a falta das marcas de objeto só se nota
  // depois, na timeline, e as marcas desse intervalo não voltam.
  if (health.detector?.foraDoAr) {
    const f = health.detector.foraDoAr;
    out.push({
      nivel: 'bad',
      texto: `Detector de objetos inacessível ${desde(f.desde, agora)}: ${f.erro}. As câmeras seguem gravando e marcando movimento, mas nenhuma marca de objeto é criada enquanto isso.`,
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
      texto: `Alertas ${meioDoAlerta(f.name)} falhando ${desde(f.since, agora)}: ${f.error}. As detecções seguem gravadas, mas ninguém está sendo avisado.`,
    });
  }
  // Os alertas que não saíram pelos meios, por teto ou fila cheia. Amarelo:
  // eles estão gravados na detecção. Some uma hora depois do último descarte,
  // e não no envio seguinte: o descarte é um evento, e não um estado.
  if (health.alerts?.dropped) {
    out.push({ nivel: 'warn', texto: alertasDescartados(health.alerts.dropped, agora) });
  }
  return out;
}

// "12 alertas não foram enviados desde 14:02:10 (5min), o último às 14:07:31:
// passaram do teto de 15 por hora." Com os dois motivos, cada um com a conta;
// com um alerta só, no singular e sem o "desde".
function alertasDescartados(d: DescarteDosAlertas, agora: number) {
  const total = d.queueFull + d.overLimit;
  const um = (n: number, singular: string, plural: string) => (n === 1 ? singular : plural);
  const teto = (n: number) =>
    `${um(n, 'passou', 'passaram')} do teto de ${d.limitPerHour} por hora`;
  const motivo =
    d.queueFull === 0
      ? teto(total)
      : d.overLimit === 0
        ? 'a fila de envio encheu'
        : `${d.overLimit} ${teto(d.overLimit)}, e ${d.queueFull} não ${um(d.queueFull, 'coube', 'couberam')} na fila de envio`;
  const oQue =
    total === 1
      ? `1 alerta não foi enviado, às ${quando(Date.parse(d.since), agora)}`
      : `${total} alertas não foram enviados ${desde(d.since, agora)}, o último às ${quando(Date.parse(d.lastAt), agora)}`;
  const onde = um(total, 'Ele está gravado', 'Eles estão gravados');
  return `${oQue}: ${motivo}. ${onde} em Detecções, no filtro só alertas.`;
}

// O que o sino precisa: quantos, e a cor do pior. Um vermelho basta para o
// sino ficar vermelho; só amarelos, ele fica amarelo e mais discreto.
export function resumoDosAvisos(avisos: Aviso[]): { total: number; nivel: NivelDoAviso | null } {
  return {
    total: avisos.length,
    nivel: avisos.some((a) => a.nivel === 'bad') ? 'bad' : avisos.length ? 'warn' : null,
  };
}
