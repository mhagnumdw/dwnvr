# Phase 2 - HTTP API

Validated on the Orange Pi Zero 3 with the 9 cameras recording, 2026-08-08.

## Endpoints

```text
POST /api/login  /api/logout       session by signed cookie
GET  /api/session                  public: says whether login is needed
GET  /api/cameras                  registered cameras + go2rtc streams
GET  /api/health                   measured bitrate, estimated days, disk
GET  /api/rec/days                 days with recordings
GET  /api/rec/timeline             ranges to draw + segments to play
GET  /api/rec/init                 init segment, immutable
GET  /api/rec/seg                  fragments (without the init), immutable
GET  /api/rec/thumb                1-frame MP4
GET  /api/rec/playlist.m3u8        HLS VOD
GET  /api/rec/export               single stitched MP4
GET  /api/live/*                   proxy to go2rtc
```

## Checks

| Check | Result |
| --- | --- |
| `thumb` really is 1 frame | 64 KB, **1 frame**, decodes at 1920x1080 |
| `seg` really skips the init | doesn't open on its own (`no tfhd was found`); with the init, it opens |
| `init` is cacheable | `Cache-Control: immutable`, 737 bytes |
| playlist | 5 segments, `EXT-X-MAP` + `EXT-X-PROGRAM-DATE-TIME` |
| export | duration **equal to the sum of the segments**, strictly increasing DTS |
| session without a cookie | 401 on all data endpoints |
| tampered signature | 401 |
| stretched expiry | 401 (the HMAC covers the deadline) |
| `cam=../../etc` | 400 |
| `g=../../../etc/passwd` | 400 |
| RSS with API + recording | 19.7 MB |

## Three bugs found running against the real hardware

### 1. go2rtc's `medias` is a list of strings, not of objects

I had modeled `medias` as `[]struct{Kind, Direction string}`. go2rtc returns
`["video, recvonly, H265", "audio, recvonly, PCMA/16000"]` - text in SDP
format. The whole decode failed and stream discovery came back empty.

Graceful degradation saved the screen: the list of registered cameras kept
answering, with the error reported separately in `go2rtcError`. Locked in by a
test that uses a real go2rtc response as a fixture.

### 2. The indexed duration omitted the last frame

`DurMs` stored the last frame's DTS, not the end of the segment - the duration
of that frame was missing from the sum. Consequence on export: each segment was
placed exactly **on top of** the last frame of the previous one.

| | Before | After |
| --- | --- | --- |
| DTS regressions in the export | 4 (one per seam) | **0** |
| `Could not find ref` errors | ~110 | **0** |
| Export duration | 184.8 s (wrong) | equal to the sum of the segments |

This also corrects an earlier diagnosis: the flood of reference errors on export
**was this bug**, not UDP packet loss. UDP loss exists, but yields 0-2 warnings
per segment, not hundreds.

### 3. Segments overlapped in wall-clock time

The segment start comes from the wall clock and the duration from the media
clock. The two drift apart with network jitter - measured at **±1.1 s on a 30 s
segment** - and when the media ran ahead, the new segment started *before* the
previous one ended. In MSE the overlapping stretch is overwritten, meaning
recording is lost.

The start is now anchored to the end of the previous segment whenever there
would be an overlap. Measured after the fix: **0 overlaps**, with 0 ms of slack
at continuous seams.

The next question was whether anchoring like this would make the time drift from
the real clock. Measured against the files' mtimes, which are an independent
reference:

| | |
| --- | --- |
| Accumulated media | 316.76 s |
| Wall clock (mtimes) | 316.72 s |
| Difference | **+0.01%**, or ~10 s projected over 24 h |

And any real gap (reconnection, camera offline) re-anchors to the wall clock,
because then `now` passes the end of the previous one and wins the comparison. In
practice the drift doesn't accumulate.

> **Revised on 2026-09-11: it does accumulate.** The 316 s measured above were
> too short to see the drift. With a full day of recording, 97% of each camera's
> segments came out glued to the previous one, meaning the anchor always acted,
> and the timeline ran ahead of the real clock: a median of 1.5 s across the 9
> cameras, up to 21.8 s on `cam_zeta`, whose clock runs 0.04% fast. On screen,
> the time printed on the image was 7 s behind the timeline. Reconnections reset
> the drift, but not often enough.
>
> Now, within a connection, the segment starts at the seam and moves half a frame
> per segment toward an estimated clock. The estimate is the smallest difference
> between arrival and media over a 30-60 s window, which a burst doesn't
> contaminate because a burst only delays. Half a frame of overlap or gap is
> played by MSE without dropping a frame and without stalling; checked in Chrome
> with a real recording. See `internal/recorder/clock.go`.

## A scare that was correct behavior

Right after a restart, `cam_iota` showed up with 12 separate ranges and gaps of
up to 39 s. Looking into the log: go2rtc returned **HTTP 500** for ~40 s for
that camera and for `cam_alfa`. The backoff rose 1s→2s→4s→8s and it reconnected
on its own.

So the gaps were **real** and the timeline was right to show them - which is
exactly what it exists for. In the same period, `cam_alfa` and `cam_delta`,
once stable, showed a single continuous range.

## Design decisions

**The UI is served without authentication, the data is not.** HTML, CSS and JS
contain nothing from the cameras, and protecting them would stop the browser
from loading the login screen itself.

**The timeline response is compact.** A day with 1-minute segments has 1,440
entries; `segments` is `[[startMs, durationMs, generationIndex]]` with a
generation table kept separately, instead of objects with repeated keys.

**Export refuses to cross a generation change.** An MP4 has only one init;
delivering something that plays halfway would be worse than refusing. Note the
known H265 limitation: since the init is always the same dummy, a resolution
change on those cameras is not detected by this test.

## Environment note

The Pi runs `firewalld`, which blocks port 8080 from outside. go2rtc doesn't
suffer from this because Docker inserts its own rules. During development access
was through an SSH tunnel; once dwnvr is packaged in Docker in Phase 5, it will
take the same path as go2rtc.
