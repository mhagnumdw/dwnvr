# TODO - servir o dwnvr por HTTPS para liberar o compartilhamento no celular

Observado em 19/08/2026, testando o botão `⤓ imagem` da tela de gravações no
Chrome do Android, contra o pitoco.

**Status: nada a corrigir no código. A decisão é de infraestrutura.**

## O que foi confirmado

No celular o botão **baixou o arquivo** em vez de abrir a folha de
compartilhamento do sistema. O caminho de compartilhamento existe em
`web/src/lib/captura.js` e está correto - ele só nunca é alcançado.

A causa é o endereço: o dwnvr é acessado em `http://pitoco:8080`, e a **Web
Share API é `[SecureContext]`**. Fora de `https://` ou `localhost`, o navegador
nem expõe `navigator.share` / `navigator.canShare`, então o teste do módulo
falha e o código cai no download - que é o comportamento desejado do fallback.

Não é o primeiro recurso a esbarrar neste mesmo portão. `VideoDecoder`
(WebCodecs), usado pelas miniaturas em `web/src/lib/thumbs.js`, também não
existe nesse endereço pela mesma razão, e por isso o caminho alternativo com
`<video>` escondido existe. Um portão, dois recursos degradados.

## Prós de resolver

- **Compartilhar é o gesto certo no celular.** Baixar joga a imagem numa pasta
  que ninguém abre; a folha do sistema leva direto para a galeria ou para a
  conversa em que a imagem seria útil. Foi por isso que o compartilhamento foi
  escolhido no desenho do botão.
- **As miniaturas ficariam mais leves.** Com contexto seguro, `thumbs.js` passa
  a decodificar por WebCodecs em vez do fallback com `<video>` escondido - que
  é o caminho lento e o único que depende de aba visível.
- Vale para qualquer recurso novo que dependa de contexto seguro (câmera,
  notificação, clipboard rico) - a lista só cresce com o tempo.
- A senha do dwnvr hoje trafega em claro na rede local.

## Contras

- **Nada quebra hoje.** O download funciona, e a imagem chega ao celular do
  mesmo jeito - com um passo a mais para achá-la.
- É mudança na rede dele, não no repositório: nenhuma linha de código muda.
- Um certificado do Tailscale só vale para quem entra pelo tailnet. Quem abre
  `http://pitoco:8080` pela LAN continua em contexto inseguro, então na prática
  passariam a existir dois endereços com comportamentos diferentes - o que é
  pior de explicar do que um comportamento só.

## Como implementar, se um dia valer

O caminho mais barato é o Tailscale, que já roda no pitoco com MagicDNS ligado
(sufixo `tailf74d98.ts.net`, confirmado em 19/08/2026) e hoje **não tem nenhum
serve configurado**:

```sh
# no pitoco, depois de habilitar HTTPS Certificates no admin do tailnet
tailscale serve --bg 8080
# passa a atender em https://pitoco.tailf74d98.ts.net, com certificado de verdade
```

O dwnvr não precisa saber de nada disso: `web/src/lib/api.js` monta as URLs
relativas (`api/...`) e o WebSocket do live deriva o esquema de
`location.protocol`, então `wss://` sai sozinho.

Alternativas, se o Tailscale não servir: proxy reverso com TLS (Caddy resolve o
certificado sozinho) na frente do `docker-compose.yml`, ou certificado próprio -
que traz de volta o aviso do navegador e não resolve contexto seguro sem
instalar a CA em cada aparelho.

## Vale a pena agora?

**Não.** O botão cumpre o que precisa cumprir nos dois lugares: no desktop
baixa, no celular baixa. A diferença é conveniência, não função.

O critério para reabrir é a frequência de uso pelo celular. Se compartilhar a
imagem da câmera pelo WhatsApp virar rotina, o `tailscale serve` é meia hora de
trabalho e melhora as miniaturas de brinde. Se a captura for coisa esporádica,
o download resolve e não há o que fazer.
