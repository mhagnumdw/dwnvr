# The GitHub repository <!-- omit in toc -->

The repository configuration that doesn't live in a file: what is turned on in
Settings, the command that turns each thing on and what to do with what it
produces. What is still missing is in
[`TODO/TODO_seguranca-o-que-sobrou.md`](TODO/TODO_seguranca-o-que-sobrou.md);
when an item there is done, it moves here.

The commands use [`gh`](https://cli.github.com), authenticated with admin
permission on the repository. They all start from:

```sh
REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner)
```

Turning on again what is already on changes nothing, so any command here can be
run again to check.

- [Checking the state](#checking-the-state)
- [Secret scanning and push protection](#secret-scanning-and-push-protection)
- [Read-only workflow token](#read-only-workflow-token)
- [Dependabot alerts and security updates](#dependabot-alerts-and-security-updates)
  - [Reviewing and accepting the Dependabot PR](#reviewing-and-accepting-the-dependabot-pr)
  - [Dependabot PR in `web/`](#dependabot-pr-in-web)
  - [Dependabot PR in `dwnvr-detect/`](#dependabot-pr-in-dwnvr-detect)
  - [An alert that doesn't close after the fix](#an-alert-that-doesnt-close-after-the-fix)
- [`dependabot.yml`](#dependabotyml)
- [SHA pinning](#sha-pinning)
- [CodeQL](#codeql)
- [Private vulnerability reporting](#private-vulnerability-reporting)
- [Ruleset on `main`](#ruleset-on-main)
- [Merging a PR without a merge commit](#merging-a-pr-without-a-merge-commit)
- [Branch deleted after merge](#branch-deleted-after-merge)
- [Weekly govulncheck](#weekly-govulncheck)
- [Weekly Trivy on the object detector image](#weekly-trivy-on-the-object-detector-image)
- [Codecov](#codecov)

## Checking the state

```sh
gh api repos/$REPO --jq '.security_and_analysis'             # secret scanning and security updates
gh api repos/$REPO/vulnerability-alerts -i | head -1         # 204 = Dependabot alerts on, 404 = off
gh api repos/$REPO/actions/permissions/workflow              # workflow token
gh api "repos/$REPO/dependabot/alerts?state=open" --jq 'length'
gh api repos/$REPO/code-scanning/default-setup --jq '.state' # configured = CodeQL on
gh api --paginate "repos/$REPO/code-scanning/alerts?state=open" --jq '.[].number' | wc -l
gh api --paginate "repos/$REPO/code-scanning/alerts?state=open&tool_name=Trivy" --jq '.[].number' | wc -l
gh api repos/$REPO/private-vulnerability-reporting
gh api repos/$REPO/rules/branches/main --jq '.[].type'       # deletion, non_fast_forward
gh api repos/$REPO --jq '.allow_merge_commit'                # false
gh api repos/$REPO --jq '.delete_branch_on_merge'            # true
```

## Secret scanning and push protection

GitHub looks for known secrets (cloud tokens, API keys) in everything already
published, and push protection rejects a `git push` that brings in a new one.
It doesn't replace the commit-time betterleaks (a hook in
[`.pre-commit-config.yaml`](../.pre-commit-config.yaml)): that one runs before
the commit exists and knows the camera password, which GitHub doesn't.

```sh
gh api -X PATCH repos/$REPO --input - <<'EOF'
{"security_and_analysis":{"secret_scanning":{"status":"enabled"},"secret_scanning_push_protection":{"status":"enabled"}}}
EOF
```

## Read-only workflow token

When a workflow doesn't declare `permissions`, the `GITHUB_TOKEN` gets
read-only access, and Actions can't approve a pull request. The workflows here
declare `permissions: {}` at the top and ask for what they need per job; this is
the default for whenever one forgets.

```sh
gh api -X PUT repos/$REPO/actions/permissions/workflow \
  -f default_workflow_permissions=read -F can_approve_pull_request_reviews=false
```

## Dependabot alerts and security updates

Turned on 2026-09-28. The alert appears when a dependency in `go.mod`,
`web/package-lock.json`, `dwnvr-detect/requirements.txt` or the workflows gets a
published vulnerability. The security update opens a PR with the fixed version,
when there is one. It doesn't depend on `dependabot.yml`, and doesn't bump
versions for any other reason.

The Docker image doesn't count: Dependabot has no alert or security update for
`docker`, only version updates. What watches the image is the
[weekly Trivy](#weekly-trivy-on-the-object-detector-image).

```sh
gh api -X PUT repos/$REPO/vulnerability-alerts       # alerts; comes first
gh api -X PUT repos/$REPO/automated-security-fixes   # security updates
```

To turn them off, the same two with `-X DELETE`, in reverse order.

The version update, one PR per month and per ecosystem, comes from
[`.github/dependabot.yml`](../.github/dependabot.yml), which says in the file
itself the reason for each rule. The Dependabot badge in the README turns red
when one of its runs fails, such as when it can't read a file.

### Reviewing and accepting the Dependabot PR

One PR at a time, in this order:

1. **Security update**, which closes an open alert.
2. **What changes the base for the others:** `github-actions`, which is the CI
   itself, and `pre-commit`, the linters. The CI of the following PRs runs with
   them.
3. **`gomod` and `docker`.**
4. **`npm`**, last, because it may ask for the build commit
   ([below](#dependabot-pr-in-web)).

A PR that needs a decision or work leaves the queue and doesn't hold back the
others. For each one:

- **Scope.** The diff only changes a version (and `go.sum` or
  `package-lock.json`). A commit from someone else on the branch changes what
  to review.
- **Replicated version.** The old version didn't survive in another file:
  `git grep` on the PR's head, outside the lockfile. Where each version lives
  is in §Ripple effects of `AGENTS.md`.
- **Action SHA.** On `github-actions`, the new SHA is the one for the tag in
  the comment: `gh api repos/<owner>/<action>/commits/<tag> --jq .sha`.
- **Changelog.** The notes of all the intermediate versions, not just the
  target one, filtered by how it is used here: a breaking change, a new default
  and a new linter rule weigh in; a feature dwnvr doesn't use doesn't. On `0.x`
  (uv, ruff), a minor breaks too.
- **Age.** On a `ghcr.io` image (today only uv), the `cooldown` doesn't apply:
  Dependabot doesn't have the publication date from there and proposes the
  day's version (in the run's log: "No verified registry publication date
  source for ghcr.io; skipping cooldown"). PR #60 brought uv 0.13.7 at 21 h old.
  The date comes from `gh api /orgs/astral-sh/packages/container/uv/versions`;
  under 7 days, the PR waits. A Docker Hub image is held by the `cooldown`.
- **Stale base.** The month's PRs start from the same commit, and `main` moves
  on. If what came in later is exercised by the bump (new code for the new
  linter, new use of the lib), the check runs on `main` before the merge, in a
  worktree of `origin/main` with the PR's file: `prek run --all-files` or
  `go test ./... -race`.
- **Merge.** Squash, tied to the reviewed head, so it fails if the branch
  changed: `gh pr merge <number> --squash --match-head-commit <sha>`. The branch
  goes away on its own ([below](#branch-deleted-after-merge)).

After the last one, the runs of the `main` push are followed by ID, with
`gh run watch <id> --exit-status`.

### Dependabot PR in `web/`

The interface build is versioned in `internal/api/dist`, and Dependabot doesn't
redo it. If the dependency goes into the bundle, the PR fails the "interface
is up to date?" step of `ci.yml`, with an annotation in the PR summary pointing
here. The warning is the check itself because Dependabot doesn't comment on the
PR, and a conversation that blocks the merge only exists with a ruleset
requiring PRs, which would end direct commits to `main`. The PR only goes in
with a commit of the rebuilt build.

If the branch is behind `main` and `main` changed `dist`, the build made there
would come out with the old interface. `main` goes in first, by merge, which
doesn't need a force push:

```sh
gh pr checkout <number>
git merge-base --is-ancestor origin/main HEAD || git merge origin/main
make web
git add internal/api/dist
git commit -m "build(web): rebuild the interface with <package> <version>"
git push
```

A development-only dependency (ESLint, svelte-check) doesn't change the bundle
and passes without this.

### Dependabot PR in `dwnvr-detect/`

`requirements.txt` comes from `requirements.in` through `uv pip compile`, with
hashes and valid for amd64 and arm64. Dependabot doesn't recompile: it swaps
the package's line, and may propose a version the rest of the file doesn't
accept. Its PR serves as a warning; the fix is to recompile:

```sh
cd dwnvr-detect
uv pip compile --universal --generate-hashes --python-version 3.12 \
  --upgrade-package <package> requirements.in -o requirements.txt
```

If the package is one of the three in `requirements.in`, the pin is changed
there first, and what the file's comment says holds: another runtime is another
number, and the delivery has to be checked again. Dependabot's PR is left to be
closed by hand.

### An alert that doesn't close after the fix

Dependabot sends the dependencies to the dependency graph in per-folder
snapshots, in the "Dependency Graph" workflow, and each snapshot is only
replaced by another with the same correlator. The `/dwnvr-detect` read descends
into `model/` and takes `model/requirements.txt` along; a push that only
touches `model/` redoes only the `/dwnvr-detect/model` snapshot. The
`/dwnvr-detect` one keeps the old version, and the alert stays open with the
file already fixed. That is what happened with `torch` on 2026-09-29.

To check, the dependency graph lists two versions of the same package in the
same file:

```sh
gh api repos/$REPO/dependency-graph/sbom \
  --jq '.sbom.packages[] | select(.name == "<package>") | .versionInfo'
```

With the fix on `main`, the alert is dismissed by hand as `inaccurate`, with
the fix's commit in the comment. An empty commit to force the read isn't worth
the clutter in the history.

## `dependabot.yml`

One PR per month, grouped by ecosystem, with a 7-day `cooldown` (zizmor
requires it: a compromised release is usually pulled before that). The commit
message comes out as `build(deps): ...`, through `commit-message`, so it doesn't
fall in the Other group of the release notes (`cliff.toml` groups by prefix).
Per ecosystem:

- **`github-actions`** and **`gomod`**: everything. `gomod` doesn't touch the
  Go version, which lives in several places (the "Go or Node version" line of
  §Ripple effects in `AGENTS.md`) and is bumped by hand.
- **`npm`** (in `/web`): everything except the TypeScript major, which
  svelte-check 4.7.6 doesn't accept yet (PR #8 failed at `npm ci`), and the
  `@types/node` major, which is the running Node's and goes up together with it
  (PR #57 proposed 26 with Node at 24).
- **`pip`** (in `/dwnvr-detect`): no version update
  (`open-pull-requests-limit: 0`, which keeps the security update). The
  versions in `requirements.in` are the ones that exported the `.onnx` and
  measured the delivery, and changing one requires checking again.
- **`docker`** (both Dockerfiles): `python:3.12.12-slim-trixie` only on the
  patch, because `requirements.txt` is compiled for 3.12. `golang:1.27-alpine`
  and `node:24-alpine` also only on the patch: their tag already floats, and
  changing version means changing it in the other places too. `ghcr.io/astral-sh/uv`,
  everything.
- **`pre-commit`**: the `rev`s in `.pre-commit-config.yaml`, except lychee.
  Dependabot only strips the leading `v` from the `rev`, and with
  `lychee-v0.24.2` it knocks the run over; and the lychee that runs is the
  `entry`'s image, not the `rev`.
- **`docker-compose`**: out. Dependabot doesn't read an image with
  `${VAR:-...}` in the tag, so go2rtc is bumped by hand.

Dependabot only gives security updates for `github-actions`, `gomod`, `npm` and
`pip`. For `docker` and `pre-commit` it only bumps versions, and depends on this
file.

## SHA pinning

Every action in the workflows goes by SHA, with the version in a comment
(`@<sha>  # v5.1.0`), and Dependabot updates both together. A tag can be moved
to other code; the SHA can't. The Docker actions receive the token with
`packages: write` in `images.yml`, and were the reason to drop the exception
that `.github/zizmor.yml` opened for `actions/*` and `docker/*`.

The repository's `sha_pinning_required` option refuses to run a workflow with
an action not pinned by SHA, and applies to all of them. Turn it on after
`main` is already pinned:

```sh
gh api -X PUT repos/$REPO/actions/permissions -F enabled=true -f allowed_actions=all -F sha_pinning_required=true
gh api repos/$REPO/actions/permissions --jq '.sha_pinning_required'   # true
```

## CodeQL

Turned on 2026-09-28, in default setup, which needs no workflow in the
repository: GitHub picks the languages (actions, go, javascript-typescript and
python) and runs the analysis on every push to `main`, on every PR and once a
week. It is data-flow analysis, such as request input reaching a file path or a
command, which the commit linters don't do. The finding goes to the Security
tab, under Code scanning, and doesn't block a merge or a push.

```sh
gh api -X PATCH repos/$REPO/code-scanning/default-setup -f state=configured
```

To turn it off, the same with `-f state=not-configured`.

## Private vulnerability reporting

Turned on 2026-09-28. It gives whoever finds a flaw a private form in the
Security tab, instead of a public issue; [`SECURITY.md`](../SECURITY.md) points
to it.

```sh
gh api -X PUT repos/$REPO/private-vulnerability-reporting
```

To turn it off, the same with `-X DELETE`.

## Ruleset on `main`

Created 2026-09-28. It refuses deleting `main` and refuses a push that rewrites
its history (`git push --force`). It requires neither a PR nor a check: direct
commits and a plain push continue, including the release's, which pushes the
version commit together with the tag.

```sh
gh api -X POST repos/$REPO/rulesets --input - <<'EOF'
{"name":"main","target":"branch","enforcement":"active",
 "conditions":{"ref_name":{"include":["~DEFAULT_BRANCH"],"exclude":[]}},
 "rules":[{"type":"deletion"},{"type":"non_fast_forward"}]}
EOF
```

The `POST` creates a new ruleset every time it runs. To change the existing
one, the id comes from `gh api repos/$REPO/rulesets` and goes in
`gh api -X PUT repos/$REPO/rulesets/<id>`, with the same JSON; to delete it, in
`-X DELETE`.

## Merging a PR without a merge commit

Turned off 2026-10-01. The PR merge button offers only squash and rebase:
AGENTS.md says `main` doesn't accept merge commits, and "Allow merge commits"
being on left the option one click away. GitHub requires at least one of the
three to be on.

```sh
gh repo edit $REPO --enable-merge-commit=false
```

## Branch deleted after merge

Turned on 2026-10-28. With the PR accepted, GitHub deletes its branch in the
repository, and the branch list is left with only what is still open. The
deleted branch comes back through the PR's "Restore branch" button. It doesn't
apply to `main`, which the ruleset protects, nor to the branch of a PR coming
from a fork. The local copy stays, and whoever created it deletes it.

```sh
gh repo edit $REPO --delete-branch-on-merge
```

## Weekly govulncheck

[`.github/workflows/govulncheck.yml`](../.github/workflows/govulncheck.yml)
runs every Monday on the binary of the `latest` image, which is what runs in
people's homes. It fails when the binary has a known flaw in code dwnvr calls,
from the Go stdlib or a dependency, and GitHub sends an e-mail. The file's
header says why it is on the binary and not on the source.

```sh
gh workflow run govulncheck.yml   # run now, without waiting for Monday
```

On a public repository, GitHub turns off the scheduled workflow after 60 days
without a commit; `gh workflow enable govulncheck.yml` turns it back on.

The same test locally, without GitHub:

```sh
go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
docker pull -q ghcr.io/$REPO:latest
id=$(docker create ghcr.io/$REPO:latest)
docker cp -q "$id:/dwnvr" ./dwnvr && docker rm "$id"
"$(go env GOPATH)/bin/govulncheck" -mode=binary ./dwnvr
```

## Weekly Trivy on the object detector image

[`.github/workflows/trivy.yml`](../.github/workflows/trivy.yml) runs every
Monday on the `latest` image of `dwnvr-detect`, on amd64 and on arm64. It flags
HIGH or CRITICAL flaws that already have a fix, in the base's Debian packages
and in the Python packages. The finding goes to the Security tab, under Code
scanning (Trivy tool, one category per architecture), and the job fails, which
sends an e-mail. The file's header says why only this image.

The fix is almost always the base: bump the `python:3.12.X-slim-trixie` tag on
the two `FROM` lines of `dwnvr-detect/Dockerfile` and do a release. The tag of
a Python patch stops being rebuilt when the next one comes out, and the image
that uses it is left with that day's Debian packages. The alert closes on its
own on the first run after the release.

```sh
gh workflow run trivy.yml   # run now, without waiting for Monday
```

The same test locally, without GitHub, with the
[Trivy release binary](https://github.com/aquasecurity/trivy/releases):

```sh
trivy image --image-src remote --platform linux/arm64 --scanners vuln \
  --ignore-unfixed --severity HIGH,CRITICAL ghcr.io/$REPO-detect:latest
```

## Codecov

The README's coverage badge comes from [Codecov](https://app.codecov.io/gh/mhagnumdw/dwnvr),
which receives the report from
[`.github/workflows/coverage.yml`](../.github/workflows/coverage.yml) on
every push to `main` and on every pull request, except Dependabot's. What it
does with the report is in [`.github/codecov.yml`](../.github/codecov.yml).

There is no token or secret: the upload authenticates through OIDC, and Codecov
checks that the run is from this repository. The first upload activates the
repository on Codecov; if it is rejected, sign in with GitHub at
app.codecov.io and enable the repository there.

The PR comment and the status with the number on each commit come from the
Codecov app, installed 2026-10-01 at <https://github.com/apps/codecov>, with
access only to dwnvr (Only select repositories). It asks for write access to
statuses, checks and pull requests, which is how it comments. Without the app,
the upload and the badge continue, and only the comment and the status go away.
The access is reviewed at <https://github.com/settings/installations>.

```sh
curl -s https://api.codecov.io/api/v2/github/mhagnumdw/repos/dwnvr/ | jq '{activated, totals: .totals.coverage}'
gh workflow run coverage.yml   # send again, without waiting for a push
```

If Codecov goes down, `coverage` goes red and the badge stays at the last
number; CI, the images and the release don't depend on it.
