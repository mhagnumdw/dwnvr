<script lang="ts">
  // Detecções: as detecções de objeto de todas as câmeras numa grade só, da
  // mais nova para a mais velha, agrupadas por hora.
  //
  // O dia inteiro não cabe na memória de um celular nem na tela, então nada
  // aqui é "tudo": o servidor entrega páginas por cursor, a tela guarda só uma
  // janela delas (MAX_ITENS) e o DOM só tem as linhas que estão à vista, com
  // uma tela de sobra para cada lado. O resto é conta: a miniatura tem largura
  // de coluna e proporção 16:9, e o cabeçalho da hora tem altura fixa - então
  // a posição de cada linha se calcula sem medir nada, e a barra de rolagem
  // tem o tamanho certo desde o primeiro quadro.
  import { onMount, tick } from 'svelte';
  import NavDia from '../components/NavDia.svelte';
  import Relogio from '../components/Relogio.svelte';
  import DeteccaoFolha from '../components/DeteccaoFolha.svelte';
  import BotaoCameras from '../components/BotaoCameras.svelte';
  import SeletorCameras from '../components/SeletorCameras.svelte';
  import Icone from '../components/Icone.svelte';
  import SeloDeFamilias from '../components/SeloDeFamilias.svelte';
  import { api, mediaURL, mensagemDe } from '../lib/api.ts';
  import type { Caixa, Deteccao, FiltroDeDeteccoes } from '../lib/api.ts';
  import { cameras, loadCameras } from '../lib/state.svelte.ts';
  import { paramsAtuais, escrever, linkGravacoes } from '../lib/rota.svelte.ts';
  import { FAMILIAS, NOMES_DAS_FAMILIAS, familia, iconeURL } from '../lib/icones.ts';
  import type { NomeDaFamilia } from '../lib/icones.ts';
  import { CAIXA_FOLGA_PX } from '../lib/caixas.ts';
  import { hhmmss, ddmm, dayKey, parseDay } from '../lib/format.ts';
  import { avisar, fecharAviso } from '../lib/snackbar.svelte.ts';

  // Uma linha da grade: o cabeçalho da hora, ou uma fileira de miniaturas.
  // `primeira` é o índice na `lista` da primeira miniatura da fileira.
  interface LinhaDaHora {
    tipo: 'hora';
    k: string;
    y: number;
    altura: number;
    rotulo: string;
    dia: string;
  }
  interface LinhaDaGrade {
    tipo: 'grade';
    k: string;
    y: number;
    altura: number;
    primeira: number;
    itens: Deteccao[];
  }
  type Linha = LinhaDaHora | LinhaDaGrade;

  // A detecção que segura a vista, e a distância dela até o topo.
  interface Ancora {
    k: string;
    dy: number;
  }

  // --- parâmetros -------------------------------------------------------------

  // Colunas da grade: o zoom anda entre as pontas, e o padrão é o que o mock
  // aprovado mostrava. No celular a miniatura de 3 colunas (103 px) já foi
  // recusada como pequena demais; 4 fica como opção de quem quer varrer.
  const COLUNAS = {
    celular: { padrao: 2, min: 1, max: 4 },
    desktop: { padrao: 5, min: 3, max: 8 },
  };
  // A mesma quebra do App.svelte, onde a navegação sobe para o cabeçalho.
  const DESKTOP_PX = 720;
  const HORA_PX = 40; // o cabeçalho da hora entre os grupos

  // Quantas telas de detecção carregar à frente do que se vê, para a rolagem
  // não alcançar a borda do que já chegou. E quantas telas de DOM manter além
  // da vista: o bastante para um arremesso de dedo não mostrar buraco.
  const BUSCA_TELAS = 1.5;
  const DOM_TELAS = 1;
  // Quantas detecções guardar ao todo. Passando disso, o que ficou longe da
  // vista, do lado oposto ao da rolagem, é solto - e é buscado de novo se a
  // rolagem voltar até lá.
  const MAX_ITENS = 600;

  // A detecção nova chega sozinha enquanto a tela está no topo. A marca de
  // objeto é gravada depois que o detector olha, com o instante do onset, então
  // uma detecção pode nascer ABAIXO do topo que já se mostrou: a busca volta
  // ATRASO_MS antes do topo para pegar essas também.
  const NOVAS_MS = 20000;
  const ATRASO_MS = 5 * 60 * 1000;

  // A altura do menu do ⋮, para abrir para cima quando não cabe embaixo. E
  // quanto tempo o "desfazer" fica na tela depois de ocultar uma câmera.
  const MENU_PX = 140;
  const DESFAZER_MS = 6000;

  const ORDEM_FAMILIAS = NOMES_DAS_FAMILIAS;

  // --- estado da URL ----------------------------------------------------------

  const params = paramsAtuais();

  const idsDaURL = <T extends string>(k: string, validos: readonly T[]): T[] | null =>
    params.has(k) ? params.getAll(k).filter((v): v is T => validos.some((x) => x === v)) : null;

  // Nulo é "todas": é o padrão, e fica fora do link.
  let camsMarcadas = $state<string[] | null>(null);
  let familiasMarcadas = $state<NomeDaFamilia[] | null>(idsDaURL('familias', ORDEM_FAMILIAS));
  let colunasEscolhidas = $state<number | null>(Number(params.get('cols')) || null);
  let comCaixas = $state(params.get('caixas') !== '0');
  let atualizando = $state(params.get('atualizar') !== '0');

  let desktop = $state(window.innerWidth >= DESKTOP_PX);
  const faixa = $derived(desktop ? COLUNAS.desktop : COLUNAS.celular);
  const colunas = $derived(
    Math.min(faixa.max, Math.max(faixa.min, colunasEscolhidas ?? faixa.padrao)),
  );

  $effect(() => {
    escrever({
      cams: camsMarcadas,
      familias: familiasMarcadas,
      cols: colunasEscolhidas,
      caixas: comCaixas ? null : '0',
      atualizar: atualizando ? null : '0',
    });
  });

  // --- as detecções -----------------------------------------------------------

  // A janela carregada, da mais nova para a mais velha. `$state.raw` porque
  // cada página troca o array inteiro: proxy em 600 objetos com caixas seria
  // custo sem uso.
  let lista = $state.raw<Deteccao[]>([]);
  // Se há mais além de cada ponta da janela. Acima: detecção mais nova que o
  // topo carregado - depois de um "ir para", ou depois de soltar o topo.
  let fimAcima = $state(true);
  let fimAbaixo = $state(false);
  let carregado = $state(false);
  let buscandoAcima = false;
  let buscandoAbaixo = false;
  let erro = $state('');
  // O instante do último "ir para", que serve de cursor quando ele caiu antes
  // de qualquer detecção e a lista começou vazia.
  let alvo: number | null = null;
  // Invalida a resposta de uma busca que já não descreve a tela: filtro novo,
  // "ir para" novo.
  let geracao = 0;

  // O ao vivo e Gravações só abrem para câmera que ainda está no cadastro: uma
  // detecção antiga pode citar uma câmera removida desde então, e as duas telas
  // trocariam o id desconhecido por outra câmera.
  const existe = (cam: string) => cameras.list.some((c) => c.id === cam);

  const chave = (d: Deteccao) => `${d.cam}|${d.instanteMs}`;

  const nomes = $derived(Object.fromEntries(cameras.list.map((c) => [c.id, c.name])));

  // O que vai no pedido. Nulo quando o filtro não deixou nada marcado, e aí nem
  // se pergunta ao servidor.
  const filtro = $derived.by((): FiltroDeDeteccoes | null => {
    if (camsMarcadas?.length === 0 || familiasMarcadas?.length === 0) return null;
    return { cams: camsMarcadas, familias: familiasMarcadas };
  });

  // Uma página é o que enche duas telas: pouco mais que isso já é o prefetch.
  function pagina() {
    const linhas = Math.ceil(window.innerHeight / (grade.alturaTile + grade.vao)) + 1;
    return Math.min(200, Math.max(24, linhas * colunas * 2));
  }

  // junta duas fatias em ordem, sem repetir: a busca de novas volta de
  // propósito sobre o que já se tem, e uma página pode empatar com a vizinha.
  function junta(a: Deteccao[], b: Deteccao[]) {
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- Set local, só para juntar
    const vistos = new Set<string>();
    const out: Deteccao[] = [];
    for (const d of [...a, ...b].sort(
      (x, y) => y.instanteMs - x.instanteMs || (x.cam < y.cam ? -1 : 1),
    )) {
      const k = chave(d);
      if (vistos.has(k)) continue;
      vistos.add(k);
      out.push(d);
    }
    return out;
  }

  async function recomeca(antes: number | null = null) {
    const g = ++geracao;
    alvo = antes;
    lista = [];
    fimAcima = antes === null;
    fimAbaixo = false;
    carregado = false;
    erro = '';
    aberto = null;
    buscandoAcima = buscandoAbaixo = false;
    window.scrollTo(0, 0);
    if (!filtro) {
      fimAbaixo = true;
      carregado = true;
      return;
    }
    try {
      const r = await api.deteccoes({ ...filtro, antes, limite: pagina() });
      if (g !== geracao) return;
      lista = r.deteccoes;
      fimAbaixo = r.fim;
      carregado = true;
      await tick();
      confere();
    } catch (e) {
      if (g === geracao) erro = mensagemDe(e);
    }
  }

  async function abaixo() {
    const ultimo = lista.at(-1);
    if (!ultimo) return;
    buscandoAbaixo = true;
    const g = geracao;
    try {
      const r = await api.deteccoes({ ...filtro, antes: ultimo.instanteMs, limite: pagina() });
      if (g !== geracao) return;
      const a = ancora();
      let nova = junta(lista, r.deteccoes);
      fimAbaixo = r.fim;
      // Solta o topo, que ficou para trás. A âncora mora na vista e a vista
      // nunca está no pedaço solto: a sobra é bem maior que uma tela.
      if (nova.length > MAX_ITENS) {
        nova = nova.slice(nova.length - MAX_ITENS);
        fimAcima = false;
      }
      lista = nova;
      await restaura(a);
    } catch (e) {
      if (g === geracao) erro = mensagemDe(e);
    } finally {
      if (g === geracao) buscandoAbaixo = false;
    }
    confere();
  }

  async function acima() {
    buscandoAcima = true;
    const g = geracao;
    try {
      const depois = lista[0]?.instanteMs ?? alvo;
      const r = await api.deteccoes({ ...filtro, depois, limite: pagina() });
      if (g !== geracao) return;
      const a = ancora();
      let nova = junta(r.deteccoes, lista);
      fimAcima = r.fim;
      if (nova.length > MAX_ITENS) {
        nova = nova.slice(0, MAX_ITENS);
        fimAbaixo = false;
      }
      lista = nova;
      await restaura(a);
    } catch (e) {
      if (g === geracao) erro = mensagemDe(e);
    } finally {
      if (g === geracao) buscandoAcima = false;
    }
    confere();
  }

  // As novas, e as atrasadas logo abaixo do topo. Só com o topo carregado: fora
  // dele, a rolagem para cima é que as traz.
  async function novas() {
    if (!atualizando || document.hidden || !carregado || !fimAcima || !filtro || buscandoAcima)
      return;
    const g = geracao;
    const topo = lista[0]?.instanteMs ?? Date.now();
    try {
      const r = await api.deteccoes({ ...filtro, depois: topo - ATRASO_MS, limite: 200 });
      const maisNova = r.deteccoes[0];
      if (g !== geracao || !maisNova) return;
      // Quem está olhando o topo vê a novidade entrar; quem rolou para baixo
      // não pode ter a tela empurrada por ela.
      const a = rolagem.ini > 0 ? ancora() : null;
      lista = junta(r.deteccoes, lista);
      // Mais de 200 no intervalo: as mais novas ficaram de fora, e a rolagem
      // para cima as busca como faria depois de um "ir para".
      if (!r.fim) fimAcima = false;
      registraDia(dayKey(new Date(maisNova.instanteMs)));
      if (a) await restaura(a);
    } catch {
      // A próxima rodada tenta de novo; um soluço da rede não vira aviso.
    }
  }

  // --- os dias do calendário -------------------------------------------------

  // Os dias com detecção do filtro, para o calendário e as setas. Vazia é o
  // modo permissivo do seletor: até a resposta chegar, ou se ela falhar, todo
  // dia até hoje vale.
  let diasComDeteccao = $state.raw<string[]>([]);
  // Segura a primeira consulta até a câmera da URL ser resolvida: antes disso o
  // filtro é "todas", e a consulta sairia à toa - e, com o disco frio, é a mais
  // cara da tela.
  let camerasResolvidas = $state(false);

  $effect(() => {
    if (camerasResolvidas) void carregaDias(filtro);
  });

  async function carregaDias(f: FiltroDeDeteccoes | null) {
    if (!f) {
      diasComDeteccao = [];
      return;
    }
    try {
      const r = await api.diasDeDeteccao(f);
      if (f === filtro) diasComDeteccao = r.dias;
    } catch {
      if (f === filtro) diasComDeteccao = [];
    }
  }

  // Hoje estreando no calendário com a tela aberta: a detecção nova já diz o
  // dia, sem reler a lista. Em modo permissivo a lista fica vazia - um dia só a
  // tiraria do "tudo liberado".
  function registraDia(dia: string) {
    if (!diasComDeteccao.length || diasComDeteccao.includes(dia)) return;
    diasComDeteccao = [...diasComDeteccao, dia].sort();
  }

  // Religar busca na hora: esperar a rodada seguinte pareceria que não pegou.
  function alternaAtualizar() {
    atualizando = !atualizando;
    if (atualizando) void novas();
  }

  // --- a grade, calculada -----------------------------------------------------

  let listaEl: HTMLDivElement | undefined;
  let largura = $state(0);

  const grade = $derived.by(() => {
    const vao = desktop ? 8 : 6;
    const larguraTile = Math.max(0, (largura - vao * (colunas - 1)) / colunas);
    return { vao, larguraTile, alturaTile: (larguraTile * 9) / 16 };
  });

  const chaveHora = (ms: number) => {
    const d = new Date(ms);
    return `${dayKey(d)}T${d.getHours()}`;
  };
  const SEMANA = ['dom', 'seg', 'ter', 'qua', 'qui', 'sex', 'sáb'];

  // As linhas na ordem da tela, cada uma com o `y` onde começa. `linhaDe[i]`
  // diz em que linha mora a detecção i, para achar a âncora depois de uma troca.
  const layout = $derived.by(() => {
    const { vao, alturaTile } = grade;
    const linhas: Linha[] = [];
    const linhaDe: number[] = [];
    let y = 0;
    let hora: string | null = null;
    let atual: LinhaDaGrade | null = null;
    for (const [i, det] of lista.entries()) {
      const ms = det.instanteMs;
      const h = chaveHora(ms);
      if (h !== hora) {
        const d = new Date(ms);
        linhas.push({
          tipo: 'hora',
          k: `h${h}`,
          y,
          altura: HORA_PX,
          rotulo: `${String(d.getHours()).padStart(2, '0')}h`,
          dia: `${SEMANA[d.getDay()]}, ${ddmm(ms)}`,
        });
        y += HORA_PX;
        hora = h;
        atual = null;
      }
      if (!atual || atual.itens.length === colunas) {
        atual = {
          tipo: 'grade',
          k: `g${chave(det)}`,
          y,
          altura: alturaTile,
          primeira: i,
          itens: [],
        };
        linhas.push(atual);
        y += alturaTile + vao;
      }
      atual.itens.push(det);
      linhaDe[i] = linhas.length - 1;
    }
    return { linhas, linhaDe, altura: y };
  });

  // --- rolagem ----------------------------------------------------------------

  // A vista em px da lista: `ini` é o que está logo abaixo da barra, `fim` o pé
  // da janela. Relida a cada quadro de rolagem.
  let rolagem = $state({ ini: 0, fim: 0 });
  let barraEl: HTMLDivElement | undefined;
  let barraH = $state(0);
  // O cabeçalho do App fica grudado no topo no desktop; a barra desta tela
  // gruda logo abaixo dele.
  let cabH = $state(0);

  function mede() {
    if (!listaEl) return;
    const topoLista = listaEl.getBoundingClientRect().top;
    const ini = cabH + barraH - topoLista;
    const fim = window.innerHeight - topoLista;
    if (ini !== rolagem.ini || fim !== rolagem.fim) rolagem = { ini, fim };
  }

  let pedido = 0;
  function rolou() {
    if (pedido) return;
    pedido = requestAnimationFrame(() => {
      pedido = 0;
      mede();
      confere();
    });
  }

  function confere() {
    if (!carregado || !filtro || erro) return;
    const tela = window.innerHeight;
    if (
      !fimAbaixo &&
      !buscandoAbaixo &&
      lista.length &&
      layout.altura - rolagem.fim < tela * BUSCA_TELAS
    ) {
      void abaixo();
    }
    if (!fimAcima && !buscandoAcima && rolagem.ini < tela * BUSCA_TELAS) void acima();
  }

  // O que vai para o DOM. A faixa anda em degraus de DEGRAU_PX, e não a cada
  // pixel: a lista de linhas só é refeita quando a rolagem cruza um degrau.
  const DEGRAU_PX = 200;
  const janela = $derived.by(() => {
    const sobra = window.innerHeight * DOM_TELAS;
    return {
      de: Math.floor((rolagem.ini - sobra) / DEGRAU_PX) * DEGRAU_PX,
      ate: Math.ceil((rolagem.fim + sobra) / DEGRAU_PX) * DEGRAU_PX,
    };
  });

  // Primeira linha cujo fim passa de `y`: busca binária, as linhas vêm em ordem.
  function primeiraAbaixo(y: number) {
    const l = layout.linhas;
    let lo = 0;
    let hi = l.length;
    while (lo < hi) {
      const m = (lo + hi) >> 1;
      const linha = l[m];
      if (linha && linha.y + linha.altura <= y) lo = m + 1;
      else hi = m;
    }
    return lo;
  }

  // A linha onde mora a detecção i da `lista`.
  function linhaDaDeteccao(i: number) {
    const li = layout.linhaDe[i];
    return li === undefined ? undefined : layout.linhas[li];
  }

  const vistas = $derived.by(() => {
    const { de, ate } = janela;
    const out: Linha[] = [];
    for (const linha of layout.linhas.slice(primeiraAbaixo(de))) {
      if (linha.y >= ate) break;
      out.push(linha);
    }
    return out;
  });

  // A primeira linha de miniaturas à vista: é ela que dá o dia e a hora da
  // barra, e o cabeçalho que gruda.
  const noTopo = $derived.by(() => {
    for (const linha of layout.linhas.slice(primeiraAbaixo(rolagem.ini + 1))) {
      if (linha.tipo !== 'grade') continue;
      const det = linha.itens[0];
      if (det) return { linha, det };
    }
    return null;
  });
  // Sem detecção à vista, vale o "ir para" - menos 1 ms, porque ele é o limite
  // exclusivo da busca: ir para o dia 29 busca antes da meia-noite do dia 30, e
  // mostrar o próprio limite fazia a barra piscar o dia 30 até a resposta chegar.
  const topoMs = $derived(noTopo?.det.instanteMs ?? (alvo != null ? alvo - 1 : Date.now()));

  // O cabeçalho da hora gruda enquanto as miniaturas dela passam. É um só,
  // fora da grade, porque o de dentro vai embora com a linha dele.
  const horaGrudada = $derived.by(() => {
    if (!noTopo || rolagem.ini <= 0) return null;
    const l = layout.linhas;
    for (let i = layout.linhaDe[noTopo.linha.primeira] ?? -1; i >= 0; i--) {
      const linha = l[i];
      if (linha?.tipo === 'hora') return linha.y + linha.altura <= rolagem.ini ? linha : null;
    }
    return null;
  });

  // A âncora é a detecção no topo da vista e a distância dela até lá. Trocar
  // o que está acima - página que chegou, topo solto, novas - mexe no `y` de
  // tudo; restaurar a âncora devolve a vista ao mesmo lugar.
  function ancora(): Ancora | null {
    if (!noTopo) return null;
    return { k: chave(noTopo.det), dy: noTopo.linha.y - rolagem.ini };
  }

  async function restaura(a: Ancora | null) {
    await tick();
    if (!a) return;
    const linha = linhaDaDeteccao(lista.findIndex((d) => chave(d) === a.k));
    if (!linha) return;
    const delta = linha.y - a.dy - rolagem.ini;
    if (Math.abs(delta) >= 1) window.scrollBy(0, delta);
    mede();
  }

  // Zoom e largura mudam a altura de tudo: a âncora segura a vista.
  async function mudaColunas(passo: number) {
    const a = ancora();
    colunasEscolhidas = Math.min(faixa.max, Math.max(faixa.min, colunas + passo));
    await restaura(a);
    confere();
  }

  // --- as miniaturas ----------------------------------------------------------

  // A proporção de cada câmera, aprendida da primeira imagem que chega. A
  // caixa do objeto é uma fração da IMAGEM, e numa câmera 4:3 dentro de uma
  // miniatura 16:9 a imagem não ocupa a miniatura inteira.
  let proporcoes = $state<Record<string, number>>({});

  function aprende(cam: string, img: HTMLImageElement) {
    const { naturalWidth: w, naturalHeight: h } = img;
    if (w && h && proporcoes[cam] !== w / h) proporcoes[cam] = w / h;
  }

  // Onde a imagem da câmera fica dentro da miniatura, em px.
  function retanguloDaImagem(cam: string) {
    const { larguraTile: W, alturaTile: H } = grade;
    const p = proporcoes[cam] ?? 16 / 9;
    const w = p >= W / H ? W : H * p;
    const h = p >= W / H ? W / p : H;
    return { x: (W - w) / 2, y: (H - h) / 2, w, h };
  }

  function areaDaImagem(cam: string) {
    const { x, y, w, h } = retanguloDaImagem(cam);
    return `left:${x}px;top:${y}px;width:${w}px;height:${h}px`;
  }

  // O tamanho do score na miniatura, o mesmo do CSS de `.score` (altura) e uma
  // folga para "0,84" (largura).
  const SCORE_ALTURA_PX = { normal: 13, compacta: 11 };
  const SCORE_LARGURA_PX = { normal: 30, compacta: 26 };

  // Em que lado de FORA da caixa o score cabe inteiro na miniatura: em cima,
  // senão embaixo, senão ao lado. Nunca por dentro, que cobre o objeto - a
  // cabeça de uma pessoa perto do topo. Sem lugar nenhum (caixa do tamanho da
  // imagem), fica sem score; ele está na folha.
  function ladoDoScore(cam: string, caixa: Caixa, compacta: boolean) {
    const { larguraTile: W, alturaTile: H } = grade;
    const img = retanguloDaImagem(cam);
    const borda = (v: number, total: number) => Math.min(1, Math.max(0, v)) * total;
    const x1 = img.x + Math.max(0, borda(caixa[0], img.w) - CAIXA_FOLGA_PX);
    const y1 = img.y + Math.max(0, borda(caixa[1], img.h) - CAIXA_FOLGA_PX);
    const x2 = img.x + Math.min(img.w, borda(caixa[2], img.w) + CAIXA_FOLGA_PX);
    const y2 = img.y + Math.min(img.h, borda(caixa[3], img.h) + CAIXA_FOLGA_PX);
    const h = compacta ? SCORE_ALTURA_PX.compacta : SCORE_ALTURA_PX.normal;
    const w = compacta ? SCORE_LARGURA_PX.compacta : SCORE_LARGURA_PX.normal;
    if (y1 >= h) return 'acima';
    if (y2 + h <= H) return 'abaixo';
    if (x2 + w <= W) return 'direita';
    if (x1 >= w) return 'esquerda';
    return null;
  }

  // As famílias de uma detecção, uma vez cada.
  function familiasDe(d: Deteccao) {
    const out: string[] = [];
    for (const o of d.objetos) if (!out.includes(o.familia)) out.push(o.familia);
    return out;
  }

  const score = (v: number) => v.toFixed(2).replace('.', ',');

  const pct = (v: number) => `${Math.min(1, Math.max(0, v)) * 100}%`;

  // A caixa com a mesma folga do vídeo e da folha (CAIXA_FOLGA_PX), parando na
  // borda da imagem. A folga é em px e a caixa em fração, então quem soma é o
  // CSS.
  const inicio = (v: number) => `max(0px, calc(${pct(v)} - ${CAIXA_FOLGA_PX}px))`;
  const tamanho = (de: number, ate: number) =>
    `calc(min(100%, calc(${pct(ate)} + ${CAIXA_FOLGA_PX}px)) - ${inicio(de)})`;

  // --- a folha ----------------------------------------------------------------

  let aberto = $state<string | null>(null);
  const iAberto = $derived(aberto ? lista.findIndex((d) => chave(d) === aberto) : -1);
  const detAberta = $derived(lista[iAberto]);

  // --- a barra ----------------------------------------------------------------

  let camerasAbertas = $state(false);
  let camsEl: HTMLDivElement | undefined;

  const marcada = (id: string) => camsMarcadas === null || camsMarcadas.includes(id);

  function alternaCamera(id: string) {
    const atuais = camsMarcadas ?? cameras.list.map((c) => c.id);
    const novas = atuais.includes(id) ? atuais.filter((x) => x !== id) : [...atuais, id];
    camsMarcadas = novas.length === cameras.list.length ? null : novas;
    void recomeca();
  }

  function todasCameras() {
    camsMarcadas = camsMarcadas === null ? [] : null;
    void recomeca();
  }

  function alternaFamilia(id: NomeDaFamilia) {
    const atuais = familiasMarcadas ?? ORDEM_FAMILIAS;
    const novas = atuais.includes(id) ? atuais.filter((x) => x !== id) : [...atuais, id];
    familiasMarcadas =
      novas.length === ORDEM_FAMILIAS.length
        ? null
        : ORDEM_FAMILIAS.filter((f) => novas.includes(f));
    void recomeca();
  }

  const nMarcadas = $derived(camsMarcadas?.length ?? cameras.list.length);

  function foraDasCameras(e: MouseEvent) {
    const alvoDoClique = e.target instanceof Element ? e.target : null;
    if (camerasAbertas && camsEl && !camsEl.contains(alvoDoClique)) camerasAbertas = false;
    if (menu && menuEl && !menuEl.contains(alvoDoClique) && !alvoDoClique?.closest('.mais'))
      menu = null;
  }

  // --- ocultar uma câmera daqui mesmo -----------------------------------------

  // O menu do ⋮ de uma miniatura. Um só, fora da grade e em `position: fixed`:
  // a miniatura corta o que passa da borda dela, e a linha pode sair do DOM
  // no meio da rolagem. Por isso mesmo ele fecha quando a tela rola.
  let menu = $state<{
    k: string;
    cam: string;
    instanteMs: number;
    x: number;
    y: number;
    paraCima: boolean;
  } | null>(null);
  let menuEl = $state<HTMLDivElement | null>(null);

  function abreMenu(d: Deteccao, ev: MouseEvent & { currentTarget: HTMLElement }) {
    if (menu?.k === chave(d)) {
      menu = null;
      return;
    }
    const r = ev.currentTarget.getBoundingClientRect();
    const paraCima = r.bottom + MENU_PX > window.innerHeight;
    menu = {
      k: chave(d),
      cam: d.cam,
      instanteMs: d.instanteMs,
      x: window.innerWidth - r.right,
      y: paraCima ? window.innerHeight - r.top : r.bottom,
      paraCima,
    };
  }

  // O aviso de desfazer, na snackbar: a miniatura some debaixo do dedo, e um
  // toque errado no meio da rolagem é fácil. Desfazer sai mais barato que
  // perguntar antes.
  let avisoDesfazer: number | undefined;

  // A âncora de `ancora()`, mas pulando a câmera que vai sair: a detecção que
  // segura a vista precisa continuar na lista depois do filtro.
  function ancoraSem(cam: string): Ancora | null {
    if (!noTopo) return null;
    for (let i = noTopo.linha.primeira; i < lista.length; i++) {
      const det = lista[i];
      const linha = linhaDaDeteccao(i);
      if (det && linha && det.cam !== cam) return { k: chave(det), dy: linha.y - rolagem.ini };
    }
    return null;
  }

  // Só desmarca a câmera no filtro desta tela, o mesmo do botão "câmeras". A
  // lista não recomeça: as detecções dela saem do que já está carregado e a
  // vista fica onde estava, que é o ponto de ocultar sem subir até a barra.
  async function ocultaCamera(cam: string) {
    menu = null;
    aberto = null;
    const antes = camsMarcadas;
    const restantes = (camsMarcadas ?? cameras.list.map((c) => c.id)).filter((x) => x !== cam);

    avisoDesfazer = avisar(
      {
        forte: nomes[cam] ?? cam,
        texto: 'oculta',
        acao: {
          rotulo: 'desfazer',
          fazer: () => {
            desfaz(antes);
          },
        },
      },
      DESFAZER_MS,
    );

    if (!restantes.length) {
      camsMarcadas = restantes;
      void recomeca();
      return;
    }
    const a = ancoraSem(cam);
    // A busca que estava no ar ainda traria a câmera: ela é descartada.
    geracao++;
    buscandoAcima = buscandoAbaixo = false;
    camsMarcadas = restantes;
    lista = lista.filter((d) => d.cam !== cam);
    await restaura(a);
    confere();
  }

  // Desfazer precisa buscar de novo: o que era da câmera já foi solto. A busca
  // parte da detecção do topo da vista, como um "ir para", e o que está acima
  // chega com a rolagem.
  function desfaz(antes: string[] | null) {
    camsMarcadas = antes;
    const ms = noTopo?.det.instanteMs;
    void recomeca(rolagem.ini > 0 && ms != null ? ms + 1 : null);
  }

  function tecla(e: KeyboardEvent) {
    if (e.key === 'Escape') menu = null;
  }

  // "Ir para": o dia vai ao fim dele, e o horário ao segundo digitado, os dois
  // incluídos - a detecção de 14:32:07.500 aparece quando se pede 14:32:07.
  const diaTopo = $derived(dayKey(new Date(topoMs)));

  function irParaDia(chaveDia: string) {
    const d = parseDay(chaveDia);
    d.setDate(d.getDate() + 1);
    void recomeca(chaveDia === dayKey() ? null : d.getTime());
  }

  function irParaHora(segundos: number) {
    void recomeca(parseDay(diaTopo).getTime() + (segundos + 1) * 1000);
  }

  // --- ciclo de vida ----------------------------------------------------------

  function redimensiona() {
    desktop = window.innerWidth >= DESKTOP_PX;
    rolou();
  }

  onMount(() => {
    // O header e a faixa de versão nova, quando ela aparece: os dois grudam no
    // topo, e a barra desta tela se encosta embaixo deles.
    const cab = document.querySelector<HTMLElement>('#app > .topo');
    const medeTudo = () => {
      cabH = cab?.offsetHeight ?? 0;
      barraH = barraEl?.offsetHeight ?? 0;
      largura = listaEl?.clientWidth ?? 0;
      rolou();
    };
    // Já agora, e não só quando o observador responder: a primeira página é
    // do tamanho da tela, e sem largura a conta dela sai errada.
    medeTudo();
    const ro = new ResizeObserver(medeTudo);
    if (cab) ro.observe(cab);
    if (barraEl) ro.observe(barraEl);
    if (listaEl) ro.observe(listaEl);

    void (async () => {
      if (!cameras.list.length) await loadCameras();
      camsMarcadas = idsDaURL(
        'cams',
        cameras.list.map((c) => c.id),
      );
      camerasResolvidas = true;
      await recomeca();
    })();

    const t = setInterval(() => {
      void novas();
    }, NOVAS_MS);
    return () => {
      ro.disconnect();
      clearInterval(t);
      fecharAviso(avisoDesfazer);
      cancelAnimationFrame(pedido);
      geracao++;
    };
  });
</script>

<svelte:window
  onscroll={() => {
    menu = null;
    rolou();
  }}
  onresize={redimensiona}
  onclick={foraDasCameras}
  onkeydown={tecla}
/>

<div class="tela" style:--cab-h="{cabH}px" style:--barra-h="{barraH}px">
  <div class="barra" bind:this={barraEl}>
    <div class="quando">
      <NavDia value={diaTopo} days={diasComDeteccao} oQue="detecção" onchange={irParaDia} />
      <Relogio ms={topoMs} onir={irParaHora} />
    </div>

    <div class="familias" role="group" aria-label="famílias">
      {#each ORDEM_FAMILIAS as f (f)}
        {@const on = familiasMarcadas === null || familiasMarcadas.includes(f)}
        <button
          class="ghost familia"
          class:on
          aria-pressed={on}
          aria-label={FAMILIAS[f].nome}
          title={on ? `ocultar ${FAMILIAS[f].nome}` : `mostrar ${FAMILIAS[f].nome}`}
          onclick={() => alternaFamilia(f)}
        >
          <img src={iconeURL(f, 14, FAMILIAS[f].cor)} alt="" width="14" height="14" />
          <span class="rotulo">{FAMILIAS[f].nome}</span>
        </button>
      {/each}
    </div>

    <div class="controles" role="group" aria-label="miniaturas">
      <button
        class="ghost"
        class:on={comCaixas}
        aria-pressed={comCaixas}
        aria-label="caixas"
        title={comCaixas ? 'esconder as caixas dos objetos' : 'mostrar as caixas dos objetos'}
        onclick={() => (comCaixas = !comCaixas)}
      >
        <Icone nome="caixas" tamanho={14} traco={2.4} />
        <span class="rotulo">caixas</span>
      </button>
      <button
        class="ghost"
        class:on={atualizando}
        aria-pressed={atualizando}
        aria-label="atualizar"
        title={atualizando
          ? 'parar de buscar sozinho as detecções novas'
          : `buscar sozinho as detecções novas, a cada ${NOVAS_MS / 1000} s`}
        onclick={alternaAtualizar}
      >
        <Icone nome="atualizar" tamanho={14} traco={2.4} />
        <span class="rotulo">atualizar</span>
      </button>
      <span class="spacer"></span>
      <button
        class="ghost zoom"
        onclick={() => mudaColunas(1)}
        disabled={colunas >= faixa.max}
        title="miniaturas menores"
        aria-label="miniaturas menores"
      >
        <Icone nome="menos" tamanho={18} />
      </button>
      <button
        class="ghost zoom"
        onclick={() => mudaColunas(-1)}
        disabled={colunas <= faixa.min}
        title="miniaturas maiores"
        aria-label="miniaturas maiores"
      >
        <Icone nome="mais" tamanho={18} />
      </button>
    </div>

    <div class="cams" bind:this={camsEl}>
      <!-- O ícone daqui tinha 16 de viewBox e traço 1.4: o 2.1 é o mesmo
           traço no viewBox 24 do componente. -->
      <BotaoCameras
        marcadas={nMarcadas}
        total={cameras.list.length}
        todas={camsMarcadas === null}
        aberto={camerasAbertas}
        onclick={() => (camerasAbertas = !camerasAbertas)}
        --traco="2.1"
      />
      {#if camerasAbertas}
        <div class="popover">
          <SeletorCameras
            cameras={cameras.list}
            {marcada}
            todas={camsMarcadas === null}
            ontoggle={alternaCamera}
            ontodas={todasCameras}
          />
        </div>
      {/if}
    </div>
  </div>

  {#if horaGrudada}
    <div class="grudada" aria-hidden="true">
      <div class="hora">
        <strong class="mono">{horaGrudada.rotulo}</strong>
        <span class="muted small">{horaGrudada.dia}</span>
      </div>
    </div>
  {/if}

  <div
    class="lista"
    class:sem-caixas={!comCaixas}
    style:height="{layout.altura}px"
    style:--cols={colunas}
    style:--vao="{grade.vao}px"
    bind:this={listaEl}
  >
    {#each vistas as l (l.k)}
      {#if l.tipo === 'hora'}
        <div class="hora" style:transform="translateY({l.y}px)">
          <strong class="mono">{l.rotulo}</strong>
          <span class="muted small">{l.dia}</span>
        </div>
      {:else if l.tipo === 'grade'}
        <div
          class="linha"
          class:compacta={colunas >= (desktop ? 7 : 4)}
          style:transform="translateY({l.y}px)"
          style:height="{l.altura}px"
        >
          {#each l.itens as d (chave(d))}
            <div class="celula">
              <button
                class="tile"
                onclick={() => (aberto = chave(d))}
                aria-label="abrir {hhmmss(d.instanteMs)}, {nomes[d.cam] ?? d.cam}"
              >
                <span class="imagem" style={areaDaImagem(d.cam)}>
                  {#if d.temQuadro}
                    <img
                      src={mediaURL.quadro(d.cam, d.instanteMs)}
                      alt=""
                      decoding="async"
                      onload={(e) => {
                        if (e.currentTarget instanceof HTMLImageElement)
                          aprende(d.cam, e.currentTarget);
                      }}
                    />
                  {/if}
                  {#each d.objetos as o, j (j)}
                    {#if o.caixa}
                      {@const lado = ladoDoScore(d.cam, o.caixa, colunas >= (desktop ? 7 : 4))}
                      <span
                        class="caixa"
                        style:left={inicio(o.caixa[0])}
                        style:top={inicio(o.caixa[1])}
                        style:width={tamanho(o.caixa[0], o.caixa[2])}
                        style:height={tamanho(o.caixa[1], o.caixa[3])}
                        style:border-color={familia(o.familia).cor}
                      >
                        {#if lado}
                          <span class="score mono {lado}" style:background={familia(o.familia).cor}
                            >{score(o.score)}</span
                          >
                        {/if}
                      </span>
                    {/if}
                  {/each}
                </span>
                <SeloDeFamilias familias={familiasDe(d)} />
                <span class="legenda">
                  <span class="mono">{hhmmss(d.instanteMs)}</span>
                  <span class="nome">{nomes[d.cam] ?? d.cam}</span>
                </span>
              </button>
              <button
                class="mais"
                onclick={(e: MouseEvent & { currentTarget: HTMLButtonElement }) => {
                  abreMenu(d, e);
                }}
                aria-label="opções da câmera {nomes[d.cam] ?? d.cam}"
                aria-haspopup="menu"
                aria-expanded={menu?.k === chave(d)}
                ><span aria-hidden="true"><Icone nome="menu" tamanho="100%" /></span></button
              >
            </div>
          {/each}
        </div>
      {/if}
    {/each}
  </div>

  {#if erro}
    <p class="empty">Não deu para buscar as detecções: {erro}</p>
  {:else if !carregado}
    <p class="empty">carregando…</p>
  {:else if !filtro}
    <p class="empty">
      Nenhuma {camsMarcadas?.length === 0 ? 'câmera marcada' : 'família marcada'}.
    </p>
  {:else if !lista.length && fimAbaixo && fimAcima}
    <p class="empty">Nenhuma detecção com esses filtros.</p>
  {:else if fimAbaixo && lista.length}
    <p class="fim muted small">Não há detecções mais antigas.</p>
  {:else if !fimAbaixo}
    <p class="fim muted small">carregando…</p>
  {/if}
</div>

{#if detAberta}
  {@const d = detAberta}
  {@const anterior = lista[iAberto + 1]}
  {@const proxima = lista[iAberto - 1]}
  {#key aberto}
    <DeteccaoFolha
      det={d}
      camera={nomes[d.cam] ?? d.cam}
      onclose={() => (aberto = null)}
      onanterior={anterior ? () => (aberto = chave(anterior)) : null}
      onproxima={proxima ? () => (aberto = chave(proxima)) : null}
      onocultar={() => {
        void ocultaCamera(d.cam);
      }}
    />
  {/key}
{/if}

{#if menu}
  {@const m = menu}
  <div
    class="menu"
    role="menu"
    bind:this={menuEl}
    style:right="{menu.x}px"
    style:top={menu.paraCima ? null : `${menu.y + 4}px`}
    style:bottom={menu.paraCima ? `${menu.y + 4}px` : null}
  >
    {#if existe(menu.cam)}
      <a
        class="item"
        role="menuitem"
        href="#live?cams={encodeURIComponent(menu.cam)}"
        onclick={() => (menu = null)}
      >
        Ir para o ao vivo de <strong>{nomes[menu.cam] ?? menu.cam}</strong>
      </a>
      <a
        class="item"
        role="menuitem"
        href={linkGravacoes(menu.cam, menu.instanteMs)}
        onclick={() => (menu = null)}
      >
        Abrir em Gravações
      </a>
    {/if}
    <button
      class="ghost"
      role="menuitem"
      onclick={() => {
        void ocultaCamera(m.cam);
      }}
    >
      Ocultar a câmera <strong>{nomes[menu.cam] ?? menu.cam}</strong>
    </button>
  </div>
{/if}

<style>
  .tela {
    padding: 0 10px 8px;
  }

  /* --- a barra --- */

  .barra {
    position: sticky;
    top: var(--cab-h);
    z-index: 10;
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: center;
    gap: 8px;
    margin: 0 -10px;
    padding: 10px;
    background: var(--bg);
    border-bottom: 1px solid var(--line);
  }

  .quando {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }

  /* No celular: dia, hora e câmeras na primeira linha; famílias e controles
     das miniaturas na segunda, todos só com o ícone. Com o nome escrito eles
     pedem ~470 px numa linha de 370. As três colunas deixam cada linha se
     dividir do seu jeito: a 1ª é a largura das famílias, a 3ª a das câmeras. */
  .quando {
    grid-column: 1 / 3;
    grid-row: 1;
  }
  .cams {
    grid-column: 3;
    grid-row: 1;
    position: relative;
  }
  .familias {
    grid-column: 1;
    grid-row: 2;
  }
  .controles {
    grid-column: 2 / 4;
    grid-row: 2;
  }
  .barra .rotulo {
    display: none;
  }

  .familias {
    display: flex;
    gap: 6px;
  }

  .familia {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 7px;
    padding: 8px 11px;
    color: var(--dim);
  }

  .familia.on {
    color: var(--fg);
    border-color: var(--accent);
  }
  .familia img {
    display: block;
    flex: none;
  }
  .familia:not(.on) img {
    opacity: 0.4;
  }

  /* Os controles das miniaturas têm a forma das famílias ao lado: o mesmo
     botão, o mesmo "ligado". */
  .controles {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .controles button {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 8px 11px;
    color: var(--dim);
  }

  .controles button.on {
    color: var(--fg);
    border-color: var(--accent);
  }

  .controles .zoom {
    justify-content: center;
    min-width: 40px;
    color: var(--fg);
  }

  .popover {
    position: absolute;
    top: calc(100% + 6px);
    right: 0;
    z-index: 40;
    width: min(300px, calc(100vw - 20px));
    display: grid;
    gap: 10px;
    padding: 12px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  }

  /* --- a grade --- */

  .lista {
    position: relative;
    /* As linhas são absolutas: a ancoragem de rolagem do navegador não teria
       em que se apoiar, e quem segura a vista é a âncora da tela. */
    overflow-anchor: none;
  }

  .hora {
    position: absolute;
    left: -10px;
    right: -10px;
    height: 40px;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 12px;
    background: var(--bg);
  }

  /* O cabeçalho que gruda logo abaixo da barra. Altura zero no fluxo: ele não
     empurra a grade, só se deita sobre ela. */
  .grudada {
    position: sticky;
    top: calc(var(--cab-h) + var(--barra-h));
    z-index: 5;
    height: 0;
  }

  .linha {
    position: absolute;
    left: 0;
    right: 0;
    display: grid;
    grid-template-columns: repeat(var(--cols), minmax(0, 1fr));
    gap: var(--vao);
  }

  .celula {
    position: relative;
    min-width: 0;
    height: 100%;
  }

  .tile {
    position: relative;
    display: block;
    width: 100%;
    height: 100%;
    min-height: 0;
    padding: 0;
    border: 0;
    border-radius: 8px;
    overflow: hidden;
    background: #000;
    text-align: left;
  }

  .imagem {
    position: absolute;
  }

  .imagem img {
    display: block;
    width: 100%;
    height: 100%;
  }

  .caixa {
    position: absolute;
    border: 2px solid;
    pointer-events: none;
  }

  /* O score do detector de objetos, colado na caixa e sempre do lado de fora:
     o lado vem de ladoDoScore. Os -2px e os 2px são a borda da caixa. */
  .score {
    position: absolute;
    padding: 0 3px;
    font:
      600 10px/13px system-ui,
      -apple-system,
      'Segoe UI',
      sans-serif;
    color: #0d1117;
    white-space: nowrap;
  }

  .score.acima {
    left: -2px;
    bottom: 100%;
    border-radius: 3px 3px 3px 0;
  }

  .score.abaixo {
    left: -2px;
    top: 100%;
    border-radius: 0 3px 3px 3px;
  }

  .score.direita {
    left: calc(100% + 2px);
    top: -2px;
    border-radius: 0 3px 3px 3px;
  }

  .score.esquerda {
    right: calc(100% + 2px);
    top: -2px;
    border-radius: 3px 0 3px 3px;
  }

  .compacta .score {
    font-size: 9px;
    line-height: 11px;
    padding: 0 2px;
  }

  .sem-caixas .caixa {
    display: none;
  }

  /* O ⋮ no canto, à direita dos ícones das famílias e com a mesma pastilha
     deles. A pastilha é pequena, mas o alvo do toque é o canto inteiro. */
  .mais {
    position: absolute;
    top: 0;
    right: 0;
    min-height: 0;
    width: 40px;
    height: 36px;
    padding: 4px 4px 0 0;
    border: 0;
    background: none;
    display: flex;
    justify-content: flex-end;
    align-items: flex-start;
  }

  .mais span {
    display: block;
    width: 22px;
    height: 20px;
    padding: 2px;
    border-radius: 6px;
    background: rgba(13, 17, 23, 0.8);
    color: var(--fg);
  }

  .mais:focus-visible {
    outline: none;
  }
  .mais:focus-visible span,
  .mais[aria-expanded='true'] span {
    outline: 2px solid var(--accent);
  }

  /* O selo das famílias abre espaço para o ⋮ à direita dele. */
  .celula :global(.selo) {
    right: 30px;
  }

  /* Com mouse o ⋮ aparece só sobre a miniatura, e fica enquanto o menu dele
     está aberto. No toque não há hover: ele fica sempre à vista. */
  @media (hover: hover) {
    .mais {
      opacity: 0;
    }
    .celula:hover .mais,
    .mais:focus-visible,
    .mais[aria-expanded='true'] {
      opacity: 1;
    }
    .celula:hover .mais span {
      background: rgba(13, 17, 23, 0.95);
    }
  }

  /* Na miniatura pequena o ⋮ não se acerta com o dedo: a ação fica na folha. */
  .compacta .mais {
    display: none;
  }
  .compacta :global(.selo) {
    right: 4px;
  }

  .menu {
    position: fixed;
    z-index: 40;
    max-width: calc(100vw - 20px);
    padding: 4px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  }

  .menu button,
  .menu .item {
    display: block;
    width: 100%;
    border: 0;
    text-align: left;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .menu .item {
    box-sizing: border-box;
    padding: 8px 10px;
    color: inherit;
    text-decoration: none;
  }

  @media (hover: hover) {
    .menu button:hover,
    .menu .item:hover {
      background: var(--panel-2);
    }
  }

  .legenda {
    position: absolute;
    left: 4px;
    bottom: 4px;
    max-width: calc(100% - 8px);
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 0 6px;
    border-radius: 6px;
    background: rgba(13, 17, 23, 0.8);
    font-size: 11px;
    line-height: 18px;
    color: var(--fg);
  }

  .nome {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* Na miniatura pequena não cabe o nome da câmera: fica só a hora. */
  .compacta .nome {
    display: none;
  }
  .compacta .legenda {
    font-size: 10px;
    line-height: 16px;
    padding: 0 4px;
  }
  .compacta :global(.selo) {
    padding: 2px 3px;
  }
  .compacta :global(.selo img) {
    width: 10px;
    height: 10px;
  }

  .fim {
    text-align: center;
    padding: 16px 0;
  }

  @media (min-width: 720px) {
    .tela {
      padding: 0 18px 8px;
    }

    /* Do tablet para cima os nomes voltam, mas as duas linhas do celular
       continuam: no iPad em pé (820 px) a barra inteira não cabe numa linha
       só. */
    .barra {
      gap: 10px;
      margin: 0 -18px;
      padding: 10px 18px;
    }

    .barra .rotulo {
      display: inline;
    }

    .hora {
      left: -18px;
      right: -18px;
      padding: 0 20px;
    }
    .legenda {
      font-size: 12px;
      line-height: 20px;
    }
  }

  /* Linha única quando tudo cabe: a barra inteira mede ~1000 px. A folga
     cobre fonte do sistema um pouco mais larga que a medida. */
  @media (min-width: 1060px) {
    .barra {
      display: flex;
    }
    .quando {
      flex: none;
    }
    .controles {
      margin-left: auto;
    }
  }
</style>
