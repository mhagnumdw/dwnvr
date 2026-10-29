# TODO - "filtrar por área" da folha com as áreas cadastradas

**Status: ideia, adiada (29/10/2026).** Saiu junto com o "Filtrar por área"
do menu ⋮ das miniaturas de Detecções. O mantenedor preferiu não mexer na
folha agora e ver no uso se faz falta.

## O que é hoje

Na folha de uma detecção, o botão "filtrar por área" só desenha: abre o editor
sobre o quadro daquela detecção (`onarea` do `DeteccaoFolha.svelte`, que chama
o `abreEditor(d.cam, d)` de `Deteccoes.svelte`). As áreas cadastradas na
câmera não aparecem ali.

O ⋮ da miniatura faz mais: com área cadastrada, "Filtrar por área ›" troca o
conteúdo do menu pela lista das áreas da câmera (marcar soma, a marcada leva
um ✓, tocar de novo tira) e, no fim, "Desenhar sobre este quadro". Sem área,
vai direto ao editor. A lista recomeça no instante daquele quadro.

Os dois lugares usam o mesmo verbo para coisas diferentes.

## O que seria

O botão da folha igual ao ⋮: com área cadastrada na câmera, ele abre a mesma
lista (as áreas com ✓, a desenhada quando houver, e "Desenhar sobre este
quadro"); sem área, continua indo direto ao editor.

## O que decidir antes

- **Onde a lista aparece.** A folha é um modal, e a lista do ⋮ é um menu
  `position: fixed` da grade. O caminho mais curto é um popover ancorado no
  próprio botão, com os mesmos itens; um componente comum aos dois evitaria
  duas cópias da lista.
- **A folha fecha ao marcar?** A lista recomeça e a detecção aberta pode sair
  do filtro. O ⋮ fecha o menu; aqui, fechar a folha é o natural, com a lista
  no instante da detecção.
