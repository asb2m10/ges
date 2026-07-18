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
	// Path is the full (absolute) path of the executable/script that was run,
	// resolved from the entry at submit time.
	Path string
	// HeaderLines is the number of leading lines in sysmsg that make up the
	// start-of-job header, written before the job's process is started. Any
	// lines beyond this in sysmsg are the end-of-job footer, appended once
	// the job finishes. See writeSpool.
	HeaderLines int
	// Tags are copied from the submitting entry's "tags" directive at job
	// creation time, letting jobs be purged in bulk by tag (cmdPurgeTag).
	Tags []string

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
// machine-parsable record of a job's lifecycle; sysmsg is a human-readable
// rendering for `ges job`/the TUI, not parsed back in.
func (j *Job) writeSpec() error {
	f, err := os.Create(filepath.Join(j.Dir, "spec"))
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	w.WriteString("pid=" + strconv.Itoa(j.PID) + "\n")
	w.WriteString("btime=" + j.BTime.UTC().Format(time.RFC3339) + "\n")
	w.WriteString("path=" + j.Path + "\n")
	w.WriteString("header_lines=" + strconv.Itoa(j.HeaderLines) + "\n")
	if len(j.Tags) > 0 {
		w.WriteString("tags=" + strings.Join(j.Tags, ",") + "\n")
	}
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
		case "path":
			j.Path = v
		case "header_lines":
			j.HeaderLines, _ = strconv.Atoi(v)
		case "tags":
			j.Tags = splitTags(v)
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

// writeSpool renders the job's unified spool view to w: the sysmsg header
// (its first HeaderLines lines), then the executable's captured output
// (stdout, then stderr if kept separate), then the rest of sysmsg — the
// end-of-job footer, appended once the job finishes. Missing files (e.g.
// stderr when not kept separate, or the footer before the job finishes) are
// silently skipped.
func (j *Job) writeSpool(w io.Writer) error {
	sysmsg, err := os.ReadFile(filepath.Join(j.Dir, "sysmsg"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	lines := strings.SplitAfter(string(sysmsg), "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	header, footer := lines, []string(nil)
	if j.HeaderLines < len(lines) {
		header, footer = lines[:j.HeaderLines], lines[j.HeaderLines:]
	}

	if _, err := io.WriteString(w, strings.Join(header, "")); err != nil {
		return err
	}

	for _, name := range []string{"stdout", "stderr"} {
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

	_, err = io.WriteString(w, strings.Join(footer, ""))
	return err
}

// ExitCode reports the job's recorded exit code, from the "exit=" field of
// its spec file. The second return value is false if the job hasn't finished
// yet.
func (j *Job) ExitCode() (int, bool) {
	return j.Exit, j.Finished
}

// HasTag reports whether the job was submitted with the given tag.
func (j *Job) HasTag(tag string) bool {
	for _, t := range j.Tags {
		if t == tag {
			return true
		}
	}
	return false
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
