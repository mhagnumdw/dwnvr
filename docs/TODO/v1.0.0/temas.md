# Temas

**Status (10/11/2026): planejado.**

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

**~115 cores escritas direto, em 24 arquivos**, contadas em 10/11/2026 fora do
`app.css`. São de três tipos, e cada um pede uma coisa:

| Tipo | Exemplos | O que fazer |
| --- | --- | --- |
| Sobre o vídeo | o fundo preto do player, o texto branco e as sombras sobre a imagem (`Live`, `Recordings`, `ThumbStrip`, `DeteccaoFolha`, `EditorDeArea`) | Nada: o vídeo é escuro em qualquer tema, e o que fica sobre ele também |
| Variação de uma cor do tema | o `--accent` com transparência (`rgba(47, 129, 247, 0.12)` no `App.svelte`), o vermelho escuro da borda do `danger` (`#5c2b2b`), o amarelo escuro da borda de aviso (`#6b5a1f`), os cinzas do Diagnóstico (`#1b1f24`, `#3d444d`) | Vira token, ou `color-mix()` sobre o token |
| Desenho em canvas | a timeline (`FUNDO`, `CALOR_COR`), as caixas (`lib/caixas.ts`), as áreas (`lib/area.ts`), o avatar | O desenho lê os tokens do CSS e redesenha quando o tema muda |

As cores das famílias (pessoa verde, veículo roxo, animal rosa, em
`web/src/lib/icones.ts`) aparecem sobre o vídeo e também nas listas, sobre o
fundo da tela: no tema claro, o contraste delas precisa de conferência.

## As paletas

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

## Decidir na etapa

- **No "sistema", que par ele alterna?** Recomendo claro e escuro, fixos.
  Deixar escolher o par, como o GitHub deixa, é enfeite até alguém pedir.
- **Onde a escolha fica?** Recomendo no aparelho (`localStorage`), com
  "sistema" como padrão: vale na tela de login, que ainda não sabe quem é a
  pessoa, e não pede rota nem campo novo no servidor. Guardada na pessoa, no
  servidor, a escolha a seguiria de um aparelho para outro, ao preço de uma
  rota e de um campo no cadastro.
- **Onde fica o seletor**, e o nome de cada tema na tela.

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
