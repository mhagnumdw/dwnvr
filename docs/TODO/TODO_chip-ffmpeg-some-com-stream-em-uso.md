# TODO - o chip "ffmpeg" some quando o stream está em uso

Levantado em 24/09/2026, ao comparar o `go2rtc.yaml` com o `/api/streams` para o
aviso de "reinicie o go2rtc". Não quebra gravação: é uma **informação que some**
da tela.

**Status: não implementado.**

## O que acontece

`Producer.Transcoding()` (`internal/go2rtc/client.go`) responde "tem ffmpeg" pelo
prefixo `ffmpeg:` do `url` do produtor. Só que o go2rtc só preenche esse `url`
com o stream **ocioso**. Com alguém consumindo, uma fonte `ffmpeg:` vira um
produtor sem `url`, e o que vem é o comando expandido no campo `source`:

```
ocioso:  "url": "ffmpeg:virtual?video=testsrc&size=640x360&rate=15#video=h264"
em uso:  "source": "exec:ffmpeg -hide_banner ... -f rtsp rtsp://127.0.0.1:8554/..."
```

Medido no go2rtc 1.9.14 local. RTSP em uso mantém o `url`, então só as fontes
`ffmpeg:` e `exec:` são afetadas.

## O que isso causa

Na tela de Câmeras, o chip "ffmpeg" de um stream de "Disponíveis no go2rtc"
some enquanto outra instalação (ou a tela Ao vivo) o está consumindo. O
Diagnóstico (`Health.svelte`, que lê `cameras.streams`) perde a mesma
informação nas câmeras cadastradas, e essas estão **sempre** em uso. É
justamente o caso em que o aviso importa: ffmpeg transcodificando é o que o
projeto quer evitar num hardware limitado.

## Caminho provável

Ler também o `source` no `Producer` e tratar `exec:ffmpeg` como transcodificação.
Conferir antes se o `exec:` de uma fonte que o usuário escreveu como `exec:` (a
`cam_teste5` do exemplo) deve contar: ela também é ffmpeg.
