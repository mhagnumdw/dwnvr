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
- `sitecustomize.py` - a trava que impede o Python do container de compilar
  módulo do fonte em runtime (ver [O bytecode](#o-bytecode)).
- `confere_pyc.py` - a conferência do `.pyc` da imagem, que roda no build.
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

## O bytecode

Nada se compila em runtime. O build gera o bytecode (`.pyc`) do stdlib, das
libs e do código daqui, que nem a imagem do Python nem o uv trazem. Sem ele,
cada subida do servidor compilava do fonte o que importa: 5,7 s num Orange Pi
Zero 3, contra 1,9 s com ele, por 22 MB a mais na imagem.

O stdlib compilado é o da própria imagem final, copiado para o stage sem
executar nada dele, e cada `.pyc` guarda o hash do seu `.py`, e não a data. O
servidor sobe como módulo (`python3 -m servidor`), porque o script que recebe
pelo caminho o Python sempre compila do fonte.

O `.pyc` que falta ou não bate, o Python ignora calado e compila o fonte: a
imagem ficaria lenta sem ninguém ver. Duas travas impedem isso:

- **No build**, o `confere_pyc.py` olha os arquivos da imagem final já montada:
  todo `.py` que o servidor pode importar tem de ter o seu `.pyc`, checked-hash
  da mesma versão do Python e com o hash do `.py` que a imagem traz. Se não
  tiver, o build falha e a imagem não sai.
- **Em runtime**, o `sitecustomize.py`, que o Python carrega antes de qualquer
  import, troca a compilação do fonte por um erro com o caminho do arquivo: ou
  o módulo vem do `.pyc`, ou o servidor não sobe. Vale também para um
  `docker exec ... python3`; para depurar à mão sem a trava, `python3 -S`.

Fica de fora só o `-c` de uma linha do healthcheck, que não é módulo.

## O modelo

RF-DETR Nano, treinado no COCO pela Roboflow (Apache-2.0), exportado a 512x288
e quantizado para int8. A calibração foi feita com quadros de câmeras de
segurança comuns, de dia e de noite. Em câmera de outra natureza (noite
colorida, olho de peixe, térmica) ou fora de 16:9, cujo quadro chega esticado,
ninguém mediu ainda (ver
[`TODO_modelo-em-cameras-de-terceiros.md`](../docs/TODO/TODO_modelo-em-cameras-de-terceiros.md)).
Detalhes em [`modelo/README.md`](modelo/README.md).

## O custo

Num Orange Pi Zero 3, uma olhada leva ~6,5 s com `DETECT_THREADS=1` e ~3,6 s
com 2, por ~10% a mais de CPU no total. Sem a variável, ele roda lá com 2
threads: a metade dos 4 núcleos da placa. O container ocupa ~230 MB, com picos
de ~250 MB. Quantas olhadas por hora ele faz depende de quantas câmeras têm a
detecção ligada e do nível de sensibilidade de cada uma: ver
`docs/configuracao.md`.

## Build local

Da raiz do repositório, uma arquitetura por vez:

```sh
docker buildx build --platform linux/arm64 -t dwnvr-detect:arm64 --load dwnvr-detect/
docker buildx build --platform linux/amd64 -t dwnvr-detect:amd64 --load dwnvr-detect/
```

## O contrato

```text
POST /detect?piso=0.20&larguraDoQuadro=480&qualidade=75
                                      corpo: o pedaço (.mp4), do frame I ao quadro a olhar
GET  /health
```

```json
{
  "achados": [
    {
      "classe": "person",
      "score": 0.87,
      "caixa": [0.31, 0.12, 0.38, 0.55]
    }
  ],
  "quadrosDecodificados": 14,
  "largura": 640,
  "altura": 360,
  "tempoMs": {
    "decodifica": 180.2,
    "preparo": 190.4,
    "modelo": 5900.1,
    "quadro": 11.4
  },
  "quadro": "/9j/4AAQSkZJRgABAQAAAQ… (base64)"
}
```

Com `larguraDoQuadro=<largura>`, e havendo achado, a resposta traz também o quadro
olhado em JPEG, reduzido àquela largura, em base64. É a miniatura da tela de
Detecções: o quadro sai daqui porque aqui ele já está decodificado. Sem o
parâmetro, ou sem achado, o campo não vem.

A caixa vai em fração do quadro (x1, y1, x2, y2). Ele devolve TODA caixa a
partir do `piso`, de qualquer classe, e não decide nada: o corte por família e
a memória de objeto parado são do dwnvr, que só lê `achados`. Os outros campos
são para quem chama à mão. Pedaço que não decodifica até o fim volta `422`:
olhar o último quadro que sobrou seria olhar outro instante.

Configuração, por variável de ambiente:

| Variável | O que é | Padrão |
| --- | --- | --- |
| `DETECT_MODELO` | o `.onnx` | `/app/modelo.onnx` |
| `DETECT_THREADS` | threads do modelo e do vídeo | `auto` |
| `DETECT_PORTA` | porta HTTP | `8480` |

O `auto` é metade dos núcleos que o container enxerga, até 4, e nunca acima
do teto de CPU do container, se alguém puser um (o `docker-compose.yml` do
dwnvr não põe). A outra metade fica para a gravação. O número escolhido, e de
onde ele saiu, vai na primeira linha do log:

```text
dwnvr-detect: modelo.onnx, entrada 512x288, 2 thread(s) (auto: metade de 4 núcleos), porta 8480
```

Um número a partir de 1 passa por cima do `auto`, e vale como veio. Não o
ponha acima do teto de CPU do container: com `cpus: 2`, 4 threads deixam a
olhada tão lenta quanto 1, gastando o dobro. O `0` é recusado, porque é o
automático do onnxruntime, que conta os núcleos da máquina e não os do
container.

Com o `docker-compose.yml` do dwnvr, o número vai no `.env` ao lado dele, como
por exemplo `DETECT_THREADS=2`, e vale no próximo `docker compose up -d`.

A porta não é a 8555 porque essa é a do WebRTC do go2rtc.

## Licença

O modelo é derivado dos pesos do RF-DETR, Copyright 2025 Roboflow, Inc., sob a
Apache License 2.0. A cópia da licença está em
[`modelo/LICENSE-RF-DETR`](modelo/LICENSE-RF-DETR).
