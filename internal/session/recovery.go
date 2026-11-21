package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	// ErrNewerLog reports a log written by a later version, which this reader must not guess at.
	ErrNewerLog = errors.New("this session was written by a newer version")
	// ErrUnknownRecord reports a record type this version does not understand, which has the same cause: a newer
	// writer owns the file.
	ErrUnknownRecord = errors.New("the session contains a record type this version does not understand")
	// ErrLogTooLarge reports a log past the preflight ceiling, refused before anything is materialised.
	ErrLogTooLarge = errors.New("the session log is larger than this version will read")
)

// MaxLogBytes is the preflight ceiling for one log.
//
// A ceiling exists because salvaging means holding the file: without one, a damaged or hostile file decides how much
// memory this process uses. Sixty-four mebibytes is far past any transcript a person produces — a long session is
// hundreds of kilobytes — and far below the point where reading one would hurt.
const MaxLogBytes = 64 << 20

// Recovered is what a tolerant read produces: the messages that survived, and what had to be set aside.
type Recovered struct {
	Messages []Message
	// Dropped is how many trailing bytes were unreadable, and Damaged names where they were moved. Empty when
	// nothing was discarded, which is the ordinary case.
	Dropped int
	Damaged string
}

// Notice is a sentence a frontend can show for a recovery, or empty when there was nothing to report.
//
// It lives here rather than in a frontend because the facts are here: how many bytes were lost and where they went is
// something only this package knows, and a frontend that had to reconstruct it would eventually get it wrong.
func (r Recovered) Notice() string {
	if r.Dropped == 0 {
		return ""
	}
	return fmt.Sprintf("set aside %d unreadable bytes at the end of the session, kept in %s",
		r.Dropped, filepath.Base(r.Damaged))
}

// Recover reads a session's log, salvaging whatever prefix is sound.
//
// Three outcomes, and the difference between them is the whole ticket:
//
//   - A readable log replays completely and nothing is written.
//   - A damaged *tail* — an unparseable line, or one whose message index does not continue the chain — stops replay at
//     the last good boundary. The valid prefix is returned, the log is truncated to that boundary, and the discarded
//     bytes are moved to `.damaged` rather than deleted, because the person may still want to see what was in them.
//   - An **unknown schema version or record type is a hard error.** That is not damage: it means a newer writer owns
//     the file, and salvaging a prefix out of a format this version does not understand would be guessing at data it
//     cannot read.
//
// The distinction is deliberate: a torn write is an accident that happened to this file, while an unknown version is a
// file that is *fine* and simply newer. Treating the second as the first would corrupt it.
func Recover(layout Layout) (Recovered, error) {
	info, err := os.Stat(layout.Events)
	if errors.Is(err, os.ErrNotExist) {
		return Recovered{}, nil
	}
	if err != nil {
		return Recovered{}, fmt.Errorf("reading the session: %w", err)
	}
	// Preflight before reading: the size decides whether this process is willing to hold the file at all.
	if info.Size() > MaxLogBytes {
		return Recovered{}, fmt.Errorf("%w: %d bytes", ErrLogTooLarge, info.Size())
	}
	if info.Size() == 0 {
		return Recovered{}, nil
	}

	body, err := os.ReadFile(layout.Events)
	if err != nil {
		return Recovered{}, fmt.Errorf("reading the session: %w", err)
	}

	read := judge(body)
	if read.newer != nil {
		// Nothing is written: the file is intact and newer, so salvaging or truncating it would be the corruption.
		return Recovered{}, read.newer
	}
	if read.good == len(body) {
		return Recovered{Messages: read.messages}, nil
	}

	// Everything from the last good boundary on is unreadable. It is moved, not deleted: the person may still want to
	// see what was in it, and truncating first would make that impossible.
	damaged, err := writeDamaged(layout.Events+".damaged", body[read.good:])
	if err != nil {
		return Recovered{}, err
	}
	// The log is truncated only after the bytes are safely elsewhere, so a failure here loses nothing.
	if err := os.Truncate(layout.Events, int64(read.good)); err != nil {
		return Recovered{}, fmt.Errorf("truncating the session log: %w", err)
	}
	return Recovered{Messages: read.messages, Dropped: len(body) - read.good, Damaged: damaged}, nil
}

// writeDamaged saves the unreadable bytes beside the log.
//
// The file is created rather than overwritten, and a name already in use is kept: two salvages of the same session
// must not destroy the first one's evidence, because the first is what somebody may already be looking at.
func writeDamaged(path string, body []byte) (string, error) {
	target := path
	for suffix := 2; fileExists(target); suffix++ {
		target = fmt.Sprintf("%s.%d", path, suffix)
	}
	// G703 is about a tainted path; this one is built from the session's own layout plus a numeric suffix, and the
	// file it writes is the bytes already read from that session.
	if err := os.WriteFile(target, body, ModeTranscript); err != nil { //nolint:gosec // G703: the path is the session's own, with a numeric suffix
		return "", fmt.Errorf("saving the unreadable part of the session: %w", err)
	}
	// The name it actually used, not the one it started from: a report that named the first path would tell a reader
	// to look at the previous salvage's bytes.
	return target, nil
}

// verdict is one function's answer to "how far can this log be trusted".
//
// It is a single return rather than a prefix plus a helper the caller must remember to call: the distinction between
// damage and a newer writer is the whole ticket, and a design that can be used while forgetting it is a design that
// will be.
type verdict struct {
	messages []Message
	// good is the byte offset of the first record that could not be used.
	good int
	// newer is set when the file is intact but written by a later version, which must never be salvaged: the prefix
	// would be this version guessing at a format it does not understand.
	newer error
}

// judge reads as much of the log as it can vouch for.
//
// It stops rather than skipping: a record after a gap describes a conversation that includes a message nobody has, and
// continuing past it would produce a transcript that never existed.
func judge(body []byte) verdict {
	var messages []Message
	offset := 0
	for offset < len(body) {
		line, _ := cutLine(body[offset:])
		next := offset + len(line)
		// The newline that ended the line belongs to it. Checking the byte rather than the remainder, because an
		// unterminated final line and a newline-terminated one both leave an empty remainder.
		if next < len(body) && body[next] == 0x0a {
			next++
		}

		var record Record
		if err := json.Unmarshal(line, &record); err != nil {
			// A torn or unparseable line: damage, so the prefix is still usable.
			return verdict{messages: messages, good: offset}
		}
		if record.SchemaVersion > SchemaVersion {
			return verdict{good: offset, newer: fmt.Errorf("%w: version %d, this reader understands %d",
				ErrNewerLog, record.SchemaVersion, SchemaVersion)}
		}
		switch record.Type {
		case RecordAppend:
			if len(record.Messages) != 1 || record.MessageIndex != len(messages) {
				return verdict{messages: messages, good: offset}
			}
			messages = append(messages, record.Messages[0])
		case RecordReplace:
			messages = append([]Message(nil), record.Messages...)
		default:
			return verdict{good: offset, newer: fmt.Errorf("%w: %q", ErrUnknownRecord, record.Type)}
		}
		offset = next
	}
	return verdict{messages: messages, good: len(body)}
}
