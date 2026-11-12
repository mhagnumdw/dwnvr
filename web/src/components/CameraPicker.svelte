<!--
  A lista de câmeras que o botão "câmeras" abre, no Ao vivo e em Detecções: um
  botão de um toque por câmera, com ponto verde e borda azul nas marcadas. Quem
  usa decide o contêiner (um card no fluxo, um popover) e o que "marcada" quer
  dizer; aqui só se desenha e se avisa o clique.

    <SeletorCameras
      cameras={cameras.list}
      marcada={(id) => selected.has(id)}
      todas={tudoMarcado}
      ontoggle={toggle}
      ontodas={todas}
      erro={cameras.error}
    />

  `todas` é o que troca "todas" por "limpar" no primeiro botão. `erro` é o
  `cameras.error`: com a lista vazia porque a carga falhou, o seletor diz isso
  em vez de "nenhuma câmera cadastrada", que seria falso.
-->
<script lang="ts">
  import type { Camera } from '../lib/api.ts';

  let {
    cameras,
    checked,
    all,
    ontoggle,
    onall,
    error = null,
  }: {
    cameras: Camera[];
    checked: (id: string) => boolean;
    all: boolean;
    ontoggle: (id: string) => void;
    onall: () => void;
    error?: string | null;
  } = $props();
</script>

<div class="chips">
  {#if cameras.length}
    <button class="camchip all" onclick={onall}>
      {all ? 'limpar' : 'todas'}
    </button>
    {#each cameras as c (c.id)}
      <button
        class="camchip"
        class:on={checked(c.id)}
        aria-pressed={checked(c.id)}
        onclick={() => ontoggle(c.id)}
      >
        <span class="cam-name">{c.name}</span>
      </button>
    {/each}
  {:else if error}
    <p class="muted small">Não deu para carregar as câmeras: {error}</p>
  {:else}
    <p class="muted small">nenhuma câmera cadastrada</p>
  {/if}
</div>

<style>
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .camchip {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    min-height: 40px;
    padding: 0 14px;
    border-radius: 10px;
    font-weight: 600;
    font-size: 14px;
    color: var(--dim);
    max-width: 100%;
  }
  .cam-name {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .camchip::before {
    content: '';
    flex: none;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--line);
  }
  .camchip.on {
    color: var(--fg);
    border-color: var(--accent);
    background: var(--ativo-fundo);
  }
  .camchip.on::before {
    background: var(--ok);
  }
  .camchip.all {
    font-weight: 500;
    background: transparent;
  }
  .camchip.all::before {
    display: none;
  }
</style>
