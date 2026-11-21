package tui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// CursorShape is how the composer draws its insertion point, per `[ui] cursor_shape`.
type CursorShape int

const (
	// CursorBar is a thin vertical line, and the default because it occludes the least.
	CursorBar CursorShape = iota
	// CursorBlock covers the character under it, which some terminals render more reliably.
	CursorBlock
	// CursorUnderline sits below the line.
	CursorUnderline
)

// String names a shape for a test failure.
func (c CursorShape) String() string {
	switch c {
	case CursorBar:
		return "bar"
	case CursorBlock:
		return "block"
	case CursorUnderline:
		return "underline"
	default:
		return "cursor shape(" + strconv.Itoa(int(c)) + ")"
	}
}

// ParseCursorShape reads the configured name, falling back to the bar.
//
// Falling back rather than failing: an unrecognised cursor shape is a cosmetic disagreement with a config file, and
// refusing to start a terminal over one would be a worse outcome than drawing the wrong character.
func ParseCursorShape(name string) CursorShape {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "block":
		return CursorBlock
	case "underline":
		return CursorUnderline
	default:
		return CursorBar
	}
}

// MaxComposerRows is the tallest the composer grows before it scrolls its own content.
const MaxComposerRows = 8

// PasteToken is what a pasted block becomes: one editable placeholder rather than its lines.
//
// A fifty-line paste inserted literally turns the composer into a wall and makes the question impossible to find inside
// it. The token keeps the composer a sentence long, and the block is substituted back on send — so what reaches the model
// is exactly what was pasted, and what the person edits is a word.
type PasteToken struct {
	// Marker is what appears in the text, e.g. "[pasted #1, 50 lines]".
	Marker string
	// Text is the block itself.
	Text string
}

// Composer is the input box: its text, where the insertion point is, and the rules for keys and pastes.
//
// It is policy rather than a text editor. There is no word wrap, no selection, no undo — those belong to a widget, and
// DEPENDENCIES.md warns off re-implementing one. What lives here is what no widget gives you: which key sends and which
// inserts a newline, how tall the box may grow, what a paste becomes, and how a cursor is drawn. Those are this
// application's decisions, and they are the ones a test can drive without a terminal.
type Composer struct {
	lines  []string
	row    int
	col    int
	pastes []PasteToken
	shape  CursorShape
	rows   int
}

// NewComposer makes an empty composer with room for rows lines of text before it scrolls.
func NewComposer(shape CursorShape) *Composer {
	return &Composer{lines: []string{""}, shape: shape, rows: MaxComposerRows}
}

// Value is the text to send, with every paste token replaced by what it stands for.
func (c *Composer) Value() string {
	joined := strings.Join(c.lines, "\n")
	for _, paste := range c.pastes {
		joined = strings.ReplaceAll(joined, paste.Marker, paste.Text)
	}
	return joined
}

// Raw is the text as displayed, tokens and all. It is what a test asserts on, and what a redraw shows.
func (c *Composer) Raw() string { return strings.Join(c.lines, "\n") }

// Empty reports whether there is anything to send. A composer holding only a paste token is not empty.
func (c *Composer) Empty() bool { return strings.TrimSpace(c.Value()) == "" }

// Clear empties the composer, including its pastes, after a send.
func (c *Composer) Clear() {
	c.lines, c.row, c.col, c.pastes = []string{""}, 0, 0, nil
}

// Height is how many rows the composer occupies: one per line, capped.
//
// The cap is what keeps a long question from pushing the transcript off the screen. Beyond it the composer scrolls its
// own content, which is a decision the widget side makes and the layout only needs the number for.
func (c *Composer) Height() int { return min(len(c.lines), c.rows) }

// CursorOffset is the insertion point: a row, and a byte offset within that row.
//
// A byte offset rather than a display column, and named for it: converting to a column needs the rendered width of the
// runes before the cursor, which is termrender's job. Returning a number called "column" that was really a byte offset
// would misplace a cursor after any multi-byte character.
func (c *Composer) CursorOffset() (int, int) { return c.row, c.col }

// Shape is the configured cursor shape.
func (c *Composer) Shape() CursorShape { return c.shape }

// Insert adds text at the insertion point, one line at a time.
func (c *Composer) Insert(text string) {
	for index, part := range strings.Split(text, "\n") {
		if index > 0 {
			c.Newline()
		}
		c.insertPart(part)
	}
}

// insertPart appends one line's worth of text at the column.
func (c *Composer) insertPart(part string) {
	if part == "" {
		return
	}
	line := c.lines[c.row]
	head, tail := line[:c.col], line[c.col:]
	c.lines[c.row] = head + part + tail
	c.col += len(part)
}

// Newline splits the current line at the insertion point.
func (c *Composer) Newline() {
	line := c.lines[c.row]
	head, tail := line[:c.col], line[c.col:]
	rest := append([]string{tail}, c.lines[c.row+1:]...)
	c.lines = append(append(c.lines[:c.row], head), rest...)
	c.row++
	c.col = 0
}

// Backspace removes the character before the insertion point, joining lines at a boundary.
func (c *Composer) Backspace() {
	if c.col > 0 {
		line := c.lines[c.row]
		_, size := utf8.DecodeLastRuneInString(line[:c.col])
		c.lines[c.row] = line[:c.col-size] + line[c.col:]
		c.col -= size
		return
	}
	if c.row == 0 {
		return
	}
	previous := c.lines[c.row-1]
	c.lines[c.row-1] = previous + c.lines[c.row]
	c.lines = append(c.lines[:c.row], c.lines[c.row+1:]...)
	c.row--
	c.col = len(previous)
}

// Paste folds a block into a single token and inserts that.
//
// One line is inserted as itself: a token for a pasted sentence would be more work to edit than the sentence. Two or more
// become a marker, because that is the point at which a paste stops being text somebody wants to read in the box.
func (c *Composer) Paste(text string) {
	cleaned := strings.ReplaceAll(text, "\r\n", "\n")
	if strings.Count(cleaned, "\n") == 0 {
		c.Insert(cleaned)
		return
	}

	lines := len(strings.Split(cleaned, "\n"))
	token := PasteToken{
		// Numbered, because Value substitutes markers textually: two pastes of the same size would otherwise share a
		// marker, and the second would be replaced by the first's text — a silent corruption of what gets sent.
		Marker: fmt.Sprintf("[pasted block %d, %d line(s)]", len(c.pastes)+1, lines),
		Text:   cleaned,
	}
	c.pastes = append(c.pastes, token)
	c.Insert(token.Marker)
}

// SendAction is what a key does in the composer: send the message, or insert a newline.
type SendAction int

const (
	// ActionNone means the composer does not act on this key.
	ActionNone SendAction = iota
	// ActionSend means the message is ready.
	ActionSend
	// ActionNewline means the key was a deliberate newline.
	ActionNewline
)

// ActionOf reports what a key does in a composer.
//
// Enter sends and every modified Enter makes a newline, which is the split that keeps a multi-line question typeable at
// all: without it, every newline would send and the question could never be two lines. Ctrl+J is included because some
// terminals report Shift+Enter as it.
func ActionOf(key Key) SendAction {
	switch {
	case key.Name == "ctrl+j":
		return ActionNewline
	// Suffix, not equality: Bubble Tea's KeyPressMsg.String() renders a modified key as "shift+enter", so an adapted key
	// never equals "enter". The first version compared for equality, which made every modified Enter fall through to
	// ActionNone — the criterion broken, and invisible to tests that built their Key values by hand.
	case !strings.HasSuffix(key.Name, "enter"):
		return ActionNone
	case key.Shift, key.Alt, key.Ctrl:
		return ActionNewline
	default:
		return ActionSend
	}
}

// Key is the part of a keystroke the composer reasons about, kept separate from Bubble Tea's message so the rules are
// testable without one.
type Key struct {
	Name             string
	Shift, Alt, Ctrl bool
}

// KeyOf adapts a Bubble Tea key press.
func KeyOf(press tea.KeyPressMsg) Key {
	return Key{
		Name:  press.String(),
		Shift: press.Mod.Contains(tea.ModShift),
		Alt:   press.Mod.Contains(tea.ModAlt),
		Ctrl:  press.Mod.Contains(tea.ModCtrl),
	}
}
