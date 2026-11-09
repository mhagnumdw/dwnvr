# TODO - Câmeras diz "nenhuma câmera cadastrada" quando a lista não carrega

**Status: confirmado lendo o código, não reproduzido na tela.**

## O problema

A tela Câmeras mostra o estado vazio quando a lista está vazia e não está
carregando (`web/src/routes/Cameras.svelte`, o `{#if !cameras.list.length &&
!cameras.loading}` antes do `<SemCameras>`). Se o `/api/cameras` falha na
primeira carga, com o servidor fora do ar por exemplo, a lista continua vazia
e a tela afirma "Nenhuma câmera cadastrada ainda", que é falso.

O `loadCameras` (`web/src/lib/state.svelte.ts`) guarda o motivo em
`cameras.error`, mas a tela Câmeras nunca o mostra (Detecções usa o campo).
Quando a falha é só do `/api/cameras`, com a saúde respondendo, o rodapé da
saúde também não avisa nada.

Só acontece na primeira carga: depois que a lista veio, uma falha ao recarregar
não a apaga.

Achado no review do PR do rodapé da saúde (branch `ultima-leitura`), fora do
escopo dele.

## Como implementar

No lugar do estado vazio, mostrar o erro quando ele existir:

```svelte
{#if cameras.error}
  <p class="card errbox small">{cameras.error}</p>
{:else if !cameras.list.length && !cameras.loading}
  <SemCameras link={false} />
{/if}
```

Conferir antes se o `errbox` em card é o que as outras telas usam para erro de
carga, e se o texto cru do erro serve para quem lê ou se vale uma frase como
"Não deu para carregar as câmeras".

## Prós

- A tela para de afirmar algo falso no cenário em que a pessoa mais precisa
  saber o que está acontecendo.
- Pouco código, sem requisição nem timer novo.

## Contras

- Mais um bloco no topo da tela, que só aparece em falha.

## Vale a pena agora?

Vale fazer num PR pequeno, à parte. É raro (só a primeira carga, e só com o
servidor fora), mas quando acontece leva o admin a achar que perdeu o cadastro.
