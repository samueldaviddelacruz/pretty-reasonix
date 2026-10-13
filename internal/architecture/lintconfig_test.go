package architecture_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// subprocessTimeout bounds every subprocess these tests start, so a linter that
// hangs fails the test rather than hanging the whole suite.
const subprocessTimeout = 2 * time.Minute

// lintFixtures are written into a throwaway module so the real .golangci.yml can
// be watched producing, or not producing, findings for them.
//
// Together they are the experiment *and* its controls, which is the only way
// this test can avoid the failure it exists to prevent:
//
//   - internal/config prints on a path the allow-list does not cover. A finding
//     there is the positive control: it proves forbidigo actually ran.
//   - internal/frontend/cli prints *and* panics. The panic finding proves the
//     file was analysed, and that is what turns the absent print finding into
//     evidence that the allow-list fired, rather than evidence that the file was
//     skipped.
//   - cmd/lintfixture does both permitted things, covering the other half of the
//     allow-list's path pattern.
var lintFixtures = map[string]string{
	"internal/config/fixture.go": `package config

import "fmt"

// Print is deliberately outside the allow-list. This is the positive control:
// if this finding disappears, forbidigo stopped running and every other
// expectation in this test becomes meaningless.
func Print() {
	fmt.Println("not allowed on this path")
}
`,
	"internal/frontend/cli/fixture.go": `package cli

import "fmt"

// Print is allowed on this path and must produce no finding.
func Print() {
	fmt.Println("allowed on this path")
}

// Panic is not covered by the allow-list, so it must produce a finding — which
// doubles as the proof that this file was analysed at all.
func Panic() {
	panic("fixture")
}
`,
	"cmd/lintfixture/fixture.go": `package lintfixture

import "fmt"

// Both is allowed to print and to panic: cmd owns the process streams, and it is
// the only place a panic is a legitimate programmer error.
func Both() {
	fmt.Println("allowed under cmd")
	panic("also allowed under cmd")
}
`,
}

// lintExpectation is one claim about the configuration, stated as the call and
// the path it appears on.
var lintExpectations = []struct {
	path        string
	call        string
	wantFlagged bool
	why         string
}{
	{"internal/config/fixture.go", "fmt.Println", true,
		"the positive control: forbidigo must run, or nothing else here means anything"},
	{"internal/frontend/cli/fixture.go", "panic", true,
		"the witness: proves the file was analysed, so the next row is evidence and not silence"},
	{"internal/frontend/cli/fixture.go", "fmt.Println", false,
		"the allow-list suppresses the process-stream ban for internal/frontend"},
	{"cmd/lintfixture/fixture.go", "fmt.Println", false,
		"the allow-list suppresses it for cmd as well"},
	{"cmd/lintfixture/fixture.go", "panic", false,
		"the allow-list covers panic under cmd too"},
}

// TestForbidigoAllowListFires proves the path-scoped allow-list in
// .golangci.yml does what it claims.
//
// This is not pedantry. The first version of that config matched the *source
// line* rather than the message golangci-lint reports, so it parsed, it looked
// right, and it never once fired — and the only thing that caught it was running
// the linter against real code and reading the output. An exclusion that
// silently never matches is worse than no exclusion, because it reads as a
// guarantee.
//
// The linter is run against a temporary module rather than this one so the
// fixtures cannot break `make lint` or leak into a commit.
func TestForbidigoAllowListFires(t *testing.T) {
	t.Parallel()
	linter := golangciLint(t)
	root := moduleRoot(t)
	work := t.TempDir()

	// The config is copied in rather than referenced from outside the module, so
	// the exclude paths are resolved exactly as they are here.
	config := filepath.Join(work, ".golangci.yml")
	writeFile(t, config, readFile(t, filepath.Join(root, ".golangci.yml")))
	writeFile(t, filepath.Join(work, "go.mod"), "module lintfixture\n\ngo 1.27\n")
	for rel, body := range lintFixtures {
		writeFile(t, filepath.Join(work, filepath.FromSlash(rel)), body)
	}

	// --default=none --enable=forbidigo keeps this to the one linter under test,
	// which is also what keeps it fast enough to run on every commit.
	output, err := runBounded(t, linter, work, "run",
		"--config", ".golangci.yml",
		"--default=none", "--enable=forbidigo",
		"./...")
	if err != nil && strings.TrimSpace(output) == "" {
		t.Fatalf("running %s: %v", linter, err)
	}
	// A non-zero exit is expected — the positive control is a finding — so only
	// the output is inspected.
	lines := forbidigoLines(output)
	if len(lines) == 0 {
		t.Fatalf("the linter reported no forbidigo findings at all, so this test proves nothing.\nOutput:\n%s", output)
	}

	for _, tc := range lintExpectations {
		got := hasFinding(lines, tc.path, tc.call)
		switch {
		case tc.wantFlagged && !got:
			t.Errorf("%s should report %q (%s), but did not.\nFindings:\n\t%s",
				tc.path, tc.call, tc.why, strings.Join(lines, "\n\t"))
		case !tc.wantFlagged && got:
			t.Errorf("%s should NOT report %q (%s), but did.\nFindings:\n\t%s",
				tc.path, tc.call, tc.why, strings.Join(lines, "\n\t"))
		}
	}
}

// forbidigoLines keeps only the lines that are actually forbidigo findings.
func forbidigoLines(output string) []string {
	var out []string
	for line := range strings.SplitSeq(output, "\n") {
		if strings.Contains(line, "(forbidigo)") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

// hasFinding reports whether a finding names both the path and the call.
func hasFinding(lines []string, path, call string) bool {
	for _, line := range lines {
		if strings.Contains(line, path) && strings.Contains(line, call) {
			return true
		}
	}
	return false
}

// golangciLint locates the linter, honouring GOLANGCI_LINT so a machine that
// keeps it outside PATH (as this project's Makefile anticipates) can still run
// the test. Skipping is deliberate: a developer without the linter should not be
// blocked, and CI has it, where E1-T6 makes this part of the gate.
func golangciLint(t *testing.T) string {
	t.Helper()
	if path := os.Getenv("GOLANGCI_LINT"); path != "" {
		return path
	}
	path, err := exec.LookPath("golangci-lint")
	if err != nil {
		t.Skip("golangci-lint is not on PATH and GOLANGCI_LINT is unset; " +
			"this test needs the real linter to prove the allow-list fires")
	}
	return path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(body)
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating the directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// runBounded runs one subprocess with a timeout and returns its combined output
// and error. Every subprocess these tests start goes through here, so a linter
// that hangs fails the test rather than the suite.
func runBounded(t *testing.T, name, dir string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), subprocessTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	return string(output), err
}
