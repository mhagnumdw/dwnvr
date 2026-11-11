# TODO: the two CodeQL alerts on the Diagnostics UID and GID

CodeQL flags `go/incorrect-integer-conversion` (high severity) in
`internal/api/diagnostics_process.go`, in `case "Uid"` and `case "Gid"`: the
number comes from `strconv.ParseInt(..., 64)` and becomes an `int` without an
upper bound check.

- It predates the en-US conversion: alerts #19 and #20 are open on `main`
  against `diagnostico_processo.go`. In PR #86 the file was only renamed, and
  CodeQL reopened the same two as #84 and #85.
- Low practical risk: the value comes from the process's own
  `/proc/self/status`, and on the image platforms (amd64 and arm64) `int` is
  64 bits. The alert is real on a 32-bit platform.
- The fix is small: read the UID and GID with `strconv.ParseUint(..., 10, 32)`
  (or check the bound before `int(n)`), without changing what Diagnostics
  shows.

It stayed out of PR #86 because that PR promises no behavior change. Agreed on
11/11/2026: fix it later, in a PR of its own, and close the four alerts
(#19, #20, #84 and #85).
