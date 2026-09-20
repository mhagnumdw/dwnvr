# TODO - segmento de zero byte sobrevive no índice depois de uma queda

Levantado em 20/09/2026, na conferência da noite, com as 9 câmeras na
instalação real. Não quebra nada e a escala é minúscula: **40 registros em
295.961** (0,014%) e 42 arquivos `.mp4` de zero byte no acervo inteiro.

**Status: não implementado.** É um caso de queda de energia, condição normal de
operação, e o efeito é um trecho de timeline que não toca. Fica registrado
porque a correção é pequena e mora num lugar que já faz quase tudo certo.

## O que acontece

Toda queda suja deixa, por câmera, um ou dois registros ruins. O padrão é
sempre o mesmo par:

```
11:03:09.923  d=35981  sz=0  g=4edbc50d8e70   <- o índice promete 36s, o arquivo tem 0 byte
11:03:45.904  d=0      sz=0  g=""             <- registro zerado, sem geração
```

E no disco, os dois arquivos correspondentes com zero byte.

Datas em que isso apareceu no acervo: 08, 12, 14, 18 e 20 de setembro - uma
para cada desligamento sem aviso. Não tem relação com a detecção: o acervo
anterior a ela tem o mesmo padrão.

## Por que cada um dos dois nasce

**O primeiro (`sz=0` com duração cheia).** `segmenter.finish()`
(`internal/recorder/recorder.go:972`) fecha o `.mp4` e só então grava a linha do
índice, e o comentário de lá explica que a ordem é essa de propósito. A ordem
está certa; o que falta é durabilidade. `finish` faz `Flush` no `bufio` e
`Close` no arquivo - os bytes ficam no page cache, sem `Sync` -, enquanto
`store.Camera.Append` (`internal/store/store.go:264`) faz `f.Sync()` na linha do
índice. Uma queda entre as duas coisas torna durável justamente o registro, e
perde o vídeo que ele descreve.

A reconciliação do boot (`internal/store/store.go:624-628`) já percebe metade
disso: ela compara o tamanho em disco com o do índice e corrige o `Size`. Mas
corrige só o `Size` - a `DurMs` fica com os 36 s originais. O resultado é um
segmento que a timeline desenha e o player não toca.

**O segundo (registro todo zerado).** É o arquivo de zero byte que nunca chegou
ao `finish`, entrando pelo caminho dos órfãos
(`internal/store/store.go:634-641`). Esse caminho descarta o órfão cujo `probe`
devolve erro - mas um arquivo vazio **não** devolve erro. Medido com um teste
descartável em `fmp4.ProbeSegment`, sobre um `.mp4` de zero byte:

    erro=<nil>  info=&{InitSize:0 FirstFragSize:0 DurationMs:0 Gen: Movie:<nil>}

Sem erro, a `Entry` entra com geração vazia, `DurMs` 0 e `Size` 0. Geração
vazia é o campo pelo qual o player agrupa o que dá para emendar, então esse
registro é um corte de faixa a mais em cima do buraco.

## A correção que se propõe

Na reconciliação, e não no caminho de gravação:

1. **Tamanho zero descarta a entrada.** Onde hoje `e.Size = size` conserta o
   tamanho, `size == 0` deve tirar o registro do índice e apagar o arquivo:
   segmento sem byte nenhum não é gravação truncada, é gravação que não houve.
2. **Órfão que o `probe` não entende não entra.** Hoje o erro do `probe` é
   tratado, mas o arquivo vazio não dá erro e vira `Entry` zerada. Recusar
   `Gen == ""` ou `DurMs == 0` **no laço dos órfãos** fecha o caso.

   O "no laço dos órfãos" é a parte que importa. O `AGENTS.md` registra que o
   índice é append-only e que "a leitura tolera zero em linha antiga": campo que
   nasceu depois lê zero numa linha velha, e recusar zero na leitura em geral
   apagaria gravação legítima. No laço dos órfãos não há esse risco - ali a
   `Entry` acabou de sair do `probe`, nunca de uma linha do disco.

O `Sync` no `.mp4` do segmento **não** se propõe: seria um fsync por segmento
por câmera para salvar um minuto de vídeo numa queda de energia, e o disco é um
HD USB num Orange Pi Zero 3. O barato é aceitar a perda e não deixar o índice
mentir sobre ela.

## O que medir depois

Os 42 arquivos que já estão no acervo não somem sozinhos: a reconciliação só
roda no dia que ela abre. Se a correção entrar, vale conferir se ela alcança os
dias antigos ou se é preciso uma passada única.
