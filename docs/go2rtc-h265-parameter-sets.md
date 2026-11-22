# H265: go2rtc writes fake parameter sets into the container

Discovered on 2026-08-08 when switching `cam_iota` from `onvif1` (1080p) to
`onvif2` (640x360). **This corrects an earlier conclusion in this
documentation**, which treated the matter as a cosmetic SPS-reading error.

## What happens

For H265 tracks, go2rtc's `pkg/mp4/muxer.go` does:

```go
vps, sps, pps := h265.GetParameterSet(codec.FmtpLine)
if len(sps) == 0 {
    sps = []byte{0x42, 0x01, 0x01, 0x01, 0x40, ...} // SPS hardcoded
}
```

On Yoosee cameras the `FmtpLine` does **not** carry the parameter sets, so
`GetParameterSet` returns empty and go2rtc writes the **SPS embedded in its own
code** into the `hvcC`. That SPS describes 2560x1440 - hence the wrong
resolution that shows up in `ffprobe`.

Confirmed byte by byte: go2rtc's hardcoded sequence is present, literally, in
the init segment recorded by dwnvr.

The true parameter sets arrive **in-band**, inside the samples. That is why
decoding comes out right even though the container lies.

> **Since 2026-10-12 dwnvr fixes the init at recording time**, replacing its
> parameter sets with the in-band ones (see §Init parameter sets in
> [architecture.md](architecture.md#init-parameter-sets-dwnvr-fixes-them)).
> The reason was a real victim: on Chrome 154 on an Android 10, the hardware
> decoder was guided by the init and the 640x360 recordings showed up at 1/4 of
> the size, in the corner of the frame, in the player and in the thumbnails.
> Live, which doesn't go through the init, showed up right. What follows
> describes the recordings from before the fix.

## Evidence

`cam_iota` after switching to `onvif2`:

| | Value |
| --- | --- |
| Resolution in the container | 2560x1440 |
| **Resolution actually decoded** | **640x360** |
| Bitrate | 46-55 kbps (against ~928 kbps on `onvif1`) |
| Framerate | 10 fps (`onvif1` is 15 fps) |

And the test that closes the case: changing the camera's resolution **did not
change the init hash** - because the init never contained the real SPS, only
the dummy, which is always the same.

## Consequence 1 - `hev1` is mandatory, not cosmetic

The sample entry's 4CC is `hev1`, which allows in-band parameter sets. `hvc1`
asserts the opposite: that they are **only** in the sample entry.

Since here the sample entry contains dummies, **converting `hev1` to `hvc1`
would break playback**. The idea of changing that 4CC to please Safari - which
accepts `hvc1` and not `hev1` - is dropped for these cameras. Anyone who needs
Safari will have to solve it another way.

## Consequence 2 - the init hash doesn't detect a codec change in H265

The index's `Gen` is the init hash. For H264 this works: `cam_epsilon` switched
from `onvif1` to `onvif2` and opened a new generation (`ead03cc7ac5a` →
`27dcb8115adf`), with both inits living on disk, exactly as designed.

For H265 the mechanism is blind: since the init is always the same dummy,
`cam_iota` kept `4edbc50d8e70` when going from 1080p to 640x360.

**This doesn't break playback**, because the decoder uses the in-band parameter
sets - verified by actually decoding a 640x360 segment recorded with the dummy
init. The effect is that, for H265, `Gen` doesn't mean "same codec
configuration"; it only means "same set of tracks".

Where this can be a nuisance: an export that spans a resolution change produces
an MP4 whose container describes one resolution and whose bitstream changes
midway. Players follow the in-band data and usually cope well, but it is a case
to test when export is implemented. (Not every player: see the notice at the
top.)

Detecting the change for real would require comparing the in-band parameter
sets, that is, walking NAL units inside the samples - real work that doesn't
pay off now, since playback isn't affected.

## Don't confuse it with data loss

`ffmpeg` sporadically emits `Could not find ref with POC N` on these
recordings. It is not dwnvr losing data: the frame rate of the recorded
segments is identical to that of the capture taken straight from go2rtc.

| Source | fps |
| --- | --- |
| Straight from go2rtc | 10.02 |
| dwnvr segments | 9.94 · 9.96 · 10.00 · 9.99 |

The cause is UDP packet loss on the link to the camera (the sources use
`#transport=udp`), which crosses both paths equally.
