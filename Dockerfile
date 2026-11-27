# syntax=docker/dockerfile:1

# UI. Runs on the builder's architecture (BUILDPLATFORM) because the result is
# HTML, CSS and JS - it does not depend on the target architecture, and
# emulating Node on ARM would only waste minutes.
FROM --platform=$BUILDPLATFORM node:24-alpine AS web
WORKDIR /src/web
# package.json apart from the rest so the dependency layer is only redone when
# the dependencies actually change.
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
# vite.config.ts sends the build to ../internal/api/dist, where Go embeds it.
RUN npm run build

# Binary. Also runs on BUILDPLATFORM and cross-compiles via GOARCH: compiling
# Go under emulation would be orders of magnitude slower than cross-compiling.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETARCH
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
# The UI comes from the previous stage, not the one versioned in the
# repository: that way the image always matches the source code that produced
# it.
COPY --from=web /src/internal/api/dist ./internal/api/dist

# Build identification. It comes from outside because .dockerignore excludes
# .git/ - in here no git command would work. Declared only now, after the
# COPYs, so changing version does not invalidate the dependency layers.
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=
ARG BUILDPKG=github.com/mhagnumdw/dwnvr/internal/buildinfo

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH \
    go build -trimpath -o /dwnvr \
      -ldflags="-s -w -X $BUILDPKG.Version=$VERSION -X $BUILDPKG.Commit=$COMMIT -X $BUILDPKG.Date=$DATE" \
      ./cmd/dwnvr

# Final image: just the binary.
#
# FROM scratch is possible because the binary is static, the time zone database
# is embedded (time/tzdata) and the healthcheck is the binary itself - there is
# no shell or curl in here for Docker to call.
FROM scratch

COPY --from=build /dwnvr /dwnvr
# Certificates are only needed if go2rtc is behind HTTPS; they cost a few KB
# and avoid an obscure error for whoever uses a TLS proxy.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

VOLUME ["/etc/dwnvr", "/storage"]
EXPOSE 8080

# A scratch image has no /usr/share/zoneinfo; the time zone database is
# embedded in the binary (time/tzdata) and TZ is the only way for dwnvr to know
# which local day a segment belongs to.
ENV TZ=UTC

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/dwnvr", "-healthcheck", "-config", "/etc/dwnvr/dwnvr.yaml"]

ENTRYPOINT ["/dwnvr"]
CMD ["-config", "/etc/dwnvr/dwnvr.yaml"]
