# SQLite

**Veredito (10/11/2026): não recomendo para a 1.0.0.** As duas razões da
proposta, migrations mais flexíveis e consultas mais rápidas, quase não se
aplicam ao que o dwnvr grava, e o custo foi medido e é alto num projeto que se
apresenta como "sem banco de dados".

**Decidido em 10/11/2026: fica no radar, fora da 1.0.0.** Volta pelo critério
de [Quando reabrir](#quando-reabrir).

A proposta, nas palavras do mantenedor: "deixa mais flexível com as migrations
de BD e também pode otimizar as consultas que hoje são feitas em arquivos json
no disco".

## O que o dwnvr grava, e se o SQLite ajudaria

| Dado | Tamanho e acesso | O SQLite ajudaria? |
| --- | --- | --- |
| `cameras.json`, `usuarios.json`, `acessos.json`, em `/etc/dwnvr` | dezenas de registros, lidos na subida e mantidos em memória; gravados por arquivo temporário e rename | Não. A gravação já é atômica, e não há volume que peça índice |
| `index/{dia}.ndjson`, por câmera | uma linha por segmento (~2.700 por dia com os 30 s do padrão), acrescentada a cada segmento; a timeline lê o dia inteiro, em sequência | Não. Append e leitura sequencial são o melhor caso do arquivo, e o segmento de vídeo continua sendo arquivo |
| `eventos/{dia}.ndjson`, por câmera | milhares de onsets e dezenas de objetos por dia; Detecções e o calendário cruzam todas as câmeras e todos os dias, com filtro | Em parte, ver [As consultas](#as-consultas) |
| `quadros/`, `init/`, `avatares/` | imagens e o init do MP4 | Não. Binário fica melhor em arquivo |

## As consultas

As que cruzam muitos arquivos são as de Detecções: a página
(`handleDetections`) e o calendário (`handleDetectionDays`), em
[`internal/api/detections.go`](../../../internal/api/detections.go). Elas já leem
cada dia do disco uma vez só: o `DayObjects`
([`internal/store/objects.go`](../../../internal/store/objects.go)) memoriza as
marcas de objeto de cada dia, e do dia corrente lê só o pedaço novo do arquivo.
O comentário do `diasComDeteccao` registra o custo no hardware de referência:
**~1,3 s na primeira chamada, com o cache de disco frio, e ~5 ms nas
seguintes.**

- O SQLite melhoraria a primeira chamada: com índice, contar não obriga a
  passar pelos milhares de onsets.
- Não melhoraria o filtro por área, que é geométrico (a caixa contra o
  polígono, em `internal/area`). O banco filtra por câmera, família e instante;
  a área continua em Go, linha a linha.
- As chamadas seguintes já saem da memória.

Se a primeira chamada incomodar no uso, há caminho barato antes do banco:
gravar as marcas de objeto num arquivo separado dos onsets, ou aquecer a
memória em segundo plano depois da subida, só com o detector de objetos ligado.

## As migrations

A flexibilidade é ao contrário. O JSON aceita campo novo sem migration: o
`omitempty` e a leitura que tolera o campo ausente, como a do `store.Entry`,
já fazem isso. O SQLite dá estrutura (tipo, unicidade, chave estrangeira,
transação) e cobra uma migration a cada mudança de schema, com um `ALTER TABLE`
limitado: renomear e apagar coluna ele faz, mudar o tipo exige recriar a
tabela.

A mudança de formato que a 1.0.0 traz, a dos nomes em português (ver
[conversao-en-us.md](conversao-en-us.md)), é uma só, e vai por um guia de
atualização, sem código de compatibilidade. O que falta hoje é o gancho para as
próximas: um número de versão no que o dwnvr grava, que a conversão avalia.

## O que o SQLite custaria

Medido em 10/11/2026 no notebook, com o Go 1.27.1. O método está em
[Como medir de novo](#como-medir-de-novo).

| | `modernc.org/sqlite` v1.62.0 | `github.com/ncruces/go-sqlite3` v0.37.0 |
| --- | --- | --- |
| Como fica sem C | o C do SQLite transpilado para Go | o SQLite compilado para WASM e convertido em Go, sem runtime de WASM no binário |
| Binário a mais, arm64 | +4,3 MB | +5,2 MB |
| Binário a mais, amd64 | +4,6 MB | +5,6 MB |
| Memória a mais, no pico (amd64) | +10 MB | +11 MB |
| Módulos que entram no binário | 8 | 4 |
| Teste com `-race` | ~45 vezes mais lento | ~34 vezes mais lento |

- **O binário do dwnvr tem ~8 MB**: o banco o deixaria uns 50% maior.
- **O dwnvr usa 18 MB de RAM** no Orange Pi Zero 3 com 9 câmeras (29 MB com o
  detector de objetos), pelo README. Os +10 MB foram medidos no notebook, e se
  conferem na placa se o SQLite entrar.
- **As dependências.** O README abre com "Sem banco de dados" e diz, em
  §Tecnologias, que "a única dependência Go do projeto é `go.yaml.in/yaml/v3`
  (...) essa ausência é o projeto". O modernc traz 8 módulos (ele, `libc`,
  `mathutil`, `memory`, `x/sys`, `go-humanize`, `uuid` e `bigfft`), e exige que
  o `go.mod` de quem o usa fixe a mesma versão do `modernc.org/libc` que o dele
  fixa ([issue #177](https://gitlab.com/cznic/sqlite/-/issues/177), citada no
  `doc.go` e no CHANGELOG deles): um PR do Dependabot que suba só o `libc`
  quebra o build.
- **Duas fontes da verdade.** Hoje o índice mora ao lado do vídeo, e o
  [`docs/arquitetura.md`](../../arquitetura.md#armazenamento) não guarda o
  caminho do segmento porque "guardar os dois abriria espaço para divergirem".
  Com o índice no banco, a retenção que cai entre apagar a pasta e apagar as
  linhas, ou alguém que apaga ou restaura uma pasta à mão, deixam o banco
  dizendo outra coisa que o disco, e a subida precisaria reconciliar os dois.
- **Disco e queda de energia.** Cada transação grava ao menos uma página de
  4 KB no WAL, e de novo no checkpoint; o append do índice grava 77 bytes. Num
  disco que recebe GB de vídeo por dia isso não pesa, mas `/etc/dwnvr` costuma
  ficar no cartão SD, onde vira desgaste. Na queda de energia, o NDJSON perde no
  máximo a última linha, que a leitura já tolera; o SQLite em WAL resiste se o
  disco respeitar o fsync, e cartão SD barato nem sempre respeita.
- **Operação.** Hoje se confere o índice com `cat`, `jq` e `tail -f`
  (§Conferir as gravações por fora da interface, no README). Com o banco, é
  preciso o `sqlite3` na máquina, porque a imagem é `FROM scratch`. E um `cp`
  do banco em uso pode sair inconsistente: o backup passa a pedir
  `VACUUM INTO`.
- **A arquitetura do request não muda.** O SQLite tem um escritor por vez. O
  princípio "request nunca espera disco" continua pedindo estado em memória e
  gravação em segundo plano, como hoje.

## O que ele traria de fato

- Consulta com índice para feature que ainda não existe: histórico dos alertas
  enviados, auditoria de quem fez o quê, busca por classe em 90 dias,
  estatísticas.
- Transação entre registros diferentes, e unicidade garantida pelo banco (o
  e-mail da pessoa, por exemplo, que hoje o Go garante).
- A primeira chamada de Detecções sem crescer com o histórico, quando houver
  muito mais câmeras ou dias guardados.

## Quando reabrir

Quando uma feature pedir consulta que o arquivo não responde barato, como as da
lista acima, ou se a primeira chamada de Detecções incomodar no uso e os
caminhos baratos não resolverem. Mesmo aí, o candidato são os eventos, e não o
índice das gravações.

## Se um dia entrar

- Só os eventos e o que for relacional de verdade. O índice das gravações fica
  em NDJSON, ao lado do vídeo.
- O driver se escolhe na hora. O ncruces traz menos módulos e não tem a amarra
  do `libc`; o modernc é o mais usado.
- Os números da tabela acima se medem de novo, e a memória, na placa.

## Como medir de novo

Três programas num módulo à parte: um com JSON e HTTP da biblioteca padrão, e
dois iguais a ele mais o driver (`_ "modernc.org/sqlite"`, ou
`_ "github.com/ncruces/go-sqlite3/driver"` com `_ ".../embed"`), que abrem um
banco em WAL com `synchronous=NORMAL`, gravam 20 mil linhas em 20 transações e
fazem uma consulta.

- Tamanho: `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w"`,
  as flags do `Dockerfile`, com e sem o driver.
- Memória: `/usr/bin/time -f %M`, o pico do processo.
- Módulos: `go version -m` no binário.
- `-race`: o mesmo trabalho num `_test.go`, com `go test -race` e sem.
