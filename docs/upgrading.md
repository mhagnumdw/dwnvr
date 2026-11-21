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

The family **values** (`pessoa`, `veiculo`) change in
[section 5](#5-the-family-and-method-values).

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

Their **values** change only in
[section 5](#5-the-family-and-method-values): `periodico` becomes `periodic`.

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

The family **values** (`pessoa`, `veiculo`) change in
[section 5](#5-the-family-and-method-values). Without this section, the new version does not see anything detected
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
taken() {
  test -d "$1/$2" && test -e "$1/$3" && test -e "$1/$3.new" &&
    echo "$1: $3/ and $3.new/ both exist: move $3.new/ out of the way and run again"
}
aside() { test ! -e "$1" || mv "$1" "$1.new"; }
t=$(mktemp) || exit 1
for cam in "$1"/*/; do
  cam=${cam%/}
  taken "$cam" eventos events || taken "$cam" quadros frames && continue
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
    test -n "$ok" && aside "$cam/events" && mv "$cam/eventos" "$cam/events" || continue
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
`quadros/` is only renamed: the images do not change. For 420 thousand lines
it takes under a minute on a small ARM board, and a few seconds on a desktop.

- A line that a power loss left cut or filled with zeros is dropped. dwnvr
  already skipped it, so nothing disappears from the screens.
- If `events/` or `frames/` is already there too, the new version ran before
  this section: the old folder has your history, so it wins, and the other one
  is kept with `.new` at the end, for you to check and delete. If that `.new`
  name is taken too, the camera is skipped with a message, before anything
  in it changes.
- A camera that fails prints why. If its marks fail, it keeps both old folder
  names, `quadros/` included. Fix the cause (a full disk, for example) and run
  the same command again: each file of the marks is read from `eventos.bak/`
  again, so a file cut by the failure is written whole, and the folders
  already renamed are left alone.
- Rewriting with `cat`, and not `mv`, keeps the owner and permissions of each
  file, as in section 2.

If you moved the storage folder with `DWNVR_STORAGE_DIR`, use that folder in
the place of `~/dwnvr/storage`. To check, this must print nothing:

```sh
ls -d ~/dwnvr/storage/*/eventos ~/dwnvr/storage/*/quadros 2>/dev/null
```

The backup also covers section 5. Once the Detections screen shows the marks
from before the update, after [Start again](#start-again), delete it:

```sh
rm -rf ~/dwnvr/storage/*/eventos.bak
```

To go back to the previous version instead, rename each `eventos.bak/` back
to `eventos/` (after removing the `events/` next to it) and each `frames/`
back to `quadros/`.

## 5. The family and method values

Who is affected: **everyone who turned motion marking on** (`detect: true`).
Three values were renamed, and each one is the same value everywhere it
appears:

| Before | Now | Where |
| --- | --- | --- |
| `pessoa` | `person` | the families of a camera's alert, the marks in `events/`, the API |
| `veiculo` | `vehicle` | the same |
| `periodico` | `periodic` | `detectMethod`, in `dwnvr.yaml`, `cameras.json` and the API |

`animal` and `kleinberg-p` keep their names. With an old value, a camera on
`periodico` falls back to the default method, an alert loses the `pessoa` and
`veiculo` families (and is turned off if none is left), and the marks from
before the update show up in gray, without an icon, and leave the family
filter of the Detections screen. On the API, `families=pessoa` is now an
error.

The stack is still down from section 0. The three steps below change only
these values: an area or camera name stays as it is, and running a step again
changes nothing.

### `dwnvr.yaml`

```sh
sed -i -E 's/^(\s*#?\s*detectMethod:\s*"?)periodico\b/\1periodic/' \
  ~/dwnvr/config/dwnvr/dwnvr.yaml
```

To check, this must print nothing:

```sh
grep -n -E 'detectMethod.*periodico' ~/dwnvr/config/dwnvr/dwnvr.yaml
```

If it prints a line, write `periodic` in it by hand.

### `cameras.json`

The method of a camera, and the families of its alert:

```sh
f=~/dwnvr/config/dwnvr/cameras.json
if test ! -e "$f.new"; then
  jq 'map(
        (if .detectMethod == "periodico" then .detectMethod = "periodic" else . end)
      | (if .alert.families then .alert.families |= map({"pessoa": "person", "veiculo": "vehicle"}[.] // .) else . end))' \
    "$f" > "$f.tmp" && mv "$f.tmp" "$f.new"
fi
test -e "$f.new" && cat "$f.new" > "$f" && rm "$f.new"
```

The converted file is only named `cameras.json.new` once it is whole. If the
last line fails (a full disk, for example), fix the cause and run the same
lines again: they finish from `cameras.json.new` instead of reading the file
the failure cut.

### The marks in `events/`

Each mark of an object has its family, written the same way on every line:
`"family":"pessoa"`. This replaces that text, and nothing else, in each file
that still has it:

```sh
sh -s ~/dwnvr/storage <<'EOF'
for f in "$1"/*/events/*.ndjson; do
  test -e "$f" || continue
  rm -f "$f.tmp"
  if test ! -e "$f.new"; then
    grep -a -q -E '"family":"(pessoa|veiculo)"' "$f" || continue
    sed -e 's/"family":"pessoa"/"family":"person"/' -e 's/"family":"veiculo"/"family":"vehicle"/' \
      "$f" > "$f.tmp" && mv "$f.tmp" "$f.new" || { rm -f "$f.tmp"; echo "$f: not converted"; continue; }
  fi
  cat "$f.new" > "$f" && rm "$f.new" || echo "$f: not rewritten"
done
EOF
```

The new value has the same length as the old one, so each file keeps its
size. A file that fails prints why; fix the cause (a full disk, for example)
and run the same command again. As with `cameras.json`, a converted file is
only named `.new` once it is whole, and a new run finishes from it. To check, this must print nothing:

```sh
grep -a -l -E '"family":"(pessoa|veiculo)"' ~/dwnvr/storage/*/events/*.ndjson
```

If you moved the configuration or the storage folder, use your folders in the
place of `~/dwnvr/config` and `~/dwnvr/storage`. The backups of sections 0
and 4 still have the old values, and are the way back to the previous
version.

## Start again

```sh
docker compose up -d
```

On the Cameras screen, the method and sensitivity of each camera are the ones
you had before the update. On the Users screen, each person has their photo
and last access. On the Detections screen, the marks from before the update
are there, each with its frame, its icon and its color.
