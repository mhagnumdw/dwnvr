<script lang="ts">
  // Footer of the screens that reread health (Cameras and Diagnostics): how
  // often the data is reread and when the data on screen is from.
  //
  // The reading is shown as a time, not as "Xs ago": Svelte only redraws when
  // state changes, and that happens right after each reading, so a computation
  // done here would always give "0s". The seconds of the time move on each
  // reading, and that already shows the screen is alive, with no timer of its
  // own. When the reading fails, the time would stall unnoticed: hence the
  // warning, which redraws by itself because health.failingSince changes on the
  // first failure.
  //
  // The time of a good reading goes through when(), which puts the date in
  // front when it is not today: a tab left hidden all night comes back with
  // yesterday's data, and the reading that succeeds redraws the sentence with
  // today's day. The failure warning always carries the date: consecutive
  // failures do not change the state, so nothing redraws it, and an outage that
  // began at 23:50 would say "23:50:00" the next morning. With the date fixed,
  // the text does not age.
  import { when } from '../lib/notices.ts';
  import { ddmm, hhmmss } from '../lib/format.ts';
  import { health, HEALTH_POLL_MS } from '../lib/state.svelte.ts';

  const withDate = (ms: number) => `${ddmm(ms)} ${hhmmss(ms)}`;
</script>

<!-- The separator goes as an expression because Svelte trims the space at the
     start of an {#if} block, and without it the output is "5s· última leitura". -->
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
