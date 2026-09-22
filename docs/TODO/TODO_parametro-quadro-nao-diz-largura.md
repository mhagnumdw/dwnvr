# TODO - o parâmetro `quadro` do dwnvr-detect não diz que é uma largura

Levantado em 20/09/2026, ao acrescentar o quadro em JPEG à resposta do
detector. Não quebra nada: é **nome ruim**, e o custo de trocar só cresce com o
tempo.

**Status: não implementado.** O nome entrou como está para não segurar a tela
de Detecções; a troca é mecânica e cabe em qualquer dia calmo.

## O nome

```
POST /detect?piso=0.20&quadro=480&qualidade=75
```

`quadro=480` é a **largura em pixels** do JPEG que volta. Lido sozinho, na URL
ou no log, `quadro=480` não diz largura: pode passar por índice do quadro,
quantidade de quadros ou instante. Ao lado de `piso` e `qualidade`, que são o
que dizem ser, ele é o único que exige ir ao código para entender.

O mesmo nome carrega duas coisas ao mesmo tempo: **se** o quadro vem (ausente
ou `0` é "não manda") e **de que tamanho** ele vem.

## O que trocar

Sugestão: `larguraDoQuadro`, que é como o Go já chama o número
(`detect.LarguraDoQuadro`). Alternativas: `quadroLargura`, `larguraQuadro`.

| Arquivo | O que muda |
|---|---|
| `dwnvr-detect/servidor.py` | a leitura da query em `do_POST`, o cabeçalho do módulo |
| `dwnvr-detect/README.md` | §O contrato |
| `internal/detect/sidecar.go` | a montagem da URL em `Olha` |
| `docs/deteccao.md` | §O dwnvr-detect |

Nenhum dado gravado muda: o nome vive só na chamada HTTP.

## O cuidado na troca

O dwnvr e o dwnvr-detect **sobem separados**, e nada garante que subam juntos.
Um dwnvr novo mandando `larguraDoQuadro` para um sidecar antigo não recebe
quadro nenhum - a detecção continua, a miniatura some. Por isso:

1. Primeiro o sidecar passa a aceitar **os dois** nomes, preferindo o novo.
2. Numa versão seguinte, o dwnvr passa a mandar só o novo.
3. Numa terceira, o sidecar para de aceitar o velho.

Se os dois sempre subirem juntos - que é o caso do `docker-compose.yml` deste
repositório -, os três passos viram um só.

## Vale a pena?

**Sim, mas sem pressa.** É nome em contrato público de um endpoint com um
cliente só. O critério para fazer agora seria um terceiro cliente do
`dwnvr-detect`, ou um terceiro parâmetro de imagem - aí o `quadro` solto passa a
confundir de verdade.
