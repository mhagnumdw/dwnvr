<script lang="ts">
  // Rodapé das telas que releem a saúde (Câmeras e Diagnóstico): de quanto em
  // quanto tempo os dados são relidos e de quando é o que está na tela.
  //
  // A leitura vai como hora, e não como "há Xs": o Svelte só redesenha quando o
  // estado muda, e isso acontece logo depois de cada leitura, então a conta
  // feita aqui daria sempre "0s". Os segundos da hora andam a cada leitura, e
  // isso já mostra que a tela está viva, sem timer próprio. Quando a leitura
  // falha, a hora pararia sem ninguém notar: por isso o aviso, que se redesenha
  // sozinho porque health.failingSince muda na primeira falha.
  //
  // As horas passam pelo quando(), que põe a data na frente quando não é de
  // hoje: a aba que ficou oculta a noite toda volta com os dados de ontem.
  import { quando } from '../lib/avisos.ts';
  import { health, HEALTH_POLL_MS } from '../lib/state.svelte.ts';
</script>

<!-- O separador vai como expressão porque o Svelte apara o espaço no início de
     um bloco {#if}, e sem isso sai "5s· última leitura". -->
{#if health.failingSince}
  <p class="small falhando">
    Sem resposta do servidor desde {quando(
      health.failingSince,
      Date.now(),
    )}{#if health.updatedAt}{' · '}os dados na tela são de {quando(
        health.updatedAt,
        Date.now(),
      )}{/if}
  </p>
{:else}
  <p class="muted small">
    Atualizado a cada {HEALTH_POLL_MS / 1000}s{#if health.updatedAt}{' · '}última leitura às {quando(
        health.updatedAt,
        Date.now(),
      )}{/if}
  </p>
{/if}

<style>
  .falhando {
    color: var(--warn);
  }
</style>
