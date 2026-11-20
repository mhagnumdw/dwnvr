<script lang="ts">
  import { onMount, untrack } from 'svelte';
  import type { Component } from 'svelte';
  import {
    session,
    cameras,
    health,
    build,
    checkSession,
    loadCameras,
    loadBuild,
    checkNewVersion,
    watchNotices,
    logout,
    isAdmin,
    sessionDropped,
    RELEASES_URL,
  } from './lib/state.svelte.ts';
  import { routeState, DEFAULT_ROUTE, fixRoute, screenLink, scene } from './lib/route.svelte.ts';
  import { appInstalled, copy } from './lib/browser.ts';
  import { newScreen, watchScreenVersion } from './lib/screen-version.svelte.ts';
  import { setUnauthorizedHandler } from './lib/api.ts';
  import type { Person } from './lib/api.ts';
  import { noticesOf, noticesSummary } from './lib/notices.ts';
  import { closeNotice } from './lib/snackbar.svelte.ts';
  import { installTouchHint } from './lib/touchHint.ts';
  import Login from './routes/Login.svelte';
  import Live from './routes/Live.svelte';
  import Recordings from './routes/Recordings.svelte';
  import Cameras from './routes/Cameras.svelte';
  import Diagnostics from './routes/Diagnostics.svelte';
  import Avatar from './components/Avatar.svelte';
  import Icon from './components/Icon.svelte';
  import Snackbar from './components/Snackbar.svelte';
  import type { UiIconName } from './lib/uiIcons.ts';
  import type InviteScreen from './routes/Invite.svelte';

  interface Route {
    id: string;
    label: string;
    icon?: UiIconName;
    // The screen that already ships in the main bundle, or the import of its
    // separate chunk.
    component?: Component;
    load?: () => Promise<{ default: Component }>;
    onlyWithObjectDetector?: boolean;
    adminOnly?: boolean;
    noTab?: boolean;
  }

  // This list is never empty: the first entry is the default.
  const ROUTES: readonly [Route, ...Route[]] = [
    { id: 'live', label: 'Ao vivo', icon: 'live', component: Live },
    { id: 'rec', label: 'Gravações', icon: 'clock', component: Recordings },
    // Separate chunk: only whoever opens the screen pays for its bytes. It only
    // exists with the object detector configured - without it there are no
    // detections to list.
    {
      id: 'detection',
      label: 'Detecções',
      icon: 'detection',
      load: () => import('./routes/Detections.svelte'),
      onlyWithObjectDetector: true,
    },
    // Both are admin-only. Hiding the tab is not security - the API rejects the
    // viewer with 403 - it just avoids showing what they cannot use. Users
    // comes in a separate chunk: the viewer never downloads the screen.
    { id: 'cams', label: 'Câmeras', icon: 'list', component: Cameras, adminOnly: true },
    {
      id: 'usuarios',
      label: 'Usuários',
      icon: 'users',
      load: () => import('./routes/Users.svelte'),
      adminOnly: true,
    },
    // For the viewer, only "Este navegador" and the version.
    { id: 'health', label: 'Diagnóstico', icon: 'heart', component: Diagnostics },
    // No tab: you get there through the avatar in the header. It only exists
    // with login, because without it there is no one the account could belong
    // to.
    {
      id: 'conta',
      label: 'Minha conta',
      load: () => import('./routes/Account.svelte'),
      noTab: true,
    },
  ];

  const canSee = (r: Route) =>
    (!r.onlyWithObjectDetector || cameras.detector) &&
    (!r.adminOnly || isAdmin()) &&
    (r.id !== 'conta' || session.authRequired);
  const tabs = $derived(ROUTES.filter((r) => !r.noTab && canSee(r)));

  // The invite link (`#convite?token=…`) opens the set-password screen, with or
  // without a session: whoever receives it does not have a password yet.
  // Separate chunk, because only people who open an invite link use it.
  const invite = $derived(routeState.id === 'convite');
  let Invite = $state<typeof InviteScreen | null>(null);
  $effect(() => {
    if (invite && !Invite) {
      void import('./routes/Invite.svelte').then((m) => {
        Invite = m.default;
      });
    }
  });

  // The tab that depends on the object detector is only decided after
  // /api/cameras. Until then the screen stays blank: falling back to the
  // default and switching right after would mount and unmount Live, with its
  // connections, for nothing.
  const requested = $derived(ROUTES.find((r) => r.id === routeState.id));
  const waiting = $derived(requested?.onlyWithObjectDetector && cameras.detector === null);

  // Hash routing: there are few screens, and a real router would cost more
  // bytes than the rest of the app combined. `lib/route.svelte.ts` reads and
  // writes the hash, because it also carries the state of each screen.
  // A hash that points to a screen that does not exist or that the person
  // cannot see - a typo, a link from a future version, `#detection` on a server
  // without the object detector, `#usuarios` opened by the viewer: the default
  // screen takes over.
  const route = $derived(
    ROUTES.find((r) => r.id === routeState.id && canSee(r)) ??
      ROUTES.find((r) => r.id === DEFAULT_ROUTE) ??
      ROUTES[0],
  );

  // The screen goes in the title, so the browser tab and the history say where
  // you are. On login, and while the screen is not decided yet, only the brand
  // shows: a passing "Ao vivo" before "Detecções" would be a lie for a moment.
  const title = $derived(
    invite || !session.checked || (session.authRequired && !session.authenticated) || waiting
      ? 'dwnvr'
      : `dwnvr · ${route.label}`,
  );

  // The screens in separate chunks that are already downloaded: id ->
  // component.
  let loadedComponents = $state<Record<string, Component>>({});
  const Screen = $derived(waiting ? null : (route.component ?? loadedComponents[route.id]));

  // The ones that failed to download. Without this, the failure left the screen
  // blank and said nothing. Leaving the screen and coming back tries again.
  let failedList = $state<Record<string, boolean>>({});

  $effect(() => {
    if (waiting) return;
    const r = route;
    if (r.load && !loadedComponents[r.id]) {
      r.load().then(
        (m) => {
          loadedComponents[r.id] = m.default;
          failedList[r.id] = false;
        },
        () => {
          failedList[r.id] = true;
        },
      );
    }
  });

  $effect(() => watchScreenVersion());

  const loggedIn = $derived(session.checked && (!session.authRequired || session.authenticated));

  // The address bar tells which screen is open. When the default takes over
  // (see route), the state it writes would end up under the requested name
  // (`#usuarios?cams=…`). Only after the session and the object detector are
  // decided: before that, even the admin looks like a viewer, and would lose
  // the `#usuarios` on reload.
  $effect(() => {
    if (loggedIn && !invite && !waiting && route.id !== routeState.id) fixRoute(route.id);
  });

  // The header bell: the same notices as the Diagnostics screen card, counted
  // by the same function. It only polls with an open session - without one
  // there is no header - and only for the admin: the viewer has no bell, and
  // their screen does not reread health.
  $effect(() => {
    if (!loggedIn || !isAdmin()) return;
    return watchNotices();
  });
  const bell = $derived(noticesSummary(noticesOf(health)));
  const bellTitle = $derived(
    `${bell.total} ${bell.total === 1 ? 'aviso' : 'avisos'} no Diagnóstico`,
  );

  // Clicking the tab you are already on must not restart the screen. The href
  // used to equal the hash and the browser fired no event; now the hash carries
  // the state (`#rec?cam=x&t=…`), and the same click would become a navigation
  // to a bare `#rec` - throwing away exactly what the URL now keeps.
  function navigate(ev: MouseEvent, id: string) {
    if (id === route.id) ev.preventDefault();
  }

  // Sharing the screen, only in the installed app: in the browser the address
  // bar already carries the scene, and the app has none. The system menu
  // (WhatsApp, email) where available - it requires HTTPS -; elsewhere, copy
  // the link.
  const installed = appInstalled();
  let copiedScreen = $state('');
  let copiedTimer: number | undefined;

  // The scene in words goes in the title - the email subject - and in the text,
  // which WhatsApp sends along with the link. A screen without a scene gets the
  // browser tab title. Copying carries only the link: whoever pastes expects a
  // link.
  //
  // With a scene, the link goes inside the text, on its own line, and not in
  // the `url` field: Android Chrome joins text and url with a space, and the
  // link stuck to the end of the sentence. WhatsApp finds the link in the text
  // and builds the preview the same way.
  async function shareScreen() {
    const url = screenLink();
    if (typeof navigator.share === 'function') {
      const data: ShareData = scene.texto
        ? { title: `dwnvr · ${scene.texto}`, text: `${scene.texto}\n${url}` }
        : { title, url };
      try {
        await navigator.share(data);
      } catch {
        // Dismissing the system menu also lands here. Nothing to do.
      }
      return;
    }
    copiedScreen = (await copy(url)) ? 'link copiado' : 'não deu para copiar';
    clearTimeout(copiedTimer);
    copiedTimer = setTimeout(() => (copiedScreen = ''), 2000);
  }

  // The version in use, fetched at boot. The query to GitHub for a new version
  // waits for it and for the session: only the admin sees the pill, and the
  // viewer does not spend the request.
  let version: Promise<void> | undefined;

  function maybeNewVersion() {
    if (isAdmin()) void version?.then(checkNewVersion);
  }

  onMount(async () => {
    setUnauthorizedHandler(sessionDropped);
    // Outside the session await: the login screen also shows the version, and
    // there is no reason for one fetch to wait for the other.
    version = loadBuild();
    await checkSession();
    if (session.authenticated) {
      void loadCameras();
      maybeNewVersion();
    }
  });

  onMount(installTouchHint);

  // The notice belongs to the screen that raised it: it does not carry over to
  // the next one. `routeState.hash` only changes on a real navigation, the same
  // one that remounts the screen.
  $effect(() => {
    /* eslint-disable-next-line @typescript-eslint/no-unused-expressions -- read only, so the $effect tracks it */
    routeState.hash;
    // Without untrack, the notice read inside would become a dependency, and
    // each new notice would close itself as it opened.
    untrack(() => {
      closeNotice();
    });
  });

  async function afterLogin(person: Person) {
    session.authenticated = true;
    session.pessoa = person;
    maybeNewVersion();
    await loadCameras();
  }

  // Once the password is set through the link, the person is already signed in.
  // The link leaves the history: going back must not reopen a screen with the
  // token, which is no longer valid.
  function afterInvite(person: Person) {
    session.checked = true;
    location.replace(location.pathname + location.search + '#' + DEFAULT_ROUTE);
    void afterLogin(person);
  }

  // The avatar menu: "Minha conta" and "Sair", for both roles. "Sair" takes one
  // extra tap, which makes it harder to sign out by accident.
  let accountMenu = $state(false);

  function outsideMenu(e: MouseEvent) {
    if (accountMenu && !(e.target instanceof Element && e.target.closest('.account')))
      accountMenu = false;
  }

  let signingOut = $state(false);

  async function leave() {
    signingOut = true;
    await logout();
    signingOut = false;
    accountMenu = false;
  }

  const role = $derived(session.pessoa?.role === 'admin' ? 'administrador' : 'usuário');
</script>

<svelte:head>
  <title>{title}</title>
</svelte:head>

<svelte:window
  onclick={outsideMenu}
  onkeydown={(e: KeyboardEvent) => {
    if (e.key === 'Escape') accountMenu = false;
  }}
/>

{#if invite}
  {#if Invite}<Invite onSuccess={afterInvite} />{:else}<div class="boot">carregando…</div>{/if}
{:else if !session.checked}
  <div class="boot">carregando…</div>
{:else if session.authRequired && !session.authenticated}
  <Login onSuccess={afterLogin} />
{:else}
  <!-- The new-version strip slides down with the header. Whoever measures the
       header height to dock at the bottom (the Detections screen) measures this
       block. -->
  <div class="top">
    <header>
      <span class="brand">
        <!-- The same file as the favicon, served from public/: one brand, one
           place to change it. -->
        <img class="mark" src="/favicon.svg" alt="" width="24" height="24" />
        dwnvr
      </span>
      <nav class="top">
        {#each tabs as r (r.id)}
          <a
            href="#{r.id}"
            class:active={r.id === route.id}
            onclick={(e) => {
              navigate(e, r.id);
            }}
          >
            {r.label}
          </a>
        {/each}
      </nav>
      <div class="actions">
        {#if installed}
          <div class="share">
            <button
              class="icon"
              onclick={shareScreen}
              title="Compartilhar esta tela"
              aria-label="Compartilhar esta tela"
            >
              <Icon name="share" size={21} />
            </button>
            {#if copiedScreen}<span class="copied" role="status">{copiedScreen}</span>{/if}
          </div>
        {/if}
        <!-- Disappears without notice: an unlit bell always in view would teach
           people not to look at it. The bubble color is the worst notice's
           color. -->
        {#if bell.total}
          <a
            class="bell {bell.nivel}"
            href="#health"
            title={bellTitle}
            aria-label={bellTitle}
            onclick={(e) => {
              navigate(e, 'health');
            }}
          >
            <Icon name="bell" size={21} />
            <span class="bubble">{bell.total}</span>
          </a>
        {/if}
        <!-- Leads to the releases, and not straight to the new version: whoever
           skipped a few needs to read the notes of all of them since theirs. -->
        {#if build.newOne && isAdmin()}
          <a
            class="new"
            href={RELEASES_URL}
            target="_blank"
            rel="noopener"
            title="Versão nova do dwnvr: {build.newOne} (você está na {build.version}). Veja o que mudou e como atualizar."
          >
            <Icon name="arrowUp" size={14} />
            {build.newOne}
          </a>
        {/if}
        <!-- Without authentication configured there is no one the account could
           belong to and no session to end, and a button that does nothing is
           worse than no button. The ring marks the open menu, and "Minha
           conta", which has no tab. -->
        {#if session.authRequired && session.pessoa}
          <div class="account">
            <button
              class="avbtn"
              class:active={accountMenu || route.id === 'conta'}
              onclick={() => (accountMenu = !accountMenu)}
              aria-haspopup="menu"
              aria-expanded={accountMenu}
              aria-label="conta de {session.pessoa.name}"
              title={session.pessoa.name}
            >
              <Avatar person={session.pessoa} size={32} />
            </button>
            {#if accountMenu}
              <div class="account-menu" role="menu">
                <div class="who">
                  <Avatar person={session.pessoa} size={40} />
                  <div>
                    <strong>{session.pessoa.name}</strong>
                    <span class="muted small">@{session.pessoa.username} · {role}</span>
                  </div>
                </div>
                <div class="sep"></div>
                <a
                  href="#conta"
                  role="menuitem"
                  class:current={route.id === 'conta'}
                  onclick={(e) => {
                    navigate(e, 'conta');
                    accountMenu = false;
                  }}>Minha conta</a
                >
                <div class="sep"></div>
                <button role="menuitem" onclick={leave} disabled={signingOut}>
                  {signingOut ? 'saindo…' : 'Sair'}
                </button>
              </div>
            {/if}
          </div>
        {/if}
      </div>
    </header>
    <!-- No close button: postponing the reload only postpones the chunk that will not download. -->
    {#if newScreen.available}
      <div class="strip" role="status">
        <span class="txt">
          O dwnvr foi atualizado. <span class="muted">Recarregue para usar a versão nova.</span>
        </span>
        <button class="primary" onclick={() => location.reload()}>Recarregar</button>
      </div>
    {/if}
  </div>

  <main>
    <!-- The key forces a remount when switching screens: each one has heavy
         resources (live connections, MediaSource) that must be released, and
         relying on manual cleanup would be a sure source of leaks.

         It is the whole hash, not just the route, because each screen reads its
         state from the URL once, on mount. `routeState.hash` only changes on a
         real navigation - back, forward, a URL pasted by hand - and that is
         when remounting is right: the screen's own writes use replaceState,
         which does not fire hashchange, so they remount nothing. -->
    {#key routeState.hash}
      {#if Screen}
        <Screen />
      {:else if route.load && failedList[route.id]}
        <div class="failed">
          <div class="ico"><Icon name="refresh" size={28} /></div>
          <h3>Não deu para abrir esta tela</h3>
          <p>
            {#if newScreen.available}
              O dwnvr foi atualizado desde que esta página foi aberta. Recarregue para continuar;
              você volta para esta mesma tela.
            {:else}
              A conexão com o dwnvr falhou. Recarregue para tentar de novo.
            {/if}
          </p>
          <button class="primary" onclick={() => location.reload()}>Recarregar</button>
        </div>
      {/if}
    {/key}
  </main>

  <nav class="bottom" style:--abas={tabs.length}>
    {#each tabs as r (r.id)}
      <a
        href="#{r.id}"
        class:active={r.id === route.id}
        onclick={(e) => {
          navigate(e, r.id);
        }}
      >
        <span class="icon">
          {#if r.icon}<Icon name={r.icon} size={18} />{/if}
        </span>
        <span class="label">{r.label}</span>
      </a>
    {/each}
  </nav>

  <Snackbar />
{/if}

<style>
  /* The whole #app column, which has the window height (see app.css). */
  .boot {
    display: grid;
    place-items: center;
    flex: 1;
    color: var(--dim);
  }

  /* The bar is always there, also on the phone without login, where it only has
     the brand: it is the same corner for the new-version notice in any case,
     and people who use login already paid for the ~37px it takes from the live
     grid. On the phone the navigation lives at the bottom. */
  header {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 6px 12px;
    background: var(--panel);
    border-bottom: 1px solid var(--line);
  }

  .top {
    position: sticky;
    top: 0;
    z-index: 20;
  }

  /* The release pill's blue, only opaque: the strip sits over the video when
     the page scrolls. */
  .strip {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 6px 6px 12px;
    background: #14233b;
    border-bottom: 1px solid rgba(47, 129, 247, 0.55);
    font-size: 14px;
  }

  .strip .txt {
    flex: 1;
    min-width: 0;
    line-height: 1.3;
  }

  /* 34px, like the pill and the avatar: the regular button's 44px would fatten
     the strip. */
  .strip button {
    min-height: 34px;
    padding: 5px 14px;
    font-size: 14px;
  }

  .failed {
    max-width: 420px;
    margin: 48px auto;
    padding: 24px 18px;
    display: grid;
    gap: 10px;
    justify-items: center;
    text-align: center;
  }

  .failed .ico {
    color: var(--accent);
  }

  .failed h3 {
    margin: 0;
    font-size: 16px;
  }

  .failed p {
    margin: 0;
    color: var(--dim);
    font-size: 14px;
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 8px;
    font-weight: 700;
    letter-spacing: 0.3px;
  }

  /* The SVG already has its own rounding, so no border-radius here - doubling
     the radius would distort the corners. */
  .mark {
    display: block;
    flex: none;
  }

  nav.top {
    display: none;
  }

  /* Pushed to the opposite end from the brand. The avatar, which leads to
     "Sair", stays away from the thumb that navigates along the bottom: signing
     out by accident costs retyping the password. */
  .actions {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 12px;
  }

  /* A pill and not a dot: with no hover on the phone, a lone dot does not say
     what it is. The number already does. The background and the border are the
     translucent --accent. */
  .new {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    /* The same height as the avatar next to it. */
    min-height: 34px;
    padding: 5px 12px;
    border-radius: 999px;
    font-size: 13px;
    font-weight: 600;
    color: var(--accent);
    background: rgba(47, 129, 247, 0.12);
    border: 1px solid rgba(47, 129, 247, 0.55);
    text-decoration: none;
    white-space: nowrap;
  }

  .new:hover {
    background: rgba(47, 129, 247, 0.2);
  }

  /* A bell and not a pill: it is the smallest of the alerts, 34px like the pill
     and the avatar beside it, and the drawing already says "there is a
     notification" without text. The drawing is an inline SVG, not an emoji: the
     emoji changes with each system and does not take color. */
  .bell {
    position: relative;
    display: inline-grid;
    place-items: center;
    width: 34px;
    height: 34px;
    border-radius: 999px;
    color: var(--fg);
  }

  /* Yellows only: the bell steps back, and only the bubble calls attention. */
  .bell.warn {
    color: var(--dim);
  }

  @media (hover: hover) {
    .bell:hover {
      background: var(--panel-2);
    }
  }

  /* The share button has the bell's size and stroke, but does not draw
     attention: it takes the muted text color. */
  .share {
    position: relative;
  }

  .icon {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    min-height: 0;
    padding: 0;
    border: none;
    border-radius: 999px;
    background: none;
    color: var(--dim);
  }

  @media (hover: hover) {
    .icon:hover {
      background: var(--panel-2);
      color: var(--fg);
    }
  }

  /* Opens under the icon, like the avatar menu. */
  .copied {
    position: absolute;
    z-index: 30;
    top: calc(100% + 6px);
    right: 0;
    padding: 6px 10px;
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--panel);
    font-size: 13px;
    white-space: nowrap;
  }

  /* A border in the header color separates the bubble from the bell drawing. */
  .bubble {
    position: absolute;
    top: -2px;
    left: 18px;
    min-width: 15px;
    height: 15px;
    padding: 0 3px;
    box-sizing: content-box;
    border: 2px solid var(--panel);
    border-radius: 999px;
    font-size: 10px;
    font-weight: 700;
    line-height: 15px;
    text-align: center;
    font-variant-numeric: tabular-nums;
  }

  .bell.bad .bubble {
    background: var(--bad);
    color: #fff;
  }
  .bell.warn .bubble {
    background: var(--warn);
    color: #1b1300;
  }

  /* The menu attaches to the avatar, not to the header: it opens under it. */
  .account {
    position: relative;
  }

  /* 34px, like the bell and the pill beside it. */
  .avbtn {
    display: grid;
    place-items: center;
    width: 34px;
    height: 34px;
    min-height: 0;
    padding: 0;
    border-radius: 50%;
    background: none;
  }

  .avbtn.active {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent);
  }

  /* The same drawing as the ⋮ menu of the Detections and Users screens. */
  .account-menu {
    position: absolute;
    z-index: 30;
    top: calc(100% + 6px);
    right: 0;
    min-width: 240px;
    max-width: calc(100vw - 24px);
    padding: 4px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.5);
  }

  .account-menu .who {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 10px;
  }

  .account-menu .who div {
    min-width: 0;
  }

  .account-menu .who strong,
  .account-menu .who span {
    display: block;
    line-height: 1.25;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .account-menu .sep {
    height: 1px;
    margin: 4px 0;
    background: var(--line);
  }

  .account-menu a,
  .account-menu button {
    display: flex;
    align-items: center;
    width: 100%;
    min-height: 40px;
    padding: 0 12px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--fg);
    font-size: 14px;
    text-align: left;
    text-decoration: none;
  }

  .account-menu a.current {
    color: var(--accent);
  }

  @media (hover: hover) {
    .account-menu a:hover,
    .account-menu button:hover:not(:disabled) {
      background: var(--panel-2);
    }
  }

  main {
    /* Room so the bottom navigation does not cover the content. */
    padding-bottom: var(--nav-h);
    /* Takes what is left of the #app column, from the window height, instead of
       asking for the full height and pushing the document past it. */
    flex: 1;
  }

  nav.bottom {
    position: fixed;
    inset: auto 0 0 0;
    display: grid;
    grid-template-columns: repeat(var(--abas), 1fr);
    background: var(--panel);
    border-top: 1px solid var(--line);
    padding-bottom: env(safe-area-inset-bottom);
    z-index: 20;
  }

  nav.bottom a {
    display: grid;
    justify-items: center;
    gap: 2px;
    padding: 8px 4px;
    color: var(--dim);
    text-decoration: none;
    font-size: 11px;
  }

  nav.bottom a.active {
    color: var(--accent);
  }

  .icon {
    height: 18px;
    display: grid;
    place-items: center;
  }

  /* On the desktop the navigation moves up: the thumb is no longer the
     constraint, and screen height becomes the scarce resource. */
  @media (min-width: 720px) {
    /* The navigation moves up into the bar, which gets extra room. */
    header {
      gap: 20px;
      padding: 10px 18px;
    }

    .strip {
      justify-content: center;
      padding: 6px 18px;
    }

    .strip .txt {
      flex: none;
    }

    nav.top {
      display: flex;
      gap: 4px;
    }

    nav.top a {
      padding: 7px 12px;
      border-radius: 8px;
      color: var(--dim);
      text-decoration: none;
    }

    nav.top a:hover {
      background: var(--panel-2);
    }
    nav.top a.active {
      color: var(--fg);
      background: var(--panel-2);
    }

    nav.bottom {
      display: none;
    }
    main {
      padding-bottom: 0;
    }
  }
</style>
