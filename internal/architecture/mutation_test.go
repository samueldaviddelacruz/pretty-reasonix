//go:build mutation

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

// mutant is one deliberate break and the test that must notice it.
type mutant struct {
	name     string // what the break is, for the report
	pkg      string // the package whose test must go red, e.g. "internal/model"
	source   string // the file to break, relative to the module root
	old, new string // the exact text to substitute
	mustFail string // the -run pattern of the test that must go red
}

var mutants = []mutant{
	{
		name:     "the drop notice reports the wrong code",
		pkg:      "internal/contract/event",
		source:   "internal/contract/event/sink.go",
		old:      "event.dropped",
		new:      "event.lost",
		mustFail: "TestADroppedEventIsReportedWithItsCount",
	},
	{
		name:     "truncation stops recognising two vendors",
		pkg:      "internal/model",
		source:   "internal/model/usage.go",
		old:      `case "length", "max_tokens", "incomplete":`,
		new:      `case "length":`,
		mustFail: "TestTruncationRecognisesEveryVendorsWord",
	},
	{
		name:     "the documented context window becomes a guess",
		pkg:      "internal/model",
		source:   "internal/model/resolve.go",
		old:      "DefaultContextWindow = 160000",
		new:      "DefaultContextWindow = 1",
		mustFail: "TestTheWindowFollowsTheDocumentedChain",
	},
}

// TestNoMutantSurvives ensures that each mutant causes the expected test to fail.
// It must not run in parallel because the mutations modify files on disk.
func TestNoMutantSurvives(t *testing.T) {
	for _, m := range mutants {
		t.Run(m.name, func(t *testing.T) {

			root := moduleRoot(t)
			path := filepath.Join(root, m.source)

			orig, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read source file: %v", err)
			}
			origStr := string(orig)

			if !strings.Contains(origStr, m.old) {
				t.Fatalf("mutant no longer applies: %q not found in %s", m.old, m.source)
			}

			mutated := strings.Replace(origStr, m.old, m.new, 1)

			t.Cleanup(func() {
				if err := os.WriteFile(path, orig, 0o644); err != nil {
					t.Errorf("failed to restore original file: %v", err)
				}
			})

			if err := os.WriteFile(path, []byte(mutated), 0o644); err != nil {
				t.Fatalf("failed to write mutated file: %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "-run", m.mustFail, "./"+m.pkg)
			cmd.Dir = root
			output, err := cmd.CombinedOutput()

			if err == nil {
				// The output is the whole value of this message: what a reader needs is evidence that
				// the test ran and passed, rather than failed to run.
				t.Errorf("mutant survived: %s — %s still passes with the code broken:\n%s", m.name, m.mustFail, output)
			}

			if err := os.WriteFile(path, orig, 0o644); err != nil {
				t.Errorf("failed to restore original file after subtest: %v", err)
			}

			restored, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read restored file: %v", err)
			}

			if string(restored) != origStr {
				t.Errorf("restore did not preserve original bytes")
			}
		})
	}
}

// moduleRoot returns the path to the module root by walking up from the current directory
// until a go.mod file is found.
func getModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	for {
		_, err := os.Stat(filepath.Join(dir, "go.mod"))
		if err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found in any parent directory")
		}
		dir = parent
	}
}
