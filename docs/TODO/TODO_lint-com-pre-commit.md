# TODO - lint com pre-commit, em etapas

Planejado em 24/09/2026. Os números abaixo foram medidos de novo em
27/09/2026, na `main` em `f4fe3b3`, com cada ferramenta só lendo, sem corrigir
nada.

Uma etapa por vez. Cada uma entra no seu commit (em dois, quando houver
correção mecânica grande), e esse commit marca o checkbox dela e atualiza o
próximo passo, logo abaixo.

## Próximo passo

**Etapa 2 (markdownlint).** Antes, conferir em Actions que o workflow `lint`
ficou verde depois do push da Etapa 1. A Etapa 2 tem dois commits: primeiro só
o `--fix` das tabelas, que vai para o `.git-blame-ignore-revs`; depois a
config, o hook e as correções feitas à mão. Os detalhes estão na seção dela.

## Checklist

- [x] Etapa 0 - este plano
- [x] Etapa 1 - a base: pre-commit-hooks e o `lint.yml`
- [ ] Etapa 2 - markdownlint-cli2
- [ ] Etapa 3 - lychee: link e âncora do Markdown
- [ ] Etapa 4 - as regras de commit do AGENTS.md como hook
- [ ] Etapa 5 - gitleaks, com uma regra para senha de câmera
- [ ] Etapa 6 - golangci-lint, primeira leva
- [ ] Etapa 7 - svelte-check
- [ ] Etapa 8 - ESLint com o plugin do Svelte
- [ ] Etapa 9 - ruff no `dwnvr-detect/`
- [ ] Etapa 10 - yamllint
- [ ] Etapa 11 - hadolint
- [ ] Etapa 12 - actionlint
- [ ] Etapa 13 - golangci-lint, segunda leva
- [ ] Etapa 14 - zizmor
- [ ] Etapa 15 - Prettier (opcional)
- [ ] Etapa 16 - cspell pt-BR (opcional)

## Por que

Qualidade de código, e agente de IA. O que o AGENTS.md pede e uma máquina
consegue conferir vira hook, em vez de depender da memória de quem escreve: o
erro aparece no commit, com arquivo e linha, e o `--fix` resolve sozinho o que
é só forma.

## Como funciona em toda etapa

- **O hook corrige no clone; a CI só avisa.** O `.github/workflows/lint.yml`
  roda o mesmo `.pre-commit-config.yaml`, com `--all-files` e
  `--show-diff-on-failure`. Se um hook mudaria algum arquivo na cópia do
  runner, o job falha e mostra o diff. Nada volta para o repositório.
- **`--fix` só nos `args` do hook**, nunca no arquivo de config da ferramenta,
  que o editor e a CI também leem.
- **Uma versão por ferramenta**: a do `rev` no `.pre-commit-config.yaml`. Sem
  job próprio da ferramenta na CI, que traria a versão embutida na action. O
  `markdownlint-cli2-action` v24.2.0, por exemplo, embute o cli2 0.23.2, e o
  hook já está no 0.23.3.
- **Lint vermelho não segura as imagens.** O `lint.yml` é separado do
  `ci.yml`, e a release só confere o `ci.yml`.
- **Cada etapa zera o seu lint**, sem ignorar achado antigo. Correção
  mecânica grande vai num commit `style:` separado, listado no
  `.git-blame-ignore-revs`.
- **O que a etapa exigir na CI vem junto**: o `setup-go` ou o `setup-node` no
  `lint.yml`. Hook que não faz sentido lá, como o gitleaks, que só olha o
  staged, entra no `SKIP` do job.
- **Versão mais nova antes de implementar**, com
  `gh api repos/<dono>/<repo>/releases/latest --jq .tag_name`. A tabela do fim
  tem as de 27/09/2026.

## As etapas

"Hoje" é o que a ferramenta acha na `main` de 27/09/2026, ou na data indicada.

### Etapa 1 - a base: pre-commit-hooks e o lint.yml

- Os hooks do [pre-commit-hooks](https://github.com/pre-commit/pre-commit-hooks):
  espaço no fim da linha, newline final, LF, BOM, marca de conflito, nome que
  só difere em maiúscula, YAML, JSON e TOML válidos, arquivo novo acima de
  500 KB e chave privada.
- Fica fora de todo hook o que é gerado: o `internal/api/dist/`, que tem 3
  arquivos sem newline final e é comparado pelo `ci.yml` com um build novo, e
  o `CHANGELOG.md`, que termina com uma linha em branco sobrando e é escrito
  pela release.
- O `lint.yml`, com o job `pre-commit`. A versão do pre-commit vem do
  `.tool-versions`.
- O `make check` roda o pre-commit também; README §Lint e AGENTS.md §Lint.
- Hoje: 0 achados.
- Commit: `ci: pre-commit com as checagens básicas de arquivo`.
- Na revisão de 27/09, o `check-added-large-files` ganhou `--enforce-all`: sem
  ele, o hook só olha arquivo novo no staged, e na CI, que não tem staged, não
  conferia nada. O modelo `.onnx` do `dwnvr-detect/`, de 28 MB, versionado de
  propósito, entrou no `exclude` do hook.

### Etapa 2 - markdownlint-cli2

[markdownlint-cli2](https://github.com/DavidAnson/markdownlint-cli2). Hoje: 400
achados em 28 arquivos.

| Regra | Achados | Como sai |
| --- | --- | --- |
| MD060, estilo de tabela | 373 | `--fix`: a linha de separação ganha espaços |
| MD040, bloco de código sem linguagem | 17 | à mão |
| MD051, âncora que não existe | 5 | à mão, ver abaixo |
| MD010, MD012, MD028, MD032 e MD046 | 5 | `--fix` ou à mão |

- Os 5 MD051 são o link `[Podman](#com-podman)` do README, quebrado de
  verdade. O espaço antes do `<!-- omit in toc -->` do título vira hífen, e a
  âncora que o GitHub gera é `#com-podman-` (conferido na página renderizada
  em 24/09). Trocar os links, ou tirar o espaço, se a extensão Markdown All in
  One ainda reconhecer o comentário assim.
- `.markdownlint-cli2.yaml`, lido pelo hook, pela CI e pela extensão do VS
  Code: `default: true`; `MD013: false`, sem limite de linha; `MD024` só entre
  irmãos; `MD033` liberando `details`, `summary` e `b`, que o README usa;
  `MD009` estrito, como o `trailing-whitespace`; `globs: ["**/*.md"]`,
  `gitignore: true` e o `CHANGELOG.md` em `ignores`.
- Hook com `args: [--no-globs, --fix]`. O `--no-globs` faz ele olhar só os
  arquivos do commit, e não o `globs` da config, que é o repositório inteiro.
- [ ] Commit 1, só o `--fix` com a MD060 ligada sozinha:
  `style(docs): normaliza as tabelas do Markdown`.
- [ ] Commit 2, com a config, o hook, as correções à mão e o
  `.git-blame-ignore-revs` apontando para o commit 1:
  `ci: markdownlint no pre-commit`.

### Etapa 3 - lychee: link e âncora do Markdown

[lychee](https://github.com/lycheeverse/lychee). Hoje: os mesmos 5
`#com-podman`, que saem na Etapa 2; os outros 137 links estão certos.

- `lychee.toml`: `offline = true`, conferindo a âncora (`include_fragments`) e
  sem barra de progresso. Offline porque link externo cai por motivo alheio ao
  commit.
- Hook `lychee`, com `types: [markdown]`. Ele baixa o binário sozinho, e o
  `rev` é a tag `lychee-vX`.
- AGENTS.md §Varredura final: sai o script de link quebrado, que pula todo
  link com `#` e por isso não viu o `#com-podman`.
- Commit: `ci: lychee confere link e âncora do Markdown`.

### Etapa 4 - as regras de commit do AGENTS.md como hook

- [conventional-pre-commit](https://github.com/compilerla/conventional-pre-commit)
  no stage `commit-msg`. Os tipos usados desde 09/08 estão todos na lista
  padrão, e o escopo fica livre. O git-cliff da release depende desse formato.
- Hook local `pygrep` para o travessão (U+2014), nos arquivos e na mensagem de
  commit. Hoje: nenhum desde que a regra entrou, em 14/08.
- Hook local de body obrigatório, a parte 1 do AGENTS.md. Hoje: 2 commits sem
  body desde que a regra entrou, em 20/08 (`1728362` e `def9240`). O
  `chore(release): vX.Y.Z` nasce na CI e não passa por hook.
- `default_install_hook_types: [pre-commit, commit-msg]`. Depois do commit,
  cada clone roda `pre-commit install` de novo, para ligar o `commit-msg`.
- Commit: `ci: as regras de commit do AGENTS.md viram hook`.

### Etapa 5 - gitleaks, com uma regra para senha de câmera

- [gitleaks](https://github.com/gitleaks/gitleaks) no commit local. Ele olha o
  staged, então na CI entra no `SKIP`. O push protection do GitHub já está
  ligado no repositório, mas só conhece token de provedor.
- `.gitleaks.toml` com as regras padrão e mais uma para URL `rtsp://` com
  usuário e senha, o vazamento mais provável aqui. Allowlist para os exemplos
  do repositório (`usuario:senha` e `admin:senha`).
- Hoje: 0 achados, nos arquivos e no histórico.
- Verificação: pôr no stage um arquivo com uma URL `rtsp://` com usuário e
  senha inventados; o commit deve parar.
- Commit: `ci: gitleaks barra segredo e senha de câmera no commit`.

### Etapa 6 - golangci-lint, primeira leva

- [golangci-lint](https://github.com/golangci/golangci-lint) v2 com o conjunto
  `standard` (errcheck, govet, ineffassign, staticcheck e unused), os presets
  `std-error-handling` e `common-false-positives`, e os formatters gofmt e
  goimports. Mais um hook local de `go mod tidy`.
- Hook `golangci-lint-full`, e não o `golangci-lint`: esse usa
  `--new-from-rev HEAD`, que na CI não acha nada. Mais o `golangci-lint-fmt` e
  o `golangci-lint-config-verify`. O `lint.yml` ganha o `setup-go`.
- Hoje: 30 achados, todos em `_test.go`. São 28 retornos ignorados (`Write`,
  `WriteString`, `AppendEvento`) e 2 comparações `f(a) != f(a)` (SA4000) que
  parecem testar determinismo de propósito: reescrever com duas variáveis. O
  código de produção está limpo, e os formatters e o `go mod tidy` não mudam
  nada. Com cache, roda em meio segundo.
- Decidir na etapa: o `go vet` e o `gofmt` do `ci.yml` e do `make check`
  ficam redundantes. Tirar de lá faz os dois pararem de segurar as imagens.
- Commit: `ci: golangci-lint no pre-commit`.

### Etapa 7 - svelte-check

- [svelte-check](https://github.com/sveltejs/language-tools): os warnings do
  compilador do Svelte, com acessibilidade e CSS sem uso.
- Hoje: 0 nos 17 componentes.
- Hook local rodando no `web/`. Nas devDependencies, ele traria o
  `typescript` (peer dele) para o `npm ci` do build da imagem; chamado por
  `npx` com a versão fixada, o build não muda. Preferir o `npx`.
- O `lint.yml` ganha o `setup-node` e o `npm ci` do `web/`.
- Commit: `ci: svelte-check no pre-commit`.

### Etapa 8 - ESLint com o plugin do Svelte

- [ESLint](https://github.com/eslint/eslint) com o
  [eslint-plugin-svelte](https://github.com/sveltejs/eslint-plugin-svelte), em
  `web/eslint.config.js`: o `recommended` dos dois e os globals do browser. O
  `src/vendor/` fica de fora, porque é código do go2rtc; o `no-unused-vars`
  ignora nome com `_`, que o `Cameras.svelte` usa para descartar campo; os
  globals do Node valem só no `vite.config.js`.
- Hoje: 21 achados fora do `vendor/`. São 9 `svelte/prefer-svelte-reactivity`
  (Map, Set ou Date mutável fora do estado reativo, que pode ser bug ou falso
  positivo: ver um a um), 5 `svelte/no-useless-mustaches`, 3
  `svelte/no-unused-svelte-ignore`, 2 `svelte/require-each-key` no
  `Health.svelte`, 1 `no-useless-assignment` e 1 `no-empty`.
- Custo: 4 devDependencies, ~130 pacotes no `node_modules`. O bundle e a
  imagem final não mudam, mas o `npm ci` do build da imagem baixa tudo.
- A correção pode mudar o comportamento da tela: ver rodando antes do commit.
- Commit: `ci: ESLint com o plugin do Svelte`.

### Etapa 9 - ruff no dwnvr-detect

- [ruff](https://github.com/astral-sh/ruff), com os hooks `ruff-check`
  (`--fix`) e `ruff-format`.
- Hoje, medido em 24/09 (o `dwnvr-detect/` não mudou desde então): 5 achados
  e 2 arquivos a reformatar. Dois achados são `EXE001`: o `servidor.py` e o
  `exporta.py` têm shebang sem permissão de execução.
- Commit: `ci: ruff no dwnvr-detect`.

### Etapa 10 - yamllint

- [yamllint](https://github.com/adrienverge/yamllint) com `--strict` e um
  `.yamllint.yaml` curto: `extends: default`, `truthy` sem conferir chave (o
  `on:` do GitHub Actions) e `document-start` desligado.
- Hoje: 9 avisos. São 5 comentários com um espaço só antes do `#`, no
  `release.yml`, e 4 linhas longas: 2 no `release.yml`, a chave do cache no
  `lint.yml` e uma de 281 caracteres no `go2rtc.example.yaml`. Decidir na
  etapa se o `line-length` fica.
- Commit: `ci: yamllint`.

### Etapa 11 - hadolint

- [hadolint](https://github.com/hadolint/hadolint), que passa o shellcheck
  nos `RUN`. Hook `hadolint-docker`, com a tag da imagem fixada no `entry`: o
  hook do repositório usa a imagem sem tag. Precisa de Docker local; o runner
  do GitHub tem.
- Hoje, medido em 24/09 (os Dockerfiles não mudaram): 0 nos 2 Dockerfiles.
- Commit: `ci: hadolint nos Dockerfiles`.

### Etapa 12 - actionlint

- [actionlint](https://github.com/rhysd/actionlint): sintaxe e expressões dos
  workflows, com o shellcheck do PATH nos `run:` (o runner do GitHub tem).
- Hoje: 0 nos 4 workflows.
- Commit: `ci: actionlint nos workflows`.

### Etapa 13 - golangci-lint, segunda leva

- Entram modernize, intrange, errorlint, perfsprint, unconvert, noctx,
  bodyclose, nolintlint, copyloopvar e usestdlibvars.
- Ficam fora, com o que acharam em 24/09: gosec (60, a maioria G115,
  conversão de inteiro no parser de fMP4), revive (52, a maioria pedindo
  comentário em exportado), misspell (42, todos falso positivo em português),
  prealloc (16, micro-otimização) e unparam.
- Hoje, no código de produção: 22 achados, a maioria com fix automático. São
  9 modernize (`strings.SplitSeq`, `WaitGroup.Go`, `slices.Contains`), 4
  intrange, 4 perfsprint, 3 errorlint (`==` num erro que pode vir embrulhado),
  1 unconvert e 1 noctx, o `http.Get` do healthcheck em `cmd/dwnvr/main.go`.
  Nos testes, o noctx acha mais 21: decidir se vale lá.
- Commit: `ci: segunda leva do golangci-lint`.

### Etapa 14 - zizmor

- [zizmor](https://github.com/zizmorcore/zizmor): segurança dos workflows,
  que publicam no GHCR e fazem push na `main`.
- Hoje: 29 achados nos 4 workflows. São 21 actions sem SHA
  (`unpinned-uses`), 3 checkouts com a credencial persistida (`artipacked`), 2
  permissões amplas (`excessive-permissions`), 2 `self-repository` e 1
  `superfluous-actions`: o `softprops/action-gh-release` pode virar
  `gh release create`.
- [ ] Commit 1: as actions fixadas por SHA, com a versão num comentário.
- [ ] Commit 2: as outras correções.
- [ ] Commit 3: o hook, do `zizmorcore/zizmor-pre-commit`.

### Etapa 15 - Prettier (opcional)

- [Prettier](https://github.com/prettier/prettier) com o
  [prettier-plugin-svelte](https://github.com/sveltejs/prettier-plugin-svelte):
  um formato só para JS, Svelte e CSS.
- Hoje: reformataria 36 arquivos do `web/`; em 24/09, eram ~4.900 linhas de
  diff.
- Se entrar, o diff vai num `style(web):` separado, listado no
  `.git-blame-ignore-revs`. Decidir vendo o diff.

### Etapa 16 - cspell pt-BR (opcional)

- [cspell](https://github.com/streetsidesoftware/cspell) com o dicionário
  `@cspell/dict-pt-br`, só nos `.md`, ignorando bloco de código e link.
- Hoje, medido em 24/09: 72 termos para o dicionário do projeto, quase todos
  jargão ("remuxar", "reencodar", "commitar"), e 1 erro de verdade: "exterma",
  no AGENTS.md.
- O [typos](https://github.com/crate-ci/typos) foi descartado: deu 1.453
  falsos positivos, porque o dicionário dele é de erro de inglês ("erro" vira
  "error").

## Fora do plano

- **Dependabot** com o ecossistema `pre-commit`, para subir os `rev`: depois
  que as etapas assentarem.
- **govulncheck**: vulnerabilidade nas dependências e na stdlib do Go. Não é
  lint; cabe num job semanal.
- **lychee online**, semanal, abrindo issue: hoje os links externos respondem
  todos, fora o `http://localhost:8080/` do README, que é o endereço do
  próprio dwnvr e ficaria de fora.
- **editorconfig-checker**: 187 achados em 24/09, quase todos falso positivo
  em Markdown, onde a continuação de lista numerada usa 3 espaços.
- **shellcheck e shfmt**: entram com o primeiro `.sh`. Hoje não há nenhum, e
  os `run:` e os `RUN` passam pelo shellcheck nas Etapas 11 e 12.
- **GitHub, Settings > General**: "Allow merge commits" está ligado, e o
  AGENTS.md diz que a `main` não aceita merge commit.

## Versões de 27/09/2026

| Ferramenta | Versão |
| --- | --- |
| pre-commit | 4.6.2 |
| pre-commit-hooks | v6.0.0 |
| markdownlint-cli2 | v0.23.3 |
| lychee | lychee-v0.24.2 |
| conventional-pre-commit | v4.4.0 |
| gitleaks | v8.30.1 |
| golangci-lint | v2.14.0 |
| svelte-check | 4.7.6 |
| eslint e eslint-plugin-svelte | 10.11.0 e 3.23.0 |
| ruff-pre-commit | v0.16.9 |
| yamllint | v1.38.0 |
| hadolint | v2.15.1 |
| actionlint | v1.7.12 |
| zizmor-pre-commit | v1.30.1 |
| prettier e prettier-plugin-svelte | 3.9.9 e 4.1.1 |
| cspell e @cspell/dict-pt-br | 10.3.4 e 2.4.2 |
