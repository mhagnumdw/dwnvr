# Configuração

O que o dwnvr lê, o que ele escreve, e o que cada política de gravação custa em
CPU e em disco.

- [Os dois arquivos](#os-dois-arquivos)
- [Política por câmera](#política-por-câmera)
- [Valor fora da faixa](#valor-fora-da-faixa)
- [Retenção](#retenção)
- [Áudio](#áudio)
- [Limite de tentativas no login](#limite-de-tentativas-no-login)

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
`.session-secret`, que assina os cookies de sessão, e com o `usuarios.json`, as
pessoas que entram além do dono do `dwnvr.yaml`, que a tela Usuários e a Minha
conta gravam (ver [Quem entra](api.md#quem-entra)). O `usuarios.json` é `0600`,
como o `.session-secret`, porque guarda as senhas, em hash. A foto de cada
pessoa fica ao lado, em `avatares/`, um JPEG de até 32 KB por pessoa. Se ele ficar ilegível, o
dwnvr sobe do mesmo jeito, só o dono entra, e nada é gravado por cima dele até
ser corrigido.

Também ao lado fica o `acessos.json`, `0600`, com o último acesso de cada
pessoa, que a tela Usuários mostra. Ele nasce no primeiro acesso e é gravado
em segundo plano, no máximo a cada 5 minutos e ao desligar: numa queda de
energia, perdem-se no máximo esses minutos. Ilegível, o dwnvr sobe do mesmo
jeito, avisa no log e recomeça os acessos do zero.

Há ainda um terceiro arquivo que o dwnvr **só lê pela API, nunca abre**: o
`go2rtc.yaml`. Ele é do go2rtc, e configurá-lo é tarefa de quem instala. Veja
[`go2rtc.example.yaml`](../go2rtc.example.yaml).

## Política por câmera

Tudo que é política de gravação é **por câmera**, não global:

| Campo | O que decide | Aceito |
| --- | --- | --- |
| stream do go2rtc | qual fonte gravar - a de alta ou a de baixa resolução | |
| `audio` | o áudio gravado - ver abaixo | `none`, `flac` ou `aac` |
| `quotaMB` | quanto disco aquela câmera pode ocupar | 100 ou mais |
| `segmentSeconds` | duração alvo de cada segmento | de 10 a 300 |
| `maxDays` | idade máxima, opcional | 0 (sem limite) ou mais |
| `stallSeconds` | tolerância antes de considerar o stream morto | de 1 a 3600 |
| `detect` | marcar movimento na timeline - ver abaixo | |
| `detectMecanismo` | quem decide disparar | `kleinberg-p` ou `periodico` |
| `detectSensibilidade` | o nível, que é o custo em CPU | de 1 a 5 |

Ser por câmera não é preciosismo: numa instalação real as câmeras não são
iguais. A do portão merece alta resolução e cota grande; a do corredor não. E o
`stallSeconds` certo depende do enlace - o tipo de conexão - de cada câmera:
uma câmera em wi-fi ruim precisa de mais tolerância que uma no cabo.

Os valores de `defaults` no `dwnvr.yaml` valem para qualquer câmera que não
defina o campo.

Salvar a câmera pela tela não a prende aos `defaults` de hoje: o campo que ela
não definia e chega igual ao default fica fora do `cameras.json`, e continua
seguindo o `dwnvr.yaml`. O campo que ela já definia fica, mesmo igual ao
default. Duas consequências:

- pela tela, não dá para fixar numa câmera um valor igual ao default de hoje:
  isso só se faz escrevendo o campo no `cameras.json`;
- uma câmera salva por uma versão anterior pode ter todos os campos gravados,
  com os defaults da época, e só volta a seguir o `dwnvr.yaml` quando eles
  saem do `cameras.json`.

Nos dois casos, pare o dwnvr antes de editar o `cameras.json` e suba de novo
depois. O arquivo só é lido no boot, e salvar qualquer câmera pela tela com o
dwnvr no ar regrava o arquivo inteiro com o que ele tem na memória, por cima
da edição.

## Valor fora da faixa

Um número fora da faixa **não impede o dwnvr de subir**. Gravar com o padrão é
melhor que não gravar nada, e uma câmera com um valor ruim não pode deixar as
outras paradas. No boot, cada valor assim:

- passa a valer o padrão: o de `defaults` para a câmera, o do código para o
  `dwnvr.yaml`;
- vira um aviso no log, que aparece também na tela de Diagnóstico, com o campo,
  o valor escrito, o que é aceito e o que ficou valendo:

  ```text
  valor fora do aceito na configuração; vale o padrão até ser corrigido
  arquivo=/etc/dwnvr/cameras.json cam=cam_portao campo=quotaMB valor=5
  aceito="100 MB ou mais" usando=10240
  ```

No `cameras.json`, zero e campo ausente são "usar o padrão" e nunca dão aviso.
No `dwnvr.yaml` o zero é inválido, porque ali o padrão é o próprio valor. As
exceções são o `maxDays`, onde zero é "sem limite", e o `storage.minFreeMB` e
o `server.loginAttemptsPerMinute`, onde zero desliga a trava global de disco e
o limite de tentativas no login.

O que continua impedindo o boot é o que não tem padrão seguro: um arquivo que
não se consegue ler, o `storage.root` vazio e o ID de câmera que viraria um
caminho fora do diretório de gravações ou que tem vírgula (ela separa os IDs
no `cams=` da API).

A tela de Câmeras e a API usam as mesmas faixas, mas lá o valor ruim é
**recusado** em vez de trocado, porque há alguém na frente da tela para ler a
mensagem e corrigir na hora. As faixas moram em
[`internal/config/parametros.go`](../internal/config/parametros.go).

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
| --- | --- | --- |
| 1 - muito baixa | 6 | 2,8% |
| 2 - baixa | 12 | 9,2% |
| 3 - média | 25 | 22,3% |
| 4 - alta | 50 | 41,5% |
| **5 - muito alta** | **100** | **57,4%** |

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
objeto vai para o mesmo `eventos/{dia}.ndjson`, e o quadro em que o objeto
aparece vai para `quadros/{dia}/{instanteMs}.jpg`, do lado. Os dois somem junto com o vídeo
na retenção, e os quadros contam na cota da câmera: ~14 KB por detecção, uns
37 MB num dia de nove câmeras movimentadas.

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
e o container ocupa ~230 MB. No nível 5, o padrão, isso é ~17% de um núcleo
por câmera, contínuo; no 4, a metade. As olhadas passam por uma **fila com
dois lugares por câmera**: se uma câmera dispara de novo com os dois ocupados,
a marca nova fica só como
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

Gravação de câmera removida sem marcar "apagar também as gravações" não entra
em nenhum dos três, nem no disco livre mínimo: o dwnvr não apaga sozinho o que o
usuário preservou, mesmo que a alternativa seja apagar gravação de câmera no
ar. Ela aparece em **Gravações de câmeras removidas**, na tela Câmeras, de onde se apaga,
e o Diagnóstico a mostra à parte na barra do disco. Com o disco apertado, o
aviso da retenção no log traz o total dela em `orfas_mb`.

A cota é aplicada a cada minuto, então o pico real é `cota + taxa × 60s` - com
uma câmera de 900 kbps isso são ~7 MB de folga, desprezível contra uma cota real.

## Áudio

O modo de áudio é escolhido por câmera e vira um filtro de codec na URL:

| Modo | CPU | Disco | Mexe no go2rtc? |
| --- | --- | --- | --- |
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

## Limite de tentativas no login

```yaml
server:
  loginAttemptsPerMinute: 2
  trustedProxies: [127.0.0.0/8, "::1/128", 172.16.0.0/12]
```

Os valores acima são os padrões, que valem sem escrever nada.

Cada endereço tem **5 tentativas de login seguidas**. Depois delas, as
seguintes passam no ritmo do `loginAttemptsPerMinute`: com o padrão, uma a cada
30 s. Quem para de tentar recupera uma vaga a cada intervalo, até as 5. Passado
o limite, o login responde sem conferir a senha, e a tela conta os segundos até
a próxima tentativa. Conta toda tentativa, a certa também.

Quem só errou a senha duas ou três vezes não vê o limite. Ele existe para quem
testa senhas sem parar: cada tentativa custa 0,77 s de CPU num Orange Pi Zero 3
(ver [O login](api.md#o-login)), e sem limite isso ocupa um núcleo o tempo
todo, e o login de quem é da casa espera atrás.

`0` desliga o limite. O aceito vai de 0 a 12 por minuto.

O limite é **por endereço**, e não por usuário: por usuário, qualquer um que
alcance a tela travaria o login do dono só errando a senha dele de propósito.
Como é por endereço, quem tenta de muitos endereços ao mesmo tempo não é
contido por ele.

### De onde vem o endereço

Atrás de um proxy, toda conexão chega ao dwnvr com o endereço do proxy, igual
para todo mundo. Quem chegou de verdade vem no header `X-Forwarded-For`, que o
proxy preenche. Mas quem fala direto com a porta do dwnvr também consegue
mandar esse header, inventando um endereço a cada tentativa. Por isso o
`trustedProxies` lista as redes dos proxies confiáveis:

- se a conexão vem de uma delas, o endereço é o do `X-Forwarded-For`, lido da
  direita para a esquerda até o primeiro que não seja de proxy confiável;
- se não vem, o endereço é o da conexão, e o header é ignorado.

O padrão cobre o loopback e a faixa que o Docker usa para as redes do compose.
É por ela que chegam os dois caminhos de HTTPS desta documentação: o
`tailscale serve` do [acesso remoto](acesso-remoto.md), pelo gateway da rede do
compose, e o Caddy do [HTTPS na rede de casa](https-na-rede-local.md), pelo IP
do container dele. Os dois põem no header o endereço de quem conectou, no lugar
do que o aparelho tiver mandado. Isso foi conferido em 13/10/2026 com o
`tailscale serve` 1.102 e o Caddy 2.11.7. Quem abre direto na 8080 pela rede de
casa chega com o próprio endereço, fora da lista.

Mude o `trustedProxies` só se o seu proxy chega ao dwnvr por outra rede, como
um nginx em outra máquina. Cada item é uma rede (`10.0.0.0/8`) ou um endereço
só (`192.168.1.20`). Um item inválido faz a lista inteira voltar ao padrão, com
aviso no log. `[]` não confia em ninguém: atrás de um proxy, todo mundo vira
um endereço só, e quem passa do limite trava o login de todos.

Dois casos fazem o padrão errar:

- **A rede de casa dentro de `172.16.0.0/12`.** Um aparelho dela, falando direto
  com a 8080, conseguiria inventar o endereço. Tire a faixa e ponha só a rede
  do compose, que o `docker network inspect` mostra.
- **Docker rootless** (não testado). Pelo que diz a documentação dele, toda
  conexão chega ao container com o endereço do gateway, que está na lista.
  Como no caso de cima, o header passa a ser aceito de qualquer aparelho.
