<!--
  Campo de senha com o olho para conferir o que se digitou. Senha digitada no
  celular erra fácil; o olho deixa conferir antes de mandar.

    <CampoSenha bind:value={senha} placeholder="senha" autocomplete="current-password" />
-->
<script lang="ts">
  import type { HTMLInputAttributes } from 'svelte/elements';
  import Icone from './Icone.svelte';

  // O resto vai direto ao <input>: minlength, maxlength, id.
  interface Props extends Omit<HTMLInputAttributes, 'value' | 'type'> {
    value?: string;
  }

  let {
    value = $bindable(''),
    placeholder = 'senha',
    autocomplete = 'current-password',
    ...resto
  }: Props = $props();

  let ver = $state(false);
</script>

<div class="senha">
  <input
    bind:value
    type={ver ? 'text' : 'password'}
    {placeholder}
    {autocomplete}
    autocapitalize="none"
    autocorrect="off"
    required
    {...resto}
  />
  <!-- type="button" para não submeter o formulário. -->
  <button
    type="button"
    class="olho"
    onclick={() => (ver = !ver)}
    aria-pressed={ver}
    aria-label={ver ? 'esconder senha' : 'mostrar senha'}
    title={ver ? 'esconder senha' : 'mostrar senha'}
  >
    <Icone nome={ver ? 'olhoCortado' : 'olho'} tamanho={18} traco={2.1} />
  </button>
</div>

<style>
  .senha {
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

  .olho {
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

  .olho:hover,
  .olho[aria-pressed='true'] {
    color: var(--fg);
  }
</style>
