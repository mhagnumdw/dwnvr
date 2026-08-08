<script>
  import { api } from '../lib/api.js';

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
    <h1>dwnvr</h1>
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
</div>

<style>
  .screen {
    display: grid;
    place-items: center;
    min-height: 100dvh;
    padding: 20px;
  }

  form {
    width: 100%;
    max-width: 320px;
    display: grid;
    gap: 12px;
  }

  h1 {
    margin: 0;
    font-size: 22px;
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
