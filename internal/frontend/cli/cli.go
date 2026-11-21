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
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/charmbracelet/x/term"

	"pretty-reasonix/internal/config"
	"pretty-reasonix/internal/frontend/run"
	"pretty-reasonix/internal/frontend/tui"
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
//
// The command name comes from the identity constant rather than a literal: a
// usage line is an owner of that string like any other, and E1-T8 asserts there
// is only one (internal/architecture/identity_test.go).
var usageLines = []string{
	"",
	"Usage:",
	fmt.Sprintf("  %-18s start an interactive session", config.BinaryName),
	fmt.Sprintf("  %-18s run a task without a terminal UI", config.BinaryName+` run "task"`),
	fmt.Sprintf("  %-18s print build information", config.BinaryName+" version"),
	"",
	"Further subcommands (setup, doctor, mcp, trust, hooks, config) are tracked by",
	"Epic E19. See docs/TECHNICAL_DESIGN.md for the design of record.",
}

// binaryVersion is the one-line version report with the program's own name in
// front of it. internal/version reports build facts and deliberately does not
// know what the binary is called (E1-T8).
func binaryVersion() string {
	return config.BinaryName + " " + version.String()
}

// Run executes the command line and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return startSession(stdout, stderr)
	}

	// The headless frontend writes as it goes, so it cannot go through the buffered path below: an answer
	// composed into a []string would arrive after the turn ended, which is the opposite of streaming.
	if args[0] == "run" {
		return run.Run(args[1:], os.Stdin, stdout, stderr)
	}
	// A print flag before the verb reroutes to run --print, because `prx -p "task"` is how every other CLI in
	// this shape is written and refusing it would be a difference nobody can see the reason for.
	if args[0] == "-p" || args[0] == "--print" {
		return run.Run(args, os.Stdin, stdout, stderr)
	}

	switch args[0] {
	case "version", "--version", "-v":
		return emit(stdout, []string{binaryVersion()}, exitOK)
	case "help", "--help", "-h":
		return emit(stdout, usageLines, exitOK)
	default:
		first := fmt.Sprintf("%s: unknown command %q", config.BinaryName, args[0])
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

// startSession opens the terminal program when there is a terminal to open it on.
//
// The usage text has promised "start an interactive session" since the first ticket, and until now it printed the
// promise instead. The condition is deliberate: a program that draws escape sequences into a pipe has produced a file
// nobody can read, so anything that is not a character device gets the usage text it always got.
func startSession(stdout, stderr io.Writer) int {
	// Both streams, because a program needs both: terminal output with piped input is a UI that can never receive a
	// key, and it would fail as a hang rather than as a message.
	if !bothAreTerminals(stdout, os.Stdin) {
		return emit(stdout, append([]string{binaryVersion()}, usageLines...), exitOK)
	}

	// Ctrl-C is handed to the program rather than intercepted: Bubble Tea installs its own handler, turns the signal
	// into an interrupt, quits cleanly and restores the terminal. A second handler here would race it, and the loser
	// would decide the exit code.
	err := tui.Run(context.Background(), tui.Options{
		In:  os.Stdin,
		Out: stdout,
		// The process boundary is where the wall clock is read; everything below takes one.
		Now: time.Now, //nolint:forbidigo // the CLI is the boundary that reads the wall clock
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s: %v\n", config.BinaryName, err)
		return exitFailure
	}
	return exitOK
}

// bothAreTerminals reports whether output and input are each a terminal.
//
// A real isatty, not os.ModeCharDevice: /dev/null is a character device, so a mode check would send `prx > /dev/null`
// — the ordinary way to silence a program, and the shape CI uses — into a terminal program with no terminal, where it
// hangs forever rather than exiting. Checking both is the same argument one stream further: terminal output with piped
// input is a UI that can never receive a key.
func bothAreTerminals(stdout io.Writer, stdin *os.File) bool {
	file, isFile := stdout.(*os.File)
	if !isFile {
		return false
	}
	outFd, outErr := fileDescriptor(file)
	inFd, inErr := fileDescriptor(stdin)
	if outErr != nil || inErr != nil {
		return false
	}
	return term.IsTerminal(outFd) && term.IsTerminal(inFd)
}

// fileDescriptor asks a file for its descriptor, which is what an isatty needs.
func fileDescriptor(file *os.File) (uintptr, error) {
	if file == nil {
		return 0, errors.New("no file")
	}
	return file.Fd(), nil
}
