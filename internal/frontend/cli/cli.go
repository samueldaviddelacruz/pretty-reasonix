// Package cli routes a command line to a frontend and owns the process exit
// code.
//
// It is one of only two places allowed to write to the process streams and
// decide an exit status (docs/TECHNICAL_DESIGN.md §6.2). It takes its writers
// as arguments rather than reaching for os.Stdout, so the whole surface is
// testable in memory (rule [#46]).
//
// Routing here is deliberately minimal: E19-T1 owns the full subcommand
// surface, and E10-T1 owns starting the terminal UI on a bare invocation. This
// ticket only needs the binary to run and report its version.
package cli

import (
	"fmt"
	"io"

	"pretty-reasonix/internal/version"
)

// Exit codes are defined in docs/TECHNICAL_DESIGN.md §6.13: 0 finished, 1 run
// failure, 2 usage error, 3 unverified, 4 untrusted.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// usageLines is the short help shown for a bare invocation, for `help`, and
// after an unrecognised command.
var usageLines = []string{
	"",
	"Usage:",
	"  prx                start an interactive session",
	`  prx run "task"     run a task without a terminal UI`,
	"  prx version        print build information",
	"",
	"Further subcommands (setup, doctor, mcp, trust, hooks, config) are tracked by",
	"Epic E19. See docs/TECHNICAL_DESIGN.md for the design of record.",
}

// Run executes the command line and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return emit(stdout, append([]string{version.String()}, usageLines...), exitOK)
	}

	switch args[0] {
	case "version", "--version", "-v":
		return emit(stdout, []string{version.String()}, exitOK)
	case "help", "--help", "-h":
		return emit(stdout, usageLines, exitOK)
	default:
		first := fmt.Sprintf("prx: unknown command %q", args[0])
		return emit(stderr, append([]string{first}, usageLines...), exitUsage)
	}
}

// emit writes the lines and returns code, downgrading to a failure if the write
// itself failed: output we could not produce must not be reported as success
// (rules [#53] and [#54]).
func emit(w io.Writer, lines []string, code int) int {
	if err := writeLines(w, lines); err != nil {
		return exitFailure
	}
	return code
}

// writeLines writes one line at a time and returns the first failure instead of
// discarding it.
func writeLines(w io.Writer, lines []string) error {
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return fmt.Errorf("writing output: %w", err)
		}
	}
	return nil
}
