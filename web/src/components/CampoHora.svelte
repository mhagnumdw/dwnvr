<script lang="ts">
  // Um campo de horário HH:MM, com a máscara do relógio (lib/mascaraHora.ts):
  // "22" já é 22:00 e "17:3" é 17:30, com o resto em cinza mostrando isso.
  // O valor é o minuto do dia, ou nulo com o campo vazio: `valor` é o de
  // partida, e `onmuda` recebe cada mudança.
  import { untrack } from 'svelte';
  import { horaDoMinuto } from '../lib/format.ts';
  import { fantasma as resto, mascara, sacudir, segundosDoDia } from '../lib/mascaraHora.ts';

  interface Props {
    valor: number | null;
    onmuda: (minuto: number | null) => void;
    rotulo: string;
    // Dentro de um formulário o Enter já o envia, e quem usa pode não ter
    // mais nada a fazer.
    onenter?: () => void;
    // O foco ao abrir, para o primeiro campo do popover.
    focar?: boolean;
  }

  let { valor, onmuda, rotulo, onenter, focar = false }: Props = $props();

  const formata = (m: number | null) => (m === null ? '' : horaDoMinuto(m));

  let input: HTMLInputElement | undefined;
  // Só o valor de quando o campo nasce: daí em diante quem escreve é a pessoa,
  // e o popover que o usa monta campos novos a cada abertura.
  let atual = untrack(() => valor);
  const inicial = formata(atual);
  let texto = $state(inicial);
  let digitados = $state<string[]>(inicial ? inicial.split(':') : ['']);
  let focado = $state(false);

  const fantasma = $derived(focado ? resto(digitados, 2) : '');

  $effect(() => {
    if (focar) input?.focus();
  });

  function digitar(e: Event & { currentTarget: HTMLInputElement }) {
    const m = mascara(
      e.currentTarget.value,
      e instanceof InputEvent && e.inputType.startsWith('delete'),
      2,
    );
    texto = m.g.join(':');
    digitados = m.g;
    // Direto no DOM, como no relógio: o dígito recusado não muda o texto.
    e.currentTarget.value = texto;
    if (m.recusou) sacudir(input);
    const s = segundosDoDia(m.g);
    atual = s === null ? null : s / 60;
    onmuda(atual);
  }

  // Ao sair, o campo mostra o horário inteiro: "17:3" vira "17:30".
  function sair() {
    focado = false;
    texto = formata(atual);
    digitados = texto ? texto.split(':') : [''];
  }
</script>

<label class="campo">
  <span class="rotulo">{rotulo}</span>
  <span class="caixa">
    {#if focado}
      <span class="espelho mono" aria-hidden="true">{texto}<i>{fantasma}</i></span>
    {/if}
    <input
      bind:this={input}
      class="mono"
      value={texto}
      placeholder="--:--"
      inputmode="decimal"
      enterkeyhint="done"
      autocomplete="off"
      spellcheck="false"
      onfocus={() => (focado = true)}
      onblur={sair}
      oninput={digitar}
      onkeydown={(e) => {
        if (e.key === 'Enter') {
          sair();
          onenter?.();
        }
      }}
    />
  </span>
</label>

<style>
  .campo {
    display: grid;
    gap: 4px;
    font-size: 13px;
    color: var(--dim);
  }

  /* O fundo é da caixa, e o input transparente por cima do espelho: o texto
     digitado e o resto em cinza ficam na mesma linha de base. */
  .caixa {
    position: relative;
    display: block;
    height: 44px;
    border-radius: 8px;
    background: var(--panel-2);
  }

  input,
  .espelho {
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

  .espelho {
    border: 1px solid transparent;
    color: transparent;
    pointer-events: none;
  }
  .espelho i {
    font-style: normal;
    color: var(--dim);
    opacity: 0.6;
  }
</style>
