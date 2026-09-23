<script>
  // Um card de consulta do Diagnóstico: vem fechado, abre num toque e mostra
  // grupos de rótulo e valor, com um "copiar" que junta tudo em texto para mandar
  // numa conversa. É o formato de "Este navegador" e de "Este servidor"; quem usa
  // decide o que coletar, e só coleta com `aberto`.
  //
  // `texto` devolve o que o "copiar" leva. `children` entra depois dos grupos,
  // para o que não cabe em rótulo e valor - as linhas do log, por exemplo.
  import { copiar } from '../lib/navegador.js';

  let { titulo, grupos, aberto = $bindable(false), texto, children } = $props();

  let copiado = $state('');

  async function aoCopiar() {
    const ok = await copiar(texto());
    copiado = ok ? 'copiado' : 'não deu para copiar';
    setTimeout(() => (copiado = ''), 2500);
  }
</script>

<div class="card diag">
  <div class="row">
    <!-- O cabeçalho inteiro é o botão, e não só a seta: no celular é o alvo
         que o dedo acha. O "copiar" fica fora dele, senão tocar para copiar
         também fecharia o card. -->
    <button class="abrir row" onclick={() => (aberto = !aberto)} aria-expanded={aberto}>
      <span class="seta-card">{aberto ? '▾' : '▸'}</span>
      <strong>{titulo}</strong>
    </button>
    {#if aberto && grupos.length}
      {#if copiado}<span class="muted small">{copiado}</span>{/if}
      <button onclick={aoCopiar}>copiar</button>
    {/if}
  </div>
  {#if aberto && grupos.length}
    <div class="grupos">
      {#each grupos as g (g.titulo)}
        <section>
          <p class="titulo muted">{g.titulo}</p>
          <dl>
            <!-- Pela posição, e não pelo rótulo: parte do rótulo vem do
                 servidor (o nome do sensor), e dois iguais fariam o Svelte
                 parar de desenhar o card inteiro. -->
            {#each g.itens as i, n (n)}
              <div class="item small">
                <dt class="muted">{i.rotulo}</dt>
                <dd class="mono" class:alerta={i.alerta}>{i.valor}</dd>
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
  .diag:not(.row) { display: grid; gap: 10px; }

  /* Uma coluna no celular, duas quando cabe. Os grupos não se partem entre
     colunas: "Vídeo" pela metade separaria o H.265 do MediaSource. */
  .grupos { display: grid; gap: 14px; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); }
  dl { margin: 0; display: grid; gap: 2px; }
  .titulo { margin: 0 0 4px; text-transform: uppercase; letter-spacing: 0.04em; font-size: 11px; }
  /* Botão sem cara de botão: lê como o título dos outros cards. Os 44px de
     toque ficam; a margem negativa devolve a altura para o card fechado não
     ficar mais alto que os vizinhos. */
  .abrir {
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
  .abrir:hover:not(:disabled) { border-color: transparent; }
  .seta-card { width: 12px; font-size: 12px; color: var(--dim); }
  .item { display: flex; gap: 10px; justify-content: space-between; }
  dd { margin: 0; text-align: right; overflow-wrap: anywhere; }
  .alerta { color: var(--warn); }
</style>
