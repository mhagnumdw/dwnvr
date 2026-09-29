# TODO - segurança do repositório no GitHub

Levantado em 27/09/2026, na revisão do lint com pre-commit
([`TODO_lint-com-pre-commit.md`](TODO_lint-com-pre-commit.md)). O estado de
cada item foi lido na API do GitHub nesse dia, e reavaliado em 28/09 contra o
repositório e a documentação do Dependabot.

O lint do commit já cobre o que dá para conferir sem rede, no commit. O que sobra
de segurança precisa de rede ou de uma base de vulnerabilidades atualizada, e
mora no GitHub: quase tudo é um clique no Settings, sem código para manter.

O que já foi feito sai daqui e vai para o [`github.md`](../github.md), com o
comando que liga.

## Andamento

- [x] Dependabot alerts e security updates (28/09)
- [x] Aviso visível no PR do Dependabot em `web/` (28/09)
- [x] CodeQL default setup (28/09)
  - [x] olhar se o `internal/api/dist` dá ruído na primeira análise (não deu)
  - [x] triagem dos 18 achados de Go da primeira análise (28/09)
    - [x] 15 dos 16 `go/path-injection`, com a trava no `newCamera` (28/09)
    - [x] o `go/path-injection` que sobrou, do `g` do init (28/09)
    - [x] os 2 `go/cookie-secure-not-set`, com o `Secure` atrás de proxy TLS (28/09)
- [x] Private vulnerability reporting, com `SECURITY.md` (28/09)
- [x] Ruleset na `main` bloqueando force push e deleção (28/09)
- [x] `dependabot.yml` (29/09)
  - [x] arquivo escrito e validado pelo schema e pelo zizmor
  - [x] push e primeiro run: PRs #8 (npm) e #9 (docker) abertos
  - [x] `docker-compose` não lê a tag do go2rtc: entrada tirada
  - [x] `pre-commit` aceita o config do prek; o lychee derruba o run e foi
    ignorado
  - [x] prefixo `build(deps):` e `build(deps-dev):` nos PRs
  - [x] TypeScript 7 barrado pela CI no PR #8: major ignorado
  - [ ] o run seguinte sem erro, com o badge verde
- [x] Build da imagem, sem push, no PR (29/09)
  - [x] falhou nos dois jobs no PR #9 (`metadata-action` com o HEAD solto) e
    foi corrigido em `669585f`
  - [ ] o PR #9, depois do rebase, construindo as duas imagens
- [x] govulncheck semanal no binário publicado (29/09): o primeiro run, pelo
  `workflow_dispatch`, deu limpo na `latest`
- [ ] Alerta do `torch` (PR #6): decidir
- [ ] Decidir de novo o pin por SHA (depois do `dependabot.yml`)

Em 27/09, nenhuma vulnerabilidade conhecida: govulncheck, `npm audit` e
`pip-audit` no `dwnvr-detect/requirements.txt` deram 0. Em 28/09, logo ao
ligar, o Dependabot deu 0 alerts, mas minutos depois abriu 1, o do `torch`,
num arquivo que não tinha sido auditado (item próprio abaixo).

## Já estava ligado

Lido em 28/09, e registrado no [`github.md`](../github.md) para não ser
reavaliado: secret scanning e push protection ligados, e o token dos workflows
só com leitura por padrão, sem poder aprovar PR.

## Cada item

### Triagem dos 18 achados de Go do CodeQL

A primeira análise, em 28/09, deu 0 achados em actions, javascript-typescript
e python. O `internal/api/dist`, JavaScript minificado e versionado, não deu
ruído. Em Go foram 18 achados, todos abertos na aba Security:

- **2 `go/cookie-secure-not-set`** (`internal/api/auth.go`, linhas 83 e 98):
  o cookie de sessão sem `Secure`. Era de propósito, porque o dwnvr roda em
  HTTP na LAN, onde um cookie `Secure` seria descartado. Mas atrás de um proxy
  TLS o cookie podia vazar numa requisição em HTTP para o mesmo endereço. Agora
  ele sai `Secure` quando o proxy manda `X-Forwarded-Proto: https`, o que o
  `tailscale serve` e o Caddy fazem, e em HTTP na LAN continua como era. O
  gosec tinha achado o mesmo.
- **16 `go/path-injection`** (`internal/store/store.go`, 10;
  `internal/api/recordings.go`, 5; `internal/api/deteccoes.go`, 1): parâmetro
  da requisição chegando em caminho de arquivo. Todos lidos em 28/09, e nenhum
  explorável. O ID de câmera na URL só passa se for de câmera cadastrada
  (`knownCamera`) ou, na remoção de gravação órfã, se for um nome saído do
  `ReadDir`; o do cadastro passa pelo `ValidateCameraID`; `g` só se for
  hexadecimal (`validGen`); e `t` passa pelo `ParseInt`. O CodeQL não
  reconhece essas validações como sanitizer.

  Mas o store aceitava qualquer ID, e com `""`, `"."` ou `".."` o `Purge`
  apagaria o storage inteiro, ou o que há acima dele. A trava foi para o
  `newCamera`, o único ponto em que o ID vira caminho, com o
  `filepath.IsLocal`, que o CodeQL reconhece. Rodado local, o CodeQL 2.27.1
  reproduziu os 16 achados e, com a trava, deixou só o do `g`, no
  `handleInit`. Esse fechou com o `validGen` reescrito como regexp, a forma
  que o CodeQL reconhece, aceitando exatamente o que o loop aceitava.

Os 18 fecharam com correção no código, e nenhum precisou ser dispensado como
falso positivo. Cada correção foi conferida antes com o CodeQL 2.27.1 rodado
local, na mesma suíte do default setup.

### `dependabot.yml`

Um PR por mês, agrupado por ecossistema, para subir versão, com `cooldown` de
7 dias (o zizmor exige: release comprometida costuma ser retirada antes
disso). Por ecossistema:

- **`github-actions`**: tudo.
- **`gomod`**: tudo. Não mexe na versão do Go, que está em vários lugares
  (a linha "versão de Go ou de Node" das Repercussões do `AGENTS.md`) e sobe
  à mão.
- **`npm`** (em `/web`): tudo menos o major do TypeScript, que o svelte-check
  4.7.6 ainda não aceita (o PR #8 falhou no `npm ci`). O PR que
  muda o bundle precisa do build refeito, e o `ci.yml` aponta para o
  [`github.md`](../github.md#pr-do-dependabot-em-web) quando falta.
- **`pip`** (em `/dwnvr-detect`): sem atualização de versão
  (`open-pull-requests-limit: 0`, que mantém o security update). As versões do
  `requirements.in` são as que exportaram o `.onnx` e mediram a entrega, e
  trocar uma exige conferir de novo; um PR mensal delas contradiz o arquivo. E
  o Dependabot troca a linha do `requirements.txt` em vez de recompilar, o que
  quebra um arquivo feito pelo `uv pip compile --universal` com hash. O PR de
  segurança serve de aviso, e o que fazer está no
  [`github.md`](../github.md#pr-do-dependabot-em-dwnvr-detect).
- **`docker`** (os dois Dockerfiles): o `python:3.12.12-slim-trixie` só no
  patch (`ignore` de minor e major), porque o `requirements.txt` é compilado
  para o 3.12; sem isso, fica velho em silêncio. O `golang:1.27-alpine` e o
  `node:24-alpine` também com `ignore` de minor e major: a tag deles já flutua
  no patch, e trocar de versão é trocar nos outros lugares junto. O
  `ghcr.io/astral-sh/uv`, tudo.
- **`docker-compose`**: tirado depois do primeiro run, em 29/09. O
  Dependabot não lê imagem com `${VAR:-...}` na tag: só enxergou as `:dev` do
  `docker-compose.build.yml`, sem erro. O go2rtc sobe à mão.
- **`pre-commit`**: os `rev` do `.pre-commit-config.yaml`. O parser aceitou o
  config do prek, que o próprio pre-commit recusa. O lychee fica de fora: o
  Dependabot só tira o `v` do começo do `rev` (`current_version`, no
  `update_checker.rb` do pre-commit), e com `lychee-v0.24.2` compara a versão
  nova com nil e derruba o run. Congelar por SHA contornaria, mas o PR ainda
  enganaria: o lychee que roda é a imagem do `entry`, e o `rev` só traz a
  definição do hook, que no `lychee-v0.24.2` ainda aponta a imagem 0.23.0.

O Dependabot só dá security update para `github-actions`, `gomod`, `npm` e
`pip`. Para `docker`, `docker-compose` e `pre-commit` ele só sobe versão, e
depende deste arquivo.

A mensagem do commit vai com `commit-message: prefix: build` e
`include: scope`, que dá `build(deps): ...`. Sem isso, o "Bump ..." padrão cai
no grupo Other das notas da release (o `cliff.toml` agrupa por prefixo). O
Dependabot tenta descobrir sozinho se o repositório usa Conventional Commits;
o primeiro PR de segurança mostra se descobriu.

### Alerta do `torch`

Aberto em 28/09 pelo Dependabot, que abriu junto o PR #6 (`torch` 2.11.0 para
2.13.0): GHSA-rrmf-rvhw-rf47, severidade baixa, corrupção de memória no
`torch.jit.script`, que pede ataque local. Está no
`dwnvr-detect/modelo/requirements.txt`, que não entra na imagem: ele guarda as
versões que geraram o `.onnx` publicado, para gerar o modelo de novo. Subir o
pin muda o que o arquivo diz.

Proposta: fechar o alerta como `tolerable_risk`, com esse motivo, e fechar o
PR. A alternativa é mergear o PR e trocar o comentário do arquivo, que deixa
de registrar a versão da exportação.

### Pin por SHA, depois do `dependabot.yml`

Hoje o `.github/zizmor.yml` aceita a tag de major nas actions `actions/*` e
`docker/*`. O padrão do próprio zizmor só libera `actions/*`, `github/*` e
`dependabot/*`, e as actions da Docker são as que recebem o token com
`packages: write`, no `imagens.yml`. O mesmo vale para os `rev:` do
`.pre-commit-config.yaml`, que são tags de repositório de terceiro rodando na
máquina de quem commita (`prek update --freeze` os troca por SHA).

O pin por SHA foi recusado na Etapa 12 (o zizmor) do lint porque o SHA não diz
a versão. Com o Dependabot, a versão fica num comentário do lado
(`@<sha> # v5.1.0`), e é ele quem atualiza os dois juntos. Vale decidir de
novo depois que o `dependabot.yml` entrar.

Se entrar, o GitHub tem a trava: a opção `sha_pinning_required` do
repositório (hoje `false`) recusa rodar workflow com action fora de SHA. Ela
vale para todas, inclusive as `actions/*`.

## Também medido, e fora por enquanto

- **gosec no golangci-lint.** Com o G104, o G115 e os G30x desligados, 3
  achados no código de produção, todos falso positivo: o cookie de sessão sem
  `Secure` (de propósito, HTTP na LAN, com comentário), o do logout sem
  `SameSite`, e um path traversal que o `validGen` já impede. Serviria de
  guarda para código novo (`InsecureSkipVerify`, `exec` com entrada externa,
  `http.Server` sem timeout). O CodeQL cobre boa parte disso.
- **Scan das imagens publicadas** (Trivy ou Grype): a do dwnvr é `scratch`
  com um binário estático, e o que ele acharia o govulncheck acha; a do
  `dwnvr-detect` tem a base Debian. Depois do Dependabot, que já sobe a base.
- **`dwnvr-detect` com `pyproject.toml` e `uv.lock`.** O ecossistema `uv` do
  Dependabot recompila o lock, em vez de trocar linha, e daria ao `pip` um PR
  que funciona. Muda como o detector de objetos é empacotado, para resolver um
  PR que hoje se resolve com um comando.
- **Renovate no lugar do Dependabot**, avaliado em 28/09 e deixado de lado
  por enquanto. Ele faria melhor quatro coisas daqui: um texto fixo no corpo
  do PR por pacote (`prBodyNotes`), recompilar o `requirements.txt` do
  `uv pip compile`, subir a versão do Go em todos os lugares num PR só e ler
  qualquer formato de tag por regex manager. Nenhum dos dois refaz o
  `internal/api/dist`: o app hospedado do Renovate não roda comando depois do
  update. Custa um app de terceiro com escrita no repositório, inclusive nos
  workflows, e depende dos Dependabot alerts do mesmo jeito. Voltar a olhar
  se o primeiro run do `dependabot.yml` não ler a tag do go2rtc no compose ou
  recusar o config do prek; a troca é apagar um arquivo e criar outro. Em
  29/09, o compose não leu, e o config do prek passou. O Renovate também
  leria a tag do lychee no `entry`, por regex.

## Vale a pena agora?

Quase tudo já foi. Falta decidir o alerta do `torch` e o pin por SHA, que já
pode ser decidido: o Dependabot está no ar.
