// O que a máquina que grava está passando, para o card "Este servidor" do
// Diagnóstico. Os números vêm do GET /api/health/servidor; aqui eles viram os
// mesmos grupos { titulo, itens } do card do navegador, com `alerta` marcando o
// valor que, sozinho, já é explicação provável de defeito.
//
// Os limites de alerta são deliberadamente largos: o alvo é hardware pequeno,
// que trabalha perto do teto no dia a dia, e um card vermelho à toa ensina a
// ignorar o vermelho.

import { bytes } from './format.js';

// Acima disso a maioria dos SoCs ARM começa a reduzir a frequência para
// esfriar, e o Orange Pi Zero 3 chega lá sem dissipador.
const TEMPERATURA_ALTA = 80;

// PSI, média de 60s, em % do tempo parado esperando o recurso. Memória é o mais
// sensível: 10% já é o kernel recuperando página no lugar de trabalhar.
const PRESSAO_ALTA = { cpu: 50, memoria: 10, io: 25 };

// Um fsync de 4 KB acima disso é disco sofrendo.
const ESCRITA_LENTA_MS = 1000;

function num(v, casas = 0) {
  return v.toLocaleString('pt-BR', { minimumFractionDigits: casas, maximumFractionDigits: casas });
}

function maquina(m) {
  const itens = [{ rotulo: 'núcleos', valor: String(m.nucleos) }];
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

function memoria(m) {
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
  if (m.limiteBytes) itens.push({ rotulo: 'limite do container', valor: bytes(m.limiteBytes) });
  if (m.oomKills != null) {
    itens.push({
      rotulo: 'mortos por falta de memória',
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
  return itens;
}

// grupos monta o card. Grupo sem fonte - kernel sem PSI, servidor fora do
// Linux - some em vez de aparecer vazio.
export function grupos(info) {
  const out = [{ titulo: 'Máquina', itens: maquina(info.maquina) }];
  if (info.memoria) out.push({ titulo: 'Memória', itens: memoria(info.memoria) });
  if (info.pressao) {
    const itens = pressao(info.pressao);
    if (itens.length) out.push({ titulo: 'Pressão (último minuto)', itens });
  }
  out.push({ titulo: 'Armazenamento', itens: storage(info.storage) });
  out.push({ titulo: 'go2rtc', itens: go2rtc(info.go2rtc) });
  return out;
}

// linhaDoLog formata como o docker logs mostraria, com a hora local de quem lê.
export function linhaDoLog(l) {
  const hora = new Date(l.em).toLocaleString('pt-BR', { dateStyle: 'short', timeStyle: 'medium' });
  return `${hora} ${l.nivel} ${l.texto}`;
}
