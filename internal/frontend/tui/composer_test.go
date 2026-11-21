// White-box: the composer is unexported state with exported behaviour, and its rules are the subject.
//
//nolint:testpackage // the subject is unexported
package tui

import (
	"strings"
	"testing"
)

// composerWith makes a composer holding text, which is the state most of these start from.
func composerWith(text string) *Composer {
	c := NewComposer(CursorBar)
	c.Insert(text)
	return c
}

// TestEnterSendsAndModifiedEnterDoesNot is the criterion's first clause, and the split that makes a multi-line question
// typeable at all: if every newline sent, a question could never be two lines.
func TestEnterSendsAndModifiedEnterDoesNot(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		key  Key
		want SendAction
	}{
		"enter sends":            {Key{Name: "enter"}, ActionSend},
		"shift+enter newlines":   {Key{Name: "enter", Shift: true}, ActionNewline},
		"alt+enter newlines":     {Key{Name: "enter", Alt: true}, ActionNewline},
		"ctrl+j newlines":        {Key{Name: "ctrl+j"}, ActionNewline},
		"an ordinary key is not": {Key{Name: "a"}, ActionNone},
		// The shapes Bubble Tea actually produces. A modified key's String() includes its modifier, so these are the
		// values ActionOf really receives — the reason the first version was broken.
		"adapted shift+enter": {Key{Name: "shift+enter", Shift: true}, ActionNewline},
		"adapted alt+enter":   {Key{Name: "alt+enter", Alt: true}, ActionNewline},
		"adapted enter":       {Key{Name: "enter"}, ActionSend},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := ActionOf(tc.key); got != tc.want {
				t.Errorf("%s gave %v, want %v", name, got, tc.want)
			}
		})
	}
}

// TestTheComposerGrowsToEightRowsThenStops is the second clause: the cap is what keeps a long question from pushing the
// transcript off the screen.
func TestTheComposerGrowsToEightRowsThenStops(t *testing.T) {
	t.Parallel()
	c := NewComposer(CursorBar)
	if got := c.Height(); got != 1 {
		t.Errorf("an empty composer is %d rows", got)
	}

	for range 3 {
		c.Newline()
	}
	if got := c.Height(); got != 4 {
		t.Errorf("four lines are %d rows", got)
	}

	for range 20 {
		c.Newline()
	}
	if got := c.Height(); got != MaxComposerRows {
		t.Errorf("a long question is %d rows, want the cap of %d", got, MaxComposerRows)
	}
}

// TestAPastedBlockBecomesOneToken is the fourth clause. A fifty-line paste inserted literally turns the composer into a
// wall, and the question is lost inside it.
func TestAPastedBlockBecomesOneToken(t *testing.T) {
	t.Parallel()
	c := NewComposer(CursorBar)
	block := "line one\nline two\nline three"
	c.Paste(block)

	if strings.Contains(c.Raw(), "line two") {
		t.Errorf("the paste was inserted literally: %q", c.Raw())
	}
	if !strings.Contains(c.Raw(), "3 line(s)") {
		t.Errorf("the marker does not say how much was pasted: %q", c.Raw())
	}
	if got := c.Height(); got != 1 {
		t.Errorf("a pasted block made the composer %d rows", got)
	}
	// What reaches the model is exactly what was pasted, which is the point of folding rather than truncating.
	if c.Value() != block {
		t.Errorf("Value() = %q, want the pasted text", c.Value())
	}
}

// TestTwoPastesOfTheSameSizeDoNotCollide is the second finding from review: markers are substituted textually, so two
// pastes sharing a marker had the second replaced by the first's text.
func TestTwoPastesOfTheSameSizeDoNotCollide(t *testing.T) {
	t.Parallel()
	c := NewComposer(CursorBar)
	c.Paste("alpha\nbeta")
	c.Insert(" and ")
	c.Paste("gamma\ndelta")

	want := "alpha\nbeta and gamma\ndelta"
	if got := c.Value(); got != want {
		t.Errorf("Value() = %q, want %q", got, want)
	}
}

// TestASingleLinePasteIsJustText: a token for one pasted sentence would be more work to edit than the sentence.
func TestASingleLinePasteIsJustText(t *testing.T) {
	t.Parallel()
	c := NewComposer(CursorBar)
	c.Paste("just a sentence")

	if c.Raw() != "just a sentence" {
		t.Errorf("a one-line paste became %q", c.Raw())
	}
	if strings.Contains(c.Raw(), "pasted") {
		t.Error("a one-line paste was tokenised")
	}
}

// TestCRLFPasteIsNormalised, because a block copied from a Windows editor would otherwise leave carriage returns in a
// rendered row.
func TestCRLFPasteIsNormalised(t *testing.T) {
	t.Parallel()
	c := NewComposer(CursorBar)
	c.Paste("one\r\ntwo")

	if strings.Contains(c.Value(), "\r") {
		t.Errorf("Value() carries a carriage return: %q", c.Value())
	}
	if c.Value() != "one\ntwo" {
		t.Errorf("Value() = %q", c.Value())
	}
}

// TestTheComposerHoldsOnlyATokenIsNotEmpty, since a send of nothing but a paste would otherwise be refused.
func TestTheComposerHoldsOnlyATokenIsNotEmpty(t *testing.T) {
	t.Parallel()
	c := NewComposer(CursorBar)
	c.Paste("a\nb")

	if c.Empty() {
		t.Error("a composer holding a paste token reported itself empty")
	}
	if got := c.Value(); got != "a\nb" {
		t.Errorf("Value() = %q", got)
	}
}

// TestBackspaceJoinsLines, and removes a rune rather than a byte when the last character is multi-byte.
func TestBackspaceJoinsLines(t *testing.T) {
	t.Parallel()
	c := composerWith("ab\ncd")
	c.row, c.col = 1, 0
	c.Backspace()

	if got := c.Raw(); got != "abcd" {
		t.Errorf("after joining, the text is %q", got)
	}
	if c.col != 2 {
		t.Errorf("the cursor is at column %d, want 2", c.col)
	}

	multibyte := composerWith("héllo")
	multibyte.Backspace()
	if got := multibyte.Raw(); got != "héll" {
		t.Errorf("a multi-byte character was cut by byte: %q", got)
	}
}

// TestClearEmptiesEverythingIncludingPastes, so a sent message does not leave its block behind for the next one.
func TestClearEmptiesEverythingIncludingPastes(t *testing.T) {
	t.Parallel()
	c := NewComposer(CursorBar)
	c.Paste("a\nb")
	c.Clear()

	if !c.Empty() || c.Raw() != "" || c.Value() != "" {
		t.Errorf("Clear left %q", c.Raw())
	}
	if got := c.Height(); got != 1 {
		t.Errorf("a cleared composer is %d rows", got)
	}
}

// TestCursorShapeParsing: an unknown name falls back rather than refusing to start a terminal over a cosmetic setting.
func TestCursorShapeParsing(t *testing.T) {
	t.Parallel()
	cases := map[string]CursorShape{
		"bar":       CursorBar,
		"block":     CursorBlock,
		"underline": CursorUnderline,
		"nonsense":  CursorBar,
		"":          CursorBar,
	}
	// TrimSpace is part of the contract, asserted outside the table: gocritic flags a padded map key as suspicious, and it
	// is right that a literal with spaces in it usually is.
	if got := ParseCursorShape("  underline\t"); got != CursorUnderline {
		t.Errorf("a padded name parsed as %v", got)
	}
	for name, want := range cases {
		if got := ParseCursorShape(name); got != want {
			t.Errorf("ParseCursorShape(%q) = %v, want %v", name, got, want)
		}
	}
	if got := CursorShape(-3).String(); got != "cursor shape(-3)" {
		t.Errorf("an invalid shape formats as %q", got)
	}
}
