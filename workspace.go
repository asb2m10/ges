package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Workspace holds the on-disk paths ges operates on, rooted at ~/.local/ges.
type Workspace struct {
	Root string
}

// NewWorkspace resolves the workspace root (~/.local/ges) and ensures the
// required directory structure exists.
func NewWorkspace() (*Workspace, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	w := &Workspace{Root: filepath.Join(home, ".local", "ges")}
	for _, dir := range []string{w.Root, w.EntryDir(), w.JobsDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	return w, nil
}

func (w *Workspace) EntryDir() string    { return filepath.Join(w.Root, "entry") }
func (w *Workspace) JobsDir() string     { return filepath.Join(w.Root, "jobs") }
func (w *Workspace) CounterFile() string { return filepath.Join(w.Root, "jobcounter") }

// EntryLink returns the symlink path for a registered entry.
func (w *Workspace) EntryLink(name string) string {
	return filepath.Join(w.EntryDir(), name)
}

// JobDir returns the spool directory for a job number + entry name.
func (w *Workspace) JobDir(jobNumber uint32, entry string) string {
	return filepath.Join(w.JobsDir(), fmt.Sprintf("%s-%s", formatJobNumber(jobNumber), entry))
}
