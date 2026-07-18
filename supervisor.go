package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/common-nighthawk/go-figure"
)

// runJobCmd is the hidden subcommand ges re-execs itself with to become the
// detached supervisor process for a submitted job. It is never invoked
// directly by users.
const runJobCmd = "__runjob__"

// runJobSupervisor runs as the detached process for a job. It starts the
// target executable, writes the sysmsg header/spec at launch, waits for it to
// finish, and appends the sysmsg footer with the run's outcome. Splitting
// this out from ges submit lets ges submit return immediately while still
// capturing end-of-job stats (exit code, CPU time), since only a process
// that Wait()s on the job can observe those.
func runJobSupervisor(args []string) {
	if len(args) != 5 {
		os.Exit(2)
	}
	dir, target, separateFlag, tagsCSV, ddCSV := args[0], args[1], args[2], args[3], args[4]
	separate := separateFlag == "1"
	var tags []string
	if tagsCSV != "" {
		tags = splitTags(tagsCSV)
	}

	num, entry, err := parseJobDir(dir)
	if err != nil {
		os.Exit(1)
	}

	stdout, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		os.Exit(1)
	}
	defer stdout.Close()

	// GES_SPOOL_DIR lets the job locate its own spool directory (e.g. to drop
	// extra artifacts alongside stdout/stderr) without having to rediscover it.
	env := append(os.Environ(), "GES_SPOOL_DIR="+dir)

	// Each "ddname=/full/path" pair (from the entry's "dd" directives) becomes
	// its own DD_<DDNAME> environment variable pointing at the linked file.
	if ddCSV != "" {
		for _, pair := range strings.Split(ddCSV, ",") {
			name, path, ok := strings.Cut(pair, "=")
			if !ok {
				continue
			}
			env = append(env, ddEnvVar(name)+"="+path)
		}
	}

	cmd := exec.Command(target)
	cmd.Stdin = nil
	cmd.Env = env
	cmd.Stdout = stdout
	if separate {
		stderr, err := os.Create(filepath.Join(dir, "stderr"))
		if err != nil {
			os.Exit(1)
		}
		defer stderr.Close()
		cmd.Stderr = stderr
	} else {
		cmd.Stderr = stdout
	}

	btime := time.Now()
	headerLines, err := writeJobStartMessage(dir, num, entry, btime, target)
	if err != nil {
		os.Exit(1)
	}

	if err := cmd.Start(); err != nil {
		os.Exit(1)
	}

	job := &Job{Number: num, Entry: entry, Dir: dir, PID: cmd.Process.Pid, BTime: btime, Env: env, Path: target, HeaderLines: headerLines, Tags: tags}
	_ = job.writeSpec()

	_ = cmd.Wait()
	etime := time.Now()

	job.Finished = true
	job.ETime = etime
	job.Runtime = etime.Sub(btime)
	if state := cmd.ProcessState; state != nil {
		job.CPUUser = state.UserTime()
		job.CPUSys = state.SystemTime()
		job.Exit = state.ExitCode()
	} else {
		job.Exit = -1
	}
	_ = job.writeSpec()

	writeJobEndMessage(dir, job.Runtime, job.CPUUser, job.CPUSys, job.Exit)
}

// parseJobDir extracts the job number and entry name from a spool directory
// path named "<jobnumber>-<entry>".
func parseJobDir(dir string) (uint32, string, error) {
	base := filepath.Base(dir)
	sep := strings.IndexByte(base, '-')
	if sep < 0 {
		return 0, "", fmt.Errorf("not a job directory: %s", base)
	}
	num, err := parseJobNumber(base[:sep])
	if err != nil {
		return 0, "", err
	}
	return num, base[sep+1:], nil
}

// writeJobStartMessage creates sysmsg with the job's start-of-job header: a
// human-readable banner page (not intended to be parsed back — see §6 of
// docs/spec.md for the parsable record). Like a JES2 job's banner page, it leads
// with the entry name rendered as large ASCII-art block letters, followed by
// the start time and the full path of the executable. Returns the number of
// lines written, which the caller records in spec as header_lines so the
// header can later be told apart from the end-of-job footer appended to the
// same file.
func writeJobStartMessage(dir string, num uint32, entry string, start time.Time, path string) (int, error) {
	var buf bytes.Buffer

	fig := figure.NewFigure(strings.ToUpper(fmt.Sprintf("Job %d", num)), "alligator2", false)
	fig2 := figure.NewFigure(strings.ToUpper(entry), "alligator2", false)

	buf.WriteString(fig.String())
	buf.WriteString(fig2.String())
	fmt.Fprintf(&buf, "Started: %s\n", start.Local().Format("2006-01-02 15:04:05 MST"))
	fmt.Fprintf(&buf, "Command: %s\n", path)
	buf.WriteString(strings.Repeat("-", 72) + "\n")

	f, err := os.Create(filepath.Join(dir, "sysmsg"))
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := f.Write(buf.Bytes()); err != nil {
		return 0, err
	}
	return strings.Count(buf.String(), "\n"), nil
}

// writeJobEndMessage appends the end-of-job footer to sysmsg: a
// human-readable summary (not intended to be parsed back — see §6 of
// docs/spec.md for the parsable record) of the end time, wall-clock runtime,
// user/system CPU time consumed, and the process exit code.
func writeJobEndMessage(dir string, runtime, cpuUser, cpuSys time.Duration, exitCode int) {
	f, err := os.OpenFile(filepath.Join(dir, "sysmsg"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	fmt.Fprintln(f, strings.Repeat("-", 72))
	fmt.Fprintf(f, "Finished: %s\n", time.Now().Local().Format("2006-01-02 15:04:05 MST"))
	fmt.Fprintf(f, "Runtime:  %s\n", runtime)
	fmt.Fprintf(f, "CPU time: %s user, %s sys\n", cpuUser, cpuSys)
	fmt.Fprintf(f, "Exit code: %d\n", exitCode)
}
