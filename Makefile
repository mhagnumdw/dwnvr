# Atalhos de build do dwnvr.
#
# A regra que importa: a interface precisa ser construída ANTES do binário,
# porque o Go a embute com embed.FS. Esquecer disso produz um binário que
# compila e sobe normalmente, mas serve a tela antiga — um erro silencioso que
# este arquivo existe para evitar.

IMAGE    ?= ghcr.io/mhagnumdw/dwnvr
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PLATFORMS = linux/amd64,linux/arm64
LDFLAGS   = -s -w

.PHONY: all web build test check clean image image-push run-pi help

all: web build

## web: constrói a interface para internal/api/dist
web:
	cd web && npm ci --no-audit --no-fund && npm run build

## build: binário para a máquina local
build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o dwnvr ./cmd/dwnvr

## arm64: binário estático para o Orange Pi (e qualquer aarch64 Linux)
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
	docker buildx build --platform $(PLATFORMS) -t $(IMAGE):$(VERSION) .

## image-push: constrói e publica a imagem multi-arch
image-push:
	docker buildx build --platform $(PLATFORMS) \
		-t $(IMAGE):$(VERSION) -t $(IMAGE):latest --push .

## run-pi: constrói o arm64 e instala no Orange Pi via ssh
PI ?= usuario@servidor.local
run-pi: web arm64
	scp -q dwnvr-linux-arm64 $(PI):/tmp/dwnvr.new
	ssh $(PI) 'cd ~/dwnvr-test && kill -TERM $$(pgrep -x dwnvr-arm64) 2>/dev/null; sleep 4; \
		mv /tmp/dwnvr.new dwnvr-arm64 && chmod +x dwnvr-arm64 && \
		nohup ./dwnvr-arm64 -config ./dwnvr.yaml > dwnvr.log 2>&1 & sleep 6; tail -3 ~/dwnvr-test/dwnvr.log'

clean:
	rm -f dwnvr dwnvr-linux-*
	rm -rf internal/api/dist/assets

## help: lista os alvos
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
