<!--
  A tela do link de convite (`#convite?token=…`): quem recebeu o link define a
  própria senha e já entra. O token fica no fragmento da URL, que não sai do
  navegador, e vai ao servidor no corpo de um POST.
-->
<script>
  import { onMount } from 'svelte';
  import { api } from '../lib/api.js';
  import { paramsAtuais } from '../lib/rota.svelte.js';
  import { hhmm } from '../lib/format.js';
  import CampoSenha from '../components/CampoSenha.svelte';

  let { onSuccess } = $props();

  const token = paramsAtuais().get('token') ?? '';

  // null enquanto confere; { usuario, nome, linkVenceEmMs, senhaMinima,
  // senhaMaxima } com o link valendo; false com ele vencido ou já usado.
  let link = $state(null);
  let erroAoConferir = $state('');
  let senha = $state('');
  let erro = $state('');
  let enviando = $state(false);

  onMount(async () => {
    try {
      link = await api.conferirConvite(token);
    } catch (e) {
      if (e.status === 410 || e.status === 400) link = false;
      else erroAoConferir = e.message;
    }
  });

  async function definir(e) {
    e.preventDefault();
    enviando = true;
    erro = '';
    try {
      const r = await api.definirSenha(token, senha);
      onSuccess(r.pessoa);
    } catch (err) {
      if (err.status === 410) link = false;
      else erro = err.message;
    } finally {
      enviando = false;
    }
  }
</script>

<div class="screen">
  <form class="card" onsubmit={definir}>
    <h1>
      <img class="mark" src="/favicon.svg" alt="" width="32" height="32" />
      dwnvr
    </h1>

    {#if link}
      <p>Olá, <strong>{link.nome}</strong>. Defina a sua senha para entrar.</p>
      <!-- O usuário vai num campo, e não só no texto, para o gerenciador de
           senhas do navegador guardá-lo junto com a senha. -->
      <label>
        Seu usuário, para entrar das próximas vezes
        <input value={link.usuario} autocomplete="username" readonly />
      </label>
      <label>
        Senha nova, com {link.senhaMinima} caracteres ou mais
        <CampoSenha
          bind:value={senha}
          placeholder="senha nova"
          autocomplete="new-password"
          minlength={link.senhaMinima}
          maxlength={link.senhaMaxima}
        />
      </label>
      <button class="primary" type="submit" disabled={enviando}>
        {enviando ? 'salvando…' : 'Definir a senha e entrar'}
      </button>
      <p class="muted small">Este link vale uma vez só, até as {hhmm(link.linkVenceEmMs)}.</p>
      <p class="error small">{erro}</p>
    {:else if link === false}
      <p>Este link venceu ou já foi usado.</p>
      <p class="muted small">Peça outro link a quem administra este dwnvr. Se você já definiu a senha, é só entrar.</p>
      <a class="entrar" href="#live">Ir para a tela de entrar</a>
    {:else if erroAoConferir}
      <p class="error small">Não deu para conferir o link: {erroAoConferir}</p>
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

  .entrar {
    justify-self: start;
  }

  .error {
    color: var(--bad);
    min-height: 1.2em;
  }
</style>
