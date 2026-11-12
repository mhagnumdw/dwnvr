<!--
  Campo de senha com o olho para conferir o que se digitou. Senha digitada no
  celular erra fácil; o olho deixa conferir antes de mandar.

    <CampoSenha bind:value={senha} placeholder="senha" autocomplete="current-password" />
-->
<script lang="ts">
  import type { HTMLInputAttributes } from 'svelte/elements';
  import Icon from './Icon.svelte';

  // O resto vai direto ao <input>: minlength, maxlength, id.
  interface Props extends Omit<HTMLInputAttributes, 'value' | 'type'> {
    value?: string;
  }

  let {
    value = $bindable(''),
    placeholder = 'senha',
    autocomplete = 'current-password',
    ...rest
  }: Props = $props();

  let ver = $state(false);
</script>

<div class="password">
  <input
    bind:value
    type={ver ? 'text' : 'password'}
    {placeholder}
    {autocomplete}
    autocapitalize="none"
    autocorrect="off"
    required
    {...rest}
  />
  <!-- type="button" para não submeter o formulário. -->
  <button
    type="button"
    class="eye"
    onclick={() => (ver = !ver)}
    aria-pressed={ver}
    aria-label={ver ? 'esconder senha' : 'mostrar senha'}
    title={ver ? 'esconder senha' : 'mostrar senha'}
  >
    <Icon name={ver ? 'eyeOff' : 'eye'} size={18} stroke={2.1} />
  </button>
</div>

<style>
  .password {
    position: relative;
  }

  /* Espaço para o olho, que fica por cima do fim do campo. */
  input {
    width: 100%;
    padding-right: 44px;
  }

  /* O Edge põe um olho próprio no campo de senha; com o nosso, seriam dois. */
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
