# TODO - release com tag, para a instalação não ficar presa ao `latest`

**Status: proposta, não implementada.**

## O problema

O GHCR só tem `latest` e `sha-*` das duas imagens, `dwnvr` e `dwnvr-detect`:
nenhuma tag `v*` foi criada ainda. O `docker-compose.yml` aponta para
`:latest`, que muda a cada push na `main`. Quem instala de verdade e roda
`docker compose up -d --pull always` recebe o que tiver acabado de entrar na
`main`, sem saber o que mudou e sem uma versão com nome para voltar.

A CI já está pronta para isso: o `docker/metadata-action` do `ci.yml` gera
`type=semver,pattern={{version}}` quando o push é de tag `v*`, e o `Makefile`
passa a carimbar a tag no binário sozinho (`git describe --tags`).

## A proposta

1. Criar a primeira tag (`v0.1.0`) e deixar a CI publicar as imagens com ela.
2. No `docker-compose.yml`, `image: ghcr.io/mhagnumdw/dwnvr:${DWNVR_VERSION:-latest}`
   (idem para o `dwnvr-detect`), para quem quiser fixar a versão pelo `.env`.
3. O "Atualizar" do README e o "Trocar de versão" do `docs/operacao.md` passam
   a dizer como fixar e como voltar.

Achado ao reformular o README em 23/09/2026.
