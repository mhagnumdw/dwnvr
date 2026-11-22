# dwnvr build shortcuts.
#
# The rule that matters: the UI must be built BEFORE the binary, because Go
# embeds it with embed.FS. Forgetting that produces a binary that compiles and
# starts normally but serves the old screen - a silent error this file exists
# to prevent.

# The developer's machine configuration (DEPLOY_HOST, DEPLOY_DIR, ...). The
# hyphen in -include makes make carry on silently when the file does not
# exist, which is the case in a fresh clone. Since the variables below use ?=,
# what comes from here beats the default, and what is passed on the command
# line beats both. See local.mk.example.
-include local.mk

IMAGE    ?= ghcr.io/mhagnumdw/dwnvr
# git describe gives the short hash while there is no tag and starts giving the
# tag by itself once there is one - nothing here changes on the day of the
# first release.
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse HEAD 2>/dev/null || echo desconhecido)
DATE     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# Without this the binary cannot say which code it is.
BUILDPKG  = github.com/mhagnumdw/dwnvr/internal/buildinfo
LDFLAGS   = -s -w \
	-X $(BUILDPKG).Version=$(VERSION) \
	-X $(BUILDPKG).Commit=$(COMMIT) \
	-X $(BUILDPKG).Date=$(DATE)

# The image build cannot see .git (.dockerignore excludes it), so the same
# values have to cross the boundary as arguments.
BUILDARGS = --build-arg VERSION=$(VERSION) \
	--build-arg COMMIT=$(COMMIT) \
	--build-arg DATE=$(DATE)

.PHONY: all web build test check image image-arm64 image-amd64 deploy deploy-wip help

# Guard used by both deploys.
EXIGE_HOST = @test -n "$(DEPLOY_HOST)" || { echo "set DEPLOY_HOST in local.mk (see local.mk.example) or pass it on the command line: make deploy DEPLOY_HOST=user@server"; exit 1; }

all: web build

## web: builds the UI into internal/api/dist
web:
	cd web && npm ci --no-audit --no-fund && npm run build

## build: binary for the local machine
build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o dwnvr ./cmd/dwnvr

## image-arm64: arm64 docker image, loaded into the local docker
#
# Cross-compiles on the developer's machine (any architecture): Go crosses
# GOARCH through TARGETARCH and the stages that execute - node and go - run on
# BUILDPLATFORM, so nothing is emulated. --load leaves the image ready in the
# local docker, without going through a registry.
#
# The tag is local on purpose, without the ghcr.io of $(IMAGE): an absent-minded
# `docker push` would publish to the public registry an image that never went
# through CI.
#
# --provenance/--sbom turned off are neither fussiness nor a leftover from
# deploy-wip: with them buildx exports a manifest list even for a single
# platform, and `--load` on a docker without the containerd image store
# refuses it. Here it may work without the flags and break on someone else's
# machine.
image-arm64:
	docker buildx build --platform linux/arm64 --provenance=false --sbom=false \
		$(BUILDARGS) -t dwnvr:$(VERSION)-arm64 --load .

## image-amd64: amd64 docker image, loaded into the local docker
image-amd64:
	docker buildx build --platform linux/amd64 --provenance=false --sbom=false \
		$(BUILDARGS) -t dwnvr:$(VERSION)-amd64 --load .

## image: multi-arch image (amd64 + arm64), like the one CI publishes
#
# The two targets above produce separate images; this one produces the single
# manifest list, which is what actually goes to GHCR - the only way to check
# locally the artifact CI produces, without publishing an intermediate
# version.
#
# No --provenance/--sbom here: a manifest list is the expected result, and the
# attestations approximate what CI publishes. In exchange this target requires
# the containerd image store in the local docker (`docker info | grep
# containerd`); the classic store does not load a manifest list and --load
# fails right away.
image:
	docker buildx build --platform linux/amd64,linux/arm64 \
		$(BUILDARGS) -t dwnvr:$(VERSION) --load .

## test: unit tests, with the race detector (needs a C compiler)
test:
	go test ./... -count=1 -race

## check: what CI runs
check: test
	prek run --all-files

## deploy: updates the remote server with the image CI published
#
# Nothing is built here: the server pulls from GHCR the image CI published. To
# receive every push to main, and not only the releases, the .env over there
# needs DWNVR_VERSION=main. No `git pull` on purpose - the docker-compose.yml
# over there is usually edited. Recreating the container costs a few seconds
# of recording.
#
# DEPLOY_HOST has no default because any value here would be someone else's
# server. Set it in local.mk.
DEPLOY_HOST ?=
DEPLOY_DIR  ?= ~/dwnvr
deploy:
	$(EXIGE_HOST)
	ssh $(DEPLOY_HOST) 'cd $(DEPLOY_DIR) && docker compose up -d --pull always'
	@sleep 20
	@ssh $(DEPLOY_HOST) 'docker ps --filter name=dwnvr --format "{{.Status}}"'
	# The proof that the deploy took - this target has failed silently once.
	# If it does not match, either CI has not published yet, or the old image
	# came up.
	@echo "expected: $(VERSION)"
	@ssh $(DEPLOY_HOST) 'curl -sf localhost:8080/api/version' || echo "no response"

## deploy-wip: ships UNcommitted code to the server, just to try it out
#
# Builds arm64 here and pushes the image over ssh, without going through the
# registry - it is the only way to test on the hardware before the commit.
# After this the server runs something that exists in no commit; `make deploy`
# undoes it.
#
# It goes in with the `main` tag, the one the compose over there uses with
# DWNVR_VERSION=main, and starts without --pull always, which would pull the CI
# image over it.
deploy-wip:
	$(EXIGE_HOST)
	# --provenance/--sbom turned off: with them buildx exports a manifest list,
	# and `docker load` on the other side does not take a manifest list.
	docker buildx build --platform linux/arm64 --provenance=false --sbom=false \
		$(BUILDARGS) -t dwnvr:wip-arm64 --load .
	docker save dwnvr:wip-arm64 | gzip | ssh $(DEPLOY_HOST) \
		'gunzip | docker load && docker tag dwnvr:wip-arm64 $(IMAGE):main'
	ssh $(DEPLOY_HOST) 'cd $(DEPLOY_DIR) && docker compose up -d'
	@sleep 20
	@echo "expected: $(VERSION)"
	@ssh $(DEPLOY_HOST) 'curl -sf localhost:8080/api/version' || echo "no response"

## help: lists the targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
