package main

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Job describes a single execution, backed by its spool directory.
type Job struct {
	Number uint32
	Entry  string
	Dir    string
	PID    int
	BTime  time.Time
	Env    []string

	// End-of-job fields, populated once the job has finished (see
	// writeJobEndMessage/writeSpec). Finished is false until then.
	Finished bool
	ETime    time.Time
	Runtime  time.Duration
	CPUUser  time.Duration
	CPUSys   time.Duration
	Exit     int
}

// writeSpec persists the job metadata (PID, btime, environment, and, once
// available, end-of-job stats) to <dir>/spec. This is the single
// machine-parsable record of a job's lifecycle; gesmsgstart/gesmsgend are
// human-readable renderings for `ges job`/the TUI, not parsed back in.
func (j *Job) writeSpec() error {
	f, err := os.Create(filepath.Join(j.Dir, "spec"))
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	w.WriteString("pid=" + strconv.Itoa(j.PID) + "\n")
	w.WriteString("btime=" + j.BTime.UTC().Format(time.RFC3339) + "\n")
	for _, e := range j.Env {
		w.WriteString("env=" + e + "\n")
	}
	if j.Finished {
		w.WriteString("etime=" + j.ETime.UTC().Format(time.RFC3339) + "\n")
		w.WriteString("runtime=" + j.Runtime.String() + "\n")
		w.WriteString("cpu_user=" + j.CPUUser.String() + "\n")
		w.WriteString("cpu_sys=" + j.CPUSys.String() + "\n")
		w.WriteString("exit=" + strconv.Itoa(j.Exit) + "\n")
	}
	return w.Flush()
}

// loadJob reads a job spool directory (named "<jobnumber>-<entry>") into a Job.
func loadJob(dir string) (*Job, error) {
	base := filepath.Base(dir)
	sep := strings.IndexByte(base, '-')
	if sep < 0 {
		return nil, errors.New("not a job directory: " + base)
	}
	num, err := parseJobNumber(base[:sep])
	if err != nil {
		return nil, err
	}
	j := &Job{Number: num, Entry: base[sep+1:], Dir: dir}

	data, err := os.ReadFile(filepath.Join(dir, "spec"))
	if err != nil {
		return j, nil // spec may be missing; return what we know
	}
	for _, line := range strings.Split(string(data), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "pid":
			j.PID, _ = strconv.Atoi(v)
		case "btime":
			j.BTime, _ = time.Parse(time.RFC3339, v)
		case "env":
			j.Env = append(j.Env, v)
		case "etime":
			j.ETime, _ = time.Parse(time.RFC3339, v)
			j.Finished = true
		case "runtime":
			j.Runtime, _ = time.ParseDuration(v)
		case "cpu_user":
			j.CPUUser, _ = time.ParseDuration(v)
		case "cpu_sys":
			j.CPUSys, _ = time.ParseDuration(v)
		case "exit":
			j.Exit, _ = strconv.Atoi(v)
		}
	}
	return j, nil
}

// spoolFileOrder is the order in which a job's spooled files are presented as
// a unified view (`ges job <n>` and the TUI's unified spool view).
var spoolFileOrder = []string{"gesmsgstart", "stdout", "stderr", "gesmsgend"}

// writeSpool concatenates the job's spooled files (see spoolFileOrder) to w,
// silently skipping any that don't exist (e.g. stderr when not kept
// separate, or gesmsgend before the job finishes).
func (j *Job) writeSpool(w io.Writer) error {
	for _, name := range spoolFileOrder {
		f, err := os.Open(filepath.Join(j.Dir, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		_, err = io.Copy(w, f)
		f.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// ExitCode reports the job's recorded exit code, from the "exit=" field of
// its spec file. The second return value is false if the job hasn't finished
// yet.
func (j *Job) ExitCode() (int, bool) {
	return j.Exit, j.Finished
}

// Running reports whether the job's recorded PID is still alive.
func (j *Job) Running() bool {
	if j.PID <= 0 {
		return false
	}
	// Signal 0 performs error checking without actually sending a signal.
	err := syscall.Kill(j.PID, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// listJobs returns all jobs in the workspace, sorted by job number.
func (w *Workspace) listJobs() ([]*Job, error) {
	entries, err := os.ReadDir(w.JobsDir())
	if err != nil {
		return nil, err
	}
	var jobs []*Job
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		j, err := loadJob(filepath.Join(w.JobsDir(), e.Name()))
		if err != nil {
			continue
		}
		jobs = append(jobs, j)
	}
	sort.Slice(jobs, func(a, b int) bool { return jobs[a].Number < jobs[b].Number })
	return jobs, nil
}

// findJob locates a job spool directory by job number.
func (w *Workspace) findJob(num uint32) (*Job, error) {
	entries, err := os.ReadDir(w.JobsDir())
	if err != nil {
		return nil, err
	}
	prefix := formatJobNumber(num) + "-"
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix) {
			return loadJob(filepath.Join(w.JobsDir(), e.Name()))
		}
	}
	return nil, errors.New("job not found: " + formatJobNumber(num))
}
