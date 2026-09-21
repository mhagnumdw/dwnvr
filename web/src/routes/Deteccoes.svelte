<script>
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
  import DayPicker from '../components/DayPicker.svelte';
  import Relogio from '../components/Relogio.svelte';
  import DeteccaoFolha from '../components/DeteccaoFolha.svelte';
  import { api, mediaURL } from '../lib/api.js';
  import { cameras, loadCameras } from '../lib/state.svelte.js';
  import { paramsAtuais, escrever } from '../lib/rota.svelte.js';
  import { FAMILIAS, familia, iconeURL } from '../lib/icones.js';
  import { hhmmss, ddmm, dayKey, parseDay } from '../lib/format.js';

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

  const ORDEM_FAMILIAS = Object.keys(FAMILIAS);

  // --- estado da URL ----------------------------------------------------------

  const params = paramsAtuais();

  const idsDaURL = (k, validos) =>
    params.has(k) ? params.getAll(k).filter((v) => validos.includes(v)) : null;

  // Nulo é "todas": é o padrão, e fica fora do link.
  let camsMarcadas = $state(null);
  let familiasMarcadas = $state(idsDaURL('familias', ORDEM_FAMILIAS));
  let colunasEscolhidas = $state(Number(params.get('cols')) || null);
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
  let lista = $state.raw([]);
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
  let alvo = null;
  // Invalida a resposta de uma busca que já não descreve a tela: filtro novo,
  // "ir para" novo.
  let geracao = 0;

  const chave = (d) => `${d.cam}|${d.instanteMs}`;

  const nomes = $derived(Object.fromEntries(cameras.list.map((c) => [c.id, c.name])));

  // O que vai no pedido. Nulo quando o filtro não deixou nada marcado, e aí nem
  // se pergunta ao servidor.
  const filtro = $derived.by(() => {
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
  function junta(a, b) {
    const vistos = new Set();
    const out = [];
    for (const d of [...a, ...b].sort((x, y) => y.instanteMs - x.instanteMs || (x.cam < y.cam ? -1 : 1))) {
      const k = chave(d);
      if (vistos.has(k)) continue;
      vistos.add(k);
      out.push(d);
    }
    return out;
  }

  async function recomeca(antes = null) {
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
      const r = await api.deteccoes({ ...filtro, antes: antes ?? undefined, limite: pagina() });
      if (g !== geracao) return;
      lista = r.deteccoes;
      fimAbaixo = r.fim;
      carregado = true;
      await tick();
      confere();
    } catch (e) {
      if (g === geracao) erro = e.message;
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
      if (g === geracao) erro = e.message;
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
      if (g === geracao) erro = e.message;
    } finally {
      if (g === geracao) buscandoAcima = false;
    }
    confere();
  }

  // As novas, e as atrasadas logo abaixo do topo. Só com o topo carregado: fora
  // dele, a rolagem para cima é que as traz.
  async function novas() {
    if (!atualizando || document.hidden || !carregado || !fimAcima || !filtro || buscandoAcima) return;
    const g = geracao;
    const topo = lista[0]?.instanteMs ?? Date.now();
    try {
      const r = await api.deteccoes({ ...filtro, depois: topo - ATRASO_MS, limite: 200 });
      if (g !== geracao || !r.deteccoes.length) return;
      // Quem está olhando o topo vê a novidade entrar; quem rolou para baixo
      // não pode ter a tela empurrada por ela.
      const a = rolagem.ini > 0 ? ancora() : null;
      lista = junta(r.deteccoes, lista);
      // Mais de 200 no intervalo: as mais novas ficaram de fora, e a rolagem
      // para cima as busca como faria depois de um "ir para".
      if (!r.fim) fimAcima = false;
      if (a) await restaura(a);
    } catch {
      // A próxima rodada tenta de novo; um soluço da rede não vira aviso.
    }
  }

  // Religar busca na hora: esperar a rodada seguinte pareceria que não pegou.
  function alternaAtualizar() {
    atualizando = !atualizando;
    if (atualizando) novas();
  }

  // --- a grade, calculada -----------------------------------------------------

  let listaEl;
  let largura = $state(0);

  const grade = $derived.by(() => {
    const vao = desktop ? 8 : 6;
    const larguraTile = Math.max(0, (largura - vao * (colunas - 1)) / colunas);
    return { vao, larguraTile, alturaTile: (larguraTile * 9) / 16 };
  });

  const chaveHora = (ms) => {
    const d = new Date(ms);
    return `${dayKey(d)}T${d.getHours()}`;
  };
  const SEMANA = ['dom', 'seg', 'ter', 'qua', 'qui', 'sex', 'sáb'];

  // As linhas na ordem da tela, cada uma com o `y` onde começa. `linhaDe[i]`
  // diz em que linha mora a detecção i, para achar a âncora depois de uma troca.
  const layout = $derived.by(() => {
    const { vao, alturaTile } = grade;
    const linhas = [];
    const linhaDe = new Array(lista.length);
    let y = 0;
    let hora = null;
    let atual = null;
    for (let i = 0; i < lista.length; i++) {
      const ms = lista[i].instanteMs;
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
        atual = { tipo: 'grade', k: `g${chave(lista[i])}`, y, altura: alturaTile, itens: [] };
        linhas.push(atual);
        y += alturaTile + vao;
      }
      atual.itens.push(i);
      linhaDe[i] = linhas.length - 1;
    }
    return { linhas, linhaDe, altura: y };
  });

  // --- rolagem ----------------------------------------------------------------

  // A vista em px da lista: `ini` é o que está logo abaixo da barra, `fim` o pé
  // da janela. Relida a cada quadro de rolagem.
  let rolagem = $state({ ini: 0, fim: 0 });
  let barraEl;
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
    if (!fimAbaixo && !buscandoAbaixo && lista.length && layout.altura - rolagem.fim < tela * BUSCA_TELAS) {
      abaixo();
    }
    if (!fimAcima && !buscandoAcima && rolagem.ini < tela * BUSCA_TELAS) acima();
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
  function primeiraAbaixo(y) {
    const l = layout.linhas;
    let lo = 0;
    let hi = l.length;
    while (lo < hi) {
      const m = (lo + hi) >> 1;
      if (l[m].y + l[m].altura <= y) lo = m + 1;
      else hi = m;
    }
    return lo;
  }

  const vistas = $derived.by(() => {
    const { de, ate } = janela;
    const l = layout.linhas;
    const out = [];
    for (let i = primeiraAbaixo(de); i < l.length && l[i].y < ate; i++) out.push(l[i]);
    return out;
  });

  // A primeira linha de miniaturas à vista: é ela que dá o dia e a hora da
  // barra, e o cabeçalho que gruda.
  const noTopo = $derived.by(() => {
    const l = layout.linhas;
    for (let i = primeiraAbaixo(rolagem.ini + 1); i < l.length; i++) {
      if (l[i].tipo === 'grade') return { linha: l[i], det: lista[l[i].itens[0]] };
    }
    return null;
  });
  const topoMs = $derived(noTopo?.det.instanteMs ?? alvo ?? Date.now());

  // O cabeçalho da hora gruda enquanto as miniaturas dela passam. É um só,
  // fora da grade, porque o de dentro vai embora com a linha dele.
  const horaGrudada = $derived.by(() => {
    if (!noTopo || rolagem.ini <= 0) return null;
    const l = layout.linhas;
    for (let i = layout.linhaDe[noTopo.linha.itens[0]]; i >= 0; i--) {
      if (l[i].tipo === 'hora') return l[i].y + l[i].altura <= rolagem.ini ? l[i] : null;
    }
    return null;
  });

  // A âncora é a detecção no topo da vista e a distância dela até lá. Trocar
  // o que está acima - página que chegou, topo solto, novas - mexe no `y` de
  // tudo; restaurar a âncora devolve a vista ao mesmo lugar.
  function ancora() {
    if (!noTopo) return null;
    return { k: chave(noTopo.det), dy: noTopo.linha.y - rolagem.ini };
  }

  async function restaura(a) {
    await tick();
    if (!a) return;
    const i = lista.findIndex((d) => chave(d) === a.k);
    if (i < 0) return;
    const y = layout.linhas[layout.linhaDe[i]].y;
    const delta = y - a.dy - rolagem.ini;
    if (Math.abs(delta) >= 1) window.scrollBy(0, delta);
    mede();
  }

  // Zoom e largura mudam a altura de tudo: a âncora segura a vista.
  async function mudaColunas(passo) {
    const a = ancora();
    colunasEscolhidas = Math.min(faixa.max, Math.max(faixa.min, colunas + passo));
    await restaura(a);
    confere();
  }

  // --- as miniaturas ----------------------------------------------------------

  // A proporção de cada câmera, aprendida da primeira imagem que chega. A
  // caixa do objeto é uma fração da IMAGEM, e numa câmera 4:3 dentro de uma
  // miniatura 16:9 a imagem não ocupa a miniatura inteira.
  let proporcoes = $state({});

  function aprende(cam, ev) {
    const { naturalWidth: w, naturalHeight: h } = ev.currentTarget;
    if (w && h && proporcoes[cam] !== w / h) proporcoes[cam] = w / h;
  }

  function areaDaImagem(cam) {
    const { larguraTile: W, alturaTile: H } = grade;
    const p = proporcoes[cam] ?? 16 / 9;
    const w = p >= W / H ? W : H * p;
    const h = p >= W / H ? W / p : H;
    return `left:${(W - w) / 2}px;top:${(H - h) / 2}px;width:${w}px;height:${h}px`;
  }

  // As famílias de uma detecção, uma vez cada, a mais importante primeiro.
  function familiasDe(d) {
    const out = [];
    for (const o of d.objetos) if (!out.includes(o.familia)) out.push(o.familia);
    return out.sort((a, b) => familia(a).prioridade - familia(b).prioridade);
  }

  const score = (v) => v.toFixed(2).replace('.', ',');

  const pct = (v) => `${Math.min(1, Math.max(0, v)) * 100}%`;

  // --- a folha ----------------------------------------------------------------

  let aberto = $state(null);
  const iAberto = $derived(aberto ? lista.findIndex((d) => chave(d) === aberto) : -1);

  // --- a barra ----------------------------------------------------------------

  let camerasAbertas = $state(false);
  let camsEl;

  const marcada = (id) => camsMarcadas === null || camsMarcadas.includes(id);

  function alternaCamera(id) {
    const atuais = camsMarcadas ?? cameras.list.map((c) => c.id);
    const novas = atuais.includes(id) ? atuais.filter((x) => x !== id) : [...atuais, id];
    camsMarcadas = novas.length === cameras.list.length ? null : novas;
    recomeca();
  }

  function todasCameras() {
    camsMarcadas = camsMarcadas === null ? [] : null;
    recomeca();
  }

  function alternaFamilia(id) {
    const atuais = familiasMarcadas ?? ORDEM_FAMILIAS;
    const novas = atuais.includes(id) ? atuais.filter((x) => x !== id) : [...atuais, id];
    familiasMarcadas = novas.length === ORDEM_FAMILIAS.length
      ? null
      : ORDEM_FAMILIAS.filter((f) => novas.includes(f));
    recomeca();
  }

  const nMarcadas = $derived(camsMarcadas?.length ?? cameras.list.length);

  function foraDasCameras(e) {
    if (camerasAbertas && camsEl && !camsEl.contains(e.target)) camerasAbertas = false;
  }

  // "Ir para": o dia vai ao fim dele, e o horário ao segundo digitado, os dois
  // incluídos - a detecção de 14:32:07.500 aparece quando se pede 14:32:07.
  const diaTopo = $derived(dayKey(new Date(topoMs)));

  function irParaDia(chaveDia) {
    const d = parseDay(chaveDia);
    d.setDate(d.getDate() + 1);
    recomeca(chaveDia === dayKey() ? null : d.getTime());
  }

  function irParaHora(segundos) {
    recomeca(parseDay(diaTopo).getTime() + (segundos + 1) * 1000);
  }

  // --- ciclo de vida ----------------------------------------------------------

  function redimensiona() {
    desktop = window.innerWidth >= DESKTOP_PX;
    rolou();
  }

  onMount(() => {
    const cab = document.querySelector('#app > header');
    const medeTudo = () => {
      cabH = cab?.offsetHeight ?? 0;
      barraH = barraEl.offsetHeight;
      largura = listaEl.clientWidth;
      rolou();
    };
    // Já agora, e não só quando o observador responder: a primeira página é
    // do tamanho da tela, e sem largura a conta dela sai errada.
    medeTudo();
    const ro = new ResizeObserver(medeTudo);
    if (cab) ro.observe(cab);
    ro.observe(barraEl);
    ro.observe(listaEl);

    (async () => {
      if (!cameras.list.length) await loadCameras();
      camsMarcadas = idsDaURL('cams', cameras.list.map((c) => c.id));
      recomeca();
    })();

    const t = setInterval(novas, NOVAS_MS);
    return () => {
      ro.disconnect();
      clearInterval(t);
      cancelAnimationFrame(pedido);
      geracao++;
    };
  });
</script>

<svelte:window onscroll={rolou} onresize={redimensiona} onclick={foraDasCameras} />

<div class="tela" style:--cab-h="{cabH}px" style:--barra-h="{barraH}px">
  <div class="barra" bind:this={barraEl}>
    <div class="quando">
      <DayPicker value={diaTopo} onchange={irParaDia} />
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
        <svg viewBox="0 0 16 16" aria-hidden="true"><rect x="2.5" y="4" width="11" height="8" rx="1" /></svg>
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
        <svg viewBox="0 0 16 16" aria-hidden="true">
          <path d="M13 8a5 5 0 1 1-1.5-3.6M13 2.5v2.5h-2.5" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
        <span class="rotulo">atualizar</span>
      </button>
      <span class="spacer"></span>
      <button
        class="ghost zoom"
        onclick={() => mudaColunas(1)}
        disabled={colunas >= faixa.max}
        title="miniaturas menores"
        aria-label="miniaturas menores">−</button
      >
      <button
        class="ghost zoom"
        onclick={() => mudaColunas(-1)}
        disabled={colunas <= faixa.min}
        title="miniaturas maiores"
        aria-label="miniaturas maiores">+</button
      >
    </div>

    <div class="cams" bind:this={camsEl}>
      <button
        class="ghost"
        onclick={() => (camerasAbertas = !camerasAbertas)}
        aria-expanded={camerasAbertas}
      >
        ☰ câmeras ({nMarcadas})
      </button>
      {#if camerasAbertas}
        <div class="popover">
          <div class="row">
            <button class="ghost" onclick={todasCameras}>
              {camsMarcadas === null ? 'limpar' : `todas (${cameras.list.length})`}
            </button>
          </div>
          <div class="lista-cams">
            {#each cameras.list as c (c.id)}
              <label class="row">
                <input type="checkbox" checked={marcada(c.id)} onchange={() => alternaCamera(c.id)} />
                <span>{c.name}</span>
              </label>
            {/each}
          </div>
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
      {:else}
        <div
          class="linha"
          class:compacta={colunas >= (desktop ? 7 : 4)}
          style:transform="translateY({l.y}px)"
          style:height="{l.altura}px"
        >
          {#each l.itens as i (chave(lista[i]))}
            {@const d = lista[i]}
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
                    onload={(e) => aprende(d.cam, e)}
                  />
                {/if}
                {#each d.objetos as o, j (j)}
                  {#if o.caixa}
                    <span
                      class="caixa"
                      style:left={pct(o.caixa[0])}
                      style:top={pct(o.caixa[1])}
                      style:width={pct(o.caixa[2] - o.caixa[0])}
                      style:height={pct(o.caixa[3] - o.caixa[1])}
                      style:border-color={familia(o.familia).cor}
                    >
                      <span
                        class="score mono"
                        class:dentro={o.caixa[1] < 0.1}
                        style:background={familia(o.familia).cor}>{score(o.score)}</span
                      >
                    </span>
                  {/if}
                {/each}
              </span>
              <span class="icones">
                {#each familiasDe(d) as f (f)}
                  <img src={iconeURL(f, 12, familia(f).cor)} alt="" width="12" height="12" />
                {/each}
              </span>
              <span class="legenda">
                <span class="mono">{hhmmss(d.instanteMs)}</span>
                <span class="nome">{nomes[d.cam] ?? d.cam}</span>
              </span>
            </button>
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

{#if iAberto >= 0}
  {@const d = lista[iAberto]}
  {#key aberto}
    <DeteccaoFolha
      det={d}
      camera={nomes[d.cam] ?? d.cam}
      onclose={() => (aberto = null)}
      onanterior={iAberto + 1 < lista.length ? () => (aberto = chave(lista[iAberto + 1])) : null}
      onproxima={iAberto > 0 ? () => (aberto = chave(lista[iAberto - 1])) : null}
    />
  {/key}
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
  .quando { grid-column: 1 / 3; grid-row: 1; }
  .cams { grid-column: 3; grid-row: 1; position: relative; }
  .familias { grid-column: 1; grid-row: 2; }
  .controles { grid-column: 2 / 4; grid-row: 2; }
  .barra .rotulo { display: none; }

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

  .familia.on { color: var(--fg); border-color: var(--accent); }
  .familia img { display: block; flex: none; }
  .familia:not(.on) img { opacity: 0.4; }

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

  .controles button.on { color: var(--fg); border-color: var(--accent); }

  .controles svg {
    width: 14px;
    height: 14px;
    flex: none;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.6;
  }

  .controles .zoom {
    justify-content: center;
    min-width: 40px;
    color: var(--fg);
    font-size: 17px;
    line-height: 1;
  }

  .cams > button { white-space: nowrap; }

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

  .lista-cams {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 6px 14px;
  }

  .lista-cams label { gap: 8px; cursor: pointer; padding: 4px 0; }
  .lista-cams span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

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

  /* O score do detector de objetos, colado no canto da caixa: em cima dela, ou
     por dentro quando a caixa encosta no topo e não sobra lugar. */
  .score {
    position: absolute;
    left: -2px;
    bottom: 100%;
    padding: 0 3px;
    border-radius: 3px 3px 3px 0;
    font: 600 10px/13px system-ui, -apple-system, 'Segoe UI', sans-serif;
    color: #0d1117;
    white-space: nowrap;
  }

  .score.dentro {
    bottom: auto;
    top: 0;
    left: 0;
    border-radius: 0 0 3px 0;
  }

  .compacta .score { font-size: 9px; line-height: 11px; padding: 0 2px; }

  .sem-caixas .caixa { display: none; }

  .icones {
    position: absolute;
    right: 4px;
    top: 4px;
    display: inline-flex;
    gap: 4px;
    padding: 3px 4px;
    border-radius: 6px;
    background: rgba(13, 17, 23, 0.8);
  }

  .icones img { display: block; }

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

  .nome { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  /* Na miniatura pequena não cabe o nome da câmera: fica só a hora. */
  .compacta .nome { display: none; }
  .compacta .legenda { font-size: 10px; line-height: 16px; padding: 0 4px; }
  .compacta .icones { padding: 2px 3px; }
  .compacta .icones img { width: 10px; height: 10px; }

  .fim {
    text-align: center;
    padding: 16px 0;
  }

  @media (min-width: 720px) {
    .tela { padding: 0 18px 8px; }

    /* Do tablet para cima os nomes voltam, mas as duas linhas do celular
       continuam: no iPad em pé (820 px) a barra inteira não cabe numa linha
       só. */
    .barra {
      gap: 10px;
      margin: 0 -18px;
      padding: 10px 18px;
    }

    .barra .rotulo { display: inline; }

    .hora { left: -18px; right: -18px; padding: 0 20px; }
    .legenda { font-size: 12px; line-height: 20px; }
  }

  /* Linha única quando tudo cabe: a barra inteira mede ~1000 px. A folga
     cobre fonte do sistema um pouco mais larga que a medida. */
  @media (min-width: 1060px) {
    .barra { display: flex; }
    .quando { flex: none; }
    .controles { margin-left: auto; }
  }
</style>
