# TODO - segurança do repositório no GitHub

Levantado em 27/09/2026, na revisão do lint com pre-commit
([`TODO_lint-com-pre-commit.md`](TODO_lint-com-pre-commit.md)). O estado de
cada item foi lido na API do GitHub nesse dia.

O pre-commit já cobre o que dá para conferir sem rede, no commit. O que sobra
de segurança precisa de rede ou de uma base de vulnerabilidades atualizada, e
mora no GitHub: quase tudo é um clique no Settings, sem código para manter.

## O que falta

| O quê | Estado hoje | Custo |
| --- | --- | --- |
| Dependabot alerts e security updates | **desligado** | um clique |
| CodeQL default setup (actions, go, js, python) | **not-configured** | um clique, grátis em repositório público |
| Private vulnerability reporting | desligado | um clique |
| `dependabot.yml` (actions, gomod, npm, pip, docker, pre-commit) | não existe | um arquivo; destrava o pin por SHA, abaixo |
| govulncheck semanal | não existe | um job; pega CVE da stdlib do Go, que o Dependabot não vê |
| Ruleset na `main` bloqueando force push e deleção | sem proteção | um clique; não atrapalha o commit direto |

Em 27/09, nenhuma vulnerabilidade conhecida: govulncheck, `npm audit` e
`pip-audit` no `dwnvr-detect/requirements.txt` deram 0. Os itens acima são
para o dia em que aparecer uma, e não para uma que já exista.

## Cada item

### Dependabot alerts e security updates

Settings, na seção de segurança. O alerta aparece quando uma dependência do
`go.mod`, do `web/package-lock.json`, do `requirements.txt` ou dos workflows
ganha uma vulnerabilidade publicada; o security update abre o PR com a
versão corrigida. Não depende do `dependabot.yml`.

### CodeQL default setup

Settings, na seção de segurança, em Code scanning. A API já detecta as
linguagens: actions, go, javascript e python. Análise de fluxo de dado
(entrada da requisição chegando num caminho de arquivo, num comando, numa
URL), que os linters do pre-commit não fazem. O resultado vai para a aba
Security, e não segura nada.

### Private vulnerability reporting

Settings, na seção de segurança. Dá a quem achar uma falha um canal privado
para contar, em vez de uma issue pública. Junto, um `SECURITY.md` curto
dizendo que o canal é esse.

### `dependabot.yml`

Um PR por mês, agrupado por ecossistema, para subir versão: `github-actions`,
`gomod`, `npm` (em `/web`), `pip` (em `/dwnvr-detect`), `docker` (os dois
Dockerfiles) e `pre-commit` (os `rev` do `.pre-commit-config.yaml`). O
svelte-check e o ESLint vêm do `npm` e sobem por ele.

A imagem base do `dwnvr-detect` está fixada no patch
(`python:3.12.12-slim-trixie`), e sem isso fica velha em silêncio.

### Pin por SHA, depois do `dependabot.yml`

Hoje o `.github/zizmor.yml` aceita a tag de major nas actions `actions/*` e
`docker/*`. O padrão do próprio zizmor só libera `actions/*`, `github/*` e
`dependabot/*`, e as actions da Docker são as que recebem o token com
`packages: write`, no `imagens.yml`. O mesmo vale para os `rev:` do
pre-commit, que são tags de repositório de terceiro rodando na máquina de
quem commita (`pre-commit autoupdate --freeze` os troca por SHA).

O pin por SHA foi recusado na Etapa 12 (o zizmor) do lint porque o SHA não diz
a versão. Com o Dependabot, a versão fica num comentário do lado
(`@<sha> # v5.1.0`), e é ele quem atualiza os dois juntos. Vale decidir de
novo depois que o `dependabot.yml` entrar.

### govulncheck semanal

Um workflow com `schedule` e `workflow_dispatch`, rodando
`govulncheck ./...`. Ele só acusa a vulnerabilidade cujo código o dwnvr
chama, então quase não tem ruído, e olha também a stdlib do Go que compila:
o Dependabot olha o `go.mod`, e não a versão do Go.

### Ruleset na `main`

Settings > Rules. Bloquear force push e deleção da `main`, sem exigir PR nem
check: o commit direto continua. Protege o histórico publicado de um
`push --force` por engano.

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

## Vale a pena agora?

Os três cliques (Dependabot alerts, CodeQL e private reporting), sim: quase
nada para manter e o maior ganho da lista. O `dependabot.yml` e o
govulncheck são uma etapa cada, e o pin por SHA vem depois do primeiro.
