# Changelog

> Gerado automaticamente na release pelo git-cliff (`.github/workflows/release.yml`); datas em UTC; não edite à mão.

## [v0.9.0](https://github.com/mhagnumdw/dwnvr/compare/v0.8.0...v0.9.0) (2026-10-15)

### Added

- feat(api): limite de tentativas no login ([#21](https://github.com/mhagnumdw/dwnvr/pull/21)) ([b76c6e20](https://github.com/mhagnumdw/dwnvr/commit/b76c6e2020008fa530d18c35d0c2407e4cff9d70))
- feat: mostra o último acesso de cada pessoa na tela Usuários ([#22](https://github.com/mhagnumdw/dwnvr/pull/22)) ([878596f3](https://github.com/mhagnumdw/dwnvr/commit/878596f3c2da6f35f70c5608856bcad926e51961))
- feat(web): avisa quando a tela aberta ficou desatualizada ([d820871d](https://github.com/mhagnumdw/dwnvr/commit/d820871db904f425196118af4f6a66fb0d90645a))
- feat(web): tela cheia no vídeo da detecção ([aee3d81a](https://github.com/mhagnumdw/dwnvr/commit/aee3d81aeef7457a09034bb3a50aa918f0473399))
- feat(web): botão de tela cheia em cada câmera do Ao vivo ([bf60a809](https://github.com/mhagnumdw/dwnvr/commit/bf60a80967bc33770874d4ba6a3e976f73dd50b4))
- feat(web): tela cheia no Ao vivo, trocando de câmera ([133d69ec](https://github.com/mhagnumdw/dwnvr/commit/133d69ecf5e94ba283185e4f8373cbfb61d6f712))
- feat(web): tela cheia nas gravações ([ec38a317](https://github.com/mhagnumdw/dwnvr/commit/ec38a3179bd70794e15c880e09f10cbea0d4fcac))
- feat(web): botão de câmeras mostra as marcadas do total ([03bb8394](https://github.com/mhagnumdw/dwnvr/commit/03bb83942591a60eea886e83b006a2dd0f2c55aa))
- feat(api): login aceito e logout vão para o log, com o endereço de origem ([5d131f5a](https://github.com/mhagnumdw/dwnvr/commit/5d131f5aff930834d0f8ecbbbfc92121f17e2815))
- feat(web): login diz o que fazer quando a senha é esquecida ([9a260057](https://github.com/mhagnumdw/dwnvr/commit/9a260057a5f9e141bfa6394fb8766e107e9bf7c5))

### Fixed

- fix(web): troca de câmera nas gravações não fica mais em tela preta ([#25](https://github.com/mhagnumdw/dwnvr/pull/25)) ([2a056631](https://github.com/mhagnumdw/dwnvr/commit/2a0566314d14607df9f7635e805d6d2fa3504b9d))
- fix(web): miniaturas das gravações não ficam mais sem imagem por http ([a1f77018](https://github.com/mhagnumdw/dwnvr/commit/a1f77018e0e8214c79376f2ef72af59d40e6504c))
- fix(web): vídeo das gravações fica na câmera escolhida mesmo trocando rápido ([fa1d4da6](https://github.com/mhagnumdw/dwnvr/commit/fa1d4da60d1c01386aec9cc79a7cb8c12320e31f))
- fix(store): trecho que cruza a meia-noite não perde mais o que vem depois dela ([5e1529f5](https://github.com/mhagnumdw/dwnvr/commit/5e1529f5bd95ff8c8536631b34c7bae663cbdaec))
- fix(web): miniaturas com erro ao trocar de câmera nas Gravações ([cecd43f8](https://github.com/mhagnumdw/dwnvr/commit/cecd43f8acdcfdc61fef0307a40bf4f259b01210))
- fix(api): exportação pela API limitada a 10 minutos ([d77756d7](https://github.com/mhagnumdw/dwnvr/commit/d77756d76fd76dd2f1370625df6b04f7709322d5))
- fix(recorder): gravação aparecia pequena, num canto, em alguns celulares ([730f5329](https://github.com/mhagnumdw/dwnvr/commit/730f5329e249ae273f1f63484cf53142edcc0264))
- fix(web): score da detecção sempre fora da caixa ([894820bb](https://github.com/mhagnumdw/dwnvr/commit/894820bbb3d345e6f51afadd171bb24969521bf9))
- fix(web): folha da detecção abre no meio da tela no celular ([b0056691](https://github.com/mhagnumdw/dwnvr/commit/b0056691f9ccc00624f8097dec279ef6abe8b1fe))
- fix(web): menu ⋮ do Ao vivo aparece inteiro no celular ([20fb788d](https://github.com/mhagnumdw/dwnvr/commit/20fb788def2037624a0f9035aceb703f57e0332f))

### Performance

- perf(web): miniaturas das gravações ocupam bem menos memória ([#24](https://github.com/mhagnumdw/dwnvr/pull/24)) ([73303c75](https://github.com/mhagnumdw/dwnvr/commit/73303c753408fcd6e7a1e85cfd59dc1c37bb1974))
- perf(web): gravação carrega antes das miniaturas ([#23](https://github.com/mhagnumdw/dwnvr/pull/23)) ([ec67568c](https://github.com/mhagnumdw/dwnvr/commit/ec67568c89d4a1d609eb654a9a3b51df2e5b4ce8))
- perf(web): miniaturas das gravações aparecem primeiro onde se está olhando ([ba53fa5a](https://github.com/mhagnumdw/dwnvr/commit/ba53fa5a8c74af0df195b0d21cf8fe519962e2c5))
- perf(store): miniaturas e vídeos das gravações sem reler o dia inteiro ([d7d7c1c4](https://github.com/mhagnumdw/dwnvr/commit/d7d7c1c47fc90725b9ef63c4e1e3e92d7532e003))

### Changed

- refactor(web): interface em TypeScript ([6f5aac81](https://github.com/mhagnumdw/dwnvr/commit/6f5aac8108e11e50e888545086ba8e27c80a2e4d))

### CI/Build

- ci: commit da tela sem o build refeito é recusado ([6e432ae3](https://github.com/mhagnumdw/dwnvr/commit/6e432ae333f4a6e03c00b5333e3ba4582259b052))
- build(deps): bump source-map-js from 1.2.1 to 1.2.2 in /web ([80dd2c48](https://github.com/mhagnumdw/dwnvr/commit/80dd2c48df6fc850e1460c42e902864a84a9e231))
- ci: Prettier formata o web/ a cada commit ([165bb0f3](https://github.com/mhagnumdw/dwnvr/commit/165bb0f3e6ab329985a113e98b2c1c3bd4d58624))

## [v0.8.0](https://github.com/mhagnumdw/dwnvr/compare/v0.7.0...v0.8.0) (2026-10-10)

### Pede ação ao atualizar

- Depois de atualizar, todos os aparelhos voltam uma vez para a tela de login. Tenha a senha à mão. ([d85fd1f5](https://github.com/mhagnumdw/dwnvr/commit/d85fd1f5c883836141de0acc86397e6800577d92))

### Added

- feat: ajustes do cadastro de usuários depois do teste com pessoa real ([1c42d461](https://github.com/mhagnumdw/dwnvr/commit/1c42d461bdcf370ce61173369f82f921ae2210f8))
- feat: Minha conta, com foto, nome e troca de senha ([7864818f](https://github.com/mhagnumdw/dwnvr/commit/7864818f0c0bf3ffb5387818f08fce5df296f33e))
- feat: cadastro de usuários pela tela, com link de convite ([3da568b7](https://github.com/mhagnumdw/dwnvr/commit/3da568b7529804f61b1483c5ecdd56bc298f8315))
- feat(api)!: sessão por pessoa e funções de administrador ([d85fd1f5](https://github.com/mhagnumdw/dwnvr/commit/d85fd1f5c883836141de0acc86397e6800577d92))
- feat(web): tira o aviso de streams simultâneos do Ao vivo ([f714e3ff](https://github.com/mhagnumdw/dwnvr/commit/f714e3ffbaea46e3b0cdc7c41491b14eb6631d51))
- feat(web): tabelas do Diagnóstico em cartões no celular ([75aa4e08](https://github.com/mhagnumdw/dwnvr/commit/75aa4e0852c9e48175a21b1377ba8601cf51e7b9))

### Fixed

- fix(api): erro da senha guardada não cita o valor no log ([6cd66c80](https://github.com/mhagnumdw/dwnvr/commit/6cd66c8040ed32baee5237892ea969b60f1325df))

### Docs

- docs: corrige comentário do .ai-memory.toml ([df8f02fa](https://github.com/mhagnumdw/dwnvr/commit/df8f02fae18a9a591c524e11d8a9ed2c3efb1f4e))
- docs: TODO do log que sobrevive à atualização ([62eb115b](https://github.com/mhagnumdw/dwnvr/commit/62eb115b660919cf55365a5b768fa7f70964100b))
- docs(operacao): copiar gravações direto do disco ([066b54e3](https://github.com/mhagnumdw/dwnvr/commit/066b54e3f9238df0daa97705404dbdc9a0259601))

## [v0.7.0](https://github.com/mhagnumdw/dwnvr/compare/v0.6.0...v0.7.0) (2026-10-08)

### Pede ação ao atualizar

- Depois de atualizar, todos os aparelhos voltam uma vez para a tela de login. Tenha a senha à mão, inclusive para os aparelhos da família. ([316ba992](https://github.com/mhagnumdw/dwnvr/commit/316ba9924bcf8c00df7d5edc1245ffba65e9d9d0))

### Added

- feat(web): botão de câmeras do Ao vivo só com ícone no celular ([3c7cfcfd](https://github.com/mhagnumdw/dwnvr/commit/3c7cfcfd25db7d15c9b2d8d1c3455578e0b5f81d))
- feat(web): ícone de câmera no botão que abre a lista de câmeras ([435bb7b3](https://github.com/mhagnumdw/dwnvr/commit/435bb7b3203bb54733293ac91612514037d8bc5f))
- feat(web): remover uma câmera da tela Ao vivo pelo menu dela ([288ce05e](https://github.com/mhagnumdw/dwnvr/commit/288ce05e7775c26f603393d0f630406b21119746))
- feat(web): sino de avisos no header ([e724728e](https://github.com/mhagnumdw/dwnvr/commit/e724728eb0255cc400f9bcfe4f677bba4faaa853))
- feat(web): título da aba acompanha a tela ([e56a7b92](https://github.com/mhagnumdw/dwnvr/commit/e56a7b9284435dc6edaec484fcd47e7105de8b5a))

### Fixed

- fix(web): modo encaixar do Ao vivo aproveita toda a tela ([d4ef3e9e](https://github.com/mhagnumdw/dwnvr/commit/d4ef3e9e06cc2ed7787e2f7d5b234431e489202d))
- fix(api)!: trocar a senha deve derrubar as sessões abertas ([316ba992](https://github.com/mhagnumdw/dwnvr/commit/316ba9924bcf8c00df7d5edc1245ffba65e9d9d0))

### Docs

- docs: HTTPS na rede de casa, sem o Tailscale ([840ccbcd](https://github.com/mhagnumdw/dwnvr/commit/840ccbcdfc1ef835e5fce67cc3afe50da5d9bfec))
- docs: rodapé BREAKING CHANGE em parágrafo próprio ([dd004246](https://github.com/mhagnumdw/dwnvr/commit/dd00424637269c76ee1d3f33562e8cc53199d58d))
- docs: TODOs do hash da senha e do limite de tentativas no login ([95ae6878](https://github.com/mhagnumdw/dwnvr/commit/95ae687881c3d7fe71ed6f33fb33046cbb00ffad))
- docs: TODO de HTTPS na rede local, sem o Tailscale ([d0283a0d](https://github.com/mhagnumdw/dwnvr/commit/d0283a0dc1d056fed3fc21a8cb18a4c9947aa45b))
- docs: dwnvr.yaml e go2rtc.yaml legíveis só pelo seu usuário ([2f065fce](https://github.com/mhagnumdw/dwnvr/commit/2f065fce4dfbf6ec6f60d1f2803c0afb1ef51148))

## [v0.6.0](https://github.com/mhagnumdw/dwnvr/compare/v0.5.0...v0.6.0) (2026-10-07)

### Pede ação ao atualizar

- A interface web do go2rtc e o RTSP deixam de abrir de outra máquina. Para voltar a abrir, ponha senha no go2rtc.yaml e GO2RTC_API_PORT=1984 e/ou GO2RTC_RTSP_PORT=8554 no .env, como em [Abrir o go2rtc para a rede](https://github.com/mhagnumdw/dwnvr#abrir-o-go2rtc-para-a-rede-). ([801d37bd](https://github.com/mhagnumdw/dwnvr/commit/801d37bd682c46504efbdcdd32c93ebbe50e4033))

### Added

- feat(web): dwnvr instalável como app ([3f8f9e6c](https://github.com/mhagnumdw/dwnvr/commit/3f8f9e6ccb5000be6dcd0d4194721e43fc19b114))
- feat(api): sessão se renova com o uso ([5f289c5b](https://github.com/mhagnumdw/dwnvr/commit/5f289c5b265ed90674df5d51f2a986dd70e291a6))
- feat(changelog): abre as notas com o que pede ação ao atualizar ([c43b5eda](https://github.com/mhagnumdw/dwnvr/commit/c43b5eda0e52cdc7e1252b4fa76c4a3d5b11ed45))

### Fixed

- fix(compose)!: API e RTSP do go2rtc só abrem no próprio servidor ([801d37bd](https://github.com/mhagnumdw/dwnvr/commit/801d37bd682c46504efbdcdd32c93ebbe50e4033))
- fix(api): o proxy do ao vivo só repassa o websocket do player ([832d90b5](https://github.com/mhagnumdw/dwnvr/commit/832d90b5e8b33034215b263281df91b8f8888411))

### Docs

- docs: regra para marcar mudança incompatível no commit ([b667ea3c](https://github.com/mhagnumdw/dwnvr/commit/b667ea3caed31e5621d378664b84fc3275e61f1e))
- docs: guia de acesso remoto pelo Tailscale ([6709ff5f](https://github.com/mhagnumdw/dwnvr/commit/6709ff5f7569320cc7a0619d3f2c2ed84ff6a62a))

## [v0.5.0](https://github.com/mhagnumdw/dwnvr/compare/v0.4.0...v0.5.0) (2026-10-06)

### Added

- feat(web): leva ao repositório no GitHub pelo Diagnóstico e pelo login ([16b0bc94](https://github.com/mhagnumdw/dwnvr/commit/16b0bc94c76b6af552c48c40e86450c7ba4a38de))
- feat(web): limita o calendário de Detecções aos dias com detecção ([220348c3](https://github.com/mhagnumdw/dwnvr/commit/220348c36f1ce0f5d27bbbaa073508560384b6a7))
- feat(web): abre a detecção em Gravações pelo menu ([a111d850](https://github.com/mhagnumdw/dwnvr/commit/a111d850b672111844234f30f40392f5fff0112f))
- feat(web): leva da detecção para o ao vivo da câmera ([5f016e42](https://github.com/mhagnumdw/dwnvr/commit/5f016e42e08e462f878a51b23b97f9cfc7286f96))
- feat(config): valor fora da faixa não impede mais o dwnvr de subir ([#17](https://github.com/mhagnumdw/dwnvr/pull/17)) ([7cf97399](https://github.com/mhagnumdw/dwnvr/commit/7cf97399dd2a9468ef10edbb486a985e10ca459f))
- feat(diagnostico): mostra no Diagnóstico o próprio dwnvr e o detector de objetos ([c6d5aae1](https://github.com/mhagnumdw/dwnvr/commit/c6d5aae1637135be61775f8a42b853cb2f39eff8))
- feat(retention): mostra e avisa quanto há em gravações de câmeras removidas ([a8922dd0](https://github.com/mhagnumdw/dwnvr/commit/a8922dd06a3acba71eed32c905db0aa826789930))

### Fixed

- fix(web): câmeras do Ao vivo voltam em 1 s depois do segundo plano ([b28f288b](https://github.com/mhagnumdw/dwnvr/commit/b28f288bfd7937eb15f567be11fe91897237be7e))
- fix(store): solta da memória as detecções de um dia apagado ([663ba94b](https://github.com/mhagnumdw/dwnvr/commit/663ba94bc3c527cf4fec162024922de31c2ed82d))
- fix(store): recadastro com o mesmo ID lê as gravações que já estão no disco ([c2030b81](https://github.com/mhagnumdw/dwnvr/commit/c2030b8110948221de203208fa7f13f2608ba8c8))
- fix(changelog): não termina o CHANGELOG.md com 2 linhas em branco ([85831eaa](https://github.com/mhagnumdw/dwnvr/commit/85831eaa7c85a3766dc5479f93e21993f4d33569))

### CI/Build

- build(actions): fixa as actions por SHA e fecha o TODO de segurança ([30f8c36f](https://github.com/mhagnumdw/dwnvr/commit/30f8c36f464769114dba06824a1bfa753cdea486))
- build(deps): bump astral-sh/uv ([#13](https://github.com/mhagnumdw/dwnvr/pull/13)) ([649490af](https://github.com/mhagnumdw/dwnvr/commit/649490afe09743cbae09b20880b6b074b079b422))
- build(deps-dev): bump @sveltejs/vite-plugin-svelte ([#12](https://github.com/mhagnumdw/dwnvr/pull/12)) ([8efcce04](https://github.com/mhagnumdw/dwnvr/commit/8efcce0461d2e53a0e3fc8a9c9059cdb11911318))
- build(deps): bump devalue from 5.9.2 to 5.9.4 in /web ([#15](https://github.com/mhagnumdw/dwnvr/pull/15)) ([a784e51a](https://github.com/mhagnumdw/dwnvr/commit/a784e51a71a7bccd96ee2ac45f74fb3e1ec427eb))

## [v0.4.0](https://github.com/mhagnumdw/dwnvr/compare/v0.3.0...v0.4.0) (2026-10-02)

### Added

- feat(web): mostra o tempo gravado quando a câmera teve buraco ([cd8bd27a](https://github.com/mhagnumdw/dwnvr/commit/cd8bd27ac20dc376009a4c58003d33b980187ecd))
- feat(web): câmeras fora da tela param de puxar vídeo no Ao Vivo ([f0aa0c58](https://github.com/mhagnumdw/dwnvr/commit/f0aa0c5881419fb842669debf3b355b79cadf2e7))

### Docs

- docs(github): registra o merge commit desligado no repositório ([04480d87](https://github.com/mhagnumdw/dwnvr/commit/04480d87fc3456942c3c1c425f1ab383a902ebfa))
- docs(todo): remove os TODOs já concluídos ([6ccafa1f](https://github.com/mhagnumdw/dwnvr/commit/6ccafa1ffc1ff169ae448d9ec76e297f1eb69f32))

### CI/Build

- ci: cobertura dos testes no Codecov, com badge no README ([8a9e4213](https://github.com/mhagnumdw/dwnvr/commit/8a9e421311302e548c2b447c4a2a62bf7db2216b))

### Other

- test(retention): a política de apagar gravação ganha testes ([1a73fa34](https://github.com/mhagnumdw/dwnvr/commit/1a73fa34a1a81adff68f9c0488389ea4aca08c30))

## [v0.3.0](https://github.com/mhagnumdw/dwnvr/compare/v0.2.0...v0.3.0) (2026-09-30)

### Added

- feat(detect): escolhe as threads pelos núcleos da máquina ([841db69b](https://github.com/mhagnumdw/dwnvr/commit/841db69b8bd27949f3ba88c544e3e6886abbe422))
- feat(web): aviso de detector de objetos fora do ar no Diagnóstico ([11addd61](https://github.com/mhagnumdw/dwnvr/commit/11addd6147a49b2a4ff06a9c6059977ca3531258))

### Fixed

- fix(detect): sai na hora com o docker stop ([6a75587d](https://github.com/mhagnumdw/dwnvr/commit/6a75587d9836554b64a390943e3ab33dcf014ff1))
- fix(web): avisos do Diagnóstico acompanham o go2rtc sem recarregar ([cc335062](https://github.com/mhagnumdw/dwnvr/commit/cc3350627b7c28dcf0dfd26ec60b31b57f185aed))
- fix(api): dois saves de câmera ao mesmo tempo não se atropelam ([0b2e05f8](https://github.com/mhagnumdw/dwnvr/commit/0b2e05f8e5f0a0a57115855789aae7189553e73b))
- fix(recorder): troca de nome ou cota não corre com a gravação ([65818e86](https://github.com/mhagnumdw/dwnvr/commit/65818e8623b9936a68e5df444846ca6feecb0fdd))
- fix(store): segmento vazio da queda de energia sai da linha do tempo ([4ba56c10](https://github.com/mhagnumdw/dwnvr/commit/4ba56c105e5bfefda82d10a366e41569925ed08d))
- fix(detect): reinicia sozinho quando uma olhada trava ([84871755](https://github.com/mhagnumdw/dwnvr/commit/84871755e03c48aa1bc0dd1210dff965953ffad6))
- fix(go2rtc): chip ffmpeg não some com o stream em uso ([afb0719b](https://github.com/mhagnumdw/dwnvr/commit/afb0719bbf11bb02823f32ee8b64dee4dba69679))
- fix(retention): log da cota em KB e fora do Info ([03103364](https://github.com/mhagnumdw/dwnvr/commit/031033646914bab1e0eca90bc9be4c95fbda94b1))

### Changed

- refactor(detect): parâmetro quadro vira larguraDoQuadro ([66387bfe](https://github.com/mhagnumdw/dwnvr/commit/66387bfecfe60dc7f824580c2edf19c6f0e4d7ab))

### CI/Build

- ci: testes rodam com o detector de corrida ([259808a4](https://github.com/mhagnumdw/dwnvr/commit/259808a4a16d3a65d0c2b65db5c2a8da3b044689))

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
