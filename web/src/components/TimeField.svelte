<script lang="ts">
  // Um campo de horário HH:MM, com a máscara do relógio (lib/mascaraHora.ts):
  // "22" já é 22:00 e "17:3" é 17:30, com o resto em cinza mostrando isso.
  // O valor é o minuto do dia, ou nulo com o campo vazio: `valor` é o de
  // partida, e `onmuda` recebe cada mudança.
  import { untrack } from 'svelte';
  import { hourOfMinute } from '../lib/format.ts';
  import { ghost as rest, mask, shake, secondsOfDay } from '../lib/hourMask.ts';

  interface Props {
    value: number | null;
    onchange: (minute: number | null) => void;
    label: string;
    // Dentro de um formulário o Enter já o envia, e quem usa pode não ter
    // mais nada a fazer.
    onenter?: () => void;
    // O foco ao abrir, para o primeiro campo do popover.
    focusNow?: boolean;
  }

  let { value, onchange, label, onenter, focusNow = false }: Props = $props();

  const format = (m: number | null) => (m === null ? '' : hourOfMinute(m));

  let input: HTMLInputElement | undefined;
  // Só o valor de quando o campo nasce: daí em diante quem escreve é a pessoa,
  // e o popover que o usa monta campos novos a cada abertura.
  let current = untrack(() => value);
  const initial = format(current);
  let text = $state(initial);
  let typed = $state<string[]>(initial ? initial.split(':') : ['']);
  let isFocused = $state(false);

  const ghost = $derived(isFocused ? rest(typed, 2) : '');

  $effect(() => {
    if (focusNow) input?.focus();
  });

  function typeIn(e: Event & { currentTarget: HTMLInputElement }) {
    const m = mask(
      e.currentTarget.value,
      e instanceof InputEvent && e.inputType.startsWith('delete'),
      2,
    );
    text = m.g.join(':');
    typed = m.g;
    // Direto no DOM, como no relógio: o dígito recusado não muda o texto.
    e.currentTarget.value = text;
    if (m.rejected) shake(input);
    const s = secondsOfDay(m.g);
    current = s === null ? null : s / 60;
    onchange(current);
  }

  // Ao sair, o campo mostra o horário inteiro: "17:3" vira "17:30".
  function leave() {
    isFocused = false;
    text = format(current);
    typed = text ? text.split(':') : [''];
  }
</script>

<label class="field">
  <span class="label">{label}</span>
  <span class="box">
    {#if isFocused}
      <span class="mirror mono" aria-hidden="true">{text}<i>{ghost}</i></span>
    {/if}
    <input
      bind:this={input}
      class="mono"
      value={text}
      placeholder="--:--"
      inputmode="decimal"
      enterkeyhint="done"
      autocomplete="off"
      spellcheck="false"
      onfocus={() => (isFocused = true)}
      onblur={leave}
      oninput={typeIn}
      onkeydown={(e) => {
        if (e.key === 'Enter') {
          leave();
          onenter?.();
        }
      }}
    />
  </span>
</label>

<style>
  .field {
    display: grid;
    gap: 4px;
    font-size: 13px;
    color: var(--dim);
  }

  /* O fundo é da caixa, e o input transparente por cima do espelho: o texto
     digitado e o resto em cinza ficam na mesma linha de base. */
  .box {
    position: relative;
    display: block;
    height: 44px;
    border-radius: 8px;
    background: var(--panel-2);
  }

  input,
  .mirror {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    margin: 0;
    /* 16px também evita o zoom automático do Safari ao focar o campo. */
    font-size: 16px;
    line-height: 24px;
    padding: 9px 12px;
    border-radius: 8px;
    white-space: pre;
  }

  input {
    z-index: 1;
    background: transparent;
  }
  input:focus-visible {
    outline: none;
    border-color: var(--accent);
  }
  input::placeholder {
    color: var(--dim);
    opacity: 0.55;
  }

  .mirror {
    border: 1px solid transparent;
    color: transparent;
    pointer-events: none;
  }
  .mirror i {
    font-style: normal;
    color: var(--dim);
    opacity: 0.6;
  }
</style>
