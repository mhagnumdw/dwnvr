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
  import { fantasma as resto, mascara, sacudir, segundosDoDia } from '../lib/mascaraHora.ts';

  interface Props {
    ms: number;
    onir: (segundosDoDia: number) => void;
    segundos?: boolean;
  }

  let { ms, onir, segundos = true }: Props = $props();

  const grupos = $derived(segundos ? 3 : 2);
  const formata = (v: number) => (segundos ? hhmmss(v) : hhmm(v));

  let input: HTMLInputElement | undefined;
  let editando = $state(false);
  let texto = $state('');
  let digitados = $state<string[]>(['']);
  // O que o player mostrava ao abrir o campo, para servir de placeholder: o
  // relógio de fundo continua andando, e o placeholder piscando distrai.
  let antes = $state('');

  const fantasma = $derived(resto(digitados, grupos));

  function abrir() {
    antes = formata(ms);
    texto = '';
    digitados = [''];
    editando = true;
  }

  function digitar(e: Event & { currentTarget: HTMLInputElement }) {
    const m = mascara(
      e.currentTarget.value,
      e instanceof InputEvent && e.inputType.startsWith('delete'),
      grupos,
    );
    texto = m.g.join(':');
    digitados = m.g;
    // Direto no DOM: quando o dígito é recusado o texto não muda, o Svelte não
    // teria o que atualizar e o caractere recusado ficaria no campo.
    e.currentTarget.value = texto;
    if (m.recusou) sacudir(input);
  }

  function tecla(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      const s = segundosDoDia(digitados);
      if (s !== null) onir(s);
      input?.blur();
    } else if (e.key === 'Escape') {
      input?.blur();
    }
  }
</script>

<span class="relogio" class:editando class:sem-segundos={!segundos}>
  {#if editando}
    <span class="espelho" aria-hidden="true">{texto}<i>{fantasma}</i></span>
  {/if}
  <input
    bind:this={input}
    class="mono"
    value={editando ? texto : ms ? formata(ms) : segundos ? '--:--:--' : '--:--'}
    placeholder={editando ? antes : ''}
    disabled={!ms}
    inputmode="decimal"
    enterkeyhint="go"
    autocomplete="off"
    spellcheck="false"
    aria-label="horário; digite outro para ir até ele"
    title="Clique para ir a um horário"
    onfocus={abrir}
    onblur={() => (editando = false)}
    oninput={digitar}
    onkeydown={tecla}
  />
</span>

<style>
  /* Em repouso e editando é a mesma caixa: nada ao lado se mexe quando o campo
     abre. A largura cabe "00:00:00" com folga; os dígitos têm largura fixa. */
  .relogio {
    position: relative;
    display: inline-block;
    width: calc(8ch + 18px);
    height: 44px;
    font-size: 16px;
  }
  .relogio.sem-segundos {
    width: calc(5ch + 18px);
  }

  input,
  .espelho {
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

  .editando input {
    border-color: var(--accent);
  }

  .espelho {
    border: 1px solid transparent;
    background: var(--panel-2);
    color: transparent;
    pointer-events: none;
    font-variant-numeric: tabular-nums;
  }
  .espelho i {
    font-style: normal;
    color: var(--dim);
    opacity: 0.6;
  }
</style>
