# ges — GNU Entry System — Specification

## 1. Overview

`ges` is a job-encapsulating command-line tool for Linux and macOS, written in
Go, that mimics some of the capabilities of IBM mainframe **JES2** (Job Entry
Subsystem 2).

It lets a user submit an executable or script as a **job**. The job is spawned
and fully detached from the terminal session (it keeps running after the shell
exits), and its `stdout`/`stderr` are spooled to disk so they can be retrieved
later. Each submitted executable/script is remembered as a reusable **entry**.

## 2. Concepts

| Term        | Meaning                                                                                   |
|-------------|-------------------------------------------------------------------------------------------|
| **Entry**   | A registered executable/script that can be submitted by name (without the `./` path).     |
| **Job**     | A single execution of an entry. Identified by a unique, monotonically assigned job number.|
| **Spool**   | The on-disk `stdout`/`stderr` output and metadata retained for a job.                     |

## 3. Job numbering

- Job numbers are assigned from a persistent counter stored in
  `~/.local/ges/jobcounter`.
- The counter is a hexadecimal value that ranges from `0x000000` to `0xFFFFFF`
  (24-bit, ~16.7M values).
- The counter is **incremented each time a job is started**.
- When it reaches `0xFFFFFF`, it **wraps back to `0x000000`**.
- Job numbers are **always displayed as 6 hexadecimal digits** (e.g. `00a3f2`)
  so the user can immediately recognize them as job numbers.

> Note: On wrap, a new job may reuse a number belonging to an older, purged job.
> No reuse-collision handling beyond wrapping is required in this version.

## 4. Filesystem layout

The workspace root is `~/.local/ges/`.

```
~/.local/ges/
├── jobcounter                       # persistent job number counter (hex)
├── entry/                           # registered entries
│   ├── <entry-name>                 # plain entry: symlink -> real executable
│   └── <entry-name>/                # configured entry: a directory containing
│       ├── <original-name>          #   the original symlink -> real executable
│       └── spec                     #   entry + configuration metadata
└── jobs/
    └── <job-number>-<entry-name>/   # spool directory for one job
        ├── spec                     # job metadata (see §6)
        ├── gesmsgstart               # start-of-job message (see §6.1)
        ├── gesmsgend                 # end-of-job message (see §6.2)
        ├── stdout                   # captured standard output
        └── stderr                   # captured standard error
```

- `~/.local/ges/` (and its subdirectories) is created on demand if missing.
- `<job-number>` in the directory name is the 6-digit hex job number.
- `<entry-name>` is the base name of the submitted executable/script.

## 5. Entries

- When a job is submitted with `ges submit ./<executable>`, `ges` **registers an
  entry** named after the base name of the executable/script.
- The entry is stored in `~/.local/ges/entry/<entry-name>` as a **symbolic link**
  to the real (absolute) path of the executable/script.
- Once an entry exists, the same program can be submitted again **by name**,
  without the leading `./` and without any path:
  `ges submit <entry-name>`.
- If an entry with the same name already exists, submitting again reuses/updates
  that entry.

### 5.1 Entry configuration blocks

- When a **new** entry is registered from a **text file (script)**, `ges` scans
  the **first 100 lines** for script comments describing a configuration block.
- A line whose trimmed content is **`## ges`** begins the configuration block.
- After that marker, every comment line that starts with **`###`** is a
  **directive** of the form `### <key> <value>`. The block ends at the first
  line of real (non-comment) code.
- Binary executables and scripts without a `## ges` block are registered as
  plain symlink entries (§5).

**Recognized directives:**

| Directive          | Effect                                                                 |
|--------------------|------------------------------------------------------------------------|
| `entry-name <name>`| Overrides the registered entry name (instead of the script base name). |

Additional directives are stored in the entry `spec` for future use.

**Configured entries become directories.** When a configuration block is
present, `~/.local/ges/entry/<entry-name>` is created as a **directory** rather
than a plain symlink. It contains:

- the **original symbolic link** (named after the real executable) pointing to
  the real executable/script, and
- a **`spec`** file describing the entry and its configuration
  (`entry`, `original`, `target`, and each parsed directive as `key=value`).

Example script header:

```sh
## ges
### entry-name job01
```

This registers the script under the entry name `job01` regardless of the
script's own file name.

## 6. Job spec file

At **job creation time**, `ges` writes a `spec` file into the job's spool
directory: `~/.local/ges/jobs/<job-number>-<entry>/spec`.

The `spec` file records, at job creation time:

- **pid** — the process id of the spawned job.
- **btime** — the begin time (start timestamp) of the job.
- **env** — the environment used to spawn the job (one `env=<KEY>=<VALUE>`
  line per variable).

Once the job's process exits, the supervisor rewrites `spec` to additionally
record the end-of-job stats:

- **etime** — the end time (RFC 3339, UTC).
- **runtime** — wall-clock run time of the job.
- **cpu_user** / **cpu_sys** — user/system CPU time consumed by the job.
- **exit** — the process exit code.

`spec` — not `gesmsgstart`/`gesmsgend` — is the single machine-parsable
record of a job's lifecycle. It is the source of truth for reporting a job's
PID, start time, and (once present) end-of-job stats; it is used to determine
whether the job is still running (by checking whether the recorded PID is
alive) and whether it has finished (by checking whether `exit` is present).

> **Implementation note**: because a submitted job is detached and released,
> nothing in the `ges submit` process remains alive to observe how the job
> ends. To capture end-of-job data, `ges` re-execs itself as a hidden
> `__runjob__` supervisor subcommand, which starts the target, writes `spec`
> and `gesmsgstart`, blocks on the target via `Wait()`, then writes
> `gesmsgend`. This supervisor process — not the target directly — is the one
> detached from the terminal session.

### 6.1 Start-of-job message (`gesmsgstart`)

Written as soon as the job is spawned, as a **human-readable** banner page
(not intended to be parsed back — see §6 for the parsable record). Like a
JES2 job's banner page, it leads with the entry name rendered as large
ASCII-art block letters (via `github.com/dimiro1/banner`), followed by the
start time, the full (absolute) path of the executable being run, and the
environment it was spawned with.

### 6.2 End-of-job message (`gesmsgend`)

Written once the job's process exits, as a **human-readable** footer (not
intended to be parsed back — see §6 for the parsable record): the end time,
wall-clock runtime, user/system CPU time consumed, and the process exit
code.

## 7. Commands

### `ges` (no arguments)
- Launches the interactive TUI job browser (§7.1).

### `ges submit [--use-stdout-stderr] ./<executable/script>` (or `ges submit <entry-name>`)
- Registers the entry (if new) as a symlink under `~/.local/ges/entry/`.
- Assigns the next job number from `jobcounter` (incrementing/wrapping it).
- Creates the job spool directory `~/.local/ges/jobs/<job-number>-<entry>/`.
- Spawns the executable **detached from the terminal session** so it survives
  the shell exiting.
- Redirects the job's output to files in the spool directory:
  - **By default**, `stdout` and `stderr` share the **same file descriptor** and
    are written together to a single `stdout` file.
  - When **`--use-stdout-stderr`** is passed, `stdout` and `stderr` are written
    to **separate** files (`stdout` and `stderr`) in the spool directory.
- Writes the `spec` file (§6).
- **Prints the assigned job number** (6-digit hex) to the user.

### `ges jobs`
- Lists all jobs and their status.
- For each job shows the job number, entry name, and status; includes the **PID
  if the job is still running**.
- Running vs. finished is determined from the `spec` file's PID.

### `ges job <job-number>`
- Concatenates and prints the job's spooled output, in order: `gesmsgstart`,
  `stdout`, `stderr` (only present when submitted with
  `--use-stdout-stderr`), then `gesmsgend`. Missing files (e.g. `gesmsgend`
  before the job finishes) are silently skipped.

### `ges kill <job-number>`
- Stops the job if it is still running (signals the recorded PID).
- No-op (or informative message) if the job has already finished.

### `ges purge <job-number>`
- Deletes the spooled output/directory for the given job number.

### `ges entry`
- Returns the list of currently registered entries (registered jobs/entries).

### 7.1 Interactive TUI (`ges`, no arguments)

Three nested screens, built on `charmbracelet/bubbletea` + `bubbles` +
`lipgloss`:

1. **Job list** — one line per job: job number, entry name, status
   (`running (pid N)` or `done`), and return code (read from `gesmsgend`;
   `-` if the job hasn't finished yet).
   - `Enter` — drill into that job's spooled files (screen 2).
   - `s` — open the pager (screen 3) directly on the job's **unified spool**
     view: `gesmsgstart`, `stdout`, `stderr`, `gesmsgend` concatenated in that
     order, same as `ges job <job-number>` (§7).
   - `Delete` — purge the selected job (deletes its spool directory, same as
     `ges purge`), then refreshes the list.
   - `q` / `Ctrl-C` — quit.
2. **File list** — the job's spooled files (`spec`, `gesmsgstart`,
   `gesmsgend`, `stdout`, `stderr`, …), **ordered by modification time,
   ascending**, one file per line.
   - `Enter` — open that single file in the pager (screen 3).
   - `Esc` / `Backspace` — back to the job list.
   - `q` / `Ctrl-C` — quit.
3. **Pager** — scrolls the selected file's content (or the unified spool, if
   opened via `s`) like `less` (via `bubbles/viewport`), showing scroll
   percentage.
   - `Esc` / `Backspace` — back to whichever screen it was opened from (job
     list for `s`, file list for a single file).
   - `q` / `Ctrl-C` — quit.

## 8. Behavioral requirements

- **Detachment**: submitted jobs must be independent of the submitting terminal
  session (no controlling terminal; survive shell/session exit).
- **Persistence**: job numbers, entries, and spooled output persist across
  invocations of `ges` and across reboots.
- **Idempotent workspace**: any required directory under `~/.local/ges/` is
  created if it does not exist.
- **Display format**: every job number shown to the user is 6-digit lowercase
  hexadecimal.

## 9. Platform & implementation

- Language: **Go**.
- Target platforms: **Linux** and **macOS**.
- Dependencies:
  - `github.com/dimiro1/banner` — JES2-style banner rendering in
    `gesmsgstart`.
  - `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/bubbles`,
    `github.com/charmbracelet/lipgloss` — the interactive TUI (§7.1).
  - Otherwise standard library only.
- Single CLI binary named `ges` with the subcommands defined in §7.
