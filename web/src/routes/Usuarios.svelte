<!--
  Usuários: quem entra no dwnvr além do dono. Sem SMTP, o admin cria a pessoa,
  o dwnvr gera um link, e ela mesma define a senha por ele. O mesmo link serve
  para "esqueci a senha" e "perdi o celular": gerar outro apaga a senha atual.
  Só o admin chega aqui; a API recusa o viewer com 403.
-->
<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { api, ApiError, mensagemDe } from '../lib/api.ts';
  import type { CampoDaPessoa, Convite, Recusas, Usuario, Usuarios } from '../lib/api.ts';
  import { Conferencia, type EstadoDoCampo } from '../lib/conferencia.svelte.ts';
  import { session } from '../lib/state.svelte.ts';
  import { copiar } from '../lib/navegador.ts';
  import { dataHora, hojeOntem } from '../lib/format.ts';
  import Modal from '../components/Modal.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
  import Avatar from '../components/Avatar.svelte';
  import Icone from '../components/Icone.svelte';

  type Acao = 'link' | 'foto' | 'remover' | 'email';

  // Nulo até a API responder.
  let dados = $state<Usuarios | null>(null);
  let erro = $state('');

  // O link recém-gerado. Ver mostrarLink.
  let gerado = $state<{
    username: string;
    name: string;
    email: string; // vazio sem e-mail
    url: string;
    venceEmMs: number;
  } | null>(null);

  // O relógio da contagem dos links, num tique de um segundo. Só anda com um
  // link aberto na tela.
  let agora = $state(Date.now());
  const temLinkAberto = $derived(
    gerado !== null || (dados?.users.some((u) => u.status === 'invitePending') ?? false),
  );
  $effect(() => {
    if (!temLinkAberto) return;
    const id = setInterval(() => (agora = Date.now()), 1000);
    return () => {
      clearInterval(id);
    };
  });

  async function carregar() {
    try {
      dados = await api.usuarios();
      // O "hoje" e o "ontem" do último acesso contam daqui: sem link aberto,
      // o tique não anda.
      agora = Date.now();
      erro = '';
    } catch (e) {
      erro = mensagemDe(e);
    }
  }

  onMount(carregar);

  // Sair da tela com uma conferência na espera: nada mais vai ao servidor.
  onDestroy(() => {
    confNovo.parar();
    confEmail.parar();
  });

  // ---- o link recém-gerado -------------------------------------------------
  //
  // É o único lugar onde o link existe: o servidor guarda só o hash do token.
  // Sai do endereço pelo qual o admin está acessando, que é o que a pessoa
  // também alcança na maioria dos casos (o mesmo nome no Tailscale, o mesmo IP
  // na rede de casa).

  let copiado = $state('');

  function mostrarLink(r: Convite) {
    gerado = {
      username: r.user.username,
      name: r.user.name,
      email: r.user.email ?? '',
      url: `${location.origin}${location.pathname}#convite?token=${r.token}`,
      // O link novo sempre vem com o prazo; sem ele, conta como vencido.
      venceEmMs: r.user.inviteExpiresAtMs ?? 0,
    };
    copiado = '';
    envio = null;
  }

  // O link por e-mail: vai o mesmo link da tela, para o e-mail da pessoa. O
  // resultado fica no card, e mandar de novo é tocar de novo.
  let envio = $state<{ ok: boolean; texto: string } | null>(null);
  let enviando = $state(false);

  async function enviarPorEmail() {
    if (!gerado) return;
    enviando = true;
    envio = null;
    try {
      const r = await api.enviarLink(gerado.username, gerado.url);
      envio = { ok: true, texto: `enviado para ${r.sentTo}` };
    } catch (e) {
      envio = { ok: false, texto: mensagemDe(e) };
    } finally {
      enviando = false;
    }
  }

  // O Compartilhar abre o menu do sistema (e-mail, WhatsApp). Só existe onde o
  // navegador oferece, e só em HTTPS; no resto fica o Copiar, que funciona
  // também em HTTP.
  const podeCompartilhar = typeof navigator.share === 'function';

  async function aoCopiar() {
    if (!gerado) return;
    copiado = (await copiar(gerado.url)) ? 'copiado' : 'não deu para copiar';
  }

  async function compartilhar() {
    if (!gerado) return;
    try {
      await navigator.share({
        title: 'dwnvr',
        text: `Link para ${gerado.name} definir a senha no dwnvr`,
        url: gerado.url,
      });
    } catch {
      // Cancelar o menu do sistema também cai aqui. Nada a fazer.
    }
  }

  // "9:41", até o link vencer.
  function falta(ms: number) {
    const s = Math.max(0, Math.ceil((ms - agora) / 1000));
    return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
  }

  // ---- criar ----------------------------------------------------------------

  let novo = $state<{ username: string; name: string; email: string } | null>(null); // com o formulário aberto
  let salvando = $state(false);
  let erroNovo = $state('');
  // Se o admin já escreveu no usuário. Até lá, o usuário segue o nome (ver
  // sugerirUsuario); apagar tudo devolve a sugestão.
  let usuarioEscrito = false;
  let confNovo = $state.raw(new Conferencia(['username', 'name', 'email'], conferirNovo));
  const camposNovo: Partial<Record<CampoDaPessoa, HTMLInputElement>> = {};

  async function conferirNovo(): Promise<Recusas> {
    if (!novo) return {};
    return (await api.conferirNovo(novo.username, novo.name, novo.email)).fields;
  }

  function abrirNovo() {
    novo = { username: '', name: '', email: '' };
    erroNovo = '';
    usuarioEscrito = false;
    confNovo = new Conferencia(['username', 'name', 'email'], conferirNovo);
  }

  function fecharNovo() {
    confNovo.parar();
    novo = null;
  }

  // "João da Silva Santos" vira "joao.santos": o primeiro e o último nome, sem
  // acento, em minúsculas, só com o que o usuário aceita, cortado no tamanho
  // máximo.
  function sugerirUsuario(nome: string, max: number) {
    const partes = nome
      .normalize('NFD')
      .replace(/\p{M}/gu, '')
      .toLowerCase()
      .split(/[^a-z0-9]+/)
      .filter(Boolean);
    const escolhidas = partes.length > 1 ? [partes[0], partes[partes.length - 1]] : partes;
    return escolhidas
      .join('.')
      .slice(0, max)
      .replace(/[._-]+$/, '');
  }

  function aoDigitarNome() {
    if (novo && dados && !usuarioEscrito) {
      novo.username = sugerirUsuario(novo.name, dados.maxUsernameLength);
    }
    confNovo.digitar();
  }

  // Quem preencheu o usuário foi o nome, e o admin talvez nem passe por ele:
  // o usuário sugerido é conferido junto, para o "já existe" aparecer antes
  // do salvar.
  function aoSairDoNome() {
    if (!novo) return;
    confNovo.sair('name', novo.name);
    if (!usuarioEscrito) confNovo.sair('username', novo.username);
  }

  function aoDigitarUsuario() {
    usuarioEscrito = novo?.username !== '';
    confNovo.digitar();
  }

  // O primeiro campo recusado, na ordem da tela, recebe o foco.
  function focarRecusa(
    conf: Conferencia,
    campos: Partial<Record<CampoDaPessoa, HTMLInputElement>>,
    ordem: CampoDaPessoa[],
  ) {
    const campo = ordem.find((c) => conf.estado(c) === 'erro');
    if (campo) campos[campo]?.focus();
  }

  // A recusa do salvar é dos valores enviados. Ela vale só para o formulário
  // que a pediu (`conf`), se ele ainda está aberto; e, se os campos mudaram
  // enquanto a resposta vinha, eles são conferidos de novo, em vez de levarem
  // o vermelho de um valor que já não está lá.
  function aplicarRecusa(
    conf: Conferencia,
    aberta: Conferencia,
    recusas: Recusas,
    mudou: boolean,
    campos: Partial<Record<CampoDaPessoa, HTMLInputElement>>,
    ordem: CampoDaPessoa[],
  ) {
    if (conf !== aberta) return;
    conf.recusado(recusas);
    if (mudou) conf.digitar();
    else focarRecusa(conf, campos, ordem);
  }

  async function criar(e: SubmitEvent) {
    e.preventDefault();
    if (!novo) return;
    const conf = confNovo;
    const enviado = { ...novo };
    // Os campos mudaram (ou o formulário fechou) enquanto a resposta vinha.
    const mudou = () => JSON.stringify(novo) !== JSON.stringify(enviado);
    salvando = true;
    erroNovo = '';
    try {
      mostrarLink(await api.criarUsuario(enviado.username, enviado.name, enviado.email));
      if (conf === confNovo) fecharNovo();
      await carregar();
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.campos).length > 0) {
        aplicarRecusa(conf, confNovo, err.campos, mudou(), camposNovo, [
          'name',
          'username',
          'email',
        ]);
      } else if (conf === confNovo) {
        erroNovo = mensagemDe(err);
      }
    } finally {
      salvando = false;
    }
  }

  // ---- o ⋮ de cada pessoa ----------------------------------------------------

  let menu = $state<string | null>(null); // o usuário com o menu aberto
  let confirmando = $state<{ acao: Acao; u: Usuario } | null>(null);
  let erroAcao = $state('');

  function foraDoMenu(e: MouseEvent) {
    if (menu && !(e.target instanceof Element && e.target.closest('.menu, .mais'))) menu = null;
  }

  function pedir(acao: Acao, u: Usuario) {
    menu = null;
    erroAcao = '';
    if (acao === 'email') {
      editandoEmail = { u, email: u.email ?? '' };
      erroEmail = '';
      confEmail = new Conferencia(['email'], conferirEmail);
      return;
    }
    // Link novo para quem ainda não tem senha não derruba ninguém: só troca
    // um link por outro, e dispensa a confirmação.
    if (acao === 'link' && u.status !== 'active') void executar(acao, u);
    else confirmando = { acao, u };
  }

  async function executar(acao: Acao, u: Usuario) {
    confirmando = null;
    try {
      if (acao === 'link') mostrarLink(await api.novoLink(u.username));
      else if (acao === 'foto') await api.tirarAvatarDe(u.username);
      else if (acao === 'remover') {
        await api.removerUsuario(u.username);
        if (gerado?.username === u.username) gerado = null;
      }
    } catch (e) {
      erroAcao = mensagemDe(e);
    }
    await carregar();
  }

  // ---- o e-mail de quem já existe ------------------------------------------

  let editandoEmail = $state<{ u: Usuario; email: string } | null>(null);
  let salvandoEmail = $state(false);
  let erroEmail = $state('');
  let confEmail = $state.raw(new Conferencia(['email'], conferirEmail));
  const camposEmail: Partial<Record<CampoDaPessoa, HTMLInputElement>> = {};

  async function conferirEmail(): Promise<Recusas> {
    if (!editandoEmail) return {};
    return (await api.conferirEmail(editandoEmail.u.username, editandoEmail.email)).fields;
  }

  function fecharEmail() {
    confEmail.parar();
    editandoEmail = null;
  }

  async function salvarEmail(e: SubmitEvent) {
    e.preventDefault();
    if (!editandoEmail) return;
    const conf = confEmail;
    const enviado = editandoEmail.email;
    // Numa closure, o TypeScript não supõe que o formulário segue aberto.
    const mudou = () => editandoEmail?.email !== enviado;
    salvandoEmail = true;
    erroEmail = '';
    try {
      const { user } = await api.trocarEmail(editandoEmail.u.username, enviado);
      // O link aberto no card passa a ir para o e-mail novo.
      if (gerado?.username === user.username) gerado.email = user.email ?? '';
      if (conf === confEmail) fecharEmail();
      await carregar();
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.campos).length > 0) {
        aplicarRecusa(conf, confEmail, err.campos, mudou(), camposEmail, ['email']);
      } else if (conf === confEmail) {
        erroEmail = mensagemDe(err);
      }
    } finally {
      salvandoEmail = false;
    }
  }

  // O sinal à direita do campo. O verde só aparece com algo escrito: no
  // e-mail, que é opcional, um "certo" no campo vazio não diria nada. `soErro`
  // é o campo que só avisa o que está errado, como o nome.
  function sinal(
    conf: Conferencia,
    campo: CampoDaPessoa,
    valor: string,
    soErro = false,
  ): EstadoDoCampo {
    const estado = conf.estado(campo);
    if (estado === 'ok' && (soErro || valor.trim() === '')) return null;
    return estado;
  }

  const voce = $derived(session.pessoa?.username);
  const minutos = $derived(dados ? Math.round(dados.inviteValidityMs / 60000) : 0);
</script>

<svelte:window
  onclick={foraDoMenu}
  onkeydown={(e: KeyboardEvent) => {
    if (e.key === 'Escape') menu = null;
  }}
/>

<div class="page">
  {#if erro}
    <p class="card small erro">Não deu para ler os usuários: {erro}</p>
  {:else if dados && !dados.authRequired}
    <p class="card small">
      A autenticação está desligada, e sem ela não há usuários: quem alcança o dwnvr entra direto.
      Para ligar, defina <code>server.username</code> e <code>server.password</code> no
      <code>dwnvr.yaml</code> e reinicie o dwnvr. Essa é a sua conta, a de administrador; as outras pessoas
      você cria aqui.
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
          <strong>Link de {gerado.name}</strong>
          <span class="spacer"></span>
          {#if gerado.venceEmMs > agora}
            <span class="chip mono"
              ><span class="dot warn"></span>vence em {falta(gerado.venceEmMs)}</span
            >
          {:else}
            <span class="chip"><span class="dot bad"></span>vencido</span>
          {/if}
        </div>
        <p class="small muted">
          Mande para {gerado.name} abrir agora: vale uma vez só, e não aparece de novo depois que você
          sair desta tela.
        </p>
        <div class="url small">{gerado.url}</div>
        <div class="row wrap">
          <button onclick={aoCopiar}>Copiar</button>
          {#if podeCompartilhar}<button onclick={compartilhar}>Compartilhar</button>{/if}
          {#if dados.emailEnabled && gerado.email && gerado.venceEmMs > agora}
            <button onclick={enviarPorEmail} disabled={enviando}
              >{enviando ? 'enviando…' : 'Enviar por e-mail'}</button
            >
          {/if}
          <span class="small muted">{copiado}</span>
          {#if envio?.ok}<span class="small muted">{envio.texto}</span>{/if}
        </div>
        {#if envio && !envio.ok}<p class="small erro">{envio.texto}</p>{/if}
      </div>
    {/if}

    {#if erroAcao}<p class="card small erro">{erroAcao}</p>{/if}

    <div class="card lista">
      {#if dados.owner}
        <div class="linha">
          <Avatar pessoa={dados.owner} tamanho={36} />
          <div class="quem">
            <div class="nome">
              <strong>{dados.owner.name}</strong>
              {#if voce === dados.owner.username}<span class="muted small">você</span>{/if}
            </div>
            <div class="muted small">@{dados.owner.username} · administrador</div>
            {#if voce !== dados.owner.username && dados.owner.lastAccessMs}
              <div class="muted small">
                último acesso <span class="quando"
                  >{hojeOntem(dados.owner.lastAccessMs, agora)}</span
                >
              </div>
            {/if}
          </div>
          {#if voce === dados.owner.username}
            <a class="small" href="#conta">Minha conta</a>
          {:else}
            <span class="muted small">do dwnvr.yaml</span>
          {/if}
        </div>
      {/if}
      {#each dados.users as u (u.username)}
        <div class="linha">
          <Avatar pessoa={u} tamanho={36} />
          <!-- O nome e o chip: no celular, um embaixo do outro; de 720 px para
               cima, lado a lado (ver .meio). -->
          <div class="meio">
            <div class="quem">
              <div class="nome"><strong>{u.name}</strong></div>
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
                  ativo desde <span class="quando">{dataHora(u.activeSinceMs)}</span>
                </div>
              {:else if u.status !== 'active' && u.lastAccessMs}
                <div class="muted small">
                  último acesso <span class="quando">{hojeOntem(u.lastAccessMs, agora)}</span>
                </div>
              {/if}
            </div>
            {#if u.status === 'active'}
              <!-- O ponto verde diz que está ativo. Sem acesso registrado é
                   quem não usou o dwnvr desde que o último acesso passou a ser
                   anotado. -->
              <span class="chip quebra">
                <span class="dot ok"></span>
                {#if u.lastAccessMs}
                  <span
                    >último acesso <span class="quando">{hojeOntem(u.lastAccessMs, agora)}</span
                    ></span
                  >
                {:else}
                  <span>nenhum acesso registrado</span>
                {/if}
              </span>
            {:else if u.status === 'invitePending' && u.inviteExpiresAtMs !== undefined && u.inviteExpiresAtMs > agora}
              <span class="chip mono"
                ><span class="dot warn"></span>link vence em {falta(u.inviteExpiresAtMs)}</span
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
            <Icone nome="menu" tamanho={18} />
          </button>
          {#if menu === u.username}
            <div class="menu" role="menu">
              <!-- Para quem está ativo, o link novo apaga a senha: o rótulo diz
                   isso. Para quem não definiu, não há senha a redefinir. -->
              <button
                class="ghost"
                role="menuitem"
                onclick={() => {
                  pedir('link', u);
                }}
              >
                {u.status === 'active' ? 'Redefinir senha' : 'Gerar link novo'}
              </button>
              <button
                class="ghost"
                role="menuitem"
                onclick={() => {
                  pedir('email', u);
                }}>{u.email ? 'Alterar o e-mail' : 'Cadastrar o e-mail'}</button
              >
              <!-- O admin não troca a foto de ninguém; só tira uma que não sirva. -->
              {#if u.avatar}
                <button
                  class="ghost"
                  role="menuitem"
                  onclick={() => {
                    pedir('foto', u);
                  }}>Remover a foto</button
                >
              {/if}
              <button
                class="ghost perigo"
                role="menuitem"
                onclick={() => {
                  pedir('remover', u);
                }}>Remover</button
              >
            </div>
          {/if}
        </div>
      {:else}
        <p class="muted small vazio">
          Ninguém além de você ainda. Crie uma pessoa para mandar o link.
        </p>
      {/each}
    </div>
  {/if}
</div>

<!-- O e-mail serve para entrar mesmo sem a seção smtp; só o envio do link
     depende dela, e a tela avisa isso onde o e-mail é escrito. -->
{#snippet semEnvio()}
  <p class="small muted sem-envio">
    <Icone nome="info" tamanho={14} />
    O envio de e-mail não está configurado neste dwnvr: o link não vai por e-mail, só copiado.
  </p>
{/snippet}

<!-- O sinal à direita de um campo conferido pelo servidor (ver
     lib/conferencia.svelte.ts). O texto da recusa vai embaixo, no `recusa`. -->
{#snippet sinalDoCampo(estado: EstadoDoCampo)}
  {#if estado}
    <span class="sinal {estado}"
      ><Icone nome={estado === 'ok' ? 'marcado' : 'fechar'} tamanho={18} /></span
    >
  {/if}
{/snippet}

<!-- A linha da recusa existe sempre, vazia ou não: o espaço já está guardado,
     e a mensagem aparece e some sem empurrar o resto do formulário. -->
{#snippet recusa(id: string, texto: string)}
  <p class="small erro recusa" {id}>{texto}</p>
{/snippet}

<!-- novalidate: a recusa do navegador é um balão com outro desenho e outro
     texto; aqui todas vêm do servidor e ficam embaixo do campo. -->
{#if novo && dados}
  {@const sNome = sinal(confNovo, 'name', novo.name, true)}
  {@const sUsuario = sinal(confNovo, 'username', novo.username)}
  {@const sEmail = sinal(confNovo, 'email', novo.email)}
  <Modal onclose={fecharNovo}>
    <form class="fields" onsubmit={criar} novalidate>
      <h3>Novo usuário</h3>
      <div class="grupos">
        <div class="grupo">
          <label>
            Nome, como aparece na tela
            <span class="campo">
              <input
                bind:this={camposNovo.name}
                bind:value={novo.name}
                oninput={aoDigitarNome}
                onblur={aoSairDoNome}
                required
                maxlength={dados.maxNameLength}
                placeholder="João Silva"
                aria-invalid={sNome === 'erro'}
                aria-describedby={sNome === 'erro' ? 'recusa-nome' : undefined}
              />
              {@render sinalDoCampo(sNome)}
            </span>
          </label>
          {@render recusa('recusa-nome', sNome === 'erro' ? confNovo.mensagem('name') : '')}
        </div>
        <div class="grupo">
          <label>
            Usuário, para entrar: letras minúsculas, números, ponto, _ e -
            <span class="campo">
              <input
                bind:this={camposNovo.username}
                bind:value={novo.username}
                oninput={aoDigitarUsuario}
                onblur={() => {
                  if (novo) confNovo.sair('username', novo.username);
                }}
                required
                maxlength={dados.maxUsernameLength}
                placeholder="joao.silva"
                autocapitalize="none"
                autocorrect="off"
                spellcheck="false"
                aria-invalid={sUsuario === 'erro'}
                aria-describedby={sUsuario === 'erro' ? 'recusa-usuario' : undefined}
              />
              {@render sinalDoCampo(sUsuario)}
            </span>
          </label>
          {@render recusa(
            'recusa-usuario',
            sUsuario === 'erro' ? confNovo.mensagem('username') : '',
          )}
        </div>
        <div class="grupo">
          <label>
            {dados.emailEnabled
              ? 'E-mail, para entrar e receber o link (opcional)'
              : 'E-mail, para entrar (opcional)'}
            <span class="campo">
              <input
                bind:this={camposNovo.email}
                inputmode="email"
                bind:value={novo.email}
                oninput={() => {
                  confNovo.digitar();
                }}
                onblur={() => {
                  if (novo) confNovo.sair('email', novo.email);
                }}
                maxlength={dados.maxEmailLength}
                placeholder="joao@exemplo.com"
                autocapitalize="none"
                autocorrect="off"
                spellcheck="false"
                aria-invalid={sEmail === 'erro'}
                aria-describedby={sEmail === 'erro' ? 'recusa-email' : undefined}
              />
              {@render sinalDoCampo(sEmail)}
            </span>
          </label>
          {@render recusa('recusa-email', sEmail === 'erro' ? confNovo.mensagem('email') : '')}
        </div>
      </div>
      {#if !dados.emailEnabled}{@render semEnvio()}{/if}
      <p class="small muted">
        O dwnvr gera um link que vale {minutos} minutos, para a pessoa definir a própria senha. Ela vê
        o ao vivo, as gravações e as detecções; o cadastro de câmeras, os usuários e o diagnóstico do
        servidor ficam só com você.
      </p>
      {#if erroNovo}<p class="small erro">{erroNovo}</p>{/if}
      <div class="row">
        <span class="spacer"></span>
        <button type="button" class="ghost" onclick={fecharNovo}>cancelar</button>
        <button class="primary" type="submit" disabled={salvando}>
          {salvando ? 'criando…' : 'criar e gerar o link'}
        </button>
      </div>
    </form>
  </Modal>
{/if}

{#if editandoEmail && dados}
  {@const alvo = editandoEmail.u}
  {@const sEmail = sinal(confEmail, 'email', editandoEmail.email)}
  <Modal onclose={fecharEmail}>
    <form class="fields" onsubmit={salvarEmail} novalidate>
      <h3>E-mail de {alvo.name}</h3>
      <div class="grupo">
        <label>
          {dados.emailEnabled
            ? 'Para entrar e receber o link de definir a senha'
            : 'Para entrar, no lugar do usuário'}
          <span class="campo">
            <input
              bind:this={camposEmail.email}
              inputmode="email"
              bind:value={editandoEmail.email}
              oninput={() => {
                confEmail.digitar();
              }}
              onblur={() => {
                if (editandoEmail) confEmail.sair('email', editandoEmail.email);
              }}
              maxlength={dados.maxEmailLength}
              placeholder="joao@exemplo.com"
              autocapitalize="none"
              autocorrect="off"
              spellcheck="false"
              aria-invalid={sEmail === 'erro'}
              aria-describedby={sEmail === 'erro' ? 'recusa-email-de' : undefined}
            />
            {@render sinalDoCampo(sEmail)}
          </span>
        </label>
        {@render recusa('recusa-email-de', sEmail === 'erro' ? confEmail.mensagem('email') : '')}
      </div>
      <p class="small muted">Vazio apaga o e-mail. A senha e o link de {alvo.name} não mudam.</p>
      {#if !dados.emailEnabled}{@render semEnvio()}{/if}
      {#if erroEmail}<p class="small erro">{erroEmail}</p>{/if}
      <div class="row">
        <span class="spacer"></span>
        <button type="button" class="ghost" onclick={fecharEmail}>cancelar</button>
        <button class="primary" type="submit" disabled={salvandoEmail}>
          {salvandoEmail ? 'salvando…' : 'salvar'}
        </button>
      </div>
    </form>
  </Modal>
{/if}

{#if confirmando?.acao === 'link'}
  {@const alvo = confirmando.u}
  <ConfirmDialog
    title="Redefinir a senha de {alvo.name}?"
    confirmLabel="redefinir senha"
    danger
    onconfirm={() => {
      void executar('link', alvo);
    }}
    oncancel={() => (confirmando = null)}
  >
    A senha atual de {alvo.name} deixa de valer agora, e os aparelhos em que ela entrou saem na hora.
    O dwnvr gera um link novo, e ela volta a entrar definindo uma senha nova por ele.
  </ConfirmDialog>
{:else if confirmando?.acao === 'foto'}
  {@const alvo = confirmando.u}
  <ConfirmDialog
    title="Remover a foto de {alvo.name}?"
    confirmLabel="remover a foto"
    danger
    onconfirm={() => {
      void executar('foto', alvo);
    }}
    oncancel={() => (confirmando = null)}
  >
    A foto é apagada, e no lugar dela ficam as iniciais. {alvo.name} pode pôr outra pela Minha conta.
  </ConfirmDialog>
{:else if confirmando?.acao === 'remover'}
  {@const alvo = confirmando.u}
  <ConfirmDialog
    title="Remover {alvo.name}?"
    confirmLabel="remover"
    danger
    onconfirm={() => {
      void executar('remover', alvo);
    }}
    oncancel={() => (confirmando = null)}
  >
    {alvo.name} sai na hora de todos os aparelhos, e o usuário <strong>@{alvo.username}</strong> fica
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

  .erro {
    color: var(--bad);
  }

  /* O ícone fica centrado na primeira linha quando o texto quebra: 13px com o
     line-height 1.5 do body dão 19,5px de linha, e o ícone tem 14. */
  .sem-envio {
    display: flex;
    gap: 6px;
    align-items: flex-start;
  }
  .sem-envio :global(svg) {
    flex: none;
    margin-top: 3px;
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

  /* No celular, o chip é a quarta linha, embaixo do nome, do @usuário e da
     data: ao lado deles, disputava a largura com o nome e quebrava em duas
     linhas. De 720 px para cima há largura, e ele volta para a direita. O
     avatar e o ⋮ ficam no meio da linha nos dois casos. */
  .meio {
    flex: 1;
    min-width: 0;
  }

  .meio > .chip {
    display: flex;
    width: fit-content;
    margin-top: 4px;
  }

  @media (min-width: 720px) {
    .meio {
      display: flex;
      align-items: center;
      gap: 12px;
    }

    .meio > .chip {
      flex: none;
      margin-top: 0;
    }
  }

  /* Em 320 px, nem na linha própria cabe "último acesso ontem 22:47": o chip
     quebra antes da data, e o raio deixa de ser de pílula. */
  .chip.quebra {
    white-space: normal;
    border-radius: 12px;
  }

  .quando {
    white-space: nowrap;
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

  .nome strong,
  .login {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .vazio {
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

  /* O campo e a linha da recusa embaixo dele, mais perto um do outro que
     dos outros campos. A linha guardada (ver o snippet recusa) ocupa o vão
     entre um campo e o próximo: os grupos ficam a 10 px, e não aos 14 do
     formulário, para o vão vazio não crescer tanto. Menos que 10 deixa a
     mensagem colada no rótulo de baixo, como se fosse dele. */
  .grupos {
    display: grid;
    gap: 10px;
  }

  .grupo {
    display: grid;
    gap: 4px;
  }

  /* Uma linha do .small (13 px com o line-height 1.5 do body). A mensagem
     que quebra em duas, no celular, ainda empurra uma linha. */
  .recusa {
    min-height: 19.5px;
  }

  /* O sinal fica dentro da borda, à direita, e o texto não passa por baixo
     dele: 12 px de margem + 18 do ícone + 8 de folga. */
  .campo {
    position: relative;
    display: block;
  }

  .campo input {
    padding-right: 38px;
  }

  .sinal {
    position: absolute;
    top: 50%;
    right: 12px;
    transform: translateY(-50%);
    pointer-events: none;
  }

  .sinal.ok {
    color: var(--ok);
  }

  .sinal.erro {
    color: var(--bad);
  }

  .campo input[aria-invalid='true'] {
    border-color: var(--bad);
  }
</style>
