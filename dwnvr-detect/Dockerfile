# syntax=docker/dockerfile:1

# dwnvr-detect - o detector de objetos do dwnvr, num container à parte.
#
#   docker buildx build --platform linux/arm64 -t dwnvr-detect:arm64 --load dwnvr-detect/
#
# Faz o build de amd64 e arm64 em qualquer máquina, sem emulação. É o mesmo
# desenho do Dockerfile do dwnvr: todo comando roda na arquitetura da máquina de
# build (BUILDPLATFORM), e a imagem final só recebe COPY. Nada executa na
# arquitetura de destino durante o build.

# O uv num stage próprio, e não num `COPY --from=ghcr.io/astral-sh/uv`: esse
# atalho traz o binário da arquitetura de DESTINO, que não roda aqui.
FROM --platform=$BUILDPLATFORM ghcr.io/astral-sh/uv:0.13.6 AS uv

# Pacotes. O Go faz cross-compile; o Python não compila, mas o uv baixa os
# wheels (pacotes já compilados) de outra arquitetura sem executá-los.
#
# --require-hashes  recusa qualquer wheel cujo hash não bata com o
#                   requirements.txt
# --only-binary     recusa código-fonte, que exigiria compilar - e compilar
#                   para arm64 aqui seria emular
# --python-version  a da imagem final, e não a deste stage
FROM --platform=$BUILDPLATFORM python:3.12.15-slim-trixie AS pacotes
ARG TARGETARCH
COPY --from=uv /uv /usr/local/bin/uv
COPY requirements.txt /tmp/

# O cache é um mount, fora do filesystem do destino: sem UV_LINK_MODE=copy o uv
# tenta hardlink entre os dois e avisa a cada pacote.
ENV UV_LINK_MODE=copy
RUN --mount=type=cache,target=/root/.cache/uv \
    case "$TARGETARCH" in \
      amd64) plataforma=x86_64-manylinux_2_28 ;; \
      arm64) plataforma=aarch64-manylinux_2_28 ;; \
      *) echo "arquitetura sem suporte: $TARGETARCH" >&2; exit 1 ;; \
    esac && \
    uv pip install --target /opt/pacotes \
      --python-platform "$plataforma" --python-version 3.12 \
      --only-binary :all: --require-hashes -r /tmp/requirements.txt

# Imagem final: só COPY.
FROM python:3.12.15-slim-trixie

COPY --from=pacotes /opt/pacotes /opt/pacotes
COPY servidor.py /app/
COPY modelo/rfdetr-n_512x288_int8.onnx /app/modelo.onnx

ENV PYTHONPATH=/opt/pacotes \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    OMP_NUM_THREADS=1 \
    OPENBLAS_NUM_THREADS=1 \
    HOME=/tmp

# Sem usuário root: o servidor não escreve nada, e não precisa de nada além
# da porta.
USER 65534:65534
EXPOSE 8480

# Healthcheck com o _socket (o módulo em C por trás do socket), e não com o
# urllib.request: a imagem não traz .pyc, e o Python compilava do zero, a cada
# execução, os ~110 módulos que ele puxa. Num Orange Pi Zero 3 (4 Cortex-A53,
# load ~3), cada execução caiu de 2,1 s para 0,12 s; antes, com a placa
# ocupada, passava dos 5 s do timeout e dava unhealthy com o detector bem.
# Sem o site (-S) não existe exit(), daí o raise SystemExit.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s \
  CMD ["python3", "-S", "-c", "import _socket as s; c = s.socket(); c.settimeout(4); c.connect(('127.0.0.1', 8480)); c.sendall(b'GET /health HTTP/1.0\\r\\n\\r\\n'); raise SystemExit(c.recv(12)[9:12] != b'200')"]

CMD ["python3", "/app/servidor.py"]
