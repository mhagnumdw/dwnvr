<script lang="ts">
  // Um card de consulta do Diagnóstico: vem fechado, abre num toque e mostra
  // grupos de rótulo e valor, com um "copiar" que junta tudo em texto para mandar
  // numa conversa. É o formato de "Este navegador" e de "Este servidor"; quem usa
  // decide o que coletar, e só coleta com `aberto`.
  //
  // `texto` devolve o que o "copiar" leva. `children` entra depois dos grupos,
  // para o que não cabe em rótulo e valor - as linhas do log, por exemplo.
  // `subtitulo`, opcional, vai embaixo do título com o card aberto - o "Coletado
  // em" do servidor, por exemplo.
  import type { Snippet } from 'svelte';
  import { copy } from '../lib/browser.ts';
  import Icon from './Icon.svelte';
  import type { CardGroup } from '../lib/browser.ts';

  interface Props {
    title: string;
    subtitle?: string;
    groups: CardGroup[];
    isOpen?: boolean;
    text: () => string;
    children?: Snippet;
  }

  let { title, subtitle = '', groups, isOpen = $bindable(false), text, children }: Props = $props();

  let copied = $state('');

  async function onCopy() {
    const ok = await copy(text());
    copied = ok ? 'copiado' : 'não deu para copiar';
    setTimeout(() => (copied = ''), 2500);
  }
</script>

<div class="card diag">
  <div class="row">
    <!-- O cabeçalho inteiro é o botão, e não só a seta: no celular é o alvo
         que o dedo acha. O "copiar" fica fora dele, senão tocar para copiar
         também fecharia o card. -->
    <button class="open row" onclick={() => (isOpen = !isOpen)} aria-expanded={isOpen}>
      <span class="arrow-card"><Icon name={isOpen ? 'chevronDown' : 'chevronDir'} size={14} /></span
      >
      <strong>{title}</strong>
    </button>
    {#if isOpen && groups.length}
      {#if copied}<span class="muted small">{copied}</span>{/if}
      <button class="copy" onclick={onCopy}>copiar</button>
    {/if}
  </div>
  <!-- Fora do botão: dentro dele, a segunda linha mudaria a altura do
       cabeçalho e o título pularia ao abrir. -->
  {#if isOpen && subtitle}<p class="subtitle muted small mono">{subtitle}</p>{/if}
  {#if isOpen && groups.length}
    <div class="groups">
      {#each groups as g (g.title)}
        <section>
          <p class="title muted">{g.title}</p>
          <dl>
            <!-- Pela posição, e não pelo rótulo: parte do rótulo vem do
                 servidor (o nome do sensor), e dois iguais fariam o Svelte
                 parar de desenhar o card inteiro. -->
            {#each g.items as i, n (n)}
              <div class="item small">
                <dt class="muted">{i.label}</dt>
                <dd class="mono" class:alert={i.alert}>{i.value}</dd>
              </div>
            {/each}
          </dl>
        </section>
      {/each}
    </div>
    {@render children?.()}
  {/if}
</div>

<style>
  .diag:not(.row) {
    display: grid;
    gap: 10px;
  }

  /* Uma coluna no celular, duas quando cabe. Os grupos não se partem entre
     colunas: "Vídeo" pela metade separaria o H.265 do MediaSource. */
  .groups {
    display: grid;
    gap: 14px;
    grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  }
  dl {
    margin: 0;
    display: grid;
    gap: 2px;
  }
  .title {
    margin: 0 0 4px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    font-size: 11px;
  }
  /* Botão sem cara de botão: lê como o título dos outros cards. Os 44px de
     toque ficam; a margem negativa devolve a altura para o card fechado não
     ficar mais alto que os vizinhos. */
  .open {
    flex: 1;
    gap: 8px;
    margin: -8px 0;
    padding: 0;
    background: none;
    border: none;
    color: inherit;
    font: inherit;
    text-align: left;
  }
  .open:hover:not(:disabled) {
    border-color: transparent;
  }
  /* A mesma margem do cabeçalho: sem ela, o "copiar" que aparece ao abrir
     deixaria a linha 16px mais alta, e o título desceria. */
  .copy {
    margin: -8px 0;
  }
  /* Alinhado com o título, depois da seta (12px) e do vão (8px), e puxado
     para perto dele. */
  .subtitle {
    margin: -6px 0 0;
    padding-left: 20px;
  }
  .arrow-card {
    display: flex;
    width: 14px;
    color: var(--dim);
  }
  .item {
    display: flex;
    gap: 10px;
    justify-content: space-between;
  }
  dd {
    margin: 0;
    text-align: right;
    overflow-wrap: anywhere;
  }
  .alert {
    color: var(--warn);
  }
</style>
