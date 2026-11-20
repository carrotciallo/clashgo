# ClashGo — Agent Contract

ClashGo is built to be operated by agents as well as by its GUI.
This document is the contract for that surface: the CLI, the status
schema, the control protocol, and the safety boundaries. The surface
is schema **v1**: fields are only added, never renamed or removed.

## CLI

Running the binary with a subcommand runs headless and exits; no
subcommand (or only GUI flags such as `--autostart`) launches the
GUI.

| Command | Effect |
|---|---|
| `clashgo status [--human]` | Full status (schema below) |
| `clashgo doctor [--human]` | Acceptance battery: per-check pass/fail/skip with evidence |
| `clashgo mode get` | Current capture mode |
| `clashgo mode set tun\|system\|off [--yes] [--dry-run]` | Switch capture mode |
| `clashgo route get` / `route set rule\|global\|direct [--dry-run]` | Core routing mode |
| `clashgo node list [--human]` | Picker groups and subscription pools |
| `clashgo node select <name> [--dry-run]` | Make a node the effective egress |
| `clashgo sub list [--human]` | Subscriptions (never includes URLs) |
| `clashgo sub refresh [name]` | Refresh one subscription, or all |
| `clashgo core restart` | Restart the core |
| `clashgo tunonly on\|off\|status` | Tun-only egress mode (uplink drops everything outside the core) |
| `clashgo core stop [--yes]` | Stop the core and release the capture |
| `clashgo quit [--yes]` | Quit the app |
| `clashgo logs [--since 2h] [--human]` | Structured event log (JSONL) |
| `clashgo watch` | Stream status changes and events as JSON lines |

Output rules:

- stdout is JSON by default; `--human` gives a short text form for
  the read commands. Errors are JSON on stdout:
  `{"ok":false,"error":{"code":"...","message":"..."}}`.
- Service-interrupting commands (`mode set off`, `core stop`,
  `quit`) refuse to run without `--yes`
  (error `confirmation_required`).
- `--dry-run` validates a mutation against live state and reports
  `wouldChange` without performing it.
- Mutating commands require a running instance and talk to it over
  the control socket; without one they fail with `not_running`.
  Read commands degrade: live status → disk snapshot (marked
  `stale`) → offline reconstruction from persisted settings.

Exit codes: `0` ok · `1` operation failed / doctor found failures ·
`2` usage error · `3` no running instance · `4` confirmation
required.

## Status schema v1

`clashgo status` and `<state>/status.json` (atomically rewritten by
the app every few seconds) share one shape:

- `schema`, `version`, `commit`, `time`, `stale`
- `instance`: `running`, `pid`, `startedAt`, `heartbeat`
- `core`: `running`, `pid`, `version`, `healthy`, `since`, `error`
- `capture`: `mode` (`tun`/`system`/`off`), `tunEnabled`,
  `systemProxyArmed`, `ready`, `mixedPort`, `controllerPort`
- `routing.mode`: `rule`/`global`/`direct`
- `selection`: `group`, `node`, `chain` (picker → … → egress node)
- `egress`: `ip`, `org`
- `traffic`: `upRate`, `downRate`, `upTotal`, `downTotal` (bytes)
- `busy`, `message`
- `subscriptions[]`: `name`, `nodes`, `updatedAt`, `lastError`,
  `local`, `split`

**The status never contains secrets**: no controller secret, no
subscription URLs. Do not add fields that carry either.

## Control protocol

The running instance listens on a Unix socket at
`$XDG_RUNTIME_DIR/clashgo/control.sock` (fallback:
`<state>/control.sock`), mode `0600`, same-user only. One
newline-delimited JSON request per connection:

```
{"cmd":"mode.set","args":{"mode":"tun"}}
{"ok":true,"result":{...}}
{"ok":false,"error":{"code":"invalid_mode","message":"..."}}
```

Commands: `status`, `node.list`, `sub.list`, `mode.set`,
`route.set`, `node.select`, `sub.refresh`, `core.restart`, `tunonly.on`, `tunonly.off`,
`core.stop`, `app.quit`.

Error codes: `bad_request`, `unknown_command`, `quitting`,
`invalid_mode`, `core_down`, `node_not_reachable`, `sub_not_found`,
`sub_local`, `op_failed`, `op_timeout`, `internal`.

Mutations run through the app's own serialized operation paths —
the same render/validate/rollback transactions the GUI uses — and
complete only when the target state is actually reached (capture
ready, node effective, core healthy). Every mutation is recorded
in the event log as `control_op`.

## Event log

`<state>/logs/events.jsonl`, one JSON object per line:
`{"time","kind",...}`. Kinds include `app_started`, `app_quit`,
`core_started`, `core_stopped`, `core_failed`,
`capture_switched`, `node_switched`, `sub_refreshed`,
`sub_refresh_failed`, `config_applied`, `config_rejected`,
`control_op`. Rotates to `events.jsonl.1` at ~2 MiB.

## Boundaries for agents

- **Do not edit the app's files while it runs.** `subscriptions.json`
  is owned by the running instance's registry; `master.yaml` changes
  must go through the app's configuration transactions, or they will
  race and be overwritten. Use the CLI/socket instead.
- The mihomo controller secret stays inside the app and its settings
  file. The agent surface deliberately never exposes it; health and
  egress verification live in `doctor`, not in raw controller calls.
- The socket is same-user only by design. There is no network
  control API and there must not be one without a new design pass.

## Development

- Build: `go build ./...` · Test: `go test ./...` · Format gate:
  `gofmt` must be clean; the full test baseline only grows.
- The agent surface lives in `cmd/clashgo/agent*.go`; the core
  client addition is `internal/core.Client.Version`.
