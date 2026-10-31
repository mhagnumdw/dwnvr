# TODO - o quadro onde a lista recomeça sai do lugar

Achado em 30/10/2026, no teste da Etapa 7 (o que conferir no uso real) das
áreas e alertas, no item "pelo ⋮, a lista recomeça no instante daquele
quadro". Investigado e medido no mesmo dia; o mantenedor vai pensar depois se
conserta.

**Status: não corrigido.**

## O que acontece

Pelo ⋮ de uma miniatura de Detecções, "Filtrar por área" aplica o filtro e a
lista recomeça no instante daquele quadro: ele vai para o começo da lista, no
topo da vista. Logo depois o quadro sai do lugar:

- **na hora**, as detecções mais novas que passam no filtro voltam acima
  dele. Quando cabem na mesma linha, o quadro vai para a direita (no desktop,
  com 4 mais novas, ele ficou na 5ª coluna);
- **na rodada da cauda ao vivo** (a busca das novas, a cada 20 s), cada
  detecção nova desloca o quadro uma posição, e ele cai para a linha de baixo.

O "1 a 8 s" do teste é a fase do `setInterval` de 20 s da cauda
(`NOVAS_MS`), contada de quando a tela abriu.

## Por que é um problema

É um problema real, mas pequeno. A tela promete que a lista recomeça naquele
quadro e não cumpre direito:

- **perde a referência**: o olho estava no quadro, ele andou, e é preciso
  procurar de novo;
- **toque errado**: a grade mexe enquanto o dedo está chegando, e abre a
  miniatura errada. É o mesmo motivo pelo qual a tela já segura a vista de
  quem rolou para baixo quando chegam novas ("quem rolou para baixo não pode
  ter a tela empurrada por ela", no `novas()`).

O quadro quase nunca some da tela: anda uma ou duas posições, ou uma linha.
Só sairia de vista com muitas detecções chegando com a pessoa parada olhando.

## Como foi medido

Na simulação local, com Cozinha e Frente marcadas e a vista no topo. Um
script na própria aba registrava os `fetch` de `/api/deteccoes`, cada
`scrollTo`/`scrollBy` (com o `scrollY` antes e depois) e, a cada 500 ms, a
posição (`top`, `left`) da miniatura escolhida.

1. **⋮ da Cozinha 17:14:49 (2ª linha), área BalcaoGeladeira.** O `acima`
   trouxe 22 mais novas, a âncora rolou 774 px e o `top` do quadro ficou em
   216 px até depois da cauda. Mas ele foi para a 3ª coluna: 22 mais novas
   numa grade de 5 colunas deixam 2 antes dele na linha.
2. **⋮ da Frente 17:21:30 (5ª coluna da 1ª linha), área desenhada.** O `acima`
   trouxe 4 mais novas, que couberam na linha do quadro: nenhum `scrollBy`, a
   vista em `scrollY = 0`, e o quadro na 5ª coluna, com as 4 antes dele.
3. **A cauda, no caso 2.** A Frente não teve detecção nova (escureceu), então
   foram injetadas 6 detecções sintéticas só na resposta da cauda, no `fetch`
   da aba, sem tocar no servidor. A cauda **ancorou** (`scrollBy 193`, então
   `rolagem.ini > 0` era verdadeiro). Mesmo assim o quadro foi de
   `(216, 1370)` para `(409, 18)`: uma linha abaixo, na 1ª coluna.

## A causa

Duas coisas do `web/src/routes/Deteccoes.svelte` se somam:

1. **O reflow da grade.** O `layout` preenche as linhas de cada hora a partir
   da detecção mais nova. Toda detecção que entra acima, na mesma hora,
   desloca TODAS as miniaturas daquela hora N posições, e elas trocam de
   linha. A âncora (`ancora()`) segura a linha, pela primeira miniatura dela
   (`noTopo.det`), e não o quadro: no caso 3 a âncora era a 17:24:52, que foi
   para a 2ª coluna da linha de baixo, e o quadro, 6 posições adiante, caiu
   uma linha.
2. **A cauda sem âncora no topo.** O `novas()` só ancora com
   `rolagem.ini > 0`. Quando a vista fica em `scrollY = 0` (caso 2) e, pelo
   jeito do layout, o `ini` não passa de zero, o que é provável no celular, a
   cauda entra sem âncora nenhuma e empurra tudo. No desktop medido o `ini`
   ficou positivo e esse caminho não disparou; no celular não foi testado. Era
   a suspeita anotada no teste, e é só parte da causa.

## A solução proposta

As duas partes juntas; uma sem a outra deixa um caminho aberto. São cerca de
15 linhas, só no `Deteccoes.svelte`.

### 1. O instante do recomeço abre uma linha

No `layout`, a primeira detecção abaixo de `alvo` começa uma linha nova, como
já acontece na troca de hora:

```ts
const iRecomeco = alvo === null ? -1 : lista.findIndex((d) => d.instanteMs < alvo);
...
if (!atual || atual.itens.length === colunas || i === iRecomeco) {
```

O que chega acima (o `acima`, a cauda) preenche linhas só dele, e da linha do
quadro para baixo nada se mexe: a âncora fica exata e o quadro, na 1ª coluna.

- **Prós:** resolve a causa 1 no ponto onde ela nasce; vale também para o "ir
  para" e para o `recomecaNoLugar` (tirar filtro), que recomeçam num instante
  e têm o mesmo problema.
- **Contras:** uma linha incompleta logo acima do quadro, enquanto a lista
  não recomeçar do topo. Ela marca onde a lista recomeçou, mas é um buraco na
  grade que hoje não existe. No "ir para" de um dia, o recomeço quase sempre
  já cai numa troca de hora, e nada muda na tela.

### 2. Segurar a vista até a pessoa rolar

Uma flag `seguraVista`, ligada no `recomeca(pedido)` quando
`pedido !== null`. A cauda passa a ancorar com
`rolagem.ini > 0 || seguraVista`. A flag desliga no `onscroll` quando o
`scrollY` difere de `rolagemPropria`, que guarda onde a própria tela pôs a
rolagem: o `scrollTo` do `recomeca`, o `scrollBy` do `restaura` e o
`scrollTo` do `acima` com a lista vazia.

- **Prós:** fecha a causa 2; quem rola de volta ao topo volta a ver a
  novidade entrar, como no desenho original.
- **Contras:** mais um estado na tela, e a distinção entre rolagem da tela e
  rolagem da pessoa depende de toda rolagem programática atualizar
  `rolagemPropria`. Rolagem nova que esquecer disso desliga a flag cedo
  (falha branda: volta ao comportamento de hoje).
- **Variante mais simples, descartada:** ancorar sempre que `alvo !== null`.
  Dispensa a flag, mas quem voltou ao topo rolando nunca mais vê a novidade
  entrar até recomeçar do topo (o "hoje" do calendário).

### Alternativa: aceitar como está

Ajustar o texto do item na Etapa 7 para "o quadro fica na primeira linha da
vista, podendo andar uma posição ou uma linha" e marcá-lo. Faz sentido se no
uso real o movimento não atrapalhar.

## Como provar o conserto

O mesmo roteiro do "Como foi medido": ⋮ num quadro da 1ª linha com área
desenhada, e 6 detecções injetadas na resposta da cauda. O quadro tem de
ficar na 1ª coluna, no mesmo `top`, depois do `acima` e depois da cauda. E,
rolando até o topo, uma nova injeção tem de entrar sem âncora (a novidade
aparece em cima).

## Achado junto, fora do ⋮

O mesmo reflow acontece sem recomeço nenhum: com a vista rolada dentro da
hora atual, cada rodada da cauda desloca as miniaturas N posições, e elas
trocam de linha, embora a linha do topo fique onde estava. A solução 1 não
cobre esse caso, porque ali não há instante de recomeço. Consertá-lo pediria
outra âncora (a miniatura, não a linha) ou outro layout (linhas preenchidas a
partir da mais velha da hora, que deixa a linha incompleta no topo da hora).
Não medido em uso; só pela leitura do código e pelo caso 3.

## Vale a pena agora?

Não é urgente: nada se perde e o quadro segue à vista. Vale se o toque errado
ou a perda de referência aparecerem no uso, ou antes de fechar a Etapa 7, para
não deixar o item aberto.
