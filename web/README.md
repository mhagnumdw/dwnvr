# Interface do dwnvr

Quatro telas, em Svelte 5 + TypeScript + Vite, embutidas no binário: **ao vivo**,
**gravações**, **câmeras** e **diagnóstico**. O aplicativo inteiro pesa
**44,3 kB gzipped**, incluindo o player de live do go2rtc.

O build sai para `../internal/api/dist`, que o Go embute com `embed.FS` - é
isso que mantém a promessa de um binário único.

```sh
npm install
npm run build       # gera internal/api/dist, depois: go build ./cmd/dwnvr
npm run dev         # desenvolvimento com recarga instantânea
npm run check       # confere os tipos; o lint do commit roda o mesmo
```

No modo `dev` o Vite faz proxy de `/api` para um dwnvr de verdade, então a tela
é construída contra dados reais desde o primeiro minuto. O default é
`http://localhost:8080`; para apontar a tela para outra instalação:

```sh
DWNVR_API=http://servidor:8080 npm run dev
```

O endereço em uso aparece no arranque do Vite - vale conferir antes de concluir
que algo está quebrado, porque a tela apontada para o servidor errado tem a
mesma aparência de uma tela funcionando.

## Por que `internal/api/dist` é versionado

`go:embed` exige que os arquivos existam em tempo de compilação. Versionar o
build faz `go build ./...` funcionar num clone limpo, sem Node instalado - o que
importa porque o alvo é um dispositivo onde ninguém quer instalar toolchain de
frontend.

Ao alterar algo em `web/`, rode `npm run build` **antes** de commitar. Sem
isso, o hook `interface-atualizada` do prek recusa o commit.

## Estrutura

```text
src/lib/         api e os tipos do JSON dela, estado (runes), rota e
                 estado na URL, formatadores, a máscara do horário
                 digitado,
                 player MSE, miniaturas, captura de quadro, ícones das
                 famílias de objeto (canvas), os ícones da interface (o
                 mapa nome -> desenho do `<Icone>`), disposição e desenho
                 das caixas,
                 diagnóstico do navegador de quem está olhando e do
                 servidor que grava, avisos do diagnóstico (o card e o
                 sino do header), a redução da foto da Minha conta, o
                 contexto 2D do canvas, a tela cheia (simulada por CSS onde
                 o navegador não a faz, como no iPhone), os controles que
                 somem sozinhos sobre o vídeo nela, o aviso de que a tela
                 aberta ficou para trás de uma atualização, a snackbar (o
                 aviso curto embaixo da tela), a dica que o toque longo
                 abre num botão e a altura que um elemento tem até o fim
                 da janela
src/routes/      as telas, o login e a de definir a senha pelo link de
                 convite; Detecções, Usuários, Minha conta e a do convite
                 são chunk à parte. Detecções só aparece com o detector de
                 objetos configurado, Câmeras e Usuários só para o admin, e
                 a Minha conta não tem aba: abre pelo avatar do header
src/components/  timeline em canvas, tira de miniaturas, relógio que aceita
                 horário digitado, o campo de horário e o intervalo de
                 horário de Detecções (o botão, o chip e os campos De e
                 Até), modal e confirmação, a fileira de abas de um
                 formulário,
                 o estado de "nenhuma câmera cadastrada", seletor de dia e as
                 setas de dia anterior e próximo,
                 caixas do detector sobre o vídeo (chunk à parte), a folha
                 que abre uma detecção com o quadro e o trecho gravado,
                 o card fechável de diagnóstico com o "copiar",
                 o campo de senha com o olho, o avatar (a foto ou as
                 iniciais), o botão que abre a lista de câmeras (marcadas
                 do total), o botão de entrar e sair da tela cheia, o
                 `<Icone>` que desenha qualquer ícone da interface pelo nome,
                 a snackbar e o selo das famílias de objeto no canto de uma
                 miniatura
src/vendor/      player de live do go2rtc (MIT), em JavaScript, com os tipos
                 dele escritos à mão - ver vendor/README.md
```

## Decisões

**TypeScript no rigor máximo.** O `tsconfig.base.json` liga o conjunto do
`@tsconfig/strictest`: `strict`, `noUncheckedIndexedAccess` (todo `lista[i]`
pode ser `undefined`), `exactOptionalPropertyTypes` (campo opcional não aceita
`undefined` escrito) e o resto. O `erasableSyntaxOnly` deixa de fora o que não
é só tipo apagável - `enum`, `namespace` -, porque o Svelte só apaga os tipos do
`<script lang="ts">`, sem preprocess. Por isso não há `svelte.config`.

São dois projetos, como no template `svelte-ts` do Vite: o
`tsconfig.app.json` para a tela, conferido pelo svelte-check, e o
`tsconfig.node.json` para as configs, que rodam no Node, conferido pelo `tsc`.
O ESLint usa as regras que leem os tipos (`strictTypeChecked` e
`stylisticTypeChecked` do typescript-eslint), e o `svelte/block-lang` recusa
componente sem `lang="ts"`. As exceções estão no `eslint.config.ts`, cada uma
com o motivo.

A formatação é do Prettier, com o plugin do Svelte, no `prettier.config.ts`:
aspas simples e linha de até 100, que era o que o código já fazia. Trecho
arrumado à mão de propósito, como os desenhos dos ícones, leva um
`// prettier-ignore` em cima.

O contrato com o servidor fica no fim do `src/lib/api.ts`: o JSON de cada
resposta, escrito a partir das structs Go de `internal/api`. O TypeScript não
confere o que chega pela rede, então mudar o JSON de um lado pede mudar o tipo
do outro.

O TypeScript é o 6, e não o 7: o svelte-check ainda só usa o 7 ao lado do 6,
com a flag `--tsgo`, e o Dependabot ignora o major até isso mudar (ver
`.github/dependabot.yml`).

**Mobile-first.** Navegação inferior no celular e superior no desktop. A grade
ao vivo aceita 1, 2 ou 3 colunas ou "encaixar tudo na tela" - escolha do
usuário, salva no `localStorage`. O padrão inicial depende da largura da tela
(2 colunas acima de 640 px, 1 abaixo), mas nada trava: no celular também dá
para pedir 3 colunas.

No modo "encaixar", a quantidade de colunas não é escolhida - é calculada. Para
cada número possível, mede-se o maior tile 16:9 que caberia considerando também
a altura das linhas resultantes, e vence o que produzir o tile maior. Como a
altura entra na conta, a grade nunca transborda a janela.

**Estado da tela na URL.** O hash não diz só em que tela você está, mas o que
você está vendo dentro dela: `#live?cams=garagem&cams=entrada&view=2`,
`#rec?cam=garagem&day=2026-08-19&t=14:32:07&rate=4&paused=1`. Copiar a barra de
endereços e colar em outra aba reproduz a mesma cena. Segue sendo hash, e não
caminho de verdade, porque o fragmento nunca sai do navegador - assim o
`internal/api/web.go`, que só serve arquivo, não precisa de rota nova.

Quem cuida disso é o `src/lib/rota.svelte.ts`. Cada tela lê a URL uma vez, ao
montar, e um `$effect` escreve de volta o que o estado real diz - `paused` sai
de `player.playing`, e não de um sinalizador paralelo, para o endereço nunca
prometer uma reprodução que o navegador bloqueou. As escritas usam
`replaceState` com piso de um segundo, porque o instante do player muda várias
vezes por segundo e o Safari recusa mais de 100 em 30s. Onde a URL e o
`localStorage` falam da mesma coisa - seleção e layout do Ao vivo -, a URL
manda; e só clique do usuário grava no `localStorage`, para o link de outra
pessoa não virar a sua preferência.

**Timeline com Pointer Events**, que cobre mouse, dedo e caneta com o mesmo
código. Arrastar desliza a janela visível, tocar navega, pinçar e roda do mouse
dão zoom, duplo toque aproxima e toque com dois dedos afasta. Um arraste nunca
vira navegação: o gesto é descartado se o ponteiro andou além do limiar - que é
maior no dedo (8 px) do que no mouse (2 px), porque dedo treme.

**Player MSE escrito à mão, em vez de hls.js.** É o que segura o tamanho: só a
biblioteca custaria ~110 kB gzip, mais do que o aplicativo inteiro. Em troca,
ganhamos controle exato sobre a janela de buffer e sobre os buracos de
gravação, que o índice já conhece.

**Instalável como app, sem service worker.** O `public/manifest.json` e os
ícones fazem o Chrome oferecer "Instalar app" e o iPhone abrir o atalho da tela
de início sem barra de endereço. Para isso o endereço precisa ser `https://`;
em `http://` o Android só cria um atalho que abre numa aba. Não há service
worker: instalar não exige um, e ele traria o risco de servir a versão velha da
tela depois de uma atualização. Para quem não instala, o custo é o manifest,
menos de 1 kB; os ícones só baixam na instalação.

Os PNGs saem do `favicon.svg`. Os de uso geral (`icon-192`, `icon-512`) são o
desenho como está, com a placa arredondada. O maskable e o `apple-touch-icon`
levam a placa até a borda, porque quem recorta é o sistema (círculo, gota ou o
canto do iPhone); a lente ocupa 64% da largura e cabe na zona segura de 80%.
Para regerar, com ImageMagick, pngquant e oxipng (a densidade é 96 dpi vezes o
tamanho desejado sobre os 64 px do SVG). O pngquant troca as cores RGBA
completas por uma paleta: corta ~70% do tamanho, e a diferença fica em torno
de 68 dB de PSNR, invisível mesmo ampliada.

```sh
cd web/public
sed 's/ rx="14"//' favicon.svg > /tmp/cheio.svg
magick -background none -density 288 favicon.svg -strip icon-192.png
magick -background none -density 768 favicon.svg -strip icon-512.png
magick -density 768 /tmp/cheio.svg -alpha off -strip icon-maskable-512.png
magick -density 270 /tmp/cheio.svg -alpha off -strip apple-touch-icon.png
pngquant --quality=90-100 --speed 1 --strip --ext .png --force icon-*.png apple-touch-icon.png
oxipng -q -o max -Z --strip all icon-*.png apple-touch-icon.png
```

**Player de live copiado do go2rtc**, não escrito. No live o formato é do
go2rtc e o problema já está resolvido - inclusive para H265, que é o caso
difícil desta instalação. Ver [`src/vendor/README.md`](src/vendor/README.md).
