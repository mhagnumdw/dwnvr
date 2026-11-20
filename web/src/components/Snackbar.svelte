<!--
  The snackbar rendering (lib/snackbar.svelte.ts). Mounted once, in App.

  The key is the id: a notice that replaces another enters again with the
  animation, and not just swaps the text in place.
-->
<script lang="ts">
  import { fly } from 'svelte/transition';
  import { snackbar, closeNotice } from '../lib/snackbar.svelte.ts';
</script>

{#if snackbar.current}
  {@const notice = snackbar.current}
  {#key notice.id}
    <div
      class="snackbar"
      class:with-action={notice.action}
      role="status"
      in:fly={{ y: 8, duration: 180 }}
      out:fly={{ y: 8, duration: 180 }}
    >
      <span
        >{#if notice.forte}<strong>{notice.forte}</strong>{' '}{/if}{notice.texto}</span
      >
      {#if notice.action}
        <button
          class="ghost"
          onclick={() => {
            closeNotice(notice.id);
            notice.action?.fazer();
          }}>{notice.action.label}</button
        >
      {/if}
    </div>
  {/key}
{/if}

<style>
  /* Above the bottom navigation, centered. */
  .snackbar {
    position: fixed;
    z-index: 40;
    left: 50%;
    bottom: calc(var(--nav-h) + 12px);
    translate: -50% 0;
    max-width: calc(100vw - 20px);
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 11px 16px;
    background: var(--panel-2);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
    font-size: 14px;
    white-space: nowrap;
  }

  /* With the button, it sets the height, and the right border keeps its
     breathing room. */
  .snackbar.with-action {
    padding: 0 6px 0 14px;
  }

  span {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* On the desktop the navigation moves up, and the notice drops closer to the edge. */
  @media (min-width: 720px) {
    .snackbar {
      bottom: 20px;
    }
  }

  button {
    border: 0;
    color: var(--accent);
    font-weight: 600;
  }
</style>
