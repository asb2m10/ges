# ges — GNU Entry System

`ges` is a lightweight job manager for Linux and macOS that mimics some of
the capabilities of IBM mainframe **JES2** (Job Entry Subsystem 2) using
plain executables and scripts.

Submit any executable or script as a **job**: it's spawned fully detached
from your terminal (it keeps running after you log out or close the shell),
and its output is spooled to disk so you can inspect it later — even after
the job has finished. Every submitted program is remembered as a reusable
**entry**, so you can resubmit it later by name.

## Installation

```sh
go build -o ges .
```

Put the resulting `ges` binary somewhere on your `$PATH`.

## Quick start

Submit a script or executable:

```sh
$ ges submit ./backup.sh
00a3f2
```

`ges` prints the **job number** (always a 6-digit hex value) it assigned.
`backup.sh` is also registered as an **entry**, so next time you can submit
it by name, from anywhere, without the path:

```sh
$ ges submit backup
00a3f3
```

Check on your jobs:

```sh
$ ges jobs
00a3f2  backup   done (pid 12345, exit 0)
00a3f3  backup   running (pid 12401)
```

Read a job's output:

```sh
$ ges job 00a3f2
```

Stop a running job:

```sh
$ ges kill 00a3f3
```

Delete a finished job's spooled output:

```sh
$ ges purge 00a3f2
```

List everything you've registered as an entry:

```sh
$ ges entry
```

## Interactive browser

Running `ges` with no arguments opens a terminal UI for browsing jobs:

- **Job list** — every job with its status and exit code.
  - `Enter` — view the job's spooled files.
  - `s` — jump straight to the job's combined output.
  - `Delete` — purge the selected job.
  - `q` / `Ctrl-C` — quit.
- **File list** — the files spooled for a job (most recent last).
  - `Enter` — open a file in the pager.
  - `Esc` — back to the job list.
- **Pager** — scroll through file contents like `less`.
  - `Esc` — back to the previous screen.

## Where things live

Everything `ges` manages is kept under `~/.local/ges/`:

- `~/.local/ges/entry/` — registered entries (symlinks to your scripts).
- `~/.local/ges/jobs/<job-number>-<entry>/` — spooled `stdout`/`stderr` and
  metadata for each job.

By default a job's `stdout` and `stderr` are combined into a single file;
pass `--use-stdout-stderr` to `ges submit` to keep them separate:

```sh
$ ges submit --use-stdout-stderr ./backup.sh
```

## Entry configuration

If a script begins a comment block with `## ges`, `ges` reads directives
from it. For example, to register the script under a custom entry name:

```sh
#!/bin/sh
## ges
### entry-name nightly-backup
```

## Learn more

See [`spec.md`](spec.md) for the full behavioral specification.
