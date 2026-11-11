# Conversão para en-US

**Status (10/11/2026): planejado, com as [decisões](#as-decisões) tomadas.**

## O que é

O repositório inteiro em en-US, como um dev experiente e nativo da língua o
escreveria: identificadores, nomes de arquivo, comentários, testes, logs,
documentação, commits, notas de release e os contratos (rotas e JSON da API, o
que o dwnvr grava, as chaves do `dwnvr.yaml`, as variáveis do compose). Não é
tradução literal: o comentário que explica uma decisão vira o comentário que
esse dev escreveria para a mesma decisão.

Três coisas continuam em português:

- o chat com o mantenedor;
- o texto que o usuário vê na tela e nos e-mails, até o [i18n](i18n.md)
  (Etapa 5), que o extrai para o catálogo pt-BR e escreve o en-US. Traduzi-lo
  aqui deixaria a interface em inglês para quem a usa hoje;
- o README, que passa a existir nos dois idiomas: o `README.md` em en-US, que é
  o que o GitHub mostra, e o `README.pt-BR.md`, cada um com um link para o
  outro no topo. O README de hoje vira o `README.pt-BR.md`, já com os nomes
  novos.

## O tamanho

Contado em 10/11/2026, sem `internal/api/dist/`, `web/src/vendor/` e os
lockfiles:

| | Arquivos | Linhas |
| --- | --- | --- |
| Go | 123, 52 deles de teste | 31 mil, 14 mil de teste |
| Svelte | 42 | 18,8 mil |
| TypeScript | 33 | 5,8 mil |
| Python | 4 | 770 |
| Markdown | 45 | 8,7 mil |
| YAML | 18 | 1,8 mil |

Cerca de 67 mil linhas em 265 arquivos: a maior etapa do plano.

## O que muda

| O quê | Exemplos | Quem atualiza precisa agir? |
| --- | --- | --- |
| Identificadores, nomes de arquivo e de pacote, comentários, testes | o pacote `usuarios`, os `parametros.go`, o `Deteccoes.svelte` | Não |
| Logs | "conexão caiu" | Não, mas o Diagnóstico mostra o log: o admin passa a lê-lo em inglês |
| Rotas, parâmetros e JSON da API | `/api/deteccoes`, `antes`, `limite`, `instanteMs`, `deteccoesPorDia` | Só quem tem script. A interface vem no mesmo build |
| O que o dwnvr grava | as chaves `detectMecanismo`, `familia`, `quadroMs`, `temQuadro`; os valores `pessoa`, `veiculo`, `periodico`; os nomes `usuarios.json`, `acessos.json`, `avatares/`, `eventos/`, `quadros/` | Sim: o guia diz como passar ao formato novo |
| O `dwnvr.yaml` | `detectMecanismo`, `detectSensibilidade` e o valor `periodico`; o resto já está em inglês | Sim: o guia diz o que renomear |
| A env e a API do `dwnvr-detect` | `DETECT_MODELO`, `DETECT_PORTA`, o `piso` do `POST /detect` | Só quem definiu a env: o guia diz o que renomear |
| Documentação e exemplos | o `README.md` (o de hoje vira o `README.pt-BR.md`), os `docs/`, o `SECURITY.md`, o `dwnvr.example.yaml` (`troque-me`) | Não. Os links das releases antigas para seções do `README.md` deixam de achar a âncora, e a página abre no topo |
| Workflows | os nomes de workflow e de job (`imagens`, `cobertura`, `preparar`) | Não, mas o status check exigido pelo ruleset muda de nome junto, e o ruleset precisa ser atualizado |
| O resto | `cliff.toml` ("Pede ação ao atualizar"), `.betterleaks.toml` (`senha`, `troque-me`), `Makefile`, compose | Não |

## O que não muda

- O texto da tela e dos e-mails, até o i18n.
- O histórico do git: os commits antigos continuam em português.
- O `CHANGELOG.md` de antes da conversão: a release só acrescenta no topo
  (`--prepend`), e cada linha aponta para um commit em português.
- O `index/{dia}.ndjson`: as chaves já são abreviadas (`t`, `d`, `sz`) e não
  têm idioma.
- O que o usuário digitou: nome de câmera, nome de área.
- O chat com o mantenedor.

## Como, em passos que se testam

Cada passo é uma série de commits pequenos, e cada commit chega à `main` e roda
na instalação de teste, que segue a imagem `main`, antes do próximo. A release
só sai no fim da etapa, com o guia inteiro.

1. **O glossário.** O vocabulário do domínio se decide antes de qualquer
   rename, para o mesmo conceito ter o mesmo nome no Go, na interface, na API e
   nos docs. Decidido em 11/11/2026: [glossario.md](glossario.md).
2. **O `AGENTS.md`** em en-US, com a regra nova de escrita (en-US é o idioma
   de trabalho do projeto: o repositório inteiro, inclusive commits e notas de
   release; o chat em pt-BR; mudança num
   README vai no outro) e sem a regra "o que já existe em português fica como
   está".
3. **Go**, pacote a pacote. O rename de identificador pelo `gopls`, que entende
   o tipo e acha toda referência, e o resto à mão. As tags de JSON e de YAML
   ficam como estão até o passo 6: para quem usa, nada muda, e a instalação de
   teste confirma isso a cada pacote.
4. **A interface**, uma tela ou um punhado de componentes por vez: rotas,
   componentes, `lib/`, identificadores e comentários. Os textos da tela ficam.
   O `svelte-check` é a rede.
5. **O `dwnvr-detect`**: o `servidor.py`, a pasta `modelo/`, os comentários.
6. **Os contratos e o guia, em partes**, porque é o que quebra: as tags de JSON
   e de YAML, as rotas, os valores das enumerações, os nomes de arquivo e de
   pasta, a env. Sem compatibilidade (ver
   [o plano](plano.md#sem-compatibilidade-com-um-guia)): a versão nova só lê
   o formato novo. Cada parte (por exemplo: a API; o `dwnvr.yaml` e a env; o
   `cameras.json` e os arquivos das pessoas; os eventos e os quadros) traz a
   sua seção do guia de atualização, que se aplica à risca na instalação de
   teste antes da parte seguinte. Aqui também se decidem os pontos que o plano
   deixou para esta etapa (comandos no guia ou flag de migração; o aviso para
   quem não seguiu o guia), e se cada arquivo que o dwnvr grava ganha um número
   de versão, para a versão seguinte reconhecer um formato antigo em vez de
   lê-lo errado.
7. **A documentação, os exemplos, os workflows e o resto**: os `docs/`, os
   `fase*-resultados.md`, os `docs/TODO/` que o mantenedor mantiver
   (decisão 5), os exemplos de yaml, o compose, o `Makefile`, o `cliff.toml`. E
   o README: o de hoje vira o `README.pt-BR.md`, e o `README.md` se reescreve
   em en-US. Vem depois dos contratos para cada doc ser traduzido uma vez só, já
   com os nomes finais.
8. **A varredura e o blame.** Um `git grep` por palavras portuguesas comuns (a
   lista sai do glossário, e o `README.pt-BR.md` fica de fora) acha o que
   sobrou, e os commits de rename entram no `.git-blame-ignore-revs`, como o do
   Prettier, para o `git blame` continuar apontando a mudança de verdade.

## Riscos

- **Trabalho em paralelo vira conflito em tudo.** Por isso a Etapa 1 (terreno
  limpo), e nenhuma feature durante a conversão.
- **A chave renomeada de um lado só.** O TypeScript confere a interface contra
  os tipos do `web/src/lib/api.ts`, e não contra o servidor. O teste Go que lê
  o JSON de cada rota pega o lado do servidor; a instalação de teste pega o
  encontro dos dois.
- **O guia é o passo com risco de perda.** Começa pelo backup, e cada parte é
  testada com os dados reais da instalação de teste.
- **Os dois READMEs divergem com o tempo.** A regra do passo 2 está na tabela
  §Ripple effects do `AGENTS.md`: mexeu num, confira o outro.

## As decisões

Tomadas pelo mantenedor em 10/11/2026:

1. **O texto da tela e dos e-mails espera o i18n**: a conversão não o traduz.
2. **O log passa a ser só en-US**, fora do i18n. O Diagnóstico mostra o log ao
   admin em inglês.
3. **O `CHANGELOG.md` de antes da conversão fica em português.**
4. **Commits e notas de release em en-US**, a partir da conversão. A parte 1 do
   body, a do usuário leigo, continua, em inglês.
5. **Os `docs/TODO/` em aberto passam antes pelo mantenedor**, e só os que
   seguem valendo se traduzem. O da tela global, por exemplo, está desatualizado
   (ver [configuracao-global.md](configuracao-global.md)).
6. **Os `docs/fase*-resultados.md` se traduzem.**
7. **Sem compatibilidade com a versão anterior**: nem nome antigo aceito, nem
   alias na API. Quem atualiza segue o guia (ver
   [o plano](plano.md#sem-compatibilidade-com-um-guia)).
8. **O README em dois idiomas**: o `README.md` em en-US, que o GitHub mostra
   por padrão, e o `README.pt-BR.md`. O resto da documentação fica só em
   en-US.
9. **Em vários commits**, cada um testado na instalação de teste antes do
   próximo.
