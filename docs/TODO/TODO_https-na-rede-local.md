# TODO - HTTPS na rede local, sem o Tailscale

Pedido em 07/10/2026, ao revisar como a senha do login é guardada. Vira uma doc
para quem instala, numa atividade própria.

**Status: não começado.** Nada aqui foi testado.

## Por quê

O dwnvr só serve HTTP. Pelo `http://<ip-da-máquina>:8080` da rede de casa, a
senha vai em claro no corpo do `POST /api/login`, e o cookie de sessão vai em
claro em toda requisição. Um aparelho comprometido na mesma rede lê os dois, e
com o cookie entra sem saber a senha.

O `https://` também libera, no navegador, o que a
[§Por que HTTPS](../acesso-remoto.md#por-que-https) lista: instalar como app,
compartilhar a imagem e as miniaturas por WebCodecs.

O [`acesso-remoto.md`](../acesso-remoto.md) já resolve tudo isso com o
Tailscale, que emite um certificado válido sem nada na rede de casa. Esta doc é
para quem não quer o Tailscale. A
[§Em casa sem o Tailscale](../acesso-remoto.md#em-casa-sem-o-tailscale) é outro
caso: reaproveita o certificado do Tailscale com um DNS local.

## O que a doc precisa cobrir

- **Um proxy TLS na frente do dwnvr**, porque ele não fala HTTPS. O candidato
  natural é o Caddy: o `tls internal` dele cria uma CA local e emite e renova
  o certificado sozinho. A alternativa é o nginx, com o certificado gerado pelo
  `mkcert` ou pelo `openssl`.
- **Certificado auto-assinado ou CA local.** Aceitar o aviso do navegador num
  certificado auto-assinado já cifra o tráfego. Mas, até onde se sabe, não
  basta para o app instalável: o Chrome não registra service worker numa página
  com erro de certificado. O que deve funcionar é uma CA local instalada como
  confiável em cada aparelho, assinando o certificado do servidor. A conferir.
- **Fazer cada aparelho confiar na CA.** É o passo que decide se dá certo, e
  muda por sistema: Android (CA de usuário), iPhone (instalar o perfil e ligar
  a confiança total), Windows, macOS, Linux, e o Firefox, que tem repositório
  próprio.
- **O nome no certificado.** Tem de bater com o endereço digitado: o IP, num
  SAN do tipo IP, ou um nome resolvido na rede de casa. Se o IP muda pelo DHCP,
  o certificado quebra, então a doc deve mandar reservar o IP no roteador.
- **O que o iPhone exige do certificado.** Desde o iOS 13, até onde se sabe:
  SAN obrigatório, EKU `serverAuth` e validade de no máximo 825 dias. Um
  certificado feito à mão com o `openssl` costuma errar algum dos três. A
  conferir.

## O que o dwnvr já faz e a doc tem de respeitar

Lido no código (`internal/api/auth.go` e `docker-compose.yml`):

- O `Secure` do cookie sai pelo header `X-Forwarded-Proto: https`. O Caddy o
  manda sozinho, e no nginx é `proxy_set_header X-Forwarded-Proto $scheme;`.
  Sem o header o login funciona, mas o cookie sai sem `Secure`.
- Se o proxy e a porta HTTP usarem o mesmo nome de host, depois de um login
  por HTTPS o navegador não deixa o HTTP gravar um cookie com o mesmo nome.
  Quem montar assim tem de entrar sempre pelo mesmo endereço.
- O ao vivo passa por WebSocket (`GET /api/live/ws`), e o proxy tem de repassar
  o upgrade. O Caddy repassa sozinho; o nginx precisa dos headers `Upgrade` e
  `Connection`.
- A mídia do WebRTC vai direto à 8555 do go2rtc, fora do proxy, como hoje (ver
  [§O ao vivo e a porta 8555](../acesso-remoto.md#o-ao-vivo-e-a-porta-8555)).
- O compose publica a 8080 em todas as interfaces (`"8080:8080"`). Com o proxy
  na frente, ela continua sendo um caminho em claro para a senha.

## Decidir antes de escrever

- **Onde o proxy mora.** Pode ser um serviço a mais no `docker-compose.yml`,
  atrás de um profile como o `detect`, ou só a receita na doc, com o proxy por
  conta de quem instala. Serviço novo repercute no README e no
  `docs/operacao.md` (ver a tabela de Repercussões do `AGENTS.md`).
- **Fechar a 8080 para a rede** quando houver proxy, como já se faz com a 1984
  e a 8554 do go2rtc (`GO2RTC_API_PORT`). Mexer na porta publicada pode ser
  mudança incompatível: conferir a regra do `AGENTS.md` (§Git).
