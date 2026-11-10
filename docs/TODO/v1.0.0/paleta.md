# Paleta customizável

**Status (10/11/2026): planejado.** Depende da Etapa 3 ([temas](temas.md)).

## O que é

Quem instala troca as cores do dwnvr para combinar com outra aplicação (a do
painel da casa, a de um provedor), sem build. A ideia do mantenedor: um comando
gera a paleta, e ela fica num lugar de onde a aplicação a lê.

## Por que depois dos temas

A paleta só troca o que for token. É a Etapa 3 que põe toda cor da tela num
token, e é nela que se decide quantas cores-base existem e quais saem delas por
`color-mix()`. Quanto mais o CSS deriva sozinho, menos o comando precisa gerar.

## O desenho macro

A decidir na etapa:

- **O contrato são os tokens.** Com a paleta publicada, o nome de um token vira
  contrato: renomeá-lo depois quebra a paleta de quem a escreveu. Por isso eles
  entram na promessa da 1.0.0 (ver [o plano](plano.md#o-que-a-100-promete)),
  já em en-US. Hoje o único nome em português é o `--ativo-fundo`.
- **Onde a paleta mora:** um arquivo na pasta de configuração (`/etc/dwnvr`,
  que o compose já monta como pasta), lido pelo servidor e entregue junto com a
  tela. Sem o arquivo, nada muda e nenhum pedido a mais acontece: custo zero
  desligado.
- **Como ela se gera:**

  | Caminho | Como | A favor | Contra |
  | --- | --- | --- | --- |
  | Só o arquivo | escrito à mão, com a lista dos tokens documentada | nada a manter além da doc | cada token à mão, nas quatro paletas |
  | Comando | uma flag do binário, no estilo do `-healthcheck` e do `-hash-password` planejado em [TODO_hash-da-senha.md](../TODO_hash-da-senha.md), recebe de uma a três cores e escreve o arquivo com as quatro paletas derivadas em OKLCH, com o contraste conferido | é o que foi pedido, e se refaz com outra cor num comando | roda por `docker compose exec`, que quem não é técnico não conhece |
  | Na tela global | seletor de cor com prévia ao vivo, na tela da Etapa 6 | o mais fácil para quem não é técnico | depende da Etapa 6, e é o caminho mais caro |

  Recomendo o arquivo e o comando nesta etapa, e a tela só se fizer falta no
  uso.

- **Até onde a paleta chega:** a interface. O e-mail de alerta e o ícone (o
  favicon e os PNG do app instalado) ficam de fora, a não ser que se decida o
  contrário.
