# TODO - mostrar o tempo EFETIVO gravado por câmera

Levantado em 12/08/2026, ao acrescentar a coluna "retido" no Diagnóstico e o chip
correspondente na tela de Câmeras.

**Status: não implementado, por decisão.** O que entrou foi a **profundidade**:
do segmento mais antigo em disco até agora (`store.Camera.OldestMs`). O que segue
é a outra medida, que ficou de fora.

## As duas medidas não são a mesma

**Profundidade** (implementada): `agora − início do segmento mais antigo`.
Responde "consigo voltar até que dia?".

**Tempo efetivo** (este TODO): a soma das durações dos segmentos que existem.
Responde "de quanto vídeo eu disponho de fato?".

Uma câmera que ficou fora do ar 3 dias no meio do período tem profundidade de
12 dias e tempo efetivo de 9. A diferença entre as duas **é** a informação
interessante: buraco de gravação. Hoje ela só é visível abrindo a timeline dia a
dia.

## Por que não foi feito agora

`DaySummary` (`internal/store/store.go:65`) guarda `Day`, `Count`, `Bytes`,
`FirstMs` e `LastMs` - nenhuma soma de duração. Obtê-la hoje custa `LoadDay()` em
cada dia: ~30 arquivos de índice lidos do disco **por câmera**, vezes 9 câmeras,
a cada leitura do `/api/health` - que a tela de Diagnóstico faz **de 3 em 3
segundos** e a de Câmeras de 5 em 5. Num Pi isso é I/O contínuo para um número
que muda uma vez por segmento.

## Opções, da melhor para a pior

### A. Acumular `DurMs` no `DaySummary` - recomendada

Acrescentar `DurMs int64` ao `DaySummary` e somá-lo em `mergeLocked`
(`store.go:225`). A leitura passa a ser O(1), igual ao `TotalBytes`.

**O ponto que torna esta opção barata:** `mergeLocked` é o funil único por onde
todo resumo se forma. Os três caminhos que alteram o índice já passam por ele:

- `Append` (`store.go:191`) - segmento novo;
- `Scan` (`store.go:404`) - reconstrução no boot, que já lê os índices inteiros,
  então a soma sai de graça e sem I/O adicional nenhum;
- `recount` (`store.go:645`) - pós-evicção; apaga o resumo do dia e remerge tudo,
  ou seja, se corrige sozinho.

`DropDay` remove o resumo inteiro e não precisa de nada.

Isso significa **um campo e uma linha** (`s.DurMs += e.DurMs`), sem um único
ponto de atualização novo a esquecer - que era o risco que faria esta opção cara.

Custo de memória: 8 bytes por dia-câmera, ~270 resumos na instalação de 9
câmeras com 30 dias. Irrelevante.

Depois disso, expor como `RecordedMs` no `recorder.Status` ao lado do
`OldestSegmentAt`, e na tela ou como coluna própria ou - melhor - como o `title`
do "retido", no formato "9 dias 4h gravados de 12 dias 6h".

Vale um teste que compare o acumulado contra a soma obtida por `LoadDay`, para
que uma alteração futura no índice não faça os dois divergirem em silêncio.

Estimativa: ~30 linhas somando store, recorder e web, mais o teste.

### B. Aproximar por `Count × segmentSeconds` - zero estado novo

`Count` já está no resumo e `segmentSeconds` é configuração da câmera. Sai de
graça, hoje, sem tocar no store.

O problema é que erra justamente onde a medida importa: o corte real é por
keyframe e nunca bate a duração alvo, e todo segmento interrompido por
reconexão é mais curto. Numa câmera instável - a que mais interessa medir - a
aproximação infla o número e esconde o buraco que se queria enxergar.

Serve como paliativo se em algum momento se quiser a informação **hoje**, com um
"≈" na frente. Não serve como resposta definitiva.

### C. Cache com TTL na API por cima do `LoadDay`

Calcular pelo caminho caro uma vez a cada N minutos e guardar o resultado.

Não exige tocar no store, mas mantém o I/O (só o dilui), acrescenta estado novo e
uma política de invalidação na API, e entrega um número que pode estar minutos
atrasado. É mais código que a opção A para um resultado pior. Fica registrada
como alternativa considerada e descartada.

## Vale a pena?

**Provavelmente sim, e mais do que parecia** - a descoberta do funil único em
`mergeLocked` derruba o custo da opção A para perto de nada. O que segura é só a
prioridade: a profundidade já responde a pergunta que se fazia no dia a dia
("até quando consigo voltar?").

O critério para puxar isto: a primeira vez que alguém olhar o "retido" e
precisar abrir a timeline para descobrir se aquele período tem buraco.
