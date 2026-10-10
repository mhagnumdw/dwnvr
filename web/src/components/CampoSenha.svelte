<!--
  Campo de senha com o olho para conferir o que se digitou. Senha digitada no
  celular erra fácil; o olho deixa conferir antes de mandar.

    <CampoSenha bind:value={senha} placeholder="senha" autocomplete="current-password" />
-->
<script>
  let { value = $bindable(''), placeholder = 'senha', autocomplete = 'current-password', ...resto } = $props();

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
    <svg viewBox="0 0 16 16" aria-hidden="true">
      <path d="M1.5 8S4 3.5 8 3.5 14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8z" stroke-linejoin="round" />
      <circle cx="8" cy="8" r="2" />
      {#if ver}
        <path d="M2.5 13.5l11-11" stroke-linecap="round" />
      {/if}
    </svg>
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

  .olho svg {
    width: 18px;
    height: 18px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.4;
  }
</style>
