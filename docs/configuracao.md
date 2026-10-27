# Configuração

O que o dwnvr lê, o que ele escreve, e o que cada política de gravação custa em
CPU e em disco.

- [Os dois arquivos](#os-dois-arquivos)
- [Política por câmera](#política-por-câmera)
- [Valor fora da faixa](#valor-fora-da-faixa)
- [Retenção](#retenção)
- [Áudio](#áudio)
- [Limite de tentativas no login](#limite-de-tentativas-no-login)
- [E-mail](#e-mail)

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

Quem edita o `usuarios.json` à mão vê o resultado no log e nos Avisos da tela
Diagnóstico, ao reiniciar: o arquivo ilegível, a pessoa ignorada (usuário fora
do formato, repetido, igual ao do dono ou com papel desconhecido), o e-mail
ilegível e o e-mail repetido. A pessoa entra pelo usuário ou pelo e-mail (ver
[O login](api.md#o-login)), e o e-mail repetido não serve para entrar: não diz
quem está entrando.

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
| `areas` | as áreas desenhadas sobre a imagem - ver abaixo | até 8 |
| `alert` | quando uma detecção vira alerta - ver abaixo | |

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

As `areas` não são política de gravação: são os trechos da imagem, como
"Portão" e "Garagem", por onde Detecções filtra. Mudar uma área não reinicia a
gravação. O admin as desenha na aba Áreas do cadastro da câmera, ou em
Detecções, com o "salvar na câmera" do editor de área.

```json
"areas": [
  { "id": "k3f9a2", "name": "Portão", "rule": "foot",
    "points": [[0.12, 0.30], [0.55, 0.30], [0.55, 0.80], [0.12, 0.80]] }
]
```

- `points` são de 3 a 20 vértices, em fração do quadro (de 0 a 1, com o Y
  crescendo para baixo), e valem em qualquer resolução da câmera.
- `rule` decide quando o objeto conta como dentro: `foot`, o padrão, quando o
  meio da base da caixa está na área; `touch`, quando a caixa encosta nela.
- `name` não pode ficar vazio nem se repetir na câmera.
- `id` é curto, só letras minúsculas e dígitos, e o servidor gera quando a área
  chega sem ele. Os links apontam para ele, e renomear a área não os quebra.
  Uma área escrita à mão sem `id` ganha um a cada boot, até a câmera ser salva
  pela tela.

Se a câmera for movida, a área fica no lugar errado: redesenhe.

O `alert` também não é política de gravação: diz quando uma detecção da câmera
vira alerta. O admin o liga na aba Alerta do cadastro da câmera, que só
funciona com a marcação de movimento ligada e o detector de objetos
configurado; sem eles, a aba diz o que falta. Por ora o alerta
vai só para o log e fica marcado na detecção, que Detecções filtra (ver
[`deteccao.md`](deteccao.md#o-alerta)). Mudar o alerta não reinicia a
gravação.

```json
"alert": { "enabled": true, "families": ["pessoa"], "areas": ["k3f9a2"] }
```

- `families` são as famílias que alertam: `pessoa`, `veiculo` ou `animal`. Com
  o alerta ligado, pelo menos uma.
- `areas` são os `id` das áreas da câmera em que o objeto precisa cair, cada
  uma pela própria `rule`. Vazio ou ausente é a imagem inteira.
- `schedules` são os horários em que o alerta sai, somados; vazio ou ausente é
  a qualquer hora. Ver abaixo.
- `enabled` desligado guarda as famílias, as áreas e os horários, para religar
  sem refazer.

Cada horário tem os dias e um De e um Até, ao minuto. A quarta com três
horários:

```json
"schedules": [
  { "days": [3], "from": "00:00", "to": "05:15" },
  { "days": [3], "from": "12:00", "to": "13:00" },
  { "days": [3], "from": "18:30", "to": "23:59" }
]
```

- `days` vai de 0 (domingo) a 6 (sábado). Vários horários podem ter o mesmo
  dia, e os que se sobrepõem valem pela soma.
- `from` e `to` são `HH:MM`, e as duas pontas entram: "até 13:00" inclui o
  minuto 13:00, e o dia inteiro é `00:00` até `23:59`.
- Nenhum horário atravessa a meia-noite: o `to` é maior que o `from`, nunca
  igual. A noite são dois horários, um até `23:59` e outro a partir de `00:00`
  do dia seguinte.
- Vale a hora em que a detecção aconteceu, no fuso do servidor, o mesmo que
  decide o dia das gravações. A detecção continua sendo gravada a qualquer
  hora: o horário decide só o alerta.
- Até 84 horários por câmera.

Na tela, os horários ficam na seção Horários da aba Alerta, em duas formas que
andam juntas: a grade da semana, em que se marca a hora inteira, e a lista, em
que se digita ao minuto. Marcar na grade refaz a lista, com os dias de mesmo
horário num horário só. Desmarcar "Limitar o alerta a alguns horários" apaga os
horários ao salvar.

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

Com as `areas` é igual: a área inválida sai, com um aviso, e as outras ficam.
No `alert`, sai com um aviso a família desconhecida, a área que a câmera não
tem e o horário inválido: sem dia, com dia fora de 0 a 6, com hora que não é
`HH:MM` ou com o `to` que não fica depois do `from`. O dia repetido num horário
conta uma vez, também com aviso. O alerta ligado que fica sem nenhuma família,
sem nenhuma das áreas que tinha ou sem nenhum dos horários que tinha é
desligado, também com aviso: sem as áreas, ele valeria na imagem inteira, e sem
os horários, a qualquer hora.

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
contido por ele. Entrar pelo e-mail conta no mesmo limite.

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

## E-mail

```yaml
smtp:
  host: smtp.gmail.com
  username: dwnvr.exemplo@gmail.com
  password: troque-me
```

Com esta seção, o dwnvr envia e-mail: hoje, o link de definir a senha, que o
admin manda da tela Usuários direto para a pessoa. Sem ela, o dwnvr não abre
conexão nenhuma, e a tela não mostra o "Enviar por e-mail" nem o teste do
Diagnóstico. O e-mail da pessoa se cadastra do mesmo jeito, porque serve para
entrar (ver [O login](api.md#o-login)), e a tela avisa que o link não vai por
e-mail. Serve qualquer provedor que fale SMTP.

| Campo | O que é | Sem escrever |
| --- | --- | --- |
| `host` | o servidor de e-mail, só o nome ou o IP | obrigatório |
| `security` | `starttls`, `tls` ou `none` | `starttls` |
| `port` | a porta | a da `security`: 587, 465 ou 25 |
| `username` e `password` | a conta que envia; os dois juntos, ou nenhum se o servidor não pede login | sem login |
| `from` | o remetente, como `dwnvr <dwnvr.exemplo@gmail.com>` | o `username`, quando ele é um endereço |

- **`starttls`**: a conexão começa sem TLS e passa para TLS antes do login. Se
  o servidor não oferecer o STARTTLS, o dwnvr não envia nada, nem a senha.
- **`tls`**: TLS desde o primeiro byte, na porta 465.
- **`none`**: sem TLS, para um servidor na rede de casa. Com login, só vale
  para `localhost`, `127.0.0.1` ou `::1`, porque a senha não sai sem TLS.

O certificado do servidor é sempre conferido, sem opção para desligar. O login
é o `AUTH PLAIN`, o mais comum. A senha fica no servidor: não vai à tela nem ao
log.

Uma seção com problema **não impede o dwnvr de subir**. Não há padrão que
substitua um servidor errado, então o e-mail fica desligado até o
`dwnvr.yaml` ser corrigido. O motivo vira aviso no card Avisos do Diagnóstico
e no sino do header, e vai também para o log:

```text
e-mail desligado: a seção smtp do dwnvr.yaml não serve até ser corrigida
arquivo=/etc/dwnvr/dwnvr.yaml problema="smtp.security \"ssl\" fora do aceito: use starttls, tls ou none"
```

Para conferir sem cadastrar ninguém, abra o card **Este servidor** do
Diagnóstico: ele mostra o servidor, a segurança e o remetente que ficaram
valendo, e o **Testar o e-mail** manda um para o endereço que você digitar,
com a resposta do servidor do jeito que ela veio.

### Com o Gmail

1. **Crie uma conta só para o dwnvr**, e não use a sua. A senha de app dá
   acesso à conta inteira e fica escrita no `dwnvr.yaml`: numa conta só dele,
   é só isso que ela abre.
2. **Ligue a verificação em duas etapas** nessa conta. Sem ela, o Google não
   oferece a senha de app.
3. **Gere uma senha de app**, como explica a
   [ajuda do Google](https://support.google.com/accounts/answer/185833). Ela
   tem 16 caracteres e é a `password`. O `username` é o endereço da conta.

O `host` é `smtp.gmail.com`, e a `security` e a porta padrão (`starttls`, 587)
já servem. Isso foi conferido em 24/10/2026, com uma conta nova e uma senha de
app: chegaram o "Testar o e-mail" do Diagnóstico e o link de definir a senha
mandado pelo "Enviar por e-mail" da tela Usuários.

Duas coisas que o Google diz e que param o envio:

- trocar a senha da conta revoga todas as senhas de app, e o dwnvr passa a
  receber "login recusado" até você gerar outra;
- uma conta comum envia até 500 e-mails por dia
  ([limites do Gmail](https://support.google.com/mail/answer/22839)).

Nos outros provedores, o `host`, a porta e a `security` estão na ajuda de
cada um. Os de e-mail transacional, como a Brevo, pedem um domínio seu para o
remetente: com um endereço `@gmail.com`, a Brevo troca o remetente por um
endereço dela.

## Alerta por e-mail

```yaml
alerts:
  email:
    to: [fulano@exemplo.com, ciclana@exemplo.com]
```

A lista de quem recebe o alerta de detecção de toda câmera com alerta ligado
(ver o `alert` em [Política por câmera](#política-por-câmera)), numa mensagem
só para todos: cinco endereços são um envio. O alerta sai pela seção `smtp`,
a mesma do link de definir a senha; aqui fica só quem recebe. Por ora, a lista
é conferida no boot, mas o alerta ainda não sai por e-mail.

| Campo | O que é | Sem escrever |
| --- | --- | --- |
| `alerts.email.to` | quem recebe; só o endereço, sem nome na frente | o alerta fica só no log e marcado na detecção |

Nada aqui impede o dwnvr de subir. Viram aviso no card Avisos do Diagnóstico e
no sino do header, e vão também para o log:

- **o que não é endereço**, como `Fulano <fulano@exemplo.com>`, que sai da
  lista enquanto os outros seguem. O endereço repetido sai calado;
- **a lista sem a seção `smtp`**, que não tem por onde sair. Com a seção
  recusada, o aviso é o dela, que diz o motivo.

```text
alertas por e-mail: alerts.email.to "Fulano <fulano@exemplo.com>" não é um endereço de e-mail e ficou de fora da lista arquivo=/etc/dwnvr/dwnvr.yaml
```
