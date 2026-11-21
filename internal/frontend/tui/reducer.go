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
	// Live is the part of a streaming row that could still change: everything after the last blank line outside a fence.
	// A frontend renders Settled as history and Live as the region it repaints.
	Live string
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
		// Keyed by the turn, not the item: the engine gives one item id to a whole round, so reasoning and answer would
		// otherwise share a row — and that row is foldable, so folding the thinking would fold away the answer.
		row := r.item(reasonKey(typed.TurnID))
		row.Foldable, row.Folded = true, true
		row.Text += normalise(typed.Text)
		row.Settled = false

	case event.TextDelta:
		row := r.item(answerKey(typed.TurnID))
		row.Text += normalise(typed.Text)
		_, live := settledPrefix(row.Text)
		row.Settled = live == ""
		if !row.Settled {
			row.Live = live
		}

	case event.MessageCommitted:
		// Committed wins over deltas: the deltas were a preview of exactly this, and the committed text is what the
		// transcript is. Anything else would let a dropped delta leave a permanent gap in a settled answer.
		if typed.Message.Role != provider.RoleAssistant && typed.Message.Role != provider.RoleTool {
			return
		}
		// The same key the deltas used, which is what makes a commit *correct* a streamed answer rather than printing a
		// second one. The engine's commit carries a different id from its deltas, so keying on that id would append a
		// duplicate row and leave the preview uncorrected.
		row := r.item(answerKey(typed.TurnID))
		if typed.Message.Content != "" {
			row.Text = normalise(typed.Message.Content)
		}
		row.Settled, row.Live = true, ""

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
		// No CallID: a decision is not a tool row, and registering it in the tool map let a later result write onto the
		// card.
		r.append(Row{ID: "decision/" + typed.CallID, Kind: RowDecision, Text: typed.Name + " " + typed.Subject})

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

// item finds or creates a turn-keyed row, and returns a pointer to it.
//
// A pointer rather than an index: every caller mutates what it gets, and an index would go stale the moment an append
// reallocated the slice.
func (r *Reducer) item(key string) *Row {
	if index, found := r.live[key]; found {
		return &r.rows[index]
	}
	index := r.append(Row{ID: key, Kind: RowSay, Text: ""})
	r.live[key] = index
	return &r.rows[index]
}

// normalise folds CRLF to LF on the way in.
//
// Not cosmetic: a carriage return inside a rendered row is a real one - the terminal returns to column zero and the next
// character overwrites the line. It also makes the settled rule work, because a CRLF answer contains no blank line for it
// to find.
func normalise(text string) string { return strings.ReplaceAll(text, "\r\n", "\n") }

// answerKey and reasonKey name the two rows a turn grows.
func answerKey(turnID string) string { return turnID + "/say" }
func reasonKey(turnID string) string { return turnID + "/reasoning" }

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
	// Normalised first, because a CRLF answer would otherwise never contain a blank line and nothing would ever settle.
	normalised := strings.ReplaceAll(text, "\r\n", "\n")

	fenced := false
	boundary := 0
	for index := 0; index < len(normalised); index++ {
		if normalised[index] != '\n' && index != 0 && normalised[index-1] != '\n' {
			continue
		}
		// A fence opens or closes a block, and both ``` and ~~~ do it: markdown allows either, and a rule that knew only
		// one would treat the other's contents as blank lines.
		if length, fence := fenceAt(normalised, index); length > 0 {
			_ = fence
			fenced = !fenced
			index += length - 1
			continue
		}
		if !fenced && strings.HasPrefix(normalised[index:], "\n\n") {
			boundary = index + 2
		}
	}
	return text[:min(boundary, len(text))], text[min(boundary, len(text)):]
}

// fenceAt reports the length of a fence marker at index, or 0. Up to three leading spaces are allowed before the
// marker, which is what an indented code fence looks like.
func fenceAt(text string, index int) (int, bool) {
	start := index
	for start < len(text) && start-index < 3 && text[start] == ' ' {
		start++
	}
	if start >= len(text) {
		return 0, false
	}
	marker := text[start]
	if marker != '`' && marker != '~' {
		return 0, false
	}
	length := 0
	for start+length < len(text) && text[start+length] == marker {
		length++
	}
	if length < 3 {
		return 0, false
	}
	return length + (start - index), true
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
