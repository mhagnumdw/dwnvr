<script>
  import { onMount } from 'svelte';
  import {
    session,
    cameras,
    health,
    build,
    checkSession,
    loadCameras,
    loadBuild,
    checarNovaVersao,
    vigiarAvisos,
    logout,
    ehAdmin,
    sessaoCaiu,
    RELEASES_URL,
  } from './lib/state.svelte.js';
  import { rota, ROTA_PADRAO } from './lib/rota.svelte.js';
  import { setUnauthorizedHandler } from './lib/api.js';
  import { avisosDe, resumoDosAvisos } from './lib/avisos.js';
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
    // As duas do admin. Esconder a aba não é segurança - a API recusa o comum
    // com 403 -, é só não mostrar o que ele não pode usar. Usuários vem em
    // chunk à parte: o comum nunca baixa a tela.
    { id: 'cams', label: 'Câmeras', icon: '☰', component: Cameras, soAdmin: true },
    {
      id: 'usuarios',
      label: 'Usuários',
      carregar: () => import('./routes/Usuarios.svelte'),
      soAdmin: true,
    },
    // Para o comum, só "Este navegador" e a versão.
    { id: 'health', label: 'Diagnóstico', icon: '♥', component: Health },
  ];

  const abas = $derived(
    ROUTES.filter((r) => (!r.soComDetector || cameras.detector) && (!r.soAdmin || ehAdmin())),
  );

  // O link de convite (`#convite?token=…`) abre a tela de definir a senha, com
  // ou sem sessão: quem o recebe ainda não tem senha. Chunk à parte, porque só
  // quem abre um link a usa.
  const convite = $derived(rota.id === 'convite');
  let Convite = $state(null);
  $effect(() => {
    if (convite && !Convite) import('./routes/Convite.svelte').then((m) => (Convite = m.default));
  });

  // A aba que depende do detector só se decide depois do /api/cameras. Até lá
  // a tela fica em branco: cair na padrão e trocar em seguida montaria e
  // desmontaria o ao vivo, com as conexões dele, à toa.
  const pedida = $derived(ROUTES.find((r) => r.id === rota.id));
  const esperando = $derived(pedida?.soComDetector && cameras.detector === null);

  // Roteamento por hash: são poucas telas, e um roteador de verdade custaria
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

  // A tela no título, para a aba e o histórico dizerem onde se está. No login
  // e enquanto a tela não se decide fica só a marca: um "Ao vivo" de passagem
  // antes de "Detecções" seria mentira por um instante.
  const titulo = $derived(
    convite || !session.checked || (session.authRequired && !session.authenticated) || esperando
      ? 'dwnvr'
      : `dwnvr · ${route.label}`,
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

  // O sino do header: os mesmos avisos do card da tela Diagnóstico, contados
  // pela mesma função. Só vigia com a sessão aberta - sem ela não há header -,
  // e só para o admin: o comum não tem sino, e a tela dele não relê a saúde.
  const logado = $derived(session.checked && (!session.authRequired || session.authenticated));
  $effect(() => {
    if (logado && ehAdmin()) return vigiarAvisos();
  });
  const sino = $derived(resumoDosAvisos(avisosDe(health)));
  const sinoTitulo = $derived(
    `${sino.total} ${sino.total === 1 ? 'aviso' : 'avisos'} no Diagnóstico`,
  );

  // Clicar na aba em que já se está não pode reiniciar a tela. Antes o href era
  // igual ao hash e o navegador nem disparava evento; agora o hash carrega o
  // estado (`#rec?cam=x&t=…`), e o mesmo clique viraria uma navegação para o
  // `#rec` pelado - jogando fora justamente o que a URL passou a guardar.
  function navegar(ev, id) {
    if (id === route.id) ev.preventDefault();
  }

  // A versão em uso, buscada no boot. A pergunta ao GitHub por versão nova
  // espera por ela e pela sessão: só o admin vê a pílula, e o comum não gasta
  // a consulta.
  let versao;

  function talvezVersaoNova() {
    if (ehAdmin()) versao.then(checarNovaVersao);
  }

  onMount(async () => {
    setUnauthorizedHandler(sessaoCaiu);
    // Fora do await da sessão: a tela de login também mostra a versão, e não
    // há motivo para uma busca esperar a outra.
    versao = loadBuild();
    await checkSession();
    if (session.authenticated) {
      loadCameras();
      talvezVersaoNova();
    }
  });

  async function afterLogin(pessoa) {
    session.authenticated = true;
    session.pessoa = pessoa ?? null;
    talvezVersaoNova();
    await loadCameras();
  }

  // Definida a senha pelo link, a pessoa já entra. O link sai do histórico:
  // voltar não pode reabrir uma tela com o token, que já não vale.
  function aposConvite(pessoa) {
    session.checked = true;
    location.replace(location.pathname + location.search + '#' + ROTA_PADRAO);
    afterLogin(pessoa);
  }

  let saindo = $state(false);

  async function sair() {
    saindo = true;
    await logout();
    saindo = false;
  }
</script>

<svelte:head>
  <title>{titulo}</title>
</svelte:head>

{#if convite}
  {#if Convite}<Convite onSuccess={aposConvite} />{:else}<div class="boot">carregando…</div>{/if}
{:else if !session.checked}
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
      <!-- Some sem aviso: um sino apagado sempre à vista ensinaria a não
           olhar para ele. A cor da bolha é a do pior aviso. -->
      {#if sino.total}
        <a
          class="sino {sino.nivel}"
          href="#health"
          title={sinoTitulo}
          aria-label={sinoTitulo}
          onclick={(e) => navegar(e, 'health')}
        >
          <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path d="M6 9a6 6 0 0 1 12 0c0 5 2 6.5 2 6.5H4S6 14 6 9" />
            <path d="M10.3 19.5a1.9 1.9 0 0 0 3.4 0" />
          </svg>
          <span class="bolha">{sino.total}</span>
        </a>
      {/if}
      <!-- Leva às releases, e não direto à versão nova: quem pulou algumas
           precisa ler as notas de todas desde a sua. -->
      {#if build.nova && ehAdmin()}
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
        <!-- Usuários não tem um caractere que preste em todo sistema: vai
             desenhado, no tamanho dos outros. -->
        <span class="icon">
          {#if r.id === 'usuarios'}
            <svg
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              aria-hidden="true"
            >
              <circle cx="9" cy="8" r="3.5" />
              <path d="M2.5 20c.8-4 3.3-6 6.5-6s5.7 2 6.5 6" />
              <circle cx="17" cy="9" r="2.6" />
              <path d="M16 14.2c2.9-.3 5 1.6 5.6 4.8" />
            </svg>
          {:else}
            {r.icon}
          {/if}
        </span>
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

  /* Sino e não pílula: é o menor dos alertas, 34px como a pílula e o Sair ao
     lado, e o desenho já diz "tem notificação" sem texto. O desenho é SVG
     inline, e não emoji: o emoji muda a cada sistema e não aceita cor. */
  .sino {
    position: relative;
    display: inline-grid;
    place-items: center;
    width: 34px;
    height: 34px;
    border-radius: 999px;
    color: var(--fg);
  }

  /* Só amarelos: o sino recua, e só a bolha chama. */
  .sino.warn { color: var(--dim); }

  .sino svg {
    width: 21px;
    height: 21px;
  }

  @media (hover: hover) {
    .sino:hover { background: var(--panel-2); }
  }

  /* A borda da cor do header separa a bolha do desenho do sino. */
  .bolha {
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

  .sino.bad .bolha { background: var(--bad); color: #fff; }
  .sino.warn .bolha { background: var(--warn); color: #1b1300; }

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
    /* A altura fixa alinha o desenho da aba Usuários com os caracteres. */
    height: 18px;
    display: grid;
    place-items: center;
  }

  .icon svg {
    width: 18px;
    height: 18px;
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
