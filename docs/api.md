# HTTP API

Endpoint reference. The implementation is in
[`internal/api/`](../internal/api/).

Conventions that apply to everything:

- **Instants are epoch in milliseconds.** There is never a date-time string,
  except in the `day=YYYY-MM-DD` shortcut, which is interpreted in the server's
  local time zone.
- **`cam`** is always the id of a registered camera. An unknown camera is an
  error, not an empty response. The id has no path (`/`, `\`, `:`) and no
  comma, which separates the ids in lists (`cams=a,b`): `POST /api/cameras`
  rejects it with `400`.
- Errors come as HTTP 4xx with the message as text.

## Session

| Endpoint | What it does |
| --- | --- |
| `POST /api/login` | opens a session; returns a signed cookie (HMAC, no state on the server) and who signed in |
| `POST /api/logout` | ends the session |
| `GET /api/session` | **public**: says whether this dwnvr requires login and, with a session, who is logged in |
| `GET /api/session/ws` | the session notice: a mute websocket that the server closes when the person is dropped |
| `GET /api/version` | **public**: version, commit and build date |
| `POST /api/invite/check` | **public**: says whose invite link it is and until when it is valid |
| `POST /api/invite` | **public**: sets the password through the invite link and opens the session right away |

`/api/session` and `/api/version` are outside authentication for the same
reason: they need to be visible before signing in. So are the two invite
routes, because whoever opens the link has no password yet (see
[The invite link](#the-invite-link)). `/api/version` is also the deploy probe -
a `curl` tells whether dwnvr came up with the new code, with no cookie.

Everything else requires a valid session, and the routes marked **admin only**
also require the admin role.

### Who signs in

- **The owner**: the `server.username` and `server.password` from
  `dwnvr.yaml`. Always admin. Their password can only be changed in the file.
- **The people in `users.json`**, in the config folder, each with a stored
  role: `admin` or `viewer`.

The viewer sees the cameras, the recordings and the detections. What belongs to
the **Câmeras** (Cameras) screen and to the server diagnostics comes back as
`403`, even without the tab on screen: hiding the tab is not security.

Without credentials in `dwnvr.yaml`, authentication is off, the people in
`users.json` are ignored, and whoever reaches the screen is treated as admin.

```json
{
  "authRequired": true,
  "authenticated": true,
  "user": { "username": "maria", "name": "Maria", "role": "viewer", "owner": false, "avatar": "4d1e0b62a7c3f915" }
}
```

The `user` only comes with `authenticated: true`; with authentication off, it
comes with `"role": "admin"` and `"owner": true`, with no `username` or `name`.
The `avatar` is the id of the photo (see [My account](#my-account)) and only
comes with a photo. `POST /api/login` returns the same `user`, together with
`"ok": true`.

### The login

The `username` of `POST /api/login` accepts the person's username or e-mail,
case-insensitively for the e-mail. A username in the registry has no `@`, so
with `@` it is an e-mail. Signing in by e-mail is signing in as the person: the
`user` in the response, the session, the last access and the log all carry the
username. The owner has no e-mail and signs in only by username, which in
`dwnvr.yaml` may contain `@`: theirs counts before any e-mail is looked up. An
e-mail that matches nobody, including one repeated in a hand-edited
`users.json`, is answered like a wrong password: `401` with
`{"error": "usuário ou senha inválidos"}`.

Every attempt costs one password computation of 0.77 s on an Orange Pi Zero 3
(PBKDF2-SHA256, 600,000 iterations), one at a time in the whole process: a few
simultaneous attempts don't occupy the recording cores, they just wait their
turn. The owner's login, whose password is in plain text in `dwnvr.yaml`, and
the login of a user that doesn't exist pay the same computation, so the
response time doesn't reveal who exists.

Each address has an attempt limit, on by default and turned off with
`server.loginAttemptsPerMinute: 0` (see
[Login attempt limit](configuration.md#login-attempt-limit)).
Past it, the response is `429`, before any password computation, with the
`Retry-After` in seconds and the same number in the error, as in
`{"error": "muitas tentativas; tente de novo em 30 s"}`. The number is what
remains until the next attempt gets through: it depends on
`loginAttemptsPerMinute` and shrinks over time. An attempt refused this way
doesn't count: insisting during the wait doesn't stretch it.

### The cookie

The cookie says whose session it is: `<username in base64>.<expiry>.<hmac>`.
The session lasts 30 days and renews with use: when less than half of the term
remains, any authenticated response, including the one from `/api/session`,
brings a new cookie with the full 30 days. In practice, the 30 days count from
the last time the screen talked to dwnvr. It is still stateless on the server:
renewing is just signing another term.

The key that signs each person's cookie comes from `.session-secret` together
with their username and password: the one from `dwnvr.yaml`, for the owner, or
the one stored in `users.json`. Changing someone's password drops only that
person's sessions, and the same holds for someone who is removed or gets a new
link, which erases the password. For the owner, this requires a restart,
because `dwnvr.yaml` is only read at startup. Someone who changes their own
password through **Minha conta** (My account) gets a new cookie in the same
response and carries on; their other devices go back to the login. To drop
everyone at once, see
[Trocar a senha e derrubar as
sessões](operations.md#changing-the-password-and-ending-the-sessions).

### The invite link

Whoever signs in for the first time does so through a link: the admin creates
the person in the **Usuários** (Users) screen, and they set their own password.
With e-mail on, the link can go straight to them (see [Users](#users)). The
link is `<address>/#invite?token=<token>`, with 32 random bytes in the token.
The address is the one the admin is accessing through. The token stays in the
URL fragment, which doesn't leave the browser, and goes to the server in the
body of a POST: it doesn't show up in the dwnvr log or in any proxy's. The
`users.json` keeps only its SHA-256. When sent by e-mail, the link is stored in
the sent folder of the sending account and in the inbox of the person: since it
is valid for minutes and only once, what is left there is a dead link.

The link is valid for 10 minutes (`users.InviteValidity`) and only once. Each
person has one link at a time: generating another invalidates the previous one.
Expired, used, replaced or made up, the response is the same, `410`.

```json
// POST /api/invite/check   {"token": "..."}
{ "username": "maria", "name": "Maria", "inviteExpiresAtMs": 1791587903000, "minPasswordLength": 8, "maxPasswordLength": 128 }
```

`POST /api/invite` receives `{"token", "password"}` and returns the same
`{"ok": true, "user": {...}}` as the login, with the cookie. The password is 8
to 128 characters, with no composition rule (NIST SP 800-63B); outside that,
`400` with the reason, and the link stays valid. The password computation waits
in the same queue as the login.

### When someone is dropped

Removing the person, generating a new link for them, or them changing their own
password changes the credential that signs their cookies, and every following
request is refused. The connections that are already open, and don't present
the cookie again, the server closes right away: the live websocket and the
session notice (`GET /api/session/ws`).

Closing the live websocket cuts the MSE video, which comes through it. The
WebRTC one it doesn't: the player closes the negotiation websocket as soon as
the video plays, and the media goes straight from go2rtc to the browser over
UDP, without passing through dwnvr. go2rtc 1.9 has no route to drop a consumer;
only restarting it, which would stop all cameras and the recording. What closes
the WebRTC is the **Ao vivo** (Live) screen:

- on seeing the session notice close, it checks the session right away, and, if
  it isn't valid, closes the video and goes to the login;
- with the video in view, it checks the session every 60 s, the safety net if
  the notice gets lost;
- if dwnvr doesn't answer the check, the video stops until the session is
  confirmed. This also holds when dwnvr restarts: the live view stops for a few
  seconds, even with go2rtc up.

Measured with go2rtc 1.9.14 and Chrome: the WebRTC consumer leaves go2rtc about
40 ms after the admin removes the person. A screen modified by whoever was
removed, ignoring the notice, keeps receiving the already open video until the
connection drops, but doesn't open another.

## Cameras and diagnostics

| Endpoint | Parameters | What it does |
| --- | --- | --- |
| `GET /api/cameras` | - | registered + go2rtc streams + orphan recordings + where the alert goes; for the viewer, only the cameras (from the `alert`, only the `enabled` when on) and the `detector` |
| `POST /api/cameras` | JSON body | **admin only**: registers or changes (upsert by id); a field the camera didn't define that arrives equal to the default is not stored (see [Per-camera policy](configuration.md#per-camera-policy)) |
| `DELETE /api/cameras` | `id`, `recordings=1` | **admin only**: unregisters; with `recordings=1` deletes the recordings too |
| `GET /api/streams/probe` | `src` | **admin only**: says whether a stream delivers audio, opening it if needed |
| `POST /api/go2rtc/restart` | - | **admin only**: restarts go2rtc, so it rereads `go2rtc.yaml`; all cameras stop for a few seconds |
| `GET /api/health` | - | **admin only**: measured bitrate, estimated days, audio and transcoding per camera, disk state, uptimes, clock, the problem of the `smtp` section and, with alerts on, the total of alerts |
| `POST /api/reconnects/reset` | - | **admin only**: zeroes the reconnect count of all cameras; no connection drops |
| `GET /api/health/server` | - | **admin only**: the machine that records and the dwnvr process: temperature, memory, pressure, storage, go2rtc, object detector, e-mail server, alert deliveries and latest log warnings |
| `POST /api/email/test` | `{"to"}` | **admin only**: sends a test e-mail to the address, through the `smtp` section |
| `DELETE /api/rec` | `cam` | **admin only**: deletes the recordings; also works for an already removed camera |

In `POST /api/cameras`, each area without an `id` gets one generated by the
server, and the response brings the camera with them. An invalid area rejects
the whole save with `400` and the reason, like the other fields:

```text
areas[1] "Garagem" fora do aceito: use de 3 a 20 pontos, com as coordenadas de 0 a 1
```

The `alert` points to the areas by `id`, and one the camera doesn't have also
rejects the save. That is why the screen takes the area out of the alert before
deleting it:

```text
alert.areas[0] "sumiu" fora do aceito: use o id de uma área desta câmera
```

An alert schedule that crosses midnight is rejected the same way:

```text
alert.schedules[0].to "06:00" fora do aceito: use uma hora depois do from 22:00; a noite são dois horários, até 23:59 e a partir de 00:00
```

The format of `areas` and `alert` is in
[Per-camera policy](configuration.md#per-camera-policy).

The `hasAudio` of `GET /api/cameras` is only reliable on a stream someone is
already consuming: go2rtc only fills the producer's `medias` while a consumer
exists, and on an idle stream it doesn't even open the connection to the
camera. That is why `GET /api/streams/probe` is a separate endpoint, and no
longer a field of the listing - its response can cost a few seconds and a new
connection.

```json
{
  "name": "cam_teste5",
  "hasAudio": true,
  "audioCodecs": ["PCMA/16000"],
  "probed": true
}
```

`probed` says whether the response cost a connection: `false` when it came from
the `medias` of a producer that was already alive, or from the cache of the
previous probe. If the probe fails - a camera that is down, for instance -, the
response is still `200`, with the reason in `error` and `hasAudio: false`; the
caller should treat this as "I don't know", and not as "it has no audio".

`GET /api/cameras` also brings the `defaults`: an empty camera with the
`defaults` of `dwnvr.yaml` applied. The new-camera form starts from it, and not
from numbers repeated in the interface. It brings the `ranges` of the camera's
numeric fields, the same validation with which `POST /api/cameras` rejects,
where the `min` and `max` of the inputs come from; without a ceiling, the `max`
doesn't come:

```json
"ranges": {
  "quotaMB": { "min": 100 },
  "segmentSeconds": { "min": 10, "max": 300 },
  "maxDays": { "min": 0 },
  "stallSeconds": { "min": 1, "max": 3600 },
  "detectSensitivity": { "min": 1, "max": 5 }
}
```

And it brings the `detector`: `true` when the object detector is configured.
Without it, the interface hides the **Detecções** (Detections) tab.

It also brings the `alertEmail`, where the alert goes, which the camera's
**Alerta** (Alert) tab shows. With the e-mail channel (the `smtp` section on
and the `alerts.email.to` list), the list comes; with the channel failing, also
since when and the error of the most recent failure, which go away on the first
send that succeeds:

```json
"alertEmail": {
  "to": ["fulano@exemplo.com", "ciclana@exemplo.com"],
  "failingSince": "2026-10-27T13:58:12-03:00",
  "error": "dial tcp 142.250.0.108:587: connect: connection timed out"
}
```

Without the channel, `missing` says which `dwnvr.yaml` sections are missing,
from `alerts.email.to` and `smtp`, and `smtpError`, the reason the `smtp`
section was rejected at boot:

```json
"alertEmail": { "missing": ["smtp"], "smtpError": "smtp.from vazio" }
```

go2rtc only reads `go2rtc.yaml` when it starts. When the file says one thing
and the running go2rtc does another, `GET /api/cameras` brings
`go2rtcConfigFile`, and the screen offers `POST /api/go2rtc/restart`:

```json
"go2rtcConfigFile": {
  "added": ["cam_quintal"],
  "changed": ["cam_portao"],
  "notInFile": ["cam_garagem"]
}
```

`added` are in the file and were not loaded; `notInFile` are running and left
the file; `changed` exist on both sides with a different source. Empty lists
are omitted, and the whole field goes away when both sides match or when go2rtc
doesn't let the file be read (its `GET /api/config`). The comparison covers the
`streams` section only. `changed` is not complete: while an `ffmpeg:` or
`exec:` source is in use, go2rtc doesn't show the original source, and that
camera is left out. RTSP in use does show, and is included. The file, which has
the URLs with username and password, is read on the server and doesn't go to
the browser - only the names.

Each camera in `/api/health` says whether motion marking is on (`detect`) and
how many onsets it has marked since dwnvr came up (`onsets`).

Each camera with a recorder also brings the configured `audio`, next to the
`hasAudio` that the connection found, and the `transcoding`: whether go2rtc
passes its source through an ffmpeg. The `transcoding` is queried from go2rtc
once per connection, and not on every health read: the source only changes when
go2rtc restarts, which reconnects all cameras. A disabled camera comes without
`audio`.

The reconnects of each camera in `/api/health` count from `reconnectsSince`:
the recorder start, which is dwnvr's or the last time the camera was restarted
by a configuration change, or the last `POST /api/reconnects/reset`.
`reconnectsInWindow` are those that fell within the last
`reconnectsWindowHours` hours, which comes at the root of the response and
lives in `recorder.ReconnectWindow` (`internal/recorder/params.go`).
`lastReconnectAt` is the most recent, and `disconnectedAt`, which only comes
with `connected: false`, is when the connection dropped:

```json
"reconnects": 24,
"reconnectsSince": "2026-09-28T08:37:02-03:00",
"reconnectsInWindow": 24,
"lastReconnectAt": "2026-09-28T13:58:12-03:00"
```

`disk.dwnvrBytes` sums only the registered cameras. What is left on disk from
removed cameras comes separately, in `disk.orphanBytes`, because no retention
limit reaches it: added to the first, it would vanish from the warning; outside
both, the screen would credit it to other programs.

The screen warnings with "since when" also come from `disk.belowMinSince`,
which the retention marks on the first pass with the disk below the minimum,
and from `go2rtc`, which only appears while go2rtc doesn't answer dwnvr. It is
marked on the first transport failure, and any HTTP response, even an error,
erases it:

```json
"go2rtc": {
  "error": "dial tcp 127.0.0.1:1984: connect: connection refused",
  "since": "2026-09-28T13:58:12-03:00"
}
```

With the object detector configured, each camera in `/api/health` also brings
the **funnel**: the destination of each onset, since dwnvr came up. The slices
add up to the camera's `onsets` -
`onsets = noVideo + dropped + failed + rejected + noObject + withObject + queued`:

```json
"funnel": {
  "clips": 412,
  "noVideo": 3,
  "dropped": 31,
  "failed": 0,
  "rejected": 2,
  "noObject": 350,
  "withObject": 28,
  "queued": 1
}
```

`clips` is not a slice: it is how many onsets had the video cut and offered to
the queue. `noVideo` are onsets that ended without a clip - right after
connecting, before the first I-frame. `queued` is what has no outcome yet:
waiting for the window's peak to be cut, waiting for its turn, or being
inferred.

`failed` is an object detector that is down or hung; `rejected` is an object
detector that is up and couldn't run the inference, almost always a video the
camera sent corrupted.

`dropped` are those the queue refused because the camera already had
`perCamera` clips waiting for their turn (see `detector` below): they don't go
to the object detector, not even later, and stay only as motion. `noObject`
includes the car that was already parked - inferred, but it didn't ARRIVE now.

The queue is a single one for all cameras, and comes outside the list of them,
in `detector`:

```json
"detector": {
  "queue": {
    "length": 1,
    "cap": 8,
    "peak": 5,
    "perCamera": 2,
    "cameras": {
      "cam_teste1": {
        "waiting": 1,
        "busy": true
      }
    }
  },
  "times": {
    "inferenceMs": 3620,
    "waitMs": 410
  }
}
```

`length` are the clips waiting for their turn, not counting the one being
inferred, and `peak` is the largest `length` since dwnvr came up. The queue has
no fixed total: each camera can have `perCamera` clips waiting for their turn,
and `cap` is that times the cameras with detection on right now - it grows and
shrinks with them. The clip the object detector is processing has already left
the queue: `waiting` never goes past `perCamera`, and `busy` says, separately,
which camera the image being processed is from. `cameras` brings only the
cameras with something in one or the other. The times are the average of the
last 20 inferences: `inferenceMs` is how long the object detector takes to
respond - only counting the ones it answered -, and `waitMs` how long the clip
waited for its turn before. Zero before the first inference. Without an object
detector configured, the field doesn't come.

While the object detector doesn't respond, the `detector` also brings `outage`,
with the first consecutive inference that failed and the reason of the most
recent one. The first inference it answers erases it. A rejected clip (corrupt
video) doesn't count: it is the object detector being up and saying no.

```json
"outage": {
  "since": "2026-09-29T14:02:10-03:00",
  "error": "dial tcp 172.18.0.3:8480: connect: connection refused"
}
```

After any camera turns the alert on, `/api/health` brings the total of alerts
since dwnvr came up. The field stays until dwnvr restarts, even if the alert is
turned off, and doesn't come before:

```json
"alerts": { "total": 12 }
```

The alerts themselves are stored in the detection, and are read in
`GET /api/detections` (`alert` and `alertAreas` in each object, and the
`alert=1` filter).

With any delivery channel configured, an alert that doesn't go out is dropped
and counted: `queueFull` didn't fit in the send queue, and `overLimit` went
past the `limitPerHour` ceiling (see [The alert](detection.md#the-alert)). The
`dropped` comes while the episode is valid, up to one hour after the last drop
(`lastAt`); the next drop starts another, with the count from zero. It is where
the yellow **Aviso** (Warning) of the **Avisos** (Warnings) card comes from.

```json
"alerts": {
  "total": 52,
  "dropped": {
    "queueFull": 0, "overLimit": 12, "limitPerHour": 15,
    "since": "2026-10-27T14:02:10-03:00", "lastAt": "2026-10-27T14:07:31-03:00"
  }
}
```

With a channel failing, the `failing` brings one item per channel, with the
`name`, the `since` (the first failure since the last send that succeeded) and
the `error` of the most recent failure. It goes away on the first send that
succeeds. It is where the red Aviso of the Avisos card comes from.

```json
"alerts": {
  "total": 52,
  "failing": [
    { "name": "email", "since": "2026-10-27T13:58:04-03:00", "error": "dial tcp 142.250.0.108:587: connect: connection timed out" }
  ]
}
```

A `dwnvr.yaml` `alerts` section with a problem (see
[E-mail alerts](configuration.md#e-mail-alerts)) puts the `warnings` in
the same `alerts`, for the Avisos card and the bell, even without a camera with
alerts, and then without the `total`. Without a problem, they don't come.

```json
"alerts": {
  "warnings": ["falta a seção smtp, e sem ela a lista alerts.email.to não recebe nada"]
}
```

`DELETE /api/rec` accepting an already removed camera is deliberate:
unregistering without deleting leaves orphan recordings, and without this
endpoint there would be no way to recover the space through the interface.

A `dwnvr.yaml` `smtp` section with a problem leaves e-mail off (see
[E-mail](configuration.md#e-mail)), and `/api/health` brings the reason, for
the Avisos card and the bell. Without a problem, the field doesn't come.

```json
"smtp": { "error": "smtp.security \"ssl\" fora do aceito: use starttls, tls ou none" }
```

A `users.json` with a problem also becomes a warning, only with authentication
on. The `error` is the file unreadable at boot: only the owner signs in until
it is fixed. The `warnings` are the entries that aren't valid: the ignored
person, the unreadable e-mail and the repeated e-mail, which can't be used to
sign in. Fixing it through the Users screen removes the warning right away;
fixing it in the file requires restarting dwnvr. Without a problem, the field
doesn't come.

```json
"users": {
  "warnings": ["usuário \"ana\": o e-mail \"casa@exemplo.com\" é o mesmo de \"maria\", e nenhum dos dois entra pelo e-mail"]
}
```

`POST /api/email/test` returns `{"sentTo": "..."}`. An empty address, one with a
name, or more than one is `400`; with e-mail off, `409` with the reason. If the
e-mail server refuses, it is `502`, with its response as it came, such as
`o e-mail não foi: o servidor recusou o login: 535 5.7.8 Username and Password
not accepted`. Delivered to the server is not delivered to the inbox: spam and
a full inbox dwnvr doesn't see.

The `uptime` of `/api/health` is in **seconds** (`appSeconds`,
`machineSeconds`) and the `clock` is an **instant**. The difference is on
purpose: a duration can't be misinterpreted by a browser in a different time
zone or with a different clock, whereas the `clock` exists precisely to show
the time over there - it is what reveals a wrong TZ or NTP on the machine that
records.

```json
"clock": {
  "now": "2026-08-18T18:37:12-03:00",
  "abbr": "-03",
  "offsetSeconds": -10800,
  "zone": "America/Fortaleza"
}
```

`now` already carries the server's offset embedded, and the interface displays
it without reconverting. `zone` is the IANA name and goes away when the server
can't find it out (neither `TZ`, nor `/etc/timezone`, nor the `/etc/localtime`
link); then only the `abbr` remains, which alone doesn't identify a region -
`-03` holds for São Paulo, Buenos Aires and others.

`GET /api/health/server` is the **Este servidor** (This server) card of the
**Diagnóstico** (Diagnostics) screen. It lives outside `/api/health` because it
costs more: on every call it writes, syncs with `fsync` and deletes 4 KB in
`storage.root`, asks go2rtc for its version and, with `detector.url`
configured, calls the object detector's `GET /health`. The screen only calls it
with the card open, every 15 seconds. Everything comes from `/proc` and `/sys`,
and each field goes away when the source doesn't exist (kernel without PSI,
system without cgroup, outside Linux).

```json
{
  "collectedAt": {
    "at": "2026-10-02T17:45:12-03:00", "abbreviation": "-03",
    "offsetSeconds": -10800, "zone": "America/Sao_Paulo"
  },
  "machine": {
    "board": "OrangePi Zero3", "kernel": "6.12.58-current-sunxi64",
    "cores": 4, "allowedCores": 1.5, "load": [3.1, 2.05, 1.5],
    "sensors": [{ "name": "cpu_thermal", "celsius": 84.3 }],
    "mhz": 1008, "mhzMax": 1512, "governor": "ondemand"
  },
  "process": {
    "cpuMs": 811550, "aliveMs": 18585000, "memoryBytes": 33980416,
    "goroutines": 112, "openFiles": 34, "fileLimit": 524287,
    "uid": 1000, "gid": 1000, "go": "go1.27.1", "architecture": "linux/arm64"
  },
  "memory": {
    "totalBytes": 1036640256, "availableBytes": 209715200,
    "swapTotalBytes": 536870912, "swapUsedBytes": 268435456,
    "limitBytes": 536870912, "oomKills": 2
  },
  "pressure": {
    "cpu":     { "some": { "avg10": 12.5, "avg60": 8, "avg300": 3.25 }, "full": { "avg10": 0, "avg60": 0, "avg300": 0 } },
    "memory": { "some": { "avg10": 0, "avg60": 0, "avg300": 0 } },
    "io":      { "some": { "avg10": 40, "avg60": 35.1, "avg300": 20 } }
  },
  "storage": {
    "path": "/storage", "type": "ext4", "origin": "/dev/sda1",
    "options": "rw,relatime", "readOnly": false,
    "write": { "ok": true, "ms": 12 }
  },
  "go2rtc": { "ok": true, "ms": 4, "version": "1.9.9", "url": "http://go2rtc:1984" },
  "detector": {
    "ok": true, "ms": 128, "model": "model.onnx", "inputSize": "512x288",
    "threads": 2, "url": "http://dwnvr-detect:8480"
  },
  "smtp": {
    "host": "smtp.gmail.com", "port": 587, "security": "starttls",
    "from": "dwnvr <dwnvr.exemplo@gmail.com>"
  },
  "alerts": [
    { "name": "email", "sent": 47, "failures": 9, "last": "2026-10-02T17:44:37-03:00", "error": "dial tcp 142.250.0.108:587: connect: connection timed out" }
  ],
  "log": {
    "total": 37,
    "lines": [{ "at": "2026-09-22T18:37:12-03:00", "level": "WARN", "text": "connection dropped cam=garagem err=EOF" }]
  }
}
```

- `collectedAt` is the instant of the read on the server's clock, with its time
  zone, like the `clock` of `/api/health`: the screen shows it at the time over
  there, without converting. `zone` goes away when the server doesn't know the
  IANA name.
- `board` comes from the device tree and only exists on ARM; `processor` comes
  from `/proc/cpuinfo` and only has a name on x86. `kernel` is the host's, even
  inside the container.
- `allowedCores` is the container's CPU ceiling, in cores (`--cpus 1.5`), and
  goes away when there is no ceiling.
- `process` is dwnvr itself. `cpuMs` is the CPU time spent since it came up,
  and `aliveMs`, how long it has been up: the percentage comes from the
  difference between two reads, and the one who keeps the previous read is the
  screen, not the server. `memoryBytes` is the process's physical memory
  (`VmRSS`), which is what counts against the `limitBytes`. The cgroup's
  `memory.current` doesn't serve for that: it sums the cache of the recorded
  videos, which the kernel gives back when it needs to.
- `detector` only comes with `detector.url` configured. It responds even with
  no motion on any camera, unlike the `outage` of `/api/health`, which only
  appears after an inference fails.
- The `url`s come without username and password.
- `smtp` only comes with the `smtp` section in `dwnvr.yaml`, without the login
  and without the password. There is no probe: checking the e-mail server means
  sending one, and `POST /api/email/test` exists for that. With the section
  rejected, it brings the `error`.
- `alerts` are the sends of each alert channel since dwnvr came up, and only
  come with a channel configured and some camera with the alert on. `last` is
  the most recent attempt and goes away before the first; `error` only comes if
  it failed.
- `pressure` is the kernel's PSI: the percentage of time some task (`some`) or
  all of them (`full`) were stalled waiting for CPU, memory or disk, averaged
  over 10, 60 and 300 seconds.
- `oomKills` counts the processes the kernel killed for lack of memory since the
  **machine** powered on, and not only the container's. When the victim is dwnvr
  itself, Docker brings up a new container and the old cgroup's counter goes
  away with it; the machine's is still there.
- `limitBytes` is the container's memory ceiling, and goes away when there is no
  ceiling.
- `readOnly` holds for the mount or for the superblock: a faulty SD card is
  remounted read-only by the kernel without telling anyone.
- `write.error` only brings a known cause (read-only, no permission, disk full,
  nonexistent directory, a disk that didn't respond in 5 s). Any other failure
  goes generic, with the detail in the log. While a previous test remains stuck
  on a hung disk, the response says so and doesn't start another test.
- `log` brings the last 50 warnings and errors, newest to oldest, and `total`
  says how many there have been since dwnvr came up.

## Recordings

| Endpoint | Parameters | Returns |
| --- | --- | --- |
| `GET /api/rec/days` | `cam` | days that have recordings |
| `GET /api/rec/timeline` | `cam` + range | contiguous bands (to draw) + segments (to play) |
| `GET /api/rec/events` | `cam` + range | the instants when there was motion |
| `GET /api/rec/init` | `cam`, `g` | the init segment of that generation, `immutable` |
| `GET /api/rec/seg` | `cam`, `t` | the segment's fragments, **without** the init, `immutable` |
| `GET /api/rec/thumb` | `cam`, `t` | 1-frame MP4 - the server decodes nothing |
| `GET /api/rec/playlist.m3u8` | `cam` + range | HLS VOD, for VLC/ffplay/Safari |
| `GET /api/rec/export` | `cam` + range | a single stitched MP4, without transcoding |

**Range** is `from`+`to` in epoch ms, or the `day=YYYY-MM-DD` shortcut, which
is the whole day in local time. `to` must be greater than `from`.

**`/api/rec/export`** accepts at most a 10 min range, the same ceiling as the
**Gravações** (Recordings) screen; above that it responds 400. That is why the
`day=` shortcut doesn't work for it.

**`t`** is the segment's start instant, exactly as it came from the timeline.
It is not converted into a file path on its own: it is looked up in the index,
which is what knows where `ftyp`+`moov` end (`io`) and the size of the first
fragment (`f0`). A segment dwnvr didn't record is not served.

**`g`** is the init's generation - the truncated hash. Only hexadecimal of up
to 32 characters is accepted, because this value becomes a file name and
accepting a path there would open a directory traversal.

The split between `/api/rec/init` and `/api/rec/seg` is what lets the MSE
player download the init just once and then ask only for media. See
[architecture.md](architecture.md#storage).

**`/api/rec/events`** returns the raw instants, in order, that the timeline's
heat strip draws, and the object marks the object detector confirmed:

```json
{
  "cam": "cam_teste1",
  "from": 1786176000000,
  "to": 1786262400000,
  "onsets": [1786220571043, 1786220604112],
  "objects": [
    {
      "atMs": 1786220571043,
      "family": "person",
      "class": "person",
      "score": 0.87,
      "frameMs": 1786220572851,
      "box": [0.5156, 0.2611, 0.6734, 0.9778]
    }
  ]
}
```

The `onsets` are bare numbers, and not objects, for the same reason the
timeline is compact: a day at the most sensitive level has ~2,400 marks, and
the verbose form would double the response to say the same thing. The
`objects` are dozens per day, and go with full-word keys; each one's instant is
that of the onset that originated it. It is one per object: two people in the
same frame are two `person` items with the same `atMs`, and the timeline merges
the two into one arrival.

`frameMs` and `box` say where to draw the object over the video: `box` is
X1, Y1, X2, Y2 as a fraction of the image, from 0 to 1, and holds for the frame
at `frameMs` - what the object detector looked at, up to 3 s after the onset -,
and not for the instant of the mark. A mark without both keys gets no box on
screen.

A camera with detection off, or without an object detector configured,
responds with an empty list - it is not an error, it is the common case.

The free range is what gives the **tail path**: on the current day the screen
asks again every ten seconds and asks only for the recent piece, instead of
re-downloading the whole day on every round. The object mark arrives **late** -
it carries the onset's instant, but is only recorded after the object detector
runs the inference -, so the tail goes back 30 min before the last onset and
discards what it already had. Asking from the last onset on would lose forever
the object of an earlier onset.

## Detections

The object detections from all cameras together: it is what the Detections
screen scrolls.

| Endpoint | Parameters | Returns |
| --- | --- | --- |
| `GET /api/detections` | `before` or `after`, `limit`, `cams`, `families`, `area`, `rule`, `areas`, `alert` | a page of detections, newest to oldest |
| `GET /api/detections/frame` | `cam`, `t` | the JPEG of the frame the object detector looked at, `immutable` |
| `GET /api/detections/days` | `cams`, `families`, `area`, `rule`, `areas`, `alert` | the days with at least one detection, in ascending order, and how many each one has |

**`/api/detections`** paginates by cursor, and not by page number: the current
day gets a new detection all the time, and "page 3" would change content
between one request and another.

- `before=<ms>`: the detections before this instant, to scroll down. With no
  cursor at all, the newest come.
- `after=<ms>`: the ones after, to scroll up after a "go to". They are the
  closest to the cursor, but the response comes in the same order as the
  others: newest to oldest.
- `limit`: how many, from 1 to 200. Default 60.
- `cams=cam_a,cam_b` and `families=person,vehicle`: only those. Without them,
  all. An unregistered camera or unknown family is an error.
- `area=<cam>:<x>,<y>,<x>,<y>,...`: on camera `<cam>`, only the objects inside
  an area drawn over its image, such as
  `area=cam_teste1:400,500,600,500,600,700`. X and Y go in thousandths of the
  frame, integers from 0 to 1000, to keep the URL short; it is 3 to 20 points,
  and the area can be concave. The area restricts only its own camera: the
  other cameras in `cams` come whole, and the area's camera outside `cams`
  doesn't appear.
- `rule=touch` or `rule=foot`: when an object counts as inside the area. With
  `touch`, it is enough for the box to have any point in common with it. With
  `foot`, the default, the middle of the bottom edge of the box counts, where
  the feet and the wheels are: the area is drawn on the ground, and whoever
  passes in front of it, close to the camera, doesn't count. `rule` without
  `area` is an error.
- `areas=<cam>/<id>`: the same, with an area registered on the camera (the
  `areas` field of `POST /api/cameras`) and the rule from the registration, such
  as `areas=cam_teste1/k3f9a2`. Several go separated by comma, like the cameras
  in `cams` (`areas=cam_a/k3f9a2,cam_b/p0rt40`). The server resolves it by the
  registration as of now: editing the area changes the filter from then on,
  including over the past. An area the camera no longer has is an error.

- `alert=1`: only the detections in which some object alerted (see below). The
  filter is per detection: the object that didn't alert stays in it. It applies
  over what is left of the other filters: with `families=vehicle`, a detection
  that alerted only because of the person drops out, because the person drops
  out. Any other value is an error.

The areas of a camera **add up**, the one from `area` included: the object
stays if it is in any of them. A camera with no area at all in the filter comes
whole.

```json
{
  "detections": [
    {
      "cam": "cam_teste1",
      "atMs": 1786220571043,
      "frameMs": 1786220572851,
      "hasFrame": true,
      "objects": [
        { "family": "person", "class": "person", "score": 0.87, "box": [0.5156, 0.2611, 0.6734, 0.9778],
          "alert": true, "alertAreas": ["k3f9a2"] },
        { "family": "vehicle", "class": "car", "score": 0.61, "box": [0.05, 0.4, 0.3, 0.7] }
      ]
    }
  ],
  "end": false
}
```

A **detection** is an inference by the object detector on one camera: the
onset's instant and everything it found in that frame. Two people and a car in
the same frame are three marks in `events/{day}.ndjson`, but only one
detection, with three objects. The family filter and the area filter take
objects out, and a detection left with none leaves the page. With an area, a
mark without a box, recorded before the box existed, is left out: there is no
way to know where it was. `box` and `frameMs` are the same as in
`/api/rec/events`.

`alert` says the object passed the camera's alert rule at the time it was
inferred, and `alertAreas`, the ids of the alert areas it fell in; without
areas in the alert, or without an alert, the field doesn't come. It is what
was recorded: changing the camera's alert doesn't change the past, and the id
of a deleted area keeps coming. The viewer receives them too.

The next cursor is the `atMs` of the last detection on the page. The page
**doesn't split an instant in half**: if the last one ties, in the same
millisecond, with one from another camera, both come and the page goes a little
past the limit - otherwise the cursor would skip the one left out. `end` says
there is nothing more in the requested direction; it can come false on the page
that happened to catch the last detection, and the next one comes empty and
with `end` true.

The object mark arrives **late**, as in `/api/rec/events`: it carries the
onset's instant, but is only recorded after the object detector runs the
inference. A new detection can, then, land below the top the screen already
showed.

The server memoizes the object marks of each day already read, without the
onsets: a past day is read from disk just once, and for the current day only
the piece the file has grown. Measured with 11 days of a real installation,
nine cameras and 7,987 detections: 0.85 MB in memory, and a page of 60 in
0.2 ms after the first read. The day leaves memory when the retention deletes
it.

**`/api/detections/days`** is what the Detections screen's calendar lets you
pick, with the number it shows on each day. `cams`, `families`, `area`, `rule`,
`areas` and `alert` are the same as in `/api/detections`, and the day is
included if it has at least one detection matching the filter.
`detectionsPerDay` counts detections, not marks, as `/api/detections` builds
them: a person and a car in the same frame count once. A day without a
recording is left out even with a mark, because scrolling doesn't reach it
either.

```json
{
  "days": ["2026-10-03", "2026-10-04", "2026-10-05"],
  "detectionsPerDay": { "2026-10-03": 508, "2026-10-04": 294, "2026-10-05": 462 }
}
```

It uses the same memory as the scrolling, and counting forces reading all
cameras of each day. Measured on the Orange Pi Zero 3 with 28 days, nine
cameras, 13 MB of events and 18,772 detections, with a cold disk cache: the
first call took 1.2 s and 1.3 s, and the following ones come from memory in
~5 ms. Stopping at the first camera with a mark, without counting, took 0.4 s.
The screen rereads the list on every calendar open, because the current day
grows while it is open.

**`/api/detections/frame`** serves the file as it is on disk, without resizing
or re-encoding: `t` is the detection's `atMs`. It never changes once recorded,
so it goes with `immutable` and scrolling back asks nothing from the server. A
detection with `hasFrame` false responds 404.

## Users

All **admin only**. Without authentication on there is no registry: the listing
comes with `"authRequired": false` and the rest responds `409`.

| Endpoint | What it does |
| --- | --- |
| `GET /api/users` | the people, with each one's status, and the owner separately |
| `POST /api/users` | creates a viewer, `{"username", "name", "email"}`, and returns the link's token; the e-mail is optional |
| `POST /api/users/check` | what `POST /api/users` would reject with the same body, without creating anyone |
| `POST /api/users/link` | new link, `{"username"}`: erases the current password and drops the person right away |
| `POST /api/users/link/email` | sends the link by e-mail, `{"username", "url"}`, to the person's e-mail |
| `POST /api/users/email` | changes someone's e-mail, `{"username", "email"}`; empty erases |
| `POST /api/users/email/check` | what `POST /api/users/email` would reject with the same body, without changing anything |
| `DELETE /api/users?username=` | removes the person, and their photo, and drops them right away |
| `DELETE /api/users/avatar?username=` | removes someone's photo; replacing it, only the person themselves |

```json
// GET /api/users
{
  "authRequired": true,
  "owner": { "username": "admin", "name": "admin", "avatar": "", "lastAccessMs": 1791986400000 },
  "users": [
    { "username": "maria", "name": "Maria", "email": "maria@exemplo.com", "role": "viewer", "status": "invitePending",
      "inviteExpiresAtMs": 1791587903000, "createdAtMs": 1791587303000, "avatar": "4d1e0b62a7c3f915",
      "lastAccessMs": 1791500000000 }
  ],
  "inviteValidityMs": 600000,
  "maxNameLength": 60,
  "maxUsernameLength": 32,
  "maxEmailLength": 254,
  "emailEnabled": true
}
```

The `status` is `active` (has a password), `invitePending` (can set the
password through the link) or `inviteExpired` (needs another link). Whoever is
`active` also brings `activeSinceMs`, when they set the password through the
link; changing their own password doesn't change it, and the new link erases
it.

The `lastAccessMs` is the person's last request with a valid session, and not
the last login, which says little with the session renewing itself. It moves
minute by minute (`users.LastAccessStep`) and comes in any `status`: whoever
got a new link keeps the access from before. It goes away for whoever hasn't
used dwnvr since the field existed, and for whoever is removed or created again
with the same username. The request only notes it in memory; the `access.json`,
in the config folder, is written in the background, at most every 5 minutes
(`users.AccessFlushDelay`) and on shutdown.

Create and the new link return
`{"user": {...}, "token": "..."}`: the token only exists in this response, and
the screen builds the link with it. A username in the wrong format, repeated or
equal to the owner's, and an empty name, are `400` with the reason; a person
who doesn't exist is `404`.

The rejection from create and from change e-mail comes field by field in
`fields`, with the first one, in screen order, also in the usual `error`:

```json
// POST /api/users -> 400
{
  "error": "já existe alguém com este usuário",
  "fields": { "username": "já existe alguém com este usuário", "email": "já existe alguém com este e-mail" }
}
```

The two `/check` routes are the same validation without storing anything: the
screen checks each field when the admin leaves it. They respond `200` with
`{"fields": {...}}`, with only the rejected fields, and `{"fields": {}}` when
everything is fine. The validation also counts the ignored entry of
`users.json` (its login and e-mail are not free), which the listing doesn't
bring: that is why the screen doesn't check on its own. With an unreadable
`users.json`, the in-memory registry is empty, and they respond `500` instead
of saying everything is fine.

The `email` is a single address, without a name in front, and is **unique**
among the people, case-insensitively: an invalid e-mail or one belonging to
another person is `400`. `POST /api/users/email` returns `{"user": {...}}` and
holds even with e-mail off, because storing sends nothing. An e-mail misspelled
by hand in `users.json` becomes a warning in the log and in `/api/health` and
doesn't come in the listing, but stays in the file. The `emailEnabled` says
whether the `smtp` section is on; without it, the screen hides "Enviar por
e-mail" (Send by e-mail) and warns, where the e-mail is typed, that the link
won't go by e-mail.

`POST /api/users/link/email` receives the link the way the screen shows it,
because the server doesn't know which address it is accessed through: the
screen builds it with the admin's browser one. The server checks that it is
that person's open link (`http` or `https`, with no username or query, only the
`#invite?token=`) and sends it to their e-mail. It returns `{"sentTo": "..."}`.
Another person's link, in another form, or a person without e-mail, are `400`;
an expired or used link, `410`; e-mail off, `409`; the e-mail server refusing,
`502` with its response. The log says who it went to, never the link.

## My account

What each person changes about themselves, of any role; nobody changes anyone
else's through here. Without authentication on there is no one the account
belongs to, and everything responds `409`. In the interface this is the
**Minha conta** screen.

| Endpoint | What it does |
| --- | --- |
| `GET /api/account` | who is logged in and the form's numbers |
| `POST /api/account/password` | changes their own password, `{"currentPassword", "newPassword"}`; the owner gets `409` |
| `POST /api/account/name` | changes their own display name, `{"name"}` |
| `PUT /api/account/avatar` | stores their own photo: the body is the JPEG, already downsized |
| `DELETE /api/account/avatar` | removes their own photo |
| `GET /api/avatar?id=` | the photo: their own, or any for the admin |

```json
// GET /api/account
{
  "user": { "username": "maria", "name": "Maria", "role": "viewer", "owner": false },
  "minPasswordLength": 8,
  "maxPasswordLength": 128,
  "maxNameLength": 60,
  "avatar": { "sidePx": 256, "maxBytes": 32768, "quality": 92 }
}
```

The routes that change something return `{"ok": true, "user": {...}}` as it
ended up, so the screen updates the header without asking again.

**The password** of the owner lives in `dwnvr.yaml`, and is changed there. For
the others, the change asks for the current password and pays two password
computations, in the same queue as the login: one to check the current one,
another to generate the new one. A wrong current password, or a new one outside
8 to 128 characters, is `400` with the reason, and nothing drops. Once changed,
the person's other devices go back to the login (see
[The cookie](#the-cookie)).

**The photo** is downsized in the browser: the original doesn't leave the
device. It crops the center square, scales down to `sidePx` pixels and
generates a JPEG at `quality`, lowering the quality only if it goes over
`maxBytes`. The server stops reading at the ceiling (`413`), decodes the whole
image and rejects with `400` whatever isn't a square JPEG of up to `sidePx`
pixels. It stores the bytes as they came, without compressing again, in
`avatars/<id>.jpg` in the config folder. The id changes on every replacement,
so `GET /api/avatar` goes with `Cache-Control: private,
max-age=31536000, immutable` and `nosniff`: a new photo is a new URL. Another
person's photo, for whoever isn't admin, responds `404`, like one that doesn't
exist.

## Live

| Endpoint | What it does |
| --- | --- |
| `GET /api/live/ws?src=` | proxy of go2rtc's websocket (`/api/ws`), with the credential staying on the server |

The browser never talks to go2rtc directly. Going through the proxy solves two
things at once: the go2rtc API password doesn't go to the client, and the live
view respects the same session as the rest of the interface. Whoever is removed
has the websocket closed right away (see
[When someone is dropped](#when-someone-is-dropped)).

Only the player's websocket passes. The rest of go2rtc's API - configuration,
restart, streams - is not reachable through here; what dwnvr needs from it, it
asks for on the server side.

## Why the UI doesn't require a session

The SPA files - HTML, CSS and JS - are served **without** authentication. It is
just the app shell, with no camera data at all. Protecting it would stop the
browser from loading the login screen itself. Everything that is data is behind
the endpoints above.
