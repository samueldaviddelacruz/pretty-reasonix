package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"pretty-reasonix/internal/contract/event"
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

	// Sink is the engine's event channel. When it is set, the program drains it and folds what arrives into one update
	// per burst; when it is nil the program draws its skeleton and nothing else.
	Sink *event.ChannelSink

	// Width and Height are the starting size. Zero leaves the size unknown until the terminal reports one, which a
	// real terminal always does and a buffer never will — so a headless test sees 0x0 forever, and that is expected.
	Width, Height int
}

// Run starts the terminal program and returns when it exits.
//
// The terminal is restored by the program itself, on every path out of Run — a clean quit, an error, a cancelled
// context and a panic. This function deliberately does not call RestoreTerminal: in Bubble Tea v2 that is
// ReleaseTerminal's *inverse*, re-initialising the input reader and re-entering raw mode, so calling it after Run has
// already restored would leave the shell exactly as broken as a crash would.
func Run(ctx context.Context, options Options) error {
	if options.Now == nil {
		return ErrNoClock
	}
	if options.In == nil || options.Out == nil {
		return ErrNoTerminal
	}

	state := newModel(options)

	program := tea.NewProgram(
		state,
		tea.WithContext(ctx),
		tea.WithInput(options.In),
		tea.WithOutput(options.Out),
		// The renderer is left on: it is what emits the restore sequences, and the output writer is the only
		// terminal the program has.
		tea.WithWindowSize(options.Width, options.Height),
	)

	// The pump runs for as long as the program does. It is started before Run so that no event is missed between the
	// screen appearing and the first read, and stopped by cancelling its context on the way out.
	if options.Sink != nil {
		pumpCtx, stopPump := context.WithCancel(ctx)
		pumped := make(chan struct{})
		go func() {
			defer close(pumped)
			newPump(options.Sink, program.Send).run(pumpCtx)
		}()
		// Cancelled *and* waited for: a pump left running keeps consuming the caller's sink, and two Runs over one
		// sink would have two pumps splitting the events between them.
		defer func() {
			stopPump()
			<-pumped
		}()
	}

	if _, err := program.Run(); err != nil {
		// A cancelled context is how the caller stops the program rather than a failure of it: the CLI maps its own
		// shutdown to a clean exit, and reporting ErrProgramKilled here would make Ctrl-C look like a crash.
		if ctx.Err() != nil && errors.Is(err, tea.ErrProgramKilled) {
			return nil
		}
		return err
	}
	return nil
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
	// reducer is the transcript. The model owns it and the view reads it, which is what keeps drawing stateless.
	reducer *Reducer
	events  int
	lost    int64
}

// newModel builds the initial state.
func newModel(options Options) *model {
	return &model{
		now:     options.Now,
		inline:  options.Inline,
		width:   options.Width,
		height:  options.Height,
		started: options.Now(),
		reducer: NewReducer(),
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
	case batchMsg:
		// One render for the whole burst, which is the point of the batch existing. The events are folded here rather
		// than in the view: the model owns the transcript, and a view that mutated it would make every redraw a
		// potential state change.
		for _, e := range typed.Events {
			m.reducer.Apply(e)
		}
		m.events += len(typed.Events)
		// Stored, not added: the pump reports a total, and accumulating it would square the loss.
		m.lost = typed.Lost
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
	// Dropped is shown next to the count rather than instead of it: a renderer that said only "3 lost" would leave a
	// reader unable to tell whether anything arrived at all.
	// The transcript, then a status line. Drawing is plain text until E13 gives termrender the job; what matters here is
	// that the rows come from the reducer and nothing else.
	var body strings.Builder
	for _, row := range m.reducer.Rows() {
		fmt.Fprintf(&body, "%s\n", row.Render(m.width))
	}
	// The counts stay beside the transcript rather than replacing it: a screen that showed only "3 lost" would leave a
	// reader unable to tell whether anything arrived at all.
	fmt.Fprintf(&body, "pretty-reasonix · %s · %dx%d · %s · %d frame(s) · %d event(s) · %d lost\n",
		mode, m.width, m.height, elapsed, m.frames, m.events, m.lost)
	view := tea.NewView(body.String())
	// Alt screen is per-View, so this is where the modes actually differ: fullscreen takes the terminal over, inline
	// leaves the person's scrollback where it was.
	view.AltScreen = !m.inline
	return view
}
