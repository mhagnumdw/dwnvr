<!--
  Usuários: quem entra no dwnvr além do dono. Sem SMTP, o admin cria a pessoa,
  o dwnvr gera um link, e ela mesma define a senha por ele. O mesmo link serve
  para "esqueci a senha" e "perdi o celular": gerar outro apaga a senha atual.
  Só o admin chega aqui; a API recusa o comum com 403.
-->
<script>
  import { onMount } from 'svelte';
  import { api } from '../lib/api.js';
  import { session } from '../lib/state.svelte.js';
  import { copiar } from '../lib/navegador.js';
  import Modal from '../components/Modal.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import Avatar from '../components/Avatar.svelte';

  // { authRequired, dono, usuarios, validadeDoLinkMs, tamanhoMaximoDoNome,
  // tamanhoMaximoDoUsuario }, da API.
  let dados = $state(null);
  let erro = $state('');

  // O link recém-gerado: { usuario, nome, url, venceEmMs }. Ver mostrarLink.
  let gerado = $state(null);

  // O relógio da contagem dos links, num tique de um segundo. Só anda com um
  // link aberto na tela.
  let agora = $state(Date.now());
  const temLinkAberto = $derived(
    gerado || dados?.usuarios.some((u) => u.situacao === 'linkAberto'),
  );
  $effect(() => {
    if (!temLinkAberto) return;
    const id = setInterval(() => (agora = Date.now()), 1000);
    return () => clearInterval(id);
  });

  async function carregar() {
    try {
      dados = await api.usuarios();
      erro = '';
    } catch (e) {
      erro = e.message;
    }
  }

  onMount(carregar);

  // ---- o link recém-gerado -------------------------------------------------
  //
  // É o único lugar onde o link existe: o servidor guarda só o hash do token.
  // Sai do endereço pelo qual o admin está acessando, que é o que a pessoa
  // também alcança na maioria dos casos (o mesmo nome no Tailscale, o mesmo IP
  // na rede de casa).

  let copiado = $state('');

  function mostrarLink(r) {
    gerado = {
      usuario: r.usuario.usuario,
      nome: r.usuario.nome,
      url: `${location.origin}${location.pathname}#convite?token=${r.token}`,
      venceEmMs: r.usuario.linkVenceEmMs,
    };
    copiado = '';
  }

  // O Compartilhar abre o menu do sistema (e-mail, WhatsApp). Só existe onde o
  // navegador oferece, e só em HTTPS; no resto fica o Copiar, que funciona
  // também em HTTP.
  const podeCompartilhar = typeof navigator.share === 'function';

  async function aoCopiar() {
    copiado = (await copiar(gerado.url)) ? 'copiado' : 'não deu para copiar';
  }

  async function compartilhar() {
    try {
      await navigator.share({ title: 'dwnvr', text: `Link para ${gerado.nome} definir a senha no dwnvr`, url: gerado.url });
    } catch {
      // Cancelar o menu do sistema também cai aqui. Nada a fazer.
    }
  }

  // "9:41", até o link vencer.
  function falta(ms) {
    const s = Math.max(0, Math.ceil((ms - agora) / 1000));
    return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
  }

  // ---- criar ----------------------------------------------------------------

  let novo = $state(null); // { usuario, nome } com o formulário aberto
  let salvando = $state(false);
  let erroNovo = $state('');

  function abrirNovo() {
    novo = { usuario: '', nome: '' };
    erroNovo = '';
  }

  async function criar(e) {
    e.preventDefault();
    salvando = true;
    erroNovo = '';
    try {
      mostrarLink(await api.criarUsuario(novo.usuario, novo.nome));
      novo = null;
      await carregar();
    } catch (err) {
      erroNovo = err.message;
    } finally {
      salvando = false;
    }
  }

  // ---- o ⋮ de cada pessoa ----------------------------------------------------

  let menu = $state(null); // o usuário com o menu aberto
  let confirmando = $state(null); // { acao: 'link' | 'foto' | 'remover', u }
  let erroAcao = $state('');

  function foraDoMenu(e) {
    if (menu && !e.target.closest?.('.menu, .mais')) menu = null;
  }

  function pedir(acao, u) {
    menu = null;
    erroAcao = '';
    // Link novo para quem ainda não tem senha não derruba ninguém: só troca
    // um link por outro, e dispensa a confirmação.
    if (acao === 'link' && u.situacao !== 'ativo') executar(acao, u);
    else confirmando = { acao, u };
  }

  async function executar(acao, u) {
    confirmando = null;
    try {
      if (acao === 'link') mostrarLink(await api.novoLink(u.usuario));
      else if (acao === 'foto') await api.tirarAvatarDe(u.usuario);
      else {
        await api.removerUsuario(u.usuario);
        if (gerado?.usuario === u.usuario) gerado = null;
      }
    } catch (e) {
      erroAcao = e.message;
    }
    await carregar();
  }

  const voce = $derived(session.pessoa?.usuario);
  const minutos = $derived(dados ? Math.round(dados.validadeDoLinkMs / 60000) : 0);
</script>

<svelte:window onclick={foraDoMenu} onkeydown={(e) => e.key === 'Escape' && (menu = null)} />

<div class="page">
  {#if erro}
    <p class="card small erro">Não deu para ler os usuários: {erro}</p>
  {:else if dados && !dados.authRequired}
    <p class="card small">
      A autenticação está desligada, e sem ela não há usuários: quem alcança o dwnvr entra direto. Para
      ligar, defina <code>server.username</code> e <code>server.password</code> no
      <code>dwnvr.yaml</code> e reinicie o dwnvr. Essa é a sua conta, a de administrador; as outras
      pessoas você cria aqui.
    </p>
  {:else if dados}
    <div class="row">
      <strong>Usuários</strong>
      <span class="spacer"></span>
      <button class="primary" onclick={abrirNovo}>+ Novo usuário</button>
    </div>

    {#if gerado}
      <div class="card link-novo">
        <div class="row wrap">
          <strong>Link de {gerado.nome}</strong>
          <span class="spacer"></span>
          {#if gerado.venceEmMs > agora}
            <span class="chip mono"><span class="dot warn"></span>vence em {falta(gerado.venceEmMs)}</span>
          {:else}
            <span class="chip"><span class="dot bad"></span>vencido</span>
          {/if}
        </div>
        <p class="small muted">
          Mande para {gerado.nome} abrir agora: vale uma vez só, e não aparece de novo depois que você
          sair desta tela.
        </p>
        <div class="url small">{gerado.url}</div>
        <div class="row wrap">
          <button onclick={aoCopiar}>Copiar</button>
          {#if podeCompartilhar}<button onclick={compartilhar}>Compartilhar</button>{/if}
          <span class="small muted">{copiado}</span>
        </div>
      </div>
    {/if}

    {#if erroAcao}<p class="card small erro">{erroAcao}</p>{/if}

    <div class="card lista">
      {#if dados.dono}
        <div class="linha">
          <Avatar pessoa={dados.dono} tamanho={36} />
          <div class="quem">
            <div class="nome">
              <strong>{dados.dono.nome}</strong>
              {#if voce === dados.dono.usuario}<span class="muted small">você</span>{/if}
            </div>
            <div class="muted small">@{dados.dono.usuario} · administrador</div>
          </div>
          <span class="muted small">do dwnvr.yaml</span>
        </div>
      {/if}
      {#each dados.usuarios as u (u.usuario)}
        <div class="linha">
          <Avatar pessoa={u} tamanho={36} />
          <div class="quem">
            <div class="nome"><strong>{u.nome}</strong></div>
            <div class="muted small">@{u.usuario}{u.papel === 'admin' ? ' · administrador' : ''}</div>
          </div>
          {#if u.situacao === 'ativo'}
            <span class="chip"><span class="dot ok"></span>ativo</span>
          {:else if u.situacao === 'linkAberto' && u.linkVenceEmMs > agora}
            <span class="chip mono"><span class="dot warn"></span>link vence em {falta(u.linkVenceEmMs)}</span>
          {:else}
            <span class="chip"><span class="dot bad"></span>link vencido</span>
          {/if}
          <button
            class="ghost mais"
            onclick={() => (menu = menu === u.usuario ? null : u.usuario)}
            aria-label="opções de {u.nome}"
            aria-haspopup="menu"
            aria-expanded={menu === u.usuario}><span aria-hidden="true">⋮</span></button
          >
          {#if menu === u.usuario}
            <div class="menu" role="menu">
              <button class="ghost" role="menuitem" onclick={() => pedir('link', u)}>Gerar link novo</button>
              <!-- O admin não troca a foto de ninguém; só tira uma que não sirva. -->
              {#if u.avatar}
                <button class="ghost" role="menuitem" onclick={() => pedir('foto', u)}>Tirar a foto</button>
              {/if}
              <button class="ghost perigo" role="menuitem" onclick={() => pedir('remover', u)}>Remover</button>
            </div>
          {/if}
        </div>
      {:else}
        <p class="muted small vazio">Ninguém além de você ainda. Crie uma pessoa para mandar o link.</p>
      {/each}
    </div>
  {/if}
</div>

{#if novo}
  <Modal onclose={() => (novo = null)}>
    <form class="fields" onsubmit={criar}>
      <h3>Novo usuário</h3>
      <label>
        Nome, como aparece na tela
        <input bind:value={novo.nome} required maxlength={dados.tamanhoMaximoDoNome} />
      </label>
      <label>
        Usuário, para entrar: letras minúsculas, números, ponto, _ e -
        <input
          bind:value={novo.usuario}
          required
          maxlength={dados.tamanhoMaximoDoUsuario}
          pattern="[a-z0-9._\-]+"
          autocapitalize="none"
          autocorrect="off"
          spellcheck="false"
        />
      </label>
      <p class="small muted">
        O dwnvr gera um link que vale {minutos} minutos, para a pessoa definir a própria senha. Ela vê
        o ao vivo, as gravações e as detecções; o cadastro de câmeras, os usuários e o diagnóstico do
        servidor ficam só com você.
      </p>
      {#if erroNovo}<p class="small erro">{erroNovo}</p>{/if}
      <div class="row">
        <span class="spacer"></span>
        <button type="button" class="ghost" onclick={() => (novo = null)}>cancelar</button>
        <button class="primary" type="submit" disabled={salvando}>
          {salvando ? 'criando…' : 'criar e gerar o link'}
        </button>
      </div>
    </form>
  </Modal>
{/if}

{#if confirmando?.acao === 'link'}
  <ConfirmDialog
    title="Gerar link novo para {confirmando.u.nome}?"
    confirmLabel="gerar link novo"
    danger
    onconfirm={() => executar('link', confirmando.u)}
    oncancel={() => (confirmando = null)}
  >
    A senha atual de {confirmando.u.nome} deixa de valer agora, e os aparelhos em que ela entrou saem na
    hora. Ela volta a entrar definindo uma senha nova pelo link.
  </ConfirmDialog>
{:else if confirmando?.acao === 'foto'}
  <ConfirmDialog
    title="Tirar a foto de {confirmando.u.nome}?"
    confirmLabel="tirar a foto"
    danger
    onconfirm={() => executar('foto', confirmando.u)}
    oncancel={() => (confirmando = null)}
  >
    A foto é apagada, e no lugar dela ficam as iniciais. {confirmando.u.nome} pode pôr outra pela Minha
    conta.
  </ConfirmDialog>
{:else if confirmando?.acao === 'remover'}
  <ConfirmDialog
    title="Remover {confirmando.u.nome}?"
    confirmLabel="remover"
    danger
    onconfirm={() => executar('remover', confirmando.u)}
    oncancel={() => (confirmando = null)}
  >
    {confirmando.u.nome} sai na hora de todos os aparelhos, e o usuário <strong>@{confirmando.u.usuario}</strong> fica
    livre. As gravações não mudam.
  </ConfirmDialog>
{/if}

<style>
  .page {
    display: grid;
    gap: 10px;
    padding: 10px;
    max-width: 900px;
    margin: 0 auto;
  }

  p {
    margin: 0;
  }

  .erro {
    color: var(--bad);
  }

  /* A borda de destaque é o --accent translúcido, a mesma da pílula de versão. */
  .link-novo {
    display: grid;
    gap: 8px;
    border-color: rgba(47, 129, 247, 0.55);
  }

  .link-novo button {
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

  .lista {
    padding: 2px 14px;
  }

  .linha {
    position: relative;
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 0;
    border-bottom: 1px solid var(--line);
  }

  .linha:last-child {
    border-bottom: 0;
  }

  .linha .chip {
    flex: none;
  }

  .quem {
    flex: 1;
    min-width: 0;
    line-height: 1.3;
  }

  .nome {
    display: flex;
    align-items: baseline;
    gap: 6px;
    min-width: 0;
  }

  .nome strong {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .vazio {
    padding: 12px 0;
  }

  .mais {
    flex: none;
    width: 36px;
    min-height: 36px;
    padding: 0;
    font-weight: 700;
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

  .menu .perigo {
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
</style>
