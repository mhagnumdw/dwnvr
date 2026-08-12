<!--
  Confirmação de uma ação, no lugar do confirm() do navegador - que trava a
  aba inteira, ignora o tema e no celular aparece grudado no topo, longe do
  polegar. O texto do corpo vem por snippet porque explicar a consequência
  costuma pedir mais que uma frase seca.

    <ConfirmDialog title="Remover X?" confirmLabel="remover" danger
                   onconfirm={…} oncancel={…}>
      As gravações <strong>não</strong> são apagadas.
    </ConfirmDialog>
-->
<script>
  import Modal from './Modal.svelte';

  let {
    title,
    confirmLabel = 'confirmar',
    cancelLabel = 'cancelar',
    danger = false,
    onconfirm,
    oncancel,
    children,
  } = $props();

  let cancelar = $state(null);

  // O foco entra no diálogo pelo cancelar: quando a ação é destrutiva, um Enter
  // por reflexo deve desfazer, não executar.
  $effect(() => cancelar?.focus());
</script>

<Modal onclose={oncancel}>
  <h3>{title}</h3>
  {#if children}<div class="body small">{@render children()}</div>{/if}
  <div class="row">
    <span class="spacer"></span>
    <button type="button" class="ghost" bind:this={cancelar} onclick={oncancel}>
      {cancelLabel}
    </button>
    <button type="button" class:danger class:primary={!danger} onclick={onconfirm}>
      {confirmLabel}
    </button>
  </div>
</Modal>

<style>
  h3 { margin: 0; font-size: 15px; }
  .body { margin: -4px 0 0; color: var(--dim); }
</style>
