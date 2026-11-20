<!--
  My account: what each person changes about themselves, of any role. The photo
  and the name apply to everyone; the password only to whoever is not the owner,
  whose password lives in dwnvr.yaml. You get here through the avatar in the
  header: there is no tab.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { api, messageOf } from '../lib/api.ts';
  import type { WithPerson, Account } from '../lib/api.ts';
  import { session } from '../lib/state.svelte.ts';
  import { shrinkPhoto } from '../lib/avatar.ts';
  import Avatar from '../components/Avatar.svelte';
  import PasswordField from '../components/PasswordField.svelte';

  // Null until the API answers.
  let data = $state<Account | null>(null);
  let error = $state('');

  const p = $derived(session.pessoa);

  onMount(async () => {
    try {
      data = await api.account();
      name = data.user.name ?? '';
    } catch (e) {
      error = messageOf(e);
    }
  });

  // Every account response carries the person as they ended up: the header
  // updates along with it, without asking again.
  function refresh(r: WithPerson) {
    session.pessoa = r.user;
  }

  // ---- foto ----------------------------------------------------------------

  let choose = $state<HTMLInputElement>(); // the <input type="file">, hidden behind the button
  let photo = $state({ busy: false, erro: '' });

  async function onChoose(e: Event & { currentTarget: HTMLInputElement }) {
    const file = e.currentTarget.files?.[0];
    // Cleared right away: choosing the same photo again must fire the change.
    e.currentTarget.value = '';
    if (!file || !data) return;
    photo = { busy: true, erro: '' };
    try {
      refresh(await api.uploadAvatar(await shrinkPhoto(file, data.avatar)));
      photo.busy = false;
    } catch (err) {
      photo = { busy: false, erro: messageOf(err) };
    }
  }

  async function takePhoto() {
    photo = { busy: true, erro: '' };
    try {
      refresh(await api.removeAvatar());
      photo.busy = false;
    } catch (err) {
      photo = { busy: false, erro: messageOf(err) };
    }
  }

  // ---- name ----------------------------------------------------------------

  let name = $state('');
  let nameState = $state({ busy: false, erro: '', ok: false });
  const nameChanged = $derived(name.trim() !== '' && name.trim() !== p?.name);

  async function saveName(e: SubmitEvent) {
    e.preventDefault();
    nameState = { busy: true, erro: '', ok: false };
    try {
      const r = await api.changeName(name);
      refresh(r);
      name = r.user.name ?? '';
      nameState = { busy: false, erro: '', ok: true };
    } catch (err) {
      nameState = { busy: false, erro: messageOf(err), ok: false };
    }
  }

  // ---- password ------------------------------------------------------------

  let current = $state('');
  let newOne = $state('');
  // The new one again: a password typed on the phone is easy to get wrong, and
  // the error would only show on the next login. The check is only done here;
  // the API receives one.
  let repeated = $state('');
  let passwordState = $state({ busy: false, erro: '', ok: false });

  async function changePassword(e: SubmitEvent) {
    e.preventDefault();
    if (newOne !== repeated) {
      passwordState = { busy: false, erro: 'a senha nova e a repetida não são iguais', ok: false };
      return;
    }
    passwordState = { busy: true, erro: '', ok: false };
    try {
      refresh(await api.changePassword(current, newOne));
      current = newOne = repeated = '';
      passwordState = { busy: false, erro: '', ok: true };
    } catch (err) {
      passwordState = { busy: false, erro: messageOf(err), ok: false };
    }
  }

  const role = $derived(p?.role === 'admin' ? 'administrador' : 'usuário');
</script>

<div class="page">
  {#if error}
    <p class="card small error">Não deu para ler a sua conta: {error}</p>
  {:else if data && p}
    <div class="card top">
      <Avatar person={p} size={88} />
      <div class="who">
        <strong>{p.name}</strong>
        <span class="muted small">@{p.username} · {role}</span>
        <div class="row wrap buttons">
          <button
            onclick={() => {
              choose?.click();
            }}
            disabled={photo.busy}
          >
            {photo.busy ? 'salvando…' : p.avatar ? 'Trocar a foto' : 'Pôr uma foto'}
          </button>
          {#if p.avatar}
            <button class="ghost" onclick={takePhoto} disabled={photo.busy}>Remover a foto</button>
          {/if}
        </div>
        {#if photo.erro}<span class="small error">{photo.erro}</span>{/if}
      </div>
      <!-- Any image the device can open. The crop and the downscale happen
           here, in the browser: the original photo never leaves the device. -->
      <input bind:this={choose} type="file" accept="image/*" hidden onchange={onChoose} />
    </div>

    <form class="card form" onsubmit={saveName}>
      <strong>Nome</strong>
      <span class="small muted">Como você aparece na tela, para você e para quem administra.</span>
      <div class="row">
        <input
          bind:value={name}
          required
          maxlength={data.maxNameLength}
          placeholder="João Silva"
          aria-label="nome"
        />
        <button type="submit" disabled={!nameChanged || nameState.busy}>
          {nameState.busy ? 'salvando…' : 'Salvar'}
        </button>
      </div>
      {#if nameState.erro}<span class="small error">{nameState.erro}</span>{/if}
      {#if nameState.ok && !nameChanged}<span class="small ok">nome salvo</span>{/if}
    </form>

    {#if p.owner}
      <div class="card form">
        <strong>Trocar senha</strong>
        <span class="small muted">
          A senha do administrador fica no <code>dwnvr.yaml</code> (<code>server.password</code>), e
          se troca lá.
        </span>
      </div>
    {:else}
      <form class="card form" onsubmit={changePassword}>
        <strong>Trocar senha</strong>
        <!-- The user, hidden, so the browser's password manager knows whose the
             new password is. -->
        <input
          class="hidden"
          value={p.username}
          autocomplete="username"
          readonly
          tabindex="-1"
          aria-hidden="true"
        />
        <PasswordField
          bind:value={current}
          placeholder="senha atual"
          autocomplete="current-password"
        />
        <PasswordField
          bind:value={newOne}
          placeholder="senha nova, com {data.minPasswordLength} caracteres ou mais"
          autocomplete="new-password"
          minlength={data.minPasswordLength}
          maxlength={data.maxPasswordLength}
        />
        <PasswordField
          bind:value={repeated}
          placeholder="repita a senha nova"
          autocomplete="new-password"
          maxlength={data.maxPasswordLength}
        />
        <button class="primary" type="submit" disabled={passwordState.busy}>
          {passwordState.busy ? 'trocando…' : 'Trocar senha'}
        </button>
        {#if passwordState.erro}<span class="small error">{passwordState.erro}</span>{/if}
        {#if passwordState.ok}
          <span class="small ok"
            >Senha trocada. Os outros aparelhos em que você entrou vão pedir a nova.</span
          >
        {:else}
          <span class="small muted"
            >Os outros aparelhos em que você entrou vão pedir a senha nova.</span
          >
        {/if}
      </form>
    {/if}
  {/if}
</div>

<style>
  .page {
    display: grid;
    gap: 10px;
    padding: 10px;
    max-width: 560px;
    margin: 0 auto;
  }

  p {
    margin: 0;
  }

  .error {
    color: var(--bad);
  }

  .ok {
    color: var(--ok);
  }

  .top {
    display: flex;
    align-items: center;
    gap: 16px;
  }

  .who {
    display: grid;
    gap: 2px;
    min-width: 0;
  }

  .who strong {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .buttons {
    margin-top: 8px;
    gap: 8px;
  }

  .buttons button {
    min-height: 36px;
    padding: 5px 12px;
    font-size: 13px;
  }

  .form {
    position: relative;
    display: grid;
    gap: 10px;
  }

  .form input {
    width: 100%;
  }

  .form .row input {
    flex: 1;
    min-width: 0;
  }

  /* Out of view, but in the form: with display: none the password manager does
     not read it. Attached to the card, so it does not widen the page on the
     phone. */
  .form input.hidden {
    position: absolute;
    top: 0;
    left: 0;
    width: 1px;
    height: 1px;
    padding: 0;
    border: 0;
    opacity: 0;
    pointer-events: none;
  }
</style>
