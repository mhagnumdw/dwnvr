# Audio: measuring FLAC mode

Measured on the Orange Pi Zero 3 on 2026-08-08, with `cam_gama` (H265, Yoosee).
For the test, `#media=video` was removed from the source in `go2rtc.yaml` -
without that the audio is dropped at the source and nothing reaches dwnvr.

## Result

| | Value |
| --- | --- |
| Source codec | `PCMA/16000` (G.711 A-law, mono) |
| Recorded codec | `fLaC` 16 kHz mono, `dfLa` box with a 34-byte STREAMINFO |
| **CPU cost** | **+0.65% of one core** per camera |
| **ffmpeg processes** | **zero** |
| **Audio bitrate** | **~260 kbps** |
| Video bitrate, same capture | ~770 kbps |
| **Storage impact** | **+34%** |

The CPU measurement was made by comparing go2rtc's cumulative time in
`/proc/<pid>/stat` over two 40s windows, with and without a FLAC consumer:
13.22% against 13.87% of one core. Instantaneous `docker stats` samples
suggested 3-6%, but that was noise.

The absence of ffmpeg was confirmed with `docker top go2rtc`: only go2rtc's own
process exists. The A-law→PCM→FLAC conversion happens in pure Go.

## The size paradox

FLAC is **lossless** compression, and the path is
`A-law (8 bits) → linear PCM (16 bits) → FLAC`. Since A-law is already a lossy
compression at 8 bits per sample, expanding it to 16 bits doubles the data
before FLAC gets a chance to compress it - and the result came out at ~260
kbps, which is **more than the 128 kbps of the original A-law**.

In other words, recording in FLAC takes twice what storing the raw A-law would.
But raw A-law doesn't play in any browser, and that is exactly the problem FLAC
solves for free in CPU.

## How to choose

The decision is between CPU and disk, and depends on which of the two is
scarce:

- **`flac`** when disk is plentiful and CPU is short. It doesn't spawn ffmpeg,
  it starts together with the recording and it doesn't depend on any extra
  go2rtc configuration.
- **`aac`** when CPU is plentiful and disk is short. It costs ~10% of one core
  per camera (the user's measurement with Opus, which uses the same ffmpeg
  path), but fits in ~64 kbps and plays anywhere, including in exported MP4s.
- **`none`** for most cameras. Audio is rarely useful on all of them.

That is why the mode is **per camera**, not global: the sensible default is
`none` everywhere and audio on only where it matters - door and gate,
typically.

## Browser compatibility

Chrome 151 / Fedora 43:

- `MediaSource.isTypeSupported` with `hev1.1.6.L153.B0,flac` → **true**
- `AudioDecoder.isConfigSupported` with `flac` + the `dfLa` STREAMINFO → **true**
- `ffmpeg -i segmento -f null -` decodes both tracks without error

Safari remains the unknown: FLAC inside MP4 is rare. Anyone who needs broad
compatibility in exported files should prefer `aac`.

## How dwnvr finds out the camera has audio

go2rtc only lists a stream's tracks (`medias`, in `GET /api/streams`) while
someone is consuming it: without a consumer it doesn't even open the connection
to the camera. For a camera not yet registered this was a chicken-and-egg
problem - the registration form needed to know whether it can offer audio
precisely when nobody was consuming.

Measured with `cam_teste5` from `go2rtc.example.yaml`, which delivers
`H264 + PCMA/16000`:

| stream state | `medias` in go2rtc |
| --- | --- |
| idle | absent |
| with a consumer attached | `["video, recvonly, H264", "audio, recvonly, PCMA/16000"]` |

That is why `GET /api/streams/probe` exists: on an idle stream it opens
`stream.mp4?mp4=flac` for a few seconds, reads the `moov` and the `medias` that
show up, and closes. On a stream that already has a live producer - every
registered camera that is recording - it answers from `medias` without opening
anything.

The FLAC mode in the request is not a detail: `mp4=flac` is the only filter
that brings the audio track along, so it is the only one that produces a `moov`
able to answer the question.

## Side effect of turning audio on at the source

Removing `#media=video` makes go2rtc pull the camera's audio track 24/7, even
if nobody asks for audio: that is ~129 kbps more on the link to the camera, per
camera. On 2.4 GHz installations - like the one that motivated this project -
that competes for airtime with the cameras themselves and is not negligible
when multiplied by several.
