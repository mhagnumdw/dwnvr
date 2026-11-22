# TODO: the dwnvr-detect healthcheck ignores DETECT_PORT

Found by CodeRabbit in the local review of PR #98 (22/11/2026), and confirmed
in the code.

- `dwnvr-detect/server.py` listens on `DETECT_PORT` (default `8480`), and
  `dwnvr-detect/README.md` documents the variable.
- The `HEALTHCHECK` in `dwnvr-detect/Dockerfile` always connects to
  `127.0.0.1:8480`.
- With `DETECT_PORT=8481`, the detector works (with `detector.url` pointing to
  the new port), but Docker marks the container `unhealthy` after the start
  period. Nothing in the compose depends on that health today, so the only
  effect is the wrong status in `docker compose ps` and in any monitoring that
  reads it.

## Options

1. Read the port in the healthcheck: `int(__import__('os').environ.get(...))`
   inside the `-S -c` one-liner. `os` is already loaded at startup even with
   `-S`, so the cost stays near the 0.12 s measured with `_socket` (measure it
   on the ARM board before merging, as the Dockerfile comment did).
2. Drop `DETECT_PORT`: the port is internal to the compose network, and nobody
   needs to change it. Removes the variable from `server.py`, the detect README,
   `docs/detection.md` and the upgrade guide table. A contract removal, so it
   needs a line in the release notes.
3. Document the limitation in the detect README only.

## Verdict

Low priority: the default works and no known installation changes the port.
Option 1 is the smallest fix that keeps the documented variable honest; option
2 is worth it if the global configuration screen (`TODO_tela-de-configuracao-global.md`)
ends up listing `DETECT_PORT` as something the user should never touch.
