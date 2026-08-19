# Interface do dwnvr

Quatro telas, em Svelte 5 + Vite, embutidas no binário: **ao vivo**,
**gravações**, **câmeras** e **diagnóstico**. O aplicativo inteiro pesa
**43,2 kB gzipped**, incluindo o player de live do go2rtc.

O build sai para `../internal/api/dist`, que o Go embute com `embed.FS` - é
isso que mantém a promessa de um binário único.

```sh
npm install
npm run build       # gera internal/api/dist, depois: go build ./cmd/dwnvr
npm run dev         # desenvolvimento com recarga instantânea
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

Ao alterar algo em `web/`, rode `npm run build` **antes** de commitar.

## Estrutura

```
src/lib/         api, estado (runes), formatadores, player MSE, miniaturas,
                 captura de quadro
src/routes/      as quatro telas + login
src/components/  timeline em canvas, tira de miniaturas
src/vendor/      player de live do go2rtc (MIT) - ver vendor/README.md
```

## Decisões

**Mobile-first.** Navegação inferior no celular e superior no desktop. A grade
ao vivo aceita 1, 2 ou 3 colunas ou "encaixar tudo na tela" - escolha do
usuário, salva no `localStorage`. O padrão inicial depende da largura da tela
(2 colunas acima de 640 px, 1 abaixo), mas nada trava: no celular também dá
para pedir 3 colunas.

No modo "encaixar", a quantidade de colunas não é escolhida - é calculada. Para
cada número possível, mede-se o maior tile 16:9 que caberia considerando também
a altura das linhas resultantes, e vence o que produzir o tile maior. Como a
altura entra na conta, a grade nunca transborda a janela.

**Timeline com Pointer Events**, que cobre mouse, dedo e caneta com o mesmo
código. Arrastar desliza a janela visível, tocar navega, pinçar e roda do mouse
dão zoom, duplo toque aproxima e toque com dois dedos afasta. Um arraste nunca
vira navegação: o gesto é descartado se o ponteiro andou além do limiar - que é
maior no dedo (8 px) do que no mouse (2 px), porque dedo treme.

**Player MSE escrito à mão, em vez de hls.js.** É o que segura o tamanho: só a
biblioteca custaria ~110 kB gzip, mais do que o aplicativo inteiro. Em troca,
ganhamos controle exato sobre a janela de buffer e sobre os buracos de
gravação, que o índice já conhece.

**Player de live copiado do go2rtc**, não escrito. No live o formato é do
go2rtc e o problema já está resolvido - inclusive para H265, que é o caso
difícil desta instalação. Ver [`src/vendor/README.md`](src/vendor/README.md).
