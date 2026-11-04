# syntax=docker/dockerfile:1

# dwnvr-detect - o detector de objetos do dwnvr, num container à parte.
#
#   docker buildx build --platform linux/arm64 -t dwnvr-detect:arm64 --load dwnvr-detect/
#
# Faz o build de amd64 e arm64 em qualquer máquina, sem emulação. É o mesmo
# desenho do Dockerfile do dwnvr: todo comando roda na arquitetura da máquina de
# build (BUILDPLATFORM), e a imagem final só recebe COPY. Nada executa na
# arquitetura de destino durante o build.

# A base da imagem final, na arquitetura de destino. Nada roda nela durante o
# build: o stage pacotes copia dela o stdlib para gerar o .pyc, e a imagem final
# parte dela. Assim o .pyc sai do mesmo .py que a imagem final traz.
FROM python:3.12.15-slim-trixie AS destino

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

# O bytecode (.pyc) do stdlib e das libs. A imagem do Python não traz o do
# stdlib, o uv não gera o das libs, e sem ele cada subida do servidor compilava
# do fonte o que importa: 5,7 s num Orange Pi Zero 3, contra 1,9 s com ele, por
# 22 MB a mais na imagem. O compileall só compila: não executa nada do que lê,
# e por isso roda aqui, como o uv, sobre o stdlib do destino, e não o deste
# stage.
#
# Vai numa pasta à parte (PYTHONPYCACHEPREFIX), que a imagem final recebe num
# COPY só, em vez de um __pycache__ ao lado de cada .py do stdlib dela.
#
# --invalidation-mode checked-hash  o .pyc guarda o hash do .py, e não a data,
#                   que um COPY não promete manter
# -x                fora os testes, o pip e o que o servidor nunca importa
#
# O PYTHONDONTWRITEBYTECODE vale para o Python que roda o compileall, e não
# para o compileall, que grava de qualquer jeito. Sem ele, esse Python gravaria
# junto o .pyc por data dos módulos que ele mesmo importa (warnings, types...),
# que o Python do destino ignoraria.
#
# A conferência do resultado é do stage confere, lá embaixo.
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYC_FORA="/(test|tests|testing|idlelib|tkinter|turtledemo|ensurepip|lib2to3|pydoc_data|site-packages|_pyinstaller|f2py)/"
COPY --from=destino /usr/local/lib/python3.12 /destino/usr/local/lib/python3.12
RUN PYTHONPYCACHEPREFIX=/tmp/pyc python3 -m compileall -q -j0 \
      --invalidation-mode checked-hash -x "$PYC_FORA" \
      /destino/usr/local/lib/python3.12 /opt/pacotes && \
    mkdir /opt/pyc && \
    mv /tmp/pyc/destino/usr /tmp/pyc/opt /opt/pyc/

# O do código do dwnvr-detect, numa pasta e numa camada à parte: mudar o
# servidor não refaz o .pyc das libs, e quem atualiza baixa só alguns KB.
COPY servidor.py sitecustomize.py /app/
RUN PYTHONPYCACHEPREFIX=/opt/pyc-app python3 -m compileall -q \
      --invalidation-mode checked-hash /app

# Imagem final: só COPY.
FROM destino AS imagem

COPY --from=pacotes /opt/pacotes /opt/pacotes
COPY --from=pacotes /opt/pyc /opt/pyc
COPY --from=pacotes /opt/pyc-app/app /opt/pyc/app
COPY servidor.py sitecustomize.py /app/
COPY modelo/rfdetr-n_512x288_int8.onnx /app/modelo.onnx

# Nada se compila em runtime: o Python lê o .pyc do /opt/pyc, o
# sitecustomize.py transforma em erro o módulo que viria do fonte, e com o
# PYTHONDONTWRITEBYTECODE nada se grava na camada do container, que numa placa
# costuma estar no cartão SD.
ENV PYTHONPATH=/opt/pacotes:/app \
    PYTHONPYCACHEPREFIX=/opt/pyc \
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
# urllib.request, que puxa ~110 módulos. Quando a imagem ainda não tinha .pyc,
# o Python compilava todos eles do zero a cada execução: num Orange Pi Zero 3
# (4 Cortex-A53, load ~3) levava 2,1 s e, com a placa ocupada, passava dos 5 s
# do timeout e dava unhealthy com o detector bem. Com o _socket, 0,12 s, sem
# depender do .pyc. Sem o site (-S) não existe exit(), daí o raise SystemExit.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s \
  CMD ["python3", "-S", "-c", "import _socket as s; c = s.socket(); c.settimeout(4); c.connect(('127.0.0.1', 8480)); c.sendall(b'GET /health HTTP/1.0\\r\\n\\r\\n'); raise SystemExit(c.recv(12)[9:12] != b'200')"]

# Como módulo (-m), e não pelo caminho do arquivo: o script recebido pelo
# caminho o Python sempre compila do fonte, e nunca procura .pyc para ele.
CMD ["python3", "-m", "servidor"]

# A conferência do .pyc da imagem acima, montada aqui sem executar nada dela, com
# o Python da máquina de build: todo .py que o servidor pode importar tem o seu
# .pyc, checked-hash da mesma versão e com o hash do .py que a imagem traz (ver
# confere_pyc.py). Se não tiver, o build falha. A imagem final copia daqui uma
# pasta vazia só para depender deste stage: sem isso o BuildKit o pularia.
FROM pacotes AS confere
RUN --mount=type=bind,from=imagem,target=/imagem \
    --mount=type=bind,source=confere_pyc.py,target=/confere_pyc.py \
    python3 /confere_pyc.py /imagem && \
    mkdir /conferido

FROM imagem
COPY --from=confere /conferido/ /
