// Package run is the headless frontend: one task in, one answer out, no terminal UI.
//
// It is a sink over the same events the TUI consumes, which is the property §6.4 exists for — a second
// frontend is a second renderer, not a second code path.
package run

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"pretty-reasonix/internal/agent"
	"pretty-reasonix/internal/assembly"
	"pretty-reasonix/internal/config"
	"pretty-reasonix/internal/contract/event"
	"pretty-reasonix/internal/contract/provider"
)

// Options is what the command line decided.
type Options struct {
	Task   string
	Dir    string
	Model  string
	Effort string
	Preset string
	// Print asks for the final answer and nothing else: no deltas, no tool rows, no informational notices.
	Print bool
	// PromptFile reads the task from a file, for a prompt too long to quote.
	PromptFile string
}

// Parse reads the arguments, accepting flags anywhere: before the verb, after it, or between the words of a
// task. A task is prose, so a person writing one should not have to remember where the flags go — and a parser
// that only accepted them first would swallow `--help` written inside a prompt.
//
// An argument that is not a flag is part of the task, joined with spaces, which is why a quoted task and a bare
// one behave identically.
func Parse(args []string) (Options, error) {
	var options Options
	var words []string

	for index := 0; index < len(args); index++ {
		arg := args[index]
		if !strings.HasPrefix(arg, "-") {
			words = append(words, arg)
			continue
		}
		name, value, hasValue := strings.Cut(arg, "=")
		next := func() (string, error) {
			if hasValue {
				return value, nil
			}
			if index+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", name)
			}
			index++
			return args[index], nil
		}
		var err error
		switch name {
		case "--dir", "-C":
			options.Dir, err = next()
		case "--model", "-m":
			options.Model, err = next()
		case "--effort":
			options.Effort, err = next()
		case "--preset":
			options.Preset, err = next()
		case "-p", "--print":
			// A flag with no value: what follows is the task.
			options.Print = true
		case "--prompt-file", "--file":
			options.PromptFile, err = next()
		default:
			return Options{}, fmt.Errorf("unknown flag %q", name)
		}
		if err != nil {
			return Options{}, err
		}
	}
	options.Task = strings.Join(words, " ")
	return options, nil
}

// Run executes one task and reports by exit code: zero for an answer, one for a failed turn, two for a command
// line that cannot be obeyed.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	options, err := Parse(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, config.BinaryName+" run: %v\n", err)
		return 2
	}
	task := strings.TrimSpace(options.Task)
	if task == "" && options.PromptFile != "" {
		fromFile, readErr := os.ReadFile(options.PromptFile)
		if readErr != nil {
			_, _ = fmt.Fprintf(stderr, config.BinaryName+" run: reading %s: %v\n", options.PromptFile, readErr)
			return 1
		}
		task = strings.TrimSpace(string(fromFile))
	}
	if task == "" {
		// stdin is the second way to say it, so a task can come from a pipe or a heredoc without quoting.
		piped, readErr := io.ReadAll(stdin)
		if readErr != nil {
			_, _ = fmt.Fprintf(stderr, config.BinaryName+" run: reading the task: %v\n", readErr)
			return 1
		}
		task = strings.TrimSpace(string(piped))
	}
	if task == "" {
		_, _ = fmt.Fprintln(stderr, config.BinaryName+" run: nothing to do; give a task as arguments or on stdin")
		return 2
	}

	sink := &printer{stdout: stdout, stderr: stderr, quiet: options.Print}
	built, err := assembly.Build(assembly.Options{
		Root:   options.Dir,
		Model:  options.Model,
		Effort: options.Effort,
		Preset: options.Preset,
		Sink:   sink,
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, config.BinaryName+" run: %v\n", err)
		return 1
	}
	for _, warning := range built.Warnings {
		_, _ = fmt.Fprintf(stderr, config.BinaryName+" run: %s: %s: %s\n", warning.Source, warning.Key, warning.Note)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	control, err := built.Controller.Run(ctx, agent.TurnInput{Text: task})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, config.BinaryName+" run: %v\n", err)
		return 1
	}
	<-control.Done()
	if sink.failed {
		return 1
	}
	return 0
}

// printer is the frontend: it renders an answer and reports anything that went wrong.
//
// Deliberately small. Presentation — streaming deltas, a status line, a diff card — is E14-T2's ticket; this one
// exists so the command is real and the engine has a second consumer that is not a test.
type printer struct {
	stdout io.Writer
	stderr io.Writer
	failed bool
	// streamed records that a delta is on screen without a line ending yet.
	streamed bool
	// quiet is --print: the answer and nothing else, so the output can be piped somewhere that parses it.
	quiet bool
}

// Emit renders one event.
//
// The rows are the TUI's without the screen control: deltas go out as they arrive, a tool says what it did on one
// line, and the answer is not reprinted when it is committed — a frontend that streamed a token and then printed
// the whole message again would say everything twice.
//
// stderr is reserved for what a person must act on. A notice at informational level is a row like any other, and
// putting it on the error stream would make `prx run > answer.txt` produce an empty file and a wall of noise.
func (p *printer) Emit(e event.Event) {
	switch typed := e.(type) {
	case event.TextDelta:
		if p.quiet {
			return
		}
		_, _ = fmt.Fprint(p.stdout, typed.Text)
		p.streamed = true
	case event.ReasoningDelta:
		if p.quiet {
			return
		}
		_, _ = fmt.Fprint(p.stdout, typed.Text)
		p.streamed = true
	case event.ToolResult:
		if p.quiet {
			return
		}
		p.endRow()
		outcome := "ok"
		switch {
		case typed.Err != nil:
			outcome = "failed"
		case typed.Truncated:
			outcome = "truncated"
		}
		_, _ = fmt.Fprintf(p.stdout, "[tool] %s\n", outcome)
	case event.MessageCommitted:
		if typed.Message.Role != provider.RoleAssistant {
			return
		}
		if p.streamed {
			// Already on screen, token by token.
			p.endRow()
			return
		}
		_, _ = fmt.Fprintln(p.stdout, typed.Message.Content)
	case event.Notice:
		if typed.Level == event.NoticeError {
			_, _ = fmt.Fprintf(p.stderr, "[%s] %s\n", typed.Code, typed.Text)
			p.failed = true
			return
		}
		if p.quiet {
			return
		}
		p.endRow()
		_, _ = fmt.Fprintf(p.stdout, "[%s] %s\n", typed.Code, typed.Text)
	case event.TurnDone:
		p.endRow()
		if typed.Status != event.StatusDone {
			p.failed = true
		}
	}
}

// endRow finishes a streamed line if one is open, so no row runs into the next.
func (p *printer) endRow() {
	if p.streamed {
		_, _ = fmt.Fprintln(p.stdout)
		p.streamed = false
	}
}
