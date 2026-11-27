<script lang="ts">
  import { api, messageOf, TooManyAttempts } from '../lib/api.ts';
  import type { Person } from '../lib/api.ts';
  import { build, REPO_URL } from '../lib/state.svelte.ts';
  import GitHubMark from '../components/GitHubMark.svelte';
  import PasswordField from '../components/PasswordField.svelte';

  interface Props {
    onSuccess: (person: Person) => void;
  }

  let { onSuccess }: Props = $props();

  let username = $state('');
  let password = $state('');
  let error = $state('');
  let busy = $state(false);

  // The 429: past the attempt limit, the error line counts down to the next
  // one, and the button stays disabled until then. The clock only runs while
  // the countdown is running.
  let unlocksAt = $state(0);
  let now = $state(Date.now());
  const wait = $derived(Math.max(0, Math.ceil((unlocksAt - now) / 1000)));

  $effect(() => {
    if (!unlocksAt) return;
    const id = setInterval(() => {
      now = Date.now();
      if (now >= unlocksAt) clearInterval(id);
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
      if (err instanceof TooManyAttempts) {
        // The password stays: the server did not even check it, and when the
        // countdown ends it is enough to tap "Entrar" again.
        now = Date.now();
        unlocksAt = now + err.seconds * 1000;
        return;
      }
      error = messageOf(err);
      password = '';
    } finally {
      busy = false;
    }
  }
</script>

<div class="screen">
  <form class="card" onsubmit={submit}>
    <h1>
      <!-- Same brand as the header and the favicon, served from public/. -->
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
    <PasswordField bind:value={password} />

    <button class="primary" type="submit" disabled={busy || wait > 0}>
      {busy ? 'entrando…' : 'Entrar'}
    </button>

    <!-- Space always reserved: without this the form jumps when the error
         appears, and the button runs away from the finger mid-tap. -->
    <p class="error small">
      {wait > 0 ? `Muitas tentativas. Tente de novo em ${wait} s.` : error}
    </p>

    <!-- There is no email to recover the password: the remedy is for the admin
         to generate a new invite link, and whoever forgot needs to know that
         from here. -->
    <p class="muted small">Esqueceu a senha? Peça um link novo a quem cuida do dwnvr.</p>
  </form>

  <!-- Before signing in you can already tell which dwnvr this is - useful when
       there is a test one and a real one on the same network - and where it
       comes from. -->
  <p class="small version">
    {#if build.version}<span class="mono">{build.version}</span> ·{/if}
    <a href={REPO_URL} target="_blank" rel="noopener"><GitHubMark size={13} />GitHub</a>
  </p>
</div>

<style>
  .screen {
    display: grid;
    place-items: center;
    /* align-content, and not just place-items: with two rows, place-items
       centers each one INSIDE its track, and the tracks stretch to fill the
       screen - which would throw the version into the middle of the empty
       space. */
    align-content: center;
    gap: 16px;
    /* The whole #app column, which has the window height (see app.css). */
    flex: 1;
    padding: 20px;
  }

  .version {
    display: flex;
    align-items: center;
    gap: 6px;
    color: var(--dim);
  }

  /* Discreet like the version beside it: it is a footer, not a call to action. */
  .version a {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: inherit;
  }

  .version a:hover {
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

  /* The SVG already has its own rounding; no border-radius here. */
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
