# Architecture: the on-disk format

This document explains **why dwnvr records the way it does**. Nothing here is
needed to install or use dwnvr - it is the rationale for the format decisions,
for anyone changing the code or wanting to understand the resource savings the
project is after.

The one-sentence summary: dwnvr consumes the fMP4 that go2rtc already produces
and cuts it into keyframe-aligned segments, without ever decoding, remuxing or
touching the media bytes.

- [Why consume go2rtc's fMP4](#why-consume-go2rtcs-fmp4)
- [Decisions the format forces](#decisions-the-format-forces)
- [Storage](#storage)
- [Generation: the init identified by hash](#generation-the-init-identified-by-hash)
- [Init parameter sets: dwnvr fixes them](#init-parameter-sets-dwnvr-fixes-them)

## Why consume go2rtc's fMP4

go2rtc already handles the hard part - RTSP, RTP depacketization, VPS/SPS/PPS
extraction and MP4 muxing. Taking advantage of that reduces the recorder to a
~150-line box reader, with no ffmpeg and no cgo. Concretely, go2rtc delivers:

- an init segment (`ftyp`+`moov`) of ~700 bytes with the parameter sets embedded
- **one `moof`+`mdat` per frame**, with the keyframe flagged in the `trun`
- a 64-bit `tfdt`, with no wraparound risk

Cutting a segment literally becomes reading one flag bit.

> **cgo** is the mechanism that lets a Go program call libraries written in C.
> Avoiding it is what keeps the binary static at ~7.6 MB, with no dependency on
> any system library, and what makes cross-compiling for ARM a
> `GOARCH=arm64 go build` and nothing more.
>
> An MP4 is made of **boxes**, each with a size and a 4-letter name. The ones
> that matter here:
>
> - **init segment** (`ftyp`+`moov`) - the header that describes the tracks
>   (codec, resolution, timescale). It has no video at all; without it the
>   player doesn't know how to decode the rest.
> - **`moof`** (*movie fragment*) - the header of a piece of video: it says
>   when and how to play the frames right behind it.
> - **`mdat`** (*media data*) - the compressed video bytes themselves. It is
>   the only box dwnvr copies without even looking at it.
> - **`trun`** (*track run*) - sits inside the `moof` and lists the frames:
>   size, duration and a bit saying whether that frame is a keyframe.
> - **`tfdt`** (*track fragment decode time*) - also inside the `moof`, it is
>   the clock: the point on the timeline where this fragment starts.
>
> And one term that isn't a box but appears above: a **keyframe** is a frame
> that decodes on its own, without depending on any other. The rest store only
> the difference from the previous one, so a file starting on them won't play.
> That is why a segment can only be cut at a keyframe - and the `trun` says, in
> one bit, which frames they are.

The box reading lives in [`internal/fmp4/`](../internal/fmp4/).

## Decisions the format forces

**Each segment is a standalone file** - it carries its own init (~700 B/minute,
negligible) and opens in VLC, in ffprobe and in an HTML `<video>` tag with no
preprocessing.

**The `tfdt` is rewritten per segment.** go2rtc delivers continuous time since
the start of the connection; without rewriting, the second segment would start
at t=24s, the third at t=48s, and so on. The rewrite keeps the box's size and
version, so no size above needs to be recalculated.

**A segment only opens on a keyframe**, otherwise it wouldn't play on its own.
Since the cut waits for the next keyframe, the real duration is the target
rounded up by the GOP: with a 4s GOP, a 30s target becomes ~31.6s.

> **GOP** (*Group of Pictures*) is the interval between two keyframes. The
> camera decides that interval - the Yoosee ones use ~4s - and it is the
> smallest "piece" video can be cut into without recompressing: since only a
> keyframe can open a file, the cut always waits for the next one.

## Storage

No database. The index is one NDJSON per camera per day, append-only:

```text
<storage.root>/
  cam_iota/
    init/4edbc50d8e70.mp4        # init segment (ftyp+moov), identified by content hash
    2026-08-08/1786220564113.mp4 # segment; the name is the start in epoch ms
    index/2026-08-08.ndjson
    events/2026-08-08.ndjson     # detection marks; only exists with detection on
    frames/2026-08-08/1786220571900.jpg  # a detection's frame; the name is its instant
```

> **NDJSON** (*Newline-Delimited JSON*) is a text file with **one complete JSON
> object per line**, with no commas and no brackets wrapping it all. It is the
> format that makes the index append-only: recording a new segment means
> appending a line to the end of the file, and reading a day means walking it
> line by line, without loading anything whole into memory. Each line looks
> like this:

```json
{
  "t": 1786220564113,
  "d": 31596,
  "sz": 2434983,
  "g": "4edbc50d8e70",
  "io": 737,
  "f0": 160567
}
```

- `t` start
- `d` duration in ms
- `sz` bytes
- `g` init generation
- `io` where ftyp+moov end
- `f0` size of the 1st fragment

That is 77 bytes per segment, ~111 KB per day per camera. The file path isn't
stored because it can be derived from `t` - storing both would leave room for
them to diverge.

Three details the format buys cheaply:

- **`io`** lets MSE delivery skip the init and serve it only once
- **`f0`** lets the server serve init + first fragment as a **one-frame MP4**:
  that is the timeline thumbnail, decoded by the browser, **without the server
  decoding anything**
- **`g` (generation)** identifies the init segment by the **hash of its
  content**, not by a counter - see below

`events/` follows the same design, append-only with one file per day, and is
only created on the first mark: a camera without detection doesn't get an empty
directory. Each line is an instant, not an interval. A motion line carries only
the instant; an object line also carries the family, the class, the confidence,
the frame that was inspected and the box as a fraction of the frame. It is one
line per object: two people arriving together are two `person` lines at the
same instant, each with its own box:

```json
{"atMs":1786220571900}
{"atMs":1786220571900,"family":"person","class":"person","score":0.87,"frameMs":1786220573300,"box":[0.41,0.32,0.47,0.62],"hasFrame":true}
```

The day's file goes away together with the day's video at retention. What each
field means is in [`detection.md`](detection.md).

### `frames/`: the detection image

`hasFrame` says that `frames/{day}/{atMs}.jpg` exists: the frame the object
detector inspected, as a 480 px JPEG. It is the thumbnail on the Detecções
(Detections) screen, and it is the only way to show the object - the segment's
keyframe can be half a minute before it. The path isn't stored, it is derived
from the instant, like the segment's.

The frame comes ready-made from `dwnvr-detect`, where it is already decoded;
dwnvr is pure Go and doesn't decode video. Since the box is a **fraction** of
the frame, it lands in the right place over the thumbnail at any size. Serving
it is opening the file and handing it over: **nothing is resized or re-encoded
per request.**

One file per detection, in one folder per day. Deleting a frame is deleting a
file, and opening one is double-clicking it. The price is the folder's
`readdir`, which the quota needs to add up bytes: each day's tally is computed
once and memoized, and the recordings that follow keep it up to date.

All the marks of the same instant, from any family, point to the same file:
there is only one frame. A mark without `hasFrame` is a mark without an image -
from before this version, or from an inference whose frame didn't come back.

The on-disk layout and the index live in [`internal/store/`](../internal/store/).

## Generation: the init identified by hash

The init segment (`ftyp`+`moov`) describes the tracks, and every segment needs
the right init to play. The index's `g` field is the SHA-256 of that init,
truncated.

Using the content as the identity, instead of a counter, buys three things for
free:

- **deduplication**: 9 cameras produce only 4 distinct init files, because all
  the H265 cameras without audio generate exactly the same bytes
- **stateless change detection**: turning audio on for a camera adds a track to
  the `moov`, the hash changes and the new segments start pointing to another
  init - without breaking playback of the old ones, and without a line of code
  dedicated to "detecting change"
- **recovery**: a counter would require persisting which number we are at; with
  a hash, reading an orphan segment is enough to know which init it belongs to

A real example, from an installation with 9 cameras:

```text
cam_teta   4edbc50d8e70   737 B   H265, video only
cam_teta   e38cb0530c62  1192 B   H265 + FLAC (after turning audio on)
cam_gama    e38cb0530c62  1192 B   identical to the above → same file
cam_eta     ead03cc7ac5a   660 B   H264 onvif1
cam_epsilon  27dcb8115adf   660 B   H264 onvif2 (different resolution)
```

## Init parameter sets: dwnvr fixes them

On Yoosee cameras go2rtc can't extract VPS/SPS/PPS from the `FmtpLine` and
writes into the `hvcC` an **SPS hardcoded in its own code**, which describes
2560x1440. With H264 the `avcC` comes out with the SDP's SPS, which on some
cameras describes a different size. The true parameter sets arrive in-band,
inside the samples.

The desktop browser, ffmpeg and mpv follow the in-band ones and decode
correctly. The hardware decoder of some Android phones doesn't: it prepares
itself from the init and draws the 640x360 frame in the corner of a 2560x1440
surface, and the image comes out at 1/4 of the size.

That is why the init is only written at the first keyframe of each connection,
already fixed: the `hvcC`/`avcC` parameter sets become the in-band ones, and
the sample entry and the `tkhd` now state the real resolution (`fmp4.FixInit`,
called by `fixaInit` in the recorder). The media bytes don't change. The fix
only happens when the in-band SPS resolution differs from the init's, and if
the fixed init doesn't parse, the original is written.

What is worth remembering:

- **The `hev1` 4CC stays.** The parameter sets keep coming in-band, which is
  what `hev1` declares, and the recordings from before the fix have only the
  dummy in the init: with `hvc1`, which asserts they are only in the sample
  entry, they would stop playing.
- **The init hash now reflects the real resolution** in new recordings, for
  H265 too: changing the camera's resolution opens a new generation. In the
  ones from before the fix, every Yoosee H265 has the same dummy init.

Details and evidence in
[go2rtc-h265-parameter-sets.md](go2rtc-h265-parameter-sets.md).
