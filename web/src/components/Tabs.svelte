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

  interface Tab {
    id: string;
    label: string;
  }

  let {
    tabs,
    active = $bindable(''),
    prefix,
  }: { tabs: Tab[]; active?: string; prefix: string } = $props();

  let lane = $state<HTMLDivElement>();

  async function keys(e: KeyboardEvent) {
    const i = tabs.findIndex((a) => a.id === active);
    const target =
      e.key === 'ArrowRight'
        ? (i + 1) % tabs.length
        : e.key === 'ArrowLeft'
          ? (i - 1 + tabs.length) % tabs.length
          : e.key === 'Home'
            ? 0
            : e.key === 'End'
              ? tabs.length - 1
              : -1;
    if (target < 0) return;
    e.preventDefault();
    active = tabs[target]?.id ?? active;
    await tick();
    lane?.querySelector<HTMLButtonElement>('[aria-selected="true"]')?.focus();
  }
</script>

<div class="tabs" role="tablist" tabindex="-1" bind:this={lane} onkeydown={keys}>
  {#each tabs as a (a.id)}
    <button
      type="button"
      role="tab"
      id="{prefix}-aba-{a.id}"
      aria-selected={a.id === active}
      aria-controls="{prefix}-painel-{a.id}"
      tabindex={a.id === active ? 0 : -1}
      onclick={() => (active = a.id)}
    >
      {a.label}
    </button>
  {/each}
</div>

<style>
  /* A linha de baixo vai de borda a borda do card: a margem negativa desfaz o
     padding dele, que é o mesmo do .card (14px). */
  .tabs {
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
