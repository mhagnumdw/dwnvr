# TODO - o áudio da câmera só aparece depois que ela já está gravando

Levantado em 19/08/2026, ao acrescentar a `cam_teste5` (a câmera de teste com áudio) no `go2rtc.example.yaml`.

## O sintoma

Numa câmera **recém-descoberta**, ainda não cadastrada, a tela de Câmeras mostra
`flac` e `aac` desabilitados no seletor de áudio - mesmo quando a câmera tem
microfone. Só depois de cadastrá-la com `audio: none`, esperar o gravador
conectar e reabrir o formulário é que as duas opções ficam clicáveis.

## Por quê

`Producer.HasAudio()` (`internal/go2rtc/client.go:186`) lê o campo `medias` que o
go2rtc publica em `GET /api/streams`, e o go2rtc **só preenche `medias` enquanto
existe alguém consumindo o stream**. Sem consumidor ele nem abre a conexão com a
câmera, então não tem como saber quais trilhas ela oferece.

Medido com a `cam_teste5`, que entrega `H264 + PCMA/16000`:

| estado do stream | `medias` no go2rtc | `hasAudio` no dwnvr |
|---|---|---|
| ocioso | ausente | `false` |
| com consumidor ligado | `["video, recvonly, H264", "audio, recvonly, PCMA/16000"]` | `true` |

O gravador é o consumidor - por isso o ovo antes da galinha: para o dwnvr
descobrir que tem áudio, a câmera precisa já estar sendo gravada.

Não é específico das câmeras de teste: vale para qualquer fonte, RTSP de câmera
de verdade inclusive.

## Caminhos possíveis

1. **Sondar sob demanda**: ao abrir o formulário de cadastro, o dwnvr consome o
   stream por 1-2s só para o go2rtc preencher `medias`. Custa uma conexão à
   câmera por abertura de tela.
2. **Não desabilitar**: deixar `flac`/`aac` sempre clicáveis e tratar a ausência
   de áudio no gravador, que já sabe lidar com isso (`audio=false` no log de
   `conectado`). Perde o aviso "esta câmera não tem áudio".
3. **Deixar como está** e documentar o passo extra.

## Como reproduzir

```sh
docker compose up -d
curl -s localhost:8080/api/cameras | jq '.streams[] | select(.name=="cam_teste5")'
# hasAudio: false

curl -sX POST localhost:8080/api/cameras -H 'Content-Type: application/json' \
  -d '{"id":"cam_teste5","name":"Teste","enabled":true,"audio":"none"}'
sleep 10
curl -s localhost:8080/api/cameras | jq '.streams[] | select(.name=="cam_teste5")'
# hasAudio: true
```
