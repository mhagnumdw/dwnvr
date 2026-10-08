# HTTPS na rede de casa, sem o Tailscale <!-- omit in toc -->

Este guia leva o dwnvr do `http://<ip-do-servidor>:8080` para
**`https://<ip-do-servidor>`**, dentro da rede de casa, sem o Tailscale e sem
conta em serviço nenhum. Na frente do dwnvr entra o
[Caddy](https://caddyserver.com), um servidor web que cria uma autoridade
certificadora (CA) própria e assina com ela o certificado do servidor. Cada
aparelho que abre o dwnvr passa a confiar nessa CA, uma vez, e daí em diante o
navegador trata o endereço como seguro e **oferece instalar a tela como app**.

Só vale dentro de casa. Para abrir também fora dela, o caminho é o
[acesso remoto](acesso-remoto.md), pelo Tailscale, que resolve o `https://`
nos dois lugares.

- [Por que HTTPS](#por-que-https)
- [O que muda e o que custa](#o-que-muda-e-o-que-custa)
- [O servidor](#o-servidor)
  - [1. Um IP fixo](#1-um-ip-fixo)
  - [2. O Caddy](#2-o-caddy)
  - [3. Subir e conferir](#3-subir-e-conferir)
  - [4. A CA, para os aparelhos](#4-a-ca-para-os-aparelhos)
- [Os aparelhos](#os-aparelhos)
  - [Android](#android)
  - [iPhone](#iphone)
  - [Windows](#windows)
  - [macOS](#macos)
  - [Linux](#linux)
  - [Conferir, em cada aparelho](#conferir-em-cada-aparelho)
- [Do lado do dwnvr](#do-lado-do-dwnvr)
- [Validade e renovação](#validade-e-renovação)
- [Desfazer](#desfazer)
- [Problemas comuns](#problemas-comuns)

## Por que HTTPS

O dwnvr só fala HTTP. Pelo `http://<ip-do-servidor>:8080`, a senha vai em
claro no login, e o cookie de sessão vai em claro em toda requisição. Um
aparelho comprometido na mesma rede lê os dois, e com o cookie entra sem saber
a senha.

O `https://` também libera no navegador o que a
[§Por que HTTPS](acesso-remoto.md#por-que-https) do acesso remoto lista:
instalar como app, compartilhar a imagem e as miniaturas por WebCodecs.

## O que muda e o que custa

- **Cada aparelho confia na CA, uma vez.** É o passo que decide se dá certo
  (ver [Os aparelhos](#os-aparelhos)). Num aparelho sem a CA, o navegador avisa
  que a conexão não é particular. **Não aceite o aviso:** um aparelho
  comprometido na rede mostraria um igual, e quem se acostuma a aceitar não
  percebe a diferença. Aceito, o aviso também não deixa instalar o app.
- **A chave da CA fica no servidor**, em `config/caddy/data`. Quem a tiver
  consegue se passar por qualquer site para os aparelhos que confiam nela:
  guarde a pasta como guarda as senhas, e ponha-a no backup. Perdida, nasce
  uma CA nova, e cada aparelho tem de instalar a nova.
- **O endereço é o IP do servidor**, que por isso precisa ser fixo.
- **O TLS gasta CPU no servidor.** O do `tailscale serve` foi medido (ver
  [§O custo](acesso-remoto.md#3-o-custo)); o do Caddy, não. Em memória, o
  Caddy ocupou perto de 15 MB, medido num PC.

## O servidor

### 1. Um IP fixo

O certificado vale para um IP, e o app instalado guarda o endereço como
identidade. Se o roteador entregar outro IP ao servidor, o `https://` para de
abrir, e o app precisa ser instalado de novo em cada aparelho.

No roteador, reserve para o servidor o IP que ele tem hoje, o mesmo do
`http://<ip-do-servidor>:8080`. A opção costuma se chamar *Reserva de DHCP*,
*IP fixo* ou *Address Reservation*, e liga o IP ao endereço MAC do servidor.

### 2. O Caddy

Este passo pede o Docker Compose 2.24.4 (janeiro de 2024) ou mais novo; confira
com `docker compose version`.

Num terminal só: os comandos daqui até o passo 4 rodam na pasta do clone e usam
as duas variáveis do primeiro bloco.

```sh
# A pasta do clone, a do docker-compose.yml e do .env
cd ~/dwnvr

# O diretório da instalação (ver "Instalar de verdade" no README) e o IP do
# servidor na rede de casa. Troque pelos seus.
DWNVR_DIR=/mnt/storage/dwnvr
DWNVR_IP=192.168.15.10

# A pasta do Caddy, com a subpasta onde ele guarda a CA. Criada antes de subir,
# para nascer sua: o Caddy roda com o mesmo usuário do dwnvr.
mkdir -p "$DWNVR_DIR/config/caddy/data"
echo "CADDY_CONFIG_DIR=$DWNVR_DIR/config/caddy" >> .env
```

No teste rápido do README, a configuração mora no próprio clone:
`DWNVR_DIR=$PWD`.

A configuração do Caddy, com o IP já preenchido pelo terminal:

```sh
cat > "$DWNVR_DIR/config/caddy/Caddyfile" <<EOF
{
	# Quem confia na CA são os aparelhos: o Caddy não tenta instalá-la no
	# próprio container, onde ela não serve para nada.
	skip_install_trust
	# Quem abre pelo IP não diz o nome do servidor na conexão (o SNI), e sem
	# nome o Caddy no container não acha o certificado: a conexão falha.
	default_sni $DWNVR_IP
	servers {
		# Sem HTTP/3, que usaria a UDP 443, fechada no compose.
		protocols h1 h2
	}
}

https://$DWNVR_IP {
	# O certificado sai da CA local do Caddy.
	tls internal
	reverse_proxy dwnvr:8080
}
EOF
```

E o serviço, num `docker-compose.override.yml` ao lado do
`docker-compose.yml`. O Compose lê os dois juntos, sem opção nenhuma, e o
`git pull` da atualização não mexe no override, que o `.gitignore` deixa de
fora. Se você já tem um override, o `cat >` abaixo o apagaria: junte os dois à
mão.

```sh
cat > docker-compose.override.yml <<'EOF'
# O Caddy na frente do dwnvr, com HTTPS na rede de casa.
# Ver docs/https-na-rede-local.md.
services:
  dwnvr:
    # A 8080 abre só no próprio servidor: a rede de casa entra pelo Caddy.
    # O !override troca a lista do docker-compose.yml, em vez de somar a ela.
    ports: !override
      - "127.0.0.1:8080:8080"

  caddy:
    # A 2.11 recebe as correções da 2.11.x a cada atualização.
    image: caddy:2.11
    restart: unless-stopped
    # O mesmo usuário do dwnvr, para a CA nascer sua no disco.
    user: "${DWNVR_UID:-1000}:${DWNVR_GID:-1000}"
    ports:
      - "443:443"
      - "80:80"   # só redireciona para o https://
    volumes:
      # A pasta, e não o arquivo: montado sozinho, o Caddyfile editado depois
      # não chegaria ao container.
      - ${CADDY_CONFIG_DIR:-./config/caddy}:/etc/caddy:ro,z
      # A CA e os certificados.
      - ${CADDY_CONFIG_DIR:-./config/caddy}/data:/data:z
    # Teto de memória como rede de segurança: o Caddy ocupa perto de 15 MB.
    deploy:
      resources:
        limits:
          memory: 64M
EOF
```

A 8080 continua aberta no próprio servidor, mas fecha para a rede, onde
seguiria sendo um caminho em claro para a senha. Quem fecha é o `!override`:
sem ele, o Compose somaria as duas listas de portas, e a 8080 continuaria
aberta.

### 3. Subir e conferir

```sh
docker compose up -d
```

> Com [Podman](../README.md#com-podman-) no lugar do Docker:
> `PODMAN_USERNS=keep-id podman-compose --in-pod false up -d`. Não testado.
> O Podman rootless só publica a 80 e a 443 com
> `net.ipv4.ip_unprivileged_port_start=80` no `sysctl` do servidor.

O dwnvr reinicia, porque a porta dele mudou, e as câmeras param de gravar por
alguns segundos.

No servidor, no mesmo terminal do passo 2:

```sh
# "certificate obtained successfully", com o IP do servidor
docker compose logs caddy | grep obtained

# O caminho inteiro: a CA, o certificado e o Caddy repassando ao dwnvr
curl -sS --cacert "$DWNVR_DIR/config/caddy/data/caddy/pki/authorities/local/root.crt" \
  "https://$DWNVR_IP/api/session"
# Responde o JSON do dwnvr, como {"authRequired":true,"authenticated":false}
```

No log do Caddy, os avisos `HTTP/2 skipped` e `HTTP/3 skipped` da porta 80 são
normais.

O que importa na pasta do Caddy, que o `tree "$DWNVR_DIR/config/caddy"` mostra
inteira (com `-pug`, também as permissões e o dono):

```text
config/caddy/
├── Caddyfile
└── data/caddy/
    ├── certificates/local/192.168.15.10/   o certificado do servidor, renovado sozinho
    └── pki/authorities/local/
        ├── root.crt                        a CA, que vai para os aparelhos
        └── root.key                        a chave da CA, que nunca sai do servidor
```

De outro aparelho da rede:

- `http://<ip-do-servidor>:8080` **não abre**;
- `http://<ip-do-servidor>` leva ao `https://<ip-do-servidor>`, com o aviso de
  conexão não particular, que é o esperado até o aparelho confiar na CA.

### 4. A CA, para os aparelhos

```sh
cp "$DWNVR_DIR/config/caddy/data/caddy/pki/authorities/local/root.crt" ~/dwnvr-ca.crt
```

É este arquivo que cada aparelho instala. Ele é público: o segredo é a chave,
o `root.key` ao lado dele, que nunca sai do servidor. Nos aparelhos, a CA
aparece como **Caddy Local Authority**, seguido do ano em que nasceu.

Num servidor sem tela, traga o arquivo antes para o seu computador, pelo ssh,
com este comando rodado no computador:

```sh
scp <usuário>@<ip-do-servidor>:dwnvr-ca.crt .
```

Leve o arquivo a cada aparelho por um caminho que não passe aberto pela rede de
casa: e-mail ou mensageiro para você mesmo, ou cabo USB. Baixado por
`http://` na rede de casa, um aparelho comprometido poderia trocá-lo por outro.

## Os aparelhos

O celular ou o computador de cada pessoa que vai usar o dwnvr, inclusive os
seus. Depois de instalar a CA, feche e abra o navegador.

### Android

1. Salve o `dwnvr-ca.crt` no celular, em Downloads.
2. Configurações > Segurança e privacidade > Mais configurações de segurança >
   Criptografia e credenciais > Instalar um certificado > **Certificado de CA**,
   e escolha o arquivo. O caminho muda de um fabricante para outro; a busca das
   Configurações acha por "certificado". O Android exige bloqueio de tela (PIN,
   padrão ou senha) antes de instalar.

O Chrome confia na CA instalada assim, e o Firefox também, da versão 120 em
diante.

### iPhone

> Esta seção ainda não foi testada num iPhone.

1. Mande o `dwnvr-ca.crt` por e-mail e toque no anexo pelo app Mail, ou mande
   por AirDrop. O iPhone avisa que baixou um perfil.
2. Em Ajustes, o perfil baixado aparece logo no topo: toque nele e em
   **Instalar**.
3. Ajustes > Geral > Sobre > Certificados Confiáveis: ligue a Caddy Local
   Authority em **Ativar Confiabilidade Total para Certificados Raiz**. Sem
   este passo o perfil fica instalado, mas o Safari continua avisando.

### Windows

> Esta seção ainda não foi testada.

No PowerShell, na pasta do arquivo:

```powershell
Import-Certificate -FilePath .\dwnvr-ca.crt -CertStoreLocation Cert:\CurrentUser\Root
```

O Windows pede confirmação. Vale para o Chrome, o Edge e o Firefox da versão
120 em diante, no seu usuário.

### macOS

> Esta seção ainda não foi testada.

```sh
sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain dwnvr-ca.crt
```

Vale para o Safari, o Chrome e o Firefox da versão 120 em diante.

### Linux

> Esta seção ainda não foi testada.

No Linux, o Chrome e o Firefox têm cada um a sua lista, e a CA entra nas duas
separadamente.

**Chrome:** pelo `certutil`, que vem no pacote `nss-tools` no Fedora e no
`libnss3-tools` no Debian e no Ubuntu. Abra o Chrome uma vez antes, para a
lista existir.

```sh
# O Chrome usa o ~/.pki/nssdb quando ele existe; nas instalações novas, o
# ~/.local/share/pki/nssdb
NSSDB=$HOME/.pki/nssdb; [ -d "$NSSDB" ] || NSSDB=$HOME/.local/share/pki/nssdb
certutil -d sql:"$NSSDB" -A -t "C,," -n dwnvr -i ~/dwnvr-ca.crt
```

**Firefox:** Configurações > Privacidade e Segurança > Certificados >
**Gerenciar certificados** > aba **Autoridades** > **Importar**. Escolha o
arquivo e marque **Confiar nesta CA para identificar sites**.

### Conferir, em cada aparelho

- `https://<ip-do-servidor>` abre o login do dwnvr sem aviso nenhum, e o ícone à
  esquerda do endereço diz que a conexão é segura.
- **Instalar o app:** no Android, Chrome, menu ⋮ > **Instalar app**; no
  iPhone, Safari, compartilhar > **Adicionar à Tela de Início**; no
  computador, o ícone de instalar na barra de endereço do Chrome.
- Aberta pelo app, a aba **diagnóstico** da tela mostra "app instalado (PWA)".

## Do lado do dwnvr

Nada muda. As URLs da tela são relativas, e o WebSocket do ao vivo passa a
`wss://` sozinho. O Caddy avisa que a conexão chegou por HTTPS
(`X-Forwarded-Proto`), e com isso o cookie de sessão sai `Secure`.

A mídia do WebRTC continua indo direto do navegador para a porta 8555 do
go2rtc, fora do Caddy, como antes (ver
[§O ao vivo e a porta 8555](acesso-remoto.md#o-ao-vivo-e-a-porta-8555)).

## Validade e renovação

- **O certificado do servidor** vale 12 horas, e o Caddy o renova sozinho antes
  de vencer. O mesmo vale para o intermediário, que fica entre ele e a CA e
  dura 7 dias. Os aparelhos não percebem essas trocas, porque confiam na CA, e
  não nos dois.
- **A CA** vale perto de 10 anos, e o Caddy ainda não a renova sozinho. Nos dois
  últimos anos, o log dele avisa (`root certificate expiring soon`). A troca é
  à mão, antes de ela vencer: apague a pasta `config/caddy/data/caddy`, rode
  `docker compose restart caddy` e instale a CA nova em cada aparelho, apagando
  a antiga.

O vencimento da CA:

```sh
openssl x509 -in "$DWNVR_DIR/config/caddy/data/caddy/pki/authorities/local/root.crt" -noout -enddate
```

## Desfazer

```sh
cd ~/dwnvr   # a pasta do clone
rm docker-compose.override.yml
docker compose up -d --remove-orphans
```

O Caddy sai, e o dwnvr volta a abrir na 8080 para a rede. A pasta
`config/caddy` fica, com a CA: subir de novo reaproveita a mesma, e os
aparelhos que ainda a tiverem não reinstalam nada. Para desfazer também nos
aparelhos, apague a CA (no Android, em Credenciais confiáveis > Usuário) e
desinstale o app.

## Problemas comuns

**O navegador avisa que a conexão não é particular.** O aparelho não confia na
CA: falta instalar, ou, no iPhone, ligar a confiança total. No Linux, o Chrome
e o Firefox têm cada um a sua lista. Se o aviso falar de nome que não bate, o
endereço digitado não é o IP do Caddyfile.

**O botão de instalar não aparece.** O Chrome só oferece instalar quando confia
no certificado, então é o caso de cima. E o endereço tem de ser o `https://`:
pelo `http://`, não instala.

**A conexão falha na hora, sem aviso de certificado.** O IP do `default_sni`
não é o do servidor. Confira o IP nas duas linhas do Caddyfile e rode
`docker compose restart caddy`.

**O servidor mudou de IP.** Troque o IP nas duas linhas do Caddyfile e rode
`docker compose restart caddy`. A CA continua a mesma, e os aparelhos não
reinstalam nada; o app, sim, porque o endereço é a identidade dele. Para não
repetir, reserve o IP no roteador (ver [1. Um IP fixo](#1-um-ip-fixo)).

**Todos os aparelhos voltaram a avisar de uma vez.** Nasceu uma CA nova: a pasta
`config/caddy/data` se perdeu ou foi trocada. Instale a nova em cada aparelho
e apague a antiga.

**`http://<ip-do-servidor>:8080` ainda abre de outro aparelho.** O override não
pegou. Confira se ele está ao lado do `docker-compose.yml` e se o
`docker compose version` é 2.24.4 ou mais novo, e rode `docker compose up -d`
de novo.

**O Caddy não sobe, com `permission denied` em `/data`.** A pasta
`config/caddy/data` não existia, e o Docker a criou como root. Rode
`sudo chown -R "$(id -u):$(id -g)" "$DWNVR_DIR/config/caddy"` e suba de novo.

**`address already in use` na 80 ou na 443.** Outro processo já usa a porta no
servidor. Libere a porta, ou, se esse processo escuta só em outro IP, prenda o
Caddy ao IP da rede de casa no override, como em `"<ip-do-servidor>:443:443"`
(não testado). Depois, suba com `docker compose up -d --force-recreate caddy`:
o `up -d` sozinho reaproveita o container da tentativa que falhou, que sobe sem
as portas.
