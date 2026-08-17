# dwnvr <!-- omit in toc -->

![Logo](web/public/favicon.svg)

NVR de gravação contínua **projetado com foco em hardware extremamente
limitado**. Ele não é feito *para* um hardware específico.

O dwnvr vem sendo testado durante todo o seu desenvolvimento em um Orange Pi
Zero 3 (4 cores Cortex-A53, 1,5 GB RAM), gravando 9 câmeras Yoosee 24/7.

**Sem transcodificação de vídeo. Sem banco de dados. Sem detecção de movimento.**

Medido nesse Orange Pi Zero 3 com as 9 câmeras gravando simultaneamente:

| | CPU | RAM |
| --- | --- | --- |
| dwnvr | **4% de 1 core** (1% dos 4) | **17 MB** |
| go2rtc | 9% de 1 core | 23 MB |

> **ATENÇÃO:** esse projeto é totalmente vibe codado e é meu primeiro projeto assim. Além de querer resolver uma necessidade minha, que eu acho que é de várias outras pessoas, eu queria saber como seria a experiência de desenvolver totalmente nesse estilo.
>
> Embora seja vibe codado, o projeto já nasceu desde o início com foco em exterma performance, baixíssimo consumo de CPU e memória, tempo de resposta ultra rápido, uma UI super rápida, leve, reativa e responsiva com excelente usabilidade para mobile e desktop (browser). Parte disso era uma necessidade em razão do hardware real que usei e uso, que é um Orange Pi Zero 3 e tudo isso se constata nos testes que faço e no meu uso no dia a dia. Testei diversas outras opções e nenhuma passou perto dos resultados que tenho, fora outros problemas/chatices diversas.

- [Como funciona](#como-funciona)
- [Subir o dwnvr](#subir-o-dwnvr)
- [Instalação definitiva](#instalação-definitiva)
- [Tecnologias](#tecnologias)
- [Estrutura do projeto](#estrutura-do-projeto)
- [Build](#build)
- [Testes](#testes)
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

## Subir o dwnvr

**Você só precisa de Docker.** Nem câmera, nem Go, nem Node: o compose constrói
a imagem a partir do clone, e o go2rtc publica uma câmera sintética - o ffmpeg
que já vem na imagem dele desenha uma carta de teste com relógio em H264 720p,
indistinguível de uma câmera de verdade para o dwnvr.

```sh
git clone https://github.com/mhagnumdw/dwnvr && cd dwnvr

# Precisam existir antes: se o Docker os criar, eles nascem de root e o
# container - que não roda como root - não consegue escrever dentro deles.
mkdir -p config storage

# Configuração de infraestrutura do dwnvr, como porta, quota de disco
# padrão por câmera, endereço do go2rtc etc
cp dwnvr.example.yaml  config/dwnvr.yaml

# As suas câmeras moram neste arquivo, que o git ignora - URLs RTSP com usuário
# e senha não têm como ir parar num commit.
cp go2rtc.example.yaml go2rtc.yaml

# Para um teste rápido isso não é necessário, mas para uma instalação
# definitiva é altamente recomendado
# Definir UID e GID para o seu usuário, evita problema com permissões
{ echo "DWNVR_UID=$(id -u)"; echo "DWNVR_GID=$(id -g)"; } > .env
# Setar para o seu timezone
echo "TZ=America/Fortaleza" >> .env

docker compose up
```

A primeira subida compila a interface e o binário dentro do Docker e leva
alguns minutos; as seguintes sobem em segundos.

Quando o log disser
`dwnvr no ar`, abra <http://localhost:8080>:

- vá na aba **Câmeras** clique na `cam_teste`, que já aparece em *Disponíveis no go2rtc*
- na aba **Ao vivo** funciona no mesmo instante
- em ~30s o primeiro segmento fecha e aparece na aba **Gravações**

Não há tela de login: enquanto `server.username` e `server.password` estiverem
vazios a autenticação fica desligada, o que é proposital para não travar o
primeiro uso - e o dwnvr avisa disso no log.

> O UID só importa se o seu não for 1000 - sem isso a tela de cadastro falha ao
gravar o `cameras.json`. O `TZ` decide a que dia local cada segmento pertence;
sem ele a virada de dia da timeline cai no horário errado.

### Colocar a sua câmera <!-- omit in toc -->

Edite o `go2rtc.yaml`: o bloco comentado traz alguns exemplos de configuração -
alta e baixa resolução, áudio, o formato geral da URL RTSP. Depois:

```sh
docker compose restart go2rtc
```

A câmera nova aparece sozinha na tela de **Câmeras**, pronta para cadastrar, e
a `cam_teste` sintética pode ser apagada do `go2rtc.yaml`.

### Conferir por fora da interface <!-- omit in toc -->

As gravações ficam em `./storage`, no host, com o seu usuário:

```sh
# o init, o índice NDJSON e os segmentos
find storage -type f

# Qualquer segmento abre sozinho, sem pré-processamento e sem o init ao lado
ffplay "$(find storage/cam_teste/2* -name '*.mp4' | head -1)"
```

### Derrubar tudo <!-- omit in toc -->

```sh
docker compose down
rm -rf config storage go2rtc.yaml .env
```

> Para a [instalação definitiva](#instalação-definitiva) a mudança é mínima:
> três caminhos de volume e um `--pull always`. Ver a seção logo abaixo.

## Instalação definitiva

É o mesmo compose do [subir o dwnvr](#subir-o-dwnvr). Muda só onde as coisas
moram no host.

### Os diretórios <!-- omit in toc -->

Supondo um disco em `/mnt/storage` - troque pelo seu:

```
No Host                                                            No Container
/mnt/storage/dwnvr/
├── config/          dwnvr.yaml, cameras.json, .session-secret  →  /etc/dwnvr
├── recordings/      as gravações                               →  /storage
└── go2rtc/
    └── go2rtc.yaml  as suas câmeras, com usuário e senha       →  /config/go2rtc.yaml
```

O `go2rtc.yaml` fica fora de `config/` de propósito: o `config/` inteiro é
montado dentro do container do dwnvr, e as URLs RTSP - com usuário e senha - não
têm por que ficar visíveis lá.

```sh
sudo mkdir -p /mnt/storage/dwnvr/{config,recordings,go2rtc}
sudo chown -R "$(id -u):$(id -g)" /mnt/storage/dwnvr

cp dwnvr.example.yaml  /mnt/storage/dwnvr/config/dwnvr.yaml
cp go2rtc.example.yaml /mnt/storage/dwnvr/go2rtc/go2rtc.yaml
```

Crie-os **antes** de subir: criados pelo Docker, eles nascem de root, e aí o
container - que não roda como root - não escreve dentro deles.

### Os três volumes <!-- omit in toc -->

```yaml
services:
  dwnvr:
    volumes:
      - /mnt/storage/dwnvr/config:/etc/dwnvr
      - /mnt/storage/dwnvr/recordings:/storage
  go2rtc:
    volumes:
      - /mnt/storage/dwnvr/go2rtc/go2rtc.yaml:/config/go2rtc.yaml
```

Só o lado esquerdo muda; o direito é o que o container enxerga por dentro e é
fixo.

**Edite o `docker-compose.yml` direto** - funciona e é o caminho mais simples.
Se preferir manter o clone limpo para o `git pull`, grave esse mesmo trecho num
`docker-compose.override.yml` ao lado: o Docker junta os dois sozinho, e o git
ignora o arquivo.

### O `.env` deixa de ser opcional <!-- omit in toc -->

`DWNVR_UID` e `DWNVR_GID` errados fazem a tela de cadastro falhar ao gravar o
`cameras.json`; `TZ` errado joga a virada de dia da timeline para o horário
errado.

### Ligar o login <!-- omit in toc -->

Preencha `server.username` e `server.password` no `dwnvr.yaml`: enquanto os dois
estiverem vazios a autenticação fica **desligada**, e quem abrir a interface
enxerga as gravações de todas as câmeras.

### As suas câmeras <!-- omit in toc -->

Apague a `cam_teste` do `go2rtc.yaml` - ela não serve para mais nada -,
publicando as suas no lugar.

### Subir <!-- omit in toc -->

```sh
# --pull always baixa a imagem pronta. Sem ele, o compose compila a partir do
# clone - o que num hardware modesto é a diferença entre segundos e um build
# que pode demorar muito e/ou não caber na memória.
docker compose up -d --pull always
```

### Atualizar <!-- omit in toc -->

```sh
git pull && docker compose up -d --pull always
```

Se o pull falhar, o comando para aí e o que está no ar continua gravando.

**Se o go2rtc já roda em outro compose ou direto no host:**

1. no `docker-compose.yml` remova o serviço do go2rtc e descomente o `extra_hosts`
2. no `dwnvr.yaml` troque a `go2rtc.url` para `http://host.docker.internal:1984`

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
│   └── dwnvr/              o binário: lê a config, sobe um recorder por câmera, serve HTTP
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
├── docker-compose.yml      dwnvr + go2rtc, o único arquivo para subir tudo
├── Dockerfile              imagem FROM scratch, multi-arch
├── dwnvr.example.yaml      configuração do dwnvr, campo a campo
├── go2rtc.example.yaml     configuração do go2rtc, com uma câmera sintética
└── Makefile                build, testes e deploy
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

### `cmd/` - convenção da comunidade <!-- omit in toc -->

Não é invenção deste projeto nem exigência do compilador: é o hábito adotado em
praticamente todo projeto Go de porte - Kubernetes, Docker, Prometheus, o
próprio go2rtc.

A regra é simples: **cada subdiretório de `cmd/` vira um binário**, e é o único
lugar onde mora `package main`. O nome do subdiretório é o nome do executável -
`cmd/dwnvr` produz `dwnvr`. Aqui só existe uma pasta porque só existe um
binário; um segundo executável seria uma pasta irmã, sem reorganizar mais nada.

O que a convenção compra: `main` fica magro - lê configuração, monta as peças e
sai da frente, com toda a lógica em pacotes testáveis sob `internal/`;
`go build ./cmd/dwnvr` fica inequívoco, sem caçar qual arquivo tem a função
`main`; e acrescentar um segundo binário não reorganiza nada.

### `internal/` - exigência do Go <!-- omit in toc -->

Aqui não é hábito, é regra que o próprio Go impõe. Um pacote sob `internal/` só
pode ser importado de dentro do próprio módulo. Outro projeto que tente
importar `github.com/mhagnumdw/dwnvr/internal/store` recebe:

```
use of internal package github.com/mhagnumdw/dwnvr/internal/store not allowed
```

É o que permite reorganizar tudo que está aqui dentro sem quebrar ninguém lá
fora: nada disto é API pública, e o Go garante isso em vez de pedir por favor.

### `internal/api/dist/` - o build da interface, versionado <!-- omit in toc -->

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

### `web/src/vendor/` - convenção, e do outro lado da cerca <!-- omit in toc -->

Guarda código de terceiros: o player de live do go2rtc (MIT), copiado sem
modificação. Ver [`web/src/vendor/README.md`](web/src/vendor/README.md).

Uma armadilha de leitura: em Go, um diretório `vendor/` **na raiz do módulo** é
especial - é onde `go mod vendor` despeja as dependências, e a partir daí o
build passa a usá-las em vez do cache de módulos. Este `vendor/` não é aquele:
está dentro de `web/`, é JavaScript, e para o Go não significa nada. O nome foi
emprestado pelo costume, não pela regra.

### `*_test.go` - exigência do Go <!-- omit in toc -->

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
| `make image-arm64` | imagem docker arm64, carregada no docker local |
| `make image-amd64` | imagem docker amd64, carregada no docker local |
| `make image` | imagem multi-arch (amd64 + arm64), como a que a CI publica |
| `make deploy` | recria o container no servidor remoto via ssh, com a imagem que a CI publicou |
| `make deploy-wip` | leva o código **não commitado** para o servidor, só para experimentar |

Como o `internal/api/dist` é versionado, **`go build ./cmd/dwnvr` funciona num
clone limpo sem Node instalado**. Isso é deliberado: o alvo é um dispositivo
onde ninguém quer instalar toolchain de frontend.

## Testes

```sh
make test        # testes de unidade
make check       # testes + gofmt + go vet
```

A CI roda isso e mais uma coisa: reconstrói a interface para conferir se o
`internal/api/dist` versionado ainda corresponde a `web/`. Fica fora do `make
check` porque exigiria Node em toda máquina que só quer compilar o Go.


Os testes vivem ao lado do código que exercitam, em `internal/*/*_test.go`, e
cobrem o que quebra em silêncio: a leitura de caixas fMP4, a reescrita do
`tfdt`, o corte em keyframe, a reconciliação de órfãos, a retenção e os
endpoints HTTP.

O workflow de CI está em `.github/workflows/ci.yml` e roda a cada push.

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

- [x] **Fase 0** - testes de viabilidade de gravação e playback ([resultados](docs/fase0-resultados.md))
- [x] **Fase 1** - recorder, índice e retenção ([resultados](docs/fase1-resultados.md))
- [x] **Fase 2** - API HTTP, autenticação, exportação ([resultados](docs/fase2-resultados.md))
- [x] **Fase 3** - SPA Svelte com as quatro telas ([resultados](docs/fase3-resultados.md))
- [x] **Fase 4** - Docker multi-arch e empacotamento ([resultados](docs/fase4-resultados.md))

> A Fase 0 foi feita com programas descartáveis, escritos só para responder duas
> perguntas antes de comprometer o projeto com elas: "dá mesmo para gravar o
> fMP4 do go2rtc sem decodificar, dentro do orçamento de CPU e RAM?" e "dá para
> tocar esses segmentos no navegador via MSE?". As duas respostas foram sim, e a
> medição que as sustenta está em
> [`docs/fase0-resultados.md`](docs/fase0-resultados.md). Os programas não
> viraram produto e já foram removidos do repositório.

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
