# API HTTP

Referência dos endpoints. A implementação está em
[`internal/api/`](../internal/api/).

Convenções que valem para tudo:

- **Instantes são epoch em milissegundos.** Nunca há string de data-hora, exceto
  no atalho `day=AAAA-MM-DD`, que é interpretado no fuso local do servidor.
- **`cam`** é sempre o id da câmera cadastrada. Câmera desconhecida é erro, não
  resposta vazia.
- Erros vêm como HTTP 4xx com a mensagem em texto.

## Sessão

| Endpoint | O que faz |
|---|---|
| `POST /api/login` | abre sessão; devolve cookie assinado (HMAC, sem estado no servidor) |
| `POST /api/logout` | encerra a sessão |
| `GET /api/session` | **público**: diz se este dwnvr exige login |
| `GET /api/version` | **público**: versão, commit e data do build |

`/api/session` e `/api/version` ficam fora da autenticação pelo mesmo motivo:
precisam ser visíveis antes de entrar. O `/api/version` é também a sonda de
deploy - um `curl` responde se o dwnvr subiu com o código novo, sem cookie.

Todo o resto exige sessão válida.

## Câmeras e diagnóstico

| Endpoint | Parâmetros | O que faz |
|---|---|---|
| `GET /api/cameras` | - | cadastradas + streams do go2rtc + gravações órfãs |
| `POST /api/cameras` | corpo JSON | cadastra ou altera (upsert por id) |
| `DELETE /api/cameras` | `id`, `recordings=1` | descadastra; com `recordings=1` apaga as gravações junto |
| `GET /api/health` | - | bitrate medido, dias estimados, estado do disco, uptimes e relógio |
| `DELETE /api/rec` | `cam` | apaga as gravações; serve também câmera já removida |

`DELETE /api/rec` aceitar câmera já removida é deliberado: descadastrar sem
apagar deixa gravações órfãs, e sem esse endpoint não haveria como recuperar o
espaço pela interface.

O `uptime` do `/api/health` vai em **segundos** (`appSeconds`, `machineSeconds`)
e o `clock` vai em **instante**. A diferença é proposital: duração não tem como
ser mal interpretada por um navegador de fuso ou de relógio diferente, enquanto
o `clock` existe justamente para mostrar a hora de lá - é com ele que se
descobre TZ ou NTP errado na máquina que grava.

```json
"clock": {
  "now": "2026-08-18T18:37:12-03:00",
  "abbr": "-03",
  "offsetSeconds": -10800,
  "zone": "America/Fortaleza"
}
```

`now` já traz o offset do servidor embutido, e a interface o exibe sem
reconverter. `zone` é o nome IANA e some quando o servidor não consegue
descobri-lo (nem `TZ`, nem `/etc/timezone`, nem o link de `/etc/localtime`); aí
resta a `abbr`, que sozinha não identifica região - `-03` vale para São Paulo,
Buenos Aires e outros.

## Gravações

| Endpoint | Parâmetros | Devolve |
|---|---|---|
| `GET /api/rec/days` | `cam` | dias que têm gravação |
| `GET /api/rec/timeline` | `cam` + intervalo | faixas contíguas (para desenhar) + segmentos (para tocar) |
| `GET /api/rec/init` | `cam`, `g` | o init segment daquela geração, `immutable` |
| `GET /api/rec/seg` | `cam`, `t` | os fragmentos do segmento, **sem** o init, `immutable` |
| `GET /api/rec/thumb` | `cam`, `t` | MP4 de 1 frame - o servidor não decodifica nada |
| `GET /api/rec/playlist.m3u8` | `cam` + intervalo | HLS VOD, para VLC/ffplay/Safari |
| `GET /api/rec/export` | `cam` + intervalo | MP4 único emendado, sem transcodificação |

**Intervalo** é `from`+`to` em epoch ms, ou o atalho `day=AAAA-MM-DD`, que
equivale ao dia inteiro em hora local. `to` precisa ser maior que `from`.

**`t`** é o instante inicial do segmento, exatamente como veio da timeline. Ele
não é convertido em caminho de arquivo por conta própria: é procurado no
índice, que é quem sabe onde terminam `ftyp`+`moov` (`io`) e qual o tamanho do
primeiro fragmento (`f0`). Segmento que o dwnvr não gravou não é servido.

**`g`** é a geração do init - o hash truncado. Só hexadecimal de até 32
caracteres é aceito, porque esse valor vira nome de arquivo e aceitar caminho
ali abriria travessia de diretório.

A separação entre `/api/rec/init` e `/api/rec/seg` é o que permite ao player
MSE baixar o init uma vez só e depois pedir apenas mídia. Ver
[arquitetura.md](arquitetura.md#armazenamento).

## Live

| Endpoint | O que faz |
|---|---|
| `GET /api/live/*` | proxy do go2rtc, com a credencial ficando no servidor |

O navegador nunca fala com o go2rtc diretamente. Passar pelo proxy resolve duas
coisas de uma vez: a senha da API do go2rtc não vai para o cliente, e o live
respeita a mesma sessão do resto da interface.

## Por que a interface não exige sessão

Os arquivos da SPA - HTML, CSS e JS - são servidos **sem** autenticação. É só o
app shell, sem dado nenhum de câmera. Protegê-lo impediria o navegador de
carregar a própria tela de login. Tudo que é dado está atrás dos endpoints
acima.
