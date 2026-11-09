# TODO - conferência órfã do Ao vivo derruba a sessão nova

**Status: confirmado lendo o código; não reproduzido na tela.** Achado do
CodeRabbit na terceira revisão do PR do "Tentar agora" (#72); o defeito é
anterior a ele.

## O problema

Uma conferência de sessão do Ao vivo que já está no ar quando a tela sai
(no logout, por exemplo) segue até terminar. Se a resposta for "não
autenticado" e chegar depois de um login novo, ela derruba a sessão nova: a
pessoa acabou de entrar e volta para a tela de login.

Para acontecer, o login novo precisa vir dentro do prazo dessa conferência
(`PRAZO_CONFERENCIA_MS`, 10 s), com a resposta atrasada pela rede.

## Por quê

Em `web/src/routes/Live.svelte`, o `conferirSessao()` espera o
`api.conferirSessao(PRAZO_CONFERENCIA_MS)` e, com `authenticated` falso, chama
`fecharTudo()` e `sessaoCaiu()`. O `sessaoCaiu()`
(`web/src/lib/state.svelte.ts`) zera o `session` global, sem saber de qual
sessão era a resposta.

O PR #72 protegeu a tentativa que ainda não tinha começado: o `telaNoAr` cai no
desmonte e o `tentarDeNovo()` não confere depois disso. A conferência que já
estava em voo não lê o `telaNoAr` na volta.

Já existia antes do PR: a conferência de rotina de 60 s, a do aviso de sessão
e a da queda de uma câmera sempre puderam estar no ar na hora do logout.

## Como implementar

Duas linhas no `conferirSessao()`, logo depois do `await`:

```ts
const r = await api.conferirSessao(PRAZO_CONFERENCIA_MS);
// A tela já saiu: esta resposta pode ser de uma sessão anterior.
if (!telaNoAr) return;
```

O `finally` ainda roda nesse `return`, então o `conferindo` e a contagem se
limpam. O `catch` não precisa da mesma guarda: ele só mexe no estado local da
tela (`acessoConfirmado`, `acessoVoltou`) e no `fecharTudo()`, que com a tela
fora já não tem player.

O `telaNoAr` hoje é declarado depois do `conferirSessao()`; funciona assim
mesmo (a função só lê a variável quando roda), mas vale subir a declaração
para junto do `conferindo`, com o comentário.

Uma alternativa mais firme, se um dia o defeito aparecer em outra tela: um
contador de geração da sessão no `state.svelte.ts`, que o login e o
`sessaoCaiu()` incrementam, e o `sessaoCaiu()` só aceita a queda da geração
que pediu a conferência. Hoje só o Ao vivo confere a sessão por conta própria,
então a guarda local basta.

## Prós e contras

- A favor: fecha a última porta conhecida por onde uma resposta velha derruba
  um login novo. A mudança é pequena e não muda nada do que se vê.
- Contra: o caso exige logout e login novo em menos de 10 s com uma conferência
  presa na rede; quase não acontece no uso real.

## Vale a pena agora?

Sim, num PR próprio e pequeno, logo depois do #72. Ficou fora dele para não
abrir mais uma rodada de revisão do CodeRabbit, que a cada commit leva cerca de
uma hora.
