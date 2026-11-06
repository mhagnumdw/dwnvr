# TODO - vídeo da câmera anterior numa câmera sem gravação

**Status: confirmado lendo o código; não reproduzido na tela.** Achado do
CodeRabbit no PR do deslize das Gravações (#71); o defeito é anterior a ele.

## O problema

Nas Gravações, trocar para uma câmera que não tem gravação no dia escolhido,
ou cuja timeline falha ao carregar, deixa na tela o vídeo da câmera anterior,
debaixo do nome da nova. Se ele estava tocando, continua tocando o que já
estava no buffer.

Vale para qualquer troca: pelas setas ‹ ›, pelo select, pelo deslize e pelas
setas da tela cheia.

## Por quê

Em `web/src/routes/Recordings.svelte`, o `load()` só chama `player.seek()`
quando o dia tem segmento, e é o `seek()` (pelo `#reset` do
`web/src/lib/player.svelte.ts`) que troca o `src` do `<video>`. O
`player.setSource()` troca a lista e invalida a geração, mas não mexe no
`<video>`. No caminho de erro do `load()`, o player nem é chamado.

O aviso "sem gravação neste dia" (ou o erro) aparece, mas por cima da imagem
errada.

## Como implementar

Um método no `Player` que pare e esvazie o `<video>` como o `destroy()` faz
(fechar o `MediaSource`, revogar a URL, `removeAttribute('src')` + `load()`),
sem soltar os ouvintes. O `load()` o chama quando o dia vem sem segmento e no
`catch`.

## Prós e contras

- A favor: a tela deixa de mostrar uma câmera com o nome de outra, e para de
  tocar o que ninguém pediu.
- Contra: o palco fica preto também no caso legítimo de voltar para um dia sem
  gravação na mesma câmera, onde hoje o último quadro fica parado. É o
  correto, mas muda o que se vê.

## Vale a pena agora?

Sim, é pequeno, mas num PR próprio: mexe no player, que este PR do deslize não
toca.
