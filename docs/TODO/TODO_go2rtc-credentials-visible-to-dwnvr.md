# TODO: the README says dwnvr can't see the camera passwords, but it can

Found by CodeRabbit in the local review of PR #98 (22/11/2026), and confirmed
in the code.

Both READMEs (§Installing for real, step 2) say that, because each container
mounts only its own `config/` subfolder, "the RTSP URLs, with username and
password, aren't visible to dwnvr". The mount part is true: dwnvr can't open
`go2rtc.yaml` on disk. But:

- dwnvr reaches the go2rtc API over the compose network, and
  `GET /api/config` returns `go2rtc.yaml` as it is, passwords included.
- dwnvr itself calls it: `go2rtc.Client.ConfigFile`
  (`internal/go2rtc/config_file.go`), every time the Cameras screen opens, to
  find the drift between the file and the running streams (the restart
  warning). The content stays on the server and never goes to the browser.

So the separation protects against a file read through the dwnvr volume, not
against a compromised dwnvr process.

## Options

1. Fix the text in both READMEs: the separation is of files on disk; the
   go2rtc API still serves the file to whoever can reach it, dwnvr included.
   Small and honest.
2. Also narrow what dwnvr keeps: `FindDrift` only needs the stream names and
   sources to compare. It could drop the body right after parsing (it likely
   already does) and never log it; worth a check, not a change.
3. Stop reading `/api/config` and lose the restart warning. Not worth it: the
   warning solved a real confusion, and the password is still one API call
   away for anything running in the dwnvr container.

## Verdict

Do option 1 soon, since it is a wrong security claim in the first page people
read; it is a docs-only change. Option 2 is a quick read of `FindDrift`.
