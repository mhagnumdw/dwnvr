<!--
  Minha conta: o que cada pessoa muda de si mesma, de qualquer papel. A foto e
  o nome valem para todos; a senha, só para quem não é o dono, cuja senha fica
  no dwnvr.yaml. Chega-se aqui pelo avatar do header: não tem aba.
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { api, mensagemDe } from '../lib/api.ts';
  import type { ComPessoa, Conta } from '../lib/api.ts';
  import { session } from '../lib/state.svelte.ts';
  import { reduzirFoto } from '../lib/avatar.ts';
  import Avatar from '../components/Avatar.svelte';
  import CampoSenha from '../components/CampoSenha.svelte';

  // Nulo até a API responder.
  let dados = $state<Conta | null>(null);
  let erro = $state('');

  const p = $derived(session.pessoa);

  onMount(async () => {
    try {
      dados = await api.conta();
      nome = dados.user.name ?? '';
    } catch (e) {
      erro = mensagemDe(e);
    }
  });

  // Toda resposta da conta traz a pessoa como ficou: o header se atualiza
  // junto, sem perguntar de novo.
  function atualizar(r: ComPessoa) {
    session.pessoa = r.user;
  }

  // ---- foto ----------------------------------------------------------------

  let escolher = $state<HTMLInputElement>(); // o <input type="file">, escondido atrás do botão
  let foto = $state({ ocupado: false, erro: '' });

  async function aoEscolher(e: Event & { currentTarget: HTMLInputElement }) {
    const arquivo = e.currentTarget.files?.[0];
    // Limpo já: escolher a mesma foto de novo tem de disparar o change.
    e.currentTarget.value = '';
    if (!arquivo || !dados) return;
    foto = { ocupado: true, erro: '' };
    try {
      atualizar(await api.enviarAvatar(await reduzirFoto(arquivo, dados.avatar)));
      foto.ocupado = false;
    } catch (err) {
      foto = { ocupado: false, erro: mensagemDe(err) };
    }
  }

  async function tirarFoto() {
    foto = { ocupado: true, erro: '' };
    try {
      atualizar(await api.tirarAvatar());
      foto.ocupado = false;
    } catch (err) {
      foto = { ocupado: false, erro: mensagemDe(err) };
    }
  }

  // ---- nome ----------------------------------------------------------------

  let nome = $state('');
  let estadoNome = $state({ ocupado: false, erro: '', ok: false });
  const nomeMudou = $derived(nome.trim() !== '' && nome.trim() !== p?.name);

  async function salvarNome(e: SubmitEvent) {
    e.preventDefault();
    estadoNome = { ocupado: true, erro: '', ok: false };
    try {
      const r = await api.mudarNome(nome);
      atualizar(r);
      nome = r.user.name ?? '';
      estadoNome = { ocupado: false, erro: '', ok: true };
    } catch (err) {
      estadoNome = { ocupado: false, erro: mensagemDe(err), ok: false };
    }
  }

  // ---- senha ---------------------------------------------------------------

  let atual = $state('');
  let nova = $state('');
  // A nova de novo: senha digitada no celular erra fácil, e o erro só
  // apareceria no próximo login. A conferência é só daqui; a API recebe uma.
  let repetida = $state('');
  let estadoSenha = $state({ ocupado: false, erro: '', ok: false });

  async function trocarSenha(e: SubmitEvent) {
    e.preventDefault();
    if (nova !== repetida) {
      estadoSenha = { ocupado: false, erro: 'a senha nova e a repetida não são iguais', ok: false };
      return;
    }
    estadoSenha = { ocupado: true, erro: '', ok: false };
    try {
      atualizar(await api.trocarSenha(atual, nova));
      atual = nova = repetida = '';
      estadoSenha = { ocupado: false, erro: '', ok: true };
    } catch (err) {
      estadoSenha = { ocupado: false, erro: mensagemDe(err), ok: false };
    }
  }

  const papel = $derived(p?.role === 'admin' ? 'administrador' : 'usuário');
</script>

<div class="page">
  {#if erro}
    <p class="card small erro">Não deu para ler a sua conta: {erro}</p>
  {:else if dados && p}
    <div class="card topo">
      <Avatar pessoa={p} tamanho={88} />
      <div class="quem">
        <strong>{p.name}</strong>
        <span class="muted small">@{p.username} · {papel}</span>
        <div class="row wrap botoes">
          <button onclick={() => { escolher?.click(); }} disabled={foto.ocupado}>
            {foto.ocupado ? 'salvando…' : p.avatar ? 'Trocar a foto' : 'Pôr uma foto'}
          </button>
          {#if p.avatar}
            <button class="ghost" onclick={tirarFoto} disabled={foto.ocupado}>Remover a foto</button>
          {/if}
        </div>
        {#if foto.erro}<span class="small erro">{foto.erro}</span>{/if}
      </div>
      <!-- Qualquer imagem que o aparelho abra. O corte e a redução são daqui,
           do navegador: a foto original não sai do aparelho. -->
      <input bind:this={escolher} type="file" accept="image/*" hidden onchange={aoEscolher} />
    </div>

    <form class="card form" onsubmit={salvarNome}>
      <strong>Nome</strong>
      <span class="small muted">Como você aparece na tela, para você e para quem administra.</span>
      <div class="row">
        <input
          bind:value={nome}
          required
          maxlength={dados.maxNameLength}
          placeholder="João Silva"
          aria-label="nome"
        />
        <button type="submit" disabled={!nomeMudou || estadoNome.ocupado}>
          {estadoNome.ocupado ? 'salvando…' : 'Salvar'}
        </button>
      </div>
      {#if estadoNome.erro}<span class="small erro">{estadoNome.erro}</span>{/if}
      {#if estadoNome.ok && !nomeMudou}<span class="small ok">nome salvo</span>{/if}
    </form>

    {#if p.owner}
      <div class="card form">
        <strong>Trocar senha</strong>
        <span class="small muted">
          A senha do administrador fica no <code>dwnvr.yaml</code> (<code>server.password</code>), e
          se troca lá.
        </span>
      </div>
    {:else}
      <form class="card form" onsubmit={trocarSenha}>
        <strong>Trocar senha</strong>
        <!-- O usuário, escondido, é para o gerenciador de senhas do navegador
             saber de quem é a senha nova. -->
        <input class="oculto" value={p.username} autocomplete="username" readonly tabindex="-1" aria-hidden="true" />
        <CampoSenha bind:value={atual} placeholder="senha atual" autocomplete="current-password" />
        <CampoSenha
          bind:value={nova}
          placeholder="senha nova, com {dados.minPasswordLength} caracteres ou mais"
          autocomplete="new-password"
          minlength={dados.minPasswordLength}
          maxlength={dados.maxPasswordLength}
        />
        <CampoSenha
          bind:value={repetida}
          placeholder="repita a senha nova"
          autocomplete="new-password"
          maxlength={dados.maxPasswordLength}
        />
        <button class="primary" type="submit" disabled={estadoSenha.ocupado}>
          {estadoSenha.ocupado ? 'trocando…' : 'Trocar senha'}
        </button>
        {#if estadoSenha.erro}<span class="small erro">{estadoSenha.erro}</span>{/if}
        {#if estadoSenha.ok}
          <span class="small ok">Senha trocada. Os outros aparelhos em que você entrou vão pedir a nova.</span>
        {:else}
          <span class="small muted">Os outros aparelhos em que você entrou vão pedir a senha nova.</span>
        {/if}
      </form>
    {/if}
  {/if}
</div>

<style>
  .page {
    display: grid;
    gap: 10px;
    padding: 10px;
    max-width: 560px;
    margin: 0 auto;
  }

  p {
    margin: 0;
  }

  .erro {
    color: var(--bad);
  }

  .ok {
    color: var(--ok);
  }

  .topo {
    display: flex;
    align-items: center;
    gap: 16px;
  }

  .quem {
    display: grid;
    gap: 2px;
    min-width: 0;
  }

  .quem strong {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .botoes {
    margin-top: 8px;
    gap: 8px;
  }

  .botoes button {
    min-height: 36px;
    padding: 5px 12px;
    font-size: 13px;
  }

  .form {
    position: relative;
    display: grid;
    gap: 10px;
  }

  .form input {
    width: 100%;
  }

  .form .row input {
    flex: 1;
    min-width: 0;
  }

  /* Fora da vista, mas no formulário: com display: none o gerenciador de
     senhas não o lê. Preso ao card, para não alargar a página no celular. */
  .form input.oculto {
    position: absolute;
    top: 0;
    left: 0;
    width: 1px;
    height: 1px;
    padding: 0;
    border: 0;
    opacity: 0;
    pointer-events: none;
  }
</style>
