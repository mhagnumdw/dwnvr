<!--
  Seletor de dia das gravações.

  Existe porque o `<input type="date">` nativo não sabe marcar quais dias têm
  material - e é essa marcação, não a escolha da data, que resolve a navegação
  às cegas pelo histórico.

  Com a lista de dias vazia - câmera que ainda não gravou nada, ou a consulta
  que falhou - ele NÃO trava: libera qualquer dia até hoje, exatamente como o
  campo nativo fazia. Um calendário inteiro desabilitado seria pior que
  calendário nenhum.
-->
<script>
  import { tick } from 'svelte';
  import { dayKey, parseDay } from '../lib/format.js';

  let { value = dayKey(), days = [], onchange = () => {} } = $props();

  let aberto = $state(false);
  let raizEl = $state(null);
  let triggerEl = $state(null);
  let gradeEl = $state(null);
  let popoverEl = $state(null);

  // Quanto o popover precisou sair do centro para não passar da borda da tela.
  // Medido, e não resolvido por media query: o seletor fica numa barra que
  // quebra em linha, então onde ele cai depende da largura E do nome da câmera.
  let desloc = $state(0);

  // Relido a cada abertura, e não uma vez na montagem: esta tela fica aberta
  // 24/7 e atravessa a meia-noite. Um `hoje` congelado deixaria o contorno do
  // dia corrente e o teto de navegação apontando para ontem.
  let hoje = $state(dayKey());

  // O dia sob o cursor do teclado. Ele anda pela grade inclusive por cima de
  // dias sem gravação - atravessar um buraco de uma semana com a seta tem que
  // ser possível -, e quem o acompanha é o foco REAL do DOM, não uma classe:
  // é assim que o leitor de tela anuncia cada data ao navegar.
  let diaFocado = $state(null);

  // Mês visível. Nasce nulo porque só a abertura sabe qual é - depende do dia
  // selecionado naquele instante.
  let viewYear = $state(null);
  let viewMonth = $state(null);

  const MESES = [
    'Janeiro', 'Fevereiro', 'Março', 'Abril', 'Maio', 'Junho',
    'Julho', 'Agosto', 'Setembro', 'Outubro', 'Novembro', 'Dezembro'
  ];
  const SEMANA = ['D', 'S', 'T', 'Q', 'Q', 'S', 'S'];

  const pad = (n) => String(n).padStart(2, '0');

  const daysSet = $derived(new Set(days));
  // Sem lista, tudo até hoje vale - ver o cabeçalho.
  const permissivo = $derived(days.length === 0);
  const temGravacao = $derived((chave) => (permissivo ? chave <= hoje : daysSet.has(chave)));

  // As duas pontas do que o teclado pode alcançar. O teto é hoje porque não
  // existe amanhã para reproduzir; o piso só existe quando há lista.
  const piso = $derived(permissivo ? null : days[0]);

  function limitar(chave) {
    if (chave > hoje) return hoje;
    if (piso && chave < piso) return piso;
    return chave;
  }

  // --- abrir, fechar, focar ---------------------------------------------------

  function irPara(chave) {
    diaFocado = chave;
    const d = parseDay(chave);
    viewYear = d.getFullYear();
    viewMonth = d.getMonth();
  }

  // O foco vai para a célula, não para o popover: é o que faz Enter e Espaço
  // caírem no `onclick` nativo do botão, sem handler de tecla nenhum no meio.
  function focarCelula() {
    tick().then(() => gradeEl?.querySelector(`[data-dia="${diaFocado}"]`)?.focus());
  }

  function abrir() {
    hoje = dayKey();
    irPara(limitar(value || hoje));
    desloc = 0;
    aberto = true;
    focarCelula();
    tick().then(caberNaTela);
  }

  function caberNaTela() {
    if (!popoverEl) return;
    const folga = 10;
    const r = popoverEl.getBoundingClientRect();
    if (r.right > window.innerWidth - folga) desloc = window.innerWidth - folga - r.right;
    else if (r.left < folga) desloc = folga - r.left;
  }

  function fechar(devolverFoco) {
    aberto = false;
    if (devolverFoco) tick().then(() => triggerEl?.focus());
  }

  function toggle() {
    if (aberto) fechar(true);
    else abrir();
  }

  function selecionar(chave) {
    if (!temGravacao(chave)) return;
    onchange(chave);
    fechar(true);
  }

  // --- navegação --------------------------------------------------------------

  function mudarMes(delta, focar) {
    const d = parseDay(diaFocado ?? hoje);
    const alvo = new Date(d.getFullYear(), d.getMonth() + delta, 1);
    // Dia 31 caindo num mês de 30: deixado ao `Date`, ele viraria para o mês
    // seguinte - justamente o pulo que este botão não deve dar.
    const ultimo = new Date(alvo.getFullYear(), alvo.getMonth() + 1, 0).getDate();
    alvo.setDate(Math.min(d.getDate(), ultimo));
    irPara(limitar(dayKey(alvo)));
    if (focar) focarCelula();
  }

  function moverFoco(dias) {
    const d = parseDay(diaFocado ?? hoje);
    d.setDate(d.getDate() + dias);
    irPara(limitar(dayKey(d)));
    focarCelula();
  }

  // Home e End vão para a ponta do HISTÓRICO, não para a ponta da semana como
  // manda o padrão de calendário genérico: aqui a pergunta que se faz o tempo
  // todo é "onde começa o que ainda existe em disco", e o começo da semana não
  // responde nada.
  function irParaPonta(fim) {
    const chave = fim
      ? (days.at(-1) ?? hoje)
      : (days[0] ?? `${viewYear}-${pad(viewMonth + 1)}-01`);
    irPara(limitar(chave));
    focarCelula();
  }

  // Só as teclas de movimento, e na própria célula: Enter e Espaço ficam com o
  // botão, que já os trata sozinho. É o que dispensa qualquer handler global -
  // um deles sequestrava o Enter de todo botão do popover.
  function onCelulaKeydown(e) {
    const passo = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 }[e.key];
    if (passo !== undefined) {
      e.preventDefault();
      moverFoco(passo);
      return;
    }
    if (e.key === 'PageUp' || e.key === 'PageDown') {
      e.preventDefault();
      mudarMes(e.key === 'PageUp' ? -1 : 1, true);
      return;
    }
    if (e.key === 'Home' || e.key === 'End') {
      e.preventDefault();
      irParaPonta(e.key === 'End');
    }
  }

  // --- o que a grade mostra ---------------------------------------------------

  const grade = $derived.by(() => {
    if (viewYear === null || viewMonth === null) return [];

    const vazias = new Date(viewYear, viewMonth, 1).getDay();
    const total = new Date(viewYear, viewMonth + 1, 0).getDate();
    const lista = Array.from({ length: vazias }, () => null);

    for (let d = 1; d <= total; d++) {
      const chave = `${viewYear}-${pad(viewMonth + 1)}-${pad(d)}`;
      const gravado = temGravacao(chave);
      lista.push({
        numero: d,
        chave,
        gravado,
        selecionado: chave === value,
        hoje: chave === hoje,
        // O estado inteiro no próprio rótulo, e não num `aria-pressed` por
        // célula: trinta botões anunciando "não pressionado" é ruído. Em modo
        // permissivo "com gravação" seria mentira - ali ninguém sabe.
        rotulo: `${d} de ${MESES[viewMonth]} de ${viewYear}`
          + (permissivo ? '' : gravado ? ', com gravação' : ', sem gravação')
          + (chave === value ? ', selecionado' : ''),
      });
    }
    return lista;
  });

  const textoBotao = $derived.by(() => {
    if (!value) return '--/--/----';
    const [y, m, d] = value.split('-');
    return `${d}/${m}/${y}`;
  });

  const podeAvancar = $derived.by(() => {
    if (viewYear === null) return false;
    const d = parseDay(hoje);
    return viewYear < d.getFullYear() || (viewYear === d.getFullYear() && viewMonth < d.getMonth());
  });

  const podeRecuar = $derived.by(() => {
    if (viewYear === null) return false;
    if (!piso) return true;
    const [y, m] = piso.split('-').map(Number);
    return viewYear > y || (viewYear === y && viewMonth > m - 1);
  });

  // --- fechar por fora --------------------------------------------------------

  function onWindowClick(e) {
    // Sem devolver o foco: quem clicou fora já escolheu para onde ir.
    if (aberto && raizEl && !raizEl.contains(e.target)) fechar(false);
  }

  function onWindowKeydown(e) {
    if (aberto && e.key === 'Escape') {
      e.preventDefault();
      fechar(true);
    }
  }

  // Tabular para fora fecha, como qualquer popover não modal. `relatedTarget`
  // nulo é foco saindo da janela inteira (ou o próprio popover desmontando),
  // e aí fechar seria atrapalhar quem só trocou de aba.
  function onFocusOut(e) {
    if (aberto && e.relatedTarget && raizEl && !raizEl.contains(e.relatedTarget)) {
      fechar(false);
    }
  }
</script>

<svelte:window onclick={onWindowClick} onkeydown={onWindowKeydown} />

<div class="day-picker" bind:this={raizEl} onfocusout={onFocusOut}>
  <button
    type="button"
    class="trigger ghost"
    onclick={toggle}
    bind:this={triggerEl}
    aria-label="dia: {textoBotao}"
    aria-haspopup="dialog"
    aria-expanded={aberto}
  >
    <span class="data mono">{textoBotao}</span>
    <svg class="icone" viewBox="0 0 16 16" width="14" height="14" fill="none"
         stroke="currentColor" stroke-width="1.5" aria-hidden="true">
      <rect x="2" y="3" width="12" height="11" rx="2" />
      <path d="M2 7h12M5 1.5v3M11 1.5v3" stroke-linecap="round" />
    </svg>
  </button>

  {#if aberto}
    <div
      class="popover card"
      bind:this={popoverEl}
      style="--desloc: {desloc}px"
      role="dialog"
      aria-label="calendário de gravações"
    >
      <div class="header">
        <button
          type="button"
          class="nav ghost"
          onclick={() => mudarMes(-1)}
          disabled={!podeRecuar}
          aria-label="mês anterior"
        >
          ‹
        </button>
        <span class="mes-ano">{MESES[viewMonth]} {viewYear}</span>
        <button
          type="button"
          class="nav ghost"
          onclick={() => mudarMes(1)}
          disabled={!podeAvancar}
          aria-label="próximo mês"
        >
          ›
        </button>
      </div>

      <div class="dias-semana" aria-hidden="true">
        {#each SEMANA as s, i (i)}
          <span class="d-sem">{s}</span>
        {/each}
      </div>

      <div class="grade" bind:this={gradeEl}>
        {#each grade as item, i (i)}
          {#if item === null}
            <span class="celula vazia" aria-hidden="true"></span>
          {:else}
            <button
              type="button"
              class="celula dia"
              class:gravado={item.gravado}
              class:hoje={item.hoje}
              class:selecionado={item.selecionado}
              data-dia={item.chave}
              onclick={() => selecionar(item.chave)}
              onkeydown={onCelulaKeydown}
              aria-label={item.rotulo}
              aria-disabled={!item.gravado}
              aria-current={item.hoje ? 'date' : undefined}
              tabindex={item.chave === diaFocado ? 0 : -1}
            >
              <span class="num">{item.numero}</span>
              {#if item.gravado && !permissivo}
                <span class="ponto" aria-hidden="true"></span>
              {/if}
            </button>
          {/if}
        {/each}
      </div>

      {#if temGravacao(hoje)}
        <div class="footer">
          <button type="button" class="hoje-btn ghost small" onclick={() => selecionar(hoje)}>
            Hoje
          </button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .day-picker {
    position: relative;
    display: inline-block;
  }

  .trigger {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    background: var(--panel-2);
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 9px 12px;
    min-height: 44px;
    color: var(--fg);
  }

  .trigger:hover {
    border-color: var(--accent);
  }

  .data {
    font-size: 14px;
  }

  .icone {
    opacity: 0.75;
    flex-shrink: 0;
  }

  .popover {
    position: absolute;
    top: calc(100% + 6px);
    left: 50%;
    transform: translateX(calc(-50% + var(--desloc, 0px)));
    z-index: 40;
    width: 280px;
    max-width: calc(100vw - 20px);
    padding: 12px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  }

  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 8px;
  }

  .mes-ano {
    font-weight: 600;
    font-size: 14px;
  }

  .nav {
    padding: 4px 8px;
    min-height: 32px;
    font-size: 18px;
    line-height: 1;
  }

  .dias-semana {
    display: grid;
    grid-template-columns: repeat(7, 1fr);
    text-align: center;
    font-size: 11px;
    color: var(--dim);
    margin-bottom: 4px;
  }

  .d-sem {
    padding: 2px 0;
  }

  .grade {
    display: grid;
    grid-template-columns: repeat(7, 1fr);
    gap: 2px;
  }

  .celula {
    aspect-ratio: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 0;
    margin: 0;
    border-radius: 6px;
    border: 1px solid transparent;
    font-size: 13px;
    min-height: unset;
    position: relative;
    background: transparent;
  }

  .celula.vazia {
    pointer-events: none;
  }

  /* Dia sem gravação continua focável de propósito - é o que permite à seta
     atravessar um buraco no histórico -, mas não é alvo de clique. */
  .celula.dia {
    color: var(--dim);
    opacity: 0.35;
    cursor: not-allowed;
  }

  .celula.dia.gravado {
    color: var(--fg);
    opacity: 1;
    cursor: pointer;
    font-weight: 500;
  }

  .celula.dia.gravado:hover {
    background: var(--panel-2);
    border-color: var(--line);
  }

  .celula.dia.hoje {
    border-color: var(--dim);
  }

  /* Declarado depois do :hover para vencer por ordem, sem !important. */
  .celula.dia.selecionado,
  .celula.dia.selecionado:hover {
    background: var(--accent);
    border-color: var(--accent);
    color: #fff;
    font-weight: 600;
  }

  .celula.dia.selecionado .ponto {
    background: #fff;
  }

  /* Contorno por fora da célula: por dentro ele sumiria no fundo do dia
     selecionado, que é justamente onde o foco costuma começar. */
  .celula.dia:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .ponto {
    width: 4px;
    height: 4px;
    border-radius: 50%;
    background: var(--accent);
    position: absolute;
    bottom: 3px;
  }

  .footer {
    display: flex;
    justify-content: center;
    margin-top: 8px;
    padding-top: 8px;
    border-top: 1px solid var(--line);
  }

  .hoje-btn {
    padding: 4px 12px;
    min-height: 28px;
    color: var(--accent);
  }
</style>
