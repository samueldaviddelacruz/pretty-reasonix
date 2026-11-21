// White-box: the viewport is unexported state with exported behaviour, and the follow rule is the subject.
//
//nolint:testpackage // the subject is unexported
package tui

import (
	"strconv"
	"testing"
)

// numbered builds n lines whose text is their index, so a failure says which lines were shown.
func numbered(n int) []string {
	out := make([]string, n)
	for index := range n {
		out[index] = strconv.Itoa(index)
	}
	return out
}

// TestTheWindowIsTheTailByDefault: an empty viewport follows, and a full one shows the newest lines.
func TestTheWindowIsTheTailByDefault(t *testing.T) {
	t.Parallel()
	v := NewViewport(3)
	v.SetLines(numbered(10))

	if !v.Following() {
		t.Error("a fresh viewport is not following")
	}
	got := v.Visible()
	want := []string{"7", "8", "9"}
	if len(got) != len(want) {
		t.Fatalf("showing %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("showing %v, want %v", got, want)
		}
	}
}

// TestNewContentDoesNotMoveAReaderWhoScrolledUp is the criterion's follow rule, and the reason the offset is counted from
// the bottom.
func TestNewContentDoesNotMoveAReaderWhoScrolledUp(t *testing.T) {
	t.Parallel()
	v := NewViewport(3)
	v.SetLines(numbered(10))

	// Scroll up two lines — negative, because positive is the direction the screen moves when you scroll down.
	v.ScrollBy(-2)
	if v.Following() {
		t.Fatal("scrolling up left the viewport following")
	}
	before := v.Visible()

	// The answer grows by five lines while the reader is looking elsewhere.
	v.SetLines(numbered(15))

	after := v.Visible()
	if len(after) != len(before) {
		t.Fatalf("the window changed size: %v then %v", before, after)
	}
	for index := range before {
		if before[index] != after[index] {
			t.Fatalf("new content moved the reader: %v then %v", before, after)
		}
	}
	if v.Following() {
		t.Error("new content resumed following while the reader was scrolled up")
	}
}

// TestFollowingContinuesWhenTheReaderWasAtTheBottom, the other half of the rule: a reader at the tail keeps seeing the
// newest line as it arrives.
func TestFollowingContinuesWhenTheReaderWasAtTheBottom(t *testing.T) {
	t.Parallel()
	v := NewViewport(3)
	v.SetLines(numbered(10))
	v.SetLines(numbered(12))

	got := v.Visible()
	if got[len(got)-1] != "11" {
		t.Errorf("the newest line is %q, want 11", got[len(got)-1])
	}
	if !v.Following() {
		t.Error("a following viewport stopped following on new content")
	}
}

// TestScrollingBackToTheBottomResumesFollowing: arriving at the tail is the same request as tracking it.
func TestScrollingBackToTheBottomResumesFollowing(t *testing.T) {
	t.Parallel()
	v := NewViewport(3)
	v.SetLines(numbered(10))
	v.ScrollBy(-4)
	if v.Following() {
		t.Fatal("scrolled up and still following")
	}

	v.ScrollBy(4)
	if !v.Following() {
		t.Error("scrolling back to the tail did not resume following")
	}
	if got := v.Visible(); got[len(got)-1] != "9" {
		t.Errorf("the window ends at %q, want the last line", got[len(got)-1])
	}
}

// TestHomeAndEndBracketTheContent, and Home cannot scroll past the top into blank space.
func TestHomeAndEndBracketTheContent(t *testing.T) {
	t.Parallel()
	v := NewViewport(3)
	v.SetLines(numbered(10))

	v.Home()
	if got := v.Visible(); got[0] != "0" {
		t.Errorf("Home shows %v, want the first line at the top", got)
	}

	v.End()
	if !v.Following() {
		t.Error("End did not resume following")
	}
	if got := v.Visible(); got[len(got)-1] != "9" {
		t.Errorf("End shows %v, want the last line", got)
	}
}

// TestPagingKeepsALineOfContext: a page jump that hid every line the reader was looking at would make navigation
// useless for reading.
func TestPagingKeepsALineOfContext(t *testing.T) {
	t.Parallel()
	v := NewViewport(4)
	v.SetLines(numbered(20))
	tail := v.Visible()

	v.PageUp()
	up := v.Visible()
	if up[len(up)-1] != tail[0] {
		t.Errorf("the page up ends at %q, want it to start where the previous page did (%q)", up[len(up)-1], tail[0])
	}

	v.PageDown()
	if !v.Following() {
		t.Error("paging back down to the tail did not resume following")
	}
}

// TestNavigationWorksWhileStreaming is the criterion's third clause, stated as one sequence: the reader pages up in the
// middle of an answer and keeps their place as it arrives.
func TestNavigationWorksWhileStreaming(t *testing.T) {
	t.Parallel()
	v := NewViewport(4)
	v.SetLines(numbered(10))
	v.PageUp()
	held := v.Visible()

	for grown := 11; grown <= 30; grown++ {
		v.SetLines(numbered(grown))
		if v.Following() {
			t.Fatalf("following resumed at %d lines while the reader was paged up", grown)
		}
	}

	after := v.Visible()
	for index := range held {
		if held[index] != after[index] {
			t.Fatalf("the reader lost their place: %v then %v", held, after)
		}
	}

	// And End still takes them to the newest line of the grown answer.
	v.End()
	if got := v.Visible(); got[len(got)-1] != "29" {
		t.Errorf("End shows %v", got)
	}
}

// TestAResizeKeepsTheReadersLine, because dragging a terminal taller is not a request to jump to the tail. The anchor is
// the top line, so a taller window shows more below it - which is what dragging a window open should do.
func TestAResizeKeepsTheReadersLine(t *testing.T) {
	t.Parallel()
	v := NewViewport(3)
	v.SetLines(numbered(20))
	v.ScrollBy(-5)
	top := v.Visible()[0]

	v.SetHeight(6)
	got := v.Visible()
	if got[0] != top {
		t.Errorf("the resize moved the reader's line from %q to %q", top, got[0])
	}
	if len(got) != 6 {
		t.Errorf("the window is %d lines after a resize to 6", len(got))
	}
}

// TestAShortAnswerLeavesNoBlankSpaceAbove: the oldest line sits at the top rather than floating in the middle.
func TestAShortAnswerLeavesNoBlankSpaceAbove(t *testing.T) {
	t.Parallel()
	v := NewViewport(10)
	v.SetLines(numbered(3))
	if got := v.Visible(); len(got) != 3 || got[0] != "0" {
		t.Errorf("showing %v, want the three lines it has", got)
	}
}

// TestPinnedBottomIsTheLastRow: the bottom region does not drift upwards as the history grows, which is what "pinned"
// means and what a status line reading "13s" would otherwise break.
func TestPinnedBottomIsTheLastRow(t *testing.T) {
	t.Parallel()
	for _, lines := range []int{0, 1, 5, 50} {
		v := NewViewport(6)
		v.SetLines(numbered(lines))

		frame := Compose(v.Visible(), "status", 6)
		if len(frame) != 6 {
			t.Fatalf("a frame of %d lines for %d lines of history", len(frame), lines)
		}
		if frame[len(frame)-1] != "status" {
			t.Errorf("the bottom row is %q, want the status line", frame[len(frame)-1])
		}
	}
}

// TestComposeWithoutABottomUsesTheWholeWindow, so the frame does not lose a row when there is nothing to pin.
func TestComposeWithoutABottomUsesTheWholeWindow(t *testing.T) {
	t.Parallel()
	frame := Compose([]string{"a", "b", "c", "d"}, "", 3)
	if len(frame) != 3 {
		t.Fatalf("the frame is %d lines", len(frame))
	}
	if frame[0] != "b" || frame[2] != "d" {
		t.Errorf("the frame is %v, want the newest three", frame)
	}
}
