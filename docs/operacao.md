# Operação

## Onde ficam os arquivos

Nada de importante vive dentro do container. Os volumes cobrem tudo. Os
caminhos do host abaixo são os da [instalação de
verdade](../README.md#instalar-de-verdade), definidos no `.env` por
`DWNVR_CONFIG_DIR`, `DWNVR_STORAGE_DIR` e `GO2RTC_CONFIG_DIR`; no teste rápido
os mesmos arquivos ficam em `./config/dwnvr`, `./storage` e `./config/go2rtc`.

| No host (exemplo) | No container | O que é |
| --- | --- | --- |
| `/mnt/storage/dwnvr/config/dwnvr/dwnvr.yaml` | `/etc/dwnvr/dwnvr.yaml` | configuração, editada à mão |
| `/mnt/storage/dwnvr/config/dwnvr/cameras.json` | `/etc/dwnvr/cameras.json` | câmeras, gravado pela tela de cadastro |
| `/mnt/storage/dwnvr/config/dwnvr/.session-secret` | `/etc/dwnvr/.session-secret` | assina os cookies de sessão (0600) |
| `/mnt/storage/dwnvr/config/go2rtc/go2rtc.yaml` | `/config/go2rtc.yaml`, no container do go2rtc | as câmeras, editado à mão |
| `/mnt/storage/dwnvr/recordings/` | `/storage/` | gravações, índices, init segments, marcas de movimento e quadros das detecções |

Ou seja: **edite e inspecione tudo pelo host**, sem entrar no container.

```sh
cat /mnt/storage/dwnvr/config/dwnvr/cameras.json
tail -f /mnt/storage/dwnvr/recordings/cam_iota/index/$(date +%F).ndjson

# as marcas de movimento, se a câmera estiver com a detecção ligada
tail -f /mnt/storage/dwnvr/recordings/cam_iota/eventos/$(date +%F).ndjson

# os quadros das detecções do dia, um .jpg por detecção, nomeado pelo instante
ls -l /mnt/storage/dwnvr/recordings/cam_iota/quadros/$(date +%F)/
```

Para descobrir os caminhos de uma instalação qualquer:

```sh
docker inspect dwnvr --format '{{range .Mounts}}{{.Source}} -> {{.Destination}}{{println}}{{end}}'
```

**Alterar o `cameras.json` na mão exige reiniciar** (`docker compose restart
dwnvr`); pela tela de cadastro a mudança vale na hora. Um valor fora da faixa
não impede o boot: vale o padrão, e o aviso aparece na tela de Diagnóstico -
ver [Valor fora da faixa](configuracao.md#valor-fora-da-faixa).

## Copiar gravações direto do disco

O jeito mais simples é o botão de exportar da tela de Gravações: ele entrega o
trecho escolhido num arquivo só. Direto do disco vale para guardar dias
inteiros, copiar várias câmeras de uma vez, ou quando a interface não está à
mão. Copiar com o dwnvr rodando não atrapalha a gravação.

No disco, a gravação é uma fila de arquivos `.mp4` de ~30s, e cada um toca
sozinho. O nome é o início em epoch ms, e a pasta do dia segue o `TZ` do
`.env`. Os comandos abaixo usam o fuso do host, que costuma ser o mesmo; se
não for, ponha `TZ=America/Fortaleza` (o do `.env`) na frente do `date`.

```sh
cd /mnt/storage/dwnvr/recordings/cam_iota/2026-08-08

# a hora de início de cada arquivo
for f in *.mp4; do echo "$(date -d @${f%???.mp4} +%T)  $f"; done
```

O último arquivo da pasta de hoje ainda está sendo gravado e corta no meio.

Para copiar só um horário, o índice diz o início (`t`) e a duração (`d`) de
cada arquivo, então acha também o que começou antes do horário pedido e o
cobre. Precisa do `jq`:

```sh
cd /mnt/storage/dwnvr/recordings/cam_iota
ini=$(date -d '2026-08-08 17:30' +%s000)
fim=$(date -d '2026-08-08 17:32' +%s000)

mkdir -p ~/trecho
jq -r --argjson ini "$ini" --argjson fim "$fim" \
  'select(.t < $fim and .t + .d > $ini) | "\(.t).mp4"' \
  index/2026-08-08.ndjson | xargs -I{} cp 2026-08-08/{} ~/trecho/
```

O índice só lista arquivo fechado, então o que ainda está sendo gravado fica
de fora. Trecho que cruza a meia-noite precisa do índice dos dois dias.

## Trocar a senha e derrubar as sessões

Quem entra uma vez continua logado: a sessão vale 30 dias e se renova com o
uso. A assinatura do cookie de cada pessoa depende da senha dela, então trocar
a senha de alguém tira só essa pessoa. Há dois caminhos aqui, e os dois pedem
reiniciar o dwnvr, que só lê esses arquivos ao subir:

- **Trocar a senha**, ou o usuário, do dono no `dwnvr.yaml`. Todo cookie do
  dono emitido antes deixa de valer, e só entra quem souber a senha nova. Os
  outros usuários continuam logados.
- **Apagar o `.session-secret`**, para tirar todo mundo, o dono e os outros
  usuários, sem trocar senha nenhuma. O dwnvr gera outro ao subir.

```sh
# Para tirar todo mundo sem trocar a senha:
rm /mnt/storage/dwnvr/config/dwnvr/.session-secret

# Nos dois caminhos, depois:
docker compose restart dwnvr
```

Nos dois casos, quem foi tirado e estava com a tela aberta volta para a tela
de login.

## Inspecionar um container sem shell

A imagem é `FROM scratch` e contém literalmente isto:

```text
/dwnvr                          o binário
/etc/ssl/certs/ca-certificates.crt
```

O resto (`/dev`, `/proc`, `/etc/hosts`, `/etc/resolv.conf`) é injetado pelo
Docker. Não há `sh`, `ls` nem `cat` - o que é o ponto: menos superfície, menos
peso e nada para um invasor usar.

Isso não impede inspecionar. Quatro caminhos, do mais simples ao mais invasivo:

### 1. Logs e estado

```sh
docker logs -f dwnvr
docker logs dwnvr | grep -E 'level=(WARN|ERROR)'
docker stats --no-stream dwnvr
docker inspect dwnvr --format '{{.State.Health.Status}}'
```

### 2. O próprio binário como ferramenta

`docker exec` não precisa de shell - precisa de um executável, e há um:

```sh
docker exec dwnvr /dwnvr -healthcheck -config /etc/dwnvr/dwnvr.yaml; echo $?
docker exec dwnvr /dwnvr -h
```

### 3. Copiar arquivos para fora

`docker cp` é implementado pelo daemon e funciona em imagem vazia:

```sh
docker cp dwnvr:/etc/dwnvr/cameras.json /tmp/
```

### 4. Sidecar compartilhando os namespaces

Quando é preciso olhar rede ou processos de dentro, sobe-se um container
descartável **com as ferramentas** que compartilha os namespaces do dwnvr:

```sh
docker run --rm -it \
  --pid=container:dwnvr \
  --network=container:dwnvr \
  busybox sh
```

Lá dentro:

```sh
ps -o pid,args              # vê o processo do dwnvr como PID 1
netstat -ltn                # vê as portas dele
wget -qO- http://127.0.0.1:8080/api/session
```

Para inspecionar também o *sistema de arquivos* do dwnvr, acrescente
`--volumes-from dwnvr` - os volumes aparecem nos mesmos caminhos.

Nada disso muda a imagem: o sidecar é descartado ao sair.

## Trocar de versão

```sh
docker compose up -d --pull always
```

A tag das duas imagens, dwnvr e dwnvr-detect, vem de `DWNVR_VERSION` no
`.env`; sem ela, vale o default do `docker-compose.yml`, que cada release
reescreve com a versão dela. Os valores possíveis:

| `DWNVR_VERSION` | O que roda |
| --- | --- |
| (ausente) | a versão do compose que está no clone: a última release, depois de um `git pull` |
| `v0.1.0` | fica nessa versão, mesmo depois de `git pull`; é também como se volta para uma anterior |
| `main` | cada commit da main, antes de virar versão |
| `sha-abc1234` | um commit específico |

As versões e o que mudou em cada uma estão nas
[releases](https://github.com/mhagnumdw/dwnvr/releases).

O go2rtc segue a mesma ideia, com a própria variável: `GO2RTC_VERSION` no
`.env` passa por cima da versão fixa no compose. A release não a reescreve;
ela muda só por commit, depois de testada com o dwnvr.

O encerramento é gracioso: o dwnvr fecha e indexa o segmento em aberto de cada
câmera antes de sair. Sem isso, todo reinício perderia o último minuto gravado.

Se algo der errado, os dados sobrevivem à imagem - eles estão nos volumes. Voltar
para a versão anterior é trocar a tag e subir de novo.

## Problemas comuns

**Arquivos aparecendo como root no disco.** Falta `DWNVR_UID` e `DWNVR_GID` no
`.env`, ou o `user:` sumiu do compose. Descubra o seu com `id -u; id -g`. Para consertar o que já foi gravado:
`sudo chown -R 1000:1000 /mnt/storage/dwnvr`.

**A timeline vira o dia no horário errado.** Falta `TZ` no `.env`. A imagem não
tem `/usr/share/zoneinfo` - a base de fusos vai embutida no binário, mas alguém
precisa dizer qual fuso usar.

**O container não enxerga o go2rtc.** Dentro do container, `localhost` é o
próprio container. Use `host.docker.internal` (com `extra_hosts:
host-gateway`), o nome do serviço se os dois estiverem na mesma rede, ou
`network_mode: host`.

**A interface do go2rtc não abre de outra máquina.** É de propósito: o compose
publica a 1984 e a 8554 só para o próprio servidor, porque o go2rtc não pede
senha por padrão. Use um túnel ssh, ou abra com senha - ver [Abrir o go2rtc
para a rede](../README.md#abrir-o-go2rtc-para-a-rede-).

**A tela está velha depois de atualizar.** A interface é embutida no binário, e o
navegador cacheia os assets - que têm hash no nome justamente para isso não
acontecer. Se persistir, é sinal de que o binário foi construído sem rodar
`npm run build` antes.

**Nenhuma marca de objeto aparece.** Três coisas, nesta ordem:

1. **O `detector.url` está vazio.** Sem ele, o dwnvr não pergunta nada a
   ninguém. Com ele, o log do dwnvr diz `detector de objetos configurado` ao
   subir.
2. **O `dwnvr-detect` não subiu.** Ele só sobe com o profile `detect`
   (`COMPOSE_PROFILES=detect` no `.env`). Confira com
   `docker inspect dwnvr-detect --format '{{.State.Health.Status}}'`.
3. **O detector caiu.** O dwnvr avisa no log uma vez, com
   `detector de objetos fora do ar`, e outra quando ele volta. Enquanto isso,
   as câmeras seguem gravando e marcando movimento. As olhadas perdidas
   aparecem como `falhas` no funil do `/api/health`.
