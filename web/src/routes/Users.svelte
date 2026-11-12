<!--
  Usuários: quem entra no dwnvr além do dono. Sem SMTP, o admin cria a pessoa,
  o dwnvr gera um link, e ela mesma define a senha por ele. O mesmo link serve
  para "esqueci a senha" e "perdi o celular": gerar outro apaga a senha atual.
  Só o admin chega aqui; a API recusa o viewer com 403.
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

  // Nulo até a API responder.
  let data = $state<Users | null>(null);
  let error = $state('');

  // O link recém-gerado. Ver mostrarLink.
  let generated = $state<{
    username: string;
    name: string;
    email: string; // vazio sem e-mail
    url: string;
    expiresInMs: number;
  } | null>(null);

  // O relógio da contagem dos links, num tique de um segundo. Só anda com um
  // link aberto na tela.
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
      // O "hoje" e o "ontem" do último acesso contam daqui: sem link aberto,
      // o tique não anda.
      now = Date.now();
      error = '';
    } catch (e) {
      error = messageOf(e);
    }
  }

  onMount(load);

  // Sair da tela com uma conferência na espera: nada mais vai ao servidor.
  onDestroy(() => {
    newValidation.stop();
    emailValidation.stop();
  });

  // ---- o link recém-gerado -------------------------------------------------
  //
  // É o único lugar onde o link existe: o servidor guarda só o hash do token.
  // Sai do endereço pelo qual o admin está acessando, que é o que a pessoa
  // também alcança na maioria dos casos (o mesmo nome no Tailscale, o mesmo IP
  // na rede de casa).

  let copied = $state('');

  function showLink(r: Invite) {
    generated = {
      username: r.user.username,
      name: r.user.name,
      email: r.user.email ?? '',
      url: `${location.origin}${location.pathname}#convite?token=${r.token}`,
      // O link novo sempre vem com o prazo; sem ele, conta como vencido.
      expiresInMs: r.user.inviteExpiresAtMs ?? 0,
    };
    copied = '';
    delivery = null;
  }

  // O link por e-mail: vai o mesmo link da tela, para o e-mail da pessoa. O
  // resultado fica no card, e mandar de novo é tocar de novo.
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

  // O Compartilhar abre o menu do sistema (e-mail, WhatsApp). Só existe onde o
  // navegador oferece, e só em HTTPS; no resto fica o Copiar, que funciona
  // também em HTTP.
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
      // Cancelar o menu do sistema também cai aqui. Nada a fazer.
    }
  }

  // "9:41", até o link vencer.
  function missing(ms: number) {
    const s = Math.max(0, Math.ceil((ms - now) / 1000));
    return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
  }

  // ---- criar ----------------------------------------------------------------

  let newItem = $state<{ username: string; name: string; email: string } | null>(null); // com o formulário aberto
  let saving = $state(false);
  let newError = $state('');
  // Se o admin já escreveu no usuário. Até lá, o usuário segue o nome (ver
  // sugerirUsuario); apagar tudo devolve a sugestão.
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

  // "João da Silva Santos" vira "joao.santos": o primeiro e o último nome, sem
  // acento, em minúsculas, só com o que o usuário aceita, cortado no tamanho
  // máximo.
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

  // Quem preencheu o usuário foi o nome, e o admin talvez nem passe por ele:
  // o usuário sugerido é conferido junto, para o "já existe" aparecer antes
  // do salvar.
  function onNameLeave() {
    if (!newItem) return;
    newValidation.leave('name', newItem.name);
    if (!typedUser) newValidation.leave('username', newItem.username);
  }

  function onUserInput() {
    typedUser = newItem?.username !== '';
    newValidation.typeIn();
  }

  // O primeiro campo recusado, na ordem da tela, recebe o foco.
  function focusFieldError(
    validation: Validation,
    campos: Partial<Record<PersonField, HTMLInputElement>>,
    order: PersonField[],
  ) {
    const field = order.find((c) => validation.condition(c) === 'erro');
    if (field) campos[field]?.focus();
  }

  // A recusa do salvar é dos valores enviados. Ela vale só para o formulário
  // que a pediu (`conf`), se ele ainda está aberto; e, se os campos mudaram
  // enquanto a resposta vinha, eles são conferidos de novo, em vez de levarem
  // o vermelho de um valor que já não está lá.
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
    // Os campos mudaram (ou o formulário fechou) enquanto a resposta vinha.
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

  // ---- o ⋮ de cada pessoa ----------------------------------------------------

  let menu = $state<string | null>(null); // o usuário com o menu aberto
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
    // Link novo para quem ainda não tem senha não derruba ninguém: só troca
    // um link por outro, e dispensa a confirmação.
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

  // ---- o e-mail de quem já existe ------------------------------------------

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
    // Numa closure, o TypeScript não supõe que o formulário segue aberto.
    const changed = () => editingEmail?.email !== sent;
    savingEmail = true;
    emailError = '';
    try {
      const { user } = await api.changeEmail(editingEmail.u.username, sent);
      // O link aberto no card passa a ir para o e-mail novo.
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

  // O sinal à direita do campo. O verde só aparece com algo escrito: no
  // e-mail, que é opcional, um "certo" no campo vazio não diria nada. `soErro`
  // é o campo que só avisa o que está errado, como o nome.
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
            <a class="small" href="#conta">Minha conta</a>
          {:else}
            <span class="muted small">do dwnvr.yaml</span>
          {/if}
        </div>
      {/if}
      {#each data.users as u (u.username)}
        <div class="row">
          <Avatar person={u} size={36} />
          <!-- O nome e o chip: no celular, um embaixo do outro; de 720 px para
               cima, lado a lado (ver .meio). -->
          <div class="middle">
            <div class="who">
              <div class="name"><strong>{u.name}</strong></div>
              <div class="muted small login">
                @{u.username}{u.role === 'admin' ? ' · administrador' : ''}
              </div>
              {#if u.email}<div class="muted small login">{u.email}</div>{/if}
              <!-- Quem está ativo tem o último acesso no chip, que é o que se
                   quer saber no dia a dia, e aqui a data em que definiu a senha.
                   Quem tem link aberto ou vencido tem o link no chip, e aqui o
                   acesso de antes, se houver. -->
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
              <!-- O ponto verde diz que está ativo. Sem acesso registrado é
                   quem não usou o dwnvr desde que o último acesso passou a ser
                   anotado. -->
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
              <!-- Para quem está ativo, o link novo apaga a senha: o rótulo diz
                   isso. Para quem não definiu, não há senha a redefinir. -->
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
              <!-- O admin não troca a foto de ninguém; só tira uma que não sirva. -->
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

<!-- O e-mail serve para entrar mesmo sem a seção smtp; só o envio do link
     depende dela, e a tela avisa isso onde o e-mail é escrito. -->
{#snippet noDelivery()}
  <p class="small muted no-delivery">
    <Icon name="info" size={14} />
    O envio de e-mail não está configurado neste dwnvr: o link não vai por e-mail, só copiado.
  </p>
{/snippet}

<!-- O sinal à direita de um campo conferido pelo servidor (ver
     lib/conferencia.svelte.ts). O texto da recusa vai embaixo, no `recusa`. -->
{#snippet fieldSignal(condition: FieldState)}
  {#if condition}
    <span class="signal {condition}"
      ><Icon name={condition === 'ok' ? 'checkmark' : 'close'} size={18} /></span
    >
  {/if}
{/snippet}

<!-- A linha da recusa existe sempre, vazia ou não: o espaço já está guardado,
     e a mensagem aparece e some sem empurrar o resto do formulário. -->
{#snippet fieldError(id: string, text: string)}
  <p class="small error field-error" {id}>{text}</p>
{/snippet}

<!-- novalidate: a recusa do navegador é um balão com outro desenho e outro
     texto; aqui todas vêm do servidor e ficam embaixo do campo. -->
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
  /* A coluna é minmax(0, 1fr), e não a implícita: esta cresce até a largura
     mínima do conteúdo, e um nome comprido, que não quebra, empurrava a tela
     para além da largura do celular. Assim o nome corta com reticências. */
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

  /* O ícone fica centrado na primeira linha quando o texto quebra: 13px com o
     line-height 1.5 do body dão 19,5px de linha, e o ícone tem 14. */
  .no-delivery {
    display: flex;
    gap: 6px;
    align-items: flex-start;
  }
  .no-delivery :global(svg) {
    flex: none;
    margin-top: 3px;
  }

  /* A borda de destaque é o --accent translúcido, a mesma da pílula de versão. */
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

  /* O link inteiro, quebrando onde precisar: cortado com reticências, não
     daria para conferir o que vai ser mandado. */
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

  /* No celular, o chip é a quarta linha, embaixo do nome, do @usuário e da
     data: ao lado deles, disputava a largura com o nome e quebrava em duas
     linhas. De 720 px para cima há largura, e ele volta para a direita. O
     avatar e o ⋮ ficam no meio da linha nos dois casos. */
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

  /* Em 320 px, nem na linha própria cabe "último acesso ontem 22:47": o chip
     quebra antes da data, e o raio deixa de ser de pílula. */
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

  /* O ícone é um svg em bloco, que o text-align do botão não centraliza: sem
     o flex, ele encostava à esquerda, 8 px fora do centro. */
  .mais {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    min-height: 36px;
    padding: 0;
  }

  /* O mesmo desenho do menu do ⋮ da tela Detecções, preso à linha. */
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

  /* O campo e a linha da recusa embaixo dele, mais perto um do outro que
     dos outros campos. A linha guardada (ver o snippet recusa) ocupa o vão
     entre um campo e o próximo: os grupos ficam a 10 px, e não aos 14 do
     formulário, para o vão vazio não crescer tanto. Menos que 10 deixa a
     mensagem colada no rótulo de baixo, como se fosse dele. */
  .groups {
    display: grid;
    gap: 10px;
  }

  .group {
    display: grid;
    gap: 4px;
  }

  /* Uma linha do .small (13 px com o line-height 1.5 do body). A mensagem
     que quebra em duas, no celular, ainda empurra uma linha. */
  .field-error {
    min-height: 19.5px;
  }

  /* O sinal fica dentro da borda, à direita, e o texto não passa por baixo
     dele: 12 px de margem + 18 do ícone + 8 de folga. */
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
