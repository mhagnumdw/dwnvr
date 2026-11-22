# Phase 1 - results

Recorder, index and retention validated on the Orange Pi Zero 3 with the 9 real
cameras recording to `/mnt/storage/dwnvr` (USB disk), 2026-08-08.

Test configuration: 30 s segments and **a quota of only 30 MB per camera**,
deliberately small so retention would act in minutes instead of days.

## Usage

| Process | CPU | RSS |
| --- | --- | --- |
| dwnvr (9 cameras + retention active) | **4.01% of 1 core** (1.0% of 4) | **17.1 MB** |
| go2rtc | 9.33% of 1 core | 22.7 MB |

Pi load average: 0.30. Total disk usage pinned at 274 MB by the quotas.

The static ARM64 binary is 6.0 MB.

## What was verified

| Check | Result |
| --- | --- |
| 9 cameras connect and record | 6× `hev1`, 3× `avc1`, zero errors |
| Segment opens on a keyframe | `key_frame=1` at `pts=0` in all |
| No re-encode | `hevc/hev1` and `h264/avc1` preserved from the camera |
| Duration aligned to the GOP | 31.6 s for a 30 s target (GOP ~4 s) |
| Index | 77 bytes/segment, ~111 KB/day/camera |
| Init dedup by hash | **2 distinct files** for the 9 cameras |
| Quota retention | `usado_mb=30 cota_mb=30 liberado_mb=3` |
| Graceful shutdown (SIGTERM) | 15 files = 15 index lines |
| Recovery from `kill -9` | orphan re-adopted with the correct duration |
| Separate-disk guard | blocked recording on the rootfs |

### Crash recovery, in detail

After the `kill -9`: 17 files against 16 index lines - one orphan of
**786,432 bytes**, exactly 3× the 256 KB write buffer. The tail still sitting
in the buffer was lost, as expected.

On restart, reconciliation probed the file and re-adopted it:

```json
{"t":1786221122736,"d":7912,"sz":786432,"g":"4edbc50d8e70","io":737,"f0":185553}
```

Duration (7.9 s) and generation recovered from the file itself. Zero errors.

The steady-state invariant is **files = index lines + 1**: the open segment only
enters the index when it closes. After a SIGTERM the numbers match.

## Two errors found by running on the real hardware

Neither would have shown up in a local test.

**1. The mount guard was semantically wrong.** I was checking whether
`storage.root` was a mount point, but the natural configuration is
`/mnt/storage/dwnvr` - a subdirectory of the `/mnt/storage` mount. The check
rejected precisely the most common case. It now compares the destination's
filesystem with the root's, which is what the guard really means: "don't record
to the system disk". Renamed to `requireSeparateDisk`.

**2. Inconsistent permissions.** `os.CreateTemp` creates with `0600`, so the
init came out `600` while the segments came out `644` - and, worse, the index
was **born** `0644` on append and **dropped** to `0600` on the first eviction, a
silent change in the middle of the file's life. That would bite in Docker with a
different UID. Fixed with an explicit `Chmod` before the rename, and locked in
by a regression test.

## Test coverage

`internal/fmp4` 77.1% · `internal/store` 76.7%

The tests build synthetic fMP4 instead of depending on recorded files, which
gives precise control over hard cases: an audio-track moof (which go2rtc marks
with the same `sampleDependsOn2` as keyframes and which therefore must never be
mistaken for a cut point), underflow in the rebase, a truncated index line and a
segment that crosses midnight.

Test quality was checked by mutation: disabling the `sample_is_non_sync_sample`
test makes `TestParseMoofDetectsKeyframe` fail.

## FLAC audio

Exercised end to end on `cam_gama` - see [audio-flac.md](audio-flac.md).
Summary: **+0.65% of a core, zero ffmpeg processes, but +34% storage**, because
FLAC is lossless and A-law has to be expanded to 16 bits before being
compressed.

The recorder detected on its own that the init had changed (two tracks instead
of one) and opened a new generation, `e38cb0530c62` against `4edbc50d8e70`, with
the init growing from 737 to 1,192 bytes - exactly the behavior the content hash
was meant to produce, with no code specific to it.

## Reconnection, verified by accident

Restarting go2rtc for the audio test dropped all 9 connections at once. dwnvr
logged `connection refused` on every one, waited out the 2 s backoff and
reconnected all 9 without intervention and without a single ERROR in the log.

## Pending

- A long-running test (24h+) for risk #2, the stability of `stream.mp4` on a
  continuous connection. The process was left running on the Pi for it.
- Confirmation of FLAC-in-MP4 on Safari.
