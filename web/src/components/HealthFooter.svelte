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
  // A hora da leitura boa passa pelo quando(), que põe a data na frente quando
  // não é de hoje: a aba que ficou oculta a noite toda volta com os dados de
  // ontem, e a leitura que dá certo redesenha a frase com o dia de agora. O
  // aviso de falha leva a data sempre: falha seguida não muda o estado, então
  // nada o redesenha, e a queda que começou às 23:50 diria "23:50:00" na manhã
  // seguinte. Com a data fixa, o texto não envelhece.
  import { when } from '../lib/notices.ts';
  import { ddmm, hhmmss } from '../lib/format.ts';
  import { health, HEALTH_POLL_MS } from '../lib/state.svelte.ts';

  const withDate = (ms: number) => `${ddmm(ms)} ${hhmmss(ms)}`;
</script>

<!-- O separador vai como expressão porque o Svelte apara o espaço no início de
     um bloco {#if}, e sem isso sai "5s· última leitura". -->
{#if health.failingSince}
  <p class="small failing">
    Sem resposta do servidor desde {withDate(health.failingSince)}{#if health.updatedAt}{' · '}os
      dados na tela são de {withDate(health.updatedAt)}{/if}{' · '}tentando de novo a cada {HEALTH_POLL_MS /
      1000}s
  </p>
{:else}
  <p class="muted small">
    Atualizado a cada {HEALTH_POLL_MS / 1000}s{#if health.updatedAt}{' · '}última leitura às {when(
        health.updatedAt,
        Date.now(),
      )}{/if}
  </p>
{/if}

<style>
  .failing {
    color: var(--warn);
  }
</style>
