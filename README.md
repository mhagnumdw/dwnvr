# dwnvr <!-- omit in toc -->

![Logo](web/public/favicon.svg)

NVR de gravação contínua **projetado com foco em hardware extremamente
limitado**. Ele não é feito *para* um hardware específico.

O dwnvr vem sendo testado durante todo o seu desenvolvimento em um Orange Pi
Zero 3 (4 cores Cortex-A53, 1,5 GB RAM), gravando 9 câmeras Yoosee 24/7.

**Sem transcodificação de vídeo. Sem banco de dados. Sem detecção de movimento.**

Medido nesse Orange Pi Zero 3 com as 9 câmeras gravando simultaneamente:

| | CPU | RAM |
|---|---|---|
| dwnvr | **4% de 1 core** (1% dos 4) | **17 MB** |
| go2rtc | 9% de 1 core | 23 MB |

- [Como funciona](#como-funciona)
- [Tecnologias](#tecnologias)
- [Estrutura do projeto](#estrutura-do-projeto)
  - [`cmd/` - convenção da comunidade](#cmd---convenção-da-comunidade)
  - [`internal/` - exigência do Go](#internal---exigência-do-go)
  - [`internal/api/dist/` - o build da interface, versionado](#internalapidist---o-build-da-interface-versionado)
  - [`web/src/vendor/` - convenção, e do outro lado da cerca](#websrcvendor---convenção-e-do-outro-lado-da-cerca)
  - [`*_test.go` - exigência do Go](#_testgo---exigência-do-go)
- [Subir para um teste rápido](#subir-para-um-teste-rápido)
- [Build](#build)
- [Testes](#testes)
- [Instalação](#instalação)
- [Configuração](#configuração)
- [Estado atual](#estado-atual)
- [Documentação](#documentação)

## Como funciona

```
9 câmeras ──RTSP──> go2rtc ──HTTP fMP4──> dwnvr ──> disco
                      │                     │
                      └──WebRTC/MSE─────────┴──> navegador
                         (live, via proxy)      (gravações)
```

O dwnvr consome o fMP4 que o **go2rtc já produz** (`/api/stream.mp4`) e corta em
segmentos alinhados a keyframe. Ele nunca decodifica, nunca remuxa e nunca toca
nos bytes de mídia - só lê cabeçalhos de caixa para saber onde cortar. É daí que
vêm os 4% de um core.

> **Segmento** é um arquivo de vídeo curto - por padrão ~30s - que toca
> sozinho. A gravação de um dia não é um arquivo gigante: é uma fila de
> segmentos, e é por isso que apagar o mais antigo, pular para um horário
> específico ou exportar um trecho custa quase nada.

**O go2rtc fica fora do escopo.** Configurá-lo é responsabilidade de quem
instala; o dwnvr apenas descobre os streams existentes via `/api/streams` e
recomenda configuração.

> Talvez no futuro o dwnvr faça a configuração automática
do go2rtc, mas por enquanto não.

O porquê de cada decisão de formato está em
[`docs/arquitetura.md`](docs/arquitetura.md).

## Tecnologias

| O quê | Onde entra |
| --- | --- |
| **Go 1.24** | Todo o servidor. Só a biblioteca padrão, **sem [cgo](https://pkg.go.dev/cmd/cgo)** - o binário é estático, de ~3 MB |
| **go2rtc** | Fonte dos streams: fala RTSP com as câmeras e entrega fMP4, WebRTC e MJPEG |
| **fMP4** (MP4 fragmentado) | O formato em disco. É o que o go2rtc já produz, então gravar é copiar bytes |
| **NDJSON** | O índice das gravações, um arquivo por câmera por dia, append-only |
| **Svelte 5 + Vite** | A interface, embutida no binário com `go:embed` |
| **MSE** (Media Source Extensions) | Player das gravações, escrito à mão para não carregar [hls.js](https://github.com/video-dev/hls.js/) |
| **Docker** | Imagem `FROM scratch` multi-arch, para `linux/arm64` e `linux/amd64` |

A única dependência Go do projeto é `gopkg.in/yaml.v3`. Não há banco de dados,
ORM, framework HTTP, ffmpeg nem detecção de movimento - e essa ausência é o
projeto, não uma etapa que faltou.

## Estrutura do projeto

```
├── cmd/
│   ├── dwnvr/              o binário: lê a config, sobe um recorder por câmera, serve HTTP
│   ├── spike/              Fase 0: provou que dá para gravar sem decodificar
│   └── spike-serve/        Fase 0: provou que dá para tocar no navegador via MSE
├── internal/
│   ├── api/                servidor HTTP
│   │   ├── server.go       rotas e o que exige sessão
│   │   ├── auth.go         sessão por cookie assinado (HMAC), sem estado no servidor
│   │   ├── cameras.go      cadastro de câmeras, cruzado com os streams do go2rtc
│   │   ├── recordings.go   dias, timeline, init, segmentos, thumbnail, HLS, exportação
│   │   ├── live.go         proxy do go2rtc, com a credencial ficando no servidor
│   │   ├── web.go          serve a SPA embutida
│   │   └── dist/           build da interface, versionado (ver web/README.md)
│   ├── buildinfo/          versão, commit e data injetados no build
│   ├── config/             leitura do dwnvr.yaml (infra) e do cameras.json (câmeras)
│   ├── fmp4/               leitor de caixas MP4 - o coração do "sem decodificar"
│   │   ├── box.go          percorre as caixas sem tocar em mídia
│   │   ├── moov.go         lê o init segment (ftyp+moov) e suas trilhas
│   │   ├── fragment.go     lê moof+mdat e descobre quais frames são keyframe
│   │   ├── rebase.go       reescreve o tfdt para cada segmento começar do zero
│   │   ├── probe.go        hash do init e sondagem de segmento órfão
│   │   └── sps.go          resolução real a partir do SPS de H264/H265
│   ├── go2rtc/             cliente da API do go2rtc e do stream fMP4
│   ├── recorder/           um recorder por câmera: corta em keyframe e grava
│   ├── retention/          apaga o mais antigo quando cota, idade ou disco estouram
│   └── store/              layout em disco e índice NDJSON
├── web/                    interface Svelte 5 + Vite (ver web/README.md)
├── docs/                   documentação longa (ver docs/README.md)
├── docker-compose.yml      dwnvr + go2rtc, pronto para subir
├── Dockerfile              imagem FROM scratch, multi-arch
├── dwnvr.example.yaml      configuração do dwnvr, campo a campo
├── go2rtc.example.yaml     configuração do go2rtc, com uma câmera sintética
└── Makefile                build, testes, imagem e deploy
```

<details>
<summary><b>Por que as pastas se chamam assim</b></summary>

Alguns nomes acima não são escolha deste projeto. Uns são **convenção da
comunidade Go** - você pode ignorá-los e nada quebra, só fica estranho para
quem lê. Outros são **exigência do Go**: mudar o nome faz o build falhar.
Resumo antes do detalhe:

| Caminho | O que é | Se você renomear |
|---|---|---|
| `cmd/` | convenção da comunidade | compila igual, só surpreende quem lê |
| `internal/` | **exigência do Go** | o pacote passa a ser importável por qualquer projeto |
| `internal/api/dist/` | escolha nossa, **imposta pelo `go:embed`** | o build quebra |
| `web/src/vendor/` | convenção, e do lado JavaScript | nada acontece |
| `*_test.go` | **exigência do Go** | o arquivo passa a entrar no binário final |

### `cmd/` - convenção da comunidade

Não é invenção deste projeto nem exigência do compilador: é o hábito adotado em
praticamente todo projeto Go de porte - Kubernetes, Docker, Prometheus, o
próprio go2rtc.

A regra é simples: **cada subdiretório de `cmd/` vira um binário**, e é o único
lugar onde mora `package main`. O nome do subdiretório é o nome do executável -
`cmd/dwnvr` produz `dwnvr`. É por isso que aqui existem três pastas mesmo que
só uma vire produto: `spike` e `spike-serve` são binários próprios, então não
teriam onde mais ficar.

O que a convenção compra: `main` fica magro - lê configuração, monta as peças e
sai da frente, com toda a lógica em pacotes testáveis sob `internal/`;
`go build ./cmd/dwnvr` fica inequívoco, sem caçar qual arquivo tem a função
`main`; e acrescentar um segundo binário não reorganiza nada.

### `internal/` - exigência do Go

Aqui não é hábito, é regra que o próprio Go impõe. Um pacote sob `internal/` só
pode ser importado de dentro do próprio módulo. Outro projeto que tente
importar `github.com/mhagnumdw/dwnvr/internal/store` recebe:

```
use of internal package github.com/mhagnumdw/dwnvr/internal/store not allowed
```

É o que permite reorganizar tudo que está aqui dentro sem quebrar ninguém lá
fora: nada disto é API pública, e o Go garante isso em vez de pedir por favor.

### `internal/api/dist/` - o build da interface, versionado

Commitar artefato gerado costuma ser sinal de desleixo. Aqui é deliberado, e
duas restrições explicam o formato.

**Por que está versionado:** `go:embed` exige que os arquivos existam em tempo
de compilação. Com o `dist` no repositório, `go build ./cmd/dwnvr` funciona num
clone limpo, sem Node instalado - o que importa porque o alvo é um dispositivo
onde ninguém quer instalar toolchain de frontend.

**Por que fica dentro de `internal/api/`, e não em `web/dist/`:** o `go:embed`
não consegue sair do diretório do pacote. Um `//go:embed ../web/dist` não
compila:

```
pattern ../web/dist: invalid pattern syntax
```

Por isso o `vite.config.js` manda o build para `../internal/api/dist`, ao lado
do `web.go` que o embute. Ao mexer em `web/`, rode `npm run build` **antes** de
commitar - a CI reprova se os dois divergirem.

### `web/src/vendor/` - convenção, e do outro lado da cerca

Guarda código de terceiros: o player de live do go2rtc (MIT), copiado sem
modificação. Ver [`web/src/vendor/README.md`](web/src/vendor/README.md).

Uma armadilha de leitura: em Go, um diretório `vendor/` **na raiz do módulo** é
especial - é onde `go mod vendor` despeja as dependências, e a partir daí o
build passa a usá-las em vez do cache de módulos. Este `vendor/` não é aquele:
está dentro de `web/`, é JavaScript, e para o Go não significa nada. O nome foi
emprestado pelo costume, não pela regra.

### `*_test.go` - exigência do Go

O sufixo não é estilo: **o Go só compila esses arquivos durante `go test`**.
Eles ficam de fora do binário final, o que permite deixá-los ao lado do código
que exercitam sem inchar o que vai para produção - e é por isso que aqui não
existe uma pasta `tests/` separada.

A parte que é convenção: mantê-los no **mesmo pacote** do código testado, o que
dá acesso ao que não é exportado.

Referências: [Organizing a Go module](https://go.dev/doc/modules/layout),
[go/build - build constraints](https://pkg.go.dev/go/build#hdr-Build_Constraints)
e [golang-standards/project-layout](https://github.com/golang-standards/project-layout).

</details>

## Subir para um teste rápido

**Você não precisa de uma câmera real.** O `go2rtc.example.yaml` traz uma fonte
sintética: o ffmpeg que já vem na imagem do go2rtc desenha uma carta de teste
com relógio, e o go2rtc a publica como H264 720p. Do ponto de vista do dwnvr é
indistinguível de uma câmera de verdade.

O dwnvr roda como binário local e o go2rtc num container - assim você testa a
sua árvore de trabalho, e não uma imagem publicada:

```sh
git clone https://github.com/mhagnumdw/dwnvr && cd dwnvr

cp go2rtc.example.yaml go2rtc.yaml
cp dwnvr.example.yaml  dwnvr.yaml

# Grava em ./storage, sem exigir que /mnt/storage/dwnvr exista na sua máquina.
sed -i 's|/mnt/storage/dwnvr|./storage|' dwnvr.yaml

# A fonte dos streams. O dwnvr.example.yaml já aponta para localhost:1984.
docker run -d --name go2rtc -p 1984:1984 \
  -v "$PWD/go2rtc.yaml:/config/go2rtc.yaml" alexxit/go2rtc

# Como o internal/api/dist é versionado, isto compila sem Node instalado.
go build ./cmd/dwnvr && ./dwnvr -config dwnvr.yaml
```

Abra <http://localhost:8080>, entre em **Câmeras** e cadastre a `cam_teste`,
que já vai estar listada como stream disponível. Em ~30s o primeiro segmento
fecha e aparece na aba **Gravações**. Para ver acontecer mais rápido, baixe
`segmentSeconds` para 10 no `dwnvr.yaml`.

Se preferir conferir por fora da interface, o primeiro segmento gravado
aparece assim:

```sh
find storage -name '*.mp4' | head    # o init, e um arquivo por segmento

# Qualquer segmento abre sozinho, sem pré-processamento e sem o init ao lado.
ffprobe "$(find storage/cam_teste/2* -name '*.mp4' | head -1)"
```

Para derrubar tudo e apagar o que foi gravado:

```sh
docker rm -f go2rtc
rm -rf storage dwnvr.yaml cameras.json go2rtc.yaml .session-secret
```

> Todos os arquivos criados acima já estão no `.gitignore` - o teste não suja
> o `git status`. Para a instalação de verdade, com os dois serviços em
> containers, veja [Instalação](#instalação).

## Build

```sh
make all         # interface + binário local, na ordem certa
make help        # lista todos os alvos
```

**A interface precisa ser construída antes do binário**, porque o Go a embute
com `go:embed`. Esquecer isso produz um binário que compila e sobe
normalmente, mas serve a tela antiga - um erro silencioso. O `Makefile`
encadeia as duas coisas, e a CI reprova se o `internal/api/dist` versionado
divergir de `web/`.

| Alvo | O que faz |
|---|---|
| `make web` | constrói a interface para `internal/api/dist` (precisa de Node) |
| `make build` | binário para a máquina local |
| `make arm64` | binário estático para o Orange Pi e qualquer aarch64 Linux |
| `make amd64` | binário estático para x86_64 Linux |
| `make image` | imagem Docker multi-arch |
| `make run-pi` | constrói a imagem arm64 e recria o container no Orange Pi via ssh |

Como o `internal/api/dist` é versionado, **`go build ./cmd/dwnvr` funciona num
clone limpo sem Node instalado**. Isso é deliberado: o alvo é um dispositivo
onde ninguém quer instalar toolchain de frontend.

## Testes

```sh
make test        # testes de unidade
make check       # o que a CI roda: testes + go vet + gofmt + dist em dia
```

Os testes vivem ao lado do código que exercitam, em `internal/*/*_test.go`, e
cobrem o que quebra em silêncio: a leitura de caixas fMP4, a reescrita do
`tfdt`, o corte em keyframe, a reconciliação de órfãos, a retenção e os
endpoints HTTP.

O workflow de CI está em `.github/workflows/ci.yml` e roda a cada push.

## Instalação

Os dois serviços, lado a lado:

```yaml
services:
  dwnvr:
    image: ghcr.io/mhagnumdw/dwnvr:latest
    restart: unless-stopped
    depends_on: [go2rtc]
    # Sem isto o container grava como root, e os arquivos nascem de root no
    # disco. Descubra o seu com: id -u; id -g
    user: "1000:1000"
    ports: ["8080:8080"]
    environment:
      # Sem TZ, todos os dias são calculados em UTC e a virada de dia da
      # timeline cai no horário errado.
      - TZ=America/Fortaleza
    volumes:
      - ./config:/etc/dwnvr          # dwnvr.yaml e cameras.json
      - /mnt/storage/dwnvr:/storage  # as gravações
    # Rede de segurança: o consumo medido com 9 câmeras é de ~20 MB.
    deploy:
      resources:
        limits:
          memory: 128M

  go2rtc:
    image: alexxit/go2rtc
    restart: unless-stopped
    shm_size: 128mb
    volumes:
      - ./go2rtc.yaml:/config/go2rtc.yaml
    ports:
      - "1984:1984"      # API e interface web do go2rtc
      - "8554:8554"      # servidor RTSP
      - "8555:8555/tcp"  # WebRTC
      - "8555:8555/udp"
```

No `dwnvr.yaml`, aponte para o serviço vizinho - o compose já resolve o nome:

```yaml
go2rtc:
  url: http://go2rtc:1984
```

Ver [`docker-compose.yml`](docker-compose.yml) completo. Imagem `FROM scratch`
de ~3 MB comprimidos, para **linux/arm64** e **linux/amd64**.

O go2rtc aparece aqui por conveniência, para você subir tudo de uma vez, **mas
continua fora do escopo do dwnvr**: o conteúdo do `go2rtc.yaml` - as câmeras,
as URLs RTSP, os codecs - é responsabilidade de quem instala. Se você já tem um
go2rtc rodando em outro compose ou direto no host, remova o serviço daqui,
troque a `url` para `http://host.docker.internal:1984` e acrescente ao dwnvr:

```yaml
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

Para binários soltos, sem Docker: `make arm64` ou `make amd64`.

A imagem não tem shell - nem `sh`, nem `ls`, nem `cat`. Isso não atrapalha a
operação, porque **configuração e gravações vivem nos volumes, no host**, e
porque `docker logs`, `docker cp` e um sidecar de namespaces cobrem o resto.
Ver [`docs/operacao.md`](docs/operacao.md).

## Configuração

Dois arquivos, de propósito:

- **`dwnvr.yaml`** - infraestrutura, editado à mão, **nunca reescrito** pela
  aplicação. Veja [`dwnvr.example.yaml`](dwnvr.example.yaml).
- **`cameras.json`** - a lista de câmeras, gravada pela tela de cadastro.

Estão separados porque têm ciclos de vida diferentes: a infraestrutura é
editada à mão e quase nunca muda; a lista de câmeras é gravada pela aplicação a
cada clique. Num arquivo só, o dwnvr teria que reescrever, várias vezes por dia,
um arquivo que alguém pode ter aberto no editor naquele instante - apagando no
caminho os comentários que essa pessoa escreveu. Do jeito que está, um erro na
tela de cadastro ou uma queda de energia no meio dela não alcançam a
configuração do serviço.

Tudo que é política de gravação é **por câmera**: qual stream do go2rtc usar
(alta ou baixa resolução), áudio, cota, tamanho do segmento e o limiar de
inatividade. Detalhes, incluindo o custo de cada modo de áudio em CPU e disco,
em [`docs/configuracao.md`](docs/configuracao.md).

## Estado atual

- [x] **Fase 0** - spikes validando gravação e playback ([resultados](docs/fase0-resultados.md))
- [x] **Fase 1** - recorder, índice e retenção ([resultados](docs/fase1-resultados.md))
- [x] **Fase 2** - API HTTP, autenticação, exportação ([resultados](docs/fase2-resultados.md))
- [x] **Fase 3** - SPA Svelte com as quatro telas ([resultados](docs/fase3-resultados.md))
- [x] **Fase 4** - Docker multi-arch e empacotamento ([resultados](docs/fase4-resultados.md))

> **Spike** é um programa descartável escrito só para responder uma pergunta
> antes de comprometer o projeto com ela - aqui, "dá mesmo para gravar o fMP4
> do go2rtc sem decodificar, dentro do orçamento de CPU e RAM?". Ele não vira
> produto; serve para transformar uma aposta em fato medido.

`cmd/spike` e `cmd/spike-serve` continuam no repositório porque documentam como
as premissas foram verificadas.

## Documentação

| Documento | Para quê |
|---|---|
| [`docs/operacao.md`](docs/operacao.md) | o dia a dia: arquivos, logs, container sem shell |
| [`docs/configuracao.md`](docs/configuracao.md) | os dois arquivos, política por câmera, retenção, áudio |
| [`docs/arquitetura.md`](docs/arquitetura.md) | o formato em disco e por que ele é assim |
| [`docs/resiliencia.md`](docs/resiliencia.md) | queda de energia e o go2rtc que emudece sem avisar |
| [`docs/api.md`](docs/api.md) | referência dos endpoints HTTP |
| [`web/README.md`](web/README.md) | desenvolver a interface |
| [`docs/README.md`](docs/README.md) | índice completo, incluindo as medições datadas |
