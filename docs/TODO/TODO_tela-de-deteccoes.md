# TODO - tela de Detecções

Uma tela própria para as detecções de objeto, ao lado de Gravações: todas as
aparições numa lista rolável, cada uma com dia e hora, a imagem com as caixas,
um player pequeno e o link para Gravações no instante exato.

**Status: não implementado, por decisão.** Três variantes foram desenhadas; a
recomendação é a B.

## O pedido

| O quê | Como |
|---|---|
| lista | rolagem infinita, com a busca paginada no servidor |
| cada item | dia e hora, a imagem do objeto com as caixas desenhadas, um player pequeno e o link para Gravações |
| player | só carrega quando alguém toca nele; até lá, a imagem com as caixas |
| ir para | um dia e um horário |
| câmeras | dá para esconder qualquer uma; por padrão, todas à mostra |
| famílias | dá para esconder `pessoa`, `veiculo` ou `animal`; por padrão, todas à mostra |

## O volume decide o desenho

Com várias câmeras no nível 5 de sensibilidade, uma casa comum passa de
centenas de marcas de objeto por dia, concentradas nas horas de movimento. Uma
câmera voltada para a rua sozinha pode dar centenas de `veiculo`, carros
passando. Por isso a pergunta principal da tela não é lista vertical ou
horizontal, e sim como atravessar centenas de detecções por dia e ainda achar
a `pessoa`: filtro de família, "ir para" e quantas cabem por tela.

## As três variantes

| Variante | A favor | Custo | Por tela (celular / desktop) |
|---|---|---|---|
| **A - lista vertical** | imagem grande, rótulos legíveis, o trecho toca no próprio card | atravessar o dia depende dos filtros e do "ir para" | 2 / 3 |
| **B - grade** | dá para achar gente olhando as miniaturas | a miniatura não tem rótulo, e o vídeo pede um toque a mais (abre a folha) | 10 / 20 |
| **C - tira horizontal** | um player só, e a régua do dia mostra onde houve detecção, como em Gravações | a tira anda devagar pelo dia, e na régua de 24 h meia hora ocupa poucos pixels | 3 / 7 |

A recomendação é a **B**, pela densidade: ela mostra cinco vezes mais que a A
no celular, e o vídeo continua a um toque.

## Já decidido

- **A mais nova no topo.** O dia e o relógio da barra mostram a detecção que
  está no topo da lista, e digitar um horário pula para ela: o mesmo `Relogio`
  e o mesmo `DayPicker` de Gravações.
- **O filtro é por família**, e não por classe: são 3 botões, contra 12 classes
  (carro, caminhão, gato...).
- **Câmeras** com a lista de checkboxes do Ao vivo, num popover.
- **A imagem inteira é o botão de tocar**, com o ▶ no canto: no meio, ele
  tampava justamente o objeto.
- **Uma detecção é um instante**, o do onset, como a marca na timeline. Várias
  marcas do mesmo instante (uma olhada grava no máximo uma por família, então
  uma pessoa e um carro juntos) são um item só, com todas as caixas.
- O trecho começa 2 s antes do onset, e a caixa acende no `quadroMs`, como o
  `Caixas.svelte` faz em Gravações.
- A aba nova fica entre Gravações e Câmeras.

## O que a implementação vai exigir

Conferir no código ao começar.

- **Uma rota paginada cruzando câmeras.** O `/api/rec/events` responde uma
  câmera por intervalo, com todas as marcas dele. A tela precisa de "as N
  detecções antes do instante X, destas câmeras e famílias", com cursor.
  Repercussões de rota nova: `docs/api.md` e `web/src/lib/api.js`.
- **A imagem com as caixas.** O `/api/rec/thumb` devolve o keyframe do início
  do segmento (30 s por padrão), e não o quadro olhado: o objeto pode nem estar
  nele. O quadro certo exige decodificar do keyframe anterior até o `quadroMs`.
  O servidor não decodifica nada, então o provável caminho é uma rota que
  entregue esse pedaço, como o `Pedaco` que o `internal/detect/recorte.go` já
  corta para o detector, decodificado no navegador por WebCodecs (como o
  `web/src/lib/thumbs.js`) e com as caixas pintadas por `web/src/lib/caixas.js`.
  No celular, com dezenas de itens, isso precisa de fila e cache (o `thumbs.js`
  usa 3 em paralelo e 240 em cache).
- **O player do item.** Reaproveitar o player MSE de Gravações
  (`web/src/lib/player.svelte.js`, `/api/rec/init` + `/api/rec/seg`) para
  alguns segundos em volta do onset, um item tocando por vez.
- **Navegação.** Quinta rota no `ROUTES` do `web/src/App.svelte` (a navegação
  de baixo passa de 4 para 5 colunas), com filtros e posição na URL pelo
  `web/src/lib/rota.svelte.js`, como Gravações e Ao vivo já fazem.
