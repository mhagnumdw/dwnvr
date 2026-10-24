# TODO - entrar com o e-mail, além do usuário

Pedido em 24/10/2026, enquanto se desenhava o envio do link de convite por
e-mail. Fica para depois dele: o campo `email` da pessoa nasce lá.

**Status: não feito.**

## O que é

Quem tem e-mail cadastrado no `usuarios.json` pode digitar o e-mail no campo
de login, no lugar do usuário. O usuário continua valendo.

## O que já ajuda

- O usuário só aceita `[a-z0-9._-]` (ver `usuarios.ValidarUsuario`). Sem `@`,
  um login com `@` é sempre e-mail, e nunca há dúvida sobre qual dos dois a
  pessoa digitou.
- O limite de tentativas é por endereço, não por usuário (ver
  [Limite de tentativas no login](../configuracao.md#limite-de-tentativas-no-login)).
  Entrar pelo e-mail não abre um caminho novo em volta dele.

## Como implementar

- O e-mail precisa ser único entre as pessoas, sem diferença de maiúscula e
  minúscula: dois iguais não diriam quem está entrando. Melhor exigir isso já
  no envio do convite, quando o campo nasce, para não haver repetido gravado
  quando o login chegar.
- O login acha a pessoa pelo e-mail e segue igual a partir daí: a sessão, o
  último acesso e o log ficam com o usuário, não com o e-mail.
- O dono não tem e-mail (ele está no `dwnvr.yaml`): entra só pelo usuário.
- A resposta de e-mail inexistente é a mesma de senha errada, como já é com o
  usuário: o login não diz quais e-mails existem.
- A tela de login troca o rótulo para "Usuário ou e-mail".

## Vale a pena agora?

Não: depende do campo `email`, que chega com o envio do convite. Depois dele,
é pequeno.
