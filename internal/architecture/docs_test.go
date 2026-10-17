package architecture_test

import (
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The design documents are part of the deliverable rather than commentary on it: the backlog is
// where the tickets come from, and the epic map in the design is what says how many there are. A
// drift between the two is a defect nothing else catches, and these checks have caught four of
// them in this project — including a ticket that depended on a later milestone, found before the
// issues existed. They were a scratch script until a temp-directory rotation deleted it mid
// session, which is the argument for them living here.

var (
	// ticketHeadingRE matches the heading line of a ticket definition.
	ticketHeadingRE = regexp.MustCompile(`(?m)^- \*\*(E\d+-T\d+) · `)
	// epicMapRowRE matches one row of the epic map's table.
	epicMapRowRE = regexp.MustCompile(`(?m)^\| \*\*(E\d+)\*\* \|[^|]*\|[^|]*\| (\d+) \|`)
	// epicSectionRE matches the heading of an epic's own section in the backlog.
	epicSectionRE = regexp.MustCompile(`(?m)^### (E\d+) · `)
	// roadmapStubRE matches a heading in the roadmap tier, which is not a v1 ticket.
	roadmapStubRE = regexp.MustCompile(`(?m)^### (R1-\d+) · `)
	// ticketRefRE matches any reference to a ticket, anywhere in prose.
	ticketRefRE = regexp.MustCompile(`E\d+-T\d+`)
	// ticketMilestoneRE captures the milestone on a ticket's heading line.
	ticketMilestoneRE = regexp.MustCompile(`milestone:(m\d)`)
	// ticketDepsRE captures the dependencies segment of a heading line.
	ticketDepsRE = regexp.MustCompile(`deps: ([^·]*)`)
	// ticketIDOnlyRE confirms a fragment is a ticket ID and not another kind of bullet.
	ticketIDOnlyRE = regexp.MustCompile(`^E\d+-T\d+$`)
	// acceptanceCriterionRE matches one acceptance criterion within a ticket.
	acceptanceCriterionRE = regexp.MustCompile(`(?m)^  - AC:`)
	// readmeCountRE captures the ticket count README.md advertises.
	readmeCountRE = regexp.MustCompile(`(\d+) v1 tickets`)

	// declaredMilestones are the milestones the design declares, in order. A ticket may not
	// depend on a later one: the ordering is what makes the backlog workable as a sequence, so
	// a backward edge is a planning defect rather than a stylistic one.
	declaredMilestones = []string{"m0", "m1", "m2", "m3", "m4", "m5"}
)

// theDocs reads the four documents these checks work on, in a fixed order so a failure names
// the same file every run.
func theDocs(t *testing.T) map[string]string {
	t.Helper()
	root := moduleRoot(t)
	names := []string{
		"README.md",
		"docs/TECHNICAL_DESIGN.md",
		"docs/APPENDIX-A-REASONIX-REFERENCE.md",
		"docs/APPENDIX-B-BACKLOG.md",
	}
	docs := make(map[string]string, len(names))
	for _, name := range names {
		body := readFile(t, filepath.Join(root, filepath.FromSlash(name)))
		if len(body) < 2000 {
			t.Fatalf("%s is %d bytes, which means a write was truncated rather than concise", name, len(body))
		}
		docs[name] = body
	}
	return docs
}

// ticketHeadings returns the heading line of every ticket definition, keyed by ticket ID.
func ticketHeadings(backlog string) map[string]string {
	headings := make(map[string]string, 200)
	for line := range strings.SplitSeq(backlog, "\n") {
		if match := ticketHeadingRE.FindStringSubmatch(line); match != nil {
			headings[match[1]] = line
		}
	}
	return headings
}

// ticketsByEpic groups the defined ticket IDs by epic, in document order.
func ticketsByEpic(backlog string) map[string][]string {
	byEpic := make(map[string][]string)
	for _, match := range ticketHeadingRE.FindAllStringSubmatch(backlog, -1) {
		epic, _, _ := strings.Cut(match[1], "-T")
		byEpic[epic] = append(byEpic[epic], match[1])
	}
	return byEpic
}

// dependenciesOf reads the ticket IDs a heading line declares as its dependencies.
func dependenciesOf(heading string) []string {
	segment := ticketDepsRE.FindStringSubmatch(heading)
	if segment == nil {
		return nil
	}
	return ticketRefRE.FindAllString(segment[1], -1)
}

// milestoneOf reads the milestone a heading line declares.
func milestoneOf(heading string) string {
	match := ticketMilestoneRE.FindStringSubmatch(heading)
	if match == nil {
		return ""
	}
	return match[1]
}

// milestoneRank turns a milestone name into its position, or -1 when it is not declared.
func milestoneRank(milestone string) int {
	return slices.Index(declaredMilestones, milestone)
}

// TestTheEpicMapMatchesTheBacklog is the check that has caught count drift three times. It
// compares per epic rather than in total, so an epic that gained a ticket without the map
// being updated fails here instead of quietly making the totals wrong.
func TestTheEpicMapMatchesTheBacklog(t *testing.T) {
	t.Parallel()
	docs := theDocs(t)

	declared := make(map[string]int)
	for _, match := range epicMapRowRE.FindAllStringSubmatch(docs["docs/TECHNICAL_DESIGN.md"], -1) {
		count, err := strconv.Atoi(match[2])
		if err != nil {
			t.Fatalf("the epic map's %s row has an unreadable count %q: %v", match[1], match[2], err)
		}
		declared[match[1]] = count
	}
	if len(declared) < 20 {
		t.Fatalf("only %d epics were read out of the map, so the pattern no longer matches it", len(declared))
	}

	byEpic := ticketsByEpic(docs["docs/APPENDIX-B-BACKLOG.md"])
	for _, epic := range slices.Sorted(maps.Keys(declared)) {
		if got := len(byEpic[epic]); got != declared[epic] {
			t.Errorf("%s: the epic map says %d tickets, the backlog defines %d", epic, declared[epic], got)
		}
	}
	for _, epic := range slices.Sorted(maps.Keys(byEpic)) {
		if _, known := declared[epic]; !known {
			t.Errorf("%s has tickets in the backlog and no row in the epic map", epic)
		}
	}
}

// TestTheReadmeCountMatchesTheBacklog ties the number a reader is told to the number that
// exists. Hard-coding the expected count in this test would have the same staleness problem it
// is meant to catch, so it is read from the README instead.
func TestTheReadmeCountMatchesTheBacklog(t *testing.T) {
	t.Parallel()
	docs := theDocs(t)

	match := readmeCountRE.FindStringSubmatch(docs["README.md"])
	if match == nil {
		t.Fatal("README.md does not advertise a ticket count, so nothing states the size of the backlog")
	}
	advertised, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatalf("the advertised count %q is not a number: %v", match[1], err)
	}
	if defined := len(ticketHeadings(docs["docs/APPENDIX-B-BACKLOG.md"])); defined != advertised {
		t.Errorf("README.md advertises %d v1 tickets and the backlog defines %d", advertised, defined)
	}
}

// TestEveryTicketReferenceResolves catches the commonest documentation slip: a ticket cited from
// a commit message, a comment or another ticket's dependencies that was renamed or never written.
func TestEveryTicketReferenceResolves(t *testing.T) {
	t.Parallel()
	docs := theDocs(t)

	headings := ticketHeadings(docs["docs/APPENDIX-B-BACKLOG.md"])
	references := make(map[string]bool)
	for _, body := range docs {
		for _, ref := range ticketRefRE.FindAllString(body, -1) {
			references[ref] = true
		}
	}
	for _, ref := range slices.Sorted(maps.Keys(references)) {
		if _, defined := headings[ref]; !defined {
			t.Errorf("%s is referenced but never defined", ref)
		}
	}
}

// TestEveryTicketIsComplete is what keeps the backlog usable as a work queue: a ticket with no
// size cannot be planned and one with no acceptance criterion cannot be finished.
func TestEveryTicketIsComplete(t *testing.T) {
	t.Parallel()
	backlog := theDocs(t)["docs/APPENDIX-B-BACKLOG.md"]

	// Splitting on the bullet marker separates one ticket's text from the next, so a missing
	// field cannot be satisfied by a neighbour's.
	for _, piece := range strings.Split(backlog, "\n- **")[1:] {
		id, body, found := strings.Cut(piece, " · ")
		if !found || !ticketIDOnlyRE.MatchString(id) {
			continue // a bullet that is not a ticket
		}
		if !strings.Contains(body, "`size:") {
			t.Errorf("%s has no size, so it cannot be planned", id)
		}
		if acceptanceCriterionRE.FindStringSubmatch(body) == nil {
			t.Errorf("%s has no acceptance criterion, so it cannot be finished", id)
		}
	}
}

// TestEveryEpicIsAContiguousSequence keeps ticket numbering meaningful: E3-T7 must imply there
// are six before it.
func TestEveryEpicIsAContiguousSequence(t *testing.T) {
	t.Parallel()
	byEpic := ticketsByEpic(theDocs(t)["docs/APPENDIX-B-BACKLOG.md"])

	for _, epic := range slices.Sorted(maps.Keys(byEpic)) {
		numbers := make([]int, 0, len(byEpic[epic]))
		for _, id := range byEpic[epic] {
			_, digits, _ := strings.Cut(id, "-T")
			number, err := strconv.Atoi(digits)
			if err != nil {
				t.Fatalf("%s has an unreadable number %q: %v", id, digits, err)
			}
			numbers = append(numbers, number)
		}
		slices.Sort(numbers)
		for i, number := range numbers {
			if number != i+1 {
				t.Errorf("%s is numbered T%v, want T1..T%d with no gaps", epic, numbers, len(numbers))
				break
			}
		}
	}
}

// TestEveryEpicHasASection checks the map and the backlog describe the same set of epics, in
// both directions — a ticket under an epic the design has never heard of is as wrong as an epic
// with no tickets.
func TestEveryEpicHasASection(t *testing.T) {
	t.Parallel()
	docs := theDocs(t)

	sections := make(map[string]bool)
	for _, match := range epicSectionRE.FindAllStringSubmatch(docs["docs/APPENDIX-B-BACKLOG.md"], -1) {
		sections[match[1]] = true
	}
	declared := make(map[string]bool)
	for _, match := range epicMapRowRE.FindAllStringSubmatch(docs["docs/TECHNICAL_DESIGN.md"], -1) {
		declared[match[1]] = true
	}

	for _, epic := range slices.Sorted(maps.Keys(declared)) {
		if !sections[epic] {
			t.Errorf("%s is in the epic map and has no section in the backlog", epic)
		}
	}
	for _, epic := range slices.Sorted(maps.Keys(sections)) {
		if !declared[epic] {
			t.Errorf("%s has a section in the backlog and no row in the epic map", epic)
		}
	}
}

// TestTheDependencyGraphIsSound checks the three properties a work queue needs: every edge
// lands on something, no ticket waits on a later milestone, and nothing waits on itself in a
// loop. The milestone rule caught a real defect before the issues were filed.
func TestTheDependencyGraphIsSound(t *testing.T) {
	t.Parallel()
	headings := ticketHeadings(theDocs(t)["docs/APPENDIX-B-BACKLOG.md"])
	ordered := slices.Sorted(maps.Keys(headings))

	for _, id := range ordered {
		for _, dep := range dependenciesOf(headings[id]) {
			if _, defined := headings[dep]; !defined {
				t.Errorf("%s depends on %s, which is not defined", id, dep)
			}
		}
	}

	for _, id := range ordered {
		mine := milestoneRank(milestoneOf(headings[id]))
		if mine < 0 {
			t.Errorf("%s has no declared milestone, so it cannot be placed in the sequence", id)
			continue
		}
		for _, dep := range dependenciesOf(headings[id]) {
			theirs := milestoneRank(milestoneOf(headings[dep]))
			if theirs > mine {
				t.Errorf("%s (m%d) depends on %s (m%d), which is scheduled later", id, mine, dep, theirs)
			}
		}
	}

	const (
		unvisited = iota
		onPath
		settled
	)
	state := make(map[string]int, len(headings))
	var walk func(id string) bool
	walk = func(id string) bool {
		switch state[id] {
		case onPath:
			t.Errorf("%s is part of a dependency cycle", id)
			return false
		case settled:
			return true
		}
		state[id] = onPath
		healthy := true
		for _, dep := range dependenciesOf(headings[id]) {
			if _, defined := headings[dep]; !defined {
				continue
			}
			if !walk(dep) {
				healthy = false
			}
		}
		state[id] = settled
		return healthy
	}
	for _, id := range ordered {
		walk(id)
	}
}

// TestTheRoadmapTierIsWellFormed: a roadmap stub is a heading rather than a ticket, so it is
// excluded from every count above — which only holds if its numbering is its own contiguous
// sequence and cannot be confused with a ticket ID.
func TestTheRoadmapTierIsWellFormed(t *testing.T) {
	t.Parallel()
	backlog := theDocs(t)["docs/APPENDIX-B-BACKLOG.md"]

	stubs := roadmapStubRE.FindAllStringSubmatch(backlog, -1)
	if len(stubs) < 10 {
		t.Fatalf("only %d roadmap stubs were found, so the pattern no longer matches them", len(stubs))
	}
	numbers := make([]int, 0, len(stubs))
	for _, stub := range stubs {
		_, digits, _ := strings.Cut(stub[1], "-")
		number, err := strconv.Atoi(digits)
		if err != nil {
			t.Fatalf("%s has an unreadable number: %v", stub[1], err)
		}
		numbers = append(numbers, number)
	}
	slices.Sort(numbers)
	for i, number := range numbers {
		if number != i+1 {
			t.Errorf("the roadmap is numbered %v, want R1-1..R1-%d with no gaps", numbers, len(numbers))
			break
		}
	}
}

// TestOnlyDeclaredMilestonesAreUsed keeps a typo out of the one field the ordering above trusts:
// an unknown milestone ranks -1, which would silently exempt that ticket from the check.
func TestOnlyDeclaredMilestonesAreUsed(t *testing.T) {
	t.Parallel()
	backlog := theDocs(t)["docs/APPENDIX-B-BACKLOG.md"]

	found := make(map[string]bool)
	for _, match := range ticketMilestoneRE.FindAllStringSubmatch(backlog, -1) {
		found[match[1]] = true
	}
	for _, milestone := range slices.Sorted(maps.Keys(found)) {
		if milestoneRank(milestone) < 0 {
			t.Errorf("milestone %q is used and not declared", milestone)
		}
	}
}

// TestTheScopeDecisionsAreRecorded: the four decisions that define this project are the reason
// the design is what it is, so a rewrite that drops one loses the argument for the whole shape.
func TestTheScopeDecisionsAreRecorded(t *testing.T) {
	t.Parallel()
	design := theDocs(t)["docs/TECHNICAL_DESIGN.md"]

	for _, decision := range []string{"D1", "D2", "D3", "D4"} {
		if !strings.Contains(design, "| "+decision+" |") {
			t.Errorf("%s is no longer recorded in the design", decision)
		}
	}
}

// TestTheDocumentsCarryNoPlaceholders is a cheap guard against a half-finished write being left
// in the tree. APPEND matches APPENDIX and TODO appears in the hygiene rules by design, so the
// sentinel set is the four that cannot be legitimate.
func TestTheDocumentsCarryNoPlaceholders(t *testing.T) {
	t.Parallel()
	docs := theDocs(t)

	for _, name := range slices.Sorted(maps.Keys(docs)) {
		for _, sentinel := range []string{"<!--", "TBD", "FIXME", "XXX"} {
			if strings.Contains(docs[name], sentinel) {
				t.Errorf("%s contains %q, which is a placeholder rather than content", name, sentinel)
			}
		}
	}
}
