# Operations

## Where the files are

Nothing important lives inside the container. The volumes cover everything.
The host paths below are those of the [real
installation](../README.md#installing-for-real), defined in `.env` by
`DWNVR_CONFIG_DIR`, `DWNVR_STORAGE_DIR` and `GO2RTC_CONFIG_DIR`; in the quick
test the same files live in `./config/dwnvr`, `./storage` and
`./config/go2rtc`.

| On the host (example) | In the container | What it is |
| --- | --- | --- |
| `/mnt/storage/dwnvr/config/dwnvr/dwnvr.yaml` | `/etc/dwnvr/dwnvr.yaml` | configuration, edited by hand |
| `/mnt/storage/dwnvr/config/dwnvr/cameras.json` | `/etc/dwnvr/cameras.json` | cameras, written by the registration screen |
| `/mnt/storage/dwnvr/config/dwnvr/.session-secret` | `/etc/dwnvr/.session-secret` | signs the session cookies (0600) |
| `/mnt/storage/dwnvr/config/dwnvr/users.json` | `/etc/dwnvr/users.json` | the people besides the owner, written by the **Usuários** (Users) screen and by **Minha conta** (My account) (0600) |
| `/mnt/storage/dwnvr/config/dwnvr/avatars/` | `/etc/dwnvr/avatars/` | each person's photo, a JPEG of up to 32 KB |
| `/mnt/storage/dwnvr/config/dwnvr/access.json` | `/etc/dwnvr/access.json` | each person's last access, which the Usuários screen shows (0600) |
| `/mnt/storage/dwnvr/config/go2rtc/go2rtc.yaml` | `/config/go2rtc.yaml`, in the go2rtc container | the cameras, edited by hand |
| `/mnt/storage/dwnvr/recordings/` | `/storage/` | recordings, indexes, init segments, motion marks and the frames of the detections |

So: **edit and inspect everything from the host**, without entering the
container.

```sh
cat /mnt/storage/dwnvr/config/dwnvr/cameras.json
tail -f /mnt/storage/dwnvr/recordings/cam_iota/index/$(date +%F).ndjson

# the motion marks, if the camera has detection on
tail -f /mnt/storage/dwnvr/recordings/cam_iota/events/$(date +%F).ndjson

# the day's detection frames, one .jpg per detection, named by the instant
ls -l /mnt/storage/dwnvr/recordings/cam_iota/frames/$(date +%F)/
```

To find the paths of any installation:

```sh
docker inspect dwnvr --format '{{range .Mounts}}{{.Source}} -> {{.Destination}}{{println}}{{end}}'
```

**Editing `cameras.json` by hand requires a restart** (`docker compose restart
dwnvr`); through the registration screen the change takes effect immediately.
An out-of-range value doesn't stop the boot: the default applies, and the
warning appears on the **Diagnóstico** (Diagnostics) screen - see
[Value out of range](configuration.md#value-out-of-range).

## Copying recordings straight from disk

The simplest way is the export button on the **Gravações** (Recordings)
screen: it hands over the chosen stretch in a single file. Straight from disk
is for keeping whole days, copying several cameras at once, or when the
interface isn't at hand. Copying while dwnvr runs doesn't disturb recording.

On disk, a recording is a series of `.mp4` files of ~30s, and each one plays
on its own. The name is the start in epoch ms, and the day's folder follows
the `TZ` in `.env`. The commands below use the host's time zone, which is
usually the same; if it isn't, put `TZ=America/Fortaleza` (the one in `.env`)
in front of `date`.

```sh
cd /mnt/storage/dwnvr/recordings/cam_iota/2026-08-08

# the start time of each file
for f in *.mp4; do echo "$(date -d @${f%???.mp4} +%T)  $f"; done
```

The last file in today's folder is still being recorded and cuts off in the
middle.

To copy just one time range, the index gives the start (`t`) and the duration
(`d`) of each file, so it also finds the one that started before the requested
time and covers it. It needs `jq`:

```sh
cd /mnt/storage/dwnvr/recordings/cam_iota
ini=$(date -d '2026-08-08 17:30' +%s000)
fim=$(date -d '2026-08-08 17:32' +%s000)

mkdir -p ~/trecho
jq -r --argjson ini "$ini" --argjson fim "$fim" \
  'select(.t < $fim and .t + .d > $ini) | "\(.t).mp4"' \
  index/2026-08-08.ndjson | xargs -I{} cp 2026-08-08/{} ~/trecho/
```

The index only lists closed files, so the one still being recorded is left
out. A range that crosses midnight needs the index of both days.

## Changing the password and ending the sessions

Whoever signs in once stays signed in: the session lasts 30 days and renews
with use. Each person's cookie signature depends on their password, so
changing someone's password signs out only that person.

For the people created in the **Usuários** tab, it is done on the screen,
without restarting anything: **gerar link novo** (generate new link) erases
their password, and **remover** (remove) erases the person. In both cases they
are signed out right away from every device, live view open included. The
person can also change their own password, in **Minha conta**, by giving the
current one: the device they changed it on stays signed in, and the others are
signed out.

For the owner, and to sign everyone out, there are two ways, and both require
restarting dwnvr, which only reads these files at startup:

- **Change the owner's password**, or user, in `dwnvr.yaml`. Every owner
  cookie issued before stops being valid, and only whoever knows the new
  password gets in. The other users stay signed in.
- **Delete `.session-secret`**, to sign everyone out, the owner and the other
  users, without changing any password. dwnvr generates another at startup.

```sh
# To sign everyone out without changing the password:
rm /mnt/storage/dwnvr/config/dwnvr/.session-secret

# In both ways, afterwards:
docker compose restart dwnvr
```

In both cases, whoever was signed out and had the page open goes back to the
login screen.

## Inspecting a container without a shell

The image is `FROM scratch` and contains literally this:

```text
/dwnvr                          the binary
/etc/ssl/certs/ca-certificates.crt
```

The rest (`/dev`, `/proc`, `/etc/hosts`, `/etc/resolv.conf`) is injected by
Docker. There is no `sh`, `ls` or `cat` - which is the point: less surface,
less weight and nothing for an intruder to use.

That doesn't prevent inspecting. Four ways, from the simplest to the most
invasive:

### 1. Logs and state

```sh
docker logs -f dwnvr
docker logs dwnvr | grep -E 'level=(WARN|ERROR)'
docker logs dwnvr | grep -E 'msg="(login accepted|login attempt rejected)"|msg=logout'
docker stats --no-stream dwnvr
docker inspect dwnvr --format '{{.State.Health.Status}}'
```

### 2. The binary itself as a tool

`docker exec` doesn't need a shell - it needs an executable, and there is one:

```sh
docker exec dwnvr /dwnvr -healthcheck -config /etc/dwnvr/dwnvr.yaml; echo $?
docker exec dwnvr /dwnvr -h
```

### 3. Copying files out

`docker cp` is implemented by the daemon and works on an empty image:

```sh
docker cp dwnvr:/etc/dwnvr/cameras.json /tmp/
```

### 4. A sidecar sharing the namespaces

When you need to look at the network or processes from the inside, you start a
disposable container **with the tools** that shares dwnvr's namespaces:

```sh
docker run --rm -it \
  --pid=container:dwnvr \
  --network=container:dwnvr \
  busybox sh
```

Inside:

```sh
ps -o pid,args              # sees the dwnvr process as PID 1
netstat -ltn                # sees its ports
wget -qO- http://127.0.0.1:8080/api/session
```

To also inspect dwnvr's *file system*, add `--volumes-from dwnvr` - the
volumes show up at the same paths.

None of this changes the image: the sidecar is discarded on exit.

## Changing version

```sh
docker compose up -d --pull always
```

The tag of both images, dwnvr and dwnvr-detect, comes from `DWNVR_VERSION` in
`.env`; without it, the default in `docker-compose.yml` applies, which each
release rewrites with its own version. The possible values:

| `DWNVR_VERSION` | What runs |
| --- | --- |
| (absent) | the compose's version in the clone: the latest release, after a `git pull` |
| `v0.1.0` | stays on that version, even after `git pull`; it is also how you go back to an earlier one |
| `main` | every commit on main, before it becomes a version |
| `sha-abc1234` | a specific commit |

The versions and what changed in each are in the
[releases](https://github.com/mhagnumdw/dwnvr/releases).

go2rtc follows the same idea, with its own variable: `GO2RTC_VERSION` in
`.env` overrides the version pinned in the compose. The release doesn't
rewrite it; it changes only by commit, after being tested with dwnvr.

Shutdown is graceful: dwnvr closes and indexes each camera's open segment
before exiting. Without that, every restart would lose the last minute
recorded.

If something goes wrong, the data survives the image - it is in the volumes.
Going back to the previous version is changing the tag and starting again.

## Common problems

**Files showing up as root on disk.** `DWNVR_UID` and `DWNVR_GID` are missing
from `.env`, or the `user:` disappeared from the compose. Find yours with
`id -u; id -g`. To fix what was already written:
`sudo chown -R 1000:1000 /mnt/storage/dwnvr`.

**The timeline turns the day at the wrong time, or the alert goes out at the
wrong hours.** `TZ` is missing from `.env`. The image has no
`/usr/share/zoneinfo` - the time zone database is embedded in the binary, but
someone has to say which zone to use.

**The container doesn't see go2rtc.** Inside the container, `localhost` is the
container itself. Use `host.docker.internal` (with `extra_hosts:
host-gateway`), the service name if both are on the same network, or
`network_mode: host`.

**The go2rtc interface doesn't open from another machine.** That is on
purpose: the compose publishes 1984 and 8554 only to the server itself,
because go2rtc doesn't ask for a password by default. Use an ssh tunnel, or
open it with a password - see [Opening go2rtc
to the network](../README.md#opening-go2rtc-to-the-network-).

**The page is stale after updating.** The interface is embedded in the binary,
and the browser caches the assets - which have a hash in the name precisely so
that doesn't happen. If it persists, it is a sign the binary was built without
running `npm run build` first.

**No object marks appear.** Three things, in this order:

1. **`detector.url` is empty.** Without it, dwnvr doesn't ask anyone
   anything. With it, the dwnvr log says `object detector configured` at
   startup.
2. **`dwnvr-detect` didn't start.** It only starts with the `detect` profile
   (`COMPOSE_PROFILES=detect` in `.env`). Check with
   `docker inspect dwnvr-detect --format '{{.State.Health.Status}}'`.
3. **The object detector went down.** dwnvr warns in the log once, with
   `object detector down`, and again when it comes back. Meanwhile, the
   cameras keep recording and marking motion. The lost inferences show up as
   `failed` in the `/api/health` funnel.
