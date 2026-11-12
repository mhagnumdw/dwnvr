<script lang="ts">
  // Relógio da reprodução que também aceita um horário digitado.
  //
  // É um <input> o tempo todo, e não um texto que vira campo no clique: o
  // Safari do iPhone só abre o teclado quando o foco nasce do próprio toque, e
  // um focus() chamado depois de trocar o elemento chega tarde demais.
  //
  // A máscara mora em lib/mascaraHora.ts, a mesma dos campos de horário de
  // Detecções. Sem os segundos (`segundos={false}`), o relógio mostra e aceita
  // só HH:MM, e `onir` recebe o segundo zero do minuto digitado.
  import { hhmm, hhmmss } from '../lib/format.ts';
  import { ghost as rest, mask, shake, secondsOfDay } from '../lib/hourMask.ts';

  interface Props {
    ms: number;
    ongo: (secondsOfDay: number) => void;
    segundos?: boolean;
  }

  let { ms, ongo, segundos = true }: Props = $props();

  const groups = $derived(segundos ? 3 : 2);
  const format = (v: number) => (segundos ? hhmmss(v) : hhmm(v));

  let input: HTMLInputElement | undefined;
  let editing = $state(false);
  let text = $state('');
  let typed = $state<string[]>(['']);
  // O que o player mostrava ao abrir o campo, para servir de placeholder: o
  // relógio de fundo continua andando, e o placeholder piscando distrai.
  let before = $state('');

  const ghost = $derived(rest(typed, groups));

  function openNow() {
    before = format(ms);
    text = '';
    typed = [''];
    editing = true;
  }

  function typeIn(e: Event & { currentTarget: HTMLInputElement }) {
    const m = mask(
      e.currentTarget.value,
      e instanceof InputEvent && e.inputType.startsWith('delete'),
      groups,
    );
    text = m.g.join(':');
    typed = m.g;
    // Direto no DOM: quando o dígito é recusado o texto não muda, o Svelte não
    // teria o que atualizar e o caractere recusado ficaria no campo.
    e.currentTarget.value = text;
    if (m.rejected) shake(input);
  }

  function keyHandler(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      const s = secondsOfDay(typed);
      if (s !== null) ongo(s);
      input?.blur();
    } else if (e.key === 'Escape') {
      input?.blur();
    }
  }
</script>

<span class="clock" class:editing class:no-seconds={!segundos}>
  {#if editing}
    <span class="mirror" aria-hidden="true">{text}<i>{ghost}</i></span>
  {/if}
  <input
    bind:this={input}
    class="mono"
    value={editing ? text : ms ? format(ms) : segundos ? '--:--:--' : '--:--'}
    placeholder={editing ? before : ''}
    disabled={!ms}
    inputmode="decimal"
    enterkeyhint="go"
    autocomplete="off"
    spellcheck="false"
    aria-label="horário; digite outro para ir até ele"
    title="Clique para ir a um horário"
    onfocus={openNow}
    onblur={() => (editing = false)}
    oninput={typeIn}
    onkeydown={keyHandler}
  />
</span>

<style>
  /* Em repouso e editando é a mesma caixa: nada ao lado se mexe quando o campo
     abre. A largura cabe "00:00:00" com folga; os dígitos têm largura fixa. */
  .clock {
    position: relative;
    display: inline-block;
    width: calc(8ch + 18px);
    height: 44px;
    font-size: 16px;
  }
  .clock.no-seconds {
    width: calc(5ch + 18px);
  }

  input,
  .mirror {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    margin: 0;
    /* 16px também evita o zoom automático do Safari ao focar o campo. */
    font: inherit;
    font-size: 16px;
    line-height: 24px;
    padding: 9px 8px;
    border-radius: 8px;
    white-space: pre;
  }

  input {
    z-index: 1;
    background: transparent;
    border: 1px solid transparent;
    cursor: text;
  }
  input:hover:not(:disabled) {
    border-color: var(--line);
  }
  input:disabled {
    cursor: default;
    color: var(--fg);
  }
  input:focus-visible {
    outline: none;
  }
  input::placeholder {
    color: var(--dim);
    opacity: 0.55;
  }

  .editing input {
    border-color: var(--accent);
  }

  .mirror {
    border: 1px solid transparent;
    background: var(--panel-2);
    color: transparent;
    pointer-events: none;
    font-variant-numeric: tabular-nums;
  }
  .mirror i {
    font-style: normal;
    color: var(--dim);
    opacity: 0.6;
  }
</style>
