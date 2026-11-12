<!--
  O teste da seção smtp, no card "Este servidor" do Diagnóstico: manda um
  e-mail para o endereço digitado e mostra a resposta do servidor de e-mail do
  jeito que ela veio. É chunk à parte: só baixa com o e-mail ligado.
-->
<script lang="ts">
  import { api, messageOf } from '../lib/api.ts';

  let dest = $state('');
  let sending = $state(false);
  let result = $state<{ ok: boolean; texto: string } | null>(null);

  async function send(e: SubmitEvent) {
    e.preventDefault();
    sending = true;
    result = null;
    try {
      const r = await api.testEmail(dest);
      // Entregue ao servidor não é entregue na caixa: o spam o dwnvr não vê.
      result = {
        ok: true,
        texto: `Enviado para ${r.sentTo}. Confira a caixa de entrada e o spam.`,
      };
    } catch (err) {
      result = { ok: false, texto: messageOf(err) };
    } finally {
      sending = false;
    }
  }
</script>

<section class="test">
  <p class="title muted">Testar o e-mail</p>
  <form class="row" onsubmit={send}>
    <input
      inputmode="email"
      bind:value={dest}
      required
      placeholder="fulano@exemplo.com"
      aria-label="para onde vai o e-mail de teste"
      autocapitalize="none"
      autocorrect="off"
      spellcheck="false"
    />
    <button type="submit" disabled={sending}>{sending ? 'enviando…' : 'enviar'}</button>
  </form>
  {#if result}
    <p class="small" class:error={!result.ok}>{result.texto}</p>
  {/if}
</section>

<style>
  .test {
    display: grid;
    gap: 6px;
  }

  /* O mesmo título das linhas do log, logo abaixo no card. */
  .title {
    margin: 0;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-size: 11px;
  }

  p {
    margin: 0;
  }

  form input {
    flex: 1;
    min-width: 0;
  }

  .error {
    color: var(--bad);
    overflow-wrap: anywhere;
  }
</style>
