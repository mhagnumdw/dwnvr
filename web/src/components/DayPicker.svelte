<!--
  Seletor de dia: das gravações, ou das detecções. `oQue` é o material que os
  dias marcados têm, e só muda o que o leitor de tela anuncia. Com `contagens`,
  o dia mostra quanto material tem, no lugar da bolinha que só diz que tem.

  Existe porque o `<input type="date">` nativo não sabe marcar quais dias têm
  material - e é essa marcação, não a escolha da data, que resolve a navegação
  às cegas pelo histórico.

  Com a lista de dias vazia - câmera que ainda não gravou nada, ou a consulta
  que falhou - ele NÃO trava: libera qualquer dia até hoje, exatamente como o
  campo nativo fazia. Um calendário inteiro desabilitado seria pior que
  calendário nenhum.
-->
<script lang="ts">
  import { tick } from 'svelte';
  import Icon from './Icon.svelte';
  import { dayKey, parseDay, shortDay } from '../lib/format.ts';

  interface Props {
    value?: string;
    days?: string[];
    // Undefined explícito: o NavDia repassa o dele, que pode não ter vindo.
    oQue?: string | undefined;
    // Dia -> quanto material ele tem. O dia de `days` que faltar aqui fica
    // com a bolinha.
    counts?: Record<string, number> | undefined;
    onchange?: (day: string) => void;
    // Avisa a abertura: quem passa `contagens` aproveita para atualizá-las,
    // porque a tela fica aberta e o dia corrente não para de crescer.
    onopen?: (() => void) | undefined;
  }

  // Uma célula do mês; nulo é o vão antes do dia 1.
  interface Cell {
    number: number;
    key: string;
    recorded: boolean;
    count: number | undefined;
    selected: boolean;
    today: boolean;
    label: string;
  }

  let {
    value = dayKey(),
    days = [],
    oQue = 'gravação',
    counts,
    onchange,
    onopen,
  }: Props = $props();

  let isOpen = $state(false);
  let rootEl = $state<HTMLDivElement | null>(null);
  let triggerEl = $state<HTMLButtonElement | null>(null);
  let gridEl = $state<HTMLDivElement | null>(null);
  let popoverEl = $state<HTMLDivElement | null>(null);

  // Quanto o popover precisou sair do centro para não passar da borda da tela.
  // Medido, e não resolvido por media query: o seletor fica numa barra que
  // quebra em linha, então onde ele cai depende da largura E do nome da câmera.
  let offset = $state(0);

  // Relido a cada abertura, e não uma vez na montagem: esta tela fica aberta
  // 24/7 e atravessa a meia-noite. Um `hoje` congelado deixaria o contorno do
  // dia corrente e o teto de navegação apontando para ontem.
  let today = $state(dayKey());

  // O dia sob o cursor do teclado. Ele anda pela grade inclusive por cima de
  // dias sem gravação - atravessar um buraco de uma semana com a seta tem que
  // ser possível -, e quem o acompanha é o foco REAL do DOM, não uma classe:
  // é assim que o leitor de tela anuncia cada data ao navegar.
  let focusedDay = $state<string | null>(null);

  // Mês visível. Nasce nulo porque só a abertura sabe qual é - depende do dia
  // selecionado naquele instante.
  let viewYear = $state<number | null>(null);
  let viewMonth = $state<number | null>(null);

  const MONTHS = [
    'Janeiro',
    'Fevereiro',
    'Março',
    'Abril',
    'Maio',
    'Junho',
    'Julho',
    'Agosto',
    'Setembro',
    'Outubro',
    'Novembro',
    'Dezembro',
  ];
  const WEEK = ['D', 'S', 'T', 'Q', 'Q', 'S', 'S'];

  const pad = (n: number) => String(n).padStart(2, '0');

  const daysSet = $derived(new Set(days));
  // Sem lista, tudo até hoje vale - ver o cabeçalho.
  const permissive = $derived(days.length === 0);
  const hasRecording = $derived((key: string) => (permissive ? key <= today : daysSet.has(key)));

  // As duas pontas do que o teclado pode alcançar. O teto é hoje porque não
  // existe amanhã para reproduzir; o piso só existe quando há lista.
  const floor = $derived(permissive ? null : days[0]);

  function clamp(key: string) {
    if (key > today) return today;
    if (floor && key < floor) return floor;
    return key;
  }

  // --- abrir, fechar, focar ---------------------------------------------------

  function goTo(key: string) {
    focusedDay = key;
    const d = parseDay(key);
    viewYear = d.getFullYear();
    viewMonth = d.getMonth();
  }

  // O foco vai para a célula, não para o popover: é o que faz Enter e Espaço
  // caírem no `onclick` nativo do botão, sem handler de tecla nenhum no meio.
  function focusCell() {
    void tick().then(() => {
      gridEl?.querySelector<HTMLElement>(`[data-dia="${focusedDay ?? ''}"]`)?.focus();
    });
  }

  function openNow() {
    today = dayKey();
    goTo(clamp(value || today));
    offset = 0;
    isOpen = true;
    onopen?.();
    focusCell();
    void tick().then(fitOnScreen);
  }

  function fitOnScreen() {
    if (!popoverEl) return;
    const slack = 10;
    const r = popoverEl.getBoundingClientRect();
    if (r.right > window.innerWidth - slack) offset = window.innerWidth - slack - r.right;
    else if (r.left < slack) offset = slack - r.left;
  }

  function close(returnFocus: boolean) {
    isOpen = false;
    if (returnFocus) {
      void tick().then(() => {
        triggerEl?.focus();
      });
    }
  }

  function toggle() {
    if (isOpen) close(true);
    else openNow();
  }

  function select(key: string) {
    if (!hasRecording(key)) return;
    onchange?.(key);
    close(true);
  }

  // --- navegação --------------------------------------------------------------

  function changeMonth(delta: number, focusNow = false) {
    const d = parseDay(focusedDay ?? today);
    const ano = d.getFullYear();
    const mes = d.getMonth() + delta;
    // Dia 31 caindo num mês de 30: deixado ao `Date`, ele viraria para o mês
    // seguinte - justamente o pulo que este botão não deve dar.
    const last = new Date(ano, mes + 1, 0).getDate();
    const target = new Date(ano, mes, Math.min(d.getDate(), last));
    goTo(clamp(dayKey(target)));
    if (focusNow) focusCell();
  }

  function moveFocus(dias: number) {
    const d = parseDay(focusedDay ?? today);
    d.setDate(d.getDate() + dias);
    goTo(clamp(dayKey(d)));
    focusCell();
  }

  // Home e End vão para a ponta do HISTÓRICO, não para a ponta da semana como
  // manda o padrão de calendário genérico: aqui a pergunta que se faz o tempo
  // todo é "onde começa o que ainda existe em disco", e o começo da semana não
  // responde nada.
  function goToEdge(end: boolean) {
    const monthStart =
      viewYear === null || viewMonth === null ? today : `${viewYear}-${pad(viewMonth + 1)}-01`;
    const key = end ? (days.at(-1) ?? today) : (days[0] ?? monthStart);
    goTo(clamp(key));
    focusCell();
  }

  // Só as teclas de movimento, e na própria célula: Enter e Espaço ficam com o
  // botão, que já os trata sozinho. É o que dispensa qualquer handler global -
  // um deles sequestrava o Enter de todo botão do popover.
  const STEPS: Readonly<Record<string, number>> = {
    ArrowLeft: -1,
    ArrowRight: 1,
    ArrowUp: -7,
    ArrowDown: 7,
  };

  function onCellKeydown(e: KeyboardEvent) {
    const step = STEPS[e.key];
    if (step !== undefined) {
      e.preventDefault();
      moveFocus(step);
      return;
    }
    if (e.key === 'PageUp' || e.key === 'PageDown') {
      e.preventDefault();
      changeMonth(e.key === 'PageUp' ? -1 : 1, true);
      return;
    }
    if (e.key === 'Home' || e.key === 'End') {
      e.preventDefault();
      goToEdge(e.key === 'End');
    }
  }

  // --- o que a grade mostra ---------------------------------------------------

  const grid = $derived.by(() => {
    if (viewYear === null || viewMonth === null) return [];

    const emptyOnes = new Date(viewYear, viewMonth, 1).getDay();
    const total = new Date(viewYear, viewMonth + 1, 0).getDate();
    const list: (Cell | null)[] = Array.from({ length: emptyOnes }, () => null);

    for (let d = 1; d <= total; d++) {
      const key = `${viewYear}-${pad(viewMonth + 1)}-${pad(d)}`;
      const recorded = hasRecording(key);
      const count = recorded && !permissive ? counts?.[key] : undefined;
      list.push({
        number: d,
        key,
        recorded,
        count,
        selected: key === value,
        today: key === today,
        // O estado inteiro no próprio rótulo, e não num `aria-pressed` por
        // célula: trinta botões anunciando "não pressionado" é ruído. Em modo
        // permissivo "com gravação" seria mentira - ali ninguém sabe.
        label:
          `${d} de ${MONTHS[viewMonth] ?? ''} de ${viewYear}` +
          (permissive ? '' : `, ${recorded ? 'com' : 'sem'} ${oQue}`) +
          (count === undefined ? '' : `: ${count}`) +
          (key === value ? ', selecionado' : ''),
      });
    }
    return list;
  });

  // A data inteira para o leitor de tela; no botão, a curta, com o ano só
  // quando não é o deste ano: no celular a barra divide a linha com a câmera.
  const wholeDate = $derived.by(() => {
    if (!value) return '--/--/----';
    const [y = '', m = '', d = ''] = value.split('-');
    return `${d}/${m}/${y}`;
  });
  const buttonText = $derived(value ? shortDay(parseDay(value).getTime()) : '--/--');

  const canAdvance = $derived.by(() => {
    if (viewYear === null || viewMonth === null) return false;
    const d = parseDay(today);
    return viewYear < d.getFullYear() || (viewYear === d.getFullYear() && viewMonth < d.getMonth());
  });

  const canGoBack = $derived.by(() => {
    if (viewYear === null || viewMonth === null) return false;
    if (!floor) return true;
    const p = parseDay(floor);
    return viewYear > p.getFullYear() || (viewYear === p.getFullYear() && viewMonth > p.getMonth());
  });

  const mesAno = $derived(
    viewYear === null || viewMonth === null ? '' : `${MONTHS[viewMonth] ?? ''} ${viewYear}`,
  );

  // --- fechar por fora --------------------------------------------------------

  function onWindowClick(e: MouseEvent) {
    // Sem devolver o foco: quem clicou fora já escolheu para onde ir.
    if (isOpen && rootEl && e.target instanceof Node && !rootEl.contains(e.target)) close(false);
  }

  function onWindowKeydown(e: KeyboardEvent) {
    if (isOpen && e.key === 'Escape') {
      e.preventDefault();
      close(true);
    }
  }

  // Tabular para fora fecha, como qualquer popover não modal. `relatedTarget`
  // nulo é foco saindo da janela inteira (ou o próprio popover desmontando),
  // e aí fechar seria atrapalhar quem só trocou de aba.
  function onFocusOut(e: FocusEvent) {
    if (isOpen && e.relatedTarget instanceof Node && rootEl && !rootEl.contains(e.relatedTarget)) {
      close(false);
    }
  }
</script>

<svelte:window onclick={onWindowClick} onkeydown={onWindowKeydown} />

<div class="day-picker" bind:this={rootEl} onfocusout={onFocusOut}>
  <button
    type="button"
    class="trigger ghost"
    onclick={toggle}
    bind:this={triggerEl}
    aria-label="dia: {wholeDate}"
    aria-haspopup="dialog"
    aria-expanded={isOpen}
  >
    <span class="data mono">{buttonText}</span>
    <span class="icon"><Icon name="calendar" size={14} stroke={2.25} /></span>
  </button>

  {#if isOpen}
    <div
      class="popover card"
      bind:this={popoverEl}
      style="--desloc: {offset}px"
      role="dialog"
      aria-label="calendário: dias com {oQue}"
    >
      <div class="header">
        <button
          type="button"
          class="nav ghost"
          onclick={() => {
            changeMonth(-1);
          }}
          disabled={!canGoBack}
          aria-label="mês anterior"
        >
          <Icon name="chevronEsq" size={18} />
        </button>
        <span class="mes-ano">{mesAno}</span>
        <button
          type="button"
          class="nav ghost"
          onclick={() => {
            changeMonth(1);
          }}
          disabled={!canAdvance}
          aria-label="próximo mês"
        >
          <Icon name="chevronDir" size={18} />
        </button>
      </div>

      <div class="week-days" aria-hidden="true">
        {#each WEEK as s, i (i)}
          <span class="d-week">{s}</span>
        {/each}
      </div>

      <div class="grid" bind:this={gridEl}>
        {#each grid as item, i (i)}
          {#if item === null}
            <span class="cell empty" aria-hidden="true"></span>
          {:else}
            <button
              type="button"
              class="cell day"
              class:recorded={item.recorded}
              class:counted={item.count !== undefined}
              class:today={item.today}
              class:selected={item.selected}
              data-dia={item.key}
              onclick={() => {
                select(item.key);
              }}
              onkeydown={onCellKeydown}
              aria-label={item.label}
              aria-disabled={!item.recorded}
              aria-current={item.today ? 'date' : undefined}
              tabindex={item.key === focusedDay ? 0 : -1}
            >
              <span class="num">{item.number}</span>
              {#if item.count !== undefined}
                <span class="count mono" aria-hidden="true">{item.count}</span>
              {:else if item.recorded && !permissive}
                <span class="point" aria-hidden="true"></span>
              {/if}
            </button>
          {/if}
        {/each}
      </div>

      {#if hasRecording(today)}
        <div class="footer">
          <button
            type="button"
            class="today-btn ghost small"
            onclick={() => {
              select(today);
            }}
          >
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

  .icon {
    display: flex;
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
  }

  .week-days {
    display: grid;
    grid-template-columns: repeat(7, 1fr);
    text-align: center;
    font-size: 11px;
    color: var(--dim);
    margin-bottom: 4px;
  }

  .d-week {
    padding: 2px 0;
  }

  .grid {
    display: grid;
    grid-template-columns: repeat(7, 1fr);
    gap: 2px;
  }

  .cell {
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

  .cell.empty {
    pointer-events: none;
  }

  /* Dia sem gravação continua focável de propósito - é o que permite à seta
     atravessar um buraco no histórico -, mas não é alvo de clique. */
  .cell.day {
    color: var(--dim);
    opacity: 0.35;
    cursor: not-allowed;
  }

  .cell.day.recorded {
    color: var(--fg);
    opacity: 1;
    cursor: pointer;
    font-weight: 500;
  }

  .cell.day.recorded:hover {
    background: var(--panel-2);
    border-color: var(--line);
  }

  .cell.day.today {
    border-color: var(--dim);
  }

  /* Declarado depois do :hover para vencer por ordem, sem !important. */
  .cell.day.selected,
  .cell.day.selected:hover {
    background: var(--accent);
    border-color: var(--accent);
    color: #fff;
    font-weight: 600;
  }

  .cell.day.selected .point {
    background: #fff;
  }

  .cell.day.selected .count {
    color: #fff;
  }

  /* Contorno por fora da célula: por dentro ele sumiria no fundo do dia
     selecionado, que é justamente onde o foco costuma começar. */
  .cell.day:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }

  .point {
    width: 4px;
    height: 4px;
    border-radius: 50%;
    background: var(--accent);
    position: absolute;
    bottom: 3px;
  }

  /* No lugar da bolinha. O número do dia sobe para abrir espaço: 1537, o
     maior dia visto numa instalação real, cabe na célula de 280 / 7 px. */
  .cell.counted .num {
    line-height: 1;
    margin-top: -6px;
  }

  .count {
    position: absolute;
    bottom: 2px;
    font-size: 9px;
    line-height: 1;
    font-weight: 600;
    letter-spacing: -0.2px;
    color: var(--accent);
  }

  .footer {
    display: flex;
    justify-content: center;
    margin-top: 8px;
    padding-top: 8px;
    border-top: 1px solid var(--line);
  }

  .today-btn {
    padding: 4px 12px;
    min-height: 28px;
    color: var(--accent);
  }
</style>
