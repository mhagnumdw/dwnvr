<!--
  Os ícones das famílias de objeto num canto de uma miniatura: um por família
  presente, na ordem de prioridade. É o mesmo selo na grade de Detecções e na
  tira de miniaturas das Gravações. Quem usa dá `position: relative` à
  miniatura e põe as famílias no nome do botão (`nomesDasFamilias`): os ícones
  são imagem sem texto, e o nome é o que o leitor de tela lê. Em Detecções, a
  detecção que alertou ganha o sino antes das famílias; no card da câmera, a
  área do alerta ganha só o sino, sem família.
-->
<script lang="ts">
  import Icon from './Icon.svelte';
  import { family, familyIcon, iconURL, byPriority } from '../lib/icons.ts';

  interface Props {
    // As famílias, sem repetição; a ordem quem dá é o selo.
    families: readonly string[];
    // Algum objeto da detecção alertou.
    alert?: boolean;
  }

  let { families, alert = false }: Props = $props();

  const order = $derived(byPriority(families));
</script>

{#if order.length || alert}
  <span class="badge">
    {#if alert}<span class="bell"><Icon name="bell" size={12} stroke={2.4} /></span>{/if}
    {#each order as f (f)}
      {@const icon = familyIcon(f)}
      {#if icon}<img src={iconURL(icon, 12, family(f).color)} alt="" width="12" height="12" />{/if}
    {/each}
  </span>
{/if}

<style>
  .badge {
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

  .bell {
    color: var(--warn);
  }
</style>
