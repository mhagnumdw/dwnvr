# TODO - segurança do repositório, o que sobrou

O que foi feito está no [`github.md`](../github.md). Aqui fica o que ainda
depende de um passo ou de uma decisão.

## Andamento

- [ ] Ligar `sha_pinning_required` no repositório, depois que as actions
  pinadas por SHA estiverem na `main` (o comando está no
  [`github.md`](../github.md#pin-por-sha))
- [ ] Confirmar no primeiro PR do Dependabot em `github-actions` que ele troca
  o SHA e o comentário de versão juntos
- [ ] Pinar os `rev:` do `.pre-commit-config.yaml` por SHA

## `rev:` do pre-commit por SHA

São tags de repositório de terceiro rodando na máquina de quem commita.
`prek update --freeze` as troca por SHA. Antes, testar se o Dependabot
(ecossistema `pre-commit`) atualiza um `rev:` em SHA com o comentário de
versão: o lychee já derrubou o run dele pelo formato da tag.

## Medido e fora por enquanto

- **gosec no golangci-lint.** Com o G104, o G115 e os G30x desligados, 3
  achados no código de produção, todos falso positivo (o cookie de sessão sem
  `Secure`, que agora sai `Secure` atrás de proxy TLS; o do logout sem
  `SameSite`; e um path traversal que o `validGen` já impede). Serviria de
  guarda para código novo (`InsecureSkipVerify`, `exec` com entrada externa,
  `http.Server` sem timeout). O CodeQL cobre boa parte disso.
- **Scan das imagens publicadas** (Trivy ou Grype): a do dwnvr é `scratch`
  com um binário estático, e o que ele acharia o govulncheck acha; a do
  `dwnvr-detect` tem a base Debian. O Dependabot já sobe a base.
- **`dwnvr-detect` com `pyproject.toml` e `uv.lock`.** O ecossistema `uv` do
  Dependabot recompila o lock, em vez de trocar linha, e daria ao `pip` um PR
  que funciona. Muda como o detector de objetos é empacotado, para resolver um
  PR que hoje se resolve com um comando.
- **Renovate no lugar do Dependabot**, avaliado em 28/09 e deixado de lado.
  Faria melhor: texto fixo no corpo do PR (`prBodyNotes`), recompilar o
  `requirements.txt` do `uv pip compile`, subir a versão do Go em todos os
  lugares num PR só e ler qualquer formato de tag por regex manager. Nenhum dos
  dois refaz o `internal/api/dist`. Custa um app de terceiro com escrita no
  repositório, inclusive nos workflows. Voltar a olhar se o Dependabot deixar
  de ler algo de que o projeto precisa; a troca é apagar um arquivo e criar
  outro.
