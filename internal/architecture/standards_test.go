package architecture_test

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// standardLinters is the set `.golangci.yml` starts from with
// `linters.default: standard`, so it is pinned in the config even though it is
// not named in the enable list.
var standardLinters = []string{"errcheck", "govet", "ineffassign", "staticcheck", "unused"}

var (
	ruleDefinition = regexp.MustCompile(`\*\*\[#(\d+)\]`)
	ruleCitation   = regexp.MustCompile(`\[#(\d+)\]`)
	linterName     = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
)

// TestLintersPinnedInConfigStillExist is the standards test promised in
// docs/TECHNICAL_DESIGN.md §7.2, and the one thing that stops a toolchain bump
// silently dropping an enforcement.
//
// A linter that disappears from golangci-lint does not make the gate fail; it
// makes the gate narrower, and a narrower gate still reports success. That is a
// worse failure than a red build, so it is checked rather than assumed.
func TestLintersPinnedInConfigStillExist(t *testing.T) {
	t.Parallel()
	linter := golangciLint(t)
	root := moduleRoot(t)

	pinned := append(enabledLinters(t, root), standardLinters...)
	sort.Strings(pinned)

	available := availableLinters(t, linter)
	if len(available) < 50 {
		t.Fatalf("only %d linters reported by `%s help linters`; the parse in availableLinters is wrong, and every assertion below would pass on an empty set", len(available), linter)
	}

	for _, name := range pinned {
		deprecated, ok := available[name]
		switch {
		case !ok:
			t.Errorf("%s is pinned in .golangci.yml but is not a linter this golangci-lint provides — the gate no longer enforces it", name)
		case deprecated:
			t.Errorf("%s is pinned in .golangci.yml but is marked deprecated — it will disappear at the next major version, so replace it now instead of then", name)
		}
	}
}

// TestCitedRulesExistInTheBaseline checks the other half of the §7.2 promise:
// every rule number cited by .golangci.yml or by docs/GO-STANDARDS.md resolves
// to a rule that actually exists in go-best-practices.md. A citation to a rule
// that was renumbered is a claim nobody can follow.
//
// This half needs no linter, so unlike the test above it never skips.
func TestCitedRulesExistInTheBaseline(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)

	defined := map[string]bool{}
	for _, match := range ruleDefinition.FindAllStringSubmatch(readFile(t, filepath.Join(root, "go-best-practices.md")), -1) {
		defined[match[1]] = true
	}
	if len(defined) != 100 {
		t.Fatalf("go-best-practices.md defines %d numbered rules; the baseline is meant to hold 100, so either the file changed or this parse is wrong", len(defined))
	}

	for _, rel := range []string{".golangci.yml", "docs/GO-STANDARDS.md"} {
		body := readFile(t, filepath.Join(root, filepath.FromSlash(rel)))
		var cited []string
		for _, match := range ruleCitation.FindAllStringSubmatch(body, -1) {
			cited = append(cited, match[1])
		}
		if len(cited) == 0 {
			t.Errorf("%s cites no rules at all, which cannot be right", rel)
		}
		for _, n := range cited {
			if !defined[n] {
				t.Errorf("%s cites [#%s], which is not a rule in go-best-practices.md", rel, n)
			}
		}
	}
}

// enabledLinters reads the names out of `.golangci.yml`'s `linters.enable` list.
//
// It extracts rather than parses YAML, deliberately: a YAML library would be a
// dependency (docs/DEPENDENCIES.md), and the shape of this one file is ours. The
// count assertion is what keeps a focused extraction honest — if the block is
// ever reshaped, this fails loudly instead of quietly reading nothing.
func enabledLinters(t *testing.T, root string) []string {
	t.Helper()
	lines := strings.Split(readFile(t, filepath.Join(root, ".golangci.yml")), "\n")

	enableAt := -1
	inLinters := false
	for i, line := range lines {
		if line == "linters:" {
			inLinters = true
			continue
		}
		if inLinters && line != "" && !strings.HasPrefix(line, " ") {
			inLinters = false // the next top-level key ends the section
		}
		if inLinters && line == "  enable:" {
			enableAt = i
			break
		}
	}
	if enableAt < 0 {
		t.Fatal(".golangci.yml has no `  enable:` line under `linters:`; this extraction needs updating")
	}

	var names []string
	for _, line := range lines[enableAt+1:] {
		entry := strings.TrimSpace(line)
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}
		if !strings.HasPrefix(line, "    - ") {
			break
		}
		name := strings.Fields(strings.TrimPrefix(line, "    - "))[0]
		names = append(names, name)
	}
	if len(names) < 50 {
		t.Fatalf("only %d enabled linters found in .golangci.yml; the list has probably been reshaped and this test is no longer reading it", len(names))
	}
	return names
}

// availableLinters parses `golangci-lint help linters` into name -> deprecated.
//
// The output is `name: description` per line, with a `[deprecated]` marker before
// the colon for the ones on their way out, grouped under section headings.
func availableLinters(t *testing.T, linter string) map[string]bool {
	t.Helper()
	output := runLinter(t, linter, "help", "linters")
	out := map[string]bool{}
	for line := range strings.SplitSeq(output, "\n") {
		before, _, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(before)
		if len(fields) == 0 || !linterName.MatchString(fields[0]) {
			continue
		}
		out[fields[0]] = strings.Contains(before, "[deprecated]")
	}
	return out
}

// runLinter runs the linter and returns its output, failing on an error or on
// empty output so that no assertion built from it can pass on nothing.
func runLinter(t *testing.T, linter string, args ...string) string {
	t.Helper()
	if len(args) == 0 {
		t.Fatal("runLinter needs arguments")
	}
	output, err := runBounded(t, linter, "", args...)
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", linter, strings.Join(args, " "), err, output)
	}
	if strings.TrimSpace(output) == "" {
		t.Fatalf("%s %s produced no output, so nothing can be asserted from it", linter, strings.Join(args, " "))
	}
	return output
}
