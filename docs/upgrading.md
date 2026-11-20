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
never reused or overwritten.

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
