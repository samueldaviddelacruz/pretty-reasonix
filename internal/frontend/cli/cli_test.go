package cli_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"pretty-reasonix/internal/config"
	"pretty-reasonix/internal/frontend/cli"
)

// TestExitCodesAndOutput covers the whole command-line surface as it stands: which stream each
// path writes to, and what a shell script sees. It exists because nothing else would fail if one of
// these codes changed, and scripts branch on them.
func TestExitCodesAndOutput(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		args     []string
		wantCode int
		stdout   string
		stderr   string
	}{
		"version": {
			args:     []string{"version"},
			wantCode: 0,
			stdout:   config.BinaryName + " ",
		},
		"no arguments shows usage": {
			wantCode: 0,
			stdout:   "Usage:",
		},
		"help shows usage": {
			args:     []string{"help"},
			wantCode: 0,
			stdout:   "Usage:",
		},
		"an unknown command": {
			args:     []string{"bogus"},
			wantCode: 2,
			stderr:   `unknown command "bogus"`,
		},
		"a flag nobody has": {
			args:     []string{"--nope"},
			wantCode: 2,
			stderr:   "unknown command",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer

			if code := cli.Run(tc.args, &stdout, &stderr); code != tc.wantCode {
				t.Errorf("exit code %d, want %d", code, tc.wantCode)
			}
			if !strings.Contains(stdout.String(), tc.stdout) {
				t.Errorf("stdout is %q, want it to contain %q", stdout.String(), tc.stdout)
			}
			if !strings.Contains(stderr.String(), tc.stderr) {
				t.Errorf("stderr is %q, want it to contain %q", stderr.String(), tc.stderr)
			}
			// A failure must not also write to stdout, or a script capturing stdout gets a message
			// where it expected data.
			if tc.wantCode != 0 && stdout.Len() > 0 {
				t.Errorf("a failing command wrote %q to stdout", stdout.String())
			}
			if tc.wantCode == 0 && stderr.Len() > 0 {
				t.Errorf("a succeeding command wrote %q to stderr", stderr.String())
			}
		})
	}
}

// TestAFailedWriteIsNotReportedAsSuccess: output we could not produce must not exit zero, which is
// the one behaviour here that no ordinary terminal run would ever show.
func TestAFailedWriteIsNotReportedAsSuccess(t *testing.T) {
	t.Parallel()
	code := cli.Run([]string{"version"}, failingWriter{}, &bytes.Buffer{})
	if code == 0 {
		t.Error("a command whose output could not be written exited zero")
	}
}

// failingWriter stands in for a closed terminal or a full pipe.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("the terminal is gone")
}

// TestABareInvocationWithoutATerminalStillPrintsUsage is E10-T9's second clause, and the one a headless test can assert:
// escape sequences written into a pipe produce a file nobody can read, so the usage text stays.
func TestABareInvocationWithoutATerminalStillPrintsUsage(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if code := cli.Run(nil, &stdout, &stderr); code != 0 {
		t.Errorf("a bare invocation exited %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Errorf("a bare invocation on a buffer printed %q, want usage", stdout.String())
	}
	// The line that has promised a session since E1 is still there, and now describes something that exists.
	if !strings.Contains(stdout.String(), "start an interactive session") {
		t.Error("the usage text no longer mentions the interactive session")
	}
}
