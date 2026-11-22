# dwnvr-detect

dwnvr's object detector, in a separate container. dwnvr sends the video clip of
each motion mark, and it answers what was in the last frame: person, vehicle,
animal or nothing.

It is optional. Without it, dwnvr only marks motion. It stays outside the dwnvr
binary because decoding video and running a model require native code, and dwnvr
is pure Go in an 8 MB image.

## What is inside

- `server.py` - the HTTP server: decodes the clip with PyAV and runs the model
  on onnxruntime. One request at a time; dwnvr does the queueing.
- `requirements.in` - the direct dependencies: `onnxruntime`, `numpy` and `av`.
- `requirements.txt` - generated from the `.in`: the direct and indirect ones,
  with exact version and the hash of each wheel, for amd64 and arm64.
- `model/` - the `.onnx` that goes in the image, and how to generate it again
  (see [`model/README.md`](model/README.md)).
- `sitecustomize.py` - the guard that stops the container's Python from
  compiling modules from source at runtime (see [The bytecode](#the-bytecode)).
- `check_pyc.py` - the check of the image's `.pyc`, which runs at build.
- `Dockerfile` - builds amd64 and arm64 on any machine, without emulation.

## The dependencies

The image does not run `pip install` on its own architecture. A stage on the
build machine's architecture downloads, with uv, the wheels (already compiled
packages) of the target architecture, and the final image only copies them. It
is the same design as dwnvr's `Dockerfile`, which cross-compiles Go: nothing of
the target architecture is executed at build, and that is why the Orange Pi
image comes out of an x86 machine without QEMU.

The build refuses a wheel whose hash is not the one in `requirements.txt`, and
refuses source code, which would require compiling.

To change a version, edit `requirements.in` and generate the `.txt` again:

```sh
cd dwnvr-detect
uv pip compile --universal --generate-hashes --python-version 3.12 \
  requirements.in -o requirements.txt
```

The current versions are the ones that exported the model and measured the
delivery. Another runtime is another number: after changing, check that the
boxes stay the same for the same clips.

## The bytecode

Nothing is compiled at runtime. The build generates the bytecode (`.pyc`) of the
stdlib, of the libs and of the code here, which neither the Python image nor uv
brings. Without it, every server start compiled from source what matters: 5.7 s
on an Orange Pi Zero 3, against 1.9 s with it, for 22 MB more in the image.

The compiled stdlib is the final image's own, copied into the stage without
executing anything from it, and each `.pyc` stores the hash of its `.py`, not
the date. The server starts as a module (`python3 -m server`), because a script
that Python receives by path is always compiled from source.

A `.pyc` that is missing or does not match, Python silently ignores and compiles
the source: the image would be slow with nobody noticing. Two guards prevent
that:

- **At build**, `check_pyc.py` looks at the files of the already assembled final
  image: every `.py` that the server can import must have its `.pyc`,
  checked-hash of the same Python version and with the hash of the `.py` that
  the image carries. If it does not, the build fails and the image does not come
  out.
- **At runtime**, `sitecustomize.py`, which Python loads before any import of
  the server, replaces compiling from source with an error with the file path:
  either the module comes from the `.pyc`, or the server does not start. It also
  applies to a `docker exec ... python3`; to debug by hand without the guard,
  `python3 -S`.

Only the healthcheck's one-line `-c` stays out, which is not a module.

## The model

RF-DETR Nano, trained on COCO by Roboflow (Apache-2.0), exported at 512x288 and
quantized to int8. The calibration was done with frames from ordinary security
cameras, by day and by night. On a camera of another nature (color night,
fisheye, thermal) or outside 16:9, whose frame arrives stretched, nobody has
measured yet (see
[`TODO_modelo-em-cameras-de-terceiros.md`](../docs/TODO/TODO_modelo-em-cameras-de-terceiros.md)).
Details in [`model/README.md`](model/README.md).

## The cost

On an Orange Pi Zero 3, one look takes ~6.5 s with `DETECT_THREADS=1` and ~3.6 s
with 2, for ~10% more CPU in total. Without the variable, it runs there with 2
threads: half of the board's 4 cores. The container takes ~230 MB, with peaks of
~250 MB. How many looks per hour it does depends on how many cameras have
detection on and on each one's sensitivity level: see `docs/configuration.md`.

## Local build

From the repository root, one architecture at a time:

```sh
docker buildx build --platform linux/arm64 -t dwnvr-detect:arm64 --load dwnvr-detect/
docker buildx build --platform linux/amd64 -t dwnvr-detect:amd64 --load dwnvr-detect/
```

## The contract

```text
POST /detect?minScore=0.20&frameWidth=480&quality=75
                                      body: the clip (.mp4), from the I frame to the frame to look at
GET  /health
```

```json
{
  "detections": [
    {
      "class": "person",
      "score": 0.87,
      "box": [0.31, 0.12, 0.38, 0.55]
    }
  ],
  "decodedFrames": 14,
  "width": 640,
  "height": 360,
  "timingsMs": {
    "decode": 180.2,
    "preprocess": 190.4,
    "model": 5900.1,
    "frame": 11.4
  },
  "frame": "/9j/4AAQSkZJRgABAQAAAQ… (base64)"
}
```

With `frameWidth=<width>`, and when there is a finding, the response also
carries the frame looked at as a JPEG, reduced to that width, in base64. It is
the thumbnail of the Detecções screen: the frame comes from here because here it
is already decoded. Without the parameter, or without a finding, the field is
absent.

The box is a fraction of the frame (x1, y1, x2, y2). It returns EVERY box from
`minScore` up, of any class, and decides nothing: the cut by family and the
memory of a still object belong to dwnvr, which only reads `detections`, `frame`
and `error`. The other fields are for whoever calls by hand. A clip that does
not decode to the end returns `422`: looking at the last frame left over would
be looking at another instant.

Configuration, by environment variable:

| Variable | What it is | Default |
| --- | --- | --- |
| `DETECT_MODEL` | the `.onnx` | `/app/model.onnx` |
| `DETECT_THREADS` | threads of the model and of the video | `auto` |
| `DETECT_PORT` | HTTP port | `8480` |

`auto` is half of the cores the container sees, up to 4, and never above the
container's CPU ceiling, if someone sets one (dwnvr's `docker-compose.yml` does
not). The other half stays for recording. The chosen number, and where it came
from, goes in the first line of the log:

```text
dwnvr-detect: model.onnx, input 512x288, 2 thread(s) (auto: half of 4 cores), port 8480
```

A number from 1 up overrides `auto`, and holds as given. Do not set it above the
container's CPU ceiling: with `cpus: 2`, 4 threads make the look as slow as 1,
spending double. `0` is refused, because it is onnxruntime's automatic, which
counts the machine's cores and not the container's.

With dwnvr's `docker-compose.yml`, the number goes in the `.env` next to it, for
example `DETECT_THREADS=2`, and takes effect on the next `docker compose up -d`.

The port is not 8555 because that one is go2rtc's WebRTC.

## License

The model is derived from the RF-DETR weights, Copyright 2025 Roboflow, Inc.,
under the Apache License 2.0. The copy of the license is in
[`model/LICENSE-RF-DETR`](model/LICENSE-RF-DETR).
