# syntax=docker/dockerfile:1

# dwnvr-detect - dwnvr's object detector, in a container of its own.
#
#   docker buildx build --platform linux/arm64 -t dwnvr-detect:arm64 --load dwnvr-detect/
#
# Builds amd64 and arm64 on any machine, without emulation. It is the same
# design as dwnvr's Dockerfile: every command runs on the build machine's
# architecture (BUILDPLATFORM), and the final image only receives COPY.
# Nothing runs on the target architecture during the build.

# The base of the final image, on the target architecture. Nothing runs on it
# during the build: the packages stage copies the stdlib from it to generate
# the .pyc, and the final image starts from it. That way the .pyc comes from
# the same .py the final image carries.
FROM python:3.12.15-slim-trixie AS target

# uv in a stage of its own, and not in a `COPY --from=ghcr.io/astral-sh/uv`:
# that shortcut brings the binary of the TARGET architecture, which doesn't run
# here.
FROM --platform=$BUILDPLATFORM ghcr.io/astral-sh/uv:0.13.7 AS uv

# Packages. Go cross-compiles; Python doesn't compile, but uv downloads the
# wheels (already-compiled packages) of another architecture without executing
# them.
#
# --require-hashes  refuses any wheel whose hash doesn't match
#                   requirements.txt
# --only-binary     refuses source code, which would require compiling - and
#                   compiling for arm64 here would mean emulating
# --python-version  the final image's, and not this stage's
FROM --platform=$BUILDPLATFORM python:3.12.15-slim-trixie AS packages
ARG TARGETARCH
COPY --from=uv /uv /usr/local/bin/uv
COPY requirements.txt /tmp/

# The cache is a mount, outside the target's filesystem: without
# UV_LINK_MODE=copy uv tries a hardlink between the two and warns on every
# package.
ENV UV_LINK_MODE=copy
RUN --mount=type=cache,target=/root/.cache/uv \
    case "$TARGETARCH" in \
      amd64) platform=x86_64-manylinux_2_28 ;; \
      arm64) platform=aarch64-manylinux_2_28 ;; \
      *) echo "unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac && \
    uv pip install --target /opt/packages \
      --python-platform "$platform" --python-version 3.12 \
      --only-binary :all: --require-hashes -r /tmp/requirements.txt

# The bytecode (.pyc) of the stdlib and the libs. The Python image doesn't
# bring the stdlib's, uv doesn't generate the libs', and without it every server
# start compiled from source what it imports: 5.7 s on an Orange Pi Zero 3,
# against 1.9 s with it, for 22 MB more in the image. compileall only compiles:
# it executes nothing of what it reads, and that is why it runs here, like uv,
# over the target's stdlib, and not this stage's.
#
# It goes in a folder of its own (PYTHONPYCACHEPREFIX), which the final image
# receives in a single COPY, instead of a __pycache__ next to each .py of its
# stdlib.
#
# --invalidation-mode checked-hash  the .pyc keeps the .py's hash, and not the
#                   date, which a COPY doesn't promise to keep
# -x                leaves out the tests, pip and what the server never imports
#
# PYTHONDONTWRITEBYTECODE applies to the Python that runs compileall, and not
# to compileall, which writes anyway. Without it, that Python would also write
# the by-date .pyc of the modules it imports itself (warnings, types...), which
# the target's Python would ignore.
#
# Checking the result is the job of the check stage, further down.
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYC_SKIP="/(test|tests|testing|idlelib|tkinter|turtledemo|ensurepip|lib2to3|pydoc_data|site-packages|_pyinstaller|f2py)/"
COPY --from=target /usr/local/lib/python3.12 /target/usr/local/lib/python3.12
RUN PYTHONPYCACHEPREFIX=/tmp/pyc python3 -m compileall -q -j0 \
      --invalidation-mode checked-hash -x "$PYC_SKIP" \
      /target/usr/local/lib/python3.12 /opt/packages && \
    mkdir /opt/pyc && \
    mv /tmp/pyc/target/usr /tmp/pyc/opt /opt/pyc/

# The one of dwnvr-detect's own code, in a folder and a layer of its own:
# changing the server doesn't redo the libs' .pyc, and whoever updates
# downloads only a few KB.
COPY server.py sitecustomize.py /app/
RUN PYTHONPYCACHEPREFIX=/opt/pyc-app python3 -m compileall -q \
      --invalidation-mode checked-hash /app

# Final image: only COPY.
FROM target AS image

COPY --from=packages /opt/packages /opt/packages
COPY --from=packages /opt/pyc /opt/pyc
COPY --from=packages /opt/pyc-app/app /opt/pyc/app
COPY server.py sitecustomize.py /app/
COPY model/rfdetr-n_512x288_int8.onnx /app/model.onnx

# Nothing compiles at runtime: Python reads the .pyc from /opt/pyc,
# sitecustomize.py turns the module that would come from source into an error,
# and with PYTHONDONTWRITEBYTECODE nothing is written to the container's layer,
# which on a board is usually on the SD card.
ENV PYTHONPATH=/opt/packages:/app \
    PYTHONPYCACHEPREFIX=/opt/pyc \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    OMP_NUM_THREADS=1 \
    OPENBLAS_NUM_THREADS=1 \
    HOME=/tmp

# No root user: the server writes nothing, and needs nothing beyond the port.
USER 65534:65534
EXPOSE 8480

# Healthcheck with _socket (the C module behind socket), and not with
# urllib.request, which pulls in ~110 modules. When the image still had no
# .pyc, Python compiled all of them from scratch on every run: on an Orange Pi
# Zero 3 (4 Cortex-A53, load ~3) it took 2.1 s and, with the board busy, went
# past the 5 s timeout and gave unhealthy with the detector fine. With _socket,
# 0.12 s, without depending on the .pyc. Without site (-S) there is no exit(),
# hence the raise SystemExit.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s \
  CMD ["python3", "-S", "-c", "import _socket as s; c = s.socket(); c.settimeout(4); c.connect(('127.0.0.1', 8480)); c.sendall(b'GET /health HTTP/1.0\\r\\n\\r\\n'); raise SystemExit(c.recv(12)[9:12] != b'200')"]

# As a module (-m), and not by the file's path: Python always compiles from
# source a script it receives by path, and never looks for a .pyc for it.
CMD ["python3", "-m", "server"]

# The check of the image's .pyc above, mounted here without executing anything
# from it, with the build machine's Python: every .py the server can import has
# its .pyc, checked-hash of the same version and with the hash of the .py the
# image carries (see check_pyc.py). If not, the build fails. The final image
# copies an empty folder from here just to depend on this stage: without that,
# BuildKit would skip it.
FROM packages AS check
RUN --mount=type=bind,from=image,target=/image \
    --mount=type=bind,source=check_pyc.py,target=/check_pyc.py \
    python3 /check_pyc.py /image && \
    mkdir /checked

FROM image
COPY --from=check /checked/ /
