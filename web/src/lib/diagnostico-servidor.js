// O que a máquina que grava está passando, para o card "Este servidor" do
// Diagnóstico. Os números vêm do GET /api/health/servidor; aqui eles viram os
// mesmos grupos { titulo, itens } do card do navegador, com `alerta` marcando o
// valor que, sozinho, já é explicação provável de defeito.
//
// Os limites de alerta são deliberadamente largos: o alvo é hardware pequeno,
// que trabalha perto do teto no dia a dia, e um card vermelho à toa ensina a
// ignorar o vermelho.

import { bytes, duracao, relogioDeFuso } from './format.js';

// Acima disso a maioria dos SoCs ARM começa a reduzir a frequência para
// esfriar, e o Orange Pi Zero 3 chega lá sem dissipador.
const TEMPERATURA_ALTA = 80;

// PSI, média de 60s, em % do tempo parado esperando o recurso. Memória é o mais
// sensível: 10% já é o kernel recuperando página no lugar de trabalhar.
const PRESSAO_ALTA = { cpu: 50, memoria: 10, io: 25 };

// Um fsync de 4 KB acima disso é disco sofrendo.
const ESCRITA_LENTA_MS = 1000;

// Fração do teto - memória do container, arquivos abertos - a partir da qual o
// processo está a um susto de bater nele.
const PERTO_DO_TETO = 0.9;

function num(v, casas = 0) {
  return v.toLocaleString('pt-BR', { minimumFractionDigits: casas, maximumFractionDigits: casas });
}

function maquina(m) {
  const itens = [];
  if (m.placa) itens.push({ rotulo: 'placa', valor: m.placa });
  if (m.processador) itens.push({ rotulo: 'processador', valor: m.processador });
  if (m.kernel) itens.push({ rotulo: 'kernel', valor: m.kernel });
  // O teto do container vai junto dos núcleos, e não numa linha à parte: é ele
  // que diz quantos o dwnvr enxerga de fato.
  const liberados = m.nucleosLiberados ? ` (o container pode usar ${num(m.nucleosLiberados, m.nucleosLiberados % 1 ? 1 : 0)})` : '';
  itens.push({ rotulo: 'núcleos', valor: `${m.nucleos}${liberados}` });
  if (m.carga) {
    // Carga acima do número de núcleos é fila: tem processo esperando CPU.
    itens.push({
      rotulo: 'carga (1, 5, 15 min)',
      valor: m.carga.map((c) => num(c, 2)).join(' · '),
      alerta: m.carga[0] > m.nucleos,
    });
  }
  for (const s of m.sensores ?? []) {
    itens.push({
      rotulo: m.sensores.length > 1 ? `temperatura (${s.nome})` : 'temperatura',
      valor: `${num(s.celsius, 1)} °C`,
      alerta: s.celsius >= TEMPERATURA_ALTA,
    });
  }
  if (m.mhz) {
    const max = m.mhzMax ? ` de ${num(m.mhzMax)}` : '';
    itens.push({ rotulo: 'frequência da CPU', valor: `${num(m.mhz)}${max} MHz` });
  }
  if (m.governor) itens.push({ rotulo: 'política de frequência', valor: m.governor });
  return itens;
}

// processo é o dwnvr visto por ele mesmo. `anterior` é a leitura de antes, da
// qual sai a CPU de agora; sem ela, vale a média desde que o dwnvr subiu.
function processo(p, nucleos, limiteBytes, anterior) {
  const itens = [];
  if (p.cpuMs != null && p.vivoMs > 0) {
    // Leitura de antes da última subida do dwnvr não serve: o tempo de CPU
    // recomeçou do zero.
    const a = anterior?.cpuMs != null && p.vivoMs > anterior.vivoMs && p.cpuMs >= anterior.cpuMs ? anterior : null;
    const usado = a ? (p.cpuMs - a.cpuMs) / (p.vivoMs - a.vivoMs) : p.cpuMs / p.vivoMs;
    const quando = a ? `últimos ${duracao(p.vivoMs - a.vivoMs)}` : 'média desde que subiu';
    // Em fração da máquina inteira, e não de um núcleo, para comparar direto
    // com a carga e a pressão ao lado.
    itens.push({ rotulo: 'CPU', valor: `${num((usado / nucleos) * 100, 1)}% da máquina (${quando})` });
  }
  if (p.memoriaBytes) {
    itens.push({
      rotulo: 'memória',
      valor: limiteBytes ? `${bytes(p.memoriaBytes)} de ${bytes(limiteBytes)} do limite` : bytes(p.memoriaBytes),
      alerta: limiteBytes > 0 && p.memoriaBytes >= limiteBytes * PERTO_DO_TETO,
    });
  }
  itens.push({ rotulo: 'goroutines', valor: num(p.goroutines) });
  if (p.arquivosAbertos) {
    const l = p.limiteDeArquivos;
    itens.push({
      rotulo: 'arquivos abertos',
      valor: l ? `${num(p.arquivosAbertos)} de ${num(l)}` : num(p.arquivosAbertos),
      alerta: l > 0 && p.arquivosAbertos >= l * PERTO_DO_TETO,
    });
  }
  if (p.uid != null) itens.push({ rotulo: 'usuário e grupo', valor: `${p.uid}:${p.gid ?? '?'}` });
  itens.push({ rotulo: 'binário', valor: `Go ${p.go.replace(/^go/, '')} · ${p.arquitetura}` });
  return itens;
}

// O limite do container vai na linha de memória do processo, que é quem bate
// nele; aqui ele só aparece se essa linha faltar.
function memoria(m, limiteNoProcesso) {
  const livre = m.totalBytes ? m.disponivelBytes / m.totalBytes : 1;
  const itens = [
    {
      rotulo: 'memória disponível',
      valor: `${bytes(m.disponivelBytes)} de ${bytes(m.totalBytes)}`,
      alerta: livre < 0.1,
    },
  ];
  if (m.swapTotalBytes) {
    // Swap usada por si só não é problema - o kernel manda para lá o que
    // ninguém usa. Metade cheia é que já é memória faltando de verdade.
    itens.push({
      rotulo: 'swap usada',
      valor: `${bytes(m.swapUsadaBytes)} de ${bytes(m.swapTotalBytes)}`,
      alerta: m.swapUsadaBytes > m.swapTotalBytes / 2,
    });
  } else {
    itens.push({ rotulo: 'swap', valor: 'nenhuma' });
  }
  if (m.limiteBytes && !limiteNoProcesso) itens.push({ rotulo: 'limite do container', valor: bytes(m.limiteBytes) });
  if (m.oomKills != null) {
    itens.push({
      rotulo: 'processos encerrados por falta de memória (OOM kill)',
      valor: m.oomKills ? `${m.oomKills} desde que a máquina ligou` : 'nenhum desde que a máquina ligou',
      alerta: m.oomKills > 0,
    });
  }
  return itens;
}

const RECURSOS = [
  ['cpu', 'esperando CPU'],
  ['memoria', 'esperando memória'],
  ['io', 'esperando disco'],
];

function pressao(p) {
  return RECURSOS.filter(([k]) => p[k]?.some).map(([k, rotulo]) => ({
    rotulo,
    valor: `${num(p[k].some.avg60, 1)}% do tempo`,
    alerta: p[k].some.avg60 >= PRESSAO_ALTA[k],
  }));
}

function storage(s) {
  const itens = [{ rotulo: 'caminho', valor: s.caminho }];
  if (s.tipo) itens.push({ rotulo: 'sistema de arquivos', valor: s.tipo });
  if (s.origem) itens.push({ rotulo: 'dispositivo', valor: s.origem });
  if (s.opcoes) {
    itens.push({
      rotulo: 'montado como',
      valor: s.somenteLeitura ? 'SOMENTE LEITURA' : 'leitura e escrita',
      alerta: s.somenteLeitura,
    });
  }
  const e = s.escrita;
  itens.push({
    rotulo: 'teste de escrita',
    valor: e.ok ? `ok em ${num(e.ms)} ms` : e.erro,
    alerta: !e.ok || e.ms >= ESCRITA_LENTA_MS,
  });
  return itens;
}

function go2rtc(g) {
  const itens = [
    { rotulo: 'responde', valor: g.ok ? `sim, em ${num(g.ms)} ms` : `não: ${g.erro}`, alerta: !g.ok },
  ];
  if (g.versao) itens.push({ rotulo: 'versão', valor: g.versao });
  if (g.url) itens.push({ rotulo: 'endereço', valor: g.url });
  return itens;
}

function detector(d) {
  const itens = [
    { rotulo: 'responde', valor: d.ok ? `sim, em ${num(d.ms)} ms` : `não: ${d.erro}`, alerta: !d.ok },
  ];
  if (d.modelo) itens.push({ rotulo: 'modelo', valor: d.entrada ? `${d.modelo}, entrada ${d.entrada}` : d.modelo });
  if (d.threads) itens.push({ rotulo: 'threads', valor: String(d.threads) });
  itens.push({ rotulo: 'endereço', valor: d.url });
  return itens;
}

// grupos monta o card. Grupo sem fonte - kernel sem PSI, servidor fora do
// Linux, instalação sem detector de objetos - some em vez de aparecer vazio.
// `anterior` é a leitura de antes, para a CPU do processo.
export function grupos(info, anterior) {
  const limite = info.memoria?.limiteBytes;
  const out = [{ titulo: 'Máquina', itens: maquina(info.maquina) }];
  // Servidor anterior a este campo não manda o processo.
  if (info.processo) {
    out.push({
      titulo: 'Processo do dwnvr',
      itens: processo(info.processo, info.maquina.nucleos, limite, anterior?.processo),
    });
  }
  if (info.memoria) {
    out.push({ titulo: 'Memória', itens: memoria(info.memoria, info.processo?.memoriaBytes > 0) });
  }
  if (info.pressao) {
    const itens = pressao(info.pressao);
    if (itens.length) out.push({ titulo: 'Pressão (último minuto)', itens });
  }
  out.push({ titulo: 'Armazenamento', itens: storage(info.storage) });
  out.push({ titulo: 'go2rtc', itens: go2rtc(info.go2rtc) });
  if (info.detector) out.push({ titulo: 'Detector de objetos', itens: detector(info.detector) });
  return out;
}

// coletadoEm diz quando o servidor fez a leitura, no relógio e no fuso DELE,
// como o "horário do servidor" da faixa do topo: convertido para o fuso de quem
// lê, esconderia um servidor com fuso errado. Servidor anterior a este campo
// não manda nada.
//
// Os espaços de dentro da data e do fuso são inquebráveis: sem caber numa
// linha, no celular, a quebra cai entre a hora e o fuso, e não no meio de um.
export function coletadoEm(c) {
  const lido = Date.parse(c?.em);
  if (isNaN(lido)) return '';
  const inteiro = (s) => s.replaceAll(' ', ' ');
  return `${inteiro(`Coletado em ${relogioDeFuso(lido, c.offsetSeconds)}`)} ${inteiro(c.zona || c.sigla)}`;
}

// linhaDoLog formata como o docker logs mostraria, com a hora local de quem lê.
export function linhaDoLog(l) {
  const hora = new Date(l.em).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'medium' });
  return `${hora} ${l.nivel} ${l.texto}`;
}
