# TODO - tiles fora da tela continuam decodificando

Levantado em 11/08/2026, enquanto se implementava o modo "encaixar" da tela ao
vivo. Não quebra nada: gasta bateria e CPU do aparelho de quem está olhando.

**Status: não implementado, por decisão.** A entrega do dia deu o modo encaixar,
em que todos os tiles cabem na janela e portanto estão todos visíveis - ali o
problema não existe. Ele sobra nos modos de coluna fixa, que é onde a grade pode
ficar mais alta que a tela.

## O que acontece

O player do go2rtc sabe pausar um stream que saiu da viewport, e o dwnvr liga
metade do mecanismo. Em `web/src/vendor/video-rtc.js:56-64`:

```js
this.visibilityCheck = true;
this.visibilityThreshold = 0;
```

`visibilityCheck` trata a **aba** em segundo plano e funciona (é o que explica o
player parado que já nos confundiu antes - ver a nota sobre `document.hidden`).
Já `visibilityThreshold = 0` **desliga** o IntersectionObserver: com zero, nenhum
grau de invisibilidade dentro da página faz o componente desconectar. O efeito é
que uma câmera rolada para fora da tela segue com a conexão aberta e o quadro
sendo decodificado.

## Quando dói

Com 9 câmeras selecionadas em `1×` num celular, as 9 decodificam enquanto só uma
aparece. O aviso `⚠ N streams simultâneos` na barra existe justamente porque a
decodificação é do aparelho, não do Pi - e nesse caso o custo é pago sem nenhum
benefício.

## O que seria preciso

Passar um `visibilityThreshold` maior que zero nos tiles (o `{@attach}` em
`web/src/routes/Live.svelte` já configura `mode` e `src`, seria mais uma linha) e
verificar duas coisas antes de dar por resolvido:

1. **A carência de reconexão.** `DISCONNECT_TIMEOUT` é de 5s
   (`video-rtc.js:21`); rolagem rápida não pode virar uma sequência de
   desconectar/reconectar que deixe o vídeo preto ao parar de rolar.
2. **O modo encaixar não pode regredir.** Ali todos os tiles estão visíveis, mas
   um tile de 98px de altura com o limiar mal escolhido pode ser considerado
   fora da tela por arredondamento e desligar sozinho.
