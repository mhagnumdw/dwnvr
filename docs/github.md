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
- [CodeQL](#codeql)
- [Private vulnerability reporting](#private-vulnerability-reporting)
- [Ruleset na `main`](#ruleset-na-main)

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
`docker`, só atualização de versão, que depende do `dependabot.yml`.

```sh
gh api -X PUT repos/$REPO/vulnerability-alerts       # alerts; vem primeiro
gh api -X PUT repos/$REPO/automated-security-fixes   # security updates
```

Para desligar, os mesmos dois com `-X DELETE`, na ordem inversa.

### PR do Dependabot em `web/`

O build da interface é versionado em `internal/api/dist`, e o Dependabot não o
refaz. Se a dependência entra no bundle, o PR falha no passo "interface está
atualizada?" do `ci.yml`, e só entra com um commit do build refeito:

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
