# Atalhos de build do dwnvr.
#
# A regra que importa: a interface precisa ser construída ANTES do binário,
# porque o Go a embute com embed.FS. Esquecer disso produz um binário que
# compila e sobe normalmente, mas serve a tela antiga - um erro silencioso que
# este arquivo existe para evitar.

# Configuração da máquina de quem desenvolve (DEPLOY_HOST, DEPLOY_DIR, ...).
# O hífen do -include faz o make seguir em silêncio quando o arquivo não
# existe, que é o caso de um clone recém-feito. Como as variáveis abaixo usam
# ?=, o que vier daqui vence o default, e o que for passado na linha de comando
# vence os dois. Veja local.mk.example.
-include local.mk

IMAGE    ?= ghcr.io/mhagnumdw/dwnvr
# git describe dá o hash curto enquanto não houver tag e passa a dar a tag
# sozinho quando houver - nada aqui muda no dia do primeiro release.
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse HEAD 2>/dev/null || echo desconhecido)
DATE     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PLATFORMS = linux/amd64,linux/arm64

# Sem isto o binário não sabe dizer que código ele é. Vale para build, arm64 e
# amd64 de uma vez, já que os três compartilham LDFLAGS.
BUILDPKG  = github.com/mhagnumdw/dwnvr/internal/buildinfo
LDFLAGS   = -s -w \
	-X $(BUILDPKG).Version=$(VERSION) \
	-X $(BUILDPKG).Commit=$(COMMIT) \
	-X $(BUILDPKG).Date=$(DATE)

# O build da imagem não enxerga o .git (o .dockerignore o exclui), então os
# mesmos valores precisam atravessar a fronteira como argumentos.
BUILDARGS = --build-arg VERSION=$(VERSION) \
	--build-arg COMMIT=$(COMMIT) \
	--build-arg DATE=$(DATE)

.PHONY: all web build test check clean image image-push deploy help

all: web build

## web: constrói a interface para internal/api/dist
web:
	cd web && npm ci --no-audit --no-fund && npm run build

## build: binário para a máquina local
build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o dwnvr ./cmd/dwnvr

## arm64: binário estático para aarch64 Linux (Orange Pi, Raspberry Pi, ...)
arm64:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
		go build -trimpath -ldflags="$(LDFLAGS)" -o dwnvr-linux-arm64 ./cmd/dwnvr

## amd64: binário estático para x86_64 Linux
amd64:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -trimpath -ldflags="$(LDFLAGS)" -o dwnvr-linux-amd64 ./cmd/dwnvr

## test: testes de unidade
test:
	go test ./... -count=1

## check: o que a CI roda
check: test
	gofmt -l . | tee /dev/stderr | (! read)
	go vet ./...

## image: imagem multi-arch, carregada localmente só para a arquitetura atual
image:
	docker buildx build --platform $(PLATFORMS) $(BUILDARGS) -t $(IMAGE):$(VERSION) .

## image-push: constrói e publica a imagem multi-arch
image-push:
	docker buildx build --platform $(PLATFORMS) $(BUILDARGS) \
		-t $(IMAGE):$(VERSION) -t $(IMAGE):latest --push .

## deploy: constrói a imagem arm64 e recria o container no servidor remoto
#
# O dwnvr do servidor roda como container a partir de $(DEPLOY_DIR), e não como
# binário solto - uma versão anterior deste alvo instalava em ~/dwnvr-test, que
# há muito não é a instalação de verdade. O efeito era pior que não fazer nada:
# o deploy terminava com sucesso e o container seguia com a versão antiga.
#
# A imagem vai pelo ssh em vez de passar por um registry porque isso mantém o
# ciclo de teste independente de rede externa e de publicar versão intermediária.
# Note que recriar o container abre um buraco de alguns segundos na gravação de
# todas as câmeras.
#
# A tag dwnvr:arm64 é acordo com o docker-compose.yml que vive em $(DEPLOY_DIR),
# no servidor: mudá-la aqui faz o compose subir a imagem antiga em silêncio.
#
# DEPLOY_HOST não tem default de propósito: qualquer valor aqui seria o servidor
# de outra pessoa, e um ssh para um host que não existe é justo o tipo de falha
# silenciosa que o resto deste alvo tenta evitar. Defina no local.mk.
DEPLOY_HOST ?=
DEPLOY_DIR  ?= ~/dwnvr-docker
deploy:
	@test -n "$(DEPLOY_HOST)" || { echo "defina DEPLOY_HOST no local.mk (veja local.mk.example) ou passe na linha de comando: make deploy DEPLOY_HOST=usuario@servidor"; exit 1; }
	# --provenance/--sbom desligados de propósito: com eles o buildx exporta uma
	# manifest list, e o `docker load` do outro lado não engole manifest list.
	docker buildx build --platform linux/arm64 --provenance=false --sbom=false \
		$(BUILDARGS) -t dwnvr:arm64 --load .
	docker save dwnvr:arm64 | gzip | ssh $(DEPLOY_HOST) 'gunzip | docker load'
	ssh $(DEPLOY_HOST) 'cd $(DEPLOY_DIR) && docker compose up -d'
	@sleep 20
	@ssh $(DEPLOY_HOST) 'docker ps --filter name=dwnvr --format "{{.Status}}"'
	# A prova de que o deploy pegou: se o servidor responder outra versão, o
	# container subiu com a imagem antiga - que é o modo como este alvo já
	# falhou em silêncio uma vez.
	@echo "esperado: $(VERSION)"
	@ssh $(DEPLOY_HOST) 'curl -sf localhost:8080/api/version' || echo "não respondeu"

clean:
	rm -f dwnvr dwnvr-linux-*
	rm -rf internal/api/dist/assets

## help: lista os alvos
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
