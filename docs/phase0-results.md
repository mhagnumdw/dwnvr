# Phase 0 - feasibility test results

Measurements taken on 2026-08-08 against the Orange Pi Zero 3 (Armbian 6.12.58
sunxi64, 4 Cortex-A53 cores, 1,471 MB RAM) with the 9 real cameras.

Before the project existed, two throwaway programs answered the two questions
that decided the design: whether recording go2rtc's fMP4 without decoding fit
the CPU and RAM budget, and whether the browser could play the recorded
segments via MSE. Both answers were yes; the programs did not become product
and are no longer in the repository. What remains is the measurement below and
the production code it justified.

## Resource usage - the central requirement

5 minutes recording the **9 cameras simultaneously**, 60 s segments:

| Process | CPU | RSS |
| --- | --- | --- |
| dwnvr (recorder) | **4.98% of 1 core** (1.2% of 4) | **16.4 MB** |
| go2rtc | 16.9% of 1 core | 15.9 MB |
| **total** | **~22% of 1 core (5.5% of the system)** | **~32 MB** |

Go heap: 5.1 MB in use, 19.6 MB allocated in total over 5 min, 5 GCs. The
buffer reused between boxes (`fmp4.Reader`) is what keeps allocation at that
level: without it there would be ~135 allocations/second for fragments alone.

The plan's target was < 100 MB and < 10% of a core. It came in at a third of
that. Pi load average during the test: 0.12.

## Actual data rate per camera (`onvif1` stream)

| Camera | kbps | GB/day |
| --- | --- | --- |
| cam_alfa | 928 | 9.6 |
| cam_iota | 928 | 9.6 |
| cam_gama | 792 | 8.2 |
| cam_delta | 683 | 7.0 |
| cam_zeta | 655 | 6.8 |
| cam_eta | 655 | 6.8 |
| cam_teta | 410 | 4.2 |
| cam_beta | 328 | 3.4 |
| cam_epsilon | 164 | 1.7 |
| **total** | **5,523** | **55.6** |

Well below the ~12.6 Mbps I had estimated: these cameras' H265 is more
efficient than expected on a still scene. **55.6 GB/day** with all 9 at high
resolution is the number that sizes retention.

## Format delivered by go2rtc

- init segment (`ftyp`+`moov`) of **737 bytes**, with VPS/SPS/PPS in the `hvcC`
- **one `moof`+`mdat` per frame**, keyframe flagged in `trun` via
  `sample_is_non_sync_sample` (constant `0x10000` in `pkg/iso/atoms.go`)
- `tfdt` **version 1 (64 bits)** - no wraparound risk
- sample entry 4CC: **`hev1`** (the `Content-Type` announces `hvc1`, but the box
  is `hev1`; `hev1` works in Chrome, **Safari only accepts `hvc1`**)
- keyframe every ~3.9 s (GOP 60 @ 15 fps), the stream always starts on a
  keyframe

### Two fixes the measurement forced

1. **`tfdt` must be rewritten per segment.** go2rtc delivers continuous time
   since the start of the connection, so without the rewrite the 2nd segment
   started at t=24s and the 3rd at t=48s: the files opened, but announced a
   growing duration and a gap at the start. Fixed in `internal/fmp4/rebase.go`,
   which subtracts the base in place, keeping the box's size and version.

2. **A segment may only open on a keyframe.** That was already the design, and
   the test confirmed it: after the fix, every segment has its 1st frame with
   `key_frame=1` at `pts=0`.

## Wrong dimensions in the container (a go2rtc limitation)

| Source | Resolution |
| --- | --- |
| `stsd/hev1` written by go2rtc | 2560x1440 |
| Actual decoding (ffmpeg→PNG and WebCodecs) | **1920x1080** |

The true resolution is 1080p, as the comment in `go2rtc.yaml` already said;
it's `ffprobe` that reports it wrong, reading the container.

> **Fixed later.** Here I attributed this to an SPS reading error. The real
> cause is different and has design consequences: go2rtc writes a **hardcoded**
> SPS when it can't extract it from the `FmtpLine`, and the true parameter sets
> come in-band. That makes the `hev1` 4CC mandatory and blinds codec-change
> detection by hash on H265. See
> [go2rtc-h265-parameter-sets.md](go2rtc-h265-parameter-sets.md).

## Playback in the browser

Chrome 151 / Fedora 43 / Intel GPU:

- `MediaSource.isTypeSupported` → **true** for `hev1.1.6.L153.B0` and `hvc1.1.6.L153.B0`
- `VideoDecoder.isConfigSupported` → **true** for both
- **actual decoding of a recorded keyframe via WebCodecs: OK**, it returned a
  1920x1080 BGRX `VideoFrame`

### MSE end to end - confirmed

The 4 recorded segments stitched into a single `<video>` via `SourceBuffer`:

| Measure | Result |
| --- | --- |
| Append 4 segments (79 s, ~9 MB) | **109 ms** |
| `buffered` | **a single range `[0, 79.06]`** - no seam between segments |
| Frames decoded | **1,199, with 1 dropped** |
| `videoWidth` x `videoHeight` | **1920x1080** (corrected by the bitstream) |
| Seek to t=50s across a segment boundary | OK |

Two conclusions that support the recordings screen's design:

1. **`timestampOffset` is enough to stitch standalone segments.** Each segment
   has its own `ftyp+moov` and starts at t=0; placing it on the timeline is an
   assignment, not a remux. Appending a segment costs ~27 ms.
2. **The browser ignores the container's wrong dimensions** and uses the
   bitstream's, which confirms that go2rtc bug is cosmetic.

**Risk #1 knocked down.** And, as a bonus, WebCodecs also works on this
hardware, so the MSE player has a proven alternative should some edge case
appear.

## Environment issues

- `/mnt/storage` (USB disk, 220 GB) belongs to root and the user has no
  passwordless sudo - the measurements were taken on the SD card. It needs
  `sudo chown $USER /mnt/storage` (or a subdirectory) before Phase 1.
- The disk has **186 GB taken by old recordings from another NVR**, leaving
  23 GB. At 55.6 GB/day that is ~10 hours of recording.
