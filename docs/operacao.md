# Operação

## Onde ficam os arquivos

Nada de importante vive dentro do container. Os volumes cobrem tudo. Os
caminhos do host abaixo são os da [instalação de
verdade](../README.md#instalar-de-verdade), definidos no `.env` por
`DWNVR_CONFIG_DIR`, `DWNVR_STORAGE_DIR` e `GO2RTC_CONFIG_DIR`; no teste rápido
os mesmos arquivos ficam em `./config/dwnvr`, `./storage` e `./config/go2rtc`.

| No host (exemplo) | No container | O que é |
|---|---|---|
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
dwnvr`); pela tela de cadastro a mudança vale na hora.

## Inspecionar um container sem shell

A imagem é `FROM scratch` e contém literalmente isto:

```
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
