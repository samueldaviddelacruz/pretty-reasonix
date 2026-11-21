// Package tui is the terminal frontend: a Bubble Tea program that drives the same controller the headless runner does.
//
// This ticket is the skeleton — the program, its options, and the promise that the terminal is given back. The
// conversation, the composer and the transcript rows are E10's later tickets and E11's.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"
)

var (
	// ErrNoClock reports options without a clock, which would make every timer untestable.
	ErrNoClock = errors.New("the terminal program needs a clock")
	// ErrNoTerminal reports options with nowhere to read or write.
	ErrNoTerminal = errors.New("the terminal program needs an input and an output")
)

// Options is what a caller decides before the program starts.
type Options struct {
	// Inline draws in the current screen rather than taking it over.
	//
	// Inline is for a program started inside somebody else's window — an editor's terminal, a task runner — where
	// taking the alternate screen would erase what the person was reading. Fullscreen is for the case where the
	// conversation is the whole task.
	Inline bool

	// In and Out are the terminal. A test passes buffers; the CLI passes the process streams.
	In  io.Reader
	Out io.Writer

	// Now is the clock every timer reads: the spinner, the flash, the edge scroll and the status poll.
	//
	// Required rather than defaulted. A spinner that advances on the wall clock is untestable by construction — the
	// only way to see it move is to sleep — and rules [#86]/[#87] exist so that a frontend never forces a test to.
	Now func() time.Time

	// Width and Height are the starting size, for a terminal that cannot report its own. Zero asks the program to
	// measure one.
	Width, Height int
}

// Run starts the terminal program and returns when it exits.
//
// The terminal is restored on every path out of here — a clean quit, an error, a cancelled context, and a panic —
// because a program that leaves a terminal in raw mode with the cursor hidden has broken the person's shell, and that
// is a worse outcome than the crash that caused it.
func Run(ctx context.Context, options Options) (err error) {
	if options.Now == nil {
		return ErrNoClock
	}
	if options.In == nil || options.Out == nil {
		return ErrNoTerminal
	}

	program := tea.NewProgram(
		newModel(options),
		tea.WithContext(ctx),
		tea.WithInput(options.In),
		tea.WithOutput(options.Out),
		// The renderer is left on: it is what emits the restore sequences, and the output writer is the only
		// terminal the program has.
		tea.WithWindowSize(options.Width, options.Height),
		// Signals are the CLI's business, and installing a handler here would fight the one `cmd/prx` sets up.
		tea.WithoutSignalHandler(),
	)

	// Deferred before Run rather than after it, so a panic inside the program is covered too. Bubble Tea catches
	// panics by default and restores on the way out, but this makes the promise local: whichever of the two runs
	// first, the terminal is given back, and a second restore is a no-op.
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("the terminal program panicked: %v", recovered)
		}
		if restoreErr := program.RestoreTerminal(); restoreErr != nil && err == nil {
			err = fmt.Errorf("restoring the terminal: %w", restoreErr)
		}
	}()

	_, err = program.Run()
	return err
}

// frameMsg asks the model to redraw from the clock.
//
// It exists so a test can drive every timer without sleeping: the model reads Now() when it handles this message, so
// advancing the fake clock and sending one frame moves the spinner, ages the flash and scrolls the edge exactly as
// time passing would.
type frameMsg struct{}

// model is the program's state.
//
// Deliberately empty of a conversation for now: this ticket is the program, not its contents, and a model that grew
// a transcript here would force E11 to change it twice.
type model struct {
	now           func() time.Time
	inline        bool
	width, height int

	// started is the clock reading the program began at, so elapsed time is derived rather than accumulated — an
	// accumulated timer drifts and, worse, cannot be advanced by a test.
	started time.Time
	frames  int
}

// newModel builds the initial state.
func newModel(options Options) *model {
	return &model{
		now:     options.Now,
		inline:  options.Inline,
		width:   options.Width,
		height:  options.Height,
		started: options.Now(),
	}
}

// Init is called once. Nothing is scheduled here: the ticker that sends frames is E10's spinner ticket, and it will
// read the same clock this does.
func (*model) Init() tea.Cmd { return nil }

// Update handles one message.
//
//nolint:ireturn // the return type is Bubble Tea's Model interface, which this method implements
func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := message.(type) {
	case tea.KeyPressMsg:
		// Ctrl-C is the one binding the skeleton needs: without it a program started by mistake cannot be left except
		// by killing the process, which would not restore the terminal.
		if typed.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width, m.height = typed.Width, typed.Height
	case frameMsg:
		m.frames++
	}
	return m, nil
}

// View renders the program. The skeleton draws only what proves it is alive: the mode, the size, and how long it has
// been running as the clock reports it.
func (m *model) View() tea.View {
	mode := "fullscreen"
	if m.inline {
		mode = "inline"
	}
	elapsed := m.now().Sub(m.started).Round(time.Second)
	return tea.NewView(fmt.Sprintf("pretty-reasonix · %s · %dx%d · %s · %d frame(s)\n",
		mode, m.width, m.height, elapsed, m.frames))
}
