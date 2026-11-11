# TODO - rumo à 1.0.0

O índice do plano da 1.0.0: em que ponto está cada etapa. O porquê da ordem e
as decisões que valem para todas estão no [plano.md](plano.md); o estudo de
cada atividade, no arquivo dela.

**Agora (11/11/2026):** a Etapa 2 (conversão para en-US) começou, na branch
`en-us-conversion`, com o glossário decidido e o `AGENTS.md` em en-US. O próximo passo é o Go,
pacote a pacote.

## Andamento

Marque aqui, e só aqui. O status vai na linha de cada etapa: não começou, a
próxima, em andamento ou concluída, com a data.

- [x] **Etapa 0 - decisões**: concluída em 10/11/2026
  - [x] O SQLite fica no radar, fora da 1.0.0 ([sqlite.md](sqlite.md))
  - [x] O que entra na conversão para en-US ([as decisões](conversao-en-us.md#as-decisões))
  - [x] Sem compatibilidade, com um guia de atualização ([plano.md](plano.md#sem-compatibilidade-com-um-guia))
  - [x] Uma release por etapa ([plano.md](plano.md#uma-release-por-etapa))
- [x] **Etapa 1 - terreno limpo**: concluída em 11/11/2026. A conversão renomeia quase todo arquivo, e o que estiver aberto em paralelo vira conflito em tudo
  - [x] A release do que está na `main` desde a v0.14.0: a [v0.15.0](https://github.com/mhagnumdw/dwnvr/releases/tag/v0.15.0), em 11/11/2026
  - [x] PR #37 (limite de 5000 tokens no commit, draft): fica de lado, por decisão do mantenedor em 11/11/2026
  - [x] PR #16 (e2e com Playwright, draft) e o worktree `modelo-reproduzivel`: ficam de lado, por decisão do mantenedor em 11/11/2026; o que a conversão quebrar neles se refaz depois
  - [x] Os PRs do Dependabot abertos: o #60 (uv 0.13.7) mergeado em 11/11/2026, antes da release
  - [x] Opcional: a fumaça das telas do PR #16 antes da conversão. Saiu junto com o PR #16; a interface entra na conversão só com a verificação estática (`svelte-check`, `tsc`, ESLint), que não pega uma chave de JSON renomeada de um lado só
- [ ] **Etapa 2 - conversão para en-US**: em andamento desde 11/11/2026 ([conversao-en-us.md](conversao-en-us.md)). Em commits pequenos, cada um rodando na instalação de teste antes do próximo
  - [x] O glossário do domínio ([glossario.md](glossario.md))
  - [x] O `AGENTS.md` com a regra nova de escrita
  - [ ] Go, pacote a pacote, sem mudar comportamento
  - [ ] A interface, sem os textos da tela
  - [ ] O `dwnvr-detect`
  - [ ] Os contratos (API, o que o dwnvr grava, `dwnvr.yaml`, env), em partes, cada uma com a sua seção do guia de atualização aplicada na instalação de teste
  - [ ] Os `docs/TODO/` abertos revistos com o mantenedor
  - [ ] A documentação, os exemplos, os workflows e o resto
  - [ ] O `README.md` em en-US e o `README.pt-BR.md`
  - [ ] A varredura final e o `.git-blame-ignore-revs`
- [ ] **Etapa 3 - temas**: não começou ([temas.md](temas.md))
  - [ ] Toda cor da tela passa por um token
  - [ ] As quatro paletas e o "sistema", com o contraste conferido
  - [ ] O seletor e onde a escolha fica
- [ ] **Etapa 4 - paleta customizável**: não começou ([paleta.md](paleta.md))
  - [ ] Os tokens documentados como contrato
  - [ ] O arquivo da paleta na pasta de configuração
  - [ ] O comando que gera a paleta
- [ ] **Etapa 5 - i18n, pt-BR e en-US**: não começou ([i18n.md](i18n.md))
  - [ ] O mecanismo e os dois catálogos
  - [ ] As telas
  - [ ] As mensagens do servidor
  - [ ] Os e-mails
  - [ ] Datas, números e plural
  - [ ] A escolha do idioma
- [ ] **Etapa 6 - tela de configuração global**: não começou ([configuracao-global.md](configuracao-global.md))
  - [ ] Revisar o estudo de 07/10 com o que mudou
  - [ ] O arquivo da tela e a precedência sobre o `dwnvr.yaml`
  - [ ] A rota e a tela
- [ ] **Etapa 7 - a 1.0.0**: não começou
  - [ ] Revisar os contratos ([plano.md](plano.md#o-que-a-100-promete)) e o `AGENTS.md`
  - [ ] Os TODOs abertos: algum é defeito que a 1.0.0 não deve levar?
  - [ ] O `[bump]` do `cliff.toml`: depois da 1.0.0, um `!` passa a sugerir major
  - [ ] A release v1.0.0

## Os arquivos

| Arquivo | O que tem |
| --- | --- |
| [plano.md](plano.md) | o veredito de cada atividade, a ordem e o porquê, o tamanho de cada etapa, a política de compatibilidade e de release, o que a 1.0.0 promete |
| [sqlite.md](sqlite.md) | por que o SQLite ficou no radar, com a medição e o critério para reabrir |
| [conversao-en-us.md](conversao-en-us.md) | o escopo da conversão, os passos e as decisões |
| [glossario.md](glossario.md) | o nome en-US de cada termo do domínio |
| [temas.md](temas.md) | o que já existe, as cores escritas direto e as paletas |
| [paleta.md](paleta.md) | o contrato dos tokens e como a paleta se gera |
| [i18n.md](i18n.md) | o inventário do que se traduz e o que se decide na etapa |
| [configuracao-global.md](configuracao-global.md) | o que mudou desde o estudo de 07/10 e a primeira leitura dos campos novos |
