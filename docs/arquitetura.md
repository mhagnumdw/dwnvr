# Arquitetura: o formato em disco

Este documento explica **por que o dwnvr grava do jeito que grava**. Nada aqui
é necessário para instalar ou usar o dwnvr - é a justificativa das decisões de
formato, para quem for mexer no código ou quiser entender a economia de
recursos que o projeto persegue.

O resumo em uma frase: o dwnvr consome o fMP4 que o go2rtc já produz e corta em
segmentos alinhados a keyframe, sem nunca decodificar, remuxar ou tocar nos
bytes de mídia.

- [Por que consumir o fMP4 do go2rtc](#por-que-consumir-o-fmp4-do-go2rtc)
- [Decisões que o formato obriga](#decisões-que-o-formato-obriga)
- [Armazenamento](#armazenamento)
- [Geração: o init identificado por hash](#geração-o-init-identificado-por-hash)
- [Limitação conhecida: parameter sets H265](#limitação-conhecida-parameter-sets-h265)

## Por que consumir o fMP4 do go2rtc

O go2rtc já resolve a parte difícil - RTSP, depacketização RTP, extração de
VPS/SPS/PPS e muxagem MP4. Aproveitar isso reduz o recorder a um leitor de
caixas de ~150 linhas, sem ffmpeg e sem cgo. Concretamente, o go2rtc entrega:

- init segment (`ftyp`+`moov`) de ~700 bytes com os parameter sets embutidos
- **um `moof`+`mdat` por frame**, com keyframe marcado no `trun`
- `tfdt` em 64 bits, sem risco de wraparound

Cortar segmento vira, literalmente, ler um bit de flag.

> **cgo** é o mecanismo que permite a um programa Go chamar bibliotecas
> escritas em C. Evitá-lo é o que mantém o binário estático de ~3 MB, sem
> depender de nenhuma biblioteca do sistema, e o que faz a compilação cruzada
> para o ARM do Orange Pi ser um `GOARCH=arm64 go build` e nada mais.

> Um MP4 é feito de **caixas** (*boxes*), cada uma com um tamanho e um nome de
> 4 letras. As que importam aqui:
>
> - **init segment** (`ftyp`+`moov`) - o cabeçalho que descreve as trilhas
>   (codec, resolução, timescale). Não tem vídeo nenhum; sem ele o player não
>   sabe decodificar o resto.
> - **`moof`** (*movie fragment*) - o cabeçalho de um pedaço de vídeo: diz
>   quando e como tocar os frames que vêm logo atrás.
> - **`mdat`** (*media data*) - os bytes comprimidos do vídeo em si. É a única
>   caixa que o dwnvr copia sem sequer olhar.
> - **`trun`** (*track run*) - fica dentro do `moof` e lista os frames: tamanho,
>   duração e um bit dizendo se aquele frame é keyframe.
> - **`tfdt`** (*track fragment decode time*) - também dentro do `moof`, é o
>   relógio: em que instante da linha do tempo esse fragmento começa.
>
> E um termo que não é caixa, mas aparece acima: **keyframe** é o frame que se
> decodifica sozinho, sem depender de nenhum outro. Os demais guardam só a
> diferença em relação ao anterior, então um arquivo que comece neles não toca.
> É por isso que o corte de segmento só pode acontecer num keyframe - e o
> `trun` diz, num bit, quais frames são.

A leitura das caixas está em [`internal/fmp4/`](../internal/fmp4/).

## Decisões que o formato obriga

**Cada segmento é um arquivo autônomo** - carrega o próprio init (~700 B/minuto,
desprezível) e abre no VLC, no ffprobe e em uma tag html `<video>` sem pré-processamento.

**O `tfdt` é reescrito por segmento.** O go2rtc entrega tempo contínuo desde o
início da conexão; sem reescrever, o segundo segmento começaria em t=24s, o
terceiro em t=48s, e assim por diante. A reescrita mantém tamanho e versão da
caixa, então nenhum tamanho acima precisa ser recalculado.

**Segmento só abre em keyframe**, senão não tocaria sozinho. Como o corte espera
o próximo keyframe, a duração real é o alvo arredondado para cima pelo GOP: com
GOP de 4s, um alvo de 30s vira ~31,6s.

> **GOP** (*Group of Pictures*) é o intervalo entre dois keyframes. Quem decide
> esse intervalo é a câmera - as Yoosee usam ~4s -, e ele é o menor "pedaço" em
> que dá para cortar vídeo sem recomprimir: como só keyframe pode abrir um
> arquivo, o corte sempre espera o próximo.

## Armazenamento

Sem banco de dados. O índice é um NDJSON por câmera por dia, append-only:

```
/mnt/storage/dwnvr/
  cam_portao/
    init/4edbc50d8e70.mp4        # init segment (ftyp+moov), identificado por hash do conteúdo
    2026-08-08/1786220564113.mp4 # segmento; o nome é o início em epoch ms
    index/2026-08-08.ndjson
```

> **NDJSON** (*Newline-Delimited JSON*) é um arquivo texto com **um objeto JSON
> completo por linha**, sem vírgula e sem colchete envolvendo tudo. É o formato
> que torna o índice append-only: registrar um segmento novo é acrescentar uma
> linha ao fim do arquivo, e ler um dia é percorrer linha a linha, sem carregar
> nada inteiro na memória. Cada linha é assim:

```json
{"t":1786220564113,"d":31596,"sz":2434983,"g":"4edbc50d8e70","io":737,"f0":160567}
```

- `t` início
- `d` duração ms
- `sz` bytes
- `g` geração do init
- `io` onde terminam ftyp+moov
- `f0` tamanho do 1º fragmento

São 77 bytes por segmento, ~111 KB por dia por câmera. O caminho do arquivo não
é guardado porque é derivável de `t` - guardar os dois abriria espaço para
divergirem.

Três detalhes que o formato compra barato:

- **`io`** permite entregar via MSE pulando o init e servindo-o uma vez só
- **`f0`** permite servir init + primeiro fragmento como um **MP4 de um frame**:
  é a thumbnail da timeline, decodificada pelo navegador, **sem o servidor
  decodificar nada**
- **`g` (geração)** identifica o init segment pelo **hash do seu conteúdo**, e
  não por um contador - ver abaixo

O layout em disco e o índice vivem em [`internal/store/`](../internal/store/).

## Geração: o init identificado por hash

O init segment (`ftyp`+`moov`) descreve as trilhas, e todo segmento precisa do
init certo para tocar. O campo `g` do índice é o SHA-256 desse init, truncado.

Usar o conteúdo como identidade, em vez de um contador, compra três coisas de
graça:

- **deduplicação**: 9 câmeras produzem apenas 4 arquivos de init distintos, porque
  todas as H265 sem áudio geram exatamente os mesmos bytes
- **detecção de mudança sem estado**: ligar áudio numa câmera acrescenta uma
  trilha ao `moov`, o hash muda e os segmentos novos passam a apontar para outro
  init - sem quebrar a reprodução dos antigos, e sem uma linha de código
  dedicada a "detectar mudança"
- **recuperação**: um contador exigiria persistir em que número se está; com
  hash, basta ler um segmento órfão para saber a que init ele pertence

Exemplo real, de uma instalação com 9 câmeras:

```
cam_cozinha   4edbc50d8e70   737 B   H265, só vídeo
cam_cozinha   e38cb0530c62  1192 B   H265 + FLAC (depois de ligar o áudio)
cam_jardim    e38cb0530c62  1192 B   idêntico ao acima → mesmo arquivo
cam_porta     ead03cc7ac5a   660 B   H264 onvif1
cam_lateral1  27dcb8115adf   660 B   H264 onvif2 (resolução diferente)
```

## Limitação conhecida: parameter sets H265

Nas câmeras Yoosee o go2rtc não consegue extrair VPS/SPS/PPS do `FmtpLine` e
grava no `hvcC` um **SPS hardcoded no próprio código**, que descreve 2560x1440.
Os parameter sets verdadeiros chegam in-band, dentro dos samples - por isso a
decodificação sai certa mesmo com o container mentindo a resolução.

Três consequências que valem lembrar:

- **O 4CC `hev1` é obrigatório**, não cosmético. Converter para `hvc1` (que o
  Safari prefere) quebraria a reprodução, porque `hvc1` afirma que os parameter
  sets estão só na sample entry - e ali só há dummies.
- **O hash do init não detecta troca de codec em H265**, já que o init é sempre
  o mesmo dummy. Em H264 funciona normalmente. A reprodução não é afetada.
- **Trocar apenas a resolução não muda o hash**, pelo mesmo motivo. Também não
  afeta a reprodução.

Detalhes e evidências em
[go2rtc-h265-parameter-sets.md](go2rtc-h265-parameter-sets.md).
