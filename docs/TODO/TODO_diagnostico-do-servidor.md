# TODO - o que falta no card "Este servidor" do Diagnóstico

Levantado em 22/09/2026, ao criar o card "Este servidor", que fica ao lado do
"Este navegador" na tela de Diagnóstico.

**Status: parcialmente implementado.** A lista abaixo é o levantamento inteiro
do que ajuda a investigar um problema na máquina que grava, na ordem de
utilidade. O que já existia antes do card fica marcado como tal, para ninguém
reimplementar.

A regra que vale para tudo, implementado ou não: só `/proc`, `/sys` e
`runtime`, sem exec (a imagem é `FROM scratch`). Cada campo é opcional e some
quando a fonte não existe. Nada de senha, secret de sessão ou URL RTSP na
resposta.

## Já existia antes do card

- Disco livre, total e usado pelo dwnvr (`/api/health`, card Disco).
- Uptime do dwnvr e da máquina (`/api/health`, faixa do topo).
- Hora e fuso do servidor (`/api/health`, faixa do topo).
- Versão, commit e data do build (`/api/version`, rodapé da tela).
- Status por câmera, reconexões e o último erro de cada uma (`/api/health`).
- Fila do detector de objetos (`/api/health`).
- go2rtc inacessível, pelo `go2rtcError` do `/api/cameras`.

## Implementado no card

Tudo pelo `GET /api/health/servidor` (`internal/api/servidor.go`). A tela é
`web/src/lib/servidor.js`, desenhada pelo `CardDiagnostico.svelte`.

| Item | Fonte |
|---|---|
| Temperatura de cada zona térmica, a mais quente primeiro | `/sys/class/thermal/thermal_zone*/{temp,type}` |
| Frequência atual e máxima da CPU, e o governor | `/sys/devices/system/cpu/cpu*/cpufreq/` |
| Load average e número de núcleos | `/proc/loadavg`, `runtime.NumCPU()` |
| Pressão (PSI) de CPU, memória e disco | `/proc/pressure/{cpu,memory,io}` |
| Memória total e disponível, swap total e usada | `/proc/meminfo` |
| Limite de memória do container | cgroup v2 `memory.max` (v1 como plano B) |
| Processos encerrados por falta de memória (OOM kill), desde que a máquina ligou | `oom_kill` do `/proc/vmstat` |
| Sistema de arquivos, dispositivo e se o storage está somente leitura | `/proc/self/mountinfo` |
| Teste de escrita no storage: 4 KB, `fsync`, tempo em ms | arquivo temporário em `storage.root` |
| go2rtc responde? Em quantos ms? Qual versão? | `GET /api` do go2rtc |
| Últimos 50 avisos e erros do log, e o total desde que subiu | `internal/logbuf`, em volta do handler do `slog` |

**Um desvio do plano original:** o levantamento falava em OOM kills do cgroup
(`memory.events`). Na implementação isso se mostrou inútil para o caso que
importa: quando o kernel mata o próprio dwnvr, o Docker sobe um container novo,
com um cgroup novo, e o contador volta a zero. O `oom_kill` do `/proc/vmstat` é
da máquina inteira e sobrevive, então foi ele que entrou.

## Falta implementar

### O processo do dwnvr

- **RSS do processo**: `VmRSS` do `/proc/self/status`. Quanto o dwnvr ocupa de
  verdade.
- **Goroutines, heap e número de GCs**: `runtime.NumGoroutine()`,
  `runtime.ReadMemStats`. Um número de goroutines que só cresce é sinal de
  vazamento (reconexão de câmera, proxy do live).
- **Arquivos abertos contra o limite**: contar `/proc/self/fd` e comparar com
  `syscall.Getrlimit(RLIMIT_NOFILE)`. Bater no limite dá erro estranho de
  socket e de arquivo.
- **CPU usada pelo dwnvr**: `utime+stime` do `/proc/self/stat` entre duas
  leituras. Diz se é o dwnvr que come a CPU ou outra coisa na máquina (go2rtc,
  detector).
- **Versão do Go, GOARCH/GOARM, GOMAXPROCS, GOMEMLIMIT**: `runtime.Version()`,
  `debug.ReadBuildInfo()`. Diz qual binário está rodando de fato, por exemplo se
  é o arm64 ou o armv7.

Cuidado com o `ReadMemStats`: ele para o mundo por um instante. Uma vez a cada
15 s com o card aberto não pesa, mas não deve ir para o `/api/health`.

### Container e permissões

- **UID/GID do processo**: `os.Getuid()`, `os.Getgid()`. O
  `docker-compose.yml` já avisa que um UID errado faz o `cameras.json` não
  gravar; ver o número na tela fecha essa investigação.
- **Se roda em container**: `/.dockerenv` ou `/proc/1/cgroup`. É contexto para
  ler o resto.
- **Limite de CPU e uso de memória do cgroup**: `cpu.max`, `memory.current`. O
  limite de memória já entrou; estes dois ficaram de fora.

### Armazenamento

- **Inodes livres**: `Files` e `Ffree` do `statfs`, no mesmo lugar onde o
  espaço livre já é lido (`internal/retention`). Muitos segmentos pequenos
  esgotam os inodes com espaço sobrando.

### Dependências externas

- **Detector de objetos responde? Em quantos ms?** Hoje só aparece a fila. Um
  GET simples no `detector.url` separaria "fora do ar" de "lento", do mesmo
  jeito que já é feito com o go2rtc.

### Identidade do ambiente

- **Modelo da placa**: `/proc/device-tree/model` ("OrangePi Zero3"). Só existe
  em ARM.
- **Modelo da CPU**: `/proc/cpuinfo`.
- **Versão do kernel**: `/proc/sys/kernel/osrelease`, que é a do host mesmo
  dentro do container.
- **Hostname**: `os.Hostname()`.

O **sistema operacional do host** fica de fora de propósito: de dentro do
container o `/etc/os-release` seria o da imagem, e a `scratch` nem tem esse
arquivo.

### Configuração em uso

- Os caminhos e as URLs do go2rtc e do detector que o dwnvr está usando de fato,
  sem senha nem secret. O caminho do storage já aparece no card.

## Onde encaixar

Tudo acima cabe no mesmo `GET /api/health/servidor` e no mesmo card: um grupo
novo "Processo" para a primeira seção, e itens novos nos grupos que já existem
para o resto. Nenhum desses itens custa mais que ler um arquivo pequeno.
