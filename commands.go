package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// cmdSubmit registers an entry and spawns a detached job. When separateStreams
// is true, stdout and stderr are written to distinct files; otherwise they
// share a single "stdout" file descriptor.
func (w *Workspace) cmdSubmit(arg string, separateStreams bool) error {
	var target, entryName string
	var tags, dds []string
	if isPathLike(arg) {
		abs, err := filepath.Abs(arg)
		if err != nil {
			return err
		}
		if _, err := os.Stat(abs); err != nil {
			return fmt.Errorf("cannot submit %q: %w", arg, err)
		}
		target = abs
		// Inspect the script for a "## ges" configuration block; an
		// "entry-name" directive overrides the registered entry name, and a
		// "tags" directive is copied onto every job submitted from it.
		cfg, err := parseEntryConfig(abs)
		if err != nil {
			return err
		}
		entryName = filepath.Base(arg)
		if cfg != nil {
			if cfg.Name != "" {
				entryName = cfg.Name
			}
			tags = cfg.Tags
			dds = cfg.DDs
		}
		if err := w.registerEntry(entryName, target, cfg); err != nil {
			return err
		}
	} else {
		// Submit an existing entry by name.
		entryName = arg
		resolved, err := w.resolveEntry(entryName)
		if err != nil {
			return err
		}
		if _, err := os.Stat(resolved); err != nil {
			return fmt.Errorf("cannot submit %q: %w", entryName, err)
		}
		target = resolved
		tags = w.resolveEntryTags(entryName)
		dds = w.resolveEntryDDs(entryName)
	}

	num, err := w.nextJobNumber()
	if err != nil {
		return err
	}
	dir := w.JobDir(num, entryName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	streamFlag := "0"
	if separateStreams {
		streamFlag = "1"
	}

	// Each "dd" directive links a file of the same name sitting alongside the
	// target script into the job's environment. Resolve the full paths now,
	// while the target's directory is at hand, and hand them to the
	// supervisor as "ddname=/full/path" pairs.
	ddPairs := make([]string, len(dds))
	for i, name := range dds {
		ddPairs[i] = name + "=" + filepath.Join(filepath.Dir(target), name)
	}

	// Re-exec ges as the detached supervisor for this job: it starts the
	// target, waits on it, and writes sysmsg/spec. Running
	// the target directly here wouldn't let us observe its end-of-job
	// exit code/CPU time without ges itself blocking on it, which would
	// defeat the "submit returns immediately" contract.
	cmd := exec.Command(self, runJobCmd, dir, target, streamFlag, strings.Join(tags, ","), strings.Join(ddPairs, ","))
	cmd.Stdin = nil
	// Detach from the controlling terminal / session.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return err
	}
	// Release the supervisor so it is fully detached.
	_ = cmd.Process.Release()

	fmt.Println(formatJobNumber(num))
	return nil
}

// cmdJobs lists jobs and their status.
func (w *Workspace) cmdJobs() error {
	jobs, err := w.listJobs()
	if err != nil {
		return err
	}
	fmt.Printf("%-8s %-20s %-8s %s\n", "JOB", "ENTRY", "STATUS", "PID")
	for _, j := range jobs {
		status, pid := "done", ""
		if j.Running() {
			status, pid = "running", fmt.Sprint(j.PID)
		}
		fmt.Printf("%-8s %-20s %-8s %s\n", formatJobNumber(j.Number), j.Entry, status, pid)
	}
	return nil
}

// cmdJob prints a job's unified spool: the sysmsg header, stdout, stderr (if
// kept separate), then the sysmsg footer.
func (w *Workspace) cmdJob(arg string) error {
	num, err := parseJobNumber(arg)
	if err != nil {
		return err
	}
	j, err := w.findJob(num)
	if err != nil {
		return err
	}
	return j.writeSpool(os.Stdout)
}

// cmdKill stops a running job.
func (w *Workspace) cmdKill(arg string) error {
	num, err := parseJobNumber(arg)
	if err != nil {
		return err
	}
	j, err := w.findJob(num)
	if err != nil {
		return err
	}
	if !j.Running() {
		fmt.Printf("job %s is not running\n", formatJobNumber(num))
		return nil
	}
	if err := syscall.Kill(j.PID, syscall.SIGTERM); err != nil {
		return err
	}
	fmt.Printf("job %s killed\n", formatJobNumber(num))
	return nil
}

// cmdPurge deletes a job's spooled output.
func (w *Workspace) cmdPurge(arg string) error {
	num, err := parseJobNumber(arg)
	if err != nil {
		return err
	}
	j, err := w.findJob(num)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(j.Dir); err != nil {
		return err
	}
	fmt.Printf("job %s purged\n", formatJobNumber(num))
	return nil
}

// cmdPurgeTag deletes the spooled output of every job carrying the given tag.
func (w *Workspace) cmdPurgeTag(tag string) error {
	jobs, err := w.listJobs()
	if err != nil {
		return err
	}
	purged := 0
	for _, j := range jobs {
		if !j.HasTag(tag) {
			continue
		}
		if err := os.RemoveAll(j.Dir); err != nil {
			return err
		}
		fmt.Printf("job %s purged\n", formatJobNumber(j.Number))
		purged++
	}
	if purged == 0 {
		fmt.Printf("no jobs tagged %q\n", tag)
	}
	return nil
}

// cmdEntry lists registered entries.
func (w *Workspace) cmdEntry() error {
	entries, err := os.ReadDir(w.EntryDir())
	if err != nil {
		return err
	}
	for _, e := range entries {
		target, _ := w.resolveEntry(e.Name())
		suffix := ""
		if e.IsDir() {
			suffix = " [configured]"
		}
		if tags := w.resolveEntryTags(e.Name()); len(tags) > 0 {
			suffix += "  tags=" + strings.Join(tags, ",")
		}
		if dds := w.resolveEntryDDs(e.Name()); len(dds) > 0 {
			suffix += "  dd=" + strings.Join(dds, ",")
		}
		fmt.Printf("%-20s -> %s%s\n", e.Name(), target, suffix)
	}
	return nil
}
