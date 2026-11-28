# Temas

**Status (28/11/2026): concluído**, na
[v0.17.0](https://github.com/mhagnumdw/dwnvr/releases/tag/v0.17.0). O passo 1
(toda cor num token) foi o PR #101, o passo 2 (as paletas e o script de
contraste) o PR #102, e o passo 3 (o seletor) o PR #103.

## O que é

Cinco escolhas: claro, pouco claro, escuro, pouco escuro e "de acordo com o
sistema" (`prefers-color-scheme`), que é o padrão.

## O que já existe

- O tema de hoje é um só, escuro, e nasce de 11 cores em variáveis no `:root`
  do [`web/src/app.css`](../../../web/src/app.css) (`--bg`, `--panel`,
  `--panel-2`, `--line`, `--fg`, `--dim`, `--accent`, `--ativo-fundo`, `--ok`,
  `--warn`, `--bad`), usadas ~385 vezes. É a paleta escura do GitHub (Primer).
  Para o que já passa por elas, trocar o tema é trocar os valores.
- O `web/index.html` declara `color-scheme: dark` e um `theme-color` fixos, e o
  `web/public/manifest.json` tem as cores fixas.

## O que falta

**122 cores escritas direto, em 25 arquivos**, recontadas em 27/11/2026 contra
o código depois da conversão (118 fora do `app.css`, em 24 arquivos). São de
três tipos, e cada um pede uma coisa:

| Tipo | Exemplos | O que fazer |
| --- | --- | --- |
| Sobre o vídeo (~45) | o fundo preto do player, o texto branco e as sombras sobre a imagem, os selos translúcidos (`Live`, `Recordings`, `ThumbStrip`, `DetectionSheet`, `AreaEditor`, `FamilyBadge`), o contorno das caixas (`lib/boxes.ts`) | Fica: o vídeo é escuro em qualquer tema, e o que fica sobre ele também. O contêiner ganha a classe `.over-video`, que fixa ali a paleta escura, porque o que está sobre o vídeo também lê `--fg`, `--line` e `--bad` |
| Variação de uma cor do tema (~60) | o `--accent` com transparência, a borda do `danger` (`#5c2b2b`) e a de aviso (`#6b5a1f`), os cinzas do Diagnóstico, o texto sobre o accent, sobre o warn e sobre cor forte, as sombras, o fundo do modal | Vira token, ou `color-mix()` sobre o token |
| Desenho em canvas | a timeline, as áreas (`lib/area.ts`) | O desenho lê o token no próprio canvas (`lib/theme.ts`), na hora de desenhar |
| Fixas | as 6 cores do avatar, com texto branco | Ficam: valem nos dois fundos |

As cores das famílias (pessoa verde, veículo roxo, animal rosa, em
`web/src/lib/icons.ts`) têm duas versões: a viva de hoje, sobre o vídeo e igual
em todo tema, e um token por tema (`--person`, `--vehicle`, `--animal`) nas
listas, nos filtros e na timeline: o verde de hoje dá ~2,4:1 sobre o branco.

## As paletas

Escolhidas em 27/11/2026 sobre um mock das quatro lado a lado. O escuro é o de
hoje, menos o fundo do botão primário: o branco sobre o `#2f81f7` dá 3,75:1, e
o `--accent-fill` (`#1f6feb`) dá 4,63:1. O pouco escuro parte do *dark dimmed*
do GitHub, que reprovava em seis pares e teve o texto e as cores clareados. O
pouco claro é desenhado, na metade do caminho entre o claro e o pouco escuro.

| Token | Escuro | Pouco escuro | Pouco claro | Claro |
| --- | --- | --- | --- | --- |
| `--bg` | `#0d1117` | `#22272e` | `#c4ccd5` | `#ffffff` |
| `--panel` | `#161b22` | `#2d333b` | `#d5dbe2` | `#f6f8fa` |
| `--panel-2` | `#1c2129` | `#323942` | `#cbd2da` | `#eff2f5` |
| `--line` | `#30363d` | `#444c56` | `#a2adb9` | `#d1d9e0` |
| `--line-soft` | `#21262d` | `#373e47` | `#b7c0ca` | `#e6eaef` |
| `--fg` | `#e6edf3` | `#c5d1de` | `#171b20` | `#1f2328` |
| `--dim` | `#8b949e` | `#9aa6b3` | `#3c4550` | `#59636e` |
| `--faint` | `#4a5058` | `#545d68` | `#7f8a96` | `#aab2bb` |
| `--accent` | `#4493f8` | `#6cb6ff` | `#0850a3` | `#0969da` |
| `--accent-fill` | `#1f6feb` | `#316dca` | `#0850a3` | `#0969da` |
| `--on-accent` | `#fff` | `#fff` | `#fff` | `#fff` |
| `--active-bg` | `#12233d` | `#263a55` | `#adc6e6` | `#ddf4ff` |
| `--ok` | `#3fb950` | `#6bc46d` | `#125f2b` | `#1a7f37` |
| `--warn` | `#d29922` | `#daaa3f` | `#6b4700` | `#9a6700` |
| `--on-warn` | `#1b1300` | `#1c1400` | `#fff` | `#fff` |
| `--bad` | `#f85149` | `#ff7b72` | `#a01822` | `#d1242f` |
| `--on-bad` | `#0d1117` | `#1c2128` | `#fff` | `#fff` |
| `--on-strong` | `#0d1117` | `#1c2128` | `#fff` | `#fff` |
| `--track` | `#1b1f24` | `#2a3038` | `#b9c2cc` | `#eaeef2` |
| `--track-strong` | `#3d444d` | `#545d68` | `#8b96a2` | `#b7bfc8` |
| `--person` | `#3fb950` | `#57ab5a` | `#125f2b` | `#1a7f37` |
| `--vehicle` | `#a371f7` | `#986ee2` | `#5d36ad` | `#8250df` |
| `--animal` | `#db61a2` | `#c96198` | `#8c2869` | `#bf3989` |
| `--shadow` | `rgba(0, 0, 0, 0.5)` | `rgba(0, 0, 0, 0.45)` | `rgba(31, 35, 40, 0.24)` | `rgba(31, 35, 40, 0.15)` |
| `--backdrop` | `rgba(0, 0, 0, 0.6)` | `rgba(0, 0, 0, 0.55)` | `rgba(31, 35, 40, 0.5)` | `rgba(31, 35, 40, 0.4)` |

No `web/src/app.css`, o escuro é o bloco `:root, .over-video`, e cada uma das
outras é um bloco `:root[data-theme='…']` (`dark-dim`, `light-dim`, `light`),
só no `:root`: o que fica sobre o vídeo segue sempre o escuro. O passo 3 põe o
atributo no `<html>`; até lá, uma paleta se vê pelo DevTools, com
`document.documentElement.dataset.theme = 'light'`.

Duas mudanças no escuro, decididas em 27/11/2026 pelo script: o `--on-bad`, o
número da bolinha vermelha do sino, era branco e dava 3,35:1, e virou texto
escuro (5,65:1), com o vermelho igual ao do resto da tela; e o `--accent` sai do
`#2f81f7`, que dava 4,31:1 sobre o `--panel-2` (a contagem do dia no hover do
calendário, um texto de Câmeras), para o `#4493f8`, o azul de texto do escuro
do GitHub de hoje (5,22:1).

O `web/scripts/contrast.mjs` lê as paletas do `app.css` e confere cada par de
texto e fundo que a tela usa (WCAG AA: 4,5:1 para texto, 3:1 para ícone); o
hook `contrast` do prek roda o script quando o `app.css` muda, e a CI roda o
mesmo arquivo. Um par novo na tela entra na lista do script. O número da fatia
"descartada" do funil do Diagnóstico ficou de fora: dá ~3:1 em todas as
paletas, o escuro de hoje inclusive, e está no
[TODO](../TODO_funnel-dropped-contrast.md).

`--bad-line` e `--warn-line`, as bordas do `danger` e do aviso, saem do
`--bad` e do `--warn` por `color-mix()` com o `--bg`, e nenhuma paleta as
define.

- Cada paleta define todos os tokens, inclusive `--ok`, `--warn` e `--bad`: o
  amarelo de hoje (`#d29922`) tem pouco contraste sobre o branco, e o claro
  precisa de outro.
- O contraste de cada par de texto e fundo se confere por script (WCAG AA,
  4,5:1 para texto), e não no olho.
- O pouco escuro tem equivalente pronto no GitHub, o *dark dimmed*; o pouco
  claro não, e se desenha.
- Os tokens se desenham já pensando na [paleta customizável](paleta.md): quanto
  mais tokens saem de poucas cores-base por `color-mix()`, menos a paleta
  precisa definir.

## Decidido em 27/11/2026

- **No "sistema", o par é claro e escuro, fixos.** Deixar escolher o par, como
  o GitHub deixa, é enfeite até alguém pedir.
- **A escolha fica no aparelho** (`localStorage`), com "sistema" como padrão:
  vale na tela de login, que ainda não sabe quem é a pessoa, e não pede rota
  nem campo novo no servidor.
- **As famílias têm duas cores**: a viva sobre o vídeo, igual em todo tema, e
  um token por tema na tela.
- **O seletor fica na tela Minha conta.**
- **O `background_color` do `manifest.json` fica o escuro**: a tela de abertura
  do app instalado é uma só.

## Os passos

Um PR por passo, com merge por squash, na instalação de teste antes de aceito.

1. Toda cor passa por um token, sem mudança visível no escuro além do botão
   primário.
2. As outras três paletas e o script de contraste no repositório.
3. O seletor, a escolha no `localStorage`, o script que aplica o tema antes da
   primeira pintura, o `theme-color` e o canvas redesenhando quando o tema
   muda.

## Cuidados

- **Sem flash na abertura.** O tema se aplica antes da primeira pintura, por um
  script curto no `index.html` (o dwnvr não manda CSP hoje, então o script
  inline passa). Sem isso, quem usa o claro vê o escuro piscar a cada abertura.
- **A barra do sistema no app instalado** segue o `theme-color`, que o mesmo
  script troca. A tela de abertura do app instalado usa o `background_color`
  do `manifest.json`, que é um só.
- **O e-mail de alerta não segue o tema**: o cliente de e-mail tem as próprias
  regras de claro e escuro.
- **Custo zero**: é CSS e umas linhas de JS, sem pedido a mais ao servidor.
