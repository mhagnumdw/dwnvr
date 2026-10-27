<!--
  O teste da seção smtp, no card "Este servidor" do Diagnóstico: manda um
  e-mail para o endereço digitado e mostra a resposta do servidor de e-mail do
  jeito que ela veio. É chunk à parte: só baixa com o e-mail ligado.
-->
<script lang="ts">
  import { api, mensagemDe } from '../lib/api.ts';

  let para = $state('');
  let enviando = $state(false);
  let resultado = $state<{ ok: boolean; texto: string } | null>(null);

  async function enviar(e: SubmitEvent) {
    e.preventDefault();
    enviando = true;
    resultado = null;
    try {
      const r = await api.testarEmail(para);
      // Entregue ao servidor não é entregue na caixa: o spam o dwnvr não vê.
      resultado = {
        ok: true,
        texto: `Enviado para ${r.sentTo}. Confira a caixa de entrada e o spam.`,
      };
    } catch (err) {
      resultado = { ok: false, texto: mensagemDe(err) };
    } finally {
      enviando = false;
    }
  }
</script>

<section class="teste">
  <p class="titulo muted">Testar o e-mail</p>
  <form class="row" onsubmit={enviar}>
    <input
      inputmode="email"
      bind:value={para}
      required
      placeholder="fulano@exemplo.com"
      aria-label="para onde vai o e-mail de teste"
      autocapitalize="none"
      autocorrect="off"
      spellcheck="false"
    />
    <button type="submit" disabled={enviando}>{enviando ? 'enviando…' : 'enviar'}</button>
  </form>
  {#if resultado}
    <p class="small" class:erro={!resultado.ok}>{resultado.texto}</p>
  {/if}
</section>

<style>
  .teste {
    display: grid;
    gap: 6px;
  }

  /* O mesmo título das linhas do log, logo abaixo no card. */
  .titulo {
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

  .erro {
    color: var(--bad);
    overflow-wrap: anywhere;
  }
</style>
