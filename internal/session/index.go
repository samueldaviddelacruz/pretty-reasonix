package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

// IndexSchemaVersion is the index format this writer produces.
//
// Separate from the log's version because they age differently: the index is a cache and may be thrown away, while
// the log is the record. A reader that cannot understand an index rebuilds it; a reader that cannot understand a log
// must stop.
const IndexSchemaVersion = 1

// PreviewLength bounds the one piece of a transcript the picker shows without decoding anything.
const PreviewLength = 160

// Index is the picker's cache of one session, per TDD §6.10.
//
// It exists so that listing sessions costs one small read per session rather than decoding every transcript — and it
// is a cache in the strict sense: `LogDigest` names the event log this was built from, and a mismatch means the index
// is wrong rather than the log.
type Index struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	Title         string    `json:"title"`
	Preview       string    `json:"preview"`
	Model         string    `json:"model"`
	Turns         int       `json:"turns"`
	Messages      int       `json:"messages"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	LogDigest     string    `json:"log_digest"`
	LogBytes      int64     `json:"log_bytes"`
}

// Build derives an index from the transcript it describes.
//
// Timestamps come from the caller rather than from this package, because a cache that reads the clock is a cache that
// cannot be tested and a package that reads the clock is a package that cannot be reasoned about. `CreatedAt` is the
// rebuild's own time: the index may lose the original, and the log's own mtime is the record that survives.
func Build(id, model string, messages []Message, logDigest string, logBytes int64, now time.Time) Index {
	title, preview := describe(messages)
	return Index{
		SchemaVersion: IndexSchemaVersion,
		ID:            id,
		Title:         title,
		Preview:       preview,
		Model:         model,
		Turns:         countTurns(messages),
		Messages:      len(messages),
		CreatedAt:     now,
		UpdatedAt:     now,
		LogDigest:     logDigest,
		LogBytes:      logBytes,
	}
}

// describe picks the first thing the person said as the title, and the last thing either party said as the preview.
//
// Host-authored entries are skipped: an execution policy block would title every session the same way, which is the
// opposite of what a title is for.
func describe(messages []Message) (string, string) {
	var title, preview string
	for _, message := range messages {
		if message.Role == "user" && !message.HostAuthored && strings.TrimSpace(message.Content) != "" {
			title = firstLine(message.Content)
			break
		}
	}
	for _, message := range slices.Backward(messages) {
		if !message.HostAuthored && strings.TrimSpace(message.Content) != "" {
			preview = truncate(firstLine(message.Content), PreviewLength)
			break
		}
	}
	return title, preview
}

// countTurns counts how many times a person spoke, which is what a reader means by "turns" — not the number of model
// round trips, which is an implementation detail of how the answer was reached.
func countTurns(messages []Message) int {
	turns := 0
	for _, message := range messages {
		if message.Role == "user" && !message.HostAuthored {
			turns++
		}
	}
	return turns
}

// firstLine is the part of a message worth showing in a list: a title cut at a newline reads as a title, while one cut
// mid-sentence reads as a bug.
func firstLine(text string) string {
	first, _, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(first)
}

// truncate cuts to a byte budget on a rune boundary, so a preview cannot end in half a character.
func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8Start(text[cut]) {
		cut--
	}
	return strings.TrimSpace(text[:cut]) + "…"
}

// utf8Start reports whether a byte begins a rune.
func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// DigestOf is the identity of a log's bytes, which is what makes the index a cache rather than a claim.
func DigestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// Write stores the index beside the session, readable by a picker running as another local user.
func (i Index) Write(path string) error {
	body, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the index: %w", err)
	}
	if err := os.WriteFile(path, append(body, '\n'), ModeIndex); err != nil {
		return fmt.Errorf("writing the index: %w", err)
	}
	return nil
}

// ErrIndexStale reports an index that no longer describes its log.
var ErrIndexStale = errors.New("the index does not describe the log")

// LoadIndex reads an index and checks it against the log it names.
//
// A stale index is reported rather than returned, because the caller must rebuild from the log: an index is a cache,
// and trusting one over the record is how a picker shows a title for a conversation that has since changed. An index
// this version cannot read is treated the same way — it is throwable, unlike a log.
func LoadIndex(layout Layout) (Index, error) {
	body, err := os.ReadFile(layout.Index)
	if errors.Is(err, os.ErrNotExist) {
		return Index{}, ErrIndexStale
	}
	if err != nil {
		return Index{}, fmt.Errorf("reading the index: %w", err)
	}

	var index Index
	if decodeErr := json.Unmarshal(body, &index); decodeErr != nil {
		// Unreadable is stale: a cache that cannot be parsed has no claim to make.
		return Index{}, ErrIndexStale
	}
	if index.SchemaVersion != IndexSchemaVersion {
		return Index{}, ErrIndexStale
	}

	log, err := os.ReadFile(layout.Events)
	if err != nil {
		return Index{}, fmt.Errorf("reading the event log: %w", err)
	}
	if index.LogDigest != DigestOf(log) {
		return Index{}, ErrIndexStale
	}
	return index, nil
}

// Rebuild reads the log and writes a fresh index, returning it.
//
// The log is the only input, which is the whole point: whatever the previous index claimed, the answer comes from the
// record. `now` is the caller's clock, so this function has no opinion about when it is.
func Rebuild(layout Layout, model string, now time.Time) (Index, error) {
	log, err := os.ReadFile(layout.Events)
	if err != nil {
		return Index{}, fmt.Errorf("reading the event log: %w", err)
	}
	messages, err := replay(log)
	if err != nil {
		return Index{}, err
	}
	index := Build(idOf(layout), model, messages, DigestOf(log), int64(len(log)), now)
	if err := index.Write(layout.Index); err != nil {
		return Index{}, err
	}
	return index, nil
}

// LoadOrRebuild is what a picker calls: trust a current index, and repair a stale one from the log.
//
// It returns whether a repair happened, because that is a fact a caller may want to report and a test needs to see.
func LoadOrRebuild(layout Layout, model string, now time.Time) (Index, bool, error) {
	index, err := LoadIndex(layout)
	if err == nil {
		return index, false, nil
	}
	if !errors.Is(err, ErrIndexStale) {
		return Index{}, false, err
	}
	repaired, buildErr := Rebuild(layout, model, now)
	return repaired, true, buildErr
}

// idOf recovers a session id from its events path, so a rebuild does not need the id passed separately and cannot be
// given one that disagrees with the files it is reading.
func idOf(layout Layout) string {
	base := strings.TrimSuffix(layout.Events, ".events.jsonl")
	return base[strings.LastIndexByte(base, '/')+1:]
}
