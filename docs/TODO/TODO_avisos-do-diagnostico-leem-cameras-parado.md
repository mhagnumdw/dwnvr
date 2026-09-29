# TODO - avisos do Diagnóstico que leem o `cameras` parado

Levantado em 28/09/2026, ao pôr o "desde quando" nos avisos da tela Diagnóstico.
Não quebra gravação: é um **aviso que fica velho** na tela.

**Status: não implementado.**

## O que acontece

O `Health.svelte` monta os avisos com dois estados de origens diferentes:

- `health`, do `GET /api/health`, relido a cada `HEALTH_POLL_MS` pelo
  `pollHealth()` (`web/src/lib/state.svelte.js`);
- `cameras`, do `GET /api/cameras`, que o `loadCameras()` busca ao abrir a
  interface e, na tela Câmeras, depois de cada ação e quando a aba volta a
  ficar visível. A tela Diagnóstico nunca o recarrega.

Dois avisos dependem do segundo:

- **transcodificação** ("usa uma fonte ffmpeg no go2rtc"), que lê
  `cameras.streams`;
- **áudio configurado sem trilha**, que lê `cameras.list` para saber o `audio`
  de cada câmera, e cruza com o `hasAudio` do `health`, que é atual.

Se o `go2rtc.yaml` muda e o go2rtc é reiniciado por fora da interface, o aviso
de transcodificação continua no Diagnóstico como estava na última leitura, até
alguém passar pela tela Câmeras ou recarregar a página.

O aviso "go2rtc inacessível" tinha o mesmo defeito e foi corrigido junto com o
"desde": passou a vir do `/api/health` (campo `go2rtc`).

## Caminho provável

Para o áudio, o `/api/health` já traz a câmera; basta mandar também o `audio`
configurado no `recorder.Status` e deixar de cruzar com o `cameras.list`.

Para a transcodificação, que depende de consultar o go2rtc, as opções são:
recarregar o `cameras` junto do polling do Diagnóstico, ou levar o `transcoding`
por câmera para o `/api/health`. A segunda custa uma chamada ao `/api/streams`
do go2rtc a cada leitura da saúde, e deve ser medida antes. O `Transcoding()`
já lê o `source` do produtor, então acerta com a câmera gravando, que é o caso
do Diagnóstico.
