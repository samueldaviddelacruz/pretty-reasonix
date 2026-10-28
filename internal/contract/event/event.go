package event

import (
	"encoding/json"
	"time"

	"pretty-reasonix/internal/contract/provider"
)

// Event is the only channel between the engine and a frontend, so that the
// terminal UI and the headless runner cannot drift.
type Event interface {
	eventKind() string
}

// TurnStarted is an event emitted when a turn begins.
type TurnStarted struct {
	TurnID string
	Input  string
}

func (TurnStarted) eventKind() string { return "turn.started" }

// TextDelta is an event emitted when text is added to a turn.
type TextDelta struct {
	TurnID string
	ItemID string
	Text   string
}

func (TextDelta) eventKind() string { return "text.delta" }

// ReasoningDelta is an event emitted when reasoning is added to a turn.
type ReasoningDelta struct {
	TurnID string
	ItemID string
	Text   string
}

func (ReasoningDelta) eventKind() string { return "reasoning.delta" }

// MessageCommitted is an event emitted when a message is committed to a turn.
type MessageCommitted struct {
	TurnID  string
	ItemID  string
	Message provider.Message
}

func (MessageCommitted) eventKind() string { return "message.committed" }

// ToolDispatched is an event emitted when a tool is dispatched.
type ToolDispatched struct {
	TurnID   string
	ItemID   string
	CallID   string
	Name     string
	Args     json.RawMessage
	ReadOnly bool
}

func (ToolDispatched) eventKind() string { return "tool.dispatched" }

// ToolProgress is an event emitted when tool progress is reported.
type ToolProgress struct {
	CallID  string
	Phase   string
	Preview string
	Elapsed time.Duration
}

func (ToolProgress) eventKind() string { return "tool.progress" }

// ToolResult is an event emitted when a tool result is returned.
type ToolResult struct {
	CallID     string
	Output     string
	Truncated  bool
	Err        error
	DurationMs int
}

func (ToolResult) eventKind() string { return "tool.result" }

// ApprovalRequested is an event emitted when approval is requested for a tool call.
type ApprovalRequested struct {
	CallID  string
	Name    string
	Subject string
	Preview string
	Options []ApprovalOption
}

func (ApprovalRequested) eventKind() string { return "approval.requested" }

// ApprovalDecided is an event emitted when approval is decided for a tool call.
type ApprovalDecided struct {
	CallID   string
	Decision Decision
	Scope    GrantScope
}

func (ApprovalDecided) eventKind() string { return "approval.decided" }

// AskRequested is an event emitted when a question is requested.
type AskRequested struct {
	ItemID    string
	Questions []Question
}

func (AskRequested) eventKind() string { return "ask.requested" }

// Notice is an event emitted to report a notice.
//
// Code is a stable dotted identifier such as permission.deny_rule, is part of the API,
// and is what tests assert and the UI localises. Prose in Text may change freely and Code may not.
type Notice struct {
	Level NoticeLevel
	Code  string
	Text  string
}

func (Notice) eventKind() string { return "notice" }

// TodoStateChanged is an event emitted when todo state changes.
type TodoStateChanged struct {
	Todos []Todo
}

func (TodoStateChanged) eventKind() string { return "todo.state.changed" }

// CompactionStarted is an event emitted when compaction begins.
type CompactionStarted struct {
	TurnID string
	Reason string
}

func (CompactionStarted) eventKind() string { return "compaction.started" }

// CompactionCommitted is an event emitted when compaction is committed.
type CompactionCommitted struct {
	TurnID       string
	Digest       string
	TokensBefore int
	TokensAfter  int
}

func (CompactionCommitted) eventKind() string { return "compaction.committed" }

// UsageReported is an event emitted when usage is reported.
type UsageReported struct {
	TurnID string
	Usage  provider.Usage
	Cost   *CostQuote
}

func (UsageReported) eventKind() string { return "usage.reported" }

// TurnDone is an event emitted when a turn is done.
type TurnDone struct {
	TurnID    string
	Status    TurnStatus
	Usage     provider.Usage
	Readiness *Readiness
}

func (TurnDone) eventKind() string { return "turn.done" }

// ToolCallStarted is an event emitted when a tool call starts.
type ToolCallStarted struct {
	TurnID string
	ItemID string
	CallID string
	Name   string
}

func (ToolCallStarted) eventKind() string { return "tool.call.started" }

// NoticeLevel is how much a Notice matters — for styling and ordering, while Code carries the
// machine-readable part.
type NoticeLevel string

// The three levels, from news worth showing to a problem that stopped something.
const (
	NoticeInfo  NoticeLevel = "info"
	NoticeWarn  NoticeLevel = "warn"
	NoticeError NoticeLevel = "error"
)

// TurnStatus is how a turn ended, which is not the same as whether it succeeded: a blocked turn is
// waiting on a question rather than wrong.
type TurnStatus string

// The four endings a turn can have.
const (
	StatusDone      TurnStatus = "done"
	StatusFailed    TurnStatus = "failed"
	StatusCancelled TurnStatus = "cancelled"
	StatusBlocked   TurnStatus = "blocked"
)

// GrantScope is how long an approval lasts, which is what separates a yes from a yes-and-stop-asking.
type GrantScope string

// The three durations an approval can be granted for.
const (
	GrantOnce    GrantScope = "once"
	GrantSession GrantScope = "session"
	GrantAlways  GrantScope = "always"
)

// Decision is the answer to an ApprovalRequested, carrying the scope it was granted for so the UI can
// show whether a yes will be asked again.
type Decision struct {
	Allow bool
	Scope GrantScope
}

// ApprovalOption is an option for approval.
type ApprovalOption struct {
	Label string
	Scope GrantScope
	Allow bool
}

// Question is a question to ask.
type Question struct {
	ID         string
	Prompt     string
	Options    []string
	AllowOther bool
}

// Todo is a todo item.
type Todo struct {
	ID   string
	Text string
	Done bool
}

// Readiness indicates readiness.
type Readiness struct {
	Confirmed bool
	Reason    string
}

// CostQuote is a cost quote.
type CostQuote struct {
	Amount    float64
	Currency  string
	Estimated bool
}
