<script lang="ts">
  // O intervalo de horário da tela Detecções: desligado é um botão com o
  // relógio, que abre os campos "De" e "Até"; ligado vira um chip com as duas
  // horas e o ✕ que o desliga. Tocar nas horas do chip reabre os campos.
  //
  // Os horários são minutos do dia. O "até" menor que o "de" termina no dia
  // seguinte: 22:00 até 06:00 é a noite. Quem decide o dia é a tela.
  import CampoHora from './CampoHora.svelte';
  import Icone from './Icone.svelte';
  import { horaDoMinuto as hora } from '../lib/format.ts';

  interface Props {
    intervalo: { de: number; ate: number } | null;
    onaplicar: (de: number, ate: number) => void;
    onlimpar: () => void;
  }

  let { intervalo, onaplicar, onlimpar }: Props = $props();

  let raiz: HTMLDivElement | undefined;
  let aberto = $state(false);
  let de = $state<number | null>(null);
  let ate = $state<number | null>(null);

  function abre() {
    if (aberto) {
      aberto = false;
      return;
    }
    de = intervalo?.de ?? null;
    ate = intervalo?.ate ?? null;
    aberto = true;
  }

  function mudaDe(m: number | null) {
    de = m;
  }

  function mudaAte(m: number | null) {
    ate = m;
  }

  function aplica() {
    if (de === null || ate === null) return;
    aberto = false;
    onaplicar(de, ate);
  }

  function fora(e: MouseEvent) {
    if (aberto && raiz && !(e.target instanceof Node && raiz.contains(e.target))) aberto = false;
  }

  function tecla(e: KeyboardEvent) {
    if (e.key === 'Escape') aberto = false;
  }

  function limpa() {
    aberto = false;
    onlimpar();
  }
</script>

<svelte:window onclick={fora} onkeydown={tecla} />

<div class="intervalo" class:ligado={intervalo} bind:this={raiz}>
  {#if intervalo}
    <span class="chip-intervalo">
      <button
        class="ghost horas mono"
        onclick={abre}
        aria-expanded={aberto}
        title="mudar o intervalo"
        aria-label="intervalo de {hora(intervalo.de)} até {hora(intervalo.ate)}; mudar"
      >
        <Icone nome="relogio" tamanho={14} traco={2.4} />
        {hora(intervalo.de)} – {hora(intervalo.ate)}
        {#if intervalo.ate < intervalo.de}<small>(dia seguinte)</small>{/if}
      </button>
      <button
        class="ghost limpa"
        onclick={limpa}
        aria-label="limpar o intervalo"
        title="limpar o intervalo"
      >
        <Icone nome="fechar" tamanho={16} traco={2.2} />
      </button>
    </span>
  {:else}
    <button
      class="ghost abre"
      onclick={abre}
      aria-expanded={aberto}
      aria-label="intervalo de horário"
      title="mostrar só um intervalo de horário"
    >
      <Icone nome="relogio" tamanho={16} traco={2.2} />
      <span class="rotulo">intervalo</span>
    </button>
  {/if}

  {#if aberto}
    <div class="popover" role="dialog" aria-label="intervalo de horário">
      <div class="campos">
        <CampoHora rotulo="De" valor={de} onmuda={mudaDe} onenter={aplica} focar />
        <CampoHora rotulo="Até" valor={ate} onmuda={mudaAte} onenter={aplica} />
      </div>
      <p class="dica">
        Se o "até" for menor que o "de", o intervalo vai até o dia seguinte: 22:00 até 06:00 é a
        noite.
      </p>
      <div class="acoes">
        {#if intervalo}<button class="ghost" onclick={limpa}>limpar</button>{/if}
        <button class="primary" onclick={aplica} disabled={de === null || ate === null}
          >aplicar</button
        >
      </div>
    </div>
  {/if}
</div>

<style>
  .intervalo {
    position: relative;
    flex: none;
  }

  /* A forma dos botões vizinhos da barra. */
  .abre {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 8px 11px;
    color: var(--dim);
  }

  /* Ligado: o "ligado" dos botões da barra (borda e fundo do accent). */
  .chip-intervalo {
    display: inline-flex;
    align-items: stretch;
    border: 1px solid var(--accent);
    border-radius: 8px;
    background: var(--ativo-fundo);
    overflow: hidden;
  }
  .chip-intervalo button {
    min-height: 42px;
    border: 0;
    border-radius: 0;
  }
  .horas {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 0 4px 0 11px;
    white-space: nowrap;
  }
  .horas small {
    color: var(--dim);
    font-size: 12px;
  }
  .limpa {
    padding: 0 10px;
    color: var(--dim);
  }
  @media (hover: hover) {
    .limpa:hover {
      color: var(--fg);
    }
  }

  .popover {
    position: absolute;
    top: calc(100% + 6px);
    left: 0;
    z-index: 40;
    width: 260px;
    display: grid;
    gap: 12px;
    padding: 14px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  }

  .campos {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
  }

  .dica {
    margin: 0;
    font-size: 12px;
    line-height: 1.4;
    color: var(--dim);
  }

  .acoes {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  /* No celular o botão fica perto da borda direita e o popover vazaria da
     tela: sem posição própria, ele se prende ao primeiro ancestral
     posicionado - a barra da tela, que é sticky - e abre logo abaixo dela. */
  @media (max-width: 719px) {
    .intervalo {
      position: static;
    }
    /* Só o ícone, como os vizinhos da barra. Ligado, o chip não cabe na
       linha do dia e a tela o põe numa linha só dele, de ponta a ponta. */
    .rotulo {
      display: none;
    }
    .chip-intervalo {
      width: 100%;
    }
    .horas {
      flex: 1;
    }
    .popover {
      top: 100%;
      left: 10px;
      width: min(300px, calc(100% - 20px));
    }
  }
</style>
