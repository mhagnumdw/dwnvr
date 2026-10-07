# TODO - as gravações não tocam no iPhone

Achado em 05/10/2026, lendo o código durante o estudo do acesso remoto e do
app instalável. **Não foi testado num iPhone.**

**Status: não corrigido.** O acesso remoto e o app instalável, que vieram
antes, estão em [`acesso-remoto.md`](../acesso-remoto.md).

## O que foi confirmado, pelo código

O player das gravações (`web/src/lib/player.svelte.js`) usa o `MediaSource`
direto: `MediaSource.isTypeSupported(mime)` e `new MediaSource()`, no
`#reset`. No iPhone esse objeto não existe. O Safari do iPhone nunca teve o
Media Source Extensions; do iOS 17.1 em diante ele tem só o
`ManagedMediaSource`, uma variante com outro nome e regras próprias. O Chrome
do iPhone usa o mesmo motor do Safari, então dá no mesmo.

Na prática, a primeira chamada lança `ReferenceError`, e a tela de gravações
não toca nada.

O ao vivo não tem o problema: o player do go2rtc (`web/src/vendor/video-rtc.js`)
procura o `ManagedMediaSource` antes do `MediaSource`.

O Diagnóstico ainda engana: `web/src/lib/navegador.js` conta o
`ManagedMediaSource` como "MediaSource: sim", e o iPhone aparece capaz de
tocar o que o player das gravações não consegue.

No iPad o `MediaSource` existe desde o iPadOS 13, então lá as gravações devem
tocar. Também não foi testado.

## Prós

- O dwnvr é público, e o iPhone é o celular de muita gente: hoje essas pessoas
  veem o ao vivo e não veem nenhuma gravação.
- O defeito não avisa. A tela abre, a timeline aparece, e o vídeo fica parado.

## Contras

- **Não é troca de nome.** O `ManagedMediaSource` só funciona com
  `disableRemotePlayback = true` no `<video>` (ou com uma fonte alternativa de
  AirPlay), e o navegador decide quando aceitar dados: avisa por
  `startstreaming` e `endstreaming`, e pode despejar o buffer sozinho. O
  player das gravações gerencia o buffer à mão (fronteira de trilha, 8×,
  seek), e cada um desses caminhos precisa de teste.
- **O H265 pode ser um segundo muro.** As câmeras em H265 gravam com a caixa
  `hev1` (ver [`go2rtc-h265-parameter-sets.md`](../go2rtc-h265-parameter-sets.md)),
  e os players da Apple são conhecidos por exigir `hvc1` para tocar arquivo
  MP4. Não se sabe se o `ManagedMediaSource` e o HLS do Safari no iPhone
  aceitam o `hev1`; conferir antes de escolher o caminho abaixo.
- Cada tentativa depende de alguém com um iPhone na mão.

## Como implementar, se um dia valer

1. No player, `window.ManagedMediaSource ?? window.MediaSource`, e
   `video.disableRemotePlayback = true` quando for o `ManagedMediaSource`.
2. Respeitar o `startstreaming`/`endstreaming`: só fazer append entre os dois.
3. No Diagnóstico, dizer qual dos dois o navegador tem.
4. Testar num iPhone: tocar, buscar, 8×, atravessar uma fronteira de trilha.

Alternativa mais barata e pior: no iPhone, tocar pela playlist HLS que o
servidor já gera (`GET /api/rec/playlist.m3u8`), que o Safari sabe tocar sem
JavaScript. Não mexe no player, mas perde o controle fino que é a razão de o
player existir, e esbarra na mesma dúvida do `hev1`.

## Vale a pena agora?

**Não.** As gravações pelo iPhone não são prioridade. Quando entrar, o critério para fechar é tocar, buscar e acelerar
num iPhone de verdade.
