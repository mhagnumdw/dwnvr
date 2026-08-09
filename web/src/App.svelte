<script>
  import { onMount } from 'svelte';
  import { session, checkSession, loadCameras } from './lib/state.svelte.js';
  import { setUnauthorizedHandler } from './lib/api.js';
  import Login from './routes/Login.svelte';
  import Live from './routes/Live.svelte';
  import Recordings from './routes/Recordings.svelte';
  import Cameras from './routes/Cameras.svelte';
  import Health from './routes/Health.svelte';

  const ROUTES = [
    { id: 'live', label: 'Ao vivo', icon: '◉', component: Live },
    { id: 'rec', label: 'Gravações', icon: '⏱', component: Recordings },
    { id: 'cams', label: 'Câmeras', icon: '☰', component: Cameras },
    { id: 'health', label: 'Diagnóstico', icon: '♥', component: Health },
  ];

  // Roteamento por hash: são quatro telas, e um roteador de verdade custaria
  // mais bytes que o resto do aplicativo junto.
  let hash = $state(location.hash.slice(1) || 'rec');
  const route = $derived(ROUTES.find((r) => r.id === hash) ?? ROUTES[1]);

  function onHashChange() {
    hash = location.hash.slice(1) || 'rec';
  }

  onMount(async () => {
    setUnauthorizedHandler(() => {
      session.authenticated = false;
    });
    await checkSession();
    if (session.authenticated) loadCameras();
  });

  async function afterLogin() {
    session.authenticated = true;
    await loadCameras();
  }
</script>

<svelte:window onhashchange={onHashChange} />

{#if !session.checked}
  <div class="boot">carregando…</div>
{:else if session.authRequired && !session.authenticated}
  <Login onSuccess={afterLogin} />
{:else}
  <header>
    <span class="brand">
      <!-- O mesmo arquivo do favicon, servido de public/: uma marca só, um
           lugar só para mudar. -->
      <img class="mark" src="/favicon.svg" alt="" width="24" height="24" />
      dwnvr
    </span>
    <nav class="top">
      {#each ROUTES as r (r.id)}
        <a href="#{r.id}" class:active={r.id === route.id}>{r.label}</a>
      {/each}
    </nav>
  </header>

  <main>
    <!-- A chave força a remontagem ao trocar de tela: cada uma tem recursos
         pesados (conexões de live, MediaSource) que precisam ser liberados,
         e depender de limpeza manual seria fonte garantida de vazamento. -->
    {#key route.id}
      <route.component />
    {/key}
  </main>

  <nav class="bottom">
    {#each ROUTES as r (r.id)}
      <a href="#{r.id}" class:active={r.id === route.id}>
        <span class="icon">{r.icon}</span>
        <span class="label">{r.label}</span>
      </a>
    {/each}
  </nav>
{/if}

<style>
  .boot {
    display: grid;
    place-items: center;
    height: 100dvh;
    color: var(--dim);
  }

  header {
    display: none;
  }

  main {
    /* Espaço para a navegação inferior não cobrir o conteúdo. */
    padding-bottom: var(--nav-h);
    min-height: 100dvh;
  }

  nav.bottom {
    position: fixed;
    inset: auto 0 0 0;
    display: grid;
    grid-template-columns: repeat(4, 1fr);
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
    font-size: 18px;
    line-height: 1;
  }

  /* No desktop a navegação sobe: o polegar deixa de ser a restrição e a
     altura da tela passa a ser o recurso escasso. */
  @media (min-width: 720px) {
    header {
      display: flex;
      align-items: center;
      gap: 20px;
      padding: 10px 18px;
      background: var(--panel);
      border-bottom: 1px solid var(--line);
      position: sticky;
      top: 0;
      z-index: 20;
    }

    .brand {
      display: flex;
      align-items: center;
      gap: 8px;
      font-weight: 700;
      letter-spacing: 0.3px;
    }

    /* O SVG já traz o próprio arredondamento, então nada de border-radius
       aqui — dobrar o raio deformaria os cantos. */
    .mark {
      display: block;
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

    nav.top a:hover { background: var(--panel-2); }
    nav.top a.active { color: var(--fg); background: var(--panel-2); }

    nav.bottom { display: none; }
    main { padding-bottom: 0; min-height: 0; }
  }
</style>
