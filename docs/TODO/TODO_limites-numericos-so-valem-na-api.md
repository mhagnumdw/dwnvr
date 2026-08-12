# TODO — os limites numéricos de câmera só valem no caminho da API

Levantado em 12/08/2026, ao dar piso de 100 MB à cota em disco. Não quebra nada
hoje: é uma **cobertura desigual** entre as duas portas por onde uma câmera entra
no dwnvr.

**Status: não implementado, por decisão.** O piso da cota entrou onde ele resolve
o caso real — a tela e a API. O que segue é o buraco que sobrou.

## As duas portas

Uma câmera chega ao recorder por dois caminhos, e eles validam coisas diferentes.

**Pela API** (`internal/api/cameras.go:114`, `validateCamera`), que é o que a tela
usa: ID, áudio, cota (`>= 100 MB`), `segmentSeconds` (0–3600), `maxDays` e
`stallSeconds` (0–3600).

**Pelo arquivo** (`internal/config/config.go:207`, `LoadCameras`), lido no boot:
só **ID e áudio**. Nenhum dos limites numéricos é checado.

E o `dwnvr.yaml` tem um terceiro conjunto (`config.go:152`, `validate`):
`storage.root`, `defaults.segmentSeconds`, `defaults.stallSeconds` e
`defaults.audio` — mas **não** `defaults.quotaMB` nem `defaults.maxDays`.

## O que isso causa

Editar o `cameras.json` à mão com `"quotaMB": 5` sobe normalmente. Pior: como o
`Resolve` (`config.go:184`) só troca por default quando o valor é `<= 0`, o 5
sobrevive inteiro até `retention.go:91`, onde vira `5 << 20`. A câmera passa a
apagar o que acabou de gravar, e o único sinal é o log de retenção evictando sem
parar.

Um `defaults.quotaMB: 5` no `dwnvr.yaml` faz o mesmo com **todas** as câmeras que
não têm cota própria, sem passar por validação nenhuma.

Vale reforçar que o `cameras.json` é um arquivo **gerenciado pela API** — o
cabeçalho do `config.go:6` já diz isso. Editar à mão não é o fluxo previsto, e é
por isso que isso é um TODO e não um defeito.

## Por que não foi feito agora

Mover a validação para o `LoadCameras` transforma um valor ruim em **falha de
boot**: hoje o dwnvr sobe e grava; depois, um `cameras.json` com um número fora
da faixa derrubaria as nove câmeras de uma vez. Para um NVR que roda esquecido
num armário, recusar-se a subir é pior que gravar com uma cota estranha.

Isso empurra a solução para "corrigir e avisar" em vez de "recusar", que é uma
decisão de comportamento — não uma linha de validação. Não cabia na tarefa do
dia.

## Como implementar, se um dia valer

1. Extrair de `validateCamera` a parte que é só faixa numérica, para
   `config.ClampCamera(cam) (Camera, []string)`: devolve a câmera corrigida mais
   a lista do que foi corrigido.
2. `LoadCameras` chama o clamp e devolve os avisos; o `main.go:73` os loga com
   `log.Warn`, no mesmo lugar onde já avisa "nenhuma câmera cadastrada". Sobe
   sempre.
3. A API continua **recusando** em vez de corrigir — lá existe alguém na frente
   da tela para ler a mensagem, que é justamente o que falta no boot.
4. Estender o `config.validate()` para `defaults.quotaMB` e `defaults.maxDays`.
   Esse pode recusar mesmo: o `dwnvr.yaml` é escrito por gente, e o arquivo já
   derruba o boot por `storage.root` vazio.

Estimativa: ~40 linhas. O `internal/config` **não tem nenhum teste hoje**, então
o custo escondido é criar o `config_test.go`.

## Vale a pena?

**Não agora.** Ninguém edita o `cameras.json` à mão nesta instalação, e a tela
cobre o caminho por onde os valores realmente entram.

O critério para reabrir é um só: **alguém além da tela passou a escrever
câmera?** Um script de provisionamento, um segundo dwnvr copiando config, um
backup restaurado à mão — em qualquer um desses o boot vira porta de entrada de
número ruim, e aí o clamp com aviso passa a valer as 40 linhas.
