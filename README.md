# dwnvr

NVR de gravação contínua para hardware modesto. Feito para rodar num Orange Pi
Zero 3 (4 cores Cortex-A53, 1,5 GB RAM) gravando 9 câmeras 24/7.

**Sem transcodificação de vídeo. Sem banco de dados. Sem detecção de movimento.**

Medido no Pi com as 9 câmeras gravando simultaneamente:

| | CPU | RAM |
|---|---|---|
| dwnvr | **4% de 1 core** (1% dos 4) | **17 MB** |
| go2rtc | 9% de 1 core | 23 MB |

## Como funciona

```
9 câmeras ──RTSP──> go2rtc ──HTTP fMP4──> dwnvr ──> disco
                      │                     │
                      └──WebRTC/MSE─────────┴──> navegador
                         (live, via proxy)      (gravações)
```

O dwnvr consome o fMP4 que o **go2rtc já produz** (`/api/stream.mp4`) e corta em
segmentos alinhados a keyframe. Ele nunca decodifica, nunca remuxa e nunca toca
nos bytes de mídia — só lê cabeçalhos de caixa para saber onde cortar.

**O go2rtc fica fora do escopo.** Configurá-lo é responsabilidade de quem
instala; o dwnvr apenas descobre os streams existentes via `/api/streams` e
recomenda configuração. Isso é deliberado: foi a dificuldade de customizar o
go2rtc embarcado que motivou este projeto.

### Por que consumir o fMP4 do go2rtc

O go2rtc já resolve a parte difícil — RTSP, depacketização RTP, extração de
VPS/SPS/PPS e muxagem MP4. Aproveitar isso reduz o recorder a um leitor de
caixas de ~150 linhas, sem ffmpeg e sem cgo. Concretamente, o go2rtc entrega:

- init segment (`ftyp`+`moov`) de ~700 bytes com os parameter sets embutidos
- **um `moof`+`mdat` por frame**, com keyframe marcado no `trun`
- `tfdt` em 64 bits, sem risco de wraparound

Cortar segmento vira, literalmente, ler um bit de flag.

### Decisões que o formato obriga

**Cada segmento é um arquivo autônomo** — carrega o próprio init (~700 B/minuto,
desprezível) e abre no VLC, no ffprobe e num `<video>` sem pré-processamento.

**O `tfdt` é reescrito por segmento.** O go2rtc entrega tempo contínuo desde o
início da conexão; sem reescrever, o segundo segmento começaria em t=24s, o
terceiro em t=48s, e assim por diante. A reescrita mantém tamanho e versão da
caixa, então nenhum tamanho acima precisa ser recalculado.

**Segmento só abre em keyframe**, senão não tocaria sozinho. Como o corte espera
o próximo keyframe, a duração real é o alvo arredondado para cima pelo GOP: com
GOP de 4s, um alvo de 30s vira ~31,6s.

## Armazenamento

Sem banco de dados. O índice é um NDJSON por câmera por dia, append-only:

```
/mnt/storage/dwnvr/
  cam_portao/
    init/4edbc50d8e70.mp4        init identificado por hash do conteúdo
    2026-08-08/1786220564113.mp4 segmento; o nome é o início em epoch ms
    index/2026-08-08.ndjson
```

```json
{"t":1786220564113,"d":31596,"sz":2434983,"g":"4edbc50d8e70","io":737,"f0":160567}
```

`t` início · `d` duração ms · `sz` bytes · `g` geração do init · `io` onde
terminam ftyp+moov · `f0` tamanho do 1º fragmento

São 77 bytes por segmento, ~111 KB por dia por câmera. O caminho do arquivo não
é guardado porque é derivável de `t` — guardar os dois abriria espaço para
divergirem.

Três detalhes que o formato compra barato:

- **`io`** permite entregar via MSE pulando o init e servindo-o uma vez só
- **`f0`** permite servir init + primeiro fragmento como um **MP4 de um frame**:
  é a thumbnail da timeline, decodificada pelo navegador, **sem o Pi decodificar
  nada**
- **hash como geração** detecta troca de codec sozinho e deduplica: as 6 câmeras
  H265 compartilham o mesmo arquivo de init

### Recuperação

O índice é escrito **depois** que o segmento é fechado. Uma queda entre as duas
coisas deixa um arquivo órfão, que a reconciliação do boot reincorpora sondando
o arquivo. A ordem inversa deixaria o índice apontando para algo que nunca
existiu.

Só o dia mais recente é reconciliado: é onde mora o estrago de uma queda, e
conferir só ele evita varrer centenas de milhares de arquivos a cada boot.

Verificado no Pi: `kill -9` deixou 1 órfão de 786.432 bytes (a cauda
bufferizada se perdeu); o reinício o reincorporou com a duração correta.

## Retenção

Três limites, nesta ordem:

1. **cota em MB por câmera** — o principal, ring buffer apagando o mais antigo
2. **idade máxima em dias** — opcional, para quem pensa em dias e não em GB
3. **disco livre mínimo, global** — rede de segurança que ignora as cotas

O terceiro existe porque a soma das cotas erra fácil: cada câmera tem uma taxa
diferente, e encher o disco é pior que perder gravação antiga.

A cota é aplicada a cada minuto, então o pico real é `cota + taxa × 60s` — com
uma câmera de 900 kbps isso são ~7 MB de folga, desprezível contra uma cota real.

## Configuração

Dois arquivos, de propósito:

- **`dwnvr.yaml`** — infraestrutura, editado à mão, **nunca reescrito** pela
  aplicação. Veja [`dwnvr.example.yaml`](dwnvr.example.yaml).
- **`cameras.json`** — a lista de câmeras, gravada pela tela de cadastro.

Estão separados porque reescrever um YAML apaga os comentários de quem o
escreveu, e a tela de cadastro precisa gravar câmeras a cada clique.

Tudo que é política de gravação é **por câmera**: qual stream do go2rtc usar
(alta ou baixa resolução), áudio, cota, tamanho do segmento.

### Áudio

O modo de áudio é escolhido por câmera e vira um filtro de codec na URL:

| Modo | CPU | Disco | Mexe no go2rtc? |
|---|---|---|---|
| `none` | zero | zero | não |
| `flac` | **+0,65% de 1 core** | **+260 kbps** (~2,8 GB/dia) | não |
| `aac` | ~10% de 1 core | ~64 kbps (~0,7 GB/dia) | sim, exige `ffmpeg:cam#audio=aac` |

Medido no Pi com câmeras Yoosee (pcm_alaw 16 kHz mono). **A escolha é entre
CPU e disco**: o FLAC é praticamente de graça em processamento e não dispara
nenhum processo ffmpeg — a conversão acontece em Go puro dentro do go2rtc —, mas
por ser sem perdas ele fica em ~260 kbps, o que numa câmera de 770 kbps de vídeo
significa **+34% de armazenamento**. O AAC inverte a conta.

Requisito comum aos dois: a fonte no `go2rtc.yaml` não pode ter `#media=video`,
que descarta o áudio já na origem.

## API

```
POST /api/login  /api/logout       sessão por cookie assinado (HMAC, sem estado no servidor)
GET  /api/session                  público: diz se precisa de login
GET  /api/cameras                  câmeras cadastradas + streams disponíveis no go2rtc
GET  /api/health                   bitrate medido, dias estimados, estado do disco
GET  /api/rec/days                 dias com gravação
GET  /api/rec/timeline             faixas contíguas (desenhar) + segmentos (tocar)
GET  /api/rec/init                 init segment, immutable
GET  /api/rec/seg                  fragmentos do segmento, sem o init, immutable
GET  /api/rec/thumb                MP4 de 1 frame — o Pi não decodifica nada
GET  /api/rec/playlist.m3u8        HLS VOD, para VLC/ffplay/Safari
GET  /api/rec/export               MP4 único emendado, sem transcodificação
GET  /api/live/*                   proxy do go2rtc, com a credencial ficando no servidor
```

A interface é servida **sem** autenticação — é só o app shell, sem dado nenhum
de câmera — enquanto todo endpoint de dados exige sessão. Sem isso, o navegador
não conseguiria carregar a própria tela de login.

## Estado atual

- [x] **Fase 0** — spikes validando gravação e playback ([resultados](docs/fase0-resultados.md))
- [x] **Fase 1** — recorder, índice e retenção ([resultados](docs/fase1-resultados.md))
- [x] **Fase 2** — API HTTP, autenticação, exportação ([resultados](docs/fase2-resultados.md))
- [x] **Fase 3** — SPA Svelte com as quatro telas ([resultados](docs/fase3-resultados.md))
- [ ] **Fase 4** — Docker multi-arch e empacotamento

## Interface

Quatro telas, em Svelte 5 + Vite, embutidas no binário: **ao vivo**,
**gravações**, **câmeras** e **diagnóstico**. O aplicativo inteiro pesa
**32,6 kB gzipped**, incluindo o player de live do go2rtc.

Mobile-first: navegação inferior no celular e superior no desktop, grade ao vivo
travada em uma coluna abaixo de 640 px, e timeline com Pointer Events — arrastar
navega, pinçar dá zoom.

Escrever o player MSE em vez de usar hls.js é o que segura o tamanho: só a
biblioteca custaria ~110 kB gzip. Em troca, ganhamos controle exato sobre a
janela de buffer e sobre os buracos de gravação, que o índice já conhece.

Ver [`web/README.md`](web/README.md) para desenvolver a interface.

## Desenvolvimento

```sh
go test ./...
go build ./cmd/dwnvr

# Alvo real: Orange Pi Zero 3 (ARM64), binário estático de ~6 MB
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o dwnvr ./cmd/dwnvr
```

`cmd/spike` e `cmd/spike-serve` são os experimentos da Fase 0, mantidos porque
documentam como as premissas foram verificadas.

## Limitação conhecida: parameter sets H265

Nas câmeras Yoosee o go2rtc não consegue extrair VPS/SPS/PPS do `FmtpLine` e
grava no `hvcC` um **SPS hardcoded no próprio código**, que descreve 2560x1440.
Os parameter sets verdadeiros chegam in-band, dentro dos samples — por isso a
decodificação sai certa mesmo com o container mentindo a resolução.

Duas consequências que valem lembrar:

- **O 4CC `hev1` é obrigatório**, não cosmético. Converter para `hvc1` (que o
  Safari prefere) quebraria a reprodução, porque `hvc1` afirma que os parameter
  sets estão só na sample entry — e ali só há dummies.
- **O hash do init não detecta troca de codec em H265**, já que o init é sempre
  o mesmo dummy. Em H264 funciona normalmente. A reprodução não é afetada.

Detalhes e evidências em [docs/go2rtc-h265-parameter-sets.md](docs/go2rtc-h265-parameter-sets.md).
