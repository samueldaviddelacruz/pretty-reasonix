package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Kinds of record. A transcript is a chain of appends with occasional whole-transcript replacements, and the
// difference is worth a field rather than a convention: a consumer that sees `replace` knows the earlier lines are
// history rather than the current answer to "what was said".
const (
	// RecordAppend adds one message to the end.
	RecordAppend = "append"
	// RecordReplace supersedes everything before it with a whole transcript.
	RecordReplace = "replace"
)

var (
	// ErrChainBroken reports an append that would leave a gap: the message index does not follow the log.
	ErrChainBroken = errors.New("the append does not continue the log")
	// ErrEmptyDigest reports a record with nothing to digest, which is a bug at the call site.
	ErrEmptyDigest = errors.New("a message with no content has no digest")
)

// Record is one line of the event log.
//
// The revision pair is what makes a log replayable in the face of a crash: `Revision` numbers this record,
// `BaseRevision` says which revision a replacement supersedes, and `MessageIndex` says where in the transcript it
// lands. `ContentDigest` is the integrity check a torn final line needs.
type Record struct {
	Revision      int       `json:"revision"`
	BaseRevision  int       `json:"base_revision"`
	Kind          string    `json:"kind"`
	MessageIndex  int       `json:"message_index"`
	ContentDigest string    `json:"content_digest"`
	Message       *Message  `json:"message,omitempty"`
	Replaced      []Message `json:"replaced,omitempty"`
}

// Log writes a transcript twice: an event log that can be replayed, and a canonical `.jsonl` that anything can read.
//
// Both are appended, never rewritten, because both are line-oriented and a conversation only ever grows. Two files
// rather than one because they answer different questions — the canonical one is "what was said", the event log is
// "how it got here", including the replacements a canonical file cannot show.
type Log struct {
	events    *os.File
	canonical *os.File
	revision  int
	length    int
}

// CreateLog opens both files for appending, creating them owner-only.
//
// A transcript can hold anything a person typed and anything a tool printed, so 0600 is the only defensible mode:
// the file is as sensitive as the conversation in it.
func CreateLog(eventsPath, canonicalPath string) (*Log, error) {
	events, err := os.OpenFile(eventsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // G304: the caller names the session's own file
	if err != nil {
		return nil, fmt.Errorf("opening the event log: %w", err)
	}
	canonical, err := os.OpenFile(canonicalPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // G304: as above
	if err != nil {
		_ = events.Close()
		return nil, fmt.Errorf("opening the canonical transcript: %w", err)
	}
	return &Log{events: events, canonical: canonical}, nil
}

// Append adds one message, refusing to leave a gap.
//
// The chain check is the whole reason a log beats a bare list of lines: an append that does not continue the last
// record means something was lost between them, and a reader that silently accepted it would answer questions about
// a conversation that never happened.
func (l *Log) Append(message Message) error {
	if message.Content == "" {
		return ErrEmptyDigest
	}
	digest := digestOf(message)

	l.revision++
	record := Record{
		Revision:      l.revision,
		BaseRevision:  l.revision - 1,
		Kind:          RecordAppend,
		MessageIndex:  l.length,
		ContentDigest: digest,
		Message:       &message,
	}
	if err := l.write(record); err != nil {
		l.revision-- // the caller may retry, and a revision that skipped would break the next chain check
		return err
	}
	l.length++
	return nil
}

// Replace supersedes everything before it with a whole transcript.
//
// Compaction rewrites history rather than deleting it, so the record carries the replacement in full and names the
// revision it supersedes. Replaying the log therefore reproduces the transcript as it stood, and still shows that a
// rewrite happened.
func (l *Log) Replace(messages []Message) error {
	l.revision++
	record := Record{
		Revision:     l.revision,
		BaseRevision: l.revision - 1,
		Kind:         RecordReplace,
		MessageIndex: 0,
		Replaced:     messages,
	}
	if err := l.write(record); err != nil {
		l.revision--
		return err
	}
	l.length = len(messages)
	return nil
}

// write puts one record on both files and flushes them.
//
// fsync per record is expensive and correct: a lost message is a lost conversation, and a half-written last line is
// worse than a missing one because it reads as data. The cost is bounded by how often a turn appends, which is once
// per message rather than once per token.
func (l *Log) write(record Record) error {
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encoding the record: %w", err)
	}
	line = append(line, '\n')

	if _, err := l.events.Write(line); err != nil {
		return fmt.Errorf("appending to the event log: %w", err)
	}
	if err := l.events.Sync(); err != nil {
		return fmt.Errorf("flushing the event log: %w", err)
	}

	// The canonical file cannot show a replacement, so it only ever records appends. It is a view, not the record.
	if record.Kind == RecordAppend && record.Message != nil {
		canonical, marshalErr := json.Marshal(record.Message)
		if marshalErr != nil {
			return fmt.Errorf("encoding the canonical message: %w", marshalErr)
		}
		if _, err := l.canonical.Write(append(canonical, '\n')); err != nil {
			return fmt.Errorf("appending to the canonical transcript: %w", err)
		}
		if err := l.canonical.Sync(); err != nil {
			return fmt.Errorf("flushing the canonical transcript: %w", err)
		}
	}
	return nil
}

// Close releases both files.
func (l *Log) Close() error {
	return errors.Join(l.events.Close(), l.canonical.Close())
}

// digestOf is the integrity check a record carries.
//
// It covers the content and the role rather than the whole message: a digest that covered tool-call arguments would
// change whenever a fixture was re-serialised, and the question being asked is "is this the message that was
// written", not "is it byte-identical to what a newer encoder would produce".
func digestOf(message Message) string {
	sum := sha256.Sum256([]byte(string(message.Role) + "\x00" + message.Content))
	return hex.EncodeToString(sum[:])
}

// ReadTranscript reads a transcript, preferring the event log when both files exist.
//
// The event log wins because it is the record; the canonical file is a view of it. That preference is the reason
// both are written during the transition rather than switching in one step — an old reader still works, and a new
// one gets the replacements.
func ReadTranscript(eventsPath, canonicalPath string) ([]Message, error) {
	if body, err := os.ReadFile(eventsPath); err == nil { //nolint:gosec // G304: the caller names the session's own file
		return replay(body)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading the event log: %w", err)
	}

	body, err := os.ReadFile(canonicalPath) //nolint:gosec // G304: as above
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the canonical transcript: %w", err)
	}

	var messages []Message
	for len(body) > 0 {
		line, rest := cutLine(body)
		body = rest
		if len(line) == 0 {
			continue
		}
		var message Message
		if err := json.Unmarshal(line, &message); err != nil {
			return nil, fmt.Errorf("a canonical line is not a message: %w", err)
		}
		messages = append(messages, message)
	}
	return messages, nil
}

// replay rebuilds a transcript from event records. Tolerating a damaged tail is E9-T3's ticket; this is strict on
// purpose so that a bug in the writer is loud here rather than interpreted later.
func replay(body []byte) ([]Message, error) {
	var messages []Message
	for len(body) > 0 {
		line, rest := cutLine(body)
		body = rest
		if len(line) == 0 {
			continue
		}
		var record Record
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, fmt.Errorf("an event line is not a record: %w", err)
		}
		switch record.Kind {
		case RecordReplace:
			messages = append([]Message(nil), record.Replaced...)
		case RecordAppend:
			if record.Message == nil {
				return nil, fmt.Errorf("an append record at revision %d has no message", record.Revision)
			}
			if record.MessageIndex != len(messages) {
				return nil, fmt.Errorf("%w: index %d after %d messages",
					ErrChainBroken, record.MessageIndex, len(messages))
			}
			messages = append(messages, *record.Message)
		default:
			return nil, fmt.Errorf("unknown record kind %q at revision %d", record.Kind, record.Revision)
		}
	}
	return messages, nil
}

// cutLine splits one newline-terminated line off the front, without allocating when there is no newline.
func cutLine(body []byte) ([]byte, []byte) {
	for index, b := range body {
		if b == '\n' {
			return body[:index], body[index+1:]
		}
	}
	return body, nil
}
