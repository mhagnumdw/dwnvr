# dwnvr-detect

O detector de objetos do dwnvr, num container à parte. O dwnvr manda o pedaço
de vídeo de cada marca de movimento, e ele responde o que havia no último
quadro: pessoa, veículo, animal ou nada.

É opcional. Sem ele, o dwnvr marca só movimento. Ele fica fora do binário do
dwnvr porque decodificar vídeo e rodar um modelo exigem código nativo, e o
dwnvr é Go puro numa imagem de 8 MB.

## O que tem dentro

- `servidor.py` - o servidor HTTP: decodifica o pedaço com o PyAV e roda o
  modelo no onnxruntime. Um pedido por vez; quem enfileira é o dwnvr.
- `requirements.in` - as dependências diretas: `onnxruntime`, `numpy` e `av`.
- `requirements.txt` - gerado a partir do `.in`: as diretas e as indiretas, com
  versão exata e o hash de cada wheel, para amd64 e arm64.
- `modelo/` - o `.onnx` que vai na imagem, e como gerá-lo de novo (ver
  [`modelo/README.md`](modelo/README.md)).
- `Dockerfile` - faz o build de amd64 e arm64 em qualquer máquina, sem emulação.

## As dependências

A imagem não roda `pip install` na arquitetura dela. Um stage na arquitetura
da máquina de build baixa, com o uv, os wheels (pacotes já compilados) da
arquitetura de destino, e a imagem final só os copia. É o mesmo desenho do
`Dockerfile` do dwnvr, que faz cross-compile do Go: nada da arquitetura de
destino é executado no build, e por isso a imagem do Orange Pi sai de uma
máquina x86 sem QEMU.

O build recusa wheel cujo hash não seja o do `requirements.txt`, e recusa
código-fonte, que exigiria compilar.

Para trocar uma versão, edite o `requirements.in` e gere o `.txt` de novo:

```sh
cd dwnvr-detect
uv pip compile --universal --generate-hashes --python-version 3.12 \
  requirements.in -o requirements.txt
```

As versões atuais são as que exportaram o modelo e mediram a entrega. Outro
runtime é outro número: depois de trocar, confira que as caixas continuam as
mesmas para os mesmos pedaços.

## O modelo

RF-DETR Nano, treinado no COCO pela Roboflow (Apache-2.0), exportado a 512x288
e quantizado para int8. A calibração foi feita com quadros de câmeras de
segurança comuns, de dia e de noite. Em câmera de outra natureza (noite
colorida, olho de peixe, térmica) ou fora de 16:9, cujo quadro chega esticado,
ninguém mediu ainda. Detalhes em [`modelo/README.md`](modelo/README.md).

## O custo

Num Orange Pi Zero 3, uma olhada leva ~6,5 s com `DETECT_THREADS=1` e ~3,6 s
com 2, por ~10% a mais de CPU no total. O container ocupa ~210 MB. Quantas
olhadas por hora ele faz depende de quantas câmeras têm a detecção ligada e do
nível de sensibilidade de cada uma: ver `docs/configuracao.md`.

## Build local

Da raiz do repositório, uma arquitetura por vez:

```sh
docker buildx build --platform linux/arm64 -t dwnvr-detect:arm64 --load dwnvr-detect/
docker buildx build --platform linux/amd64 -t dwnvr-detect:amd64 --load dwnvr-detect/
```

## O contrato

```
POST /detect?piso=0.20       corpo: o pedaço (.mp4), do frame I ao quadro a olhar
GET  /health
```

```json
{"achados": [{"classe": "person", "score": 0.87, "caixa": [0.31, 0.12, 0.38, 0.55]}],
 "quadrosDecodificados": 14, "largura": 640, "altura": 360,
 "tempoMs": {"decodifica": 180.2, "preparo": 190.4, "modelo": 5900.1}}
```

A caixa vai em fração do quadro (x1, y1, x2, y2). Ele devolve TODA caixa a
partir do `piso`, de qualquer classe, e não decide nada: o corte por família e
a memória de objeto parado são do dwnvr, que só lê `achados`. Os outros campos
são para quem chama à mão. Pedaço que não decodifica até o fim volta `422`:
olhar o último quadro que sobrou seria olhar outro instante.

Configuração, por variável de ambiente:

| Variável | O que é | Padrão |
|---|---|---|
| `DETECT_MODELO` | o `.onnx` | `/app/modelo.onnx` |
| `DETECT_THREADS` | threads do modelo e do vídeo | `1` |
| `DETECT_PORTA` | porta HTTP | `8480` |

A porta não é a 8555 porque essa é a do WebRTC do go2rtc.

## Licença

O modelo é derivado dos pesos do RF-DETR, Copyright 2025 Roboflow, Inc., sob a
Apache License 2.0. A cópia da licença está em
[`modelo/LICENSE-RF-DETR`](modelo/LICENSE-RF-DETR).
