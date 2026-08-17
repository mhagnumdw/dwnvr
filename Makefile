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

# Sem isto o binário não sabe dizer que código ele é.
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

.PHONY: all web build test check image image-arm64 image-amd64 deploy deploy-wip help

# Guarda usada pelos dois deploys.
EXIGE_HOST = @test -n "$(DEPLOY_HOST)" || { echo "defina DEPLOY_HOST no local.mk (veja local.mk.example) ou passe na linha de comando: make deploy DEPLOY_HOST=usuario@servidor"; exit 1; }

all: web build

## web: constrói a interface para internal/api/dist
web:
	cd web && npm ci --no-audit --no-fund && npm run build

## build: binário para a máquina local
build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o dwnvr ./cmd/dwnvr

## image-arm64: imagem docker arm64, carregada no docker local
#
# Compila cruzado na máquina de quem desenvolve (qualquer arquitetura): o Go
# cruza GOARCH pelo TARGETARCH e os estágios que executam - node e go - rodam no
# BUILDPLATFORM, então não emula nada. --load deixa a imagem pronta no docker
# local, sem passar por registry.
#
# A tag é local de propósito, sem o ghcr.io de $(IMAGE): um `docker push`
# distraído publicaria no registry público uma imagem que nunca passou pela CI.
#
# --provenance/--sbom desligados não são frescura nem sobra do deploy-wip: com
# eles o buildx exporta uma manifest list mesmo para uma plataforma só, e o
# `--load` de um docker sem containerd image store a recusa. Aqui pode
# funcionar sem as flags e quebrar na máquina de outra pessoa.
image-arm64:
	docker buildx build --platform linux/arm64 --provenance=false --sbom=false \
		$(BUILDARGS) -t dwnvr:$(VERSION)-arm64 --load .

## image-amd64: imagem docker amd64, carregada no docker local
image-amd64:
	docker buildx build --platform linux/amd64 --provenance=false --sbom=false \
		$(BUILDARGS) -t dwnvr:$(VERSION)-amd64 --load .

## image: imagem multi-arch (amd64 + arm64), como a que a CI publica
#
# Os dois alvos acima geram imagens separadas; este gera a manifest list única,
# que é o que de fato vai para o GHCR - a única forma de conferir localmente o
# artefato que a CI produz, sem publicar versão intermediária.
#
# Sem --provenance/--sbom aqui: uma manifest list é o resultado esperado, e as
# attestations aproximam o que a CI publica. Em compensação este alvo exige o
# containerd image store no docker local (`docker info | grep containerd`); o
# store clássico não carrega manifest list e o --load falha na hora.
image:
	docker buildx build --platform linux/amd64,linux/arm64 \
		$(BUILDARGS) -t dwnvr:$(VERSION) --load .

## test: testes de unidade
test:
	go test ./... -count=1

## check: o que a CI roda
check: test
	gofmt -l . | tee /dev/stderr | (! read)
	go vet ./...

## deploy: atualiza o servidor remoto com a imagem que a CI publicou
#
# Nada é construído aqui: o servidor baixa do GHCR a imagem que a CI publicou.
# Sem `git pull` de propósito - o docker-compose.yml de lá costuma estar
# editado. Recriar o container custa alguns segundos de gravação.
#
# DEPLOY_HOST não tem default porque qualquer valor aqui seria o servidor de
# outra pessoa. Defina no local.mk.
DEPLOY_HOST ?=
DEPLOY_DIR  ?= ~/dwnvr
deploy:
	$(EXIGE_HOST)
	ssh $(DEPLOY_HOST) 'cd $(DEPLOY_DIR) && docker compose up -d --pull always'
	@sleep 20
	@ssh $(DEPLOY_HOST) 'docker ps --filter name=dwnvr --format "{{.Status}}"'
	# A prova de que o deploy pegou - este alvo já falhou em silêncio uma vez.
	# Se não bater, ou a CI ainda não publicou, ou subiu a imagem antiga.
	@echo "esperado: $(VERSION)"
	@ssh $(DEPLOY_HOST) 'curl -sf localhost:8080/api/version' || echo "não respondeu"

## deploy-wip: leva o código NÃO commitado para o servidor, só para experimentar
#
# Compila arm64 aqui e empurra a imagem pelo ssh, sem passar pelo registry - é o
# único jeito de testar no hardware antes do commit. Depois disto o servidor roda
# algo que não existe em commit nenhum; `make deploy` desfaz.
#
# Entra com a tag que o compose de lá espera, e sobe sem --pull always, que
# baixaria a imagem da CI por cima.
deploy-wip:
	$(EXIGE_HOST)
	# --provenance/--sbom desligados: com eles o buildx exporta uma manifest
	# list, e o `docker load` do outro lado não engole manifest list.
	docker buildx build --platform linux/arm64 --provenance=false --sbom=false \
		$(BUILDARGS) -t dwnvr:wip-arm64 --load .
	docker save dwnvr:wip-arm64 | gzip | ssh $(DEPLOY_HOST) \
		'gunzip | docker load && docker tag dwnvr:wip-arm64 $(IMAGE):latest'
	ssh $(DEPLOY_HOST) 'cd $(DEPLOY_DIR) && docker compose up -d'
	@sleep 20
	@echo "esperado: $(VERSION)"
	@ssh $(DEPLOY_HOST) 'curl -sf localhost:8080/api/version' || echo "não respondeu"

## help: lista os alvos
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
