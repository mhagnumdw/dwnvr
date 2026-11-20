# API HTTP

Referência dos endpoints. A implementação está em
[`internal/api/`](../internal/api/).

Convenções que valem para tudo:

- **Instantes são epoch em milissegundos.** Nunca há string de data-hora, exceto
  no atalho `day=AAAA-MM-DD`, que é interpretado no fuso local do servidor.
- **`cam`** é sempre o id da câmera cadastrada. Câmera desconhecida é erro, não
  resposta vazia. O id não tem caminho (`/`, `\`, `:`) nem vírgula, que separa
  os ids nas listas (`cams=a,b`): o `POST /api/cameras` o recusa com `400`.
- Erros vêm como HTTP 4xx com a mensagem em texto.

## Sessão

| Endpoint | O que faz |
| --- | --- |
| `POST /api/login` | abre sessão; devolve cookie assinado (HMAC, sem estado no servidor) e quem entrou |
| `POST /api/logout` | encerra a sessão |
| `GET /api/session` | **público**: diz se este dwnvr exige login e, com sessão, quem está logado |
| `GET /api/session/ws` | o aviso de sessão: websocket mudo que o servidor fecha quando a pessoa é derrubada |
| `GET /api/version` | **público**: versão, commit e data do build |
| `POST /api/invite/check` | **público**: diz de quem é o link de convite e até quando vale |
| `POST /api/invite` | **público**: define a senha pelo link de convite e já abre a sessão |

`/api/session` e `/api/version` ficam fora da autenticação pelo mesmo motivo:
precisam ser visíveis antes de entrar. As duas do convite também, porque quem
abre o link ainda não tem senha (ver [O link de convite](#o-link-de-convite)). O `/api/version` é também a sonda de
deploy - um `curl` responde se o dwnvr subiu com o código novo, sem cookie.

Todo o resto exige sessão válida, e as rotas marcadas **só admin** exigem
também o papel de admin.

### Quem entra

- **O dono**: o `server.username` e o `server.password` do `dwnvr.yaml`.
  Sempre admin. A senha dele só se troca no arquivo.
- **As pessoas do `users.json`**, na pasta de config, cada uma com o papel
  gravado: `admin` ou `viewer`.

O viewer vê as câmeras, as gravações e as detecções. O que é da tela
Câmeras e do diagnóstico do servidor ele recebe como `403`, mesmo sem a aba
na tela: esconder a aba não é segurança.

Sem credencial no `dwnvr.yaml` a autenticação fica desligada, as pessoas do
`users.json` são ignoradas, e quem alcança a tela é tratado como admin.

```json
{
  "authRequired": true,
  "authenticated": true,
  "user": { "username": "maria", "name": "Maria", "role": "viewer", "owner": false, "avatar": "4d1e0b62a7c3f915" }
}
```

O `user` só vem com `authenticated: true`; com a autenticação desligada, ele
vem com `"role": "admin"` e `"owner": true`, sem `username` nem `name`. O
`avatar` é o id da foto (ver [Minha conta](#minha-conta)) e só vem com foto.
O `POST /api/login` devolve o mesmo `user`, junto com `"ok": true`.

### O login

O `username` do `POST /api/login` aceita o usuário ou o e-mail da pessoa, sem
diferença de maiúscula e minúscula no e-mail. O usuário do cadastro não tem
`@`, então com `@` é e-mail. Entrar pelo e-mail é entrar como a pessoa: o
`user` da resposta, a sessão, o último acesso e o log ficam com o usuário. O
dono não tem e-mail e entra só pelo usuário, que no `dwnvr.yaml` pode ter `@`:
o dele vale antes de procurar um e-mail. O e-mail que não acha ninguém, inclusive o
repetido no `users.json` editado à mão, responde como a senha errada:
`401` com `{"error": "usuário ou senha inválidos"}`.

Toda tentativa custa uma conta de senha de 0,77 s num Orange Pi Zero 3
(PBKDF2-SHA256, 600.000 iterações), uma por vez no processo inteiro: algumas
tentativas simultâneas não ocupam os núcleos da gravação, só esperam a vez. O
login do dono, cuja senha está em texto no `dwnvr.yaml`, e o de um usuário que
não existe pagam a mesma conta, para o tempo de resposta não dizer quem
existe.

Cada endereço tem um limite de tentativas, ligado por padrão e desligado com
`server.loginAttemptsPerMinute: 0` (ver
[Limite de tentativas no login](configuracao.md#limite-de-tentativas-no-login)).
Passado dele, a resposta é `429`, antes de qualquer conta de senha, com o
`Retry-After` em segundos e o mesmo número no erro, como em
`{"error": "muitas tentativas; tente de novo em 30 s"}`. O número é o que
falta para a próxima tentativa passar: depende do `loginAttemptsPerMinute` e
diminui com o tempo. A tentativa recusada assim não conta: insistir durante a
espera não a estica.

### O cookie

O cookie diz de quem é a sessão: `<usuário em base64>.<validade>.<hmac>`. A
sessão vale 30 dias e se renova com o uso: quando falta menos da metade do
prazo, qualquer resposta autenticada, e também a do `/api/session`, traz um
cookie novo com os 30 dias inteiros. Na prática, os 30 dias contam da última
vez que a tela falou com o dwnvr. Continua sem estado no servidor: renovar é só
assinar outro prazo.

A chave que assina o cookie de cada pessoa sai do `.session-secret` junto com
o usuário e a senha dela: a do `dwnvr.yaml`, para o dono, ou a guardada no
`users.json`. Trocar a senha de alguém derruba só as sessões dessa pessoa,
e o mesmo vale para quem é removido ou ganha link novo, que apaga a senha. Para
o dono, isso pede reiniciar, porque o `dwnvr.yaml` só é lido ao subir. Quem
troca a própria senha pela Minha conta recebe um cookie novo na mesma resposta
e continua; os outros aparelhos dela voltam para o login. Para derrubar todo
mundo de uma vez, ver
[Trocar a senha e derrubar as
sessões](operacao.md#trocar-a-senha-e-derrubar-as-sessões).

### O link de convite

Quem entra pela primeira vez entra por um link: o admin cria a pessoa na
tela Usuários, e ela define a própria senha. Com o e-mail ligado, o link pode
ir direto para ela (ver [Usuários](#usuários)). O link é
`<endereço>/#invite?token=<token>`, com 32 bytes aleatórios no token. O
endereço é o pelo qual o admin está acessando. O token fica no fragmento da
URL, que não sai do navegador, e vai ao servidor no corpo de um POST: não
aparece em log do dwnvr nem de proxy nenhum. O `users.json` guarda só o
SHA-256 dele. Mandado por e-mail, o link fica guardado na caixa de saída da
conta que envia e na de entrada da pessoa: como vale minutos e uma vez só, o
que sobra lá é link morto.

O link vale 10 minutos (`users.InviteValidity`) e uma vez só. Cada pessoa
tem um link por vez: gerar outro invalida o anterior. Vencido, usado,
substituído ou inventado, a resposta é a mesma, `410`.

```json
// POST /api/invite/check   {"token": "..."}
{ "username": "maria", "name": "Maria", "inviteExpiresAtMs": 1791587903000, "minPasswordLength": 8, "maxPasswordLength": 128 }
```

O `POST /api/invite` recebe `{"token", "password"}` e devolve o mesmo
`{"ok": true, "user": {...}}` do login, com o cookie. A senha vai de 8 a 128
caracteres, sem regra de composição (NIST SP 800-63B); fora disso, `400` com o
motivo, e o link continua valendo. A conta da senha espera a mesma fila do
login.

### A queda de quem é derrubado

Remover a pessoa, gerar link novo para ela ou ela trocar a própria senha muda
a credencial que assina os cookies dela, e toda requisição seguinte é
recusada. As conexões que já estão
abertas, e não voltam a apresentar o cookie, o servidor fecha na hora: o
websocket do ao vivo e o aviso de sessão (`GET /api/session/ws`).

Fechar o websocket do ao vivo corta o vídeo do MSE, que vem por dentro dele. O
do WebRTC não: o player fecha o websocket da negociação assim que o vídeo toca,
e a mídia segue direto do go2rtc ao navegador pela UDP, sem passar pelo dwnvr.
O go2rtc 1.9 não tem rota para derrubar um consumidor; só reiniciá-lo, o que
pararia todas as câmeras e a gravação. Quem fecha o WebRTC é a tela Ao vivo:

- ao ver o aviso de sessão fechar, ela confere a sessão na hora, e, se não
  valer, fecha o vídeo e vai para o login;
- com o vídeo à vista, ela confere a sessão a cada 60 s, a rede de segurança se
  o aviso se perder;
- se o dwnvr não responder à conferência, o vídeo para até a sessão se
  confirmar. Isso vale também quando o dwnvr reinicia: o ao vivo para por
  alguns segundos, mesmo com o go2rtc no ar.

Medido com o go2rtc 1.9.14 e o Chrome: o consumidor WebRTC sai do go2rtc uns
40 ms depois de o admin remover a pessoa. Uma tela modificada por quem foi
removido, que ignore o aviso, segue recebendo o vídeo já aberto até a conexão
cair, mas não abre outra.

## Câmeras e diagnóstico

| Endpoint | Parâmetros | O que faz |
| --- | --- | --- |
| `GET /api/cameras` | - | cadastradas + streams do go2rtc + gravações órfãs + para onde vai o alerta; para o viewer, só as câmeras (do `alert`, só o `enabled` quando ligado) e o `detector` |
| `POST /api/cameras` | corpo JSON | **só admin**: cadastra ou altera (upsert por id); o campo que a câmera não definia e chega igual ao default não é gravado (ver [Política por câmera](configuracao.md#política-por-câmera)) |
| `DELETE /api/cameras` | `id`, `recordings=1` | **só admin**: descadastra; com `recordings=1` apaga as gravações junto |
| `GET /api/streams/probe` | `src` | **só admin**: diz se um stream entrega áudio, abrindo-o se preciso |
| `POST /api/go2rtc/restart` | - | **só admin**: reinicia o go2rtc, para ele reler o `go2rtc.yaml`; todas as câmeras param por alguns segundos |
| `GET /api/health` | - | **só admin**: bitrate medido, dias estimados, áudio e transcodificação por câmera, estado do disco, uptimes, relógio, o problema da seção `smtp` e, com alerta ligado, o total de alertas |
| `POST /api/reconnects/reset` | - | **só admin**: zera a contagem de reconexões de todas as câmeras; nenhuma conexão cai |
| `GET /api/health/server` | - | **só admin**: a máquina que grava e o processo do dwnvr: temperatura, memória, pressão, storage, go2rtc, detector de objetos, servidor de e-mail, envios do alerta e últimos avisos do log |
| `POST /api/email/test` | `{"to"}` | **só admin**: manda um e-mail de teste para o endereço, pela seção `smtp` |
| `DELETE /api/rec` | `cam` | **só admin**: apaga as gravações; serve também câmera já removida |

No `POST /api/cameras`, cada área sem `id` ganha um gerado pelo servidor, e a
resposta traz a câmera com eles. Uma área inválida recusa o save inteiro com
`400` e o motivo, como os outros campos:

```text
areas[1] "Garagem" fora do aceito: use de 3 a 20 pontos, com as coordenadas de 0 a 1
```

O `alert` aponta para as áreas pelo `id`, e o que a câmera não tem também
recusa o save. Por isso a tela tira a área do alerta antes de apagá-la:

```text
alert.areas[0] "sumiu" fora do aceito: use o id de uma área desta câmera
```

O horário do alerta que atravessa a meia-noite recusa do mesmo jeito:

```text
alert.schedules[0].to "06:00" fora do aceito: use uma hora depois do from 22:00; a noite são dois horários, até 23:59 e a partir de 00:00
```

O formato das `areas` e do `alert` está em
[Política por câmera](configuracao.md#política-por-câmera).

O `hasAudio` do `GET /api/cameras` só é confiável em stream que alguém já está
consumindo: o go2rtc só preenche o `medias` do produtor enquanto existe
consumidor, e num stream ocioso ele nem abre a conexão com a câmera. É por isso
que `GET /api/streams/probe` é um endpoint à parte, e não mais um campo da
listagem - a resposta dele pode custar alguns segundos e uma conexão nova.

```json
{
  "name": "cam_teste5",
  "hasAudio": true,
  "audioCodecs": ["PCMA/16000"],
  "probed": true
}
```

`probed` diz se a resposta custou uma conexão: `false` quando ela veio do
`medias` de um produtor que já estava vivo, ou do cache da sonda anterior. Se a
sonda falhar - câmera fora do ar, por exemplo -, a resposta ainda é `200`, com o
motivo em `error` e `hasAudio: false`; quem chama deve tratar isso como "não sei",
e não como "não tem áudio".

O `GET /api/cameras` traz também o `defaults`: uma câmera vazia com os `defaults`
do `dwnvr.yaml` aplicados. É dele que o formulário de câmera nova parte, e não
de números repetidos na interface. Traz as `ranges` dos campos numéricos da
câmera, a mesma régua com que o `POST /api/cameras` recusa, de onde saem o
`min` e o `max` dos inputs; sem teto, o `max` não vem:

```json
"ranges": {
  "quotaMB": { "min": 100 },
  "segmentSeconds": { "min": 10, "max": 300 },
  "maxDays": { "min": 0 },
  "stallSeconds": { "min": 1, "max": 3600 },
  "detectSensitivity": { "min": 1, "max": 5 }
}
```

E traz o `detector`: `true` quando o detector de objetos está configurado. Sem
ele, a interface esconde a aba Detecções.

Traz também o `alertEmail`, para onde vai o alerta, que a aba Alerta da câmera
mostra. Com o meio de e-mail (a seção `smtp` ligada e a lista
`alerts.email.to`), vem a lista; com o meio falhando, também o desde quando e o
erro da falha mais recente, que somem no primeiro envio que der certo:

```json
"alertEmail": {
  "to": ["fulano@exemplo.com", "ciclana@exemplo.com"],
  "failingSince": "2026-10-27T13:58:12-03:00",
  "error": "dial tcp 142.250.0.108:587: connect: connection timed out"
}
```

Sem o meio, `missing` diz as seções do `dwnvr.yaml` que faltam, de
`alerts.email.to` e `smtp`, e `smtpError`, o motivo da seção `smtp` recusada no
boot:

```json
"alertEmail": { "missing": ["smtp"], "smtpError": "smtp.from vazio" }
```

O go2rtc só lê o `go2rtc.yaml` quando sobe. Quando o arquivo diz uma coisa e o
go2rtc em execução faz outra, o `GET /api/cameras` traz `go2rtcConfigFile`, e a
tela oferece o `POST /api/go2rtc/restart`:

```json
"go2rtcConfigFile": {
  "added": ["cam_quintal"],
  "changed": ["cam_portao"],
  "notInFile": ["cam_garagem"]
}
```

`added` estão no arquivo e não foram carregadas; `notInFile` estão rodando
e saíram do arquivo; `changed` existem nos dois lados com fonte diferente.
Listas vazias são omitidas, e o campo inteiro some quando os dois lados batem
ou quando o go2rtc não deixa ler o arquivo (`GET /api/config` dele). A
comparação é só da seção `streams`. `changed` não é completa: enquanto uma
fonte `ffmpeg:` ou `exec:` está em uso, o go2rtc não mostra a fonte original, e
essa câmera fica de fora. RTSP em uso mostra, e entra. O arquivo, que tem as
URLs com usuário e senha, é lido no servidor e não vai ao navegador - só os
nomes.

Cada câmera do `/api/health` diz se a marcação de movimento está ligada
(`detect`) e quantos onsets ela marcou desde que o dwnvr subiu (`onsets`).

Cada câmera com recorder traz também o `audio` configurado, ao lado do
`hasAudio` que a conexão achou, e o `transcoding`: se o go2rtc passa a fonte
dela por um ffmpeg. O `transcoding` é consultado ao go2rtc uma vez por conexão,
e não a cada leitura da saúde: a fonte só muda com o go2rtc reiniciando, o que
reconecta todas as câmeras. Câmera desabilitada vem sem `audio`.

As reconexões de cada câmera do `/api/health` contam desde `reconnectsSince`:
a subida do recorder, que é a do dwnvr ou a última vez em que a câmera foi
reiniciada por mudança de configuração, ou o último
`POST /api/reconnects/reset`. `reconnectsInWindow` são as que caíram dentro das
últimas `reconnectsWindowHours` horas, que vem na raiz da resposta e mora em
`recorder.ReconnectWindow` (`internal/recorder/params.go`).
`lastReconnectAt` é a mais recente, e `disconnectedAt`, que só vem com
`connected: false`, é quando a conexão caiu:

```json
"reconnects": 24,
"reconnectsSince": "2026-09-28T08:37:02-03:00",
"reconnectsInWindow": 24,
"lastReconnectAt": "2026-09-28T13:58:12-03:00"
```

O `disk.dwnvrBytes` soma só as câmeras cadastradas. O que sobrou em disco de
câmeras removidas vem à parte, em `disk.orphanBytes`, porque nenhum limite da
retenção o alcança: somado ao primeiro, sumiria do aviso; fora dos dois, a tela
o creditaria a outros programas.

Os avisos da tela com "desde quando" saem também do `disk.belowMinSince`, que a
retenção marca na primeira passada com o disco abaixo do mínimo, e do `go2rtc`,
que só aparece enquanto o go2rtc não responde ao dwnvr. Ele é marcado na
primeira falha de transporte, e qualquer resposta HTTP, mesmo um erro, o apaga:

```json
"go2rtc": {
  "error": "dial tcp 127.0.0.1:1984: connect: connection refused",
  "since": "2026-09-28T13:58:12-03:00"
}
```

Com o detector de objetos configurado, cada câmera do `/api/health` traz também
o **funil**: o destino de cada onset, desde que o dwnvr subiu. As fatias fecham
a conta com o `onsets` da câmera -
`onsets = noVideo + dropped + failed + rejected + noObject + withObject + queued`:

```json
"funnel": {
  "clips": 412,
  "noVideo": 3,
  "dropped": 31,
  "failed": 0,
  "rejected": 2,
  "noObject": 350,
  "withObject": 28,
  "queued": 1
}
```

`clips` não é fatia: é quantos onsets tiveram o vídeo cortado e oferecido à
fila. `noVideo` são onsets que terminaram sem pedaço - logo depois de
conectar, antes do primeiro frame I. `queued` é o que ainda não tem desfecho:
esperando o pico da janela para ser cortado, esperando a vez, ou sendo olhado.

`failed` é detector fora do ar ou travado; `rejected` é detector no ar que não
conseguiu olhar, quase sempre vídeo que a câmera mandou corrompido.

`dropped` são os que a fila recusou porque a câmera já tinha `perCamera`
pedaços esperando a vez (ver `detector` abaixo): não vão ao detector nem
depois, e ficam só como movimento. `noObject` inclui o carro que já estava
estacionado - olhado, mas não CHEGOU agora.

A fila é uma só para todas as câmeras, e vem fora da lista delas, em
`detector`:

```json
"detector": {
  "queue": {
    "length": 1,
    "cap": 8,
    "peak": 5,
    "perCamera": 2,
    "cameras": {
      "cam_teste1": {
        "waiting": 1,
        "busy": true
      }
    }
  },
  "times": {
    "inferenceMs": 3620,
    "waitMs": 410
  }
}
```

`length` são os pedaços esperando a vez, sem contar o que está sendo olhado, e
`peak` o maior `length` desde que o dwnvr subiu. A fila não tem total fixo: cada
câmera pode ter `perCamera` pedaços esperando a vez, e `cap` é isso vezes as
câmeras com detecção ligada agora - cresce e diminui com elas. O pedaço que o
detector está processando já saiu da fila: `waiting` nunca passa de
`perCamera`, e `busy` diz, à parte, de qual câmera é a imagem em
processamento. `cameras` traz só as câmeras com algo numa coisa ou na outra. Os tempos são
a média das últimas 20 olhadas: `inferenceMs` é quanto o detector leva para
responder - só as que ele respondeu -, e `waitMs` quanto o pedaço esperou a
vez antes. Zero antes da primeira olhada. Sem detector configurado, o campo
não vem.

Enquanto o detector não responde, o `detector` traz também `outage`, com a
primeira olhada seguida que falhou e o motivo da mais recente. A primeira
olhada que ele responde o apaga. Pedaço recusado (vídeo corrompido) não conta:
é o detector no ar dizendo não.

```json
"outage": {
  "since": "2026-09-29T14:02:10-03:00",
  "error": "dial tcp 172.18.0.3:8480: connect: connection refused"
}
```

Depois que alguma câmera liga o alerta, o `/api/health` traz o total de
alertas desde que o dwnvr subiu. O campo fica até o dwnvr reiniciar, mesmo que
o alerta seja desligado, e não vem antes:

```json
"alerts": { "total": 12 }
```

Os alertas em si ficam gravados na detecção, e se leem no `GET /api/detections`
(`alert` e `alertAreas` em cada objeto, e o filtro `alert=1`).

Com algum meio de envio configurado, o alerta que não sai é descartado e
contado: `queueFull` não coube na fila de envio, e `overLimit` passou do teto
de `limitPerHour` (ver [O alerta](deteccao.md#o-alerta)). O `dropped` vem
enquanto o episódio vale, até uma hora depois do último descarte (`lastAt`);
o descarte seguinte começa outro, com a contagem do zero. É dele que sai o
Aviso amarelo do card Avisos.

```json
"alerts": {
  "total": 52,
  "dropped": {
    "queueFull": 0, "overLimit": 12, "limitPerHour": 15,
    "since": "2026-10-27T14:02:10-03:00", "lastAt": "2026-10-27T14:07:31-03:00"
  }
}
```

Com um meio falhando, o `failing` traz um item por meio, com o `name`, o
`since` (a primeira falha desde o último envio que deu certo) e o `error` da
falha mais recente. Some no primeiro envio que der certo. É dele que sai o
Aviso vermelho do card Avisos.

```json
"alerts": {
  "total": 52,
  "failing": [
    { "name": "email", "since": "2026-10-27T13:58:04-03:00", "error": "dial tcp 142.250.0.108:587: connect: connection timed out" }
  ]
}
```

A seção `alerts` do `dwnvr.yaml` com problema (ver
[Alerta por e-mail](configuracao.md#alerta-por-e-mail)) põe os `warnings` no
mesmo `alerts`, para o card Avisos e o sino, mesmo sem câmera com alerta, e
então sem o `total`. Sem problema, eles não vêm.

```json
"alerts": {
  "warnings": ["falta a seção smtp, e sem ela a lista alerts.email.to não recebe nada"]
}
```

`DELETE /api/rec` aceitar câmera já removida é deliberado: descadastrar sem
apagar deixa gravações órfãs, e sem esse endpoint não haveria como recuperar o
espaço pela interface.

A seção `smtp` do `dwnvr.yaml` com problema deixa o e-mail desligado (ver
[E-mail](configuracao.md#e-mail)), e o `/api/health` traz o motivo, para o card
Avisos e o sino. Sem problema, o campo não vem.

```json
"smtp": { "error": "smtp.security \"ssl\" fora do aceito: use starttls, tls ou none" }
```

O `users.json` com problema também vira aviso, só com a autenticação ligada.
O `error` é o arquivo ilegível no boot: só o dono entra até ele ser corrigido.
Os `warnings` são as entradas que não valem: a pessoa ignorada, o e-mail
ilegível e o e-mail repetido, que não serve para entrar. Corrigir pela tela de
Usuários tira o aviso na hora; corrigir no arquivo pede reiniciar o dwnvr. Sem
problema, o campo não vem.

```json
"users": {
  "warnings": ["usuário \"ana\": o e-mail \"casa@exemplo.com\" é o mesmo de \"maria\", e nenhum dos dois entra pelo e-mail"]
}
```

O `POST /api/email/test` devolve `{"sentTo": "..."}`. Endereço vazio, com nome
ou mais de um é `400`; com o e-mail desligado, `409` com o motivo. Se o
servidor de e-mail recusar, é `502`, com a resposta dele do jeito que veio, como
`o e-mail não foi: o servidor recusou o login: 535 5.7.8 Username and Password
not accepted`. Entregue ao servidor não é entregue na caixa: spam e caixa
cheia o dwnvr não vê.

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

O `GET /api/health/server` é o card "Este servidor" do Diagnóstico. Fica fora
do `/api/health` porque custa mais: a cada chamada ele grava, sincroniza com
`fsync` e apaga 4 KB no `storage.root`, pergunta a versão ao go2rtc e, com
`detector.url` configurado, chama o `GET /health` do detector de objetos. A
tela só o chama com o card aberto, de 15 em 15 segundos. Tudo sai de `/proc` e
`/sys`, e cada campo some quando a fonte não existe (kernel sem PSI, sistema
sem cgroup, fora do Linux).

```json
{
  "collectedAt": {
    "at": "2026-10-02T17:45:12-03:00", "abbreviation": "-03",
    "offsetSeconds": -10800, "zone": "America/Sao_Paulo"
  },
  "machine": {
    "board": "OrangePi Zero3", "kernel": "6.12.58-current-sunxi64",
    "cores": 4, "allowedCores": 1.5, "load": [3.1, 2.05, 1.5],
    "sensors": [{ "name": "cpu_thermal", "celsius": 84.3 }],
    "mhz": 1008, "mhzMax": 1512, "governor": "ondemand"
  },
  "process": {
    "cpuMs": 811550, "aliveMs": 18585000, "memoryBytes": 33980416,
    "goroutines": 112, "openFiles": 34, "fileLimit": 524287,
    "uid": 1000, "gid": 1000, "go": "go1.27.1", "architecture": "linux/arm64"
  },
  "memory": {
    "totalBytes": 1036640256, "availableBytes": 209715200,
    "swapTotalBytes": 536870912, "swapUsedBytes": 268435456,
    "limitBytes": 536870912, "oomKills": 2
  },
  "pressure": {
    "cpu":     { "some": { "avg10": 12.5, "avg60": 8, "avg300": 3.25 }, "full": { "avg10": 0, "avg60": 0, "avg300": 0 } },
    "memory": { "some": { "avg10": 0, "avg60": 0, "avg300": 0 } },
    "io":      { "some": { "avg10": 40, "avg60": 35.1, "avg300": 20 } }
  },
  "storage": {
    "path": "/storage", "type": "ext4", "origin": "/dev/sda1",
    "options": "rw,relatime", "readOnly": false,
    "write": { "ok": true, "ms": 12 }
  },
  "go2rtc": { "ok": true, "ms": 4, "version": "1.9.9", "url": "http://go2rtc:1984" },
  "detector": {
    "ok": true, "ms": 128, "model": "model.onnx", "inputSize": "512x288",
    "threads": 2, "url": "http://dwnvr-detect:8480"
  },
  "smtp": {
    "host": "smtp.gmail.com", "port": 587, "security": "starttls",
    "from": "dwnvr <dwnvr.exemplo@gmail.com>"
  },
  "alerts": [
    { "name": "email", "sent": 47, "failures": 9, "last": "2026-10-02T17:44:37-03:00", "error": "dial tcp 142.250.0.108:587: connect: connection timed out" }
  ],
  "log": {
    "total": 37,
    "lines": [{ "at": "2026-09-22T18:37:12-03:00", "level": "WARN", "text": "connection dropped cam=garagem err=EOF" }]
  }
}
```

- `collectedAt` é o instante da leitura no relógio do servidor, com o fuso dele,
  como o `clock` do `/api/health`: a tela o mostra na hora de lá, sem converter.
  `zone` some quando o servidor não sabe o nome IANA.
- `board` vem do device tree e só existe em ARM; `processor` vem do
  `/proc/cpuinfo` e só tem nome no x86. `kernel` é o do host, mesmo dentro do
  container.
- `allowedCores` é o teto de CPU do container, em núcleos (`--cpus 1.5`), e
  some quando não há teto.
- `process` é o próprio dwnvr. `cpuMs` é o tempo de CPU gasto desde que ele
  subiu, e `aliveMs`, há quanto tempo está no ar: a porcentagem sai da diferença
  entre duas leituras, e quem guarda a leitura anterior é a tela, não o
  servidor. `memoryBytes` é a memória física do processo (`VmRSS`), que é o
  que bate contra o `limitBytes`. O `memory.current` do cgroup não serve para
  isso: soma o cache dos vídeos gravados, que o kernel devolve quando precisa.
- `detector` só vem com `detector.url` configurado. Responde mesmo sem
  movimento em câmera nenhuma, ao contrário do `outage` do `/api/health`, que
  só aparece depois de uma olhada falhar.
- As `url` vêm sem usuário e senha.
- `smtp` só vem com a seção `smtp` no `dwnvr.yaml`, sem o login e sem a senha.
  Não há sonda: conferir o servidor de e-mail é mandar um, e para isso existe o
  `POST /api/email/test`. Com a seção recusada, traz o `error`.
- `alerts` são os envios de cada meio do alerta desde que o dwnvr subiu, e só
  vêm com meio configurado e alguma câmera com o alerta ligado. `last` é a
  tentativa mais recente e some antes da primeira; `error` vem só se ela falhou.
- `pressure` é o PSI do kernel: a porcentagem do tempo em que alguma tarefa
  (`some`) ou todas (`full`) ficaram paradas esperando CPU, memória ou disco,
  em média de 10, 60 e 300 segundos.
- `oomKills` conta os processos que o kernel matou por falta de memória desde
  que a **máquina** ligou, e não só os do container. Quando a vítima é o próprio
  dwnvr, o Docker sobe um container novo e o contador do cgroup antigo some
  junto; o da máquina continua lá.
- `limitBytes` é o teto de memória do container, e some quando não há teto.
- `readOnly` vale para a montagem ou para o superbloco: um cartão SD com
  defeito é remontado somente leitura pelo kernel sem avisar ninguém.
- `write.error` só traz causa conhecida (somente leitura, sem permissão, disco
  cheio, diretório inexistente, disco que não respondeu em 5 s). Qualquer outra
  falha vai genérica, com o detalhe no log. Enquanto um teste anterior continuar
  preso num disco travado, a resposta diz isso e não abre outro teste.
- `log` traz os últimos 50 avisos e erros, do mais novo para o mais velho, e
  `total` diz quantos houve desde que o dwnvr subiu.

## Gravações

| Endpoint | Parâmetros | Devolve |
| --- | --- | --- |
| `GET /api/rec/days` | `cam` | dias que têm gravação |
| `GET /api/rec/timeline` | `cam` + intervalo | faixas contíguas (para desenhar) + segmentos (para tocar) |
| `GET /api/rec/events` | `cam` + intervalo | os instantes em que houve movimento |
| `GET /api/rec/init` | `cam`, `g` | o init segment daquela geração, `immutable` |
| `GET /api/rec/seg` | `cam`, `t` | os fragmentos do segmento, **sem** o init, `immutable` |
| `GET /api/rec/thumb` | `cam`, `t` | MP4 de 1 frame - o servidor não decodifica nada |
| `GET /api/rec/playlist.m3u8` | `cam` + intervalo | HLS VOD, para VLC/ffplay/Safari |
| `GET /api/rec/export` | `cam` + intervalo | MP4 único emendado, sem transcodificação |

**Intervalo** é `from`+`to` em epoch ms, ou o atalho `day=AAAA-MM-DD`, que
equivale ao dia inteiro em hora local. `to` precisa ser maior que `from`.

**`/api/rec/export`** aceita no máximo 10 min de intervalo, o mesmo teto da tela
de Gravações; acima disso responde 400. Por isso o atalho `day=` não serve
para ele.

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

**`/api/rec/events`** devolve os instantes crus, em ordem, que a faixa de calor
da timeline desenha, e as marcas de objeto que o detector confirmou:

```json
{
  "cam": "cam_teste1",
  "from": 1786176000000,
  "to": 1786262400000,
  "onsets": [1786220571043, 1786220604112],
  "objects": [
    {
      "atMs": 1786220571043,
      "family": "pessoa",
      "class": "person",
      "score": 0.87,
      "frameMs": 1786220572851,
      "box": [0.5156, 0.2611, 0.6734, 0.9778]
    }
  ]
}
```

Os `onsets` são números soltos, e não objetos, pela mesma razão que a timeline
é compacta: um dia no nível mais sensível tem ~2.400 marcas, e a forma verbosa
dobraria a resposta para dizer o mesmo. Os `objects` são dezenas por dia, e vão
com chave por extenso; o instante de cada um é o do onset que o originou. É um
por objeto: duas pessoas no mesmo quadro são dois itens de `pessoa` com o mesmo
`atMs`, e a timeline junta os dois numa chegada.

`frameMs` e `box` dizem onde desenhar o objeto sobre o vídeo: `box` é
X1, Y1, X2, Y2 em fração da imagem, de 0 a 1, e vale para o quadro de
`frameMs` - o que o detector olhou, até 3 s depois do onset -, e não para o
instante da marca. Marca sem as duas chaves não ganha caixa na tela.

Câmera com a detecção desligada, ou sem detector configurado, responde lista
vazia - não é erro, é o caso comum.

O intervalo livre é o que dá o **caminho de cauda**: no dia corrente a tela
pergunta de novo a cada dez segundos e pede só o pedaço recente, em vez de
rebaixar o dia inteiro a cada rodada. A marca de objeto chega **atrasada** - ela
leva o instante do onset, mas só é gravada depois que o detector olha -, então
a cauda volta 30 min antes do último onset e descarta o que já tinha. Pedir a
partir do último onset perderia para sempre o objeto de um onset anterior.

## Detecções

As detecções de objeto de todas as câmeras juntas: é o que a tela de Detecções
rola.

| Endpoint | Parâmetros | Devolve |
| --- | --- | --- |
| `GET /api/detections` | `before` ou `after`, `limit`, `cams`, `families`, `area`, `rule`, `areas`, `alert` | uma página de detecções, da mais nova para a mais velha |
| `GET /api/detections/frame` | `cam`, `t` | o JPEG do quadro que o detector olhou, `immutable` |
| `GET /api/detections/days` | `cams`, `families`, `area`, `rule`, `areas`, `alert` | os dias com pelo menos uma detecção, em ordem crescente, e quantas cada um tem |

**`/api/detections`** pagina por cursor, e não por número de página: o dia
corrente ganha detecção nova a todo momento, e "página 3" mudaria de conteúdo
entre um pedido e outro.

- `before=<ms>`: as detecções anteriores a este instante, para rolar para baixo.
  Sem cursor nenhum, vêm as mais novas.
- `after=<ms>`: as posteriores, para rolar para cima depois de um "ir para".
  São as mais próximas do cursor, mas a resposta vem na mesma ordem das outras:
  da mais nova para a mais velha.
- `limit`: quantas, de 1 a 200. Padrão 60.
- `cams=cam_a,cam_b` e `families=pessoa,veiculo`: só essas. Sem eles, todas.
  Câmera não cadastrada ou família desconhecida é erro.
- `area=<cam>:<x>,<y>,<x>,<y>,...`: na câmera `<cam>`, só os objetos dentro
  de uma área desenhada sobre a imagem dela, como
  `area=cam_teste1:400,500,600,500,600,700`. X e Y vão em milésimos do quadro,
  inteiros de 0 a 1000, para a URL ficar curta; são de 3 a 20 pontos, e a área
  pode ser côncava. A área restringe só a câmera dela: as outras câmeras de
  `cams` vêm inteiras, e a câmera da área fora de `cams` não aparece.
- `rule=touch` ou `rule=foot`: quando um objeto conta como dentro da área. Com
  `touch`, basta a caixa ter algum ponto em comum com ela. Com `foot`, o
  padrão, conta o meio da borda de baixo da caixa, onde ficam os pés e as
  rodas: a área se desenha no chão, e quem passa na frente dela, perto da
  câmera, não conta. `rule` sem `area` é erro.
- `areas=<cam>/<id>`: o mesmo, com uma área cadastrada na câmera (o campo
  `areas` de `POST /api/cameras`) e a regra do cadastro, como
  `areas=cam_teste1/k3f9a2`. Várias vão separadas por vírgula, como as
  câmeras de `cams` (`areas=cam_a/k3f9a2,cam_b/p0rt40`). O servidor resolve
  pelo cadastro de agora: editar a área muda o filtro dali em diante, inclusive
  sobre o passado. Área que a câmera não tem mais é erro.

- `alert=1`: só as detecções em que algum objeto alertou (ver abaixo). O
  filtro é por detecção: o objeto que não alertou fica nela. Ele vale sobre o
  que sobra dos outros filtros: com `families=veiculo`, a detecção que alertou
  só pela pessoa sai, porque a pessoa sai. Outro valor é erro.

As áreas de uma câmera **somam**, a de `area` junto: o objeto fica se está em
qualquer uma delas. A câmera sem área nenhuma no filtro vem inteira.

```json
{
  "detections": [
    {
      "cam": "cam_teste1",
      "atMs": 1786220571043,
      "frameMs": 1786220572851,
      "hasFrame": true,
      "objects": [
        { "family": "pessoa", "class": "person", "score": 0.87, "box": [0.5156, 0.2611, 0.6734, 0.9778],
          "alert": true, "alertAreas": ["k3f9a2"] },
        { "family": "veiculo", "class": "car", "score": 0.61, "box": [0.05, 0.4, 0.3, 0.7] }
      ]
    }
  ],
  "end": false
}
```

Uma **detecção** é uma olhada do detector numa câmera: o instante do onset e
tudo o que ele achou naquele quadro. Duas pessoas e um carro no mesmo quadro
são três marcas no `eventos/{dia}.ndjson`, mas uma detecção só, com três
objetos. O filtro de família e o de área tiram objetos, e a detecção que fica
sem nenhum sai da página. Com área, a marca sem caixa, gravada antes de a
caixa existir, fica de fora: não há como saber onde ela estava.
`box` e `frameMs` são os mesmos de `/api/rec/events`.

`alert` diz que o objeto passou na regra de alerta da câmera na hora em que foi
olhado, e `alertAreas`, os ids das áreas do alerta em que ele caiu; sem áreas
no alerta, ou sem alerta, o campo não vem. É o que foi gravado: mudar o alerta
da câmera não muda o passado, e o id de uma área apagada continua vindo. O
viewer também os recebe.

O próximo cursor é o `atMs` da última detecção da página. A página **não
parte um instante ao meio**: se a última empata, no mesmo milissegundo, com a de
outra câmera, as duas vêm e a página passa um pouco do limite - senão o cursor
pularia a que ficou de fora. `end` diz que não há mais nada na direção pedida;
ele pode vir falso na página que por acaso pegou a última detecção, e a próxima
vem vazia e com `end` verdadeiro.

A marca de objeto chega **atrasada**, como em `/api/rec/events`: ela leva o
instante do onset, mas só é gravada depois que o detector olha. Uma detecção
nova pode, então, cair abaixo do topo que a tela já mostrou.

O servidor memoriza as marcas de objeto de cada dia já lido, sem os onsets:
dia passado é lido do disco uma vez só, e do dia corrente só o pedaço que o
arquivo cresceu. Medido com 11 dias de uma instalação real, nove câmeras e 7.987
detecções: 0,85 MB em memória, e uma página de 60 em 0,2 ms depois da primeira
leitura. O dia sai da memória quando a retenção o apaga.

**`/api/detections/days`** é o que o calendário da tela de Detecções deixa
escolher, com o número que ele mostra em cada dia. `cams`, `families`, `area`,
`rule`, `areas` e `alert` são os mesmos de `/api/detections`, e o dia entra se tiver ao menos uma detecção do
filtro. `detectionsPerDay` conta detecções, não marcas, como `/api/detections`
as monta: pessoa e carro no mesmo quadro contam uma vez. Dia sem gravação fica
de fora mesmo com marca, porque a rolagem também não chega nele.

```json
{
  "days": ["2026-10-03", "2026-10-04", "2026-10-05"],
  "detectionsPerDay": { "2026-10-03": 508, "2026-10-04": 294, "2026-10-05": 462 }
}
```

Ele usa a mesma memória da rolagem, e contar obriga a ler todas as câmeras de
cada dia. Medido no Orange Pi Zero 3 com 28 dias, nove câmeras, 13 MB de
eventos e 18.772 detecções, com o cache de disco frio: a primeira chamada levou
1,2 s e 1,3 s, e as seguintes saem da memória em ~5 ms. Parar na primeira
câmera com marca, sem contar, levava 0,4 s. A tela relê a lista a cada abertura
do calendário, porque o dia corrente cresce com ela aberta.

**`/api/detections/frame`** serve o arquivo como ele está no disco, sem
redimensionar nem reencodar: `t` é o `atMs` da detecção. Ele nunca muda
depois de gravado, então vai com `immutable` e rolar de volta não pede nada ao
servidor. Detecção com `hasFrame` falso responde 404.

## Usuários

Tudo **só admin**. Sem autenticação ligada não há cadastro: a listagem vem com
`"authRequired": false` e o resto responde `409`.

| Endpoint | O que faz |
| --- | --- |
| `GET /api/users` | as pessoas, com a situação de cada uma, e o dono à parte |
| `POST /api/users` | cria um viewer, `{"username", "name", "email"}`, e devolve o token do link; o e-mail é opcional |
| `POST /api/users/check` | o que o `POST /api/users` recusaria com o mesmo corpo, sem criar ninguém |
| `POST /api/users/link` | link novo, `{"username"}`: apaga a senha atual e derruba a pessoa na hora |
| `POST /api/users/link/email` | manda o link por e-mail, `{"username", "url"}`, para o e-mail da pessoa |
| `POST /api/users/email` | troca o e-mail de alguém, `{"username", "email"}`; vazio apaga |
| `POST /api/users/email/check` | o que o `POST /api/users/email` recusaria com o mesmo corpo, sem trocar nada |
| `DELETE /api/users?username=` | remove a pessoa, e a foto dela, e a derruba na hora |
| `DELETE /api/users/avatar?username=` | tira a foto de alguém; trocar, só a própria pessoa |

```json
// GET /api/users
{
  "authRequired": true,
  "owner": { "username": "admin", "name": "admin", "avatar": "", "lastAccessMs": 1791986400000 },
  "users": [
    { "username": "maria", "name": "Maria", "email": "maria@exemplo.com", "role": "viewer", "status": "invitePending",
      "inviteExpiresAtMs": 1791587903000, "createdAtMs": 1791587303000, "avatar": "4d1e0b62a7c3f915",
      "lastAccessMs": 1791500000000 }
  ],
  "inviteValidityMs": 600000,
  "maxNameLength": 60,
  "maxUsernameLength": 32,
  "maxEmailLength": 254,
  "emailEnabled": true
}
```

O `status` é `active` (tem senha), `invitePending` (pode definir a senha pelo
link) ou `inviteExpired` (precisa de outro link). Quem está `active` traz também
`activeSinceMs`, quando definiu a senha pelo link; trocar a própria senha não o
muda, e o link novo o apaga.

O `lastAccessMs` é a última requisição da pessoa com sessão válida, e não o
último login, que diz pouco com a sessão se renovando sozinha. Anda de minuto
em minuto (`users.LastAccessStep`) e vem em qualquer `status`: quem
ganhou link novo continua com o acesso de antes. Some de quem não usou o
dwnvr desde que o campo existe, e de quem é removido ou criado de novo com o
mesmo usuário. A requisição só anota na memória; o `access.json`, na pasta de
config, é gravado em segundo plano, no máximo a cada 5 minutos
(`users.AccessFlushDelay`) e ao desligar.

Criar e o link novo devolvem
`{"user": {...}, "token": "..."}`: o token só existe nessa resposta, e a
tela monta o link com ele. Usuário fora do formato, repetido ou igual ao do
dono, e nome vazio, são `400` com o motivo; pessoa que não existe é `404`.

A recusa do criar e a do trocar e-mail vêm campo a campo em `fields`, com a
primeira, na ordem da tela, também no `error` de sempre:

```json
// POST /api/users -> 400
{
  "error": "já existe alguém com este usuário",
  "fields": { "username": "já existe alguém com este usuário", "email": "já existe alguém com este e-mail" }
}
```

As duas rotas `/conferir` são a mesma régua sem gravar nada: a tela confere
cada campo quando o admin sai dele. Respondem `200` com `{"fields": {...}}`,
só com os campos recusados, e `{"fields": {}}` quando está tudo certo. A régua
conta também a entrada ignorada do `users.json` (o login e o e-mail dela
não ficam livres), que a listagem não traz: por isso a tela não confere sozinha.
Com o `users.json` ilegível, o cadastro em memória está vazio, e elas
respondem `500` em vez de dizer que está tudo certo.

O `email` é um endereço só, sem nome na frente, e é **único** entre as
pessoas, sem diferença de maiúscula e minúscula: e-mail inválido ou de outra
pessoa é `400`. O `POST /api/users/email` devolve `{"user": {...}}` e vale
mesmo com o e-mail desligado, porque guardar não envia nada. Um e-mail
escrito errado à mão no `users.json` vira aviso no log e no `/api/health`
e não vem na listagem, mas continua no arquivo. O `emailEnabled` diz se a
seção `smtp` está ligada; sem ela, a tela esconde o "Enviar por e-mail" e avisa,
onde o e-mail é escrito, que o link não vai por e-mail.

O `POST /api/users/link/email` recebe o link do jeito que a tela o mostra,
porque o servidor não sabe por qual endereço é acessado: a tela o monta com o
do navegador do admin. O servidor confere que é o link em aberto daquela
pessoa (`http` ou `https`, sem usuário nem query, só o `#invite?token=`) e
manda para o e-mail dela. Devolve `{"sentTo": "..."}`. Link de outra pessoa,
em outra forma, ou pessoa sem e-mail, são `400`; link vencido ou usado, `410`;
e-mail desligado, `409`; servidor de e-mail recusando, `502` com a resposta
dele. O log diz para quem foi, nunca o link.

## Minha conta

O que cada pessoa muda de si mesma, de qualquer papel; ninguém muda o dos
outros por aqui. Sem autenticação ligada não há de quem ser a conta, e tudo
responde `409`.

| Endpoint | O que faz |
| --- | --- |
| `GET /api/account` | quem está logado e os números do formulário |
| `POST /api/account/password` | troca a própria senha, `{"currentPassword", "newPassword"}`; o dono recebe `409` |
| `POST /api/account/name` | troca o próprio nome de exibição, `{"name"}` |
| `PUT /api/account/avatar` | guarda a própria foto: o corpo é o JPEG, já reduzido |
| `DELETE /api/account/avatar` | tira a própria foto |
| `GET /api/avatar?id=` | a foto: a própria, ou qualquer uma para o admin |

```json
// GET /api/account
{
  "user": { "username": "maria", "name": "Maria", "role": "viewer", "owner": false },
  "minPasswordLength": 8,
  "maxPasswordLength": 128,
  "maxNameLength": 60,
  "avatar": { "sidePx": 256, "maxBytes": 32768, "quality": 92 }
}
```

As rotas que mudam algo devolvem `{"ok": true, "user": {...}}` como ficou,
para a tela atualizar o header sem perguntar de novo.

**A senha** do dono fica no `dwnvr.yaml`, e se troca lá. Para os outros, a
troca pede a senha atual e paga duas contas de senha, na mesma fila do login:
uma para conferir a atual, outra para gerar a nova. Senha atual errada, ou
nova fora de 8 a 128 caracteres, é `400` com o motivo, e nada cai. Trocada, os
outros aparelhos da pessoa voltam para o login (ver [O cookie](#o-cookie)).

**A foto** é reduzida no navegador: a original não sai do aparelho. Ele corta
o quadrado do centro, reduz a até `sidePx` pixels e gera um JPEG na `quality`,
baixando a qualidade só se passar de `maxBytes`. O servidor para de ler no
teto (`413`), decodifica a imagem inteira e recusa com `400` o que não for
JPEG quadrado de até `sidePx` pixels. Guarda os bytes como vieram, sem
comprimir de novo, em `avatars/<id>.jpg` na pasta de config. O id muda a cada
troca, então o `GET /api/avatar` vai com `Cache-Control: private,
max-age=31536000, immutable` e `nosniff`: foto nova é URL nova. A foto de
outra pessoa, para quem não é admin, responde `404`, como a que não existe.

## Live

| Endpoint | O que faz |
| --- | --- |
| `GET /api/live/ws?src=` | proxy do websocket do go2rtc (`/api/ws`), com a credencial ficando no servidor |

O navegador nunca fala com o go2rtc diretamente. Passar pelo proxy resolve duas
coisas de uma vez: a senha da API do go2rtc não vai para o cliente, e o live
respeita a mesma sessão do resto da interface. Quem é removido tem o websocket
fechado na hora (ver [A queda de quem é derrubado](#a-queda-de-quem-é-derrubado)).

Só o websocket do player passa. O resto da API do go2rtc - configuração,
restart, streams - não é alcançável por aqui; o que o dwnvr precisa dela, ele
pede do lado do servidor.

## Por que a interface não exige sessão

Os arquivos da SPA - HTML, CSS e JS - são servidos **sem** autenticação. É só o
app shell, sem dado nenhum de câmera. Protegê-lo impediria o navegador de
carregar a própria tela de login. Tudo que é dado está atrás dos endpoints
acima.
