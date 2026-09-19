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
| `detect` | marcar movimento na timeline - ver abaixo |
| `detectMecanismo` | `kleinberg-p` ou `periodico` |
| `detectSensibilidade` | de 1 a 5, e é o custo em CPU |

Ser por câmera não é preciosismo: numa instalação real as câmeras não são
iguais. A do portão merece alta resolução e cota grande; a do corredor não. E o
`stallSeconds` certo depende do enlace - o tipo de conexão - de cada câmera:
uma câmera em wi-fi ruim precisa de mais tolerância que uma no cabo.

Os valores de `defaults` no `dwnvr.yaml` valem para qualquer câmera que não
defina o campo.

## Marcação de movimento

Com `detect` ligado, a timeline ganha uma **faixa de calor** por baixo da barra
azul: quanto mais forte o âmbar, mais coisas aconteceram naquele instante.

O sinal é o **tamanho de cada quadro**, que o gravador já tem na mão - nenhum
pixel é decodificado, e nada é gravado além de uma linha por marca em
`eventos/{dia}.ndjson`. A faixa diz QUANDO houve movimento; dizer o que era é
do detector de objetos, opcional - ver abaixo.

**A sensibilidade é o custo, e ele é por câmera.** Com seis câmeras no nível 4
são 300 marcas por hora, não 50:

| nível | marcas/hora por câmera | pega |
|---|---|---|
| 1 - muito baixa | 6 | 2,8% |
| 2 - baixa | 12 | 9,2% |
| 3 - média | 25 | 22,3% |
| **4 - alta** | **50** | **41,5%** |
| 5 - muito alta | 100 | 57,4% |

"Pega" é a fatia das chegadas de pessoa, veículo ou animal em que o gatilho
dispara a tempo. As duas colunas são medidas, não estimadas: centenas de horas
de gravação real, contra chegadas marcadas à mão.

**O mecanismo** decide como disparar. O `kleinberg-p` compara cada quadro com o
normal da própria câmera - é por isso que o mesmo nível significa a mesma coisa
numa câmera silenciosa e numa cheia de ruído de infravermelho. O `periodico`
dispara em intervalo fixo, ignorando a imagem: mesmo custo, e pega de 2 a 3
vezes menos. Ele existe por ser a régua a bater, e para quem preferir um
comportamento sem surpresa numa câmera específica.

Mudar qualquer um dos três **não reabre a conexão**: o gravador troca o gatilho
no quadro seguinte, sem abrir buraco na gravação.

Como o destaque, o onset e os níveis funcionam por dentro está em
[`deteccao.md`](deteccao.md).

## Detector de objetos

A marca de movimento diz QUANDO. O detector diz O QUÊ: a cada marca, o dwnvr
separa o pedaço de vídeo daquele instante e pergunta ao container
`dwnvr-detect` o que havia nele. Se era pessoa, veículo ou animal, a marca de
objeto vai para o mesmo `eventos/{dia}.ndjson`, e some junto com o vídeo na
retenção.

Ele é **opcional** e mora num container à parte, porque decodificar vídeo e
rodar um modelo exigem código nativo e o dwnvr é Go puro. Liga-se no
`dwnvr.yaml`:

```yaml
detector:
  url: http://dwnvr-detect:8480
```

Sem a `url`, as câmeras com `detect` marcam só movimento, e o dwnvr não guarda
um byte de vídeo a mais por isso.

**O que ele custa.** No Orange Pi Zero 3, cada olhada leva ~6,5 s de um núcleo
e o container ocupa ~230 MB. No nível 4 isso é ~9% de um núcleo por câmera,
contínuo. As olhadas passam por uma **fila com dois lugares por câmera**: se
uma câmera dispara de novo com os dois ocupados, a marca nova fica só como
movimento. Isso impede a câmera mais agitada de ocupar a vez das outras, e é o
que segura a entrega de `pessoa` perto do teto medido. Se o
detector cair, a gravação e a faixa de movimento seguem como se ele não
existisse; o log avisa uma vez quando ele sai do ar, e outra quando volta.

**Carro estacionado não vira marca a cada olhada.** O dwnvr lembra, por câmera,
onde já há um objeto parado, e só marca a CHEGADA. Essa memória não sobrevive
a um reinício: depois dele, cada objeto parado aparece uma vez como se tivesse
acabado de chegar.

O caminho inteiro, do quadro à marca de objeto, o que acontece quando o
detector cai e os limites do modelo estão em [`deteccao.md`](deteccao.md).

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
