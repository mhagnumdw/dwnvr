# Fase 4 — empacotamento

Imagem Docker multi-arch para `linux/arm64` e `linux/amd64`, validada rodando no
Orange Pi Zero 3 em 08/08/2026.

## A imagem

`FROM scratch` com um único arquivo dentro:

| | |
|---|---|
| Binário | 7,9 MB |
| Certificados CA | 222 KB |
| **Camada comprimida** | **~3 MB** |

Três coisas precisaram existir antes para que `scratch` fosse viável:

1. **`time/tzdata` embutido.** O dwnvr usa hora local para decidir a que dia um
   segmento pertence, e em `scratch` não existe `/usr/share/zoneinfo`. Sem isso
   `TZ` seria silenciosamente ignorado e todos os dias virariam UTC. Confirmado
   no Pi: os timestamps do log saem com `-03:00`, não `Z`.
2. **Healthcheck no próprio binário.** Não há shell nem curl na imagem para o
   `HEALTHCHECK` do Docker chamar, então `dwnvr -healthcheck` consulta a
   instância local e devolve o código de saída. O container reportou `healthy`.
3. **Binário estático** (`CGO_ENABLED=0`), que já era o caso.

## Build

Os dois estágios de build rodam em `$BUILDPLATFORM` e o Go compila cruzado por
`$TARGETARCH`. Emular ARM para compilar seria ordens de grandeza mais lento, e a
interface nem depende de arquitetura — é HTML, CSS e JS.

A interface é construída **dentro** da imagem, e não copiada da versão
versionada, para que a imagem sempre corresponda ao código-fonte que a gerou.

## Medições no Pi, em container

| | CPU | RAM |
|---|---|---|
| dwnvr (9 câmeras, container) | **2,82% de 1 core** | **14,1 MB** de 128 MB |

Ligeiramente abaixo do binário nativo (4,0% / 17,1 MB), principalmente porque os
segmentos passaram de 30 s para 60 s — menos rotações por minuto.

Verificado também: interface servida em 118 ms, 9 câmeras conectadas, índice
existente carregado sem migração, e exportação com DTS estritamente crescente.

## A armadilha do lightNVR, evitada

O compose define `user: "1000:1000"`. Sem isso, o container gravaria como root e
os arquivos nasceriam de root no disco — que é **exatamente** o que aconteceu
com o lightNVR neste mesmo Pi e que impediu, no início deste projeto, até criar
um diretório em `/mnt/storage`.

Confirmado depois de um minuto gravando em container:

```
usuario:usuario /mnt/storage/dwnvr/cam_frente/2026-08-08/1786231487217.mp4
arquivos de root: 0
```

## Rede

O `firewalld` do Pi bloqueava a porta do binário nativo; o Docker publica portas
inserindo as próprias regras e passa por fora dele — o mesmo caminho do go2rtc.
A porta aberta manualmente durante o desenvolvimento deixa de ser necessária.

O container alcança o go2rtc por `host.docker.internal`, mapeado via
`extra_hosts: host-gateway`. O go2rtc **não** faz parte do compose de propósito:
acoplar os dois recriaria a dificuldade que motivou o projeto.

## CI

O workflow roda apenas quando algo em `dwnvr/` muda — o repositório também
guarda configurações de outros NVRs.

Além de testes, `go vet` e `gofmt`, ele confere que **`internal/api/dist`
corresponde ao código em `web/`**. Essa checagem existe porque esquecer o
`npm run build` produz um binário que compila e sobe normalmente, mas serve a
tela antiga — uma falha silenciosa que só apareceria em produção.

A checagem foi verificada por mutação: alterar uma cor no CSS e reconstruir faz
o `dist` divergir, e a CI reprovaria. (Um primeiro teste com um comentário CSS
não acusou nada — corretamente, já que o minificador o remove e a saída não
muda de fato.)

## Arquiteturas

Só `linux/arm64` e `linux/amd64` por enquanto, que cobrem Orange Pi, Raspberry
Pi e qualquer PC. Outras entram quando houver quem as use.
