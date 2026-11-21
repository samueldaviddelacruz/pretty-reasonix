package tui

// Viewport is the scrollable history above a pinned bottom region.
//
// The reader's place is an **anchor**: the index of the first visible line. That is the only model that satisfies both
// halves of the follow rule, and the first version of this file got it wrong by counting from the bottom. A
// bottom-relative offset keeps the *distance to the tail* fixed, so when the answer grows the window slides forward with
// it — the reader is carried away from what they were reading, which is precisely what "do not move the reader" exists
// to prevent. Anchoring to a line and clamping it keeps the line, and following is a separate fact rather than a value of
// the offset.
type Viewport struct {
	lines  []string
	height int
	// top is the index of the first visible line, when the reader is not following.
	top int
	// following means the window tracks the tail; top is then derived rather than stored.
	following bool
}

// NewViewport makes an empty viewport with room for height lines of history.
func NewViewport(height int) *Viewport {
	return &Viewport{height: max(height, 0), following: true}
}

// SetHeight resizes the window, keeping the reader where they were.
//
// The offset is clamped rather than reset: a resize is not a request to jump to the tail, and a terminal being dragged
// taller should not scroll somebody away from what they were reading.
func (v *Viewport) SetHeight(height int) {
	v.height = max(height, 0)
	v.clamp()
}

// Height is how many lines of history fit.
func (v *Viewport) Height() int { return v.height }

// SetLines replaces the content. A reader who was following stays following; one who was not stays exactly as far from
// the bottom as they were, which is what makes streaming answerable while somebody reads.
func (v *Viewport) SetLines(lines []string) {
	v.lines = lines
	v.clamp()
}

// Lines is how much content there is.
func (v *Viewport) Lines() int { return len(v.lines) }

// Top is the index of the first visible line, which is the reader's place.
func (v *Viewport) Top() int {
	if v.height == 0 || len(v.lines) == 0 {
		return 0
	}
	if v.following {
		return max(len(v.lines)-v.height, 0)
	}
	return v.top
}

// Following reports whether the window is pinned to the newest line.
func (v *Viewport) Following() bool { return v.following }

// Visible is the window: at most height lines, ending no later than the last line.
func (v *Viewport) Visible() []string {
	if v.height == 0 || len(v.lines) == 0 {
		return nil
	}
	start := v.Top()
	end := min(start+v.height, len(v.lines))
	return v.lines[start:end]
}

// ScrollBy moves the window by delta lines, positive towards newer content — the way the screen moves when you scroll
// down.
//
// Arriving at the last line resumes following, because that is the same request as tracking the tail. Any other movement
// stops: a reader who scrolled away asked to be left where they are.
func (v *Viewport) ScrollBy(delta int) {
	// Anchored from the current position first, so moving out of a following state starts where the reader could see
	// rather than from zero.
	v.top = v.Top() + delta
	v.following = false
	v.clamp()
	if v.top >= max(len(v.lines)-v.height, 0) {
		v.following = true
	}
}

// PageUp and PageDown move by a window less one line, so a reader keeps a line of context across the jump.
func (v *Viewport) PageUp() { v.ScrollBy(-max(v.height-1, 1)) }

// PageDown moves a window less one line towards newer content.
//
// Both pages move at least one line, because a zero-height window would otherwise turn PageUp into a scroll *down*.
func (v *Viewport) PageDown() { v.ScrollBy(max(v.height-1, 1)) }

// Home goes to the oldest content.
func (v *Viewport) Home() { v.following, v.top = false, 0 }

// End returns to the tail and resumes following.
func (v *Viewport) End() { v.following, v.top = true, 0 }

// clamp keeps the anchor inside the content.
//
// A following viewport needs no clamping: its top is derived from the end. A reader who is not following keeps their line
// unless it no longer exists, which is the one case where their place genuinely cannot be honoured.
func (v *Viewport) clamp() {
	if v.following {
		v.top = max(len(v.lines)-v.height, 0)
		return
	}
	v.top = max(v.top, 0)
	furthest := max(len(v.lines)-v.height, 0)
	if v.top >= furthest {
		// Landed on the tail — by shrinking content, growing the window, or scrolling there. Following is a separate fact
		// and has to agree: a viewport whose anchor is the bottom but which is not following would refuse to track the
		// next line, which is exactly what the follow rule forbids.
		v.top, v.following = furthest, true
	}
}

// Compose stacks the scrollable history above a pinned bottom region.
//
// The bottom region is whatever the caller hands over — the status line now, the composer too once E11 adds it — and it
// is not part of the viewport's arithmetic: a pinned region that could scroll is not pinned.
func Compose(visible []string, bottom string, height int) []string {
	if height <= 0 {
		return nil
	}
	room := height
	if bottom != "" {
		room = height - 1
	}

	// `visible` is already the window the viewport chose, so re-slicing it here would discard the line the reader is
	// anchored to — and would do it every frame, since the window is normally full. Short history is padded *above*
	// itself so the oldest line sits at the top of the frame rather than drifting down it.
	out := make([]string, 0, height)
	for len(out) < room-len(visible) {
		out = append(out, "")
	}
	out = append(out, visible...)
	if len(out) > room {
		out = out[len(out)-room:]
	}
	// Every row filled: a frame shorter than the terminal leaves the previous one showing underneath it.
	for len(out) < room {
		out = append(out, "")
	}
	if bottom != "" {
		out = append(out, bottom)
	}
	return out
}
