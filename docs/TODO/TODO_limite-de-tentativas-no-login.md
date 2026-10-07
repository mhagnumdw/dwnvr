# TODO - limite de tentativas no login

Registrado em 07/10/2026, ao medir quanto custaria guardar a senha com hash.
Vai ser feito numa atividade própria.

**Status: não começado.**

## Por quê

O `POST /api/login` não limita tentativas: quem alcança a tela pode testar
senhas sem parar. A
[§Por que não expor o servidor na internet](../acesso-remoto.md#por-que-não-expor-o-servidor-na-internet)
já cita isso.

Hoje cada tentativa custa microssegundos ao servidor, porque a senha é
comparada do jeito que está escrita no `dwnvr.yaml`. Com a senha guardada em
[hash](TODO_hash-da-senha.md), cada tentativa passaria a custar de 0,3 a 0,8 s
de CPU num Orange Pi Zero 3 (medido em 07/10/2026, com a gravação e o detector
de objetos rodando). Sem limite, algumas tentativas em paralelo ocupariam os
núcleos que a gravação e o detector usam.

## O que já existe

- Cada recusa vai para o log, com o usuário tentado e o endereço de origem
  (`tentativa de login recusada`, no `handleLogin` de `internal/api/auth.go`).
- Atrás do `tailscale serve`, toda conexão chega ao dwnvr pelo mesmo endereço,
  o do proxy (a conferir). Um limite por endereço pelo `RemoteAddr` trataria
  todo mundo como uma pessoa só. O endereço real viria do `X-Forwarded-For`,
  que só vale quando quem o põe é o proxy: aceito de qualquer um, cada
  tentativa poderia inventar um endereço novo.

## Decidir antes de implementar

- Limite por endereço, global ou os dois. O global é o único que não depende
  do `X-Forwarded-For`, mas trava também o dono durante um ataque.
- O que recebe quem passa do limite: `429` com `Retry-After`, ou uma espera
  que cresce a cada recusa.
- A ordem com o [hash da senha](TODO_hash-da-senha.md), que também vai ser
  feito: o limite antes, ou o hash já com uma fila de uma verificação por vez.
