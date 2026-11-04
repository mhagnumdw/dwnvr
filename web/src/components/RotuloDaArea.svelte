<!--
  O rótulo de uma área: a miniatura dela, o nome e a regra por extenso. É o
  conteúdo da linha da área na aba Áreas do cadastro e no ⋮ do card de
  Detecções; a linha em volta é de quem usa, porque cada uma faz uma coisa (lá
  editar e apagar, aqui marcar o filtro).
-->
<script lang="ts">
  import type { AreaDaCamera } from '../lib/api.ts';
  import { REGRAS, REGRA_PADRAO } from '../lib/area.ts';
  import Icone from './Icone.svelte';
  import MiniaturaDasAreas from './MiniaturaDasAreas.svelte';

  interface Props {
    cam: string;
    area: AreaDaCamera;
    largura?: number;
    // O nome em negrito: na aba Áreas, onde a área é o assunto da linha.
    negrito?: boolean;
    fundo?: string | undefined;
  }

  let { cam, area, largura = 64, negrito = false, fundo }: Props = $props();

  const regra = $derived(REGRAS[area.rule ?? REGRA_PADRAO]);
</script>

<span class="rotulo" data-toque-longo>
  <MiniaturaDasAreas {cam} areas={[area]} {largura} desenho="cheia" raio={4} {fundo} zoom />
  <span class="texto">
    <span class="nome" class:negrito>{area.name}</span>
    <span class="muted small regra">
      <Icone nome={regra.icone} tamanho={16} />
      {regra.nome}
    </span>
  </span>
</span>

<style>
  .rotulo {
    display: flex;
    flex: 1;
    align-items: center;
    gap: 10px;
    min-width: 0;
  }
  .texto {
    display: grid;
    min-width: 0;
    line-height: 1.35;
  }
  .nome {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .negrito {
    font-weight: 700;
  }
  .regra {
    display: inline-flex;
    align-items: center;
    gap: 5px;
  }
</style>
