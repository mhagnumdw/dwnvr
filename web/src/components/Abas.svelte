<!--
  A fileira de abas de um formulário: só os botões. Os painéis ficam com quem
  usa, que os amarra pelo id (`{prefixo}-painel-{id}`, com role="tabpanel"):

    <Abas abas={[{ id: 'gravacao', rotulo: 'Gravação' }, ...]} bind:ativa prefixo="cam" />
    <div role="tabpanel" id="cam-painel-gravacao" aria-labelledby="cam-aba-gravacao">…</div>

  As setas trocam de aba, como o padrão de abas do WAI-ARIA pede, e só a aba
  ativa entra no Tab: dele em diante, o Tab segue para o painel.
-->
<script lang="ts">
  import { tick } from 'svelte';

  interface Aba {
    id: string;
    rotulo: string;
  }

  let {
    abas,
    ativa = $bindable(''),
    prefixo,
  }: { abas: Aba[]; ativa?: string; prefixo: string } = $props();

  let fileira = $state<HTMLDivElement>();

  async function teclas(e: KeyboardEvent) {
    const i = abas.findIndex((a) => a.id === ativa);
    const alvo =
      e.key === 'ArrowRight'
        ? (i + 1) % abas.length
        : e.key === 'ArrowLeft'
          ? (i - 1 + abas.length) % abas.length
          : e.key === 'Home'
            ? 0
            : e.key === 'End'
              ? abas.length - 1
              : -1;
    if (alvo < 0) return;
    e.preventDefault();
    ativa = abas[alvo]?.id ?? ativa;
    await tick();
    fileira?.querySelector<HTMLButtonElement>('[aria-selected="true"]')?.focus();
  }
</script>

<div class="abas" role="tablist" tabindex="-1" bind:this={fileira} onkeydown={teclas}>
  {#each abas as a (a.id)}
    <button
      type="button"
      role="tab"
      id="{prefixo}-aba-{a.id}"
      aria-selected={a.id === ativa}
      aria-controls="{prefixo}-painel-{a.id}"
      tabindex={a.id === ativa ? 0 : -1}
      onclick={() => (ativa = a.id)}
    >
      {a.rotulo}
    </button>
  {/each}
</div>

<style>
  /* A linha de baixo vai de borda a borda do card: a margem negativa desfaz o
     padding dele, que é o mesmo do .card (14px). */
  .abas {
    display: flex;
    gap: 2px;
    margin: 0 -14px;
    padding: 0 14px;
    border-bottom: 1px solid var(--line);
    overflow-x: auto;
    scrollbar-width: none;
  }
  button {
    flex: none;
    min-height: 44px;
    padding: 6px 12px;
    border: 0;
    border-bottom: 2px solid transparent;
    border-radius: 0;
    background: transparent;
    color: var(--dim);
    white-space: nowrap;
  }
  button[aria-selected='true'] {
    color: var(--fg);
    border-bottom-color: var(--accent);
  }
  @media (hover: hover) {
    button:hover:not([aria-selected='true']) {
      color: var(--fg);
      border-color: transparent;
      border-bottom-color: var(--line);
    }
  }
</style>
