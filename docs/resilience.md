# Resilience: how dwnvr survives outages and silent streams

An NVR spends 24 hours a day waiting for nothing to go wrong, and what does go
wrong rarely announces itself. This document describes the two failure modes
dwnvr faces for real - a power cut and a go2rtc that goes silent - and the
defense against each.

- [Recovering from an outage](#recovering-from-an-outage)
- [When go2rtc goes silent](#when-go2rtc-goes-silent)

## Recovering from an outage

The index is written **after** the segment is closed. An outage between the two
leaves an orphan file, which the boot reconciliation adopts again by probing
the file. The reverse order would leave the index pointing at something that
never existed.

In a real power cut, the order is not enough: the index line is `Sync`ed and
the video is not, so the disk can end up with the line and a zero-byte file.
That is why the reconciliation deletes every zero-byte segment, and its line
leaves the index. An orphan without an init or without a fragment doesn't get
in either. A segment without video becomes a gap in the timeline, and not a
stretch that won't play. A `Sync` per segment would avoid the loss, but it
would be one fsync per camera every 30 s on a USB HDD, to save half a minute of
video.

Only the most recent day is reconciled: that is where the damage from an outage
lives, and checking only it avoids scanning hundreds of thousands of files on
every boot.

Verified on the Orange Pi Zero 3: `kill -9` left 1 orphan of 786,432 bytes (the
buffered tail was lost); the restart adopted it again with the correct
duration.

## When go2rtc goes silent

The most dangerous failure of an NVR is not stopping recording: it is stopping
recording **without telling anyone**. And go2rtc produces exactly that.

Its RTSP producers run over UDP. When the camera's flow stops, there is no
socket error at all - go2rtc simply stops writing, with the HTTP response left
open. On our side, `Read` blocks forever: no error, no EOF, no log, and nothing
triggers the reconnection.

It happened on 2026-08-09: all 9 cameras stopped at 08:18. Four came back on
their own 2h30 later, when go2rtc recreated the producer by itself. Five stayed
**stopped for 3h38** reporting `connected: true` and `reconnects: 0`. Meanwhile
the log piled up 380 warnings - all about thumbnail 404s, none about the
cameras.

That is why every open stream carries an inactivity threshold (`stallSeconds`,
15s by default): past it without receiving a byte, the connection drops and the
backoff that already exists reconnects.

Closing the connection is also what **recovers**. Since dwnvr is the only
consumer of the stream, leaving makes go2rtc drop the dead producer, and the
reopening forces a new RTSP session. Measured on a camera that had been stopped
for 3h37: it was recording again in under 1 second.

The first read gets double the allowance, because opening the stream makes
go2rtc establish the RTSP session with the camera - legitimately slower than
delivering the next fragment of a stream that is already running.

The threshold is adjustable per camera: see `stallSeconds` in
[configuration.md](configuration.md). The implementation is in
[`internal/go2rtc/client.go`](../internal/go2rtc/client.go).
