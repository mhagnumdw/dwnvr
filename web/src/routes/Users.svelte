<!--
  Users: who signs in to dwnvr besides the owner. Without SMTP, the admin
  creates the person, dwnvr generates a link, and they set the password
  themselves through it. The same link serves "forgot my password" and "lost my
  phone": generating another one erases the current password. Only the admin
  gets here; the API rejects the viewer with 403.
-->
<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api, ApiError, messageOf } from '../lib/api.ts';
  import type { PersonField, Invite, FieldErrors, User, Users } from '../lib/api.ts';
  import { Validation, type FieldState } from '../lib/validation.svelte.ts';
  import { session } from '../lib/state.svelte.ts';
  import { copy } from '../lib/browser.ts';
  import { dateTime, todayYesterday } from '../lib/format.ts';
  import Modal from '../components/Modal.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import Avatar from '../components/Avatar.svelte';
  import Icon from '../components/Icon.svelte';

  type Action = 'link' | 'foto' | 'remover' | 'email';

  // Null until the API answers.
  let data = $state<Users | null>(null);
  let error = $state('');

  // The newly generated link. See showLink.
  let generated = $state<{
    username: string;
    name: string;
    email: string; // empty without email
    url: string;
    expiresInMs: number;
  } | null>(null);

  // The clock for the link countdown, ticking once a second. It only runs with
  // a link open on screen.
  let now = $state(Date.now());
  const hasOpenLink = $derived(
    generated !== null || (data?.users.some((u) => u.status === 'invitePending') ?? false),
  );
  $effect(() => {
    if (!hasOpenLink) return;
    const id = setInterval(() => (now = Date.now()), 1000);
    return () => {
      clearInterval(id);
    };
  });

  async function load() {
    try {
      data = await api.users();
      // The "hoje" and "ontem" of the last access count from here: with no link
      // open, the tick does not run.
      now = Date.now();
      error = '';
    } catch (e) {
      error = messageOf(e);
    }
  }

  onMount(load);

  // Leaving the screen with a check pending: nothing else goes to the server.
  onDestroy(() => {
    newValidation.stop();
    emailValidation.stop();
  });

  // ---- the newly generated link --------------------------------------------
  //
  // It is the only place where the link exists: the server keeps only the token
  // hash. It comes from the address the admin is using, which is the one the
  // person can also reach in most cases (the same name on Tailscale, the same
  // IP on the home network).

  let copied = $state('');

  function showLink(r: Invite) {
    generated = {
      username: r.user.username,
      name: r.user.name,
      email: r.user.email ?? '',
      url: `${location.origin}${location.pathname}#invite?token=${r.token}`,
      // The new link always comes with the deadline; without it, it counts as
      // expired.
      expiresInMs: r.user.inviteExpiresAtMs ?? 0,
    };
    copied = '';
    delivery = null;
  }

  // The link by email: the same link as on screen goes to the person's email.
  // The result stays on the card, and sending again means tapping again.
  let delivery = $state<{ ok: boolean; texto: string } | null>(null);
  let sending = $state(false);

  async function sendByEmail() {
    if (!generated) return;
    sending = true;
    delivery = null;
    try {
      const r = await api.sendLink(generated.username, generated.url);
      delivery = { ok: true, texto: `enviado para ${r.sentTo}` };
    } catch (e) {
      delivery = { ok: false, texto: messageOf(e) };
    } finally {
      sending = false;
    }
  }

  // "Compartilhar" opens the system menu (email, WhatsApp). It only exists
  // where the browser offers it, and only over HTTPS; elsewhere "Copiar"
  // remains, which also works over HTTP.
  const canShare = typeof navigator.share === 'function';

  async function onCopy() {
    if (!generated) return;
    copied = (await copy(generated.url)) ? 'copiado' : 'não deu para copiar';
  }

  async function share() {
    if (!generated) return;
    try {
      await navigator.share({
        title: 'dwnvr',
        text: `Link para ${generated.name} definir a senha no dwnvr`,
        url: generated.url,
      });
    } catch {
      // Dismissing the system menu also lands here. Nothing to do.
    }
  }

  // "9:41", until the link expires.
  function missing(ms: number) {
    const s = Math.max(0, Math.ceil((ms - now) / 1000));
    return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
  }

  // ---- criar ----------------------------------------------------------------

  let newItem = $state<{ username: string; name: string; email: string } | null>(null); // with the form open
  let saving = $state(false);
  let newError = $state('');
  // Whether the admin has already typed in the user field. Until then, the user
  // follows the name (see suggestUser); clearing everything brings the
  // suggestion back.
  let typedUser = false;
  let newValidation = $state.raw(new Validation(['username', 'name', 'email'], checkNew));
  const newFields: Partial<Record<PersonField, HTMLInputElement>> = {};

  async function checkNew(): Promise<FieldErrors> {
    if (!newItem) return {};
    return (await api.checkNew(newItem.username, newItem.name, newItem.email)).fields;
  }

  function openNew() {
    newItem = { username: '', name: '', email: '' };
    newError = '';
    typedUser = false;
    newValidation = new Validation(['username', 'name', 'email'], checkNew);
  }

  function closeNew() {
    newValidation.stop();
    newItem = null;
  }

  // "João da Silva Santos" becomes "joao.santos": the first and last names,
  // without accents, in lowercase, with only what the user field accepts, cut
  // to the maximum length.
  function suggestUser(name: string, max: number) {
    const parts = name
      .normalize('NFD')
      .replace(/\p{M}/gu, '')
      .toLowerCase()
      .split(/[^a-z0-9]+/)
      .filter(Boolean);
    const chosen = parts.length > 1 ? [parts[0], parts[parts.length - 1]] : parts;
    return chosen
      .join('.')
      .slice(0, max)
      .replace(/[._-]+$/, '');
  }

  function onNameInput() {
    if (newItem && data && !typedUser) {
      newItem.username = suggestUser(newItem.name, data.maxUsernameLength);
    }
    newValidation.typeIn();
  }

  // What filled in the user was the name, and the admin may not even go through
  // it: the suggested user is checked along with it, so the "já existe" shows
  // up before the save.
  function onNameLeave() {
    if (!newItem) return;
    newValidation.leave('name', newItem.name);
    if (!typedUser) newValidation.leave('username', newItem.username);
  }

  function onUserInput() {
    typedUser = newItem?.username !== '';
    newValidation.typeIn();
  }

  // The first rejected field, in screen order, gets focus.
  function focusFieldError(
    validation: Validation,
    campos: Partial<Record<PersonField, HTMLInputElement>>,
    order: PersonField[],
  ) {
    const field = order.find((c) => validation.condition(c) === 'erro');
    if (field) campos[field]?.focus();
  }

  // The save's rejection is about the values that were sent. It only applies to
  // the form that requested it (`validation`), if it is still open; and, if the
  // fields changed while the response was on its way, they are checked again,
  // instead of carrying the red of a value that is no longer there.
  function applyFieldError(
    validation: Validation,
    opened: Validation,
    fieldErrors: FieldErrors,
    changed: boolean,
    campos: Partial<Record<PersonField, HTMLInputElement>>,
    order: PersonField[],
  ) {
    if (validation !== opened) return;
    validation.invalid(fieldErrors);
    if (changed) validation.typeIn();
    else focusFieldError(validation, campos, order);
  }

  async function create(e: SubmitEvent) {
    e.preventDefault();
    if (!newItem) return;
    const validation = newValidation;
    const sent = { ...newItem };
    // The fields changed (or the form closed) while the response was on its
    // way.
    const changed = () => JSON.stringify(newItem) !== JSON.stringify(sent);
    saving = true;
    newError = '';
    try {
      showLink(await api.createUser(sent.username, sent.name, sent.email));
      if (validation === newValidation) closeNew();
      await load();
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.campos).length > 0) {
        applyFieldError(validation, newValidation, err.campos, changed(), newFields, [
          'name',
          'username',
          'email',
        ]);
      } else if (validation === newValidation) {
        newError = messageOf(err);
      }
    } finally {
      saving = false;
    }
  }

  // ---- each person's ⋮ ------------------------------------------------------

  let menu = $state<string | null>(null); // the user with the menu open
  let confirming = $state<{ action: Action; u: User } | null>(null);
  let actionError = $state('');

  function outsideMenu(e: MouseEvent) {
    if (menu && !(e.target instanceof Element && e.target.closest('.menu, .mais'))) menu = null;
  }

  function request(action: Action, u: User) {
    menu = null;
    actionError = '';
    if (action === 'email') {
      editingEmail = { u, email: u.email ?? '' };
      emailError = '';
      emailValidation = new Validation(['email'], checkEmail);
      return;
    }
    // A new link for someone who does not have a password yet signs no one out:
    // it only swaps one link for another, and skips the confirmation.
    if (action === 'link' && u.status !== 'active') void run(action, u);
    else confirming = { action, u };
  }

  async function run(action: Action, u: User) {
    confirming = null;
    try {
      if (action === 'link') showLink(await api.newLink(u.username));
      else if (action === 'foto') await api.removeAvatarOf(u.username);
      else if (action === 'remover') {
        await api.removeUser(u.username);
        if (generated?.username === u.username) generated = null;
      }
    } catch (e) {
      actionError = messageOf(e);
    }
    await load();
  }

  // ---- the email of an existing user ---------------------------------------

  let editingEmail = $state<{ u: User; email: string } | null>(null);
  let savingEmail = $state(false);
  let emailError = $state('');
  let emailValidation = $state.raw(new Validation(['email'], checkEmail));
  const camposEmail: Partial<Record<PersonField, HTMLInputElement>> = {};

  async function checkEmail(): Promise<FieldErrors> {
    if (!editingEmail) return {};
    return (await api.checkEmail(editingEmail.u.username, editingEmail.email)).fields;
  }

  function closeEmail() {
    emailValidation.stop();
    editingEmail = null;
  }

  async function saveEmail(e: SubmitEvent) {
    e.preventDefault();
    if (!editingEmail) return;
    const validation = emailValidation;
    const sent = editingEmail.email;
    // In a closure, TypeScript does not assume the form is still open.
    const changed = () => editingEmail?.email !== sent;
    savingEmail = true;
    emailError = '';
    try {
      const { user } = await api.changeEmail(editingEmail.u.username, sent);
      // The link open on the card now goes to the new email.
      if (generated?.username === user.username) generated.email = user.email ?? '';
      if (validation === emailValidation) closeEmail();
      await load();
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.campos).length > 0) {
        applyFieldError(validation, emailValidation, err.campos, changed(), camposEmail, ['email']);
      } else if (validation === emailValidation) {
        emailError = messageOf(err);
      }
    } finally {
      savingEmail = false;
    }
  }

  // The signal to the right of the field. Green only shows with something
  // written: in the email, which is optional, a "right" on the empty field
  // would say nothing. `onlyError` is the field that only warns about what is
  // wrong, like the name.
  function signal(
    validation: Validation,
    field: PersonField,
    value: string,
    onlyError = false,
  ): FieldState {
    const condition = validation.condition(field);
    if (condition === 'ok' && (onlyError || value.trim() === '')) return null;
    return condition;
  }

  const you = $derived(session.pessoa?.username);
  const minutes = $derived(data ? Math.round(data.inviteValidityMs / 60000) : 0);
</script>

<svelte:window
  onclick={outsideMenu}
  onkeydown={(e: KeyboardEvent) => {
    if (e.key === 'Escape') menu = null;
  }}
/>

<div class="page">
  {#if error}
    <p class="card small error">Não deu para ler os usuários: {error}</p>
  {:else if data && !data.authRequired}
    <p class="card small">
      A autenticação está desligada, e sem ela não há usuários: quem alcança o dwnvr entra direto.
      Para ligar, defina <code>server.username</code> e <code>server.password</code> no
      <code>dwnvr.yaml</code> e reinicie o dwnvr. Essa é a sua conta, a de administrador; as outras pessoas
      você cria aqui.
    </p>
  {:else if data}
    <div class="row">
      <strong>Usuários</strong>
      <span class="spacer"></span>
      <button class="primary" onclick={openNew}>+ Novo usuário</button>
    </div>

    {#if generated}
      <div class="card new-link">
        <div class="row wrap">
          <strong>Link de {generated.name}</strong>
          <span class="spacer"></span>
          {#if generated.expiresInMs > now}
            <span class="chip mono"
              ><span class="dot warn"></span>vence em {missing(generated.expiresInMs)}</span
            >
          {:else}
            <span class="chip"><span class="dot bad"></span>vencido</span>
          {/if}
        </div>
        <p class="small muted">
          Mande para {generated.name} abrir agora: vale uma vez só, e não aparece de novo depois que você
          sair desta tela.
        </p>
        <div class="url small">{generated.url}</div>
        <div class="row wrap">
          <button onclick={onCopy}>Copiar</button>
          {#if canShare}<button onclick={share}>Compartilhar</button>{/if}
          {#if data.emailEnabled && generated.email && generated.expiresInMs > now}
            <button onclick={sendByEmail} disabled={sending}
              >{sending ? 'enviando…' : 'Enviar por e-mail'}</button
            >
          {/if}
          <span class="small muted">{copied}</span>
          {#if delivery?.ok}<span class="small muted">{delivery.texto}</span>{/if}
        </div>
        {#if delivery && !delivery.ok}<p class="small error">{delivery.texto}</p>{/if}
      </div>
    {/if}

    {#if actionError}<p class="card small error">{actionError}</p>{/if}

    <div class="card list">
      {#if data.owner}
        <div class="row">
          <Avatar person={data.owner} size={36} />
          <div class="who">
            <div class="name">
              <strong>{data.owner.name}</strong>
              {#if you === data.owner.username}<span class="muted small">você</span>{/if}
            </div>
            <div class="muted small">@{data.owner.username} · administrador</div>
            {#if you !== data.owner.username && data.owner.lastAccessMs}
              <div class="muted small">
                último acesso <span class="when"
                  >{todayYesterday(data.owner.lastAccessMs, now)}</span
                >
              </div>
            {/if}
          </div>
          {#if you === data.owner.username}
            <a class="small" href="#account">Minha conta</a>
          {:else}
            <span class="muted small">do dwnvr.yaml</span>
          {/if}
        </div>
      {/if}
      {#each data.users as u (u.username)}
        <div class="row">
          <Avatar person={u} size={36} />
          <!-- The name and the chip: on the phone, one under the other; from
               720 px up, side by side (see .middle). -->
          <div class="middle">
            <div class="who">
              <div class="name"><strong>{u.name}</strong></div>
              <div class="muted small login">
                @{u.username}{u.role === 'admin' ? ' · administrador' : ''}
              </div>
              {#if u.email}<div class="muted small login">{u.email}</div>{/if}
              <!-- Whoever is active has the last access on the chip, which is
                   what you want to know day to day, and here the date they set
                   the password. Whoever has an open or expired link has the
                   link on the chip, and here the earlier access, if any. -->
              {#if u.status === 'active' && u.activeSinceMs}
                <div class="muted small">
                  ativo desde <span class="when">{dateTime(u.activeSinceMs)}</span>
                </div>
              {:else if u.status !== 'active' && u.lastAccessMs}
                <div class="muted small">
                  último acesso <span class="when">{todayYesterday(u.lastAccessMs, now)}</span>
                </div>
              {/if}
            </div>
            {#if u.status === 'active'}
              <!-- The green dot says they are active. No recorded access means
                   someone who has not used dwnvr since the last access started
                   being recorded. -->
              <span class="chip wrap">
                <span class="dot ok"></span>
                {#if u.lastAccessMs}
                  <span
                    >último acesso <span class="when">{todayYesterday(u.lastAccessMs, now)}</span
                    ></span
                  >
                {:else}
                  <span>nenhum acesso registrado</span>
                {/if}
              </span>
            {:else if u.status === 'invitePending' && u.inviteExpiresAtMs !== undefined && u.inviteExpiresAtMs > now}
              <span class="chip mono"
                ><span class="dot warn"></span>link vence em {missing(u.inviteExpiresAtMs)}</span
              >
            {:else}
              <span class="chip"><span class="dot bad"></span>link vencido</span>
            {/if}
          </div>
          <button
            class="ghost mais"
            onclick={() => (menu = menu === u.username ? null : u.username)}
            aria-label="opções de {u.name}"
            aria-haspopup="menu"
            aria-expanded={menu === u.username}
          >
            <Icon name="menu" size={18} />
          </button>
          {#if menu === u.username}
            <div class="menu" role="menu">
              <!-- For whoever is active, the new link erases the password: the
                   label says so. For whoever has not set one, there is no
                   password to reset. -->
              <button
                class="ghost"
                role="menuitem"
                onclick={() => {
                  request('link', u);
                }}
              >
                {u.status === 'active' ? 'Redefinir senha' : 'Gerar link novo'}
              </button>
              <button
                class="ghost"
                role="menuitem"
                onclick={() => {
                  request('email', u);
                }}>{u.email ? 'Alterar o e-mail' : 'Cadastrar o e-mail'}</button
              >
              <!-- The admin does not change anyone's photo; they only remove
                   one that does not fit. -->
              {#if u.avatar}
                <button
                  class="ghost"
                  role="menuitem"
                  onclick={() => {
                    request('foto', u);
                  }}>Remover a foto</button
                >
              {/if}
              <button
                class="ghost danger"
                role="menuitem"
                onclick={() => {
                  request('remover', u);
                }}>Remover</button
              >
            </div>
          {/if}
        </div>
      {:else}
        <p class="muted small empty">
          Ninguém além de você ainda. Crie uma pessoa para mandar o link.
        </p>
      {/each}
    </div>
  {/if}
</div>

<!-- The email serves for signing in even without the smtp section; only sending
     the link depends on it, and the screen says so where the email is written. -->
{#snippet noDelivery()}
  <p class="small muted no-delivery">
    <Icon name="info" size={14} />
    O envio de e-mail não está configurado neste dwnvr: o link não vai por e-mail, só copiado.
  </p>
{/snippet}

<!-- The signal to the right of a field checked by the server (see
     lib/validation.svelte.ts). The rejection text goes below, in `fieldError`. -->
{#snippet fieldSignal(condition: FieldState)}
  {#if condition}
    <span class="signal {condition}"
      ><Icon name={condition === 'ok' ? 'checkmark' : 'close'} size={18} /></span
    >
  {/if}
{/snippet}

<!-- The rejection line always exists, empty or not: the space is already
     reserved, and the message appears and disappears without pushing the rest
     of the form. -->
{#snippet fieldError(id: string, text: string)}
  <p class="small error field-error" {id}>{text}</p>
{/snippet}

<!-- novalidate: the browser's rejection is a bubble with another look and
     another text; here they all come from the server and sit under the field. -->
{#if newItem && data}
  {@const sName = signal(newValidation, 'name', newItem.name, true)}
  {@const sUser = signal(newValidation, 'username', newItem.username)}
  {@const sEmail = signal(newValidation, 'email', newItem.email)}
  <Modal onclose={closeNew}>
    <form class="fields" onsubmit={create} novalidate>
      <h3>Novo usuário</h3>
      <div class="groups">
        <div class="group">
          <label>
            Nome, como aparece na tela
            <span class="field">
              <input
                bind:this={newFields.name}
                bind:value={newItem.name}
                oninput={onNameInput}
                onblur={onNameLeave}
                required
                maxlength={data.maxNameLength}
                placeholder="João Silva"
                aria-invalid={sName === 'erro'}
                aria-describedby={sName === 'erro' ? 'recusa-nome' : undefined}
              />
              {@render fieldSignal(sName)}
            </span>
          </label>
          {@render fieldError('recusa-nome', sName === 'erro' ? newValidation.message('name') : '')}
        </div>
        <div class="group">
          <label>
            Usuário, para entrar: letras minúsculas, números, ponto, _ e -
            <span class="field">
              <input
                bind:this={newFields.username}
                bind:value={newItem.username}
                oninput={onUserInput}
                onblur={() => {
                  if (newItem) newValidation.leave('username', newItem.username);
                }}
                required
                maxlength={data.maxUsernameLength}
                placeholder="joao.silva"
                autocapitalize="none"
                autocorrect="off"
                spellcheck="false"
                aria-invalid={sUser === 'erro'}
                aria-describedby={sUser === 'erro' ? 'recusa-usuario' : undefined}
              />
              {@render fieldSignal(sUser)}
            </span>
          </label>
          {@render fieldError(
            'recusa-usuario',
            sUser === 'erro' ? newValidation.message('username') : '',
          )}
        </div>
        <div class="group">
          <label>
            {data.emailEnabled
              ? 'E-mail, para entrar e receber o link (opcional)'
              : 'E-mail, para entrar (opcional)'}
            <span class="field">
              <input
                bind:this={newFields.email}
                inputmode="email"
                bind:value={newItem.email}
                oninput={() => {
                  newValidation.typeIn();
                }}
                onblur={() => {
                  if (newItem) newValidation.leave('email', newItem.email);
                }}
                maxlength={data.maxEmailLength}
                placeholder="joao@exemplo.com"
                autocapitalize="none"
                autocorrect="off"
                spellcheck="false"
                aria-invalid={sEmail === 'erro'}
                aria-describedby={sEmail === 'erro' ? 'recusa-email' : undefined}
              />
              {@render fieldSignal(sEmail)}
            </span>
          </label>
          {@render fieldError(
            'recusa-email',
            sEmail === 'erro' ? newValidation.message('email') : '',
          )}
        </div>
      </div>
      {#if !data.emailEnabled}{@render noDelivery()}{/if}
      <p class="small muted">
        O dwnvr gera um link que vale {minutes} minutos, para a pessoa definir a própria senha. Ela vê
        o ao vivo, as gravações e as detecções; o cadastro de câmeras, os usuários e o diagnóstico do
        servidor ficam só com você.
      </p>
      {#if newError}<p class="small error">{newError}</p>{/if}
      <div class="row">
        <span class="spacer"></span>
        <button type="button" class="ghost" onclick={closeNew}>cancelar</button>
        <button class="primary" type="submit" disabled={saving}>
          {saving ? 'criando…' : 'criar e gerar o link'}
        </button>
      </div>
    </form>
  </Modal>
{/if}

{#if editingEmail && data}
  {@const target = editingEmail.u}
  {@const sEmail = signal(emailValidation, 'email', editingEmail.email)}
  <Modal onclose={closeEmail}>
    <form class="fields" onsubmit={saveEmail} novalidate>
      <h3>E-mail de {target.name}</h3>
      <div class="group">
        <label>
          {data.emailEnabled
            ? 'Para entrar e receber o link de definir a senha'
            : 'Para entrar, no lugar do usuário'}
          <span class="field">
            <input
              bind:this={camposEmail.email}
              inputmode="email"
              bind:value={editingEmail.email}
              oninput={() => {
                emailValidation.typeIn();
              }}
              onblur={() => {
                if (editingEmail) emailValidation.leave('email', editingEmail.email);
              }}
              maxlength={data.maxEmailLength}
              placeholder="joao@exemplo.com"
              autocapitalize="none"
              autocorrect="off"
              spellcheck="false"
              aria-invalid={sEmail === 'erro'}
              aria-describedby={sEmail === 'erro' ? 'recusa-email-de' : undefined}
            />
            {@render fieldSignal(sEmail)}
          </span>
        </label>
        {@render fieldError(
          'recusa-email-de',
          sEmail === 'erro' ? emailValidation.message('email') : '',
        )}
      </div>
      <p class="small muted">Vazio apaga o e-mail. A senha e o link de {target.name} não mudam.</p>
      {#if !data.emailEnabled}{@render noDelivery()}{/if}
      {#if emailError}<p class="small error">{emailError}</p>{/if}
      <div class="row">
        <span class="spacer"></span>
        <button type="button" class="ghost" onclick={closeEmail}>cancelar</button>
        <button class="primary" type="submit" disabled={savingEmail}>
          {savingEmail ? 'salvando…' : 'salvar'}
        </button>
      </div>
    </form>
  </Modal>
{/if}

{#if confirming?.action === 'link'}
  {@const target = confirming.u}
  <ConfirmDialog
    title="Redefinir a senha de {target.name}?"
    confirmLabel="redefinir senha"
    danger
    onconfirm={() => {
      void run('link', target);
    }}
    oncancel={() => (confirming = null)}
  >
    A senha atual de {target.name} deixa de valer agora, e os aparelhos em que ela entrou saem na hora.
    O dwnvr gera um link novo, e ela volta a entrar definindo uma senha nova por ele.
  </ConfirmDialog>
{:else if confirming?.action === 'foto'}
  {@const target = confirming.u}
  <ConfirmDialog
    title="Remover a foto de {target.name}?"
    confirmLabel="remover a foto"
    danger
    onconfirm={() => {
      void run('foto', target);
    }}
    oncancel={() => (confirming = null)}
  >
    A foto é apagada, e no lugar dela ficam as iniciais. {target.name} pode pôr outra pela Minha conta.
  </ConfirmDialog>
{:else if confirming?.action === 'remover'}
  {@const target = confirming.u}
  <ConfirmDialog
    title="Remover {target.name}?"
    confirmLabel="remover"
    danger
    onconfirm={() => {
      void run('remover', target);
    }}
    oncancel={() => (confirming = null)}
  >
    {target.name} sai na hora de todos os aparelhos, e o usuário <strong>@{target.username}</strong> fica
    livre.
  </ConfirmDialog>
{/if}

<style>
  /* The column is minmax(0, 1fr), not the implicit one: that one grows to the
     content's minimum width, and a long name, which does not wrap, pushed the
     screen past the phone width. This way the name is cut with an ellipsis. */
  .page {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 10px;
    padding: 10px;
    max-width: 900px;
    margin: 0 auto;
  }

  p {
    margin: 0;
  }

  .error {
    color: var(--bad);
  }

  /* The icon stays centered on the first line when the text wraps: 13px with
     the body's line-height 1.5 gives a 19.5px line, and the icon is 14. */
  .no-delivery {
    display: flex;
    gap: 6px;
    align-items: flex-start;
  }
  .no-delivery :global(svg) {
    flex: none;
    margin-top: 3px;
  }

  /* The highlight border is the translucent --accent, the same as the version pill. */
  .new-link {
    display: grid;
    gap: 8px;
    border-color: rgba(47, 129, 247, 0.55);
  }

  .new-link button {
    min-height: 36px;
    padding: 5px 14px;
    font-size: 14px;
  }

  /* The whole link, wrapping where it must: cut with an ellipsis, there would
     be no way to check what is about to be sent. */
  .url {
    padding: 8px 10px;
    border-radius: 8px;
    background: var(--bg);
    border: 1px solid var(--line);
    overflow-wrap: anywhere;
  }

  .list {
    padding: 2px 14px;
  }

  .row {
    position: relative;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 0;
    border-bottom: 1px solid var(--line);
  }

  .row:last-child {
    border-bottom: 0;
  }

  /* On the phone, the chip is the fourth line, under the name, the @user and
     the date: beside them, it fought the name for width and wrapped onto two
     lines. From 720 px up there is width, and it goes back to the right. The
     avatar and the ⋮ stay in the middle of the row in both cases. */
  .middle {
    flex: 1;
    min-width: 0;
  }

  .middle > .chip {
    display: flex;
    width: fit-content;
    margin-top: 4px;
  }

  @media (min-width: 720px) {
    .middle {
      display: flex;
      align-items: center;
      gap: 12px;
    }

    .middle > .chip {
      flex: none;
      margin-top: 0;
    }
  }

  /* At 320 px, not even on its own line does "último acesso ontem 22:47" fit:
     the chip wraps before the date, and the radius stops being a pill's. */
  .chip.wrap {
    white-space: normal;
    border-radius: 12px;
  }

  .when {
    white-space: nowrap;
  }

  .who {
    flex: 1;
    min-width: 0;
    line-height: 1.3;
  }

  .name {
    display: flex;
    align-items: baseline;
    gap: 6px;
    min-width: 0;
  }

  .name strong,
  .login {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .empty {
    padding: 12px 0;
  }

  /* The icon is a block svg, which the button's text-align does not center:
     without the flex, it stuck to the left, 8 px off center. */
  .mais {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    min-height: 36px;
    padding: 0;
  }

  /* The same drawing as the ⋮ menu of the Detections screen, attached to the row. */
  .menu {
    position: absolute;
    z-index: 30;
    right: 0;
    top: calc(100% - 4px);
    min-width: 190px;
    padding: 4px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  }

  .menu button {
    display: block;
    width: 100%;
    border: 0;
    text-align: left;
    white-space: nowrap;
  }

  .menu .danger {
    color: var(--bad);
  }

  @media (hover: hover) {
    .menu button:hover {
      background: var(--panel-2);
    }
  }

  .fields {
    display: grid;
    gap: 14px;
  }

  h3 {
    margin: 0 0 4px;
    font-size: 15px;
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
    font-size: 15px;
  }

  /* The field and the rejection line below it, closer to each other than to the
     other fields. The reserved line (see the fieldError snippet) fills the gap
     between one field and the next: the groups sit 10 px apart, not the form's
     14, so the empty gap does not grow so much. Less than 10 leaves the message
     glued to the label below, as if it were its own. */
  .groups {
    display: grid;
    gap: 10px;
  }

  .group {
    display: grid;
    gap: 4px;
  }

  /* One line of .small (13 px with the body's line-height 1.5). A message that
     wraps onto two, on the phone, still pushes one line. */
  .field-error {
    min-height: 19.5px;
  }

  /* The signal sits inside the border, on the right, and the text does not run
     under it: 12 px of margin + 18 of icon + 8 of slack. */
  .field {
    position: relative;
    display: block;
  }

  .field input {
    padding-right: 38px;
  }

  .signal {
    position: absolute;
    top: 50%;
    right: 12px;
    transform: translateY(-50%);
    pointer-events: none;
  }

  .signal.ok {
    color: var(--ok);
  }

  .signal.error {
    color: var(--bad);
  }

  .field input[aria-invalid='true'] {
    border-color: var(--bad);
  }
</style>
