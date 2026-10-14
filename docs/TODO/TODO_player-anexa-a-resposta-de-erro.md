# TODO - o player anexa a resposta de erro como se fosse vídeo

Na tela Gravações, quando o servidor responde com erro a um pedaço de vídeo
(o arquivo sumiu, a sessão expirou, o dwnvr está reiniciando), o player não
percebe o erro: ele entrega a mensagem de erro ao navegador como se fosse
vídeo. A tela mostra um aviso que não diz o que aconteceu, ou o vídeo para.

Achado em 14/10/2026, lendo o código durante a correção da troca de câmera
que esperava o download anterior. **Não foi medido nem reproduzido.**

**Status: não corrigido.**

## O que foi confirmado, pelo código

Em `web/src/lib/player.svelte.ts`, nenhum dos dois `fetch` confere `res.ok`:

- **`#mimeFor`** lê o corpo do init e procura os nomes dos codecs nele. Um
  corpo de erro é o JSON do `writeError` (`{"error": "..."}`), sem codec
  nenhum: o mime sai `video/mp4; codecs=""`, o `isTypeSupported` recusa e a
  tela diz "este navegador não reproduz video/mp4; codecs=""" - culpa o
  navegador por um erro do servidor.
- **`#append`** faz `fetch(url).then((r) => r.arrayBuffer())` e anexa o que
  vier ao `SourceBuffer`. O JSON de erro vira "falha ao anexar segmento", ou
  um erro de decodificação que o `#onError` tenta pular de segundo em segundo.

## Quando acontece

- **Segmento apagado com a tela aberta:** a retenção (`maxDays` ou cota)
  apaga o começo do dia mais antigo enquanto alguém o assiste. A timeline na
  tela ainda lista o segmento e o pedido volta 404. O init não some: a
  retenção não apaga `init/`.
- **Sessão expirada:** as rotas de mídia estão no `requireAuth`, e a resposta
  é 401.
- **dwnvr reiniciando** atrás do Caddy ou do `tailscale serve`: o proxy
  responde 502 com corpo HTML.

## A ideia

Conferir `res.ok` nos dois `fetch` e lançar um erro com o status, que o
`catch` do seek e do pump já levam para a tela. O 401 talvez mereça o mesmo
tratamento das outras telas (voltar ao login), a conferir em `api.ts`.

## Prós

- A mensagem passa a dizer o que aconteceu, em vez de culpar o navegador.
- Nada de lixo no `SourceBuffer`, e o `#onError` não sai pulando à toa.

## Contras

- Os três casos são raros, e nenhum foi visto no uso real até aqui.

## Vale a pena agora?

Não com urgência. É pequeno e de baixo risco; cabe junto da próxima mexida
no player.
