# Interface do dwnvr

Svelte 5 + Vite. O build sai para `../internal/api/dist`, que o Go embute com
`embed.FS` — é isso que mantém a promessa de um binário único.

```sh
npm install
npm run build       # gera internal/api/dist, depois: go build ./cmd/dwnvr
npm run dev         # desenvolvimento com recarga instantânea
```

No modo `dev` o Vite faz proxy de `/api` para um dwnvr de verdade, então a tela
é construída contra dados reais desde o primeiro minuto:

```sh
DWNVR_API=http://servidor.local:8080 npm run dev
```

## Por que `internal/api/dist` é versionado

`go:embed` exige que os arquivos existam em tempo de compilação. Versionar o
build faz `go build ./...` funcionar num clone limpo, sem Node instalado — o que
importa porque o alvo é um dispositivo onde ninguém quer instalar toolchain de
frontend.

Ao alterar algo em `web/`, rode `npm run build` **antes** de commitar.

## Estrutura

```
src/lib/         api, estado (runes), formatadores, player MSE, miniaturas
src/routes/      as quatro telas + login
src/components/  timeline em canvas, tira de miniaturas
src/vendor/      player de live do go2rtc (MIT) — ver vendor/README.md
```
