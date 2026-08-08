# syntax=docker/dockerfile:1

# Interface. Roda na arquitetura de quem constrói (BUILDPLATFORM) porque o
# resultado é HTML, CSS e JS — não depende da arquitetura de destino, e emular
# Node em ARM só desperdiçaria minutos.
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
# package.json separado do resto para que a camada de dependências só refaça
# quando as dependências realmente mudarem.
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
# O vite.config.js manda o build para ../internal/api/dist, onde o Go o embute.
RUN npm run build

# Binário. Também roda no BUILDPLATFORM e faz cross-compile via GOARCH: compilar
# Go emulado seria ordens de grandeza mais lento que compilar cruzado.
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
ARG TARGETARCH
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
# A interface vem do estágio anterior, e não a versionada no repositório: assim
# a imagem sempre corresponde ao código-fonte que a gerou.
COPY --from=web /src/internal/api/dist ./internal/api/dist

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /dwnvr ./cmd/dwnvr

# Imagem final: só o binário.
#
# FROM scratch é possível porque o binário é estático, a base de fusos horários
# vai embutida (time/tzdata) e o healthcheck é o próprio binário — não há shell
# nem curl aqui dentro para o Docker chamar.
FROM scratch

COPY --from=build /dwnvr /dwnvr
# Certificados só são necessários se o go2rtc estiver atrás de HTTPS; custam
# poucos KB e evitam um erro obscuro em quem usa proxy TLS.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

VOLUME ["/etc/dwnvr", "/storage"]
EXPOSE 8080

# Sem RTC no Orange Pi Zero 3 e sem /usr/share/zoneinfo aqui, TZ é a única
# forma de o dwnvr saber a que dia local um segmento pertence.
ENV TZ=UTC

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/dwnvr", "-healthcheck", "-config", "/etc/dwnvr/dwnvr.yaml"]

ENTRYPOINT ["/dwnvr"]
CMD ["-config", "/etc/dwnvr/dwnvr.yaml"]
