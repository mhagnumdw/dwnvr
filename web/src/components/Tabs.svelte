<!--
  The row of tabs of a form: only the buttons. The panels stay with whoever uses
  it, who ties them by id (`{prefix}-painel-{id}`, with role="tabpanel"):

  <Tabs tabs={[{ id: 'gravacao', label: 'Gravação' }, ...]} bind:active
  prefix="cam" />
  <div role="tabpanel" id="cam-painel-gravacao"
  aria-labelledby="cam-aba-gravacao">…</div>

  The arrows switch tabs, as the WAI-ARIA tabs pattern asks, and only the active
  tab enters the Tab order: from it on, Tab moves on to the panel.
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
  /* The bottom line runs from card edge to card edge: the negative margin
     undoes its padding, which is the same as the .card's (14px). */
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
