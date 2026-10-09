# TODO - limite de tentativas no login

Registrado em 07/10/2026, ao medir quanto custaria guardar a senha com hash.
Vai ser feito numa atividade própria.

**Status: não começado.**

## Por quê

O `POST /api/login` não limita tentativas: quem alcança a tela pode testar
senhas sem parar. A
[§Por que não expor o servidor na internet](../acesso-remoto.md#por-que-não-expor-o-servidor-na-internet)
já cita isso.

Desde o cadastro de usuários (09/10/2026), cada tentativa custa 0,77 s de CPU
num Orange Pi Zero 3, a conta da senha em [hash](TODO_hash-da-senha.md),
inclusive a do dono e a de quem não existe. A fila de uma conta por vez impede
que tentativas em paralelo ocupem os núcleos que a gravação e o detector de
objetos usam, mas não limita nada: quem testa senhas sem parar ocupa um núcleo
o tempo todo, e o login das pessoas de verdade espera atrás.

## O que já existe

- A fila de uma conta de senha por vez (`internal/usuarios/senha.go`). Protege
  a CPU, e não limita tentativas.
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
