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
- [ ] Aviso visível no PR do Dependabot em `web/`
- [ ] CodeQL default setup
- [ ] Private vulnerability reporting, com `SECURITY.md`
- [ ] Ruleset na `main` bloqueando force push e deleção
- [ ] `dependabot.yml`
  - [ ] conferir se o `docker-compose` lê a tag do go2rtc
  - [ ] conferir se o `pre-commit` aceita o config do prek
  - [ ] conferir o prefixo do commit no primeiro PR
- [ ] Build da imagem, sem push, no PR que mexe em Dockerfile
- [ ] govulncheck semanal no binário publicado
- [ ] Decidir de novo o pin por SHA (depois do `dependabot.yml`)

Em 27/09, nenhuma vulnerabilidade conhecida: govulncheck, `npm audit` e
`pip-audit` no `dwnvr-detect/requirements.txt` deram 0. Em 28/09, ao ligar, o
Dependabot também deu 0 alerts. Os itens são para o dia em que aparecer uma, e
não para uma que já exista.

## Já estava ligado

Lido em 28/09, e registrado no [`github.md`](../github.md) para não ser
reavaliado: secret scanning e push protection ligados, e o token dos workflows
só com leitura por padrão, sem poder aprovar PR.

## Cada item

### Aviso visível no PR do Dependabot em `web/`

Já vale com os security updates ligados. O build da interface é versionado em
`internal/api/dist`, e o Dependabot não o refaz: o PR que muda uma dependência
do bundle falha no passo "interface está atualizada?" do `ci.yml`.

O Dependabot não comenta no PR nem abre conversa: do PR dele só se configuram
label, assignee e mensagem do commit. Uma conversa que segura o merge, como a
thread do GitLab, só existe no GitHub dentro da regra "exigir PR" de um ruleset,
que acabaria com o commit direto na `main`. Um workflow que comenta nos PRs do
Dependabot daria, mas é mais um workflow com permissão de escrita para dizer o
que o check já diz.

Proposta: o lugar visível é o próprio check que falha. O passo troca o `echo`
por uma anotação `::error title=...::`, que aparece no resumo do PR, e aponta
para o [`github.md`](../github.md#pr-do-dependabot-em-web), onde estão os
comandos.

### CodeQL default setup

Settings, na seção de segurança, em Code scanning. A API já detecta as
linguagens: actions, go, javascript-typescript e python. Análise de fluxo de dado
(entrada da requisição chegando num caminho de arquivo, num comando, numa
URL), que os linters do commit não fazem. O resultado vai para a aba
Security, e não segura nada.

O build de Go funciona sem passo extra, porque o `internal/api/dist` que o
`go:embed` pede está versionado. O mesmo `dist` é JavaScript minificado, e o
CodeQL vai analisá-lo junto com o `web/src`: olhar a primeira análise. Se der
ruído, o default setup não exclui caminho, e a saída é o advanced setup (um
workflow com `paths-ignore`).

### Private vulnerability reporting

Settings, na seção de segurança. Dá a quem achar uma falha um canal privado
para contar, em vez de uma issue pública. Junto, um `SECURITY.md` curto
dizendo que o canal é esse.

### Ruleset na `main`

Settings > Rules. Bloquear force push e deleção da `main`, sem exigir PR nem
check: o commit direto continua. Protege o histórico publicado de um
`push --force` por engano.

### `dependabot.yml`

Um PR por mês, agrupado por ecossistema, para subir versão. Por ecossistema:

- **`github-actions`**: tudo.
- **`gomod`**: tudo. Não mexe na versão do Go, que está em quatro lugares
  (`go.mod`, `golang:1.27-alpine` no `Dockerfile`, `go-version` no `ci.yml` e
  no `lint.yml`) e sobe à mão.
- **`npm`** (em `/web`): tudo, incluindo o svelte-check e o ESLint. O PR que
  muda o bundle precisa do build refeito, como no item do aviso.
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
- **`docker-compose`**: o go2rtc. A tag é `${GO2RTC_VERSION:-1.9.14}`, e falta
  conferir se o Dependabot lê a interpolação. O PR dele é só proposta: a
  versão nova só entra depois de testada, como pede o `AGENTS.md`.
- **`pre-commit`**: os `rev` do `.pre-commit-config.yaml`. O arquivo é só do
  prek, e o próprio pre-commit o recusa (o `default_language_version`); falta
  conferir se o parser do Dependabot também recusa.

O Dependabot só dá security update para `github-actions`, `gomod`, `npm` e
`pip`. Para `docker`, `docker-compose` e `pre-commit` ele só sobe versão, e
depende deste arquivo.

A mensagem do commit vai com `commit-message: prefix: build` e
`include: scope`, que dá `build(deps): ...`. Sem isso, o "Bump ..." padrão cai
no grupo Other das notas da release (o `cliff.toml` agrupa por prefixo). O
Dependabot tenta descobrir sozinho se o repositório usa Conventional Commits;
o primeiro PR de segurança mostra se descobriu.

### Build da imagem, sem push, no PR que mexe em Dockerfile

O `imagens` do `ci.yml` não roda em PR, para não publicar. Então um PR do
Dependabot que sobe a imagem base passa na CI sem que a imagem tenha sido
construída, e a quebra só aparece no push na `main`. Um build sem push no PR
que toca `Dockerfile`, `dwnvr-detect/` ou o `imagens.yml` fecha esse buraco.

### govulncheck semanal no binário publicado

Um workflow com `schedule` e `workflow_dispatch`. Ele só acusa a
vulnerabilidade cujo código o dwnvr chama, então quase não tem ruído, e olha
também a stdlib do Go que compilou: o Dependabot olha o `go.mod`, e não a
versão do Go.

Rodar no fonte (`govulncheck ./...`) com o `setup-go` na `1.27` analisa o
patch mais novo do dia, e daria limpo mesmo quando o binário da release foi
compilado com um patch mais velho que tem a falha. O que roda nas casas é o
binário da última imagem publicada: ele é que vai para o
`govulncheck -mode=binary`. Quando acusar algo da stdlib, a correção é uma
release nova, porque a tag `golang:1.27-alpine` flutua sozinha e o patch só
entra num build novo.

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

## Vale a pena agora?

Os cliques (CodeQL, private reporting e ruleset), sim: quase nada para manter.
O aviso no PR é uma linha no `ci.yml`. O `dependabot.yml`, o build no PR e o
govulncheck são uma etapa cada, e o pin por SHA vem depois do primeiro.
