# AGENTS.md

## What this project is

A continuous-recording NVR **designed for extremely constrained hardware**
(such as the Orange Pi Zero 3), but not built *for* any specific board. The
focus is performance, very low CPU and memory use, fast responses, and a light,
reactive, responsive UI that works well on mobile and desktop (browser).

## Principles

- **Zero cost when off.** An optional feature costs nothing to whoever leaves
  it off: no request, goroutine, buffer, file or directory created for nothing,
  and no bytes in the bundle (load it with `import()`). Look for that cost on
  purpose.
- **A request never waits on disk.** No fsync or write in the path of an HTTP
  request, and no lock the request reads held during a write. State that
  changes on every request lives in memory, and a timer writes it in the
  background (and on shutdown). It is the same disk the cameras record to.
- **Per-camera config, not assumptions.** Resolution, quota, audio and the like
  are deployment decisions: dwnvr runs on cameras and disks nobody here knows.
  Ask "should this be configurable per camera?"; the number from one
  installation is a validation example, never a default.
- **No C.** No cgo and no dependency with `import "C"` or `.c`/`.h` files (the
  binary is static and the image is `FROM scratch`). Bumping the Go version to
  keep it that way is fine (where to change it: §Ripple effects).
- **A swappable part is a contract.** Engine, statistical function, object
  detector and the like: a named Go `interface`, with a comment on what it
  promises, and a name -> constructor map, so swapping the implementation is a
  one-line change. Every tunable number (threshold, duration, budget, class
  map) lives in a `parametros.go`, never in a stray constant.
- **Readable JSON.** Keys spelled out (`instanteMs`, not `t`), even if longer.
  Exception: `index/{dia}.ndjson`, abbreviated because of its volume.
- **Keys and values in en-US**, in JSON (API, `cameras.json`, `usuarios.json`,
  `eventos/`) and in YAML (`dwnvr.yaml`, compose): `rule`, not `regra`;
  `"touch"`, not `"encosta"`. This covers enum values, not text the user types
  (an area name). The Portuguese keys and values still in place (`instanteMs`,
  `familia`, `"pessoa"`, `detectMecanismo`) are renamed in step 6 of the
  [conversion](docs/TODO/v1.0.0/conversao-en-us.md), together with the upgrade
  guide, and never on their own: renaming one breaks the stored file and
  whoever reads the API.
- **A term says what it is.** "Object detector" (the model), never a bare
  "detector": it gets confused with motion detection. Prefer the verb for what
  actually happens ("dropped", not "spared"). Don't change the meaning of a
  server concept (queue, funnel) to fit a design: propose a design that
  respects it.
- **An out-of-scope finding** doesn't widen the task or get lost in the chat:
  it becomes `docs/TODO/TODO_<subject>.md`, in the format of the ones already
  there (what was confirmed, pros, cons, how to implement, and whether it is
  worth doing now).

## Writing

- en-US is the project's working language: identifiers, file names, comments,
  tests, logs, docs, commit messages, PRs and release notes. Write it the way
  an experienced native developer would, not as a literal translation. Domain
  terms use the names in the [glossary](docs/TODO/v1.0.0/glossario.md); a new
  term goes there first.
- Three things stay in pt-BR: the chat with the maintainer; the text shown on
  screen and in emails, until i18n (Stage 5 of the
  [1.0.0 plan](docs/TODO/v1.0.0/README.md)) moves it into catalogs; and
  `README.pt-BR.md`, the Portuguese twin of `README.md`.
- Until the [conversion](docs/TODO/v1.0.0/conversao-en-us.md) is done, part of
  the repository is still in Portuguese. New code is written in en-US anyway,
  with the glossary names; existing names change in the conversion's own
  commits, not alongside an unrelated change.
- Use `-` (hyphen) instead of the em dash (U+2014) in any text (code, comments,
  docs, commits, chat), including when rewriting a line that already had one.
- The repository is public: in a file, comment, commit or PR, never the machine
  name, IP, user or installation path of the maintainer, nor the name of
  another organization's project. A camera URL in an example or test is
  `rtsp://usuario:senha@...`.

## Git

- Only run `git add`, commit, push or open a PR when the maintainer asks; "go
  ahead with the change" does not authorize the commit. The stage may be the
  reviewer's comparison point: without a request, leave the change ready in
  the working tree (a file to delete: `rm`, not `git rm`), show the `git diff`
  and the proposed message, and stop. A doc or TODO describing what the commit
  does goes with it, and the diff shown includes its full text.
- `main` does not accept merge commits. To integrate, use rebase
  (`git pull --rebase`) or fast-forward; never a `git merge` that creates a
  merge commit.
- Fixing an already published commit: on `main`, amend (one commit per change,
  and only the maintainer allows the force push, since the ruleset blocks it);
  on a PR branch that will be squashed, a new commit, no force.
- Messages follow Conventional Commits (`feat(web): ...`; the scope is the part
  touched, omitted if the change is cross-cutting). Never the
  `Claude-Session:` trailer; `Co-Authored-By` stays. A long message goes
  through `git commit -F file`.
- The commit body has two parts, separated by a `---` line:
  1. For the end user, possibly non-technical: what changed in practice, in
     everyday language, with no file name, function or technical term.
     Required.
  2. For technical people and AI agents: files, implementation decisions, what
     was left out. Optional; with nothing to say, omit part 2 and the `---`.

  ```text
  feat(web): download the frame on screen as an image

  You can now save the frame on screen as an image, without exporting
  the whole video.

  ---

  `Player.svelte` draws the `<video>` on a canvas and calls `toBlob`.
  The file name comes from `mediaURL`.
  ```

- Breaking change (someone who only updates, with `git pull` and
  `docker compose up`, loses something that worked or has to act): `!` in the
  type (`fix(compose)!:`) and the `BREAKING CHANGE:` footer, which becomes the
  "Pede ação ao atualizar" section of the release notes (`cliff.toml`).
  - The footer says what to do (the subject already says what changed),
    written for the end user, in a single paragraph. It goes at the end of the
    message, after part 2, in its own paragraph, separated from
    `Co-Authored-By` by a blank line: in the same paragraph, git stops
    recognizing `Co-Authored-By` (`BREAKING CHANGE` has a space, so to git it
    is not a trailer). Check with `git interpret-trailers --parse --no-divider`,
    which must list it.

    ```text
    (part 2)

    BREAKING CHANGE: After updating, every device goes back to the login screen once. Have the password at hand.

    Co-Authored-By: ...
    ```

  - When proposing a commit that touches any of these, say in the chat whether
    it is breaking and why, even when the answer is "no": a field or default in
    `internal/config/config.go` (rename, removal, default change); a port,
    volume, env var or service in `docker-compose.yml`; an internal path or
    `DayLayout` (are old recordings still readable?); a flag in
    `cmd/dwnvr/main.go`; a route or JSON documented in `docs/api.md`.

## UI

- A UI icon is `<Icone nome="…" tamanho={…} />`, and its drawing lives in
  `web/src/lib/iconesUi.ts`. A new icon goes there, in a 24×24 viewBox. No
  inline `<svg>` in a screen and no character (⋮ ✕ ▶) in place of an icon: the
  character changes shape on every system.
- The only icons outside the map are the detection icons drawn on canvas
  (`lib/icones.ts`) and the GitHub mark (`MarcaGitHub.svelte`).

## Lint

- Once per clone: `prek install` (prek installs with
  `uv tool install prek`). On every commit the hooks in
  `.pre-commit-config.yaml` check the files and fix what they can; if a hook
  fixes something, the commit is aborted: review it, `git add` and commit
  again.
- Before proposing a commit: `prek run --all-files`. CI
  (`.github/workflows/lint.yml`) runs the same file, without fixing anything.
- The `betterleaks` hook only looks at what is staged, so a green
  `--all-files` does not guarantee the commit. In tests, no literal with a user
  and password: build the URL with `url.UserPassword`, put the password in a
  variable and use the literals `.betterleaks.toml` allows (`senha`,
  `troque-me`; `admin` in `_test.go`).
- Never `git commit --no-verify`. A lint rule is not turned off to get through:
  fix it; if there is no way, turn it off on that line only, with the reason on
  it.

## Commands

- `make test` (`go test -race`, which needs a C compiler only for the race
  detector), `make web` (rebuilds the UI) and `make check` (what CI runs:
  test + prek). `make help` lists the rest.
- Dev server: `cd web && DWNVR_API=http://server:8080 npm run dev` (without the
  variable it points to `localhost:8080`; without a backend the screen is
  empty).
- Visual change: show it rendered (a screenshot or the dev server address)
  before committing. A 200 status and the right size don't prove the asset
  draws.

## Ripple effects

The same fact lives in several files here. Before finishing the task, find on
the left what you touched and update everything on the right. `§` = section of
the file.

| Touched | Check |
| --- | --- |
| `web/src/`, `web/index.html`, `web/public/` | `make web`, then commit all of `internal/api/dist/`, including the removal of old assets (the `interface-atualizada` hook and CI check it) |
| file in `web/src/{lib,routes,components,vendor}` | §Estrutura of `web/README.md` |
| route in `internal/api/server.go`, including entering/leaving `requireAuth` | `docs/api.md` · `web/src/lib/api.ts` (JSON types at the end; `api` for JSON, `mediaURL` for a built URL) |
| format of a JSON response or error | the screen in `web/src/routes/` |
| new `.go` in `internal/api/` | the §Estrutura do projeto tree in `README.md` |
| field in `internal/config/config.go` | `dwnvr.example.yaml` (with a comment) · `docs/configuracao.md` · §Configuração of `README.md` |
| default in `config.defaults()` | the number is repeated by hand in `dwnvr.example.yaml` · `docs/configuracao.md` · `web/src/routes/Cameras.svelte` (`audio`, `quotaMB`, `segmentSeconds`, `maxDays`, and in the `padrao` fallback: `detect`, `detectMecanismo`, `detectSensibilidade`) |
| field in `config.Camera` | `config.Resolve` and, with a default, its way back in `config.TiraDefaultsNovos` · `config.ConfereCamera` (the validation shared by the API and boot) · `Cameras.svelte` · §Política por câmera of `docs/configuracao.md` |
| range in `internal/config/parametros.go` | a field with an input on screen goes into `FaixasDaCamera`, where `Cameras.svelte` reads `min`/`max` · the Aceito column of §Política por câmera in `docs/configuracao.md` · the field's comment in `dwnvr.example.yaml` · `faixas` in `docs/api.md` |
| path or `DayLayout` in `internal/store/store.go` | `docs/arquitetura.md` · `docs/operacao.md` · §Conferir por fora da interface of `README.md` |
| field in `store.Entry` | append-only index: reading tolerates zero in an old line · `internal/retention/retention.go` · `internal/api/recordings.go` |
| variable in `internal/buildinfo` | `Makefile` (`LDFLAGS`, `BUILDARGS`) · `Dockerfile` (`ARG`, `-ldflags`) · `.github/workflows/imagens.yml` (`build-args`) · `GET /api/version` |
| dwnvr or dwnvr-detect `image:` in `docker-compose.yml` | keep `${DWNVR_VERSION:-vX.Y.Z}` on a single line: the compose step in `.github/workflows/release.yml` rewrites the default and fails if it doesn't find both lines |
| lychee version | `rev` and `entry` (the image tag) of the `lychee-docker` hook in `.pre-commit-config.yaml`, together: `rev` only brings the hook definition, and Dependabot bumps neither |
| go2rtc `image:` in `docker-compose.yml` | keep `${GO2RTC_VERSION:-X.Y.Z}` with a tested tag, never untagged (without it, `--pull always` brings any version) · §Atualizar of `README.md` · §Trocar de versão of `docs/operacao.md` |
| update command | §Atualizar of `README.md` (every release page points to it) · §Trocar de versão of `docs/operacao.md` |
| target or variable in the `Makefile` | the `## target:` comment (`make help` reads it) · §Build of `README.md` · `local.mk.example` |
| Go or Node version | `go.mod` · `.tool-versions` (local asdf) · `Dockerfile` (`golang:`/`node:`) · `ci.yml` and `lint.yml` (`setup-go`/`setup-node`) · major of `@types/node` in `web/package.json` (Node only) · §Tecnologias of `README.md` · `default_language_version` in `.pre-commit-config.yaml` (golangci-lint built with an old Go refuses to run) |
| new file or directory at the root | `.gitignore` if it is local. `.dockerignore` is an inverted allowlist (`**` + `!`): whatever the build needs requires an explicit `!`, or it disappears from the context |
| generated file, not edited by hand | `exclude` in `.pre-commit-config.yaml` (today `internal/api/dist/` and `CHANGELOG.md`): without it a hook rewrites it and CI lint fails |
| volume, port, env var or service in `docker-compose.yml` | §Experimentar em poucos minutos, §Instalar de verdade and §Detecção de movimento e de objetos of `README.md` · `docker-compose.build.yml` (if the service has its own image) · `docs/operacao.md` · `go2rtc.url` in `dwnvr.example.yaml` (depends on the service name) |
| internal path (`/etc/dwnvr`, `/storage`) | `Dockerfile` (`VOLUME`, `CMD`, `HEALTHCHECK`) · `docker-compose.yml` · `storage.root` in `dwnvr.example.yaml` · `README.md` |
| binary path in the image (`/dwnvr`) | `Dockerfile` (`ENTRYPOINT`, `HEALTHCHECK`) · `docker cp` in `.github/workflows/govulncheck.yml` · §govulncheck semanal of `docs/github.md` |
| flag in `cmd/dwnvr/main.go` | `CMD` and `HEALTHCHECK` in the `Dockerfile` (`-config`, `-healthcheck`) · `docs/operacao.md` |
| new doc in `docs/` | the `docs/README.md` index · §Documentação of `README.md`. `docs/TODO/` has no per-file index |
| `README.md` or `README.pt-BR.md` | the other one, so both say the same thing |
| new or renamed domain term | the [glossary](docs/TODO/v1.0.0/glossario.md) |
| renaming or moving a file or identifier | relative links in the `.md` files · `README.md` and `docs/` cite `.go`/`.svelte` by path · the §Estrutura do projeto tree · this file, which cites files and identifiers by name |

## Final sweep

The table covers what was foreseen; this catches the rest. Before committing:

```sh
git grep -n "old-name"   # renamed something? the old name must not be left behind
```

Also check that no personal name or name of another organization slipped in
(§Writing). Lint (§Lint) catches broken links and anchors in Markdown at commit
time.
