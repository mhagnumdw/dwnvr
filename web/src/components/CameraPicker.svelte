<!--
  The list of cameras that the "câmeras" button opens, in Live and Detections: a
  one-tap button per camera, with a green dot and a blue border on the checked
  ones. Whoever uses it decides the container (a card in the flow, a popover)
  and what "checked" means; here we only draw and report the click.

  <CameraPicker
  cameras={cameras.list}
  checked={(id) => selected.has(id)}
  all={allChecked}
  ontoggle={toggle}
  onall={all}
  error={cameras.error}
  />

  `all` is what swaps "todas" for "limpar" on the first button. `error` is
  `cameras.error`: with the list empty because the load failed, the picker says
  so instead of "nenhuma câmera cadastrada", which would be false.
-->
<script lang="ts">
  import type { Camera } from '../lib/api.ts';

  let {
    cameras,
    checked,
    all,
    ontoggle,
    onall,
    error = null,
  }: {
    cameras: Camera[];
    checked: (id: string) => boolean;
    all: boolean;
    ontoggle: (id: string) => void;
    onall: () => void;
    error?: string | null;
  } = $props();
</script>

<div class="chips">
  {#if cameras.length}
    <button class="camchip all" onclick={onall}>
      {all ? 'limpar' : 'todas'}
    </button>
    {#each cameras as c (c.id)}
      <button
        class="camchip"
        class:on={checked(c.id)}
        aria-pressed={checked(c.id)}
        onclick={() => ontoggle(c.id)}
      >
        <span class="cam-name">{c.name}</span>
      </button>
    {/each}
  {:else if error}
    <p class="muted small">Não deu para carregar as câmeras: {error}</p>
  {:else}
    <p class="muted small">nenhuma câmera cadastrada</p>
  {/if}
</div>

<style>
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .camchip {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    min-height: 40px;
    padding: 0 14px;
    border-radius: 10px;
    font-weight: 600;
    font-size: 14px;
    color: var(--dim);
    max-width: 100%;
  }
  .cam-name {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .camchip::before {
    content: '';
    flex: none;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--line);
  }
  .camchip.on {
    color: var(--fg);
    border-color: var(--accent);
    background: var(--active-bg);
  }
  .camchip.on::before {
    background: var(--ok);
  }
  .camchip.all {
    font-weight: 500;
    background: transparent;
  }
  .camchip.all::before {
    display: none;
  }
</style>
