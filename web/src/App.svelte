<script>
  import { onMount } from 'svelte';
  import {
    session,
    cameras,
    build,
    checkSession,
    loadCameras,
    loadBuild,
    checarNovaVersao,
    logout,
    RELEASES_URL,
  } from './lib/state.svelte.js';
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
    // Chunk à parte: só quem abre a tela paga os bytes dela. E só existe com o
    // detector de objetos configurado - sem ele não há detecção para listar.
    {
      id: 'detection',
      label: 'Detecções',
      icon: '▣',
      carregar: () => import('./routes/Deteccoes.svelte'),
      soComDetector: true,
    },
    { id: 'cams', label: 'Câmeras', icon: '☰', component: Cameras },
    { id: 'health', label: 'Diagnóstico', icon: '♥', component: Health },
  ];

  const abas = $derived(ROUTES.filter((r) => !r.soComDetector || cameras.detector));

  // A aba que depende do detector só se decide depois do /api/cameras. Até lá
  // a tela fica em branco: cair na padrão e trocar em seguida montaria e
  // desmontaria o ao vivo, com as conexões dele, à toa.
  const pedida = $derived(ROUTES.find((r) => r.id === rota.id));
  const esperando = $derived(pedida?.soComDetector && cameras.detector === null);

  // Roteamento por hash: são cinco telas, e um roteador de verdade custaria
  // mais bytes que o resto do aplicativo junto. Quem lê e escreve o hash é o
  // `lib/rota.svelte.js`, porque ele carrega também o estado de cada tela.
  // Hash apontando para tela que não existe - erro de digitação, link de uma
  // versão futura, `#detection` num servidor sem detector: a tela padrão
  // assume. O nome inventado continua na barra de endereços, e é de propósito:
  // reescrevê-lo custaria um efeito que lê e grava o mesmo estado, e o link
  // segue funcionando exatamente igual do jeito que é.
  const route = $derived(
    abas.find((r) => r.id === rota.id) ?? ROUTES.find((r) => r.id === ROTA_PADRAO),
  );

  // As telas em chunk à parte, já baixadas: id -> componente.
  let carregadas = $state({});
  const Tela = $derived(esperando ? null : (route.component ?? carregadas[route.id]));

  $effect(() => {
    if (esperando) return;
    const r = route;
    if (r.carregar && !carregadas[r.id]) {
      r.carregar().then((m) => (carregadas[r.id] = m.default));
    }
  });

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
    loadBuild().then(checarNovaVersao);
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
  <header>
    <span class="brand">
      <!-- O mesmo arquivo do favicon, servido de public/: uma marca só, um
           lugar só para mudar. -->
      <img class="mark" src="/favicon.svg" alt="" width="24" height="24" />
      dwnvr
    </span>
    <nav class="top">
      {#each abas as r (r.id)}
        <a href="#{r.id}" class:active={r.id === route.id} onclick={(e) => navegar(e, r.id)}>
          {r.label}
        </a>
      {/each}
    </nav>
    <div class="acoes">
      <!-- Leva às releases, e não direto à versão nova: quem pulou algumas
           precisa ler as notas de todas desde a sua. -->
      {#if build.nova}
        <a
          class="nova"
          href={RELEASES_URL}
          target="_blank"
          rel="noopener"
          title="Versão nova do dwnvr: {build.nova} (você está na {build.version}). Veja o que mudou e como atualizar."
        >
          ↑ {build.nova}
        </a>
      {/if}
      <!-- Sem autenticação configurada não há sessão para encerrar, e um botão
           que não faz nada é pior que botão nenhum. -->
      {#if session.authRequired}
        <button class="ghost sair" onclick={sair} disabled={saindo}>
          {saindo ? 'saindo…' : 'Sair'}
        </button>
      {/if}
    </div>
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
      {#if Tela}<Tela />{/if}
    {/key}
  </main>

  <nav class="bottom" style:--abas={abas.length}>
    {#each abas as r (r.id)}
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

  /* A barra existe sempre, também no celular sem login, onde ela só tem a
     marca: é o mesmo canto para o aviso de versão nova em todo caso, e os
     ~37px que ela come da grade ao vivo quem usa com login já pagava. No
     celular a navegação mora embaixo. */
  header {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 6px 12px;
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
     aqui - dobrar o raio deformaria os cantos. */
  .mark {
    display: block;
    flex: none;
  }

  nav.top {
    display: none;
  }

  /* Empurradas para a ponta oposta da marca. O Sair, em especial, longe do
     polegar que navega pelo rodapé: sair por engano custa digitar a senha de
     novo. */
  .acoes {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 12px;
  }

  /* Pílula e não ponto: sem hover no celular, um ponto sozinho não diz o que
     é. O número já diz. O fundo e a borda são o --accent translúcido. */
  .nova {
    display: inline-flex;
    align-items: center;
    /* A mesma altura do Sair ao lado. */
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

  .nova:hover {
    background: rgba(47, 129, 247, 0.2);
  }

  .sair {
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
    font-size: 18px;
    line-height: 1;
  }

  /* No desktop a navegação sobe: o polegar deixa de ser a restrição e a
     altura da tela passa a ser o recurso escasso. */
  @media (min-width: 720px) {
    /* A navegação sobe para a barra, que ganha folga. */
    header {
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
