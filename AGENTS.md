# AGENTS.md

## O que esse projeto é

NVR de gravação contínua **projetado com foco em hardware extremamente
limitado**, como exemplo o Orange Pi Zero 3. Ele não é feito *para* um hardware específico. É focado em exterma performance, baixíssimo consumo de CPU e memória, tempo de resposta ultra rápido, uma UI super rápida, leve, reativa e responsiva com excelente usabilidade para mobile (browser) e desktop (browser).

## Escrita

- Use `-` (hífen) no lugar do travessão (em dash, U+2014), em qualquer texto:
  código, comentários, documentação, mensagens de commit e respostas no chat.
  A regra vale inclusive ao reescrever uma linha que já tinha o caractere.

## Git

- A `main` não aceita commit de merge. Para integrar, use rebase
  (`git pull --rebase`) ou fast-forward; nunca `git merge` que gere merge commit.
- O body do commit tem duas partes, nesta ordem, separadas pela linha `---`:
  1. Para o usuário final, possivelmente leigo: o que mudou na prática, em
     linguagem do dia a dia, sem nome de arquivo, função ou termo técnico.
     Obrigatória.
  2. Depois do `---`, para pessoas técnicas e agentes de IA: arquivos, decisões
     de implementação, o que ficou de fora. Opcional; se não houver o que dizer,
     omita a parte 2 e o `---` junto.

  ```
  feat(web): baixa a imagem do quadro mostrado

  Agora dá para salvar como imagem o quadro que está na tela, sem
  precisar exportar o vídeo inteiro.

  ---

  `Player.svelte` desenha o `<video>` num canvas e usa `toBlob`.
  O nome do arquivo sai de `mediaURL`.
  ```

## Repercussões

Um mesmo fato vive em vários arquivos aqui. Antes de encerrar a tarefa, ache na
esquerda o que você tocou e atualize tudo na direita. `§` = seção do arquivo.

| Mexeu em | Confira |
|---|---|
| `web/src/`, `web/index.html`, `web/public/` | `make web`, depois commitar `internal/api/dist/` inteiro, assets antigos incluídos (a CI confere) |
| arquivo em `web/src/{lib,routes,components,vendor}` | §Estrutura do `web/README.md` |
| rota em `internal/api/server.go`, inclusive entrar/sair do `requireAuth` | `docs/api.md` · `web/src/lib/api.js` (`api` p/ JSON, `mediaURL` p/ URL montada) |
| formato do JSON de resposta ou de erro | a tela em `web/src/routes/` |
| `.go` novo em `internal/api/` | árvore §Estrutura do projeto do `README.md` |
| campo em `internal/config/config.go` | `dwnvr.example.yaml` (com comentário) · `docs/configuracao.md` · §Configuração do `README.md` |
| default em `config.defaults()` | o número está repetido à mão em `dwnvr.example.yaml` · `docs/configuracao.md` · `web/src/routes/Cameras.svelte` (`audio`, `quotaMB`, `segmentSeconds`, `maxDays`) |
| campo em `config.Camera` | `config.Resolve` · validação em `internal/api/cameras.go` · `Cameras.svelte` · §Política por câmera do `docs/configuracao.md` |
| limite em `internal/api/cameras.go` (`minQuotaMB`, faixas) | `min`/`max`/`step` do input em `Cameras.svelte` - divergem hoje: `docs/TODO/TODO_limites-numericos-so-valem-na-api.md` |
| caminho ou `DayLayout` em `internal/store/store.go` | `docs/arquitetura.md` · `docs/operacao.md` · §Conferir por fora da interface do `README.md` |
| campo em `store.Entry` | índice append-only: a leitura tolera zero em linha antiga · `internal/retention/retention.go` · `internal/api/recordings.go` |
| variável de `internal/buildinfo` | `Makefile` (`LDFLAGS`, `BUILDARGS`) · `Dockerfile` (`ARG`, `-ldflags`) · `.github/workflows/ci.yml` (`build-args`) · `GET /api/version` |
| alvo ou variável no `Makefile` | comentário `## alvo:` (o `make help` lê) · §Build do `README.md` · `local.mk.example` |
| versão de Go ou de Node | `go.mod` · `Dockerfile` (`golang:`/`node:`) · `ci.yml` (`setup-go`/`setup-node`) · §Tecnologias do `README.md` |
| arquivo ou diretório novo na raiz | `.gitignore` se for local. O `.dockerignore` é allowlist invertida (`**` + `!`): o que o build precisar exige um `!` explícito, senão some do contexto |
| volume, porta, env ou serviço no `docker-compose.yml` | §Subir o dwnvr e §Instalação definitiva do `README.md` · `docs/operacao.md` · `go2rtc.url` do `dwnvr.example.yaml` (depende do nome do serviço) |
| caminho interno (`/etc/dwnvr`, `/storage`) | `Dockerfile` (`VOLUME`, `CMD`, `HEALTHCHECK`) · `docker-compose.yml` · `storage.root` do `dwnvr.example.yaml` · `README.md` |
| flag em `cmd/dwnvr/main.go` | `CMD` e `HEALTHCHECK` do `Dockerfile` (`-config`, `-healthcheck`) · `docs/operacao.md` |
| documento novo em `docs/` | índice `docs/README.md` · §Documentação do `README.md`. `docs/TODO/` não tem índice por arquivo |
| renomear ou mover arquivo | links relativos nos `.md` · `README.md` e `docs/` citam `.go`/`.svelte` por caminho · árvore §Estrutura do projeto |

## Varredura final

A tabela cobre o previsto; isto pega o resto. Antes do commit:

```sh
git grep -n "nome-antigo"   # renomeou algo? o nome velho não pode ter sobrado

# Link relativo apontando para arquivo inexistente. O AGENTS.md fica de fora:
# não tem link nenhum, e casaria com a própria linha abaixo.
git ls-files '*.md' | grep -v AGENTS.md | while read -r f; do
  grep -o '](\([^)#]*\))' "$f" | sed 's/](//;s/)$//' | grep -v '^http' |
    while read -r l; do
      [ -e "$(dirname "$f")/$l" ] || echo "QUEBRADO: $f -> $l"
    done
done
```
