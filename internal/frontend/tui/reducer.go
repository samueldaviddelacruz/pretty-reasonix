package tui

import (
	"fmt"
	"strings"

	"pretty-reasonix/internal/contract/event"
	"pretty-reasonix/internal/contract/provider"
)

// RowKind is what a transcript row is, per TDD §6.12.2.
type RowKind int

// The row vocabulary. Naming every kind now costs one line each and saves the reducer from growing a boolean per
// special case later.
const (
	RowUser RowKind = iota
	RowSay
	RowTool
	RowDecision
	RowAsk
	RowNotice
	RowTodos
	RowCompaction
	RowReceipt
	RowUsage
	RowError
)

// String names a kind for a test failure.
func (k RowKind) String() string {
	switch k {
	case RowUser:
		return "user"
	case RowSay:
		return "say"
	case RowTool:
		return "tool"
	case RowDecision:
		return "decision"
	case RowAsk:
		return "ask"
	case RowNotice:
		return "notice"
	case RowTodos:
		return "todos"
	case RowCompaction:
		return "compaction"
	case RowReceipt:
		return "receipt"
	case RowUsage:
		return "usage"
	case RowError:
		return "error"
	default:
		return fmt.Sprintf("rowkind(%d)", int(k))
	}
}

// Row is one entry in the transcript, and it knows whether it is final.
type Row struct {
	ID   string
	Kind RowKind
	// Text is the plain content. Drawing it is E13's: this ticket decides what a row *is*, not what it looks like.
	Text string
	// Settled means it will not change again, so a frontend may print it to the scrollback and forget it.
	Settled bool
	// Foldable marks a row a frontend may collapse once settled — reasoning, long tool output — and Folded is the
	// current state of that. Inline mode prints the unfolded form because it cannot repaint.
	Foldable bool
	Folded   bool

	// Tool bookkeeping: a dispatch, its progress and its result are one row, keyed by call id.
	CallID string
	// Tool is the name the model asked for, and Phase is what it is doing now. Two fields because they are two facts:
	// one field let progress overwrite the name, so a finished row could not say which tool produced it.
	Tool  string
	Phase string
}

// Render draws a row as plain text.
//
// Width is accepted and ignored: terminal styling, wrapping and folding are `termrender`'s, and E13 replaces this
// method with a call into it. Keeping the signature now means the call sites do not change when it does.
func (r Row) Render(int) string { return r.Text }

// Reducer folds the engine's events into rows.
//
// It is the only place that decides what the transcript is: the model owns one, the view reads it, and nothing else may
// append. That is what makes the stream testable without a terminal — feed events, assert rows.
type Reducer struct {
	rows []Row
	// byCall and byItem find the row a later event belongs to. Maps rather than a scan because a long turn is
	// thousands of rows and every event would otherwise pay for the whole history.
	byCall map[string]int
	byItem map[string]int
	// live is the row currently receiving deltas for an item, so a burst appends rather than creating a row each.
	live map[string]int
}

// NewReducer makes an empty transcript.
func NewReducer() *Reducer {
	return &Reducer{
		byCall: make(map[string]int),
		byItem: make(map[string]int),
		live:   make(map[string]int),
	}
}

// Rows returns a copy, so a caller can read the transcript without being able to append to it.
func (r *Reducer) Rows() []Row {
	out := make([]Row, len(r.rows))
	copy(out, r.rows)
	return out
}

// Apply folds one event into the transcript.
func (r *Reducer) Apply(e event.Event) {
	switch typed := e.(type) {
	case event.TurnStarted:
		r.append(Row{ID: typed.TurnID + "/you", Kind: RowUser, Text: typed.Input, Settled: true})

	case event.ReasoningDelta:
		row := r.item(typed.ItemID, typed.TurnID+"/reasoning", RowSay, typed.TurnID)
		row.Foldable, row.Folded = true, true
		row.Text += typed.Text
		row.Settled = false

	case event.TextDelta:
		row := r.item(typed.ItemID, typed.TurnID+"/say", RowSay, typed.TurnID)
		row.Text += typed.Text
		row.Settled = false

	case event.MessageCommitted:
		// Committed wins over deltas: the deltas were a preview of exactly this, and the committed text is what the
		// transcript is. Anything else would let a dropped delta leave a permanent gap in a settled answer.
		if typed.Message.Role != provider.RoleAssistant && typed.Message.Role != provider.RoleTool {
			return
		}
		row := r.item(typed.ItemID, typed.TurnID+"/say", RowSay, typed.TurnID)
		if typed.Message.Content != "" {
			row.Text = typed.Message.Content
		}
		row.Settled = true

	case event.ToolCallStarted:
		r.callRow(typed.CallID, typed.TurnID, typed.Name)

	case event.ToolDispatched:
		row := r.callRow(typed.CallID, typed.TurnID, typed.Name)
		if len(typed.Args) > 0 {
			row.Text = string(typed.Args)
		}

	case event.ToolProgress:
		// Progress merges into the row the call already has, and an empty field never erases: a phase with no preview
		// must not blank the preview that came before it.
		if index, found := r.byCall[typed.CallID]; found {
			if typed.Preview != "" {
				r.rows[index].Text = typed.Preview
			}
			if typed.Phase != "" {
				r.rows[index].Phase = typed.Phase
			}
			return
		}
		r.append(Row{ID: typed.CallID, Kind: RowTool, CallID: typed.CallID, Phase: typed.Phase, Text: typed.Preview})

	case event.ToolResult:
		row := r.callRow(typed.CallID, "", "")
		// Same rule one field further: a result that says nothing must not erase what the dispatch said.
		switch {
		case typed.Err != nil:
			row.Text = typed.Err.Error()
			row.Kind = RowError
		case typed.Output != "":
			row.Text = typed.Output
		}
		row.Folded, row.Foldable = true, true
		row.Settled = true

	case event.Notice:
		kind := RowNotice
		if typed.Level == event.NoticeError {
			kind = RowError
		}
		r.append(Row{ID: "notice/" + typed.Code, Kind: kind, Text: typed.Text, Settled: true})

	case event.UsageReported:
		r.append(Row{ID: "usage", Kind: RowUsage, Text: usageText(typed.Usage), Settled: false})

	case event.CompactionStarted:
		r.append(Row{ID: "compaction", Kind: RowCompaction, Text: "compacting", Settled: false})

	case event.CompactionCommitted:
		if index, found := r.indexOf("compaction"); found {
			r.rows[index].Text, r.rows[index].Settled = "compacted", true
			return
		}
		r.append(Row{ID: "compaction", Kind: RowCompaction, Text: "compacted", Settled: true})

	case event.ApprovalRequested:
		// The subject is what the decision is about — a path, a command — and the name is which rule asked.
		r.append(Row{ID: typed.CallID, Kind: RowDecision, Text: typed.Name + " " + typed.Subject, CallID: typed.CallID})

	case event.AskRequested:
		r.append(Row{ID: typed.ItemID, Kind: RowAsk, Text: askText(typed.Questions)})

	case event.TurnDone:
		// Everything still open belongs to a turn that is over, so it is final now whatever it says.
		for index := range r.rows {
			r.rows[index].Settled = true
		}
	}
}

// append adds a row and returns its index.
func (r *Reducer) append(row Row) int {
	r.rows = append(r.rows, row)
	index := len(r.rows) - 1
	if row.CallID != "" {
		r.byCall[row.CallID] = index
	}
	if row.ID != "" {
		r.byItem[row.ID] = index
	}
	return index
}

// item finds or creates the row a delta belongs to, and returns a pointer to it.
//
// A pointer rather than an index because every caller mutates what it gets, and passing an index around invites the
// slice being reallocated underneath it.
func (r *Reducer) item(itemID, fallbackID string, kind RowKind, turnID string) *Row {
	key := itemID
	if key == "" {
		key = fallbackID
	}
	if index, found := r.live[key]; found {
		return &r.rows[index]
	}
	index := r.append(Row{ID: key, Kind: kind, Text: ""})
	r.live[key] = index
	return &r.rows[index]
}

// callRow finds or creates the row a tool call owns.
func (r *Reducer) callRow(callID, turnID, name string) *Row {
	if index, found := r.byCall[callID]; found {
		if name != "" {
			r.rows[index].Tool = name
		}
		return &r.rows[index]
	}
	id := callID
	if id == "" {
		id = turnID + "/tool"
	}
	index := r.append(Row{ID: id, Kind: RowTool, CallID: callID, Tool: name})
	return &r.rows[index]
}

// indexOf finds a row by id.
func (r *Reducer) indexOf(id string) (int, bool) {
	index, found := r.byItem[id]
	return index, found
}

// settledPrefix splits a streaming answer into the part that will not change and the part that still might.
//
// The rule is the last blank line **outside a fenced block**, and the fence clause is the whole point: a code block
// contains blank lines, and splitting inside one would print half a listing to the scrollback and then re-render the
// rest somewhere it can never be corrected. Inline mode cannot repaint, so this boundary is what keeps it honest.
func settledPrefix(text string) (string, string) {
	fenced := false
	boundary := 0
	for index := 0; index < len(text); index++ {
		// A fence is a line beginning with three backticks; the same sequence closes it.
		if isFenceLine(text, index) {
			fenced = !fenced
			index += 2 // the two remaining backticks of this fence
			continue
		}
		if !fenced && strings.HasPrefix(text[index:], "\n\n") {
			boundary = index + 2
		}
	}
	return text[:boundary], text[boundary:]
}

// isFenceLine reports whether a triple-backtick fence starts at index and begins its own line.
func isFenceLine(text string, index int) bool {
	if !strings.HasPrefix(text[index:], "```") {
		return false
	}
	if index == 0 {
		return true
	}
	return text[index-1] == '\n'
}

// usageText renders a receipt as one line. A frontend with room shows more; this is what a row carries.
func usageText(usage provider.Usage) string {
	return fmt.Sprintf("%d in · %d out · %d cached",
		usage.InputTokens, usage.OutputTokens, usage.CacheHitTokens)
}

// askText joins the questions of an ask into one row.
//
// One row rather than one per question: the decision card is a single component with one owner (E11-T3), and a
// transcript that split an ask across rows would give a later ticket two things to keep in step.
func askText(questions []event.Question) string {
	parts := make([]string, 0, len(questions))
	for _, question := range questions {
		parts = append(parts, question.Prompt)
	}
	return strings.Join(parts, " · ")
}
