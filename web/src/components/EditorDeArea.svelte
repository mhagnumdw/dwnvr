<!--
  O editor de área: um polígono livre desenhado sobre o quadro de uma câmera.
  Cada toque põe um vértice, e tocar no primeiro fecha a área; arrastar um
  vértice ajusta.

  No celular ocupa a tela inteira: em pé, o quadro em cima e os controles
  embaixo; deitado, os controles numa coluna à direita. No desktop é um modal.

  Vem num chunk à parte (`import()`): quem não desenha área não o baixa. O
  cadastro da câmera (aba Áreas) o abre com o nome da área, as outras áreas da
  câmera em cinza, o "apagar área" e o "atualizar o quadro". Detecções o abre
  com as áreas da câmera em cinza e, para o admin, o "salvar na câmera", que
  pede o nome e grava a área no cadastro.
-->
<script lang="ts">
  import { onMount, tick, untrack } from 'svelte';
  import Icone from './Icone.svelte';
  import type { Caixa } from '../lib/api.ts';
  import { REGRAS, MIN_PONTOS, MAX_PONTOS, MAX_AREAS, pontosSVG } from '../lib/area.ts';
  import type { Ponto, Regra } from '../lib/area.ts';

  interface Props {
    camera: string;
    // O quadro de fundo: o JPEG de uma detecção, ou o MP4 de um quadro só do
    // keyframe de um segmento. Nulo enquanto a tela ainda procura qual.
    fundo: { tipo: 'imagem' | 'video'; url: string } | null;
    // As caixas da detecção de fundo, só para orientar o desenho.
    caixas?: { caixa: Caixa; cor: string }[];
    inicial?: { pontos: Ponto[]; regra: Regra } | null;
    erro?: string;
    // Com string, o nome vira campo, obrigatório e diferente dos `nomesEmUso`.
    nome?: string | null;
    nomesEmUso?: string[];
    // As outras áreas da câmera, em cinza: para não desenhar uma por cima da
    // outra sem querer.
    outras?: { name: string; points: Ponto[] }[];
    // De quando é o quadro de fundo, e como pedir um mais novo.
    quadro?: { legenda: string; atualizando: boolean; onatualizar: () => void } | null;
    titulo?: string;
    rotuloAplicar?: string;
    onaplicar: (pontos: Ponto[], regra: Regra, nome: string) => void;
    // O "salvar na câmera": pede o nome e grava. Rejeita com a mensagem que o
    // editor mostra. `cheia` é a câmera que já tem MAX_AREAS áreas.
    salvar?: {
      cheia: boolean;
      onsalvar: (pontos: Ponto[], regra: Regra, nome: string) => Promise<void>;
    } | null;
    onapagar?: (() => void) | null;
    onfechar: () => void;
  }

  let {
    camera,
    fundo,
    caixas = [],
    inicial = null,
    erro = '',
    nome = null,
    nomesEmUso = [],
    outras = [],
    quadro = null,
    titulo = 'Área',
    rotuloAplicar = 'aplicar',
    onaplicar,
    salvar = null,
    onapagar = null,
    onfechar,
  }: Props = $props();

  // O alvo do dedo em volta de um vértice, em px da tela: 22 é a metade do
  // alvo de toque de 44 do app.css.
  const RAIO_DO_TOQUE_PX = 22;
  // A mesma quebra do resto da interface.
  const DESKTOP_PX = 720;

  // O editor nasce com o valor inicial e não o segue.
  const ini = untrack(() => inicial);
  let pontos = $state<Ponto[]>(ini ? ini.pontos.map((p): Ponto => [p[0], p[1]]) : []);
  let fechada = $state(!!ini && ini.pontos.length >= MIN_PONTOS);
  let regra = $state<Regra>(ini?.regra ?? 'foot');
  // O nome é campo desde o começo no cadastro, e só depois do "salvar na
  // câmera" em Detecções.
  const nomeDesdeOComeco = untrack(() => nome !== null);
  let pedindoNome = $state(false);
  const comNome = $derived(nomeDesdeOComeco || pedindoNome);
  let salvando = $state(false);
  let erroAoSalvar = $state('');
  let nomeAtual = $state(untrack(() => nome ?? ''));
  let historico = $state<{ pontos: Ponto[]; fechada: boolean }[]>([]);
  let arrasto: number | null = null;

  let modo = $state<'retrato' | 'deitado' | 'desktop'>('retrato');
  function mede() {
    const w = window.innerWidth;
    const h = window.innerHeight;
    modo = w >= DESKTOP_PX && h >= 520 ? 'desktop' : w > h ? 'deitado' : 'retrato';
  }

  // O palco tem a proporção do quadro, e cabe inteiro no espaço que sobra.
  let proporcao = $state(16 / 9);
  let espaco = $state({ w: 0, h: 0 });
  const palco = $derived.by(() => {
    const { w, h } = espaco;
    if (!w || !h) return { w: 0, h: 0 };
    const pw = Math.min(w, h * proporcao);
    return { w: pw, h: pw / proporcao };
  });

  const desenhada = $derived(fechada && pontos.length >= MIN_PONTOS);
  // A mesma régua do servidor (config.confereAreas): sem distinguir maiúscula.
  const erroDoNome = $derived.by(() => {
    if (!comNome) return '';
    const n = nomeAtual.trim().toLowerCase();
    if (!n) return 'Dê um nome à área.';
    if (nomesEmUso.some((u) => u.trim().toLowerCase() === n))
      return 'Já existe uma área com esse nome.';
    return '';
  });
  const valida = $derived(desenhada && !erroDoNome);

  let svg = $state<SVGSVGElement | null>(null);

  function ponto(e: PointerEvent): Ponto {
    const r = svg?.getBoundingClientRect();
    if (!r?.width || !r.height) return [0, 0];
    const lim = (v: number) => Math.min(1, Math.max(0, v));
    return [lim((e.clientX - r.left) / r.width), lim((e.clientY - r.top) / r.height)];
  }

  // O vértice mais perto do toque, dentro do raio, em px da tela.
  function perto(p: Ponto) {
    let melhor = -1;
    let dist = RAIO_DO_TOQUE_PX;
    for (const [i, q] of pontos.entries()) {
      const d = Math.hypot((q[0] - p[0]) * palco.w, (q[1] - p[1]) * palco.h);
      if (d <= dist) {
        dist = d;
        melhor = i;
      }
    }
    return melhor;
  }

  function guarda() {
    historico.push({ pontos: pontos.map((p): Ponto => [p[0], p[1]]), fechada });
  }

  function desfaz() {
    const s = historico.pop();
    if (!s) return;
    pontos = s.pontos;
    fechada = s.fechada;
  }

  function limpa() {
    guarda();
    pontos = [];
    fechada = false;
  }

  function aperta(e: PointerEvent) {
    e.preventDefault();
    svg?.setPointerCapture(e.pointerId);
    const p = ponto(e);
    const i = perto(p);
    if (!fechada && i === 0 && pontos.length >= MIN_PONTOS) {
      guarda();
      fechada = true;
      return;
    }
    if (i >= 0) {
      guarda();
      arrasto = i;
      return;
    }
    if (!fechada && pontos.length < MAX_PONTOS) {
      guarda();
      pontos.push(p);
    }
  }

  function move(e: PointerEvent) {
    if (arrasto !== null) pontos[arrasto] = ponto(e);
  }

  function solta() {
    arrasto = null;
  }

  function aplica() {
    if (!valida) return;
    if (pedindoNome) void salva();
    else onaplicar(pontos, regra, nomeAtual.trim());
  }

  async function salva() {
    if (!salvar || salvando) return;
    salvando = true;
    erroAoSalvar = '';
    try {
      await salvar.onsalvar(pontos, regra, nomeAtual.trim());
    } catch (e) {
      erroAoSalvar = e instanceof Error ? e.message : String(e);
    } finally {
      salvando = false;
    }
  }

  let campoDoNome = $state<HTMLInputElement | null>(null);
  async function pedeNome() {
    pedindoNome = true;
    erroAoSalvar = '';
    await tick();
    campoDoNome?.focus();
  }

  function voltaDoNome() {
    pedindoNome = false;
    erroAoSalvar = '';
  }

  function tecla(e: KeyboardEvent) {
    if (e.key === 'Escape') onfechar();
  }

  const situacao = $derived(
    desenhada
      ? erroAoSalvar || erroDoNome || `${pontos.length} pontos`
      : pontos.length < MIN_PONTOS
        ? `Toque pelo menos ${MIN_PONTOS} pontos.`
        : 'Toque no ponto verde para fechar.',
  );

  // Onde vai o nome de uma área em cinza: a média dos vértices, que cai dentro
  // de qualquer área que não seja muito torta.
  const centro = (pts: Ponto[]): Ponto => [
    pts.reduce((s, p) => s + p[0], 0) / pts.length,
    pts.reduce((s, p) => s + p[1], 0) / pts.length,
  ];

  // O pé de cada caixa, com a regra `pisa`: o meio da borda de baixo.
  const pe = (c: Caixa): Ponto => [(c[0] + c[2]) / 2, Math.min(1, c[3])];

  onMount(() => {
    mede();
    // A tela de trás não rola por baixo do editor.
    const antes = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = antes;
    };
  });
</script>

<svelte:window onresize={mede} onkeydown={tecla} />

<div class="editor {modo}" role="dialog" aria-modal="true" aria-label={titulo}>
  <div class="janela">
    <div class="topo">
      <button class="ghost" onclick={onfechar} aria-label="cancelar">
        <Icone nome="fechar" tamanho={16} />
      </button>
      <b>{titulo}</b>
      <span class="muted small camera">{camera}</span>
    </div>

    <div class="espaco" bind:clientWidth={espaco.w} bind:clientHeight={espaco.h}>
      <div class="palco" style:width="{palco.w}px" style:height="{palco.h}px">
        {#if fundo?.tipo === 'imagem'}
          <img
            src={fundo.url}
            alt=""
            onload={(e) => {
              const el = e.currentTarget;
              if (el instanceof HTMLImageElement && el.naturalWidth && el.naturalHeight)
                proporcao = el.naturalWidth / el.naturalHeight;
            }}
          />
        {:else if fundo?.tipo === 'video'}
          <video
            src={fundo.url}
            muted
            playsinline
            preload="auto"
            onloadeddata={(e) => {
              const { videoWidth: w, videoHeight: h } = e.currentTarget;
              if (w && h) proporcao = w / h;
            }}
          ></video>
        {/if}
        {#if !fundo}<span class="aviso muted small">{erro || 'buscando um quadro da câmera…'}</span
          >{/if}
        <svg
          bind:this={svg}
          viewBox="0 0 {palco.w || 1} {palco.h || 1}"
          onpointerdown={aperta}
          onpointermove={move}
          onpointerup={solta}
          onpointercancel={solta}
          role="application"
          aria-label="toque para pôr os pontos da área"
        >
          {#each outras as o, i (i)}
            {@const [cx, cy] = centro(o.points)}
            <polygon
              points={pontosSVG(o.points.map(([x, y]): Ponto => [x * palco.w, y * palco.h]))}
              class="outra"
            />
            <text x={cx * palco.w} y={cy * palco.h} class="rotulo">{o.name}</text>
          {/each}
          {#each caixas as c, i (i)}
            {@const [x1, y1, x2, y2] = c.caixa}
            <rect
              x={x1 * palco.w}
              y={y1 * palco.h}
              width={(x2 - x1) * palco.w}
              height={(y2 - y1) * palco.h}
              class="caixa"
              style:stroke={c.cor}
            />
            {#if regra === 'foot'}
              {@const [px, py] = pe(c.caixa)}
              <circle cx={px * palco.w} cy={py * palco.h} r="4" style:fill={c.cor} />
            {/if}
          {/each}
          {#if pontos.length}
            {@const pts = pontosSVG(pontos.map(([x, y]): Ponto => [x * palco.w, y * palco.h]))}
            {#if fechada}
              <polygon points={pts} class="area" />
            {:else}
              <polyline points={pts} class="traco" />
              {#if pontos.length >= 2}
                {@const a = pontos[pontos.length - 1] ?? [0, 0]}
                {@const b = pontos[0] ?? [0, 0]}
                <line
                  x1={a[0] * palco.w}
                  y1={a[1] * palco.h}
                  x2={b[0] * palco.w}
                  y2={b[1] * palco.h}
                  class="volta"
                />
              {/if}
            {/if}
            {#each pontos as [x, y], i (i)}
              {@const primeiro = !fechada && i === 0 && pontos.length >= MIN_PONTOS}
              <circle
                cx={x * palco.w}
                cy={y * palco.h}
                r={primeiro ? 11 : 8}
                class="vertice"
                class:primeiro
              />
            {/each}
          {/if}
        </svg>
      </div>
    </div>

    <div class="controles">
      <div class="titulo">
        <b>{titulo}</b>
        <span class="muted small camera">{camera}</span>
      </div>
      {#if quadro}
        <div class="quadro small">
          <span class="muted">{quadro.legenda}</span>
          <button class="ghost" onclick={quadro.onatualizar} disabled={quadro.atualizando}>
            {quadro.atualizando ? 'buscando…' : 'atualizar o quadro'}
          </button>
        </div>
      {/if}
      {#if comNome}
        <label class="nome">
          Nome
          <input
            bind:this={campoDoNome}
            bind:value={nomeAtual}
            oninput={() => (erroAoSalvar = '')}
            maxlength="40"
            placeholder="Portão, Garagem…"
            onkeydown={(e) => {
              if (e.key === 'Enter') aplica();
            }}
          />
        </label>
      {/if}
      <div class="regras" role="radiogroup" aria-label="quando o objeto conta como dentro">
        {#each Object.entries(REGRAS) as [id, r] (id)}
          <button
            role="radio"
            aria-checked={regra === id}
            class:on={regra === id}
            onclick={() => (regra = id as Regra)}>{r.nome}</button
          >
        {/each}
      </div>
      <p class="dica regra">{REGRAS[regra].dica}</p>
      <p
        class="situacao small"
        class:ok={valida && !erroAoSalvar}
        class:erro={desenhada && (!!erroDoNome || !!erroAoSalvar)}
      >
        {situacao}
      </p>
      <div class="acoes">
        <button class="ghost" onclick={desfaz} disabled={!historico.length}>desfazer</button>
        <button class="ghost" onclick={limpa} disabled={!pontos.length}>limpar</button>
        {#if onapagar}
          <button class="ghost danger" onclick={onapagar}>apagar área</button>
        {/if}
        <span class="spacer"></span>
        {#if pedindoNome}
          <button class="ghost" onclick={voltaDoNome} disabled={salvando}>voltar</button>
          <button class="primary" onclick={aplica} disabled={!valida || salvando}>
            {salvando ? 'salvando…' : 'salvar área'}
          </button>
        {:else}
          {#if salvar}
            <button
              class="ghost"
              onclick={pedeNome}
              disabled={!desenhada || salvar.cheia}
              title={salvar.cheia
                ? `a câmera já tem ${MAX_AREAS} áreas`
                : 'grava a área no cadastro da câmera, com um nome'}>salvar na câmera</button
            >
          {/if}
          <button class="primary" onclick={aplica} disabled={!valida}>{rotuloAplicar}</button>
        {/if}
      </div>
      <p class="dica">
        Toque para pôr um ponto; toque no primeiro para fechar. Arraste um ponto para ajustar.
      </p>
    </div>
  </div>
</div>

<style>
  .editor {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: flex;
    background: var(--bg);
  }

  .janela {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }

  .topo {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    border-bottom: 1px solid var(--line);
  }
  .topo button {
    min-height: 40px;
    padding: 6px 10px;
  }
  .camera {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .espaco {
    position: relative;
    flex: 1;
    min-height: 0;
    display: grid;
    place-items: center;
    background: #000;
  }

  .palco {
    position: relative;
    touch-action: none;
  }
  .palco img,
  .palco video {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    display: block;
    user-select: none;
    pointer-events: none;
  }
  .aviso {
    position: absolute;
    inset: 0;
    display: grid;
    place-items: center;
    padding: 12px;
    text-align: center;
  }
  svg {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    touch-action: none;
    cursor: crosshair;
  }

  .caixa {
    fill: none;
    stroke-width: 1.5;
    opacity: 0.8;
  }
  .area {
    fill: rgba(47, 129, 247, 0.28);
    stroke: var(--accent);
    stroke-width: 2.5;
    stroke-linejoin: round;
  }
  .traco {
    fill: none;
    stroke: var(--accent);
    stroke-width: 2.5;
  }
  .volta {
    stroke: var(--accent);
    stroke-width: 1.5;
    stroke-dasharray: 5 5;
    opacity: 0.7;
  }
  .outra {
    fill: none;
    stroke: #9aa3ad;
    stroke-width: 1.5;
    stroke-dasharray: 6 4;
    opacity: 0.8;
  }
  .rotulo {
    fill: #fff;
    font-size: 12px;
    text-anchor: middle;
    dominant-baseline: middle;
    paint-order: stroke;
    stroke: rgba(0, 0, 0, 0.7);
    stroke-width: 3;
    pointer-events: none;
  }
  .vertice {
    fill: var(--accent);
    stroke: #fff;
    stroke-width: 2;
  }
  .vertice.primeiro {
    fill: var(--ok);
  }

  .controles {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 12px;
    border-top: 1px solid var(--line);
  }
  .titulo {
    display: none;
  }

  .regras {
    display: inline-flex;
    align-self: flex-start;
    border: 1px solid var(--line);
    border-radius: 8px;
    overflow: hidden;
  }
  .regras button {
    min-height: 40px;
    padding: 6px 16px;
    border: 0;
    border-radius: 0;
    background: transparent;
    color: var(--dim);
  }
  .regras button.on {
    background: var(--ativo-fundo);
    color: var(--fg);
    box-shadow: inset 0 0 0 1px var(--accent);
  }

  .dica,
  .situacao {
    margin: 0;
    font-size: 12px;
    line-height: 1.4;
    color: var(--dim);
  }
  .situacao {
    font-size: 14px;
  }
  /* As duas dicas têm tamanhos diferentes: a altura de duas linhas fica
     reservada, e trocar a regra não mexe no quadro nem nos botões. */
  .dica.regra {
    min-height: calc(2 * 1.4em);
  }
  .situacao.ok {
    color: var(--ok);
  }
  .situacao.erro {
    color: var(--warn);
  }

  .acoes {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .quadro {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .quadro button {
    min-height: 36px;
    padding: 4px 10px;
    margin-left: auto;
  }
  .nome {
    display: grid;
    gap: 5px;
    font-size: 13px;
    color: var(--dim);
  }
  .nome input {
    width: 100%;
    color: var(--fg);
    font-size: 15px;
  }

  /* Deitado: o quadro na altura toda, e os controles numa coluna à direita,
     com o título que saiu do topo. */
  .deitado .janela {
    flex-direction: row;
  }
  .deitado .topo {
    display: none;
  }
  .deitado .controles {
    flex: none;
    width: 220px;
    padding: 8px;
    gap: 8px;
    overflow: auto;
    border-top: 0;
    border-left: 1px solid var(--line);
  }
  .deitado .titulo {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .deitado .regras {
    align-self: stretch;
  }
  .deitado .regras button {
    flex: 1;
    padding: 6px 8px;
  }
  .deitado .acoes {
    flex-direction: column;
  }
  .deitado .spacer {
    display: none;
  }

  /* Desktop: um modal sobre a tela. */
  .desktop {
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.6);
  }
  .desktop .janela {
    flex: none;
    width: min(960px, calc(100vw - 40px));
    height: min(760px, calc(100vh - 40px));
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    overflow: hidden;
  }
  .desktop .controles {
    flex-direction: row;
    flex-wrap: wrap;
    align-items: center;
  }
  .desktop .acoes {
    margin-left: auto;
  }
  .desktop .situacao {
    min-width: 180px;
  }
  /* A dica da regra numa linha só dela: dividindo a linha com o nome, o
     tamanho dela mudava a largura do campo e empurrava os botões. */
  .desktop .dica:last-child,
  .desktop .dica.regra,
  .desktop .quadro {
    flex-basis: 100%;
  }
  .desktop .dica.regra {
    min-height: 0;
  }
  .desktop .nome {
    flex: 1 1 220px;
  }
  /* Na base, junto ao campo do nome, e não no meio do rótulo + campo. */
  .desktop .regras {
    align-self: flex-end;
  }
</style>
