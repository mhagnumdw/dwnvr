# dwnvr documentation

The [root README](../README.md) answers "what is it, what does it need, how do
I run it". The long answers live here.

## Operating

- [operations.md](operations.md) - day to day: where the files are, how to copy
  a stretch of a recording, how to read the logs, how to inspect a container
  without a shell
- [configuration.md](configuration.md) - the two configuration files, the
  per-camera policy, retention and the cost of each audio mode
- [remote-access.md](remote-access.md) - dwnvr on `https://` from anywhere and
  installable as an app, through Tailscale, without exposing the server; giving
  other people access
- [https-local-network.md](https-local-network.md) - dwnvr on `https://` on the
  home network only, without Tailscale: Caddy in front and its CA on each
  device

## Upgrading

- [upgrading.md](upgrading.md) - the upgrade guide to 1.0.0: what to rename
  before bringing the new version up

## Understanding

- [architecture.md](architecture.md) - the on-disk format: why it consumes the
  go2rtc fMP4, the NDJSON index, the init segment identified by hash
- [detection.md](detection.md) - motion and object detection: the flow from a
  frame to the mark, `dwnvr-detect`, the cost and the limits of the model
- [resilience.md](resilience.md) - the two real failure modes: power loss and a
  go2rtc that goes silent without warning
- [api.md](api.md) - HTTP endpoint reference

## Maintaining the repository

- [github.md](github.md) - what is turned on in GitHub (Dependabot, CodeQL,
  secret scanning, the ruleset on `main`), the command for each thing and what
  to do with a Dependabot PR

## Measurements and investigations

Dated documents that record how each assumption was verified. They are not kept
up to date: they stand as evidence of what was measured, and when.

- [phase0-results.md](phase0-results.md) - the feasibility tests that validated
  recording and playback before the project existed
- [phase1-results.md](phase1-results.md) - recorder, index and retention
- [phase2-results.md](phase2-results.md) - HTTP API
- [phase3-results.md](phase3-results.md) - the UI
- [phase4-results.md](phase4-results.md) - multi-arch Docker packaging
- [audio-flac.md](audio-flac.md) - the measurement of FLAC mode
- [go2rtc-h265-parameter-sets.md](go2rtc-h265-parameter-sets.md) - go2rtc
  writes fake parameter sets into the H265 container, and why that doesn't
  break playback

## Known defects

[TODO/](TODO/) - one file per defect or pending item found outside the scope of
the task where it showed up, so it is neither lost nor allowed to widen that
task.

The UI has its own documentation in [`web/README.md`](../web/README.md).
