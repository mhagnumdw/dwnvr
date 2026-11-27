# Third-party code

## video-rtc.js and video-stream.js

From [go2rtc](https://github.com/AlexxIT/go2rtc) (`www/`), MIT, © 2022 Alexey Khit.

Copied without modification. They are the live-view player: they negotiate
WebRTC, MSE and MJPEG on their own, picking what the browser accepts, and are
already battle-tested with H265 - which is precisely the hard case of this
installation.

Reusing them instead of writing a live player is deliberate: dwnvr writes the
player for the *recordings*, where the format is its own and fine control of
the timeline matters. In the live view, the format is go2rtc's and the problem
is already solved.

Because they are copied, they stay in JavaScript. What dwnvr uses from them has
its types handwritten next to them, in `video-rtc.d.ts` and `video-stream.d.ts`.

To update, and then check that the names in the `.d.ts` files still exist in
the new `.js`:

```sh
curl -O https://raw.githubusercontent.com/AlexxIT/go2rtc/master/www/video-rtc.js
curl -O https://raw.githubusercontent.com/AlexxIT/go2rtc/master/www/video-stream.js
```
