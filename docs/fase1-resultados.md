# Fase 1 — resultados

Recorder, índice e retenção validados no Orange Pi Zero 3 (`servidor.local`) com
as 9 câmeras reais gravando em `/mnt/storage/dwnvr` (disco USB), 08/08/2026.

Configuração do teste: segmentos de 30s e **cota de apenas 30 MB por câmera**,
deliberadamente pequena para que a retenção agisse em minutos em vez de dias.

## Consumo

| Processo | CPU | RSS |
|---|---|---|
| dwnvr (9 câmeras + retenção ativa) | **4,01% de 1 core** (1,0% dos 4) | **17,1 MB** |
| go2rtc | 9,33% de 1 core | 22,7 MB |

Load average do Pi: 0,30. Uso total de disco travado em 274 MB pelas cotas.

O binário ARM64 estático tem 6,0 MB.

## O que foi verificado

| Verificação | Resultado |
|---|---|
| 9 câmeras conectam e gravam | 6× `hev1`, 3× `avc1`, zero erros |
| Segmento abre em keyframe | `key_frame=1` em `pts=0` em todos |
| Sem reencode | `hevc/hev1` e `h264/avc1` preservados da câmera |
| Duração alinhada ao GOP | 31,6s para alvo de 30s (GOP ~4s) |
| Índice | 77 bytes/segmento, ~111 KB/dia/câmera |
| Dedup do init por hash | **2 arquivos distintos** para as 9 câmeras |
| Retenção por cota | `usado_mb=30 cota_mb=30 liberado_mb=3` |
| Encerramento gracioso (SIGTERM) | 15 arquivos = 15 linhas de índice |
| Recuperação de `kill -9` | órfão reincorporado com duração correta |
| Trava de disco separado | barrou gravação no rootfs |

### Recuperação de queda, em detalhe

Depois do `kill -9`: 17 arquivos contra 16 linhas de índice — um órfão de
**786.432 bytes**, exatamente 3× o buffer de escrita de 256 KB. A cauda ainda
bufferizada se perdeu, como esperado.

No reinício, a reconciliação sondou o arquivo e o reincorporou:

```json
{"t":1786221122736,"d":7912,"sz":786432,"g":"4edbc50d8e70","io":737,"f0":185553}
```

Duração (7,9s) e geração recuperadas a partir do próprio arquivo. Zero erros.

A invariante em regime é **arquivos = linhas de índice + 1**: o segmento em
aberto só entra no índice quando fecha. Após um SIGTERM os números se igualam.

## Dois erros encontrados por rodar no hardware real

Nenhum dos dois apareceria em teste local.

**1. A trava de montagem estava semanticamente errada.** Eu verificava se
`storage.root` era um ponto de montagem, mas a configuração natural é
`/mnt/storage/dwnvr` — um subdiretório do mount `/mnt/storage`. A checagem
reprovava justamente o caso mais comum. Passou a comparar o sistema de arquivos
do destino com o da raiz, que é o que a trava realmente quer dizer: "não grave
no disco do sistema". Renomeada para `requireSeparateDisk`.

**2. Permissões inconsistentes.** `os.CreateTemp` cria com `0600`, então o init
saía `600` enquanto os segmentos saíam `644` — e, pior, o índice **nascia**
`0644` no append e **caía** para `0600` na primeira evicção, uma mudança
silenciosa no meio da vida do arquivo. Isso morderia em Docker com UID
diferente. Corrigido com `Chmod` explícito antes do rename, e travado por teste
de regressão.

## Cobertura de testes

`internal/fmp4` 77,1% · `internal/store` 76,7%

Os testes constroem fMP4 sintético em vez de depender de arquivos gravados, o
que dá controle preciso sobre casos difíceis: moof de trilha de áudio (que o
go2rtc marca com o mesmo `sampleDependsOn2` dos keyframes e que portanto nunca
pode ser confundido com ponto de corte), underflow no rebase, linha de índice
truncada e segmento que atravessa a meia-noite.

A qualidade dos testes foi conferida por mutação: desativar o teste de
`sample_is_non_sync_sample` faz `TestParseMoofDetectaKeyframe` falhar.

## Áudio FLAC

Exercitado ponta a ponta na `cam_jardim` — ver [audio-flac.md](audio-flac.md).
Resumo: **+0,65% de um core, zero processos ffmpeg, mas +34% de armazenamento**,
porque o FLAC é sem perdas e o A-law precisa ser expandido para 16 bits antes de
ser comprimido.

O recorder detectou sozinho que o init mudou (duas trilhas em vez de uma) e
abriu uma nova geração, `e38cb0530c62` contra `4edbc50d8e70`, com o init
crescendo de 737 para 1192 bytes — exatamente o comportamento que o hash de
conteúdo deveria produzir, sem nenhum código específico para isso.

## Reconexão, verificada por acidente

Reiniciar o go2rtc para o teste de áudio derrubou as 9 conexões ao mesmo tempo.
O dwnvr registrou `connection refused` em todas, esperou o backoff de 2s e
reconectou as 9 sem intervenção e sem um único ERROR no log.

## Pendente

- Teste de longa duração (24h+) para o risco #2, estabilidade do `stream.mp4`
  em conexão contínua. O processo ficou rodando no Pi para isso.
- Confirmação de FLAC-em-MP4 no Safari.
