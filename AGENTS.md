# AGENTS.md

Regras para agentes de IA que trabalham neste repositório.

## Escrita

- Use `-` (hífen) no lugar do travessão (em dash, U+2014), em qualquer texto:
  código, comentários, documentação, mensagens de commit e respostas no chat.
  A regra vale inclusive ao reescrever uma linha que já tinha o caractere.

## Repercussões

Neste repositório o mesmo fato vive em vários arquivos: um endpoint existe no
`server.go`, na tabela do `docs/api.md` e no cliente `api.js`; a versão do build
é carimbada em quatro lugares diferentes. Mexer num canto e esquecer o outro
deixa referência inválida, documentação mentindo ou - pior - um binário que sobe
normalmente servindo a tela antiga.

Antes de encerrar a tarefa, procure abaixo o que você tocou e confira o que vem
junto.

### Interface (`web/`)

- Alterou `web/src/`, `web/index.html` ou `web/public/`: rode `make web` (ou
  `npm run build` dentro de `web/`) e faça commit de `internal/api/dist/` junto.
  O `.github/workflows/ci.yml` refaz o build e reprova se sobrar diff.
- Os assets saem com hash no nome (`internal/api/dist/assets/index-*.js|css`).
  O `emptyOutDir` do `web/vite.config.js` apaga os antigos: confira o
  `git status` para que a remoção também entre no commit.
- Arquivo novo ou removido em `src/lib|routes|components|vendor`: bloco
  "Estrutura" do `web/README.md`.

### Rotas HTTP

- Rota nova, alterada ou removida em `internal/api/server.go`: a tabela
  correspondente do `docs/api.md` (Sessão / Câmeras e diagnóstico / Gravações /
  Live) e o cliente `web/src/lib/api.js` - `api` para o que devolve JSON,
  `mediaURL` para as URLs que são montadas.
- Rota que entra ou sai do `requireAuth`: o `docs/api.md` diz quais são públicas
  e por quê (`/api/session`, `/api/version`).
- Mudou o formato do JSON de resposta ou de erro: a tela que consome, em
  `web/src/routes/`.
- Arquivo `.go` novo em `internal/api/`: a árvore "Estrutura do projeto" do
  `README.md` lista esses arquivos um a um.

### Configuração

- Campo novo, renomeado ou removido em `internal/config/config.go`
  (`Server`/`Go2RTC`/`Storage`/`Defaults`): `dwnvr.example.yaml`, com o
  comentário explicando o campo, `docs/configuracao.md` e a seção
  "Configuração" do `README.md`.
- Mudou um default em `config.defaults()`: o mesmo número aparece escrito à mão
  no `dwnvr.example.yaml`, no `docs/configuracao.md` e no formulário
  `web/src/routes/Cameras.svelte` (`audio`, `quotaMB`, `segmentSeconds`,
  `maxDays`).
- Campo novo em `config.Camera`: aplicar o default em `config.Resolve`, validar
  em `internal/api/cameras.go`, expor no `Cameras.svelte` e listar na tabela
  "Política por câmera" do `docs/configuracao.md`.
- Limite numérico em `internal/api/cameras.go` (`minQuotaMB`, faixas de
  `segmentSeconds`/`stallSeconds`/`maxDays`): os `min`/`max`/`step` do input
  correspondente no `Cameras.svelte`. Os dois já divergem hoje - ver
  `docs/TODO/TODO_limites-numericos-so-valem-na-api.md`.

### Layout em disco e índice

- Diretórios, nome de arquivo ou `DayLayout` em `internal/store/store.go`:
  `docs/arquitetura.md`, `docs/operacao.md` (a tabela host↔container e o `tail`
  do índice) e o bloco "Conferir por fora da interface" do `README.md`.
- Campo novo em `store.Entry`: o índice é append-only, então as linhas já
  gravadas não terão o campo - a leitura precisa tolerar o zero.
  `internal/retention/retention.go` e `internal/api/recordings.go` percorrem
  esses mesmos caminhos.

### Build e versão

- Variável de `internal/buildinfo`: quatro lugares carimbam o mesmo valor por
  caminhos diferentes - `Makefile` (`LDFLAGS` e `BUILDARGS`), `Dockerfile`
  (`ARG` mais o `-ldflags`), `.github/workflows/ci.yml` (`build-args`) e o
  `GET /api/version`, que os dois deploys usam como prova de que a imagem certa
  subiu.
- Alvo novo ou renomeado no `Makefile`: o comentário `## alvo:` acima dele (é o
  que o `make help` lê) e a tabela de alvos da seção "Build" do `README.md`.
- Variável nova que o `Makefile` espera da máquina de quem desenvolve:
  `local.mk.example`.
- Versão de Go ou de Node: `go.mod`, `Dockerfile` (tags `golang:` e `node:`),
  `.github/workflows/ci.yml` (`setup-go` e `setup-node`) e a tabela
  "Tecnologias" do `README.md`.
- Arquivo ou diretório novo na raiz: `.gitignore` se for local, `.dockerignore`
  se não deve entrar na imagem. Os dois listam quase a mesma coisa por motivos
  diferentes, então mexer num pede olhar o outro.

### Docker e caminhos de instalação

- Volume, porta, variável de ambiente ou nome de serviço no
  `docker-compose.yml`: `README.md` - "Subir o dwnvr" e "Instalação definitiva"
  repetem os três volumes e as portas -, `docs/operacao.md` e o comentário de
  `go2rtc.url` no `dwnvr.example.yaml`, que só funciona porque o serviço se
  chama `go2rtc`.
- Caminho de dentro do container (`/etc/dwnvr`, `/storage`): `Dockerfile`
  (`VOLUME`, `CMD` e `HEALTHCHECK`), `docker-compose.yml`, o `storage.root` do
  `dwnvr.example.yaml` e as duas seções de instalação do `README.md`.
- Flag em `cmd/dwnvr/main.go`: o `CMD` e o `HEALTHCHECK` do `Dockerfile` passam
  `-config` e `-healthcheck`, e o `docs/operacao.md` mostra o comando inteiro.

### Documentação

- Documento novo em `docs/`: o índice `docs/README.md` e, se for para operar ou
  entender, a tabela "Documentação" do `README.md`. O `docs/TODO/` não tem
  índice por arquivo - só o diretório é linkado -, então criar um TODO não pede
  atualização nenhuma.
- Renomear ou mover um `.md`: todos os links relativos que apontam para ele.
- Renomear ou mover arquivo de código: o `README.md` e os documentos de `docs/`
  citam arquivos `.go` e `.svelte` pelo caminho, e a árvore "Estrutura do
  projeto" lista os de `internal/api/` e `internal/fmp4/` um a um.

## Varredura final

A lista acima cobre o previsto. Estes dois comandos pegam o resto, e valem como
último passo antes do commit:

```sh
# Renomeou ou moveu algo? o nome antigo não pode ter sobrado em lugar nenhum.
git grep -n "nome-antigo"

# Nenhum link relativo entre .md pode apontar para arquivo inexistente. Este
# arquivo fica de fora da lista de propósito: ele não tem link relativo nenhum,
# e o grep encontraria a própria linha abaixo.
for f in README.md docs/*.md docs/TODO/*.md web/README.md web/src/vendor/README.md; do
  d=$(dirname "$f")
  grep -o '](\([^)#]*\))' "$f" | sed 's/](//;s/)$//' | while read -r l; do
    case "$l" in http*|"") continue;; esac
    [ -e "$d/$l" ] || echo "QUEBRADO: $f -> $l"
  done
done
```
