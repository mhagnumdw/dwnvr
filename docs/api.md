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
| `GET /api/streams/probe` | `src` | diz se um stream entrega áudio, abrindo-o se preciso |
| `POST /api/go2rtc/restart` | - | reinicia o go2rtc, para ele reler o `go2rtc.yaml`; todas as câmeras param por alguns segundos |
| `GET /api/health` | - | bitrate medido, dias estimados, estado do disco, uptimes e relógio |
| `GET /api/health/servidor` | - | a máquina que grava: temperatura, memória, pressão, storage, go2rtc e últimos avisos do log |
| `DELETE /api/rec` | `cam` | apaga as gravações; serve também câmera já removida |

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
motivo em `erro` e `hasAudio: false`; quem chama deve tratar isso como "não sei",
e não como "não tem áudio".

O `GET /api/cameras` traz também o `padrao`: uma câmera vazia com os `defaults`
do `dwnvr.yaml` aplicados. É dele que o formulário de câmera nova parte, e não
de números repetidos na interface. E traz o `detector`: `true` quando o
detector de objetos está configurado. Sem ele, a interface esconde a aba
Detecções.

O go2rtc só lê o `go2rtc.yaml` quando sobe. Quando o arquivo diz uma coisa e o
go2rtc em execução faz outra, o `GET /api/cameras` traz `go2rtcConfigFile`, e a
tela oferece o `POST /api/go2rtc/restart`:

```json
"go2rtcConfigFile": {
  "novas": ["cam_quintal"],
  "alteradas": ["cam_portao"],
  "foraDoArquivo": ["cam_garagem"]
}
```

`novas` estão no arquivo e não foram carregadas; `foraDoArquivo` estão rodando
e saíram do arquivo; `alteradas` existem nos dois lados com fonte diferente.
Listas vazias são omitidas, e o campo inteiro some quando os dois lados batem
ou quando o go2rtc não deixa ler o arquivo (`GET /api/config` dele). A
comparação é só da seção `streams`. `alteradas` não é completa: enquanto uma
fonte `ffmpeg:` ou `exec:` está em uso, o go2rtc não mostra a fonte original, e
essa câmera fica de fora. RTSP em uso mostra, e entra. O arquivo, que tem as
URLs com usuário e senha, é lido no servidor e não vai ao navegador - só os
nomes.

Cada câmera do `/api/health` diz se a marcação de movimento está ligada
(`detect`) e quantos onsets ela marcou desde que o dwnvr subiu (`onsets`).

Com o detector de objetos configurado, cada câmera do `/api/health` traz também
o **funil**: o destino de cada onset, desde que o dwnvr subiu. As fatias fecham
a conta com o `onsets` da câmera -
`onsets = semVideo + descartados + falhas + recusados + semObjeto + comObjeto + naFila`:

```json
"funil": {
  "pedacos": 412,
  "semVideo": 3,
  "descartados": 31,
  "falhas": 0,
  "recusados": 2,
  "semObjeto": 350,
  "comObjeto": 28,
  "naFila": 1
}
```

`pedacos` não é fatia: é quantos onsets tiveram o vídeo cortado e oferecido à
fila. `semVideo` são onsets que terminaram sem pedaço - logo depois de
conectar, antes do primeiro frame I. `naFila` é o que ainda não tem desfecho:
esperando o pico da janela para ser cortado, esperando a vez, ou sendo olhado.

`falhas` é detector fora do ar ou travado; `recusados` é detector no ar que não
conseguiu olhar, quase sempre vídeo que a câmera mandou corrompido.

`descartados` são os que a fila recusou porque a câmera já tinha `porCamera`
pedaços esperando a vez (ver `detector` abaixo): não vão ao detector nem
depois, e ficam só como movimento. `semObjeto` inclui o carro que já estava
estacionado - olhado, mas não CHEGOU agora.

A fila é uma só para todas as câmeras, e vem fora da lista delas, em
`detector`:

```json
"detector": {
  "fila": {
    "agora": 1,
    "cap": 8,
    "pico": 5,
    "porCamera": 2,
    "cameras": {
      "cam_teste1": {
        "esperando": 1,
        "olhando": true
      }
    }
  },
  "tempos": {
    "analiseMs": 3620,
    "esperaMs": 410
  }
}
```

`agora` são os pedaços esperando a vez, sem contar o que está sendo olhado, e
`pico` o maior `agora` desde que o dwnvr subiu. A fila não tem total fixo: cada
câmera pode ter `porCamera` pedaços esperando a vez, e `cap` é isso vezes as
câmeras com detecção ligada agora - cresce e diminui com elas. O pedaço que o
detector está processando já saiu da fila: `esperando` nunca passa de
`porCamera`, e `olhando` diz, à parte, de qual câmera é a imagem em
processamento. `cameras` traz só as câmeras com algo numa coisa ou na outra. Os tempos são
a média das últimas 20 olhadas: `analiseMs` é quanto o detector leva para
responder - só as que ele respondeu -, e `esperaMs` quanto o pedaço esperou a
vez antes. Zero antes da primeira olhada. Sem detector configurado, o campo
não vem.

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

O `GET /api/health/servidor` é o card "Este servidor" do Diagnóstico. Fica fora
do `/api/health` porque custa mais: a cada chamada ele grava, sincroniza com
`fsync` e apaga 4 KB no `storage.root`, e pergunta a versão ao go2rtc. A tela só
o chama com o card aberto, de 15 em 15 segundos. Tudo sai de `/proc` e `/sys`,
e cada campo some quando a fonte não existe (kernel sem PSI, sistema sem
cgroup, fora do Linux).

```json
{
  "maquina": {
    "nucleos": 4, "carga": [3.1, 2.05, 1.5],
    "sensores": [{ "nome": "cpu_thermal", "celsius": 84.3 }],
    "mhz": 1008, "mhzMax": 1512, "governor": "ondemand"
  },
  "memoria": {
    "totalBytes": 1036640256, "disponivelBytes": 209715200,
    "swapTotalBytes": 536870912, "swapUsadaBytes": 268435456,
    "limiteBytes": 536870912, "oomKills": 2
  },
  "pressao": {
    "cpu":     { "some": { "avg10": 12.5, "avg60": 8, "avg300": 3.25 }, "full": { "avg10": 0, "avg60": 0, "avg300": 0 } },
    "memoria": { "some": { "avg10": 0, "avg60": 0, "avg300": 0 } },
    "io":      { "some": { "avg10": 40, "avg60": 35.1, "avg300": 20 } }
  },
  "storage": {
    "caminho": "/storage", "tipo": "ext4", "origem": "/dev/sda1",
    "opcoes": "rw,relatime", "somenteLeitura": false,
    "escrita": { "ok": true, "ms": 12 }
  },
  "go2rtc": { "ok": true, "ms": 4, "versao": "1.9.9" },
  "log": {
    "total": 37,
    "linhas": [{ "em": "2026-09-22T18:37:12-03:00", "nivel": "WARN", "texto": "conexão caiu cam=garagem erro=EOF" }]
  }
}
```

- `pressao` é o PSI do kernel: a porcentagem do tempo em que alguma tarefa
  (`some`) ou todas (`full`) ficaram paradas esperando CPU, memória ou disco,
  em média de 10, 60 e 300 segundos.
- `oomKills` conta os processos que o kernel matou por falta de memória desde
  que a **máquina** ligou, e não só os do container. Quando a vítima é o próprio
  dwnvr, o Docker sobe um container novo e o contador do cgroup antigo some
  junto; o da máquina continua lá.
- `limiteBytes` é o teto de memória do container, e some quando não há teto.
- `somenteLeitura` vale para a montagem ou para o superbloco: um cartão SD com
  defeito é remontado somente leitura pelo kernel sem avisar ninguém.
- `escrita.erro` só traz causa conhecida (somente leitura, sem permissão, disco
  cheio, diretório inexistente, disco que não respondeu em 5 s). Qualquer outra
  falha vai genérica, com o detalhe no log. Enquanto um teste anterior continuar
  preso num disco travado, a resposta diz isso e não abre outro teste.
- `log` traz os últimos 50 avisos e erros, do mais novo para o mais velho, e
  `total` diz quantos houve desde que o dwnvr subiu.

## Gravações

| Endpoint | Parâmetros | Devolve |
|---|---|---|
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
  "objetos": [
    {
      "instanteMs": 1786220571043,
      "familia": "pessoa",
      "classe": "person",
      "score": 0.87,
      "quadroMs": 1786220572851,
      "caixa": [0.5156, 0.2611, 0.6734, 0.9778]
    }
  ]
}
```

Os `onsets` são números soltos, e não objetos, pela mesma razão que a timeline
é compacta: um dia no nível mais sensível tem ~2.400 marcas, e a forma verbosa
dobraria a resposta para dizer o mesmo. Os `objetos` são dezenas por dia, e vão
com chave por extenso; o instante de cada um é o do onset que o originou.

`quadroMs` e `caixa` dizem onde desenhar o objeto sobre o vídeo: `caixa` é
X1, Y1, X2, Y2 em fração da imagem, de 0 a 1, e vale para o quadro de
`quadroMs` - o que o detector olhou, até 3 s depois do onset -, e não para o
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
|---|---|---|
| `GET /api/deteccoes` | `antes` ou `depois`, `limite`, `cams`, `familias` | uma página de detecções, da mais nova para a mais velha |
| `GET /api/deteccoes/quadro` | `cam`, `t` | o JPEG do quadro que o detector olhou, `immutable` |

**`/api/deteccoes`** pagina por cursor, e não por número de página: o dia
corrente ganha detecção nova a todo momento, e "página 3" mudaria de conteúdo
entre um pedido e outro.

- `antes=<ms>`: as detecções anteriores a este instante, para rolar para baixo.
  Sem cursor nenhum, vêm as mais novas.
- `depois=<ms>`: as posteriores, para rolar para cima depois de um "ir para".
  São as mais próximas do cursor, mas a resposta vem na mesma ordem das outras:
  da mais nova para a mais velha.
- `limite`: quantas, de 1 a 200. Padrão 60.
- `cams=cam_a,cam_b` e `familias=pessoa,veiculo`: só essas. Sem eles, todas.
  Câmera não cadastrada ou família desconhecida é erro.

```json
{
  "deteccoes": [
    {
      "cam": "cam_teste1",
      "instanteMs": 1786220571043,
      "quadroMs": 1786220572851,
      "temQuadro": true,
      "objetos": [
        { "familia": "pessoa", "classe": "person", "score": 0.87, "caixa": [0.5156, 0.2611, 0.6734, 0.9778] },
        { "familia": "veiculo", "classe": "car", "score": 0.61, "caixa": [0.05, 0.4, 0.3, 0.7] }
      ]
    }
  ],
  "fim": false
}
```

Uma **detecção** é uma olhada do detector numa câmera: o instante do onset e
tudo o que ele achou naquele quadro. Pessoa e carro no mesmo quadro são duas
marcas no `eventos/{dia}.ndjson`, mas uma detecção só, com dois objetos. O
filtro de família tira objetos, e a detecção que fica sem nenhum sai da página.
`caixa` e `quadroMs` são os mesmos de `/api/rec/events`.

O próximo cursor é o `instanteMs` da última detecção da página. A página **não
parte um instante ao meio**: se a última empata, no mesmo milissegundo, com a de
outra câmera, as duas vêm e a página passa um pouco do limite - senão o cursor
pularia a que ficou de fora. `fim` diz que não há mais nada na direção pedida;
ele pode vir falso na página que por acaso pegou a última detecção, e a próxima
vem vazia e com `fim` verdadeiro.

A marca de objeto chega **atrasada**, como em `/api/rec/events`: ela leva o
instante do onset, mas só é gravada depois que o detector olha. Uma detecção
nova pode, então, cair abaixo do topo que a tela já mostrou.

O servidor memoriza as marcas de objeto de cada dia já lido, sem os onsets:
dia passado é lido do disco uma vez só, e do dia corrente só o pedaço que o
arquivo cresceu. Medido com 11 dias de uma instalação real, nove câmeras e 7.987
detecções: 0,85 MB em memória, e uma página de 60 em 0,2 ms depois da primeira
leitura.

**`/api/deteccoes/quadro`** serve o arquivo como ele está no disco, sem
redimensionar nem reencodar: `t` é o `instanteMs` da detecção. Ele nunca muda
depois de gravado, então vai com `immutable` e rolar de volta não pede nada ao
servidor. Detecção com `temQuadro` falso responde 404.

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
