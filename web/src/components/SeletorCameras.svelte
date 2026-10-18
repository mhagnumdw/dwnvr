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
    />

  `todas` é o que troca "todas" por "limpar" no primeiro botão.
-->
<script lang="ts">
  import type { Camera } from '../lib/api.ts';

  let {
    cameras,
    marcada,
    todas,
    ontoggle,
    ontodas,
  }: {
    cameras: Camera[];
    marcada: (id: string) => boolean;
    todas: boolean;
    ontoggle: (id: string) => void;
    ontodas: () => void;
  } = $props();
</script>

<div class="chips">
  {#if cameras.length}
    <button class="camchip todas" onclick={ontodas}>
      {todas ? 'limpar' : 'todas'}
    </button>
    {#each cameras as c (c.id)}
      <button
        class="camchip"
        class:on={marcada(c.id)}
        aria-pressed={marcada(c.id)}
        onclick={() => ontoggle(c.id)}
      >
        <span class="nome-cam">{c.name}</span>
      </button>
    {/each}
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
  .nome-cam {
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
  .camchip.todas {
    font-weight: 500;
    background: transparent;
  }
  .camchip.todas::before {
    display: none;
  }
</style>
