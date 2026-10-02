# O repositório no GitHub <!-- omit in toc -->

A configuração do repositório que não mora em arquivo: o que está ligado no
Settings, o comando que liga cada coisa e o que fazer com o que ela produz. O
que ainda falta ligar está em
[`TODO/TODO_seguranca-do-repositorio-no-github.md`](TODO/TODO_seguranca-do-repositorio-no-github.md);
quando um item de lá é feito, ele passa para cá.

Os comandos usam o [`gh`](https://cli.github.com), autenticado com permissão de
admin no repositório. Todos partem de:

```sh
REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)
```

Ligar de novo o que já está ligado não muda nada, então qualquer comando daqui
pode ser rodado outra vez para conferir.

- [Conferir o estado](#conferir-o-estado)
- [Secret scanning e push protection](#secret-scanning-e-push-protection)
- [Token dos workflows só com leitura](#token-dos-workflows-só-com-leitura)
- [Dependabot alerts e security updates](#dependabot-alerts-e-security-updates)
  - [PR do Dependabot em `web/`](#pr-do-dependabot-em-web)
  - [PR do Dependabot em `dwnvr-detect/`](#pr-do-dependabot-em-dwnvr-detect)
  - [Alert que não fecha depois da correção](#alert-que-não-fecha-depois-da-correção)
- [CodeQL](#codeql)
- [Private vulnerability reporting](#private-vulnerability-reporting)
- [Ruleset na `main`](#ruleset-na-main)
- [Merge do PR sem merge commit](#merge-do-pr-sem-merge-commit)
- [govulncheck semanal](#govulncheck-semanal)
- [Codecov](#codecov)

## Conferir o estado

```sh
gh api repos/$REPO --jq '.security_and_analysis'             # secret scanning e security updates
gh api repos/$REPO/vulnerability-alerts -i | head -1         # 204 = Dependabot alerts ligado, 404 = desligado
gh api repos/$REPO/actions/permissions/workflow              # token dos workflows
gh api "repos/$REPO/dependabot/alerts?state=open" --jq 'length'
gh api repos/$REPO/code-scanning/default-setup --jq '.state' # configured = CodeQL ligado
gh api "repos/$REPO/code-scanning/alerts?state=open" --jq 'length'
gh api repos/$REPO/private-vulnerability-reporting
gh api repos/$REPO/rules/branches/main --jq '.[].type'       # deletion, non_fast_forward
gh api repos/$REPO --jq '.allow_merge_commit'                # false
```

## Secret scanning e push protection

O GitHub procura segredo conhecido (token de nuvem, chave de API) em tudo que
já foi publicado, e o push protection recusa o `git push` que traz um novo. Não
substitui o betterleaks do commit (um hook do
[`.pre-commit-config.yaml`](../.pre-commit-config.yaml)): ele roda antes do
commit existir e conhece a senha de câmera, que o GitHub não conhece.

```sh
gh api -X PATCH repos/$REPO --input - <<'EOF'
{"security_and_analysis":{"secret_scanning":{"status":"enabled"},"secret_scanning_push_protection":{"status":"enabled"}}}
EOF
```

## Token dos workflows só com leitura

Quando um workflow não declara `permissions`, o `GITHUB_TOKEN` recebe só
leitura, e o Actions não pode aprovar pull request. Os workflows daqui
declaram `permissions: {}` no topo e pedem o que precisam por job; este é o
padrão para quando algum esquecer.

```sh
gh api -X PUT repos/$REPO/actions/permissions/workflow \
  -f default_workflow_permissions=read -F can_approve_pull_request_reviews=false
```

## Dependabot alerts e security updates

Ligados em 28/09/2026. O alert aparece quando uma dependência do `go.mod`, do
`web/package-lock.json`, do `dwnvr-detect/requirements.txt` ou dos workflows
ganha uma vulnerabilidade publicada. O security update abre um PR com a versão
corrigida, quando ela existe. Não depende de `dependabot.yml`, e não sobe
versão por outro motivo.

A imagem Docker não entra: o Dependabot não tem alert nem security update para
`docker`, só atualização de versão.

```sh
gh api -X PUT repos/$REPO/vulnerability-alerts       # alerts; vem primeiro
gh api -X PUT repos/$REPO/automated-security-fixes   # security updates
```

Para desligar, os mesmos dois com `-X DELETE`, na ordem inversa.

A atualização de versão, um PR por mês e por ecossistema, vem do
[`.github/dependabot.yml`](../.github/dependabot.yml), que diz no próprio
arquivo o porquê de cada regra. O badge do Dependabot no README fica vermelho
quando um run dele falha, como ao não conseguir ler um arquivo.

### PR do Dependabot em `web/`

O build da interface é versionado em `internal/api/dist`, e o Dependabot não o
refaz. Se a dependência entra no bundle, o PR falha no passo "interface está
atualizada?" do `ci.yml`, com uma anotação no resumo do PR que aponta para
cá. O aviso é o próprio check porque o Dependabot não comenta no PR, e uma
conversa que segura o merge só existe com um ruleset exigindo PR, o que
acabaria com o commit direto na `main`. O PR só entra com um commit do build
refeito:

```sh
gh pr checkout <número>
make web
git add internal/api/dist
git commit -m "build(web): interface refeita com <pacote> <versão>"
git push
```

Dependência só de desenvolvimento (ESLint, svelte-check) não muda o bundle e
passa sem isso.

### PR do Dependabot em `dwnvr-detect/`

O `requirements.txt` sai do `requirements.in` pelo `uv pip compile`, com hash
e valendo para amd64 e arm64. O Dependabot não recompila: troca a linha do
pacote, e pode propor uma versão que o resto do arquivo não aceita. O PR dele
serve de aviso; a correção é recompilar:

```sh
cd dwnvr-detect
uv pip compile --universal --generate-hashes --python-version 3.12 \
  --upgrade-package <pacote> requirements.in -o requirements.txt
```

Se o pacote é um dos três do `requirements.in`, o pin é trocado lá antes, e
vale o que diz o comentário do arquivo: outro runtime é outro número, e a
entrega precisa ser conferida de novo. O PR do Dependabot fica para fechar à
mão.

### Alert que não fecha depois da correção

O Dependabot manda as dependências ao dependency graph em snapshots por pasta,
no workflow "Dependency Graph", e cada snapshot só é trocado por outro de mesmo
correlator. A leitura de `/dwnvr-detect` desce até `modelo/` e leva o
`modelo/requirements.txt` junto; um push que só mexe em `modelo/` refaz apenas
o snapshot de `/dwnvr-detect/modelo`. O de `/dwnvr-detect` fica com a versão
velha, e o alert continua aberto com o arquivo já corrigido. Foi o que
aconteceu com o `torch` em 29/09/2026.

Para conferir, o dependency graph lista duas versões do mesmo pacote no mesmo
arquivo:

```sh
gh api repos/$REPO/dependency-graph/sbom \
  --jq '.sbom.packages[] | select(.name == "<pacote>") | .versionInfo'
```

Com a correção na `main`, o alert é dispensado à mão como `inaccurate`, com o
commit da correção no comentário. Commit vazio para forçar a leitura não vale
a sujeira no histórico.

## CodeQL

Ligado em 28/09/2026, no default setup, que dispensa workflow no repositório:
o GitHub escolhe as linguagens (actions, go, javascript-typescript e python) e
roda a análise a cada push na `main`, em todo PR e uma vez por semana. É
análise de fluxo de dado, como a entrada da requisição chegando num caminho de
arquivo ou num comando, que os linters do commit não fazem. O achado vai para
a aba Security, em Code scanning, e não segura merge nem push.

```sh
gh api -X PATCH repos/$REPO/code-scanning/default-setup -f state=configured
```

Para desligar, o mesmo com `-f state=not-configured`.

## Private vulnerability reporting

Ligado em 28/09/2026. Dá a quem achar uma falha um formulário privado na aba
Security, em vez de uma issue pública; o [`SECURITY.md`](../SECURITY.md) aponta
para ele.

```sh
gh api -X PUT repos/$REPO/private-vulnerability-reporting
```

Para desligar, o mesmo com `-X DELETE`.

## Ruleset na `main`

Criado em 28/09/2026. Recusa apagar a `main` e recusa o push que reescreve o
histórico dela (`git push --force`). Não exige PR nem check: o commit direto e
o push comum continuam, inclusive o da release, que empurra o commit da versão
junto com a tag.

```sh
gh api -X POST repos/$REPO/rulesets --input - <<'EOF'
{"name":"main","target":"branch","enforcement":"active",
 "conditions":{"ref_name":{"include":["~DEFAULT_BRANCH"],"exclude":[]}},
 "rules":[{"type":"deletion"},{"type":"non_fast_forward"}]}
EOF
```

O `POST` cria um ruleset novo a cada vez que roda. Para mudar o que existe, o
id sai de `gh api repos/$REPO/rulesets` e vai em
`gh api -X PUT repos/$REPO/rulesets/<id>`, com o mesmo JSON; para apagar, em
`-X DELETE`.

## Merge do PR sem merge commit

Desligado em 01/10/2026. O botão de merge do PR oferece só squash e rebase: o
AGENTS.md diz que a `main` não aceita merge commit, e o "Allow merge commits"
ligado deixava a opção a um clique. O GitHub exige ao menos uma das três
ligada.

```sh
gh repo edit $REPO --enable-merge-commit=false
```

## govulncheck semanal

O [`.github/workflows/govulncheck.yml`](../.github/workflows/govulncheck.yml)
roda toda segunda no binário da imagem `latest`, que é o que roda nas casas. Ele
falha quando o binário tem falha conhecida em código que o dwnvr chama, da
stdlib do Go ou de dependência, e o GitHub manda e-mail. O cabeçalho do arquivo
diz por que é no binário, e não no fonte.

```sh
gh workflow run govulncheck.yml   # rodar agora, sem esperar a segunda
```

Em repositório público, o GitHub desliga o workflow agendado depois de 60 dias
sem commit; `gh workflow enable govulncheck.yml` religa.

O mesmo teste local, sem o GitHub:

```sh
go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
docker pull -q ghcr.io/$REPO:latest
id=$(docker create ghcr.io/$REPO:latest)
docker cp -q "$id:/dwnvr" ./dwnvr && docker rm "$id"
"$(go env GOPATH)/bin/govulncheck" -mode=binary ./dwnvr
```

## Codecov

O badge de cobertura do README vem do [Codecov](https://app.codecov.io/gh/mhagnumdw/dwnvr),
que recebe o relatório do [`.github/workflows/cobertura.yml`](../.github/workflows/cobertura.yml)
a cada push na `main` e em cada pull request, menos os do Dependabot. O que ele
faz com o relatório está no [`.github/codecov.yml`](../.github/codecov.yml).

Não há token nem secret: o upload se autentica por OIDC, e o Codecov confere
que o run é deste repositório. O primeiro upload ativa o repositório no
Codecov; se ele for recusado, entrar com o GitHub em app.codecov.io e
habilitar o repositório lá.

O comentário no PR e o status com o número em cada commit são do app do
Codecov, instalado em 01/10/2026 em <https://github.com/apps/codecov>, com
acesso só ao dwnvr (Only select repositories). Ele pede escrita em status,
checks e pull requests, que é por onde comenta. Sem o app, o upload e o badge
continuam, e só o comentário e o status somem. O acesso se revê em
<https://github.com/settings/installations>.

```sh
curl -s https://api.codecov.io/api/v2/github/mhagnumdw/repos/dwnvr/ | jq '{activated, totals: .totals.coverage}'
gh workflow run cobertura.yml   # mandar de novo, sem esperar um push
```

Se o Codecov sair do ar, o `cobertura` fica vermelho e o badge para no último
número; a CI, as imagens e a release não dependem dele.
