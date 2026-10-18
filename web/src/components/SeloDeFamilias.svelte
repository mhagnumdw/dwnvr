<!--
  Os ícones das famílias de objeto num canto de uma miniatura: um por família
  presente, na ordem de prioridade. É o mesmo selo na grade de Detecções e na
  tira de miniaturas das Gravações. Quem usa dá `position: relative` à
  miniatura.
-->
<script lang="ts">
  import { familia, iconeDaFamilia, iconeURL } from '../lib/icones.ts';

  interface Props {
    // As famílias, sem repetição; a ordem quem dá é o selo.
    familias: readonly string[];
  }

  let { familias }: Props = $props();

  const ordem = $derived(
    [...familias].sort((a, b) => familia(a).prioridade - familia(b).prioridade),
  );
</script>

{#if ordem.length}
  <span class="selo">
    {#each ordem as f (f)}
      {@const icone = iconeDaFamilia(f)}
      {#if icone}<img
          src={iconeURL(icone, 12, familia(f).cor)}
          alt=""
          width="12"
          height="12"
        />{/if}
    {/each}
  </span>
{/if}

<style>
  .selo {
    position: absolute;
    right: 4px;
    top: 4px;
    display: inline-flex;
    gap: 4px;
    padding: 3px 4px;
    border-radius: 6px;
    background: rgba(13, 17, 23, 0.8);
  }

  img {
    display: block;
  }
</style>
