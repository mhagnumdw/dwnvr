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
  import Icon from './Icon.svelte';
  import type { Box } from '../lib/api.ts';
  import { RULES, MIN_POINTS, MAX_POINTS, MAX_AREAS, svgPoints } from '../lib/area.ts';
  import type { Point, Rule } from '../lib/area.ts';

  interface Props {
    camera: string;
    // O quadro de fundo: o JPEG de uma detecção, ou o MP4 de um quadro só do
    // keyframe de um segmento. Nulo enquanto a tela ainda procura qual.
    background: { tipo: 'imagem' | 'video'; url: string } | null;
    // As caixas da detecção de fundo, só para orientar o desenho.
    boxes?: { caixa: Box; color: string }[];
    initial?: { points: Point[]; rule: Rule } | null;
    error?: string;
    // Com string, o nome vira campo, obrigatório e diferente dos `nomesEmUso`.
    name?: string | null;
    namesInUse?: string[];
    // As outras áreas da câmera, em cinza: para não desenhar uma por cima da
    // outra sem querer.
    otherAreas?: { name: string; points: Point[] }[];
    // De quando é o quadro de fundo, e como pedir um mais novo.
    frame?: { caption: string; refreshing: boolean; onrefresh: () => void } | null;
    title?: string;
    applyLabel?: string;
    onapply: (points: Point[], rule: Rule, name: string) => void;
    // O "salvar na câmera": pede o nome e grava. Rejeita com a mensagem que o
    // editor mostra. `cheia` é a câmera que já tem MAX_AREAS áreas.
    save?: {
      full: boolean;
      onsave: (points: Point[], rule: Rule, name: string) => Promise<void>;
    } | null;
    ondelete?: (() => void) | null;
    onclose: () => void;
  }

  let {
    camera,
    background,
    boxes = [],
    initial = null,
    error = '',
    name = null,
    namesInUse = [],
    otherAreas = [],
    frame = null,
    title = 'Área',
    applyLabel = 'aplicar',
    onapply,
    save = null,
    ondelete = null,
    onclose,
  }: Props = $props();

  // O alvo do dedo em volta de um vértice, em px da tela: 22 é a metade do
  // alvo de toque de 44 do app.css.
  const TOUCH_RADIUS_PX = 22;
  // A mesma quebra do resto da interface.
  const DESKTOP_PX = 720;

  // O editor nasce com o valor inicial e não o segue.
  const ini = untrack(() => initial);
  let points = $state<Point[]>(ini ? ini.points.map((p): Point => [p[0], p[1]]) : []);
  let isClosed = $state(!!ini && ini.points.length >= MIN_POINTS);
  let rule = $state<Rule>(ini?.rule ?? 'foot');
  // O nome é campo desde o começo no cadastro, e só depois do "salvar na
  // câmera" em Detecções.
  const nameFromStart = untrack(() => name !== null);
  let askingName = $state(false);
  const withName = $derived(nameFromStart || askingName);
  let saving = $state(false);
  let saveError = $state('');
  let currentName = $state(untrack(() => name ?? ''));
  let undoStack = $state<{ points: Point[]; isClosed: boolean }[]>([]);
  let drag: number | null = null;

  let mode = $state<'portrait' | 'landscape' | 'desktop'>('portrait');
  function measure() {
    const w = window.innerWidth;
    const h = window.innerHeight;
    mode = w >= DESKTOP_PX && h >= 520 ? 'desktop' : w > h ? 'landscape' : 'portrait';
  }

  // O palco tem a proporção do quadro, e cabe inteiro no espaço que sobra.
  let ratio = $state(16 / 9);
  let space = $state({ w: 0, h: 0 });
  const stage = $derived.by(() => {
    const { w, h } = space;
    if (!w || !h) return { w: 0, h: 0 };
    const pw = Math.min(w, h * ratio);
    return { w: pw, h: pw / ratio };
  });

  const drawn = $derived(isClosed && points.length >= MIN_POINTS);
  // A mesma régua do servidor (config.confereAreas): sem distinguir maiúscula.
  const nameError = $derived.by(() => {
    if (!withName) return '';
    const n = currentName.trim().toLowerCase();
    if (!n) return 'Dê um nome à área.';
    if (namesInUse.some((u) => u.trim().toLowerCase() === n))
      return 'Já existe uma área com esse nome.';
    return '';
  });
  const validate = $derived(drawn && !nameError);

  let svg = $state<SVGSVGElement | null>(null);

  function point(e: PointerEvent): Point {
    const r = svg?.getBoundingClientRect();
    if (!r?.width || !r.height) return [0, 0];
    const lim = (v: number) => Math.min(1, Math.max(0, v));
    return [lim((e.clientX - r.left) / r.width), lim((e.clientY - r.top) / r.height)];
  }

  // O vértice mais perto do toque, dentro do raio, em px da tela.
  function near(p: Point) {
    let best = -1;
    let dist = TOUCH_RADIUS_PX;
    for (const [i, q] of points.entries()) {
      const d = Math.hypot((q[0] - p[0]) * stage.w, (q[1] - p[1]) * stage.h);
      if (d <= dist) {
        dist = d;
        best = i;
      }
    }
    return best;
  }

  function snapshot() {
    undoStack.push({ points: points.map((p): Point => [p[0], p[1]]), isClosed });
  }

  function undo() {
    const s = undoStack.pop();
    if (!s) return;
    points = s.points;
    isClosed = s.isClosed;
  }

  function clear() {
    snapshot();
    points = [];
    isClosed = false;
  }

  function press(e: PointerEvent) {
    e.preventDefault();
    // O preventDefault segura o foco onde estava: sem soltá-lo, o campo do nome
    // seguiria focado, e no celular cada toque reabriria o teclado.
    if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
    svg?.setPointerCapture(e.pointerId);
    const p = point(e);
    const i = near(p);
    if (!isClosed && i === 0 && points.length >= MIN_POINTS) {
      snapshot();
      isClosed = true;
      return;
    }
    if (i >= 0) {
      snapshot();
      drag = i;
      return;
    }
    if (!isClosed && points.length < MAX_POINTS) {
      snapshot();
      points.push(p);
    }
  }

  function move(e: PointerEvent) {
    if (drag !== null) points[drag] = point(e);
  }

  function release() {
    drag = null;
  }

  function apply() {
    if (!validate) return;
    if (askingName) void saveIt();
    else onapply(points, rule, currentName.trim());
  }

  async function saveIt() {
    if (!save || saving) return;
    saving = true;
    saveError = '';
    try {
      await save.onsave(points, rule, currentName.trim());
    } catch (e) {
      saveError = e instanceof Error ? e.message : String(e);
    } finally {
      saving = false;
    }
  }

  let nameField = $state<HTMLInputElement | null>(null);
  async function askName() {
    askingName = true;
    saveError = '';
    await tick();
    nameField?.focus();
  }

  function nameBack() {
    askingName = false;
    saveError = '';
  }

  function keyHandler(e: KeyboardEvent) {
    if (e.key === 'Escape') onclose();
  }

  const statusText = $derived(
    drawn
      ? saveError || nameError || `${points.length} pontos`
      : points.length < MIN_POINTS
        ? `Toque pelo menos ${MIN_POINTS} pontos.`
        : 'Toque no ponto verde para fechar.',
  );

  // Onde vai o nome de uma área em cinza: a média dos vértices, que cai dentro
  // de qualquer área que não seja muito torta.
  const center = (pts: Point[]): Point => [
    pts.reduce((s, p) => s + p[0], 0) / pts.length,
    pts.reduce((s, p) => s + p[1], 0) / pts.length,
  ];

  // O pé de cada caixa, com a regra `pisa`: o meio da borda de baixo.
  const pe = (c: Box): Point => [(c[0] + c[2]) / 2, Math.min(1, c[3])];

  onMount(() => {
    measure();
    // A tela de trás não rola por baixo do editor.
    const before = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = before;
    };
  });
</script>

<svelte:window onresize={measure} onkeydown={keyHandler} />

<div class="editor {mode}" role="dialog" aria-modal="true" aria-label={title}>
  <div class="window">
    <div class="top">
      <button class="ghost" onclick={onclose} aria-label="cancelar">
        <Icon name="close" size={16} />
      </button>
      <b>{title}</b>
      <span class="muted small camera">{camera}</span>
    </div>

    <div class="space" bind:clientWidth={space.w} bind:clientHeight={space.h}>
      <div class="stage" style:width="{stage.w}px" style:height="{stage.h}px">
        {#if background?.tipo === 'imagem'}
          <img
            src={background.url}
            alt=""
            onload={(e) => {
              const el = e.currentTarget;
              if (el instanceof HTMLImageElement && el.naturalWidth && el.naturalHeight)
                ratio = el.naturalWidth / el.naturalHeight;
            }}
          />
        {:else if background?.tipo === 'video'}
          <video
            src={background.url}
            muted
            playsinline
            preload="auto"
            onloadeddata={(e) => {
              const { videoWidth: w, videoHeight: h } = e.currentTarget;
              if (w && h) ratio = w / h;
            }}
          ></video>
        {/if}
        {#if !background}<span class="notice muted small"
            >{error || 'buscando um quadro da câmera…'}</span
          >{/if}
        <svg
          bind:this={svg}
          viewBox="0 0 {stage.w || 1} {stage.h || 1}"
          onpointerdown={press}
          onpointermove={move}
          onpointerup={release}
          onpointercancel={release}
          role="application"
          aria-label="toque para pôr os pontos da área"
        >
          {#each otherAreas as o, i (i)}
            {@const [cx, cy] = center(o.points)}
            <polygon
              points={svgPoints(o.points.map(([x, y]): Point => [x * stage.w, y * stage.h]))}
              class="other"
            />
            <text x={cx * stage.w} y={cy * stage.h} class="label">{o.name}</text>
          {/each}
          {#each boxes as c, i (i)}
            {@const [x1, y1, x2, y2] = c.caixa}
            <rect
              x={x1 * stage.w}
              y={y1 * stage.h}
              width={(x2 - x1) * stage.w}
              height={(y2 - y1) * stage.h}
              class="box"
              style:stroke={c.color}
            />
            {#if rule === 'foot'}
              {@const [px, py] = pe(c.caixa)}
              <circle cx={px * stage.w} cy={py * stage.h} r="4" style:fill={c.color} />
            {/if}
          {/each}
          {#if points.length}
            {@const pts = svgPoints(points.map(([x, y]): Point => [x * stage.w, y * stage.h]))}
            {#if isClosed}
              <polygon points={pts} class="area" />
            {:else}
              <polyline points={pts} class="stroke" />
              {#if points.length >= 2}
                {@const a = points[points.length - 1] ?? [0, 0]}
                {@const b = points[0] ?? [0, 0]}
                <line
                  x1={a[0] * stage.w}
                  y1={a[1] * stage.h}
                  x2={b[0] * stage.w}
                  y2={b[1] * stage.h}
                  class="back"
                />
              {/if}
            {/if}
            {#each points as [x, y], i (i)}
              {@const first = !isClosed && i === 0 && points.length >= MIN_POINTS}
              <circle
                cx={x * stage.w}
                cy={y * stage.h}
                r={first ? 11 : 8}
                class="vertex"
                class:first
              />
            {/each}
          {/if}
        </svg>
      </div>
    </div>

    <div class="controls">
      <!-- Deitado, o topo some e o título vem para cá: o cancelar vem junto,
           ou o editor fica sem saída no celular. -->
      <div class="title">
        <button class="ghost" onclick={onclose} aria-label="cancelar">
          <Icon name="close" size={16} />
        </button>
        <b>{title}</b>
        <span class="muted small camera">{camera}</span>
      </div>
      {#if frame}
        <div class="frame small">
          <span class="muted">{frame.caption}</span>
          <button class="ghost" onclick={frame.onrefresh} disabled={frame.refreshing}>
            {frame.refreshing ? 'buscando…' : 'atualizar o quadro'}
          </button>
        </div>
      {/if}
      {#if withName}
        <label class="name">
          Nome
          <input
            bind:this={nameField}
            bind:value={currentName}
            oninput={() => (saveError = '')}
            maxlength="40"
            placeholder="Portão, Garagem…"
            onkeydown={(e) => {
              if (e.key === 'Enter') apply();
            }}
          />
        </label>
      {/if}
      <div class="rules" role="radiogroup" aria-label="quando o objeto conta como dentro">
        {#each Object.entries(RULES) as [id, r] (id)}
          <button
            role="radio"
            aria-checked={rule === id}
            class:on={rule === id}
            onclick={() => (rule = id as Rule)}
          >
            <Icon name={r.icon} size={20} />
            {r.nome}
          </button>
        {/each}
      </div>
      <p class="hint rule">{RULES[rule].hint}</p>
      <p
        class="status small"
        class:ok={validate && !saveError}
        class:error={drawn && (!!nameError || !!saveError)}
      >
        {statusText}
      </p>
      <div class="actions">
        <button class="ghost" onclick={undo} disabled={!undoStack.length}>desfazer</button>
        <button class="ghost" onclick={clear} disabled={!points.length}>limpar</button>
        {#if ondelete}
          <button class="ghost danger" onclick={ondelete}>apagar área</button>
        {/if}
        <span class="spacer"></span>
        {#if askingName}
          <button class="ghost" onclick={nameBack} disabled={saving}>voltar</button>
          <button class="primary" onclick={apply} disabled={!validate || saving}>
            {saving ? 'salvando…' : 'salvar área'}
          </button>
        {:else}
          {#if save}
            <button
              class="ghost"
              onclick={askName}
              disabled={!drawn || save.full}
              title={save.full
                ? `a câmera já tem ${MAX_AREAS} áreas`
                : 'grava a área no cadastro da câmera, com um nome'}>salvar na câmera</button
            >
          {/if}
          <button class="primary" onclick={apply} disabled={!validate}>{applyLabel}</button>
        {/if}
      </div>
      <p class="hint">
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

  .window {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }

  .top {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    border-bottom: 1px solid var(--line);
  }
  .top button,
  .title button {
    min-height: 40px;
    padding: 6px 10px;
  }
  .camera {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .space {
    position: relative;
    flex: 1;
    min-height: 0;
    display: grid;
    place-items: center;
    background: #000;
  }

  .stage {
    position: relative;
    touch-action: none;
  }
  .stage img,
  .stage video {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    display: block;
    user-select: none;
    pointer-events: none;
  }
  .notice {
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

  .box {
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
  .stroke {
    fill: none;
    stroke: var(--accent);
    stroke-width: 2.5;
  }
  .back {
    stroke: var(--accent);
    stroke-width: 1.5;
    stroke-dasharray: 5 5;
    opacity: 0.7;
  }
  .other {
    fill: none;
    stroke: #9aa3ad;
    stroke-width: 1.5;
    stroke-dasharray: 6 4;
    opacity: 0.8;
  }
  .label {
    fill: #fff;
    font-size: 12px;
    text-anchor: middle;
    dominant-baseline: middle;
    paint-order: stroke;
    stroke: rgba(0, 0, 0, 0.7);
    stroke-width: 3;
    pointer-events: none;
  }
  .vertex {
    fill: var(--accent);
    stroke: #fff;
    stroke-width: 2;
  }
  .vertex.first {
    fill: var(--ok);
  }

  .controls {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 12px;
    border-top: 1px solid var(--line);
  }
  .title {
    display: none;
  }

  .rules {
    display: inline-flex;
    align-self: flex-start;
    border: 1px solid var(--line);
    border-radius: 8px;
    overflow: hidden;
  }
  .rules button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 7px;
    min-height: 40px;
    padding: 6px 16px;
    border: 0;
    border-radius: 0;
    background: transparent;
    color: var(--dim);
  }
  .rules button.on {
    background: var(--ativo-fundo);
    color: var(--fg);
    box-shadow: inset 0 0 0 1px var(--accent);
  }

  .hint,
  .status {
    margin: 0;
    font-size: 12px;
    line-height: 1.4;
    color: var(--dim);
  }
  .status {
    font-size: 14px;
  }
  /* As duas dicas têm tamanhos diferentes: a altura de duas linhas fica
     reservada, e trocar a regra não mexe no quadro nem nos botões. */
  .hint.rule {
    min-height: calc(2 * 1.4em);
  }
  .status.ok {
    color: var(--ok);
  }
  .status.error {
    color: var(--warn);
  }

  .actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }

  .frame {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .frame button {
    min-height: 36px;
    padding: 4px 10px;
    margin-left: auto;
  }
  .name {
    display: grid;
    gap: 5px;
    font-size: 13px;
    color: var(--dim);
  }
  .name input {
    width: 100%;
    color: var(--fg);
    font-size: 15px;
  }

  /* Deitado: o quadro na altura toda, e os controles numa coluna à direita,
     com o título que saiu do topo. */
  .landscape .window {
    flex-direction: row;
  }
  .landscape .top {
    display: none;
  }
  .landscape .controls {
    flex: none;
    width: 220px;
    padding: 8px;
    gap: 8px;
    overflow: auto;
    border-top: 0;
    border-left: 1px solid var(--line);
  }
  /* Na coluna de altura fixa, o que não cabe tem de rolar, e não encolher: o
     flex encolhia primeiro, e o seletor da regra sumia (ele corta o que passa
     da borda) e a dica dela, de três linhas, vazava sobre a situação. */
  .landscape .controls > * {
    flex-shrink: 0;
  }
  .landscape .title {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .landscape .rules {
    align-self: stretch;
  }
  .landscape .rules button {
    flex: 1;
    padding: 6px 8px;
  }
  .landscape .actions {
    flex-direction: column;
  }
  .landscape .spacer {
    display: none;
  }

  /* Desktop: um modal sobre a tela. */
  .desktop {
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.6);
  }
  .desktop .window {
    flex: none;
    width: min(960px, calc(100vw - 40px));
    height: min(760px, calc(100vh - 40px));
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    overflow: hidden;
  }
  .desktop .controls {
    flex-direction: row;
    flex-wrap: wrap;
    align-items: center;
  }
  .desktop .actions {
    margin-left: auto;
  }
  .desktop .status {
    min-width: 180px;
  }
  /* A dica da regra numa linha só dela: dividindo a linha com o nome, o
     tamanho dela mudava a largura do campo e empurrava os botões. */
  .desktop .hint:last-child,
  .desktop .hint.rule,
  .desktop .frame {
    flex-basis: 100%;
  }
  .desktop .hint.rule {
    min-height: 0;
  }
  .desktop .name {
    flex: 1 1 220px;
  }
  /* Na base, junto ao campo do nome, e não no meio do rótulo + campo. */
  .desktop .rules {
    align-self: flex-end;
  }
</style>
