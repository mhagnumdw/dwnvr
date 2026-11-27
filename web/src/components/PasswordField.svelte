<!--
  Password field with the eye to check what was typed. A password typed on the
  phone is easy to get wrong; the eye lets you check before sending.

  <PasswordField bind:value={password} placeholder="senha"
  autocomplete="current-password" />
-->
<script lang="ts">
  import type { HTMLInputAttributes } from 'svelte/elements';
  import Icon from './Icon.svelte';

  // The rest goes straight to the <input>: minlength, maxlength, id.
  interface Props extends Omit<HTMLInputAttributes, 'value' | 'type'> {
    value?: string;
  }

  let {
    value = $bindable(''),
    placeholder = 'senha',
    autocomplete = 'current-password',
    ...rest
  }: Props = $props();

  let revealed = $state(false);
</script>

<div class="password">
  <input
    bind:value
    type={revealed ? 'text' : 'password'}
    {placeholder}
    {autocomplete}
    autocapitalize="none"
    autocorrect="off"
    required
    {...rest}
  />
  <!-- type="button" so it does not submit the form. -->
  <button
    type="button"
    class="eye"
    onclick={() => (revealed = !revealed)}
    aria-pressed={revealed}
    aria-label={revealed ? 'esconder senha' : 'mostrar senha'}
    title={revealed ? 'esconder senha' : 'mostrar senha'}
  >
    <Icon name={revealed ? 'eyeOff' : 'eye'} size={18} stroke={2.1} />
  </button>
</div>

<style>
  .password {
    position: relative;
  }

  /* Room for the eye, which sits over the end of the field. */
  input {
    width: 100%;
    padding-right: 44px;
  }

  /* Edge puts its own eye on the password field; with ours there would be two. */
  input::-ms-reveal {
    display: none;
  }

  .eye {
    position: absolute;
    top: 0;
    right: 0;
    bottom: 0;
    width: 44px;
    min-height: 0;
    display: grid;
    place-items: center;
    padding: 0;
    border: none;
    background: transparent;
    color: var(--dim);
  }

  .eye:hover,
  .eye[aria-pressed='true'] {
    color: var(--fg);
  }
</style>
