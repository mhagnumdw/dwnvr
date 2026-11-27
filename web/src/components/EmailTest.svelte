<!--
  The test of the smtp section, in the "Este servidor" card of Diagnostics: it
  sends an email to the typed address and shows the email server's response as
  it came. It is a separate chunk: it only downloads with email turned on.
-->
<script lang="ts">
  import { api, messageOf } from '../lib/api.ts';

  let dest = $state('');
  let sending = $state(false);
  let result = $state<{ ok: boolean; text: string } | null>(null);

  async function send(e: SubmitEvent) {
    e.preventDefault();
    sending = true;
    result = null;
    try {
      const r = await api.testEmail(dest);
      // Delivered to the server is not delivered to the inbox: dwnvr does not
      // see spam.
      result = {
        ok: true,
        text: `Enviado para ${r.sentTo}. Confira a caixa de entrada e o spam.`,
      };
    } catch (err) {
      result = { ok: false, text: messageOf(err) };
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
    <p class="small" class:error={!result.ok}>{result.text}</p>
  {/if}
</section>

<style>
  .test {
    display: grid;
    gap: 6px;
  }

  /* The same title as the log rows, right below in the card. */
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
