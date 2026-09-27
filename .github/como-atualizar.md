## Como atualizar

Na pasta do clone:

```sh
git pull && docker compose up -d --pull always
```

Com Podman:

```sh
git pull && PODMAN_USERNS=keep-id podman-compose --in-pod false up -d --pull-always
```

O `git pull` traz o `docker-compose.yml` desta versão, que já aponta para as imagens dela. A configuração e as gravações ficam onde estão.

Se você fixou a versão com `DWNVR_VERSION` no `.env`, troque o valor por esta versão antes de subir. Para voltar a uma versão anterior, e para os detalhes, veja o [Atualizar do README](https://github.com/mhagnumdw/dwnvr#atualizar).
