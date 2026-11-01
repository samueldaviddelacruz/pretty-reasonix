package builtin_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pretty-reasonix/internal/contract/tool"
	"pretty-reasonix/internal/tool/builtin"
)

// workspace builds a tree and returns an os.Root over it, which is how every tool is confined.
func workspace(t *testing.T) (*os.Root, string) {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"main.go":               "package main\n\nfunc main() {}\n",
		"internal/a.go":         "package internal\n\nvar A = 1\n",
		"internal/deep/b.go":    "package deep\n\nvar B = 2\n",
		"node_modules/noise.js": "var noise = true\n",
		".git/config":           "[core]\n",
		"README.md":             "# title\n",
		"binary.bin":            "\x00\x01\x02",
	}
	for name, body := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return root, dir
}

func run(t *testing.T, subject tool.Tool, args string) tool.Result {
	t.Helper()
	result, err := subject.Execute(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s: %v", subject.Name(), err)
	}
	return result
}

// TestReadFileNumbersItsLines: the numbering is what lets a model cite a line or ask for a range, and without
// it the model has to count.
func TestReadFileNumbersItsLines(t *testing.T) {
	t.Parallel()
	root, _ := workspace(t)
	result := run(t, builtin.NewReadFile(root), `{"path":"main.go"}`)

	lines := strings.Split(strings.TrimRight(result.Output, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("%d lines, want 3:\n%s", len(lines), result.Output)
	}
	if !strings.HasPrefix(lines[0], "     1→package main") {
		t.Errorf("the first line is %q, want it numbered", lines[0])
	}
	if result.Truncated {
		t.Error("a short file was reported truncated")
	}
}

// TestReadFileHonoursOffsetAndLimit: a range, which is the follow-up the numbering makes possible.
func TestReadFileHonoursOffsetAndLimit(t *testing.T) {
	t.Parallel()
	root, _ := workspace(t)
	result := run(t, builtin.NewReadFile(root), `{"path":"main.go","offset":1,"limit":1}`)

	if !strings.Contains(result.Output, "     2→") {
		t.Errorf("offset 1 did not skip the first line:\n%s", result.Output)
	}
	if strings.Contains(result.Output, "package main") || strings.Contains(result.Output, "func main") {
		t.Errorf("the limit was not applied:\n%s", result.Output)
	}
	if !result.Truncated {
		t.Error("a limited read was not reported truncated, so the model believes it saw the end")
	}
}

// TestReadFileReportsAMissingFileAsAFact: a missing file is the world, not a broken tool, so the turn
// continues and the model can try another path.
func TestReadFileReportsAMissingFileAsAFact(t *testing.T) {
	t.Parallel()
	root, _ := workspace(t)
	result, err := builtin.NewReadFile(root).Execute(context.Background(), json.RawMessage(`{"path":"nope.txt"}`))
	if err != nil {
		t.Fatalf("a missing file was reported as a broken tool: %v", err)
	}
	if result.Err == nil {
		t.Error("a missing file produced neither output nor an error")
	}
}

// TestReadFileCannotEscapeTheRoot is what os.Root buys: a path with .. in it does not reach the parent.
func TestReadFileCannotEscapeTheRoot(t *testing.T) {
	t.Parallel()
	root, dir := workspace(t)
	secret := filepath.Join(filepath.Dir(dir), "outside.txt")
	if err := os.WriteFile(secret, []byte("do not read me"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(secret) })

	result := run(t, builtin.NewReadFile(root), `{"path":"../outside.txt"}`)
	if strings.Contains(result.Output, "do not read me") {
		t.Fatalf("a path escaped the workspace root:\n%s", result.Output)
	}
}

// TestLsSkipsNoise: version control and dependency directories are noise for every question a model asks.
func TestLsSkipsNoise(t *testing.T) {
	t.Parallel()
	root, _ := workspace(t)
	result := run(t, builtin.NewLs(root), `{}`)

	for _, absent := range []string{"node_modules", ".git"} {
		if strings.Contains(result.Output, absent) {
			t.Errorf("ls listed %q:\n%s", absent, result.Output)
		}
	}
	for _, present := range []string{"main.go", "internal/", "README.md"} {
		if !strings.Contains(result.Output, present) {
			t.Errorf("ls did not list %q:\n%s", present, result.Output)
		}
	}
	if !sortd(result.Output) {
		t.Errorf("ls did not sort:\n%s", result.Output)
	}
}

// TestGlobMatchesAnyDepth: ** is what makes glob useful for a model that does not know where a file lives.
func TestGlobMatchesAnyDepth(t *testing.T) {
	t.Parallel()
	root, _ := workspace(t)
	result := run(t, builtin.NewGlob(root), `{"pattern":"**/*.go"}`)

	for _, want := range []string{"main.go", "internal/a.go", "internal/deep/b.go"} {
		if !strings.Contains(result.Output, want) {
			t.Errorf("glob missed %q:\n%s", want, result.Output)
		}
	}
	if strings.Contains(result.Output, "noise.js") {
		t.Errorf("glob searched node_modules:\n%s", result.Output)
	}
}

// TestGrepFindsLinesAndSkipsBinaries: a match inside a binary is noise the model cannot use.
func TestGrepFindsLinesAndSkipsBinaries(t *testing.T) {
	t.Parallel()
	root, _ := workspace(t)
	result := run(t, builtin.NewGrep(root), `{"pattern":"var [AB]"}`)

	if !strings.Contains(result.Output, "internal/a.go:3:") {
		t.Errorf("grep did not report a match with its line:\n%s", result.Output)
	}
	if !strings.Contains(result.Output, "internal/deep/b.go:3:") {
		t.Errorf("grep did not search deeply:\n%s", result.Output)
	}
	if strings.Contains(result.Output, "binary.bin") {
		t.Errorf("grep searched a binary file:\n%s", result.Output)
	}
	if strings.Contains(result.Output, "noise.js") {
		t.Errorf("grep searched node_modules:\n%s", result.Output)
	}
}

// TestGrepCanRestrictToAPath, so a model that knows where to look does not pay to look everywhere.
func TestGrepCanRestrictToAPath(t *testing.T) {
	t.Parallel()
	root, _ := workspace(t)
	result := run(t, builtin.NewGrep(root), `{"pattern":"var B","path":"internal/deep"}`)
	if !strings.Contains(result.Output, "b.go") {
		t.Errorf("the restricted search found nothing:\n%s", result.Output)
	}
	if strings.Contains(result.Output, "a.go") {
		t.Errorf("the search left its directory:\n%s", result.Output)
	}
}

// TestTheToolsDeclareThemselvesReadOnly is what lets them share a parallel segment. A read-only tool that
// forgot to say so would run serially forever and nothing would report it.
func TestTheToolsDeclareThemselvesReadOnly(t *testing.T) {
	t.Parallel()
	root, _ := workspace(t)
	for _, subject := range []tool.Tool{
		builtin.NewReadFile(root), builtin.NewLs(root), builtin.NewGlob(root), builtin.NewGrep(root),
	} {
		readOnly, ok := subject.(tool.ReadOnly)
		if !ok || !readOnly.ReadOnly() {
			t.Errorf("%s does not declare itself read-only, so it will never run in parallel", subject.Name())
		}
		if len(subject.Schema()) == 0 || subject.Description() == "" {
			t.Errorf("%s has no schema or no description, so the model cannot call it", subject.Name())
		}
	}
}

// TestEveryToolCanBeCalled: a tool whose Execute rejects the arguments its schema describes is unusable.
func TestEveryToolCanBeCalled(t *testing.T) {
	t.Parallel()
	root, _ := workspace(t)
	cases := map[string]struct {
		subject tool.Tool
		args    string
	}{
		"read_file": {builtin.NewReadFile(root), `{"path":"README.md"}`},
		"ls":        {builtin.NewLs(root), `{"path":"internal"}`},
		"glob":      {builtin.NewGlob(root), `{"pattern":"*"}`},
		"grep":      {builtin.NewGrep(root), `{"pattern":"title"}`},
	}
	for name, tc := range cases {
		result, err := tc.subject.Execute(context.Background(), json.RawMessage(tc.args))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if result.Err != nil {
			t.Errorf("%s reported %v", name, result.Err)
		}
	}
}

// sortd reports whether lines are in order, without importing sort just to check what sort did.
func sortd(s string) bool {
	lines := strings.Split(s, "\n")
	for i := 1; i < len(lines); i++ {
		if lines[i-1] > lines[i] {
			return false
		}
	}
	return true
}
