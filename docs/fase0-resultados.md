# Fase 0 - resultados dos testes de viabilidade

Medições feitas em 08/08/2026 contra o Orange Pi Zero 3 (Armbian 6.12.58
sunxi64, 4 cores Cortex-A53, 1471 MB RAM) com as 9 câmeras reais.

Antes de existir projeto, dois programas descartáveis responderam às duas
perguntas que decidiam o desenho: gravar o fMP4 do go2rtc sem decodificar cabia
no orçamento de CPU e RAM, e o navegador tocava os segmentos gravados via MSE.
As duas respostas foram sim; os programas não viraram produto e não estão mais
no repositório. O que ficou é a medição abaixo e o código de produção que ela
justificou.

## Consumo de recursos - o requisito central

5 minutos gravando as **9 câmeras simultaneamente**, segmentos de 60s:

| Processo | CPU | RSS |
|---|---|---|
| dwnvr (gravador) | **4,98% de 1 core** (1,2% dos 4) | **16,4 MB** |
| go2rtc | 16,9% de 1 core | 15,9 MB |
| **soma** | **~22% de 1 core (5,5% do sistema)** | **~32 MB** |

Heap Go: 5,1 MB em uso, 19,6 MB alocados no total em 5 min, 5 GCs. O buffer
reaproveitado entre caixas (`fmp4.Reader`) é o que mantém a alocação nesse nível:
sem ele seriam ~135 alocações/segundo só de fragmentos.

A meta do plano era < 100 MB e < 10% de core. Ficou em um terço disso.
Load average do Pi durante o teste: 0,12.

## Taxa de dados real por câmera (stream `onvif1`)

| Câmera | kbps | GB/dia |
|---|---|---|
| cam_frente | 928 | 9,6 |
| cam_portao | 928 | 9,6 |
| cam_jardim | 792 | 8,2 |
| cam_quintal | 683 | 7,0 |
| cam_lateral2 | 655 | 6,8 |
| cam_porta | 655 | 6,8 |
| cam_cozinha | 410 | 4,2 |
| cam_fundo | 328 | 3,4 |
| cam_lateral1 | 164 | 1,7 |
| **total** | **5523** | **55,6** |

Bem abaixo dos ~12,6 Mbps que eu havia estimado: o H265 dessas câmeras é mais
eficiente que o esperado em cena parada. **55,6 GB/dia** com as 9 em alta
resolução é o número que dimensiona a retenção.

## Formato entregue pelo go2rtc

- init segment (`ftyp`+`moov`) de **737 bytes**, com VPS/SPS/PPS no `hvcC`
- **um `moof`+`mdat` por frame**, keyframe marcado em `trun` via
  `sample_is_non_sync_sample` (constante `0x10000` em `pkg/iso/atoms.go`)
- `tfdt` **versão 1 (64 bits)** - sem risco de wraparound
- 4CC da sample entry: **`hev1`** (o `Content-Type` anuncia `hvc1`, mas a caixa
  é `hev1`; `hev1` funciona no Chrome, **Safari só aceita `hvc1`**)
- keyframe a cada ~3,9s (GOP 60 @ 15 fps), stream sempre começa em keyframe

### Duas correções que a medição obrigou

1. **`tfdt` precisa ser reescrito por segmento.** O go2rtc entrega tempo contínuo
   desde o início da conexão, então sem reescrita o 2º segmento começava em
   t=24s, o 3º em t=48s: os arquivos abriam, mas anunciavam duração crescente e
   um buraco no começo. Resolvido em `internal/fmp4/rebase.go`, que subtrai a
   base no lugar mantendo tamanho e versão da caixa.

2. **Segmento só pode abrir em keyframe.** Já era o desenho, e o teste confirmou:
   depois da correção, todo segmento tem o 1º frame com `key_frame=1` em `pts=0`.

## Dimensões erradas no container (limitação do go2rtc)

| Fonte | Resolução |
|---|---|
| `stsd/hev1` gravado pelo go2rtc | 2560x1440 |
| Decodificação real (ffmpeg→PNG e WebCodecs) | **1920x1080** |

A resolução verdadeira é 1080p, como já dizia o comentário no `go2rtc.yaml`;
é o `ffprobe` que relata errado, lendo o container.

> **Corrigido depois.** Aqui eu atribuí isso a um erro de leitura do SPS. A
> causa real é outra e tem consequências de projeto: o go2rtc grava um SPS
> **hardcoded** quando não consegue extraí-lo do `FmtpLine`, e os parameter sets
> verdadeiros vêm in-band. Isso torna o 4CC `hev1` obrigatório e cega a detecção
> de troca de codec por hash em H265. Ver
> [go2rtc-h265-parameter-sets.md](go2rtc-h265-parameter-sets.md).

## Playback no navegador

Chrome 151 / Fedora 43 / GPU Intel:

- `MediaSource.isTypeSupported` → **true** para `hev1.1.6.L153.B0` e `hvc1.1.6.L153.B0`
- `VideoDecoder.isConfigSupported` → **true** para os dois
- **decodificação real de um keyframe gravado via WebCodecs: OK**, devolveu um
  `VideoFrame` 1920x1080 BGRX

### MSE ponta a ponta - confirmado

Os 4 segmentos gravados costurados num único `<video>` via `SourceBuffer`:

| Medida | Resultado |
|---|---|
| Anexar 4 segmentos (79s, ~9 MB) | **109 ms** |
| `buffered` | **uma faixa única `[0, 79.06]`** - sem emenda entre segmentos |
| Frames decodificados | **1199, com 1 descartado** |
| `videoWidth` x `videoHeight` | **1920x1080** (corrigido pelo bitstream) |
| Seek para t=50s atravessando fronteira de segmento | OK |

Duas conclusões que sustentam o desenho da tela de gravações:

1. **`timestampOffset` é suficiente para costurar segmentos autônomos.** Cada
   segmento tem o próprio `ftyp+moov` e começa em t=0; posicioná-lo na linha do
   tempo é uma atribuição, não uma remuxagem. Anexar um segmento custa ~27 ms.
2. **O navegador ignora as dimensões erradas do container** e usa as do
   bitstream, o que confirma que aquele bug do go2rtc é cosmético.

**Risco #1 derrubado.** E, como bônus, WebCodecs também funciona neste hardware,
então o player MSE tem uma alternativa comprovada caso apareça algum caso de
borda.

## Pendências de ambiente

- `/mnt/storage` (disco USB, 220 GB) pertence ao root e o usuário não tem sudo
  sem senha - as medições foram feitas no cartão SD. Precisa de
  `sudo chown $USER /mnt/storage` (ou um subdiretório) antes da Fase 1.
- O disco está com **186 GB ocupados por gravações antigas de outro NVR**,
  restando 23 GB. A 55,6 GB/dia isso são ~10 horas de gravação.
