# Upgrading to 1.0.0

The 1.0.0 line renames what dwnvr exposes and stores from Portuguese to en-US.
The new version reads **only** the new names: there is no alias and no
fallback, so follow every section below, in order, before you start it.

The guide grows one section per part of the conversion. Each section says what
to do, and what you may leave alone.

## 0. Back up first

The backup is also the way back to the previous version.

```sh
docker compose down
backup=~/dwnvr/config.bak-$(date +%Y%m%d-%H%M%S)
test ! -e "$backup" && cp -a ~/dwnvr/config "$backup"
```

The backup goes to a new, dated folder, so a backup from an earlier update is
never reused or overwritten. If you moved the configuration folder with
`DWNVR_CONFIG_DIR`, back up that folder instead.

Keep the `storage` folder as it is. The sections below say when something in it
changes.

## 1. The HTTP API and the web interface

Who is affected: **anyone who calls the API with a script**, and anyone who
keeps **bookmarks** or **invite links** to the interface. If you only use the
interface, there is nothing to change in your configuration: the new build
brings the interface and the server together.

### Routes

| Before | Now |
| --- | --- |
| `POST /api/convite/conferir` | `POST /api/invite/check` |
| `POST /api/convite` | `POST /api/invite` |
| `/api/usuarios` | `/api/users` |
| `POST /api/usuarios/conferir` | `POST /api/users/check` |
| `POST /api/usuarios/link` | `POST /api/users/link` |
| `POST /api/usuarios/link/email` | `POST /api/users/link/email` |
| `POST /api/usuarios/email` | `POST /api/users/email` |
| `POST /api/usuarios/email/conferir` | `POST /api/users/email/check` |
| `DELETE /api/usuarios/avatar` | `DELETE /api/users/avatar` |
| `GET /api/conta` | `GET /api/account` |
| `POST /api/conta/senha` | `POST /api/account/password` |
| `POST /api/conta/nome` | `POST /api/account/name` |
| `PUT`, `DELETE /api/conta/avatar` | `PUT`, `DELETE /api/account/avatar` |
| `GET /api/deteccoes` | `GET /api/detections` |
| `GET /api/deteccoes/quadro` | `GET /api/detections/frame` |
| `GET /api/deteccoes/dias` | `GET /api/detections/days` |
| `GET /api/health/servidor` | `GET /api/health/server` |
| `POST /api/email/teste` | `POST /api/email/test` |

The other routes (`/api/rec/*`, `/api/cameras`, `/api/health`, ...) keep their
paths.

### Query parameters

On `GET /api/detections` and `GET /api/detections/days`:

| Before | Now |
| --- | --- |
| `antes` | `before` |
| `depois` | `after` |
| `limite` | `limit` |
| `familias` | `families` |

The family **values** (`pessoa`, `veiculo`, `animal`) do not change in this
section.

### JSON keys in the responses

Only a script that reads these needs the change. The names follow the
documentation in [api.md](api.md).

| Response | Before → now |
| --- | --- |
| `/api/detections` | `deteccoes` → `detections`, `fim` → `end`, `instanteMs` → `atMs`, `quadroMs` → `frameMs`, `temQuadro` → `hasFrame`, `objetos` → `objects`, `familia` → `family`, `classe` → `class`, `caixa` → `box` |
| `/api/detections/days` | `dias` → `days`, `deteccoesPorDia` → `detectionsPerDay` |
| `/api/rec/events` | `objetos` → `objects`, and inside each object the same keys as above |
| `/api/cameras` | `padrao` → `defaults`, `faixas` → `ranges` |
| `/api/streams/probe` | `erro` → `error` |
| `/api/go2rtc/restart` | `reiniciado` → `restarted` |
| `/api/reconnects/reset` | `zeradas` → `reset` |
| `/api/health`, per camera | `funil` → `funnel` (`pedacos` → `clips`, `semVideo` → `noVideo`, `descartados` → `dropped`, `falhas` → `failed`, `recusados` → `rejected`, `semObjeto` → `noObject`, `comObjeto` → `withObject`, `naFila` → `queued`) |
| `/api/health`, `detector` | `fila` → `queue` (`agora` → `length`, `pico` → `peak`, `porCamera` → `perCamera`, `esperando` → `waiting`, `olhando` → `busy`), `tempos` → `times` (`analiseMs` → `inferenceMs`, `esperaMs` → `waitMs`), `foraDoAr` → `outage` (`desde` → `since`, `erro` → `error`) |
| `/api/health`, `go2rtc` drift | `novas` → `added`, `alteradas` → `changed`, `foraDoArquivo` → `notInFile` |
| `/api/health/server` | every key moves to en-US: see the example under `GET /api/health/server` in [api.md](api.md) |

### Interface links and saved choices

- A saved link to the **Users**, **Account** or **invite** screens stops
  opening them: `#usuarios` is now `#users`, `#conta` is `#account` and
  `#convite?token=...` is `#invite?token=...`. An **invite link that was sent
  but not used yet** has the old form: replace `#convite` with `#invite` in it,
  or generate a new link.
- The address-bar options of the screens were renamed: `editar` → `edit`,
  `familias` → `families`, `caixas` → `boxes`, `rotulos` → `labels`,
  `atualizar` → `refresh`, and on Detections `dia`, `de`, `ate` → `day`,
  `from`, `to`. The sort column of Diagnostics (`sort`, `dsort`) also changed
  its values. A link with the old options still opens the screen, with the
  default view.
- Three browser preferences go back to their defaults once, because their
  storage keys were renamed: the recordings thumbnails, the pinned recording
  controls, and the "always on" cameras of the live screen.

## 2. `dwnvr.yaml`, `cameras.json` and the object detector

Who is affected: **everyone who turned motion marking on** (`detect: true`),
and anyone who calls `/api/cameras` with a script. Two keys were renamed, and
they are the same key in three places:

| Before | Now |
| --- | --- |
| `detectMecanismo` | `detectMethod` |
| `detectSensibilidade` | `detectSensitivity` |

Their **values** (`kleinberg-p`, `periodico`, `1` to `5`) do not change in
this section.

A key with the old name is ignored, with no warning: the camera falls back to
the default method and sensitivity.

### `dwnvr.yaml`

The keys live under `defaults`. With the stack still down from section 0:

```sh
sed -i -E 's/^(\s*#?\s*)detectMecanismo:/\1detectMethod:/; s/^(\s*#?\s*)detectSensibilidade:/\1detectSensitivity:/' \
  ~/dwnvr/config/dwnvr/dwnvr.yaml
```

The expression only touches a key at the start of a line, so the name of an
area or a camera is left alone. It does not reach a key written inline, as in
`defaults: {detectMecanismo: periodico}`. To check, this must print nothing:

```sh
grep -n -E 'detectMecanismo|detectSensibilidade' ~/dwnvr/config/dwnvr/dwnvr.yaml
```

If it prints a line, rename the key in it by hand.

### `cameras.json`

The Cameras screen writes this file, with one entry per camera. A camera that
uses the defaults from `dwnvr.yaml` has neither key, and that is fine.

```sh
f=~/dwnvr/config/dwnvr/cameras.json
jq 'map(with_entries(.key |= ({"detectMecanismo": "detectMethod", "detectSensibilidade": "detectSensitivity"}[.] // .)))' \
  "$f" > "$f.new" && cat "$f.new" > "$f" && rm "$f.new"
```

`cat` over the file, and not `mv`, keeps its owner and permissions. If you
moved the configuration folder with `DWNVR_CONFIG_DIR`, use that folder in the
two commands above.

### The object detector (`dwnvr-detect`)

Two environment variables were renamed:

| Before | Now |
| --- | --- |
| `DETECT_MODELO` | `DETECT_MODEL` |
| `DETECT_PORTA` | `DETECT_PORT` |

`DETECT_THREADS` keeps its name. The `docker-compose.yml` of dwnvr sets none of
the two renamed ones, so only an installation that added them by hand needs a
change. To find them:

```sh
grep -n -E 'DETECT_(MODELO|PORTA)' ~/dwnvr/.env ~/dwnvr/docker-compose*.y*ml
```

Rename each line it prints. With the old name, the object detector uses its
default model and port.

The HTTP contract between dwnvr and the object detector also moved to en-US.
There is nothing to do about it: the two images of the same version speak the
same names, so update both together, as the `docker-compose.yml` already does.
Only a script that calls `POST /detect` by hand needs the new names, in
[dwnvr-detect/README.md](../dwnvr-detect/README.md).

## 3. The people files

Who is affected: **everyone who turned authentication on** and invited
someone on the Users screen, set a name or a photo on My account, or logged
in. Three names in the configuration folder were renamed:

| Before | Now |
| --- | --- |
| `usuarios.json` | `users.json` |
| `acessos.json` | `access.json` |
| `avatares/` | `avatars/` |

What is inside them does not change. With the old names, only the owner from
`dwnvr.yaml` logs in, the photos are gone and the Users screen shows no last
access.

With the stack still down from section 0:

```sh
d=~/dwnvr/config/dwnvr
for pair in usuarios.json:users.json acessos.json:access.json avatares:avatars; do
  old="$d/${pair%%:*}" new="$d/${pair##*:}"
  test -e "$old" || continue
  test ! -e "$new" || mv "$new" "$new.new"
  mv "$old" "$new"
done
```

Each old name that exists gets the new one. If the new name is already there
too, the new version ran before this section: the old file has your people,
so it wins, and the other one is kept with `.new` at the end, for you to check
and delete. Running the loop again does nothing.

`mv` keeps the owner and the `0600` permissions. If you moved the
configuration folder with `DWNVR_CONFIG_DIR`, set `d` to that folder.

## 4. The detection marks and frames

Who is affected: **everyone who turned motion marking on** (`detect: true`).
In the storage folder, two folders of each camera were renamed, and so were
the keys of each line in the marks:

| Before | Now |
| --- | --- |
| `eventos/` | `events/` |
| `quadros/` | `frames/` |
| `instanteMs` | `atMs` |
| `familia` | `family` |
| `classe` | `class` |
| `quadroMs` | `frameMs` |
| `caixa` | `box` |
| `temQuadro` | `hasFrame` |

The family **values** (`pessoa`, `veiculo`, `animal`) do not change in this
section. Without this section, the new version does not see anything detected
before the update: the timeline loses the motion strip of the old recordings,
and the Detections screen starts empty.

The backup is a copy of each `eventos/`, which is small next to the video
(about 15 MB for nine cameras and 30 days). To see how much room it takes:

```sh
du -sch ~/dwnvr/storage/*/eventos | tail -1
```

With the stack still down from section 0:

```sh
sh -s ~/dwnvr/storage <<'EOF'
aside() {
  test ! -e "$1" && return
  test ! -e "$1.new" && mv "$1" "$1.new" && return
  echo "$1.new already exists: move it out of the way and run again"
  return 1
}
t=$(mktemp) || exit 1
for cam in "$1"/*/; do
  cam=${cam%/}
  if test -d "$cam/eventos"; then
    test -e "$cam/eventos.bak" || {
      rm -rf "$cam/eventos.bak.tmp" &&
      cp -a "$cam/eventos" "$cam/eventos.bak.tmp" &&
      mv "$cam/eventos.bak.tmp" "$cam/eventos.bak"
    } || { echo "$cam: backup failed, skipped"; continue; }
    ok=1
    for b in "$cam"/eventos.bak/*.ndjson; do
      test -e "$b" || continue
      jq -R -c 'fromjson? | objects | with_entries(.key |= ({"instanteMs": "atMs", "familia": "family", "classe": "class", "quadroMs": "frameMs", "caixa": "box", "temQuadro": "hasFrame"}[.] // .))' \
        "$b" > "$t" && cat "$t" > "$cam/eventos/${b##*/}" || { ok=; echo "$b: not converted"; }
    done
    test -n "$ok" && aside "$cam/events" && mv "$cam/eventos" "$cam/events"
  fi
  if test -d "$cam/quadros"; then
    aside "$cam/frames" && mv "$cam/quadros" "$cam/frames"
  fi
done
rm -f "$t"
EOF
```

For each camera, it copies `eventos/` to `eventos.bak/`, writes each file of
`eventos/` again from its copy, with the new keys, and renames the folder.
`quadros/` is only renamed: the images do not change. For 420 thousand lines it takes under a minute on a
small ARM board, and a few seconds on a desktop.

- A line that a power loss left cut or filled with zeros is dropped. dwnvr
  already skipped it, so nothing disappears from the screens.
- If `events/` or `frames/` is already there too, the new version ran before
  this section: the old folder has your history, so it wins, and the other one
  is kept with `.new` at the end, for you to check and delete. If that `.new`
  name is taken too, the camera is skipped with a message.
- A camera that fails prints why and keeps the old names. Fix the cause (a
  full disk, for example) and run the same command again: each file is read
  from `eventos.bak/` again, so a file cut by the failure is written whole,
  and a camera that is done is left alone.
- Rewriting with `cat`, and not `mv`, keeps the owner and permissions of each
  file, as in section 2.

If you moved the storage folder with `DWNVR_STORAGE_DIR`, use that folder in
the place of `~/dwnvr/storage`. To check, this must print nothing:

```sh
ls -d ~/dwnvr/storage/*/eventos ~/dwnvr/storage/*/quadros 2>/dev/null
```

Once the Detections screen shows the marks from before the update, delete the
backup:

```sh
rm -rf ~/dwnvr/storage/*/eventos.bak
```

To go back to the previous version instead, rename each `eventos.bak/` back
to `eventos/` (after removing the `events/` next to it) and each `frames/`
back to `quadros/`.

## Start again

```sh
docker compose up -d
```

On the Cameras screen, the method and sensitivity of each camera are the ones
you had before the update. On the Users screen, each person has their photo
and last access. On the Detections screen, the marks from before the update
are there, each with its frame.
