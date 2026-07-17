package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxConfigLines is how many leading lines of a script we scan for a "## ges"
// configuration block.
const maxConfigLines = 100

// EntryConfig holds directives parsed from a script's "## ges" comment block.
// A directive line looks like "### <key> <value>".
type EntryConfig struct {
	// Name is the "entry-name" override, if present. When set it replaces the
	// executable/script base name as the registered entry name.
	Name string
	// Directives keeps every parsed key/value directive (including entry-name),
	// in the order they appeared.
	Directives [][2]string
}

// configured reports whether any directives were found (i.e. the entry needs a
// directory-style registration rather than a plain symlink).
func (c *EntryConfig) configured() bool { return c != nil && len(c.Directives) > 0 }

// isTextFile reports whether path looks like a text file (script) rather than a
// binary, by checking the first chunk for NUL bytes.
func isTextFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	return n > 0 && !bytes.ContainsRune(buf[:n], 0)
}

// parseEntryConfig scans the first maxConfigLines lines of a text file for a
// "## ges" marker. Once found, subsequent comment lines starting with "###" are
// parsed as "### <key> <value>" directives, until a non-comment line ends the
// block. Returns a nil config (not an error) when the file is binary or has no
// ges block.
func parseEntryConfig(path string) (*EntryConfig, error) {
	if !isTextFile(path) {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	cfg := &EntryConfig{}
	inBlock := false
	sc := bufio.NewScanner(f)
	for line := 0; line < maxConfigLines && sc.Scan(); line++ {
		trimmed := strings.TrimSpace(sc.Text())
		switch {
		case !inBlock:
			if trimmed == "## ges" || strings.HasPrefix(trimmed, "## ges ") {
				inBlock = true
			}
		case strings.HasPrefix(trimmed, "###"):
			key, value := splitDirective(trimmed)
			if key == "" {
				continue
			}
			cfg.Directives = append(cfg.Directives, [2]string{key, value})
			if key == "entry-name" {
				cfg.Name = value
			}
		case trimmed == "" || strings.HasPrefix(trimmed, "#"):
			// Blank or an ordinary comment inside the block: keep scanning.
		default:
			// First line of real code ends the configuration block.
			return cfg, sc.Err()
		}
	}
	return cfg, sc.Err()
}

// splitDirective turns "### <key> <value>" into its key and value parts.
func splitDirective(line string) (key, value string) {
	body := strings.TrimSpace(strings.TrimPrefix(line, "###"))
	if body == "" {
		return "", ""
	}
	key, value, _ = strings.Cut(body, " ")
	return strings.TrimSpace(key), strings.TrimSpace(value)
}

// registerEntry records an entry under ~/.local/ges/entry. Without directives
// the entry is a plain symlink to the target. With directives the entry becomes
// a directory holding the original symlink plus a "spec" file describing the
// entry and its configuration.
func (w *Workspace) registerEntry(name, target string, cfg *EntryConfig) error {
	path := w.EntryLink(name)
	if err := os.RemoveAll(path); err != nil {
		return err
	}

	if !cfg.configured() {
		return os.Symlink(target, path)
	}

	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	// Keep the original symbolic link, named after the real executable.
	if err := os.Symlink(target, filepath.Join(path, filepath.Base(target))); err != nil {
		return err
	}
	return writeEntrySpec(filepath.Join(path, "spec"), name, target, cfg)
}

// writeEntrySpec records entry metadata and its directives.
func writeEntrySpec(specPath, name, target string, cfg *EntryConfig) error {
	f, err := os.Create(specPath)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	fmt.Fprintf(w, "entry=%s\n", name)
	fmt.Fprintf(w, "original=%s\n", filepath.Base(target))
	fmt.Fprintf(w, "target=%s\n", target)
	for _, d := range cfg.Directives {
		fmt.Fprintf(w, "%s=%s\n", d[0], d[1])
	}
	return w.Flush()
}

// resolveEntry returns the target executable for a registered entry name,
// handling both plain-symlink and directory-style entries.
func (w *Workspace) resolveEntry(name string) (string, error) {
	path := w.EntryLink(name)
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("no such entry %q: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return os.Readlink(path)
	}
	if info.IsDir() {
		return entryDirTarget(path)
	}
	return "", fmt.Errorf("invalid entry %q", name)
}

// entryDirTarget finds the target of a directory-style entry by reading the
// symlink stored inside it.
func entryDirTarget(dir string) (string, error) {
	items, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, it := range items {
		if it.Type()&os.ModeSymlink != 0 {
			return os.Readlink(filepath.Join(dir, it.Name()))
		}
	}
	return "", errors.New("entry directory has no symlink: " + dir)
}
