<script lang="ts">
  import { onMount } from 'svelte';
  import type { Component } from 'svelte';
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
  } from './lib/state.svelte.ts';
  import { rota, ROTA_PADRAO, corrigirRota, linkDaTela, cena } from './lib/rota.svelte.ts';
  import { appInstalado, copiar } from './lib/navegador.ts';
  import { telaNova, vigiarVersaoDaTela } from './lib/versao-tela.svelte.ts';
  import { setUnauthorizedHandler } from './lib/api.ts';
  import type { Pessoa } from './lib/api.ts';
  import { avisosDe, resumoDosAvisos } from './lib/avisos.ts';
  import Login from './routes/Login.svelte';
  import Live from './routes/Live.svelte';
  import Recordings from './routes/Recordings.svelte';
  import Cameras from './routes/Cameras.svelte';
  import Health from './routes/Health.svelte';
  import Avatar from './components/Avatar.svelte';
  import type TelaDoConvite from './routes/Convite.svelte';

  interface Rota {
    id: string;
    label: string;
    icon?: string;
    // A tela que já vem no bundle principal, ou o import do chunk à parte dela.
    component?: Component;
    carregar?: () => Promise<{ default: Component }>;
    soComDetector?: boolean;
    soAdmin?: boolean;
    semAba?: boolean;
  }

  // Lista que nunca é vazia: a primeira é a padrão.
  const ROUTES: readonly [Rota, ...Rota[]] = [
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
    // As duas do admin. Esconder a aba não é segurança - a API recusa o viewer
    // com 403 -, é só não mostrar o que ele não pode usar. Usuários vem em
    // chunk à parte: o viewer nunca baixa a tela.
    { id: 'cams', label: 'Câmeras', icon: '☰', component: Cameras, soAdmin: true },
    {
      id: 'usuarios',
      label: 'Usuários',
      carregar: () => import('./routes/Usuarios.svelte'),
      soAdmin: true,
    },
    // Para o viewer, só "Este navegador" e a versão.
    { id: 'health', label: 'Diagnóstico', icon: '♥', component: Health },
    // Sem aba: chega-se pelo avatar do header. Só existe com login, porque sem
    // ele não há de quem ser a conta.
    {
      id: 'conta',
      label: 'Minha conta',
      carregar: () => import('./routes/Conta.svelte'),
      semAba: true,
    },
  ];

  const podeVer = (r: Rota) =>
    (!r.soComDetector || cameras.detector) &&
    (!r.soAdmin || ehAdmin()) &&
    (r.id !== 'conta' || session.authRequired);
  const abas = $derived(ROUTES.filter((r) => !r.semAba && podeVer(r)));

  // O link de convite (`#convite?token=…`) abre a tela de definir a senha, com
  // ou sem sessão: quem o recebe ainda não tem senha. Chunk à parte, porque só
  // quem abre um link a usa.
  const convite = $derived(rota.id === 'convite');
  let Convite = $state<typeof TelaDoConvite | null>(null);
  $effect(() => {
    if (convite && !Convite) {
      void import('./routes/Convite.svelte').then((m) => {
        Convite = m.default;
      });
    }
  });

  // A aba que depende do detector só se decide depois do /api/cameras. Até lá
  // a tela fica em branco: cair na padrão e trocar em seguida montaria e
  // desmontaria o ao vivo, com as conexões dele, à toa.
  const pedida = $derived(ROUTES.find((r) => r.id === rota.id));
  const esperando = $derived(pedida?.soComDetector && cameras.detector === null);

  // Roteamento por hash: são poucas telas, e um roteador de verdade custaria
  // mais bytes que o resto do aplicativo junto. Quem lê e escreve o hash é o
  // `lib/rota.svelte.ts`, porque ele carrega também o estado de cada tela.
  // Hash apontando para tela que não existe ou que a pessoa não pode ver -
  // erro de digitação, link de uma versão futura, `#detection` num servidor
  // sem detector, `#usuarios` aberto pelo viewer: a tela padrão assume.
  const route = $derived(
    ROUTES.find((r) => r.id === rota.id && podeVer(r)) ??
      ROUTES.find((r) => r.id === ROTA_PADRAO) ??
      ROUTES[0],
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
  let carregadas = $state<Record<string, Component>>({});
  const Tela = $derived(esperando ? null : (route.component ?? carregadas[route.id]));

  // As que não baixaram. Sem isto a falha deixava a tela em branco, sem dizer
  // nada. Sair e voltar para a tela tenta de novo.
  let falharam = $state<Record<string, boolean>>({});

  $effect(() => {
    if (esperando) return;
    const r = route;
    if (r.carregar && !carregadas[r.id]) {
      r.carregar().then(
        (m) => {
          carregadas[r.id] = m.default;
          falharam[r.id] = false;
        },
        () => {
          falharam[r.id] = true;
        },
      );
    }
  });

  $effect(() => vigiarVersaoDaTela());

  const logado = $derived(session.checked && (!session.authRequired || session.authenticated));

  // A barra de endereços diz a tela que está aberta. Quando a padrão assume
  // (ver route), o estado que ela escreve iria parar debaixo do nome pedido
  // (`#usuarios?cams=…`). Só depois de a sessão e o detector se decidirem:
  // antes, até o admin parece um viewer, e perderia o `#usuarios` do recarregar.
  $effect(() => {
    if (logado && !convite && !esperando && route.id !== rota.id) corrigirRota(route.id);
  });

  // O sino do header: os mesmos avisos do card da tela Diagnóstico, contados
  // pela mesma função. Só vigia com a sessão aberta - sem ela não há header -,
  // e só para o admin: o viewer não tem sino, e a tela dele não relê a saúde.
  $effect(() => {
    if (!logado || !ehAdmin()) return;
    return vigiarAvisos();
  });
  const sino = $derived(resumoDosAvisos(avisosDe(health)));
  const sinoTitulo = $derived(
    `${sino.total} ${sino.total === 1 ? 'aviso' : 'avisos'} no Diagnóstico`,
  );

  // Clicar na aba em que já se está não pode reiniciar a tela. Antes o href era
  // igual ao hash e o navegador nem disparava evento; agora o hash carrega o
  // estado (`#rec?cam=x&t=…`), e o mesmo clique viraria uma navegação para o
  // `#rec` pelado - jogando fora justamente o que a URL passou a guardar.
  function navegar(ev: MouseEvent, id: string) {
    if (id === route.id) ev.preventDefault();
  }

  // Compartilhar a tela, só no app instalado: no navegador a barra de endereços
  // já leva a cena, e no app ela não existe. O menu do sistema (WhatsApp,
  // e-mail) onde houver - pede HTTPS -; no resto, copiar o link.
  const instalado = appInstalado();
  let copiouTela = $state('');
  let timerCopiou: number | undefined;

  // A cena em palavras vai no título - o assunto do e-mail - e no texto, que o
  // WhatsApp manda junto do link. Tela sem cena fica com o título da aba.
  // Copiar leva só o link: quem cola espera um link.
  //
  // Com a cena, o link vai dentro do texto, numa linha própria, e não no campo
  // `url`: o Chrome do Android junta texto e url com um espaço, e o link
  // grudava no fim da frase. O WhatsApp acha o link no texto e monta a prévia
  // do mesmo jeito.
  async function compartilharTela() {
    const url = linkDaTela();
    if (typeof navigator.share === 'function') {
      const dados: ShareData = cena.texto
        ? { title: `dwnvr · ${cena.texto}`, text: `${cena.texto}\n${url}` }
        : { title: titulo, url };
      try {
        await navigator.share(dados);
      } catch {
        // Cancelar o menu do sistema também cai aqui. Nada a fazer.
      }
      return;
    }
    copiouTela = (await copiar(url)) ? 'link copiado' : 'não deu para copiar';
    clearTimeout(timerCopiou);
    timerCopiou = setTimeout(() => (copiouTela = ''), 2000);
  }

  // A versão em uso, buscada no boot. A pergunta ao GitHub por versão nova
  // espera por ela e pela sessão: só o admin vê a pílula, e o viewer não gasta
  // a consulta.
  let versao: Promise<void> | undefined;

  function talvezVersaoNova() {
    if (ehAdmin()) void versao?.then(checarNovaVersao);
  }

  onMount(async () => {
    setUnauthorizedHandler(sessaoCaiu);
    // Fora do await da sessão: a tela de login também mostra a versão, e não
    // há motivo para uma busca esperar a outra.
    versao = loadBuild();
    await checkSession();
    if (session.authenticated) {
      void loadCameras();
      talvezVersaoNova();
    }
  });

  async function afterLogin(pessoa: Pessoa) {
    session.authenticated = true;
    session.pessoa = pessoa;
    talvezVersaoNova();
    await loadCameras();
  }

  // Definida a senha pelo link, a pessoa já entra. O link sai do histórico:
  // voltar não pode reabrir uma tela com o token, que já não vale.
  function aposConvite(pessoa: Pessoa) {
    session.checked = true;
    location.replace(location.pathname + location.search + '#' + ROTA_PADRAO);
    void afterLogin(pessoa);
  }

  // O menu do avatar: Minha conta e Sair, para os dois papéis. O Sair fica a
  // um toque a mais, o que dificulta sair por engano.
  let menuConta = $state(false);

  function foraDoMenu(e: MouseEvent) {
    if (menuConta && !(e.target instanceof Element && e.target.closest('.conta')))
      menuConta = false;
  }

  let saindo = $state(false);

  async function sair() {
    saindo = true;
    await logout();
    saindo = false;
    menuConta = false;
  }

  const papel = $derived(session.pessoa?.role === 'admin' ? 'administrador' : 'usuário');
</script>

<svelte:head>
  <title>{titulo}</title>
</svelte:head>

<svelte:window
  onclick={foraDoMenu}
  onkeydown={(e: KeyboardEvent) => {
    if (e.key === 'Escape') menuConta = false;
  }}
/>

{#if convite}
  {#if Convite}<Convite onSuccess={aposConvite} />{:else}<div class="boot">carregando…</div>{/if}
{:else if !session.checked}
  <div class="boot">carregando…</div>
{:else if session.authRequired && !session.authenticated}
  <Login onSuccess={afterLogin} />
{:else}
  <!-- A faixa de versão nova desce junto com o header. Quem mede a altura dele
       para se encostar embaixo (a tela Detecções) mede este bloco. -->
  <div class="topo">
    <header>
      <span class="brand">
        <!-- O mesmo arquivo do favicon, servido de public/: uma marca só, um
           lugar só para mudar. -->
        <img class="mark" src="/favicon.svg" alt="" width="24" height="24" />
        dwnvr
      </span>
      <nav class="top">
        {#each abas as r (r.id)}
          <a
            href="#{r.id}"
            class:active={r.id === route.id}
            onclick={(e) => {
              navegar(e, r.id);
            }}
          >
            {r.label}
          </a>
        {/each}
      </nav>
      <div class="acoes">
        {#if instalado}
          <div class="compartilhar">
            <button
              class="icone"
              onclick={compartilharTela}
              title="Compartilhar esta tela"
              aria-label="Compartilhar esta tela"
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
                <circle cx="18" cy="5" r="3" />
                <circle cx="6" cy="12" r="3" />
                <circle cx="18" cy="19" r="3" />
                <path d="m8.59 13.51 6.83 3.98" />
                <path d="m15.41 6.51-6.82 3.98" />
              </svg>
            </button>
            {#if copiouTela}<span class="copiou" role="status">{copiouTela}</span>{/if}
          </div>
        {/if}
        <!-- Some sem aviso: um sino apagado sempre à vista ensinaria a não
           olhar para ele. A cor da bolha é a do pior aviso. -->
        {#if sino.total}
          <a
            class="sino {sino.nivel}"
            href="#health"
            title={sinoTitulo}
            aria-label={sinoTitulo}
            onclick={(e) => {
              navegar(e, 'health');
            }}
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
        <!-- Sem autenticação configurada não há de quem ser a conta nem sessão
           para encerrar, e um botão que não faz nada é pior que botão nenhum.
           O anel marca o menu aberto, e a Minha conta, que não tem aba. -->
        {#if session.authRequired && session.pessoa}
          <div class="conta">
            <button
              class="avbtn"
              class:ativo={menuConta || route.id === 'conta'}
              onclick={() => (menuConta = !menuConta)}
              aria-haspopup="menu"
              aria-expanded={menuConta}
              aria-label="conta de {session.pessoa.name}"
              title={session.pessoa.name}
            >
              <Avatar pessoa={session.pessoa} tamanho={32} />
            </button>
            {#if menuConta}
              <div class="menu-conta" role="menu">
                <div class="quem">
                  <Avatar pessoa={session.pessoa} tamanho={40} />
                  <div>
                    <strong>{session.pessoa.name}</strong>
                    <span class="muted small">@{session.pessoa.username} · {papel}</span>
                  </div>
                </div>
                <div class="sep"></div>
                <a
                  href="#conta"
                  role="menuitem"
                  class:atual={route.id === 'conta'}
                  onclick={(e) => {
                    navegar(e, 'conta');
                    menuConta = false;
                  }}>Minha conta</a
                >
                <div class="sep"></div>
                <button role="menuitem" onclick={sair} disabled={saindo}>
                  {saindo ? 'saindo…' : 'Sair'}
                </button>
              </div>
            {/if}
          </div>
        {/if}
      </div>
    </header>
    <!-- Sem botão de fechar: adiar a recarga só adia o chunk que não baixa. -->
    {#if telaNova.disponivel}
      <div class="faixa" role="status">
        <span class="txt">
          O dwnvr foi atualizado. <span class="muted">Recarregue para usar a versão nova.</span>
        </span>
        <button class="primary" onclick={() => location.reload()}>Recarregar</button>
      </div>
    {/if}
  </div>

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
      {#if Tela}
        <Tela />
      {:else if route.carregar && falharam[route.id]}
        <div class="falhou">
          <div class="ico" aria-hidden="true">↻</div>
          <h3>Não deu para abrir esta tela</h3>
          <p>
            {#if telaNova.disponivel}
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

  <nav class="bottom" style:--abas={abas.length}>
    {#each abas as r (r.id)}
      <a
        href="#{r.id}"
        class:active={r.id === route.id}
        onclick={(e) => {
          navegar(e, r.id);
        }}
      >
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
  }

  .topo {
    position: sticky;
    top: 0;
    z-index: 20;
  }

  /* O azul da pílula de release, só que opaco: a faixa fica sobre o vídeo
     quando a página rola. */
  .faixa {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 6px 6px 12px;
    background: #14233b;
    border-bottom: 1px solid rgba(47, 129, 247, 0.55);
    font-size: 14px;
  }

  .faixa .txt {
    flex: 1;
    min-width: 0;
    line-height: 1.3;
  }

  /* 34px, como a pílula e o avatar: os 44px do botão comum engordariam a
     faixa. */
  .faixa button {
    min-height: 34px;
    padding: 5px 14px;
    font-size: 14px;
  }

  .falhou {
    max-width: 420px;
    margin: 48px auto;
    padding: 24px 18px;
    display: grid;
    gap: 10px;
    justify-items: center;
    text-align: center;
  }

  .falhou .ico {
    font-size: 28px;
    line-height: 1;
    color: var(--accent);
  }

  .falhou h3 {
    margin: 0;
    font-size: 16px;
  }

  .falhou p {
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

  /* O SVG já traz o próprio arredondamento, então nada de border-radius
     aqui - dobrar o raio deformaria os cantos. */
  .mark {
    display: block;
    flex: none;
  }

  nav.top {
    display: none;
  }

  /* Empurradas para a ponta oposta da marca. O avatar, que leva ao Sair,
     longe do polegar que navega pelo rodapé: sair por engano custa digitar a
     senha de novo. */
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
    /* A mesma altura do avatar ao lado. */
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

  /* Sino e não pílula: é o menor dos alertas, 34px como a pílula e o avatar ao
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
  .sino.warn {
    color: var(--dim);
  }

  .sino svg {
    width: 21px;
    height: 21px;
  }

  @media (hover: hover) {
    .sino:hover {
      background: var(--panel-2);
    }
  }

  /* O compartilhar tem o tamanho e o traço do sino, mas não chama atenção:
     fica na cor do texto apagado. */
  .compartilhar {
    position: relative;
  }

  .icone {
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

  .icone svg {
    width: 21px;
    height: 21px;
  }

  @media (hover: hover) {
    .icone:hover {
      background: var(--panel-2);
      color: var(--fg);
    }
  }

  /* Abre sob o ícone, como o menu do avatar. */
  .copiou {
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

  .sino.bad .bolha {
    background: var(--bad);
    color: #fff;
  }
  .sino.warn .bolha {
    background: var(--warn);
    color: #1b1300;
  }

  /* O menu se prende ao avatar, e não ao header: abre sob ele. */
  .conta {
    position: relative;
  }

  /* 34px, como o sino e a pílula ao lado. */
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

  .avbtn.ativo {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent);
  }

  /* O mesmo desenho do menu do ⋮ das telas Detecções e Usuários. */
  .menu-conta {
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

  .menu-conta .quem {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 10px;
  }

  .menu-conta .quem div {
    min-width: 0;
  }

  .menu-conta .quem strong,
  .menu-conta .quem span {
    display: block;
    line-height: 1.25;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .menu-conta .sep {
    height: 1px;
    margin: 4px 0;
    background: var(--line);
  }

  .menu-conta a,
  .menu-conta button {
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

  .menu-conta a.atual {
    color: var(--accent);
  }

  @media (hover: hover) {
    .menu-conta a:hover,
    .menu-conta button:hover:not(:disabled) {
      background: var(--panel-2);
    }
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

    .faixa {
      justify-content: center;
      padding: 6px 18px;
    }

    .faixa .txt {
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
