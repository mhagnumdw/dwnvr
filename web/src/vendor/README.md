# Código de terceiros

## video-rtc.js e video-stream.js

Do [go2rtc](https://github.com/AlexxIT/go2rtc) (`www/`), MIT, © 2022 Alexey Khit.

Copiados sem modificação. São o player de visualização ao vivo: negociam
WebRTC, MSE e MJPEG sozinhos, escolhendo o que o navegador aceita, e já são
batalhados com H265 — que é justamente o caso difícil desta instalação.

Reaproveitá-los em vez de escrever um player de live é deliberado: o dwnvr
escreve o player das *gravações*, onde o formato é dele e o controle fino da
timeline importa. No live, o formato é do go2rtc e o problema já está resolvido.

Para atualizar:

```sh
curl -O https://raw.githubusercontent.com/AlexxIT/go2rtc/master/www/video-rtc.js
curl -O https://raw.githubusercontent.com/AlexxIT/go2rtc/master/www/video-stream.js
```
