# Phase 4 - packaging

Multi-arch Docker image for `linux/arm64` and `linux/amd64`, validated running
on the Orange Pi Zero 3 on 2026-08-08.

## The image

`FROM scratch` with a single file inside:

| | |
| --- | --- |
| Binary | 7.9 MB |
| CA certificates | 222 KB |
| **Compressed layer** | **~3 MB** |

Three things had to exist first for `scratch` to be viable:

1. **Embedded `time/tzdata`.** dwnvr uses local time to decide which day a
   segment belongs to, and in `scratch` there is no `/usr/share/zoneinfo`.
   Without it `TZ` would be silently ignored and every day would become UTC.
   Confirmed on the Pi: the log timestamps come out with `-03:00`, not `Z`.
2. **Healthcheck in the binary itself.** There is no shell or curl in the image
   for Docker's `HEALTHCHECK` to call, so `dwnvr -healthcheck` queries the local
   instance and returns the exit code. The container reported `healthy`.
3. **Static binary** (`CGO_ENABLED=0`), which was already the case.

## Build

Both build stages run on `$BUILDPLATFORM` and Go cross-compiles for
`$TARGETARCH`. Emulating ARM to compile would be orders of magnitude slower, and
the UI doesn't depend on architecture at all - it's HTML, CSS and JS.

The UI is built **inside** the image, rather than copied from the versioned
copy, so the image always matches the source code that produced it.

## Measurements on the Pi, in a container

| | CPU | RAM |
| --- | --- | --- |
| dwnvr (9 cameras, container) | **2.82% of 1 core** | **14.1 MB** of 128 MB |

Slightly below the native binary (4.0% / 17.1 MB), mainly because segments went
from 30 s to 60 s - fewer rotations per minute.

Also verified: UI served in 118 ms, 9 cameras connected, existing index loaded
without migration, and export with strictly increasing DTS.

## The root-owned files trap, avoided

The compose file sets `user: "1000:1000"`. Without it, the container would
record as root and the files would be born root-owned on disk - which is
**exactly** what had already happened on this same Pi and which, at the start of
this project, prevented even creating a directory in `/mnt/storage`.

Confirmed after a minute recording in a container:

```text
usuario:usuario /mnt/storage/dwnvr/cam_alfa/2026-08-08/1786231487217.mp4
arquivos de root: 0
```

## Network

The Pi's `firewalld` blocked the native binary's port; Docker publishes ports by
inserting its own rules and goes around it - the same path as go2rtc. The port
opened manually during development is no longer needed.

The container reaches go2rtc through `host.docker.internal`, mapped via
`extra_hosts: host-gateway`. go2rtc is **not** part of the compose file on
purpose: coupling the two would recreate the difficulty that motivated the
project.

## CI

The workflow lives in `.github/workflows/ci.yml` **inside** the dwnvr folder and
doesn't run today: GitHub only reads workflows at the repository root, and dwnvr
is still a subfolder of `cameras`.

The placement is deliberate. It disables the workflow by **location**, not by a
flag someone might forget to revert, and leaves it exactly in the right place
for when the folder becomes an independent repository - at which point it takes
effect with no edit, since it is already written assuming dwnvr as the root.

Besides tests, `go vet` and `gofmt`, it checks that **`internal/api/dist`
matches the code in `web/`**. That check exists because forgetting
`npm run build` produces a binary that compiles and starts normally but serves
the old screen - a silent failure that would only show up in production.

The check was verified by mutation: changing a color in the CSS and rebuilding
makes `dist` diverge, and CI would fail. (A first test with a CSS comment
flagged nothing - correctly, since the minifier removes it and the output
doesn't actually change.)

## Architectures

Only `linux/arm64` and `linux/amd64` for now, which cover Orange Pi, Raspberry
Pi and any PC. Others come in when someone uses them.
