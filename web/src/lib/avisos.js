// Os avisos do diagnóstico: o card Avisos da tela Diagnóstico e o sino do
// header contam com a mesma função, então os dois não têm como discordar.
//
// Função pura sobre o `health` de `state.svelte.js`: chamada dentro de um
// $derived, o Svelte rastreia os campos que ela lê.

import { bytes, bytesDeMB, duracao, hhmmss, ddmm } from './format.js';

// De quanto em quanto tempo o header relê a saúde para o sino, fora das telas
// que já a releem a cada HEALTH_POLL_MS. Um problema novo aparece no sino em
// até este tempo; mais curto, cada aba aberta no ao vivo pagaria uma
// requisição a mais por intervalo só para manter um número.
export const AVISOS_POLL_MS = 30000;

const MS_HORA = 3600e3;

// "Agora" pelo relógio do servidor, que é quem carimbou os instantes dos
// avisos: com o relógio do aparelho errado, "desde 13:58 (12min)" viraria
// "(3h12min)". Cai no do aparelho contra servidor antigo, sem o campo.
export function agoraDoServidor(health) {
  const lido = Date.parse(health.clock?.now);
  return isNaN(lido) ? health.updatedAt || Date.now() : lido;
}

// quando escreve um instante: só a hora se é de hoje, com a data na frente se
// não é - "13:58:12" de ontem leria como de hoje.
const deHoje = (ms, agora) => new Date(agora).toDateString() === new Date(ms).toDateString();
export const quando = (ms, agora) => (deHoje(ms, agora) ? hhmmss(ms) : `${ddmm(ms)} ${hhmmss(ms)}`);

// desde é o "desde quando" de todo aviso que depende de tempo, no formato do
// "NÃO ESTÁ GRAVANDO": "desde 13:58:12 (12min)".
function desde(iso, agora) {
  const ms = Date.parse(iso);
  return `desde ${quando(ms, agora)} (${duracao(agora - ms)})`;
}

const porHora = (v) => v.toLocaleString('pt-BR', { maximumFractionDigits: v < 10 ? 1 : 0 });

// A mensagem de reconexão diz o total, desde quando ele conta, a taxa, o
// recente e a última: "24 vezes" sozinho não diz se é muito.
function reconexoes(c, health, agora) {
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
  const ultima = Date.parse(c.lastReconnectAt);
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
export function avisosDe(health) {
  const agora = agoraDoServidor(health);
  const disk = health.disk;
  const out = [];
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
    out.push({ nivel: 'bad', texto: `${c.name} desconectada${quandoCaiu}: ${c.lastError || 'motivo desconhecido'}` });
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
    out.push({ nivel: 'bad', texto: `go2rtc inacessível ${desde(health.go2rtc.since, agora)}: ${health.go2rtc.error}` });
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
  return out;
}

// O que o sino precisa: quantos, e a cor do pior. Um vermelho basta para o
// sino ficar vermelho; só amarelos, ele fica amarelo e mais discreto.
export function resumoDosAvisos(avisos) {
  return {
    total: avisos.length,
    nivel: avisos.some((a) => a.nivel === 'bad') ? 'bad' : avisos.length ? 'warn' : null,
  };
}
