// Package run is the headless frontend: one task in, one answer out, no terminal UI.
//
// It is a sink over the same events the TUI consumes, which is the property §6.4 exists for — a second
// frontend is a second renderer, not a second code path.
package run

import (
	"context"
	"fmt"
	"io"
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

	sink := &printer{stdout: stdout, stderr: stderr}
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
}

// Emit renders one event.
func (p *printer) Emit(e event.Event) {
	switch typed := e.(type) {
	case event.MessageCommitted:
		if typed.Message.Role != provider.RoleAssistant {
			return
		}
		_, _ = fmt.Fprintln(p.stdout, typed.Message.Content)
	case event.Notice:
		_, _ = fmt.Fprintf(p.stderr, "[%s] %s\n", typed.Code, typed.Text)
		if typed.Level == event.NoticeError {
			p.failed = true
		}
	case event.TurnDone:
		if typed.Status != event.StatusDone {
			p.failed = true
		}
	}
}
