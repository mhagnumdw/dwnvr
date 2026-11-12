<!--
  A tela do link de convite (`#convite?token=…`): quem recebeu o link define a
  própria senha e já entra. O token fica no fragmento da URL, que não sai do
  navegador, e vai ao servidor no corpo de um POST.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { api, ApiError, messageOf } from '../lib/api.ts';
  import type { InviteLink, Person } from '../lib/api.ts';
  import { currentParams } from '../lib/route.svelte.ts';
  import { hhmm } from '../lib/format.ts';
  import PasswordField from '../components/PasswordField.svelte';

  interface Props {
    onSuccess: (person: Person) => void;
  }

  let { onSuccess }: Props = $props();

  const token = currentParams().get('token') ?? '';

  // null enquanto confere; o link valendo; false com ele vencido ou já usado.
  let link = $state<InviteLink | false | null>(null);
  let checkError = $state('');
  let password = $state('');
  // A senha de novo, como na Minha conta: digitada no celular erra fácil, e o
  // erro só apareceria no primeiro login. A conferência é só daqui.
  let repeated = $state('');
  let error = $state('');
  let sending = $state(false);

  onMount(async () => {
    try {
      link = await api.checkInvite(token);
    } catch (e) {
      if (e instanceof ApiError && (e.status === 410 || e.status === 400)) link = false;
      else checkError = messageOf(e);
    }
  });

  async function define(e: SubmitEvent) {
    e.preventDefault();
    if (password !== repeated) {
      error = 'a senha e a repetida não são iguais';
      return;
    }
    sending = true;
    error = '';
    try {
      const r = await api.setPassword(token, password);
      onSuccess(r.user);
    } catch (err) {
      if (err instanceof ApiError && err.status === 410) link = false;
      else error = messageOf(err);
    } finally {
      sending = false;
    }
  }
</script>

<div class="screen">
  <form class="card" onsubmit={define}>
    <h1>
      <img class="mark" src="/favicon.svg" alt="" width="32" height="32" />
      dwnvr
    </h1>

    {#if link}
      <p>Olá, <strong>{link.name}</strong>. Defina a sua senha para entrar.</p>
      <!-- O usuário vai num campo, e não só no texto, para o gerenciador de
           senhas do navegador guardá-lo junto com a senha. -->
      <label>
        Seu usuário, para entrar das próximas vezes
        <input value={link.username} autocomplete="username" readonly />
      </label>
      <label>
        Senha nova, com {link.minPasswordLength} caracteres ou mais
        <PasswordField
          bind:value={password}
          placeholder="senha nova"
          autocomplete="new-password"
          minlength={link.minPasswordLength}
          maxlength={link.maxPasswordLength}
        />
      </label>
      <PasswordField
        bind:value={repeated}
        placeholder="repita a senha nova"
        autocomplete="new-password"
        maxlength={link.maxPasswordLength}
        aria-label="repita a senha nova"
      />
      <button class="primary" type="submit" disabled={sending}>
        {sending ? 'salvando…' : 'Definir a senha e entrar'}
      </button>
      <p class="muted small">Este link vale uma vez só, até as {hhmm(link.inviteExpiresAtMs)}.</p>
      <p class="error small">{error}</p>
    {:else if link === false}
      <p>Este link venceu ou já foi usado.</p>
      <p class="muted small">
        Peça outro link a quem administra este dwnvr. Se você já definiu a senha, é só entrar.
      </p>
      <a class="enter" href="#live">Ir para a tela de entrar</a>
    {:else if checkError}
      <p class="error small">Não deu para conferir o link: {checkError}</p>
    {:else}
      <p class="muted small">conferindo o link…</p>
    {/if}
  </form>
</div>

<style>
  /* A mesma moldura da tela de login. */
  .screen {
    display: grid;
    place-items: center;
    /* A coluna do #app inteira, que tem a altura da janela (ver app.css). */
    flex: 1;
    padding: 20px;
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

  .mark {
    display: block;
    flex: none;
  }

  p {
    margin: 0;
  }

  label {
    display: grid;
    gap: 5px;
    font-size: 13px;
    color: var(--dim);
  }

  label input {
    width: 100%;
    color: var(--fg);
  }

  input[readonly] {
    background: var(--bg);
  }

  .enter {
    justify-self: start;
  }

  .error {
    color: var(--bad);
    min-height: 1.2em;
  }
</style>
