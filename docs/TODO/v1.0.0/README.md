# TODO - rumo à 1.0.0

O índice do plano da 1.0.0: em que ponto está cada etapa. O porquê da ordem e
as decisões que valem para todas estão no [plano.md](plano.md); o estudo de
cada atividade, no arquivo dela.

**Agora (27/11/2026):** a Etapa 2 (conversão para en-US) está concluída, com a
[v0.16.0](https://github.com/mhagnumdw/dwnvr/releases/tag/v0.16.0), e a Etapa 3
(temas) está em andamento: o passo 1 foi mergeado, e o passo 2 (as paletas e o
script de contraste) está em PR. O guia de atualização da conversão está em
[docs/upgrading.md](../../upgrading.md), com uma seção por parte.

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
- [x] **Etapa 2 - conversão para en-US**: concluída em 27/11/2026 ([conversao-en-us.md](conversao-en-us.md)). Um PR por passo, em commits pequenos, que roda na instalação de teste antes de ser aceito
  - [x] O glossário do domínio ([glossario.md](glossario.md))
  - [x] O `AGENTS.md` com a regra nova de escrita
  - [x] Go, pacote a pacote, sem mudar comportamento: PR #86, em 11/11/2026
  - [x] A interface, sem os textos da tela: PR #88, em 20/11/2026
  - [x] O `dwnvr-detect`: PR #89, em 20/11/2026
    - [x] Os nomes e os comentários do `server.py`, `check_pyc.py`, `sitecustomize.py` e `model/export.py`; os arquivos renomeados (`server.py`, `check_pyc.py`, `model/`, `export.py`)
    - [x] A mesma resposta da imagem de antes para o mesmo clipe (57 achados, o quadro em base64 inclusive), e a imagem na instalação de teste
    - [x] O PR mergeado por squash
  - [x] Os contratos (API, o que o dwnvr grava, `dwnvr.yaml`, env), em partes, cada uma com a sua seção do guia de atualização aplicada na instalação de teste. Sem aviso para quem não seguiu o guia, sem número de versão nos arquivos e sem compatibilidade nenhuma com o formato antigo (decidido em 20/11/2026)
    - [x] Parte 1, a API e a interface: as rotas, os parâmetros, as chaves do JSON, as rotas do hash, as chaves da URL, as chaves do `localStorage` e os ids de coluna do Diagnóstico: PR #90, em 20/11/2026
    - [x] Parte 2, o `dwnvr.yaml`, a env e o contrato com o `dwnvr-detect` (`DETECT_MODEL` e `DETECT_PORT`, os parâmetros e as chaves do `POST /detect` e do `/health`, e `detectMethod` e `detectSensitivity`, que são a mesma chave no yaml, no `cameras.json` e na API): PR #91, em 20/11/2026
      - [x] As duas pontas e os docs, na branch `en-us-config`; a seção 2 do guia
      - [x] A mesma resposta da imagem de antes para o mesmo clipe, com os nomes novos (57 achados, o quadro em base64 inclusive)
      - [x] A seção 2 do guia aplicada na instalação de teste, com uma câmera fora do padrão, e o deploy da branch lá
      - [x] O PR mergeado por squash
    - [x] Parte 3, os nomes dos arquivos das pessoas (`usuarios.json`, `acessos.json`, `avatares/` viram `users.json`, `access.json`, `avatars/`): PR #92, em 20/11/2026
      - [x] O Go, a interface e os docs, na branch `en-us-people`; a seção 3 do guia
      - [x] A seção 3 do guia aplicada na instalação de teste e o deploy da branch lá
      - [x] O PR mergeado por squash
    - [x] Parte 4, os eventos e os quadros gravados (`eventos/`, `quadros/` e as chaves de cada linha viram `events/`, `frames/`, `atMs`, `family`, `class`, `frameMs`, `box`, `hasFrame`); começa pelo backup
      - [x] O Go e os docs, na branch `en-us-events`; a seção 4 do guia, com o backup de cada `eventos/`
      - [x] A seção 4 testada numa cópia dos eventos da instalação de teste: as mesmas linhas, chave e valor; rodar de novo não muda nada; a versão nova que rodou antes, a rodada interrompida e a falha do `jq`
      - [x] A seção 4 aplicada na instalação de teste e o deploy da branch lá
      - [x] O PR mergeado por squash: PR #95, em 20/11/2026
    - [x] Parte 5, os valores das enumerações (`pessoa`, `veiculo`, `periodico` viram `person`, `vehicle`, `periodic`), que são a mesma constante na API, no `cameras.json`, no yaml e nos eventos
      - [x] O Go, a interface e os docs, na branch `en-us-enums`; a seção 5 do guia
      - [x] A seção 5 testada numa cópia da instalação de teste: as mesmas linhas, só com a família trocada; rodar de novo não muda nada; a falha no meio e a nova rodada
      - [x] A seção 5 aplicada na instalação de teste e o deploy da branch lá
      - [x] O PR mergeado por squash: PR #96, em 20/11/2026
  - [x] Os `docs/TODO/` abertos revistos com o mantenedor, em 22/11/2026: quatro apagados (já feitos ou desistidos); os outros ficam em pt-BR, sem tradução na conversão. O mantenedor os traduz depois, sem pressa, e TODO novo já nasce em en-US
  - [x] A documentação, os exemplos, os workflows e o resto, na branch `en-us-docs`, em 22/11/2026: os `docs/` com nomes e texto em en-US, os exemplos, o build, os workflows (`images.yml`, `coverage.yml`) e as notas de release ("Action required on upgrade")
  - [x] O `README.md` em en-US e o `README.pt-BR.md`, em 22/11/2026, com o `SECURITY.md` em en-US
  - [x] O PR do passo 7 mergeado por squash: PR #98, em 22/11/2026
  - [x] A varredura final e o `.git-blame-ignore-revs`: PR #100, em 27/11/2026
    - [x] Os restos conhecidos: `session.pessoa` e a chave `nome` na interface, o log `dwnvr no ar` (agora `dwnvr is up`, nos dois READMEs) e o hook `interface-atualizada` (agora `ui-up-to-date`), em 27/11/2026
    - [x] A varredura: o que o passo 4 deixou em português no `web/` (identificadores, chaves internas, variáveis e classes de CSS, ids), os comentários e nomes dos arquivos de build e de lint e os nomes de câmera dos testes Go, em 27/11/2026. Ficam em pt-BR o texto de tela e de e-mail, os rótulos de tela citados nos docs e os dados de exemplo (`cam_teste*`, `troque-me`, `usuario:senha`)
    - [x] O `.git-blame-ignore-revs` com os passos 3, 4 e 5 (#86, #88, #89), que só renomeiam; o passo 6 muda contrato e os passos 2 e 7 reescrevem texto, e ficam no blame
    - [x] O PR mergeado por squash, com o conserto das classes de CSS que o passo 4 fundiu (a tabela do reconhecimento no Diagnóstico e mais cinco)
  - [x] A release da etapa: a [v0.16.0](https://github.com/mhagnumdw/dwnvr/releases/tag/v0.16.0), em 27/11/2026, com a CI da `main` verde
- [ ] **Etapa 3 - temas**: em andamento desde 27/11/2026 ([temas.md](temas.md))
  - [x] Toda cor da tela passa por um token: PR #101, em 27/11/2026
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
