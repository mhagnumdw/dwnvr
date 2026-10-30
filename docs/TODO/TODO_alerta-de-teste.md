# TODO - um alerta de teste na aba Alerta da câmera

**Status: desenhado e adiado (27/10/2026).** O mantenedor achou preciosismo
para o momento. Saiu da Etapa 5 (o e-mail) de
[`TODO_areas-e-alertas-de-deteccao.md`](TODO_areas-e-alertas-de-deteccao.md),
em que tinha sido decidido no mesmo dia.

## O que seria

Um botão "Enviar um alerta de teste" na aba Alerta do cadastro da câmera,
dentro do bloco "Para onde vai", abaixo dos endereços. Ele manda um e-mail de
teste para a lista `alerts.email.to`, com "[teste]" no assunto e o quadro de
agora da câmera anexo, com as áreas do alerta desenhadas. Não vira alerta nem
marca nada em Detecções. A tela não mostra o quadro, só o resultado embaixo do
botão: "Enviado às 14:32 para 2 endereços", ou a resposta do servidor do jeito
que ela veio.

Hoje, quem quer conferir o envio tem o "Testar o e-mail" do card Este servidor
(`POST /api/email/teste`, do PR #50), que confere a seção `smtp`, mas não a
lista nem o formato do alerta.

## O que o código diz

**Não existe um "quadro de agora" da câmera.** O único JPEG do dwnvr é o que o
sidecar devolve ao olhar um pedaço, e esse caminho passa pela fila e custa de
3,6 a 6,5 s de detector de objetos no hardware de referência. As duas saídas:

- **(a) O `frame.jpeg` do go2rtc** (`/api/frame.jpeg?src=<câmera>`), pelo
  `go2rtc.Client` que o dwnvr já usa. É o quadro de agora; o go2rtc da imagem
  oficial já traz o ffmpeg. Sem ffmpeg (um go2rtc externo) ou com a câmera fora
  do ar, o e-mail sai sem anexo e diz o motivo. Falta conferir se o 1.9.14
  aceita reduzir para 480 px pelo parâmetro. Era a recomendada.
- **(b) O último quadro olhado da câmera**, em `quadros/`. Não pede nada novo,
  mas pode ter dias, e a câmera sem detecção fica sem imagem.

## O desenho, com a (a)

- **`POST /api/alertas/teste {camera}`**, só do admin. Pega o quadro de agora,
  desenha o contorno das áreas do alerta salvas da câmera, sem caixas, e manda
  pelo meio `email`, pelo `Envia` do contrato `Notificador`. O `Alerta` ganha um
  `Teste bool`, e a `mensagemDoAlerta` escreve "[teste] Alerta de Garagem às
  14:32" e um corpo que diz que é teste: um meio novo testa pelo mesmo caminho,
  sem método novo no contrato.
- **Não depende da Central**, que só nasce com a primeira câmera de alerta
  ligado: a rota monta o meio na hora, a partir do `cfg`.
- **A resposta** é `{sentTo, sentAt, frameError?}`. O erro do servidor de
  e-mail volta em 502, do jeito que veio, como no `/api/email/teste`.
- **Fora do teto por hora, dos números do meio e do Aviso de meio falhando:**
  eles contam alertas. Com o meio falhando, o vermelho segue e o "Enviado"
  aparece embaixo dele.
- **Na tela:** desligado em câmera ainda não salva ("Salve a câmera para
  testar"); some sem lista ou sem e-mail; o resultado embaixo do botão, na hora
  do servidor, com "sem o quadro: o motivo" quando o anexo não veio.

**Repercussões:** `docs/api.md` e `web/src/lib/api.ts` (a rota nova), testes em
`internal/api` (com o go2rtc e o `Enviador` falsos) e em `internal/alerta` (a
mensagem de teste).
