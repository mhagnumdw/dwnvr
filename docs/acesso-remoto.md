# Acesso remoto: o dwnvr no celular, em casa e fora <!-- omit in toc -->

Este guia leva o dwnvr do `http://<ip-da-máquina>:8080` da rede de casa para
um endereço **`https://`** que abre de qualquer lugar, **com o navegador oferecendo
instalar a tela como app**. Também mostra como dar acesso a outras pessoas, como
a família, **sem abrir o servidor para a internet**.

O caminho é o [Tailscale](https://tailscale.com): uma VPN que liga os seus
dispositivos ao servidor sem abrir porta no roteador, e que emite o certificado
do `https://`. No dwnvr não há nada para configurar.

- [Por que HTTPS](#por-que-https)
- [Por que não expor o servidor na internet](#por-que-não-expor-o-servidor-na-internet)
- [O servidor: Tailscale e `tailscale serve`](#o-servidor-tailscale-e-tailscale-serve)
  - [1. O Tailscale no servidor](#1-o-tailscale-no-servidor)
  - [2. O `serve`](#2-o-serve)
  - [3. O custo](#3-o-custo)
- [Dar acesso a outras pessoas](#dar-acesso-a-outras-pessoas)
  - [1. A policy, antes do convite](#1-a-policy-antes-do-convite)
  - [2. O convite](#2-o-convite)
  - [3. Conferir, do dispositivo da pessoa](#3-conferir-do-dispositivo-da-pessoa)
- [Os dispositivos de quem acessa](#os-dispositivos-de-quem-acessa)
  - [Em todos, instalar e configurar o Tailscale](#em-todos-instalar-e-configurar-o-tailscale)
  - [Android](#android)
  - [iPhone](#iphone)
  - [Desktop](#desktop)
- [O ao vivo e a porta 8555](#o-ao-vivo-e-a-porta-8555)
- [Em casa sem o Tailscale](#em-casa-sem-o-tailscale)
- [Problemas comuns](#problemas-comuns)

## Por que HTTPS

Em `http://`, o navegador trata a página como insegura e esconde dela uma
parte dos recursos. No dwnvr, isso aparece em três lugares:

- **Instalar como app.** O Chrome só instala uma página servida em `https://`.
  Em `http://`, o Android cria um atalho que abre numa aba comum.
- **Compartilhar a imagem.** No celular, o `⤓ imagem` das gravações abre a
  folha de compartilhar do sistema, que leva a imagem direto para a galeria ou
  para uma conversa. Em `http://` essa folha não existe, e a imagem só é
  baixada.
- **Miniaturas da timeline.** Em `https://` elas são decodificadas por
  WebCodecs. Em `http://`, por um `<video>` escondido, que é mais lento.

**E a senha do dwnvr deixa de passar em claro pela rede.**

A aba **diagnóstico** da tela mostra "endereço seguro (https)" e, depois de
instalar, "app instalado (PWA)".

## Por que não expor o servidor na internet

Abrir uma porta no roteador, ou publicar o dwnvr por um túnel público (como o
Tailscale Funnel ou o Cloudflare Tunnel), deixa a tela de login à vista de
qualquer um. O login do dwnvr passa a ser a única proteção das câmeras, e ele
não foi feito para isso: é um usuário só, e as tentativas não têm limite.

Com o Tailscale, o servidor só responde a dispositivos que entraram na VPN. Do
resto da internet, não há o que encontrar.

## O servidor: Tailscale e `tailscale serve`

### 1. O Tailscale no servidor

Instale o Tailscale na máquina do dwnvr, pela
[página de download](https://tailscale.com/download), e entre com a sua conta.
A rede de dispositivos da sua conta é o seu **tailnet**.

No [admin do Tailscale](https://login.tailscale.com/admin/dns), deixe ligados
o **MagicDNS** e o **HTTPS Certificates**. O MagicDNS dá a cada dispositivo um
nome do tipo `servidor.tail1234.ts.net`, e o HTTPS Certificates deixa o
Tailscale emitir um certificado para esse nome.

**Escolha o nome antes de instalar o app em algum dispositivo.** O endereço
`https://servidor.tail1234.ts.net` é formado pelo nome da máquina e pelo nome
do tailnet, e é a identidade do app instalado: trocar qualquer um dos dois
depois exige instalar o app de novo em cada dispositivo.

### 2. O `serve`

No servidor:

```sh
# 8080 é a porta em que o dwnvr atende no servidor, a do docker-compose.yml
sudo tailscale serve --bg 8080
```

O `serve` atende em `https://<máquina>.<tailnet>.ts.net`, na porta 443, com
certificado do Let's Encrypt, e repassa para o dwnvr em
`http://127.0.0.1:8080`. O `--bg` deixa a configuração gravada: ela volta
sozinha depois de reiniciar o servidor. O `sudo` deixa de ser preciso depois de
um `sudo tailscale set --operator=$USER`.

Para conferir e para desfazer:

```sh
tailscale serve status   # https://servidor.tail1234.ts.net -> http://127.0.0.1:8080
sudo tailscale serve reset
```

O primeiro acesso pode levar alguns segundos, enquanto o certificado é emitido.
O nome da máquina fica registrado nos logs públicos de Certificate
Transparency, como acontece com todo certificado do Let's Encrypt.

Do lado do dwnvr, nada muda. As URLs da tela são relativas, e o WebSocket do
ao vivo passa a `wss://` sozinho. O `serve` avisa que a conexão chegou por
HTTPS (`X-Forwarded-Proto`), e com isso o cookie de sessão sai `Secure`.

O `http://<ip-da-máquina>:8080` continua funcionando na rede de casa. Para o
navegador, é outra origem: login separado, e sem os recursos de HTTPS.

### 3. O custo

O TLS do `serve` gasta CPU no servidor. Medido num Orange Pi Zero 3: perto de
2,7× a CPU por byte de uma conexão sem TLS. Na prática, a grade do ao vivo
cheia pelo MSE (ver [O ao vivo e a porta 8555](#o-ao-vivo-e-a-porta-8555))
custaria perto de 6% de um núcleo, e uma exportação de 100 MB leva perto de
80 s.

## Dar acesso a outras pessoas

Cada pessoa usa a própria conta do Tailscale, e você **compartilha a máquina
do dwnvr** com ela. **Não convide a pessoa para o seu tailnet**: como membro, ela
enxergaria todos os seus dispositivos, e, se o servidor anuncia uma subnet route,
o tráfego da rede dela poderia passar pela sua. No compartilhamento ela vê só
a máquina compartilhada, e as subnet routes não vão junto.

Quem acessa o quê é decidido em dois lugares:

- **O compartilhamento** decide *qual máquina* a pessoa enxerga.
- **A policy do seu tailnet** decide *quais portas* dessa máquina ela alcança.

### 1. A policy, antes do convite

A policy padrão libera tudo para todos (`"src": ["*"]`), e o `*` inclui quem
recebeu um compartilhamento. Sem mudar a policy, a pessoa alcança todas as
portas do servidor: o dwnvr sem HTTPS na 8080, o ssh e o que mais estiver
escutando.

Em [Access controls](https://login.tailscale.com/admin/acls/file), no JSON
editor:

Partindo da policy padrão de um tailnet novo, a mudança é esta (`-` sai, `+`
entra):

```diff
 {
+    // O IP do servidor no tailnet: `tailscale ip -4`, rodado nele.
+    "hosts": {
+        "servidor": "100.x.y.z",
+    },
+
     "grants": [
-        {"src": ["*"], "dst": ["*"], "ip": ["*"]},
+        // Os seus dispositivos: tudo, como antes.
+        {"src": ["autogroup:member"], "dst": ["*"], "ip": ["*"]},
+
+        // Quem recebeu o compartilhamento: só o `tailscale serve`.
+        {"src": ["autogroup:shared"], "dst": ["servidor"], "ip": ["tcp:443"]},
     ],
 }
```

Ou seja:

- o nome `servidor` passa a valer o IP do servidor no tailnet;
- o `"*"` da regra que libera tudo vira `"autogroup:member"`, só os seus
  dispositivos;
- entra a regra do `autogroup:shared`, quem recebeu o compartilhamento.

O resto da policy (`ssh`, `nodeAttrs`, os comentários) fica como está.

Se a sua policy usa `acls` em vez de `grants`, a regra equivalente é
`{"action": "accept", "src": ["autogroup:shared"], "proto": "tcp", "dst": ["servidor:443"]}`.

Antes de salvar, a aba **Preview rules** mostra o que o seu usuário alcança.
Ele tem de continuar alcançando tudo. Dispositivo com tag não entra em
`autogroup:member`: se algum dos seus tiver tag, ele precisa de regra própria.

### 2. O convite

Em [Machines](https://login.tailscale.com/admin/machines), na linha do
servidor: **⋯ > Share...**, por e-mail ou por link. O link tem de ser aberto
com a mesma conta que a pessoa usa no app do Tailscale. Um convite por pessoa.

Quem recebe chega ao servidor só pelo nome completo,
`https://servidor.tail1234.ts.net`. O nome curto, `servidor` sozinho, funciona
apenas nos dispositivos que entram no Tailscale com a sua conta, e não nos de
quem recebeu o compartilhamento.

### 3. Conferir, do dispositivo da pessoa

- `https://servidor.tail1234.ts.net` abre o login do dwnvr;
- `http://servidor.tail1234.ts.net:8080` **não abre**. Se abrir, a policy não
  pegou.

O dwnvr tem um usuário só, então todos entram com o mesmo usuário e a mesma
senha.

## Os dispositivos de quem acessa

O celular ou o computador de cada pessoa que vai usar o dwnvr, inclusive os
seus: primeiro o Tailscale, depois o dwnvr pelo navegador.

A sessão do dwnvr vale 30 dias e se renova com o uso: quem abre o app de vez
em quando não precisa entrar de novo.

### Em todos, instalar e configurar o Tailscale

1. O app do Tailscale, com a conta da pessoa. Para quem nunca usou, o login
   cria o tailnet dela.
2. Aceitar o compartilhamento, pelo convite.
3. **Desligar o key expiry do dispositivo.** Por padrão, cada dispositivo
   precisa entrar de novo no Tailscale a cada 180 dias, e até lá o app do
   dwnvr simplesmente para de abrir. Quem desliga é o dono do dispositivo, no
   admin do tailnet dele: [Machines](https://login.tailscale.com/admin/machines)
   > o dispositivo > **⋯ > Disable key expiry**.

### Android

- **Abrir e entrar:** no Chrome, `https://servidor.tail1234.ts.net`, com o
  usuário e a senha do dwnvr.
- **Instalar:** na mesma página, menu ⋮ > **Instalar app**.
- **Tailscale sempre ligado:** Configurações > VPN > a engrenagem do Tailscale
  > **VPN sempre ativa** (Always-on VPN). Deixe desligado o "bloquear conexões
  sem VPN": ligado, uma falha do Tailscale deixa o celular sem internet
  nenhuma.
- **DNS particular** em **Automático** (ou desativado). Com um provedor fixo,
  como o `dns.google`, o nome `.ts.net` costuma parar de resolver.

O Android não tem regra para ligar o Tailscale só fora de casa. Ele fica
sempre ligado, em casa e fora, e é por ele que o app abre nos dois lugares.

### iPhone

> Esta seção ainda não foi testada num iPhone.

- **Abrir e entrar:** no Safari, `https://servidor.tail1234.ts.net`, com o
  usuário e a senha do dwnvr.
- **Instalar:** na mesma página, compartilhar > **Adicionar à Tela de
  Início**. Do iOS 26 em diante, "Abrir como app" já vem ligado.
- **Tailscale sempre ligado:** no app do Tailscale, **VPN On Demand**, ligado
  no Wi-Fi e nos dados móveis.
- **Gravações:** pelo código, não tocam no iPhone; o ao vivo, sim. Ver
  [`TODO_gravacoes-no-iphone.md`](TODO/TODO_gravacoes-no-iphone.md).

### Desktop

Abra `https://servidor.tail1234.ts.net` no Chrome e entre; o ícone de
instalar aparece na barra de endereço. No Linux, o Tailscale liga e desliga
pelo `tailscale up` e `tailscale down`.

## O ao vivo e a porta 8555

O ao vivo tenta primeiro o WebRTC, de menor latência, e cai para o MSE quando
o WebRTC não conecta. A conversa inicial passa pelo dwnvr, e portanto pelo
`serve`. A mídia do WebRTC, não: ela vai direto do navegador para a porta 8555
do go2rtc. Já o MSE vem pelo próprio WebSocket do dwnvr.

Com a policy acima, quem recebeu o compartilhamento não alcança a 8555 pelo
Tailscale. Mesmo assim, fora de casa o WebRTC pode conectar pela internet,
atravessando o NAT de casa, sem porta aberta (hole punching). Quando a rede da
operadora não deixa, o ao vivo cai no MSE e toca do mesmo jeito, com um pouco
mais de atraso. O WebRTC é uma otimização, não um requisito.

> A exceção possível é o iPhone com câmera em H265, que pode depender do
> WebRTC para tocar o ao vivo. Ainda não foi testado.

Para o WebRTC passar sempre pelo Tailscale, o go2rtc precisaria anunciar o
endereço do tailnet (`webrtc.candidates` no `go2rtc.yaml`), e a policy
precisaria liberar a 8555, em udp e tcp, para `autogroup:shared`. Também não
foi testado.

## Em casa sem o Tailscale

> Este caminho não foi testado.

O mesmo app instalado pode abrir em casa com o Tailscale desligado, mas são
precisas duas peças na rede de casa:

1. Um DNS local que responda `servidor.tail1234.ts.net` com o IP do servidor
   na rede de casa. Muitos roteadores não têm esse recurso; pôr o próprio
   servidor como DNS da casa resolve, mas faz a internet da casa inteira
   depender dele.
2. HTTPS na porta 443 da rede de casa, com o mesmo certificado (o
   `tailscale cert` o emite em arquivo), renovado de forma automática. Tem de
   ser a 443: outra porta é outra origem, e o app instalado não a reconhece.

Na maioria das casas não compensa. O Android deixa o Tailscale sempre ligado de
qualquer jeito. Quem ganha é o iPhone, que pode desligar o Tailscale na Wi-Fi
de casa pelo VPN On Demand. O endereço é o mesmo nos dois caminhos, então
decidir depois não gera retrabalho.

## Problemas comuns

**O app abre a tela de "sem conexão" do navegador.** O Tailscale está
desligado no dispositivo. Ligue, ou deixe sempre ligado (Always-on VPN no
Android, VPN On Demand no iPhone).

**Funcionava, e depois de meses parou.** O key expiry do dispositivo venceu: o
app do Tailscale pede para entrar de novo. Entre, e desligue o key expiry
(ver [Em todos, instalar e configurar o Tailscale](#em-todos-instalar-e-configurar-o-tailscale)).

**O nome `.ts.net` não resolve no Android.** O DNS particular está fixo num
provedor. Ponha em Automático.

**O nome curto não funciona.** Para quem recebeu o compartilhamento,
`servidor` sozinho não resolve; só o nome completo,
`servidor.tail1234.ts.net`.

**O `servidor.local` deixou de resolver com o Tailscale ligado.** No Android,
os nomes `.local` não resolvem com a VPN ligada nem nos dados móveis. Use o
endereço `https://`.

**O primeiro acesso demorou.** É a emissão do certificado. Os seguintes são
imediatos.

**O botão de instalar não aparece.** Confira se o endereço é `https://`. Em
`http://`, inclusive pelo IP da rede de casa, o navegador não instala.
