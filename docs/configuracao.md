# Configuração

O que o dwnvr lê, o que ele escreve, e o que cada política de gravação custa em
CPU e em disco.

- [Os dois arquivos](#os-dois-arquivos)
- [Política por câmera](#política-por-câmera)
- [Retenção](#retenção)
- [Áudio](#áudio)

## Os dois arquivos

- **`dwnvr.yaml`** - infraestrutura, editado à mão, **nunca reescrito** pela
  aplicação. Cada campo está comentado em
  [`dwnvr.example.yaml`](../dwnvr.example.yaml).
- **`cameras.json`** - a lista de câmeras, gravada pela tela de cadastro.

Estão separados porque reescrever um YAML apaga os comentários de quem o
escreveu, e a tela de cadastro precisa gravar câmeras a cada clique. A
separação também garante que um erro na tela de cadastro não consiga corromper
a sua infraestrutura: o dwnvr nunca escreve no `dwnvr.yaml`.

Os dois vivem no mesmo diretório - `/etc/dwnvr` na imagem Docker -, junto com o
`.session-secret`, que assina os cookies de sessão.

Há ainda um terceiro arquivo que o dwnvr **só lê pela API, nunca abre**: o
`go2rtc.yaml`. Ele é do go2rtc, e configurá-lo é tarefa de quem instala. Veja
[`go2rtc.example.yaml`](../go2rtc.example.yaml).

## Política por câmera

Tudo que é política de gravação é **por câmera**, não global:

| Campo | O que decide |
|---|---|
| stream do go2rtc | qual fonte gravar - a de alta ou a de baixa resolução |
| `audio` | `none`, `flac` ou `aac` - ver abaixo |
| `quotaMB` | quanto disco aquela câmera pode ocupar |
| `segmentSeconds` | duração alvo de cada segmento |
| `maxDays` | idade máxima, opcional |
| `stallSeconds` | tolerância antes de considerar o stream morto |

Ser por câmera não é preciosismo: numa instalação real as câmeras não são
iguais. A do portão merece alta resolução e cota grande; a do corredor não. E o
`stallSeconds` certo depende do enlace - o tipo de conexão - de cada câmera:
uma câmera em wi-fi ruim precisa de mais tolerância que uma no cabo.

Os valores de `defaults` no `dwnvr.yaml` valem para qualquer câmera que não
defina o campo.

## Retenção

Três limites, nesta ordem:

1. **cota em MB por câmera** - o principal, ring buffer apagando o mais antigo
2. **idade máxima em dias** - opcional, para quem pensa em dias e não em GB
3. **disco livre mínimo, global** - rede de segurança que ignora as cotas

O terceiro existe porque a soma das cotas erra fácil: cada câmera tem uma taxa
diferente, e encher o disco é pior que perder gravação antiga.

A cota é aplicada a cada minuto, então o pico real é `cota + taxa × 60s` - com
uma câmera de 900 kbps isso são ~7 MB de folga, desprezível contra uma cota real.

## Áudio

O modo de áudio é escolhido por câmera e vira um filtro de codec na URL:

| Modo | CPU | Disco | Mexe no go2rtc? |
|---|---|---|---|
| `none` | zero | zero | não |
| `flac` | **+0,65% de 1 core** | **+260 kbps** (~2,8 GB/dia) | não |
| `aac` | ~10% de 1 core | ~64 kbps (~0,7 GB/dia) | sim, exige `ffmpeg:cam#audio=aac` |

Medido no Orange Pi Zero 3 com câmeras Yoosee (pcm_alaw 16 kHz mono). **A
escolha é entre CPU e disco**: o FLAC é praticamente de graça em processamento
e não dispara nenhum processo ffmpeg - a conversão acontece em Go puro dentro
do go2rtc -, mas por ser sem perdas ele fica em ~260 kbps, o que numa câmera de
770 kbps de vídeo significa **+34% de armazenamento**. O AAC inverte a conta.

Requisito comum aos dois: a fonte no `go2rtc.yaml` não pode ter `#media=video`,
que descarta o áudio já na origem. Nenhuma configuração do dwnvr traz de volta
um áudio descartado lá.

A medição completa do FLAC está em [audio-flac.md](audio-flac.md).
