# AGENTS.md

## O que esse projeto é

NVR de gravação contínua **projetado para hardware extremamente limitado**
(como o Orange Pi Zero 3), mas não feito *para* um hardware específico. Foco em
performance, baixíssimo consumo de CPU e memória, resposta ultra rápida e uma UI
leve, reativa e responsiva, com ótima usabilidade no mobile e no desktop
(browser).

## Princípios

- **Custo zero desligado.** Feature opcional não cobra nada de quem não a liga:
  sem requisição, goroutine, buffer, arquivo ou diretório criado à toa, e sem
  bytes no bundle (carregue com `import()`). Procure esse custo de propósito.
- **Request nunca espera disco.** Nada de fsync ou escrita no caminho de uma
  requisição HTTP, nem lock que ela lê segurado durante a escrita. O estado que
  muda a cada request fica em memória, e um timer grava em segundo plano (e no
  desligamento). O disco é o mesmo onde as câmeras gravam.
- **Config por câmera, não premissa.** Resolução, cota, áudio e afins são
  decisão de deployment: o dwnvr roda em câmeras e discos que ninguém aqui
  conhece. Pergunte "isso deveria ser configurável por câmera?"; o número de uma
  instalação serve de exemplo de validação, nunca de default.
- **Sem C.** Nada de cgo nem de dependência com `import "C"` ou arquivos
  `.c`/`.h` (o binário é estático e a imagem é `FROM scratch`). Subir a versão do
  Go para respeitar isso é aceitável (onde mexer: §Repercussões).
- **Peça trocável é contrato.** Motor, função estatística, detector de objetos e
  afins: `interface` Go nomeada, com o comentário do que ela promete, e um mapa
  nome -> construtor para trocar a implementação mexendo numa linha. Todo número
  ajustável (limiar, tempo, orçamento, mapa de classes) mora num `parametros.go`,
  nunca numa constante solta.
- **JSON legível.** Chave por extenso (`instanteMs`, não `t`), mesmo ficando
  maior. Exceção: o `index/{dia}.ndjson`, abreviado por volume.
- **O termo diz o que é.** "Detector de objetos" (o modelo), nunca "detector"
  solto: confunde com a detecção de movimento. Prefira o verbo do que acontece
  ("descartada", não "poupada"). Não mude o significado de um conceito do
  servidor (fila, funil) para caber num desenho: proponha o desenho que o
  respeita.
- **Achado fora do escopo** não alarga a tarefa nem se perde no chat: vira
  `docs/TODO/TODO_<assunto>.md`, no formato dos que já estão lá (o que foi
  confirmado, prós, contras, como implementar e se vale a pena agora).

## Escrita

- Português do Brasil em tudo (chat, comentário, doc, commit). O jargão técnico
  fica em inglês, como a comunidade dev brasileira fala: timeout, build, deploy,
  stage, registry, mount, wheel. Vale o termo que um dev diria assim numa
  conversa de trabalho; vocabulário de domínio do projeto (quadro, pedaço) e
  verbo comum (rodar, subir) seguem em português.
- Use `-` (hífen) no lugar do travessão (em dash, U+2014), em qualquer texto
  (código, comentários, docs, commits, chat), inclusive ao reescrever uma linha
  que já tinha o caractere.
- O repositório é público: em arquivo, comentário, commit ou PR, nunca o nome de
  máquina, IP, usuário ou caminho da instalação de quem mantém, nem o nome de
  projeto de outra organização. URL de câmera em exemplo ou teste é
  `rtsp://usuario:senha@...`.

## Git

- Só rode `git add`, commit, push ou abra PR quando o mantenedor pedir; "pode
  fazer a mudança" não autoriza o commit. O stage pode ser o ponto de comparação
  de quem revisa: sem pedido, deixe a mudança pronta no working tree (arquivo
  a apagar: `rm`, não `git rm`), mostre o `git diff` e a mensagem proposta, e
  pare. Doc ou TODO que descreve o fato do commit vai junto, e o diff mostrado
  traz o texto inteiro.
- A `main` não aceita commit de merge. Para integrar, use rebase
  (`git pull --rebase`) ou fast-forward; nunca `git merge` que gere merge commit.
- Consertar commit já publicado: na `main`, amend (um commit por mudança, e o
  force push só o mantenedor libera, pois o ruleset o barra); em branch de PR
  que vai a squash, commit novo, sem force.
- Mensagem em conventional commits (`feat(web): ...`; escopo pelo pedaço tocado,
  omitido se a mudança for transversal). Nunca o trailer `Claude-Session:`; o
  `Co-Authored-By` fica. Mensagem longa vai por `git commit -F arquivo`.
- O body do commit tem duas partes, separadas pela linha `---`:
  1. Para o usuário final, possivelmente leigo: o que mudou na prática, em
     linguagem do dia a dia, sem nome de arquivo, função ou termo técnico.
     Obrigatória.
  2. Para pessoas técnicas e agentes de IA: arquivos, decisões de implementação,
     o que ficou de fora. Opcional; sem o que dizer, omita a parte 2 e o `---`.

  ```text
  feat(web): baixa a imagem do quadro mostrado

  Agora dá para salvar como imagem o quadro que está na tela, sem
  precisar exportar o vídeo inteiro.

  ---

  `Player.svelte` desenha o `<video>` num canvas e usa `toBlob`.
  O nome do arquivo sai de `mediaURL`.
  ```

- Mudança incompatível (quem só atualiza, com `git pull` e `docker compose up`,
  perde algo que funcionava ou precisa agir): `!` no tipo (`fix(compose)!:`) e o
  rodapé `BREAKING CHANGE:`, que vira a seção "Pede ação ao atualizar" das notas
  da release (`cliff.toml`).
  - O rodapé diz o que fazer (o que mudou, o assunto já diz), escrito para o
    usuário final, num parágrafo só. Fica no fim da mensagem, depois da parte 2,
    num parágrafo próprio e separado do `Co-Authored-By` por uma linha em
    branco: no mesmo parágrafo, o git deixa de reconhecer o `Co-Authored-By`
    (`BREAKING CHANGE` tem espaço, então para o git não é trailer). Confira com
    `git interpret-trailers --parse --no-divider`, que tem de listá-lo.

    ```text
    (parte 2)

    BREAKING CHANGE: Depois de atualizar, todos os aparelhos voltam uma vez para a tela de login. Tenha a senha à mão.

    Co-Authored-By: ...
    ```

  - Ao propor um commit que mexe em algum destes itens, diga no chat se ele é
    incompatível e por quê, mesmo quando for "não": campo ou default em
    `internal/config/config.go` (renomear, remover, mudar o default); porta,
    volume, env ou serviço no `docker-compose.yml`; caminho interno ou
    `DayLayout` (a gravação antiga continua legível?); flag em
    `cmd/dwnvr/main.go`; rota ou JSON documentado em `docs/api.md`.

## Interface

- Ícone da interface é `<Icone nome="…" tamanho={…} />`, e o desenho mora em
  `web/src/lib/iconesUi.ts`. Ícone novo entra lá, num viewBox 24×24. Nada de
  `<svg>` inline numa tela nem de caractere (⋮ ✕ ▶) no lugar de ícone: o
  caractere muda de forma a cada sistema.
- Fora do mapa ficam só os ícones de detecção em canvas (`lib/icones.ts`) e a
  marca do GitHub (`MarcaGitHub.svelte`).

## Lint

- Uma vez por clone: `prek install` (o prek se instala com
  `uv tool install prek`). A cada commit os hooks do `.pre-commit-config.yaml`
  conferem os arquivos e corrigem o que dá; se um hook corrigir algo, o commit é
  interrompido: confira, dê `git add` e commite de novo.
- Antes de propor um commit: `prek run --all-files`. A CI
  (`.github/workflows/lint.yml`) roda o mesmo arquivo, sem corrigir nada.
- O hook `betterleaks` só olha o que está no stage, então o `--all-files` verde
  não garante o commit. Em teste, nada de literal com usuário e senha: monte a
  URL com `url.UserPassword`, ponha a senha numa variável e use os literais que o
  `.betterleaks.toml` libera (`senha`, `troque-me`; `admin` em `_test.go`).
- Nunca `git commit --no-verify`. Regra de lint não se desliga para passar:
  corrija; se não houver como, desligue só naquela linha, com o motivo nela.

## Comandos

- `make test` (`go test -race`, que precisa de compilador C só para o detector
  de corrida), `make web` (refaz a interface) e `make check` (o que a CI roda:
  test + prek). `make help` lista o resto.
- Dev server: `cd web && DWNVR_API=http://servidor:8080 npm run dev` (sem a
  variável, aponta para `localhost:8080`; sem backend, a tela fica vazia).
- Mudança visual: mostre-a renderizada (print ou endereço do dev server) antes
  de commitar. Status 200 e tamanho certo não provam que o asset desenha.

## Repercussões

Um mesmo fato vive em vários arquivos aqui. Antes de encerrar a tarefa, ache na
esquerda o que você tocou e atualize tudo na direita. `§` = seção do arquivo.

| Mexeu em | Confira |
| --- | --- |
| `web/src/`, `web/index.html`, `web/public/` | `make web`, depois commitar `internal/api/dist/` inteiro, assets antigos incluídos (o hook `interface-atualizada` e a CI conferem) |
| arquivo em `web/src/{lib,routes,components,vendor}` | §Estrutura do `web/README.md` |
| rota em `internal/api/server.go`, inclusive entrar/sair do `requireAuth` | `docs/api.md` · `web/src/lib/api.ts` (tipos do JSON no fim; `api` para JSON, `mediaURL` para URL montada) |
| formato do JSON de resposta ou de erro | a tela em `web/src/routes/` |
| `.go` novo em `internal/api/` | árvore §Estrutura do projeto do `README.md` |
| campo em `internal/config/config.go` | `dwnvr.example.yaml` (com comentário) · `docs/configuracao.md` · §Configuração do `README.md` |
| default em `config.defaults()` | o número se repete à mão em `dwnvr.example.yaml` · `docs/configuracao.md` · `web/src/routes/Cameras.svelte` (`audio`, `quotaMB`, `segmentSeconds`, `maxDays`, e no fallback do `padrao`: `detect`, `detectMecanismo`, `detectSensibilidade`) |
| campo em `config.Camera` | `config.Resolve` · `config.ConfereCamera` (a régua da API e do boot) · `Cameras.svelte` · §Política por câmera do `docs/configuracao.md` |
| faixa em `internal/config/parametros.go` | campo com input na tela entra em `FaixasDaCamera`, de onde o `Cameras.svelte` lê `min`/`max` · coluna Aceito da §Política por câmera do `docs/configuracao.md` · comentário do campo no `dwnvr.example.yaml` · `faixas` do `docs/api.md` |
| caminho ou `DayLayout` em `internal/store/store.go` | `docs/arquitetura.md` · `docs/operacao.md` · §Conferir por fora da interface do `README.md` |
| campo em `store.Entry` | índice append-only: a leitura tolera zero em linha antiga · `internal/retention/retention.go` · `internal/api/recordings.go` |
| variável de `internal/buildinfo` | `Makefile` (`LDFLAGS`, `BUILDARGS`) · `Dockerfile` (`ARG`, `-ldflags`) · `.github/workflows/imagens.yml` (`build-args`) · `GET /api/version` |
| `image:` do dwnvr ou do dwnvr-detect no `docker-compose.yml` | manter `${DWNVR_VERSION:-vX.Y.Z}` numa linha só: o passo do compose no `.github/workflows/release.yml` reescreve o default e falha se não achar as 2 linhas |
| versão do lychee | `rev` e `entry` (a tag da imagem) do hook `lychee-docker` no `.pre-commit-config.yaml`, juntos: o `rev` só traz a definição do hook, e o Dependabot não sobe nenhum dos dois |
| `image:` do go2rtc no `docker-compose.yml` | manter `${GO2RTC_VERSION:-X.Y.Z}` com uma tag testada, nunca sem tag (sem ela o `--pull always` traz qualquer versão) · §Atualizar do `README.md` · §Trocar de versão do `docs/operacao.md` |
| comando de atualizar | §Atualizar do `README.md` (a página de cada release aponta para ela) · §Trocar de versão do `docs/operacao.md` |
| alvo ou variável no `Makefile` | comentário `## alvo:` (o `make help` lê) · §Build do `README.md` · `local.mk.example` |
| versão de Go ou de Node | `go.mod` · `.tool-versions` (o asdf local) · `Dockerfile` (`golang:`/`node:`) · `ci.yml` e `lint.yml` (`setup-go`/`setup-node`) · §Tecnologias do `README.md` · `default_language_version` do `.pre-commit-config.yaml` (o golangci-lint compilado com o Go velho se recusa a rodar) |
| arquivo ou diretório novo na raiz | `.gitignore` se for local. O `.dockerignore` é allowlist invertida (`**` + `!`): o que o build precisar exige um `!` explícito, senão some do contexto |
| arquivo gerado, que não se edita à mão | `exclude` do `.pre-commit-config.yaml` (hoje `internal/api/dist/` e `CHANGELOG.md`): sem ele, um hook o reescreve e o lint da CI falha |
| volume, porta, env ou serviço no `docker-compose.yml` | §Experimentar em poucos minutos, §Instalar de verdade e §Detecção de movimento e de objetos do `README.md` · `docker-compose.build.yml` (se for serviço com imagem própria) · `docs/operacao.md` · `go2rtc.url` do `dwnvr.example.yaml` (depende do nome do serviço) |
| caminho interno (`/etc/dwnvr`, `/storage`) | `Dockerfile` (`VOLUME`, `CMD`, `HEALTHCHECK`) · `docker-compose.yml` · `storage.root` do `dwnvr.example.yaml` · `README.md` |
| caminho do binário na imagem (`/dwnvr`) | `Dockerfile` (`ENTRYPOINT`, `HEALTHCHECK`) · `docker cp` do `.github/workflows/govulncheck.yml` · §govulncheck semanal do `docs/github.md` |
| flag em `cmd/dwnvr/main.go` | `CMD` e `HEALTHCHECK` do `Dockerfile` (`-config`, `-healthcheck`) · `docs/operacao.md` |
| documento novo em `docs/` | índice `docs/README.md` · §Documentação do `README.md`. `docs/TODO/` não tem índice por arquivo |
| renomear ou mover arquivo | links relativos nos `.md` · `README.md` e `docs/` citam `.go`/`.svelte` por caminho · árvore §Estrutura do projeto |

## Varredura final

A tabela cobre o previsto; isto pega o resto. Antes do commit:

```sh
git grep -n "nome-antigo"   # renomeou algo? o nome velho não pode ter sobrado
```

Confira também que nenhum nome pessoal ou de outra organização entrou (§Escrita).
Link e âncora quebrados no Markdown o lychee acha no commit (§Lint).
