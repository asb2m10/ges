package main

import (
	"fmt"
	"os"
	"strings"
)

const usage = `ges - gnu entry system

Usage:
  ges                                      launch the interactive TUI job
                                          browser (jobs -> files -> pager)
  ges submit [--use-stdout-stderr] <./executable | entry-name>
                                          submit a job, print its job number
                                          (stderr shares stdout unless the flag
                                          keeps them in separate files)
  ges jobs                                list jobs and their status
  ges job <job-number>                    print a job's stdout
  ges kill <job-number>                   stop a running job
  ges purge <job-number>                  delete a job's spooled output
  ges entry                               list registered entries
`

// isPathLike reports whether the argument refers to a filesystem path rather
// than a bare entry name.
func isPathLike(s string) bool {
	return strings.ContainsRune(s, '/')
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == runJobCmd {
		runJobSupervisor(os.Args[2:])
		return
	}

	w, err := NewWorkspace()
	if err != nil {
		fatal(err)
	}

	if len(os.Args) < 2 {
		if err := runTUI(w); err != nil {
			fatal(err)
		}
		return
	}

	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "submit":
		target, separate := parseSubmitArgs(args)
		if target == "" {
			fmt.Fprintln(os.Stderr, "usage: ges submit [--use-stdout-stderr] <./executable | entry-name>")
			os.Exit(2)
		}
		err = w.cmdSubmit(target, separate)
	case "jobs":
		err = w.cmdJobs()
	case "job":
		requireArg(args, "job <job-number>")
		err = w.cmdJob(args[0])
	case "kill":
		requireArg(args, "kill <job-number>")
		err = w.cmdKill(args[0])
	case "purge":
		requireArg(args, "purge <job-number>")
		err = w.cmdPurge(args[0])
	case "entry":
		err = w.cmdEntry()
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n%s", cmd, usage)
		os.Exit(2)
	}

	if err != nil {
		fatal(err)
	}
}

// parseSubmitArgs extracts the submit target and whether stdout/stderr should
// be kept in separate files (--use-stdout-stderr). Returns an empty target if
// none was supplied.
func parseSubmitArgs(args []string) (target string, separate bool) {
	for _, a := range args {
		switch a {
		case "--use-stdout-stderr":
			separate = true
		default:
			if target == "" {
				target = a
			}
		}
	}
	return target, separate
}

func requireArg(args []string, hint string) {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "usage: ges %s\n", hint)
		os.Exit(2)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "ges:", err)
	os.Exit(1)
}
