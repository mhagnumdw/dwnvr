<script>
  import { api } from '../lib/api.js';
  import { build } from '../lib/state.svelte.js';

  let { onSuccess } = $props();

  let username = $state('');
  let password = $state('');
  let error = $state('');
  let busy = $state(false);

  async function submit(e) {
    e.preventDefault();
    busy = true;
    error = '';
    try {
      await api.login(username, password);
      onSuccess();
    } catch (err) {
      error = err.message;
      password = '';
    } finally {
      busy = false;
    }
  }
</script>

<div class="screen">
  <form class="card" onsubmit={submit}>
    <h1>
      <!-- Mesma marca do header e do favicon, servida de public/. -->
      <img class="mark" src="/favicon.svg" alt="" width="32" height="32" />
      dwnvr
    </h1>
    <p class="muted small">Entre para ver as câmeras</p>

    <input
      bind:value={username}
      placeholder="usuário"
      autocomplete="username"
      autocapitalize="none"
      autocorrect="off"
      required
    />
    <input
      bind:value={password}
      type="password"
      placeholder="senha"
      autocomplete="current-password"
      required
    />

    <button class="primary" type="submit" disabled={busy}>
      {busy ? 'entrando…' : 'Entrar'}
    </button>

    <!-- Espaço reservado sempre: sem isto o formulário salta quando o erro
         aparece, e o botão foge do dedo no meio do toque. -->
    <p class="error small">{error}</p>
  </form>

  <!-- Antes de entrar já dá para saber qual dwnvr é este - útil quando há um
       de teste e um de verdade na mesma rede. -->
  {#if build.version}
    <p class="muted small ver">{build.version}</p>
  {/if}
</div>

<style>
  .screen {
    display: grid;
    place-items: center;
    /* align-content, e não só place-items: com duas linhas, place-items
       centraliza cada uma DENTRO da sua faixa, e as faixas esticam para
       preencher a tela - o que jogaria a versão para o meio do vazio. */
    align-content: center;
    gap: 16px;
    min-height: 100dvh;
    padding: 20px;
  }

  .ver {
    color: var(--dim);
  }

  form {
    width: 100%;
    max-width: 320px;
    display: grid;
    gap: 12px;
  }

  h1 {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: 0;
    font-size: 22px;
  }

  /* O SVG já traz o próprio arredondamento; nada de border-radius aqui. */
  .mark {
    display: block;
    flex: none;
  }

  p {
    margin: 0;
  }

  input {
    width: 100%;
  }

  .error {
    color: var(--bad);
    min-height: 1.2em;
  }
</style>
