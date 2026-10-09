# TODO - o header do admin não cabe abaixo de ~900 px

Achado em 09/10/2026, no mock da navegação do cadastro de usuários, e medido de
novo no header de verdade quando a aba Usuários entrou. Decidido resolver só
quando incomodar no uso.

**Status: não corrigido.**

## O que acontece

Da largura de 720 px para cima, as abas sobem para o header. O admin tem seis
(Ao vivo, Gravações, Detecções, Câmeras, Usuários e Diagnóstico) e, à direita,
o sino de avisos, a pílula de versão nova e o Sair. Com tudo isso à vista:

| Largura | O que se vê |
| --- | --- |
| 1024 px | cabe |
| 900 px | cabe, mas o "Ao vivo" quebra em duas linhas |
| 820 px (iPad em pé) | o header passa 61 px da tela, e o Sair sai do quadro |
| 760 px | passa 121 px |

Sem o detector de objetos (cinco abas), ou sem o sino ou a pílula, sobra mais
espaço. O usuário comum, com quatro abas e sem sino nem pílula, não tem o
problema. Abaixo de 720 px as abas descem para o rodapé, e o header fica só
com a marca, o sino, a pílula e o Sair.

Quando o avatar entrar no lugar do Sair (Etapa 3 do cadastro de usuários), ele
é que sai do quadro, e com ele o único caminho para a Minha conta e o Sair.

## Como foi medido

O dwnvr local com uma câmera sintética, o Chrome headless em cada largura, e a
aba Detecções e a pílula postas na página com as mesmas classes do
`App.svelte`, porque o teste não tinha detector nem versão nova. O número é
`scrollWidth - clientWidth` do `<header>`.

## A ideia

Um ⋮ no fim do header com o que não couber, como os menus de ⋮ que a interface
já tem. Antes de desenhar, decidir com um mock o que vai para dentro dele
primeiro: as abas menos usadas (Usuários, Câmeras) ou os botões da direita.
