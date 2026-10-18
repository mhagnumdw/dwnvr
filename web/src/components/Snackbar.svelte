<!--
  O desenho da snackbar (lib/snackbar.svelte.ts). Montado uma vez, no App.

  A chave é o id: um aviso que substitui outro entra de novo com a animação,
  e não só troca o texto no lugar.
-->
<script lang="ts">
  import { fly } from 'svelte/transition';
  import { snackbar, fecharAviso } from '../lib/snackbar.svelte.ts';
</script>

{#if snackbar.atual}
  {@const aviso = snackbar.atual}
  {#key aviso.id}
    <div
      class="snackbar"
      class:com-acao={aviso.acao}
      role="status"
      in:fly={{ y: 8, duration: 180 }}
      out:fly={{ y: 8, duration: 180 }}
    >
      <span
        >{#if aviso.forte}<strong>{aviso.forte}</strong>{' '}{/if}{aviso.texto}</span
      >
      {#if aviso.acao}
        <button
          class="ghost"
          onclick={() => {
            fecharAviso(aviso.id);
            aviso.acao?.fazer();
          }}>{aviso.acao.rotulo}</button
        >
      {/if}
    </div>
  {/key}
{/if}

<style>
  /* Acima da navegação inferior, centrada. */
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

  /* Com o botão, quem dá a altura é ele, e a borda direita fica com o respiro
     dele. */
  .snackbar.com-acao {
    padding: 0 6px 0 14px;
  }

  span {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  /* No desktop a navegação sobe, e o aviso desce para perto da borda. */
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
