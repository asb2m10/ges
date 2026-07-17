package main

import (
	"bufio"
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
// target executable, writes gesmsgstart/spec at launch, waits for it to
// finish, and writes gesmsgend with the run's outcome. Splitting this out
// from ges submit lets ges submit return immediately while still capturing
// end-of-job stats (exit code, CPU time), since only a process that Wait()s
// on the job can observe those.
func runJobSupervisor(args []string) {
	if len(args) != 3 {
		os.Exit(2)
	}
	dir, target, separateFlag := args[0], args[1], args[2]
	separate := separateFlag == "1"

	num, entry, err := parseJobDir(dir)
	if err != nil {
		os.Exit(1)
	}

	stdout, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		os.Exit(1)
	}
	defer stdout.Close()

	cmd := exec.Command(target)
	cmd.Stdin = nil
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
	writeJobStartMessage(dir, num, entry, btime, target, os.Environ())

	if err := cmd.Start(); err != nil {
		os.Exit(1)
	}

	job := &Job{Number: num, Entry: entry, Dir: dir, PID: cmd.Process.Pid, BTime: btime, Env: os.Environ()}
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

// writeJobStartMessage writes gesmsgstart, a human-readable banner page
// documenting the job's start time, the full path of the executable, and the
// environment it runs with. Like a JES2 job's banner page, it leads with the
// entry name rendered as large ASCII-art block letters. This file is for
// display only (`ges job`/TUI); the machine-parsable record lives in spec.
func writeJobStartMessage(dir string, num uint32, entry string, start time.Time, path string, env []string) {
	f, err := os.Create(filepath.Join(dir, "gesmsgstart"))
	if err != nil {
		return
	}
	defer f.Close()

	fig := figure.NewFigure(strings.ToUpper(fmt.Sprintf("Job %d", num)), "alligator2", false)
    fig2 := figure.NewFigure(strings.ToUpper(entry), "alligator2", false)

	w := bufio.NewWriter(f)
	w.WriteString(fig.String())
	w.WriteString(fig2.String())
	fmt.Fprintf(w, "Started: %s\n", start.Local().Format("2006-01-02 15:04:05 MST"))
	fmt.Fprintf(w, "Command: %s\n", path)
	for _, e := range env {
		fmt.Fprintf(w, "  %s\n", e)
	}
	w.WriteString(strings.Repeat("-", 72) + "\n")
	w.Flush()
}

// writeJobEndMessage writes gesmsgend, a human-readable footer documenting
// the job's run time, CPU time used, and exit code. This file is for display
// only (`ges job`/TUI); the machine-parsable record lives in spec.
func writeJobEndMessage(dir string, runtime, cpuUser, cpuSys time.Duration, exitCode int) {
	f, err := os.Create(filepath.Join(dir, "gesmsgend"))
	if err != nil {
		return
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	w.WriteString(strings.Repeat("-", 72) + "\n")
	fmt.Fprintf(w, "Finished: %s\n", time.Now().Local().Format("2006-01-02 15:04:05 MST"))
	fmt.Fprintf(w, "Runtime:  %s\n", runtime)
	fmt.Fprintf(w, "CPU time: %s user, %s sys\n", cpuUser, cpuSys)
	fmt.Fprintf(w, "Exit code: %d\n", exitCode)
	w.Flush()
}
