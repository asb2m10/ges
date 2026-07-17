# CLAUDE.md

Guidance for future sessions working in this repo.

## What this is

`ges` (GNU Entry System) is a Linux CLI in Go that submits executables/scripts
as detached, spooled jobs — a small JES2-like job manager. See **`spec.md`** for
the authoritative behavior spec.

## Docs workflow (important)

- **`spec.md`** is the source of truth for behavior. Keep it in sync with code.
- `README.md` is the original brief; `spec.md` supersedes it (and fixes its
  `~/.local/entry` typo → entries live under `~/.local/ges/entry/`).

## Code map

Single `package main`, one concern per file:

- `main.go` — CLI dispatch, usage, arg parsing (`parseSubmitArgs`).
- `workspace.go` — `~/.local/ges` layout; dirs created on demand.
- `counter.go` — 24-bit job counter (`0x000000`–`0xFFFFFF`, wraps), 6-hex-digit format.
- `job.go` — `Job` model, job `spec` read/write, PID liveness, listing/lookup, `ExitCode()`.
- `entry.go` — entry registration/resolution + `## ges` config-block parsing.
- `commands.go` — one `cmd*` method per subcommand.
- `supervisor.go` — hidden `__runjob__` supervisor: writes `gesmsgstart`/`gesmsgend`,
  runs+waits on the target so `ges submit` can return immediately while still
  capturing end-of-job stats.
- `tui.go` — interactive job browser (`ges` with no args): job list -> file
  list -> pager, via bubbletea/bubbles/lipgloss.

## Conventions

- Job numbers are always shown as 6 lowercase hex digits (`formatJobNumber`).
- Entries are either a plain symlink or, when a script has a `## ges` config
  block, a directory holding the symlink + an entry `spec`. Resolve via
  `Workspace.resolveEntry` (handles both) — don't `os.Readlink` directly.
- Jobs are spawned detached with `Setsid` + `Process.Release()`.
- Standard library only — no external deps, except:
  - `github.com/dimiro1/banner` (used in `supervisor.go` to render the
    JES2-style entry-name banner in `gesmsgstart`).
  - `github.com/charmbracelet/{bubbletea,bubbles,lipgloss}` (used in `tui.go`
    for the interactive job browser).
  Keep it that way unless asked. These deps are fine on Linux and macOS;
  Windows isn't a target.

## Build & check

```sh
go build ./... && go vet ./...
```

There are no automated tests yet; verify changes by exercising the CLI
(`./ges submit ...`, `jobs`, `job <n>`, `kill`, `purge`, `entry`) against a
throwaway script.
