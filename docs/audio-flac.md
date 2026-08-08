# Áudio: medição do modo FLAC

Medido no Orange Pi Zero 3 em 08/08/2026, com a `cam_jardim` (H265, Yoosee).
Para o teste, o `#media=video` foi removido da fonte no `go2rtc.yaml` — sem
isso o áudio é descartado na origem e nada chega ao dwnvr.

## Resultado

| | Valor |
|---|---|
| Codec de origem | `PCMA/16000` (G.711 A-law, mono) |
| Codec gravado | `fLaC` 16 kHz mono, caixa `dfLa` com STREAMINFO de 34 bytes |
| **Custo de CPU** | **+0,65% de um core** por câmera |
| **Processos ffmpeg** | **zero** |
| **Taxa do áudio** | **~260 kbps** |
| Taxa do vídeo, mesma captura | ~770 kbps |
| **Impacto no armazenamento** | **+34%** |

A medição de CPU foi feita comparando o tempo acumulado em `/proc/<pid>/stat`
do go2rtc em duas janelas de 40s, com e sem um consumidor FLAC: 13,22% contra
13,87% de um core. Amostras instantâneas de `docker stats` sugeriam 3-6%, mas
eram ruído.

A ausência de ffmpeg foi confirmada com `docker top go2rtc`: só existe o
processo do próprio go2rtc. A conversão A-law→PCM→FLAC acontece em Go puro.

## O contra-senso do tamanho

FLAC é compressão **sem perdas**, e o caminho é
`A-law (8 bits) → PCM linear (16 bits) → FLAC`. Como o A-law já é uma compressão
com perdas de 8 bits por amostra, expandi-lo para 16 bits dobra os dados antes
de o FLAC ter chance de comprimir — e o resultado ficou em ~260 kbps, ou seja,
**mais que os 128 kbps do A-law original**.

Ou seja, gravar em FLAC ocupa o dobro do que ocuparia guardar o A-law cru. Só
que o A-law cru não toca em navegador nenhum, e é justamente esse o problema que
o FLAC resolve de graça em CPU.

## Como escolher

A decisão é entre CPU e disco, e depende de qual dos dois é escasso:

- **`flac`** quando sobra disco e falta CPU. Não dispara ffmpeg, sobe junto com
  a gravação e não depende de nenhuma configuração extra no go2rtc.
- **`aac`** quando sobra CPU e falta disco. Custa ~10% de um core por câmera
  (medição do usuário com Opus, que usa o mesmo caminho de ffmpeg), mas cabe em
  ~64 kbps e toca em qualquer lugar, inclusive nos MP4 exportados.
- **`none`** para a maioria das câmeras. Áudio raramente é útil em todas.

Por isso o modo é **por câmera**, e não global: o padrão sensato é `none` em
tudo e o áudio ligado só onde ele importa — porta e portão, tipicamente.

## Compatibilidade no navegador

Chrome 151 / Fedora 43:

- `MediaSource.isTypeSupported` com `hev1.1.6.L153.B0,flac` → **true**
- `AudioDecoder.isConfigSupported` com `flac` + o STREAMINFO do `dfLa` → **true**
- `ffmpeg -i segmento -f null -` decodifica as duas trilhas sem erro

Safari continua sendo a incógnita: FLAC dentro de MP4 é pouco usual. Quem
precisar de compatibilidade ampla nos arquivos exportados deve preferir `aac`.

## Efeito colateral de ligar o áudio na origem

Remover o `#media=video` faz o go2rtc puxar a trilha de áudio da câmera 24/7,
mesmo que ninguém peça áudio: são ~129 kbps a mais no enlace com a câmera, por
câmera. Nas instalações em 2,4 GHz — como a que motivou este projeto — isso
disputa airtime com as próprias câmeras e não é desprezível quando multiplicado
por várias.
