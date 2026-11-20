<!--
  Confirmation of an action, in place of the browser's confirm() - which freezes
  the whole tab, ignores the theme and on the phone shows up stuck to the top,
  far from the thumb. The body text comes as a snippet because explaining the
  consequence usually takes more than a dry sentence.

  <ConfirmDialog title="Remover X?" confirmLabel="remover" danger
  onconfirm={…} oncancel={…}>
  As gravações <strong>não</strong> são apagadas.
  </ConfirmDialog>
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import Modal from './Modal.svelte';

  interface Props {
    title: string;
    confirmLabel?: string;
    cancelLabel?: string;
    danger?: boolean;
    onconfirm: () => void;
    oncancel: () => void;
    children?: Snippet;
  }

  let {
    title,
    confirmLabel = 'confirmar',
    cancelLabel = 'cancelar',
    danger = false,
    onconfirm,
    oncancel,
    children,
  }: Props = $props();

  let cancel = $state<HTMLButtonElement | null>(null);

  // Focus enters the dialog on cancel: when the action is destructive, a reflex
  // Enter should undo, not execute.
  $effect(() => {
    cancel?.focus();
  });
</script>

<Modal onclose={oncancel}>
  <h3>{title}</h3>
  {#if children}<div class="body small">{@render children()}</div>{/if}
  <div class="row">
    <span class="spacer"></span>
    <button type="button" class="ghost" bind:this={cancel} onclick={oncancel}>
      {cancelLabel}
    </button>
    <button type="button" class:danger class:primary={!danger} onclick={onconfirm}>
      {confirmLabel}
    </button>
  </div>
</Modal>

<style>
  h3 {
    margin: 0;
    font-size: 15px;
  }
  .body {
    margin: -4px 0 0;
    color: var(--dim);
  }
</style>
