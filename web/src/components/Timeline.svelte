<script lang="ts">
  import type { MarkedObject, Span } from '../lib/api.ts';
  import { context2d } from '../lib/canvas.ts';
  import { hhmm, hhmmss } from '../lib/format.ts';
  import { family, iconOf, drawIcon, CLASS_NAME } from '../lib/icons.ts';
  import type { IconName } from '../lib/icons.ts';

  interface Props {
    ranges?: Span[];
    events?: number[];
    objects?: MarkedObject[];
    dayStart?: number;
    dayEnd?: number;
    currentMs?: number;
    onseek?: (ms: number) => void;
    viewFrom?: number;
    viewTo?: number;
  }

  // As marcas de um mesmo instante, que viram UMA marca dividida.
  interface Group {
    t: number;
    marks: MarkedObject[];
    priority: number;
  }

  let {
    ranges = [], // [[inícioMs, fimMs], …] faixas com gravação
    // Instantes em que o gatilho viu movimento, crescentes. Vazio quer dizer
    // "não há o que mostrar" - seja porque a câmera está com a detecção
    // desligada, seja porque o dia foi quieto -, e nesse caso a faixa de calor
    // não é desenhada NEM ocupa altura: uma tira vazia permanente seria pior
    // que nada para quem não usa detecção.
    events = [],
    // As marcas que o detector confirmou: [{ instanteMs, familia, classe }, …],
    // em qualquer ordem. Uma marca é um INSTANTE - o do onset que a originou -,
    // e não um intervalo: o detector olha uma vez por chegada, então não sabe
    // quando o objeto saiu. Vazio segue a regra da faixa de calor: nem desenha
    // nem ocupa altura.
    objects = [],
    dayStart = 0,
    dayEnd = 0,
    currentMs = 0,
    onseek,
    // Janela visível: o único estado que o zoom e o arraste alteram. Sobe para
    // quem chama porque é ela que vai para a URL - o link precisa reproduzir o
    // zoom, não só o instante. Zero nos dois quer dizer "ainda não escolhida", e
    // é o que faz o efeito abaixo ancorar no dia inteiro.
    viewFrom = $bindable(0),
    viewTo = $bindable(0),
  }: Props = $props();

  let canvas: HTMLCanvasElement | undefined;
  let width = $state(0);
  let hoverMs = $state<number | null>(null);
  // A marca sob o ponteiro, quando ele está a um alvo dela: é o que a dica
  // descreve e o que o fio do ponteiro aponta.
  let hoverGroup = $state<Group | null>(null);

  const MIN_SPAN = 20_000; // 20s de zoom máximo

  // A PILHA, de cima para baixo: objeto, vídeo, movimento - cada altura quer
  // dizer uma coisa só. Os números da barra e da régua são os que ela sempre
  // teve; a faixa de calor entra por baixo e a de objeto por cima, cada uma só
  // quando há o que mostrar.
  const TOP_PX = 6;
  // Onde o ícone se apoia, acima da linha de objeto. Fixa, e não do tamanho do
  // ícone: o ícone de classe é maior que o de família, e com a zona crescendo a
  // barra desceria no meio de um zoom.
  const ZONE_PX = 24;
  const OBJ_PX = 10; // a linha de objeto
  const BAR_PX = 36;
  const VAO_PX = 3; // respiro entre a barra e cada faixa vizinha
  const HEAT_PX = 10;
  const RULER_PX = 20; // o que sobra embaixo, para os rótulos de hora

  // O desenho da marca de objeto: um tique na linha, e o ícone em cima SEMPRE
  // que couber. Uma câmera marca dezenas de objetos por dia, não centenas, e o
  // ícone cabe em boa parte do dia. O que não cabe fica só no tique - a cor já
  // diz a família.
  const TICK_PX = 3;
  const FAMILY_ICON_PX = 14;
  // A 20px o ícone de classe separa moto de bicicleta e gato de cachorro; a
  // 14px eles viram o mesmo desenho. Por isso a classe só entra de perto.
  const CLASS_ICON_PX = 20;
  const CLASS_UNTIL_MS = 20 * 60_000;
  const ICON_SLACK_PX = 4; // respiro entre dois ícones vizinhos
  // Distância em que o toque "cai" numa marca e vai ao instante dela, em vez de
  // ao pixel tocado. O dedo é impreciso, o mouse não - o mesmo raciocínio do
  // limiar de arraste lá embaixo.
  const MOUSE_TARGET_PX = 8;
  const FINGER_TARGET_PX = 16;
  const BACKGROUND = '#1b1f24';

  // A intensidade é ADAPTATIVA: o pico da janela visível vira o topo da
  // escala. A alternativa era escala fixa, comparável entre telas e entre
  // câmeras - perdeu porque numa câmera quieta ela pinta o dia inteiro do mesmo
  // cinza, que é justamente quando a faixa precisava estar dizendo alguma
  // coisa. O preço, assumido: a mesma cor quer dizer densidades diferentes em
  // zooms diferentes.
  //
  // O piso de alfa existe para que uma coluna com UMA marca ainda apareça; a
  // raiz abre o meio da escala, onde a diferença entre 2 e 4 marcas se perderia
  // se a rampa fosse linear.
  const HEAT_COLOR = '#d29922';
  const HEAT_ALPHA_MIN = 0.3;

  // Reancorar quando o dia muda; comparar os limites evita reancorar a cada
  // repintura e perder o zoom que o usuário acabou de dar. O piso do span faz o
  // mesmo papel para uma janela vinda da URL: o `zoomTo` nunca produz menos que
  // MIN_SPAN, mas um link editado à mão produz - e aí o zoom máximo, que é
  // regra desta timeline, passaria a valer só para quem chegou clicando.
  $effect(() => {
    const visibleWindow = viewTo - viewFrom;
    if (
      dayStart &&
      (viewFrom < dayStart || viewTo > dayEnd || viewTo === 0 || visibleWindow < MIN_SPAN)
    ) {
      viewFrom = dayStart;
      viewTo = dayEnd;
    }
  });

  const hasHeat = $derived(events.length > 0);
  const hasObjects = $derived(objects.length > 0);
  const objTop = TOP_PX + ZONE_PX;
  const barTop = $derived(hasObjects ? objTop + OBJ_PX + VAO_PX : TOP_PX);
  const canvasHeight = $derived(barTop + BAR_PX + (hasHeat ? VAO_PX + HEAT_PX : 0) + RULER_PX);

  // As marcas agrupadas por instante, crescentes. Uma olhada pode confirmar
  // mais de uma família no mesmo quadro - pessoa e moto chegam juntas, e numa
  // rua isso é comum -, e o grupo é
  // desenhado como UMA marca dividida, e não como duas que se tampam.
  // Dentro do grupo, a família de mais prioridade vem primeiro.
  //
  // A timeline marca chegada por família: duas pessoas no mesmo instante são
  // duas linhas gravadas, mas uma chegada de `pessoa`, com a de maior
  // confiança - a classe dela é a que o ícone e o tooltip mostram.
  const groups = $derived.by((): Group[] => {
    const out: { t: number; marks: MarkedObject[] }[] = [];
    for (const o of [...objects].sort((a, b) => a.instanteMs - b.instanteMs)) {
      const ult = out.at(-1);
      if (ult?.t !== o.instanteMs) {
        out.push({ t: o.instanteMs, marks: [o] });
        continue;
      }
      const i = ult.marks.findIndex((m) => m.familia === o.familia);
      const same = ult.marks[i];
      if (!same) ult.marks.push(o);
      else if (o.score > same.score) ult.marks[i] = o;
    }
    return out.map((gr) => {
      const priorities = gr.marks.map((m) => family(m.familia).priority);
      gr.marks.sort((a, b) => family(a.familia).priority - family(b.familia).priority);
      return { ...gr, priority: Math.min(...priorities) };
    });
  });

  const span = $derived(Math.max(1, viewTo - viewFrom));
  const toX = (ms: number) => ((ms - viewFrom) / span) * width;
  const toMs = (x: number) => viewFrom + (x / Math.max(1, width)) * span;

  // Passo das marcas de hora: escolhido para que os rótulos nunca se toquem,
  // seja num celular de 360px ou num monitor largo.
  const step = $derived.by(() => {
    const larger = 216e5;
    const opts = [1e3, 5e3, 15e3, 60e3, 3e5, 9e5, 18e5, 36e5, 108e5, larger];
    const minPx = width < 500 ? 56 : 78;
    return opts.find((s) => (s / span) * width >= minPx) ?? larger;
  });

  $effect(() => {
    // Redesenha sempre que qualquer uma destas mudar.
    /* eslint-disable @typescript-eslint/no-unused-expressions -- só a leitura, para o $effect rastrear */
    ranges;
    events;
    groups;
    viewFrom;
    viewTo;
    currentMs;
    width;
    hoverMs;
    canvasHeight;
    /* eslint-enable @typescript-eslint/no-unused-expressions */
    draw();
  });

  /* Quantas marcas caem em cada coluna de pixel da janela visível.
   *
   * Contar por coluna, e não desenhar marca a marca, é o que faz a faixa
   * aguentar o dia inteiro: a 1200px o dia dá 72 s por coluna, e um risco por
   * marca viraria um borrão sólido que não distingue "passou alguém" de
   * "choveu a tarde toda". A DENSIDADE é a informação; o risco solto não é.
   *
   * A busca binária evita percorrer as milhares de marcas do dia a cada
   * repintura - e há repintura a cada pixel de arraste. */
  function countPerColumn() {
    const cols = new Int32Array(Math.max(1, Math.ceil(width)));
    for (let i = lowerLimit(events, viewFrom); i < events.length; i++) {
      const e = events[i];
      if (e === undefined || e >= viewTo) break;
      const x = Math.floor(toX(e));
      if (x >= 0 && x < cols.length) cols[x] = (cols[x] ?? 0) + 1;
    }
    return cols;
  }

  function lowerLimit(arr: number[], v: number) {
    let lo = 0;
    let hi = arr.length;
    while (lo < hi) {
      const m = (lo + hi) >> 1;
      if ((arr[m] ?? v) < v) lo = m + 1;
      else hi = m;
    }
    return lo;
  }

  /* O tique de um grupo na linha de objeto. Duas famílias no mesmo instante
   * dividem o tique em faixas, a de mais prioridade no alto. */
  function tick(g: CanvasRenderingContext2D, gr: Group) {
    const x = toX(gr.t) - TICK_PX / 2;
    const h = OBJ_PX / gr.marks.length;
    gr.marks.forEach((m, i) => {
      g.fillStyle = family(m.familia).color;
      g.fillRect(x, objTop + i * h, TICK_PX, h);
    });
  }

  /* Onde cada ícone vai, sem que dois se sobreponham.
   *
   * Percorre por PRIORIDADE, e não por tempo: `pessoa` escolhe lugar antes de
   * todos, então um gato vizinho nunca tira o ícone de uma pessoa. Um grupo de
   * duas famílias pede lugar para os dois ícones lado a lado; sem espaço, tenta
   * ao menos o da família principal. O que não coube continua no tique. */
  function fit(visibleOnes: Group[], side: number, byClass: boolean) {
    const order = [...visibleOnes].sort((a, b) => a.priority - b.priority || a.t - b.t);
    const occupied: [number, number][] = [];
    const placements: { nome: IconName; x: number; color: string }[] = [];
    const free = (x0: number, x1: number) =>
      occupied.every(([a, b]) => x1 + ICON_SLACK_PX <= a || x0 >= b + ICON_SLACK_PX);
    for (const gr of order) {
      const attempts = gr.marks.length > 1 ? [gr.marks, gr.marks.slice(0, 1)] : [gr.marks];
      for (const marks of attempts) {
        const wid = marks.length * side + (marks.length - 1) * 2;
        const x0 = toX(gr.t) - wid / 2;
        if (!free(x0, x0 + wid)) continue;
        occupied.push([x0, x0 + wid]);
        marks.forEach((m, i) => {
          const name = iconOf(m, byClass);
          if (name)
            placements.push({ nome: name, x: x0 + i * (side + 2), color: family(m.familia).color });
        });
        break;
      }
    }
    return placements;
  }

  function drawObjects(g: CanvasRenderingContext2D) {
    g.fillStyle = BACKGROUND;
    g.fillRect(0, objTop, width, OBJ_PX);

    // Um pouco além das bordas, para o ícone de uma marca logo fora da janela
    // entrar pela metade em vez de pular para dentro de uma vez.
    const visibleOnes = groups.filter((gr) => {
      const x = toX(gr.t);
      return x > -CLASS_ICON_PX && x < width + CLASS_ICON_PX;
    });

    // A de menos prioridade primeiro: onde dois tiques caem no mesmo pixel, o
    // de `pessoa` é o último pintado, e fica por cima.
    for (const gr of [...visibleOnes].sort((a, b) => b.priority - a.priority)) tick(g, gr);

    const byClass = span <= CLASS_UNTIL_MS;
    const side = byClass ? CLASS_ICON_PX : FAMILY_ICON_PX;
    for (const p of fit(visibleOnes, side, byClass)) {
      drawIcon(g, p.nome, p.x, objTop - side - 2, side, p.color);
    }
  }

  /* A marca a até `alvo` px de x, ou null. No empate, a de mais prioridade. */
  function nearGroup(x: number, target: number) {
    let best: Group | null = null;
    let dist = Infinity;
    for (const gr of groups) {
      const d = Math.abs(toX(gr.t) - x);
      if (d > target) continue;
      if (d < dist || (d === dist && best && gr.priority < best.priority)) {
        best = gr;
        dist = d;
      }
    }
    return best;
  }

  function describe(gr: Group) {
    return gr.marks.map((m) => CLASS_NAME[m.classe] ?? m.classe).join(' + ');
  }

  function draw() {
    if (!canvas || !width) return;
    const dpr = window.devicePixelRatio || 1;
    const h = canvasHeight;
    canvas.width = width * dpr;
    canvas.height = h * dpr;

    const g = context2d(canvas);
    g.setTransform(dpr, 0, 0, dpr, 0, 0);

    const barH = BAR_PX;
    const heatTop = barTop + barH + VAO_PX;
    // O fio do cursor e o do ponteiro atravessam a pilha inteira: eles marcam
    // UM instante, e um instante vale igual nas três camadas.
    const stackTop = hasObjects ? objTop : barTop;
    const stackH = (hasHeat ? heatTop + HEAT_PX : barTop + barH) - stackTop;

    if (hasObjects) drawObjects(g);

    g.fillStyle = BACKGROUND;
    g.fillRect(0, barTop, width, barH);

    g.fillStyle = '#2f81f7';
    for (const [s, e] of ranges) {
      const x0 = Math.max(0, toX(s));
      const x1 = Math.min(width, toX(e));
      // Mínimo de 2px: uma gravação curta não pode desaparecer da barra só
      // porque o zoom está afastado.
      if (x1 > 0 && x0 < width) g.fillRect(x0, barTop, Math.max(2, x1 - x0), barH);
    }

    if (hasHeat) {
      const cols = countPerColumn();
      let ceiling = 1;
      for (const c of cols) if (c > ceiling) ceiling = c;

      g.fillStyle = BACKGROUND;
      g.fillRect(0, heatTop, width, HEAT_PX);
      g.fillStyle = HEAT_COLOR;
      for (let x = 0; x < cols.length; x++) {
        const c = cols[x];
        if (!c) continue;
        g.globalAlpha = HEAT_ALPHA_MIN + (1 - HEAT_ALPHA_MIN) * Math.sqrt(Math.min(1, c / ceiling));
        g.fillRect(x, heatTop, 1, HEAT_PX);
      }
      g.globalAlpha = 1;
    }

    g.fillStyle = '#8b949e';
    g.font = '10px system-ui, sans-serif';
    g.textBaseline = 'top';
    const first = Math.ceil(viewFrom / step) * step;
    for (let t = first; t < viewTo; t += step) {
      const x = toX(t);
      g.fillRect(x, h - 18, 1, 5);
      g.fillText(step >= 60e3 ? hhmm(t) : hhmmss(t), x + 3, h - 13);
    }

    if (hoverMs !== null) {
      const x = toX(hoverMs);
      g.fillStyle = 'rgba(230,237,243,.35)';
      g.fillRect(x, stackTop, 1, stackH);
    }

    if (currentMs) {
      const x = toX(currentMs);
      if (x >= -2 && x <= width + 2) {
        g.fillStyle = '#f85149';
        g.fillRect(x - 1.5, stackTop - 3, 3, stackH + 6);
      }
    }
  }

  // --- interação -----------------------------------------------------------
  //
  // Ponteiros ativos: um arrasta a janela, dois pinçam para dar zoom. Usar
  // Pointer Events cobre mouse, dedo e caneta com o mesmo código.
  // Cada ponteiro guarda também o x inicial: o limiar de arraste precisa do
  // deslocamento acumulado, não do passo do último evento.
  // eslint-disable-next-line svelte/prefer-svelte-reactivity -- estado do gesto, não da tela
  const pointers = new Map<number, { x: number; x0: number }>();
  let dragged = false;
  let pinchStart: {
    dist: number;
    from: number;
    to: number;
    mid: number;
    anchorMs: number;
    t: number;
    moved: boolean;
  } | null = null;
  let lastTap: { t: number; x: number } | null = null; // o último toque limpo, para o duplo toque
  let swallowUp = false; // o up do 2º dedo não pode virar seek

  const DOUBLE_TAP_MS = 300;
  const DOUBLE_TAP_PX = 32;
  const ZOOM_STEP = 3; // um gesto deliberado merece um salto maior que o da roda
  // Zoom da roda proporcional ao deltaY: um clique de mouse (~100px) dá o passo
  // de 1.35, e o touchpad, que manda dezenas de deltas pequenos, soma o mesmo
  // zoom na medida do movimento dos dedos em vez de um passo cheio por evento.
  const WHEEL_ZOOM_PER_PX = Math.log(1.35) / 100;
  const WHEEL_PX_PER_LINE = 33; // Firefox pode mandar o delta em linhas
  const WHEEL_DELTA_MAX_PX = 300; // um evento isolado nunca salta mais que ~2.5x nem ~¼ da tela

  function localX(ev: MouseEvent) {
    return ev.clientX - (canvas?.getBoundingClientRect().left ?? 0);
  }

  function onPointerDown(ev: PointerEvent) {
    canvas?.setPointerCapture(ev.pointerId);
    const x = localX(ev);
    pointers.set(ev.pointerId, { x, x0: x });
    dragged = false;
    const [a, b] = [...pointers.values()];
    if (pointers.size === 2 && a && b) {
      const mid = (a.x + b.x) / 2;
      pinchStart = {
        dist: Math.abs(a.x - b.x),
        from: viewFrom,
        to: viewTo,
        mid,
        anchorMs: toMs(mid), // o instante sob os dedos é o que fica parado
        t: ev.timeStamp,
        moved: false,
      };
      lastTap = null; // dois dedos nunca continuam um duplo toque
    }
  }

  function onPointerMove(ev: PointerEvent) {
    const p = pointers.get(ev.pointerId);
    if (!p) {
      if (ev.pointerType === 'mouse') {
        const x = localX(ev);
        hoverGroup = nearGroup(x, MOUSE_TARGET_PX);
        hoverMs = hoverGroup ? hoverGroup.t : toMs(x);
      }
      return;
    }
    const x = localX(ev);
    const prev = p.x;
    p.x = x;

    const [a, b] = [...pointers.values()];
    if (pointers.size === 2 && pinchStart && a && b) {
      const dist = Math.max(1, Math.abs(a.x - b.x));
      const mid = (a.x + b.x) / 2;
      if (Math.abs(dist - pinchStart.dist) > 8 || Math.abs(mid - pinchStart.mid) > 8) {
        pinchStart.moved = true;
      }
      const factor = pinchStart.dist / dist;
      // Ancorar entre os dedos, como a roda ancora no cursor. Usar o mid vivo
      // como fração dá o arrastar-enquanto-pinça de brinde.
      zoomTo(pinchStart.anchorMs, (pinchStart.to - pinchStart.from) * factor, mid / width);
      dragged = true;
      return;
    }

    const dx = x - prev;
    // Dedo treme: 1px marcaria arraste e cancelaria o seek. Mouse é preciso.
    const slop = ev.pointerType === 'mouse' ? 2 : 8;
    if (Math.abs(x - p.x0) > slop) dragged = true;
    pan((-dx / Math.max(1, width)) * span);
  }

  function onPointerUp(ev: PointerEvent) {
    const p = pointers.get(ev.pointerId);
    const x = p ? p.x : localX(ev);
    pointers.delete(ev.pointerId);

    // Toque rápido com dois dedos, sem pinçar nem arrastar: afasta um passo.
    if (pinchStart) {
      const { mid, moved, t } = pinchStart;
      pinchStart = null;
      lastTap = null;
      swallowUp = true;
      if (!moved && ev.timeStamp - t < DOUBLE_TAP_MS) {
        zoomTo(toMs(mid), span * ZOOM_STEP, mid / width);
      }
      return;
    }
    if (swallowUp) {
      swallowUp = pointers.size > 0;
      return;
    }

    // Um arraste não deve virar um salto: só o toque limpo navega.
    if (dragged) {
      lastTap = null;
      return;
    }

    // Duplo toque: o 1º já pulou para o ponto, o 2º aproxima ancorado ali -
    // por isso nenhum gesto precisa esperar para saber se vem um segundo toque.
    if (
      lastTap &&
      ev.timeStamp - lastTap.t < DOUBLE_TAP_MS &&
      Math.abs(x - lastTap.x) < DOUBLE_TAP_PX
    ) {
      lastTap = null;
      zoomTo(toMs(x), span / ZOOM_STEP, x / width);
      return;
    }

    // No desktop a roda já resolve o zoom, e clicar repetido para ajustar a
    // posição não pode virar zoom surpresa.
    if (ev.pointerType !== 'mouse') lastTap = { t: ev.timeStamp, x };
    // Toque que cai numa marca de objeto vai ao instante DELA: acertar o pixel
    // de um tique de 3px com o dedo não pode ser a condição para chegar nele.
    const gr = nearGroup(x, ev.pointerType === 'mouse' ? MOUSE_TARGET_PX : FINGER_TARGET_PX);
    onseek?.(gr ? gr.t : Math.round(toMs(x)));
  }

  function onWheel(ev: WheelEvent) {
    ev.preventDefault();
    let dx = wheelPx(ev, ev.deltaX);
    let dy = wheelPx(ev, ev.deltaY);
    // Shift + roda é o horizontal do mouse comum; nem todo navegador já entrega
    // isso trocado para deltaX.
    if (ev.shiftKey && dx === 0) [dx, dy] = [dy, 0];

    // O dedo nunca anda reto: aplicar os dois eixos juntos faria a janela dar
    // zoom enquanto se quer só andar no tempo. Cada evento vale pelo eixo dominante.
    if (Math.abs(dx) > Math.abs(dy)) {
      // Andar é proporcional ao zoom: o deslocamento na tela vira tempo na mesma
      // escala do arraste, então o conteúdo acompanha o dedo.
      pan((dx / Math.max(1, width)) * span);
      return;
    }
    const x = localX(ev);
    zoomTo(toMs(x), span * Math.exp(dy * WHEEL_ZOOM_PER_PX), x / width);
  }

  function wheelPx(ev: WheelEvent, d: number) {
    if (ev.deltaMode === WheelEvent.DOM_DELTA_LINE) d *= WHEEL_PX_PER_LINE;
    else if (ev.deltaMode === WheelEvent.DOM_DELTA_PAGE) d *= width;
    return Math.max(-WHEEL_DELTA_MAX_PX, Math.min(WHEEL_DELTA_MAX_PX, d));
  }

  function zoomTo(anchorMs: number, nextSpan: number, frac = 0.5) {
    const total = dayEnd - dayStart;
    const s = Math.min(total, Math.max(MIN_SPAN, nextSpan));
    viewFrom = anchorMs - frac * s;
    viewTo = viewFrom + s;
    clamp();
  }

  function pan(deltaMs: number) {
    viewFrom += deltaMs;
    viewTo += deltaMs;
    clamp();
  }

  // A janela nunca sai do dia: navegar para o vazio confunde mais que ajuda.
  function clamp() {
    const s = viewTo - viewFrom;
    if (viewFrom < dayStart) {
      viewFrom = dayStart;
      viewTo = dayStart + s;
    }
    if (viewTo > dayEnd) {
      viewTo = dayEnd;
      viewFrom = dayEnd - s;
    }
  }

  export function reset() {
    viewFrom = dayStart;
    viewTo = dayEnd;
  }
</script>

<div class="wrapper" bind:clientWidth={width}>
  <canvas
    bind:this={canvas}
    style:height="{canvasHeight}px"
    onpointerdown={onPointerDown}
    onpointermove={onPointerMove}
    onpointerup={onPointerUp}
    onpointercancel={onPointerUp}
    onpointerleave={() => {
      hoverMs = null;
      hoverGroup = null;
    }}
    onwheel={onWheel}
  ></canvas>

  {#if hoverMs !== null}
    <!-- A margem da borda cresce com o texto: a dica de uma marca é mais longa
         que a de um horário, e sairia pela lateral. -->
    {@const margin = hoverGroup ? 90 : 28}
    <span
      class="tip mono"
      style:left="{Math.min(Math.max(toX(hoverMs), margin), width - margin)}px"
    >
      {hhmmss(hoverMs)}{#if hoverGroup}&nbsp;· {describe(hoverGroup)}{/if}
    </span>
  {/if}
</div>

<style>
  .wrapper {
    position: relative;
    width: 100%;
  }

  canvas {
    width: 100%;
    /* A altura vem do script: ela muda conforme a faixa de calor entra ou não. */
    display: block;
    border-radius: 8px;
    cursor: crosshair;
    /* Impede o navegador de rolar a página enquanto se arrasta a timeline. */
    touch-action: none;
  }

  .tip {
    position: absolute;
    top: -22px;
    transform: translateX(-50%);
    background: var(--panel-2);
    border: 1px solid var(--line);
    border-radius: 6px;
    padding: 1px 6px;
    font-size: 11px;
    pointer-events: none;
    white-space: nowrap;
  }
</style>
