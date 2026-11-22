# Phase 3 - the UI

SPA in Svelte 5 + Vite, embedded in the binary. Validated against the Orange Pi
Zero 3 with the 9 cameras recording, 2026-08-08.

## Size

| | raw | gzip |
| --- | --- | --- |
| JS (includes go2rtc's live player) | 88.2 kB | **32.6 kB** |
| CSS | 9.9 kB | 2.7 kB |
| HTML | 0.5 kB | 0.3 kB |

The ARM64 binary with everything embedded: 6.8 MB. Serving the bundle from the
Pi took 17 ms.

Writing the MSE player instead of using hls.js is what holds this number down:
the library alone would cost ~110 kB gzip, more than tripling the whole app.

## The four screens

**Ao vivo** (Live) - a grid of 1 to 3 columns (always 1 on a phone), selection
persisted in localStorage. It uses go2rtc's own `<video-stream>`, which
negotiates WebRTC/MSE by itself. Confirmed on the Pi: both tiles connected in
MSE mode through dwnvr's proxy, decoding 1920x1080.

**Gravações** (Recordings) - a 24 h timeline on a canvas, a thumbnail strip and
the MSE player. Verified: the UI clock showed 19:58:52 at the same instant the
OSD recorded by the camera showed 19:58:52, which validates the wall-clock
mapping end to end.

**Câmeras** (Cameras) - registration from the streams go2rtc already serves,
with the audio mode disabled on cameras that don't deliver an audio track. The
quota always appears alongside the retention estimated at the measured rate.

**Diagnóstico** (Diagnostics) - a disk gauge separating what belongs to dwnvr
from what belongs to others, totals (9 connected, 4.52 Mbps, 45.5 GB/day) and a
list of warnings that explains problems before they turn into a mystery.

## Thumbnails at no cost on the Pi

Each thumbnail is the init plus the first fragment of the segment - a one-frame
MP4 cut out of what is already recorded. The browser does the decoding, via
WebCodecs, with a hidden `<video>` as a fallback.

The Pi only cuts bytes. Confirmed on screen: the strip shows real frames every
~30 s of recording.

## Registration applies hot

Registering a camera and having to restart the service for it to record would be
a good way to lose recording without noticing. The manager gained `Set` and
`Remove`, each recorder with its own cancellation.

Two decisions within that:

- **Restart only when what the recorder uses changes.** Changing the display
  name or the quota doesn't drop the connection; changing the audio or the
  segment duration does.
- **Retention now reads the live list** instead of a copy made at startup. A
  quota changed from the screen has to take effect on the next pass.

And removing a camera **does not delete the recordings**: deleting hours of
video as a side effect of a click would be too destructive to be implicit. The
API responds with the path where they were left.

## Responsiveness

Mobile-first: the base rules apply to the phone and the media queries add what
the big screen allows.

- bottom navigation on the phone (thumb reach), top on the desktop
- touch targets of at least 44 px
- live grid locked to one column below 640 px - two videos side by side on a
  phone show nothing useful of either
- the registration form slides up as a sheet from the bottom, respecting
  `env(safe-area-inset-bottom)`
- timeline with Pointer Events: dragging navigates, pinching zooms, and
  `touch-action: none` keeps the page from scrolling along

Measured at 500 px wide: **no horizontal scroll**, bottom navigation active, top
one hidden.

## Three fixes found by looking at the finished screen

**The clock stayed at `--:--:--` with the video already visible.** `currentMs`
was only updated on `timeupdate`, which doesn't fire when autoplay is blocked.
It now listens to `seeked` and `loadeddata` and reflects the position right
after the seek.

**The estimated retention showed as "0h".** Small quotas give fractions of an
hour, and rounding everything to hours told nothing. It now shows minutes below
an hour.

**Live showed a progress bar.** go2rtc's component creates the `<video>` with
`controls=true`, which on a live grid shows a timeline that doesn't exist - and
it even showed up on some cameras and not others, depending on the negotiated
mode. Turned off, with double-click for full screen instead.

## Note on the versioned build

`go:embed` requires the files to exist at compile time, so `internal/api/dist`
is versioned. That makes `go build ./...` work on a clean clone without Node
installed - which matters when the target is a device where nobody wants to
install a frontend toolchain.

When changing `web/`, you must run `npm run build` before committing.
