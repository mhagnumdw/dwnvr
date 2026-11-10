# TODO - Detecções rola para o lado entre 1060 e ~1218 px

Achado em 09/11/2026, ao conferir no desktop a correção da rolagem do app
instalado depois do puxar para atualizar (o `min-height` do `#app`). A medida
é igual antes e depois dela: o defeito é anterior.

**Status: não corrigido.**

## O que acontece

A partir de 1060 px de largura, a barra de Detecções (dia e hora, famílias,
controles das miniaturas e o botão das câmeras) passa a uma linha só, pelo
`@media (min-width: 1060px)` do `Deteccoes.svelte`, cujo comentário diz que a
barra inteira mede ~1000 px. Com as famílias e as contagens de um dia real,
ela mede ~1208 px. Até a janela chegar lá, a linha não cabe: o botão das
câmeras sai da tela e a página inteira rola para o lado.

| Largura da janela | Rolagem lateral |
| --- | --- |
| 1059 px | nenhuma: a barra fica em duas linhas |
| 1060 px | 158 px |
| 1100 px | 118 px |
| 1150 px | 68 px |
| 1200 px | 18 px |
| 1210 px | 8 px |
| 1220 px ou mais | nenhuma |

Acontece no Chrome e no Firefox. No Firefox com a escala de tela em 125%,
numa janela de 1366 px, a página tem 1092,8 px de largura e rola 121 px. Cai
nessa faixa quem usa a janela do navegador não maximizada, ou um notebook com
escala de tela: 1366 px a 125% dá 1093 px, e 1440 px a 125% dá 1152 px.

## Como foi medido

O dwnvr local com as detecções de um dia de 3 câmeras, logado como admin, e o
Chrome headless em cada largura. O número é `scrollWidth - clientWidth` do
documento. Quem passa da janela é a `div.cams` e, em 1060 px, também a
`div.controles`.

## Como resolver

- **Subir o breakpoint** da linha única para uns 1240 px, com folga para
  fonte do sistema mais larga. Prós: uma linha de CSS. Contras: o número fica
  velho de novo quando a barra ganhar um botão ou uma família, e entre 1060 e
  1240 px a barra passa a ocupar duas linhas, tirando altura da grade.
- **Deixar a linha única quebrar** (`flex-wrap: wrap`) em vez de decidir pela
  largura da janela. Prós: não depende de número nenhum. Contras: a quebra
  pode deixar um botão sozinho na segunda linha. Pede um mock antes.

## Vale a pena agora?

Vale, e é barato. Quem está nessa faixa de largura vê o botão das câmeras
cortado e a tela inteira rolando para o lado. A primeira opção resolve com
uma linha. A segunda é melhor a longo prazo, mas pede ver o mock.
