# TODO - pular 10 s e pausar pelo teclado nas gravações

Planejado em 17/10/2026. **Status: implementado e testado no celular e no
desktop em 17/10/2026.**

O objetivo é avançar e voltar 10 segundos no player das gravações, como no
YouTube: no celular, por duplo toque nas laterais do vídeo; no desktop, pelas
setas do teclado. O espaço passa a tocar e pausar.

## Como fica

### Celular (toque)

O vídeo é dividido em três terços:

| Onde | Toque simples | Duplo toque | Toques seguidos |
| --- | --- | --- | --- |
| terço esquerdo | mostra os controles (como hoje); **não pula** | **−10 s** | cada toque a mais soma −10 s |
| terço do meio | mostra os controles | **liga ou desliga a tela cheia** (como hoje) | - |
| terço direito | mostra os controles (como hoje); **não pula** | **+10 s** | cada toque a mais soma +10 s |

Como no YouTube: 2 toques = 10 s, 3 toques = 20 s, 4 toques = 30 s.

Vale dentro e fora da tela cheia.

### Desktop (mouse e teclado)

- O duplo clique em qualquer ponto do vídeo liga e desliga a tela cheia, sem
  mudança.
- **←** volta 10 s e **→** avança 10 s. Segurar a tecla repete e acumula.
- **Espaço** toca e pausa.
- As teclas valem **sempre** na tela Gravações, e não só na tela cheia, como
  no YouTube. É diferente do Ao vivo, onde as setas trocam de câmera só na
  tela cheia.

### Fora do escopo

- O player da folha de detecção: os trechos são curtos.
- O Ao vivo: não tem para onde voltar.

## Decisões

1. **Duplo toque no meio:** liga e desliga a tela cheia, como hoje.
2. **Toques seguidos:** somam 10 s cada, como no YouTube.
3. **Teclas fora da tela cheia:** valem sempre na tela Gravações.
4. **Tamanho do pulo:** 10 s fixos, numa constante comentada do player, sem
   configuração.
5. **Retorno visual:** a meia-lua do YouTube (variante A do mock), escolhida em
   17/10/2026 entre quatro variantes.

## O que existe hoje

- **Duplo clique:** é o `ondblclick={alternarCheia}` no `.stage`
  (`web/src/routes/Recordings.svelte`). Ele vale para o mouse e para o toque. No
  celular, só funciona porque o Chrome do Android gera `dblclick` a partir de
  dois toques.
- **Toque simples:** o vídeo não reage a ele. Na tela cheia, o
  `onpointerdowncapture` do `.cheia` mostra os controles.
- **Teclado:** a tela Gravações não ouve nenhuma tecla. O Ao vivo
  (`Live.svelte`) e a folha de detecção (`DeteccaoFolha.svelte`) usam as setas,
  mas nenhum dos dois fica aberto junto com Gravações.
- **Seek:** o `player.seek(ms)` (`web/src/lib/player.svelte.ts`) **sempre**
  recria o `MediaSource`. O `#reset` baixa de novo o init e os segmentos, e o
  "carregando…" acende. Fazer isso a cada ±10 s faria o vídeo piscar e
  carregar a cada toque.

## Como implementar

### 1. Player: um pulo curto que não recria o `MediaSource`

Um método novo no `Player`, `pular(deltaMs)`, mais a constante `PULO_RAPIDO_MS
= 10000` junto das outras do topo do arquivo.

Não foi preciso guardar um "alvo pendente". Durante um seek, o `currentMs` já
é o alvo dele, porque o portão do `#seeking` o segura lá. Fora de um seek, o
`currentTime` muda na hora. Nos dois casos, o pulo seguinte parte de onde o
anterior mandou ir.

- **Usa o buffer que já está no navegador.** O player mantém 15 s para trás
  (`BEHIND_S`) e pelo menos 25 s à frente (`AHEAD_S`). Na maior parte das
  vezes, os ±10 s já estão no buffer: basta mudar o `video.currentTime`, sem
  rede e sem "carregando…".
- **Cai no `seek(ms)` quando não dá.** Isso acontece quando o alvo está fora
  do `buffered` (logo depois de um seek, ou mais longe que o buffer), quando
  cai num buraco da gravação, ou quando passa de uma `#boundary` (a troca de
  geração, que exige recriar o `MediaSource`). O `seek` já sabe pular buracos.
- **Fica preso aos limites do dia.** O pulo para no `firstMs` e no `lastMs`.
- **Acumula a partir do último alvo.** Três setas rápidas dão 30 s; três
  toques, 20 s.
- **Não depende de estar tocando.** Com o vídeo pausado, o pulo muda a posição
  e o vídeo continua pausado. Quando o pulo cai no `seek`, que termina em
  `play()`, o vídeo é pausado de novo depois dele.
- **No fim do dia, para 1 s antes do fim.** Pousar exatamente no fim faria o
  `seek` voltar ao começo do último segmento.

### 2. Celular: um detector de duplo toque próprio

O módulo `web/src/lib/toqueDuplo.ts`, sem estado de Svelte e com a regra
inteira num lugar só:

- Ouve `pointerup` no `.stage`, só com `pointerType === 'touch'`.
- Dois toques em menos de ~300 ms e perto um do outro formam um duplo toque. O
  terço onde caiu o **segundo** toque decide a ação.
- Depois de um duplo toque numa lateral, cada toque a mais **no mesmo lado**,
  dentro da mesma janela, soma mais um pulo. A janela se renova a cada toque.
  Um toque no outro lado ou no meio encerra a sequência.
- Toque num botão sobre o vídeo (o de tela cheia, por exemplo) não conta: o
  detector ignora alvos com `closest('button')`.
- Os tempos (300 ms e 48 px entre os toques, 700 ms de janela da sequência)
  ficam em constantes comentadas no topo do módulo.
- O `.stage` ganhou `touch-action: manipulation`, para o navegador não usar o
  duplo toque como zoom.

O `ondblclick` do `.stage` continua, mas **só para o mouse**: ele ignora o
evento quando o último `pointerdown` foi de toque. Sem isso, o Chrome do
Android geraria um `dblclick` depois do duplo toque na lateral, e o vídeo
pularia e entraria na tela cheia ao mesmo tempo.

O detector próprio também deixa de depender do `dblclick` sintético do
navegador para o toque. O Safari do iPhone não o gera de forma confiável, mas
lá as gravações nem tocam hoje
([`TODO_gravacoes-no-iphone.md`](TODO_gravacoes-no-iphone.md)).

### 3. Teclado

Um `<svelte:window onkeydown>` em `Recordings.svelte`:

- **←/→:** `player.pular(∓PULO_RAPIDO_MS)`, com `preventDefault`. A repetição
  de tecla segurada passa pelo mesmo caminho e acumula.
- **Espaço:** `player.toggle()`, com `preventDefault`. Sem ele, a página rola
  para baixo. Segurar o espaço não repete.
- **Não age** quando:
  - o foco está num `input`, `select`, `textarea` ou elemento editável. Assim
    o campo de hora do `Relogio` e os selects de câmera e velocidade continuam
    funcionando como sempre;
  - o evento já veio com `defaultPrevented`. É o caso das setas no calendário
    do dia (`DayPicker`), que andam pelos dias;
  - há um modificador junto (Ctrl, Alt, Meta), para não roubar atalhos do
    navegador.

  A tela Gravações não abre nenhum `Modal`, então não há checagem de modal.
- **Foco preso no select:** depois de escolher a velocidade ou a câmera, o
  foco fica no `<select>`, e a seta seguinte mudaria a opção em vez de pular.
  Resolve-se com `blur()` no `onchange` desses dois selects.
- **Espaço com o foco num botão:** o navegador "clica" o botão no espaço.
  - **Dentro do player** (o `.cheia`: ▶, fixar, tela cheia), o espaço toca e
    pausa, e o `preventDefault` no `keydown` impede o clique do botão. Sem
    ele, com o foco no ▶, o vídeo alternaria duas vezes e ficaria como estava.
    Medido no Chrome em 17/10/2026, numa página de teste: o botão recebeu 1
    clique sem o `preventDefault` e 0 com ele. **O Firefox não foi testado.**
  - **Fora do player** (os botões de câmera, o dia, o calendário), o espaço
    continua sendo do botão.

### 4. Retorno visual

Sem retorno, o duplo toque parece não ter funcionado, ainda mais quando cai no
`seek` e o "carregando…" acende.

Uma **meia-lua do lado do pulo**, como no YouTube: um arco claro e
translúcido colado na borda esquerda ou direita do vídeo, cobrindo ~40% da
largura, com "◀◀◀" ou "▶▶▶" e "10 segundos" no meio. O texto soma nos toques
seguidos ("30 segundos"), e a meia-lua some ~700 ms depois do último pulo. A
mesma meia-lua aparece nas setas do desktop. O espaço não ganha retorno: o
▶/⏸ dos controles já muda.

Escolhida em 17/10/2026 entre quatro variantes, num mock HTML local. As
outras eram uma pílula na lateral, um aviso no topo e um ícone circular com o
número.

## Como conferir

O `web/` não tem teste automatizado de front-end. A conferência é manual, com
`npm run check` e o lint verdes antes.

- **Desktop:**
  - ←/→ dentro e fora da tela cheia, tocando e pausado;
  - tecla segurada;
  - perto do começo e do fim do dia;
  - através de um buraco da gravação;
  - espaço com o foco no ▶, no "fixar os controles" e em lugar nenhum;
  - setas e espaço com o foco no campo de hora e nos selects (não pulam);
  - o duplo clique continua só alternando a tela cheia.
- **Celular** (build + `vite preview`, sem o dev server):
  - duplo toque em cada terço, dentro e fora da tela cheia;
  - três e quatro toques seguidos numa lateral;
  - troca de lado no meio da sequência;
  - toque simples continua só mostrando os controles;
  - o duplo toque na lateral não entra na tela cheia.
- **Sem recarregar:** nos pulos dentro do buffer, o "carregando…" não acende e
  a aba Network do DevTools não mostra segmento novo.
