# TODO - log que sobrevive à recriação do container

Levantado em 08 e 09/10/2026, só em conversa, sem código. Os números vêm da
instalação de teste (Orange Pi Zero 3 com Armbian, cartão SD).

**Status: não implementado.**

## tl;dr

**Vale a pena:**

- **O dwnvr grava o próprio log num arquivo, com rotação feita por ele, dentro
  do volume que já existe** (`/storage`, numa pasta reservada). O compose não
  muda e ninguém precisa criar pasta. Funciona igual em Docker, Podman e em
  qualquer host, e fica no disco, fora da RAM. Ele continua escrevendo no
  stderr, então o `docker logs` não muda. O arquivo vira o lugar central da
  investigação, porque o dwnvr já registra o que vê do go2rtc (conexão que cai
  ou trava) e do detector de objetos (caiu, voltou).

**Não vale a pena:**

- **Depender do driver `journald` do Docker.** Ele resolve sem código, mas
  depende do host: não existe no Docker Desktop (Mac e Windows) nem sem systemd.
  No Armbian, o `/var/log` mora em RAM e se perde numa queda de energia, que é
  justamente quando o log faria falta.
- **Mudar o driver de log no compose.** O compose é o mesmo para todo mundo, e
  as opções de um driver dão erro no outro: não dá para escolher pelo `.env`.
- **Arquivo próprio no go2rtc.** Ele grava em arquivo, mas sem rotação, e a
  saída é uma só: ligar o arquivo esvazia o `docker logs` dele. Além disso, o
  `go2rtc.yaml` é de quem instala.
- **Arquivo próprio no dwnvr-detect.** Ele quase não escreve, e o arquivo
  exigiria um volume novo para uma imagem que roda como UID 65534: a pasta
  criada pelo Docker nasceria de root e ele não conseguiria escrever.
- **Documentar agora os drivers do Docker no `docs/operacao.md`.** Fica para
  depois (ver o fim).

## O problema

O Docker guarda o log junto do container. Ele resiste a `restart`, a crash e a
reboot da máquina, mas é apagado quando o container é **recriado**: `docker
compose down`, ou `docker compose up` depois de um pull. Toda atualização,
portanto, apaga o histórico de antes dela.

Existe um segundo problema: sem opções no `logging`, o driver padrão do Docker
(`json-file`) não faz rotação, e o arquivo cresce sem limite no disco do host.

## O que foi medido

Na instalação de teste:

| Container | Volume de log | Observação |
| --- | --- | --- |
| dwnvr | ~70 KB por dia | 53 KB em 19 h; 107 INFO, 269 WARN, 4 ERROR |
| go2rtc | ~170 KB por dia | 495 KB em 3 dias |
| dwnvr-detect | quase nada | 97 bytes desde a subida |

Sem rotação, o go2rtc soma uns 60 MB por ano. Não é urgente.

Driver em uso: `json-file`, sem `max-size`. O `/var/log` do Armbian é um zram
(`armbian-ramlog`), copiado para o cartão de tempos em tempos e no desligamento
normal.

## Os drivers do Docker

| Driver | Sobrevive à recriação | Rotação | Contra |
| --- | --- | --- | --- |
| `json-file` (padrão) | não | só com `max-size` e `max-file` | é o problema de hoje |
| `local` | não | sim | idem |
| `journald` | sim | do journald | depende do host; o `/var/log` pode morar em RAM |
| `syslog`, `fluentd`, `gelf` | sim | do destino | exigem um serviço do outro lado |

Nenhum deles grava num arquivo do host com rotação e sobrevive à recriação sem
depender de algo instalado no host.

## O que cada container faz

| Container | Destino | Motivo |
| --- | --- | --- |
| dwnvr | arquivo próprio em `/storage`, mais o stderr | é o log que mais ajuda a investigar, e o código é nosso |
| go2rtc | `docker logs` | `log.output: file:/caminho` não faz rotação e desliga o stdout; o go2rtc fica fora do escopo do dwnvr |
| dwnvr-detect | `docker logs` | quase não escreve; o dwnvr já avisa quando ele cai e quando volta |

## Onde o arquivo do dwnvr fica

Dentro do `/storage`, e não num volume novo nem no `/etc/dwnvr`:

- **Volume novo:** muda o compose e obriga cada instalação a criar a pasta antes
  de subir. É o mesmo problema que o compose já documenta para as outras duas
  pastas.
- **`/etc/dwnvr`:** mistura log com configuração, e essa pasta costuma ficar no
  cartão SD, e não no disco das gravações.

**Cuidado: hoje o store trataria a pasta como câmera órfã.** O `Store.Orphans`
(`internal/store/store.go`) lista como órfão todo diretório da raiz que não
pertence a uma câmera cadastrada. Uma pasta `logs/` apareceria na lista de
gravações órfãs, e o botão que apaga órfãs a apagaria. Além disso, o
`config.ValidateCameraID` aceita `logs` como ID de câmera. O nome da pasta tem
que ser reservado nos dois lugares: o `Orphans` ignora a pasta, e o
`ValidateCameraID` recusa o nome.

## Como implementar, quando valer

1. Três campos novos no `dwnvr.yaml`, com padrões que funcionam sem mexer: a
   pasta, o tamanho de cada arquivo e quantos arquivos guardar (por exemplo, 5
   de 5 MB). Com os 70 KB por dia, isso guarda perto de um ano.
2. Rotação em Go puro, sem C: um `io.Writer` que troca de arquivo ao passar do
   tamanho. Ele entra ao lado do `os.Stderr`, por baixo do `logbuf`, que
   continua guardando em memória os avisos para o Diagnóstico.
3. A pasta reservada no `Store.Orphans` e no `config.ValidateCameraID`.
4. Se a pasta não der para escrever, o dwnvr avisa no stderr e segue só com
   ele. O log não pode derrubar a gravação.
5. Escrever direto, sem buffer próprio: numa queda de energia se perdem só as
   últimas linhas, que ainda estavam no cache do sistema.
6. O resto (campo no `config.go`, `dwnvr.example.yaml`, `docs/configuracao.md`,
   `docs/operacao.md`) está na tabela de repercussões do `AGENTS.md`. Não é
   mudança incompatível: quem atualiza ganha o arquivo sem precisar agir.

Extensão possível, só se fizer falta: o Diagnóstico ler o arquivo para mostrar
também os avisos de antes do último reinício, que hoje se perdem com a memória.

## Fica para depois

- **Documentar os drivers do Docker no `docs/operacao.md`:** o `daemon.json` do
  host com `json-file` mais `max-size`, ou o `journald`. Resolveria o
  crescimento sem limite do go2rtc e do dwnvr-detect sem tocar no compose.
- **Mudar o nível de log em tempo de execução.** Hoje o nível só se escolhe na
  subida, pela flag `-debug`, e ligá-la exige recriar o container, o que apaga o
  log que se queria investigar. O `slog.LevelVar` permitiria mudar sem
  reiniciar, sem custo. Mas só há 4 pontos em DEBUG no código, e um deles
  ("segmento fechado") é uma linha por câmera a cada 30 s, ou seja, quase só
  ruído. A troca só vale quando houver DEBUG útil para ligar.

## Achado ao lado

Dos 269 WARN medidos no dwnvr, cerca de 170 são "requisição recusada" com
status 401 ou 404, que parecem rotina (uma sessão vencida, por exemplo). Eles
ocupam o buffer de 50 linhas do Diagnóstico e empurram para fora os avisos que
importam, como "conexão caiu". Não foi investigado.
