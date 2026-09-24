<script>
  import { api } from '../lib/api.js';
  import { build } from '../lib/state.svelte.js';

  let { onSuccess } = $props();

  let username = $state('');
  let password = $state('');
  let error = $state('');
  let busy = $state(false);
  let verSenha = $state(false);

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
    <!-- Senha digitada no celular erra fácil; o olho deixa conferir antes de
         mandar. É type="button" para não submeter o formulário. -->
    <div class="senha">
      <input
        bind:value={password}
        type={verSenha ? 'text' : 'password'}
        placeholder="senha"
        autocomplete="current-password"
        autocapitalize="none"
        autocorrect="off"
        required
      />
      <button
        type="button"
        class="olho"
        onclick={() => (verSenha = !verSenha)}
        aria-pressed={verSenha}
        aria-label={verSenha ? 'esconder senha' : 'mostrar senha'}
        title={verSenha ? 'esconder senha' : 'mostrar senha'}
      >
        <svg viewBox="0 0 16 16" aria-hidden="true">
          <path d="M1.5 8S4 3.5 8 3.5 14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8z" stroke-linejoin="round" />
          <circle cx="8" cy="8" r="2" />
          {#if verSenha}
            <path d="M2.5 13.5l11-11" stroke-linecap="round" />
          {/if}
        </svg>
      </button>
    </div>

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

  .senha {
    position: relative;
  }

  /* Espaço para o olho, que fica por cima do fim do campo. */
  .senha input {
    padding-right: 44px;
  }

  /* O Edge põe um olho próprio no campo de senha; com o nosso, seriam dois. */
  .senha input::-ms-reveal {
    display: none;
  }

  .olho {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    width: 44px;
    min-height: 0;
    display: grid;
    place-items: center;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--dim);
  }

  .olho:hover,
  .olho[aria-pressed='true'] {
    color: var(--fg);
  }

  .olho svg {
    width: 18px;
    height: 18px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.4;
  }

  .error {
    color: var(--bad);
    min-height: 1.2em;
  }
</style>
