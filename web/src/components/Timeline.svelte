<script>
  import { hhmm, hhmmss } from '../lib/format.js';
  import { familia, iconeDa, desenhaIcone, NOME_DA_CLASSE } from '../lib/icones.js';

  let {
    ranges = [],       // [[inícioMs, fimMs], …] faixas com gravação
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
    objetos = [],
    dayStart = 0,
    dayEnd = 0,
    currentMs = 0,
    onseek = () => {},
    // Janela visível: o único estado que o zoom e o arraste alteram. Sobe para
    // quem chama porque é ela que vai para a URL - o link precisa reproduzir o
    // zoom, não só o instante. Zero nos dois quer dizer "ainda não escolhida", e
    // é o que faz o efeito abaixo ancorar no dia inteiro.
    viewFrom = $bindable(0),
    viewTo = $bindable(0),
  } = $props();

  let canvas;
  let width = $state(0);
  let hoverMs = $state(null);
  // A marca sob o ponteiro, quando ele está a um alvo dela: é o que a dica
  // descreve e o que o fio do ponteiro aponta.
  let hoverGrupo = $state(null);

  const MIN_SPAN = 20_000; // 20s de zoom máximo

  // A PILHA, de cima para baixo: objeto, vídeo, movimento - cada altura quer
  // dizer uma coisa só. Os números da barra e da régua são os que ela sempre
  // teve; a faixa de calor entra por baixo e a de objeto por cima, cada uma só
  // quando há o que mostrar.
  const TOPO_PX = 6;
  // Onde o ícone se apoia, acima da linha de objeto. Fixa, e não do tamanho do
  // ícone: o ícone de classe é maior que o de família, e com a zona crescendo a
  // barra desceria no meio de um zoom.
  const ZONA_PX = 24;
  const OBJ_PX = 10;   // a linha de objeto
  const BARRA_PX = 36;
  const VAO_PX = 3;    // respiro entre a barra e cada faixa vizinha
  const CALOR_PX = 10;
  const REGUA_PX = 20; // o que sobra embaixo, para os rótulos de hora

  // O desenho da marca de objeto: um tique na linha, e o ícone em cima SEMPRE
  // que couber. Uma câmera marca dezenas de objetos por dia, não centenas, e o
  // ícone cabe em boa parte do dia. O que não cabe fica só no tique - a cor já
  // diz a família.
  const TIQUE_PX = 3;
  const ICONE_FAMILIA_PX = 14;
  // A 20px o ícone de classe separa moto de bicicleta e gato de cachorro; a
  // 14px eles viram o mesmo desenho. Por isso a classe só entra de perto.
  const ICONE_CLASSE_PX = 20;
  const CLASSE_ATE_MS = 20 * 60_000;
  const ICONE_FOLGA_PX = 4; // respiro entre dois ícones vizinhos
  // Distância em que o toque "cai" numa marca e vai ao instante dela, em vez de
  // ao pixel tocado. O dedo é impreciso, o mouse não - o mesmo raciocínio do
  // limiar de arraste lá embaixo.
  const ALVO_MOUSE_PX = 8;
  const ALVO_DEDO_PX = 16;
  const FUNDO = '#1b1f24';

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
  const CALOR_COR = '#d29922';
  const CALOR_ALFA_MIN = 0.3;

  // Reancorar quando o dia muda; comparar os limites evita reancorar a cada
  // repintura e perder o zoom que o usuário acabou de dar. O piso do span faz o
  // mesmo papel para uma janela vinda da URL: o `zoomTo` nunca produz menos que
  // MIN_SPAN, mas um link editado à mão produz - e aí o zoom máximo, que é
  // regra desta timeline, passaria a valer só para quem chegou clicando.
  $effect(() => {
    const janela = viewTo - viewFrom;
    if (dayStart && (viewFrom < dayStart || viewTo > dayEnd || viewTo === 0 || janela < MIN_SPAN)) {
      viewFrom = dayStart;
      viewTo = dayEnd;
    }
  });

  const temCalor = $derived(events.length > 0);
  const temObjetos = $derived(objetos.length > 0);
  const objTop = TOPO_PX + ZONA_PX;
  const barTop = $derived(temObjetos ? objTop + OBJ_PX + VAO_PX : TOPO_PX);
  const alturaCanvas = $derived(
    barTop + BARRA_PX + (temCalor ? VAO_PX + CALOR_PX : 0) + REGUA_PX,
  );

  // As marcas agrupadas por instante, crescentes. Uma olhada pode confirmar
  // mais de uma família no mesmo quadro - pessoa e moto chegam juntas, e numa
  // rua isso é comum -, e o grupo é
  // desenhado como UMA marca dividida, e não como duas que se tampam.
  // Dentro do grupo, a família de mais prioridade vem primeiro.
  const grupos = $derived.by(() => {
    const out = [];
    for (const o of [...objetos].sort((a, b) => a.instanteMs - b.instanteMs)) {
      const ult = out.at(-1);
      if (ult && ult.t === o.instanteMs) ult.marcas.push(o);
      else out.push({ t: o.instanteMs, marcas: [o] });
    }
    for (const gr of out) {
      gr.marcas.sort((a, b) => familia(a.familia).prioridade - familia(b.familia).prioridade);
      gr.prioridade = familia(gr.marcas[0].familia).prioridade;
    }
    return out;
  });

  const span = $derived(Math.max(1, viewTo - viewFrom));
  const toX = (ms) => ((ms - viewFrom) / span) * width;
  const toMs = (x) => viewFrom + (x / Math.max(1, width)) * span;

  // Passo das marcas de hora: escolhido para que os rótulos nunca se toquem,
  // seja num celular de 360px ou num monitor largo.
  const step = $derived.by(() => {
    const opts = [1e3, 5e3, 15e3, 60e3, 3e5, 9e5, 18e5, 36e5, 108e5, 216e5];
    const minPx = width < 500 ? 56 : 78;
    return opts.find((s) => (s / span) * width >= minPx) ?? opts.at(-1);
  });

  $effect(() => {
    // Redesenha sempre que qualquer uma destas mudar.
    ranges; events; grupos; viewFrom; viewTo; currentMs; width; hoverMs; alturaCanvas;
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
  function contaPorColuna() {
    const cols = new Int32Array(Math.max(1, Math.ceil(width)));
    for (let i = limiteInferior(events, viewFrom); i < events.length; i++) {
      if (events[i] >= viewTo) break;
      const x = Math.floor(toX(events[i]));
      if (x >= 0 && x < cols.length) cols[x]++;
    }
    return cols;
  }

  function limiteInferior(arr, v) {
    let lo = 0;
    let hi = arr.length;
    while (lo < hi) {
      const m = (lo + hi) >> 1;
      if (arr[m] < v) lo = m + 1;
      else hi = m;
    }
    return lo;
  }

  /* O tique de um grupo na linha de objeto. Duas famílias no mesmo instante
   * dividem o tique em faixas, a de mais prioridade no alto. */
  function tique(g, gr) {
    const x = toX(gr.t) - TIQUE_PX / 2;
    const h = OBJ_PX / gr.marcas.length;
    gr.marcas.forEach((m, i) => {
      g.fillStyle = familia(m.familia).cor;
      g.fillRect(x, objTop + i * h, TIQUE_PX, h);
    });
  }

  /* Onde cada ícone vai, sem que dois se sobreponham.
   *
   * Percorre por PRIORIDADE, e não por tempo: `pessoa` escolhe lugar antes de
   * todos, então um gato vizinho nunca tira o ícone de uma pessoa. Um grupo de
   * duas famílias pede lugar para os dois ícones lado a lado; sem espaço, tenta
   * ao menos o da família principal. O que não coube continua no tique. */
  function encaixa(visiveis, lado, porClasse) {
    const ordem = [...visiveis].sort((a, b) => a.prioridade - b.prioridade || a.t - b.t);
    const ocupados = [];
    const postos = [];
    const livre = (x0, x1) =>
      ocupados.every(([a, b]) => x1 + ICONE_FOLGA_PX <= a || x0 >= b + ICONE_FOLGA_PX);
    for (const gr of ordem) {
      const tentativas = gr.marcas.length > 1 ? [gr.marcas, gr.marcas.slice(0, 1)] : [gr.marcas];
      for (const marcas of tentativas) {
        const larg = marcas.length * lado + (marcas.length - 1) * 2;
        const x0 = toX(gr.t) - larg / 2;
        if (!livre(x0, x0 + larg)) continue;
        ocupados.push([x0, x0 + larg]);
        marcas.forEach((m, i) => {
          const nome = iconeDa(m, porClasse);
          if (nome) postos.push({ nome, x: x0 + i * (lado + 2), cor: familia(m.familia).cor });
        });
        break;
      }
    }
    return postos;
  }

  function desenhaObjetos(g) {
    g.fillStyle = FUNDO;
    g.fillRect(0, objTop, width, OBJ_PX);

    // Um pouco além das bordas, para o ícone de uma marca logo fora da janela
    // entrar pela metade em vez de pular para dentro de uma vez.
    const visiveis = grupos.filter((gr) => {
      const x = toX(gr.t);
      return x > -ICONE_CLASSE_PX && x < width + ICONE_CLASSE_PX;
    });

    // A de menos prioridade primeiro: onde dois tiques caem no mesmo pixel, o
    // de `pessoa` é o último pintado, e fica por cima.
    for (const gr of [...visiveis].sort((a, b) => b.prioridade - a.prioridade)) tique(g, gr);

    const porClasse = span <= CLASSE_ATE_MS;
    const lado = porClasse ? ICONE_CLASSE_PX : ICONE_FAMILIA_PX;
    for (const p of encaixa(visiveis, lado, porClasse)) {
      desenhaIcone(g, p.nome, p.x, objTop - lado - 2, lado, p.cor);
    }
  }

  /* A marca a até `alvo` px de x, ou null. No empate, a de mais prioridade. */
  function grupoPerto(x, alvo) {
    let melhor = null;
    let dist = Infinity;
    for (const gr of grupos) {
      const d = Math.abs(toX(gr.t) - x);
      if (d > alvo) continue;
      if (d < dist || (d === dist && gr.prioridade < melhor.prioridade)) {
        melhor = gr;
        dist = d;
      }
    }
    return melhor;
  }

  function descreve(gr) {
    return gr.marcas.map((m) => NOME_DA_CLASSE[m.classe] ?? m.classe).join(' + ');
  }

  function draw() {
    if (!canvas || !width) return;
    const dpr = window.devicePixelRatio || 1;
    const h = alturaCanvas;
    canvas.width = width * dpr;
    canvas.height = h * dpr;

    const g = canvas.getContext('2d');
    g.setTransform(dpr, 0, 0, dpr, 0, 0);

    const barH = BARRA_PX;
    const calorTop = barTop + barH + VAO_PX;
    // O fio do cursor e o do ponteiro atravessam a pilha inteira: eles marcam
    // UM instante, e um instante vale igual nas três camadas.
    const pilhaTop = temObjetos ? objTop : barTop;
    const pilhaH = (temCalor ? calorTop + CALOR_PX : barTop + barH) - pilhaTop;

    if (temObjetos) desenhaObjetos(g);

    g.fillStyle = FUNDO;
    g.fillRect(0, barTop, width, barH);

    g.fillStyle = '#2f81f7';
    for (const [s, e] of ranges) {
      const x0 = Math.max(0, toX(s));
      const x1 = Math.min(width, toX(e));
      // Mínimo de 2px: uma gravação curta não pode desaparecer da barra só
      // porque o zoom está afastado.
      if (x1 > 0 && x0 < width) g.fillRect(x0, barTop, Math.max(2, x1 - x0), barH);
    }

    if (temCalor) {
      const cols = contaPorColuna();
      let teto = 1;
      for (const c of cols) if (c > teto) teto = c;

      g.fillStyle = FUNDO;
      g.fillRect(0, calorTop, width, CALOR_PX);
      g.fillStyle = CALOR_COR;
      for (let x = 0; x < cols.length; x++) {
        if (!cols[x]) continue;
        g.globalAlpha =
          CALOR_ALFA_MIN + (1 - CALOR_ALFA_MIN) * Math.sqrt(Math.min(1, cols[x] / teto));
        g.fillRect(x, calorTop, 1, CALOR_PX);
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
      g.fillRect(x, pilhaTop, 1, pilhaH);
    }

    if (currentMs) {
      const x = toX(currentMs);
      if (x >= -2 && x <= width + 2) {
        g.fillStyle = '#f85149';
        g.fillRect(x - 1.5, pilhaTop - 3, 3, pilhaH + 6);
      }
    }
  }

  // --- interação -----------------------------------------------------------
  //
  // Ponteiros ativos: um arrasta a janela, dois pinçam para dar zoom. Usar
  // Pointer Events cobre mouse, dedo e caneta com o mesmo código.
  // Cada ponteiro guarda também o x inicial: o limiar de arraste precisa do
  // deslocamento acumulado, não do passo do último evento.
  const pointers = new Map();
  let dragged = false;
  let pinchStart = null;
  let lastTap = null;     // { t, x } do último toque limpo, para o duplo toque
  let swallowUp = false;  // o up do 2º dedo não pode virar seek

  const DOUBLE_TAP_MS = 300;
  const DOUBLE_TAP_PX = 32;
  const ZOOM_STEP = 3; // um gesto deliberado merece um salto maior que o da roda
  // Zoom da roda proporcional ao deltaY: um clique de mouse (~100px) dá o passo
  // de 1.35, e o touchpad, que manda dezenas de deltas pequenos, soma o mesmo
  // zoom na medida do movimento dos dedos em vez de um passo cheio por evento.
  const WHEEL_ZOOM_POR_PX = Math.log(1.35) / 100;
  const WHEEL_PX_POR_LINHA = 33;   // Firefox pode mandar o delta em linhas
  const WHEEL_DELTA_MAX_PX = 300;  // um evento isolado nunca salta mais que ~2.5x nem ~¼ da tela

  function localX(ev) {
    return ev.clientX - canvas.getBoundingClientRect().left;
  }

  function onPointerDown(ev) {
    canvas.setPointerCapture(ev.pointerId);
    const x = localX(ev);
    pointers.set(ev.pointerId, { x, x0: x });
    dragged = false;
    if (pointers.size === 2) {
      const [a, b] = [...pointers.values()];
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

  function onPointerMove(ev) {
    const p = pointers.get(ev.pointerId);
    if (!p) {
      if (ev.pointerType === 'mouse') {
        const x = localX(ev);
        hoverGrupo = grupoPerto(x, ALVO_MOUSE_PX);
        hoverMs = hoverGrupo ? hoverGrupo.t : toMs(x);
      }
      return;
    }
    const x = localX(ev);
    const prev = p.x;
    p.x = x;

    if (pointers.size === 2 && pinchStart) {
      const [a, b] = [...pointers.values()];
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

  function onPointerUp(ev) {
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
    if (lastTap && ev.timeStamp - lastTap.t < DOUBLE_TAP_MS
        && Math.abs(x - lastTap.x) < DOUBLE_TAP_PX) {
      lastTap = null;
      zoomTo(toMs(x), span / ZOOM_STEP, x / width);
      return;
    }

    // No desktop a roda já resolve o zoom, e clicar repetido para ajustar a
    // posição não pode virar zoom surpresa.
    if (ev.pointerType !== 'mouse') lastTap = { t: ev.timeStamp, x };
    // Toque que cai numa marca de objeto vai ao instante DELA: acertar o pixel
    // de um tique de 3px com o dedo não pode ser a condição para chegar nele.
    const gr = grupoPerto(x, ev.pointerType === 'mouse' ? ALVO_MOUSE_PX : ALVO_DEDO_PX);
    onseek(gr ? gr.t : Math.round(toMs(x)));
  }

  function onWheel(ev) {
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
    zoomTo(toMs(x), span * Math.exp(dy * WHEEL_ZOOM_POR_PX), x / width);
  }

  function wheelPx(ev, d) {
    if (ev.deltaMode === WheelEvent.DOM_DELTA_LINE) d *= WHEEL_PX_POR_LINHA;
    else if (ev.deltaMode === WheelEvent.DOM_DELTA_PAGE) d *= width;
    return Math.max(-WHEEL_DELTA_MAX_PX, Math.min(WHEEL_DELTA_MAX_PX, d));
  }

  function zoomTo(anchorMs, nextSpan, frac = 0.5) {
    const total = dayEnd - dayStart;
    const s = Math.min(total, Math.max(MIN_SPAN, nextSpan));
    viewFrom = anchorMs - frac * s;
    viewTo = viewFrom + s;
    clamp();
  }

  function pan(deltaMs) {
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
    style:height="{alturaCanvas}px"
    onpointerdown={onPointerDown}
    onpointermove={onPointerMove}
    onpointerup={onPointerUp}
    onpointercancel={onPointerUp}
    onpointerleave={() => {
      hoverMs = null;
      hoverGrupo = null;
    }}
    onwheel={onWheel}
  ></canvas>

  {#if hoverMs !== null}
    <!-- A margem da borda cresce com o texto: a dica de uma marca é mais longa
         que a de um horário, e sairia pela lateral. -->
    {@const margem = hoverGrupo ? 90 : 28}
    <span class="tip mono" style:left="{Math.min(Math.max(toX(hoverMs), margem), width - margem)}px">
      {hhmmss(hoverMs)}{#if hoverGrupo}&nbsp;· {descreve(hoverGrupo)}{/if}
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
