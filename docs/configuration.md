# Configuration

What dwnvr reads, what it writes, and what each recording policy costs in CPU
and disk.

- [The two files](#the-two-files)
- [Per-camera policy](#per-camera-policy)
- [Value out of range](#value-out-of-range)
- [Retention](#retention)
- [Audio](#audio)
- [Login attempt limit](#login-attempt-limit)
- [E-mail](#e-mail)

## The two files

- **`dwnvr.yaml`** - infrastructure, edited by hand, **never rewritten** by the
  application. Every field is commented in
  [`dwnvr.example.yaml`](../dwnvr.example.yaml).
- **`cameras.json`** - the list of cameras, written by the camera registration
  screen.

They are separate because rewriting a YAML file erases the comments of whoever
wrote it, and the registration screen has to save cameras on every click. The
split also guarantees that a bug in the registration screen can't corrupt your
infrastructure: dwnvr never writes to `dwnvr.yaml`.

Both live in the same directory - `/etc/dwnvr` in the Docker image - next to
`.session-secret`, which signs the session cookies, and `users.json`, the
people who log in besides the owner in `dwnvr.yaml`, which the Usuários
(Users) screen and Minha conta (My account) write (see
[Quem entra](api.md#who-signs-in)). `users.json` is `0600`, like
`.session-secret`, because it holds the passwords, hashed. Each person's photo
sits beside it, in `avatars/`, a JPEG of up to 32 KB per person. If it becomes
unreadable, dwnvr still starts, only the owner can log in, and nothing is
written over it until it is fixed.

Whoever edits `users.json` by hand sees the result in the log and in the Avisos
(Warnings) of the Diagnóstico (Diagnostics) screen, on restart: the unreadable
file, the ignored person (username in the wrong format, duplicated, the same as
the owner's, or with an unknown role), the unreadable e-mail and the duplicated
e-mail. A person logs in with the username or the e-mail (see
[O login](api.md#the-login)), and a duplicated e-mail can't be used to log in: it
doesn't say who is logging in.

Also beside it is `access.json`, `0600`, with each person's last access, which
the Usuários screen shows. It is created on the first access and written in the
background, at most every 5 minutes and on shutdown: in a power outage, you
lose at most those minutes. If it is unreadable, dwnvr still starts, logs a
warning and starts the accesses over from zero.

There is also a third file that dwnvr **only reads through the API, never
opens**: `go2rtc.yaml`. It belongs to go2rtc, and configuring it is the
installer's job. See [`go2rtc.example.yaml`](../go2rtc.example.yaml).

## Per-camera policy

Everything that is recording policy is **per camera**, not global:

| Field | What it decides | Accepted |
| --- | --- | --- |
| go2rtc stream | which source to record - the high or the low resolution one | |
| `audio` | the recorded audio - see below | `none`, `flac` or `aac` |
| `quotaMB` | how much disk that camera may use | 100 or more |
| `segmentSeconds` | target duration of each segment | 10 to 300 |
| `maxDays` | maximum age, optional | 0 (no limit) or more |
| `stallSeconds` | tolerance before the stream is considered dead | 1 to 3600 |
| `detect` | mark motion on the timeline - see below | |
| `detectMethod` | what decides to trigger | `kleinberg-p` or `periodic` |
| `detectSensitivity` | the level, which is the CPU cost | 1 to 5 |
| `areas` | the areas drawn over the image - see below | up to 8 |
| `alert` | when a detection becomes an alert - see below | |

Being per camera is not pedantry: in a real installation the cameras are not
all alike. The gate camera deserves high resolution and a big quota; the
hallway one doesn't. And the right `stallSeconds` depends on each camera's
link - the type of connection: a camera on bad wi-fi needs more tolerance than
one on a cable.

The values in `defaults` in `dwnvr.yaml` apply to any camera that doesn't set
the field.

Saving a camera through the screen doesn't pin it to today's `defaults`: a
field it didn't set that arrives equal to the default stays out of
`cameras.json`, and keeps following `dwnvr.yaml`. A field it already set stays,
even if equal to the default. Two consequences:

- through the screen, you can't pin on a camera a value equal to today's
  default: that is only done by writing the field in `cameras.json`;
- a camera saved by an earlier version may have every field written, with the
  defaults of the time, and only goes back to following `dwnvr.yaml` when they
  are removed from `cameras.json`.

In both cases, stop dwnvr before editing `cameras.json` and start it again
afterwards. The file is only read at boot, and saving any camera through the
screen while dwnvr is running rewrites the whole file with what it has in
memory, over your edit.

The `areas` are not recording policy: they are the parts of the image, like
"Portão" and "Garagem" (Gate and Garage), that Detecções (Detections) filters
by. Changing an area doesn't restart the recording. The admin draws them in the
Áreas (Areas) tab of the camera registration, or in Detecções, with the "salvar
na câmera" (save to the camera) of the area editor.

```json
"areas": [
  { "id": "k3f9a2", "name": "Portão", "rule": "foot",
    "points": [[0.12, 0.30], [0.55, 0.30], [0.55, 0.80], [0.12, 0.80]] }
]
```

- `points` are 3 to 20 vertices, as a fraction of the frame (0 to 1, with Y
  growing downward), and hold at any camera resolution.
- `rule` decides when the object counts as inside: `foot`, the default, when
  the middle of the box's bottom edge is in the area; `touch`, when the box
  touches it.
- `name` can't be empty or repeated in the camera.
- `id` is short, only lowercase letters and digits, and the server generates it
  when the area arrives without one. Links point to it, and renaming the area
  doesn't break them. An area written by hand without an `id` gets a new one on
  every boot, until the camera is saved through the screen.

If the camera is moved, the area ends up in the wrong place: redraw it.

The `alert` isn't recording policy either: it says when a detection of the
camera becomes an alert. The admin turns it on in the Alerta (Alert) tab of the
camera registration, which only works with motion marking on and the object
detector configured; without them, the tab says what is missing. The alert goes
to the log, is flagged on the detection, which Detecções filters, and, with the
list in [E-mail alerts](#e-mail-alerts), goes out by e-mail (see
[`detection.md`](detection.md#the-alert)). Changing the alert doesn't restart
the recording.

```json
"alert": { "enabled": true, "families": ["person"], "areas": ["k3f9a2"] }
```

- `families` are the families that alert: `person`, `vehicle` or `animal`. With
  the alert on, at least one.
- `areas` are the `id`s of the camera's areas the object has to fall in, each
  by its own `rule`. Empty or absent means the whole image.
- `schedules` are the times the alert goes out, added together; empty or absent
  means at any time. See below.
- `enabled` off keeps the families, the areas and the schedules, to turn it
  back on without redoing them.

Each schedule has the days and a From and a To, to the minute. Wednesday with
three schedules:

```json
"schedules": [
  { "days": [3], "from": "00:00", "to": "05:15" },
  { "days": [3], "from": "12:00", "to": "13:00" },
  { "days": [3], "from": "18:30", "to": "23:59" }
]
```

- `days` goes from 0 (Sunday) to 6 (Saturday). Several schedules can have the
  same day, and overlapping ones count as their sum.
- `from` and `to` are `HH:MM`, and both ends are included: "until 13:00"
  includes the minute 13:00, and the whole day is `00:00` to `23:59`.
- No schedule crosses midnight: `to` is greater than `from`, never equal. A
  night is two schedules, one until `23:59` and another from `00:00` on the
  next day.
- What counts is the time the detection happened, in the server's time zone,
  the same one that decides the day of the recordings. The detection is still
  recorded at any time: the schedule only decides the alert.
- Up to 84 schedules per camera.

On screen, the schedules are in the Horários (Schedules) section of the Alerta
tab, in two forms that move together: the week grid, where you mark whole
hours, and the list, where you type to the minute. Marking the grid rebuilds
the list, with days that have the same schedule in a single schedule. Clearing
"Limitar o alerta a alguns horários" (Limit the alert to certain times) erases
the schedules on save.

## Value out of range

A number out of range **doesn't stop dwnvr from starting**. Recording with the
default is better than not recording at all, and a camera with a bad value
can't leave the others stopped. At boot, each such value:

- takes the default: the one from `defaults` for the camera, the one in the code
  for `dwnvr.yaml`;
- becomes a warning in the log, which also shows up on the Diagnóstico screen,
  with the field, the value written, what is accepted and what ended up
  applying:

  ```text
  value out of range in the configuration; the default applies until it is fixed
  file=/etc/dwnvr/cameras.json cam=cam_portao field=quotaMB value=5
  accepted="100 MB ou mais" using=10240
  ```

In `cameras.json`, zero and an absent field mean "use the default" and never
produce a warning. In `dwnvr.yaml` zero is invalid, because there the default is
the value itself. The exceptions are `maxDays`, where zero means "no limit",
and `storage.minFreeMB` and `server.loginAttemptsPerMinute`, where zero turns
off the global disk guard and the login attempt limit.

With the `areas` it is the same: the invalid area is dropped, with a warning,
and the others stay. In the `alert`, what is dropped with a warning is the
unknown family, the area the camera doesn't have and the invalid schedule:
without a day, with a day outside 0 to 6, with a time that isn't `HH:MM` or
with a `to` that isn't after the `from`. A day repeated in a schedule counts
once, also with a warning. An enabled alert left with no family, with none of
the areas it had or with none of the schedules it had is turned off, also with
a warning: without the areas, it would apply to the whole image, and without
the schedules, at any time.

What still stops the boot is what has no safe default: a file that can't be
read, an empty `storage.root` and a camera ID that would become a path outside
the recordings directory or that has a comma (it separates the IDs in the
API's `cams=`).

The Câmeras (Cameras) screen and the API use the same ranges, but there a bad
value is **rejected** instead of replaced, because someone is in front of the
screen to read the message and fix it right away. The ranges live in
[`internal/config/params.go`](../internal/config/params.go).

## Motion marking

With `detect` on, the timeline gets a **heat strip** under the blue bar: the
stronger the amber, the more happened at that instant.

The signal is the **size of each frame**, which the recorder already has on
hand - no pixel is decoded, and nothing is written beyond one line per mark in
`events/{day}.ndjson`. The strip says WHEN there was motion; saying what it was
is the job of the object detector, optional - see below.

**The sensitivity is the cost, and it is per camera.** With six cameras at
level 4 that is 300 marks per hour, not 50:

| level | marks/hour per camera | catches |
| --- | --- | --- |
| 1 - very low | 6 | 2.8% |
| 2 - low | 12 | 9.2% |
| 3 - medium | 25 | 22.3% |
| 4 - high | 50 | 41.5% |
| **5 - very high** | **100** | **57.4%** |

"Catches" is the share of person, vehicle or animal arrivals where the trigger
fires in time. Both columns are measured, not estimated: hundreds of hours of
real recording, against arrivals marked by hand.

**The method** decides how to trigger. `kleinberg-p` compares each frame with
the camera's own normal - which is why the same level means the same thing on a
quiet camera and on one full of infrared noise. `periodic` triggers at a fixed
interval, ignoring the image: same cost, and it catches 2 to 3 times less. It
exists as the reference to beat, and for whoever prefers unsurprising behavior
on a specific camera.

Changing any of the three **doesn't reopen the connection**: the recorder swaps
the trigger on the next frame, without opening a gap in the recording.

How the score, the onset and the levels work inside is in
[`detection.md`](detection.md).

## Object detector

The motion mark says WHEN. The object detector says WHAT: for each mark, dwnvr
cuts the video clip of that instant and asks the `dwnvr-detect` container what
was in it. If it was a person, vehicle or animal, the object mark goes to the
same `events/{day}.ndjson`, and the frame where the object appears goes to
`frames/{day}/{atMs}.jpg`, beside it. Both go away together with the video in
retention, and the frames count against the camera's quota: ~14 KB per
detection, about 37 MB in a day with nine busy cameras.

It is **optional** and lives in a separate container, because decoding video
and running a model need native code and dwnvr is pure Go. You turn it on in
`dwnvr.yaml`:

```yaml
detector:
  url: http://dwnvr-detect:8480
```

Without the `url`, cameras with `detect` mark only motion, and dwnvr doesn't
store a single extra byte of video for it.

**What it costs.** On the Orange Pi Zero 3, each inference takes ~6.5 s of one
core and the container takes ~230 MB. At level 5, the default, that is ~17% of
one core per camera, continuous; at 4, half. Inferences go through a **queue
with two spots per camera**: if a camera triggers again with both taken, the
new mark stays as motion only. That keeps the busiest camera from taking the
others' turn, and it is what holds the `person` delivery near the measured
ceiling. If the object detector goes down, the recording and the motion strip
carry on as if it didn't exist; the log warns once when it goes out, and once
when it comes back.

**A parked car doesn't become a mark on every inference.** dwnvr remembers, per
camera, where there is already a stationary object, and only marks the
ARRIVAL. That memory doesn't survive a restart: after it, each stationary
object shows up once as if it had just arrived.

The whole path, from the frame to the object mark, what happens when the object
detector goes down and the model's limits are in
[`detection.md`](detection.md).

## Retention

Three limits, in this order:

1. **quota in MB per camera** - the main one, a ring buffer deleting the oldest
2. **maximum age in days** - optional, for whoever thinks in days and not in GB
3. **minimum free disk, global** - a safety net that ignores the quotas

The third exists because the sum of the quotas is easy to get wrong: each
camera has a different rate, and filling the disk is worse than losing old
recordings.

The recordings of a removed camera, without ticking "apagar também as
gravações" (also delete the recordings), fall under none of the three, not even
the minimum free disk: dwnvr doesn't delete on its own what the user preserved,
even if the alternative is deleting the recordings of a camera that is live.
They show up in **Gravações de câmeras removidas** (Recordings of removed
cameras), on the Câmeras screen, where you can delete them, and the Diagnóstico
shows them separately on the disk bar. With the disk tight, the retention
warning in the log carries their total in `orfas_mb`.

The quota is applied every minute, so the real peak is `quota + rate × 60s` -
with a 900 kbps camera that is ~7 MB of slack, negligible against a real quota.

## Audio

The audio mode is chosen per camera and becomes a codec filter in the URL:

| Mode | CPU | Disk | Touches go2rtc? |
| --- | --- | --- | --- |
| `none` | zero | zero | no |
| `flac` | **+0.65% of 1 core** | **+260 kbps** (~2.8 GB/day) | no |
| `aac` | ~10% of 1 core | ~64 kbps (~0.7 GB/day) | yes, requires `ffmpeg:cam#audio=aac` |

Measured on the Orange Pi Zero 3 with Yoosee cameras (pcm_alaw 16 kHz mono).
**The choice is between CPU and disk**: FLAC is practically free in processing
and doesn't start any ffmpeg process - the conversion happens in pure Go inside
go2rtc -, but being lossless it sits at ~260 kbps, which on a camera with 770
kbps of video means **+34% storage**. AAC reverses the trade.

A requirement common to both: the source in `go2rtc.yaml` can't have
`#media=video`, which drops the audio at the origin. No dwnvr setting brings
back audio dropped there.

The full FLAC measurement is in [audio-flac.md](audio-flac.md).

## Login attempt limit

```yaml
server:
  loginAttemptsPerMinute: 2
  trustedProxies: [127.0.0.0/8, "::1/128", 172.16.0.0/12]
```

The values above are the defaults, which apply without writing anything.

Each address gets **5 login attempts in a row**. After them, the following ones
go through at the pace of `loginAttemptsPerMinute`: with the default, one every
30 s. Whoever stops trying recovers one slot per interval, up to 5. Past the
limit, the login answers without checking the password, and the screen counts
the seconds until the next attempt. Every attempt counts, the right one too.

Someone who only got the password wrong two or three times doesn't see the
limit. It exists for whoever tests passwords nonstop: each attempt costs 0.77 s
of CPU on an Orange Pi Zero 3 (see [O login](api.md#the-login)), and without a
limit that keeps a core busy all the time, and the login of the household waits
behind it.

`0` turns the limit off. The accepted range is 0 to 12 per minute.

The limit is **per address**, not per user: per user, anyone who can reach the
screen could lock the owner out of logging in just by getting their password
wrong on purpose. Since it is per address, someone trying from many addresses
at once isn't contained by it. Logging in by e-mail counts toward the same
limit.

### Where the address comes from

Behind a proxy, every connection reaches dwnvr with the proxy's address, the
same for everyone. Who really connected comes in the `X-Forwarded-For` header,
which the proxy fills in. But whoever talks directly to dwnvr's port can also
send that header, making up an address on each attempt. That's why
`trustedProxies` lists the networks of the trusted proxies:

- if the connection comes from one of them, the address is the one in
  `X-Forwarded-For`, read from right to left up to the first one that isn't a
  trusted proxy;
- if it doesn't, the address is the connection's, and the header is ignored.

The default covers the loopback and the range Docker uses for the compose
networks. That is how the two HTTPS paths in this documentation arrive: the
`tailscale serve` of [remote access](remote-access.md), through the compose
network's gateway, and the Caddy of [HTTPS on the home network](https-local-network.md),
through its container's IP. Both put in the header the address of whoever
connected, in place of whatever the device sent. This was checked on 2026-10-13
with `tailscale serve` 1.102 and Caddy 2.11.7. Whoever opens port 8080 directly
over the home network arrives with their own address, outside the list.

Change `trustedProxies` only if your proxy reaches dwnvr over another network,
like an nginx on another machine. Each item is a network (`10.0.0.0/8`) or a
single address (`192.168.1.20`). An invalid item makes the whole list go back
to the default, with a warning in the log. `[]` trusts no one: behind a proxy,
everyone becomes a single address, and whoever goes over the limit locks the
login for everyone.

Two cases make the default wrong:

- **The home network inside `172.16.0.0/12`.** A device on it, talking directly
  to 8080, could make up the address. Remove the range and put only the compose
  network, which `docker network inspect` shows.
- **Rootless Docker** (untested). From what its documentation says, every
  connection reaches the container with the gateway's address, which is in the
  list. As in the case above, the header becomes accepted from any device.

## E-mail

```yaml
smtp:
  host: smtp.gmail.com
  username: dwnvr.exemplo@gmail.com
  password: troque-me
```

With this section, dwnvr sends e-mail: the set-password link, which the admin
sends from the Usuários screen straight to the person, and the detection alert,
to the list in [E-mail alerts](#e-mail-alerts). Without it, dwnvr opens no
connection at all, and the screen doesn't show the "Enviar por e-mail" (Send by
e-mail) or the Diagnóstico test. A person's e-mail is registered the same way,
because it is used to log in (see [O login](api.md#the-login)), and the screen
warns that the link won't go by e-mail. Any provider that speaks SMTP works.

| Field | What it is | Without writing |
| --- | --- | --- |
| `host` | the e-mail server, just the name or the IP | required |
| `security` | `starttls`, `tls` or `none` | `starttls` |
| `port` | the port | the one for the `security`: 587, 465 or 25 |
| `username` and `password` | the account that sends; both together, or neither if the server doesn't ask for a login | no login |
| `from` | the sender, like `dwnvr <dwnvr.exemplo@gmail.com>` | the `username`, when it is an address |

- **`starttls`**: the connection starts without TLS and switches to TLS before
  the login. If the server doesn't offer STARTTLS, dwnvr sends nothing, not
  even the password.
- **`tls`**: TLS from the first byte, on port 465.
- **`none`**: no TLS, for a server on the home network. With a login, it only
  works for `localhost`, `127.0.0.1` or `::1`, because the password doesn't
  leave without TLS.

The server's certificate is always checked, with no option to turn that off. The
login is `AUTH PLAIN`, the most common. The password stays on the server: it
doesn't go to the screen or the log.

A section with a problem **doesn't stop dwnvr from starting**. There is no
default that replaces a wrong server, so e-mail stays off until `dwnvr.yaml` is
fixed. The reason becomes a warning in the Avisos card of the Diagnóstico and
in the bell in the header, and also goes to the log:

```text
e-mail off: the smtp section of dwnvr.yaml is unusable until it is fixed
file=/etc/dwnvr/dwnvr.yaml problem="smtp.security \"ssl\" fora do aceito: use starttls, tls ou none"
```

To check without registering anyone, open the **Este servidor** (This server)
card of the Diagnóstico: it shows the server, the security and the sender that
ended up applying, and **Testar o e-mail** (Test the e-mail) sends one to the
address you type, with the server's reply exactly as it came.

### With Gmail

1. **Create an account just for dwnvr**, and don't use your own. The app
   password gives access to the whole account and is written in `dwnvr.yaml`:
   on an account just for it, that is all it opens.
2. **Turn on 2-step verification** on that account. Without it, Google doesn't
   offer the app password.
3. **Generate an app password**, as explained in
   [Google's help](https://support.google.com/accounts/answer/185833). It has
   16 characters and is the `password`. The `username` is the account's
   address.

The `host` is `smtp.gmail.com`, and the default `security` and port (`starttls`,
587) already work. This was checked on 2026-10-24, with a new account and an app
password: the "Testar o e-mail" of the Diagnóstico arrived, and so did the
set-password link sent by the "Enviar por e-mail" of the Usuários screen.

Two things Google says that stop sending:

- changing the account password revokes all app passwords, and dwnvr starts
  receiving "login recusado" (login refused) until you generate another;
- a regular account sends up to 500 e-mails per day
  ([Gmail limits](https://support.google.com/mail/answer/22839)).

For other providers, the `host`, port and `security` are in each one's help.
Transactional e-mail ones, like Brevo, ask for a domain of yours for the
sender: with a `@gmail.com` address, Brevo replaces the sender with an address
of its own.

## E-mail alerts

```yaml
alerts:
  email:
    to: [fulano@exemplo.com, ciclana@exemplo.com]
```

The list of who receives the detection alert from every camera with the alert
on (see the `alert` in [Per-camera policy](#per-camera-policy)), in a single
message for everyone: five addresses are one send. The alert goes out through
the `smtp` section, the same one as the set-password link; here is only who
receives it. The message, with the frame and the button that opens the
recording, is in [The alert](detection.md#the-alert); the button needs the
[dwnvr address](#the-dwnvr-address).

| Field | What it is | Without writing |
| --- | --- | --- |
| `alerts.email.to` | who receives; just the address, no name in front. A single address also works without the brackets: `to: fulano@exemplo.com` | the alert stays only in the log and flagged on the detection |

Nothing here stops dwnvr from starting. They become a warning in the Avisos
card of the Diagnóstico and in the bell in the header, and also go to the log:

- **the section or the list in the wrong format**, like
  `email: fulano@exemplo.com` without the `to:`. What can't be read is left
  out, and with no list no alert e-mail goes out;
- **what isn't an address**, like `Fulano <fulano@exemplo.com>`, which is
  dropped from the list while the others carry on. A repeated address is
  dropped silently;
- **the list without the `smtp` section**, which has nowhere to go out through.
  With the section rejected, the warning is its own, which says the reason.

```text
e-mail alerts: alerts.email.to "Fulano <fulano@exemplo.com>" não é um endereço de e-mail e ficou de fora da lista file=/etc/dwnvr/dwnvr.yaml
```

`dwnvr.yaml` is only read when dwnvr starts: the new list applies after
restarting it.

To check, the **Para onde vai** (Where it goes) block, at the end of the
camera's Alerta tab, says whether the alert goes out by e-mail and to whom, or,
if it doesn't, what is missing in `dwnvr.yaml`. There is no alert test: the
**Testar o e-mail** of the Este servidor card checks the `smtp` section. After
some camera turns the alert on:

- **a send that fails** becomes a red warning, with since when and the server's
  error, in the Avisos card and in the bell, and also shows up in "Para onde
  vai". It goes away on the first send that works;
- **an alert that didn't go out**, over the send ceiling or with the send queue
  full, becomes a yellow warning, which says how many and why, and goes away an
  hour after the last one. It stays recorded in Detecções. The ceiling protects
  the e-mail account from the provider's daily limit (see the limits in
  [The alert](detection.md#the-alert));
- **the send numbers** (sent, failures and the last result) are in the group
  **Alertas por e-mail** (E-mail alerts) of the Este servidor card.

## The dwnvr address

```yaml
server:
  publicUrl: https://dwnvr.exemplo.ts.net
```

The address dwnvr is opened at, as it appears in the browser's address bar. The
server has no way to know it, because each person arrives by a different path.
Today only the [e-mail alert](#e-mail-alerts) uses it: with it, the e-mail
carries the **Ver na gravação** (View in the recording) button, which opens the
Gravações screen on the camera and at the instant of the alert.

| Field | What it is | Without writing |
| --- | --- | --- |
| `server.publicUrl` | optional; the dwnvr address, like `https://dwnvr.exemplo.ts.net` | nothing stops working: the alert e-mail goes out without the button and asks, in its place, that the administrator set it |

Pick the address that opens from wherever the person is when reading the
e-mail: with [remote access](remote-access.md), the `https://….ts.net`. A path
at the end works for dwnvr behind a proxy on a subpath
(`https://exemplo.com/dwnvr`), and a trailing `/` is ignored. Anything that
isn't an `http://` or `https://` address, or has `?`, `#` or a username and
password, becomes a warning in the log and in the Diagnóstico, and counts as
not written: dwnvr still starts.

```text
level=WARN msg="value out of range in the configuration; the default applies until it is fixed" file=/etc/dwnvr/dwnvr.yaml field=server.publicUrl value=dwnvr.exemplo.ts.net accepted="o endereço do dwnvr, como https://dwnvr.exemplo.ts.net" using="nenhum: o e-mail de alerta sai sem o botão"
```

`dwnvr.yaml` is only read when dwnvr starts: the new address applies after
restarting it.
