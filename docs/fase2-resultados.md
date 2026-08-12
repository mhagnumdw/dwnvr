# Fase 2 - API HTTP

Validada no Orange Pi Zero 3 com as 9 câmeras gravando, 08/08/2026.

## Endpoints

```
POST /api/login  /api/logout       sessão por cookie assinado
GET  /api/session                  público: diz se precisa de login
GET  /api/cameras                  câmeras cadastradas + streams do go2rtc
GET  /api/health                   bitrate medido, dias estimados, disco
GET  /api/rec/days                 dias com gravação
GET  /api/rec/timeline             faixas para desenhar + segmentos para tocar
GET  /api/rec/init                 init segment, immutable
GET  /api/rec/seg                  fragmentos (sem o init), immutable
GET  /api/rec/thumb                MP4 de 1 frame
GET  /api/rec/playlist.m3u8        HLS VOD
GET  /api/rec/export               MP4 único emendado
GET  /api/live/*                   proxy para o go2rtc
```

## Verificações

| Verificação | Resultado |
|---|---|
| `thumb` é mesmo 1 frame | 64 KB, **1 frame**, decodifica 1920x1080 |
| `seg` realmente pula o init | não abre sozinho (`no tfhd was found`); com o init, abre |
| `init` cacheável | `Cache-Control: immutable`, 737 bytes |
| playlist | 5 segmentos, `EXT-X-MAP` + `EXT-X-PROGRAM-DATE-TIME` |
| export | duração **igual à soma dos segmentos**, DTS estritamente crescente |
| sessão sem cookie | 401 em todos os endpoints de dados |
| assinatura adulterada | 401 |
| validade esticada | 401 (o HMAC cobre o prazo) |
| `cam=../../etc` | 400 |
| `g=../../../etc/passwd` | 400 |
| RSS com API + gravação | 19,7 MB |

## Três bugs encontrados rodando contra o hardware real

### 1. `medias` do go2rtc é lista de strings, não de objetos

Eu havia modelado `medias` como `[]struct{Kind, Direction string}`. O go2rtc
devolve `["video, recvonly, H265", "audio, recvonly, PCMA/16000"]` - texto no
formato SDP. O decode falhava inteiro e a descoberta de streams ficava vazia.

A degradação graciosa salvou a tela: a lista de câmeras cadastradas continuou
respondendo, com o erro reportado em `go2rtcError` à parte. Travado por um teste
que usa uma resposta real do go2rtc como fixture.

### 2. A duração indexada omitia o último frame

`DurMs` guardava o DTS do último frame, não o fim do segmento - faltava somar a
duração desse frame. Consequência na exportação: cada segmento era posicionado
exatamente **em cima** do último frame do anterior.

| | Antes | Depois |
|---|---|---|
| Regressões de DTS no export | 4 (uma por emenda) | **0** |
| Erros `Could not find ref` | ~110 | **0** |
| Duração do export | 184,8s (errada) | igual à soma dos segmentos |

Isso também corrige um diagnóstico anterior: a enxurrada de erros de referência
na exportação **era esse bug**, não perda de pacote UDP. A perda de UDP existe,
mas rende 0-2 avisos por segmento, não centenas.

### 3. Segmentos se sobrepunham no tempo de parede

O início do segmento vem do relógio de parede e a duração vem do relógio de
mídia. Os dois derivam com o jitter da rede - medido em **±1,1s num segmento de
30s** -, e quando a mídia adiantava, o segmento novo começava *antes* de o
anterior terminar. No MSE o trecho sobreposto é sobrescrito, ou seja, perde-se
gravação.

Agora o início é ancorado no fim do segmento anterior quando haveria
sobreposição. Medido depois da correção: **0 sobreposições**, com folga de 0 ms
nas emendas contínuas.

A pergunta seguinte era se ancorar assim faria o horário derivar do relógio real.
Medido contra os mtimes dos arquivos, que são uma referência independente:

| | |
|---|---|
| Mídia acumulada | 316,76s |
| Parede (mtimes) | 316,72s |
| Diferença | **+0,01%**, ou ~10s projetados em 24h |

E qualquer buraco real (reconexão, câmera fora do ar) reancora no relógio de
parede, porque aí `agora` passa o fim do anterior e vence a comparação. Na
prática o desvio não acumula.

## Um susto que era comportamento correto

Logo depois de um reinício, a `cam_portao` apareceu com 12 faixas separadas e
buracos de até 39s. Investigando o log: o go2rtc devolveu **HTTP 500** por ~40s
para essa câmera e para a `cam_frente`. O backoff subiu 1s→2s→4s→8s e reconectou
sozinho.

Ou seja, os buracos eram **reais** e a timeline estava certa ao mostrá-los - que
é exatamente para isso que ela existe. No mesmo período, `cam_frente` e
`cam_quintal` estabilizadas mostraram uma única faixa contínua.

## Decisões de projeto

**A interface é servida sem autenticação, os dados não.** HTML, CSS e JS não
contêm nada das câmeras, e protegê-los impediria o navegador de carregar a
própria tela de login.

**A resposta da timeline é compacta.** Um dia com segmentos de 1 minuto tem 1440
entradas; `segments` é `[[inícioMs, duraçãoMs, índiceDeGeração]]` com uma tabela
de gerações à parte, em vez de objetos com chaves repetidas.

**A exportação recusa atravessar troca de geração.** Um MP4 só tem um init;
entregar algo que toca até a metade seria pior que recusar. Note a limitação
conhecida de H265: como o init é sempre o mesmo dummy, uma troca de resolução
nessas câmeras não é detectada por esse teste.

## Nota de ambiente

O Pi roda `firewalld`, que bloqueia a porta 8080 vinda de fora. O go2rtc não
sofre disso porque o Docker insere as próprias regras. Durante o desenvolvimento
o acesso foi por túnel SSH; quando o dwnvr for empacotado em Docker na Fase 5,
cairá no mesmo caminho do go2rtc.
