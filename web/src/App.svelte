<script>
  import { onMount } from 'svelte';
  import { session, checkSession, loadCameras, loadBuild, logout } from './lib/state.svelte.js';
  import { rota, ROTA_PADRAO } from './lib/rota.svelte.js';
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
  // mais bytes que o resto do aplicativo junto. Quem lê e escreve o hash é o
  // `lib/rota.svelte.js`, porque ele carrega também o estado de cada tela.
  // Hash apontando para tela que não existe - erro de digitação, link de uma
  // versão futura: a tela padrão assume. O nome inventado continua na barra de
  // endereços, e é de propósito: reescrevê-lo custaria um efeito que lê e grava
  // o mesmo estado, e o link segue funcionando exatamente igual do jeito que é.
  const route = $derived(
    ROUTES.find((r) => r.id === rota.id) ?? ROUTES.find((r) => r.id === ROTA_PADRAO),
  );

  // Clicar na aba em que já se está não pode reiniciar a tela. Antes o href era
  // igual ao hash e o navegador nem disparava evento; agora o hash carrega o
  // estado (`#rec?cam=x&t=…`), e o mesmo clique viraria uma navegação para o
  // `#rec` pelado - jogando fora justamente o que a URL passou a guardar.
  function navegar(ev, id) {
    if (id === route.id) ev.preventDefault();
  }

  onMount(async () => {
    setUnauthorizedHandler(() => {
      session.authenticated = false;
    });
    // Fora do await da sessão: a tela de login também mostra a versão, e não
    // há motivo para uma busca esperar a outra.
    loadBuild();
    await checkSession();
    if (session.authenticated) loadCameras();
  });

  async function afterLogin() {
    session.authenticated = true;
    await loadCameras();
  }

  let saindo = $state(false);

  async function sair() {
    saindo = true;
    await logout();
    saindo = false;
  }
</script>

{#if !session.checked}
  <div class="boot">carregando…</div>
{:else if session.authRequired && !session.authenticated}
  <Login onSuccess={afterLogin} />
{:else}
  <header class:autenticado={session.authRequired}>
    <span class="brand">
      <!-- O mesmo arquivo do favicon, servido de public/: uma marca só, um
           lugar só para mudar. -->
      <img class="mark" src="/favicon.svg" alt="" width="24" height="24" />
      dwnvr
    </span>
    <nav class="top">
      {#each ROUTES as r (r.id)}
        <a href="#{r.id}" class:active={r.id === route.id} onclick={(e) => navegar(e, r.id)}>
          {r.label}
        </a>
      {/each}
    </nav>
    <!-- Sem autenticação configurada não há sessão para encerrar, e um botão
         que não faz nada é pior que botão nenhum. -->
    {#if session.authRequired}
      <button class="ghost sair" onclick={sair} disabled={saindo}>
        {saindo ? 'saindo…' : 'Sair'}
      </button>
    {/if}
  </header>

  <main>
    <!-- A chave força a remontagem ao trocar de tela: cada uma tem recursos
         pesados (conexões de live, MediaSource) que precisam ser liberados,
         e depender de limpeza manual seria fonte garantida de vazamento.

         É o hash inteiro, e não só a rota, porque cada tela lê o seu estado da
         URL uma vez, ao montar. `rota.hash` só muda em navegação de verdade -
         voltar, avançar, URL colada à mão -, e é aí que remontar é o certo: as
         escritas da própria tela usam replaceState, que não dispara
         hashchange, então elas não remontam nada. -->
    {#key rota.hash}
      <route.component />
    {/key}
  </main>

  <nav class="bottom">
    {#each ROUTES as r (r.id)}
      <a href="#{r.id}" class:active={r.id === route.id} onclick={(e) => navegar(e, r.id)}>
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

  /* No celular a barra existe só para hospedar o Sair: a navegação mora
     embaixo, e cada pixel de altura faz falta na grade ao vivo. Por isso as
     propriedades ficam todas aqui e só o `display` muda entre os estados. */
  header {
    display: none;
    align-items: center;
    gap: 12px;
    padding: 6px 12px;
    background: var(--panel);
    border-bottom: 1px solid var(--line);
    position: sticky;
    top: 0;
    z-index: 20;
  }

  header.autenticado {
    display: flex;
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 8px;
    font-weight: 700;
    letter-spacing: 0.3px;
  }

  /* O SVG já traz o próprio arredondamento, então nada de border-radius
     aqui - dobrar o raio deformaria os cantos. */
  .mark {
    display: block;
    flex: none;
  }

  nav.top {
    display: none;
  }

  /* Empurrado para a ponta oposta da marca, longe do polegar que navega pelo
     rodapé: sair por engano custa digitar a senha de novo. */
  .sair {
    margin-left: auto;
    min-height: 34px;
    padding: 5px 12px;
    font-size: 13px;
    color: var(--dim);
  }

  .sair:hover:not(:disabled) {
    color: var(--fg);
  }

  main {
    /* Espaço para a navegação inferior não cobrir o conteúdo. */
    padding-bottom: var(--nav-h);
    /* Ocupa o que sobra da coluna de 100dvh do #app, em vez de pedir a altura
       cheia da janela e empurrar o documento para além dela. */
    flex: 1;
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
    /* Aqui a barra vale para todo mundo: ela também carrega a navegação, que
       no desktop não tem onde mais ficar. */
    header {
      display: flex;
      gap: 20px;
      padding: 10px 18px;
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
    main { padding-bottom: 0; }
  }
</style>
