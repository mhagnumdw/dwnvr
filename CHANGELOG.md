# Changelog

> Gerado automaticamente na release pelo git-cliff (`.github/workflows/release.yml`); datas em UTC; não edite à mão.

## [v0.2.0](https://github.com/mhagnumdw/dwnvr/compare/v0.1.0...v0.2.0) (2026-09-29)

### Added

- feat(web): picture-in-picture por câmera no Ao Vivo ([5378e219](https://github.com/mhagnumdw/dwnvr/commit/5378e2191d2b1cd39cafe9b0b7c9ff4b0feafcbc))
- feat(web): menu ⋮ nas câmeras do Ao Vivo ([a833ca43](https://github.com/mhagnumdw/dwnvr/commit/a833ca4302afaefef38a4bd6c35ddeae6a82303f))
- feat(diagnostico): avisos com título, "desde quando" e botão de zerar reconexões ([#7](https://github.com/mhagnumdw/dwnvr/pull/7)) ([7074e756](https://github.com/mhagnumdw/dwnvr/commit/7074e756cdfdf144ce02675b932027e89c41cce9))

### Fixed

- fix(ci): build das imagens em pull request não calcula tag ([669585f9](https://github.com/mhagnumdw/dwnvr/commit/669585f91392cea31396b0631cdaf2cd61a752f5))
- fix(api): cookie de sessão sai Secure atrás de proxy TLS ([5cd5a87f](https://github.com/mhagnumdw/dwnvr/commit/5cd5a87f39b51011604b071f304eaaad20815dc7))
- fix(store): recusa ID de câmera que não é nome de diretório ([515612fd](https://github.com/mhagnumdw/dwnvr/commit/515612fd97127fec8cdc669e3688f5d87063d7e3))

### Changed

- refactor(api): valida a geração do init com regexp ([72c51d0a](https://github.com/mhagnumdw/dwnvr/commit/72c51d0a7cb053c5ed9f47f936c556a3d32b07fc))

### Docs

- docs: o alerta do torch dispensado e por que não fechou sozinho ([1f5205af](https://github.com/mhagnumdw/dwnvr/commit/1f5205af046a9ccf188b377a0475a528bf388910))
- docs(todo): o log da cota diz liberado_mb=0 quando liberou ([2a27929a](https://github.com/mhagnumdw/dwnvr/commit/2a27929aff34b0400e7d414296c2e049a0d21efe))
- docs: fontes públicas candidatas para recalibrar o modelo ([407f22d5](https://github.com/mhagnumdw/dwnvr/commit/407f22d59035de144a834298be9a8d97d4b9368b))
- docs: torch 2.13.0 e segundo run do Dependabot no TODO de segurança ([07886d89](https://github.com/mhagnumdw/dwnvr/commit/07886d89fdab1b822abe8cceb383f961f62595ef))
- docs: CodeQL, canal privado de falha e ruleset na main ([20c4bf9d](https://github.com/mhagnumdw/dwnvr/commit/20c4bf9dd62cffcc57027d37482728dbd1f1816f))
- docs: reavaliação da segurança no GitHub e doc da configuração ([8761f979](https://github.com/mhagnumdw/dwnvr/commit/8761f979b50d6aa5dad097339bc0a604243925ac))
- docs: TODO de segurança do repositório no GitHub ([17c26969](https://github.com/mhagnumdw/dwnvr/commit/17c26969bcf971bfad3b068ce909c18e27f032ff))
- docs: Etapa 14 cancelada no TODO de lint ([32fa63ae](https://github.com/mhagnumdw/dwnvr/commit/32fa63ae85f80f16becd30808fd381c9a50d8350))
- docs: etapas canceladas no TODO de lint ([56cb3087](https://github.com/mhagnumdw/dwnvr/commit/56cb30874cb636f1081fb9e5ec8cb16c722c1e62))
- docs(todo): reordena as etapas do lint ([b204d350](https://github.com/mhagnumdw/dwnvr/commit/b204d350d95765d37be8ff3ab555dad8e6eb6bb8))
- docs(todo): plano de lint com pre-commit, em etapas ([1272f3c5](https://github.com/mhagnumdw/dwnvr/commit/1272f3c5ea3517bd04cebc228d39eb63b4a96f14))
- docs: badges no README e o TODO da retenção sem teste ([f4fe3b38](https://github.com/mhagnumdw/dwnvr/commit/f4fe3b381b1c492e3fb9b90fee7b78b01aaf1e77))

### CI/Build

- build(deps): torch 2.13.0 no modelo, que refaz o .onnx publicado ([15a180d8](https://github.com/mhagnumdw/dwnvr/commit/15a180d88c2c8845e0cf3a6b5666119e9cf8052a))
- build(deps-dev): bump svelte from 5.57.0 to 5.57.1 in /web in the npm group across 1 directory ([#10](https://github.com/mhagnumdw/dwnvr/pull/10)) ([9a006126](https://github.com/mhagnumdw/dwnvr/commit/9a006126e69870e356e58399ecd066fee6163a11))
- build(deps): bump the docker group across 1 directory with 2 updates ([0c85cbd7](https://github.com/mhagnumdw/dwnvr/commit/0c85cbd7d50667b9f4f3a865305a5672ca0cb46d))
- build: Dependabot sem o lychee, sem o compose e sem TypeScript 7 ([82ee021f](https://github.com/mhagnumdw/dwnvr/commit/82ee021ff97a02f3f81512fdcb505359e0ff0fc7))
- ci: govulncheck semanal no binário da última release ([123de6de](https://github.com/mhagnumdw/dwnvr/commit/123de6de8faa39a9d715766492d0d33306066b22))
- build: Dependabot sobe as dependências uma vez por mês ([32e10c7d](https://github.com/mhagnumdw/dwnvr/commit/32e10c7d55e9fa2789163664a7c0f36b713d9c19))
- ci: pull request constrói as imagens sem publicar (apenas para validação) ([027d2063](https://github.com/mhagnumdw/dwnvr/commit/027d20631bffdaa0e802d6d983cccb951865e8d7))
- ci: interface web desatualizada vira anotação com link para a documentação ([0bbdc5ec](https://github.com/mhagnumdw/dwnvr/commit/0bbdc5ecc2f76edf239d0b3b596ba3d53fd4a32c))
- ci: prek no lugar do pre-commit, com hooks em paralelo ([210365bf](https://github.com/mhagnumdw/dwnvr/commit/210365bf758958707c880796c25e8d77fd56a850))
- ci: betterleaks barra segredo e senha de câmera no commit ([575627b1](https://github.com/mhagnumdw/dwnvr/commit/575627b1785c102451256c08b45fd21ace71967e))
- ci: zizmor no pre-commit ([28db0f2a](https://github.com/mhagnumdw/dwnvr/commit/28db0f2aa7a3e0d7c2805ea5672ec218662cff55))
- ci: correções do zizmor nos workflows ([6ecb9d07](https://github.com/mhagnumdw/dwnvr/commit/6ecb9d07f8259f29463f43efe8729a66e7ed7640))
- ci: política de pin das actions no zizmor ([7b74d984](https://github.com/mhagnumdw/dwnvr/commit/7b74d984e86cff7d01de7258483aa3b1635f9913))
- ci: segunda leva do golangci-lint ([659705fc](https://github.com/mhagnumdw/dwnvr/commit/659705fc4fd137b628ee64f19f975b1482aa7b1f))
- ci: ruff no dwnvr-detect ([01acfd90](https://github.com/mhagnumdw/dwnvr/commit/01acfd90d2dffa07f946c89677bbdb089411f09e))
- ci: ESLint com o plugin do Svelte ([587d42ff](https://github.com/mhagnumdw/dwnvr/commit/587d42fff59f8ead6d0a629a3778be87519970a9))
- ci: svelte-check no pre-commit ([01f4a9a4](https://github.com/mhagnumdw/dwnvr/commit/01f4a9a48bcd21ff2d054c64220824ea5800c4c2))
- ci: golangci-lint no pre-commit ([2642d4f5](https://github.com/mhagnumdw/dwnvr/commit/2642d4f57f6b3df5ba309ad8c4f801167c6daf05))
- ci: actionlint nos workflows ([993a6aa0](https://github.com/mhagnumdw/dwnvr/commit/993a6aa0e98ed896f961ceb501623c8faa34a36f))
- ci: hadolint nos Dockerfiles ([dcc65adb](https://github.com/mhagnumdw/dwnvr/commit/dcc65adb892e5337bcaf57c06f095e530f2886ba))
- ci: yamllint ([6afacfd0](https://github.com/mhagnumdw/dwnvr/commit/6afacfd0064f4653b59fefd4fca9b9c28b60df18))
- ci: lychee confere link e âncora do Markdown ([609c6846](https://github.com/mhagnumdw/dwnvr/commit/609c68460cbec25c12fb64252352346177408138))
- ci: markdownlint no pre-commit ([d75d6afd](https://github.com/mhagnumdw/dwnvr/commit/d75d6afd073600f737da56f89165356fde6f0454))
- ci: pre-commit com as checagens básicas de arquivo ([c9137090](https://github.com/mhagnumdw/dwnvr/commit/c9137090b00e7f66e8c7f2bf321e2c47c392afb4))
- build(compose): fixa o go2rtc na 1.9.14 ([ac61c5ae](https://github.com/mhagnumdw/dwnvr/commit/ac61c5ae6eb8c40054817ccaf4508bc53172bdd0))

## [v0.1.0](https://github.com/mhagnumdw/dwnvr/tree/v0.1.0) (2026-09-27)

Primeira versão com número. Até aqui o dwnvr saía só como `latest`, a cada commit; daqui em diante cada versão tem nome, notas e imagens próprias, e a interface avisa quando sai uma nova.

