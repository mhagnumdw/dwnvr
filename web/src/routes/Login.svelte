<script lang="ts">
  import { api, mensagemDe, MuitasTentativas } from '../lib/api.ts';
  import type { Pessoa } from '../lib/api.ts';
  import { build, REPO_URL } from '../lib/state.svelte.ts';
  import MarcaGitHub from '../components/MarcaGitHub.svelte';
  import CampoSenha from '../components/CampoSenha.svelte';

  interface Props {
    onSuccess: (pessoa: Pessoa) => void;
  }

  let { onSuccess }: Props = $props();

  let username = $state('');
  let password = $state('');
  let error = $state('');
  let busy = $state(false);

  // O 429: passado o limite de tentativas, a linha do erro conta até a
  // próxima, e o botão fica desligado até lá. O relógio só anda enquanto a
  // contagem corre.
  let liberaEm = $state(0);
  let agora = $state(Date.now());
  const espera = $derived(Math.max(0, Math.ceil((liberaEm - agora) / 1000)));

  $effect(() => {
    if (!liberaEm) return;
    const id = setInterval(() => {
      agora = Date.now();
      if (agora >= liberaEm) clearInterval(id);
    }, 250);
    return () => clearInterval(id);
  });

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    busy = true;
    error = '';
    try {
      const r = await api.login(username, password);
      onSuccess(r.user);
    } catch (err) {
      if (err instanceof MuitasTentativas) {
        // A senha fica: o servidor nem a conferiu, e ao fim da contagem basta
        // tocar em Entrar de novo.
        agora = Date.now();
        liberaEm = agora + err.segundos * 1000;
        return;
      }
      error = mensagemDe(err);
      password = '';
    } finally {
      busy = false;
    }
  }
</script>

<div class="screen">
  <form class="card" onsubmit={submit}>
    <h1>
      <!-- Mesma marca do header e do favicon, servida de public/. -->
      <img class="mark" src="/favicon.svg" alt="" width="32" height="32" />
      dwnvr
    </h1>
    <p class="muted small">Entre para ver as câmeras</p>

    <input
      bind:value={username}
      placeholder="usuário ou e-mail"
      autocomplete="username"
      autocapitalize="none"
      autocorrect="off"
      required
    />
    <CampoSenha bind:value={password} />

    <button class="primary" type="submit" disabled={busy || espera > 0}>
      {busy ? 'entrando…' : 'Entrar'}
    </button>

    <!-- Espaço reservado sempre: sem isto o formulário salta quando o erro
         aparece, e o botão foge do dedo no meio do toque. -->
    <p class="error small">
      {espera > 0 ? `Muitas tentativas. Tente de novo em ${espera} s.` : error}
    </p>

    <!-- Não há e-mail para recuperar a senha: o remédio é o admin gerar um
         link de convite novo, e quem esqueceu precisa saber disso daqui. -->
    <p class="muted small">Esqueceu a senha? Peça um link novo a quem cuida do dwnvr.</p>
  </form>

  <!-- Antes de entrar já dá para saber qual dwnvr é este - útil quando há um
       de teste e um de verdade na mesma rede - e de onde ele vem. -->
  <p class="small ver">
    {#if build.version}<span class="mono">{build.version}</span> ·{/if}
    <a href={REPO_URL} target="_blank" rel="noopener"><MarcaGitHub tamanho={13} />GitHub</a>
  </p>
</div>

<style>
  .screen {
    display: grid;
    place-items: center;
    /* align-content, e não só place-items: com duas linhas, place-items
       centraliza cada uma DENTRO da sua faixa, e as faixas esticam para
       preencher a tela - o que jogaria a versão para o meio do vazio. */
    align-content: center;
    gap: 16px;
    min-height: 100dvh;
    padding: 20px;
  }

  .ver {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--dim);
  }

  /* Discreto como a versão ao lado: é rodapé, não chamada. */
  .ver a {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: inherit;
  }

  .ver a:hover {
    color: var(--fg);
  }

  form {
    width: 100%;
    max-width: 320px;
    display: grid;
    gap: 12px;
  }

  h1 {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: 0;
    font-size: 22px;
  }

  /* O SVG já traz o próprio arredondamento; nada de border-radius aqui. */
  .mark {
    display: block;
    flex: none;
  }

  p {
    margin: 0;
  }

  input {
    width: 100%;
  }

  .error {
    color: var(--bad);
    min-height: 1.2em;
  }
</style>
