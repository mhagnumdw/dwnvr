# dwnvr UI

Four screens, in Svelte 5 + TypeScript + Vite, embedded in the binary: **Ao vivo**
(live), **Gravações** (recordings), **Câmeras** (cameras) and **Diagnóstico**
(diagnostics). The whole app weighs **44.3 kB gzipped**, including go2rtc's live
player.

The build goes to `../internal/api/dist`, which Go embeds with `embed.FS` - that
is what keeps the single-binary promise.

```sh
npm install
npm run build       # generates internal/api/dist, then: go build ./cmd/dwnvr
npm run dev         # development with instant reload
npm run check       # checks the types; the commit lint runs the same
```

In `dev` mode Vite proxies `/api` to a real dwnvr, so the screen is built
against real data from the first minute. The default is `http://localhost:8080`;
to point the screen at another installation:

```sh
DWNVR_API=http://servidor:8080 npm run dev
```

The address in use shows up at Vite startup - worth checking before concluding
that something is broken, because a screen pointed at the wrong server looks
exactly like a working one.

## Why `internal/api/dist` is versioned

`go:embed` requires the files to exist at compile time. Versioning the build
makes `go build ./...` work on a clean clone, without Node installed - which
matters because the target is a device where nobody wants to install a frontend
toolchain.

When you change something in `web/`, run `npm run build` **before** committing.
Without it, prek's `interface-atualizada` hook refuses the commit.

## Structure

```text
src/lib/         the api and the types of its JSON, state (runes), route and
                 URL state, formatters, the mask for the typed time,
                 MSE player, thumbnails, frame capture, icons of the
                 object families (canvas), the UI icons (the
                 name -> drawing map of `<Icon>`), layout and drawing
                 of the boxes,
                 diagnostics of the browser of whoever is looking and of the
                 recording server, diagnostics notices (the card and the
                 header bell), the downscaling of the My account photo, the
                 canvas 2D context, fullscreen (simulated with CSS where
                 the browser does not do it, as on the iPhone), the controls
                 that fade away by themselves over the video in it, the
                 notice that the open screen fell behind an update, the
                 snackbar (the short notice at the bottom of the screen), the
                 hint that a long press opens on a button (and the long-press
                 notice for whoever asks for it), the height an element has
                 down to the end of the window, the numbers of the sideways
                 swipe that switches camera (shared by Ao vivo and
                 Gravações) and the
                 area drawn over the image (the round trip of the query,
                 the outline, the full area and the framing of the small
                 area for the zoom), and the server's check of the fields
                 of a person form, when leaving them
src/routes/      the screens, the login and the one for setting the password
                 from the invitation link; Detecções, Usuários, Minha conta
                 and the invitation one are a separate chunk. Detecções only
                 shows up with the object detector configured, Câmeras and
                 Usuários only for the admin, and Minha conta has no tab: it
                 opens from the header avatar
src/components/  canvas timeline, thumbnail strip, clock that accepts a typed
                 time, the time field and the Detecções time range (the
                 button, the chip and the De and Até fields), modal and
                 confirmation, the tab row of a form,
                 the "no camera registered" state, day picker and the
                 previous and next day arrows,
                 object detector boxes over the video (separate chunk), the
                 sheet that opens a detection with the frame and the recorded
                 clip,
                 the dismissible diagnostics card with the "copy", the
                 Diagnóstico e-mail test (separate chunk), the footer of
                 Câmeras and Diagnóstico with the time of the last health
                 read, which warns when it fails,
                 the password field with the eye, the avatar (the photo or the
                 initials), the button that opens the camera list (checked
                 out of the total), the button to enter and leave fullscreen,
                 the `<Icon>` that draws any UI icon by name,
                 the snackbar, the badge of the object families in the corner
                 of a thumbnail (with the bell, on the detection that
                 alerted), the area editor (the polygon over the frame,
                 separate chunk), the thumbnail of a camera's areas (with
                 the zoom on the small area), the chip, the tile and the
                 label of an area (its thumbnail, the name and the rule) and
                 the Alerta tab of the camera form (separate chunk)
src/vendor/      go2rtc's live player (MIT), in JavaScript, with its types
                 written by hand - see vendor/README.md
```

## Decisions

**TypeScript at maximum strictness.** `tsconfig.base.json` turns on the
`@tsconfig/strictest` set: `strict`, `noUncheckedIndexedAccess` (every `list[i]`
can be `undefined`), `exactOptionalPropertyTypes` (an optional field does not
accept a written `undefined`) and the rest. `erasableSyntaxOnly` leaves out
whatever is not just erasable types - `enum`, `namespace` -, because Svelte only
erases the types of `<script lang="ts">`, without a preprocess. That is why there
is no `svelte.config`.

There are two projects, as in Vite's `svelte-ts` template: `tsconfig.app.json`
for the screen, checked by svelte-check, and `tsconfig.node.json` for the
configs, which run on Node, checked by `tsc`. ESLint uses the rules that read
the types (typescript-eslint's `strictTypeChecked` and `stylisticTypeChecked`),
and `svelte/block-lang` refuses a component without `lang="ts"`. The exceptions
are in `eslint.config.ts`, each one with its reason.

Formatting is Prettier's, with the Svelte plugin, in `prettier.config.ts`:
single quotes and lines of up to 100, which is what the code already did. A
snippet arranged by hand on purpose, like the icon drawings, gets a
`// prettier-ignore` above it.

The contract with the server is at the end of `src/lib/api.ts`: the JSON of
each response, written from the Go structs in `internal/api`. TypeScript does
not check what arrives over the network, so changing the JSON on one side
means changing the type on the other.

TypeScript is 6, not 7: svelte-check still only uses 7 alongside 6, with the
`--tsgo` flag, and Dependabot ignores the major until that changes (see
`.github/dependabot.yml`).

**Mobile-first.** Bottom navigation on the phone and top navigation on the
desktop. The live grid accepts 1, 2 or 3 columns or "fit everything on the
screen" - the user's choice, saved in `localStorage`. The initial default
depends on the screen width (2 columns above 640 px, 1 below), but nothing is
locked: on the phone you can also ask for 3 columns.

In "fit" mode, the number of columns is not chosen - it is computed. For each
possible number, it measures the largest 16:9 tile that would fit, taking into
account the height of the resulting rows too, and the one that produces the
larger tile wins. Because the height enters the calculation, the grid never
overflows the window.

**Screen state in the URL.** The hash does not only say which screen you are on,
but what you are looking at inside it: `#live?cams=garagem&cams=entrada&view=2`,
`#rec?cam=garagem&day=2026-08-19&t=14:32:07&rate=4&paused=1`. Copying the
address bar and pasting it in another tab reproduces the same scene. It is still
a hash, and not a real path, because the fragment never leaves the browser - so
`internal/api/web.go`, which only serves files, does not need a new route.

`src/lib/route.svelte.ts` takes care of this. Each screen reads the URL once,
on mount, and an `$effect` writes back what the real state says - `paused` comes
from `player.playing`, and not from a parallel flag, so the address never
promises a playback that the browser blocked. Writes use `replaceState` with a
one-second floor, because the player's instant changes several times per second
and Safari refuses more than 100 in 30 s. Where the URL and `localStorage` talk
about the same thing - selection and layout of Ao vivo -, the URL wins; and only
a user click writes to `localStorage`, so that someone else's link does not
become your preference.

**Timeline with Pointer Events**, which covers mouse, finger and pen with the
same code. Dragging slides the visible window, tapping navigates, pinching and
the mouse wheel zoom, a double tap zooms in and a two-finger tap zooms out. A
drag never becomes navigation: the gesture is discarded if the pointer moved
past the threshold - which is larger for a finger (8 px) than for a mouse
(2 px), because fingers shake.

**MSE player written by hand, instead of hls.js.** This is what holds the size
down: the library alone would cost ~110 kB gzip, more than the whole app. In
exchange, we get exact control over the buffer window and over the recording
gaps, which the index already knows.

**Installable as an app, without a service worker.** `public/manifest.json` and
the icons make Chrome offer "Install app" and make the iPhone open the home
screen shortcut without an address bar. For that the address must be `https://`;
on `http://` Android only creates a shortcut that opens in a tab. There is no
service worker: installing does not require one, and it would bring the risk of
serving the old version of the screen after an update. For those who do not
install, the cost is the manifest, under 1 kB; the icons are only downloaded on
install.

The PNGs come from `favicon.svg`. The general-purpose ones (`icon-192`,
`icon-512`) are the drawing as is, with the rounded plate. The maskable and the
`apple-touch-icon` take the plate to the edge, because the system does the
cropping (circle, teardrop or the iPhone corner); the lens takes up 64% of the
width and fits in the 80% safe zone. To regenerate them, with ImageMagick,
pngquant and oxipng (the density is 96 dpi times the desired size over the SVG's
64 px). pngquant swaps the full RGBA colors for a palette: it cuts ~70% of the
size, and the difference stays around 68 dB of PSNR, invisible even when
enlarged.

```sh
cd web/public
sed 's/ rx="14"//' favicon.svg > /tmp/cheio.svg
magick -background none -density 288 favicon.svg -strip icon-192.png
magick -background none -density 768 favicon.svg -strip icon-512.png
magick -density 768 /tmp/cheio.svg -alpha off -strip icon-maskable-512.png
magick -density 270 /tmp/cheio.svg -alpha off -strip apple-touch-icon.png
pngquant --quality=90-100 --speed 1 --strip --ext .png --force icon-*.png apple-touch-icon.png
oxipng -q -o max -Z --strip all icon-*.png apple-touch-icon.png
```

**Live player copied from go2rtc**, not written. In live the format is go2rtc's
and the problem is already solved - including for H265, which is the hard case
of this installation. See [`src/vendor/README.md`](src/vendor/README.md).
