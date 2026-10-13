package architecture_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// identityConstRE pulls the values out of internal/config/identity.go, so this
// test never spells an identity string itself. That is not fussiness: a test that
// named them would be a second owner, and would keep passing after the values
// changed.
var identityConstRE = regexp.MustCompile(`(?m)^\s*([A-Z][A-Za-z0-9_]*)\s*=\s*"([^"]+)"`)

// allowedFiles name an identity string by necessity, each with its reason.
// Keeping this list short is the whole point: every entry is a place the string
// can drift out of sync with identity.go.
var allowedFiles = map[string]string{
	"internal/config/identity.go": "the single owner: this is where the values are defined",
	"README.md":                   "documentation the acceptance criterion exempts",
	"Makefile":                    "a build script cannot read a Go constant, so it names the binary it produces",
	"go.sum":                      "generated base64 hashes, in which three legal characters can land in sequence by coincidence",
}

// allowedPrefixes are the same, for whole directories.
var allowedPrefixes = map[string]string{
	"docs/": "documentation may name the thing it documents",
}

// TestIdentityStringsHaveOneOwner is E1-T8.
//
// Read the scope carefully, because it is a deliberate reading of the ticket:
// for Go files this scans **string literals**, not text, so a comment is free to
// say `prx doctor` and a literal is not. A comment cannot drift into behaviour,
// and policing prose would only make the prose vaguer. Non-Go files are scanned
// as text, because there the two are not separable.
func TestIdentityStringsHaveOneOwner(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	values := identityValues(t, root)
	ordered := namesByLengthDesc(values)

	var leaks []string
	scanned := 0
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			// Everything else, including .github, is scanned: the workflow and the
			// pull request template are files this repository owns like any other.
			if skippingDirs[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if _, ok := allowed(rel); ok {
			return nil
		}

		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++
		if !utf8.Valid(body) {
			return nil // a binary, so nothing to read
		}
		if strings.HasSuffix(rel, ".go") {
			found, parseErr := goLiteralLeaks(rel, body, values, ordered)
			if parseErr != nil {
				return parseErr
			}
			leaks = append(leaks, found...)
			return nil
		}
		leaks = append(leaks, textLeaks(rel, string(body), values, ordered)...)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("scanning %s: %v", root, walkErr)
	}

	// A guard against the scan silently covering nothing, which would make the
	// assertion below true for the wrong reason.
	if scanned < 20 {
		t.Fatalf("the scan only read %d files, so it is not walking the repository", scanned)
	}
	if len(leaks) > 0 {
		sort.Strings(leaks)
		t.Fatalf("these files use an identity string that internal/config/identity.go owns:\n\t%s\n\n"+
			"Refer to the constant instead, or add the file to this test's allow-list with a reason.",
			strings.Join(leaks, "\n\t"))
	}
}

// TestGoLiteralScanCatchesALeak tests the matcher directly. The walk above passes
// on a conforming tree whether or not the matcher can see anything, so the
// matcher needs its own control.
func TestGoLiteralScanCatchesALeak(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	values := identityValues(t, root)
	binary := values["BinaryName"]
	home := values["HomeDirName"]

	// The comment deliberately names the value: this asserts that prose is exempt
	// while the two literals below are not.
	body := fmt.Sprintf(`package p

// A comment naming %s is fine: prose cannot drift into behaviour.
const greeting = %q

const path = %q
`, binary, binary+" says hello", "/home/me/"+home+"/config.toml")

	leaks, err := goLiteralLeaks("internal/example/example.go", []byte(body), values, namesByLengthDesc(values))
	if err != nil {
		t.Fatalf("parsing the fixture: %v", err)
	}
	if len(leaks) != 2 {
		t.Fatalf("found %d leaks in a fixture with exactly two literals; the matcher is not seeing what it should:\n\t%s",
			len(leaks), strings.Join(leaks, "\n\t"))
	}
}

// identityValues reads the constants from identity.go.
func identityValues(t *testing.T, root string) map[string]string {
	t.Helper()
	body := readFile(t, filepath.Join(root, "internal", "config", "identity.go"))
	values := map[string]string{}
	for _, match := range identityConstRE.FindAllStringSubmatch(body, -1) {
		values[match[1]] = match[2]
	}
	if len(values) < 3 {
		t.Fatalf("read %d constants out of identity.go; if that file has been reshaped, this scan is no longer reading it: %v", len(values), values)
	}
	return values
}

// goLiteralLeaks reports the string literals in one Go file that contain an
// identity value. Comments are not visited, which is the point.
func goLiteralLeaks(rel string, body []byte, values map[string]string, ordered []string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, rel, body, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", rel, err)
	}
	var leaks []string
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		value, unquoteErr := strconv.Unquote(literal.Value)
		if unquoteErr != nil {
			return true
		}
		if name, found := containsAny(value, values, ordered); found {
			leaks = append(leaks, fmt.Sprintf("%s:%d uses the %s value in code: %q",
				rel, fset.Position(literal.Pos()).Line, name, value))
		}
		return true
	})
	return leaks, nil
}

// textLeaks does the same for a file that is not Go, where there is no way to
// tell prose from data.
func textLeaks(rel, body string, values map[string]string, ordered []string) []string {
	var leaks []string
	for lineNo, line := range strings.Split(body, "\n") {
		if name, found := containsAny(line, values, ordered); found {
			leaks = append(leaks, fmt.Sprintf("%s:%d uses the %s value: %s",
				rel, lineNo+1, name, strings.TrimSpace(line)))
		}
	}
	return leaks
}

// containsAny reports which identity value a piece of text contains, checking the
// longer values first so that `.prx` is reported as the home directory rather
// than as the binary name it also contains.
func containsAny(text string, values map[string]string, ordered []string) (string, bool) {
	for _, name := range ordered {
		if strings.Contains(text, values[name]) {
			return name, true
		}
	}
	return "", false
}

// namesByLengthDesc orders the constants so the most specific value is matched
// first.
func namesByLengthDesc(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if len(values[names[i]]) != len(values[names[j]]) {
			return len(values[names[i]]) > len(values[names[j]])
		}
		return names[i] < names[j]
	})
	return names
}

// allowed reports whether a path may name an identity string, and why.
func allowed(rel string) (string, bool) {
	if why, ok := allowedFiles[rel]; ok {
		return why, true
	}
	for prefix, why := range allowedPrefixes {
		if strings.HasPrefix(rel, prefix) {
			return why, true
		}
	}
	return "", false
}

// skippingDirs hold no source this test should read: a version-control store, the
// tool's own session state, build output, and generated fixtures.
var skippingDirs = map[string]bool{
	".git": true, ".reasonix": true, "bin": true, "dist": true,
	"testdata": true, "node_modules": true,
}
