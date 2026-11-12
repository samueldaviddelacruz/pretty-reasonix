package run_test

import (
	"bytes"
	"strings"
	"testing"

	"pretty-reasonix/internal/frontend/run"
)

// TestFlagsAreAcceptedAnywhere is E14-T1's criterion for the parser. A task is prose, so a person should not have
// to remember where the flags go — and with a quoted task, "before or after" is a difference nobody can see.
func TestFlagsAreAcceptedAnywhere(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		args []string
		want run.Options
	}{
		"flags first": {
			args: []string{"--model", "m1", "--effort", "high", "fix the build"},
			want: run.Options{Model: "m1", Effort: "high", Task: "fix the build"},
		},
		"flags last": {
			args: []string{"fix the build", "--model", "m1", "--effort", "high"},
			want: run.Options{Model: "m1", Effort: "high", Task: "fix the build"},
		},
		"flags between the words": {
			args: []string{"fix", "--model", "m1", "the", "--effort", "high", "build"},
			want: run.Options{Model: "m1", Effort: "high", Task: "fix the build"},
		},
		"with an equals sign": {
			args: []string{"--model=m1", "--preset=delivery", "do it"},
			want: run.Options{Model: "m1", Preset: "delivery", Task: "do it"},
		},
		"a short form": {
			args: []string{"-C", "/tmp", "-m", "m1", "do it"},
			want: run.Options{Dir: "/tmp", Model: "m1", Task: "do it"},
		},
		"no flags at all": {
			args: []string{"just a task"},
			want: run.Options{Task: "just a task"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := run.Parse(tc.args)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got != tc.want {
				t.Errorf("parsed %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestAFlagWithoutAValueIsAUsageError, because silently treating the next word as the value would eat the task.
func TestAFlagWithoutAValueIsAUsageError(t *testing.T) {
	t.Parallel()
	if _, err := run.Parse([]string{"--model"}); err == nil {
		t.Fatal("a flag with no value was accepted")
	}
	if _, err := run.Parse([]string{"do it", "--nonsense"}); err == nil {
		t.Fatal("an unknown flag was accepted")
	}
}

// TestRunReportsAUsageErrorInTwoWords: nothing to do, and an unknown flag, are both exit 2 rather than 1 — the
// difference between a command line that cannot be obeyed and a run that failed.
func TestRunReportsUsageErrors(t *testing.T) {
	t.Parallel()
	cases := map[string][]string{
		"nothing to do": {},
		"empty task":    {"   "},
		"unknown flag":  {"--nope", "task"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			if code := run.Run(args, strings.NewReader(""), &stdout, &stderr); code != 2 {
				t.Errorf("exit code %d, want 2", code)
			}
			if stderr.Len() == 0 {
				t.Error("a usage error said nothing")
			}
		})
	}
}

// TestTheTaskCanComeFromStdin: a pipe or a heredoc is the second way to say it, and neither should need quoting.
//
// The run stops at the provider — there is no model configured in a test — but the point is that stdin reached
// the point where a task is used, which is what the exit code shows: 1 from wire failure, not 2 from "nothing to
// do".
func TestTheTaskCanComeFromStdin(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code := run.Run(nil, strings.NewReader("a task from a pipe"), &stdout, &stderr)
	if code == 2 {
		t.Fatalf("stdin was not read as a task: %s", stderr.String())
	}
}

// TestPrintAndPromptFileAreParsed: the flag has no value, and the file is a third source for the task.
func TestPrintAndPromptFileAreParsed(t *testing.T) {
	t.Parallel()
	withPrint, err := run.Parse([]string{"-p", "do the thing"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !withPrint.Print || withPrint.Task != "do the thing" {
		t.Errorf("parsed %+v, want print mode with the task after the flag", withPrint)
	}

	fromFile, err := run.Parse([]string{"--prompt-file", "/tmp/prompt.md"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if fromFile.PromptFile != "/tmp/prompt.md" {
		t.Errorf("parsed %+v, want the prompt file", fromFile)
	}
}
