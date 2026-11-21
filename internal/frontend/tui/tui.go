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

	// FrameInterval is how often a running turn asks for a redraw. Zero means DefaultFrameInterval.
	FrameInterval time.Duration

	// Send is what a submitted message goes to. Nil is a program that records what was typed and does nothing with it,
	// which is what makes the input path testable without a controller behind it.
	//
	// It returns an error because a send can be refused — a turn is already running — and the composer has to give the
	// words back rather than clear them. Printing to stderr instead would scribble over the frame the program owns.
	Send func(string) error

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
	// viewport is how much of it fits, and where the reader is looking.
	viewport *Viewport
	// composer is where typing lands, and stack is whatever modal has taken the keyboard.
	composer *Composer
	stack    *Stack
	// send is where a submitted message goes, or nil.
	send func(string) error
	// lastSent is what was last submitted, so a test can assert that Enter did something without a controller.
	lastSent string
	// animating is true while a turn is running: the screen has something to show that changes without an event, and a
	// static screen looks like a hung one.
	animating bool
	// interval is how often a frame is requested while animating.
	interval time.Duration
	// ticking is true while a frame is in flight, so two turns cannot arm two chains that re-arm each other.
	ticking bool
	// sendErr is why the last send was refused, if it was. Shown in the status line, because the program owns the
	// terminal and there is nowhere else for it to go.
	sendErr string
	events  int
	lost    int64
}

// newModel builds the initial state.
func newModel(options Options) *model {
	return &model{
		now:      options.Now,
		inline:   options.Inline,
		width:    options.Width,
		height:   options.Height,
		started:  options.Now(),
		reducer:  NewReducer(),
		viewport: NewViewport(max(options.Height-2, 0)),
		composer: NewComposer(CursorBar),
		stack:    &Stack{},
		send:     options.Send,
		interval: frameInterval(options.FrameInterval),
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
		// Global bindings first, before the overlay stack: Escape and Ctrl-C are the caller's, and an overlay must not be
		// able to take away the key that leaves.
		switch typed.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.stack.Pop()
		}
		// Then the overlay: a modal owns the keyboard, so nothing below it — the composer included — reacts.
		if m.stack.Swallows() {
			return m, nil
		}
		// Then the composer.
		switch ActionOf(KeyOf(typed)) {
		case ActionSend:
			m.submit()
		case ActionNewline:
			m.composer.Newline()
		case ActionNone:
			switch typed.String() {
			case "backspace":
				m.composer.Backspace()
			default:
				m.composer.Insert(typed.Text)
			}
		}
	case tea.PasteMsg:
		// A bracketed paste arrives as one message with its whole content, which is what makes folding it into a token
		// possible: by the time the keys were separate, the lines would already be in the composer.
		if !m.stack.Swallows() {
			m.composer.Paste(typed.Content)
		}
	case tea.WindowSizeMsg:
		m.width, m.height = typed.Width, typed.Height
	case frameMsg:
		m.frames++
		m.ticking = false
		// Re-armed from here rather than from the command, so the chain stops the moment a turn ends: a frame arrives, the
		// model decides whether there is another.
		return m, m.frame()
	case batchMsg:
		// One render for the whole burst, which is the point of the batch existing. The events are folded here rather
		// than in the view: the model owns the transcript, and a view that mutated it would make every redraw a
		// potential state change.
		for _, e := range typed.Events {
			// Animation state is read here, from the events as they actually arrive: every burst from the pump is wrapped,
			// so a case on a raw TurnStarted never fires and the spinner never starts. A start and a finish in one batch
			// leaves the program idle, which is why this is a loop over the burst rather than a case outside it.
			switch e.(type) {
			case event.TurnStarted:
				m.animating = true
			case event.TurnDone:
				m.animating = false
			}
			m.reducer.Apply(e)
		}
		m.events += len(typed.Events)
		// Stored, not added: the pump reports a total, and accumulating it would square the loss.
		m.lost = typed.Lost
		if m.animating {
			return m, m.frame()
		}
	}
	// Derived once, at the end of every update. The bottom region depends on the composer's height and on whether an
	// overlay has taken the screen, and syncing it at each site that changes either was wrong twice: a modal opening and
	// an overlay closing each grew the frame past the terminal until the next key.
	m.syncViewport()
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
	// The transcript goes through the viewport so the reader can scroll it; the status line is the pinned bottom row. The
	// counts stay beside the transcript rather than replacing it: a screen showing only "3 lost" would leave a reader
	// unable to tell whether anything arrived at all.
	rows := m.reducer.Rows()
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, strings.Split(row.Render(m.width), "\n")...)
	}
	m.viewport.SetLines(lines)

	// The composer and the status line are the pinned bottom; the viewport above them gets what is left. The composer is
	// hidden entirely when an overlay has taken the keyboard, which is what BottomHeight computes.
	var body strings.Builder
	for _, line := range m.viewport.Visible() {
		body.WriteString(line)
		body.WriteString("\n")
	}
	if m.stack.ComposerVisible() {
		for line := range strings.SplitSeq(m.composer.Raw(), "\n") {
			fmt.Fprintf(&body, "> %s\n", line)
		}
	}
	prefix := "pretty-reasonix"
	if m.animating {
		prefix = m.spinner() + " pretty-reasonix"
	}
	status := fmt.Sprintf("%s · %s · %dx%d · %s · %d frame(s) · %d event(s) · %d lost · %s · cursor:%s",
		prefix, mode, m.width, m.height, elapsed, m.frames, m.events, m.lost, followingLabel(m.viewport), m.composer.Shape())
	if m.sendErr != "" {
		status += " · " + m.sendErr
	}
	body.WriteString(status)
	body.WriteString("\n")
	view := tea.NewView(body.String())
	// Alt screen is per-View, so this is where the modes actually differ: fullscreen takes the terminal over, inline
	// leaves the person's scrollback where it was.
	view.AltScreen = !m.inline
	return view
}

// followingLabel says whether new output will move the screen, which is the one piece of viewport state a reader needs to
// see: a paused transcript with no indication of it looks like a stalled one.
func followingLabel(v *Viewport) string {
	if v.Following() {
		return "following"
	}
	return "paused"
}

// submit hands the composer's text to the caller and clears it.
//
// Clearing before the send rather than after: if the caller blocks, a composer still holding the message invites a second
// Enter and a duplicate question. What was sent is remembered, so a test asserts the text rather than the absence of it.
func (m *model) submit() {
	text := strings.TrimSpace(m.composer.Value())
	if text == "" {
		return
	}
	m.lastSent = text
	m.composer.Clear()
	if m.send == nil {
		return
	}
	if err := m.send(text); err != nil {
		// Put it back. A refused send happens when a turn is already running, and the second message is one the person
		// still wants — losing it to a race would be worse than the race.
		m.composer.Insert(text)
		m.sendErr = err.Error()
		return
	}
	m.sendErr = ""
}

// syncViewport gives the transcript whatever the bottom region does not need.
//
// Recomputed rather than stored: the composer grows as lines are added and shrinks when an overlay hides it, so a height
// captured once would be wrong by the second keystroke.
func (m *model) syncViewport() {
	bottom := m.stack.BottomHeight(m.composer.Height(), 1)
	m.viewport.SetHeight(max(m.height-bottom, 0))
}

// DefaultFrameInterval is how often a running turn asks for a redraw.
//
// Ten a second: fast enough that a spinner reads as motion, slow enough that a redraw costs nothing against the render
// budget. Deliberately not tied to the model's speed — a frame is asked for on a timer rather than per token, so a slow
// vendor still looks alive and a fast one does not redraw per character.
const DefaultFrameInterval = 100 * time.Millisecond

// frameInterval resolves the option, so a caller can leave it unset.
func frameInterval(interval time.Duration) time.Duration {
	if interval > 0 {
		return interval
	}
	return DefaultFrameInterval
}

// frame asks for the next redraw.
//
// tea.Tick is the only timer here, and it is a *scheduling* timer rather than a clock reading: what the frame shows is
// derived from the injected clock, so a test advances that clock and sends one frameMsg instead of waiting for this. The
// interval is an option for the same reason — a test that had to wait for the real one would be a test that sleeps.
//
// It returns nil when nothing is running, which is what stops the chain: a ticker with no turn behind it would redraw an
// unchanging screen ten times a second forever.
func (m *model) frame() tea.Cmd {
	if !m.animating || m.ticking {
		return nil
	}
	m.ticking = true
	return tea.Tick(m.interval, func(time.Time) tea.Msg { return frameMsg{} })
}

// spinner is the frame's animation: one of four cells chosen by the injected clock.
//
// Chosen from clock reading rather than a counter that increments per frame, because a counter's speed depends on how
// often the ticker fires — so the same turn would spin at different rates on a loaded machine and a test could not predict
// it. Elapsed time is the same number however the frames fall.
func (m *model) spinner() string {
	cells := []string{"⠋", "⠙", "⠹", "⠸"}
	// Clamped and normalised: a clock that reads before the start, or an interval of zero on a model not built by newModel,
	// would panic here — and this runs inside View, which is the worst place to panic while a terminal is in raw mode.
	interval := frameInterval(m.interval)
	elapsed := max(m.now().Sub(m.started), 0)
	return cells[int(elapsed/interval)%len(cells)]
}
